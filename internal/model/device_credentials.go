package model

import (
	"encoding/json"
	"strings"
)

// UsesPlatformCredentials distinguishes authenticated HTTP/MQTT ingress from
// protocol connections. A device's explicit connector takes precedence over
// the product transport. Inventory without either uses the managed HTTP API.
func (d ManagedDevice) UsesPlatformCredentials(product Product) bool {
	if d.DeviceRole == "CHILD" || d.GatewayID != "" {
		return false
	}
	transport := d.Connector
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
	d.OnboardingRequestHash = ""
	return d
}

// systemTagKeys were stored in Tags before the fields became structured. They
// cannot be used as user labels.
var systemTagKeys = []string{"connector", "connectorProfileId", "childAddress", "childType", "onboardingRequestHash"}

// SystemTag reports whether a label key is reserved for a platform field.
func SystemTag(key string) bool {
	for _, v := range systemTagKeys {
		if v == key {
			return true
		}
	}
	return false
}

// UnmarshalJSON also reads bodies written before the structured fields existed,
// such as unmigrated rows, caches and backups, and removes the old keys from Tags.
func (d *ManagedDevice) UnmarshalJSON(data []byte) error {
	type plain ManagedDevice
	if err := json.Unmarshal(data, (*plain)(d)); err != nil {
		return err
	}
	for key, field := range map[string]*string{"connector": &d.Connector, "connectorProfileId": &d.ConnectorProfileID, "childAddress": &d.ChildAddress, "childType": &d.ChildType, "onboardingRequestHash": &d.OnboardingRequestHash} {
		if value, ok := d.Tags[key]; ok {
			if *field == "" {
				*field = value
			}
			delete(d.Tags, key)
		}
	}
	return nil
}
