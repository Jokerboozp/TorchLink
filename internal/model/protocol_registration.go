package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"unicode"
)

var ErrProtocolRegistration = errors.New("protocol registration is disabled, stale or conflicts with existing inventory")

func ValidProtocolDeviceID(id string) bool {
	if len(id) == 0 || len(id) > 128 || strings.TrimSpace(id) != id {
		return false
	}
	for _, c := range id {
		if unicode.IsSpace(c) || unicode.IsControl(c) || c == '/' || c == '\\' {
			return false
		}
	}
	return true
}

func ProtocolDeviceAccessKey(tenant, id string) string {
	hash := sha256.Sum256([]byte(tenant + "\x00" + id))
	return "protocol_" + hex.EncodeToString(hash[:])
}

func ProtocolRegistrationDevice(expected, current DeviceAccessProfile, product Product, id, name string, now int64) (ManagedDevice, error) {
	if expected.Configuration() != current.Configuration() || !current.Enabled || !current.AutoRegister || current.Mode != "listener" || (current.Network != "tcp" && current.Network != "udp") || current.TenantID == "" || current.ID == "" || !ValidProtocolDeviceID(id) || (current.DeviceID != "" && current.DeviceID != id) || product.TenantID != current.TenantID || product.ID != current.ProductID || product.Status != "ENABLED" || len(name) > 256 {
		return ManagedDevice{}, ErrProtocolRegistration
	}
	if name == "" {
		name = id
	}
	return ManagedDevice{ID: id, TenantID: current.TenantID, ProductID: current.ProductID, Name: name, Status: "ENABLED", DeviceRole: "DIRECT", RegistrationSource: "PROTOCOL_AUTO", AutoRegistered: true, CreatedAt: now, UpdatedAt: now, AccessKey: ProtocolDeviceAccessKey(current.TenantID, id), Tags: map[string]string{"connector": strings.ToUpper(current.Network), "connectorProfileId": current.ID}}, nil
}

func RegisteredProtocolDevice(existing ManagedDevice, profile DeviceAccessProfile) error {
	if existing.TenantID != profile.TenantID || existing.ProductID != profile.ProductID || existing.Status != "ENABLED" {
		return ErrProtocolRegistration
	}
	return nil
}
