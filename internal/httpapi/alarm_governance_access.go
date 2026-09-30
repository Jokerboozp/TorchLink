package httpapi

import (
	"iot-platform/internal/alarmgovernance"
	"iot-platform/internal/model"
	"net/http"
	"strings"
)

var governanceActionNames = map[string]string{"view": "查看反复报警治理", "cases": "建立、核查与指派事项", "record": "填写核实、活动与关联记录", "cause": "确认、争议与更正原因", "measures": "安排与实施改善措施", "acceptance": "独立专业验收", "observation": "确认观察计划与评价", "complete": "完成治理轮次", "reopen": "人工复核并重开", "ai": "生成AI解读", "analyse": "运行确定性分析", "export": "导出正式报告", "templates": "编辑配置草稿", "publish": "发布及退休配置"}

func governanceRoutePermission(method, path string) (string, bool) {
	if !strings.HasPrefix(path, "/api/v1/alarm-governance/") {
		return "", false
	}
	if strings.HasSuffix(path, "/export") {
		return "export", true
	}
	if strings.HasSuffix(path, "/source-fields") {
		return "templates", true
	}
	if strings.Contains(path, "/ai-jobs") {
		if method == http.MethodGet {
			return "view", true
		}
		return "ai", true
	}
	if strings.Contains(path, "/runs") || strings.Contains(path, "/historical-projections") {
		if method == http.MethodGet {
			return "view", true
		}
		return "analyse", true
	}
	if method == http.MethodGet {
		return "view", true
	}
	parts := strings.Split(strings.TrimPrefix(path, "/api/v1/alarm-governance/"), "/")
	kind := governanceResources[parts[0]]
	op := "create"
	if len(parts) > 2 {
		op = parts[len(parts)-1]
	}
	if method == http.MethodPatch {
		op = "update"
	}
	if parts[0] == "rounds" && len(parts) > 2 {
		kind = governanceResources[parts[2]]
	}
	if strings.Contains(path, "/attachments") || strings.Contains(path, "/business-links") || parts[0] == "reminders" {
		return "record", true
	}
	if kind == "" {
		kind = model.GovernanceCaseKind
	}
	return alarmgovernance.Permission(kind, op), true
}
func allowsGovernanceRoute(p map[string]bool, method, path string) (bool, bool) {
	action, ok := governanceRoutePermission(method, path)
	if !ok {
		return false, false
	}
	allowed := p["menu:alarmGovernance"] && p["action:alarmGovernance:"+action]
	if action == "view" && p["menu:alarmGovernance"] {
		allowed = true
	}
	if strings.Contains(path, "/templates") || strings.Contains(path, "/scene-presets") || strings.Contains(path, "/type-profiles") {
		return allowed, true
	}
	return allowed && p["menu:devices"] && p["menu:alarms"], true
}
