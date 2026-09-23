package main

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var componentExample = mustHex("40 40 03 00 01 01 0D 10 16 15 0A 13 71 11 01 00 00 00 38 5B 01 00 00 00  30 00 02 02 01 01 01 17 05 00 65 00 02 00 20 20 20 20 20 20 20 20 20  20 20 20 20 20 20 20 20 20 20 20 20 20 20 20 20 20 20 20 20 20 00  0C 10 16 15 0A 13 5F 23 23")

func mustHex(s string) []byte {
	b, e := hex.DecodeString(strings.Join(strings.Fields(s), ""))
	if e != nil {
		panic(e)
	}
	return b
}

func Protocol() Definition {
	return Definition{
		Name: "富贝 2018 消防传输装置", Version: "1.0.0", Transport: "TCP", Decode: decodeFB, Ingress: ingressFB,
		Samples: []Sample{{Name: "手报火警", Data: componentExample, Context: Context{Now: 1789000000000}, Want: Message{MessageType: "STATE_CHANGE", Properties: map[string]any{"typeFlag": 2, "objectCount": 1, "fireAlarm": true}}}},
		Operations: []OperationSample{
			{Name: "半帧", Operation: "ingress", Data: componentExample[:10], Want: Frame{NeedMore: true}},
			{Name: "完整报文", Operation: "ingress", Data: componentExample, Want: Frame{Consumed: len(componentExample), DeviceID: "fb2018_70001"}},
		},
	}
}

func fbLength(data []byte) (int, error) {
	if len(data) < 2 {
		return 0, nil
	}
	if data[0] != 0x40 || data[1] != 0x40 {
		return 0, errors.New("FB2018 帧头错误")
	}
	if len(data) < 30 {
		return 0, nil
	}
	n := 30 + int(binary.LittleEndian.Uint16(data[24:26]))
	if n > 65536 {
		return 0, errors.New("FB2018 帧过长")
	}
	if len(data) < n {
		return 0, nil
	}
	if data[n-2] != 0x23 || data[n-1] != 0x23 {
		return 0, errors.New("FB2018 帧尾错误")
	}
	var sum byte
	for _, b := range data[2 : n-3] {
		sum += b
	}
	if sum != data[n-3] {
		return 0, errors.New("FB2018 校验和错误")
	}
	return n, nil
}

func fbDevice(data []byte, ctx Context) string {
	// Source is six-byte little-endian. An all-zero source needs a dedicated
	// listener/profile or explicit device mapping to avoid identity collision.
	var n uint64
	for i := 17; i >= 12; i-- {
		n = n<<8 | uint64(data[i])
	}
	if n == 0 && ctx.DeviceID != "" {
		return ctx.DeviceID
	}
	return "fb2018_" + strconv.FormatUint(n, 10)
}

func ingressFB(data []byte, ctx Context) (Frame, error) {
	n, e := fbLength(data)
	if e != nil {
		return Frame{}, e
	}
	if n == 0 {
		return Frame{NeedMore: true}, nil
	}
	return Frame{Consumed: n, DeviceID: fbDevice(data, ctx)}, nil
}

