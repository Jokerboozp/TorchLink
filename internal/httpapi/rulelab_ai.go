package httpapi

import (
	"encoding/json"
	"net/http"

	"iot-platform/internal/analytics"
	"iot-platform/internal/model"
)

func (s *Server) ruleLabAIRoutes() {
	p := "/api/v1/rule-lab/experiments/:id/ai-jobs"
	s.router.POST(p, s.authorize("viewer"), s.endpoint(s.ruleLabAI(s.analysisAICreate(analytics.KindRuleLab)), "id"))
	s.router.GET(p, s.authorize("viewer"), s.endpoint(s.ruleLabAI(s.analysisAIList(analytics.KindRuleLab)), "id"))
	s.router.GET(p+"/:jobId", s.authorize("viewer"), s.endpoint(s.ruleLabAI(s.analysisAIGet(analytics.KindRuleLab)), "id", "jobId"))
	s.router.POST(p+"/:jobId/stop", s.authorize("viewer"), s.endpoint(s.ruleLabAI(s.analysisAIStop(analytics.KindRuleLab)), "id", "jobId"))
}

// The public resource is the immutable experiment revision; the shared worker
// runs against one of that revision's completed fixed snapshots. A historical
// job resolves its own run, so creating another run never hides its full body.
func (s *Server) ruleLabAI(next endpointHandler) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		id, a := r.PathValue("id"), analysisActor(r)
		if _, err := s.rulelab.GetConfig(r.Context(), a, "experiments", id); err != nil {
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
		if runID == "" {
			run, err = s.rulelab.GetExperimentCompletedRun(r.Context(), a, id)
		} else {
			run, err = s.analysis.Get(r.Context(), a, analytics.KindRuleLab, runID)
		}
		if err != nil {
			analysisProblem(w, err)
			return
		}
		var p model.RuleLabRunParameters
		if json.Unmarshal(run.Parameters, &p) != nil || p.Phase != "EXPERIMENT" || p.ExperimentRevisionID != id || run.SnapshotID == "" || (run.Status != model.AnalysisSucceeded && run.Status != model.AnalysisPartial) {
			analysisProblem(w, model.ErrNotFound)
			return
		}
		r.SetPathValue("id", run.ID)
		next(w, r)
	}
}
