package protocolruntime /* 声明 protocolruntime 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"         /* 执行当前语句并推进处理流程。 */
	"encoding/binary" /* 执行当前语句并推进处理流程。 */
	"encoding/hex"    /* 执行当前语句并推进处理流程。 */
	"encoding/json"   /* 执行当前语句并推进处理流程。 */
	"errors"          /* 执行当前语句并推进处理流程。 */
	"fmt"             /* 执行当前语句并推进处理流程。 */
	"io"              /* 执行当前语句并推进处理流程。 */
	"log/slog"        /* 执行当前语句并推进处理流程。 */
	"net"             /* 执行当前语句并推进处理流程。 */
	"strconv"         /* 执行当前语句并推进处理流程。 */
	"strings"         /* 执行当前语句并推进处理流程。 */
	"sync"            /* 执行当前语句并推进处理流程。 */
	"time"            /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/modbusframe" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"       /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports"       /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

var modbusReadSlots = make(chan struct{}, 32) /* 声明 modbusReadSlots。 */

type IngestFunc func(context.Context, model.RawMessage) error /* 定义 IngestFunc 类型。 */

// ModbusReadError preserves wire evidence for connection tests without changing
// the runtime's retry and collection behavior.
type ModbusReadError struct { /* 定义 ModbusReadError 类型。 */
	Request, Response []byte /* 执行当前语句并推进处理流程。 */
	Cause             error  /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (e *ModbusReadError) Error() string { return e.Cause.Error() } /* 定义 Error 函数。 */
func (e *ModbusReadError) Unwrap() error { return e.Cause }         /* 定义 Unwrap 函数。 */

type ModbusException struct{ Code byte } /* 定义 ModbusException 类型。 */

func (e *ModbusException) Error() string { return fmt.Sprintf("Modbus exception code 0x%02X", e.Code) } /* 定义 Error 函数。 */

// Runtime executes active protocol collection plans. It deliberately depends
// on the repository and an ingest callback rather than the HTTP or core
// packages, keeping the active transport layer separate from parsing.
type Runtime struct { /* 定义 Runtime 类型。 */
	coordinator  *Coordinator         /* 执行当前语句并推进处理流程。 */
	repo         ports.Repository     /* 执行当前语句并推进处理流程。 */
	ingest       IngestFunc           /* 执行当前语句并推进处理流程。 */
	log          *slog.Logger         /* 执行当前语句并推进处理流程。 */
	mu           sync.Mutex           /* 执行当前语句并推进处理流程。 */
	last         map[string]time.Time /* 执行当前语句并推进处理流程。 */
	running      map[string]bool      /* 执行当前语句并推进处理流程。 */
	allowedCIDRs []string             /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func New(repo ports.Repository, ingest IngestFunc, log *slog.Logger, allowedCIDRs ...string) *Runtime { /* 定义 New 函数。 */
	return &Runtime{repo: repo, ingest: ingest, log: log, last: map[string]time.Time{}, running: map[string]bool{}, allowedCIDRs: append([]string(nil), allowedCIDRs...)} /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *Runtime) SetCoordinator(c *Coordinator) { r.coordinator = c } /* 定义 SetCoordinator 函数。 */

func (r *Runtime) Start(ctx context.Context) { /* 定义 Start 函数。 */
	go func() { /* 执行当前语句并推进处理流程。 */
		ticker := time.NewTicker(time.Second) /* 更新 ticker 的值。 */
		defer ticker.Stop()                   /* 安排函数结束时执行清理。 */
		for {                                 /* 循环处理当前数据。 */
			select { /* 根据条件选择处理路径。 */
			case <-ctx.Done(): /* 处理当前分支。 */
				return /* 返回当前处理结果。 */
			case now := <-ticker.C: /* 处理当前分支。 */
				r.scan(ctx, now) /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	}() /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (r *Runtime) scan(ctx context.Context, now time.Time) { /* 定义 scan 函数。 */
	profiles, err := r.repo.ListDeviceAccessProfiles(ctx, "") /* 更新 err 的值。 */
	if err != nil {                                           /* 判断条件并选择处理分支。 */
		r.log.Error("list protocol access profiles", "error", err) /* 执行当前语句并推进处理流程。 */
		return                                                     /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for _, profile := range profiles { /* 循环处理当前数据。 */
		if !profile.Enabled || profile.Mode == "listener" || profile.EdgeNodeID != "" { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		executionCtx := ctx       /* 更新 executionCtx 的值。 */
		if r.coordinator != nil { /* 判断条件并选择处理分支。 */
			var owned bool                                          /* 声明 owned。 */
			executionCtx, owned = r.coordinator.Claim(ctx, profile) /* 更新 owned 的值。 */
			if !owned {                                             /* 判断条件并选择处理分支。 */
				continue /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		release, err := r.repo.GetProtocolRelease(ctx, profile.TenantID, profile.ProtocolID, profile.ProtocolVersion) /* 更新 err 的值。 */
		if err != nil || release.Status != "PUBLISHED" {                                                              /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		blocks, err := releaseBlocks(release) /* 更新 err 的值。 */
		if err != nil {                       /* 判断条件并选择处理分支。 */
			r.updateFailure(ctx, profile, err) /* 执行当前语句并推进处理流程。 */
			continue                           /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		due := make([]model.ModbusReadBlock, 0, len(blocks)) /* 更新 due 的值。 */
		r.mu.Lock()                                          /* 执行当前语句并推进处理流程。 */
		for _, block := range blocks {                       /* 循环处理当前数据。 */
			key := profile.TenantID + "\x00" + profile.ID + "\x00" + block.ID /* 更新 key 的值。 */
			interval := time.Duration(block.PollIntervalSec) * time.Second    /* 更新 interval 的值。 */
			if interval <= 0 {                                                /* 判断条件并选择处理分支。 */
				interval = 10 * time.Second /* 更新 interval 的值。 */
			} /* 结束当前表达式或代码块。 */
			if last := r.last[key]; last.IsZero() || now.Sub(last) >= interval { /* 判断条件并选择处理分支。 */
				due = append(due, block) /* 更新 due 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		profileKey := profile.TenantID + "\x00" + profile.ID               /* 更新 profileKey 的值。 */
		if len(due) > 0 && !r.running[profileKey] && len(r.running) < 32 { /* 判断条件并选择处理分支。 */
			r.running[profileKey] = true /* 更新 r.running[profileKey] 的值。 */
			for _, block := range due {  /* 循环处理当前数据。 */
				r.last[profileKey+"\x00"+block.ID] = now /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
		} else { /* 结束当前表达式或代码块。 */
			due = nil /* 更新 due 的值。 */
		} /* 结束当前表达式或代码块。 */
		r.mu.Unlock()      /* 执行当前语句并推进处理流程。 */
		if len(due) == 0 { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		go r.collect(executionCtx, profile, release, due, profileKey) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (r *Runtime) collect(ctx context.Context, profile model.DeviceAccessProfile, release model.ProtocolRelease, blocks []model.ModbusReadBlock, key string) { /* 定义 collect 函数。 */
	defer func() { r.mu.Lock(); delete(r.running, key); r.mu.Unlock() }()               /* 安排函数结束时执行清理。 */
	raws, err := ReadModbusTCPWithPolicy(ctx, profile, release, blocks, r.allowedCIDRs) /* 更新 err 的值。 */
	if err != nil {                                                                     /* 判断条件并选择处理分支。 */
		r.updateFailure(ctx, profile, err) /* 执行当前语句并推进处理流程。 */
		return                             /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for _, raw := range raws { /* 循环处理当前数据。 */
		if r.coordinator != nil { /* 判断条件并选择处理分支。 */
			if err = r.coordinator.Validate(ctx, profile); err != nil { /* 判断条件并选择处理分支。 */
				r.updateFailure(ctx, profile, err) /* 执行当前语句并推进处理流程。 */
				return                             /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		if err = r.ingest(ctx, raw); err != nil { /* 判断条件并选择处理分支。 */
			r.updateFailure(ctx, profile, err) /* 执行当前语句并推进处理流程。 */
			return                             /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	_, err = r.repo.UpdateDeviceAccessStatus(ctx, profile, "ONLINE", "", time.Now().UnixMilli()) /* 更新 err 的值。 */
	if err != nil && r.log != nil {                                                              /* 判断条件并选择处理分支。 */
		r.log.Warn("save collection status", "profileId", profile.ID, "error", err) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (r *Runtime) updateFailure(ctx context.Context, profile model.DeviceAccessProfile, err error) { /* 定义 updateFailure 函数。 */
	_, saveErr := r.repo.UpdateDeviceAccessStatus(ctx, profile, "ERROR", limitError(err.Error(), 512), time.Now().UnixMilli()) /* 更新 saveErr 的值。 */
	if r.log != nil {                                                                                                          /* 判断条件并选择处理分支。 */
		r.log.Warn("active protocol collection failed", "profileId", profile.ID, "deviceId", profile.DeviceID, "error", err) /* 执行当前语句并推进处理流程。 */
		if saveErr != nil {                                                                                                  /* 判断条件并选择处理分支。 */
			r.log.Warn("save collection status", "profileId", profile.ID, "error", saveErr) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func releaseBlocks(release model.ProtocolRelease) ([]model.ModbusReadBlock, error) { /* 定义 releaseBlocks 函数。 */
	if release.Transport != "MODBUS_TCP" && release.Transport != "MODBUS_RTU" { /* 判断条件并选择处理分支。 */
		return nil, errors.New("中心轮询仅支持 Modbus TCP") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var blocks []model.ModbusReadBlock               /* 声明 blocks。 */
	b, err := json.Marshal(release.Config["blocks"]) /* 更新 err 的值。 */
	if err != nil {                                  /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err = json.Unmarshal(b, &blocks); err != nil { /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("decode collection blocks: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(blocks) == 0 { /* 判断条件并选择处理分支。 */
		return nil, errors.New("protocol release has no collection blocks") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return blocks, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func ReadModbusTCP(ctx context.Context, profile model.DeviceAccessProfile, release model.ProtocolRelease, blocks []model.ModbusReadBlock) ([]model.RawMessage, error) { /* 定义 ReadModbusTCP 函数。 */
	return ReadModbusTCPWithPolicy(ctx, profile, release, blocks, nil) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func ReadModbusTCPWithPolicy(ctx context.Context, profile model.DeviceAccessProfile, release model.ProtocolRelease, blocks []model.ModbusReadBlock, allowedCIDRs []string) ([]model.RawMessage, error) { /* 定义 ReadModbusTCPWithPolicy 函数。 */
	if profile.EdgeNodeID != "" || (profile.Network != "" && profile.Network != "tcp") { /* 判断条件并选择处理分支。 */
		return nil, errors.New("remote Edge execution is not supported by the central runtime") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err := ctx.Err(); err != nil { /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	select { /* 根据条件选择处理路径。 */
	case modbusReadSlots <- struct{}{}: /* 处理当前分支。 */
		defer func() { <-modbusReadSlots }() /* 安排函数结束时执行清理。 */
	default: /* 处理当前分支。 */
		return nil, errors.New("Modbus read concurrency limit reached") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */

	rtu := profile.WireFormat == "rtu_over_tcp"                                                                                        /* 更新 rtu 的值。 */
	if (!rtu && !strings.EqualFold(release.Transport, "MODBUS_TCP")) || (rtu && !strings.EqualFold(release.Transport, "MODBUS_RTU")) { /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("transport %q is not MODBUS_TCP", release.Transport) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if strings.TrimSpace(profile.Host) == "" { /* 判断条件并选择处理分支。 */
		return nil, errors.New("device host is required") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if profile.Port <= 0 || profile.Port > 65535 { /* 判断条件并选择处理分支。 */
		return nil, errors.New("device port is invalid") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if profile.UnitID < 0 || profile.UnitID > 255 { /* 判断条件并选择处理分支。 */
		return nil, errors.New("unitId must be between 0 and 255") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	timeout := time.Duration(profile.TimeoutMs) * time.Millisecond /* 更新 timeout 的值。 */
	if timeout <= 0 {                                              /* 判断条件并选择处理分支。 */
		timeout = 3 * time.Second /* 更新 timeout 的值。 */
	} /* 结束当前表达式或代码块。 */
	dialer := net.Dialer{Timeout: timeout}                                   /* 更新 dialer 的值。 */
	targetHost, err := resolveAllowedTarget(ctx, profile.Host, allowedCIDRs) /* 更新 err 的值。 */
	if err != nil {                                                          /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	unlock, err := lockModbusBus(ctx, net.JoinHostPort(targetHost, strconv.Itoa(profile.Port))) /* 更新 err 的值。 */
	if err != nil {                                                                             /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer unlock()    /* 安排函数结束时执行清理。 */
	var conn net.Conn /* 声明 conn。 */
	defer func() {    /* 安排函数结束时执行清理。 */
		if conn != nil { /* 判断条件并选择处理分支。 */
			_ = conn.Close() /* 更新 _ 的值。 */
		} /* 结束当前表达式或代码块。 */
	}() /* 结束当前表达式或代码块。 */
	raws := make([]model.RawMessage, 0, len(blocks)) /* 更新 raws 的值。 */
	var transaction uint16                           /* 声明 transaction。 */
	for _, block := range blocks {                   /* 循环处理当前数据。 */
		transaction++                                                                   /* 执行当前语句并推进处理流程。 */
		var response []byte                                                             /* 声明 response。 */
		request, buildErr := buildReadRequest(transaction, byte(profile.UnitID), block) /* 更新 buildErr 的值。 */
		if buildErr != nil {                                                            /* 判断条件并选择处理分支。 */
			return nil, buildErr /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if rtu { /* 判断条件并选择处理分支。 */
			request = modbusframe.AppendCRC(append([]byte(nil), request[6:]...)) /* 更新 request 的值。 */
		} /* 结束当前表达式或代码块。 */
		startedAt := time.Now()         /* 更新 startedAt 的值。 */
		attempts := profile.Retries + 1 /* 更新 attempts 的值。 */
		if attempts < 1 {               /* 判断条件并选择处理分支。 */
			attempts = 1 /* 更新 attempts 的值。 */
		} /* 结束当前表达式或代码块。 */
		for attempt := 0; attempt < attempts; attempt++ { /* 循环处理当前数据。 */
			if ctx.Err() != nil { /* 判断条件并选择处理分支。 */
				return nil, ctx.Err() /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			if conn == nil { /* 判断条件并选择处理分支。 */
				conn, err = dialer.DialContext(ctx, "tcp", net.JoinHostPort(targetHost, strconv.Itoa(profile.Port))) /* 更新 err 的值。 */
				if err != nil {                                                                                      /* 判断条件并选择处理分支。 */
					continue /* 执行当前语句并推进处理流程。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
			if err = setDeadline(conn, ctx, timeout); err != nil { /* 判断条件并选择处理分支。 */
				_ = conn.Close() /* 更新 _ 的值。 */
				conn = nil       /* 更新 conn 的值。 */
				continue         /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			active := conn                                                      /* 更新 active 的值。 */
			stopCancel := context.AfterFunc(ctx, func() { _ = active.Close() }) /* 更新 stopCancel 的值。 */
			if _, err = conn.Write(request); err == nil {                       /* 判断条件并选择处理分支。 */
				if rtu { /* 判断条件并选择处理分支。 */
					response, err = readRTUResponse(conn, byte(profile.UnitID), byte(block.FunctionCode)) /* 更新 err 的值。 */
				} else { /* 结束当前表达式或代码块。 */
					response, err = readResponse(conn, transaction, byte(profile.UnitID), byte(block.FunctionCode)) /* 更新 err 的值。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
			stopCancel()          /* 执行当前语句并推进处理流程。 */
			if ctx.Err() != nil { /* 判断条件并选择处理分支。 */
				return nil, ctx.Err() /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			if err == nil { /* 判断条件并选择处理分支。 */
				break /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			_ = conn.Close() /* 更新 _ 的值。 */
			conn = nil       /* 更新 conn 的值。 */
		} /* 结束当前表达式或代码块。 */
		if err != nil { /* 判断条件并选择处理分支。 */
			return nil, &ModbusReadError{Request: request, Response: response, Cause: fmt.Errorf("read block %s: %w", block.ID, err)} /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if err = validateReadByteCount(response, block, rtu); err != nil { /* 判断条件并选择处理分支。 */
			return nil, &ModbusReadError{Request: request, Response: response, Cause: err} /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		payload, _ := json.Marshal(strings.ToUpper(hex.EncodeToString(response))) /* 更新 _ 的值。 */
		now := time.Now()                                                         /* 更新 now 的值。 */
		transport := "MODBUS_TCP"                                                 /* 更新 transport 的值。 */
		if rtu {                                                                  /* 判断条件并选择处理分支。 */
			transport = "MODBUS_RTU_TCP" /* 更新 transport 的值。 */
		} /* 结束当前表达式或代码块。 */
		raws = append(raws, model.RawMessage{MessageID: fmt.Sprintf("raw_modbus_%d_%d", now.UnixNano(), transaction), Source: "modbus-tcp-collector", TenantID: profile.TenantID, ProductID: profile.ProductID, DeviceID: profile.DeviceID, Protocol: "modbus-tcp", Transport: transport, ReceivedAt: now.UnixMilli(), PayloadFormat: "hex", Payload: payload, RemoteAddress: net.JoinHostPort(profile.Host, strconv.Itoa(profile.Port)), ProtocolID: release.ProtocolID, ProtocolVersion: release.Version, PointTableVersion: release.PointTableVersion, CollectorID: profile.CollectorID, Metadata: map[string]any{"profileId": profile.ID, "blockId": block.ID, "functionCode": block.FunctionCode, "startAddress": block.StartAddress, "quantity": block.Quantity, "transactionId": transaction, "requestHex": strings.ToUpper(hex.EncodeToString(request)), "latencyMs": time.Since(startedAt).Milliseconds(), "wireFormat": profile.WireFormat}}) /* 更新 raws 的值。 */
	} /* 结束当前表达式或代码块。 */
	return raws, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func resolveAllowedTarget(ctx context.Context, host string, allowedCIDRs []string) (string, error) { /* 定义 resolveAllowedTarget 函数。 */
	host = strings.TrimSpace(host) /* 更新 host 的值。 */
	if len(allowedCIDRs) == 0 {    /* 判断条件并选择处理分支。 */
		return host, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	networks := make([]*net.IPNet, 0, len(allowedCIDRs)) /* 更新 networks 的值。 */
	for _, value := range allowedCIDRs {                 /* 循环处理当前数据。 */
		_, network, err := net.ParseCIDR(strings.TrimSpace(value)) /* 更新 err 的值。 */
		if err != nil {                                            /* 判断条件并选择处理分支。 */
			return "", fmt.Errorf("invalid configured Modbus CIDR %q", value) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		networks = append(networks, network) /* 更新 networks 的值。 */
	} /* 结束当前表达式或代码块。 */
	addresses := []net.IP{}                           /* 更新 addresses 的值。 */
	if literal := net.ParseIP(host); literal != nil { /* 判断条件并选择处理分支。 */
		addresses = append(addresses, literal) /* 更新 addresses 的值。 */
	} else { /* 结束当前表达式或代码块。 */
		resolved, err := net.DefaultResolver.LookupIPAddr(ctx, host) /* 更新 err 的值。 */
		if err != nil {                                              /* 判断条件并选择处理分支。 */
			return "", fmt.Errorf("resolve Modbus host: %w", err) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		for _, address := range resolved { /* 循环处理当前数据。 */
			addresses = append(addresses, address.IP) /* 更新 addresses 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	for _, address := range addresses { /* 循环处理当前数据。 */
		for _, network := range networks { /* 循环处理当前数据。 */
			if network.Contains(address) { /* 判断条件并选择处理分支。 */
				return address.String(), nil /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return "", fmt.Errorf("Modbus host %q is outside IOT_MODBUS_ALLOWED_CIDRS", host) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// ResolveAllowedTarget pins a resolved address before protocol libraries connect.
func ResolveAllowedTarget(ctx context.Context, host string, allowedCIDRs []string) (string, error) { /* 定义 ResolveAllowedTarget 函数。 */
	return resolveAllowedTarget(ctx, host, allowedCIDRs) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func buildReadRequest(transaction uint16, unit byte, block model.ModbusReadBlock) ([]byte, error) { /* 定义 buildReadRequest 函数。 */
	if block.FunctionCode < 1 || block.FunctionCode > 4 { /* 判断条件并选择处理分支。 */
		return nil, errors.New("read functionCode must be 01, 02, 03 or 04") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	limit := 125                 /* 更新 limit 的值。 */
	if block.FunctionCode <= 2 { /* 判断条件并选择处理分支。 */
		limit = 2000 /* 更新 limit 的值。 */
	} /* 结束当前表达式或代码块。 */
	if block.StartAddress < 0 || block.StartAddress > 65535 || block.Quantity <= 0 || block.Quantity > limit || block.StartAddress+block.Quantity > 65536 { /* 判断条件并选择处理分支。 */
		return nil, errors.New("Modbus read block is outside protocol limits") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	request := make([]byte, 12)                                           /* 更新 request 的值。 */
	binary.BigEndian.PutUint16(request[0:2], transaction)                 /* 执行当前语句并推进处理流程。 */
	binary.BigEndian.PutUint16(request[4:6], 6)                           /* 执行当前语句并推进处理流程。 */
	request[6] = unit                                                     /* 更新 request[6] 的值。 */
	request[7] = byte(block.FunctionCode)                                 /* 更新 request[7] 的值。 */
	binary.BigEndian.PutUint16(request[8:10], uint16(block.StartAddress)) /* 执行当前语句并推进处理流程。 */
	binary.BigEndian.PutUint16(request[10:12], uint16(block.Quantity))    /* 执行当前语句并推进处理流程。 */
	return request, nil                                                   /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func readResponse(conn net.Conn, transaction uint16, unit, function byte) ([]byte, error) { /* 定义 readResponse 函数。 */
	header := make([]byte, 7)                            /* 更新 header 的值。 */
	if _, err := io.ReadFull(conn, header); err != nil { /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if binary.BigEndian.Uint16(header[0:2]) != transaction { /* 判断条件并选择处理分支。 */
		return nil, errors.New("Modbus transaction id mismatch") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if binary.BigEndian.Uint16(header[2:4]) != 0 { /* 判断条件并选择处理分支。 */
		return nil, errors.New("Modbus protocol id is not zero") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	length := int(binary.BigEndian.Uint16(header[4:6])) /* 更新 length 的值。 */
	if length < 3 || length > 254 {                     /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("invalid Modbus response length %d", length) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if header[6] != unit { /* 判断条件并选择处理分支。 */
		return nil, errors.New("Modbus unit id mismatch") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	rest := make([]byte, length-1)                     /* 更新 rest 的值。 */
	if _, err := io.ReadFull(conn, rest); err != nil { /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(rest) < 2 { /* 判断条件并选择处理分支。 */
		return nil, errors.New("Modbus response PDU is incomplete") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if rest[0]&0x80 != 0 { /* 判断条件并选择处理分支。 */
		return append(header, rest...), &ModbusException{Code: rest[1]} /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if rest[0] != function { /* 判断条件并选择处理分支。 */
		return nil, errors.New("Modbus function code mismatch") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return append(header, rest...), nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func setDeadline(conn net.Conn, ctx context.Context, timeout time.Duration) error { /* 定义 setDeadline 函数。 */
	deadline := time.Now().Add(timeout)                            /* 更新 deadline 的值。 */
	if value, ok := ctx.Deadline(); ok && value.Before(deadline) { /* 判断条件并选择处理分支。 */
		deadline = value /* 更新 deadline 的值。 */
	} /* 结束当前表达式或代码块。 */
	return conn.SetDeadline(deadline) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func limitError(value string, max int) string { /* 定义 limitError 函数。 */
	if len(value) <= max { /* 判断条件并选择处理分支。 */
		return value /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return value[:max] /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
