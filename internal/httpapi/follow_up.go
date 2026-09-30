package httpapi

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"iot-platform/internal/analytics"
	"iot-platform/internal/analytics/maintenance"
	"iot-platform/internal/analytics/response"
	"iot-platform/internal/model"
)

// A follow-up catalog reads the current business objects. It does not own a
// second action lifecycle or treat completion of a duty item as verification.
type followUpSource struct {
	SourceKind   string   `json:"sourceKind"`
	ResourceID   string   `json:"resourceId"`
	Version      int64    `json:"version"`
	Title        string   `json:"title"`
	Status       string   `json:"status"`
	Owner        string   `json:"owner"`
	DueAt        int64    `json:"dueAt"`
	DeviceIDs    []string `json:"deviceIds"`
	Verification string   `json:"verification,omitempty"`
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
	requested := r.URL.Query().Get("sourceKind")
	if requested != "" && requested != response.CorrectiveKind && requested != maintenance.InterventionKind {
		analysisProblem(w, model.ErrAnalysisInvalid)
		return
	}
	rows := []followUpSource{}
	eligible := false
	for _, kind := range []string{response.CorrectiveKind, maintenance.InterventionKind} {
		if requested != "" && requested != kind {
			continue
		}
		menu := "menu:response"
		if kind == maintenance.InterventionKind {
			menu = "menu:maintenance"
		}
		if !slices.Contains(a.Permissions, "*") && !slices.Contains(a.Permissions, menu) {
			continue
		}
		eligible = true
		for offset := 0; ; {
			var page []model.AnalysisConfigRevision
			var total int
			if kind == response.CorrectiveKind {
				page, total, err = s.responseService().List(r.Context(), a, kind, model.AnalysisFilter{Limit: 100, Offset: offset})
			} else {
				page, total, err = s.maintenanceService().List(r.Context(), a, kind, model.AnalysisFilter{Limit: 100, Offset: offset})
			}
			if err != nil {
				analysisProblem(w, err)
				return
			}
			for _, fixed := range page {
				row := followUpSource{SourceKind: kind, ResourceID: fixed.ResourceID, Version: fixed.Version, DeviceIDs: slices.Clone(fixed.DeviceIDs)}
				if kind == response.CorrectiveKind {
					var body model.CorrectiveAction
					if json.Unmarshal(fixed.Body, &body) != nil {
						analysisProblem(w, model.ErrAnalysisInvalid)
						return
					}
					row.Title, row.Status, row.Owner, row.DueAt = body.Title, body.Status, body.Owner, body.DueAt
				} else {
					var body model.MaintenanceIntervention
					if json.Unmarshal(fixed.Body, &body) != nil {
						analysisProblem(w, model.ErrAnalysisInvalid)
						return
					}
					row.Title, row.Status, row.Verification = body.Name, body.Status, "UNKNOWN"
					// Participants may include external personnel; their order
					// does not establish a single business owner.
					if len(body.Verifications) > 0 {
						row.Verification = body.Verifications[len(body.Verifications)-1].Result
					}
				}
				if status := r.URL.Query().Get("status"); status != "" && row.Status != status {
					continue
				}
				if owner := r.URL.Query().Get("owner"); owner != "" && row.Owner != owner {
					continue
				}
				rows = append(rows, row)
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
	}
	if !eligible {
		analysisProblem(w, analytics.ErrForbidden)
		return
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
		app := analytics.KindResponse
		if row.SourceKind == maintenance.InterventionKind {
			app = analytics.KindMaintenance
		}
		if !current.Allows(app, "", row.DeviceIDs) {
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
