package protocolruntime

import (
	"context"
	"errors"
	"io"
	"iot-platform/internal/modbusframe"
	"iot-platform/internal/model"
	"net"
	"sync"
)

var modbusBuses = struct {
	sync.Mutex
	entries map[string]*modbusBus
}{entries: map[string]*modbusBus{}}

type modbusBus struct {
	token chan struct{}
	refs  int
}

// One transaction sequence per physical TCP endpoint, including all unit IDs.
func lockModbusBus(ctx context.Context, key string) (func(), error) {
	modbusBuses.Lock()
	b := modbusBuses.entries[key]
	if b == nil {
		b = &modbusBus{token: make(chan struct{}, 1)}
		modbusBuses.entries[key] = b
	}
	b.refs++
	modbusBuses.Unlock()
	releaseRef := func() {
		modbusBuses.Lock()
		b.refs--
		if b.refs == 0 {
			delete(modbusBuses.entries, key)
		}
		modbusBuses.Unlock()
	}
	select {
	case b.token <- struct{}{}:
		return func() { <-b.token; releaseRef() }, nil
	case <-ctx.Done():
		releaseRef()
		return nil, ctx.Err()
	}
}

func readRTUResponse(conn net.Conn, unit, function byte) ([]byte, error) {
	head := make([]byte, 3)
	if _, err := io.ReadFull(conn, head); err != nil {
		return head, err
	}
	if head[0] != unit || (head[1] != function && head[1] != function|0x80) {
		return head, errors.New("Modbus RTU address or function mismatch")
	}
	size := int(head[2]) + 2
	if head[1]&0x80 != 0 {
		size = 2
	}
	if size > 252 {
		return head, errors.New("Modbus RTU response too large")
	}
	tail := make([]byte, size)
	n, err := io.ReadFull(conn, tail)
	frame := append(head, tail[:n]...)
	if err != nil {
		return frame, err
	}
	if err = modbusframe.Validate(frame); err != nil {
		return frame, err
	}
	if head[1]&0x80 != 0 {
		return frame, &ModbusException{Code: head[2]}
	}
	return frame, nil
}

func validateReadByteCount(frame []byte, block model.ModbusReadBlock, rtu bool) error {
	offset := 8
	if rtu {
		offset = 2
	}
	expected := block.Quantity * 2
	if block.FunctionCode <= 2 {
		expected = (block.Quantity + 7) / 8
	}
	if len(frame) <= offset || int(frame[offset]) != expected {
		return errors.New("Modbus response byte count does not match query")
	}
	size := offset + 1 + expected
	if rtu {
		size += 2
	}
	if len(frame) != size {
		return errors.New("Modbus response length does not match query")
	}
	return nil
}
