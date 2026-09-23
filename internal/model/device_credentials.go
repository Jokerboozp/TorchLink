package model /* 声明 model 包。 */

import "strings" /* 引入当前代码需要的依赖。 */

// UsesPlatformCredentials distinguishes authenticated HTTP/MQTT ingress from
// protocol connections. A device's explicit connector takes precedence over
// the product transport. Inventory without either uses the managed HTTP API.
func (d ManagedDevice) UsesPlatformCredentials(product Product) bool { /* 定义 UsesPlatformCredentials 函数。 */
	if d.DeviceRole == "CHILD" || d.GatewayID != "" { /* 判断条件并选择处理分支。 */
		return false /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	transport := d.Tags["connector"] /* 更新 transport 的值。 */
	if transport == "" {             /* 判断条件并选择处理分支。 */
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
	return d /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
