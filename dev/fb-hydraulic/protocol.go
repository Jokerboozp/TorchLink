package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strconv"
)

// The wire format is 15 ASCII IMEI digits followed by one 0x46 RTU report.
// A connection may carry multiple reports; ingress consumes exactly one.
func Protocol() Definition {
	return Definition{
		Name: "富贝防爆液压传感器", Version: "1.0.0", Transport: "TCP",
		Decode: decodeSensor, Ingress: ingressSensor, Encode: encodeSensor,
		Samples: []Sample{
			{Name: "实时压力", Data: report(0x0000, []byte{0, 1, 0, 80, 0, 10, 0, 0, 4, 210, 0, 20, 0, 5, 0, 0, 1, 244, 0, 0, 11, 184, 0, 1, 0, 0}), Want: properties(map[string]any{"pressureMPa": 12.34, "batteryLevel": 80, "uploadInterval": 10, "signalStrength": 20, "collectionInterval": 5, "alarmLowerLimit": 5.0, "alarmUpperLimit": 30.0, "alarmEnabled": true, "offset": 0, "partType": "液压传感器"})},
			{Name: "报警", Data: report(0x0016, []byte{0, 1}), Context: Context{Now: 1789000000000}, Want: Message{MessageType: "STATE_CHANGE", Properties: map[string]any{"alarm": true, "partType": "液压报警"}, Event: map[string]any{"components": []map[string]any{{"id": "sensor", "name": "液压传感器", "alarms": map[string]bool{"PRESSURE_ALARM": true}}}}}},
		},
		Operations: []OperationSample{
			{Name: "半帧", Operation: "ingress", Data: []byte("868892074243446"), Want: Frame{NeedMore: true}},
			{Name: "完整上报", Operation: "ingress", Data: report(0x0016, []byte{0, 1}), Want: Frame{Consumed: 26, DeviceID: "868892074243446", State: map[string]any{"unit": 1}}},
			{Name: "第二帧无 IMEI", Operation: "ingress", Data: rtu([]byte{1, 0x46, 0, 0x16, 0, 1, 2, 0, 1}), Context: Context{DeviceID: "868892074243446", State: map[string]any{"unit": 1}}, Want: Frame{Consumed: 11, DeviceID: "868892074243446", State: map[string]any{"unit": 1}}},
			{Name: "写采集间隔", Operation: "encode", Command: Command{Type: "setDetectionTime", Params: map[string]any{"value": 5}}, Context: Context{State: map[string]any{"unit": 1}}, Want: Frame{Reply: rtu([]byte{1, 6, 0, 6, 0, 5}), CorrelationID: "06-0006", State: map[string]any{"unit": 1, "pendingFunction": 6, "pendingRegister": 6, "pendingValue": 5}}},
			{Name: "写入确认", Operation: "ingress", Data: rtu([]byte{1, 6, 0, 6, 0, 5}), Context: Context{DeviceID: "868892074243446", State: map[string]any{"unit": 1, "pendingFunction": 6, "pendingRegister": 6, "pendingValue": 5}}, Want: Frame{Consumed: 8, DeviceID: "868892074243446", CorrelationID: "06-0006", State: map[string]any{"unit": 1}}},
		},
	}
}

func report(register uint16, body []byte) []byte {
	data := []byte{1, 0x46, byte(register >> 8), byte(register), 0, byte(len(body) / 2), byte(len(body))}
	data = append(data, body...)
	return append([]byte("868892074243446"), rtu(data)...)
}

func rtu(data []byte) []byte {
	crc := uint16(0xffff)
	for _, b := range data {
		crc ^= uint16(b)
		for i := 0; i < 8; i++ {
			if crc&1 != 0 {
				crc = crc>>1 ^ 0xa001
			} else {
				crc >>= 1
			}
		}
	}
	return append(data, byte(crc), byte(crc>>8))
}

func validCRC(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	probe := rtu(append([]byte(nil), data[:len(data)-2]...))
	return probe[len(probe)-2] == data[len(data)-2] && probe[len(probe)-1] == data[len(data)-1]
}

