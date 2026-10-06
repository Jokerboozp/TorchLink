package core

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"iot-platform/internal/logkey"
	"iot-platform/internal/netguard"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"iot-platform/internal/messagetopics"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
	"iot-platform/internal/sites"
)

type Engine struct {
	standardLocks [256]sync.Mutex
	stateLocks    [64]sync.Mutex
	rules         ruleCache
	protocols     protocolCache
	ingestLocks   [256]sync.Mutex
	outboxMu      sync.Mutex
	outboxWake    chan struct{}
	Repo          ports.Repository
	Archive       ports.Archive
	// Locator, when set, copies the device's site position into new alarms.
	Locator       ports.AlarmLocator
	RawStore      ports.RawMessageStore
	Bus           ports.EventBus
	Realtime      ports.RealtimePublisher
	MessageTopics *messagetopics.Service
	AI            ports.AIClient
	AIPlugins     ports.AIPluginRegistry
	AIWorkflows   ports.AIWorkflowRuntime
	// AIRuns, when set, keeps the record of every finished AI run.
	AIRuns ports.AIRunStore
	// AIConversations, when set, keeps assistant conversations per user.
	AIConversations ports.AIConversationStore
	// DeviceSignals and TelemetryStats, when set, keep and compute device
	// health signals (see ComputeDeviceSignalsOnce).
	DeviceSignals  ports.DeviceSignalStore
	TelemetryStats ports.DeviceTelemetryStats
	SignalOptions  DeviceSignalOptions
	// HarnessTokens signs MCP credentials for business runs (alarm analysis,
	// inspection, reports, protocol assistant, rule drafts) executed by Harness.
	HarnessTokens    ports.HarnessTokenIssuer
	KB               ports.KnowledgeBase
	KnowledgeReindex *KnowledgeReindexer
	Parsers          *parser.Registry
	Clock            ports.Clock
	Log              *slog.Logger
	Metrics          interface{ Inc(string) }
	ingestPaused     atomic.Bool
	// replays tracks running replays of this process; runCtx ends them when
	// the engine stops.
	replays      replayRegistry
	runCtx       context.Context
	identity     string
	identityOnce sync.Once
	// PublishExternalTopics keeps publishing parsed messages to the
	// property/event/parsed topics for external subscribers.
	PublishExternalTopics bool
	// BusinessRunTimeout bounds one business AI run (zero: 4 minutes).
	BusinessRunTimeout time.Duration
	// ChatRunTimeout is the Harness client's limit for one chat turn
	// (zero: 90 seconds); chat MCP credentials outlive it by a minute.
	ChatRunTimeout time.Duration
	// MediaOutbound limits which addresses third-party media downloads may
	// reach (zero value: public addresses only).
	MediaOutbound netguard.Policy
}

func New(repo ports.Repository, archive ports.Archive, bus ports.EventBus, realtime ports.RealtimePublisher, parsers *parser.Registry, log *slog.Logger) *Engine {
	topics := messagetopics.New(repo)
	engine := &Engine{Repo: repo, Archive: archive, Bus: topics.WrapBus(bus), Realtime: topics.WrapRealtime(realtime), MessageTopics: topics, Parsers: parsers, Clock: ports.RealClock{}, Log: log, outboxWake: make(chan struct{}, 1), PublishExternalTopics: true}
	if rawStore, ok := archive.(ports.RawMessageStore); ok {
		engine.RawStore = rawStore
	}
	engine.Locator = sites.New(repo)
	return engine
}

func (e *Engine) GetRaw(ctx context.Context, index model.RawArchiveIndex) (model.RawMessage, error) {
	if e.RawStore == nil {
		return model.RawMessage{}, errors.New("raw message store is not configured")
	}
	return e.RawStore.GetRaw(ctx, index)
}

// SetIngestPaused makes IngestRaw refuse new messages with model.ErrBackpressure
// while the processing backlog is too large.
func (e *Engine) SetIngestPaused(paused bool) { e.ingestPaused.Store(paused) }

