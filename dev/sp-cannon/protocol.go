package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Query address 200/count 3 for the host; 0..7/count 1 for faults;
// 100..107/count 1 for status. The old bridge polled all 17 ranges.
func Protocol() Definition {
	state := map[string]any{"tx": 1, "address": 200, "count": 3}
	return Definition{
		Name: "消防水炮 Modbus TCP", Version: "1.0.1", Transport: "TCP", Decode: decodeCannon, Ingress: ingressCannon, Encode: encodeCannon,
		Samples: []Sample{{Name: "主机火警及泵", Data: []byte{0, 1, 0, 0, 0, 9, 1, 4, 6, 0, 1, 0, 0, 0, 1}, Context: Context{Now: 1789000000000, State: state}, Want: Message{MessageType: "STATE_CHANGE", Properties: map[string]any{"partType": "主机整体状态", "state": "火警", "fireAlarm": true, "fault": false, "pumpState": 1}}}},
		Operations: []OperationSample{
			{Name: "半帧", Operation: "ingress", Data: []byte{0, 1, 0, 0, 0}, Want: Frame{NeedMore: true}},
			{Name: "主机响应", Operation: "ingress", Data: []byte{0, 1, 0, 0, 0, 9, 1, 4, 6, 0, 1, 0, 0, 0, 1}, Context: Context{DeviceID: "CANNON_HOST", State: state}, Want: Frame{Consumed: 15, DeviceID: "CANNON_HOST", CorrelationID: "1", State: state}},
			{Name: "读取主机", Operation: "encode", Command: Command{Type: "host"}, Context: Context{State: map[string]any{"tx": 0}}, Want: Frame{Reply: []byte{0, 1, 0, 0, 0, 6, 1, 4, 0, 200, 0, 3}, CorrelationID: "1", State: state}},
		},
	}
}

func spInt(s map[string]any, key string) int {
	switch v := s[key].(type) {
	case int:
		return v
	case float64:
		return int(v)
	default:
		return 0
	}
}

func cannonFrame(data []byte) (int, error) {
	if len(data) < 6 {
		return 0, nil
	}
	if binary.BigEndian.Uint16(data[2:4]) != 0 {
		return 0, errors.New("Modbus 协议标识非零")
	}
	length := int(binary.BigEndian.Uint16(data[4:6]))
	if length < 3 || length > 253 {
		return 0, errors.New("Modbus 长度无效")
	}
	if len(data) < 6+length {
		return 0, nil
	}
	f := data[:6+length]
	if f[6] != 1 {
		return 0, errors.New("站号须为 1")
	}
	if f[7]&0x80 != 0 {
		return 0, fmt.Errorf("Modbus 异常码 %02x", f[8])
	}
	if f[7] != 4 || int(f[8]) != length-3 || int(f[8])%2 != 0 {
		return 0, errors.New("输入寄存器响应格式无效")
	}
	return 6 + length, nil
}

func ingressCannon(data []byte, ctx Context) (Frame, error) {
	n, e := cannonFrame(data)
	if e != nil {
		return Frame{}, e
	}
	if n == 0 {
		return Frame{NeedMore: true}, nil
	}
	if ctx.DeviceID == "" {
		return Frame{}, errors.New("主动连接实例须预配置设备 ID")
	}
	tx := int(binary.BigEndian.Uint16(data[:2]))
	if tx != spInt(ctx.State, "tx") || int(data[8]) != spInt(ctx.State, "count")*2 {
		return Frame{}, errors.New("响应与待查询命令不匹配")
	}
	return Frame{Consumed: n, DeviceID: ctx.DeviceID, CorrelationID: strconv.Itoa(tx), State: ctx.State}, nil
}

