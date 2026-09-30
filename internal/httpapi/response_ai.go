package httpapi

import (
	"encoding/json"
	"net/http"

	"iot-platform/internal/analytics"
	"iot-platform/internal/analytics/response"
	"iot-platform/internal/model"
)

func (s *Server) responseAIRoutes() {
	p := "/api/v1/response-runs/:id/ai-jobs"
	s.router.POST(p, s.authorize("viewer"), s.endpoint(s.responseAI(s.analysisAICreate(analytics.KindResponse)), "id"))
	s.router.GET(p, s.authorize("viewer"), s.endpoint(s.responseAI(s.analysisAIList(analytics.KindResponse)), "id"))
	s.router.GET(p+"/:jobId", s.authorize("viewer"), s.endpoint(s.responseAI(s.analysisAIGet(analytics.KindResponse)), "id", "jobId"))
	s.router.POST(p+"/:jobId/stop", s.authorize("viewer"), s.endpoint(s.responseAI(s.analysisAIStop(analytics.KindResponse)), "id", "jobId"))
}

// A business execution owns many immutable evaluation runs. Explicit runId and
// each historical job resolve their fixed source, rather than today's latest.
func (s *Server) responseAI(next endpointHandler) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		a, resource := analysisActor(r), r.PathValue("id")
		svc := s.responseService()
		if _, err := svc.Latest(r.Context(), a, response.ExecutionKind, resource); err != nil {
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
		var err error
		if runID != "" {
			run, err = s.analysis.Get(r.Context(), a, analytics.KindResponse, runID)
		} else {
			for offset := 0; ; {
				var page []model.AnalysisRun
				var total int
				page, total, err = svc.EvaluationRuns(r.Context(), a, resource, model.AnalysisFilter{Limit: 100, Offset: offset})
				if err != nil {
					break
				}
				for _, candidate := range page {
					if candidate.SnapshotID != "" && (candidate.Status == model.AnalysisSucceeded || candidate.Status == model.AnalysisPartial) && (candidate.CreatedAt > run.CreatedAt || candidate.CreatedAt == run.CreatedAt && candidate.ID > run.ID) {
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
		var parameters response.EvaluationParameters
		if json.Unmarshal(run.Parameters, &parameters) != nil {
			analysisProblem(w, model.ErrAnalysisInvalid)
			return
		}
		source, err := svc.Revision(r.Context(), a, response.ExecutionKind, parameters.ExecutionRevisionID)
		if err != nil {
			analysisProblem(w, err)
			return
		}
		if source.ResourceID != resource || source.Hash != run.ConfigurationVersion || run.SnapshotID == "" || run.Status != model.AnalysisSucceeded && run.Status != model.AnalysisPartial {
			analysisProblem(w, model.ErrNotFound)
			return
		}
		r.SetPathValue("id", run.ID)
		next(w, r)
	}
}