func (e *Engine) IngestRaw(ctx context.Context, raw model.RawMessage) (model.RawArchiveIndex, bool, error) {
	if e.ingestPaused.Load() {
		return model.RawArchiveIndex{}, false, model.ErrBackpressure
	}
	raw.Normalize(e.Clock.Now())
	digest := sha256.Sum256([]byte(raw.TenantID + "\x00" + raw.MessageID))
	lock := &e.ingestLocks[digest[0]]
	lock.Lock()
	defer lock.Unlock()
	if raw.ProtocolID == "" || raw.ProtocolVersion == "" {
		if binding, err := e.productBinding(ctx, raw.TenantID, raw.ProductID); err == nil {
			raw.ProtocolID, raw.ProtocolVersion = binding.ProtocolID, binding.Version
			if release, releaseErr := e.protocolRelease(ctx, raw.TenantID, binding.ProtocolID, binding.Version); releaseErr == nil {
				raw.PointTableVersion = release.PointTableVersion
			}
		}
	}
	if err := raw.Validate(); err != nil {
		return model.RawArchiveIndex{}, false, err
	}
	if err := e.ensureGatewayChild(ctx, raw); err != nil {
		return model.RawArchiveIndex{}, false, err
	}
	// Reserving first costs one write for a new message; only a message whose
	// identity was seen before needs the archive index read.
	reserved, newReservation, reserveErr := e.Repo.ReserveRawMessage(ctx, raw)
	if errors.Is(reserveErr, model.ErrRawConflict) {
		if e.Log != nil {
			e.Log.Warn("raw message id conflict", logkey.Tenant, raw.TenantID, "messageId", raw.MessageID)
		}
		existing, _ := e.Repo.GetRawIndex(ctx, raw.TenantID, raw.MessageID)
		return existing, false, model.ErrRawConflict
	}
	if reserveErr != nil {
		return model.RawArchiveIndex{}, false, reserveErr
	}
	if !newReservation {
		existing, err := e.Repo.GetRawIndex(ctx, raw.TenantID, raw.MessageID)
		if err != nil && !errors.Is(err, model.ErrNotFound) {
			// The archive may already hold this message; archiving it again
			// after a transient read error would duplicate it. Retry instead.
			return model.RawArchiveIndex{}, false, fmt.Errorf("read reserved raw index: %w", err)
		}
		if err == nil {
			if existing.PayloadHash != raw.PayloadHash() || existing.DeviceID != raw.DeviceID || existing.ProductID != raw.ProductID {
				if e.Log != nil {
					e.Log.Warn("raw message id conflict", logkey.Tenant, raw.TenantID, "messageId", raw.MessageID)
				}
				return existing, false, model.ErrRawConflict
			}
			if existing.PublishedAt == 0 {
				stored, readErr := e.GetRaw(ctx, existing)
				if readErr != nil {
					return existing, false, fmt.Errorf("read pending raw: %w", readErr)
				}
				if publishErr := e.publishArchivedRaw(ctx, existing, stored); publishErr != nil {
					return existing, false, publishErr
				}
			}
			return existing, false, nil
		}
	}
	raw = reserved
	if e.RawStore == nil {
		return model.RawArchiveIndex{}, false, errors.New("raw message store is not configured")
	}
	idx, err := e.RawStore.PutRaw(ctx, raw)
	if err != nil {
		if e.Metrics != nil {
			e.Metrics.Inc("raw_archive_failed_total")
		}
		return idx, false, fmt.Errorf("archive raw: %w", err)
	}
	created, err := e.Repo.SaveRawIndex(ctx, idx)
	if err != nil {
		return idx, false, fmt.Errorf("index raw: %w", err)
	}
	if !created {
		existing, err := e.Repo.GetRawIndex(ctx, raw.TenantID, raw.MessageID)
		if err != nil {
			return idx, false, err
		}
		if existing.PayloadHash != raw.PayloadHash() || existing.DeviceID != raw.DeviceID || existing.ProductID != raw.ProductID {
			return existing, false, model.ErrRawConflict
		}
		return existing, false, nil
	}
	if e.Metrics != nil {
		e.Metrics.Inc("mqtt_ingest_qps")
		e.Metrics.Inc("raw_archive_success_total")
	}
	if err := e.publishArchivedRaw(ctx, idx, raw); err != nil {
		return idx, true, err
	}
	return idx, true, nil
}

