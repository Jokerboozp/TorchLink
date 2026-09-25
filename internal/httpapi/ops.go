package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/opscenter"
	"iot-platform/internal/ports"
)

// Ops center HTTP API. Handlers only parse input, check the extra permission
// needed for free-form queries, audit changes and map errors; component
// access and validation live in internal/opscenter and its adapters.

func (s *Server) SetOpsCenter(ops *opscenter.Service) { s.ops = ops }

func (s *Server) opsRoutes() {
	a := s.authorize("admin")
	r := s.router
	e := s.endpoint
	r.GET("/api/v1/ops/status", a, e(s.opsStatus))
	r.GET("/api/v1/ops/overview", a, e(s.opsOverview))
	r.GET("/api/v1/ops/overview/series", a, e(s.opsOverviewSeries))

	r.GET("/api/v1/ops/metrics/catalog", a, e(s.opsMetricCatalog))
	r.GET("/api/v1/ops/metrics/labels", a, e(s.opsMetricLabels))
	r.GET("/api/v1/ops/metrics/label-values", a, e(s.opsMetricLabelValues))
	r.GET("/api/v1/ops/metrics/explore", a, e(s.opsMetricExplore))
	r.GET("/api/v1/ops/metrics/validate", a, e(s.opsMetricValidate))
	r.POST("/api/v1/ops/metrics/query", a, e(s.opsMetricQuery))
	r.GET("/api/v1/ops/metrics/targets", a, e(s.opsTargets))
	r.GET("/api/v1/ops/metrics/rules", a, e(s.opsRuleGroups(opscenter.SourcePrometheus)))
	r.POST("/api/v1/ops/metrics/rule-groups", a, e(s.opsSaveRuleGroup(opscenter.SourcePrometheus)))
	r.PUT("/api/v1/ops/metrics/rule-groups/:name", a, e(s.opsSaveRuleGroup(opscenter.SourcePrometheus), "name"))
	r.DELETE("/api/v1/ops/metrics/rule-groups/:name", a, e(s.opsDeleteRuleGroup(opscenter.SourcePrometheus), "name"))

	r.GET("/api/v1/ops/logs/labels", a, e(s.opsLogLabels))
	r.GET("/api/v1/ops/logs/label-values", a, e(s.opsLogLabelValues))
	r.GET("/api/v1/ops/logs/search", a, e(s.opsLogSearch))
	r.GET("/api/v1/ops/logs/volume", a, e(s.opsLogVolume))
	r.POST("/api/v1/ops/logs/query", a, e(s.opsLogQuery))
	r.GET("/api/v1/ops/logs/validate", a, e(s.opsLogValidate))
	r.GET("/api/v1/ops/logs/context", a, e(s.opsLogContext))
	r.GET("/api/v1/ops/logs/tail", a, e(s.opsLogTail))
	r.POST("/api/v1/ops/logs/export", a, e(s.opsLogExport))
	r.GET("/api/v1/ops/logs/rules", a, e(s.opsRuleGroups(opscenter.SourceLoki)))
	r.POST("/api/v1/ops/logs/rule-groups", a, e(s.opsSaveRuleGroup(opscenter.SourceLoki)))
	r.PUT("/api/v1/ops/logs/rule-groups/:name", a, e(s.opsSaveRuleGroup(opscenter.SourceLoki), "name"))
	r.DELETE("/api/v1/ops/logs/rule-groups/:name", a, e(s.opsDeleteRuleGroup(opscenter.SourceLoki), "name"))
	r.GET("/api/v1/ops/logs/retention", a, e(s.opsRetention))
	r.PUT("/api/v1/ops/logs/retention", a, e(s.opsSaveRetention))
	r.GET("/api/v1/ops/logs/delete-requests", a, e(s.opsDeleteRequests))
	r.POST("/api/v1/ops/logs/delete-requests", a, e(s.opsCreateDeleteRequest))
	r.DELETE("/api/v1/ops/logs/delete-requests/:id", a, e(s.opsCancelDeleteRequest, "id"))

	r.GET("/api/v1/ops/dashboards", a, e(s.opsDashboards))
	r.GET("/api/v1/ops/dashboards/templates", a, e(s.opsDashboardTemplates))
	r.GET("/api/v1/ops/dashboards/:uid", a, e(s.opsDashboard, "uid"))
	r.GET("/api/v1/ops/dashboards/:uid/export", a, e(s.opsExportDashboard, "uid"))
	r.GET("/api/v1/ops/dashboards/:uid/panels/:panelId/data", a, e(s.opsPanelData, "uid", "panelId"))
	r.GET("/api/v1/ops/dashboards/:uid/variables/:name/options", a, e(s.opsVariableOptions, "uid", "name"))
	r.POST("/api/v1/ops/dashboards", a, e(s.opsSaveDashboard))
	r.POST("/api/v1/ops/dashboards/import", a, e(s.opsImportDashboard))
	r.POST("/api/v1/ops/dashboards/preview", a, e(s.opsPreview))
	r.POST("/api/v1/ops/dashboards/:uid/copy", a, e(s.opsCopyDashboard, "uid"))
	r.PUT("/api/v1/ops/dashboards/:uid", a, e(s.opsSaveDashboard, "uid"))
	r.DELETE("/api/v1/ops/dashboards/:uid", a, e(s.opsDeleteDashboard, "uid"))
	r.GET("/api/v1/ops/folders", a, e(s.opsFolders))
	r.POST("/api/v1/ops/folders", a, e(s.opsSaveFolder))
	r.PUT("/api/v1/ops/folders/:uid", a, e(s.opsSaveFolder, "uid"))
	r.DELETE("/api/v1/ops/folders/:uid", a, e(s.opsDeleteFolder, "uid"))
	r.GET("/api/v1/ops/datasources", a, e(s.opsDataSources))
	r.GET("/api/v1/ops/datasources/:uid", a, e(s.opsDataSource, "uid"))
	r.POST("/api/v1/ops/datasources", a, e(s.opsSaveDataSource))
	r.PUT("/api/v1/ops/datasources/:uid", a, e(s.opsSaveDataSource, "uid"))
	r.DELETE("/api/v1/ops/datasources/:uid", a, e(s.opsDeleteDataSource, "uid"))
	r.POST("/api/v1/ops/datasources/:uid/test", a, e(s.opsTestDataSource, "uid"))

	r.GET("/api/v1/ops/alerts", a, e(s.opsAlerts))
	r.GET("/api/v1/ops/alerts/groups", a, e(s.opsAlertGroups))
	r.GET("/api/v1/ops/alerts/rules", a, e(s.opsAlertRules))
	r.GET("/api/v1/ops/alerts/history", a, e(s.opsAlertHistory))
	r.GET("/api/v1/ops/silences", a, e(s.opsSilences))
	r.POST("/api/v1/ops/silences", a, e(s.opsSaveSilence))
	r.PUT("/api/v1/ops/silences/:id", a, e(s.opsSaveSilence, "id"))
	r.DELETE("/api/v1/ops/silences/:id", a, e(s.opsExpireSilence, "id"))
	r.GET("/api/v1/ops/notifications", a, e(s.opsNotifications))
	r.PUT("/api/v1/ops/notifications", a, e(s.opsSaveNotifications))
	r.POST("/api/v1/ops/notifications/receivers/:name/test", a, e(s.opsTestReceiver, "name"))

	r.GET("/api/v1/ops/preferences/saved-queries", a, e(s.opsSavedQueries))
	r.POST("/api/v1/ops/preferences/saved-queries", a, e(s.opsSaveQuery))
	r.DELETE("/api/v1/ops/preferences/saved-queries/:id", a, e(s.opsDeleteSavedQuery, "id"))
	r.GET("/api/v1/ops/preferences/history", a, e(s.opsHistory))
	r.DELETE("/api/v1/ops/preferences/history", a, e(s.opsClearHistory))
	r.PUT("/api/v1/ops/preferences/favorites/:uid", a, e(s.opsFavorite(true), "uid"))
	r.DELETE("/api/v1/ops/preferences/favorites/:uid", a, e(s.opsFavorite(false), "uid"))
}

