package main /* 声明 main 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"encoding/binary" /* 执行当前语句并推进处理流程。 */
	"errors"          /* 执行当前语句并推进处理流程。 */
	"fmt"             /* 执行当前语句并推进处理流程。 */
	"strconv"         /* 执行当前语句并推进处理流程。 */
	"strings"         /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

// Query address 200/count 3 for the host; 0..7/count 1 for faults;
// 100..107/count 1 for status. The old bridge polled all 17 ranges.
func Protocol() Definition { /* 定义 Protocol 函数。 */
	state := map[string]any{"tx": 1, "address": 200, "count": 3} /* 更新 state 的值。 */
	return Definition{                                           /* 返回当前处理结果。 */
		Name: "消防水炮 Modbus TCP", Version: "1.0.1", Transport: "TCP", Decode: decodeCannon, Ingress: ingressCannon, Encode: encodeCannon, /* 执行当前语句并推进处理流程。 */
		Samples: []Sample{{Name: "主机火警及泵", Data: []byte{0, 1, 0, 0, 0, 9, 1, 4, 6, 0, 1, 0, 0, 0, 1}, Context: Context{Now: 1789000000000, State: state}, Want: Message{MessageType: "STATE_CHANGE", Properties: map[string]any{"partType": "主机整体状态", "state": "火警", "fireAlarm": true, "fault": false, "pumpState": 1}}}}, /* 执行当前语句并推进处理流程。 */
		Operations: []OperationSample{ /* 执行当前语句并推进处理流程。 */
			{Name: "半帧", Operation: "ingress", Data: []byte{0, 1, 0, 0, 0}, Want: Frame{NeedMore: true}}, /* 执行当前语句并推进处理流程。 */
			{Name: "主机响应", Operation: "ingress", Data: []byte{0, 1, 0, 0, 0, 9, 1, 4, 6, 0, 1, 0, 0, 0, 1}, Context: Context{DeviceID: "CANNON_HOST", State: state}, Want: Frame{Consumed: 15, DeviceID: "CANNON_HOST", CorrelationID: "1", State: state}}, /* 执行当前语句并推进处理流程。 */
			{Name: "读取主机", Operation: "encode", Command: Command{Type: "host"}, Context: Context{State: map[string]any{"tx": 0}}, Want: Frame{Reply: []byte{0, 1, 0, 0, 0, 6, 1, 4, 0, 200, 0, 3}, CorrelationID: "1", State: state}},                      /* 执行当前语句并推进处理流程。 */
		}, /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func spInt(s map[string]any, key string) int { /* 定义 spInt 函数。 */
	switch v := s[key].(type) { /* 根据条件选择处理路径。 */
	case int: /* 处理当前分支。 */
		return v /* 返回当前处理结果。 */
	case float64: /* 处理当前分支。 */
		return int(v) /* 返回当前处理结果。 */
	default: /* 处理当前分支。 */
		return 0 /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func cannonFrame(data []byte) (int, error) { /* 定义 cannonFrame 函数。 */
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
	if f[7] != 4 || int(f[8]) != length-3 || int(f[8])%2 != 0 { /* 判断条件并选择处理分支。 */
		return 0, errors.New("输入寄存器响应格式无效") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return 6 + length, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func ingressCannon(data []byte, ctx Context) (Frame, error) { /* 定义 ingressCannon 函数。 */
	n, e := cannonFrame(data) /* 更新 e 的值。 */
	if e != nil {             /* 判断条件并选择处理分支。 */
		return Frame{}, e /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if n == 0 { /* 判断条件并选择处理分支。 */
		return Frame{NeedMore: true}, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if ctx.DeviceID == "" { /* 判断条件并选择处理分支。 */
		return Frame{}, errors.New("主动连接实例须预配置设备 ID") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	tx := int(binary.BigEndian.Uint16(data[:2]))                                     /* 更新 tx 的值。 */
	if tx != spInt(ctx.State, "tx") || int(data[8]) != spInt(ctx.State, "count")*2 { /* 判断条件并选择处理分支。 */
		return Frame{}, errors.New("响应与待查询命令不匹配") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return Frame{Consumed: n, DeviceID: ctx.DeviceID, CorrelationID: strconv.Itoa(tx), State: ctx.State}, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func decodeCannon(data []byte, ctx Context) (Message, error) { /* 定义 decodeCannon 函数。 */
	n, e := cannonFrame(data) /* 更新 e 的值。 */
	if e != nil {             /* 判断条件并选择处理分支。 */
		return Message{}, e /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if n != len(data) { /* 判断条件并选择处理分支。 */
		return Message{}, errors.New("需要单个完整水炮响应") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	address, count := spInt(ctx.State, "address"), spInt(ctx.State, "count") /* 更新 count 的值。 */
	if int(data[8]) != count*2 {                                             /* 判断条件并选择处理分支。 */
		return Message{}, errors.New("数据长度与查询数量不符") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if address == 200 && count == 3 { /* 判断条件并选择处理分支。 */
		fire, fault, pump := binary.BigEndian.Uint16(data[9:11]) == 1, binary.BigEndian.Uint16(data[11:13]) == 1, int(binary.BigEndian.Uint16(data[13:15])) /* 更新 pump 的值。 */
		state := "正常"                                                                                                                                       /* 更新 state 的值。 */
		if fire {                                                                                                                                           /* 判断条件并选择处理分支。 */
			state = "火警" /* 更新 state 的值。 */
		} else if fault { /* 结束当前表达式或代码块。 */
			state = "故障" /* 更新 state 的值。 */
		} /* 结束当前表达式或代码块。 */
		return Message{MessageType: "STATE_CHANGE", Properties: map[string]any{"partType": "主机整体状态", "state": state, "fireAlarm": fire, "fault": fault, "pumpState": pump}, Event: map[string]any{"components": []map[string]any{{"id": "host", "name": "消防水炮主机", "alarms": map[string]bool{"FIRE": fire, "DEVICE_FAULT": fault}}}}}, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if count != 1 { /* 判断条件并选择处理分支。 */
		return Message{}, errors.New("水炮单点查询须读取一个寄存器") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	v := int(binary.BigEndian.Uint16(data[9:11])) /* 更新 v 的值。 */
	if address >= 0 && address <= 7 {             /* 判断条件并选择处理分支。 */
		id := address + 1                                                                                                                                                                                                                                                                                                          /* 更新 id 的值。 */
		active := v == 1                                                                                                                                                                                                                                                                                                           /* 更新 active 的值。 */
		return Message{MessageType: "STATE_CHANGE", Properties: map[string]any{"partType": "消防水炮状态", "cannonIndex": id, "fault": active}, Event: map[string]any{"components": []map[string]any{{"id": fmt.Sprintf("cannon/%d", id), "name": fmt.Sprintf("消防水炮 %d", id), "alarms": map[string]bool{"DEVICE_FAULT": active}}}}}, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if address >= 100 && address <= 107 { /* 判断条件并选择处理分支。 */
		id := address - 99          /* 更新 id 的值。 */
		fire := v&1 != 0            /* 更新 fire 的值。 */
		mode := (v >> 4) & 3        /* 更新 mode 的值。 */
		if mode != 1 && mode != 2 { /* 判断条件并选择处理分支。 */
			mode = 0 /* 更新 mode 的值。 */
		} /* 结束当前表达式或代码块。 */
		return Message{MessageType: "STATE_CHANGE", Properties: map[string]any{"partType": "消防水炮状态", "cannonIndex": id, "fireAlarm": fire, "workState": mode, "waterFlow": boolInt(v&0x40 != 0), "valveOpen": boolInt(v&0x80 != 0), "isFog": boolInt(v&0x100 != 0)}, Event: map[string]any{"components": []map[string]any{{"id": fmt.Sprintf("cannon/%d", id), "name": fmt.Sprintf("消防水炮 %d", id), "alarms": map[string]bool{"FIRE": fire}}}}}, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return Message{}, errors.New("查询地址未配置") /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func boolInt(b bool) int { /* 定义 boolInt 函数。 */
	if b { /* 判断条件并选择处理分支。 */
		return 1 /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return 0 /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func encodeCannon(cmd Command, ctx Context) (Frame, error) { /* 定义 encodeCannon 函数。 */
	var address, count int /* 声明 address。 */
	switch {               /* 根据条件选择处理路径。 */
	case cmd.Type == "host": /* 处理当前分支。 */
		address, count = 200, 3 /* 更新 count 的值。 */
	case strings.HasPrefix(cmd.Type, "fault-"): /* 处理当前分支。 */
		n, err := strconv.Atoi(strings.TrimPrefix(cmd.Type, "fault-")) /* 更新 err 的值。 */
		if err != nil || n < 1 || n > 8 {                              /* 判断条件并选择处理分支。 */
			return Frame{}, errors.New("无效水炮故障点") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		address, count = n-1, 1 /* 更新 count 的值。 */
	case strings.HasPrefix(cmd.Type, "status-"): /* 处理当前分支。 */
		n, err := strconv.Atoi(strings.TrimPrefix(cmd.Type, "status-")) /* 更新 err 的值。 */
		if err != nil || n < 1 || n > 8 {                               /* 判断条件并选择处理分支。 */
			return Frame{}, errors.New("无效水炮状态点") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		address, count = 99+n, 1 /* 更新 count 的值。 */
	case cmd.Type == "read-input": /* 处理当前分支。 */
		address, count = spInt(cmd.Params, "address"), spInt(cmd.Params, "count") /* 更新 count 的值。 */
	default: /* 处理当前分支。 */
		return Frame{}, fmt.Errorf("不支持命令 %s", cmd.Type) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !((address == 200 && count == 3) || (count == 1 && ((address >= 0 && address <= 7) || (address >= 100 && address <= 107)))) { /* 判断条件并选择处理分支。 */
		return Frame{}, errors.New("查询地址或数量不在旧水炮点位表内") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	tx := spInt(ctx.State, "tx")%65535 + 1                                                                                                                                                                                               /* 更新 tx 的值。 */
	return Frame{Reply: []byte{byte(tx >> 8), byte(tx), 0, 0, 0, 6, 1, 4, byte(address >> 8), byte(address), 0, byte(count)}, CorrelationID: strconv.Itoa(tx), State: map[string]any{"tx": tx, "address": address, "count": count}}, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
