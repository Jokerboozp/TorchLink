package gb26875

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	gb26875ControlLength = 25
	gb26875MaxDataLength = 512
)

// Frame contains a validated complete GB/T 26875.3-2011 frame.
type Frame struct {
	Sequence     uint16
	VersionMajor byte
	VersionMinor byte
	Source       string
	Destination  string
	Command      byte
	Data         []byte
	FrameTime    int64
}

func Decode(raw RawMessage) (*StandardMessage, error) {
	text := strings.TrimSpace(strings.Trim(string(raw.Payload), `"`))
	text = strings.NewReplacer(" ", "", "\r", "", "\n", "", "\t", "").Replace(text)
	data, err := hex.DecodeString(text)
	if err != nil {
		return nil, fmt.Errorf("invalid hex payload: %w", err)
	}
	frame, err := DecodeFrame(data)
	if err != nil {
		return nil, err
	}
	msg := &StandardMessage{
		MessageID:    "msg_" + strings.TrimPrefix(raw.MessageID, "raw_"),
		RawMessageID: raw.MessageID,
		TenantID:     raw.TenantID,
		ProductID:    raw.ProductID,
		DeviceID:     raw.DeviceID,
		MessageType:  PropertyReport,
		Timestamp:    raw.ReceivedAt,
		Properties:   map[string]any{},
		Event:        map[string]any{},
		Tags:         map[string]string{"protocol": "GB/T 26875.3-2011", "terminalVendor": "Dahua"},
		Raw: map[string]any{
			"payloadFormat": "hex", "payload": strings.ToUpper(text), "sequence": frame.Sequence,
			"protocolVersion": fmt.Sprintf("%d.%02d", frame.VersionMajor, frame.VersionMinor),
			"sourceAddress":   frame.Source, "destinationAddress": frame.Destination,
			"command": fmt.Sprintf("0x%02X", frame.Command),
		},
	}
	if frame.FrameTime > 0 {
		msg.Timestamp = frame.FrameTime
	}
	if err := applyGB26875Data(msg, frame); err != nil {
		return nil, err
	}
	return msg, nil
}

func DecodeFrame(data []byte) (Frame, error) {
	if len(data) < 2+gb26875ControlLength+1+2 {
		return Frame{}, errors.New("GB26875 frame is too short")
	}
	if data[0] != '@' || data[1] != '@' {
		return Frame{}, errors.New("GB26875 start marker must be @@")
	}
	if data[len(data)-2] != '#' || data[len(data)-1] != '#' {
		return Frame{}, errors.New("GB26875 end marker must be ##")
	}
	dataLength := int(binary.LittleEndian.Uint16(data[24:26]))
	if dataLength > gb26875MaxDataLength {
		return Frame{}, fmt.Errorf("GB26875 application data length %d exceeds %d", dataLength, gb26875MaxDataLength)
	}
	wantLength := 2 + gb26875ControlLength + dataLength + 1 + 2
	if len(data) != wantLength {
		return Frame{}, fmt.Errorf("GB26875 frame length mismatch: header says %d application bytes, frame has %d bytes", dataLength, len(data))
	}
	var sum byte
	for _, value := range data[2 : 27+dataLength] {
		sum += value
	}
	if got := data[27+dataLength]; got != sum {
		return Frame{}, fmt.Errorf("GB26875 checksum mismatch: got %02X, want %02X", got, sum)
	}
	return Frame{
		Sequence: binary.LittleEndian.Uint16(data[2:4]), VersionMajor: data[4], VersionMinor: data[5],
		FrameTime: decodeGB26875Time(data[6:12]), Source: strings.ToUpper(hex.EncodeToString(data[12:18])),
		Destination: strings.ToUpper(hex.EncodeToString(data[18:24])), Command: data[26], Data: append([]byte(nil), data[27:27+dataLength]...),
	}, nil
}

func applyGB26875Data(msg *StandardMessage, frame Frame) error {
	if frame.Command == 0x07 && len(frame.Data) == 0 {
		msg.Event = map[string]any{"type": "KEEPALIVE"}
		return nil
	}
	if frame.Command == 0x03 && len(frame.Data) == 0 {
		msg.MessageType = CommandReply
		msg.Event = map[string]any{"type": "ACK"}
		return nil
	}
	if len(frame.Data) < 2 {
		return fmt.Errorf("GB26875 command 0x%02X requires an application data header", frame.Command)
	}
	typeFlag, count := frame.Data[0], int(frame.Data[1])
	msg.Raw["typeFlag"] = fmt.Sprintf("0x%02X", typeFlag)
	msg.Raw["objectCount"] = count
	if frame.Command == 0x00 && typeFlag == 0x00 {
		if count != 1 || len(frame.Data) != 112 {
			return fmt.Errorf("GB26875 registration expects one 110-byte object")
		}
		msg.MessageType = StateChange
		msg.Event = map[string]any{"type": "REGISTER", "objectCount": count}
		msg.Properties = map[string]any{"registered": true, "registrationObjectLength": len(frame.Data[2:]), "registrationPayload": strings.ToUpper(hex.EncodeToString(frame.Data[2:]))}
		return nil
	}
	if frame.Command == 0x01 && typeFlag == 0x5A {
		if count != 1 || len(frame.Data) != 8 {
			return fmt.Errorf("GB26875 time synchronization expects one 6-byte time object")
		}
		at := decodeGB26875Time(frame.Data[2:])
		if isGB26875PlatformAddress(frame.Source) {
			msg.MessageType = CommandReply
			msg.Event = map[string]any{"type": "TIME_SYNC", "synchronizedAt": at}
		} else {
			msg.MessageType = EventReport
			msg.Event = map[string]any{"type": "TIME_SYNC_REQUEST", "requestedAt": at}
		}
		msg.Properties = map[string]any{"requestedAt": at}
		return nil
	}
	if frame.Command == 0x02 && typeFlag == 0x02 {
		return applyGB26875ComponentStatus(msg, frame.Data[2:], count)
	}
	return fmt.Errorf("unsupported GB26875 command/type combination 0x%02X/0x%02X", frame.Command, typeFlag)
}