func (s *Server) opsService(w http.ResponseWriter) *opscenter.Service {
	if s.ops == nil {
		opsProblem(w, http.StatusServiceUnavailable, "OPS_NOT_CONFIGURED", "运维中心未启用", nil)
		return nil
	}
	return s.ops
}

func opsProblem(w http.ResponseWriter, status int, code, detail string, extra map[string]any) {
	body := map[string]any{"type": "about:blank", "title": http.StatusText(status), "status": status, "detail": detail, "code": code}
	for k, v := range extra {
		body[k] = v
	}
	write(w, status, body)
}

// opsError maps service and adapter errors to stable codes. Upstream
// addresses and credentials never reach the response.
func (s *Server) opsError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, context.Canceled) || r.Context().Err() != nil {
		return
	}
	var validation *opscenter.ValidationError
	var apply *opscenter.ApplyError
	var upstream *ports.OpsUpstreamError
	switch {
	case errors.As(err, &validation):
		opsProblem(w, http.StatusUnprocessableEntity, "OPS_INVALID", validation.Message, map[string]any{"field": validation.Field})
	case errors.As(err, &apply):
		opsProblem(w, http.StatusUnprocessableEntity, "OPS_APPLY_FAILED", apply.Message, map[string]any{"reason": apply.Detail, "rolledBack": apply.RolledBack})
	case errors.Is(err, ports.ErrOpsNotConfigured):
		opsProblem(w, http.StatusServiceUnavailable, "OPS_NOT_CONFIGURED", "运维组件未配置：请部署 Prometheus、Loki、Grafana、Alertmanager（Compose 的 ops 配置），设置对应的 IOT_OPS_*_URL 后重启平台，详见 docs/OPS_CENTER.md", nil)
	case errors.Is(err, ports.ErrOpsTimeout):
		opsProblem(w, http.StatusGatewayTimeout, "OPS_UPSTREAM_TIMEOUT", "组件响应超时，请缩小时间范围或简化查询后重试", nil)
	case errors.Is(err, ports.ErrOpsUnavailable):
		opsProblem(w, http.StatusBadGateway, "OPS_UPSTREAM_UNAVAILABLE", "无法连接组件，请检查组件是否运行", nil)
	case errors.Is(err, ports.ErrOpsNotFound):
		opsProblem(w, http.StatusNotFound, "OPS_NOT_FOUND", trimSentinel(err, ports.ErrOpsNotFound, "资源不存在或已被删除"), nil)
	case errors.Is(err, ports.ErrOpsConflict):
		opsProblem(w, http.StatusConflict, "OPS_CONFLICT", trimSentinel(err, ports.ErrOpsConflict, "内容已被其他操作修改，请刷新后重试"), nil)
	case errors.Is(err, ports.ErrOpsReadOnly):
		opsProblem(w, http.StatusConflict, "OPS_READ_ONLY", trimSentinel(err, ports.ErrOpsReadOnly, "该资源为只读，不能在平台中修改"), nil)
	case errors.As(err, &upstream):
		switch {
		case upstream.Status == http.StatusUnauthorized || upstream.Status == http.StatusForbidden:
			opsProblem(w, http.StatusBadGateway, "OPS_UPSTREAM_AUTH", "组件拒绝了平台的凭据，请检查运维中心的组件认证配置", nil)
		case upstream.Status >= 400 && upstream.Status < 500:
			opsProblem(w, http.StatusBadRequest, "OPS_UPSTREAM_REJECTED", upstream.Message, nil)
		default:
			opsProblem(w, http.StatusBadGateway, "OPS_UPSTREAM_ERROR", upstream.Message, nil)
		}
	default:
		if s.log != nil {
			s.log.Error("ops center request failed", "route", r.URL.Path, "error", err)
		}
		opsProblem(w, http.StatusInternalServerError, "OPS_FAILED", "运维操作失败，请查看平台日志", nil)
	}
}

