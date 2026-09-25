package model /* 声明 model 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"crypto/sha256" /* 执行当前语句并推进处理流程。 */
	"encoding/hex"  /* 执行当前语句并推进处理流程。 */
	"errors"        /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */
	"unicode"       /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

var ErrProtocolRegistration = errors.New("protocol registration is disabled, stale or conflicts with existing inventory") /* 声明 ErrProtocolRegistration。 */

func ValidProtocolDeviceID(id string) bool { /* 定义 ValidProtocolDeviceID 函数。 */
	if len(id) == 0 || len(id) > 128 || strings.TrimSpace(id) != id { /* 判断条件并选择处理分支。 */
		return false /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for _, c := range id { /* 循环处理当前数据。 */
		if unicode.IsSpace(c) || unicode.IsControl(c) || c == '/' || c == '\\' { /* 判断条件并选择处理分支。 */
			return false /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return true /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func ProtocolDeviceAccessKey(tenant, id string) string { /* 定义 ProtocolDeviceAccessKey 函数。 */
	hash := sha256.Sum256([]byte(tenant + "\x00" + id)) /* 更新 hash 的值。 */
	return "protocol_" + hex.EncodeToString(hash[:])    /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func ProtocolRegistrationDevice(expected, current DeviceAccessProfile, product Product, id, name string, now int64) (ManagedDevice, error) { /* 定义 ProtocolRegistrationDevice 函数。 */
	if expected.Configuration() != current.Configuration() || !current.Enabled || !current.AutoRegister || current.Mode != "listener" || (current.Network != "tcp" && current.Network != "udp") || current.TenantID == "" || current.ID == "" || !ValidProtocolDeviceID(id) || (current.DeviceID != "" && current.DeviceID != id) || product.TenantID != current.TenantID || product.ID != current.ProductID || product.Status != "ENABLED" || len(name) > 256 { /* 判断条件并选择处理分支。 */
		return ManagedDevice{}, ErrProtocolRegistration /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if name == "" { /* 判断条件并选择处理分支。 */
		name = id /* 更新 name 的值。 */
	} /* 结束当前表达式或代码块。 */
	return ManagedDevice{ID: id, TenantID: current.TenantID, ProductID: current.ProductID, Name: name, Status: "ENABLED", DeviceRole: "DIRECT", RegistrationSource: "PROTOCOL_AUTO", AutoRegistered: true, CreatedAt: now, UpdatedAt: now, AccessKey: ProtocolDeviceAccessKey(current.TenantID, id), Connector: strings.ToUpper(current.Network), ConnectorProfileID: current.ID}, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func RegisteredProtocolDevice(existing ManagedDevice, profile DeviceAccessProfile) error { /* 定义 RegisteredProtocolDevice 函数。 */
	if existing.TenantID != profile.TenantID || existing.ProductID != profile.ProductID || existing.Status != "ENABLED" { /* 判断条件并选择处理分支。 */
		return ErrProtocolRegistration /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
