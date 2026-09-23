package model /* 声明 model 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"errors"        /* 执行当前语句并推进处理流程。 */
	"strings"
) /* 结束当前表达式或代码块。 */

// ChildProductBinding maps a protocol's device type to a preconfigured product.
// Protocol versions remain owned by ProductProtocolBinding.
type ChildProductBinding struct { /* 定义 ChildProductBinding 类型。 */
	Type      string `json:"type"`      /* 执行当前语句并推进处理流程。 */
	ProductID string `json:"productId"` /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type ChildIdentity struct { /* 定义 ChildIdentity 类型。 */
	Address string `json:"address"`        /* 执行当前语句并推进处理流程。 */
	Type    string `json:"type"`           /* 执行当前语句并推进处理流程。 */
	Name    string `json:"name,omitempty"` /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func ProtocolChildDevice(expected, current DeviceAccessProfile, parent ManagedDevice, product Product, identity ChildIdentity, now int64) (ManagedDevice, error) { /* 定义 ProtocolChildDevice 函数。 */
	if expected.Configuration() != current.Configuration() || !current.Enabled || current.EdgeNodeID != "" || current.Mode != "listener" || parent.TenantID != current.TenantID || parent.ProductID != current.ProductID || parent.Status != "ENABLED" || parent.DeviceRole == "CHILD" || (current.DeviceID != "" && parent.ID != current.DeviceID) || !ValidProtocolDeviceID(identity.Address) || len(identity.Name) > 256 || product.TenantID != current.TenantID || product.Status != "ENABLED" { /* 判断条件并选择处理分支。 */
		return ManagedDevice{}, ErrProtocolRegistration /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	allowed := false                          /* 更新 allowed 的值。 */
	for _, v := range current.ChildProducts { /* 循环处理当前数据。 */
		if v.Type == identity.Type && v.ProductID == product.ID { /* 判断条件并选择处理分支。 */
			allowed = true /* 更新 allowed 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if !allowed { /* 判断条件并选择处理分支。 */
		return ManagedDevice{}, ErrProtocolRegistration /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	id := ChildDeviceID(current.TenantID, parent.ID, identity.Address) /* 更新 id 的值。 */
	name := identity.Name                                              /* 更新 name 的值。 */
	if name == "" {                                                    /* 判断条件并选择处理分支。 */
		name = identity.Address /* 更新 name 的值。 */
	} /* 结束当前表达式或代码块。 */
	return ManagedDevice{ID: id, TenantID: current.TenantID, ProductID: product.ID, Name: name, Status: "ENABLED", DeviceRole: "CHILD", GatewayID: parent.ID, RegistrationSource: "PROTOCOL_CHILD_AUTO", AutoRegistered: true, AccessKey: ProtocolDeviceAccessKey(current.TenantID, id), CreatedAt: now, UpdatedAt: now, Tags: map[string]string{"connector": "TCP_CHILD", "connectorProfileId": current.ID, "childAddress": identity.Address, "childType": identity.Type}}, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func ExistingProtocolChild(old, next ManagedDevice) error { /* 定义 ExistingProtocolChild 函数。 */
	if old.TenantID != next.TenantID || old.ProductID != next.ProductID || old.GatewayID != next.GatewayID || old.DeviceRole != "CHILD" || old.Status != "ENABLED" || old.Tags["childAddress"] != next.Tags["childAddress"] || old.Tags["childType"] != next.Tags["childType"] { /* 判断条件并选择处理分支。 */
		return ErrProtocolRegistration /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

type ProtocolQuery struct { /* 定义 ProtocolQuery 类型。 */
	Type        string         `json:"type"`             /* 执行当前语句并推进处理流程。 */
	IntervalSec int            `json:"intervalSec"`      /* 执行当前语句并推进处理流程。 */
	Params      map[string]any `json:"params,omitempty"` /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func ValidateProtocolAccess(p DeviceAccessProfile) error { /* 定义 ValidateProtocolAccess 函数。 */
	if p.PublicHost != "" {
		host := strings.TrimSpace(p.PublicHost)
		if p.Mode != "listener" || p.ConnectionMode == "dial" || host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" || strings.ContainsAny(host, "/@?# ") {
			return errors.New("平台对外地址须为现场设备可填写的域名或 IP，不能使用监听地址")
		}
	}
	if p.ConnectionMode != "" && p.ConnectionMode != "listen" && p.ConnectionMode != "dial" { /* 判断条件并选择处理分支。 */
		return errors.New("连接方向须为 listen 或 dial") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if p.ConnectionMode == "dial" && (p.Mode != "listener" || p.Network != "tcp" || p.DeviceID == "" || p.Host == "") { /* 判断条件并选择处理分支。 */
		return errors.New("主动 TCP 连接需要设备标识与目标地址") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if p.WireFormat != "" && p.WireFormat != "modbus_tcp" && p.WireFormat != "rtu_over_tcp" { /* 判断条件并选择处理分支。 */
		return errors.New("不支持的 Modbus 报文格式") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if p.WireFormat != "" && p.Mode != "poll" { /* 判断条件并选择处理分支。 */
		return errors.New("Modbus 报文格式仅用于轮询实例") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(p.Queries) > 32 || len(p.ChildProducts) > 64 { /* 判断条件并选择处理分支。 */
		return errors.New("查询或子设备产品数量超限") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if (len(p.Queries) > 0 || len(p.ChildProducts) > 0) && p.Mode != "listener" { /* 判断条件并选择处理分支。 */
		return errors.New("查询与子设备映射需要 Go 协议接入实例") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(p.Queries) > 0 && p.Network != "tcp" { /* 判断条件并选择处理分支。 */
		return errors.New("定时查询仅支持 TCP 接入实例") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	seen := map[string]bool{}     /* 更新 seen 的值。 */
	for _, q := range p.Queries { /* 循环处理当前数据。 */
		if !ValidProtocolDeviceID(q.Type) || q.IntervalSec < 1 || q.IntervalSec > 86400 || seen[q.Type] { /* 判断条件并选择处理分支。 */
			return errors.New("查询类型须唯一，周期须为 1 至 86400 秒") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		seen[q.Type] = true            /* 更新 seen[q.Type] 的值。 */
		b, e := json.Marshal(q.Params) /* 更新 e 的值。 */
		if e != nil || len(b) > 4096 { /* 判断条件并选择处理分支。 */
			return errors.New("查询参数无效或超过 4 KiB") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	seen = map[string]bool{}            /* 更新 seen 的值。 */
	for _, c := range p.ChildProducts { /* 循环处理当前数据。 */
		if !ValidProtocolDeviceID(c.Type) || !ValidProtocolDeviceID(c.ProductID) || c.ProductID == p.ProductID || seen[c.Type] { /* 判断条件并选择处理分支。 */
			return errors.New("子设备类型须唯一并关联不同的有效产品") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		seen[c.Type] = true /* 更新 seen[c.Type] 的值。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func ChildDeviceID(tenant, parent, address string) string { /* 定义 ChildDeviceID 函数。 */
	return "child_" + ProtocolDeviceAccessKey(tenant, parent+"\x00"+address)[9:] /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