// trimSentinel keeps the specific message wrapped around a sentinel error.
func trimSentinel(err, sentinel error, fallback string) string {
	msg := strings.TrimSpace(strings.TrimPrefix(err.Error(), sentinel.Error()))
	msg = strings.TrimSpace(strings.TrimPrefix(msg, ":"))
	if msg == "" {
		return fallback
	}
	return msg
}

func opsActor(r *http.Request) string {
	c := claims(r)
	return c.Username + "@" + c.TenantID
}

func queryInt64(r *http.Request, name string) int64 {
	v, _ := strconv.ParseInt(r.URL.Query().Get(name), 10, 64)
	return v
}

func queryJSON(r *http.Request, name string, out any) error {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return nil
	}
	if len(raw) > 64<<10 {
		return &opscenter.ValidationError{Field: name, Message: "参数过长"}
	}
	if err := json.Unmarshal([]byte(raw), out); err != nil {
		return &opscenter.ValidationError{Field: name, Message: "参数格式无效"}
	}
	return nil
}

func (s *Server) opsStatus(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	write(w, 200, map[string]any{"capabilities": ops.Capabilities(), "components": ops.Components(r.Context())})
}

func (s *Server) opsOverview(w http.ResponseWriter, r *http.Request) {
	if ops := s.opsService(w); ops != nil {
		write(w, 200, ops.Overview(r.Context()))
	}
}

func (s *Server) opsOverviewSeries(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	ids := strings.Split(r.URL.Query().Get("ids"), ",")
	if len(ids) > 20 {
		ids = ids[:20]
	}
	series, err := ops.OverviewSeries(r.Context(), ids, queryInt64(r, "start"), queryInt64(r, "end"), int(queryInt64(r, "maxPoints")))
	if err != nil {
		s.opsError(w, r, err)
		return
	}
	write(w, 200, map[string]any{"items": series})
}

func (s *Server) opsMetricCatalog(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	catalog, err := ops.MetricCatalog(r.Context(), r.URL.Query().Get("search"), int(queryInt64(r, "limit")))
	if err != nil {
		s.opsError(w, r, err)
		return
	}
	write(w, 200, catalog)
}

func (s *Server) opsMetricLabels(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	labels, err := ops.MetricLabels(r.Context(), r.URL.Query().Get("metric"))
	if err != nil {
		s.opsError(w, r, err)
		return
	}
	write(w, 200, map[string]any{"items": labels})
}

func (s *Server) opsMetricLabelValues(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	values, err := ops.MetricLabelValues(r.Context(), r.URL.Query().Get("label"), r.URL.Query().Get("metric"))
	if err != nil {
		s.opsError(w, r, err)
		return
	}
	write(w, 200, map[string]any{"items": values})
}

func (s *Server) opsMetricExplore(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	q := r.URL.Query()
	in := opscenter.ExploreInput{Metric: q.Get("metric"), Fn: q.Get("fn"), Window: q.Get("window"), By: q.Get("by"), Start: queryInt64(r, "start"), End: queryInt64(r, "end"), StepMs: queryInt64(r, "stepMs"), MaxPoints: int(queryInt64(r, "maxPoints"))}
	if err := queryJSON(r, "matchers", &in.Matchers); err != nil {
		s.opsError(w, r, err)
		return
	}
	result, err := ops.ExploreMetric(r.Context(), in)
	if err != nil {
		s.opsError(w, r, err)
		return
	}
	write(w, 200, result)
}

func (s *Server) opsMetricValidate(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	formatted, err := ops.ValidatePromQL(r.Context(), r.URL.Query().Get("query"))
	if err != nil {
		s.opsError(w, r, err)
		return
	}
	write(w, 200, map[string]any{"valid": true, "formatted": formatted})
}

func (s *Server) opsMetricQuery(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	var in opscenter.MetricQueryInput
	if decode(w, r, &in) != nil {
		return
	}
	result, err := ops.QueryMetrics(r.Context(), in)
	if err != nil {
		s.opsError(w, r, err)
		return
	}
	ops.RecordHistory(context.WithoutCancel(r.Context()), claims(r).TenantID, claims(r).Username, "promql", in.Query, nil)
	write(w, 200, result)
}

