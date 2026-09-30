package httpapi

import "strings"

var analyticsMenus = map[string]string{
	"dataQuality":    "数据质量",
	"monitoringGaps": "监测连续性",
	"ruleLab":        "告警策略实验台",
	"response":       "演练与复盘",
	"maintenance":    "维护与投入",
}

func analyticsRouteMenu(path string) (string, bool) {
	for _, entry := range [][2]string{{"/api/v1/data-quality", "dataQuality"}, {"/api/v1/monitoring-gaps", "monitoringGaps"}, {"/api/v1/rule-lab", "ruleLab"}, {"/api/v1/response-procedures", "response"}, {"/api/v1/drills", "response"}, {"/api/v1/response-cases", "response"}, {"/api/v1/response-runs", "response"}, {"/api/v1/response-staff", "response"}, {"/api/v1/response-evaluations", "response"}, {"/api/v1/response-revisions", "response"}, {"/api/v1/response-attachments", "response"}, {"/api/v1/corrective-actions", "response"}, {"/api/v1/follow-up-sources", "response"}, {"/api/v1/assets", "maintenance"}, {"/api/v1/maintenance-records", "maintenance"}, {"/api/v1/maintenance-revisions", "maintenance"}, {"/api/v1/maintenance-attachments", "maintenance"}, {"/api/v1/maintenance-observations", "maintenance"}, {"/api/v1/maintenance-costs", "maintenance"}, {"/api/v1/maintenance-contexts", "maintenance"}, {"/api/v1/maintenance-admissions", "maintenance"}, {"/api/v1/maintenance-fault-cycles", "maintenance"}, {"/api/v1/investment-evaluations", "maintenance"}, {"/api/v1/investment-scenarios", "maintenance"}} {
		if path == entry[0] || strings.HasPrefix(path, entry[0]+"/") {
			return entry[1], true
		}
	}
	return "", false
}

func analyticsActionName(method, path string) (string, bool) {
	menu, ok := analyticsRouteMenu(path)
	if !ok {
		return "", false
	}
	if path == "/api/v1/maintenance-costs" || strings.HasPrefix(path, "/api/v1/maintenance-costs/") {
		return "读取 / 管理维护资金信息", true
	}
	if path == "/api/v1/maintenance-attachments/:id" {
		return "下载维修私有附件", true
	}
	if path == "/api/v1/data-quality/calibrations/attachments/:id" {
		return "下载校准附件", true
	}
	if path == "/api/v1/data-quality/calibrations/attachments" {
		return "上传校准附件", true
	}
	if strings.HasSuffix(path, "/export") || strings.HasSuffix(path, "/report") {
		return "导出" + analyticsMenus[menu] + "报告", true
	}
	if strings.HasSuffix(path, "/ai-jobs") {
		if method == "GET" {
			return "查看 AI 解读", true
		}
		return "启动 AI 解读", true
	}
	if strings.HasSuffix(path, "/stop") {
		return "停止分析任务", true
	}
	if strings.Contains(path, "/reviews") {
		return "人工核实与确认", true
	}
	if strings.HasSuffix(path, "/publish") {
		return "发布共享配置", true
	}
	if method == "GET" {
		return "查看" + analyticsMenus[menu], true
	}
	return "管理 / 执行" + analyticsMenus[menu], true
}

func analyticsProtectedRead(path string) bool {
	if path == "/api/v1/maintenance-costs" || strings.HasPrefix(path, "/api/v1/maintenance-costs/") || path == "/api/v1/response-attachments/:id" || path == "/api/v1/maintenance-attachments/:id" {
		return true
	}
	_, ok := analyticsRouteMenu(path)
	return ok && (strings.HasSuffix(path, "/export") || strings.HasSuffix(path, "/report") || path == "/api/v1/data-quality/calibrations/attachments/:id")
}
