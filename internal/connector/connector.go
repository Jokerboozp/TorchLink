// Package connector defines the control plane contract, independent of HTTP.
package connector

import (
	"context"
	"iot-platform/internal/model"
)

type Type string

const (
	MQTT      Type = "MQTT"
	HTTP      Type = "HTTP"
	TCP       Type = "TCP"
	UDP       Type = "UDP"
	ModbusTCP Type = "MODBUS_TCP"
	Edge      Type = "EDGE"
)

type Request struct {
	Reuse   bool                      `json:"-"`
	Type    Type                      `json:"type"`
	Profile model.DeviceAccessProfile `json:"profile"`
	Release model.ProtocolRelease     `json:"release"`
	Raw     model.RawMessage          `json:"raw"`
}
type Result struct {
	TestToken        string                   `json:"testToken,omitempty"`
	ExceptionCode    int                      `json:"exceptionCode,omitempty"`
	Success          bool                     `json:"success"`
	Stage            string                   `json:"stage"`
	Message          string                   `json:"message"`
	LatencyMs        int64                    `json:"latencyMs"`
	ErrorCode        string                   `json:"errorCode"`
	DeviceID         string                   `json:"deviceId,omitempty"`
	RawRequest       string                   `json:"rawRequest,omitempty"`
	RawResponse      string                   `json:"rawResponse,omitempty"`
	Raw              []model.RawMessage       `json:"raw,omitempty"`
	Parsed           any                      `json:"parsed,omitempty"`
	StandardMessages []*model.StandardMessage `json:"standardMessages,omitempty"`
	ProtocolID       string                   `json:"protocolId"`
	ProtocolVersion  string                   `json:"protocolVersion"`
	Parser           string                   `json:"parser"`
	PointTable       any                      `json:"pointTable,omitempty"`
	Mapping          any                      `json:"mapping,omitempty"`
}
type Connector interface {
	Type() Type
	Test(context.Context, Request) (*Result, error)
}
type Adapter struct {
	Kind  Type
	Probe func(context.Context, Request) (*Result, error)
}

func (a Adapter) Type() Type                                           { return a.Kind }
func (a Adapter) Test(ctx context.Context, r Request) (*Result, error) { return a.Probe(ctx, r) }

// Instance is a projection of existing device/profile records, not a second
// runtime configuration or lifecycle owner.
type Instance struct {
	Type     Type                       `json:"type"`
	DeviceID string                     `json:"deviceId"`
	Profile  *model.DeviceAccessProfile `json:"profile,omitempty"`
}
