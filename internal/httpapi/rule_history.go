package httpapi

import (
	"errors"
	"iot-platform/internal/model"
	"net/http"
	"strings"
)

func (s *Server) ruleHistoryRoutes() {
	s.router.GET("/api/v1/rules/:id/revisions", s.authorize("viewer"), s.endpoint(s.ruleRevisions, "id"))
	s.router.GET("/api/v1/rules/:id/activations", s.authorize("viewer"), s.endpoint(s.ruleActivations, "id"))
	s.router.POST("/api/v1/rules/:id/publish", s.authorize("operator"), s.endpoint(s.publishRuleRevision, "id"))
}

func (s *Server) publishRuleRevision(w http.ResponseWriter, r *http.Request) {
	if limited(r.Context()) {
		problem(w, http.StatusForbidden, "发布规则需要当前全设备管理范围")
		return
	}
	var q struct {
		Rule                    model.AlarmRule `json:"rule"`
		ExpectedBaselineVersion *int            `json:"expectedBaselineVersion"`
		Reason                  string          `json:"reason"`
		ExperimentID            string          `json:"experimentId"`
		RollbackFrom            string          `json:"rollbackFrom,omitempty"`
	}
	if decode(w, r, &q) != nil {
		return
	}
	if q.ExpectedBaselineVersion == nil || *q.ExpectedBaselineVersion < 0 || strings.TrimSpace(q.Reason) == "" || len(q.Reason) > 2000 || q.ExperimentID == "" {
		problem(w, 422, "发布需要固定实验引用、expectedBaselineVersion及原因")
		return
	}
	current := claims(r)
	q.Rule.ID = r.PathValue("id")
	q.Rule.TenantID = current.TenantID
	if s.rulelab == nil {
		analysisProblem(w, model.ErrAnalysisInvalid)
		return
	}
	if err := s.rulelab.ValidatePublication(r.Context(), analysisActor(r), q.ExperimentID, q.Rule, *q.ExpectedBaselineVersion); err != nil {
		if errors.Is(err, model.ErrRuleConflict) {
			problem(w, 409, err.Error())
			return
		}
		analysisProblem(w, err)
		return
	}
	_, conflicts, err := s.engine.ValidateRuleDraft(r.Context(), q.Rule)
	if err != nil {
		problem(w, 422, err.Error())
		return
	}
	if len(conflicts) > 0 && !strings.EqualFold(r.URL.Query().Get("confirmConflicts"), "true") {
		write(w, 409, map[string]any{"type": "rule-conflict", "detail": "rule conflicts require explicit confirmation", "conflicts": conflicts})
		return
	}
	revision, err := s.engine.PublishRule(r.Context(), model.RulePublishRequest{Rule: q.Rule, ExpectedBaselineVersion: *q.ExpectedBaselineVersion, Reason: q.Reason, Actor: current.Username, ExperimentID: q.ExperimentID, RollbackFrom: q.RollbackFrom})
	if errors.Is(err, model.ErrRuleConflict) {
		problem(w, 409, err.Error())
		return
	}
	if err != nil {
		if errors.Is(err, model.ErrRuleHistoryInvalid) {
			problem(w, 422, "回滚引用与发布候选不一致")
			return
		}
		problem(w, 500, err.Error())
		return
	}
	s.audit(r, "rule.publish", "rule", q.Rule.ID, map[string]any{"revisionId": revision.ID, "version": revision.Version, "experimentId": q.ExperimentID, "reason": q.Reason})
	write(w, 201, revision)
}

func (s *Server) ruleRevisions(w http.ResponseWriter, r *http.Request) {
	if limited(r.Context()) {
		problem(w, http.StatusForbidden, "规则历史需要当前全设备管理范围")
		return
	}
	page := parseListPagination(r)
	rows, total, err := s.engine.Repo.ListRuleRevisions(r.Context(), claims(r).TenantID, r.PathValue("id"), page.PageSize, page.Offset)
	if err != nil {
		problem(w, 500, err.Error())
		return
	}
	if total == 0 {
		problem(w, 404, "rule not found")
		return
	}
	writeList(w, 200, rows, total, page, nil)
}
func (s *Server) ruleActivations(w http.ResponseWriter, r *http.Request) {
	if limited(r.Context()) {
		problem(w, http.StatusForbidden, "规则生效历史需要当前全设备管理范围")
		return
	}
	page := parseListPagination(r)
	rows, total, err := s.engine.Repo.ListRuleActivations(r.Context(), claims(r).TenantID, r.PathValue("id"), page.PageSize, page.Offset)
	if err != nil {
		problem(w, 500, err.Error())
		return
	}
	if total == 0 {
		problem(w, 404, "rule not found")
		return
	}
	writeList(w, 200, rows, total, page, nil)
}
