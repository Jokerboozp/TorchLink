package httpapi

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"iot-platform/internal/analytics"
	"iot-platform/internal/analytics/response"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func (s *Server) responseLookupRoutes() {
	s.router.GET("/api/v1/response-staff", s.authorize("viewer"), s.endpoint(s.responseStaff))
	s.router.GET("/api/v1/response-runs/:id/evaluations", s.authorize("viewer"), s.endpoint(s.responseEvaluationRuns, "id"))
	s.router.GET("/api/v1/response-runs/:id/evidence-candidates", s.authorize("viewer"), s.endpoint(s.responseEvidenceCandidates, "id"))
}
func (s *Server) responseEvaluationRuns(w http.ResponseWriter, r *http.Request) {
	rows, total, err := s.responseService().EvaluationRuns(r.Context(), analysisActor(r), r.PathValue("id"), analysisPage(r))
	if err != nil {
		analysisProblem(w, err)
		return
	}
	for i := range rows {
		rows[i] = publicAnalysisRun(rows[i])
	}
	write(w, 200, map[string]any{"items": rows, "total": total})
}
func (s *Server) responseStaff(w http.ResponseWriter, r *http.Request) {
	ids := strings.Split(r.URL.Query().Get("deviceIds"), ",")
	for i := range ids {
		ids[i] = strings.TrimSpace(ids[i])
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	current, err := s.analysis.Current(r.Context(), analysisActor(r))
	if err != nil || !current.Allows(analytics.KindResponse, "", ids) || len(ids) > s.cfg.Analytics.WithDefaults().MaxDevices || len(ids) == 0 || ids[0] == "" {
		analysisProblem(w, analytics.ErrForbidden)
		return
	}
	for _, id := range ids {
		if _, err = s.unscopedRepo().GetManagedDevice(r.Context(), current.TenantID, id); err != nil {
			analysisProblem(w, analytics.ErrForbidden)
			return
		}
	}
	rows := []map[string]string{}
	if adminTenantAllowed(s.cfg.AdminTenants, current.TenantID) {
		rows = append(rows, map[string]string{"username": s.cfg.AdminUser, "displayName": "平台管理员"})
	}
	store, ok := s.unscopedRepo().(ports.AccessStore)
	if !ok {
		analysisProblem(w, analytics.ErrUnsupported)
		return
	}
	state, err := store.LoadAccessState(r.Context(), current.TenantID)
	if err != nil {
		analysisProblem(w, err)
		return
	}
	for _, user := range state.Users {
		if !user.Enabled || user.Username == s.cfg.AdminUser {
			continue
		}
		if err = s.validateResponseStaff(r.Context(), current.TenantID, user.Username, ids); err != nil {
			if err == analytics.ErrForbidden {
				continue
			}
			analysisProblem(w, err)
			return
		}
		rows = append(rows, map[string]string{"username": user.Username, "displayName": user.DisplayName})
	}
	slices.SortFunc(rows, func(a, b map[string]string) int { return strings.Compare(a["username"], b["username"]) })
	refreshed, err := s.analysis.Current(r.Context(), analysisActor(r))
	if err != nil || !refreshed.Allows(analytics.KindResponse, "", ids) {
		analysisProblem(w, analytics.ErrForbidden)
		return
	}
	write(w, 200, map[string]any{"items": rows, "total": len(rows)})
}
func (s *Server) responseEvidenceCandidates(w http.ResponseWriter, r *http.Request) {
	svc := s.responseService()
	fixed, err := svc.Latest(r.Context(), analysisActor(r), response.ExecutionKind, r.PathValue("id"))
	if err != nil {
		analysisProblem(w, err)
		return
	}
	current, err := s.analysis.Current(r.Context(), analysisActor(r))
	if err != nil {
		analysisProblem(w, err)
		return
	}
	var exec model.ResponseExecution
	_ = json.Unmarshal(fixed.Body, &exec)
	source := r.URL.Query().Get("kind")
	if source == "" {
		source = "ALARM_LIFECYCLE"
	}
	if !slices.Contains([]string{"ALARM_LIFECYCLE", "ALARM_REPORT", "RAW_MESSAGE"}, source) {
		analysisProblem(w, model.ErrAnalysisInvalid)
		return
	}
	requiredMenu := "menu:alarms"
	if source == "RAW_MESSAGE" {
		requiredMenu = "menu:raw"
	}
	if !slices.Contains(current.Permissions, "*") && !slices.Contains(current.Permissions, requiredMenu) {
		analysisProblem(w, analytics.ErrForbidden)
		return
	}
	ids := fixed.DeviceIDs
	if id := r.URL.Query().Get("deviceId"); id != "" {
		if !slices.Contains(ids, id) {
			analysisProblem(w, analytics.ErrForbidden)
			return
		}
		ids = []string{id}
	}
	now := time.Now().UnixMilli()
	start := max(int64(0), exec.StartedAt-int64(24*time.Hour/time.Millisecond))
	if exec.StartedAt == 0 {
		start = max(int64(0), now-int64(24*time.Hour/time.Millisecond))
	}
	start = responseQueryTime(r.URL.Query().Get("start"), start)
	end := responseQueryTime(r.URL.Query().Get("end"), now+1)
	if start < 0 || end <= start || end > now+1 || end-start > s.analysis.Limits.MaxRange.Milliseconds() {
		analysisProblem(w, model.ErrAnalysisInvalid)
		return
	}
	if s.analysisFacts == nil {
		analysisProblem(w, analytics.ErrUnsupported)
		return
	}
	refs := []model.ResponseEvidenceReference{}
	readLimitReached := false
	err = s.analysisFacts.AnalyticsFactsRead(r.Context(), current.TenantID, func(reader ports.AnalyticsFactReader) error {
		q := model.FactQuery{DeviceIDs: ids, Start: start, End: end, Limit: 1000}
		for count := 0; count < s.analysis.Limits.RecordLimit; {
			var cursor string
			var read int
			if source == "RAW_MESSAGE" {
				page, e := reader.ListRawParseOutcomes(q)
				if e != nil {
					return e
				}
				cursor = page.Cursor
				read = len(page.Items)
				for _, v := range page.Items {
					refs = append(refs, model.ResponseEvidenceReference{Kind: source, SourceID: v.RawMessageID, DeviceID: v.DeviceID, ReceivedAt: v.ReceivedAt, RecordedAt: v.ArchivedAt, Classification: "PRODUCTION", Description: "具体原始报文归档"})
				}
			} else {
				var page model.FactPage[model.BusinessEventFact]
				var e error
				if source == "ALARM_REPORT" {
					page, e = reader.ListAlarmReportEvents(q)
				} else {
					page, e = reader.ListAlarmLifecycleEvents(q)
				}
				if e != nil {
					return e
				}
				cursor = page.Cursor
				read = len(page.Items)
				for _, v := range page.Items {
					if exec.Source == "REAL_CASE" && v.ResourceID != exec.AlarmID {
						continue
					}
					refs = append(refs, model.ResponseEvidenceReference{Kind: source, SourceID: v.SourceEventID, DeviceID: v.DeviceID, ResourceID: v.ResourceID, EventType: v.Type, OccurredAt: v.OccurredAt, RecordedAt: v.RecordedAt, Classification: "PRODUCTION", Description: "已提交的单次生产事务事件"})
				}
			}
			count += read
			if cursor == "" {
				return nil
			}
			if cursor == q.Cursor || read == 0 {
				return model.ErrAnalysisInvalid
			}
			q.Cursor = cursor
		}
		readLimitReached = true
		return nil
	})
	if err != nil {
		analysisProblem(w, err)
		return
	}
	// Recheck the full execution before returning any candidates or totals.
	if _, err = svc.Latest(r.Context(), analysisActor(r), response.ExecutionKind, r.PathValue("id")); err != nil {
		analysisProblem(w, err)
		return
	}
	total := len(refs)
	f := analysisPage(r)
	limit, offset := analytics.NormalizeAnalysisPage(f.Limit, f.Offset)
	items := []model.ResponseEvidenceReference{}
	if offset < total {
		items = refs[offset:min(total, offset+limit)]
	}
	write(w, 200, map[string]any{"items": items, "total": total, "readLimitReached": readLimitReached, "start": start, "end": end})
}

func responseQueryTime(value string, fallback int64) int64 {
	if value == "" {
		return fallback
	}
	v, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return -1
	}
	return v
}
