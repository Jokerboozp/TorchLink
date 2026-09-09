package protocolruntime

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

var modbusReadSlots = make(chan struct{}, 32)

type IngestFunc func(context.Context, model.RawMessage) error

// ModbusReadError preserves wire evidence for connection tests without changing
// the runtime's retry and collection behavior.
type ModbusReadError struct {
	Request, Response []byte
	Cause             error
}

func (e *ModbusReadError) Error() string { return e.Cause.Error() }
func (e *ModbusReadError) Unwrap() error { return e.Cause }

type ModbusException struct{ Code byte }

func (e *ModbusException) Error() string { return fmt.Sprintf("Modbus exception code 0x%02X", e.Code) }

// Runtime executes active protocol collection plans. It deliberately depends
// on the repository and an ingest callback rather than the HTTP or core
// packages, keeping the active transport layer separate from parsing.
type Runtime struct {
	readOther    func(context.Context, model.DeviceAccessProfile, model.ProtocolRelease) ([]model.RawMessage, error)
	coordinator  *Coordinator
	repo         ports.Repository
	ingest       IngestFunc
	log          *slog.Logger
	mu           sync.Mutex
	last         map[string]time.Time
	running      map[string]bool
	allowedCIDRs []string
	serialPorts  []string
}

func New(repo ports.Repository, ingest IngestFunc, log *slog.Logger, allowedCIDRs ...string) *Runtime {
	return &Runtime{repo: repo, ingest: ingest, log: log, last: map[string]time.Time{}, running: map[string]bool{}, allowedCIDRs: append([]string(nil), allowedCIDRs...)}
}

func (r *Runtime) SetCoordinator(c *Coordinator) { r.coordinator = c }
func (r *Runtime) SetCollector(read func(context.Context, model.DeviceAccessProfile, model.ProtocolRelease) ([]model.RawMessage, error)) {
	r.readOther = read
}
func (r *Runtime) SetSerialPorts(ports []string) { r.serialPorts = append([]string(nil), ports...) }

func (r *Runtime) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				r.scan(ctx, now)
			}
		}
	}()
}

func (r *Runtime) scan(ctx context.Context, now time.Time) {
	profiles, err := r.repo.ListDeviceAccessProfiles(ctx, "")
	if err != nil {
		r.log.Error("list protocol access profiles", "error", err)
		return
	}
	for _, profile := range profiles {
		if !profile.Enabled || profile.Mode == "listener" || profile.EdgeNodeID != "" {
			continue
		}
		executionCtx := ctx
		if r.coordinator != nil {
			var owned bool
			executionCtx, owned = r.coordinator.Claim(ctx, profile)
			if !owned {
				continue
			}
		}
		release, err := r.repo.GetProtocolRelease(ctx, profile.TenantID, profile.ProtocolID, profile.ProtocolVersion)
		if err != nil || release.Status != "PUBLISHED" {
			continue
		}
		blocks, err := releaseBlocks(release)
		if err != nil {
			r.updateFailure(ctx, profile, err)
			continue
		}
		due := make([]model.ModbusReadBlock, 0, len(blocks))
		r.mu.Lock()
		for _, block := range blocks {
			key := profile.TenantID + "\x00" + profile.ID + "\x00" + block.ID
			interval := time.Duration(block.PollIntervalSec) * time.Second
			if interval <= 0 {
				interval = 10 * time.Second
			}
			if last := r.last[key]; last.IsZero() || now.Sub(last) >= interval {
				due = append(due, block)
			}
		}
		profileKey := profile.TenantID + "\x00" + profile.ID
		if len(due) > 0 && !r.running[profileKey] && len(r.running) < 32 {
			r.running[profileKey] = true
			for _, block := range due {
				r.last[profileKey+"\x00"+block.ID] = now
			}
		} else {
			due = nil
		}
		r.mu.Unlock()
		if len(due) == 0 {
			continue
		}
		go r.collect(executionCtx, profile, release, due, profileKey)
	}
}

func (r *Runtime) collect(ctx context.Context, profile model.DeviceAccessProfile, release model.ProtocolRelease, blocks []model.ModbusReadBlock, key string) {
	defer func() { r.mu.Lock(); delete(r.running, key); r.mu.Unlock() }()
	var raws []model.RawMessage
	var err error
	if r.readOther != nil && (release.Transport == "OPC_UA" || (release.Transport == "SNMP" || release.Transport == "BACNET")) {
		raws, err = r.readOther(ctx, profile, release)
	} else if release.Transport == "MODBUS_RTU" {
		raws, err = ReadModbusRTU(ctx, profile, release, blocks, r.serialPorts)
	} else {
		raws, err = ReadModbusTCPWithPolicy(ctx, profile, release, blocks, r.allowedCIDRs)
	}
	if err != nil {
		r.updateFailure(ctx, profile, err)
		return
	}
	for _, raw := range raws {
		if r.coordinator != nil {
			if err = r.coordinator.Validate(ctx, profile); err != nil {
				r.updateFailure(ctx, profile, err)
				return
			}
		}
		if err = r.ingest(ctx, raw); err != nil {
			r.updateFailure(ctx, profile, err)
			return
		}
	}
	_, err = r.repo.UpdateDeviceAccessStatus(ctx, profile, "ONLINE", "", time.Now().UnixMilli())
	if err != nil && r.log != nil {
		r.log.Warn("save collection status", "profileId", profile.ID, "error", err)
	}
}

