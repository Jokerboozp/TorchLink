package gb26875 /* 声明 gb26875 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"encoding/binary" /* 执行当前语句并推进处理流程。 */
	"encoding/hex"    /* 执行当前语句并推进处理流程。 */
	"encoding/json"   /* 执行当前语句并推进处理流程。 */
	"errors"          /* 执行当前语句并推进处理流程。 */
	"fmt"             /* 执行当前语句并推进处理流程。 */
	"strings"         /* 执行当前语句并推进处理流程。 */
	"time"            /* 执行当前语句并推进处理流程。 */
	"unicode/utf8"    /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

const ( /* 执行当前语句并推进处理流程。 */
	gb26875ControlLength = 25  /* 更新 gb26875ControlLength 的值。 */
	gb26875MaxDataLength = 512 /* 更新 gb26875MaxDataLength 的值。 */
) /* 结束当前表达式或代码块。 */

// Frame contains a validated complete GB/T 26875.3-2011 frame.
type Frame struct { /* 定义 Frame 类型。 */
	Sequence     uint16 /* 执行当前语句并推进处理流程。 */
	VersionMajor byte   /* 执行当前语句并推进处理流程。 */
	VersionMinor byte   /* 执行当前语句并推进处理流程。 */
	Source       string /* 执行当前语句并推进处理流程。 */
	Destination  string /* 执行当前语句并推进处理流程。 */
	Command      byte   /* 执行当前语句并推进处理流程。 */
	Data         []byte /* 执行当前语句并推进处理流程。 */
	FrameTime    int64  /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func Decode(raw RawMessage) (*StandardMessage, error) { /* 定义 Decode 函数。 */
	text := strings.TrimSpace(strings.Trim(string(raw.Payload), `"`))               /* 更新 text 的值。 */
	text = strings.NewReplacer(" ", "", "\r", "", "\n", "", "\t", "").Replace(text) /* 更新 text 的值。 */
	data, err := hex.DecodeString(text)                                             /* 更新 err 的值。 */
	if err != nil {                                                                 /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("invalid hex payload: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	frame, err := DecodeFrame(data) /* 更新 err 的值。 */
	if err != nil {                 /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	msg := &StandardMessage{ /* 更新 msg 的值。 */
		MessageID:    "msg_" + strings.TrimPrefix(raw.MessageID, "raw_"),                            /* 执行当前语句并推进处理流程。 */
		RawMessageID: raw.MessageID,                                                                 /* 执行当前语句并推进处理流程。 */
		TenantID:     raw.TenantID,                                                                  /* 执行当前语句并推进处理流程。 */
		ProductID:    raw.ProductID,                                                                 /* 执行当前语句并推进处理流程。 */
		DeviceID:     raw.DeviceID,                                                                  /* 执行当前语句并推进处理流程。 */
		MessageType:  PropertyReport,                                                                /* 执行当前语句并推进处理流程。 */
		Timestamp:    raw.ReceivedAt,                                                                /* 执行当前语句并推进处理流程。 */
		Properties:   map[string]any{},                                                              /* 执行当前语句并推进处理流程。 */
		Event:        map[string]any{},                                                              /* 执行当前语句并推进处理流程。 */
		Tags:         map[string]string{"protocol": "GB/T 26875.3-2011", "terminalVendor": "Dahua"}, /* 执行当前语句并推进处理流程。 */
		Raw: map[string]any{ /* 执行当前语句并推进处理流程。 */
			"payloadFormat": "hex", "payload": strings.ToUpper(text), "sequence": frame.Sequence, /* 执行当前语句并推进处理流程。 */
			"protocolVersion": fmt.Sprintf("%d.%02d", frame.VersionMajor, frame.VersionMinor), /* 执行当前语句并推进处理流程。 */
			"sourceAddress":   frame.Source, "destinationAddress": frame.Destination,          /* 执行当前语句并推进处理流程。 */
			"command": fmt.Sprintf("0x%02X", frame.Command), /* 执行当前语句并推进处理流程。 */
		}, /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if frame.FrameTime > 0 { /* 判断条件并选择处理分支。 */
		msg.Timestamp = frame.FrameTime /* 更新 msg.Timestamp 的值。 */
	} /* 结束当前表达式或代码块。 */
	if err := applyGB26875Data(msg, frame); err != nil { /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return msg, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func DecodeFrame(data []byte) (Frame, error) { /* 定义 DecodeFrame 函数。 */
	if len(data) < 2+gb26875ControlLength+1+2 { /* 判断条件并选择处理分支。 */
		return Frame{}, errors.New("GB26875 frame is too short") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if data[0] != '@' || data[1] != '@' { /* 判断条件并选择处理分支。 */
		return Frame{}, errors.New("GB26875 start marker must be @@") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if data[len(data)-2] != '#' || data[len(data)-1] != '#' { /* 判断条件并选择处理分支。 */
		return Frame{}, errors.New("GB26875 end marker must be ##") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	dataLength := int(binary.LittleEndian.Uint16(data[24:26])) /* 更新 dataLength 的值。 */
	if dataLength > gb26875MaxDataLength {                     /* 判断条件并选择处理分支。 */
		return Frame{}, fmt.Errorf("GB26875 application data length %d exceeds %d", dataLength, gb26875MaxDataLength) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	wantLength := 2 + gb26875ControlLength + dataLength + 1 + 2 /* 更新 wantLength 的值。 */
	if len(data) != wantLength {                                /* 判断条件并选择处理分支。 */
		return Frame{}, fmt.Errorf("GB26875 frame length mismatch: header says %d application bytes, frame has %d bytes", dataLength, len(data)) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var sum byte                                    /* 声明 sum。 */
	for _, value := range data[2 : 27+dataLength] { /* 循环处理当前数据。 */
		sum += value /* 更新 sum 的值。 */
	} /* 结束当前表达式或代码块。 */
	if got := data[27+dataLength]; got != sum { /* 判断条件并选择处理分支。 */
		return Frame{}, fmt.Errorf("GB26875 checksum mismatch: got %02X, want %02X", got, sum) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return Frame{ /* 返回当前处理结果。 */
		Sequence: binary.LittleEndian.Uint16(data[2:4]), VersionMajor: data[4], VersionMinor: data[5], /* 执行当前语句并推进处理流程。 */
		FrameTime: decodeGB26875Time(data[6:12]), Source: strings.ToUpper(hex.EncodeToString(data[12:18])), /* 执行当前语句并推进处理流程。 */
		Destination: strings.ToUpper(hex.EncodeToString(data[18:24])), Command: data[26], Data: append([]byte(nil), data[27:27+dataLength]...), /* 执行当前语句并推进处理流程。 */
	}, nil /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func applyGB26875Data(msg *StandardMessage, frame Frame) error { /* 定义 applyGB26875Data 函数。 */
	if frame.Command == 0x07 && len(frame.Data) == 0 { /* 判断条件并选择处理分支。 */
		msg.Event = map[string]any{"type": "KEEPALIVE"} /* 更新 msg.Event 的值。 */
		return nil                                      /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if frame.Command == 0x03 && len(frame.Data) == 0 { /* 判断条件并选择处理分支。 */
		msg.MessageType = CommandReply            /* 更新 msg.MessageType 的值。 */
		msg.Event = map[string]any{"type": "ACK"} /* 更新 msg.Event 的值。 */
		return nil                                /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(frame.Data) < 2 { /* 判断条件并选择处理分支。 */
		return fmt.Errorf("GB26875 command 0x%02X requires an application data header", frame.Command) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	typeFlag, count := frame.Data[0], int(frame.Data[1])  /* 更新 count 的值。 */
	msg.Raw["typeFlag"] = fmt.Sprintf("0x%02X", typeFlag) /* 执行当前语句并推进处理流程。 */
	msg.Raw["objectCount"] = count                        /* 执行当前语句并推进处理流程。 */
	if frame.Command == 0x00 && typeFlag == 0x00 {        /* 判断条件并选择处理分支。 */
		if count != 1 || len(frame.Data) != 112 { /* 判断条件并选择处理分支。 */
			return fmt.Errorf("GB26875 registration expects one 110-byte object") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		msg.MessageType = StateChange                                                                                                                                                    /* 更新 msg.MessageType 的值。 */
		msg.Event = map[string]any{"type": "REGISTER", "objectCount": count}                                                                                                             /* 更新 msg.Event 的值。 */
		msg.Properties = map[string]any{"registered": true, "registrationObjectLength": len(frame.Data[2:]), "registrationPayload": strings.ToUpper(hex.EncodeToString(frame.Data[2:]))} /* 更新 msg.Properties 的值。 */
		return nil                                                                                                                                                                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if frame.Command == 0x01 && typeFlag == 0x5A { /* 判断条件并选择处理分支。 */
		if count != 1 || len(frame.Data) != 8 { /* 判断条件并选择处理分支。 */
			return fmt.Errorf("GB26875 time synchronization expects one 6-byte time object") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		at := decodeGB26875Time(frame.Data[2:])     /* 更新 at 的值。 */
		if isGB26875PlatformAddress(frame.Source) { /* 判断条件并选择处理分支。 */
			msg.MessageType = CommandReply                                        /* 更新 msg.MessageType 的值。 */
			msg.Event = map[string]any{"type": "TIME_SYNC", "synchronizedAt": at} /* 更新 msg.Event 的值。 */
		} else { /* 结束当前表达式或代码块。 */
			msg.MessageType = EventReport                                              /* 更新 msg.MessageType 的值。 */
			msg.Event = map[string]any{"type": "TIME_SYNC_REQUEST", "requestedAt": at} /* 更新 msg.Event 的值。 */
		} /* 结束当前表达式或代码块。 */
		msg.Properties = map[string]any{"requestedAt": at} /* 更新 msg.Properties 的值。 */
		return nil                                         /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if frame.Command == 0x02 && typeFlag == 0x02 { /* 判断条件并选择处理分支。 */
		return applyGB26875ComponentStatus(msg, frame.Data[2:], count) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return fmt.Errorf("unsupported GB26875 command/type combination 0x%02X/0x%02X", frame.Command, typeFlag) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// BuildGB26875RegistrationFrame builds the 112-byte v1.03 registration
// application unit: a 2-byte data identifier followed by the protocol's
// 110-byte registration object. Fields not needed by the virtual device remain
// zero-filled.
func BuildGB26875RegistrationFrame(sequence uint16, source [6]byte, at time.Time) []byte { /* 定义 BuildGB26875RegistrationFrame 函数。 */
	application := make([]byte, 112)                                                                               /* 更新 application 的值。 */
	application[0], application[1] = 0x00, 0x01                                                                    /* 更新 application[1] 的值。 */
	application[2], application[3] = 0x01, 0x03                                                                    /* 更新 application[3] 的值。 */
	copy(application[18:34], []byte(strings.ToUpper(hex.EncodeToString(source[:]))))                               /* 执行当前语句并推进处理流程。 */
	return buildGB26875Frame(sequence, source, [6]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, 0x00, application, at) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func applyGB26875ComponentStatus(msg *StandardMessage, data []byte, count int) error { /* 定义 applyGB26875ComponentStatus 函数。 */
	const objectLength = 46                           /* 声明 objectLength。 */
	if count < 1 || len(data) != count*objectLength { /* 判断条件并选择处理分支。 */
		return fmt.Errorf("GB26875 component status expects %d objects (%d bytes), got %d bytes", count, count*objectLength, len(data)) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	objects := make([]map[string]any, 0, count)    /* 更新 objects 的值。 */
	anyAlarm, anyFault := false, false             /* 更新 anyFault 的值。 */
	components := make([]map[string]any, 0, count) /* 更新 components 的值。 */
	for i := 0; i < count; i++ {                   /* 循环处理当前数据。 */
		v := data[i*objectLength : (i+1)*objectLength]        /* 更新 v 的值。 */
		status := binary.LittleEndian.Uint16(v[7:9])          /* 更新 status 的值。 */
		descriptionBytes := v[9:40]                           /* 更新 descriptionBytes 的值。 */
		if cut := bytesIndex(descriptionBytes, 0); cut >= 0 { /* 判断条件并选择处理分支。 */
			descriptionBytes = descriptionBytes[:cut] /* 更新 descriptionBytes 的值。 */
		} /* 结束当前表达式或代码块。 */
		description := strings.TrimSpace(string(descriptionBytes)) /* 更新 description 的值。 */
		if !utf8.ValidString(description) {                        /* 判断条件并选择处理分支。 */
			description = strings.ToUpper(hex.EncodeToString(descriptionBytes)) /* 更新 description 的值。 */
		} /* 结束当前表达式或代码块。 */
		object := map[string]any{ /* 更新 object 的值。 */
			"systemType": int(v[0]), "systemAddress": int(v[1]), "componentType": int(v[2]), /* 执行当前语句并推进处理流程。 */
			"componentTypeName": gb26875ComponentName(v[2]), "circuitAddress": int(binary.LittleEndian.Uint16(v[3:5])), /* 执行当前语句并推进处理流程。 */
			"nodeAddress": int(binary.LittleEndian.Uint16(v[5:7])), "statusWord": int(status), "description": description, /* 执行当前语句并推进处理流程。 */
			"occurredAt": decodeGB26875Time(v[40:46]),                                                 /* 执行当前语句并推进处理流程。 */
			"test":       status&1 != 0, "fireAlarm": status&(1<<1) != 0, "fault": status&(1<<2) != 0, /* 执行当前语句并推进处理流程。 */
			"shielded": status&(1<<3) != 0, "supervision": status&(1<<4) != 0, "started": status&(1<<5) != 0, /* 执行当前语句并推进处理流程。 */
			"feedback": status&(1<<6) != 0, "delayed": status&(1<<7) != 0, "powerFault": status&(1<<8) != 0, /* 执行当前语句并推进处理流程。 */
			"offline": status&(1<<9) != 0, "openCircuit": status&(1<<10) != 0, "shortCircuit": status&(1<<11) != 0, /* 执行当前语句并推进处理流程。 */
			"removed": status&(1<<12) != 0, "sensorFault": status&(1<<14) != 0, "upgradeFault": status&(1<<15) != 0, /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		componentID := fmt.Sprintf("system-%d-%d/type-%d/circuit-%d/node-%d", v[0], v[1], v[2], binary.LittleEndian.Uint16(v[3:5]), binary.LittleEndian.Uint16(v[5:7])) /* 更新 componentID 的值。 */
		at := decodeGB26875Time(v[40:46])                                                                                                                               /* 更新 at 的值。 */
		if at <= 0 {                                                                                                                                                    /* 判断条件并选择处理分支。 */
			at = msg.Timestamp /* 更新 at 的值。 */
		} /* 结束当前表达式或代码块。 */
		components = append(components, map[string]any{"id": componentID, "name": gb26875ComponentName(v[2]), "location": description, "timestamp": at, /* 更新 components 的值。 */
			"alarms": map[string]bool{"FIRE": status&(1<<1) != 0, "DEVICE_FAULT": status&(1<<2|1<<8|1<<9|1<<10|1<<11|1<<12|1<<14|1<<15) != 0}}) /* 执行当前语句并推进处理流程。 */
		objects = append(objects, object)                                                 /* 更新 objects 的值。 */
		anyAlarm = anyAlarm || status&(1<<1) != 0                                         /* 更新 anyAlarm 的值。 */
		anyFault = anyFault || status&(1<<2|1<<8|1<<9|1<<10|1<<11|1<<12|1<<14|1<<15) != 0 /* 更新 anyFault 的值。 */
	} /* 结束当前表达式或代码块。 */
	first := objects[0]              /* 更新 first 的值。 */
	msg.Properties = map[string]any{ /* 更新 msg.Properties 的值。 */
		"fireAlarm": anyAlarm, "fault": anyFault, "componentType": first["componentType"], /* 执行当前语句并推进处理流程。 */
		"componentTypeName": first["componentTypeName"], "circuitAddress": first["circuitAddress"], /* 执行当前语句并推进处理流程。 */
		"nodeAddress": first["nodeAddress"], "statusWord": first["statusWord"], "started": first["started"], /* 执行当前语句并推进处理流程。 */
		"feedback": first["feedback"], "shielded": first["shielded"], "supervision": first["supervision"], /* 执行当前语句并推进处理流程。 */
		"offline": first["offline"], "powerFault": first["powerFault"], "openCircuit": first["openCircuit"], /* 执行当前语句并推进处理流程。 */
		"shortCircuit": first["shortCircuit"], "removed": first["removed"], "sensorFault": first["sensorFault"], /* 执行当前语句并推进处理流程。 */
		"objects": objects, /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	// Top-level location/status represents a single object only. Multi-object
	// frames retain aggregate flags and complete objects without a false address.
	if count > 1 { /* 判断条件并选择处理分支。 */
		msg.Properties = map[string]any{"fireAlarm": anyAlarm, "fault": anyFault, "objects": objects} /* 更新 msg.Properties 的值。 */
	} /* 结束当前表达式或代码块。 */
	msg.Event = map[string]any{"type": "COMPONENT_STATUS", "alarm": anyAlarm, "fault": anyFault, "objects": objects, "components": components} /* 更新 msg.Event 的值。 */
	if anyAlarm || anyFault {                                                                                                                  /* 判断条件并选择处理分支。 */
		msg.MessageType = AlarmReport /* 更新 msg.MessageType 的值。 */
	} else { /* 结束当前表达式或代码块。 */
		msg.MessageType = StateChange /* 更新 msg.MessageType 的值。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func bytesIndex(data []byte, value byte) int { /* 定义 bytesIndex 函数。 */
	for i, b := range data { /* 循环处理当前数据。 */
		if b == value { /* 判断条件并选择处理分支。 */
			return i /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return -1 /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func decodeGB26875Time(data []byte) int64 { /* 定义 decodeGB26875Time 函数。 */
	if len(data) != 6 { /* 判断条件并选择处理分支。 */
		return 0 /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	values := make([]int, 6)     /* 更新 values 的值。 */
	for i, value := range data { /* 循环处理当前数据。 */
		values[i] = decodeBCD(value) /* 更新 values[i] 的值。 */
	} /* 结束当前表达式或代码块。 */
	year := 2000 + values[5]                                                                                                                          /* 更新 year 的值。 */
	t := time.Date(year, time.Month(values[4]), values[3], values[2], values[1], values[0], 0, DeviceLocation)                                        /* 更新 t 的值。 */
	if values[4] < 1 || values[4] > 12 || values[3] < 1 || values[3] > 31 || values[2] > 23 || values[1] > 59 || values[0] > 59 || t.Year() != year { /* 判断条件并选择处理分支。 */
		return 0 /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return t.UnixMilli() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func decodeBCD(value byte) int { /* 定义 decodeBCD 函数。 */
	if value>>4 <= 9 && value&0x0f <= 9 { /* 判断条件并选择处理分支。 */
		return int(value>>4)*10 + int(value&0x0f) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return int(value) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func gb26875ComponentName(value byte) string { /* 定义 gb26875ComponentName 函数。 */
	switch value { /* 根据条件选择处理路径。 */
	case 23: /* 处理当前分支。 */
		return "手动火灾报警按钮" /* 返回当前处理结果。 */
	case 30: /* 处理当前分支。 */
		return "感温火灾探测器" /* 返回当前处理结果。 */
	case 40: /* 处理当前分支。 */
		return "感烟火灾探测器" /* 返回当前处理结果。 */
	case 137: /* 处理当前分支。 */
		return "声光报警器" /* 返回当前处理结果。 */
	default: /* 处理当前分支。 */
		return fmt.Sprintf("部件类型%d", value) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

// BuildGB26875ComponentStatusFrame builds a standards-shaped frame for test
// devices and protocol conformance tests.
func BuildGB26875ComponentStatusFrame(sequence uint16, source [6]byte, systemType, systemAddress, componentType byte, circuitAddress, nodeAddress, status uint16, description string, at time.Time) []byte { /* 定义 BuildGB26875ComponentStatusFrame 函数。 */
	object := make([]byte, 46)                                                                                                               /* 更新 object 的值。 */
	object[0], object[1], object[2] = systemType, systemAddress, componentType                                                               /* 更新 object[2] 的值。 */
	binary.LittleEndian.PutUint16(object[3:5], circuitAddress)                                                                               /* 执行当前语句并推进处理流程。 */
	binary.LittleEndian.PutUint16(object[5:7], nodeAddress)                                                                                  /* 执行当前语句并推进处理流程。 */
	binary.LittleEndian.PutUint16(object[7:9], status)                                                                                       /* 执行当前语句并推进处理流程。 */
	copy(object[9:40], []byte(description))                                                                                                  /* 执行当前语句并推进处理流程。 */
	copy(object[40:46], encodeGB26875Time(at))                                                                                               /* 执行当前语句并推进处理流程。 */
	return buildGB26875Frame(sequence, source, [6]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, 0x02, append([]byte{0x02, 0x01}, object...), at) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// BuildGB26875AckFrame builds the platform confirmation required after a valid
// device upload. The platform address is all F and the destination is the
// device source address from the received frame.
func BuildGB26875AckFrame(sequence uint16, destination [6]byte, at time.Time) []byte { /* 定义 BuildGB26875AckFrame 函数。 */
	return buildGB26875Frame(sequence, [6]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, destination, 0x03, nil, at) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// BuildGB26875TimeSyncFrame builds the platform response to a device time
// synchronization request, or the optional platform-initiated time sync
// command. The v1.03 frame uses command 0x01 and a type 0x5A, one-object,
// six-byte BCD time application unit.
func BuildGB26875TimeSyncFrame(sequence uint16, destination [6]byte, at time.Time) []byte { /* 定义 BuildGB26875TimeSyncFrame 函数。 */
	application := append([]byte{0x5A, 0x01}, encodeGB26875Time(at)...)                                                 /* 更新 application 的值。 */
	return buildGB26875Frame(sequence, [6]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, destination, 0x01, application, at) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// BuildGB26875TimeSyncRequestFrame builds the optional device-originated time
// synchronization request used by the v1.03 request/response flow.
func BuildGB26875TimeSyncRequestFrame(sequence uint16, source [6]byte, at time.Time) []byte { /* 定义 BuildGB26875TimeSyncRequestFrame 函数。 */
	application := append([]byte{0x5A, 0x01}, encodeGB26875Time(at)...)                                            /* 更新 application 的值。 */
	return buildGB26875Frame(sequence, source, [6]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, 0x01, application, at) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func buildGB26875Frame(sequence uint16, source, destination [6]byte, command byte, application []byte, at time.Time) []byte { /* 定义 buildGB26875Frame 函数。 */
	frame := make([]byte, 2+gb26875ControlLength+len(application)+1+2)    /* 更新 frame 的值。 */
	copy(frame[:2], "@@")                                                 /* 执行当前语句并推进处理流程。 */
	binary.LittleEndian.PutUint16(frame[2:4], sequence)                   /* 执行当前语句并推进处理流程。 */
	frame[4], frame[5] = 0x01, 0x03                                       /* 更新 frame[5] 的值。 */
	copy(frame[6:12], encodeGB26875Time(at))                              /* 执行当前语句并推进处理流程。 */
	copy(frame[12:18], source[:])                                         /* 执行当前语句并推进处理流程。 */
	copy(frame[18:24], destination[:])                                    /* 执行当前语句并推进处理流程。 */
	binary.LittleEndian.PutUint16(frame[24:26], uint16(len(application))) /* 执行当前语句并推进处理流程。 */
	frame[26] = command                                                   /* 更新 frame[26] 的值。 */
	copy(frame[27:], application)                                         /* 执行当前语句并推进处理流程。 */
	var sum byte                                                          /* 声明 sum。 */
	for _, value := range frame[2 : 27+len(application)] {                /* 循环处理当前数据。 */
		sum += value /* 更新 sum 的值。 */
	} /* 结束当前表达式或代码块。 */
	frame[27+len(application)] = sum        /* 执行当前语句并推进处理流程。 */
	copy(frame[28+len(application):], "##") /* 执行当前语句并推进处理流程。 */
	return frame                            /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func encodeGB26875Time(at time.Time) []byte { /* 定义 encodeGB26875Time 函数。 */
	at = at.In(DeviceLocation)                                                                                                                                       /* 更新 at 的值。 */
	return []byte{encodeBCD(at.Second()), encodeBCD(at.Minute()), encodeBCD(at.Hour()), encodeBCD(at.Day()), encodeBCD(int(at.Month())), encodeBCD(at.Year() % 100)} /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func encodeBCD(value int) byte { return byte(value/10<<4 | value%10) } /* 定义 encodeBCD 函数。 */

func isGB26875PlatformAddress(value string) bool { /* 定义 isGB26875PlatformAddress 函数。 */
	return strings.EqualFold(value, "FFFFFFFFFFFF") || strings.EqualFold(value, "000000000000") /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func MarshalHexPayload(frame []byte) json.RawMessage { /* 定义 MarshalHexPayload 函数。 */
	value, _ := json.Marshal(strings.ToUpper(hex.EncodeToString(frame))) /* 更新 _ 的值。 */
	return value                                                         /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
