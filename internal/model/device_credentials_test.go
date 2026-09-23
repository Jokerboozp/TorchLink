package model /* 声明 model 包。 */

import "testing" /* 引入当前代码需要的依赖。 */

func TestDeviceCredentialScope(t *testing.T) { /* 定义 TestDeviceCredentialScope 函数。 */
	for _, tc := range []struct { /* 循环处理当前数据。 */
		name, transport, connector, role, parent string /* 执行当前语句并推进处理流程。 */
		want                                     bool   /* 执行当前语句并推进处理流程。 */
	}{ /* 结束当前表达式或代码块。 */
		{"standard HTTP on TCP product", "TCP", "HTTP", "DIRECT", "", true},           /* 执行当前语句并推进处理流程。 */
		{"standard MQTT on Modbus product", "MODBUS_TCP", "MQTT", "DIRECT", "", true}, /* 执行当前语句并推进处理流程。 */
		{"explicit TCP on HTTP product", "HTTP", "TCP", "DIRECT", "", false},          /* 执行当前语句并推进处理流程。 */
		{"manual Modbus", "MODBUS_RTU_TCP", "", "DIRECT", "", false},                  /* 执行当前语句并推进处理流程。 */
		{"child of HTTP gateway", "HTTP", "HTTP", "CHILD", "main", false},             /* 执行当前语句并推进处理流程。 */
		{"child with incomplete role", "HTTP", "HTTP", "", "main", false},             /* 执行当前语句并推进处理流程。 */
		{"HTTP gateway", "HTTP", "", "GATEWAY", "", true},                             /* 执行当前语句并推进处理流程。 */
		{"managed HTTP default", "", "", "DIRECT", "", true},                          /* 执行当前语句并推进处理流程。 */
	} { /* 结束当前表达式或代码块。 */
		t.Run(tc.name, func(t *testing.T) { /* 执行当前语句并推进处理流程。 */
			d := ManagedDevice{DeviceRole: tc.role, GatewayID: tc.parent, Tags: map[string]string{"connector": tc.connector}} /* 更新 d 的值。 */
			if got := d.UsesPlatformCredentials(Product{Transport: tc.transport}); got != tc.want {                           /* 判断条件并选择处理分支。 */
				t.Fatalf("credential support %v, want %v", got, tc.want) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		}) /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