func (s *Server) opsTargets(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	targets, err := ops.Targets(r.Context())
	if err != nil {
		s.opsError(w, r, err)
		return
	}
	write(w, 200, map[string]any{"items": targets})
}

func (s *Server) opsRuleGroups(source string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		ops := s.opsService(w)
		if ops == nil {
			return
		}
		groups, err := ops.RuleGroups(r.Context(), source)
		if err != nil {
			s.opsError(w, r, err)
			return
		}
		caps := ops.Capabilities()
		writable := caps.MetricRulesWritable
		if source == opscenter.SourceLoki {
			writable = caps.LogRulesWritable
		}
		write(w, 200, map[string]any{"items": groups, "writable": writable})
	}
}

func (s *Server) opsSaveRuleGroup(source string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		ops := s.opsService(w)
		if ops == nil {
			return
		}
		var in struct {
			opscenter.RuleGroupInput
			Toggle *bool `json:"toggle"`
		}
		if decode(w, r, &in) != nil {
			return
		}
		name := r.PathValue("name")
		action := source + ".rule_group.create"
		var err error
		var group model.OpsRuleGroup
		switch {
		case name != "" && in.Toggle != nil:
			action = source + ".rule_group.toggle"
			err = ops.SetRuleGroupEnabled(r.Context(), source, name, *in.Toggle, in.Revision, opsActor(r))
		default:
			if name != "" {
				action = source + ".rule_group.update"
			}
			group, err = ops.SaveRuleGroup(r.Context(), source, name, in.RuleGroupInput, opsActor(r))
		}
		details := map[string]any{"name": firstNonEmptyString(in.Name, name), "enabled": in.Enabled}
		if in.Toggle != nil {
			details["enabled"] = *in.Toggle
		}
		if err != nil {
			details["error"] = err.Error()
			s.audit(r, "ops."+action+".failed", "ops_rule_group", firstNonEmptyString(name, in.Name), details)
			s.opsError(w, r, err)
			return
		}
		s.audit(r, "ops."+action, "ops_rule_group", firstNonEmptyString(name, in.Name), details)
		if in.Toggle != nil {
			write(w, 200, map[string]any{"success": true})
			return
		}
		write(w, 200, map[string]any{"success": true, "group": group})
	}
}

func (s *Server) opsDeleteRuleGroup(source string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		ops := s.opsService(w)
		if ops == nil {
			return
		}
		name := r.PathValue("name")
		err := ops.DeleteRuleGroup(r.Context(), source, name, r.URL.Query().Get("revision"), opsActor(r))
		if err != nil {
			s.audit(r, "ops."+source+".rule_group.delete.failed", "ops_rule_group", name, map[string]any{"error": err.Error()})
			s.opsError(w, r, err)
			return
		}
		s.audit(r, "ops."+source+".rule_group.delete", "ops_rule_group", name, nil)
		write(w, 200, map[string]any{"success": true})
	}
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func (s *Server) opsLogLabels(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	labels, err := ops.LogLabels(r.Context())
	if err != nil {
		s.opsError(w, r, err)
		return
	}
	write(w, 200, map[string]any{"items": labels})
}

func (s *Server) opsLogLabelValues(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	values, err := ops.LogLabelValues(r.Context(), r.URL.Query().Get("label"))
	if err != nil {
		s.opsError(w, r, err)
		return
	}
	write(w, 200, map[string]any{"items": values})
}

func logSearchFromQuery(r *http.Request) (opscenter.LogSearchInput, error) {
	q := r.URL.Query()
	in := opscenter.LogSearchInput{Query: q.Get("query"), Start: queryInt64(r, "start"), End: queryInt64(r, "end"), Cursor: q.Get("cursor"), Limit: int(queryInt64(r, "limit")), Direction: q.Get("direction"), StepMs: queryInt64(r, "stepMs"), MaxPoints: int(queryInt64(r, "maxPoints"))}
	return in, queryJSON(r, "filter", &in.Filter)
}

func (s *Server) opsLogSearch(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	in, err := logSearchFromQuery(r)
	if err == nil {
		var result model.LogQueryResult
		if result, err = ops.SearchLogs(r.Context(), in, false); err == nil {
			ops.RecordHistory(context.WithoutCancel(r.Context()), claims(r).TenantID, claims(r).Username, "logfilter", "", filterMap(in.Filter))
			write(w, 200, result)
			return
		}
	}
	s.opsError(w, r, err)
}

func (s *Server) opsLogVolume(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	in, err := logSearchFromQuery(r)
	if err == nil {
		var result model.LogQueryResult
		if result, err = ops.LogVolume(r.Context(), in); err == nil {
			write(w, 200, result)
			return
		}
	}
	s.opsError(w, r, err)
}

func filterMap(f opscenter.LogFilter) map[string]any {
	b, _ := json.Marshal(f)
	out := map[string]any{}
	_ = json.Unmarshal(b, &out)
	return out
}

func (s *Server) opsLogQuery(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	var in opscenter.LogSearchInput
	if decode(w, r, &in) != nil {
		return
	}
	result, err := ops.SearchLogs(r.Context(), in, true)
	if err != nil {
		s.opsError(w, r, err)
		return
	}
	ops.RecordHistory(context.WithoutCancel(r.Context()), claims(r).TenantID, claims(r).Username, "logql", in.Query, nil)
	write(w, 200, result)
}

