package main

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var switchExample = hexBytes("40 40 00 00 01 01 0F 22 0E 19 07 13 01 00 00 00 00 00 FF FF 00 00 00 00 12 00 02 96 01 00 03 5B 01 00 00 00 02 00 00 0F 22 0E 19 07 13 F1 23 23")

func hexBytes(s string) []byte {
	b, e := hex.DecodeString(strings.Join(strings.Fields(s), ""))
	if e != nil {
		panic(e)
	}
	return b
}

func Protocol() Definition {
	return Definition{
		Name: "富贝 2024 消防传输装置", Version: "1.0.0", Transport: "TCP", Decode: decodeFB, Ingress: ingressFB,
		Samples: []Sample{
			{Name: "开关量输入", Data: switchExample, Want: Message{
				MessageType: "PROPERTY_REPORT",
				Properties:  map[string]any{"typeFlag": 150, "objectCount": 1},
			}},
		},
		Operations: []OperationSample{
			{Name: "半帧", Operation: "ingress", Data: switchExample[:10], Want: Frame{NeedMore: true}},
			{Name: "完整上报", Operation: "ingress", Data: switchExample, Want: Frame{Consumed: len(switchExample), DeviceID: "fb_1"}},
		},
	}
}

func fbLength(data []byte) (int, error) {
	if len(data) < 2 {
		return 0, nil
	}
	if data[0] != 0x40 || data[1] != 0x40 {
		return 0, errors.New("FB2024 帧头错误")
	}
	if len(data) < 30 {
		return 0, nil
	}
	n := 30 + int(binary.LittleEndian.Uint16(data[24:26]))
	if n > 65536 {
		return 0, errors.New("FB2024 帧过长")
	}
	if len(data) < n {
		return 0, nil
	}
	if data[n-2] != 0x23 || data[n-1] != 0x23 {
		return 0, errors.New("FB2024 帧尾错误")
	}
	var sum byte
	for _, b := range data[2 : n-3] {
		sum += b
	}
	if sum != data[n-3] {
		return 0, errors.New("FB2024 校验和错误")
	}
	return n, nil
}

func fbDevice(data []byte, ctx Context) string {
	var n uint64
	for i := 17; i >= 12; i-- {
		n = n<<8 | uint64(data[i])
	}
	if n == 0 && ctx.DeviceID != "" {
		return ctx.DeviceID
	}
	return "fb_" + strconv.FormatUint(n, 10)
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
		return Message{}, errors.New("需要单个完整 FB2024 帧")
	}
	command := data[26]
	app := data[27 : n-3]
	if command != 2 || len(app) < 2 {
		return Message{MessageType: "COMMAND_REPLY", Event: map[string]any{"type": "CONTROL", "command": int(command)}}, nil
	}
	typ, count := app[0], int(app[1])
	props := map[string]any{"typeFlag": int(typ), "objectCount": count, "serialNo": int(binary.LittleEndian.Uint16(data[2:4]))}
	var size int
	switch typ {
	case 2, 3, 0x87, 0x96, 0x97:
		size = 16
	case 0x15, 0x88:
		size = 7
	default:
		props["payloadHex"] = hex.EncodeToString(app)
		return properties(props), nil
	}
	if len(app) != 2+count*size {
		return Message{}, fmt.Errorf("FB2024 类型 %02x 对象长度不符", typ)
	}
	objects := make([]map[string]any, 0, count)
	components := make([]map[string]any, 0, count)
	for i := 0; i < count; i++ {
		b := app[2+i*size : 2+(i+1)*size]
		if typ == 0x15 || typ == 0x88 {
			status := b[0]
			if typ == 0x88 {
				status = 0
			}
			objects = append(objects, map[string]any{"transmissionStatus": int(status), "mainPowerFault": status&8 != 0, "backupPowerFault": status&16 != 0})
			components = append(components, map[string]any{"id": "gateway", "name": "用户信息传输装置", "alarms": map[string]bool{"DEVICE_FAULT": status&0x18 != 0}})
			continue
		}
		sys, dev, addr, port := int(b[1]), int(b[2]), int(b[3]), int(b[5])
		access, value := int(b[7]), int(binary.LittleEndian.Uint16(b[8:10]))
		loop := port
		if typ == 0x96 || typ == 3 {
			if port == 0 {
				loop = 1
			}
		}
		if typ == 0x97 {
			if port >= addr && port < addr+4 {
				loop = port - addr + 1
			} else if port == 0 {
				loop = 1
			}
		}
		if loop == 0 {
			loop = addr
		}
		obj := map[string]any{"systemAddress": sys, "systemType": int(b[0]), "deviceType": dev, "HLH": loop, "TDH": addr, "port": port, "accessType": access}
		switch typ {
		case 3:
			obj["analogType"] = access
			obj["analogValue"] = value
			obj["analogue"] = map[string]any{"analogueTypeCode": analogCode(access), "analogueValue": value}
		case 0x87:
			obj["switchState"] = value
			obj["state"] = "恢复"
		default:
			obj["switchState"] = value
			obj["state"] = switchText(access, value)
		}
		objects = append(objects, obj)
	}
	props["objects"] = objects
	if len(components) > 0 {
		return Message{MessageType: "STATE_CHANGE", Properties: props, Event: map[string]any{"components": components}}, nil
	}
	return properties(props), nil
}

func analogCode(t int) string {
	switch t {
	case 1:
		return "HUMIDITY"
	case 2:
		return "LEVEL"
	case 3:
		return "TEMPERATURE"
	case 4:
		return "PRESSURE_MPA"
	case 5:
		return "PRESSURE_KPA"
	default:
		return "UNKNOWN"
	}
}
func switchText(t, v int) string {
	on := v != 0
	switch t {
	case 0:
		if on {
			return "手动"
		}
		return "自动"
	case 1:
		if on {
			return "断电"
		}
		return "通电"
	case 2:
		if on {
			return "启动"
		}
		return "停止"
	case 3:
		if on {
			return "故障"
		}
		return "正常"
	default:
		return strconv.Itoa(v)
	}
}
