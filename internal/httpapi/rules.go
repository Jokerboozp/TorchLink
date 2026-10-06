package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"iot-platform/internal/core"
	"iot-platform/internal/model"
)

func (s *Server) rules(w http.ResponseWriter, r *http.Request) {
	pagination := parseListPagination(r)
	v, total, err := s.engine.Repo.ListRulesPage(r.Context(), claims(r).TenantID, pagination.PageSize, pagination.Offset)
	if err != nil {
		s.fail(w, r, err, "")
		return
	}
	writeList(w, 200, v, total, pagination, nil)
}

// ruleFields lists the thing-model fields of a product for the rule editor.
func (s *Server) ruleFields(w http.ResponseWriter, r *http.Request) {
	productID := strings.TrimSpace(r.URL.Query().Get("productId"))
	if productID == "" {
		problem(w, http.StatusUnprocessableEntity, "请选择设备模板")
		return
	}
	product, err := s.engine.Repo.GetProduct(r.Context(), claims(r).TenantID, productID)
	if err != nil {
		problem(w, http.StatusNotFound, "设备模板不存在")
		return
	}
	write(w, http.StatusOK, map[string]any{"items": core.RuleFields(product)})
}

func (s *Server) saveRule(w http.ResponseWriter, r *http.Request) {
	var v model.AlarmRule
	if decode(w, r, &v) != nil {
		return
	}
	c := claims(r)
	v.TenantID = c.TenantID
	status := http.StatusCreated
	wasEnabled := false
	if id := r.PathValue("id"); id != "" {
		status = http.StatusOK
		v.ID = id
		items, err := s.engine.Repo.ListRules(r.Context(), c.TenantID)
		if err != nil {
			s.fail(w, r, err, "")
			return
		}
		found := false
		for _, current := range items {
			if current.ID == id {
				wasEnabled = current.Enabled
				v.CreatedAt = current.CreatedAt
				v.Version = current.Version + 1
				found = true
				break
			}
		}
		if !found {
			problem(w, 404, "rule not found")
			return
		}
	} else if v.ID == "" {
		v.ID = fmt.Sprintf("rule_%d", time.Now().UnixNano())
	}
	if v.Version == 0 {
		v.Version = 1
	}
	now := time.Now().UnixMilli()
	if v.CreatedAt == 0 {
		v.CreatedAt = now
	}
	v.UpdatedAt = now
	if len(v.Conditions) == 0 && strings.TrimSpace(v.Expression) == "" {
		problem(w, 422, "at least one condition or a Gengine expression is required")
		return
	}
	if v.Expression != "" {
		if err := core.ValidateGengineExpression(v.Expression); err != nil {
			problem(w, 422, err.Error())
			return
		}
	}
	_, conflicts, validationErr := s.engine.ValidateRuleDraft(r.Context(), v)
	if validationErr != nil {
		problem(w, 422, validationErr.Error())
		return
	}
	if len(conflicts) > 0 && !strings.EqualFold(r.URL.Query().Get("confirmConflicts"), "true") {
		write(w, 409, map[string]any{"type": "rule-conflict", "detail": "rule conflicts require explicit confirmation", "conflicts": conflicts})
		return
	}
	if status == http.StatusOK && wasEnabled && !v.Enabled {
		if err := s.engine.DisableRule(r.Context(), c.TenantID, v.ID); err != nil {
			s.fail(w, r, err, "")
			return
		}
	}
	if err := s.engine.Repo.SaveRule(r.Context(), v); err != nil {
		s.fail(w, r, err, "")
		return
	}
	s.engine.RulesChanged(c.TenantID)
	s.audit(r, "rule.save", "rule", v.ID, map[string]any{"version": v.Version, "enabled": v.Enabled})
	write(w, status, v)
}

func (s *Server) deleteRule(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.engine.DeleteRule(r.Context(), claims(r).TenantID, id); err != nil {
		problem(w, 404, "rule not found")
		return
	}
	s.audit(r, "rule.delete", "rule", id, nil)
	write(w, 200, map[string]any{"deleted": true, "id": id})
}