func (s *Server) opsLogValidate(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	formatted, err := ops.ValidateLogQL(r.Context(), r.URL.Query().Get("query"))
	if err != nil {
		s.opsError(w, r, err)
		return
	}
	write(w, 200, map[string]any{"valid": true, "formatted": formatted})
}

func (s *Server) opsLogContext(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	labels := map[string]string{}
	if err := queryJSON(r, "labels", &labels); err != nil {
		s.opsError(w, r, err)
		return
	}
	result, err := ops.LogContext(r.Context(), labels, r.URL.Query().Get("ts"), int(queryInt64(r, "size")))
	if err != nil {
		s.opsError(w, r, err)
		return
	}
	write(w, 200, result)
}

const logQueryPermission = "/api/v1/ops/logs/query"

// opsLogTail streams live log lines as server-sent events. Free-form LogQL
// needs the same permission as the LogQL query endpoint.
func (s *Server) opsLogTail(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	in, err := logSearchFromQuery(r)
	if err != nil {
		s.opsError(w, r, err)
		return
	}
	raw := strings.TrimSpace(in.Query) != ""
	if raw && !requestAllows(r, http.MethodPost, logQueryPermission) {
		opsProblem(w, http.StatusForbidden, "OPS_FORBIDDEN", "没有执行 LogQL 查询的权限", nil)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		opsProblem(w, http.StatusInternalServerError, "OPS_FAILED", "当前连接不支持实时推送", nil)
		return
	}
	var since time.Time
	if ns, err := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64); err == nil && ns > 0 {
		since = time.Unix(0, ns)
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	send := func(event string, payload any) error {
		b, _ := json.Marshal(payload)
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}
	_ = send("ready", map[string]any{"type": "ready"})
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	done := make(chan error, 1)
	events := make(chan map[string]any, 16)
	go func() {
		done <- ops.TailLogs(ctx, in, raw, since, func(entries []model.LogEntry, dropped int) error {
			select {
			case events <- map[string]any{"type": "entries", "entries": entries, "dropped": dropped}:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	for {
		select {
		case event := <-events:
			if send("entries", event) != nil {
				return
			}
		case <-heartbeat.C:
			if send("heartbeat", map[string]any{"type": "heartbeat"}) != nil {
				return
			}
		case err := <-done:
			for len(events) > 0 {
				_ = send("entries", <-events)
			}
			if r.Context().Err() != nil {
				return
			}
			if err == nil || errors.Is(err, context.DeadlineExceeded) {
				_ = send("end", map[string]any{"type": "end", "reason": "timeout"})
				return
			}
			rec := httptestRecorder()
			s.opsError(rec, r, err)
			_ = send("error", map[string]any{"type": "error", "status": rec.status, "body": json.RawMessage(rec.body)})
			return
		}
	}
}

func (s *Server) opsLogExport(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	var in struct {
		opscenter.LogSearchInput
		Format string `json:"format"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	raw := strings.TrimSpace(in.Query) != ""
	if raw && !requestAllows(r, http.MethodPost, logQueryPermission) {
		opsProblem(w, http.StatusForbidden, "OPS_FORBIDDEN", "没有执行 LogQL 查询的权限", nil)
		return
	}
	export, err := ops.ExportLogs(r.Context(), in.LogSearchInput, raw, in.Format)
	if err != nil {
		s.opsError(w, r, err)
		return
	}
	s.audit(r, "ops.logs.export", "ops_logs", export.FileName, map[string]any{"lines": export.Lines, "truncated": export.Truncated, "query": in.Query, "filter": filterMap(in.Filter), "start": in.Start, "end": in.End})
	w.Header().Set("Content-Type", export.ContentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+export.FileName+`"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Export-Lines", strconv.Itoa(export.Lines))
	w.Header().Set("X-Export-Truncated", strconv.FormatBool(export.Truncated))
	w.Header().Set("Access-Control-Expose-Headers", "X-Export-Lines, X-Export-Truncated, Content-Disposition")
	_, _ = w.Write(export.Content)
}

func (s *Server) lokiTenant() string { return s.cfg.Ops.LokiTenant }

func (s *Server) opsRetention(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	settings, err := ops.Retention(r.Context(), s.lokiTenant())
	if err != nil {
		s.opsError(w, r, err)
		return
	}
	write(w, 200, settings)
}

func (s *Server) opsSaveRetention(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	var in opscenter.RetentionInput
	if decode(w, r, &in) != nil {
		return
	}
	settings, err := ops.SaveRetention(r.Context(), s.lokiTenant(), in, opsActor(r))
	if err != nil {
		s.audit(r, "ops.logs.retention.update.failed", "ops_logs_retention", "loki", map[string]any{"period": in.Period, "error": err.Error()})
		s.opsError(w, r, err)
		return
	}
	s.audit(r, "ops.logs.retention.update", "ops_logs_retention", "loki", map[string]any{"period": in.Period, "streams": len(in.Streams)})
	write(w, 200, settings)
}

func (s *Server) opsDeleteRequests(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	items, err := ops.LogDeleteRequests(r.Context())
	if err != nil {
		s.opsError(w, r, err)
		return
	}
	write(w, 200, map[string]any{"items": items})
}

func (s *Server) opsCreateDeleteRequest(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	var in opscenter.DeleteRequestInput
	if decode(w, r, &in) != nil {
		return
	}
	query, err := ops.CreateLogDeleteRequest(r.Context(), in)
	details := map[string]any{"query": query, "start": in.Start, "end": in.End}
	if err != nil {
		details["error"] = err.Error()
		s.audit(r, "ops.logs.delete.request.failed", "ops_logs", "loki", details)
		s.opsError(w, r, err)
		return
	}
	s.audit(r, "ops.logs.delete.request", "ops_logs", "loki", details)
	write(w, 200, map[string]any{"success": true, "query": query})
}

func (s *Server) opsCancelDeleteRequest(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	id := r.PathValue("id")
	if err := ops.CancelLogDeleteRequest(r.Context(), id, r.URL.Query().Get("force") == "true"); err != nil {
		s.opsError(w, r, err)
		return
	}
	s.audit(r, "ops.logs.delete.cancel", "ops_logs", id, nil)
	write(w, 200, map[string]any{"success": true})
}

func (s *Server) opsDashboards(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	q := r.URL.Query()
	items, err := ops.ListDashboards(r.Context(), opscenter.DashboardListInput{Query: q.Get("query"), FolderUID: q.Get("folderUid"), Tag: q.Get("tag"), Favorites: q.Get("favorites") == "true"}, claims(r).TenantID, claims(r).Username)
	if err != nil {
		s.opsError(w, r, err)
		return
	}
	write(w, 200, map[string]any{"items": items})
}

func (s *Server) opsDashboardTemplates(w http.ResponseWriter, r *http.Request) {
	write(w, 200, map[string]any{"items": opscenter.DashboardTemplates()})
}

func (s *Server) opsDashboard(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	view, err := ops.GetDashboard(r.Context(), r.PathValue("uid"), claims(r).TenantID, claims(r).Username)
	if err != nil {
		s.opsError(w, r, err)
		return
	}
	write(w, 200, view)
}

func (s *Server) opsExportDashboard(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	dash, err := ops.ExportDashboard(r.Context(), r.PathValue("uid"), r.URL.Query().Get("external") == "true")
	if err != nil {
		s.opsError(w, r, err)
		return
	}
	write(w, 200, dash)
}

func (s *Server) opsPanelData(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	panelID, err := strconv.Atoi(r.PathValue("panelId"))
	if err != nil {
		opsProblem(w, http.StatusNotFound, "OPS_NOT_FOUND", "面板不存在", nil)
		return
	}
	in := opscenter.PanelDataInput{From: queryInt64(r, "from"), To: queryInt64(r, "to"), MaxDataPoints: int(queryInt64(r, "maxDataPoints"))}
	if err := queryJSON(r, "vars", &in.Vars); err != nil {
		s.opsError(w, r, err)
		return
	}
	data, err := ops.PanelData(r.Context(), r.PathValue("uid"), panelID, in)
	if err != nil {
		s.opsError(w, r, err)
		return
	}
	write(w, 200, data)
}

func (s *Server) opsVariableOptions(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	in := opscenter.VariableOptionsInput{From: queryInt64(r, "from"), To: queryInt64(r, "to")}
	if err := queryJSON(r, "vars", &in.Vars); err != nil {
		s.opsError(w, r, err)
		return
	}
	options, err := ops.VariableOptions(r.Context(), r.PathValue("uid"), r.PathValue("name"), in)
	if err != nil {
		s.opsError(w, r, err)
		return
	}
	write(w, 200, map[string]any{"items": options})
}

func (s *Server) opsSaveDashboard(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	var in opscenter.DashboardSaveInput
	if decode(w, r, &in) != nil {
		return
	}
	uid := r.PathValue("uid")
	result, err := ops.SaveDashboard(r.Context(), uid, in)
	action := "ops.dashboard.create"
	if uid != "" {
		action = "ops.dashboard.update"
	}
	if err != nil {
		s.opsError(w, r, err)
		return
	}
	s.audit(r, action, "ops_dashboard", result.UID, map[string]any{"title": in.Dashboard["title"], "version": result.Version, "folderUid": in.FolderUID})
	write(w, 200, result)
}

func (s *Server) opsCopyDashboard(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	var in struct {
		Title     string `json:"title"`
		FolderUID string `json:"folderUid"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	result, err := ops.CopyDashboard(r.Context(), r.PathValue("uid"), in.Title, in.FolderUID)
	if err != nil {
		s.opsError(w, r, err)
		return
	}
	s.audit(r, "ops.dashboard.copy", "ops_dashboard", result.UID, map[string]any{"source": r.PathValue("uid"), "title": in.Title})
	write(w, 200, result)
}

func (s *Server) opsDeleteDashboard(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	uid := r.PathValue("uid")
	if err := ops.DeleteDashboard(r.Context(), uid); err != nil {
		s.opsError(w, r, err)
		return
	}
	s.audit(r, "ops.dashboard.delete", "ops_dashboard", uid, nil)
	write(w, 200, map[string]any{"success": true})
}

func (s *Server) opsImportDashboard(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	var in opscenter.ImportInput
	if decode(w, r, &in) != nil {
		return
	}
	result, err := ops.ImportDashboard(r.Context(), in)
	if err != nil {
		if errors.Is(err, ports.ErrOpsConflict) {
			opsProblem(w, http.StatusConflict, "OPS_CONFLICT", trimSentinel(err, ports.ErrOpsConflict, "仪表盘已存在"), map[string]any{"analysis": result})
			return
		}
		s.opsError(w, r, err)
		return
	}
	if !in.DryRun && result.Saved != nil {
		s.audit(r, "ops.dashboard.import", "ops_dashboard", result.Saved.UID, map[string]any{"title": result.Title, "template": in.Template, "overwrite": in.Overwrite, "support": result.Support.Level})
	}
	write(w, 200, result)
}

func (s *Server) opsPreview(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	var in struct {
		opscenter.PanelDataInput
		Variable string `json:"variable"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	if in.Variable != "" {
		options, err := ops.VariableOptions(r.Context(), "", in.Variable, opscenter.VariableOptionsInput{From: in.From, To: in.To, Vars: in.Vars, Dashboard: in.Dashboard})
		if err != nil {
			s.opsError(w, r, err)
			return
		}
		write(w, 200, map[string]any{"items": options})
		return
	}
	data, err := ops.PreviewPanel(r.Context(), in.PanelDataInput)
	if err != nil {
		s.opsError(w, r, err)
		return
	}
	write(w, 200, data)
}

func (s *Server) opsFolders(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	items, err := ops.Folders(r.Context())
	if err != nil {
		s.opsError(w, r, err)
		return
	}
	write(w, 200, map[string]any{"items": items})
}

func (s *Server) opsSaveFolder(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	var in struct {
		Title string `json:"title"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	folder, err := ops.SaveFolder(r.Context(), r.PathValue("uid"), in.Title)
	if err != nil {
		s.opsError(w, r, err)
		return
	}
	s.audit(r, "ops.folder.save", "ops_folder", folder.UID, map[string]any{"title": folder.Title})
	write(w, 200, folder)
}

func (s *Server) opsDeleteFolder(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	uid := r.PathValue("uid")
	if err := ops.DeleteFolder(r.Context(), uid); err != nil {
		s.opsError(w, r, err)
		return
	}
	s.audit(r, "ops.folder.delete", "ops_folder", uid, nil)
	write(w, 200, map[string]any{"success": true})
}

func (s *Server) opsDataSources(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	items, err := ops.DataSources(r.Context(), requestAllows(r, http.MethodGet, "/api/v1/ops/datasources/:uid"))
	if err != nil {
		s.opsError(w, r, err)
		return
	}
	write(w, 200, map[string]any{"items": items})
}

func (s *Server) opsDataSource(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	ds, err := ops.DataSource(r.Context(), r.PathValue("uid"))
	if err != nil {
		s.opsError(w, r, err)
		return
	}
	write(w, 200, ds)
}

func (s *Server) opsSaveDataSource(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	var in opscenter.DataSourceInput
	if decode(w, r, &in) != nil {
		return
	}
	uid := r.PathValue("uid")
	ds, err := ops.SaveDataSource(r.Context(), uid, in)
	if err != nil {
		s.opsError(w, r, err)
		return
	}
	s.audit(r, "ops.datasource.save", "ops_datasource", ds.UID, map[string]any{"name": ds.Name, "type": ds.Type, "created": uid == "", "passwordChanged": in.BasicAuthPassword.Mode == "replace" || in.BasicAuthPassword.Mode == "clear"})
	write(w, 200, ds)
}

func (s *Server) opsDeleteDataSource(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	uid := r.PathValue("uid")
	if err := ops.DeleteDataSource(r.Context(), uid); err != nil {
		s.opsError(w, r, err)
		return
	}
	s.audit(r, "ops.datasource.delete", "ops_datasource", uid, nil)
	write(w, 200, map[string]any{"success": true})
}

func (s *Server) opsTestDataSource(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	result, err := ops.TestDataSource(r.Context(), r.PathValue("uid"))
	if err != nil {
		s.opsError(w, r, err)
		return
	}
	write(w, 200, result)
}

func alertListInput(r *http.Request) (opscenter.AlertListInput, error) {
	q := r.URL.Query()
	in := opscenter.AlertListInput{Silenced: q.Get("silenced") != "false", Inhibited: q.Get("inhibited") != "false", Receiver: q.Get("receiver")}
	return in, queryJSON(r, "matchers", &in.Matchers)
}

func (s *Server) opsAlerts(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	in, err := alertListInput(r)
	if err == nil {
		var alerts []model.OpsAlert
		if alerts, err = ops.CurrentAlerts(r.Context(), in); err == nil {
			write(w, 200, map[string]any{"items": alerts})
			return
		}
	}
	s.opsError(w, r, err)
}

func (s *Server) opsAlertGroups(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	in, err := alertListInput(r)
	if err == nil {
		var groups []model.OpsAlertGroup
		if groups, err = ops.AlertGroups(r.Context(), in); err == nil {
			write(w, 200, map[string]any{"items": groups})
			return
		}
	}
	s.opsError(w, r, err)
}

func (s *Server) opsAlertRules(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	groups, warnings, err := ops.AlertRules(r.Context())
	if err != nil {
		s.opsError(w, r, err)
		return
	}
	write(w, 200, map[string]any{"items": groups, "warnings": warnings})
}

func (s *Server) opsAlertHistory(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	items, truncated, stepMs, err := ops.AlertHistory(r.Context(), queryInt64(r, "start"), queryInt64(r, "end"))
	if err != nil {
		s.opsError(w, r, err)
		return
	}
	write(w, 200, map[string]any{"items": items, "truncated": truncated, "stepMs": stepMs})
}

func (s *Server) opsSilences(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	items, err := ops.Silences(r.Context())
	if err != nil {
		s.opsError(w, r, err)
		return
	}
	write(w, 200, map[string]any{"items": items})
}

func (s *Server) opsSaveSilence(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	var in opscenter.SilenceInput
	if decode(w, r, &in) != nil {
		return
	}
	if id := r.PathValue("id"); id != "" {
		in.ID = id
	}
	id, err := ops.SaveSilence(r.Context(), in, opsActor(r))
	if err != nil {
		s.opsError(w, r, err)
		return
	}
	s.audit(r, "ops.silence.save", "ops_silence", id, map[string]any{"replaces": in.ID, "matchers": in.Matchers, "endsAt": in.EndsAt, "comment": in.Comment})
	write(w, 200, map[string]any{"success": true, "id": id})
}

func (s *Server) opsExpireSilence(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	id := r.PathValue("id")
	if err := ops.ExpireSilence(r.Context(), id); err != nil {
		s.opsError(w, r, err)
		return
	}
	s.audit(r, "ops.silence.expire", "ops_silence", id, nil)
	write(w, 200, map[string]any{"success": true})
}

func (s *Server) opsNotifications(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	cfg, err := ops.NotificationConfig(r.Context())
	if err != nil {
		s.opsError(w, r, err)
		return
	}
	write(w, 200, cfg)
}

func (s *Server) opsSaveNotifications(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	var in model.OpsNotificationConfig
	if decode(w, r, &in) != nil {
		return
	}
	cfg, err := ops.SaveNotificationConfig(r.Context(), in, opsActor(r))
	names := []string{}
	for _, receiver := range in.Receivers {
		names = append(names, receiver.Name)
	}
	if err != nil {
		s.audit(r, "ops.notifications.update.failed", "ops_notifications", "alertmanager", map[string]any{"receivers": names, "error": err.Error()})
		s.opsError(w, r, err)
		return
	}
	s.audit(r, "ops.notifications.update", "ops_notifications", "alertmanager", map[string]any{"receivers": names})
	write(w, 200, cfg)
}

func (s *Server) opsTestReceiver(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	name := r.PathValue("name")
	if err := ops.TestReceiver(r.Context(), name, opsActor(r)); err != nil {
		s.opsError(w, r, err)
		return
	}
	s.audit(r, "ops.notifications.test", "ops_receiver", name, nil)
	write(w, 200, map[string]any{"success": true})
}

func (s *Server) opsSavedQueries(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	items, err := ops.SavedQueries(r.Context(), claims(r).TenantID, claims(r).Username, r.URL.Query().Get("language"))
	if err != nil {
		s.opsPrefError(w, r, err)
		return
	}
	write(w, 200, map[string]any{"items": items})
}

func (s *Server) opsPrefError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, opscenter.ErrNoPreferences) {
		opsProblem(w, http.StatusServiceUnavailable, "OPS_NOT_CONFIGURED", "个人查询与收藏存储不可用", nil)
		return
	}
	s.opsError(w, r, err)
}

func (s *Server) opsSaveQuery(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	var in opscenter.SavedQueryInput
	if decode(w, r, &in) != nil {
		return
	}
	item, err := ops.SaveQuery(r.Context(), claims(r).TenantID, claims(r).Username, in)
	if err != nil {
		s.opsPrefError(w, r, err)
		return
	}
	write(w, 200, item)
}

func (s *Server) opsDeleteSavedQuery(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	if err := ops.DeleteSavedQuery(r.Context(), claims(r).TenantID, claims(r).Username, r.PathValue("id")); err != nil {
		s.opsPrefError(w, r, err)
		return
	}
	write(w, 200, map[string]any{"success": true})
}

func (s *Server) opsHistory(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	items, err := ops.History(r.Context(), claims(r).TenantID, claims(r).Username, r.URL.Query().Get("language"))
	if err != nil {
		s.opsPrefError(w, r, err)
		return
	}
	write(w, 200, map[string]any{"items": items})
}

func (s *Server) opsClearHistory(w http.ResponseWriter, r *http.Request) {
	ops := s.opsService(w)
	if ops == nil {
		return
	}
	if err := ops.ClearHistory(r.Context(), claims(r).TenantID, claims(r).Username); err != nil {
		s.opsPrefError(w, r, err)
		return
	}
	write(w, 200, map[string]any{"success": true})
}

func (s *Server) opsFavorite(favorite bool) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		ops := s.opsService(w)
		if ops == nil {
			return
		}
		if err := ops.SetFavorite(r.Context(), claims(r).TenantID, claims(r).Username, r.PathValue("uid"), favorite); err != nil {
			s.opsPrefError(w, r, err)
			return
		}
		write(w, 200, map[string]any{"success": true, "favorite": favorite})
	}
}

// recorder captures an error response so it can be relayed as an SSE event.
type recorder struct {
	header http.Header
	status int
	body   []byte
}

func httptestRecorder() *recorder { return &recorder{header: http.Header{}} }

func (r *recorder) Header() http.Header         { return r.header }
func (r *recorder) WriteHeader(status int)      { r.status = status }
func (r *recorder) Write(b []byte) (int, error) { r.body = append(r.body, b...); return len(b), nil }
