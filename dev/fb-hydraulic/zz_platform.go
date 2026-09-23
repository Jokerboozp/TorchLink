// 平台适配代码。业务逻辑写在 protocol.go；本文件无需修改。
package main /* 声明 main 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"encoding/hex"  /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"errors"        /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */
	"os"            /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type Message struct { /* 定义 Message 类型。 */
	MessageType string            `json:"messageType"`          /* 执行当前语句并推进处理流程。 */
	Properties  map[string]any    `json:"properties,omitempty"` /* 执行当前语句并推进处理流程。 */
	Event       map[string]any    `json:"event,omitempty"`      /* 执行当前语句并推进处理流程。 */
	Timestamp   int64             `json:"timestamp,omitempty"`  /* 执行当前语句并推进处理流程。 */
	Tags        map[string]string `json:"tags,omitempty"`       /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type Context struct { /* 定义 Context 类型。 */
	DeviceID string         /* 执行当前语句并推进处理流程。 */
	Now      int64          /* 执行当前语句并推进处理流程。 */ // Unix 毫秒
	State    map[string]any /* 执行当前语句并推进处理流程。 */ // 由平台保存并传入下一次调用，不使用全局变量保存会话
	Metadata map[string]any /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type Child struct { /* 定义 Child 类型。 */
	Address string /* 执行当前语句并推进处理流程。 */
	Type    string /* 执行当前语句并推进处理流程。 */
	Name    string /* 执行当前语句并推进处理流程。 */
	Data    []byte /* 执行当前语句并推进处理流程。 */ // 子设备原始报文；空表示仅登记台账
} /* 结束当前表达式或代码块。 */

type Frame struct { /* 定义 Frame 类型。 */
	Children      []Child        /* 执行当前语句并推进处理流程。 */
	Consumed      int            /* 执行当前语句并推进处理流程。 */
	NeedMore      bool           /* 执行当前语句并推进处理流程。 */
	DeviceID      string         /* 执行当前语句并推进处理流程。 */
	DeviceName    string         /* 执行当前语句并推进处理流程。 */
	Reply         []byte         /* 执行当前语句并推进处理流程。 */
	State         map[string]any /* 执行当前语句并推进处理流程。 */
	CorrelationID string         /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type Command struct { /* 定义 Command 类型。 */
	Type   string         /* 执行当前语句并推进处理流程。 */
	Params map[string]any /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type Sample struct { /* 定义 Sample 类型。 */
	Name    string  /* 执行当前语句并推进处理流程。 */
	Data    []byte  /* 执行当前语句并推进处理流程。 */
	Want    Message /* 执行当前语句并推进处理流程。 */
	Context Context /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type OperationSample struct { /* 定义 OperationSample 类型。 */
	Name      string  /* 执行当前语句并推进处理流程。 */
	Operation string  /* 执行当前语句并推进处理流程。 */ // ingress 或 encode
	Data      []byte  /* 执行当前语句并推进处理流程。 */
	Command   Command /* 执行当前语句并推进处理流程。 */
	Context   Context /* 执行当前语句并推进处理流程。 */
	Want      Frame   /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type Definition struct { /* 定义 Definition 类型。 */
	Name       string                                 /* 执行当前语句并推进处理流程。 */
	Version    string                                 /* 执行当前语句并推进处理流程。 */ // 可留空，由平台生成新版本号
	Transport  string                                 /* 执行当前语句并推进处理流程。 */ // 可留空：有 Ingress 时 TCP，否则 MQTT
	Decode     func([]byte, Context) (Message, error) /* 执行当前语句并推进处理流程。 */
	Ingress    func([]byte, Context) (Frame, error)   /* 执行当前语句并推进处理流程。 */
	Encode     func(Command, Context) (Frame, error)  /* 执行当前语句并推进处理流程。 */
	Samples    []Sample                               /* 执行当前语句并推进处理流程。 */
	Operations []OperationSample                      /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func properties(values map[string]any) Message { /* 定义 properties 函数。 */
	return Message{MessageType: "PROPERTY_REPORT", Properties: values} /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func platformFrame(frame Frame) map[string]any { /* 定义 platformFrame 函数。 */
	result := map[string]any{"consumed": frame.Consumed, "needMore": frame.NeedMore, "deviceId": frame.DeviceID, "reply": hex.EncodeToString(frame.Reply)} /* 更新 result 的值。 */
	if frame.DeviceName != "" {                                                                                                                            /* 判断条件并选择处理分支。 */
		result["deviceName"] = frame.DeviceName /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if frame.State != nil { /* 判断条件并选择处理分支。 */
		result["state"] = frame.State /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if frame.CorrelationID != "" { /* 判断条件并选择处理分支。 */
		result["correlationId"] = frame.CorrelationID /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if len(frame.Children) > 0 { /* 判断条件并选择处理分支。 */
		children := []any{}                /* 更新 children 的值。 */
		for _, c := range frame.Children { /* 循环处理当前数据。 */
			children = append(children, map[string]any{"address": c.Address, "type": c.Type, "name": c.Name, "payload": hex.EncodeToString(c.Data)}) /* 更新 children 的值。 */
		} /* 结束当前表达式或代码块。 */
		result["children"] = children /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	return result /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func platformCommand(command Command) map[string]any { /* 定义 platformCommand 函数。 */
	result := map[string]any{}         /* 更新 result 的值。 */
	for k, v := range command.Params { /* 循环处理当前数据。 */
		result[k] = v /* 更新 result[k] 的值。 */
	} /* 结束当前表达式或代码块。 */
	result["type"] = command.Type /* 执行当前语句并推进处理流程。 */
	return result                 /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func platformDescribe(p Definition) (any, error) { /* 定义 platformDescribe 函数。 */
	if p.Decode == nil { /* 判断条件并选择处理分支。 */
		return nil, errors.New("请在 Protocol 中设置 Decode 函数") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	capabilities := []string{"decode"} /* 更新 capabilities 的值。 */
	transport := p.Transport           /* 更新 transport 的值。 */
	if transport == "" {               /* 判断条件并选择处理分支。 */
		transport = "MQTT"    /* 更新 transport 的值。 */
		if p.Ingress != nil { /* 判断条件并选择处理分支。 */
			transport = "TCP" /* 更新 transport 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if p.Ingress != nil { /* 判断条件并选择处理分支。 */
		capabilities = append(capabilities, "ingress") /* 更新 capabilities 的值。 */
	} /* 结束当前表达式或代码块。 */
	if p.Encode != nil { /* 判断条件并选择处理分支。 */
		capabilities = append(capabilities, "encode") /* 更新 capabilities 的值。 */
	} /* 结束当前表达式或代码块。 */
	cases := []any{}              /* 更新 cases 的值。 */
	for _, s := range p.Samples { /* 循环处理当前数据。 */
		if s.Want.MessageType == "" { /* 判断条件并选择处理分支。 */
			return nil, errors.New("每条 Go 样例需要 Want.MessageType") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		c := map[string]any{"name": s.Name, "input": map[string]any{"payloadFormat": "hex", "payload": hex.EncodeToString(s.Data), "deviceId": s.Context.DeviceID, "receivedAt": s.Context.Now, "metadata": s.Context.Metadata}, "expectedMessageType": s.Want.MessageType, "expectedProperties": s.Want.Properties, "expectedEvent": s.Want.Event, "expectedTags": s.Want.Tags} /* 更新 c 的值。 */
		if s.Context.State != nil {                                                                                                                                                                                                                                                                                                                                              /* 判断条件并选择处理分支。 */
			input := c["input"].(map[string]any)   /* 更新 input 的值。 */
			metadata := map[string]any{}           /* 更新 metadata 的值。 */
			for k, v := range s.Context.Metadata { /* 循环处理当前数据。 */
				metadata[k] = v /* 更新 metadata[k] 的值。 */
			} /* 结束当前表达式或代码块。 */
			metadata["protocolState"] = s.Context.State /* 执行当前语句并推进处理流程。 */
			input["metadata"] = metadata                /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if s.Want.Timestamp != 0 { /* 判断条件并选择处理分支。 */
			c["expectedTimestamp"] = s.Want.Timestamp /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if value, ok := s.Want.Event["type"]; ok { /* 判断条件并选择处理分支。 */
			c["expectedEventType"] = value /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		cases = append(cases, c) /* 更新 cases 的值。 */
	} /* 结束当前表达式或代码块。 */
	operations := []any{}            /* 更新 operations 的值。 */
	for _, s := range p.Operations { /* 循环处理当前数据。 */
		request := map[string]any{"operation": s.Operation, "deviceId": s.Context.DeviceID, "data": hex.EncodeToString(s.Data), "state": s.Context.State, "now": s.Context.Now} /* 更新 request 的值。 */
		if s.Operation == "encode" {                                                                                                                                            /* 判断条件并选择处理分支。 */
			request["command"] = platformCommand(s.Command) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		operations = append(operations, map[string]any{"name": s.Name, "request": request, "expected": platformFrame(s.Want)}) /* 更新 operations 的值。 */
	} /* 结束当前表达式或代码块。 */
	return map[string]any{"metadata": map[string]any{"name": p.Name, "version": p.Version, "runtime": "go-protocol-v2", "transport": transport, "payloadFormat": "hex", "capabilities": capabilities}, "cases": cases, "operations": operations}, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func platformHandle() (any, error) { /* 定义 platformHandle 函数。 */
	var q struct { /* 声明 q。 */
		Operation string         `json:"operation"` /* 执行当前语句并推进处理流程。 */
		DeviceID  string         `json:"deviceId"`  /* 执行当前语句并推进处理流程。 */
		Data      string         `json:"data"`      /* 执行当前语句并推进处理流程。 */
		State     map[string]any `json:"state"`     /* 执行当前语句并推进处理流程。 */
		Command   map[string]any `json:"command"`   /* 执行当前语句并推进处理流程。 */
		Now       int64          `json:"now"`       /* 执行当前语句并推进处理流程。 */
		Raw       *struct {      /* 执行当前语句并推进处理流程。 */
			Payload  json.RawMessage `json:"payload"`  /* 执行当前语句并推进处理流程。 */
			DeviceID string          `json:"deviceId"` /* 执行当前语句并推进处理流程。 */
			Metadata map[string]any  `json:"metadata"` /* 执行当前语句并推进处理流程。 */
		} `json:"raw"` /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if err := json.NewDecoder(os.Stdin).Decode(&q); err != nil { /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	p := Protocol()                /* 更新 p 的值。 */
	if q.Operation == "describe" { /* 判断条件并选择处理分支。 */
		return platformDescribe(p) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	context := Context{Now: q.Now, State: q.State, DeviceID: q.DeviceID} /* 更新 context 的值。 */
	if context.State == nil {                                            /* 判断条件并选择处理分支。 */
		context.State = map[string]any{} /* 更新 context.State 的值。 */
	} /* 结束当前表达式或代码块。 */
	switch q.Operation { /* 根据条件选择处理路径。 */
	case "decode": /* 处理当前分支。 */
		if q.Raw == nil || p.Decode == nil { /* 判断条件并选择处理分支。 */
			return nil, errors.New("缺少报文或 Decode 函数") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		var payload string                                              /* 声明 payload。 */
		if err := json.Unmarshal(q.Raw.Payload, &payload); err != nil { /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		data, err := hex.DecodeString(strings.Join(strings.Fields(payload), "")) /* 更新 err 的值。 */
		if err != nil {                                                          /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		context.DeviceID = q.Raw.DeviceID       /* 更新 context.DeviceID 的值。 */
		context.Metadata = q.Raw.Metadata       /* 更新 context.Metadata 的值。 */
		message, err := p.Decode(data, context) /* 更新 err 的值。 */
		if err != nil {                         /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return map[string]any{"standardMessage": message}, nil /* 返回当前处理结果。 */
	case "ingress": /* 处理当前分支。 */
		if p.Ingress == nil { /* 判断条件并选择处理分支。 */
			return nil, errors.New("未实现 Ingress") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		data, err := hex.DecodeString(q.Data) /* 更新 err 的值。 */
		if err != nil {                       /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		frame, err := p.Ingress(data, context) /* 更新 err 的值。 */
		if err != nil {                        /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return platformFrame(frame), nil /* 返回当前处理结果。 */
	case "encode": /* 处理当前分支。 */
		if p.Encode == nil { /* 判断条件并选择处理分支。 */
			return nil, errors.New("未实现 Encode") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		kind, _ := q.Command["type"].(string)                                   /* 更新 _ 的值。 */
		frame, err := p.Encode(Command{Type: kind, Params: q.Command}, context) /* 更新 err 的值。 */
		if err != nil {                                                         /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return platformFrame(frame), nil /* 返回当前处理结果。 */
	default: /* 处理当前分支。 */
		return nil, fmt.Errorf("未知操作 %q", q.Operation) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func main() { /* 定义 main 函数。 */
	output, err := platformHandle() /* 更新 err 的值。 */
	if err != nil {                 /* 判断条件并选择处理分支。 */
		output = map[string]string{"error": err.Error()} /* 更新 output 的值。 */
	} /* 结束当前表达式或代码块。 */
	if err := json.NewEncoder(os.Stdout).Encode(output); err != nil { /* 判断条件并选择处理分支。 */
		fmt.Fprintln(os.Stderr, err) /* 执行当前语句并推进处理流程。 */
		os.Exit(1)                   /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