func (e *Engine) publishArchivedRaw(ctx context.Context, idx model.RawArchiveIndex, raw model.RawMessage) error {
	b, _ := json.Marshal(raw)
	if err := e.Bus.Publish(ctx, model.TopicRaw, model.DeviceKey(raw.TenantID, raw.DeviceID), b); err != nil {
		_ = e.Repo.MarkRawPublished(ctx, idx.TenantID, idx.MessageID, idx.ReceivedAt, 0, err.Error())
		if e.Metrics != nil {
			e.Metrics.Inc("raw_publish_failed_total")
		}
		return fmt.Errorf("publish archived raw: %w", err)
	}
	if err := e.Repo.MarkRawPublished(ctx, idx.TenantID, idx.MessageID, idx.ReceivedAt, e.Clock.Now().UnixMilli(), ""); err != nil {
		return fmt.Errorf("mark raw published: %w", err)
	}
	return nil
}

// retryPendingRawOnce republishes archived raw messages whose queue publish
// failed. It runs as a cluster singleton job.
func (e *Engine) retryPendingRawOnce(ctx context.Context) error {
	indexes, err := e.Repo.ListPendingRawIndexes(ctx, 200)
	if err != nil {
		return fmt.Errorf("list pending raw: %w", err)
	}
	if e.Metrics != nil {
		if stalled, countErr := e.Repo.CountStalledRawIndexes(ctx); countErr == nil {
			if gauges, ok := e.Metrics.(interface{ Set(string, float64) }); ok {
				gauges.Set("raw_publish_stalled", float64(stalled))
			}
		}
	}
	for _, idx := range indexes {
		raw, readErr := e.GetRaw(ctx, idx)
		if readErr != nil {
			_ = e.Repo.MarkRawPublished(ctx, idx.TenantID, idx.MessageID, idx.ReceivedAt, 0, readErr.Error())
			continue
		}
		if publishErr := e.publishArchivedRaw(ctx, idx, raw); publishErr != nil && e.Log != nil {
			e.Log.Warn("retry pending raw", "messageId", idx.MessageID, "error", publishErr)
		}
	}
	return nil
}

func (e *Engine) ensureGatewayChild(ctx context.Context, raw model.RawMessage) error {
	if raw.GatewayID == "" || raw.DeviceID == raw.GatewayID {
		return nil
	}
	// Registration mismatches cannot be fixed by retrying the same message,
	// so they are marked permanent: the MQTT inbox quarantines them and Kafka
	// dead-letters them instead of blocking later messages behind them.
	// Repository failures stay transient and are retried.
	gateway, err := e.Repo.GetManagedDevice(ctx, raw.TenantID, raw.GatewayID)
	if errors.Is(err, model.ErrNotFound) {
		return model.Permanent(fmt.Errorf("gateway %s is not registered", raw.GatewayID))
	}
	if err != nil {
		return fmt.Errorf("load gateway %s: %w", raw.GatewayID, err)
	}
	if gateway.DeviceRole != "GATEWAY" {
		return model.Permanent(fmt.Errorf("device %s is not configured as a gateway", gateway.ID))
	}
	if raw.ProductID == "" {
		return model.Permanent(fmt.Errorf("child productId is required"))
	}
	childProduct, err := e.Repo.GetProduct(ctx, raw.TenantID, raw.ProductID)
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		return fmt.Errorf("load child product %s: %w", raw.ProductID, err)
	}
	if err != nil || childProduct.Status != "ENABLED" {
		return model.Permanent(fmt.Errorf("child product is not enabled"))
	}
	child, childErr := e.Repo.GetManagedDevice(ctx, raw.TenantID, raw.DeviceID)
	if childErr == nil {
		if child.DeviceRole != "CHILD" || child.GatewayID != gateway.ID {
			return model.Permanent(fmt.Errorf("device %s is already registered outside gateway %s", child.ID, gateway.ID))
		}
		if child.ProductID != raw.ProductID {
			return model.Permanent(fmt.Errorf("child device product does not match its registration"))
		}
		return nil
	}
	if !errors.Is(childErr, model.ErrNotFound) {
		return fmt.Errorf("load child device %s: %w", raw.DeviceID, childErr)
	}
	now := e.Clock.Now().UnixMilli()
	secret := id("child_secret")
	hash := sha256.Sum256([]byte(secret))
	child = model.ManagedDevice{
		ID:                 raw.DeviceID,
		TenantID:           raw.TenantID,
		ProductID:          raw.ProductID,
		Name:               raw.DeviceName,
		Status:             "ENABLED",
		DeviceRole:         "CHILD",
		GatewayID:          gateway.ID,
		RegistrationSource: "GATEWAY_AUTO",
		AutoRegistered:     true,
		AccessKey:          "dk_" + strings.TrimPrefix(id(""), "_"),
		SecretHash:         hex.EncodeToString(hash[:]),
		SecretHint:         "网关托管",
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if child.Name == "" {
		child.Name = "子设备 " + raw.DeviceID
	}
	if err = e.Repo.SaveManagedDevice(ctx, child); err != nil {
		return fmt.Errorf("auto-register child device: %w", err)
	}
	e.RecordAudit(ctx, model.AuditLog{TenantID: raw.TenantID, Actor: "gateway:" + gateway.ID, Action: "device.child.auto-register", TargetType: "device", TargetID: child.ID, Details: map[string]any{"gatewayId": gateway.ID, "productId": child.ProductID}, CreatedAt: now})
	return nil
}

