package core

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"iot-platform/internal/netguard"
	"log/slog"
	"slices"
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

const directAlarmRulePrefix = "device-report:"

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
	identity         string
	identityOnce     sync.Once
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
			e.Log.Warn("raw message id conflict", "tenantId", raw.TenantID, "messageId", raw.MessageID)
		}
		existing, _ := e.Repo.GetRawIndex(ctx, raw.TenantID, raw.MessageID)
		return existing, false, model.ErrRawConflict
	}
	if reserveErr != nil {
		return model.RawArchiveIndex{}, false, reserveErr
	}
	if !newReservation {
		if existing, err := e.Repo.GetRawIndex(ctx, raw.TenantID, raw.MessageID); err == nil {
			if existing.PayloadHash != raw.PayloadHash() || existing.DeviceID != raw.DeviceID || existing.ProductID != raw.ProductID {
				if e.Log != nil {
					e.Log.Warn("raw message id conflict", "tenantId", raw.TenantID, "messageId", raw.MessageID)
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
		_ = e.Repo.MarkRawPublished(ctx, idx.TenantID, idx.MessageID, 0, err.Error())
		if e.Metrics != nil {
			e.Metrics.Inc("raw_publish_failed_total")
		}
		return fmt.Errorf("publish archived raw: %w", err)
	}
	if err := e.Repo.MarkRawPublished(ctx, idx.TenantID, idx.MessageID, e.Clock.Now().UnixMilli(), ""); err != nil {
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
			_ = e.Repo.MarkRawPublished(ctx, idx.TenantID, idx.MessageID, 0, readErr.Error())
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
	e.RecordAudit(ctx, model.AuditLog{ID: id("audit"), TenantID: raw.TenantID, Actor: "gateway:" + gateway.ID, Action: "device.child.auto-register", TargetType: "device", TargetID: child.ID, Details: map[string]any{"gatewayId": gateway.ID, "productId": child.ProductID}, CreatedAt: now})
	return nil
}
func (e *Engine) handleRaw(ctx context.Context, b []byte) error {
	var raw model.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return model.Permanent(err)
	}
	var msg *model.StandardMessage
	var err error
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
			msg, err = e.Parsers.ParseWithConfig(release.ParserType, release.Config, raw)
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
			msg, err = e.Parsers.ParseVersionWithConfig(pkg.ParserType, raw.ParserVersion, pkg.Config, raw)
		}
	}
	if msg == nil && err == nil {
		msg, err = e.Parsers.Parse(raw)
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
	if storeErr := e.Repo.MarkRawParseResult(ctx, raw.TenantID, raw.MessageID, e.Clock.Now().UnixMilli(), parseError); storeErr != nil {
		return storeErr
	}
	if err != nil {
		if e.Metrics != nil {
			e.Metrics.Inc("parse_failed_total")
		}
		if e.Log != nil {
			e.Log.Warn("raw message was not forwarded because parsing failed", "messageId", raw.MessageID, "deviceId", raw.DeviceID, "error", err)
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

// clearDuration forgets a duration rule's first match; rules without a
// duration never store one, so they cost no query per message.
func (e *Engine) clearDuration(ctx context.Context, rule model.AlarmRule, msg model.StandardMessage) error {
	if rule.DurationSeconds <= 0 {
		return nil
	}
	return e.Repo.DeleteRulePending(ctx, rule.TenantID, rule.ID, msg.DeviceID)
}
func (e *Engine) durationSatisfied(ctx context.Context, rule model.AlarmRule, msg model.StandardMessage) (bool, error) {
	now := e.Clock.Now().Unix()
	since, found, err := e.Repo.GetRulePending(ctx, rule.TenantID, rule.ID, msg.DeviceID)
	if err != nil {
		return false, err
	}
	if !found {
		return false, e.Repo.SaveRulePending(ctx, rule.TenantID, rule.ID, msg.DeviceID, now)
	}
	return now-since >= rule.DurationSeconds, nil
}
func (e *Engine) touchState(ctx context.Context, msg model.StandardMessage) error {
	return e.applyMessageState(ctx, msg, false)
}

// Reconcile after alarm processing so one message persists only its final
// state. Raw archive/claim/alarm ordering and the processed marker are retained.
func (e *Engine) applyMessageState(ctx context.Context, msg model.StandardMessage, reconcile bool) error {
	unlock := e.lockDeviceState(msg.TenantID, msg.DeviceID)
	defer unlock()
	before, after, written, err := e.mutateDeviceState(ctx, msg.TenantID, msg.DeviceID, func(state *model.DeviceState, found bool) (bool, error) {
		open := false
		if reconcile {
			var err error
			if open, err = e.Repo.HasOpenAlarm(ctx, msg.TenantID, msg.DeviceID); err != nil {
				return false, err
			}
		}
		if !found {
			state.ReportIntervalSec, state.OfflineToleranceSec = e.deviceTiming(ctx, msg.TenantID, msg.ProductID, msg.DeviceID)
		}
		return nextMessageState(state, found, msg, reconcile, open), nil
	})
	if err == nil && written {
		e.publishStateChange(ctx, before, after)
	}
	return err
}

// nextMessageState applies one processed message to the device state and
// reports whether it must be written. With reconcile the business status
// follows the device's open alarms.
func nextMessageState(state *model.DeviceState, found bool, msg model.StandardMessage, reconcile, open bool) bool {
	if !found {
		interval, tolerance := state.ReportIntervalSec, state.OfflineToleranceSec
		if interval <= 0 {
			interval, tolerance = model.DefaultReportIntervalSec, model.DefaultOfflineToleranceSec
		}
		*state = model.DeviceState{TenantID: msg.TenantID, ProductID: msg.ProductID, DeviceID: msg.DeviceID, ReportIntervalSec: interval, OfflineToleranceSec: tolerance, ConnectionStatus: "UNKNOWN"}
	}
	// Late retransmissions remain archived but must not roll back current state.
	late := msg.Timestamp < state.LastSeenAt
	if late && !reconcile {
		return false
	}
	previous := *state
	old := state.BusinessStatus
	state.DataStatus = "ACTIVE"
	if msg.MessageType == model.AlarmReport || strings.EqualFold(old, "ALARM") {
		state.BusinessStatus = "ALARM"
	} else {
		state.BusinessStatus = "ONLINE"
	}
	state.LastSeenAt = msg.Timestamp
	state.LastMessageID = msg.MessageID
	state.StatusSource = "RAW_MESSAGE"
	if msg.MessageType == model.StateChange && msg.Parser == parser.StandardParserName {
		if status, ok := msg.Properties["connectionStatus"].(string); ok && (status == "CONNECTED" || status == "DISCONNECTED" || status == "UNKNOWN") {
			state.ConnectionStatus = status
			if status == "CONNECTED" {
				state.LastConnectAt = msg.Timestamp
			} else if status == "DISCONNECTED" {
				state.LastDisconnectAt = msg.Timestamp
			}
		}
	}
	if late {
		*state = previous
	}
	if reconcile {
		if open {
			state.BusinessStatus = "ALARM"
			state.StatusSource = "ACTIVE_ALARM"
			state.Reason = "存在活动告警"
		} else if !late || state.BusinessStatus == "ALARM" {
			state.BusinessStatus = "ONLINE"
			state.StatusSource = "RAW_MESSAGE"
			state.Reason = ""
		}
	}
	return true
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

func (e *Engine) syncDeviceBusinessStatus(ctx context.Context, tenant, product, device string) error {
	unlock := e.lockDeviceState(tenant, device)
	defer unlock()
	before, after, written, err := e.mutateDeviceState(ctx, tenant, device, func(state *model.DeviceState, found bool) (bool, error) {
		if !found {
			return false, nil
		}
		open, err := e.hasOpenAlarm(ctx, tenant, device)
		if err != nil {
			return false, err
		}
		nextStatus, source, reason := "ONLINE", "RAW_MESSAGE", ""
		if open {
			nextStatus, source, reason = "ALARM", "ACTIVE_ALARM", "存在活动告警"
		}
		if state.ProductID == "" {
			state.ProductID = product
		}
		state.BusinessStatus, state.StatusSource, state.Reason = nextStatus, source, reason
		return true, nil
	})
	if err == nil && written {
		e.publishStateChange(ctx, before, after)
	}
	return err
}

func (e *Engine) hasOpenAlarm(ctx context.Context, tenant, device string) (bool, error) {
	return e.Repo.HasOpenAlarm(ctx, tenant, device)
}

func (e *Engine) raiseDirectAlarm(ctx context.Context, msg model.StandardMessage) (model.Alarm, bool, error) {
	now := e.Clock.Now().UnixMilli()
	alarmType, level := directAlarmMetadata(msg)
	a := model.Alarm{
		ID: id("alarm"), TenantID: msg.TenantID, RuleID: directAlarmRuleID(alarmType), TriggerID: msg.MessageID,
		DeviceID: msg.DeviceID, DeviceName: e.alarmDeviceName(ctx, msg.TenantID, msg.DeviceID), AlarmType: alarmType, AlarmLevel: level, Status: "ACTIVE", Source: "device",
		CityCode: tag(msg, "cityCode", "unknown"), DistrictCode: tag(msg, "districtCode", "unknown"),
		BuildingID: tag(msg, "buildingId", "unknown"), DeviceType: tag(msg, "deviceType", msg.ProductID),
		AreaID: tag(msg, "areaId", ""), FirstTriggeredAt: now, LastTriggeredAt: now, TriggerCount: 1,
		Content: alarmContent(msg), Details: map[string]any{"message": msg, "direct": true},
	}
	a.Cameras, _ = e.ListCameraSummaries(ctx, msg.TenantID, msg.DeviceID)
	a.Location = e.alarmLocation(ctx, msg.TenantID, msg.DeviceID, "")
	saved, created, _, err := e.upsertReportedAlarm(ctx, a, msg)
	if err != nil {
		return saved, false, err
	}
	if created {
		if e.Metrics != nil {
			e.Metrics.Inc("alarm_trigger_total")
		}
		payload, _ := json.Marshal(saved)
		e.publishEvent(ctx, model.TopicAlarmRaised, saved.ID, saved.MQTTTopic("raised"), payload)
	}
	e.flushOutbox(ctx)
	return saved, created, nil
}

func directAlarmRuleID(alarmType string) string {
	return directAlarmRulePrefix + alarmType
}

func (e *Engine) alarmLocation(ctx context.Context, tenant, deviceID, componentID string) *model.AlarmLocation {
	if e.Locator == nil {
		return nil
	}
	return e.Locator.AlarmLocation(ctx, tenant, deviceID, componentID)
}

func (e *Engine) alarmDeviceName(ctx context.Context, tenantID, deviceID string) string {
	device, err := e.Repo.GetManagedDevice(ctx, tenantID, deviceID)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(device.Name)
}

func firstMessageValue(msg model.StandardMessage, keys ...string) any {
	for _, key := range keys {
		if value, ok := messageValue(msg, key); ok {
			return value
		}
	}
	return nil
}

func messageValue(msg model.StandardMessage, key string) (any, bool) {
	for _, source := range directMessageSources(msg) {
		if value, ok := source[key]; ok {
			return value, true
		}
	}
	return nil, false
}

func directMessageSources(msg model.StandardMessage) []map[string]any {
	sources := []map[string]any{msg.Properties, msg.Event, msg.Raw}
	if payload := messageMap(msg.Raw["payload"]); payload != nil {
		sources = append(sources, payload)
		if alarm := messageMap(payload["alarm"]); alarm != nil {
			sources = append(sources, alarm)
		}
	}
	if alarm := messageMap(msg.Event["alarm"]); alarm != nil {
		sources = append(sources, alarm)
	}
	return sources
}

func messageMap(value any) map[string]any {
	switch item := value.(type) {
	case map[string]any:
		return item
	case json.RawMessage:
		var out map[string]any
		if json.Unmarshal(item, &out) == nil {
			return out
		}
	case []byte:
		var out map[string]any
		if json.Unmarshal(item, &out) == nil {
			return out
		}
	case string:
		var out map[string]any
		if json.Unmarshal([]byte(item), &out) == nil {
			return out
		}
	}
	return nil
}

func messageFlag(msg model.StandardMessage, key string) bool {
	value, ok := messageValue(msg, key)
	return ok && truthy(value)
}

func truthy(value any) bool {
	switch item := value.(type) {
	case bool:
		return item
	case float64:
		return item != 0
	case float32:
		return item != 0
	case int:
		return item != 0
	case int64:
		return item != 0
	case uint:
		return item != 0
	case uint64:
		return item != 0
	case string:
		return strings.EqualFold(strings.TrimSpace(item), "true") || strings.TrimSpace(item) == "1" || strings.EqualFold(strings.TrimSpace(item), "yes") || strings.EqualFold(strings.TrimSpace(item), "on")
	default:
		return false
	}
}

func normalizeAlarmToken(value any) string {
	if value == nil {
		return ""
	}
	text := strings.ToUpper(strings.TrimSpace(fmt.Sprint(value)))
	if text == "" || text == "<NIL>" || text == "TRUE" || text == "FALSE" {
		return ""
	}
	text = strings.NewReplacer(" ", "_", "-", "_").Replace(text)
	var out strings.Builder
	for _, r := range text {
		if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '.' {
			out.WriteRune(r)
		}
	}
	normalized := strings.Trim(out.String(), "_.")
	if runes := []rune(normalized); len(runes) > 64 {
		normalized = string(runes[:64])
	}
	return normalized
}

// maxAlarmContent bounds the description copied from a device report.
const maxAlarmContent = 500

// alarmContent is the description a device reported with the alarm; when the
// report has none, the first non-empty fallback (such as the rule
// description or name) is used.
func alarmContent(msg model.StandardMessage, fallbacks ...string) string {
	for _, key := range []string{"content", "alarmContent", "alarm_content", "description", "alarmDesc", "alarm_desc"} {
		value, ok := messageValue(msg, key)
		if !ok {
			continue
		}
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			return truncateRunes(strings.TrimSpace(text), maxAlarmContent)
		}
	}
	for _, text := range fallbacks {
		if text = strings.TrimSpace(text); text != "" {
			return truncateRunes(text, maxAlarmContent)
		}
	}
	return ""
}

func truncateRunes(text string, limit int) string {
	if runes := []rune(text); len(runes) > limit {
		return string(runes[:limit])
	}
	return text
}

func directAlarmMetadata(msg model.StandardMessage) (string, string) {
	alarmType := normalizeAlarmToken(firstMessageValue(msg, "alarmType", "alarm_type"))
	if alarmType == "" {
		switch {
		case messageFlag(msg, "fireAlarm"):
			alarmType = "FIRE"
		case messageFlag(msg, "smoke") || messageFlag(msg, "smokeDetected"):
			alarmType = "SMOKE_DETECTED"
		case messageFlag(msg, "fault") || messageFlag(msg, "powerFault") || messageFlag(msg, "sensorFault"):
			alarmType = "DEVICE_FAULT"
		case messageFlag(msg, "offline"):
			alarmType = "DEVICE_OFFLINE"
		default:
			alarmType = "MANUAL_ALARM"
		}
	}
	level := normalizeAlarmToken(firstMessageValue(msg, "alarmLevel", "alarm_level", "level"))
	switch level {
	case "CRITICAL", "HIGH", "MEDIUM", "LOW", "INFO":
	default:
		level = "HIGH"
	}
	return alarmType, level
}

func directAlarmCleared(msg model.StandardMessage) bool {
	// Old immutable protocol releases expose aggregate objects without a safe
	// component identity contract. Never guess a whole-controller recovery.
	if msg.Event["type"] == "COMPONENT_STATUS" {
		if _, exists := msg.Event["objects"]; exists {
			return false
		}
	}
	if msg.MessageType != model.PropertyReport && msg.MessageType != model.StateChange {
		return false
	}
	found := false
	for _, key := range []string{"alarm", "fireAlarm", "smoke", "smokeDetected", "fault", "offline", "powerFault", "openCircuit", "shortCircuit", "removed", "sensorFault", "upgradeFault"} {
		value, ok := messageValue(msg, key)
		if !ok {
			continue
		}
		found = true
		if truthy(value) {
			return false
		}
	}
	return found
}

func (e *Engine) recoverDirectAlarms(ctx context.Context, msg model.StandardMessage) error {
	for _, status := range []string{"ACTIVE", "ACKED"} {
		alarms, err := e.Repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: msg.TenantID, DeviceID: msg.DeviceID, Status: status, Limit: 100})
		if err != nil {
			return err
		}
		for _, listed := range alarms {
			if listed.ComponentID != "" || strings.Contains(listed.RuleID, ":external:") || !strings.HasPrefix(listed.RuleID, directAlarmRulePrefix) || !directAlarmTypeCleared(msg, listed.AlarmType) {
				continue
			}
			alarm, written, err := e.mutateAlarm(ctx, listed.TenantID, listed.ID, func(a *model.Alarm) (bool, error) {
				if a.Status != "ACTIVE" && a.Status != "ACKED" {
					return false, nil
				}
				a.Status = "RECOVERED"
				a.RecoveredAt = e.Clock.Now().UnixMilli()
				return true, nil
			})
			if err != nil {
				return err
			}
			if written {
				payload := mustJSON(alarm)
				e.publishEvent(ctx, model.TopicAlarmRecovered, alarm.ID, alarm.MQTTTopic("recovered"), payload)
			}
		}
	}
	return nil
}
func (e *Engine) raiseRuleAlarm(ctx context.Context, rule model.AlarmRule, msg model.StandardMessage) (model.Alarm, bool, error) {
	now := e.Clock.Now().UnixMilli()
	a := model.Alarm{ID: id("alarm"), TenantID: msg.TenantID, RuleID: rule.ID, TriggerID: msg.MessageID, DeviceID: msg.DeviceID, DeviceName: e.alarmDeviceName(ctx, msg.TenantID, msg.DeviceID), AlarmType: rule.AlarmType, AlarmLevel: rule.Level, Status: "ACTIVE", Source: "device", CityCode: tag(msg, "cityCode", "unknown"), DistrictCode: tag(msg, "districtCode", "unknown"), BuildingID: tag(msg, "buildingId", "unknown"), DeviceType: tag(msg, "deviceType", msg.ProductID), AreaID: tag(msg, "areaId", ""), FirstTriggeredAt: now, LastTriggeredAt: now, TriggerCount: 1, Content: alarmContent(msg, rule.Description, rule.Name), Details: map[string]any{"message": msg, "ruleName": rule.Name}}
	a.Cameras, _ = e.ListCameraSummaries(ctx, msg.TenantID, msg.DeviceID)
	a.Location = e.alarmLocation(ctx, msg.TenantID, msg.DeviceID, "")
	saved, created, reportChanged, err := e.upsertReportedAlarm(ctx, a, msg)
	if err != nil {
		return saved, false, err
	}
	if created {
		if e.Metrics != nil {
			e.Metrics.Inc("alarm_trigger_total")
		}
		payload, _ := json.Marshal(saved)
		e.publishEvent(ctx, model.TopicAlarmRaised, saved.ID, saved.MQTTTopic("raised"), payload)
	}
	e.flushOutbox(ctx)
	// Alarm records are deduplicated while ACTIVE/ACKED, but a new matching
	// message must still execute the rule actions. Exact duplicate messages
	// keep the original trigger ID and must not execute actions twice.
	if reportChanged {
		for _, action := range rule.Actions {
			event := model.UIActionEvent{ID: id("ui_action"), TenantID: msg.TenantID, RuleID: rule.ID, AlarmID: saved.ID, DeviceID: msg.DeviceID, Action: action, TriggeredAt: now}
			actionPayload, _ := json.Marshal(event)
			e.publishEvent(ctx, model.TopicUIAction, event.ID, fmt.Sprintf("/iot/ui-action/%s", msg.TenantID), actionPayload)
		}
	}
	return saved, created, nil
}
func (e *Engine) recoverRuleAlarm(ctx context.Context, rule model.AlarmRule, msg model.StandardMessage) error {
	// Acknowledged alarms are still open: a recovery report must close them
	// the same way as direct and component alarms, or the device stays ALARM.
	for _, status := range []string{"ACTIVE", "ACKED"} {
		alarms, err := e.Repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: msg.TenantID, DeviceID: msg.DeviceID, Status: status, Limit: 100})
		if err != nil {
			return err
		}
		for _, listed := range alarms {
			if listed.RuleID != rule.ID {
				continue
			}
			a, written, err := e.mutateAlarm(ctx, listed.TenantID, listed.ID, func(a *model.Alarm) (bool, error) {
				if a.Status != "ACTIVE" && a.Status != "ACKED" {
					return false, nil
				}
				a.Status = "RECOVERED"
				a.RecoveredAt = e.Clock.Now().UnixMilli()
				return true, nil
			})
			if err != nil {
				return err
			}
			if written {
				payload, _ := json.Marshal(a)
				e.publishEvent(ctx, model.TopicAlarmRecovered, a.ID, a.MQTTTopic("recovered"), payload)
			}
		}
	}
	return nil
}

