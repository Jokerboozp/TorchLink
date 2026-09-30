package httpapi

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"iot-platform/internal/analytics"
	"iot-platform/internal/analytics/response"
	"iot-platform/internal/model"
)

// A follow-up catalog reads the current business objects. It does not own a
// second action lifecycle or treat completion of a duty item as verification.
type followUpSource struct {
	SourceKind string   `json:"sourceKind"`
	ResourceID string   `json:"resourceId"`
	Version    int64    `json:"version"`
	Title      string   `json:"title"`
	Status     string   `json:"status"`
	Owner      string   `json:"owner"`
	DueAt      int64    `json:"dueAt"`
	DeviceIDs  []string `json:"deviceIds"`
}

func (s *Server) followUpRoutes() {
	s.router.GET("/api/v1/follow-up-sources", s.authorize("viewer"), s.endpoint(s.followUpSources))
}

func (s *Server) followUpSources(w http.ResponseWriter, r *http.Request) {
	a, err := s.analysis.Current(r.Context(), analysisActor(r))
	if err != nil {
		analysisProblem(w, err)
		return
	}
	if kind := r.URL.Query().Get("sourceKind"); kind != "" && kind != response.CorrectiveKind {
		analysisProblem(w, model.ErrAnalysisInvalid)
		return
	}
	if !slices.Contains(a.Permissions, "*") && !slices.Contains(a.Permissions, "menu:response") {
		analysisProblem(w, analytics.ErrForbidden)
		return
	}
	rows := []followUpSource{}
	for offset := 0; ; {
		page, total, err := s.responseService().List(r.Context(), a, response.CorrectiveKind, model.AnalysisFilter{Limit: 100, Offset: offset})
		if err != nil {
			analysisProblem(w, err)
			return
		}
		for _, fixed := range page {
			var body model.CorrectiveAction
			if json.Unmarshal(fixed.Body, &body) != nil {
				analysisProblem(w, model.ErrAnalysisInvalid)
				return
			}
			if status := r.URL.Query().Get("status"); status != "" && body.Status != status {
				continue
			}
			if owner := r.URL.Query().Get("owner"); owner != "" && body.Owner != owner {
				continue
			}
			rows = append(rows, followUpSource{SourceKind: response.CorrectiveKind, ResourceID: fixed.ResourceID, Version: fixed.Version, Title: body.Title, Status: body.Status, Owner: body.Owner, DueAt: body.DueAt, DeviceIDs: slices.Clone(fixed.DeviceIDs)})
		}
		offset += len(page)
		if offset >= total {
			break
		}
		if offset >= 10000 || len(page) == 0 {
			analysisProblem(w, model.ErrAnalysisInvalid)
			return
		}
	}
	// One stable order and one visible denominator, independent of pagination.
	slices.SortFunc(rows, func(a, b followUpSource) int {
		return strings.Compare(a.SourceKind+"/"+a.ResourceID, b.SourceKind+"/"+b.ResourceID)
	})
	current, err := s.analysis.Current(r.Context(), analysisActor(r))
	if err != nil {
		analysisProblem(w, err)
		return
	}
	for _, row := range rows {
		if !current.Allows(analytics.KindResponse, "", row.DeviceIDs) {
			analysisProblem(w, analytics.ErrForbidden)
			return
		}
	}
	page := analysisPage(r)
	limit, offset := analytics.NormalizeAnalysisPage(page.Limit, page.Offset)
	items := []followUpSource{}
	if offset < len(rows) {
		items = rows[offset:min(len(rows), offset+limit)]
	}
	write(w, 200, map[string]any{"items": items, "total": len(rows)})
}