// BuildGB26875RegistrationFrame builds the 112-byte v1.03 registration
// application unit: a 2-byte data identifier followed by the protocol's
// 110-byte registration object. Fields not needed by the virtual device remain
// zero-filled.
func BuildGB26875RegistrationFrame(sequence uint16, source [6]byte, at time.Time) []byte {
	application := make([]byte, 112)
	application[0], application[1] = 0x00, 0x01
	application[2], application[3] = 0x01, 0x03
	copy(application[18:34], []byte(strings.ToUpper(hex.EncodeToString(source[:]))))
	return buildGB26875Frame(sequence, source, [6]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, 0x00, application, at)
}

func applyGB26875ComponentStatus(msg *StandardMessage, data []byte, count int) error {
	const objectLength = 46
	if count < 1 || len(data) != count*objectLength {
		return fmt.Errorf("GB26875 component status expects %d objects (%d bytes), got %d bytes", count, count*objectLength, len(data))
	}
	objects := make([]map[string]any, 0, count)
	anyAlarm, anyFault := false, false
	for i := 0; i < count; i++ {
		v := data[i*objectLength : (i+1)*objectLength]
		status := binary.LittleEndian.Uint16(v[7:9])
		descriptionBytes := v[9:40]
		if cut := bytesIndex(descriptionBytes, 0); cut >= 0 {
			descriptionBytes = descriptionBytes[:cut]
		}
		description := strings.TrimSpace(string(descriptionBytes))
		if !utf8.ValidString(description) {
			description = strings.ToUpper(hex.EncodeToString(descriptionBytes))
		}
		object := map[string]any{
			"systemType": int(v[0]), "systemAddress": int(v[1]), "componentType": int(v[2]),
			"componentTypeName": gb26875ComponentName(v[2]), "circuitAddress": int(binary.LittleEndian.Uint16(v[3:5])),
			"nodeAddress": int(binary.LittleEndian.Uint16(v[5:7])), "statusWord": int(status), "description": description,
			"occurredAt": decodeGB26875Time(v[40:46]),
			"test":       status&1 != 0, "fireAlarm": status&(1<<1) != 0, "fault": status&(1<<2) != 0,
			"shielded": status&(1<<3) != 0, "supervision": status&(1<<4) != 0, "started": status&(1<<5) != 0,
			"feedback": status&(1<<6) != 0, "delayed": status&(1<<7) != 0, "powerFault": status&(1<<8) != 0,
			"offline": status&(1<<9) != 0, "openCircuit": status&(1<<10) != 0, "shortCircuit": status&(1<<11) != 0,
			"removed": status&(1<<12) != 0, "sensorFault": status&(1<<14) != 0, "upgradeFault": status&(1<<15) != 0,
		}
		objects = append(objects, object)
		anyAlarm = anyAlarm || status&(1<<1) != 0
		anyFault = anyFault || status&(1<<2|1<<8|1<<9|1<<10|1<<11|1<<12|1<<14|1<<15) != 0
	}
	first := objects[0]
	msg.Properties = map[string]any{
		"fireAlarm": anyAlarm, "fault": anyFault, "componentType": first["componentType"],
		"componentTypeName": first["componentTypeName"], "circuitAddress": first["circuitAddress"],
		"nodeAddress": first["nodeAddress"], "statusWord": first["statusWord"], "started": first["started"],
		"feedback": first["feedback"], "shielded": first["shielded"], "supervision": first["supervision"],
		"offline": first["offline"], "powerFault": first["powerFault"], "openCircuit": first["openCircuit"],
		"shortCircuit": first["shortCircuit"], "removed": first["removed"], "sensorFault": first["sensorFault"],
		"objects": objects,
	}
	msg.Event = map[string]any{"type": "COMPONENT_STATUS", "alarm": anyAlarm, "fault": anyFault, "objects": objects}
	if anyAlarm || anyFault {
		msg.MessageType = AlarmReport
	} else {
		msg.MessageType = StateChange
	}
	return nil
}

