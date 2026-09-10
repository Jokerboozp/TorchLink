package model

import (
	"encoding/json"
	"errors"
)

// ChildProductBinding maps a protocol's device type to a preconfigured product.
// Protocol versions remain owned by ProductProtocolBinding.
type ChildProductBinding struct {
	Type      string `json:"type"`
	ProductID string `json:"productId"`
}

type ChildIdentity struct {
	Address string `json:"address"`
	Type    string `json:"type"`
	Name    string `json:"name,omitempty"`
}

func ProtocolChildDevice(expected, current DeviceAccessProfile, parent ManagedDevice, product Product, identity ChildIdentity, now int64) (ManagedDevice, error) {
	if expected.Configuration() != current.Configuration() || !current.Enabled || current.EdgeNodeID != "" || current.Mode != "listener" || parent.TenantID != current.TenantID || parent.ProductID != current.ProductID || parent.Status != "ENABLED" || parent.DeviceRole == "CHILD" || (current.DeviceID != "" && parent.ID != current.DeviceID) || !ValidProtocolDeviceID(identity.Address) || len(identity.Name) > 256 || product.TenantID != current.TenantID || product.Status != "ENABLED" {
		return ManagedDevice{}, ErrProtocolRegistration
	}
	allowed := false
	for _, v := range current.ChildProducts {
		if v.Type == identity.Type && v.ProductID == product.ID {
			allowed = true
		}
	}
	if !allowed {
		return ManagedDevice{}, ErrProtocolRegistration
	}
	id := ChildDeviceID(current.TenantID, parent.ID, identity.Address)
	name := identity.Name
	if name == "" {
		name = identity.Address
	}
	return ManagedDevice{ID: id, TenantID: current.TenantID, ProductID: product.ID, Name: name, Status: "ENABLED", DeviceRole: "CHILD", GatewayID: parent.ID, RegistrationSource: "PROTOCOL_CHILD_AUTO", AutoRegistered: true, AccessKey: ProtocolDeviceAccessKey(current.TenantID, id), CreatedAt: now, UpdatedAt: now, Tags: map[string]string{"connector": "TCP_CHILD", "connectorProfileId": current.ID, "childAddress": identity.Address, "childType": identity.Type}}, nil
}

func ExistingProtocolChild(old, next ManagedDevice) error {
	if old.TenantID != next.TenantID || old.ProductID != next.ProductID || old.GatewayID != next.GatewayID || old.DeviceRole != "CHILD" || old.Status != "ENABLED" || old.Tags["childAddress"] != next.Tags["childAddress"] || old.Tags["childType"] != next.Tags["childType"] {
		return ErrProtocolRegistration
	}
	return nil
}

type ProtocolQuery struct {
	Type        string         `json:"type"`
	IntervalSec int            `json:"intervalSec"`
	Params      map[string]any `json:"params,omitempty"`
}

func ValidateProtocolAccess(p DeviceAccessProfile) error {
	if p.ConnectionMode != "" && p.ConnectionMode != "listen" && p.ConnectionMode != "dial" {
		return errors.New("连接方向须为 listen 或 dial")
	}
	if p.ConnectionMode == "dial" && (p.Mode != "listener" || p.Network != "tcp" || p.DeviceID == "" || p.Host == "") {
		return errors.New("主动 TCP 连接需要设备标识与目标地址")
	}
	if p.WireFormat != "" && p.WireFormat != "modbus_tcp" && p.WireFormat != "rtu_over_tcp" {
		return errors.New("不支持的 Modbus 报文格式")
	}
	if p.WireFormat != "" && p.Mode != "poll" {
		return errors.New("Modbus 报文格式仅用于轮询实例")
	}
	if len(p.Queries) > 32 || len(p.ChildProducts) > 64 {
		return errors.New("查询或子设备产品数量超限")
	}
	if (len(p.Queries) > 0 || len(p.ChildProducts) > 0) && p.Mode != "listener" {
		return errors.New("查询与子设备映射需要 Go 协议接入实例")
	}
	if len(p.Queries) > 0 && p.Network != "tcp" {
		return errors.New("定时查询仅支持 TCP 接入实例")
	}
	seen := map[string]bool{}
	for _, q := range p.Queries {
		if !ValidProtocolDeviceID(q.Type) || q.IntervalSec < 1 || q.IntervalSec > 86400 || seen[q.Type] {
			return errors.New("查询类型须唯一，周期须为 1 至 86400 秒")
		}
		seen[q.Type] = true
		b, e := json.Marshal(q.Params)
		if e != nil || len(b) > 4096 {
			return errors.New("查询参数无效或超过 4 KiB")
		}
	}
	seen = map[string]bool{}
	for _, c := range p.ChildProducts {
		if !ValidProtocolDeviceID(c.Type) || !ValidProtocolDeviceID(c.ProductID) || c.ProductID == p.ProductID || seen[c.Type] {
			return errors.New("子设备类型须唯一并关联不同的有效产品")
		}
		seen[c.Type] = true
	}
	return nil
}

func ChildDeviceID(tenant, parent, address string) string {
	return "child_" + ProtocolDeviceAccessKey(tenant, parent+"\x00"+address)[9:]
}