// DeleteRule removes a rule and closes any alarms that can no longer be
// recovered by the deleted rule. Historical alarm rows are retained.
func (e *Engine) DeleteRule(ctx context.Context, tenant, ruleID string) error {
	if err := e.Repo.DeleteRule(ctx, tenant, ruleID); err != nil {
		return err
	}
	e.RulesChanged(tenant)
	if err := e.Repo.DeleteRulePendings(ctx, tenant, ruleID); err != nil {
		return err
	}
	return e.closeRuleAlarms(ctx, tenant, ruleID)
}

// DisableRule clears duration state and closes active/acknowledged alarms
// before a rule is switched off. Historical alarm rows remain available.
func (e *Engine) DisableRule(ctx context.Context, tenant, ruleID string) error {
	defer e.RulesChanged(tenant)
	if err := e.Repo.DeleteRulePendings(ctx, tenant, ruleID); err != nil {
		return err
	}
	return e.closeRuleAlarms(ctx, tenant, ruleID)
}

func (e *Engine) closeRuleAlarms(ctx context.Context, tenant, ruleID string) error {
	const batchSize = 1000
	affectedDevices := make(map[string]struct{})
	for _, status := range []string{"ACTIVE", "ACKED"} {
		offset := 0
		for {
			alarms, err := e.Repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: tenant, Status: status, Limit: batchSize, Offset: offset})
			if err != nil {
				return err
			}
			if len(alarms) == 0 {
				break
			}
			changed := false
			for _, listed := range alarms {
				if listed.RuleID != ruleID && !strings.HasPrefix(listed.RuleID, ruleID+":external:") {
					continue
				}
				alarm, written, err := e.mutateAlarm(ctx, listed.TenantID, listed.ID, func(a *model.Alarm) (bool, error) {
					if a.Status != "ACTIVE" && a.Status != "ACKED" {
						return false, nil
					}
					a.Status = "RECOVERED"
					a.RecoveredAt = e.Clock.Now().UnixMilli()
					return true, nil
				})
				if err != nil {
					return err
				}
				if !written {
					continue
				}
				affectedDevices[alarm.DeviceID] = struct{}{}
				payload := mustJSON(alarm)
				e.publishEvent(ctx, model.TopicAlarmRecovered, alarm.ID, alarm.MQTTTopic("recovered"), payload)
				changed = true
			}
			if changed {
				// Updating rows removes them from the status-filtered result set;
				// restart at zero so offset pagination cannot skip the next row.
				offset = 0
				continue
			}
			offset += len(alarms)
		}
	}
	for deviceID := range affectedDevices {
		if err := e.syncDeviceBusinessStatus(ctx, tenant, "", deviceID); err != nil {
			return err
		}
	}
	return nil
}
func (e *Engine) handleState(ctx context.Context, b []byte) error {
	var state model.DeviceState
	if err := json.Unmarshal(b, &state); err == nil && state.DeviceID != "" {
		return e.UpdateDeviceState(ctx, state)
	}
	var msg model.StandardMessage
	if err := json.Unmarshal(b, &msg); err != nil {
		return model.Permanent(err)
	}
	return e.touchState(ctx, msg)
}
func (e *Engine) UpdateDeviceState(ctx context.Context, state model.DeviceState) error {
	unlock := e.lockDeviceState(state.TenantID, state.DeviceID)
	defer unlock()
	return e.updateDeviceState(ctx, state)
}

