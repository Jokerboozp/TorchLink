package httpapi

import (
	"net/http"
	"strings"
)

// governanceReminderRoutes is registered with the governance route family. It
// keeps recipient controls in the service even when a user has tenant-wide scope.
func (s *Server) governanceReminderRoutes() {
	base := "/api/v1/alarm-governance/reminders"
	s.router.GET(base, s.authorize("viewer"), s.endpoint(s.governanceUserReminders))
	s.router.POST(base+"/refresh", s.authorize("viewer"), s.endpoint(s.governanceRefreshReminders))
	for _, op := range []string{"read", "handled"} {
		s.router.POST(base+"/:id/"+op, s.authorize("viewer"), s.endpoint(s.governanceReminderUpdate(op), "id"))
	}
}
func (s *Server) governanceUserReminders(w http.ResponseWriter, r *http.Request) {
	if !s.governanceReady(w) {
		return
	}
	q := r.URL.Query()
	items, total, err := s.governance.UserReminders(r.Context(), governanceActor(r), q.Get("status"), intval(q.Get("limit"), 20), intval(q.Get("offset"), 0))
	if err != nil {
		governanceProblem(w, err)
		return
	}
	write(w, 200, map[string]any{"items": items, "total": total})
}
func (s *Server) governanceRefreshReminders(w http.ResponseWriter, r *http.Request) {
	if !s.governanceReady(w) {
		return
	}
	created, err := s.governance.RefreshUserReminders(r.Context(), governanceActor(r))
	if err != nil {
		governanceProblem(w, err)
		return
	}
	write(w, 200, map[string]any{"created": created})
}
func (s *Server) governanceReminderUpdate(op string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.governanceReady(w) {
			return
		}
		var q struct {
			ExpectedVersion int64  `json:"expectedVersion"`
			IdempotencyKey  string `json:"idempotencyKey"`
		}
		if decode(w, r, &q) != nil {
			return
		}
		out, err := s.governance.UpdateReminder(r.Context(), governanceActor(r), r.PathValue("id"), strings.ToUpper(op), q.ExpectedVersion, q.IdempotencyKey)
		if err != nil {
			governanceProblem(w, err)
			return
		}
		write(w, 200, out)
	}
}
