package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// The former bridge polled these six coils and published one MQTT message per
// result. Configure six read-coil queries on one active TCP profile instead.
var coilNames = map[int]string{0: "系统运行监测", 3001: "火花探测组1", 3002: "火花探测组2", 3003: "火花探测组3", 3013: "喷淋增压系统", 3042: "手动测试"}

func Protocol() Definition {
	state := map[string]any{"tx": 1, "address": 3001}
	return Definition{
		Name: "KUKA 火花探测 Modbus TCP", Version: "1.0.1", Transport: "TCP", Decode: decodeCoil, Ingress: ingressCoil, Encode: encodeCoil,
		Samples: []Sample{{Name: "火花探测组1报警", Data: []byte{0, 1, 0, 0, 0, 4, 1, 1, 1, 1}, Context: Context{Now: 1789000000000, State: state}, Want: Message{MessageType: "STATE_CHANGE", Properties: map[string]any{"coilAddress": 3001, "value": 1, "partType": "火花探测组1", "state": "火警"}}}},
		Operations: []OperationSample{
			{Name: "半帧", Operation: "ingress", Data: []byte{0, 1, 0, 0, 0}, Want: Frame{NeedMore: true}},
			{Name: "完整线圈响应", Operation: "ingress", Data: []byte{0, 1, 0, 0, 0, 4, 1, 1, 1, 1}, Context: Context{DeviceID: "kuka-host", State: state}, Want: Frame{Consumed: 10, DeviceID: "kuka-host", CorrelationID: "1", State: state}},
			{Name: "读取线圈", Operation: "encode", Command: Command{Type: "coil-3001"}, Context: Context{State: map[string]any{"tx": 0}}, Want: Frame{Reply: []byte{0, 1, 0, 0, 0, 6, 1, 1, 0x0b, 0xb9, 0, 1}, CorrelationID: "1", State: state}},
		},
	}
}

func coilFrame(data []byte) (int, error) {
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
	if f[7] != 1 || length != 4 || f[8] != 1 {
		return 0, errors.New("线圈响应格式无效")
	}
	return len(f), nil
}

func stateInt(s map[string]any, key string) int {
	switch x := s[key].(type) {
	case int:
		return x
	case float64:
		return int(x)
	default:
		return 0
	}
}

func ingressCoil(data []byte, ctx Context) (Frame, error) {
	n, e := coilFrame(data)
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
	if tx != stateInt(ctx.State, "tx") {
		return Frame{}, errors.New("事务 ID 与待查询命令不匹配")
	}
	return Frame{Consumed: n, DeviceID: ctx.DeviceID, CorrelationID: strconv.Itoa(tx), State: ctx.State}, nil
}

func decodeCoil(data []byte, ctx Context) (Message, error) {
	n, e := coilFrame(data)
	if e != nil {
		return Message{}, e
	}
	if n != len(data) {
		return Message{}, errors.New("需要单个完整线圈响应")
	}
	address := stateInt(ctx.State, "address")
	name, ok := coilNames[address]
	if !ok {
		return Message{}, errors.New("缺少查询地址状态")
	}
	bit := int(data[9] & 1)
	props := map[string]any{"coilAddress": address, "value": bit, "partType": name}
	var event map[string]any
	if address == 0 {
		props["state"] = map[bool]string{true: "正常", false: "故障"}[bit == 1]
		event = map[string]any{"components": []map[string]any{{"id": "coil/0", "name": name, "alarms": map[string]bool{"DEVICE_FAULT": bit == 0}}}}
	} else {
		if bit == 1 {
			props["state"] = "火警"
		} else {
			props["state"] = "正常"
		}
		event = map[string]any{"components": []map[string]any{{"id": fmt.Sprintf("coil/%d", address), "name": name, "alarms": map[string]bool{"FIRE": bit == 1}}}}
	}
	return Message{MessageType: "STATE_CHANGE", Properties: props, Event: event}, nil
}

func encodeCoil(cmd Command, ctx Context) (Frame, error) {
	var address int
	if strings.HasPrefix(cmd.Type, "coil-") {
		var err error
		address, err = strconv.Atoi(strings.TrimPrefix(cmd.Type, "coil-"))
		if err != nil {
			return Frame{}, errors.New("无效线圈查询类型")
		}
	} else if cmd.Type == "read-coil" {
		if _, ok := cmd.Params["address"]; !ok {
			return Frame{}, errors.New("缺少 address 参数")
		}
		address = stateInt(cmd.Params, "address")
	} else {
		return Frame{}, fmt.Errorf("不支持命令 %s", cmd.Type)
	}
	if _, ok := coilNames[address]; !ok {
		return Frame{}, errors.New("只允许旧程序配置的六个线圈地址")
	}
	tx := stateInt(ctx.State, "tx")%65535 + 1
	reply := []byte{byte(tx >> 8), byte(tx), 0, 0, 0, 6, 1, 1, byte(address >> 8), byte(address), 0, 1}
	return Frame{Reply: reply, CorrelationID: strconv.Itoa(tx), State: map[string]any{"tx": tx, "address": address}}, nil
}