// parseDurationBuckets bound protocol parsing times (including external Go
// workers), in seconds.
var parseDurationBuckets = []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 2, 5, 10}

func (e *Engine) handleRaw(ctx context.Context, b []byte) error {
	var raw model.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return model.Permanent(err)
	}
	var msg *model.StandardMessage
	var err error
	parseStarted := time.Now()
	protocolID, protocolVersion := raw.ProtocolID, raw.ProtocolVersion
	// Repository failures are returned for redelivery; only a missing binding,
	// release or product is a parse result recorded on the raw message.
	if protocolID == "" || protocolVersion == "" {
		binding, bindingErr := e.productBinding(ctx, raw.TenantID, raw.ProductID)
		if bindingErr == nil {
			protocolID, protocolVersion = binding.ProtocolID, binding.Version
		} else if !errors.Is(bindingErr, model.ErrNotFound) {
			return fmt.Errorf("load protocol binding of %s: %w", raw.ProductID, bindingErr)
		}
	}
	if protocolID != "" && protocolVersion != "" {
		release, releaseErr := e.protocolRelease(ctx, raw.TenantID, protocolID, protocolVersion)
		if releaseErr != nil && !errors.Is(releaseErr, model.ErrNotFound) {
			return fmt.Errorf("load protocol release %s@%s: %w", protocolID, protocolVersion, releaseErr)
		}
		if releaseErr != nil {
			err = fmt.Errorf("protocol release %s@%s not found: %w", protocolID, protocolVersion, releaseErr)
		} else if release.Status == "REVOKED" {
			err = fmt.Errorf("protocol release %s@%s is revoked", protocolID, protocolVersion)
		} else {
			raw.ProtocolID, raw.ProtocolVersion = protocolID, protocolVersion
			if raw.PointTableVersion == "" {
				raw.PointTableVersion = release.PointTableVersion
			}
			msg, err = e.Parsers.ParseWithConfigContext(ctx, release.ParserType, release.Config, raw)
		}
	}
	product, productErr := e.cachedProduct(ctx, raw.TenantID, raw.ProductID)
	if productErr != nil && !errors.Is(productErr, model.ErrNotFound) {
		return fmt.Errorf("load product %s: %w", raw.ProductID, productErr)
	}
	if msg == nil && err == nil && productErr == nil && product.ProtocolPackageID != "" {
		pkg, pkgErr := e.Repo.GetProtocolPackage(ctx, raw.TenantID, product.ProtocolPackageID)
		if pkgErr != nil && !errors.Is(pkgErr, model.ErrNotFound) {
			return fmt.Errorf("load protocol package %s: %w", product.ProtocolPackageID, pkgErr)
		}
		if pkgErr == nil && pkg.Status == "PUBLISHED" {
			msg, err = e.Parsers.ParseVersionWithConfigContext(ctx, pkg.ParserType, raw.ParserVersion, pkg.Config, raw)
		}
	}
	if msg == nil && err == nil {
		msg, err = e.Parsers.Parse(raw)
	}
	if h, ok := e.Metrics.(interface {
		ObserveIn(string, []float64, float64)
	}); ok {
		h.ObserveIn("parse_duration_seconds", parseDurationBuckets, time.Since(parseStarted).Seconds())
	}
	if err == nil && msg != nil {
		_, err = model.MessageComponents(*msg)
		if productErr == nil {
			e.markThingQuality(product, msg)
		}
		if raw.Source == "external-data" {
			if msg.Tags == nil {
				msg.Tags = map[string]string{}
			}
			for _, name := range []string{"externalEventKey", "externalSourceId", "externalEventId", "externalVideoEvent"} {
				if value, ok := raw.Metadata[name].(string); ok {
					msg.Tags[name] = value
				}
			}
		}
	}
	parseError := ""
	if err != nil {
		parseError = err.Error()
		if len(parseError) > 512 {
			parseError = parseError[:512]
		}
	}
	if storeErr := e.Repo.MarkRawParseResult(ctx, raw.TenantID, raw.MessageID, raw.ReceivedAt, e.Clock.Now().UnixMilli(), parseError); storeErr != nil {
		return storeErr
	}
	if err != nil {
		if e.Metrics != nil {
			e.Metrics.Inc("parse_failed_total")
		}
		if e.Log != nil {
			e.Log.Warn("raw message was not forwarded because parsing failed", "messageId", raw.MessageID, logkey.Device, raw.DeviceID, "error", err)
		}
		// A parse failure is deliberately terminal for the forwarding path. The
		// raw payload is already archived and remains available for replay, but
		// no parsed Kafka or MQTT message is emitted.
		return nil
	}
	if e.Metrics != nil {
		e.Metrics.Inc("parse_success_total")
	}
	out, _ := json.Marshal(msg)
	// The business stream is published first: a redelivered raw message
	// republishes it, and the processor's claim makes that idempotent.
	key := model.DeviceKey(msg.TenantID, msg.DeviceID)
	if err := e.Bus.Publish(ctx, model.TopicDeviceBusiness, key, out); err != nil {
		return fmt.Errorf("publish parsed message to device business stream: %w", err)
	}
	if e.PublishExternalTopics {
		topic := model.TopicParsed
		switch msg.MessageType {
		case model.EventReport, model.AlarmReport:
			topic = model.TopicEventReport
		case model.PropertyReport:
			topic = model.TopicPropertyReport
		}
		if err := e.Bus.Publish(ctx, topic, key, out); err != nil {
			return fmt.Errorf("publish parsed message to kafka topic %s: %w", topic, err)
		}
	}
	if e.Realtime == nil {
		return nil
	}
	if err := e.Realtime.Publish(ctx, msg.MQTTTopic(), out, 1, false); err != nil {
		if e.Metrics != nil {
			e.Metrics.Inc("parsed_mqtt_publish_failed_total")
		}
		return fmt.Errorf("publish parsed message to mqtt: %w", err)
	}
	return nil
}

