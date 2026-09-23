package main /* 声明 main 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"encoding/binary" /* 执行当前语句并推进处理流程。 */
	"errors"          /* 执行当前语句并推进处理流程。 */
	"fmt"             /* 执行当前语句并推进处理流程。 */
	"math"            /* 执行当前语句并推进处理流程。 */
	"strconv"         /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

// The wire format is 15 ASCII IMEI digits followed by one 0x46 RTU report.
// A connection may carry multiple reports; ingress consumes exactly one.
func Protocol() Definition { /* 定义 Protocol 函数。 */
	return Definition{ /* 返回当前处理结果。 */
		Name: "富贝防爆液位传感器", Version: "1.0.0", Transport: "TCP", /* 执行当前语句并推进处理流程。 */
		Decode: decodeSensor, Ingress: ingressSensor, Encode: encodeSensor, /* 执行当前语句并推进处理流程。 */
		Samples: []Sample{ /* 执行当前语句并推进处理流程。 */
			{Name: "实时液位", Data: report(0x0000, []byte{0, 1, 0, 80, 0, 10, 0, 0, 4, 210, 0, 20, 0, 5, 0, 0, 1, 244, 0, 0, 11, 184, 0, 1, 0, 0}), Want: properties(map[string]any{"levelMeters": 12.34, "batteryLevel": 80, "uploadInterval": 10, "signalStrength": 20, "collectionInterval": 5, "alarmLowerLimit": 5.0, "alarmUpperLimit": 30.0, "alarmEnabled": true, "offset": 0, "partType": "液位传感器"})}, /* 执行当前语句并推进处理流程。 */
			{Name: "报警", Data: report(0x0016, []byte{0, 1}), Context: Context{Now: 1789000000000}, Want: Message{MessageType: "STATE_CHANGE", Properties: map[string]any{"alarm": true, "partType": "液位报警"}, Event: map[string]any{"components": []map[string]any{{"id": "sensor", "name": "液位传感器", "alarms": map[string]bool{"LEVEL_ALARM": true}}}}}},                                                    /* 执行当前语句并推进处理流程。 */
		}, /* 结束当前表达式或代码块。 */
		Operations: []OperationSample{ /* 执行当前语句并推进处理流程。 */
			{Name: "半帧", Operation: "ingress", Data: []byte("868892074243446"), Want: Frame{NeedMore: true}},                                                                                                                                                                                                                                                       /* 执行当前语句并推进处理流程。 */
			{Name: "完整上报", Operation: "ingress", Data: report(0x0016, []byte{0, 1}), Want: Frame{Consumed: 26, DeviceID: "868892074243446", State: map[string]any{"unit": 1}}},                                                                                                                                                                                     /* 执行当前语句并推进处理流程。 */
			{Name: "第二帧无 IMEI", Operation: "ingress", Data: rtu([]byte{1, 0x46, 0, 0x16, 0, 1, 2, 0, 1}), Context: Context{DeviceID: "868892074243446", State: map[string]any{"unit": 1}}, Want: Frame{Consumed: 11, DeviceID: "868892074243446", State: map[string]any{"unit": 1}}},                                                                               /* 执行当前语句并推进处理流程。 */
			{Name: "写采集间隔", Operation: "encode", Command: Command{Type: "setDetectionTime", Params: map[string]any{"value": 5}}, Context: Context{State: map[string]any{"unit": 1}}, Want: Frame{Reply: rtu([]byte{1, 6, 0, 6, 0, 5}), CorrelationID: "06-0006", State: map[string]any{"unit": 1, "pendingFunction": 6, "pendingRegister": 6, "pendingValue": 5}}}, /* 执行当前语句并推进处理流程。 */
			{Name: "写入确认", Operation: "ingress", Data: rtu([]byte{1, 6, 0, 6, 0, 5}), Context: Context{DeviceID: "868892074243446", State: map[string]any{"unit": 1, "pendingFunction": 6, "pendingRegister": 6, "pendingValue": 5}}, Want: Frame{Consumed: 8, DeviceID: "868892074243446", CorrelationID: "06-0006", State: map[string]any{"unit": 1}}},           /* 执行当前语句并推进处理流程。 */
		}, /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func report(register uint16, body []byte) []byte { /* 定义 report 函数。 */
	data := []byte{1, 0x46, byte(register >> 8), byte(register), 0, byte(len(body) / 2), byte(len(body))} /* 更新 data 的值。 */
	data = append(data, body...)                                                                          /* 更新 data 的值。 */
	return append([]byte("868892074243446"), rtu(data)...)                                                /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func rtu(data []byte) []byte { /* 定义 rtu 函数。 */
	crc := uint16(0xffff)    /* 更新 crc 的值。 */
	for _, b := range data { /* 循环处理当前数据。 */
		crc ^= uint16(b)         /* 执行当前语句并推进处理流程。 */
		for i := 0; i < 8; i++ { /* 循环处理当前数据。 */
			if crc&1 != 0 { /* 判断条件并选择处理分支。 */
				crc = crc>>1 ^ 0xa001 /* 更新 crc 的值。 */
			} else { /* 结束当前表达式或代码块。 */
				crc >>= 1 /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return append(data, byte(crc), byte(crc>>8)) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func validCRC(data []byte) bool { /* 定义 validCRC 函数。 */
	if len(data) < 4 { /* 判断条件并选择处理分支。 */
		return false /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	probe := rtu(append([]byte(nil), data[:len(data)-2]...))                                    /* 更新 probe 的值。 */
	return probe[len(probe)-2] == data[len(data)-2] && probe[len(probe)-1] == data[len(data)-1] /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func sensorFrame(data []byte, ctx Context) (int, string, []byte, error) { /* 定义 sensorFrame 函数。 */
	if len(data) == 0 { /* 判断条件并选择处理分支。 */
		return 0, "", nil, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	start, imei := 0, ctx.DeviceID        /* 更新 imei 的值。 */
	if data[0] >= '0' && data[0] <= '9' { /* 判断条件并选择处理分支。 */
		if len(data) < 15 { /* 判断条件并选择处理分支。 */
			return 0, "", nil, nil /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		for _, b := range data[:15] { /* 循环处理当前数据。 */
			if b < '0' || b > '9' { /* 判断条件并选择处理分支。 */
				return 0, "", nil, errors.New("IMEI 必须为 15 位数字") /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		start, imei = 15, string(data[:15]) /* 更新 imei 的值。 */
	} else if imei == "" { /* 结束当前表达式或代码块。 */
		return 0, "", nil, errors.New("无 IMEI 的 RTU 帧须在已识别会话内") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(data) < start+2 { /* 判断条件并选择处理分支。 */
		return 0, imei, nil, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if data[start] == 0 { /* 判断条件并选择处理分支。 */
		return 0, "", nil, errors.New("设备号无效") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var length int         /* 声明 length。 */
	switch data[start+1] { /* 根据条件选择处理路径。 */
	case 0x46: /* 处理当前分支。 */
		if len(data) < start+7 { /* 判断条件并选择处理分支。 */
			return 0, imei, nil, nil /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		count := int(data[start+6]) /* 更新 count 的值。 */
		if count > 250 {            /* 判断条件并选择处理分支。 */
			return 0, "", nil, errors.New("报文数据过长") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		length = 7 + count + 2 /* 更新 length 的值。 */
	case 0x06, 0x10: /* 处理当前分支。 */
		length = 8 /* 更新 length 的值。 */
	default: /* 处理当前分支。 */
		return 0, "", nil, errors.New("不支持的功能码") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(data) < start+length { /* 判断条件并选择处理分支。 */
		return 0, imei, nil, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	frame := data[start : start+length] /* 更新 frame 的值。 */
	if !validCRC(frame) {               /* 判断条件并选择处理分支。 */
		return 0, "", nil, errors.New("CRC16 校验失败") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if frame[1] == 0x46 && int(binary.BigEndian.Uint16(frame[4:6]))*2 != int(frame[6]) { /* 判断条件并选择处理分支。 */
		return 0, "", nil, errors.New("寄存器数量与字节数不符") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return start + length, imei, frame, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func ingressSensor(data []byte, ctx Context) (Frame, error) { /* 定义 ingressSensor 函数。 */
	length, imei, frame, err := sensorFrame(data, ctx) /* 更新 err 的值。 */
	if err != nil {                                    /* 判断条件并选择处理分支。 */
		return Frame{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if length == 0 { /* 判断条件并选择处理分支。 */
		return Frame{NeedMore: true}, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if frame[1] == 0x06 || frame[1] == 0x10 { /* 判断条件并选择处理分支。 */
		fn, reg := int(frame[1]), int(binary.BigEndian.Uint16(frame[2:4]))                                       /* 更新 reg 的值。 */
		if stateNumber(ctx.State, "pendingFunction") != fn || stateNumber(ctx.State, "pendingRegister") != reg { /* 判断条件并选择处理分支。 */
			return Frame{}, errors.New("写入确认与待发命令不匹配") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if fn == 6 && stateNumber(ctx.State, "pendingValue") != int(binary.BigEndian.Uint16(frame[4:6])) { /* 判断条件并选择处理分支。 */
			return Frame{}, errors.New("写入值与命令不匹配") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if fn == 0x10 && binary.BigEndian.Uint16(frame[4:6]) != 5 { /* 判断条件并选择处理分支。 */
			return Frame{}, errors.New("批量写入数量不匹配") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return Frame{Consumed: length, DeviceID: imei, CorrelationID: fmt.Sprintf("%02x-%04x", fn, reg), State: map[string]any{"unit": int(frame[0])}}, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	state := map[string]any{}           /* 更新 state 的值。 */
	for key, value := range ctx.State { /* 循环处理当前数据。 */
		state[key] = value /* 更新 state[key] 的值。 */
	} /* 结束当前表达式或代码块。 */
	state["unit"] = int(frame[0])                                     /* 执行当前语句并推进处理流程。 */
	return Frame{Consumed: length, DeviceID: imei, State: state}, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func stateNumber(s map[string]any, key string) int { /* 定义 stateNumber 函数。 */
	switch v := s[key].(type) { /* 根据条件选择处理路径。 */
	case int: /* 处理当前分支。 */
		return v /* 返回当前处理结果。 */
	case float64: /* 处理当前分支。 */
		return int(v) /* 返回当前处理结果。 */
	default: /* 处理当前分支。 */
		return 0 /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func decodeSensor(data []byte, ctx Context) (Message, error) { /* 定义 decodeSensor 函数。 */
	length, _, frame, err := sensorFrame(data, ctx) /* 更新 err 的值。 */
	if err != nil {                                 /* 判断条件并选择处理分支。 */
		return Message{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if length == 0 || length != len(data) { /* 判断条件并选择处理分支。 */
		return Message{}, errors.New("需要单个完整传感器报文") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if frame[1] == 6 || frame[1] == 0x10 { /* 判断条件并选择处理分支。 */
		return Message{MessageType: "COMMAND_REPLY", Event: map[string]any{"type": "ACK", "function": int(frame[1]), "register": int(binary.BigEndian.Uint16(frame[2:4]))}}, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	register := binary.BigEndian.Uint16(frame[2:4]) /* 更新 register 的值。 */
	body := frame[7 : len(frame)-2]                 /* 更新 body 的值。 */
	switch register {                               /* 根据条件选择处理路径。 */
	case 0: /* 处理当前分支。 */
		if len(body) != 26 { /* 判断条件并选择处理分支。 */
			return Message{}, errors.New("实时数据须为 26 字节") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		value := float64(int32(binary.BigEndian.Uint32(body[6:10]))) / 100                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       /* 更新 value 的值。 */
		return properties(map[string]any{"partType": "液位传感器", "levelMeters": value, "batteryLevel": int(binary.BigEndian.Uint16(body[2:4])), "uploadInterval": int(binary.BigEndian.Uint16(body[4:6])), "signalStrength": int(binary.BigEndian.Uint16(body[10:12])), "collectionInterval": int(binary.BigEndian.Uint16(body[12:14])), "alarmLowerLimit": float64(int32(binary.BigEndian.Uint32(body[14:18]))) / 100, "alarmUpperLimit": float64(int32(binary.BigEndian.Uint32(body[18:22]))) / 100, "alarmEnabled": binary.BigEndian.Uint16(body[22:24]) == 1, "offset": int(int16(binary.BigEndian.Uint16(body[24:26])))}), nil /* 返回当前处理结果。 */
	case 0x16: /* 处理当前分支。 */
		if len(body) != 2 { /* 判断条件并选择处理分支。 */
			return Message{}, errors.New("报警数据须为 2 字节") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		active := binary.BigEndian.Uint16(body) == 1                                                                                                                                                                                                                  /* 更新 active 的值。 */
		return Message{MessageType: "STATE_CHANGE", Properties: map[string]any{"alarm": active, "partType": "液位报警"}, Event: map[string]any{"components": []map[string]any{{"id": "sensor", "name": "液位传感器", "alarms": map[string]bool{"LEVEL_ALARM": active}}}}}, nil /* 返回当前处理结果。 */
	case 0x19: /* 处理当前分支。 */
		if len(body) != 10 { /* 判断条件并选择处理分支。 */
			return Message{}, errors.New("历史数据须为 10 字节") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return properties(map[string]any{"partType": "液位传感器", "historicalLevelMeters": float64(int32(binary.BigEndian.Uint32(body[6:10]))) / 100, "historyTimestamp": int64(binary.BigEndian.Uint32(body[:4])) * 1000}), nil /* 返回当前处理结果。 */
	default: /* 处理当前分支。 */
		return Message{}, fmt.Errorf("未知寄存器 %04x", register) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func intParam(params map[string]any, key string, min, max int64) (int64, error) { /* 定义 intParam 函数。 */
	v, ok := params[key] /* 更新 ok 的值。 */
	if !ok {             /* 判断条件并选择处理分支。 */
		return 0, fmt.Errorf("缺少参数 %s", key) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var n int64            /* 声明 n。 */
	switch x := v.(type) { /* 根据条件选择处理路径。 */
	case int: /* 处理当前分支。 */
		n = int64(x) /* 更新 n 的值。 */
	case int64: /* 处理当前分支。 */
		n = x /* 更新 n 的值。 */
	case float64: /* 处理当前分支。 */
		if math.Trunc(x) != x { /* 判断条件并选择处理分支。 */
			return 0, fmt.Errorf("%s 必须为整数", key) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		n = int64(x) /* 更新 n 的值。 */
	case string: /* 处理当前分支。 */
		var err error                        /* 声明 err。 */
		n, err = strconv.ParseInt(x, 10, 64) /* 更新 err 的值。 */
		if err != nil {                      /* 判断条件并选择处理分支。 */
			return 0, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	default: /* 处理当前分支。 */
		return 0, fmt.Errorf("%s 必须为整数", key) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if n < min || n > max { /* 判断条件并选择处理分支。 */
		return 0, fmt.Errorf("%s 超出范围", key) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return n, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func encodeSensor(cmd Command, ctx Context) (Frame, error) { /* 定义 encodeSensor 函数。 */
	unit, ok := ctx.State["unit"].(float64) /* 更新 ok 的值。 */
	if !ok {                                /* 判断条件并选择处理分支。 */
		if n, yes := ctx.State["unit"].(int); yes { /* 判断条件并选择处理分支。 */
			unit = float64(n) /* 更新 unit 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if unit < 1 || unit > 247 { /* 判断条件并选择处理分支。 */
		return Frame{}, errors.New("请先等待设备上报取得站号") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if cmd.Type == "setMultipleParams" { /* 判断条件并选择处理分支。 */
		collection, e := intParam(cmd.Params, "collectionTime", 0, 65535) /* 更新 e 的值。 */
		if e != nil {                                                     /* 判断条件并选择处理分支。 */
			return Frame{}, e /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		lower, e := intParam(cmd.Params, "alarmLowerLimit", -2147483648, 2147483647) /* 更新 e 的值。 */
		if e != nil {                                                                /* 判断条件并选择处理分支。 */
			return Frame{}, e /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		upper, e := intParam(cmd.Params, "alarmUpperLimit", -2147483648, 2147483647) /* 更新 e 的值。 */
		if e != nil {                                                                /* 判断条件并选择处理分支。 */
			return Frame{}, e /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		body := []byte{byte(unit), 0x10, 0, 6, 0, 5, 10, byte(collection >> 8), byte(collection)}                                                            /* 更新 body 的值。 */
		limits := make([]byte, 8)                                                                                                                            /* 更新 limits 的值。 */
		binary.BigEndian.PutUint32(limits[:4], uint32(int32(lower)))                                                                                         /* 执行当前语句并推进处理流程。 */
		binary.BigEndian.PutUint32(limits[4:], uint32(int32(upper)))                                                                                         /* 执行当前语句并推进处理流程。 */
		body = append(body, limits...)                                                                                                                       /* 更新 body 的值。 */
		return Frame{Reply: rtu(body), CorrelationID: "10-0006", State: map[string]any{"unit": int(unit), "pendingFunction": 16, "pendingRegister": 6}}, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	reg := uint16(0)  /* 更新 reg 的值。 */
	switch cmd.Type { /* 根据条件选择处理路径。 */
	case "setDetectionTime": /* 处理当前分支。 */
		reg = 6 /* 更新 reg 的值。 */
	case "setChangeAlarmValue": /* 处理当前分支。 */
		reg = 0 /* 更新 reg 的值。 */
	case "setUploadTime": /* 处理当前分支。 */
		reg = 2 /* 更新 reg 的值。 */
	case "setOffset": /* 处理当前分支。 */
		reg = 11 /* 更新 reg 的值。 */
	default: /* 处理当前分支。 */
		return Frame{}, fmt.Errorf("不支持命令 %s", cmd.Type) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	v, err := intParam(cmd.Params, "value", -32768, 65535) /* 更新 err 的值。 */
	if err != nil {                                        /* 判断条件并选择处理分支。 */
		return Frame{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	data := rtu([]byte{byte(unit), 6, byte(reg >> 8), byte(reg), byte(uint16(v) >> 8), byte(v)})                                                                                                            /* 更新 data 的值。 */
	return Frame{Reply: data, CorrelationID: fmt.Sprintf("06-%04x", reg), State: map[string]any{"unit": int(unit), "pendingFunction": 6, "pendingRegister": int(reg), "pendingValue": int(uint16(v))}}, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