// updateDeviceState replaces the stored state with state (keeping the last
// seen time when state has none), version-checked against concurrent writers.
func (e *Engine) updateDeviceState(ctx context.Context, state model.DeviceState) error {
	before, after, written, err := e.mutateDeviceState(ctx, state.TenantID, state.DeviceID, func(current *model.DeviceState, _ bool) (bool, error) {
		next := state
		if next.LastSeenAt == 0 {
			next.LastSeenAt = current.LastSeenAt
		}
		*current = next
		return true, nil
	})
	if err == nil && written {
		e.publishStateChange(ctx, before, after)
	}
	return err
}

// ScanOffline marks silent devices offline. Each device is re-evaluated on
// its freshest state inside a version-checked write, so a report that arrived
// after the listing is never overwritten by the stale snapshot.
func (e *Engine) ScanOffline(ctx context.Context) error {
	now := e.Clock.Now().UnixMilli()
	for ctx.Err() == nil {
		// Only devices whose check time has passed are read, in batches.
		states, err := e.Repo.ListOfflineDue(ctx, now, offlineScanBatch)
		if err != nil {
			return err
		}
		for _, listed := range states {
			if ctx.Err() != nil {
				break
			}
			unlock := e.lockDeviceState(listed.TenantID, listed.DeviceID)
			before, after, written, err := e.mutateDeviceState(ctx, listed.TenantID, listed.DeviceID, func(s *model.DeviceState, found bool) (bool, error) {
				check := s.OfflineCheckAt()
				if !found || check == 0 || check >= now {
					return false, nil
				}
				s.DataStatus, s.BusinessStatus = "SILENT", s.OfflineStatus()
				s.OfflineAt = s.OfflineDeadline()
				s.OfflineDetectedAt = now
				s.StatusSource = "RAW_MESSAGE_TIMEOUT"
				return true, nil
			})
			unlock()
			if err == nil && written {
				e.publishStateChange(ctx, before, after)
			}
		}
		if len(states) < offlineScanBatch {
			return nil
		}
	}
	return ctx.Err()
}

