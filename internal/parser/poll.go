package parser

import (
	"encoding/json"
	"errors"
	"fmt"
	"iot-platform/internal/model"
	"math"
	"strings"
)

const PollResponseParserName = "protocol_read_response_v1"

type PollResponseParser struct{}

func (PollResponseParser) Name() string    { return PollResponseParserName }
func (PollResponseParser) Version() string { return "1.0.0" }
func (PollResponseParser) Match(Meta) bool { return false }
func (PollResponseParser) Parse(model.RawMessage) (*model.StandardMessage, error) {
	return nil, errors.New("protocol read response requires versioned point mapping")
}
func (PollResponseParser) ParseWithConfig(raw model.RawMessage, config map[string]any) (*model.StandardMessage, error) {
	var response model.PollResponse
	if err := json.Unmarshal(raw.Payload, &response); err != nil {
		return nil, err
	}
	if response.Transport != raw.Transport || (raw.Transport != "OPC_UA" && raw.Transport != "SNMP" && raw.Transport != "BACNET" && raw.Transport != "ONVIF") {
		return nil, errors.New("protocol response transport mismatch")
	}
	points, err := PollPoints(config)
	if err != nil {
		return nil, err
	}
	properties := map[string]any{}
	for _, p := range points {
		found := false
		for _, value := range response.Values {
			if value.Address != p.Address {
				continue
			}
			if found {
				return nil, errors.New("duplicate protocol response address")
			}
			found = true
			if value.Quality != "GOOD" {
				return nil, fmt.Errorf("point %s quality: %s", p.Identifier, value.Quality)
			}
			v := value.Value
			if number, ok := v.(float64); ok {
				scale := p.Scale
				if scale == 0 {
					scale = 1
				}
				number = number*scale + p.Offset
				if math.IsNaN(number) || math.IsInf(number, 0) {
					return nil, errors.New("invalid numeric response")
				}
				v = number
			}
			properties[p.Identifier] = v
		}
		if !found {
			return nil, fmt.Errorf("point %s missing from response", p.Identifier)
		}
	}
	return &model.StandardMessage{MessageID: "msg_" + strings.TrimPrefix(raw.MessageID, "raw_"), RawMessageID: raw.MessageID, TenantID: raw.TenantID, ProductID: raw.ProductID, DeviceID: raw.DeviceID, MessageType: model.PropertyReport, Timestamp: raw.ReceivedAt, Properties: properties, Tags: map[string]string{"protocolId": raw.ProtocolID, "protocolVersion": raw.ProtocolVersion}, Raw: map[string]any{"response": response}}, nil
}
func PollPoints(config map[string]any) ([]model.PollPoint, error) {
	b, err := json.Marshal(config["reads"])
	if err != nil {
		return nil, err
	}
	var points []model.PollPoint
	if err = json.Unmarshal(b, &points); err != nil {
		return nil, err
	}
	if len(points) < 1 || len(points) > 64 {
		return nil, errors.New("read mapping requires 1 to 64 points")
	}
	seen := map[string]bool{}
	addresses := map[string]bool{}
	for _, p := range points {
		if strings.TrimSpace(p.Identifier) == "" || len(p.Identifier) > 128 || strings.TrimSpace(p.Address) == "" || len(p.Address) > 512 || seen[p.Identifier] || addresses[p.Address] {
			return nil, errors.New("invalid or duplicate read point")
		}
		seen[p.Identifier] = true
		addresses[p.Address] = true
	}
	return points, nil
}
