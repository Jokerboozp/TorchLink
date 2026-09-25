package httpapi

import (
	"strings"
)

// Ops center data (metrics, logs, dashboards, infrastructure alerts) is shared
// by every tenant and cannot be isolated per tenant or device. Managed users
// may therefore receive ops permissions only in tenants listed in
// IOT_OPS_TENANTS (the platform operations tenants); in any other tenant the
// permissions are neither offered nor effective. The built-in administrator
// configured by environment variables is the platform-level operator.

var opsMenus = map[string]string{
	"opsOverview":   "运维总览",
	"opsMetrics":    "指标中心",
	"opsLogs":       "日志中心",
	"opsDashboards": "仪表盘",
	"opsAlerts":     "监控告警",
}

func init() {
	for id, name := range opsMenus {
		menuNames[id] = name
	}
}

const opsPrefix = "/api/v1/ops/"

// opsRouteMenu maps ops routes to their menu. Shared routes (status and
// personal preferences) return ok with an empty menu.
func opsRouteMenu(path string) (string, bool) {
	if !strings.HasPrefix(path, opsPrefix) {
		return "", false
	}
	rest := strings.TrimPrefix(path, opsPrefix)
	switch {
	case strings.HasPrefix(rest, "overview"):
		return "opsOverview", true
	case strings.HasPrefix(rest, "metrics"):
		return "opsMetrics", true
	case strings.HasPrefix(rest, "logs"):
		return "opsLogs", true
	case strings.HasPrefix(rest, "dashboards"), strings.HasPrefix(rest, "folders"), strings.HasPrefix(rest, "datasources"):
		return "opsDashboards", true
	case strings.HasPrefix(rest, "alerts"), strings.HasPrefix(rest, "silences"), strings.HasPrefix(rest, "notifications"):
		return "opsAlerts", true
	}
	return "", true
}

func opsSharedRoute(path string) bool {
	menu, ok := opsRouteMenu(path)
	return ok && menu == ""
}

func hasOpsMenu(p map[string]bool) bool {
	for id := range opsMenus {
		if p["menu:"+id] {
			return true
		}
	}
	return false
}

var opsActionNames = map[string]string{
	"POST /api/v1/ops/metrics/query":                      "执行 PromQL 查询",
	"POST /api/v1/ops/metrics/rule-groups":                "新建指标规则组",
	"PUT /api/v1/ops/metrics/rule-groups/:name":           "编辑 / 启停指标规则组",
	"DELETE /api/v1/ops/metrics/rule-groups/:name":        "删除指标规则组",
	"POST /api/v1/ops/logs/query":                         "执行 LogQL 查询",
	"POST /api/v1/ops/logs/export":                        "导出日志",
	"POST /api/v1/ops/logs/rule-groups":                   "新建日志规则组",
	"PUT /api/v1/ops/logs/rule-groups/:name":              "编辑 / 启停日志规则组",
	"DELETE /api/v1/ops/logs/rule-groups/:name":           "删除日志规则组",
	"PUT /api/v1/ops/logs/retention":                      "修改日志保留策略",
	"POST /api/v1/ops/logs/delete-requests":               "提交日志删除请求",
	"DELETE /api/v1/ops/logs/delete-requests/:id":         "取消日志删除请求",
	"POST /api/v1/ops/dashboards":                         "新建仪表盘",
	"PUT /api/v1/ops/dashboards/:uid":                     "编辑仪表盘",
	"DELETE /api/v1/ops/dashboards/:uid":                  "删除仪表盘",
	"POST /api/v1/ops/dashboards/:uid/copy":               "复制仪表盘",
	"POST /api/v1/ops/dashboards/import":                  "导入仪表盘",
	"POST /api/v1/ops/dashboards/preview":                 "预览面板查询",
	"POST /api/v1/ops/folders":                            "新建仪表盘文件夹",
	"PUT /api/v1/ops/folders/:uid":                        "重命名仪表盘文件夹",
	"DELETE /api/v1/ops/folders/:uid":                     "删除仪表盘文件夹",
	"GET /api/v1/ops/datasources/:uid":                    "查看数据源配置",
	"POST /api/v1/ops/datasources":                        "新建数据源",
	"PUT /api/v1/ops/datasources/:uid":                    "编辑数据源",
	"DELETE /api/v1/ops/datasources/:uid":                 "删除数据源",
	"POST /api/v1/ops/datasources/:uid/test":              "测试数据源连接",
	"POST /api/v1/ops/silences":                           "新建静默",
	"PUT /api/v1/ops/silences/:id":                        "编辑静默",
	"DELETE /api/v1/ops/silences/:id":                     "解除静默",
	"PUT /api/v1/ops/notifications":                       "修改通知路由与渠道",
	"POST /api/v1/ops/notifications/receivers/:name/test": "发送测试通知",
}

func opsActionName(method, path string) (string, bool) {
	if !strings.HasPrefix(path, opsPrefix) {
		return "", false
	}
	if name, ok := opsActionNames[method+" "+path]; ok {
		return name, true
	}
	return "", false
}

func (s *Server) opsTenantAllowed(tenantID string) bool {
	for _, t := range s.cfg.Ops.Tenants {
		if strings.TrimSpace(t) == tenantID {
			return true
		}
	}
	return false
}

func isOpsPermission(id string) bool {
	if strings.HasPrefix(id, "menu:") {
		_, ok := opsMenus[strings.TrimPrefix(id, "menu:")]
		return ok
	}
	return strings.Contains(id, " "+opsPrefix)
}

// stripOpsPermissions removes ops grants for users outside ops tenants.
func (s *Server) stripOpsPermissions(tenantID string, p map[string]bool) {
	if s.opsTenantAllowed(tenantID) {
		return
	}
	for id := range p {
		if isOpsPermission(id) {
			delete(p, id)
		}
	}
}

func (s *Server) permissionCatalogFor(tenantID string) []permissionItem {
	items := s.permissionCatalog()
	if s.opsTenantAllowed(tenantID) {
		return items
	}
	out := items[:0]
	for _, item := range items {
		if !isOpsPermission(item.ID) {
			out = append(out, item)
		}
	}
	return out
}

func (s *Server) knownPermissionsFor(tenantID string) map[string]bool {
	known := map[string]bool{}
	for _, v := range s.permissionCatalogFor(tenantID) {
		known[v.ID] = true
	}
	return known
}

func (s *Server) validPermissionsFor(tenantID string, values []string) bool {
	known := s.knownPermissionsFor(tenantID)
	for _, v := range values {
		if !known[v] {
			return false
		}
	}
	return true
}