func decodeFB(data []byte, ctx Context) (Message, error) {
	n, e := fbLength(data)
	if e != nil {
		return Message{}, e
	}
	if n == 0 || n != len(data) {
		return Message{}, errors.New("需要单个完整 FB2018 帧")
	}
	command := data[26]
	app := data[27 : n-3]
	if command != 2 || len(app) < 2 {
		return Message{MessageType: "COMMAND_REPLY", Event: map[string]any{"type": "CONTROL", "command": int(command)}}, nil
	}
	typ, count := app[0], int(app[1])
	if count > 256 {
		return Message{}, errors.New("对象数过多")
	}
	props := map[string]any{"typeFlag": int(typ), "objectCount": count, "serialNo": int(binary.LittleEndian.Uint16(data[2:4]))}
	var objects []map[string]any
	var components []map[string]any
	var fire, fault bool
	var bodySize int
	switch typ {
	case 1, 0x86:
		bodySize = 10
	case 2, 0x87:
		if len(app) == 2+count*46 {
			bodySize = 46
		} else {
			bodySize = 15
		}
	case 0x15, 0x88:
		bodySize = 7
	case 0x18:
		bodySize = 8
	case 0x19:
		if len(app) < 4 {
			return Message{}, errors.New("软件版本报文过短")
		}
		props["version"] = fmt.Sprintf("%d.%d", app[2], app[3])
		return properties(props), nil
	case 0x1c:
		if len(app) < 8 {
			return Message{}, errors.New("系统时间报文过短")
		}
		props["systemTime"] = fbTime(app[2:8])
		return properties(props), nil
	default:
		props["payloadHex"] = hex.EncodeToString(app)
		return properties(props), nil
	}
	if len(app) != 2+count*bodySize {
		return Message{}, fmt.Errorf("FB2018 类型 %02x 对象长度不符", typ)
	}
	for i := 0; i < count; i++ {
		b := app[2+i*bodySize : 2+(i+1)*bodySize]
		var obj map[string]any
		var component map[string]any
		switch typ {
		case 1, 0x86:
			status := binary.LittleEndian.Uint16(b[2:4])
			if typ == 0x86 {
				status = 0
			}
			id := fmt.Sprintf("system/%d", b[1])
			alarms := map[string]bool{"FIRE": status&2 != 0, "DEVICE_FAULT": status&4 != 0 || status&0x700 != 0}
			obj = map[string]any{"systemType": int(b[0]), "systemAddress": int(b[1]), "status": int(status), "id": id}
			component = map[string]any{"id": id, "name": fmt.Sprintf("系统 %d", b[1]), "alarms": alarms}
		case 2, 0x87:
			point, zone, status := binary.LittleEndian.Uint16(b[3:5]), binary.LittleEndian.Uint16(b[5:7]), binary.LittleEndian.Uint16(b[7:9])
			if typ == 0x87 {
				status = 0
			}
			id := fmt.Sprintf("system/%d/zone/%d/point/%d", b[1], zone, point)
			alarms := map[string]bool{"FIRE": status&2 != 0, "DEVICE_FAULT": status&4 != 0 || status&0x100 != 0}
			obj = map[string]any{"systemAddress": int(b[1]), "systemType": int(b[0]), "deviceType": int(b[2]), "HLH": int(zone), "TDH": int(point), "status": int(status), "id": id}
			component = map[string]any{"id": id, "name": fbTypeName(b[2]), "alarms": alarms}
		case 0x15, 0x88:
			status := uint16(b[0])
			if typ == 0x88 {
				status = 0
			}
			alarms := map[string]bool{"FIRE": status&2 != 0, "DEVICE_FAULT": status&0x7c != 0}
			obj = map[string]any{"status": int(status), "id": "gateway"}
			component = map[string]any{"id": "gateway", "name": "用户信息传输装置", "alarms": alarms}
		case 0x18:
			obj = map[string]any{"operation": int(b[0]), "operator": int(b[1]), "eventTime": fbTime(b[2:8])}
		}
		objects = append(objects, obj)
		if component != nil {
			components = append(components, component)
			alarms := component["alarms"].(map[string]bool)
			fire = fire || alarms["FIRE"]
			fault = fault || alarms["DEVICE_FAULT"]
		}
	}
	props["objects"] = objects
	if len(components) > 0 {
		props["fireAlarm"] = fire
		props["fault"] = fault
		return Message{MessageType: "STATE_CHANGE", Properties: props, Event: map[string]any{"components": components}}, nil
	}
	return properties(props), nil
}

func fbTypeName(code byte) string {
	if code == 23 {
		return "手动火灾报警按钮"
	}
	return fmt.Sprintf("部件类型 %d", code)
}

func fbTime(data []byte) string {
	return fmt.Sprintf("%04d-%02d-%02dT%02d:%02d:%02d", 2000+int(data[5]), data[4], data[3], data[2], data[1], data[0])
}
