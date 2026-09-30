package httpapi

import (
	"iot-platform/internal/model"
	"net/http"
	"strings"
)

func (s *Server) monitoringRoutes() {
	for _, resource := range []string{"profiles", "observations"} {
		base := "/api/v1/monitoring-gaps/" + resource
		s.router.GET(base, s.authorize("viewer"), s.endpoint(s.monitoringConfigs(resource)))
		s.router.POST(base, s.authorize("viewer"), s.endpoint(s.monitoringSave(resource)))
	}
	s.router.POST("/api/v1/monitoring-gaps/profiles/publish", s.authorize("viewer"), s.endpoint(s.monitoringSave("profiles")))
	s.router.POST("/api/v1/monitoring-gaps/observations/:id/confirm", s.authorize("viewer"), s.endpoint(s.monitoringConfirm, "id"))
	s.router.POST("/api/v1/monitoring-gaps/runs/:id/hypotheses", s.authorize("viewer"), s.endpoint(s.monitoringHypothesis, "id"))
	s.router.GET("/api/v1/monitoring-gaps/runs/:id/hypotheses", s.authorize("viewer"), s.endpoint(s.monitoringHypotheses, "id"))
	s.router.POST("/api/v1/monitoring-gaps/findings/:id/reviews", s.authorize("viewer"), s.endpoint(s.monitoringReview, "id"))
	s.router.GET("/api/v1/monitoring-gaps/findings/:id/reviews", s.authorize("viewer"), s.endpoint(s.monitoringReviews, "id"))
	s.router.GET("/api/v1/monitoring-gaps/runs/:id/reviews", s.authorize("viewer"), s.endpoint(s.monitoringRunReviews, "id"))
	s.router.GET("/api/v1/monitoring-gaps/runs/:id/export", s.authorize("viewer"), s.endpoint(s.monitoringExport, "id"))
}
func (s *Server) monitoringConfigs(resource string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		items, total, err := s.monitoring.ListConfigs(r.Context(), analysisActor(r), resource, analysisPage(r))
		if err != nil {
			analysisProblem(w, err)
			return
		}
		write(w, 200, map[string]any{"items": items, "total": total})
	}
}
func (s *Server) monitoringSave(resource string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		var q model.MonitoringConfigRequest
		if decode(w, r, &q) != nil {
			return
		}
		if strings.HasSuffix(r.URL.Path, "/publish") {
			q.Scope = "shared"
		}
		var revision model.AnalysisConfigRevision
		var err error
		if resource == "profiles" {
			revision, err = s.monitoring.SaveProfile(r.Context(), analysisActor(r), q)
		} else {
			revision, err = s.monitoring.SaveObservation(r.Context(), analysisActor(r), q)
		}
		if err != nil {
			analysisProblem(w, err)
			return
		}
		write(w, 201, revision)
	}
}
func (s *Server) monitoringConfirm(w http.ResponseWriter, r *http.Request) {
	var q struct {
		ExpectedVersion int64 `json:"expectedVersion"`
	}
	if decode(w, r, &q) != nil {
		return
	}
	v, err := s.monitoring.ConfirmObservation(r.Context(), analysisActor(r), r.PathValue("id"), q.ExpectedVersion)
	if err != nil {
		analysisProblem(w, err)
		return
	}
	write(w, 201, v)
}
func (s *Server) monitoringHypothesis(w http.ResponseWriter, r *http.Request) {
	var q model.MonitoringHypothesisRequest
	if decode(w, r, &q) != nil {
		return
	}
	v, err := s.monitoring.Hypothesis(r.Context(), analysisActor(r), r.PathValue("id"), q)
	if err != nil {
		analysisProblem(w, err)
		return
	}
	write(w, 201, v)
}
func (s *Server) monitoringHypotheses(w http.ResponseWriter, r *http.Request) {
	items, total, err := s.monitoring.Hypotheses(r.Context(), analysisActor(r), r.PathValue("id"), analysisPage(r))
	if err != nil {
		analysisProblem(w, err)
		return
	}
	write(w, 200, map[string]any{"items": items, "total": total})
}
func (s *Server) monitoringReview(w http.ResponseWriter, r *http.Request) {
	var q model.MonitoringReviewRequest
	if decode(w, r, &q) != nil {
		return
	}
	v, err := s.monitoring.Review(r.Context(), analysisActor(r), r.PathValue("id"), q)
	if err != nil {
		analysisProblem(w, err)
		return
	}
	write(w, 201, v)
}
func (s *Server) monitoringReviews(w http.ResponseWriter, r *http.Request) {
	items, total, err := s.monitoring.Reviews(r.Context(), analysisActor(r), r.URL.Query().Get("runId"), r.PathValue("id"), analysisPage(r))
	if err != nil {
		analysisProblem(w, err)
		return
	}
	write(w, 200, map[string]any{"items": items, "total": total})
}
func (s *Server) monitoringRunReviews(w http.ResponseWriter, r *http.Request) {
	items, total, err := s.monitoring.Reviews(r.Context(), analysisActor(r), r.PathValue("id"), "", analysisPage(r))
	if err != nil {
		analysisProblem(w, err)
		return
	}
	write(w, 200, map[string]any{"items": items, "total": total})
}
func (s *Server) monitoringExport(w http.ResponseWriter, r *http.Request) {
	data, err := s.monitoring.Export(r.Context(), analysisActor(r), r.PathValue("id"))
	if err != nil {
		analysisProblem(w, err)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="monitoring-`+r.PathValue("id")+`.json"`)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write(data)
}