func (e *Engine) handleStandard(ctx context.Context, b []byte) error {
	var msg model.StandardMessage
	if err := json.Unmarshal(b, &msg); err != nil {
		return model.Permanent(err)
	}
	digest := sha256.Sum256([]byte(msg.TenantID + "\x00" + msg.DeviceID))
	serial := &e.standardLocks[digest[0]]
	serial.Lock()
	defer serial.Unlock()
	components, err := model.MessageComponents(msg)
	if err != nil {
		return model.Permanent(err)
	}
	claim, err := e.claimStandard(ctx, msg)
	if err != nil {
		return err
	}
	if !claim.ShouldProcess {
		return nil
	}
	if msg.MessageType == model.CommandReply && msg.Parser == parser.StandardParserName {
		if id, ok := msg.Event["commandId"].(string); ok {
			if err := e.Repo.CompleteDeviceCommand(ctx, msg.TenantID, msg.DeviceID, id, msg.Event, e.Clock.Now().UnixMilli()); err != nil {
				return err
			}
		}
	}
	rules, err := e.tenantRules(ctx, msg.TenantID) /* 短时缓存，避免每条消息读取全部规则。 */
	if err != nil {
		return err
	}
	ruleAlarmHandled := false
	externalAlarmIDs := []string{}
	for _, rule := range rules {
		// Rules of other products can neither raise nor recover this device's
		// alarms; skip their per-message queries.
		if !ruleCovers(rule, msg) {
			continue
		}
		if MatchRule(rule, msg) {
			if rule.DurationSeconds > 0 {
				satisfied, durationErr := e.durationSatisfied(ctx, rule, msg)
				if durationErr != nil {
					return durationErr
				}
				if !satisfied {
					continue
				}
			}
			ruleAlarmHandled = true
			if alarm, _, err := e.raiseRuleAlarm(ctx, rule, msg); err != nil {
				return err
			} else {
				externalAlarmIDs = append(externalAlarmIDs, alarm.ID)
			}
		} else if MatchConditions(rule.Recovery, msg) {
			if err := e.clearDuration(ctx, rule, msg); err != nil {
				return err
			}
			if err := e.recoverRuleAlarm(ctx, rule, msg); err != nil {
				return err
			}
		} else {
			if err := e.clearDuration(ctx, rule, msg); err != nil {
				return err
			}
		}
	}
	// An ALARM_REPORT is already an assertion made by the device. Rules can
	// classify it and trigger actions when they match, but a missing rule must
	// never discard a device-originated alarm.
	if len(components) > 0 {
		ids, err := e.applyComponentAlarms(ctx, msg, components)
		if err != nil {
			return err
		}
		externalAlarmIDs = append(externalAlarmIDs, ids...)
	}
	if len(components) == 0 && msg.MessageType == model.AlarmReport && !ruleAlarmHandled {
		if alarm, _, err := e.raiseDirectAlarm(ctx, msg); err != nil {
			return err
		} else {
			externalAlarmIDs = append(externalAlarmIDs, alarm.ID)
		}
	}
	if len(components) == 0 && msg.MessageType != model.AlarmReport && directAlarmCleared(msg) {
		if err := e.recoverDirectAlarms(ctx, msg); err != nil {
			return err
		}
	}
	if err := e.saveExternalDelivery(ctx, msg, externalAlarmIDs); err != nil {
		return err
	}
	return e.completeStandard(ctx, msg, claim.Token)
}

