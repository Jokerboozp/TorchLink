package httpapi

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"iot-platform/internal/analytics"
	"iot-platform/internal/analytics/recurring"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func (s *Server) SetGovernanceHistoricalArchive(reader ports.GovernanceHistoricalArchiveReader) {
	if s.recurring != nil {
		s.recurring.HistoricalArchive = reader
	}
}

func isHistoricalProjection(run model.AnalysisRun) bool {
	var p recurring.Parameters
	return json.Unmarshal(run.Parameters, &p) == nil && p.JobMode == recurring.HistoricalProjectionMode
}

func (s *Server) governanceProjectionRoutes() {
	base := "/api/v1/alarm-governance/historical-projections"
	s.router.POST(base, s.authorize("viewer"), s.endpoint(s.governanceProjectionCreate))
	s.router.GET(base, s.authorize("viewer"), s.endpoint(s.governanceProjectionList))
	s.router.GET(base+"/:id", s.authorize("viewer"), s.endpoint(s.governanceProjectionRead("run"), "id"))
	s.router.POST(base+"/:id/stop", s.authorize("viewer"), s.endpoint(s.governanceProjectionRead("stop"), "id"))
	s.router.GET(base+"/:id/snapshot", s.authorize("viewer"), s.endpoint(s.governanceProjectionRead("snapshot"), "id"))
	s.router.GET(base+"/:id/observations", s.authorize("viewer"), s.endpoint(s.governanceProjectionRead("observations"), "id"))
}

func (s *Server) governanceProjectionCreate(w http.ResponseWriter, r *http.Request) {
	var q analytics.CreateRequest
	if decode(w, r, &q) != nil {
		return
	}
	if q.IdempotencyKey == "" {
		q.IdempotencyKey = r.Header.Get("Idempotency-Key")
	}
	p := recurring.Parameters{TimeBasis: "EVENT_AT"}
	if len(q.Parameters) != 0 {
		// Preserve strict unknown-field validation in recurring.ValidateCreate.
		var fields map[string]json.RawMessage
		if json.Unmarshal(q.Parameters, &fields) != nil {
			analysisProblem(w, model.ErrAnalysisInvalid)
			return
		}
		if mode, ok := fields["jobMode"]; ok && string(mode) != `"HISTORICAL_PROJECTION"` {
			analysisProblem(w, model.ErrAnalysisInvalid)
			return
		}
		fields["jobMode"], _ = json.Marshal(recurring.HistoricalProjectionMode)
		q.Parameters, _ = json.Marshal(fields)
	} else {
		p.JobMode = recurring.HistoricalProjectionMode
		q.Parameters, _ = json.Marshal(p)
	}
	if err := s.recurring.ValidateCreate(r.Context(), analysisActor(r), &q); err != nil {
		analysisProblem(w, err)
		return
	}
	run, err := s.analysis.Create(r.Context(), analysisActor(r), analytics.KindRecurring, recurring.ProjectionAlgorithmVersion, q)
	if err != nil {
		analysisProblem(w, err)
		return
	}
	write(w, 202, publicAnalysisRun(run))
}

func (s *Server) governanceProjectionList(w http.ResponseWriter, r *http.Request) {
	f := analysisPage(r)
	f.Limit = min(100, max(1, f.Limit))
	f.Offset = max(0, f.Offset)
	if status := r.URL.Query().Get("status"); status != "" {
		f.Statuses = []string{status}
	}
	requested := []string{}
	if value := r.URL.Query().Get("deviceIds"); value != "" {
		requested = strings.Split(value, ",")
	}
	if len(requested) > s.analysis.Limits.MaxDevices {
		analysisProblem(w, model.ErrAnalysisInvalid)
		return
	}
	start, end := governanceInt(r.URL.Query().Get("start"), 0), governanceInt(r.URL.Query().Get("end"), 0)
	all := []model.AnalysisRun{}
	// The shared service authorizes every candidate before exposing a total.
	// Apply the mode filter after that check; never leak another task's count.
	pageFilter := f
	pageFilter.Offset, pageFilter.Limit = 0, 100
	for {
		items, total, err := s.analysis.List(r.Context(), analysisActor(r), analytics.KindRecurring, pageFilter)
		if err != nil {
			analysisProblem(w, err)
			return
		}
		for _, run := range items {
			if !isHistoricalProjection(run) || start > 0 && run.End <= start || end > 0 && run.Start >= end {
				continue
			}
			match := len(requested) == 0
			for _, id := range requested {
				if slices.Contains(run.DeviceIDs, id) {
					match = true
				}
			}
			if match {
				all = append(all, publicAnalysisRun(run))
			}
		}
		pageFilter.Offset += len(items)
		if pageFilter.Offset >= total || len(items) == 0 {
			break
		}
	}
	at := min(f.Offset, len(all))
	stop := min(at+f.Limit, len(all))
	write(w, 200, map[string]any{"items": all[at:stop], "total": len(all), "offset": f.Offset, "limit": f.Limit})
}

func (s *Server) governanceProjectionRead(operation string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := analysisActor(r)
		run, err := s.analysis.Get(r.Context(), actor, analytics.KindRecurring, r.PathValue("id"))
		if err != nil {
			analysisProblem(w, err)
			return
		}
		if !isHistoricalProjection(run) {
			analysisProblem(w, model.ErrNotFound)
			return
		}
		switch operation {
		case "run":
			write(w, 200, publicAnalysisRun(run))
		case "stop":
			s.analysisStop(analytics.KindRecurring)(w, r)
		case "snapshot":
			s.analysisSnapshot(analytics.KindRecurring)(w, r)
		case "observations":
			f := analysisPage(r)
			f.Kind = "projection-observations"
			f.Limit, f.Offset = min(100, max(1, f.Limit)), max(0, f.Offset)
			items, total, err := s.analysis.Outputs(r.Context(), actor, analytics.KindRecurring, run.ID, f)
			if err != nil {
				analysisProblem(w, err)
				return
			}
			write(w, 200, map[string]any{"items": items, "total": total, "limit": f.Limit, "offset": f.Offset})
		}
	}
}
