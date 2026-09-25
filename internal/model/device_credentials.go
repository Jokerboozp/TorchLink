package model /* 声明 model 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"encoding/json"
	"strings"
) /* 结束当前表达式或代码块。 */

// UsesPlatformCredentials distinguishes authenticated HTTP/MQTT ingress from
// protocol connections. A device's explicit connector takes precedence over
// the product transport. Inventory without either uses the managed HTTP API.
func (d ManagedDevice) UsesPlatformCredentials(product Product) bool { /* 定义 UsesPlatformCredentials 函数。 */
	if d.DeviceRole == "CHILD" || d.GatewayID != "" { /* 判断条件并选择处理分支。 */
		return false /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	transport := d.Connector /* 更新 transport 的值。 */
	if transport == "" {     /* 判断条件并选择处理分支。 */
		transport = product.Transport /* 更新 transport 的值。 */
	} /* 结束当前表达式或代码块。 */
	switch strings.ToUpper(transport) { /* 根据条件选择处理路径。 */
	case "TCP", "UDP", "TCP_UDP", "TCP_CHILD", "MODBUS_TCP", "MODBUS_RTU_TCP", "MODBUS_RTU": /* 处理当前分支。 */
		return false /* 返回当前处理结果。 */
	default: /* 处理当前分支。 */
		return true /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

// Public keeps the repository's unique internal key out of protocol-device UI
// and onboarding exports. The stored key is not a protocol authentication secret.
func (d ManagedDevice) Public(product Product) ManagedDevice { /* 定义 Public 函数。 */
	if !d.UsesPlatformCredentials(product) { /* 判断条件并选择处理分支。 */
		d.AccessKey, d.SecretHash, d.SecretHint = "", "", "" /* 更新 d.SecretHint 的值。 */
	} /* 结束当前表达式或代码块。 */
	d.OnboardingRequestHash = ""
	return d /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

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
