package model

import "strings"

// UsesPlatformCredentials distinguishes authenticated HTTP/MQTT ingress from
// protocol connections. A device's explicit connector takes precedence over
// the product transport. Inventory without either uses the managed HTTP API.
func (d ManagedDevice) UsesPlatformCredentials(product Product) bool {
	if d.DeviceRole == "CHILD" || d.GatewayID != "" {
		return false
	}
	transport := d.Tags["connector"]
	if transport == "" {
		transport = product.Transport
	}
	switch strings.ToUpper(transport) {
	case "TCP", "UDP", "TCP_UDP", "TCP_CHILD", "MODBUS_TCP", "MODBUS_RTU_TCP", "MODBUS_RTU":
		return false
	default:
		return true
	}
}

// Public keeps the repository's unique internal key out of protocol-device UI
// and onboarding exports. The stored key is not a protocol authentication secret.
func (d ManagedDevice) Public(product Product) ManagedDevice {
	if !d.UsesPlatformCredentials(product) {
		d.AccessKey, d.SecretHash, d.SecretHint = "", "", ""
	}
	return d
}
