package fieldprotocol

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"iot-platform/internal/model"
	"math"
	"net"
	"strconv"
	"strings"
	"unicode/utf8"
)

type bacnetPoint struct {
	Object   uint32
	Property uint32
	Index    *uint32
}

func parseBACnetAddress(address string) (bacnetPoint, error) {
	var p bacnetPoint
	parts := strings.Split(address, ":")
	if len(parts) < 3 || len(parts) > 4 {
		return p, errors.New("BACnet address must be objectType:instance:property[:arrayIndex]")
	}
	numbers := make([]uint32, len(parts))
	for i, part := range parts {
		n, err := strconv.ParseUint(part, 10, 32)
		if err != nil {
			return p, errors.New("invalid BACnet numeric address")
		}
		numbers[i] = uint32(n)
	}
	if numbers[0] > 1023 || numbers[1] > 4194303 {
		return p, errors.New("BACnet object identifier is outside protocol limits")
	}
	p.Object = numbers[0]<<22 | numbers[1]
	p.Property = numbers[2]
	if len(parts) == 4 {
		p.Index = &numbers[3]
	}
	return p, nil
}
func contextUnsigned(tag byte, value uint32) []byte {
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, value)
	for len(buf) > 1 && buf[0] == 0 {
		buf = buf[1:]
	}
	return append([]byte{tag<<4 | 8 | byte(len(buf))}, buf...)
}
func bacnetReferences(p bacnetPoint) []byte {
	data := make([]byte, 5)
	data[0] = 0x0c
	binary.BigEndian.PutUint32(data[1:], p.Object)
	data = append(data, contextUnsigned(1, p.Property)...)
	if p.Index != nil {
		data = append(data, contextUnsigned(2, *p.Index)...)
	}
	return data
}
func readBACnet(ctx context.Context, host string, p model.DeviceAccessProfile, points []model.PollPoint, credential Credential) (model.PollResponse, error) {
	out := model.PollResponse{}
	if credential.BACnetMode != "ip" {
		return out, errors.New("local BACnet mode must explicitly select native BACnet/IP")
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "udp", net.JoinHostPort(host, fmt.Sprint(p.Port)))
	if err != nil {
		return out, err
	}
	defer conn.Close()
	deadline, ok := ctx.Deadline()
	if !ok {
		return out, errors.New("BACnet read requires a deadline")
	}
	if err = conn.SetDeadline(deadline); err != nil {
		return out, err
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	var seed [1]byte
	if _, err = rand.Read(seed[:]); err != nil {
		return out, err
	}
	evidence := []map[string]any{}
	for i, point := range points {
		ref, err := parseBACnetAddress(point.Address)
		if err != nil {
			return out, err
		}
		invoke := seed[0] + byte(i)
		request := append([]byte{0x81, 0x0a, 0, 0, 1, 4, 0, 3, invoke, 12}, bacnetReferences(ref)...)
		binary.BigEndian.PutUint16(request[2:4], uint16(len(request)))
		if _, err = conn.Write(request); err != nil {
			return out, err
		}
		buf := make([]byte, 1500)
		n, err := conn.Read(buf)
		if err != nil {
			return out, err
		}
		response := buf[:n]
		value, err := decodeBACnetRead(response, invoke, ref)
		quality := "GOOD"
		if err != nil {
			quality = err.Error()
		}
		evidence = append(evidence, map[string]any{"requestHex": hex.EncodeToString(request), "responseHex": hex.EncodeToString(response), "address": point.Address})
		out.Values = append(out.Values, model.PollValue{Address: point.Address, Value: value, Quality: quality})
	}
	out.Response = map[string]any{"authentication": "none-native-bacnet-ip", "exchanges": evidence}
	return out, nil
}

func decodeBACnetRead(data []byte, invoke byte, p bacnetPoint) (any, error) {
	if len(data) < 9 || data[0] != 0x81 || data[1] != 0x0a || int(binary.BigEndian.Uint16(data[2:4])) != len(data) {
		return nil, errors.New("invalid BACnet BVLC response")
	}
	// This direct-IP reader accepts unsegmented, unrouted confirmed responses.
	// Unsupported encodings are retained as failed raw evidence, never guessed.
	if data[4] != 1 || data[5]&0xfc != 0 {
		return nil, errors.New("unsupported routed/network BACnet NPDU")
	}
	apdu := data[6:]
	if apdu[1] != invoke {
		return nil, errors.New("BACnet invoke id mismatch")
	}
	if apdu[0]>>4 == 5 || apdu[0]>>4 == 6 || apdu[0]>>4 == 7 {
		return nil, fmt.Errorf("BACnet error/reject/abort PDU: %x", apdu)
	}
	if apdu[0] != 0x30 || apdu[2] != 12 {
		return nil, errors.New("unsupported BACnet response or segmentation")
	}
	references := bacnetReferences(p)
	if len(apdu) < 3+len(references)+3 || !bytes.Equal(apdu[3:3+len(references)], references) {
		return nil, errors.New("BACnet object/property response mismatch")
	}
	value := apdu[3+len(references):]
	if value[0] != 0x3e || value[len(value)-1] != 0x3f {
		return nil, errors.New("invalid BACnet property value container")
	}
	return decodeBACnetScalar(value[1 : len(value)-1])
}
func decodeBACnetScalar(data []byte) (any, error) {
	if len(data) == 0 {
		return nil, errors.New("empty BACnet value")
	}
	tag, n := data[0]>>4, int(data[0]&7)
	if data[0]&8 != 0 || tag == 15 {
		return nil, errors.New("unsupported BACnet value tag")
	}
	payload := data[1:]
	if tag == 1 {
		if len(payload) != 0 || n > 1 {
			return nil, errors.New("invalid BACnet boolean")
		}
		return n == 1, nil
	}
	if n == 5 {
		if len(payload) < 1 {
			return nil, errors.New("truncated BACnet extended length")
		}
		n = int(payload[0])
		payload = payload[1:]
		if n >= 254 {
			return nil, errors.New("BACnet scalar exceeds supported length")
		}
	}
	if n != len(payload) {
		return nil, errors.New("BACnet scalar length mismatch")
	}
	switch tag {
	case 0:
		if n == 0 {
			return nil, nil
		}
	case 2, 3, 9:
		if n >= 1 && n <= 4 {
			v := uint32(0)
			for _, b := range payload {
				v = v<<8 | uint32(b)
			}
			if tag == 3 {
				shift := uint(32 - n*8)
				return int64(int32(v<<shift) >> shift), nil
			}
			return uint64(v), nil
		}
	case 4:
		if n == 4 {
			v := float64(math.Float32frombits(binary.BigEndian.Uint32(payload)))
			if !math.IsNaN(v) && !math.IsInf(v, 0) {
				return v, nil
			}
		}
	case 5:
		if n == 8 {
			v := math.Float64frombits(binary.BigEndian.Uint64(payload))
			if !math.IsNaN(v) && !math.IsInf(v, 0) {
				return v, nil
			}
		}
	case 7:
		if n >= 1 && payload[0] == 0 && utf8.Valid(payload[1:]) {
			return string(payload[1:]), nil
		}
	case 12:
		if n == 4 {
			return fmt.Sprintf("%d:%d", binary.BigEndian.Uint32(payload)>>22, binary.BigEndian.Uint32(payload)&0x3fffff), nil
		}
	}
	return nil, errors.New("unsupported or invalid BACnet scalar")
}
