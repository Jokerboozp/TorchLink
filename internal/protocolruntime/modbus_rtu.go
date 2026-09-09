package protocolruntime

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.bug.st/serial"
	"iot-platform/internal/modbusframe"
	"iot-platform/internal/model"
)

var serialBuses = struct {
	sync.Mutex
	held map[string]bool
}{held: map[string]bool{}}

func ValidateSerialProfile(p model.DeviceAccessProfile) error {
	if p.SerialPort == "" || len(p.SerialPort) > 256 || strings.ContainsRune(p.SerialPort, 0) {
		return errors.New("serialPort is required")
	}
	if p.BaudRate < 300 || p.BaudRate > 115200 {
		return errors.New("baudRate must be between 300 and 115200")
	}
	if p.Parity != "N" && p.Parity != "E" && p.Parity != "O" {
		return errors.New("parity must be N, E or O")
	}
	if p.StopBits != 1 && p.StopBits != 2 {
		return errors.New("stopBits must be 1 or 2")
	}
	if p.UnitID < 1 || p.UnitID > 247 {
		return errors.New("RTU unitId must be between 1 and 247; broadcast reads are not allowed")
	}
	if p.TimeoutMs < 1 || p.TimeoutMs > 30000 || p.Retries < 0 || p.Retries > 5 {
		return errors.New("invalid RTU timeout or retries")
	}
	return nil
}

func ReadModbusRTU(ctx context.Context, p model.DeviceAccessProfile, release model.ProtocolRelease, blocks []model.ModbusReadBlock, allowedPorts []string) ([]model.RawMessage, error) {
	if p.EdgeNodeID != "" {
		return nil, errors.New("RTU must execute on its assigned Edge node")
	}
	if err := ValidateSerialProfile(p); err != nil {
		return nil, err
	}
	if release.Transport != "MODBUS_RTU" || len(blocks) == 0 || len(blocks) > 256 {
		return nil, errors.New("invalid RTU read plan")
	}
	// Explicit local allowlist; remote configuration cannot choose arbitrary files.
	allowed := false
	for _, name := range allowedPorts {
		if strings.TrimSpace(name) == p.SerialPort {
			allowed = true
		}
	}
	if !allowed {
		return nil, errors.New("serial port is outside the node's local allowlist")
	}
	name := p.SerialPort
	if resolved, err := filepath.EvalSymlinks(name); err == nil {
		name = resolved
	}
	serialBuses.Lock()
	if serialBuses.held[name] {
		serialBuses.Unlock()
		return nil, errors.New("serial bus is busy")
	}
	serialBuses.held[name] = true
	serialBuses.Unlock()
	defer func() { serialBuses.Lock(); delete(serialBuses.held, name); serialBuses.Unlock() }()
	mode := &serial.Mode{BaudRate: p.BaudRate, DataBits: 8, StopBits: serial.OneStopBit}
	if p.StopBits == 2 {
		mode.StopBits = serial.TwoStopBits
	}
	if p.Parity == "E" {
		mode.Parity = serial.EvenParity
	}
	if p.Parity == "O" {
		mode.Parity = serial.OddParity
	}
	port, err := serial.Open(name, mode)
	if err != nil {
		return nil, err
	}
	defer port.Close()
	stop := context.AfterFunc(ctx, func() { _ = port.Close() })
	defer stop()
	if err = port.SetReadTimeout(50 * time.Millisecond); err != nil {
		return nil, err
	}
	return readRTU(ctx, p, release, blocks, port)
}

type rtuPort interface {
	io.ReadWriteCloser
	ResetInputBuffer() error
}