func decodeCannon(data []byte, ctx Context) (Message, error) {
	n, e := cannonFrame(data)
	if e != nil {
		return Message{}, e
	}
	if n != len(data) {
		return Message{}, errors.New("需要单个完整水炮响应")
	}
	address, count := spInt(ctx.State, "address"), spInt(ctx.State, "count")
	if int(data[8]) != count*2 {
		return Message{}, errors.New("数据长度与查询数量不符")
	}
	if address == 200 && count == 3 {
		fire, fault, pump := binary.BigEndian.Uint16(data[9:11]) == 1, binary.BigEndian.Uint16(data[11:13]) == 1, int(binary.BigEndian.Uint16(data[13:15]))
		state := "正常"
		if fire {
			state = "火警"
		} else if fault {
			state = "故障"
		}
		return Message{MessageType: "STATE_CHANGE", Properties: map[string]any{"partType": "主机整体状态", "state": state, "fireAlarm": fire, "fault": fault, "pumpState": pump}, Event: map[string]any{"components": []map[string]any{{"id": "host", "name": "消防水炮主机", "alarms": map[string]bool{"FIRE": fire, "DEVICE_FAULT": fault}}}}}, nil
	}
	if count != 1 {
		return Message{}, errors.New("水炮单点查询须读取一个寄存器")
	}
	v := int(binary.BigEndian.Uint16(data[9:11]))
	if address >= 0 && address <= 7 {
		id := address + 1
		active := v == 1
		return Message{MessageType: "STATE_CHANGE", Properties: map[string]any{"partType": "消防水炮状态", "cannonIndex": id, "fault": active}, Event: map[string]any{"components": []map[string]any{{"id": fmt.Sprintf("cannon/%d", id), "name": fmt.Sprintf("消防水炮 %d", id), "alarms": map[string]bool{"DEVICE_FAULT": active}}}}}, nil
	}
	if address >= 100 && address <= 107 {
		id := address - 99
		fire := v&1 != 0
		mode := (v >> 4) & 3
		if mode != 1 && mode != 2 {
			mode = 0
		}
		return Message{MessageType: "STATE_CHANGE", Properties: map[string]any{"partType": "消防水炮状态", "cannonIndex": id, "fireAlarm": fire, "workState": mode, "waterFlow": boolInt(v&0x40 != 0), "valveOpen": boolInt(v&0x80 != 0), "isFog": boolInt(v&0x100 != 0)}, Event: map[string]any{"components": []map[string]any{{"id": fmt.Sprintf("cannon/%d", id), "name": fmt.Sprintf("消防水炮 %d", id), "alarms": map[string]bool{"FIRE": fire}}}}}, nil
	}
	return Message{}, errors.New("查询地址未配置")
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func encodeCannon(cmd Command, ctx Context) (Frame, error) {
	var address, count int
	switch {
	case cmd.Type == "host":
		address, count = 200, 3
	case strings.HasPrefix(cmd.Type, "fault-"):
		n, err := strconv.Atoi(strings.TrimPrefix(cmd.Type, "fault-"))
		if err != nil || n < 1 || n > 8 {
			return Frame{}, errors.New("无效水炮故障点")
		}
		address, count = n-1, 1
	case strings.HasPrefix(cmd.Type, "status-"):
		n, err := strconv.Atoi(strings.TrimPrefix(cmd.Type, "status-"))
		if err != nil || n < 1 || n > 8 {
			return Frame{}, errors.New("无效水炮状态点")
		}
		address, count = 99+n, 1
	case cmd.Type == "read-input":
		address, count = spInt(cmd.Params, "address"), spInt(cmd.Params, "count")
	default:
		return Frame{}, fmt.Errorf("不支持命令 %s", cmd.Type)
	}
	if !((address == 200 && count == 3) || (count == 1 && ((address >= 0 && address <= 7) || (address >= 100 && address <= 107)))) {
		return Frame{}, errors.New("查询地址或数量不在旧水炮点位表内")
	}
	tx := spInt(ctx.State, "tx")%65535 + 1
	return Frame{Reply: []byte{byte(tx >> 8), byte(tx), 0, 0, 0, 6, 1, 4, byte(address >> 8), byte(address), 0, byte(count)}, CorrelationID: strconv.Itoa(tx), State: map[string]any{"tx": tx, "address": address, "count": count}}, nil
}