// completeStandard writes the message's final device state and marks it
// processed together: one read of state and open alarms, then one
// version-checked statement. A conflicting state write is retried; if the
// claim was taken over meanwhile the new owner records completion.
func (e *Engine) completeStandard(ctx context.Context, msg model.StandardMessage, token int64) error {
	unlock := e.lockDeviceState(msg.TenantID, msg.DeviceID)
	defer unlock()
	for attempt := 0; attempt < casAttempts; attempt++ {
		current, open, err := e.Repo.LoadDeviceStateWithAlarms(ctx, msg.TenantID, msg.DeviceID)
		found := err == nil
		if err != nil && !errors.Is(err, model.ErrNotFound) {
			return err
		}
		if !found {
			current = model.DeviceState{}
		}
		next := current
		if !found {
			next.ReportIntervalSec, next.OfflineToleranceSec = e.deviceTiming(ctx, msg.TenantID, msg.ProductID, msg.DeviceID)
		}
		var write *model.DeviceState
		if nextMessageState(&next, found, msg, true, open) {
			next.TenantID, next.DeviceID, next.Version = msg.TenantID, msg.DeviceID, current.Version
			write = &next
		}
		ok, err := e.Repo.CompleteStandardMessage(ctx, write, msg.TenantID, msg.MessageID, token)
		if ok && write != nil {
			after := next
			after.Version++
			e.publishStateChange(ctx, current, after)
		}
		switch {
		case errors.Is(err, model.ErrStaleClaim):
			// Our lease expired and another worker took the message over; it
			// records completion. Side effects are idempotent per trigger
			// identity and state writes are version-checked.
			e.count("standard_claim_fenced_total")
			if e.Log != nil {
				e.Log.Warn("standard message claim was taken over; completion left to the new owner", "messageId", msg.MessageID)
			}
			return nil
		case err != nil:
			return err
		case ok:
			return nil
		}
		e.count("device_state_conflict_total")
		backoff(ctx, attempt)
	}
	return fmt.Errorf("device %s state: %w", msg.DeviceID, model.ErrConcurrentUpdate)
}

func tag(m model.StandardMessage, k, fallback string) string {
	if v := m.Tags[k]; v != "" {
		return v
	}
	return fallback
}

func id(prefix string) string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return prefix + "_" + hex.EncodeToString(b)
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }
