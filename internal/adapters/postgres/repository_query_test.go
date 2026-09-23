package postgres /* 声明 postgres 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"strings" /* 执行当前语句并推进处理流程。 */
	"testing" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestCountManagedDeviceChildrenQueryMatchesDeviceRegistrySchema(t *testing.T) { /* 定义 TestCountManagedDeviceChildrenQueryMatchesDeviceRegistrySchema 函数。 */
	query := strings.ToLower(countManagedDeviceChildrenSQL)                                                                                                    /* 更新 query 的值。 */
	if !strings.Contains(query, "body->>'gatewayid'") || strings.Contains(query, "device_registry.gateway_id") || strings.Contains(query, " and gateway_id") { /* 判断条件并选择处理分支。 */
		t.Fatalf("device child count query must read gatewayId from body jsonb, not a physical gateway_id column: %s", countManagedDeviceChildrenSQL) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
