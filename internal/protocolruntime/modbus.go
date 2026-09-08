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
	repo         ports.Repository
	ingest       IngestFunc
	log          *slog.Logger
	mu           sync.Mutex
	last         map[string]time.Time
	running      map[string]bool
	allowedCIDRs []string
}

func New(repo ports.Repository, ingest IngestFunc, log *slog.Logger, allowedCIDRs ...string) *Runtime {
	return &Runtime{repo: repo, ingest: ingest, log: log, last: map[string]time.Time{}, running: map[string]bool{}, allowedCIDRs: append([]string(nil), allowedCIDRs...)}
}

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
		if !profile.Enabled || profile.Mode == "listener" {
			continue
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
				r.last[key] = now
				due = append(due, block)
			}
		}
		profileKey := profile.TenantID + "\x00" + profile.ID
		if len(due) > 0 && !r.running[profileKey] {
			r.running[profileKey] = true
		} else {
			due = nil
		}
		r.mu.Unlock()
		if len(due) == 0 {
			continue
		}
		go r.collect(ctx, profile, release, due, profileKey)
	}
}

func (r *Runtime) collect(ctx context.Context, profile model.DeviceAccessProfile, release model.ProtocolRelease, blocks []model.ModbusReadBlock, key string) {
	defer func() { r.mu.Lock(); delete(r.running, key); r.mu.Unlock() }()
	raws, err := ReadModbusTCPWithPolicy(ctx, profile, release, blocks, r.allowedCIDRs)
	if err != nil {
		r.updateFailure(ctx, profile, err)
		return
	}
	for _, raw := range raws {
		if err = r.ingest(ctx, raw); err != nil {
			r.updateFailure(ctx, profile, err)
			return
		}
	}
	profile.RuntimeStatus = "ONLINE"
	profile.LastSuccessAt = time.Now().UnixMilli()
	profile.LastError = ""
	profile.UpdatedAt = profile.LastSuccessAt
	_ = r.repo.SaveDeviceAccessProfile(ctx, profile)
}

func (r *Runtime) updateFailure(ctx context.Context, profile model.DeviceAccessProfile, err error) {
	profile.RuntimeStatus = "ERROR"
	profile.LastErrorAt = time.Now().UnixMilli()
	profile.LastError = limitError(err.Error(), 512)
	profile.UpdatedAt = profile.LastErrorAt
	_ = r.repo.SaveDeviceAccessProfile(ctx, profile)
	if r.log != nil {
		r.log.Warn("active protocol collection failed", "profileId", profile.ID, "deviceId", profile.DeviceID, "error", err)
	}
}

func releaseBlocks(release model.ProtocolRelease) ([]model.ModbusReadBlock, error) {
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
			if _, err = conn.Write(request); err == nil {
				response, err = readResponse(conn, transaction, byte(profile.UnitID), byte(block.FunctionCode))
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
