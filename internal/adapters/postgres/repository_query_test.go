package postgres /* 声明 postgres 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"strings" /* 执行当前语句并推进处理流程。 */
	"testing" /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/ports"
) /* 结束当前表达式或代码块。 */

func TestCountManagedDeviceChildrenQueryMatchesDeviceRegistrySchema(t *testing.T) { /* 定义 TestCountManagedDeviceChildrenQueryMatchesDeviceRegistrySchema 函数。 */
	query := strings.ToLower(countManagedDeviceChildrenSQL)                                                                                                    /* 更新 query 的值。 */
	if !strings.Contains(query, "body->>'gatewayid'") || strings.Contains(query, "device_registry.gateway_id") || strings.Contains(query, " and gateway_id") { /* 判断条件并选择处理分支。 */
		t.Fatalf("device child count query must read gatewayId from body jsonb, not a physical gateway_id column: %s", countManagedDeviceChildrenSQL) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestDeviceFilterSQLBindsEveryValue(t *testing.T) {
	where, args := deviceFilterSQL(ports.DeviceFilter{TenantID: "t", Role: "GATEWAY", GatewayProductIDs: []string{"gw"}, RestrictProducts: true, Query: `50%_a\b`, Status: "ENABLED", Runtime: "NEVER_SEEN"})
	if len(args) != 7 || !strings.Contains(where, "$7") || strings.Contains(where, "$8") {
		t.Fatalf("placeholders do not match arguments: %s %v", where, args)
	}
	if products, ok := args[3].([]string); !ok || products == nil || len(products) != 0 {
		t.Fatalf("restricted empty product list must match nothing, got %#v", args[3])
	}
	if args[4] != `%50\%\_a\\b%` || !strings.Contains(where, `ESCAPE '\'`) {
		t.Fatalf("keyword wildcards must be escaped: %v", args[4])
	}
	for _, column := range []string{"d.body->>'deviceRole'", "d.body->>'name'", "s.business_status", "d.status", "d.product_id"} {
		if !strings.Contains(where, column) {
			t.Fatalf("missing %s in %s", column, where)
		}
	}
	if where, args = deviceFilterSQL(ports.DeviceFilter{TenantID: "t"}); where != "d.tenant_id=$1" || len(args) != 1 {
		t.Fatalf("empty filter: %s %v", where, args)
	}
}
