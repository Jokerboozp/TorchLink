package parser /* 声明 parser 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bytes"                       /* 执行当前语句并推进处理流程。 */
	"encoding/json"               /* 执行当前语句并推进处理流程。 */
	"errors"                      /* 执行当前语句并推进处理流程。 */
	"io"                          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"time"                        /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

const StandardProtocolID = "iot-standard"        /* 声明 StandardProtocolID。 */
const StandardParserName = "iot_standard_parser" /* 声明 StandardParserName。 */

// StandardParser keeps the original envelope in Raw; transport identity and
// message kind come only from the authenticated ingress route/topic.
type StandardParser struct{} /* 定义 StandardParser 类型。 */

func (StandardParser) Name() string      { return StandardParserName }               /* 定义 Name 函数。 */
func (StandardParser) Version() string   { return "1.0.0" }                          /* 定义 Version 函数。 */
func (StandardParser) Match(m Meta) bool { return m.Protocol == StandardProtocolID } /* 定义 Match 函数。 */
func (StandardParser) Parse(raw model.RawMessage) (*model.StandardMessage, error) { /* 定义 Parse 函数。 */
	if len(raw.Payload) > 64<<10 { /* 判断条件并选择处理分支。 */
		return nil, errors.New("payload exceeds 64 KiB") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err := validateStandardJSON(raw.Payload); err != nil { /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var body struct { /* 声明 body。 */
		Version   string         `json:"version"`   /* 执行当前语句并推进处理流程。 */
		Event     string         `json:"event"`     /* 执行当前语句并推进处理流程。 */
		Online    *bool          `json:"online"`    /* 执行当前语句并推进处理流程。 */
		CommandID string         `json:"commandId"` /* 执行当前语句并推进处理流程。 */
		Success   *bool          `json:"success"`   /* 执行当前语句并推进处理流程。 */
		ID        string         `json:"id"`        /* 执行当前语句并推进处理流程。 */
		Timestamp int64          `json:"timestamp"` /* 执行当前语句并推进处理流程。 */
		Data      map[string]any `json:"data"`      /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if err := json.Unmarshal(raw.Payload, &body); err != nil { /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if body.Version != "" && body.Version != "1.0" { /* 判断条件并选择处理分支。 */
		return nil, errors.New("unsupported standard message version") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if body.ID == "" || len(body.ID) > 128 || body.Timestamp <= 0 || body.Timestamp > 253402300799999 { /* 判断条件并选择处理分支。 */
		return nil, errors.New("id and valid positive millisecond timestamp are required") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if raw.ReceivedAt > 0 && body.Timestamp > raw.ReceivedAt+int64(5*time.Minute/time.Millisecond) { /* 判断条件并选择处理分支。 */
		return nil, errors.New("device timestamp is more than 5 minutes in the future") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if body.Data == nil { /* 判断条件并选择处理分支。 */
		body.Data = map[string]any{} /* 更新 body.Data 的值。 */
	} /* 结束当前表达式或代码块。 */
	switch raw.Headers["messageKind"] { /* 根据条件选择处理路径。 */
	case "state": /* 处理当前分支。 */
		if body.Online != nil { /* 判断条件并选择处理分支。 */
			status := "DISCONNECTED" /* 更新 status 的值。 */
			if *body.Online {        /* 判断条件并选择处理分支。 */
				status = "CONNECTED" /* 更新 status 的值。 */
			} /* 结束当前表达式或代码块。 */
			if old, ok := body.Data["connectionStatus"]; ok && old != status { /* 判断条件并选择处理分支。 */
				return nil, errors.New("online conflicts with connectionStatus") /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			body.Data["connectionStatus"] = status /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	case "event": /* 处理当前分支。 */
		if body.Version == "1.0" && body.Event == "" { /* 判断条件并选择处理分支。 */
			return nil, errors.New("event identifier is required") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if body.Event != "" { /* 判断条件并选择处理分支。 */
			body.Data["event"] = body.Event /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	case "command-reply": /* 处理当前分支。 */
		if body.CommandID != "" { /* 判断条件并选择处理分支。 */
			if old, ok := body.Data["commandId"]; ok && old != body.CommandID { /* 判断条件并选择处理分支。 */
				return nil, errors.New("conflicting commandId") /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			body.Data["commandId"] = body.CommandID /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if body.Success != nil { /* 判断条件并选择处理分支。 */
			if old, ok := body.Data["success"]; ok && old != *body.Success { /* 判断条件并选择处理分支。 */
				return nil, errors.New("conflicting success") /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			body.Data["success"] = *body.Success /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if len(body.Data) == 0 { /* 判断条件并选择处理分支。 */
		return nil, errors.New("non-empty data object is required") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	m := &model.StandardMessage{MessageID: "msg_" + raw.MessageID, RawMessageID: raw.MessageID, TenantID: raw.TenantID, ProductID: raw.ProductID, DeviceID: raw.DeviceID, Timestamp: body.Timestamp, Parser: StandardParserName, ParserVersion: "1.0.0"} /* 更新 m 的值。 */
	switch raw.Headers["messageKind"] {                                                                                                                                                                                                                  /* 根据条件选择处理路径。 */
	case "property": /* 处理当前分支。 */
		m.MessageType = model.PropertyReport /* 更新 m.MessageType 的值。 */
		m.Properties = body.Data             /* 更新 m.Properties 的值。 */
	case "command-reply": /* 处理当前分支。 */
		id, ok := body.Data["commandId"].(string) /* 更新 ok 的值。 */
		if !ok || id == "" || len(id) > 128 {     /* 判断条件并选择处理分支。 */
			return nil, errors.New("commandId is required") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if _, ok := body.Data["success"].(bool); !ok { /* 判断条件并选择处理分支。 */
			return nil, errors.New("success must be boolean") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		m.MessageType = model.CommandReply /* 更新 m.MessageType 的值。 */
		m.Event = body.Data                /* 更新 m.Event 的值。 */
	case "event": /* 处理当前分支。 */
		m.MessageType = model.EventReport /* 更新 m.MessageType 的值。 */
		m.Event = body.Data               /* 更新 m.Event 的值。 */
	case "state": /* 处理当前分支。 */
		m.MessageType = model.StateChange                                                                                                /* 更新 m.MessageType 的值。 */
		m.Properties = body.Data                                                                                                         /* 更新 m.Properties 的值。 */
		if status, ok := body.Data["connectionStatus"]; ok && status != "CONNECTED" && status != "DISCONNECTED" && status != "UNKNOWN" { /* 判断条件并选择处理分支。 */
			return nil, errors.New("connectionStatus must be CONNECTED, DISCONNECTED or UNKNOWN") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	default: /* 处理当前分支。 */
		return nil, errors.New("message kind must be property, event, state or command-reply") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err := json.Unmarshal(raw.Payload, &m.Raw); err != nil { /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	m.Tags = map[string]string{"protocolId": raw.ProtocolID, "protocolVersion": raw.ProtocolVersion} /* 更新 m.Tags 的值。 */
	if _, err := model.MessageComponents(*m); err != nil {                                           /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return m, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// Reject ambiguous duplicate keys and cap nesting before materializing device data.
func validateStandardJSON(data []byte) error { /* 定义 validateStandardJSON 函数。 */
	d := json.NewDecoder(bytes.NewReader(data)) /* 更新 d 的值。 */
	d.UseNumber()                               /* 执行当前语句并推进处理流程。 */
	var value func(int) error                   /* 声明 value。 */
	value = func(depth int) error {             /* 更新 value 的值。 */
		if depth > 16 { /* 判断条件并选择处理分支。 */
			return errors.New("JSON nesting exceeds 16 levels") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		token, err := d.Token() /* 更新 err 的值。 */
		if err != nil {         /* 判断条件并选择处理分支。 */
			return err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		delim, ok := token.(json.Delim) /* 更新 ok 的值。 */
		if !ok {                        /* 判断条件并选择处理分支。 */
			return nil /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		switch delim { /* 根据条件选择处理路径。 */
		case '{': /* 处理当前分支。 */
			keys := map[string]bool{} /* 更新 keys 的值。 */
			for d.More() {            /* 循环处理当前数据。 */
				key, err := d.Token() /* 更新 err 的值。 */
				if err != nil {       /* 判断条件并选择处理分支。 */
					return err /* 返回当前处理结果。 */
				} /* 结束当前表达式或代码块。 */
				name, ok := key.(string) /* 更新 ok 的值。 */
				if !ok || keys[name] {   /* 判断条件并选择处理分支。 */
					return errors.New("duplicate or invalid JSON key") /* 返回当前处理结果。 */
				} /* 结束当前表达式或代码块。 */
				keys[name] = true                        /* 更新 keys[name] 的值。 */
				if err := value(depth + 1); err != nil { /* 判断条件并选择处理分支。 */
					return err /* 返回当前处理结果。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
		case '[': /* 处理当前分支。 */
			for d.More() { /* 循环处理当前数据。 */
				if err := value(depth + 1); err != nil { /* 判断条件并选择处理分支。 */
					return err /* 返回当前处理结果。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
		default: /* 处理当前分支。 */
			return errors.New("invalid JSON delimiter") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		_, err = d.Token() /* 更新 err 的值。 */
		return err         /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err := value(0); err != nil { /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if _, err := d.Token(); err != io.EOF { /* 判断条件并选择处理分支。 */
		return errors.New("unexpected trailing JSON") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