func (r *Runtime) updateFailure(ctx context.Context, profile model.DeviceAccessProfile, err error) {
	_, saveErr := r.repo.UpdateDeviceAccessStatus(ctx, profile, "ERROR", limitError(err.Error(), 512), time.Now().UnixMilli())
	if r.log != nil {
		r.log.Warn("active protocol collection failed", "profileId", profile.ID, "deviceId", profile.DeviceID, "error", err)
		if saveErr != nil {
			r.log.Warn("save collection status", "profileId", profile.ID, "error", saveErr)
		}
	}
}

func releaseBlocks(release model.ProtocolRelease) ([]model.ModbusReadBlock, error) {
	if release.Transport == "OPC_UA" || (release.Transport == "SNMP" || release.Transport == "BACNET") {
		interval := 10
		if b, err := json.Marshal(release.Config["pollIntervalSec"]); err == nil {
			_ = json.Unmarshal(b, &interval)
		}
		if interval < 1 || interval > 3600 {
			return nil, errors.New("invalid protocol poll interval")
		}
		return []model.ModbusReadBlock{{ID: "protocol-read", PollIntervalSec: interval}}, nil
	}
	var blocks []model.ModbusReadBlock
	b, err := json.Marshal(release.Config["blocks"])
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(b, &blocks); err != nil {
		return nil, fmt.Errorf("decode collection blocks: %w", err)
	}
	if len(blocks) == 0 {
		return nil, errors.New("protocol release has no collection blocks")
	}
	return blocks, nil
}

func ReadModbusTCP(ctx context.Context, profile model.DeviceAccessProfile, release model.ProtocolRelease, blocks []model.ModbusReadBlock) ([]model.RawMessage, error) {
	return ReadModbusTCPWithPolicy(ctx, profile, release, blocks, nil)
}

func ReadModbusTCPWithPolicy(ctx context.Context, profile model.DeviceAccessProfile, release model.ProtocolRelease, blocks []model.ModbusReadBlock, allowedCIDRs []string) ([]model.RawMessage, error) {
	if profile.EdgeNodeID != "" {
		return nil, errors.New("remote Edge execution is not supported by the central runtime")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case modbusReadSlots <- struct{}{}:
		defer func() { <-modbusReadSlots }()
	default:
		return nil, errors.New("Modbus read concurrency limit reached")
	}

	if !strings.EqualFold(release.Transport, "MODBUS_TCP") {
		return nil, fmt.Errorf("transport %q is not MODBUS_TCP", release.Transport)
	}
	if strings.TrimSpace(profile.Host) == "" {
		return nil, errors.New("device host is required")
	}
	if profile.Port <= 0 || profile.Port > 65535 {
		return nil, errors.New("device port is invalid")
	}
	if profile.UnitID < 0 || profile.UnitID > 255 {
		return nil, errors.New("unitId must be between 0 and 255")
	}
	timeout := time.Duration(profile.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	dialer := net.Dialer{Timeout: timeout}
	targetHost, err := resolveAllowedTarget(ctx, profile.Host, allowedCIDRs)
	if err != nil {
		return nil, err
	}
	var conn net.Conn
	defer func() {
		if conn != nil {
			_ = conn.Close()
		}
	}()
	raws := make([]model.RawMessage, 0, len(blocks))
	var transaction uint16
	for _, block := range blocks {
		transaction++
		var response []byte
		request, buildErr := buildReadRequest(transaction, byte(profile.UnitID), block)
		if buildErr != nil {
			return nil, buildErr
		}
		startedAt := time.Now()
		attempts := profile.Retries + 1
		if attempts < 1 {
			attempts = 1
		}
		for attempt := 0; attempt < attempts; attempt++ {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if conn == nil {
				conn, err = dialer.DialContext(ctx, "tcp", net.JoinHostPort(targetHost, strconv.Itoa(profile.Port)))
				if err != nil {
					continue
				}
			}
			if err = setDeadline(conn, ctx, timeout); err != nil {
				_ = conn.Close()
				conn = nil
				continue
			}
			active := conn
			stopCancel := context.AfterFunc(ctx, func() { _ = active.Close() })
			if _, err = conn.Write(request); err == nil {
				response, err = readResponse(conn, transaction, byte(profile.UnitID), byte(block.FunctionCode))
			}
			stopCancel()
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if err == nil {
				break
			}
			_ = conn.Close()
			conn = nil
		}
		if err != nil {
			return nil, &ModbusReadError{Request: request, Response: response, Cause: fmt.Errorf("read block %s: %w", block.ID, err)}
		}
		payload, _ := json.Marshal(strings.ToUpper(hex.EncodeToString(response)))
		now := time.Now()
		raws = append(raws, model.RawMessage{MessageID: fmt.Sprintf("raw_modbus_%d_%d", now.UnixNano(), transaction), Source: "modbus-tcp-collector", TenantID: profile.TenantID, ProductID: profile.ProductID, DeviceID: profile.DeviceID, Protocol: "modbus-tcp", Transport: "MODBUS_TCP", ReceivedAt: now.UnixMilli(), PayloadFormat: "hex", Payload: payload, RemoteAddress: net.JoinHostPort(profile.Host, strconv.Itoa(profile.Port)), ProtocolID: release.ProtocolID, ProtocolVersion: release.Version, PointTableVersion: release.PointTableVersion, CollectorID: profile.CollectorID, Metadata: map[string]any{"profileId": profile.ID, "blockId": block.ID, "functionCode": block.FunctionCode, "startAddress": block.StartAddress, "quantity": block.Quantity, "transactionId": transaction, "requestHex": strings.ToUpper(hex.EncodeToString(request)), "latencyMs": time.Since(startedAt).Milliseconds()}})
	}
	return raws, nil
}

func resolveAllowedTarget(ctx context.Context, host string, allowedCIDRs []string) (string, error) {
	host = strings.TrimSpace(host)
	if len(allowedCIDRs) == 0 {
		return host, nil
	}
	networks := make([]*net.IPNet, 0, len(allowedCIDRs))
	for _, value := range allowedCIDRs {
		_, network, err := net.ParseCIDR(strings.TrimSpace(value))
		if err != nil {
			return "", fmt.Errorf("invalid configured Modbus CIDR %q", value)
		}
		networks = append(networks, network)
	}
	addresses := []net.IP{}
	if literal := net.ParseIP(host); literal != nil {
		addresses = append(addresses, literal)
	} else {
		resolved, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return "", fmt.Errorf("resolve Modbus host: %w", err)
		}
		for _, address := range resolved {
			addresses = append(addresses, address.IP)
		}
	}
	for _, address := range addresses {
		for _, network := range networks {
			if network.Contains(address) {
				return address.String(), nil
			}
		}
	}
	return "", fmt.Errorf("Modbus host %q is outside IOT_MODBUS_ALLOWED_CIDRS", host)
}

