package httpapi

import (
	"encoding/json"
	"net/http"

	"iot-platform/internal/analytics"
	"iot-platform/internal/analytics/maintenance"
	"iot-platform/internal/model"
)

func (s *Server) investmentAIRoutes() {
	p := "/api/v1/investment-scenarios/:id/ai-jobs"
	s.router.POST(p, s.authorize("viewer"), s.endpoint(s.investmentAI(s.analysisAICreate(analytics.KindInvestment)), "id"))
	s.router.GET(p, s.authorize("viewer"), s.endpoint(s.investmentAI(s.analysisAIList(analytics.KindInvestment)), "id"))
	s.router.GET(p+"/:jobId", s.authorize("viewer"), s.endpoint(s.investmentAI(s.analysisAIGet(analytics.KindInvestment)), "id", "jobId"))
	s.router.POST(p+"/:jobId/stop", s.authorize("viewer"), s.endpoint(s.investmentAI(s.analysisAIStop(analytics.KindInvestment)), "id", "jobId"))
}

func (s *Server) investmentAI(next endpointHandler) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		a, resource := analysisActor(r), r.PathValue("id")
		svc := maintenance.NewService(s.analysis, s.analysisFacts)
		latest, err := svc.Latest(r.Context(), a, maintenance.ScenarioKind, resource)
		if err != nil {
			analysisProblem(w, err)
			return
		}
		runID := r.URL.Query().Get("runId")
		if jobID := r.PathValue("jobId"); jobID != "" {
			job, err := s.analysis.Store.GetAnalysisAIRevision(r.Context(), a.TenantID, jobID)
			if err != nil {
				analysisProblem(w, err)
				return
			}
			runID = job.RunID
		}
		var run model.AnalysisRun
		if runID != "" {
			run, err = s.analysis.Get(r.Context(), a, analytics.KindInvestment, runID)
		} else {
			for offset := 0; ; {
				var page []model.AnalysisRun
				var total int
				page, total, err = s.analysis.List(r.Context(), a, analytics.KindInvestment, model.AnalysisFilter{Limit: 100, Offset: offset, Statuses: []string{model.AnalysisSucceeded, model.AnalysisPartial}})
				if err != nil {
					break
				}
				for _, candidate := range page {
					var p struct {
						ScenarioRevisionID string `json:"scenarioRevisionId"`
					}
					if json.Unmarshal(candidate.Parameters, &p) != nil || p.ScenarioRevisionID != latest.ID || candidate.SnapshotID == "" {
						continue
					}
					if candidate.CreatedAt > run.CreatedAt || candidate.CreatedAt == run.CreatedAt && candidate.ID > run.ID {
						run = candidate
					}
				}
				offset += len(page)
				if offset >= total {
					break
				}
				if len(page) == 0 || offset > 10000 {
					err = model.ErrAnalysisInvalid
					break
				}
			}
			if err == nil && run.ID == "" {
				err = model.ErrNotFound
			}
		}
		if err != nil {
			analysisProblem(w, err)
			return
		}
		var p struct {
			ScenarioRevisionID string `json:"scenarioRevisionId"`
		}
		if json.Unmarshal(run.Parameters, &p) != nil {
			analysisProblem(w, model.ErrAnalysisInvalid)
			return
		}
		source, err := svc.Revision(r.Context(), a, maintenance.ScenarioKind, p.ScenarioRevisionID)
		if err != nil {
			analysisProblem(w, err)
			return
		}
		if source.ResourceID != resource || run.SnapshotID == "" || run.Status != model.AnalysisSucceeded && run.Status != model.AnalysisPartial {
			analysisProblem(w, model.ErrNotFound)
			return
		}
		r.SetPathValue("id", run.ID)
		next(w, r)
	}
}
