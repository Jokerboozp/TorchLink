// Package connector defines the control plane contract, independent of HTTP.
package connector

import (
	"context"
	"errors"
	"iot-platform/internal/model"
)

type Type string

const (
	MQTT      Type = "MQTT"
	HTTP      Type = "HTTP"
	TCP       Type = "TCP"
	UDP       Type = "UDP"
	ModbusTCP Type = "MODBUS_TCP"
	ModbusRTU Type = "MODBUS_RTU"
	OPCUA     Type = "OPC_UA"
	SNMP      Type = "SNMP"
	BACnet    Type = "BACNET"
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
	Source           string                   `json:"source"`
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
	Capabilities() Capabilities
	Test(context.Context, Request) (*Result, error)
}
type Adapter struct {
	Kind  Type
	Probe func(context.Context, Request) (*Result, error)
}

func (a Adapter) Type() Type                 { return a.Kind }
func (a Adapter) Capabilities() Capabilities { return Describe(a.Kind).Capabilities }
func (a Adapter) Test(ctx context.Context, r Request) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !Describe(a.Kind).Supported || a.Probe == nil {
		return &Result{Stage: "unsupported", ErrorCode: "UNSUPPORTED", Message: ErrUnsupported.Error()}, ErrUnsupported
	}
	return a.Probe(ctx, r)
}

var ErrUnsupported = errors.New("connector capability is unsupported")

type Capabilities struct {
	Preview          bool `json:"preview"`
	ReadOnce         bool `json:"readOnce"`
	Listener         bool `json:"listener"`
	DeviceCredential bool `json:"deviceCredential"`
	Command          bool `json:"command"`
}
type Description struct {
	Type         Type         `json:"type"`
	Name         string       `json:"name"`
	Supported    bool         `json:"supported"`
	Capabilities Capabilities `json:"capabilities"`
}

func Types() []Description {
	return []Description{
		{MQTT, "MQTT 标准设备", true, Capabilities{Preview: true, DeviceCredential: true, Command: true}},
		{HTTP, "HTTP 标准上报", true, Capabilities{Preview: true, DeviceCredential: true}},
		{ModbusTCP, "Modbus TCP", true, Capabilities{Preview: true, ReadOnce: true}},
		{ModbusRTU, "Modbus RTU / RS485（现场节点）", true, Capabilities{Preview: true, ReadOnce: true}},
		{OPCUA, "OPC UA", true, Capabilities{Preview: true, ReadOnce: true}},
		{SNMP, "SNMP", true, Capabilities{Preview: true, ReadOnce: true}},
		{BACnet, "BACnet/IP", true, Capabilities{Preview: true, ReadOnce: true}},
		{TCP, "TCP 设备", true, Capabilities{Preview: true, Listener: true, Command: true}},
		{UDP, "UDP 设备", true, Capabilities{Preview: true, Listener: true, Command: true}},
		{Edge, "Edge Agent（规划中）", false, Capabilities{}},
	}
}

// Command describes the transport potential; the device endpoint must still
// check the active protocol, publisher, session, and user permissions.
func Describe(kind Type) Description {
	for _, d := range Types() {
		if d.Type == kind {
			return d
		}
	}
	return Description{Type: kind}
}

// Instance is a projection of existing device/profile records, not a second
// runtime configuration or lifecycle owner.
type Instance struct {
	Type     Type                       `json:"type"`
	DeviceID string                     `json:"deviceId"`
	Profile  *model.DeviceAccessProfile `json:"profile,omitempty"`
}
