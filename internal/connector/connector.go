// Package connector defines the control plane contract, independent of HTTP.
package connector /* 声明 connector 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                     /* 执行当前语句并推进处理流程。 */
	"errors"                      /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type Type string /* 定义 Type 类型。 */

const ( /* 执行当前语句并推进处理流程。 */
	MQTT         Type = "MQTT"           /* 执行当前语句并推进处理流程。 */
	HTTP         Type = "HTTP"           /* 执行当前语句并推进处理流程。 */
	TCP          Type = "TCP"            /* 执行当前语句并推进处理流程。 */
	UDP          Type = "UDP"            /* 执行当前语句并推进处理流程。 */
	ModbusTCP    Type = "MODBUS_TCP"     /* 执行当前语句并推进处理流程。 */
	ModbusRTUTCP Type = "MODBUS_RTU_TCP" /* 执行当前语句并推进处理流程。 */
	ModbusRTU    Type = "MODBUS_RTU"     /* 执行当前语句并推进处理流程。 */
	OPCUA        Type = "OPC_UA"         /* 执行当前语句并推进处理流程。 */
	SNMP         Type = "SNMP"           /* 执行当前语句并推进处理流程。 */
	BACnet       Type = "BACNET"         /* 执行当前语句并推进处理流程。 */
	Edge         Type = "EDGE"           /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type Request struct { /* 定义 Request 类型。 */
	Reuse   bool                      `json:"-"`       /* 执行当前语句并推进处理流程。 */
	Type    Type                      `json:"type"`    /* 执行当前语句并推进处理流程。 */
	Profile model.DeviceAccessProfile `json:"profile"` /* 执行当前语句并推进处理流程。 */
	Release model.ProtocolRelease     `json:"release"` /* 执行当前语句并推进处理流程。 */
	Raw     model.RawMessage          `json:"raw"`     /* 执行当前语句并推进处理流程。 */
}                    /* 结束当前表达式或代码块。 */
type Result struct { /* 定义 Result 类型。 */
	Source           string                   `json:"source"`                     /* 执行当前语句并推进处理流程。 */
	TestToken        string                   `json:"testToken,omitempty"`        /* 执行当前语句并推进处理流程。 */
	ExceptionCode    int                      `json:"exceptionCode,omitempty"`    /* 执行当前语句并推进处理流程。 */
	Success          bool                     `json:"success"`                    /* 执行当前语句并推进处理流程。 */
	Stage            string                   `json:"stage"`                      /* 执行当前语句并推进处理流程。 */
	Message          string                   `json:"message"`                    /* 执行当前语句并推进处理流程。 */
	LatencyMs        int64                    `json:"latencyMs"`                  /* 执行当前语句并推进处理流程。 */
	ErrorCode        string                   `json:"errorCode"`                  /* 执行当前语句并推进处理流程。 */
	DeviceID         string                   `json:"deviceId,omitempty"`         /* 执行当前语句并推进处理流程。 */
	RawRequest       string                   `json:"rawRequest,omitempty"`       /* 执行当前语句并推进处理流程。 */
	RawResponse      string                   `json:"rawResponse,omitempty"`      /* 执行当前语句并推进处理流程。 */
	Raw              []model.RawMessage       `json:"raw,omitempty"`              /* 执行当前语句并推进处理流程。 */
	Parsed           any                      `json:"parsed,omitempty"`           /* 执行当前语句并推进处理流程。 */
	StandardMessages []*model.StandardMessage `json:"standardMessages,omitempty"` /* 执行当前语句并推进处理流程。 */
	ProtocolID       string                   `json:"protocolId"`                 /* 执行当前语句并推进处理流程。 */
	ProtocolVersion  string                   `json:"protocolVersion"`            /* 执行当前语句并推进处理流程。 */
	Parser           string                   `json:"parser"`                     /* 执行当前语句并推进处理流程。 */
	PointTable       any                      `json:"pointTable,omitempty"`       /* 执行当前语句并推进处理流程。 */
	Mapping          any                      `json:"mapping,omitempty"`          /* 执行当前语句并推进处理流程。 */
}                          /* 结束当前表达式或代码块。 */
type Connector interface { /* 定义 Connector 类型。 */
	Type() Type                                     /* 执行当前语句并推进处理流程。 */
	Capabilities() Capabilities                     /* 执行当前语句并推进处理流程。 */
	Test(context.Context, Request) (*Result, error) /* 执行当前语句并推进处理流程。 */
}                     /* 结束当前表达式或代码块。 */
type Adapter struct { /* 定义 Adapter 类型。 */
	Kind  Type                                            /* 执行当前语句并推进处理流程。 */
	Probe func(context.Context, Request) (*Result, error) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (a Adapter) Type() Type                 { return a.Kind }                        /* 定义 Type 函数。 */
func (a Adapter) Capabilities() Capabilities { return Describe(a.Kind).Capabilities } /* 定义 Capabilities 函数。 */
func (a Adapter) Test(ctx context.Context, r Request) (*Result, error) { /* 定义 Test 函数。 */
	if err := ctx.Err(); err != nil { /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !Describe(a.Kind).Supported || a.Probe == nil { /* 判断条件并选择处理分支。 */
		return &Result{Stage: "unsupported", ErrorCode: "UNSUPPORTED", Message: ErrUnsupported.Error()}, ErrUnsupported /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return a.Probe(ctx, r) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

var ErrUnsupported = errors.New("connector capability is unsupported") /* 声明 ErrUnsupported。 */

type Capabilities struct { /* 定义 Capabilities 类型。 */
	Preview          bool `json:"preview"`          /* 执行当前语句并推进处理流程。 */
	ReadOnce         bool `json:"readOnce"`         /* 执行当前语句并推进处理流程。 */
	Listener         bool `json:"listener"`         /* 执行当前语句并推进处理流程。 */
	DeviceCredential bool `json:"deviceCredential"` /* 执行当前语句并推进处理流程。 */
	Command          bool `json:"command"`          /* 执行当前语句并推进处理流程。 */
}                         /* 结束当前表达式或代码块。 */
type Description struct { /* 定义 Description 类型。 */
	Type         Type         `json:"type"`         /* 执行当前语句并推进处理流程。 */
	Name         string       `json:"name"`         /* 执行当前语句并推进处理流程。 */
	Supported    bool         `json:"supported"`    /* 执行当前语句并推进处理流程。 */
	Capabilities Capabilities `json:"capabilities"` /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func Types() []Description { /* 定义 Types 函数。 */
	return []Description{ /* 返回当前处理结果。 */
		{MQTT, "MQTT 标准设备", true, Capabilities{Preview: true, DeviceCredential: true, Command: true}}, /* 执行当前语句并推进处理流程。 */
		{HTTP, "HTTP 标准上报", true, Capabilities{Preview: true, DeviceCredential: true}},                /* 执行当前语句并推进处理流程。 */
		{ModbusTCP, "Modbus TCP", true, Capabilities{Preview: true, ReadOnce: true}},                  /* 执行当前语句并推进处理流程。 */
		{ModbusRTUTCP, "Modbus RTU 串口透传 TCP", true, Capabilities{Preview: true, ReadOnce: true}},      /* 执行当前语句并推进处理流程。 */
		{TCP, "TCP 设备", true, Capabilities{Preview: true, Listener: true, Command: true}},             /* 执行当前语句并推进处理流程。 */
		{UDP, "UDP 设备", true, Capabilities{Preview: true, Listener: true, Command: true}},             /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

// Command describes the transport potential; the device endpoint must still
// check the active protocol, publisher, session, and user permissions.
func Describe(kind Type) Description { /* 定义 Describe 函数。 */
	for _, d := range Types() { /* 循环处理当前数据。 */
		if d.Type == kind { /* 判断条件并选择处理分支。 */
			return d /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return Description{Type: kind} /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// Instance is a projection of existing device/profile records, not a second
// runtime configuration or lifecycle owner.
type Instance struct { /* 定义 Instance 类型。 */
	Type     Type                       `json:"type"`              /* 执行当前语句并推进处理流程。 */
	DeviceID string                     `json:"deviceId"`          /* 执行当前语句并推进处理流程。 */
	Profile  *model.DeviceAccessProfile `json:"profile,omitempty"` /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
