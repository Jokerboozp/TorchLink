package httpapi

import (
	"net/http"
	"strings"
)

var dutyActionNames = map[string]string{
	"settings": "管理值班岗位、班组与模板", "roster": "管理、导入与发布排班",
	"participate": "到岗、接班与代班", "record": "填写和更正值班记录",
	"ai": "AI 整理交接", "handover": "编辑和提交交班", "accept": "确认接班",
	"item": "创建和跟进事项", "history": "查看历史交接", "export": "导出交接报告",
}

func dutyRoutePermission(method, path string) (string, bool) {
	if !strings.HasPrefix(path, "/api/v1/duty/") {
		return "", false
	}
	if strings.HasSuffix(path, "/pdf") || strings.HasSuffix(path, "/export") || strings.HasSuffix(path, ".csv") {
		return "export", true
	}
	if strings.Contains(path, "/ai-jobs") || strings.HasSuffix(path, "/start-ai") {
		if method == http.MethodGet {
			return "", true
		}
		return "ai", true
	}
	if strings.HasSuffix(path, "/accept") || strings.HasSuffix(path, "/return") {
		return "accept", true
	}
	if strings.Contains(path, "/attachments") {
		if method == http.MethodGet {
			return "", true
		}
		return "record", true
	}
	if method == http.MethodGet {
		return "", true
	}
	if strings.Contains(path, "/stations") || strings.Contains(path, "/teams") || strings.Contains(path, "/shift-templates") {
		return "settings", true
	}
	if strings.Contains(path, "/rosters") {
		return "roster", true
	}
	if strings.Contains(path, "/records") {
		return "record", true
	}
	if strings.Contains(path, "/items") {
		return "item", true
	}
	if strings.Contains(path, "/handovers") {
		return "handover", true
	}
	if strings.Contains(path, "/notifications") {
		return "", true
	}
	return "participate", true
}

func allowsDutyRoute(p map[string]bool, method, path string) (bool, bool) {
	action, ok := dutyRoutePermission(method, path)
	if !ok {
		return false, false
	}
	return p["menu:duty"] && (action == "" || p["action:duty:"+action]), true
}