const offlineScanBatch = 1000

// ListCameraSummaries resolves the cameras associated with one device without
// exposing stream URLs or vendor credentials. The relation is intentionally
// one camera -> one device; a device may return many camera summaries.
func (e *Engine) ListCameraSummaries(ctx context.Context, tenant, deviceID string) ([]model.CameraSummary, error) {
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(deviceID) == "" {
		return nil, nil
	}
	relations, err := e.Repo.ListVideoCameraRelationsByTarget(ctx, tenant, "device", deviceID)
	if err != nil {
		return nil, err
	}
	out := make([]model.CameraSummary, 0, len(relations))
	seen := make(map[string]struct{}, len(relations))
	for _, relation := range relations {
		if relation.CameraID == "" {
			continue
		}
		if _, ok := seen[relation.CameraID]; ok {
			continue
		}
		mapping, getErr := e.Repo.GetVideoCameraMapping(ctx, tenant, relation.CameraID)
		if getErr != nil {
			return nil, getErr
		}
		out = append(out, cameraSummary(mapping))
		seen[relation.CameraID] = struct{}{}
	}
	return out, nil
}

// ListCameraSummariesForDevices resolves a list page with one batched relation
// and mapping lookup, preserving the same safe fields as ListCameraSummaries.
func (e *Engine) ListCameraSummariesForDevices(ctx context.Context, tenant string, deviceIDs []string) (map[string][]model.CameraSummary, error) {
	out := make(map[string][]model.CameraSummary, len(deviceIDs))
	if len(deviceIDs) == 0 {
		return out, nil
	}
	mappings, err := e.Repo.ListVideoCameraMappingsByDeviceIDs(ctx, tenant, deviceIDs)
	if err != nil {
		return nil, err
	}
	for deviceID, cameras := range mappings {
		seen := make(map[string]bool, len(cameras))
		for _, camera := range cameras {
			if camera.CameraID == "" || seen[camera.CameraID] {
				continue
			}
			out[deviceID] = append(out[deviceID], cameraSummary(camera))
			seen[camera.CameraID] = true
		}
	}
	return out, nil
}