func sensorFrame(data []byte, ctx Context) (int, string, []byte, error) {
	if len(data) == 0 {
		return 0, "", nil, nil
	}
	start, imei := 0, ctx.DeviceID
	if data[0] >= '0' && data[0] <= '9' {
		if len(data) < 15 {
			return 0, "", nil, nil
		}
		for _, b := range data[:15] {
			if b < '0' || b > '9' {
				return 0, "", nil, errors.New("IMEI 必须为 15 位数字")
			}
		}
		start, imei = 15, string(data[:15])
	} else if imei == "" {
		return 0, "", nil, errors.New("无 IMEI 的 RTU 帧须在已识别会话内")
	}
	if len(data) < start+2 {
		return 0, imei, nil, nil
	}
	if data[start] == 0 {
		return 0, "", nil, errors.New("设备号无效")
	}
	var length int
	switch data[start+1] {
	case 0x46:
		if len(data) < start+7 {
			return 0, imei, nil, nil
		}
		count := int(data[start+6])
		if count > 250 {
			return 0, "", nil, errors.New("报文数据过长")
		}
		length = 7 + count + 2
	case 0x06, 0x10:
		length = 8
	default:
		return 0, "", nil, errors.New("不支持的功能码")
	}
	if len(data) < start+length {
		return 0, imei, nil, nil
	}
	frame := data[start : start+length]
	if !validCRC(frame) {
		return 0, "", nil, errors.New("CRC16 校验失败")
	}
	if frame[1] == 0x46 && int(binary.BigEndian.Uint16(frame[4:6]))*2 != int(frame[6]) {
		return 0, "", nil, errors.New("寄存器数量与字节数不符")
	}
	return start + length, imei, frame, nil
}

func ingressSensor(data []byte, ctx Context) (Frame, error) {
	length, imei, frame, err := sensorFrame(data, ctx)
	if err != nil {
		return Frame{}, err
	}
	if length == 0 {
		return Frame{NeedMore: true}, nil
	}
	if frame[1] == 0x06 || frame[1] == 0x10 {
		fn, reg := int(frame[1]), int(binary.BigEndian.Uint16(frame[2:4]))
		if stateNumber(ctx.State, "pendingFunction") != fn || stateNumber(ctx.State, "pendingRegister") != reg {
			return Frame{}, errors.New("写入确认与待发命令不匹配")
		}
		if fn == 6 && stateNumber(ctx.State, "pendingValue") != int(binary.BigEndian.Uint16(frame[4:6])) {
			return Frame{}, errors.New("写入值与命令不匹配")
		}
		if fn == 0x10 && binary.BigEndian.Uint16(frame[4:6]) != 5 {
			return Frame{}, errors.New("批量写入数量不匹配")
		}
		return Frame{Consumed: length, DeviceID: imei, CorrelationID: fmt.Sprintf("%02x-%04x", fn, reg), State: map[string]any{"unit": int(frame[0])}}, nil
	}
	state := map[string]any{}
	for key, value := range ctx.State {
		state[key] = value
	}
	state["unit"] = int(frame[0])
	return Frame{Consumed: length, DeviceID: imei, State: state}, nil
}

func stateNumber(s map[string]any, key string) int {
	switch v := s[key].(type) {
	case int:
		return v
	case float64:
		return int(v)
	default:
		return 0
	}
}

func decodeSensor(data []byte, ctx Context) (Message, error) {
	length, _, frame, err := sensorFrame(data, ctx)
	if err != nil {
		return Message{}, err
	}
	if length == 0 || length != len(data) {
		return Message{}, errors.New("需要单个完整传感器报文")
	}
	if frame[1] == 6 || frame[1] == 0x10 {
		return Message{MessageType: "COMMAND_REPLY", Event: map[string]any{"type": "ACK", "function": int(frame[1]), "register": int(binary.BigEndian.Uint16(frame[2:4]))}}, nil
	}
	register := binary.BigEndian.Uint16(frame[2:4])
	body := frame[7 : len(frame)-2]
	switch register {
	case 0:
		if len(body) != 26 {
			return Message{}, errors.New("实时数据须为 26 字节")
		}
		value := float64(int32(binary.BigEndian.Uint32(body[6:10]))) / 100
		return properties(map[string]any{"partType": "液压传感器", "pressureMPa": value, "batteryLevel": int(binary.BigEndian.Uint16(body[2:4])), "uploadInterval": int(binary.BigEndian.Uint16(body[4:6])), "signalStrength": int(binary.BigEndian.Uint16(body[10:12])), "collectionInterval": int(binary.BigEndian.Uint16(body[12:14])), "alarmLowerLimit": float64(int32(binary.BigEndian.Uint32(body[14:18]))) / 100, "alarmUpperLimit": float64(int32(binary.BigEndian.Uint32(body[18:22]))) / 100, "alarmEnabled": binary.BigEndian.Uint16(body[22:24]) == 1, "offset": int(int16(binary.BigEndian.Uint16(body[24:26])))}), nil
	case 0x16:
		if len(body) != 2 {
			return Message{}, errors.New("报警数据须为 2 字节")
		}
		active := binary.BigEndian.Uint16(body) == 1
		return Message{MessageType: "STATE_CHANGE", Properties: map[string]any{"alarm": active, "partType": "液压报警"}, Event: map[string]any{"components": []map[string]any{{"id": "sensor", "name": "液压传感器", "alarms": map[string]bool{"PRESSURE_ALARM": active}}}}}, nil
	case 0x19:
		if len(body) != 10 {
			return Message{}, errors.New("历史数据须为 10 字节")
		}
		return properties(map[string]any{"partType": "液压传感器", "historicalPressureMPa": float64(int32(binary.BigEndian.Uint32(body[6:10]))) / 100, "historyTimestamp": int64(binary.BigEndian.Uint32(body[:4])) * 1000}), nil
	default:
		return Message{}, fmt.Errorf("未知寄存器 %04x", register)
	}
}

