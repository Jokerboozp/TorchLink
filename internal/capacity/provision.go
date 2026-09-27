package capacity

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"iot-platform/internal/onboarding"
)

// provision creates or reuses the test product and the stressAlarm rule
// through the platform API with the operator's own permissions
// (fixtures.autoProvision). Existing products are left unchanged; the test
// rule is reset to its fixed definition so alarm reconciliation is valid.
func (c *controller) provision(ctx context.Context) []PreflightCheck {
	f := c.plan.Fixtures
	api := strings.TrimRight(c.inv.API, "/")
	auth := map[string]string{"Authorization": "Bearer " + c.opToken}
	var out []PreflightCheck

	status, _, err := c.get(ctx, "/api/v1/onboarding/preflight?productId="+url.QueryEscape(f.Product), c.opToken)
	switch {
	case err == nil && status == http.StatusOK:
		out = append(out, PreflightCheck{Name: "自动准备测试产品", OK: true, Detail: "复用已有产品 " + f.Product})
	default:
		body, _ := json.Marshal(map[string]any{
			"id": f.Product, "name": "容量测试标准设备 " + f.Product, "category": "sensor",
			"protocolPackageId": onboarding.StandardPackageID,
			"description":       "capacity-test 自动创建，用于容量测试设备；可在测试结束后删除",
		})
		status, resp, err := doHTTP(ctx, c.httpc, http.MethodPut, api+"/api/v1/products/"+url.PathEscape(f.Product), body, auth)
		ok := err == nil && status/100 == 2
		detail := fmt.Sprintf("创建标准协议产品 %s → %s", f.Product, codeOf(status, err))
		if !ok {
			detail += "：" + clip(string(resp), 160) + "（需要设备模板的新增权限）"
		}
		out = append(out, PreflightCheck{Name: "自动准备测试产品", OK: ok, Detail: detail})
		c.provisioned = append(c.provisioned, "产品 "+f.Product)
	}

	if f.AlarmRuleID == "" {
		return out
	}
	rule := map[string]any{
		"id": f.AlarmRuleID, "name": "容量测试告警 " + f.AlarmRuleID, "productId": f.Product,
		"description": "capacity-test 自动创建：stressAlarm=1 触发，stressAlarm=0 恢复",
		"alarmType":   "CAPACITY_TEST", "level": "LOW", "match": "all", "enabled": true,
		"conditions": []map[string]any{{"field": "stressAlarm", "operator": "eq", "value": 1}},
		"recovery":   []map[string]any{{"field": "stressAlarm", "operator": "eq", "value": 0}},
	}
	body, _ := json.Marshal(rule)
	status, resp, err := doHTTP(ctx, c.httpc, http.MethodPut, api+"/api/v1/rules/"+url.PathEscape(f.AlarmRuleID)+"?confirmConflicts=true", body, auth)
	action := "更新"
	if err == nil && status == http.StatusNotFound {
		action = "创建"
		status, resp, err = doHTTP(ctx, c.httpc, http.MethodPost, api+"/api/v1/rules?confirmConflicts=true", body, auth)
	}
	ok := err == nil && status/100 == 2
	detail := fmt.Sprintf("%s测试规则 %s → %s", action, f.AlarmRuleID, codeOf(status, err))
	if !ok {
		detail += "：" + clip(string(resp), 160) + "（需要告警规则的新增与编辑权限）"
	}
	out = append(out, PreflightCheck{Name: "自动准备测试规则", OK: ok, Detail: detail})
	if action == "创建" && ok {
		c.provisioned = append(c.provisioned, "规则 "+f.AlarmRuleID)
	}
	return out
}