// ResolveAllowedTarget pins a resolved address before protocol libraries connect.
func ResolveAllowedTarget(ctx context.Context, host string, allowedCIDRs []string) (string, error) {
	return resolveAllowedTarget(ctx, host, allowedCIDRs)
}

func buildReadRequest(transaction uint16, unit byte, block model.ModbusReadBlock) ([]byte, error) {
	if block.FunctionCode < 1 || block.FunctionCode > 4 {
		return nil, errors.New("read functionCode must be 01, 02, 03 or 04")
	}
	limit := 125
	if block.FunctionCode <= 2 {
		limit = 2000
	}
	if block.StartAddress < 0 || block.StartAddress > 65535 || block.Quantity <= 0 || block.Quantity > limit || block.StartAddress+block.Quantity > 65536 {
		return nil, errors.New("Modbus read block is outside protocol limits")
	}
	request := make([]byte, 12)
	binary.BigEndian.PutUint16(request[0:2], transaction)
	binary.BigEndian.PutUint16(request[4:6], 6)
	request[6] = unit
	request[7] = byte(block.FunctionCode)
	binary.BigEndian.PutUint16(request[8:10], uint16(block.StartAddress))
	binary.BigEndian.PutUint16(request[10:12], uint16(block.Quantity))
	return request, nil
}
func readResponse(conn net.Conn, transaction uint16, unit, function byte) ([]byte, error) {
	header := make([]byte, 7)
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, err
	}
	if binary.BigEndian.Uint16(header[0:2]) != transaction {
		return nil, errors.New("Modbus transaction id mismatch")
	}
	if binary.BigEndian.Uint16(header[2:4]) != 0 {
		return nil, errors.New("Modbus protocol id is not zero")
	}
	length := int(binary.BigEndian.Uint16(header[4:6]))
	if length < 3 || length > 254 {
		return nil, fmt.Errorf("invalid Modbus response length %d", length)
	}
	if header[6] != unit {
		return nil, errors.New("Modbus unit id mismatch")
	}
	rest := make([]byte, length-1)
	if _, err := io.ReadFull(conn, rest); err != nil {
		return nil, err
	}
	if len(rest) < 2 {
		return nil, errors.New("Modbus response PDU is incomplete")
	}
	if rest[0]&0x80 != 0 {
		return append(header, rest...), &ModbusException{Code: rest[1]}
	}
	if rest[0] != function {
		return nil, errors.New("Modbus function code mismatch")
	}
	return append(header, rest...), nil
}
func setDeadline(conn net.Conn, ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	if value, ok := ctx.Deadline(); ok && value.Before(deadline) {
		deadline = value
	}
	return conn.SetDeadline(deadline)
}
func limitError(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}
