package core /* 声明 core 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"       /* 执行当前语句并推进处理流程。 */
	"crypto/rand"   /* 执行当前语句并推进处理流程。 */
	"crypto/sha256" /* 执行当前语句并推进处理流程。 */
	"encoding/hex"  /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"errors"        /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */
	"log/slog"      /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */
	"sync"          /* 执行当前语句并推进处理流程。 */
	"time"          /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports"  /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

const directAlarmRulePrefix = "device-report:" /* 声明 directAlarmRulePrefix。 */

type Engine struct { /* 定义 Engine 类型。 */
	stateLocks                [64]sync.Mutex           /* 执行当前语句并推进处理流程。 */
	ingestLocks               [256]sync.Mutex          /* 执行当前语句并推进处理流程。 */
	Repo                      ports.Repository         /* 执行当前语句并推进处理流程。 */
	Archive                   ports.Archive            /* 执行当前语句并推进处理流程。 */
	RawStore                  ports.RawMessageStore    /* 执行当前语句并推进处理流程。 */
	Bus                       ports.EventBus           /* 执行当前语句并推进处理流程。 */
	Realtime                  ports.RealtimePublisher  /* 执行当前语句并推进处理流程。 */
	AI                        ports.AIClient           /* 执行当前语句并推进处理流程。 */
	AIPlugins                 ports.AIPluginRegistry   /* 执行当前语句并推进处理流程。 */
	AIWorkflows               ports.AIWorkflowRuntime  /* 执行当前语句并推进处理流程。 */
	KB                        ports.KnowledgeBase      /* 执行当前语句并推进处理流程。 */
	Parsers                   *parser.Registry         /* 执行当前语句并推进处理流程。 */
	Clock                     ports.Clock              /* 执行当前语句并推进处理流程。 */
	Log                       *slog.Logger             /* 执行当前语句并推进处理流程。 */
	Metrics                   interface{ Inc(string) } /* 执行当前语句并推进处理流程。 */
	VideoMediaAllowedHosts    []string                 /* 执行当前语句并推进处理流程。 */
	RequireVideoCameraMapping bool                     /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func New(repo ports.Repository, archive ports.Archive, bus ports.EventBus, realtime ports.RealtimePublisher, parsers *parser.Registry, log *slog.Logger) *Engine { /* 定义 New 函数。 */
	engine := &Engine{Repo: repo, Archive: archive, Bus: bus, Realtime: realtime, Parsers: parsers, Clock: ports.RealClock{}, Log: log} /* 更新 engine 的值。 */
	if rawStore, ok := archive.(ports.RawMessageStore); ok {                                                                            /* 判断条件并选择处理分支。 */
		engine.RawStore = rawStore /* 更新 engine.RawStore 的值。 */
	} /* 结束当前表达式或代码块。 */
	return engine /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (e *Engine) GetRaw(ctx context.Context, index model.RawArchiveIndex) (model.RawMessage, error) { /* 定义 GetRaw 函数。 */
	if e.RawStore == nil { /* 判断条件并选择处理分支。 */
		return model.RawMessage{}, errors.New("raw message store is not configured") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return e.RawStore.GetRaw(ctx, index) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (e *Engine) Start(ctx context.Context) error { /* 定义 Start 函数。 */
	subs := []struct { /* 更新 subs 的值。 */
		topic, group string        /* 执行当前语句并推进处理流程。 */
		h            ports.Handler /* 执行当前语句并推进处理流程。 */
	}{{model.TopicRaw, "parser", e.handleRaw}, {model.TopicPropertyReport, "storage", e.handleStandard}, {model.TopicEventReport, "storage", e.handleStandard}, {model.TopicParsed, "storage", e.handleStandard}, {model.TopicDeviceState, "state", e.handleState}, {model.TopicAlarmRaised, "ai", e.handleAI}} /* 结束当前表达式或代码块。 */
	for _, s := range subs { /* 循环处理当前数据。 */
		if err := e.Bus.Subscribe(ctx, s.topic, s.group, s.h); err != nil { /* 判断条件并选择处理分支。 */
			return err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	go e.retryPendingRaw(ctx)        /* 执行当前语句并推进处理流程。 */
	go e.retryPendingVideoMedia(ctx) /* 执行当前语句并推进处理流程。 */
	return nil                       /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (e *Engine) IngestRaw(ctx context.Context, raw model.RawMessage) (model.RawArchiveIndex, bool, error) { /* 定义 IngestRaw 函数。 */
	raw.Normalize(e.Clock.Now())                                           /* 执行当前语句并推进处理流程。 */
	digest := sha256.Sum256([]byte(raw.TenantID + "\x00" + raw.MessageID)) /* 更新 digest 的值。 */
	lock := &e.ingestLocks[digest[0]]                                      /* 更新 lock 的值。 */
	lock.Lock()                                                            /* 执行当前语句并推进处理流程。 */
	defer lock.Unlock()                                                    /* 安排函数结束时执行清理。 */
	if raw.ProtocolID == "" || raw.ProtocolVersion == "" {                 /* 判断条件并选择处理分支。 */
		if binding, err := e.Repo.GetProductProtocolBinding(ctx, raw.TenantID, raw.ProductID); err == nil { /* 判断条件并选择处理分支。 */
			raw.ProtocolID, raw.ProtocolVersion = binding.ProtocolID, binding.Version                                                        /* 更新 raw.ProtocolVersion 的值。 */
			if release, releaseErr := e.Repo.GetProtocolRelease(ctx, raw.TenantID, binding.ProtocolID, binding.Version); releaseErr == nil { /* 判断条件并选择处理分支。 */
				raw.PointTableVersion = release.PointTableVersion /* 更新 raw.PointTableVersion 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if err := raw.Validate(); err != nil { /* 判断条件并选择处理分支。 */
		return model.RawArchiveIndex{}, false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err := e.ensureGatewayChild(ctx, raw); err != nil { /* 判断条件并选择处理分支。 */
		return model.RawArchiveIndex{}, false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if existing, err := e.Repo.GetRawIndex(ctx, raw.TenantID, raw.MessageID); err == nil { /* 判断条件并选择处理分支。 */
		if existing.PayloadHash != raw.PayloadHash() || existing.DeviceID != raw.DeviceID || existing.ProductID != raw.ProductID { /* 判断条件并选择处理分支。 */
			if e.Log != nil { /* 判断条件并选择处理分支。 */
				e.Log.Warn("raw message id conflict", "tenantId", raw.TenantID, "messageId", raw.MessageID) /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			return existing, false, model.ErrRawConflict /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if existing.PublishedAt == 0 { /* 判断条件并选择处理分支。 */
			stored, readErr := e.GetRaw(ctx, existing) /* 更新 readErr 的值。 */
			if readErr != nil {                        /* 判断条件并选择处理分支。 */
				return existing, false, fmt.Errorf("read pending raw: %w", readErr) /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			if publishErr := e.publishArchivedRaw(ctx, existing, stored); publishErr != nil { /* 判断条件并选择处理分支。 */
				return existing, false, publishErr /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		return existing, false, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var reserveErr error                                 /* 声明 reserveErr。 */
	raw, reserveErr = e.Repo.ReserveRawMessage(ctx, raw) /* 更新 reserveErr 的值。 */
	if reserveErr != nil {                               /* 判断条件并选择处理分支。 */
		return model.RawArchiveIndex{}, false, reserveErr /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if e.RawStore == nil { /* 判断条件并选择处理分支。 */
		return model.RawArchiveIndex{}, false, errors.New("raw message store is not configured") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	idx, err := e.RawStore.PutRaw(ctx, raw) /* 更新 err 的值。 */
	if err != nil {                         /* 判断条件并选择处理分支。 */
		if e.Metrics != nil { /* 判断条件并选择处理分支。 */
			e.Metrics.Inc("raw_archive_failed_total") /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		return idx, false, fmt.Errorf("archive raw: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	created, err := e.Repo.SaveRawIndex(ctx, idx) /* 更新 err 的值。 */
	if err != nil {                               /* 判断条件并选择处理分支。 */
		return idx, false, fmt.Errorf("index raw: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !created { /* 判断条件并选择处理分支。 */
		existing, err := e.Repo.GetRawIndex(ctx, raw.TenantID, raw.MessageID) /* 更新 err 的值。 */
		if err != nil {                                                       /* 判断条件并选择处理分支。 */
			return idx, false, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if existing.PayloadHash != raw.PayloadHash() || existing.DeviceID != raw.DeviceID || existing.ProductID != raw.ProductID { /* 判断条件并选择处理分支。 */
			return existing, false, model.ErrRawConflict /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return existing, false, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if e.Metrics != nil { /* 判断条件并选择处理分支。 */
		e.Metrics.Inc("mqtt_ingest_qps")           /* 执行当前语句并推进处理流程。 */
		e.Metrics.Inc("raw_archive_success_total") /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if err := e.publishArchivedRaw(ctx, idx, raw); err != nil { /* 判断条件并选择处理分支。 */
		return idx, true, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return idx, true, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (e *Engine) publishArchivedRaw(ctx context.Context, idx model.RawArchiveIndex, raw model.RawMessage) error { /* 定义 publishArchivedRaw 函数。 */
	b, _ := json.Marshal(raw)                                                   /* 更新 _ 的值。 */
	if err := e.Bus.Publish(ctx, model.TopicRaw, raw.DeviceID, b); err != nil { /* 判断条件并选择处理分支。 */
		_ = e.Repo.MarkRawPublished(ctx, idx.TenantID, idx.MessageID, 0, err.Error()) /* 更新 _ 的值。 */
		if e.Metrics != nil {                                                         /* 判断条件并选择处理分支。 */
			e.Metrics.Inc("raw_publish_failed_total") /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		return fmt.Errorf("publish archived raw: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err := e.Repo.MarkRawPublished(ctx, idx.TenantID, idx.MessageID, e.Clock.Now().UnixMilli(), ""); err != nil { /* 判断条件并选择处理分支。 */
		return fmt.Errorf("mark raw published: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (e *Engine) retryPendingRaw(ctx context.Context) { /* 定义 retryPendingRaw 函数。 */
	ticker := time.NewTicker(5 * time.Second) /* 更新 ticker 的值。 */
	defer ticker.Stop()                       /* 安排函数结束时执行清理。 */
	for {                                     /* 循环处理当前数据。 */
		select { /* 根据条件选择处理路径。 */
		case <-ctx.Done(): /* 处理当前分支。 */
			return /* 返回当前处理结果。 */
		case <-ticker.C: /* 处理当前分支。 */
			indexes, err := e.Repo.ListPendingRawIndexes(ctx, 200) /* 更新 err 的值。 */
			if err != nil {                                        /* 判断条件并选择处理分支。 */
				e.Log.Error("list pending raw", "error", err) /* 执行当前语句并推进处理流程。 */
				continue                                      /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			for _, idx := range indexes { /* 循环处理当前数据。 */
				raw, readErr := e.GetRaw(ctx, idx) /* 更新 readErr 的值。 */
				if readErr != nil {                /* 判断条件并选择处理分支。 */
					_ = e.Repo.MarkRawPublished(ctx, idx.TenantID, idx.MessageID, 0, readErr.Error()) /* 更新 _ 的值。 */
					continue                                                                          /* 执行当前语句并推进处理流程。 */
				} /* 结束当前表达式或代码块。 */
				if publishErr := e.publishArchivedRaw(ctx, idx, raw); publishErr != nil { /* 判断条件并选择处理分支。 */
					e.Log.Warn("retry pending raw", "messageId", idx.MessageID, "error", publishErr) /* 执行当前语句并推进处理流程。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (e *Engine) ensureGatewayChild(ctx context.Context, raw model.RawMessage) error { /* 定义 ensureGatewayChild 函数。 */
	if raw.GatewayID == "" || raw.DeviceID == raw.GatewayID { /* 判断条件并选择处理分支。 */
		return nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	gateway, err := e.Repo.GetManagedDevice(ctx, raw.TenantID, raw.GatewayID) /* 更新 err 的值。 */
	if err != nil {                                                           /* 判断条件并选择处理分支。 */
		return fmt.Errorf("gateway %s is not registered", raw.GatewayID) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if gateway.DeviceRole != "GATEWAY" { /* 判断条件并选择处理分支。 */
		return fmt.Errorf("device %s is not configured as a gateway", gateway.ID) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if raw.ProductID == "" { /* 判断条件并选择处理分支。 */
		return fmt.Errorf("child productId is required") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	childProduct, err := e.Repo.GetProduct(ctx, raw.TenantID, raw.ProductID) /* 更新 err 的值。 */
	if err != nil || childProduct.Status != "ENABLED" {                      /* 判断条件并选择处理分支。 */
		return fmt.Errorf("child product is not enabled") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if child, childErr := e.Repo.GetManagedDevice(ctx, raw.TenantID, raw.DeviceID); childErr == nil { /* 判断条件并选择处理分支。 */
		if child.DeviceRole != "CHILD" || child.GatewayID != gateway.ID { /* 判断条件并选择处理分支。 */
			return fmt.Errorf("device %s is already registered outside gateway %s", child.ID, gateway.ID) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if child.ProductID != raw.ProductID { /* 判断条件并选择处理分支。 */
			return fmt.Errorf("child device product does not match its registration") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	now := e.Clock.Now().UnixMilli()      /* 更新 now 的值。 */
	secret := id("child_secret")          /* 更新 secret 的值。 */
	hash := sha256.Sum256([]byte(secret)) /* 更新 hash 的值。 */
	child := model.ManagedDevice{         /* 更新 child 的值。 */
		ID:                 raw.DeviceID,                            /* 执行当前语句并推进处理流程。 */
		TenantID:           raw.TenantID,                            /* 执行当前语句并推进处理流程。 */
		ProductID:          raw.ProductID,                           /* 执行当前语句并推进处理流程。 */
		Name:               raw.DeviceName,                          /* 执行当前语句并推进处理流程。 */
		Status:             "ENABLED",                               /* 执行当前语句并推进处理流程。 */
		DeviceRole:         "CHILD",                                 /* 执行当前语句并推进处理流程。 */
		GatewayID:          gateway.ID,                              /* 执行当前语句并推进处理流程。 */
		RegistrationSource: "GATEWAY_AUTO",                          /* 执行当前语句并推进处理流程。 */
		AutoRegistered:     true,                                    /* 执行当前语句并推进处理流程。 */
		AccessKey:          "dk_" + strings.TrimPrefix(id(""), "_"), /* 执行当前语句并推进处理流程。 */
		SecretHash:         hex.EncodeToString(hash[:]),             /* 执行当前语句并推进处理流程。 */
		SecretHint:         "网关托管",                                  /* 执行当前语句并推进处理流程。 */
		CreatedAt:          now,                                     /* 执行当前语句并推进处理流程。 */
		UpdatedAt:          now,                                     /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if child.Name == "" { /* 判断条件并选择处理分支。 */
		child.Name = "子设备 " + raw.DeviceID /* 更新 child.Name 的值。 */
	} /* 结束当前表达式或代码块。 */
	if err = e.Repo.SaveManagedDevice(ctx, child); err != nil { /* 判断条件并选择处理分支。 */
		return fmt.Errorf("auto-register child device: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	_ = e.Repo.SaveAudit(ctx, model.AuditLog{ID: id("audit"), TenantID: raw.TenantID, Actor: "gateway:" + gateway.ID, Action: "device.child.auto-register", TargetType: "device", TargetID: child.ID, Details: map[string]any{"gatewayId": gateway.ID, "productId": child.ProductID}, CreatedAt: now}) /* 更新 _ 的值。 */
	return nil                                                                                                                                                                                                                                                                                         /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (e *Engine) handleRaw(ctx context.Context, b []byte) error { /* 定义 handleRaw 函数。 */
	var raw model.RawMessage                        /* 声明 raw。 */
	if err := json.Unmarshal(b, &raw); err != nil { /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var msg *model.StandardMessage                                     /* 声明 msg。 */
	var err error                                                      /* 声明 err。 */
	protocolID, protocolVersion := raw.ProtocolID, raw.ProtocolVersion /* 更新 protocolVersion 的值。 */
	if protocolID == "" || protocolVersion == "" {                     /* 判断条件并选择处理分支。 */
		if binding, bindingErr := e.Repo.GetProductProtocolBinding(ctx, raw.TenantID, raw.ProductID); bindingErr == nil { /* 判断条件并选择处理分支。 */
			protocolID, protocolVersion = binding.ProtocolID, binding.Version /* 更新 protocolVersion 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if protocolID != "" && protocolVersion != "" { /* 判断条件并选择处理分支。 */
		release, releaseErr := e.Repo.GetProtocolRelease(ctx, raw.TenantID, protocolID, protocolVersion) /* 更新 releaseErr 的值。 */
		if releaseErr != nil {                                                                           /* 判断条件并选择处理分支。 */
			err = fmt.Errorf("protocol release %s@%s not found: %w", protocolID, protocolVersion, releaseErr) /* 验证实际结果符合预期。 */
		} else if release.Status == "REVOKED" { /* 结束当前表达式或代码块。 */
			err = fmt.Errorf("protocol release %s@%s is revoked", protocolID, protocolVersion) /* 验证实际结果符合预期。 */
		} else { /* 结束当前表达式或代码块。 */
			raw.ProtocolID, raw.ProtocolVersion = protocolID, protocolVersion /* 更新 raw.ProtocolVersion 的值。 */
			if raw.PointTableVersion == "" {                                  /* 判断条件并选择处理分支。 */
				raw.PointTableVersion = release.PointTableVersion /* 更新 raw.PointTableVersion 的值。 */
			} /* 结束当前表达式或代码块。 */
			msg, err = e.Parsers.ParseWithConfig(release.ParserType, release.Config, raw) /* 更新 err 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	product, productErr := e.Repo.GetProduct(ctx, raw.TenantID, raw.ProductID)            /* 更新 productErr 的值。 */
	if msg == nil && err == nil && productErr == nil && product.ProtocolPackageID != "" { /* 判断条件并选择处理分支。 */
		pkg, pkgErr := e.Repo.GetProtocolPackage(ctx, raw.TenantID, product.ProtocolPackageID) /* 更新 pkgErr 的值。 */
		if pkgErr == nil && pkg.Status == "PUBLISHED" {                                        /* 判断条件并选择处理分支。 */
			msg, err = e.Parsers.ParseVersionWithConfig(pkg.ParserType, raw.ParserVersion, pkg.Config, raw) /* 更新 err 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if msg == nil && err == nil { /* 判断条件并选择处理分支。 */
		msg, err = e.Parsers.Parse(raw) /* 更新 err 的值。 */
	} /* 结束当前表达式或代码块。 */
	if err == nil && msg != nil { /* 判断条件并选择处理分支。 */
		_, err = model.MessageComponents(*msg) /* 更新 err 的值。 */
	} /* 结束当前表达式或代码块。 */
	parseError := "" /* 更新 parseError 的值。 */
	if err != nil {  /* 判断条件并选择处理分支。 */
		parseError = err.Error()   /* 更新 parseError 的值。 */
		if len(parseError) > 512 { /* 判断条件并选择处理分支。 */
			parseError = parseError[:512] /* 更新 parseError 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if storeErr := e.Repo.MarkRawParseResult(ctx, raw.TenantID, raw.MessageID, e.Clock.Now().UnixMilli(), parseError); storeErr != nil { /* 判断条件并选择处理分支。 */
		return storeErr /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err != nil { /* 判断条件并选择处理分支。 */
		if e.Metrics != nil { /* 判断条件并选择处理分支。 */
			e.Metrics.Inc("parse_failed_total") /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if e.Log != nil { /* 判断条件并选择处理分支。 */
			e.Log.Warn("raw message was not forwarded because parsing failed", "messageId", raw.MessageID, "deviceId", raw.DeviceID, "error", err) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		// A parse failure is deliberately terminal for the forwarding path. The
		// raw payload is already archived and remains available for replay, but
		// no parsed Kafka or MQTT message is emitted.
		return nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	out, _ := json.Marshal(msg)        /* 更新 _ 的值。 */
	topic := model.TopicPropertyReport /* 更新 topic 的值。 */
	switch msg.MessageType {           /* 根据条件选择处理路径。 */
	case model.EventReport, model.AlarmReport: /* 处理当前分支。 */
		topic = model.TopicEventReport /* 更新 topic 的值。 */
	case model.PropertyReport: /* 处理当前分支。 */
		topic = model.TopicPropertyReport /* 更新 topic 的值。 */
	default: /* 处理当前分支。 */
		topic = model.TopicParsed /* 更新 topic 的值。 */
	} /* 结束当前表达式或代码块。 */
	if err := e.Bus.Publish(ctx, topic, msg.DeviceID, out); err != nil { /* 判断条件并选择处理分支。 */
		return fmt.Errorf("publish parsed message to kafka topic %s: %w", topic, err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if e.Realtime == nil { /* 判断条件并选择处理分支。 */
		return nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err := e.Realtime.Publish(ctx, msg.MQTTTopic(), out, 1, false); err != nil { /* 判断条件并选择处理分支。 */
		if e.Metrics != nil { /* 判断条件并选择处理分支。 */
			e.Metrics.Inc("parsed_mqtt_publish_failed_total") /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		return fmt.Errorf("publish parsed message to mqtt: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (e *Engine) handleStandard(ctx context.Context, b []byte) error { /* 定义 handleStandard 函数。 */
	var msg model.StandardMessage                   /* 声明 msg。 */
	if err := json.Unmarshal(b, &msg); err != nil { /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	components, err := model.MessageComponents(msg) /* 更新 err 的值。 */
	if err != nil {                                 /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	shouldProcess, _, err := e.Repo.ClaimStandardMessage(ctx, msg) /* 更新 err 的值。 */
	if err != nil {                                                /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !shouldProcess { /* 判断条件并选择处理分支。 */
		return nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if msg.MessageType == model.CommandReply && msg.Parser == parser.StandardParserName { /* 判断条件并选择处理分支。 */
		if id, ok := msg.Event["commandId"].(string); ok { /* 判断条件并选择处理分支。 */
			if err := e.Repo.CompleteDeviceCommand(ctx, msg.TenantID, msg.DeviceID, id, msg.Event, e.Clock.Now().UnixMilli()); err != nil { /* 判断条件并选择处理分支。 */
				return err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if err := e.touchState(ctx, msg); err != nil { /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	rules, err := e.Repo.ListRules(ctx, msg.TenantID) /* 更新 err 的值。 */
	if err != nil {                                   /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	ruleAlarmHandled := false    /* 更新 ruleAlarmHandled 的值。 */
	for _, rule := range rules { /* 循环处理当前数据。 */
		if MatchRule(rule, msg) { /* 判断条件并选择处理分支。 */
			if rule.DurationSeconds > 0 { /* 判断条件并选择处理分支。 */
				satisfied, durationErr := e.durationSatisfied(ctx, rule, msg) /* 更新 durationErr 的值。 */
				if durationErr != nil {                                       /* 判断条件并选择处理分支。 */
					return durationErr /* 返回当前处理结果。 */
				} /* 结束当前表达式或代码块。 */
				if !satisfied { /* 判断条件并选择处理分支。 */
					continue /* 执行当前语句并推进处理流程。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
			ruleAlarmHandled = true                                        /* 更新 ruleAlarmHandled 的值。 */
			if _, _, err := e.raiseRuleAlarm(ctx, rule, msg); err != nil { /* 判断条件并选择处理分支。 */
				return err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} else if MatchConditions(rule.Recovery, msg) { /* 结束当前表达式或代码块。 */
			if err := e.clearDuration(ctx, rule, msg); err != nil { /* 判断条件并选择处理分支。 */
				return err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			if err := e.recoverRuleAlarm(ctx, rule, msg); err != nil { /* 判断条件并选择处理分支。 */
				return err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} else { /* 结束当前表达式或代码块。 */
			if err := e.clearDuration(ctx, rule, msg); err != nil { /* 判断条件并选择处理分支。 */
				return err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	// An ALARM_REPORT is already an assertion made by the device. Rules can
	// classify it and trigger actions when they match, but a missing rule must
	// never discard a device-originated alarm.
	if len(components) > 0 { /* 判断条件并选择处理分支。 */
		if err := e.applyComponentAlarms(ctx, msg, components); err != nil { /* 判断条件并选择处理分支。 */
			return err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if len(components) == 0 && msg.MessageType == model.AlarmReport && !ruleAlarmHandled { /* 判断条件并选择处理分支。 */
		if _, _, err := e.raiseDirectAlarm(ctx, msg); err != nil { /* 判断条件并选择处理分支。 */
			return err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if len(components) == 0 && msg.MessageType != model.AlarmReport && directAlarmCleared(msg) { /* 判断条件并选择处理分支。 */
		if err := e.recoverDirectAlarms(ctx, msg); err != nil { /* 判断条件并选择处理分支。 */
			return err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if err := e.syncDeviceBusinessStatus(ctx, msg.TenantID, msg.ProductID, msg.DeviceID); err != nil { /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return e.Repo.MarkStandardMessageProcessed(ctx, msg.TenantID, msg.MessageID) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (e *Engine) clearDuration(ctx context.Context, rule model.AlarmRule, msg model.StandardMessage) error { /* 定义 clearDuration 函数。 */
	return e.Repo.DeleteRulePending(ctx, rule.TenantID, rule.ID, msg.DeviceID) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (e *Engine) durationSatisfied(ctx context.Context, rule model.AlarmRule, msg model.StandardMessage) (bool, error) { /* 定义 durationSatisfied 函数。 */
	now := e.Clock.Now().Unix()                                                           /* 更新 now 的值。 */
	since, found, err := e.Repo.GetRulePending(ctx, rule.TenantID, rule.ID, msg.DeviceID) /* 更新 err 的值。 */
	if err != nil {                                                                       /* 判断条件并选择处理分支。 */
		return false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !found { /* 判断条件并选择处理分支。 */
		return false, e.Repo.SaveRulePending(ctx, rule.TenantID, rule.ID, msg.DeviceID, now) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return now-since >= rule.DurationSeconds, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (e *Engine) touchState(ctx context.Context, msg model.StandardMessage) error { /* 定义 touchState 函数。 */
	unlock := e.lockDeviceState(msg.TenantID, msg.DeviceID)              /* 更新 unlock 的值。 */
	defer unlock()                                                       /* 安排函数结束时执行清理。 */
	state, err := e.Repo.GetDeviceState(ctx, msg.TenantID, msg.DeviceID) /* 更新 err 的值。 */
	if err != nil {                                                      /* 判断条件并选择处理分支。 */
		state = model.DeviceState{TenantID: msg.TenantID, ProductID: msg.ProductID, DeviceID: msg.DeviceID, ReportIntervalSec: 300, OfflineToleranceSec: 60, ConnectionStatus: "UNKNOWN"} /* 更新 state 的值。 */
	} /* 结束当前表达式或代码块。 */
	// Late retransmissions remain archived but must not roll back current state.
	if msg.Timestamp < state.LastSeenAt { /* 判断条件并选择处理分支。 */
		return nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	old := state.BusinessStatus                                                  /* 更新 old 的值。 */
	oldConnection := state.ConnectionStatus                                      /* 更新 oldConnection 的值。 */
	state.DataStatus = "ACTIVE"                                                  /* 更新 state.DataStatus 的值。 */
	if msg.MessageType == model.AlarmReport || strings.EqualFold(old, "ALARM") { /* 判断条件并选择处理分支。 */
		state.BusinessStatus = "ALARM" /* 更新 state.BusinessStatus 的值。 */
	} else { /* 结束当前表达式或代码块。 */
		state.BusinessStatus = "ONLINE" /* 更新 state.BusinessStatus 的值。 */
	} /* 结束当前表达式或代码块。 */
	state.LastSeenAt = msg.Timestamp                                                     /* 更新 state.LastSeenAt 的值。 */
	state.LastMessageID = msg.MessageID                                                  /* 更新 state.LastMessageID 的值。 */
	state.StatusSource = "RAW_MESSAGE"                                                   /* 更新 state.StatusSource 的值。 */
	if msg.MessageType == model.StateChange && msg.Parser == parser.StandardParserName { /* 判断条件并选择处理分支。 */
		if status, ok := msg.Properties["connectionStatus"].(string); ok && (status == "CONNECTED" || status == "DISCONNECTED" || status == "UNKNOWN") { /* 判断条件并选择处理分支。 */
			state.ConnectionStatus = status /* 更新 state.ConnectionStatus 的值。 */
			if status == "CONNECTED" {      /* 判断条件并选择处理分支。 */
				state.LastConnectAt = msg.Timestamp /* 更新 state.LastConnectAt 的值。 */
			} else if status == "DISCONNECTED" { /* 结束当前表达式或代码块。 */
				state.LastDisconnectAt = msg.Timestamp /* 更新 state.LastDisconnectAt 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if err := e.Repo.UpsertDeviceState(ctx, state); err != nil { /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if old != state.BusinessStatus || oldConnection != state.ConnectionStatus { /* 判断条件并选择处理分支。 */
		_ = e.Repo.SaveDeviceStateEvent(ctx, state)                                                                                               /* 更新 _ 的值。 */
		payload, _ := json.Marshal(state)                                                                                                         /* 更新 _ 的值。 */
		_ = e.Realtime.Publish(ctx, fmt.Sprintf("/iot/device/state/%s/%s/%s", state.TenantID, state.ProductID, state.DeviceID), payload, 1, true) /* 更新 _ 的值。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (e *Engine) syncDeviceBusinessStatus(ctx context.Context, tenant, product, device string) error { /* 定义 syncDeviceBusinessStatus 函数。 */
	state, err := e.Repo.GetDeviceState(ctx, tenant, device) /* 更新 err 的值。 */
	if err != nil {                                          /* 判断条件并选择处理分支。 */
		return nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	open, err := e.hasOpenAlarm(ctx, tenant, device) /* 更新 err 的值。 */
	if err != nil {                                  /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	nextStatus, source, reason := "ONLINE", "RAW_MESSAGE", "" /* 更新 reason 的值。 */
	if open {                                                 /* 判断条件并选择处理分支。 */
		nextStatus, source, reason = "ALARM", "ACTIVE_ALARM", "存在活动告警" /* 更新 reason 的值。 */
	} /* 结束当前表达式或代码块。 */
	if state.ProductID == "" { /* 判断条件并选择处理分支。 */
		state.ProductID = product /* 更新 state.ProductID 的值。 */
	} /* 结束当前表达式或代码块。 */
	state.BusinessStatus = nextStatus      /* 更新 state.BusinessStatus 的值。 */
	state.StatusSource = source            /* 更新 state.StatusSource 的值。 */
	state.Reason = reason                  /* 更新 state.Reason 的值。 */
	return e.UpdateDeviceState(ctx, state) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (e *Engine) hasOpenAlarm(ctx context.Context, tenant, device string) (bool, error) { /* 定义 hasOpenAlarm 函数。 */
	for _, status := range []string{"ACTIVE", "ACKED"} { /* 循环处理当前数据。 */
		items, err := e.Repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: tenant, DeviceID: device, Status: status, Limit: 1}) /* 更新 err 的值。 */
		if err != nil {                                                                                                       /* 判断条件并选择处理分支。 */
			return false, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if len(items) > 0 { /* 判断条件并选择处理分支。 */
			return true, nil /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return false, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (e *Engine) raiseDirectAlarm(ctx context.Context, msg model.StandardMessage) (model.Alarm, bool, error) { /* 定义 raiseDirectAlarm 函数。 */
	now := e.Clock.Now().UnixMilli()             /* 更新 now 的值。 */
	alarmType, level := directAlarmMetadata(msg) /* 更新 level 的值。 */
	a := model.Alarm{                            /* 更新 a 的值。 */
		ID: id("alarm"), TenantID: msg.TenantID, RuleID: directAlarmRuleID(alarmType), TriggerID: msg.MessageID, /* 执行当前语句并推进处理流程。 */
		DeviceID: msg.DeviceID, DeviceName: e.alarmDeviceName(ctx, msg.TenantID, msg.DeviceID), AlarmType: alarmType, AlarmLevel: level, Status: "ACTIVE", Source: "device", /* 执行当前语句并推进处理流程。 */
		CityCode: tag(msg, "cityCode", "unknown"), DistrictCode: tag(msg, "districtCode", "unknown"), /* 执行当前语句并推进处理流程。 */
		BuildingID: tag(msg, "buildingId", "unknown"), DeviceType: tag(msg, "deviceType", msg.ProductID), /* 执行当前语句并推进处理流程。 */
		AreaID: tag(msg, "areaId", ""), FirstTriggeredAt: now, LastTriggeredAt: now, TriggerCount: 1, /* 执行当前语句并推进处理流程。 */
		Details: map[string]any{"message": msg, "direct": true}, /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	a.Cameras, _ = e.ListCameraSummaries(ctx, msg.TenantID, msg.DeviceID) /* 更新 _ 的值。 */
	saved, created, err := e.Repo.UpsertAlarm(ctx, a)                     /* 更新 err 的值。 */
	if err != nil {                                                       /* 判断条件并选择处理分支。 */
		return saved, false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if created { /* 判断条件并选择处理分支。 */
		if e.Metrics != nil { /* 判断条件并选择处理分支。 */
			e.Metrics.Inc("alarm_trigger_total") /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		payload, _ := json.Marshal(saved)                                         /* 更新 _ 的值。 */
		_ = e.Bus.Publish(ctx, model.TopicAlarmRaised, saved.ID, payload)         /* 更新 _ 的值。 */
		_ = e.Realtime.Publish(ctx, saved.MQTTTopic("raised"), payload, 1, false) /* 更新 _ 的值。 */
	} /* 结束当前表达式或代码块。 */
	return saved, created, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func directAlarmRuleID(alarmType string) string { /* 定义 directAlarmRuleID 函数。 */
	return directAlarmRulePrefix + alarmType /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (e *Engine) alarmDeviceName(ctx context.Context, tenantID, deviceID string) string { /* 定义 alarmDeviceName 函数。 */
	device, err := e.Repo.GetManagedDevice(ctx, tenantID, deviceID) /* 更新 err 的值。 */
	if err != nil {                                                 /* 判断条件并选择处理分支。 */
		return "" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return strings.TrimSpace(device.Name) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func firstMessageValue(msg model.StandardMessage, keys ...string) any { /* 定义 firstMessageValue 函数。 */
	for _, key := range keys { /* 循环处理当前数据。 */
		if value, ok := messageValue(msg, key); ok { /* 判断条件并选择处理分支。 */
			return value /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func messageValue(msg model.StandardMessage, key string) (any, bool) { /* 定义 messageValue 函数。 */
	for _, source := range directMessageSources(msg) { /* 循环处理当前数据。 */
		if value, ok := source[key]; ok { /* 判断条件并选择处理分支。 */
			return value, true /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return nil, false /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func directMessageSources(msg model.StandardMessage) []map[string]any { /* 定义 directMessageSources 函数。 */
	sources := []map[string]any{msg.Properties, msg.Event, msg.Raw} /* 更新 sources 的值。 */
	if payload := messageMap(msg.Raw["payload"]); payload != nil {  /* 判断条件并选择处理分支。 */
		sources = append(sources, payload)                       /* 更新 sources 的值。 */
		if alarm := messageMap(payload["alarm"]); alarm != nil { /* 判断条件并选择处理分支。 */
			sources = append(sources, alarm) /* 更新 sources 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if alarm := messageMap(msg.Event["alarm"]); alarm != nil { /* 判断条件并选择处理分支。 */
		sources = append(sources, alarm) /* 更新 sources 的值。 */
	} /* 结束当前表达式或代码块。 */
	return sources /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func messageMap(value any) map[string]any { /* 定义 messageMap 函数。 */
	switch item := value.(type) { /* 根据条件选择处理路径。 */
	case map[string]any: /* 处理当前分支。 */
		return item /* 返回当前处理结果。 */
	case json.RawMessage: /* 处理当前分支。 */
		var out map[string]any                 /* 声明 out。 */
		if json.Unmarshal(item, &out) == nil { /* 判断条件并选择处理分支。 */
			return out /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	case []byte: /* 处理当前分支。 */
		var out map[string]any                 /* 声明 out。 */
		if json.Unmarshal(item, &out) == nil { /* 判断条件并选择处理分支。 */
			return out /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	case string: /* 处理当前分支。 */
		var out map[string]any                         /* 声明 out。 */
		if json.Unmarshal([]byte(item), &out) == nil { /* 判断条件并选择处理分支。 */
			return out /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func messageFlag(msg model.StandardMessage, key string) bool { /* 定义 messageFlag 函数。 */
	value, ok := messageValue(msg, key) /* 更新 ok 的值。 */
	return ok && truthy(value)          /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func truthy(value any) bool { /* 定义 truthy 函数。 */
	switch item := value.(type) { /* 根据条件选择处理路径。 */
	case bool: /* 处理当前分支。 */
		return item /* 返回当前处理结果。 */
	case float64: /* 处理当前分支。 */
		return item != 0 /* 返回当前处理结果。 */
	case float32: /* 处理当前分支。 */
		return item != 0 /* 返回当前处理结果。 */
	case int: /* 处理当前分支。 */
		return item != 0 /* 返回当前处理结果。 */
	case int64: /* 处理当前分支。 */
		return item != 0 /* 返回当前处理结果。 */
	case uint: /* 处理当前分支。 */
		return item != 0 /* 返回当前处理结果。 */
	case uint64: /* 处理当前分支。 */
		return item != 0 /* 返回当前处理结果。 */
	case string: /* 处理当前分支。 */
		return strings.EqualFold(strings.TrimSpace(item), "true") || strings.TrimSpace(item) == "1" || strings.EqualFold(strings.TrimSpace(item), "yes") || strings.EqualFold(strings.TrimSpace(item), "on") /* 返回当前处理结果。 */
	default: /* 处理当前分支。 */
		return false /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func normalizeAlarmToken(value any) string { /* 定义 normalizeAlarmToken 函数。 */
	if value == nil { /* 判断条件并选择处理分支。 */
		return "" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	text := strings.ToUpper(strings.TrimSpace(fmt.Sprint(value)))           /* 更新 text 的值。 */
	if text == "" || text == "<NIL>" || text == "TRUE" || text == "FALSE" { /* 判断条件并选择处理分支。 */
		return "" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	text = strings.NewReplacer(" ", "_", "-", "_").Replace(text) /* 更新 text 的值。 */
	var out strings.Builder                                      /* 声明 out。 */
	for _, r := range text {                                     /* 循环处理当前数据。 */
		if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '.' { /* 判断条件并选择处理分支。 */
			out.WriteRune(r) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	normalized := strings.Trim(out.String(), "_.")    /* 更新 normalized 的值。 */
	if runes := []rune(normalized); len(runes) > 64 { /* 判断条件并选择处理分支。 */
		normalized = string(runes[:64]) /* 更新 normalized 的值。 */
	} /* 结束当前表达式或代码块。 */
	return normalized /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func directAlarmMetadata(msg model.StandardMessage) (string, string) { /* 定义 directAlarmMetadata 函数。 */
	alarmType := normalizeAlarmToken(firstMessageValue(msg, "alarmType", "alarm_type")) /* 更新 alarmType 的值。 */
	if alarmType == "" {                                                                /* 判断条件并选择处理分支。 */
		switch { /* 根据条件选择处理路径。 */
		case messageFlag(msg, "fireAlarm"): /* 处理当前分支。 */
			alarmType = "FIRE" /* 更新 alarmType 的值。 */
		case messageFlag(msg, "smoke") || messageFlag(msg, "smokeDetected"): /* 处理当前分支。 */
			alarmType = "SMOKE_DETECTED" /* 更新 alarmType 的值。 */
		case messageFlag(msg, "fault") || messageFlag(msg, "powerFault") || messageFlag(msg, "sensorFault"): /* 处理当前分支。 */
			alarmType = "DEVICE_FAULT" /* 更新 alarmType 的值。 */
		case messageFlag(msg, "offline"): /* 处理当前分支。 */
			alarmType = "DEVICE_OFFLINE" /* 更新 alarmType 的值。 */
		default: /* 处理当前分支。 */
			alarmType = "MANUAL_ALARM" /* 更新 alarmType 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	level := normalizeAlarmToken(firstMessageValue(msg, "alarmLevel", "alarm_level", "level")) /* 更新 level 的值。 */
	switch level {                                                                             /* 根据条件选择处理路径。 */
	case "CRITICAL", "HIGH", "MEDIUM", "LOW", "INFO": /* 处理当前分支。 */
	default: /* 处理当前分支。 */
		level = "HIGH" /* 更新 level 的值。 */
	} /* 结束当前表达式或代码块。 */
	return alarmType, level /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func directAlarmCleared(msg model.StandardMessage) bool { /* 定义 directAlarmCleared 函数。 */
	// Old immutable protocol releases expose aggregate objects without a safe
	// component identity contract. Never guess a whole-controller recovery.
	if msg.Event["type"] == "COMPONENT_STATUS" { /* 判断条件并选择处理分支。 */
		if _, exists := msg.Event["objects"]; exists { /* 判断条件并选择处理分支。 */
			return false /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if msg.MessageType != model.PropertyReport && msg.MessageType != model.StateChange { /* 判断条件并选择处理分支。 */
		return false /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	found := false                                                                                                                                                                            /* 更新 found 的值。 */
	for _, key := range []string{"alarm", "fireAlarm", "smoke", "smokeDetected", "fault", "offline", "powerFault", "openCircuit", "shortCircuit", "removed", "sensorFault", "upgradeFault"} { /* 循环处理当前数据。 */
		value, ok := messageValue(msg, key) /* 更新 ok 的值。 */
		if !ok {                            /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		found = true       /* 更新 found 的值。 */
		if truthy(value) { /* 判断条件并选择处理分支。 */
			return false /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return found /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (e *Engine) recoverDirectAlarms(ctx context.Context, msg model.StandardMessage) error { /* 定义 recoverDirectAlarms 函数。 */
	for _, status := range []string{"ACTIVE", "ACKED"} { /* 循环处理当前数据。 */
		alarms, err := e.Repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: msg.TenantID, DeviceID: msg.DeviceID, Status: status, Limit: 100}) /* 更新 err 的值。 */
		if err != nil {                                                                                                                      /* 判断条件并选择处理分支。 */
			return err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		for _, alarm := range alarms { /* 循环处理当前数据。 */
			if alarm.ComponentID != "" || !strings.HasPrefix(alarm.RuleID, directAlarmRulePrefix) || !directAlarmTypeCleared(msg, alarm.AlarmType) { /* 判断条件并选择处理分支。 */
				continue /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			alarm.Status = "RECOVERED"                             /* 更新 alarm.Status 的值。 */
			alarm.RecoveredAt = e.Clock.Now().UnixMilli()          /* 更新 alarm.RecoveredAt 的值。 */
			if err := e.Repo.UpdateAlarm(ctx, alarm); err != nil { /* 判断条件并选择处理分支。 */
				return err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			payload := mustJSON(alarm)                                                   /* 更新 payload 的值。 */
			_ = e.Bus.Publish(ctx, model.TopicAlarmRecovered, alarm.ID, payload)         /* 更新 _ 的值。 */
			_ = e.Realtime.Publish(ctx, alarm.MQTTTopic("recovered"), payload, 1, false) /* 更新 _ 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (e *Engine) raiseRuleAlarm(ctx context.Context, rule model.AlarmRule, msg model.StandardMessage) (model.Alarm, bool, error) { /* 定义 raiseRuleAlarm 函数。 */
	now := e.Clock.Now().UnixMilli()                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  /* 更新 now 的值。 */
	a := model.Alarm{ID: id("alarm"), TenantID: msg.TenantID, RuleID: rule.ID, TriggerID: msg.MessageID, DeviceID: msg.DeviceID, DeviceName: e.alarmDeviceName(ctx, msg.TenantID, msg.DeviceID), AlarmType: rule.AlarmType, AlarmLevel: rule.Level, Status: "ACTIVE", Source: "device", CityCode: tag(msg, "cityCode", "unknown"), DistrictCode: tag(msg, "districtCode", "unknown"), BuildingID: tag(msg, "buildingId", "unknown"), DeviceType: tag(msg, "deviceType", msg.ProductID), AreaID: tag(msg, "areaId", ""), FirstTriggeredAt: now, LastTriggeredAt: now, TriggerCount: 1, Details: map[string]any{"message": msg, "ruleName": rule.Name}} /* 更新 a 的值。 */
	a.Cameras, _ = e.ListCameraSummaries(ctx, msg.TenantID, msg.DeviceID)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             /* 更新 _ 的值。 */
	saved, created, err := e.Repo.UpsertAlarm(ctx, a)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   /* 判断条件并选择处理分支。 */
		return saved, false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if created { /* 判断条件并选择处理分支。 */
		if e.Metrics != nil { /* 判断条件并选择处理分支。 */
			e.Metrics.Inc("alarm_trigger_total") /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		payload, _ := json.Marshal(saved)                                         /* 更新 _ 的值。 */
		_ = e.Bus.Publish(ctx, model.TopicAlarmRaised, saved.ID, payload)         /* 更新 _ 的值。 */
		_ = e.Realtime.Publish(ctx, saved.MQTTTopic("raised"), payload, 1, false) /* 更新 _ 的值。 */
	} /* 结束当前表达式或代码块。 */
	// Alarm records are deduplicated while ACTIVE/ACKED, but a new matching
	// message must still execute the rule actions. Exact duplicate messages
	// keep the original trigger ID and must not execute actions twice.
	if created || saved.TriggerID != msg.MessageID { /* 判断条件并选择处理分支。 */
		for _, action := range rule.Actions { /* 循环处理当前数据。 */
			event := model.UIActionEvent{ID: id("ui_action"), TenantID: msg.TenantID, RuleID: rule.ID, AlarmID: saved.ID, DeviceID: msg.DeviceID, Action: action, TriggeredAt: now} /* 更新 event 的值。 */
			actionPayload, _ := json.Marshal(event)                                                                                                                                 /* 更新 _ 的值。 */
			_ = e.Bus.Publish(ctx, model.TopicUIAction, event.ID, actionPayload)                                                                                                    /* 更新 _ 的值。 */
			_ = e.Realtime.Publish(ctx, fmt.Sprintf("/iot/ui-action/%s", msg.TenantID), actionPayload, 1, false)                                                                    /* 更新 _ 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return saved, created, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (e *Engine) recoverRuleAlarm(ctx context.Context, rule model.AlarmRule, msg model.StandardMessage) error { /* 定义 recoverRuleAlarm 函数。 */
	alarms, err := e.Repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: msg.TenantID, DeviceID: msg.DeviceID, Status: "ACTIVE", Limit: 100}) /* 更新 err 的值。 */
	if err != nil {                                                                                                                        /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for _, a := range alarms { /* 循环处理当前数据。 */
		if a.RuleID != rule.ID { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		a.Status = "RECOVERED"                             /* 更新 a.Status 的值。 */
		a.RecoveredAt = e.Clock.Now().UnixMilli()          /* 更新 a.RecoveredAt 的值。 */
		if err := e.Repo.UpdateAlarm(ctx, a); err != nil { /* 判断条件并选择处理分支。 */
			return err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		payload, _ := json.Marshal(a)                                            /* 更新 _ 的值。 */
		_ = e.Bus.Publish(ctx, model.TopicAlarmRecovered, a.ID, payload)         /* 更新 _ 的值。 */
		_ = e.Realtime.Publish(ctx, a.MQTTTopic("recovered"), payload, 1, false) /* 更新 _ 的值。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// DeleteRule removes a rule and closes any alarms that can no longer be
// recovered by the deleted rule. Historical alarm rows are retained.
func (e *Engine) DeleteRule(ctx context.Context, tenant, ruleID string) error { /* 定义 DeleteRule 函数。 */
	if err := e.Repo.DeleteRule(ctx, tenant, ruleID); err != nil { /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err := e.Repo.DeleteRulePendings(ctx, tenant, ruleID); err != nil { /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return e.closeRuleAlarms(ctx, tenant, ruleID) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// DisableRule clears duration state and closes active/acknowledged alarms
// before a rule is switched off. Historical alarm rows remain available.
func (e *Engine) DisableRule(ctx context.Context, tenant, ruleID string) error { /* 定义 DisableRule 函数。 */
	if err := e.Repo.DeleteRulePendings(ctx, tenant, ruleID); err != nil { /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return e.closeRuleAlarms(ctx, tenant, ruleID) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (e *Engine) closeRuleAlarms(ctx context.Context, tenant, ruleID string) error { /* 定义 closeRuleAlarms 函数。 */
	const batchSize = 1000                               /* 声明 batchSize。 */
	affectedDevices := make(map[string]struct{})         /* 更新 affectedDevices 的值。 */
	for _, status := range []string{"ACTIVE", "ACKED"} { /* 循环处理当前数据。 */
		offset := 0 /* 更新 offset 的值。 */
		for {       /* 循环处理当前数据。 */
			alarms, err := e.Repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: tenant, Status: status, Limit: batchSize, Offset: offset}) /* 更新 err 的值。 */
			if err != nil {                                                                                                              /* 判断条件并选择处理分支。 */
				return err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			if len(alarms) == 0 { /* 判断条件并选择处理分支。 */
				break /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			changed := false               /* 更新 changed 的值。 */
			for _, alarm := range alarms { /* 循环处理当前数据。 */
				if alarm.RuleID != ruleID { /* 判断条件并选择处理分支。 */
					continue /* 执行当前语句并推进处理流程。 */
				} /* 结束当前表达式或代码块。 */
				alarm.Status = "RECOVERED"                             /* 更新 alarm.Status 的值。 */
				alarm.RecoveredAt = e.Clock.Now().UnixMilli()          /* 更新 alarm.RecoveredAt 的值。 */
				if err := e.Repo.UpdateAlarm(ctx, alarm); err != nil { /* 判断条件并选择处理分支。 */
					return err /* 返回当前处理结果。 */
				} /* 结束当前表达式或代码块。 */
				affectedDevices[alarm.DeviceID] = struct{}{}                                 /* 更新 affectedDevices[alarm.DeviceID] 的值。 */
				payload := mustJSON(alarm)                                                   /* 更新 payload 的值。 */
				_ = e.Bus.Publish(ctx, model.TopicAlarmRecovered, alarm.ID, payload)         /* 更新 _ 的值。 */
				_ = e.Realtime.Publish(ctx, alarm.MQTTTopic("recovered"), payload, 1, false) /* 更新 _ 的值。 */
				changed = true                                                               /* 更新 changed 的值。 */
			} /* 结束当前表达式或代码块。 */
			if changed { /* 判断条件并选择处理分支。 */
				// Updating rows removes them from the status-filtered result set;
				// restart at zero so offset pagination cannot skip the next row.
				offset = 0 /* 更新 offset 的值。 */
				continue   /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			offset += len(alarms) /* 更新 offset 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	for deviceID := range affectedDevices { /* 循环处理当前数据。 */
		if err := e.syncDeviceBusinessStatus(ctx, tenant, "", deviceID); err != nil { /* 判断条件并选择处理分支。 */
			return err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (e *Engine) handleState(ctx context.Context, b []byte) error { /* 定义 handleState 函数。 */
	var state model.DeviceState                                               /* 声明 state。 */
	if err := json.Unmarshal(b, &state); err == nil && state.DeviceID != "" { /* 判断条件并选择处理分支。 */
		return e.UpdateDeviceState(ctx, state) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var msg model.StandardMessage                   /* 声明 msg。 */
	if err := json.Unmarshal(b, &msg); err != nil { /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return e.touchState(ctx, msg) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (e *Engine) UpdateDeviceState(ctx context.Context, state model.DeviceState) error { /* 定义 UpdateDeviceState 函数。 */
	unlock := e.lockDeviceState(state.TenantID, state.DeviceID) /* 更新 unlock 的值。 */
	defer unlock()                                              /* 安排函数结束时执行清理。 */
	return e.updateDeviceState(ctx, state)                      /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (e *Engine) updateDeviceState(ctx context.Context, state model.DeviceState) error { /* 定义 updateDeviceState 函数。 */
	old, _ := e.Repo.GetDeviceState(ctx, state.TenantID, state.DeviceID) /* 更新 _ 的值。 */
	if state.LastSeenAt == 0 {                                           /* 判断条件并选择处理分支。 */
		state.LastSeenAt = old.LastSeenAt /* 更新 state.LastSeenAt 的值。 */
	} /* 结束当前表达式或代码块。 */
	if err := e.Repo.UpsertDeviceState(ctx, state); err != nil { /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if old.BusinessStatus != state.BusinessStatus || old.ConnectionStatus != state.ConnectionStatus { /* 判断条件并选择处理分支。 */
		_ = e.Repo.SaveDeviceStateEvent(ctx, state)                                                                                            /* 更新 _ 的值。 */
		b, _ := json.Marshal(state)                                                                                                            /* 更新 _ 的值。 */
		return e.Realtime.Publish(ctx, fmt.Sprintf("/iot/device/state/%s/%s/%s", state.TenantID, state.ProductID, state.DeviceID), b, 1, true) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (e *Engine) ScanOffline(ctx context.Context) error { /* 定义 ScanOffline 函数。 */
	states, err := e.Repo.ListDeviceStates(ctx, "") /* 更新 err 的值。 */
	if err != nil {                                 /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	now := e.Clock.Now().UnixMilli() /* 更新 now 的值。 */
	for _, s := range states {       /* 循环处理当前数据。 */
		deadline := s.LastSeenAt + (s.ReportIntervalSec+s.OfflineToleranceSec)*1000 /* 更新 deadline 的值。 */
		if s.LastSeenAt == 0 || deadline >= now {                                   /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		s.DataStatus = "SILENT"                /* 更新 s.DataStatus 的值。 */
		if s.ConnectionStatus == "CONNECTED" { /* 判断条件并选择处理分支。 */
			s.BusinessStatus = "SUSPECTED_OFFLINE" /* 更新 s.BusinessStatus 的值。 */
		} else { /* 结束当前表达式或代码块。 */
			s.BusinessStatus = "OFFLINE" /* 更新 s.BusinessStatus 的值。 */
		} /* 结束当前表达式或代码块。 */
		s.OfflineAt = deadline                 /* 更新 s.OfflineAt 的值。 */
		s.OfflineDetectedAt = now              /* 更新 s.OfflineDetectedAt 的值。 */
		s.StatusSource = "RAW_MESSAGE_TIMEOUT" /* 更新 s.StatusSource 的值。 */
		_ = e.UpdateDeviceState(ctx, s)        /* 更新 _ 的值。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (e *Engine) IngestVideo(ctx context.Context, v model.VideoAlarmEvent) (model.Alarm, bool, error) { /* 定义 IngestVideo 函数。 */
	if v.EventID == "" || v.TenantID == "" || v.CameraID == "" || v.AlarmType == "" { /* 判断条件并选择处理分支。 */
		return model.Alarm{}, false, errors.New("eventId, tenantId, cameraId and alarmType are required") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if v.ReceivedAt == 0 { /* 判断条件并选择处理分支。 */
		v.ReceivedAt = e.Clock.Now().UnixMilli() /* 更新 v.ReceivedAt 的值。 */
	} /* 结束当前表达式或代码块。 */
	mapping, mappingErr := e.Repo.GetVideoCameraMapping(ctx, v.TenantID, v.CameraID) /* 更新 mappingErr 的值。 */
	if mappingErr != nil && e.RequireVideoCameraMapping {                            /* 判断条件并选择处理分支。 */
		return model.Alarm{}, false, fmt.Errorf("camera %s is not bound to tenant %s", v.CameraID, v.TenantID) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if mappingErr == nil { /* 判断条件并选择处理分支。 */
		if !mapping.Enabled { /* 判断条件并选择处理分支。 */
			return model.Alarm{}, false, fmt.Errorf("camera %s is disabled", v.CameraID) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if v.CameraName == "" { /* 判断条件并选择处理分支。 */
			v.CameraName = mapping.CameraName /* 更新 v.CameraName 的值。 */
		} /* 结束当前表达式或代码块。 */
		if v.ProjectID == "" { /* 判断条件并选择处理分支。 */
			v.ProjectID = mapping.ProjectID /* 更新 v.ProjectID 的值。 */
		} /* 结束当前表达式或代码块。 */
		if v.AreaID == "" { /* 判断条件并选择处理分支。 */
			v.AreaID = mapping.AreaID /* 更新 v.AreaID 的值。 */
		} /* 结束当前表达式或代码块。 */
		if v.CityCode == "" { /* 判断条件并选择处理分支。 */
			v.CityCode = mapping.CityCode /* 更新 v.CityCode 的值。 */
		} /* 结束当前表达式或代码块。 */
		if v.DistrictCode == "" { /* 判断条件并选择处理分支。 */
			v.DistrictCode = mapping.DistrictCode /* 更新 v.DistrictCode 的值。 */
		} /* 结束当前表达式或代码块。 */
		if v.BuildingID == "" { /* 判断条件并选择处理分支。 */
			v.BuildingID = mapping.Building /* 更新 v.BuildingID 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if err := e.validateVideoMediaURLs(v); err != nil { /* 判断条件并选择处理分支。 */
		return model.Alarm{}, false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if v.Raw == nil { /* 判断条件并选择处理分支。 */
		v.Raw = map[string]any{} /* 更新 v.Raw 的值。 */
	} /* 结束当前表达式或代码块。 */
	if isExternalMedia(v.SnapshotURL) || isExternalMedia(v.VideoClipURL) { /* 判断条件并选择处理分支。 */
		v.Raw["mediaTransferStatus"] = "PENDING" /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	created, err := e.Repo.SaveVideoEvent(ctx, v) /* 更新 err 的值。 */
	if err != nil || !created {                   /* 判断条件并选择处理分支。 */
		if err != nil && e.Metrics != nil { /* 判断条件并选择处理分支。 */
			e.Metrics.Inc("video_alarm_failed_total") /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		return model.Alarm{}, false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if e.Metrics != nil { /* 判断条件并选择处理分支。 */
		e.Metrics.Inc("video_alarm_ingest_total") /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	_ = e.Bus.Publish(ctx, model.TopicVideoAlarm, v.CameraID, mustJSON(v))                                                                                                                                                                                                                                                                                                                                                                                                                                       /* 更新 _ 的值。 */
	now := e.Clock.Now().UnixMilli()                                                                                                                                                                                                                                                                                                                                                                                                                                                                             /* 更新 now 的值。 */
	a := model.Alarm{ID: id("alarm"), TenantID: v.TenantID, RuleID: "video:" + v.AlarmType, TriggerID: v.EventID, DeviceID: v.CameraID, DeviceName: v.CameraName, AlarmType: v.AlarmType, AlarmLevel: v.AlarmLevel, Status: "ACTIVE", Source: "video", CityCode: v.CityCode, DistrictCode: v.DistrictCode, BuildingID: v.BuildingID, DeviceType: "video_ai", AreaID: v.AreaID, FirstTriggeredAt: now, LastTriggeredAt: now, TriggerCount: 1, Confidence: v.Confidence, Details: map[string]any{"videoEvent": v}} /* 更新 a 的值。 */
	if mappingErr == nil {                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       /* 判断条件并选择处理分支。 */
		a.Cameras = []model.CameraSummary{cameraSummary(mapping)} /* 更新 a.Cameras 的值。 */
	} /* 结束当前表达式或代码块。 */
	if a.AlarmLevel == "" { /* 判断条件并选择处理分支。 */
		a.AlarmLevel = map[bool]string{true: "MEDIUM", false: "HIGH"}[v.Confidence < 0.6] /* 更新 a.AlarmLevel 的值。 */
	} /* 结束当前表达式或代码块。 */
	if v.Confidence < 0.6 { /* 判断条件并选择处理分支。 */
		a.Details["requiresVerification"] = true /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if fused, ok, fuseErr := e.fuseVideoAlarm(ctx, a); fuseErr != nil { /* 判断条件并选择处理分支。 */
		return model.Alarm{}, false, fuseErr /* 返回当前处理结果。 */
	} else if ok { /* 结束当前表达式或代码块。 */
		return fused, false, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	saved, isNew, err := e.Repo.UpsertAlarm(ctx, a) /* 更新 err 的值。 */
	if err == nil && isNew {                        /* 判断条件并选择处理分支。 */
		payload := mustJSON(saved)                                                /* 更新 payload 的值。 */
		_ = e.Bus.Publish(ctx, model.TopicAlarmRaised, saved.ID, payload)         /* 更新 _ 的值。 */
		_ = e.Realtime.Publish(ctx, saved.MQTTTopic("raised"), payload, 1, false) /* 更新 _ 的值。 */
	} /* 结束当前表达式或代码块。 */
	if v.Raw["mediaTransferStatus"] == "PENDING" { /* 判断条件并选择处理分支。 */
		go e.processVideoMedia(context.Background(), v) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	return saved, isNew, err /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (e *Engine) fuseVideoAlarm(ctx context.Context, incoming model.Alarm) (model.Alarm, bool, error) { /* 定义 fuseVideoAlarm 函数。 */
	active, err := e.Repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: incoming.TenantID, Status: "ACTIVE", Limit: 1000}) /* 更新 err 的值。 */
	if err != nil {                                                                                                      /* 判断条件并选择处理分支。 */
		return incoming, false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	windowStart := incoming.LastTriggeredAt - 120_000 /* 更新 windowStart 的值。 */
	for _, existing := range active {                 /* 循环处理当前数据。 */
		if existing.LastTriggeredAt < windowStart || existing.AreaID == "" || incoming.AreaID == "" || existing.AreaID != incoming.AreaID { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if existing.Source == "video" && existing.AlarmType == incoming.AlarmType { /* 判断条件并选择处理分支。 */
			existing.LastTriggeredAt = incoming.LastTriggeredAt /* 更新 existing.LastTriggeredAt 的值。 */
			existing.TriggerCount++                             /* 执行当前语句并推进处理流程。 */
			if incoming.Confidence > existing.Confidence {      /* 判断条件并选择处理分支。 */
				existing.Confidence = incoming.Confidence /* 更新 existing.Confidence 的值。 */
			} /* 结束当前表达式或代码块。 */
			if existing.Details == nil { /* 判断条件并选择处理分支。 */
				existing.Details = map[string]any{} /* 更新 existing.Details 的值。 */
			} /* 结束当前表达式或代码块。 */
			existing.Details["latestVideoEvent"] = incoming.Details["videoEvent"] /* 执行当前语句并推进处理流程。 */
			if err = e.Repo.UpdateAlarm(ctx, existing); err != nil {              /* 判断条件并选择处理分支。 */
				return incoming, false, err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			payload := mustJSON(existing)                                                /* 更新 payload 的值。 */
			_ = e.Realtime.Publish(ctx, existing.MQTTTopic("raised"), payload, 1, false) /* 更新 _ 的值。 */
			return existing, true, nil                                                   /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if existing.Source != "video" && relatedFireAlarm(existing.AlarmType, incoming.AlarmType) { /* 判断条件并选择处理分支。 */
			incoming.MultiSource = true            /* 更新 incoming.MultiSource 的值。 */
			existing.MultiSource = true            /* 更新 existing.MultiSource 的值。 */
			if incoming.AlarmLevel != "CRITICAL" { /* 判断条件并选择处理分支。 */
				incoming.AlarmLevel = "CRITICAL" /* 更新 incoming.AlarmLevel 的值。 */
			} /* 结束当前表达式或代码块。 */
			if existing.AlarmLevel != "CRITICAL" { /* 判断条件并选择处理分支。 */
				existing.AlarmLevel = "CRITICAL" /* 更新 existing.AlarmLevel 的值。 */
			} /* 结束当前表达式或代码块。 */
			if existing.Details == nil { /* 判断条件并选择处理分支。 */
				existing.Details = map[string]any{} /* 更新 existing.Details 的值。 */
			} /* 结束当前表达式或代码块。 */
			existing.Details["videoConfirmation"] = incoming.Details["videoEvent"] /* 执行当前语句并推进处理流程。 */
			_ = e.Repo.UpdateAlarm(ctx, existing)                                  /* 更新 _ 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return incoming, false, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func relatedFireAlarm(deviceType, videoType string) bool { /* 定义 relatedFireAlarm 函数。 */
	a := strings.ToUpper(deviceType)                                                                                                                                                                            /* 更新 a 的值。 */
	b := strings.ToUpper(videoType)                                                                                                                                                                             /* 更新 b 的值。 */
	return (strings.Contains(a, "FIRE") || strings.Contains(a, "SMOKE") || strings.Contains(a, "TEMPERATURE")) && (strings.Contains(b, "FIRE") || strings.Contains(b, "FLAME") || strings.Contains(b, "SMOKE")) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// ListCameraSummaries resolves the cameras associated with one device without
// exposing stream URLs or vendor credentials. The relation is intentionally
// one camera -> one device; a device may return many camera summaries.
func (e *Engine) ListCameraSummaries(ctx context.Context, tenant, deviceID string) ([]model.CameraSummary, error) { /* 定义 ListCameraSummaries 函数。 */
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(deviceID) == "" { /* 判断条件并选择处理分支。 */
		return nil, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	relations, err := e.Repo.ListVideoCameraRelationsByTarget(ctx, tenant, "device", deviceID) /* 更新 err 的值。 */
	if err != nil {                                                                            /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	out := make([]model.CameraSummary, 0, len(relations)) /* 更新 out 的值。 */
	seen := make(map[string]struct{}, len(relations))     /* 更新 seen 的值。 */
	for _, relation := range relations {                  /* 循环处理当前数据。 */
		if relation.CameraID == "" { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if _, ok := seen[relation.CameraID]; ok { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		mapping, getErr := e.Repo.GetVideoCameraMapping(ctx, tenant, relation.CameraID) /* 更新 getErr 的值。 */
		if getErr != nil {                                                              /* 判断条件并选择处理分支。 */
			return nil, getErr /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		out = append(out, cameraSummary(mapping)) /* 更新 out 的值。 */
		seen[relation.CameraID] = struct{}{}      /* 更新 seen[relation.CameraID] 的值。 */
	} /* 结束当前表达式或代码块。 */
	return out, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// ListCameraSummariesForDevices resolves a list page with one batched relation
// and mapping lookup, preserving the same safe fields as ListCameraSummaries.
func (e *Engine) ListCameraSummariesForDevices(ctx context.Context, tenant string, deviceIDs []string) (map[string][]model.CameraSummary, error) { /* 定义 ListCameraSummariesForDevices 函数。 */
	out := make(map[string][]model.CameraSummary, len(deviceIDs)) /* 更新 out 的值。 */
	if len(deviceIDs) == 0 {                                      /* 判断条件并选择处理分支。 */
		return out, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	mappings, err := e.Repo.ListVideoCameraMappingsByDeviceIDs(ctx, tenant, deviceIDs) /* 更新 err 的值。 */
	if err != nil {                                                                    /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for deviceID, cameras := range mappings { /* 循环处理当前数据。 */
		seen := make(map[string]bool, len(cameras)) /* 更新 seen 的值。 */
		for _, camera := range cameras {            /* 循环处理当前数据。 */
			if camera.CameraID == "" || seen[camera.CameraID] { /* 判断条件并选择处理分支。 */
				continue /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			out[deviceID] = append(out[deviceID], cameraSummary(camera)) /* 更新 out[deviceID] 的值。 */
			seen[camera.CameraID] = true                                 /* 更新 seen[camera.CameraID] 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return out, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func cameraSummary(v model.VideoCameraMapping) model.CameraSummary { /* 定义 cameraSummary 函数。 */
	return model.CameraSummary{CameraID: v.CameraID, Brand: v.Brand, CameraName: v.CameraName, CameraPoint: v.CameraPoint, DeviceID: v.DeviceID, Building: v.Building, Floor: v.Floor, Room: v.Room, Enabled: v.Enabled} /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (e *Engine) handleAI(ctx context.Context, b []byte) error { /* 定义 handleAI 函数。 */
	if e.AI == nil { /* 判断条件并选择处理分支。 */
		return nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var alarm model.Alarm                             /* 声明 alarm。 */
	if err := json.Unmarshal(b, &alarm); err != nil { /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	_, err := e.AnalyzeAlarm(ctx, alarm.TenantID, alarm.ID) /* 更新 err 的值。 */
	return err                                              /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// AnalyzeAlarm runs the same automatic analysis path used by alarm events and
// is also exposed to the operator UI for a manual re-run.
func (e *Engine) AnalyzeAlarm(ctx context.Context, tenantID, alarmID string) (model.AIAnalysis, error) { /* 定义 AnalyzeAlarm 函数。 */
	if e.AI == nil { /* 判断条件并选择处理分支。 */
		return model.AIAnalysis{}, errors.New("AI model is not configured") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	alarm, err := e.Repo.GetAlarm(ctx, tenantID, alarmID) /* 更新 err 的值。 */
	if err != nil {                                       /* 判断条件并选择处理分支。 */
		return model.AIAnalysis{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	history := []map[string]any{}                                                                            /* 更新 history 的值。 */
	if device, deviceErr := e.Repo.GetManagedDevice(ctx, alarm.TenantID, alarm.DeviceID); deviceErr == nil { /* 判断条件并选择处理分支。 */
		history = append(history, map[string]any{"contextType": "deviceMetadata", "device": device}) /* 更新 history 的值。 */
	} /* 结束当前表达式或代码块。 */
	properties := []string{"temperature", "smoke", "water_pressure", "voltage", "current", "gas"} /* 更新 properties 的值。 */
	for _, property := range properties {                                                         /* 循环处理当前数据。 */
		for _, window := range []struct { /* 循环处理当前数据。 */
			name  string /* 执行当前语句并推进处理流程。 */
			ms    int64  /* 执行当前语句并推进处理流程。 */
			limit int    /* 执行当前语句并推进处理流程。 */
		}{{"10m", 10 * 60 * 1000, 200}, {"1h", 60 * 60 * 1000, 500}, {"24h", 24 * 60 * 60 * 1000, 1000}} { /* 结束当前表达式或代码块。 */
			items, historyErr := e.Repo.PropertyHistory(ctx, alarm.TenantID, alarm.DeviceID, property, alarm.LastTriggeredAt-window.ms, alarm.LastTriggeredAt, window.limit) /* 更新 historyErr 的值。 */
			if historyErr == nil && len(items) > 0 {                                                                                                                         /* 判断条件并选择处理分支。 */
				history = append(history, map[string]any{"contextType": "propertyHistory", "property": property, "window": window.name, "items": items}) /* 更新 history 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if similar, similarErr := e.Repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: alarm.TenantID, DeviceID: alarm.DeviceID, Limit: 20}); similarErr == nil { /* 判断条件并选择处理分支。 */
		filtered := []model.Alarm{}    /* 更新 filtered 的值。 */
		for _, item := range similar { /* 循环处理当前数据。 */
			if item.ID != alarm.ID && item.AlarmType == alarm.AlarmType { /* 判断条件并选择处理分支。 */
				filtered = append(filtered, item) /* 更新 filtered 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		history = append(history, map[string]any{"contextType": "similarAlarms", "items": filtered}) /* 更新 history 的值。 */
	} /* 结束当前表达式或代码块。 */
	knowledge := []string{} /* 更新 knowledge 的值。 */
	if e.KB != nil {        /* 判断条件并选择处理分支。 */
		knowledge, _ = e.KB.Search(ctx, alarm.TenantID, strings.Join([]string{alarm.AlarmType, alarm.DeviceType, "处置 SOP 维修"}, " "), 8) /* 更新 _ 的值。 */
	} /* 结束当前表达式或代码块。 */
	analysis, err := e.AI.AnalyzeAlarm(ctx, alarm, history, knowledge) /* 更新 err 的值。 */
	if err != nil {                                                    /* 判断条件并选择处理分支。 */
		if e.Metrics != nil { /* 判断条件并选择处理分支。 */
			e.Metrics.Inc("ai_analysis_failed_total") /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		analysis = model.AIAnalysis{ /* 更新 analysis 的值。 */
			AlarmID:       alarm.ID,                  /* 执行当前语句并推进处理流程。 */
			Summary:       "AI 研判暂时失败，已保留告警供人工研判。",   /* 执行当前语句并推进处理流程。 */
			RiskLevel:     alarm.AlarmLevel,          /* 执行当前语句并推进处理流程。 */
			Model:         aiModelName(e.AI),         /* 执行当前语句并推进处理流程。 */
			PromptVersion: "fallback-v2",             /* 执行当前语句并推进处理流程。 */
			CreatedAt:     e.Clock.Now().UnixMilli(), /* 执行当前语句并推进处理流程。 */
			Error:         err.Error(),               /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if err == nil && e.Metrics != nil { /* 判断条件并选择处理分支。 */
		e.Metrics.Inc("ai_analysis_success_total") /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	analysis.TenantID = alarm.TenantID                                   /* 更新 analysis.TenantID 的值。 */
	analysis.AlarmID = alarm.ID                                          /* 更新 analysis.AlarmID 的值。 */
	if saveErr := e.Repo.SaveAIAnalysis(ctx, analysis); saveErr != nil { /* 判断条件并选择处理分支。 */
		return analysis, saveErr /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	payload := mustJSON(analysis)                                                  /* 更新 payload 的值。 */
	_ = e.Bus.Publish(ctx, model.TopicAlarmAIAnalysis, alarm.ID, payload)          /* 更新 _ 的值。 */
	_ = e.Realtime.Publish(ctx, alarm.MQTTTopic("ai-analysis"), payload, 1, false) /* 更新 _ 的值。 */
	return analysis, nil                                                           /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func aiModelName(client ports.AIClient) string { /* 定义 aiModelName 函数。 */
	if provider, ok := client.(ports.AIInspectable); ok { /* 判断条件并选择处理分支。 */
		info := provider.ProviderInfo()                        /* 更新 info 的值。 */
		if name := strings.TrimSpace(info.Model); name != "" { /* 判断条件并选择处理分支。 */
			return name /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if id := strings.TrimSpace(info.ID); id != "" { /* 判断条件并选择处理分支。 */
			return id /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return "unavailable" /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (e *Engine) SetAlarmStatus(ctx context.Context, tenant, alarmID, status, actor string) (model.Alarm, error) { /* 定义 SetAlarmStatus 函数。 */
	a, err := e.Repo.GetAlarm(ctx, tenant, alarmID) /* 更新 err 的值。 */
	if err != nil {                                 /* 判断条件并选择处理分支。 */
		return a, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	now := e.Clock.Now().UnixMilli() /* 更新 now 的值。 */
	switch status {                  /* 根据条件选择处理路径。 */
	case "ACKED": /* 处理当前分支。 */
		if a.Status != "ACTIVE" { /* 判断条件并选择处理分支。 */
			return a, fmt.Errorf("only active alarms can be acknowledged") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		a.Status = status /* 更新 a.Status 的值。 */
		a.AckedAt = now   /* 更新 a.AckedAt 的值。 */
	case "CLOSED": /* 处理当前分支。 */
		a.Status = status /* 更新 a.Status 的值。 */
		a.ClosedAt = now  /* 更新 a.ClosedAt 的值。 */
	case "SUPPRESSED": /* 处理当前分支。 */
		a.Status = status /* 更新 a.Status 的值。 */
	default: /* 处理当前分支。 */
		return a, fmt.Errorf("unsupported status %s", status) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err := e.Repo.UpdateAlarm(ctx, a); err != nil { /* 判断条件并选择处理分支。 */
		return a, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err := e.syncDeviceBusinessStatus(ctx, a.TenantID, "", a.DeviceID); err != nil { /* 判断条件并选择处理分支。 */
		return a, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	_ = e.Repo.SaveAudit(ctx, model.AuditLog{ID: id("audit"), TenantID: tenant, Actor: actor, Action: "alarm." + strings.ToLower(status), TargetType: "alarm", TargetID: alarmID, CreatedAt: now}) /* 更新 _ 的值。 */
	payload := mustJSON(a)                                                                                                                                                                         /* 更新 payload 的值。 */
	_ = e.Bus.Publish(ctx, model.TopicAlarmConfirmed, a.ID, payload)                                                                                                                               /* 更新 _ 的值。 */
	_ = e.Realtime.Publish(ctx, a.MQTTTopic("confirmed"), payload, 1, false)                                                                                                                       /* 更新 _ 的值。 */
	return a, nil                                                                                                                                                                                  /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func tag(m model.StandardMessage, k, fallback string) string { /* 定义 tag 函数。 */
	if v := m.Tags[k]; v != "" { /* 判断条件并选择处理分支。 */
		return v /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return fallback /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func id(prefix string) string { /* 定义 id 函数。 */
	b := make([]byte, 12)                       /* 更新 b 的值。 */
	_, _ = rand.Read(b)                         /* 更新 _ 的值。 */
	return prefix + "_" + hex.EncodeToString(b) /* 返回当前处理结果。 */
}                           /* 结束当前表达式或代码块。 */
func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b } /* 定义 mustJSON 函数。 */
