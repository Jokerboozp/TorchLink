package parser

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"iot-platform/internal/model"
	"time"
)

const StandardProtocolID = "iot-standard"
const StandardParserName = "iot_standard_parser"

// StandardParser keeps the original envelope in Raw; transport identity and
// message kind come only from the authenticated ingress route/topic.
type StandardParser struct{}

func (StandardParser) Name() string      { return StandardParserName }
func (StandardParser) Version() string   { return "1.0.0" }
func (StandardParser) Match(m Meta) bool { return m.Protocol == StandardProtocolID }
func (StandardParser) Parse(raw model.RawMessage) (*model.StandardMessage, error) {
	if len(raw.Payload) > 64<<10 {
		return nil, errors.New("payload exceeds 64 KiB")
	}
	if err := validateStandardJSON(raw.Payload); err != nil {
		return nil, err
	}
	var body struct {
		Version   string         `json:"version"`
		Event     string         `json:"event"`
		Online    *bool          `json:"online"`
		CommandID string         `json:"commandId"`
		Success   *bool          `json:"success"`
		ID        string         `json:"id"`
		Timestamp int64          `json:"timestamp"`
		Data      map[string]any `json:"data"`
	}
	if err := json.Unmarshal(raw.Payload, &body); err != nil {
		return nil, err
	}
	if body.Version != "" && body.Version != "1.0" {
		return nil, errors.New("unsupported standard message version")
	}
	if body.ID == "" || len(body.ID) > 128 || body.Timestamp <= 0 || body.Timestamp > 253402300799999 {
		return nil, errors.New("id and valid positive millisecond timestamp are required")
	}
	if raw.ReceivedAt > 0 && body.Timestamp > raw.ReceivedAt+int64(5*time.Minute/time.Millisecond) {
		return nil, errors.New("device timestamp is more than 5 minutes in the future")
	}
	if body.Data == nil {
		body.Data = map[string]any{}
	}
	switch raw.Headers["messageKind"] {
	case "state":
		if body.Online != nil {
			status := "DISCONNECTED"
			if *body.Online {
				status = "CONNECTED"
			}
			if old, ok := body.Data["connectionStatus"]; ok && old != status {
				return nil, errors.New("online conflicts with connectionStatus")
			}
			body.Data["connectionStatus"] = status
		}
	case "event":
		if body.Version == "1.0" && body.Event == "" {
			return nil, errors.New("event identifier is required")
		}
		if body.Event != "" {
			body.Data["event"] = body.Event
		}
	case "command-reply":
		if body.CommandID != "" {
			if old, ok := body.Data["commandId"]; ok && old != body.CommandID {
				return nil, errors.New("conflicting commandId")
			}
			body.Data["commandId"] = body.CommandID
		}
		if body.Success != nil {
			if old, ok := body.Data["success"]; ok && old != *body.Success {
				return nil, errors.New("conflicting success")
			}
			body.Data["success"] = *body.Success
		}
	}
	if len(body.Data) == 0 {
		return nil, errors.New("non-empty data object is required")
	}
	m := &model.StandardMessage{MessageID: "msg_" + raw.MessageID, RawMessageID: raw.MessageID, TenantID: raw.TenantID, ProductID: raw.ProductID, DeviceID: raw.DeviceID, Timestamp: body.Timestamp, Parser: StandardParserName, ParserVersion: "1.0.0"}
	switch raw.Headers["messageKind"] {
	case "property":
		m.MessageType = model.PropertyReport
		m.Properties = body.Data
	case "command-reply":
		id, ok := body.Data["commandId"].(string)
		if !ok || id == "" || len(id) > 128 {
			return nil, errors.New("commandId is required")
		}
		if _, ok := body.Data["success"].(bool); !ok {
			return nil, errors.New("success must be boolean")
		}
		m.MessageType = model.CommandReply
		m.Event = body.Data
	case "event":
		m.MessageType = model.EventReport
		m.Event = body.Data
	case "state":
		m.MessageType = model.StateChange
		m.Properties = body.Data
		if status, ok := body.Data["connectionStatus"]; ok && status != "CONNECTED" && status != "DISCONNECTED" && status != "UNKNOWN" {
			return nil, errors.New("connectionStatus must be CONNECTED, DISCONNECTED or UNKNOWN")
		}
	default:
		return nil, errors.New("message kind must be property, event, state or command-reply")
	}
	if err := json.Unmarshal(raw.Payload, &m.Raw); err != nil {
		return nil, err
	}
	m.Tags = map[string]string{"protocolId": raw.ProtocolID, "protocolVersion": raw.ProtocolVersion}
	return m, nil
}

// Reject ambiguous duplicate keys and cap nesting before materializing device data.
func validateStandardJSON(data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var value func(int) error
	value = func(depth int) error {
		if depth > 16 {
			return errors.New("JSON nesting exceeds 16 levels")
		}
		token, err := d.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			keys := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || keys[name] {
					return errors.New("duplicate or invalid JSON key")
				}
				keys[name] = true
				if err := value(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for d.More() {
				if err := value(depth + 1); err != nil {
					return err
				}
			}
		default:
			return errors.New("invalid JSON delimiter")
		}
		_, err = d.Token()
		return err
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("unexpected trailing JSON")
	}
	return nil
}