func bytesIndex(data []byte, value byte) int {
	for i, b := range data {
		if b == value {
			return i
		}
	}
	return -1
}

func decodeGB26875Time(data []byte) int64 {
	if len(data) != 6 {
		return 0
	}
	values := make([]int, 6)
	for i, value := range data {
		values[i] = decodeBCD(value)
	}
	year := 2000 + values[5]
	t := time.Date(year, time.Month(values[4]), values[3], values[2], values[1], values[0], 0, DeviceLocation)
	if values[4] < 1 || values[4] > 12 || values[3] < 1 || values[3] > 31 || values[2] > 23 || values[1] > 59 || values[0] > 59 || t.Year() != year {
		return 0
	}
	return t.UnixMilli()
}

func decodeBCD(value byte) int {
	if value>>4 <= 9 && value&0x0f <= 9 {
		return int(value>>4)*10 + int(value&0x0f)
	}
	return int(value)
}

func gb26875ComponentName(value byte) string {
	switch value {
	case 23:
		return "手动火灾报警按钮"
	case 30:
		return "感温火灾探测器"
	case 40:
		return "感烟火灾探测器"
	case 137:
		return "声光报警器"
	default:
		return fmt.Sprintf("部件类型%d", value)
	}
}

// BuildGB26875ComponentStatusFrame builds a standards-shaped frame for test
// devices and protocol conformance tests.
func BuildGB26875ComponentStatusFrame(sequence uint16, source [6]byte, systemType, systemAddress, componentType byte, circuitAddress, nodeAddress, status uint16, description string, at time.Time) []byte {
	object := make([]byte, 46)
	object[0], object[1], object[2] = systemType, systemAddress, componentType
	binary.LittleEndian.PutUint16(object[3:5], circuitAddress)
	binary.LittleEndian.PutUint16(object[5:7], nodeAddress)
	binary.LittleEndian.PutUint16(object[7:9], status)
	copy(object[9:40], []byte(description))
	copy(object[40:46], encodeGB26875Time(at))
	return buildGB26875Frame(sequence, source, [6]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, 0x02, append([]byte{0x02, 0x01}, object...), at)
}

// BuildGB26875AckFrame builds the platform confirmation required after a valid
// device upload. The platform address is all F and the destination is the
// device source address from the received frame.
func BuildGB26875AckFrame(sequence uint16, destination [6]byte, at time.Time) []byte {
	return buildGB26875Frame(sequence, [6]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, destination, 0x03, nil, at)
}

// BuildGB26875TimeSyncFrame builds the platform response to a device time
// synchronization request, or the optional platform-initiated time sync
// command. The v1.03 frame uses command 0x01 and a type 0x5A, one-object,
// six-byte BCD time application unit.
func BuildGB26875TimeSyncFrame(sequence uint16, destination [6]byte, at time.Time) []byte {
	application := append([]byte{0x5A, 0x01}, encodeGB26875Time(at)...)
	return buildGB26875Frame(sequence, [6]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, destination, 0x01, application, at)
}

// BuildGB26875TimeSyncRequestFrame builds the optional device-originated time
// synchronization request used by the v1.03 request/response flow.
func BuildGB26875TimeSyncRequestFrame(sequence uint16, source [6]byte, at time.Time) []byte {
	application := append([]byte{0x5A, 0x01}, encodeGB26875Time(at)...)
	return buildGB26875Frame(sequence, source, [6]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, 0x01, application, at)
}

func buildGB26875Frame(sequence uint16, source, destination [6]byte, command byte, application []byte, at time.Time) []byte {
	frame := make([]byte, 2+gb26875ControlLength+len(application)+1+2)
	copy(frame[:2], "@@")
	binary.LittleEndian.PutUint16(frame[2:4], sequence)
	frame[4], frame[5] = 0x01, 0x03
	copy(frame[6:12], encodeGB26875Time(at))
	copy(frame[12:18], source[:])
	copy(frame[18:24], destination[:])
	binary.LittleEndian.PutUint16(frame[24:26], uint16(len(application)))
	frame[26] = command
	copy(frame[27:], application)
	var sum byte
	for _, value := range frame[2 : 27+len(application)] {
		sum += value
	}
	frame[27+len(application)] = sum
	copy(frame[28+len(application):], "##")
	return frame
}

func encodeGB26875Time(at time.Time) []byte {
	at = at.In(DeviceLocation)
	return []byte{encodeBCD(at.Second()), encodeBCD(at.Minute()), encodeBCD(at.Hour()), encodeBCD(at.Day()), encodeBCD(int(at.Month())), encodeBCD(at.Year() % 100)}
}

func encodeBCD(value int) byte { return byte(value/10<<4 | value%10) }

func isGB26875PlatformAddress(value string) bool {
	return strings.EqualFold(value, "FFFFFFFFFFFF") || strings.EqualFold(value, "000000000000")
}

func MarshalHexPayload(frame []byte) json.RawMessage {
	value, _ := json.Marshal(strings.ToUpper(hex.EncodeToString(frame)))
	return value
}