func cameraSummary(v model.VideoCameraMapping) model.CameraSummary {
	return model.CameraSummary{CameraID: v.CameraID, Brand: v.Brand, CameraName: v.CameraName, CameraPoint: v.CameraPoint, DeviceID: v.DeviceID, Building: v.Building, Floor: v.Floor, Room: v.Room, Enabled: v.Enabled}
}

func (e *Engine) SetAlarmStatus(ctx context.Context, tenant, alarmID, status, actor string) (model.Alarm, error) {
	now := e.Clock.Now().UnixMilli()
	a, _, err := e.mutateAlarm(ctx, tenant, alarmID, func(a *model.Alarm) (bool, error) {
		switch status {
		case "ACKED":
			if a.Status != "ACTIVE" {
				return false, fmt.Errorf("only active alarms can be acknowledged")
			}
			a.Status = status
			a.AckedAt = now
		case "RECOVERED":
			// An external system may assert recovery for alarm types that no
			// clearing property describes.
			if a.Status != "ACTIVE" && a.Status != "ACKED" {
				return false, fmt.Errorf("only active or acknowledged alarms can be recovered")
			}
			a.Status = status
			a.RecoveredAt = now
		case "CLOSED":
			if a.RequiresVerification() && a.Disposition == nil {
				return false, model.ErrDispositionRequired
			}
			a.Status = status
			a.ClosedAt = now
		case "SUPPRESSED":
			// Suppression silences an open alarm; reopening a recovered or
			// closed one as suppressed would rewrite its outcome.
			if a.Status != "ACTIVE" && a.Status != "ACKED" {
				return false, fmt.Errorf("only active or acknowledged alarms can be suppressed")
			}
			a.Status = status
		default:
			return false, fmt.Errorf("unsupported status %s", status)
		}
		return true, nil
	})
	if err != nil {
		return a, err
	}
	if err := e.syncDeviceBusinessStatus(ctx, a.TenantID, "", a.DeviceID); err != nil {
		return a, err
	}
	e.RecordAudit(ctx, model.AuditLog{ID: id("audit"), TenantID: tenant, Actor: actor, Action: "alarm." + strings.ToLower(status), TargetType: "alarm", TargetID: alarmID, CreatedAt: now})
	payload := mustJSON(a)
	if status == "RECOVERED" {
		e.publishEvent(ctx, model.TopicAlarmRecovered, a.ID, a.MQTTTopic("recovered"), payload)
		return a, nil
	}
	e.publishEvent(ctx, model.TopicAlarmConfirmed, a.ID, a.MQTTTopic("confirmed"), payload)
	return a, nil
}

