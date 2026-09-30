package httpapi

import (
	"iot-platform/internal/model"
	"net/http"
	"strings"
)

func (s *Server) ruleLabRoutes() {
	prefix := "/api/v1/rule-lab"
	s.router.GET(prefix+"/rule-sources", s.authorize("viewer"), s.endpoint(s.ruleLabRuleSources))
	s.router.GET(prefix+"/datasets", s.authorize("viewer"), s.endpoint(s.ruleLabDatasets))
	s.router.POST(prefix+"/datasets", s.authorize("viewer"), s.endpoint(s.ruleLabDatasetCreate(false)))
	s.router.POST(prefix+"/datasets/publish", s.authorize("viewer"), s.endpoint(s.ruleLabDatasetCreate(true)))
	s.router.GET(prefix+"/datasets/:id", s.authorize("viewer"), s.endpoint(s.ruleLabDataset, "id"))
	s.router.GET(prefix+"/datasets/:id/inputs", s.authorize("viewer"), s.endpoint(s.ruleLabInputs, "id"))
	for _, resource := range []string{"experiments", "labels"} {
		s.router.GET(prefix+"/"+resource, s.authorize("viewer"), s.endpoint(s.ruleLabConfigs(resource)))
		s.router.POST(prefix+"/"+resource, s.authorize("viewer"), s.endpoint(s.ruleLabSaveConfig(resource, false)))
		s.router.POST(prefix+"/"+resource+"/publish", s.authorize("viewer"), s.endpoint(s.ruleLabSaveConfig(resource, true)))
		s.router.GET(prefix+"/"+resource+"/:id", s.authorize("viewer"), s.endpoint(s.ruleLabConfig(resource), "id"))
	}
	s.router.POST(prefix+"/labels/:id/confirm", s.authorize("viewer"), s.endpoint(s.ruleLabLabelConfirm, "id"))
	s.router.POST(prefix+"/experiments/:id/runs", s.authorize("viewer"), s.endpoint(s.ruleLabRunCreate, "id"))
	s.router.GET(prefix+"/experiments/:id/runs", s.authorize("viewer"), s.endpoint(s.ruleLabExperimentRuns, "id"))
	s.router.GET(prefix+"/experiments/:id/report", s.authorize("viewer"), s.endpoint(s.ruleLabReport, "id"))
	s.router.POST(prefix+"/findings/:id/reviews", s.authorize("viewer"), s.endpoint(s.ruleLabReview, "id"))
	s.router.GET(prefix+"/findings/:id/reviews", s.authorize("viewer"), s.endpoint(s.ruleLabReviews(true), "id"))
	s.router.GET(prefix+"/runs/:id/reviews", s.authorize("viewer"), s.endpoint(s.ruleLabReviews(false), "id"))
}
func (s *Server) ruleLabDatasets(w http.ResponseWriter, r *http.Request) {
	items, total, err := s.rulelab.ListDatasets(r.Context(), analysisActor(r), analysisPage(r))
	if err != nil {
		analysisProblem(w, err)
		return
	}
	write(w, 200, map[string]any{"items": items, "total": total})
}
func (s *Server) ruleLabDatasetCreate(shared bool) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		var q model.RuleLabDatasetRequest
		if decode(w, r, &q) != nil {
			return
		}
		if shared {
			q.Scope = "SHARED"
		}
		if q.IdempotencyKey == "" {
			q.IdempotencyKey = r.Header.Get("Idempotency-Key")
		}
		value, err := s.rulelab.CreateDataset(r.Context(), analysisActor(r), q)
		if err != nil {
			analysisProblem(w, err)
			return
		}
		write(w, 202, value)
	}
}
func (s *Server) ruleLabDataset(w http.ResponseWriter, r *http.Request) {
	value, err := s.rulelab.Dataset(r.Context(), analysisActor(r), r.PathValue("id"))
	if err != nil {
		analysisProblem(w, err)
		return
	}
	write(w, 200, value)
}
func (s *Server) ruleLabInputs(w http.ResponseWriter, r *http.Request) {
	items, total, err := s.rulelab.DatasetInputs(r.Context(), analysisActor(r), r.PathValue("id"), analysisPage(r))
	if err != nil {
		analysisProblem(w, err)
		return
	}
	write(w, 200, map[string]any{"items": items, "total": total})
}
func (s *Server) ruleLabConfigs(resource string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		items, total, err := s.rulelab.ListConfigs(r.Context(), analysisActor(r), resource, analysisPage(r))
		if err != nil {
			analysisProblem(w, err)
			return
		}
		write(w, 200, map[string]any{"items": items, "total": total})
	}
}
func (s *Server) ruleLabConfig(resource string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		value, err := s.rulelab.GetConfig(r.Context(), analysisActor(r), resource, r.PathValue("id"))
		if err != nil {
			analysisProblem(w, err)
			return
		}
		write(w, 200, value)
	}
}
func (s *Server) ruleLabSaveConfig(resource string, shared bool) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		var q model.RuleLabConfigRequest
		if decode(w, r, &q) != nil {
			return
		}
		if shared {
			q.Scope = "SHARED"
		}
		var value model.AnalysisConfigRevision
		var err error
		if resource == "experiments" {
			value, err = s.rulelab.SaveExperiment(r.Context(), analysisActor(r), q)
		} else {
			value, err = s.rulelab.SaveLabel(r.Context(), analysisActor(r), q)
		}
		if err != nil {
			analysisProblem(w, err)
			return
		}
		write(w, 201, value)
	}
}
func (s *Server) ruleLabLabelConfirm(w http.ResponseWriter, r *http.Request) {
	var q struct {
		ExpectedVersion int64 `json:"expectedVersion"`
	}
	if decode(w, r, &q) != nil {
		return
	}
	value, err := s.rulelab.ConfirmLabel(r.Context(), analysisActor(r), r.PathValue("id"), q.ExpectedVersion)
	if err != nil {
		analysisProblem(w, err)
		return
	}
	write(w, 201, value)
}
func (s *Server) ruleLabRunCreate(w http.ResponseWriter, r *http.Request) {
	var q model.RuleLabRunRequest
	if decode(w, r, &q) != nil {
		return
	}
	if q.IdempotencyKey == "" {
		q.IdempotencyKey = r.Header.Get("Idempotency-Key")
	}
	value, err := s.rulelab.CreateExperimentRun(r.Context(), analysisActor(r), r.PathValue("id"), q)
	if err != nil {
		analysisProblem(w, err)
		return
	}
	write(w, 202, publicAnalysisRun(value))
}
func (s *Server) ruleLabExperimentRuns(w http.ResponseWriter, r *http.Request) {
	items, total, err := s.rulelab.ExperimentRuns(r.Context(), analysisActor(r), r.PathValue("id"), analysisPage(r))
	if err != nil {
		analysisProblem(w, err)
		return
	}
	write(w, 200, map[string]any{"items": items, "total": total})
}
func (s *Server) ruleLabReport(w http.ResponseWriter, r *http.Request) {
	value, err := s.rulelab.Report(r.Context(), analysisActor(r), r.PathValue("id"), r.URL.Query().Get("runId"))
	if err != nil {
		analysisProblem(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=rule-lab-report.json")
	w.WriteHeader(200)
	_, _ = w.Write(value)
}
func (s *Server) ruleLabReview(w http.ResponseWriter, r *http.Request) {
	var q model.QualityReviewRequest
	if decode(w, r, &q) != nil {
		return
	}
	value, err := s.rulelab.Review(r.Context(), analysisActor(r), r.PathValue("id"), q)
	if err != nil {
		analysisProblem(w, err)
		return
	}
	write(w, 201, value)
}
func (s *Server) ruleLabReviews(finding bool) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		run, id := r.PathValue("id"), ""
		if finding {
			run = r.URL.Query().Get("runId")
			id = r.PathValue("id")
		}
		items, total, err := s.rulelab.Reviews(r.Context(), analysisActor(r), run, id, analysisPage(r))
		if err != nil {
			analysisProblem(w, err)
			return
		}
		write(w, 200, map[string]any{"items": items, "total": total})
	}
}

func (s *Server) ruleLabRuleSources(w http.ResponseWriter, r *http.Request) {
	ids := r.URL.Query()["deviceId"]
	for _, list := range r.URL.Query()["deviceIds"] {
		ids = append(ids, strings.Split(list, ",")...)
	}
	values, total, err := s.rulelab.RuleSources(r.Context(), analysisActor(r), ids, analysisPage(r))
	if err != nil {
		analysisProblem(w, err)
		return
	}
	write(w, 200, map[string]any{"items": values, "total": total})
}