func readRTU(ctx context.Context, p model.DeviceAccessProfile, release model.ProtocolRelease, blocks []model.ModbusReadBlock, port rtuPort) ([]model.RawMessage, error) {
	raws := make([]model.RawMessage, 0, len(blocks))
	// Modbus specifies a fixed 1.75 ms silence above 19200 baud, otherwise 3.5 characters.
	silence := 1750 * time.Microsecond
	if p.BaudRate <= 19200 {
		silence = time.Duration(38500000000 / int64(p.BaudRate))
	}
	for i, block := range blocks {
		tcpRequest, err := buildReadRequest(uint16(i+1), byte(p.UnitID), block)
		if err != nil {
			return nil, err
		}
		request := modbusframe.AppendCRC(append([]byte(nil), tcpRequest[6:]...))
		var response []byte
		started := time.Now()
		for attempt := 0; attempt <= p.Retries; attempt++ {
			if err = ctx.Err(); err != nil {
				return nil, err
			}
			timer := time.NewTimer(silence)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
			if err = port.ResetInputBuffer(); err != nil {
				return nil, err
			}
			deadline := time.Now().Add(time.Duration(p.TimeoutMs) * time.Millisecond)
			// Closing the port also interrupts a blocked serial driver operation.
			timeout := time.AfterFunc(time.Until(deadline), func() { _ = port.Close() })
			var n int
			n, err = port.Write(request)
			if err == nil && n != len(request) {
				err = io.ErrShortWrite
			}
			if err == nil {
				response, err = readRTUResponse(ctx, port, deadline, byte(p.UnitID), block)
			}
			timeout.Stop()
			if err == nil {
				break
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			var exception *ModbusException
			if errors.As(err, &exception) || !time.Now().Before(deadline) {
				break
			}
		}
		if err != nil {
			return nil, &ModbusReadError{Request: request, Response: response, Cause: fmt.Errorf("read RTU block %s: %w", block.ID, err)}
		}
		payload, _ := json.Marshal(strings.ToUpper(hex.EncodeToString(response)))
		now := time.Now()
		raws = append(raws, model.RawMessage{MessageID: fmt.Sprintf("raw_rtu_%d_%d", now.UnixNano(), i), TenantID: p.TenantID, ProductID: p.ProductID, DeviceID: p.DeviceID, Source: "modbus-rtu-collector", Transport: "MODBUS_RTU", Protocol: release.ProtocolID, ProtocolID: release.ProtocolID, ProtocolVersion: release.Version, PointTableVersion: release.PointTableVersion, CollectorID: p.CollectorID, ReceivedAt: now.UnixMilli(), PayloadFormat: "hex", Payload: payload, RemoteAddress: p.SerialPort, Metadata: map[string]any{"profileId": p.ID, "blockId": block.ID, "startAddress": block.StartAddress, "functionCode": block.FunctionCode, "quantity": block.Quantity, "requestHex": strings.ToUpper(hex.EncodeToString(request)), "latencyMs": time.Since(started).Milliseconds()}})
	}
	return raws, nil
}

func readRTUResponse(ctx context.Context, port io.Reader, deadline time.Time, unit byte, block model.ModbusReadBlock) ([]byte, error) {
	frame := make([]byte, 0, 256)
	expected := 3
	for len(frame) < expected {
		if err := ctx.Err(); err != nil {
			return frame, err
		}
		if !time.Now().Before(deadline) {
			return frame, context.DeadlineExceeded
		}
		buf := make([]byte, expected-len(frame))
		n, err := port.Read(buf)
		frame = append(frame, buf[:n]...)
		if err != nil {
			return frame, err
		}
		if len(frame) == 3 && expected == 3 {
			if frame[0] != unit {
				return frame, errors.New("RTU unit id mismatch")
			}
			if frame[1] != byte(block.FunctionCode) && frame[1] != byte(block.FunctionCode)|0x80 {
				return frame, errors.New("RTU function code mismatch")
			}
			if frame[1]&0x80 != 0 {
				expected = 5
			} else {
				bytes := block.Quantity * 2
				if block.FunctionCode <= 2 {
					bytes = (block.Quantity + 7) / 8
				}
				if int(frame[2]) != bytes {
					return frame, errors.New("RTU byte count does not match read request")
				}
				expected = 5 + bytes
			}
		}
	}
	if err := modbusframe.Validate(frame); err != nil {
		return frame, err
	}
	if frame[1]&0x80 != 0 {
		return frame, &ModbusException{Code: frame[2]}
	}
	return frame, nil
}