// VerifyAlarm records the on-site verification of an alarm. It can be
// corrected until the alarm is closed.
func (e *Engine) VerifyAlarm(ctx context.Context, tenant, alarmID string, d model.AlarmDisposition, actor string) (model.Alarm, error) {
	if !model.ValidDispositionResult(d.Result) {
		return model.Alarm{}, fmt.Errorf("unknown verification result %q", d.Result)
	}
	now := e.Clock.Now().UnixMilli()
	d.Handler, d.VerifiedAt = actor, now
	// The AI snapshot is taken by the platform, never from the request.
	d.AIAnalysisAt, d.AIRiskLevel, d.AIPromptVersion = 0, "", ""
	if analysis, ok := e.latestAIAnalysis(ctx, tenant, alarmID); ok {
		d.AIAnalysisAt, d.AIRiskLevel, d.AIPromptVersion = analysis.CreatedAt, analysis.RiskLevel, analysis.PromptVersion
	}
	a, _, err := e.mutateAlarm(ctx, tenant, alarmID, func(a *model.Alarm) (bool, error) {
		if a.Status == "CLOSED" {
			return false, fmt.Errorf("closed alarms cannot be verified again")
		}
		if d.ArrivedAt != 0 && (d.ArrivedAt < a.FirstTriggeredAt || d.ArrivedAt > now) {
			return false, fmt.Errorf("arrival time must be between the alarm and now")
		}
		disposition := d
		a.Disposition = &disposition
		return true, nil
	})
	if err != nil {
		return a, err
	}
	e.RecordAudit(ctx, model.AuditLog{ID: id("audit"), TenantID: tenant, Actor: actor, Action: "alarm.verify", TargetType: "alarm", TargetID: alarmID, Details: map[string]any{"result": d.Result, "dispatchId": d.DispatchID}, CreatedAt: now})
	return a, nil
}

