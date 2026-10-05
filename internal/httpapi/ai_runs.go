package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"iot-platform/internal/ports"
)

func (s *Server) aiWorkflowRuns(w http.ResponseWriter, r *http.Request) {
	if limited(r.Context()) {
		problem(w, http.StatusForbidden, "管理工作流需要当前租户全部设备的访问权限")
		return
	}
	manager, ok := s.engine.AIWorkflows.(ports.AIWorkflowRunManager)
	if !ok {
		problem(w, http.StatusServiceUnavailable, "AI 工作流运行管理不可用，请更新 Harness")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	items, err := manager.ListWorkflowRuns(ctx, claims(r).TenantID)
	if err != nil {
		workflowRunProblem(w, err)
		return
	}
	// Keep the tenant boundary even when a runtime adapter returns extra rows.
	visible := []ports.AIWorkflowRun{}
	for _, item := range items {
		if item.TenantID == claims(r).TenantID {
			visible = append(visible, item)
		}
	}
	write(w, http.StatusOK, map[string]any{"items": visible, "total": len(visible)})
}

func (s *Server) stopAIWorkflowRun(w http.ResponseWriter, r *http.Request) {
	if limited(r.Context()) {
		problem(w, http.StatusForbidden, "管理工作流需要当前租户全部设备的访问权限")
		return
	}
	manager, ok := s.engine.AIWorkflows.(ports.AIWorkflowRunManager)
	if !ok {
		problem(w, http.StatusServiceUnavailable, "AI 工作流运行管理不可用，请更新 Harness")
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if !validAIModelName(id) || strings.ContainsAny(id, "/\\?#%") {
		problem(w, http.StatusUnprocessableEntity, "工作流运行编号无效")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := manager.StopWorkflowRun(ctx, claims(r).TenantID, id); err != nil {
		workflowRunProblem(w, err)
		return
	}
	s.audit(r, "ai.workflow.run.stop", "ai-workflow-run", id, nil)
	write(w, http.StatusAccepted, map[string]any{"runId": id, "status": "stopping"})
}

func workflowRunProblem(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ports.ErrAIWorkflowRunNotFound):
		problem(w, http.StatusNotFound, ports.ErrAIWorkflowRunNotFound.Error())
	case errors.Is(err, ports.ErrAIWorkflowManagementUnavailable):
		problem(w, http.StatusServiceUnavailable, ports.ErrAIWorkflowManagementUnavailable.Error())
	default:
		problem(w, http.StatusBadGateway, "无法访问 AI 工作流运行管理，请检查 Harness 服务后重试")
	}
}

// aiRunFilter reads the run history filters; a missing range covers the
// last 30 days.
func aiRunFilter(r *http.Request) ports.AIRunFilter {
	q := r.URL.Query()
	f := ports.AIRunFilter{TenantID: claims(r).TenantID, WorkflowID: strings.TrimSpace(q.Get("workflowId")), Status: strings.ToUpper(strings.TrimSpace(q.Get("status"))), Start: queryInt64(r, "start"), End: queryInt64(r, "end")}
	if f.Start <= 0 && f.End <= 0 {
		f.Start = time.Now().AddDate(0, 0, -30).UnixMilli()
	}
	page := parseListPagination(r)
	f.Limit, f.Offset = page.PageSize, page.Offset
	return f
}

// aiRunHistory lists finished runs with their sizes, token usage and outcome.
func (s *Server) aiRunHistory(w http.ResponseWriter, r *http.Request) {
	if limited(r.Context()) {
		problem(w, http.StatusForbidden, "查看 AI 运行记录需要当前租户全部设备的访问权限")
		return
	}
	if s.engine.AIRuns == nil {
		write(w, http.StatusOK, map[string]any{"items": []any{}, "total": 0, "available": false})
		return
	}
	items, total, err := s.engine.AIRuns.ListAIRuns(r.Context(), aiRunFilter(r))
	if err != nil {
		problem(w, http.StatusInternalServerError, "AI 运行记录读取失败")
		return
	}
	write(w, http.StatusOK, map[string]any{"items": items, "total": total, "available": true})
}

// aiRunUsage sums runs per day and workflow for the same filters.
func (s *Server) aiRunUsage(w http.ResponseWriter, r *http.Request) {
	if limited(r.Context()) {
		problem(w, http.StatusForbidden, "查看 AI 运行记录需要当前租户全部设备的访问权限")
		return
	}
	if s.engine.AIRuns == nil {
		write(w, http.StatusOK, map[string]any{"items": []any{}, "available": false})
		return
	}
	items, err := s.engine.AIRuns.AIRunUsage(r.Context(), aiRunFilter(r))
	if err != nil {
		problem(w, http.StatusInternalServerError, "AI 用量统计读取失败")
		return
	}
	write(w, http.StatusOK, map[string]any{"items": items, "available": true})
}
