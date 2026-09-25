// Package connector describes the device connection types the platform offers.
package connector

type Type string

const (
	MQTT         Type = "MQTT"
	HTTP         Type = "HTTP"
	TCP          Type = "TCP"
	UDP          Type = "UDP"
	ModbusTCP    Type = "MODBUS_TCP"
	ModbusRTUTCP Type = "MODBUS_RTU_TCP"
)

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

// Types describes transport potential; the device endpoint must still check the
// active protocol, publisher, session and user permissions.
func Types() []Description {
	return []Description{
		{MQTT, "MQTT 标准设备", true, Capabilities{Preview: true, DeviceCredential: true, Command: true}},
		{HTTP, "HTTP 标准上报", true, Capabilities{Preview: true, DeviceCredential: true}},
		{ModbusTCP, "Modbus TCP", true, Capabilities{Preview: true, ReadOnce: true}},
		{ModbusRTUTCP, "Modbus RTU 串口透传 TCP", true, Capabilities{Preview: true, ReadOnce: true}},
		{TCP, "TCP 设备", true, Capabilities{Preview: true, Listener: true, Command: true}},
		{UDP, "UDP 设备", true, Capabilities{Preview: true, Listener: true, Command: true}},
	}
}