// latestAIAnalysis returns the newest successful analysis of an alarm across
// knowledge scopes; fallback results written after a failed run are skipped.
func (e *Engine) latestAIAnalysis(ctx context.Context, tenant, alarmID string) (model.AIAnalysis, bool) {
	var latest model.AIAnalysis
	found := false
	for _, scope := range []string{model.AIAnalysisScopeNone, model.AlarmAnalysisWorkflowID, model.AIAnalysisScopeLegacyTenant} {
		analysis, err := e.Repo.GetAIAnalysis(ctx, tenant, alarmID, scope)
		if err != nil || analysis.Error != "" || analysis.RiskLevel == "" || found && analysis.CreatedAt <= latest.CreatedAt {
			continue
		}
		latest, found = analysis, true
	}
	return latest, found
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

// AddAlarmAttachment records an uploaded attachment, whose ID comes from
// NewAlarmAttachmentID, on an alarm that is not closed yet.
func (e *Engine) AddAlarmAttachment(ctx context.Context, tenant, alarmID string, att model.AlarmAttachment, actor string) (model.Alarm, error) {
	att.UploadedBy, att.UploadedAt = actor, e.Clock.Now().UnixMilli()
	return e.changeAttachments(ctx, tenant, alarmID, actor, "alarm.attachment.add", att, func(a *model.Alarm) error {
		if len(a.Attachments) >= model.MaxAlarmAttachments {
			return model.ErrTooManyAttachments
		}
		a.Attachments = append(a.Attachments, att)
		return nil
	})
}

// NewAlarmAttachmentID reserves the identity of an attachment before its
// file is stored, so the object key is known up front.
func NewAlarmAttachmentID() string { return id("att") }

// RemoveAlarmAttachment drops an attachment from an alarm that is not closed
// yet and returns it, so the caller can delete the file.
func (e *Engine) RemoveAlarmAttachment(ctx context.Context, tenant, alarmID, attachmentID, actor string) (model.AlarmAttachment, error) {
	var removed model.AlarmAttachment
	_, err := e.changeAttachments(ctx, tenant, alarmID, actor, "alarm.attachment.remove", model.AlarmAttachment{ID: attachmentID}, func(a *model.Alarm) error {
		index := slices.IndexFunc(a.Attachments, func(v model.AlarmAttachment) bool { return v.ID == attachmentID })
		if index < 0 {
			return model.ErrAttachmentNotFound
		}
		removed = a.Attachments[index]
		a.Attachments = slices.Delete(slices.Clone(a.Attachments), index, index+1)
		return nil
	})
	return removed, err
}

func (e *Engine) changeAttachments(ctx context.Context, tenant, alarmID, actor, action string, att model.AlarmAttachment, change func(*model.Alarm) error) (model.Alarm, error) {
	a, _, err := e.mutateAlarm(ctx, tenant, alarmID, func(a *model.Alarm) (bool, error) {
		if a.Status == "CLOSED" {
			return false, model.ErrAlarmClosed
		}
		return true, change(a)
	})
	if err != nil {
		return a, err
	}
	e.RecordAudit(ctx, model.AuditLog{ID: id("audit"), TenantID: tenant, Actor: actor, Action: action, TargetType: "alarm", TargetID: alarmID, Details: map[string]any{"attachmentId": att.ID, "name": att.Name, "size": att.Size}, CreatedAt: e.Clock.Now().UnixMilli()})
	return a, nil
}

// objectCleanupBatch is how many queued files one cleanup pass deletes.
const objectCleanupBatch = 200

// DeleteObjectLater deletes a file whose record is gone; when the delete
// fails the file is queued for CleanupObjectsOnce.
func (e *Engine) DeleteObjectLater(ctx context.Context, bucket, key string) {
	if deleter, ok := e.Archive.(ports.ObjectDeleter); ok && deleter.DeleteObject(ctx, bucket, key) == nil {
		return
	}
	if err := e.Repo.EnqueueObjectCleanup(context.WithoutCancel(ctx), bucket, key); err != nil && e.Log != nil {
		e.Log.Warn("queue object cleanup failed", "bucket", bucket, "key", key, "error", err)
	}
}

// CleanupObjectsOnce deletes queued files; it runs as a Jobs singleton.
func (e *Engine) CleanupObjectsOnce(ctx context.Context) error {
	deleter, ok := e.Archive.(ports.ObjectDeleter)
	if !ok {
		return nil
	}
	refs, err := e.Repo.PendingObjectCleanups(ctx, objectCleanupBatch)
	if err != nil {
		return err
	}
	var failed error
	for _, ref := range refs {
		if err = deleter.DeleteObject(ctx, ref.Bucket, ref.Key); err != nil {
			failed = err
			continue
		}
		if err = e.Repo.FinishObjectCleanup(ctx, ref.Bucket, ref.Key); err != nil {
			return err
		}
	}
	return failed
}

// publishEvent delivers an event to the internal bus and the realtime channel.
// The stored state is already authoritative, so a delivery failure does not
// undo it; it is logged and counted so lost notifications are visible.
func (e *Engine) publishEvent(ctx context.Context, topic, key, realtimeTopic string, payload []byte) {
	if err := e.Bus.Publish(ctx, topic, key, payload); err != nil {
		e.deliveryFailed("bus", topic, err)
	}
	if err := e.Realtime.Publish(ctx, realtimeTopic, payload, 1, false); err != nil {
		e.deliveryFailed("realtime", topic, err)
	}
}

func (e *Engine) deliveryFailed(channel, topic string, err error) {
	if e.Metrics != nil {
		e.Metrics.Inc("event_publish_failed_total")
	}
	if e.Log != nil {
		e.Log.Warn("event delivery failed", "channel", channel, "topic", topic, "error", err)
	}
}

// RecordAudit writes an audit entry. The audited action has already happened,
// so a failed write is reported instead of failing the action.
func (e *Engine) RecordAudit(ctx context.Context, entry model.AuditLog) {
	if err := e.Repo.SaveAudit(ctx, entry); err != nil {
		if e.Metrics != nil {
			e.Metrics.Inc("audit_write_failed_total")
		}
		if e.Log != nil {
			e.Log.Error("audit write failed", "tenant", entry.TenantID, "action", entry.Action, "error", err)
		}
	}
}
