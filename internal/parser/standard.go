package parser

import (
	"encoding/json"
	"errors"
	"iot-platform/internal/model"
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
	var body struct {
		ID        string         `json:"id"`
		Timestamp int64          `json:"timestamp"`
		Data      map[string]any `json:"data"`
	}
	if err := json.Unmarshal(raw.Payload, &body); err != nil {
		return nil, err
	}
	if body.ID == "" || len(body.ID) > 128 || body.Timestamp <= 0 || len(body.Data) == 0 {
		return nil, errors.New("id, positive timestamp and non-empty data object are required")
	}
	m := &model.StandardMessage{MessageID: "msg_" + raw.MessageID, RawMessageID: raw.MessageID, TenantID: raw.TenantID, ProductID: raw.ProductID, DeviceID: raw.DeviceID, Timestamp: body.Timestamp, Parser: StandardParserName, ParserVersion: "1.0.0"}
	switch raw.Headers["messageKind"] {
	case "property":
		m.MessageType = model.PropertyReport
		m.Properties = body.Data
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
		return nil, errors.New("message kind must be property, event or state")
	}
	if err := json.Unmarshal(raw.Payload, &m.Raw); err != nil {
		return nil, err
	}
	m.Tags = map[string]string{"protocolId": raw.ProtocolID, "protocolVersion": raw.ProtocolVersion}
	return m, nil
}