func intParam(params map[string]any, key string, min, max int64) (int64, error) {
	v, ok := params[key]
	if !ok {
		return 0, fmt.Errorf("缺少参数 %s", key)
	}
	var n int64
	switch x := v.(type) {
	case int:
		n = int64(x)
	case int64:
		n = x
	case float64:
		if math.Trunc(x) != x {
			return 0, fmt.Errorf("%s 必须为整数", key)
		}
		n = int64(x)
	case string:
		var err error
		n, err = strconv.ParseInt(x, 10, 64)
		if err != nil {
			return 0, err
		}
	default:
		return 0, fmt.Errorf("%s 必须为整数", key)
	}
	if n < min || n > max {
		return 0, fmt.Errorf("%s 超出范围", key)
	}
	return n, nil
}

func encodeSensor(cmd Command, ctx Context) (Frame, error) {
	unit, ok := ctx.State["unit"].(float64)
	if !ok {
		if n, yes := ctx.State["unit"].(int); yes {
			unit = float64(n)
		}
	}
	if unit < 1 || unit > 247 {
		return Frame{}, errors.New("请先等待设备上报取得站号")
	}
	if cmd.Type == "setMultipleParams" {
		collection, e := intParam(cmd.Params, "collectionTime", 0, 65535)
		if e != nil {
			return Frame{}, e
		}
		lower, e := intParam(cmd.Params, "alarmLowerLimit", -2147483648, 2147483647)
		if e != nil {
			return Frame{}, e
		}
		upper, e := intParam(cmd.Params, "alarmUpperLimit", -2147483648, 2147483647)
		if e != nil {
			return Frame{}, e
		}
		body := []byte{byte(unit), 0x10, 0, 6, 0, 5, 10, byte(collection >> 8), byte(collection)}
		limits := make([]byte, 8)
		binary.BigEndian.PutUint32(limits[:4], uint32(int32(lower)))
		binary.BigEndian.PutUint32(limits[4:], uint32(int32(upper)))
		body = append(body, limits...)
		return Frame{Reply: rtu(body), CorrelationID: "10-0006", State: map[string]any{"unit": int(unit), "pendingFunction": 16, "pendingRegister": 6}}, nil
	}
	reg := uint16(0)
	switch cmd.Type {
	case "setDetectionTime":
		reg = 6
	case "setChangeAlarmValue":
		reg = 0
	case "setUploadTime":
		reg = 2
	case "setOffset":
		reg = 11
	default:
		return Frame{}, fmt.Errorf("不支持命令 %s", cmd.Type)
	}
	v, err := intParam(cmd.Params, "value", -32768, 65535)
	if err != nil {
		return Frame{}, err
	}
	data := rtu([]byte{byte(unit), 6, byte(reg >> 8), byte(reg), byte(uint16(v) >> 8), byte(v)})
	return Frame{Reply: data, CorrelationID: fmt.Sprintf("06-%04x", reg), State: map[string]any{"unit": int(unit), "pendingFunction": 6, "pendingRegister": int(reg), "pendingValue": int(uint16(v))}}, nil
}
