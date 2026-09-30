package httpapi

import (
	"net/http"
	"strings"

	"iot-platform/internal/analytics"
)

func (s *Server) governanceFactRoutes() {
	// Gin's decoded path needs a wildcard for the summary fact ID's slash.
	// The exact decoded value is still validated by the fixed-fact service.
	s.router.GET("/api/v1/alarm-governance/runs/:id/facts/*factId", s.authorize("viewer"), s.endpoint(s.governanceFact, "id", "factId"))
}

func (s *Server) governanceFact(w http.ResponseWriter, r *http.Request) {
	if s.analysis.AI == nil {
		analysisProblem(w, analytics.ErrUnsupported)
		return
	}
	fact, err := s.analysis.AI.Fact(r.Context(), analysisActor(r), analytics.KindRecurring, r.PathValue("id"), strings.TrimPrefix(r.PathValue("factId"), "/"))
	if err != nil {
		analysisProblem(w, err)
		return
	}
	write(w, 200, fact)
}
