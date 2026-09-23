package main /* 声明 main 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"encoding/binary" /* 执行当前语句并推进处理流程。 */
	"errors"          /* 执行当前语句并推进处理流程。 */
	"fmt"             /* 执行当前语句并推进处理流程。 */
	"strconv"         /* 执行当前语句并推进处理流程。 */
	"strings"         /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

// The former bridge polled these six coils and published one MQTT message per
// result. Configure six read-coil queries on one active TCP profile instead.
var coilNames = map[int]string{0: "系统运行监测", 3001: "火花探测组1", 3002: "火花探测组2", 3003: "火花探测组3", 3013: "喷淋增压系统", 3042: "手动测试"} /* 声明 coilNames。 */

func Protocol() Definition { /* 定义 Protocol 函数。 */
	state := map[string]any{"tx": 1, "address": 3001} /* 更新 state 的值。 */
	return Definition{                                /* 返回当前处理结果。 */
		Name: "KUKA 火花探测 Modbus TCP", Version: "1.0.1", Transport: "TCP", Decode: decodeCoil, Ingress: ingressCoil, Encode: encodeCoil, /* 执行当前语句并推进处理流程。 */
		Samples: []Sample{{Name: "火花探测组1报警", Data: []byte{0, 1, 0, 0, 0, 4, 1, 1, 1, 1}, Context: Context{Now: 1789000000000, State: state}, Want: Message{MessageType: "STATE_CHANGE", Properties: map[string]any{"coilAddress": 3001, "value": 1, "partType": "火花探测组1", "state": "火警"}}}}, /* 执行当前语句并推进处理流程。 */
		Operations: []OperationSample{ /* 执行当前语句并推进处理流程。 */
			{Name: "半帧", Operation: "ingress", Data: []byte{0, 1, 0, 0, 0}, Want: Frame{NeedMore: true}},                                                                                                                                       /* 执行当前语句并推进处理流程。 */
			{Name: "完整线圈响应", Operation: "ingress", Data: []byte{0, 1, 0, 0, 0, 4, 1, 1, 1, 1}, Context: Context{DeviceID: "kuka-host", State: state}, Want: Frame{Consumed: 10, DeviceID: "kuka-host", CorrelationID: "1", State: state}},      /* 执行当前语句并推进处理流程。 */
			{Name: "读取线圈", Operation: "encode", Command: Command{Type: "coil-3001"}, Context: Context{State: map[string]any{"tx": 0}}, Want: Frame{Reply: []byte{0, 1, 0, 0, 0, 6, 1, 1, 0x0b, 0xb9, 0, 1}, CorrelationID: "1", State: state}}, /* 执行当前语句并推进处理流程。 */
		}, /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func coilFrame(data []byte) (int, error) { /* 定义 coilFrame 函数。 */
	if len(data) < 6 { /* 判断条件并选择处理分支。 */
		return 0, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if binary.BigEndian.Uint16(data[2:4]) != 0 { /* 判断条件并选择处理分支。 */
		return 0, errors.New("Modbus 协议标识非零") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	length := int(binary.BigEndian.Uint16(data[4:6])) /* 更新 length 的值。 */
	if length < 3 || length > 253 {                   /* 判断条件并选择处理分支。 */
		return 0, errors.New("Modbus 长度无效") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(data) < 6+length { /* 判断条件并选择处理分支。 */
		return 0, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	f := data[:6+length] /* 更新 f 的值。 */
	if f[6] != 1 {       /* 判断条件并选择处理分支。 */
		return 0, errors.New("站号须为 1") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if f[7]&0x80 != 0 { /* 判断条件并选择处理分支。 */
		return 0, fmt.Errorf("Modbus 异常码 %02x", f[8]) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if f[7] != 1 || length != 4 || f[8] != 1 { /* 判断条件并选择处理分支。 */
		return 0, errors.New("线圈响应格式无效") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return len(f), nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func stateInt(s map[string]any, key string) int { /* 定义 stateInt 函数。 */
	switch x := s[key].(type) { /* 根据条件选择处理路径。 */
	case int: /* 处理当前分支。 */
		return x /* 返回当前处理结果。 */
	case float64: /* 处理当前分支。 */
		return int(x) /* 返回当前处理结果。 */
	default: /* 处理当前分支。 */
		return 0 /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func ingressCoil(data []byte, ctx Context) (Frame, error) { /* 定义 ingressCoil 函数。 */
	n, e := coilFrame(data) /* 更新 e 的值。 */
	if e != nil {           /* 判断条件并选择处理分支。 */
		return Frame{}, e /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if n == 0 { /* 判断条件并选择处理分支。 */
		return Frame{NeedMore: true}, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if ctx.DeviceID == "" { /* 判断条件并选择处理分支。 */
		return Frame{}, errors.New("主动连接实例须预配置设备 ID") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	tx := int(binary.BigEndian.Uint16(data[:2])) /* 更新 tx 的值。 */
	if tx != stateInt(ctx.State, "tx") {         /* 判断条件并选择处理分支。 */
		return Frame{}, errors.New("事务 ID 与待查询命令不匹配") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return Frame{Consumed: n, DeviceID: ctx.DeviceID, CorrelationID: strconv.Itoa(tx), State: ctx.State}, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func decodeCoil(data []byte, ctx Context) (Message, error) { /* 定义 decodeCoil 函数。 */
	n, e := coilFrame(data) /* 更新 e 的值。 */
	if e != nil {           /* 判断条件并选择处理分支。 */
		return Message{}, e /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if n != len(data) { /* 判断条件并选择处理分支。 */
		return Message{}, errors.New("需要单个完整线圈响应") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	address := stateInt(ctx.State, "address") /* 更新 address 的值。 */
	name, ok := coilNames[address]            /* 更新 ok 的值。 */
	if !ok {                                  /* 判断条件并选择处理分支。 */
		return Message{}, errors.New("缺少查询地址状态") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	bit := int(data[9] & 1)                                                         /* 更新 bit 的值。 */
	props := map[string]any{"coilAddress": address, "value": bit, "partType": name} /* 更新 props 的值。 */
	var event map[string]any                                                        /* 声明 event。 */
	if address == 0 {                                                               /* 判断条件并选择处理分支。 */
		props["state"] = map[bool]string{true: "正常", false: "故障"}[bit == 1]                                                                         /* 执行当前语句并推进处理流程。 */
		event = map[string]any{"components": []map[string]any{{"id": "coil/0", "name": name, "alarms": map[string]bool{"DEVICE_FAULT": bit == 0}}}} /* 更新 event 的值。 */
	} else { /* 结束当前表达式或代码块。 */
		if bit == 1 { /* 判断条件并选择处理分支。 */
			props["state"] = "火警" /* 执行当前语句并推进处理流程。 */
		} else { /* 结束当前表达式或代码块。 */
			props["state"] = "正常" /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		event = map[string]any{"components": []map[string]any{{"id": fmt.Sprintf("coil/%d", address), "name": name, "alarms": map[string]bool{"FIRE": bit == 1}}}} /* 更新 event 的值。 */
	} /* 结束当前表达式或代码块。 */
	return Message{MessageType: "STATE_CHANGE", Properties: props, Event: event}, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func encodeCoil(cmd Command, ctx Context) (Frame, error) { /* 定义 encodeCoil 函数。 */
	var address int                           /* 声明 address。 */
	if strings.HasPrefix(cmd.Type, "coil-") { /* 判断条件并选择处理分支。 */
		var err error                                                      /* 声明 err。 */
		address, err = strconv.Atoi(strings.TrimPrefix(cmd.Type, "coil-")) /* 更新 err 的值。 */
		if err != nil {                                                    /* 判断条件并选择处理分支。 */
			return Frame{}, errors.New("无效线圈查询类型") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} else if cmd.Type == "read-coil" { /* 结束当前表达式或代码块。 */
		if _, ok := cmd.Params["address"]; !ok { /* 判断条件并选择处理分支。 */
			return Frame{}, errors.New("缺少 address 参数") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		address = stateInt(cmd.Params, "address") /* 更新 address 的值。 */
	} else { /* 结束当前表达式或代码块。 */
		return Frame{}, fmt.Errorf("不支持命令 %s", cmd.Type) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if _, ok := coilNames[address]; !ok { /* 判断条件并选择处理分支。 */
		return Frame{}, errors.New("只允许旧程序配置的六个线圈地址") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	tx := stateInt(ctx.State, "tx")%65535 + 1                                                                             /* 更新 tx 的值。 */
	reply := []byte{byte(tx >> 8), byte(tx), 0, 0, 0, 6, 1, 1, byte(address >> 8), byte(address), 0, 1}                   /* 更新 reply 的值。 */
	return Frame{Reply: reply, CorrelationID: strconv.Itoa(tx), State: map[string]any{"tx": tx, "address": address}}, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
