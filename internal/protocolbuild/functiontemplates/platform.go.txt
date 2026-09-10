// 平台适配代码。业务逻辑写在 protocol.go；本文件无需修改。
package main

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

type Message struct {
	MessageType string            `json:"messageType"`
	Properties  map[string]any    `json:"properties,omitempty"`
	Event       map[string]any    `json:"event,omitempty"`
	Timestamp   int64             `json:"timestamp,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
}

type Context struct {
	DeviceID string
	Now      int64          // Unix 毫秒
	State    map[string]any // 由平台保存并传入下一次调用，不使用全局变量保存会话
	Metadata map[string]any
}

type Child struct {
	Address string
	Type    string
	Name    string
	Data    []byte // 子设备原始报文；空表示仅登记台账
}

type Frame struct {
	Children      []Child
	Consumed      int
	NeedMore      bool
	DeviceID      string
	DeviceName    string
	Reply         []byte
	State         map[string]any
	CorrelationID string
}

type Command struct {
	Type   string
	Params map[string]any
}

type Sample struct {
	Name    string
	Data    []byte
	Want    Message
	Context Context
}

type OperationSample struct {
	Name      string
	Operation string // ingress 或 encode
	Data      []byte
	Command   Command
	Context   Context
	Want      Frame
}

type Definition struct {
	Name       string
	Version    string // 可留空，由平台生成新版本号
	Transport  string // 可留空：有 Ingress 时 TCP，否则 MQTT
	Decode     func([]byte, Context) (Message, error)
	Ingress    func([]byte, Context) (Frame, error)
	Encode     func(Command, Context) (Frame, error)
	Samples    []Sample
	Operations []OperationSample
}

func properties(values map[string]any) Message {
	return Message{MessageType: "PROPERTY_REPORT", Properties: values}
}

func platformFrame(frame Frame) map[string]any {
	result := map[string]any{"consumed": frame.Consumed, "needMore": frame.NeedMore, "deviceId": frame.DeviceID, "reply": hex.EncodeToString(frame.Reply)}
	if frame.DeviceName != "" {
		result["deviceName"] = frame.DeviceName
	}
	if frame.State != nil {
		result["state"] = frame.State
	}
	if frame.CorrelationID != "" {
		result["correlationId"] = frame.CorrelationID
	}
	if len(frame.Children) > 0 {
		children := []any{}
		for _, c := range frame.Children {
			children = append(children, map[string]any{"address": c.Address, "type": c.Type, "name": c.Name, "payload": hex.EncodeToString(c.Data)})
		}
		result["children"] = children
	}
	return result
}

func platformCommand(command Command) map[string]any {
	result := map[string]any{}
	for k, v := range command.Params {
		result[k] = v
	}
	result["type"] = command.Type
	return result
}

func platformDescribe(p Definition) (any, error) {
	if p.Decode == nil {
		return nil, errors.New("请在 Protocol 中设置 Decode 函数")
	}
	capabilities := []string{"decode"}
	transport := p.Transport
	if transport == "" {
		transport = "MQTT"
		if p.Ingress != nil {
			transport = "TCP"
		}
	}
	if p.Ingress != nil {
		capabilities = append(capabilities, "ingress")
	}
	if p.Encode != nil {
		capabilities = append(capabilities, "encode")
	}
	cases := []any{}
	for _, s := range p.Samples {
		if s.Want.MessageType == "" {
			return nil, errors.New("每条 Go 样例需要 Want.MessageType")
		}
		c := map[string]any{"name": s.Name, "input": map[string]any{"payloadFormat": "hex", "payload": hex.EncodeToString(s.Data), "deviceId": s.Context.DeviceID, "receivedAt": s.Context.Now, "metadata": s.Context.Metadata}, "expectedMessageType": s.Want.MessageType, "expectedProperties": s.Want.Properties, "expectedEvent": s.Want.Event, "expectedTags": s.Want.Tags}
		if s.Context.State != nil {
			input := c["input"].(map[string]any)
			metadata := map[string]any{}
			for k, v := range s.Context.Metadata {
				metadata[k] = v
			}
			metadata["protocolState"] = s.Context.State
			input["metadata"] = metadata
		}
		if s.Want.Timestamp != 0 {
			c["expectedTimestamp"] = s.Want.Timestamp
		}
		if value, ok := s.Want.Event["type"]; ok {
			c["expectedEventType"] = value
		}
		cases = append(cases, c)
	}
	operations := []any{}
	for _, s := range p.Operations {
		request := map[string]any{"operation": s.Operation, "deviceId": s.Context.DeviceID, "data": hex.EncodeToString(s.Data), "state": s.Context.State, "now": s.Context.Now}
		if s.Operation == "encode" {
			request["command"] = platformCommand(s.Command)
		}
		operations = append(operations, map[string]any{"name": s.Name, "request": request, "expected": platformFrame(s.Want)})
	}
	return map[string]any{"metadata": map[string]any{"name": p.Name, "version": p.Version, "runtime": "go-protocol-v2", "transport": transport, "payloadFormat": "hex", "capabilities": capabilities}, "cases": cases, "operations": operations}, nil
}

func platformHandle() (any, error) {
	var q struct {
		Operation string         `json:"operation"`
		DeviceID  string         `json:"deviceId"`
		Data      string         `json:"data"`
		State     map[string]any `json:"state"`
		Command   map[string]any `json:"command"`
		Now       int64          `json:"now"`
		Raw       *struct {
			Payload  json.RawMessage `json:"payload"`
			DeviceID string          `json:"deviceId"`
			Metadata map[string]any  `json:"metadata"`
		} `json:"raw"`
	}
	if err := json.NewDecoder(os.Stdin).Decode(&q); err != nil {
		return nil, err
	}
	p := Protocol()
	if q.Operation == "describe" {
		return platformDescribe(p)
	}
	context := Context{Now: q.Now, State: q.State, DeviceID: q.DeviceID}
	if context.State == nil {
		context.State = map[string]any{}
	}
	switch q.Operation {
	case "decode":
		if q.Raw == nil || p.Decode == nil {
			return nil, errors.New("缺少报文或 Decode 函数")
		}
		var payload string
		if err := json.Unmarshal(q.Raw.Payload, &payload); err != nil {
			return nil, err
		}
		data, err := hex.DecodeString(strings.Join(strings.Fields(payload), ""))
		if err != nil {
			return nil, err
		}
		context.DeviceID = q.Raw.DeviceID
		context.Metadata = q.Raw.Metadata
		message, err := p.Decode(data, context)
		if err != nil {
			return nil, err
		}
		return map[string]any{"standardMessage": message}, nil
	case "ingress":
		if p.Ingress == nil {
			return nil, errors.New("未实现 Ingress")
		}
		data, err := hex.DecodeString(q.Data)
		if err != nil {
			return nil, err
		}
		frame, err := p.Ingress(data, context)
		if err != nil {
			return nil, err
		}
		return platformFrame(frame), nil
	case "encode":
		if p.Encode == nil {
			return nil, errors.New("未实现 Encode")
		}
		kind, _ := q.Command["type"].(string)
		frame, err := p.Encode(Command{Type: kind, Params: q.Command}, context)
		if err != nil {
			return nil, err
		}
		return platformFrame(frame), nil
	default:
		return nil, fmt.Errorf("未知操作 %q", q.Operation)
	}
}

func main() {
	output, err := platformHandle()
	if err != nil {
		output = map[string]string{"error": err.Error()}
	}
	if err := json.NewEncoder(os.Stdout).Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
