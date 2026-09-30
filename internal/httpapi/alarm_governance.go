package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iot-platform/internal/alarmgovernance"
	"iot-platform/internal/analytics"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

var governanceResources = map[string]string{"templates": model.GovernanceTemplateKind, "scene-presets": model.GovernanceSceneKind, "type-profiles": model.GovernanceProfileKind, "cases": model.GovernanceCaseKind, "rounds": model.GovernanceRoundKind, "alarm-links": model.GovernanceAlarmLinkKind, "verifications": model.GovernanceVerificationKind, "verification-links": model.GovernanceVerificationLinkKind, "activities": model.GovernanceActivityKind, "activity-coverages": model.GovernanceCoverageKind, "causes": model.GovernanceCauseKind, "measures": model.GovernanceMeasureKind, "observation-plans": model.GovernancePlanKind, "observation-reviews": model.GovernanceReviewKind, "reports": model.GovernanceReportKind, "events": model.GovernanceEventKind, "business-links": model.GovernanceBusinessLinkKind, "reminders": model.GovernanceReminderKind}

func (s *Server) setupAlarmGovernance() {
	store, _ := s.unscopedRepo().(ports.AlarmGovernanceStore)
	observations, _ := s.unscopedRepo().(ports.AlarmObservationStore)
	if store != nil {
		s.SetAlarmGovernanceStorage(store, observations)
	}
}
func (s *Server) SetAlarmGovernanceStorage(store ports.AlarmGovernanceStore, observations ports.AlarmObservationStore) {
	old := s.governance
	s.governance = alarmgovernance.New(store, s.resolveGovernanceActor, func(ctx context.Context, tenant, id string) error {
		_, e := s.unscopedRepo().GetManagedDevice(ctx, tenant, id)
		return e
	}, observations)
	s.governance.ResolveTx = s.resolveGovernanceActorTx
	s.governance.AuthorizeSource = func(ctx context.Context, a alarmgovernance.Actor, d model.GovernanceDocument) error {
		return s.authorizeGovernanceSource(ctx, a, d)
	}
	if old != nil {
		s.governance.ValidateReview = old.ValidateReview
		s.governance.AuthorizeSource = old.AuthorizeSource
	}
	if s.recurring != nil {
		s.recurring.Store = store
		s.governance.ValidateReview = s.recurring.ValidateReview
		s.governance.AuthorizeSource = func(ctx context.Context, a alarmgovernance.Actor, d model.GovernanceDocument) error {
			return s.authorizeGovernanceSource(ctx, a, d)
		}
		s.analysis.AuthorizeSources = func(ctx context.Context, a analytics.Actor, r model.AnalysisRun) error {
			if r.Kind != analytics.KindRecurring {
				return nil
			}
			actor := alarmgovernance.Actor{TenantID: a.TenantID, Username: a.Username, Managed: a.Managed, SessionVersion: a.SessionVersion, AccessVersion: a.AccessVersion, Permissions: a.Permissions, DeviceIDs: a.DeviceIDs, AllDevices: a.AllDevices}
			return s.recurring.AuthorizeInputs(ctx, r, func(d model.GovernanceDocument) error {
				if !actor.AllDevices {
					for _, id := range d.DeviceIDs {
						if !slices.Contains(actor.DeviceIDs, id) {
							return analytics.ErrForbidden
						}
					}
				}
				return s.governance.AuthorizeProvenance(ctx, actor, d, s.governanceSource)
			})
		}

	}
}
func (s *Server) resolveGovernanceActorTx(tx ports.AlarmGovernanceTx, a alarmgovernance.Actor) (alarmgovernance.Actor, error) {
	if !a.Managed && a.Username == s.cfg.AdminUser && adminTenantAllowed(s.cfg.AdminTenants, a.TenantID) {
		a.AllDevices = true
		a.Permissions = []string{"*"}
		return a, nil
	}
	reader, ok := tx.(ports.GovernanceAuthorizationReader)
	if !ok {
		return a, alarmgovernance.ErrForbidden
	}
	state, e := reader.GovernanceAccessState()
	if e != nil {
		return a, e
	}
	for _, u := range state.Users {
		if u.Username == a.Username && u.Enabled && (!a.Managed || u.SessionVersion == a.SessionVersion) {
			u = resolveUserDeviceScope(state, u)
			p := effectivePermissions(state, u)
			scope := scopeFor(u, p, a.TenantID)
			a.Permissions = permissionList(p)
			a.DeviceIDs = u.DeviceIDs
			a.AllDevices = scope.All
			a.AccessVersion = accessVersion(u, p, a.TenantID)
			return a, nil
		}
	}
	return a, alarmgovernance.ErrForbidden
}
func (s *Server) AlarmGovernanceService() *alarmgovernance.Service { return s.governance }
func (s *Server) resolveGovernanceActor(ctx context.Context, a alarmgovernance.Actor) (alarmgovernance.Actor, error) {
	if !a.Managed && a.Username == s.cfg.AdminUser && adminTenantAllowed(s.cfg.AdminTenants, a.TenantID) {
		a.AllDevices = true
		a.Permissions = []string{"*"}
		return a, nil
	}
	if !a.Managed {
		store, ok := s.unscopedRepo().(ports.AccessStore)
		if !ok {
			return a, alarmgovernance.ErrForbidden
		}
		state, e := store.LoadAccessState(ctx, a.TenantID)
		if e != nil {
			return a, e
		}
		for _, u := range state.Users {
			if u.Username == a.Username && u.Enabled {
				u = resolveUserDeviceScope(state, u)
				p := effectivePermissions(state, u)
				scope := scopeFor(u, p, a.TenantID)
				a.DeviceIDs = u.DeviceIDs
				a.AllDevices = scope.All
				a.Permissions = permissionList(p)
				a.SessionVersion = u.SessionVersion
				a.AccessVersion = accessVersion(u, p, a.TenantID)
				return a, nil
			}
		}
		return a, alarmgovernance.ErrForbidden
	}
	fresh, e := s.resolveAnalysisActor(ctx, analytics.Actor{TenantID: a.TenantID, Username: a.Username, Managed: true, SessionVersion: a.SessionVersion})
	if e != nil {
		return a, alarmgovernance.ErrForbidden
	}
	a.DeviceIDs = fresh.DeviceIDs
	a.AllDevices = fresh.AllDevices
	a.Permissions = fresh.Permissions
	a.AccessVersion = fresh.AccessVersion
	return a, nil
}
func governanceActor(r *http.Request) alarmgovernance.Actor {
	c := claims(r)
	return alarmgovernance.Actor{TenantID: c.TenantID, Username: c.Username, Managed: c.TokenUse == "user" || c.ManagedUser, SessionVersion: c.SessionVersion}
}
func governanceProblem(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, alarmgovernance.ErrForbidden), errors.Is(err, analytics.ErrForbidden):
		problem(w, 403, err.Error())
	case errors.Is(err, model.ErrNotFound):
		problem(w, 404, "治理资源不存在")
	case errors.Is(err, model.ErrGovernanceConflict), errors.Is(err, model.ErrAnalysisConflict):
		problem(w, 409, err.Error())
	case errors.Is(err, model.ErrGovernanceInvalid):
		problem(w, 422, err.Error())
	default:
		problem(w, 500, "治理操作失败")
	}
}
func (s *Server) governanceReady(w http.ResponseWriter) bool {
	if s.governance == nil {
		problem(w, 503, "治理持久化服务不可用")
		return false
	}
	return true
}
func (s *Server) alarmGovernanceRoutes() {
	base := "/api/v1/alarm-governance"
	s.governanceReminderRoutes()
	s.governanceEvidenceRoutes()
	s.router.GET(base+"/reports/:id/analysis", s.authorize("viewer"), s.endpoint(s.governanceReportAnalysis, "id"))
	s.governanceSourceRoutes()
	s.governanceProjectionRoutes()
	s.governanceFactRoutes()
	s.router.GET(base+"/source-fields", s.authorize("viewer"), s.endpoint(s.governanceSourceFields))
	for resource, kind := range governanceResources {
		if kind == model.GovernanceBusinessLinkKind || kind == model.GovernanceReminderKind {
			continue
		}
		path := base + "/" + resource
		s.router.GET(path, s.authorize("viewer"), s.endpoint(s.governanceList(kind, "")))
		s.router.GET(path+"/:id", s.authorize("viewer"), s.endpoint(s.governanceGet(kind), "id"))
		switch kind {
		case model.GovernanceCaseKind, model.GovernanceVerificationKind, model.GovernanceActivityKind, model.GovernanceCoverageKind, model.GovernanceTemplateKind, model.GovernanceSceneKind, model.GovernanceProfileKind:
			s.router.POST(path, s.authorize("viewer"), s.endpoint(s.governanceCommand(kind, "create", "")))
		}
		switch kind {
		case model.GovernanceCaseKind, model.GovernanceVerificationKind, model.GovernanceCauseKind, model.GovernanceMeasureKind:
			s.router.PATCH(path+"/:id", s.authorize("viewer"), s.endpoint(s.governanceCommand(kind, "update", ""), "id"))
		}
		if kind == model.GovernanceTemplateKind || kind == model.GovernanceSceneKind || kind == model.GovernanceProfileKind {
			s.router.GET(path+"/:id/revisions", s.authorize("viewer"), s.endpoint(s.governanceConfigRevisions(kind), "id"))
			s.router.POST(path+"/:id/revisions", s.authorize("viewer"), s.endpoint(s.governanceCommand(kind, "revisions", ""), "id"))
			s.router.GET(path+"/:id/revisions/:revisionId", s.authorize("viewer"), s.endpoint(s.governanceConfigGet(kind), "id", "revisionId"))
			s.router.PATCH(path+"/:id/revisions/:revisionId", s.authorize("viewer"), s.endpoint(s.governanceCommand(kind, "update", "revisionId"), "id", "revisionId"))
			for _, op := range []string{"validate", "publish", "retire"} {
				s.router.POST(path+"/:id/revisions/:revisionId/"+op, s.authorize("viewer"), s.endpoint(s.governanceCommand(kind, op, "revisionId"), "id", "revisionId"))
			}
		}
	}
	for _, op := range []string{"start", "assign", "confirm-identity", "start-observation", "complete", "cancel", "reopen"} {
		s.router.POST(base+"/cases/:id/"+op, s.authorize("viewer"), s.endpoint(s.governanceCommand(model.GovernanceCaseKind, op, ""), "id"))
	}
	for _, resource := range []string{"rounds", "reports", "events"} {
		s.router.GET(base+"/cases/:id/"+resource, s.authorize("viewer"), s.endpoint(s.governanceList(governanceResources[resource], "case"), "id"))
	}
	for _, resource := range []string{"alarm-links", "verification-links", "causes", "measures", "observation-plans", "observation-reviews", "events"} {
		path := base + "/rounds/:id/" + resource
		kind := governanceResources[resource]
		s.router.GET(path, s.authorize("viewer"), s.endpoint(s.governanceList(kind, "round"), "id"))
		if resource != "events" {
			s.router.POST(path, s.authorize("viewer"), s.endpoint(s.governanceCommand(kind, "create", "round"), "id"))
		}
	}
	for resource, ops := range map[string][]string{"alarm-links": {"corrections"}, "verifications": {"confirm", "corrections"}, "verification-links": {"corrections"}, "activities": {"confirm", "revisions"}, "activity-coverages": {"revisions"}, "causes": {"confirm", "dispute", "reject", "corrections"}, "measures": {"start", "implement", "verify", "cancel", "corrections"}, "observation-plans": {"confirm"}, "observation-reviews": {"confirm", "corrections"}} {
		for _, op := range ops {
			s.router.POST(base+"/"+resource+"/:id/"+op, s.authorize("viewer"), s.endpoint(s.governanceCommand(governanceResources[resource], op, ""), "id"))
		}
	}
	s.router.GET(base+"/activities/:id/revisions", s.authorize("viewer"), s.endpoint(s.governanceActivityRevisions, "id"))
	s.router.GET(base+"/reports/:id/export", s.authorize("viewer"), s.endpoint(s.governanceReportExport, "id"))
	s.router.GET(base+"/observations", s.authorize("viewer"), s.endpoint(s.governanceObservations))
	s.router.GET(base+"/observations/:id", s.authorize("viewer"), s.endpoint(s.governanceObservation, "id"))
}
func (s *Server) governanceGet(kind string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.governanceReady(w) {
			return
		}
		d, e := s.governance.Get(r.Context(), governanceActor(r), kind, r.PathValue("id"))
		if errors.Is(e, model.ErrNotFound) && isGovernanceConfig(kind) {
			items, _, err := s.governance.List(r.Context(), governanceActor(r), model.GovernanceFilter{Kind: kind, ResourceID: r.PathValue("id"), Status: "PUBLISHED", Limit: 100})
			e = err
			if e == nil {
				if len(items) == 0 {
					e = model.ErrNotFound
				} else {
					d = items[0]
				}
			}
		}
		if e != nil {
			governanceProblem(w, e)
			return
		}
		write(w, 200, d)
	}
}
func (s *Server) governanceList(kind, parent string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.governanceReady(w) {
			return
		}
		q := r.URL.Query()
		f := model.GovernanceFilter{Kind: kind, Status: q.Get("status"), OwnerUserID: q.Get("ownerUserId"), ResourceID: q.Get("resourceId"), Limit: intval(q.Get("limit"), 20), Offset: intval(q.Get("offset"), 0), Start: governanceInt(q.Get("start"), 0), End: governanceInt(q.Get("end"), 0)}
		if parent == "case" {
			f.CaseID = r.PathValue("id")
		} else if parent == "round" {
			f.RoundID = r.PathValue("id")
		}
		items, total, e := s.governance.List(r.Context(), governanceActor(r), f)
		if e != nil {
			governanceProblem(w, e)
			return
		}
		write(w, 200, map[string]any{"items": items, "total": total})
	}
}
func (s *Server) governanceCommand(kind, op, idMode string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.governanceReady(w) {
			return
		}
		var body json.RawMessage
		if decode(w, r, &body) != nil {
			return
		}
		var q struct {
			ExpectedVersion int64  `json:"expectedVersion"`
			IdempotencyKey  string `json:"idempotencyKey"`
		}
		if json.Unmarshal(body, &q) != nil {
			problem(w, 400, "请求格式无效")
			return
		}
		c := alarmgovernance.Command{Kind: kind, Operation: op, ID: r.PathValue("id"), ExpectedVersion: q.ExpectedVersion, IdempotencyKey: q.IdempotencyKey, Body: body}
		if idMode == "round" {
			c.RoundID = c.ID
			c.ID = ""
		} else if idMode == "revisionId" {
			c.ID = r.PathValue("revisionId")
			d, e := s.governance.Get(r.Context(), governanceActor(r), kind, c.ID)
			if e != nil {
				governanceProblem(w, e)
				return
			}
			if d.ResourceID != r.PathValue("id") {
				governanceProblem(w, model.ErrNotFound)
				return
			}
		}
		if isGovernanceConfig(kind) && op == "revisions" {
			items, _, e := s.governance.List(r.Context(), governanceActor(r), model.GovernanceFilter{Kind: kind, ResourceID: c.ID, Limit: 100})
			if e != nil {
				governanceProblem(w, e)
				return
			}
			if len(items) == 0 {
				governanceProblem(w, model.ErrNotFound)
				return
			}
			c.ID = items[0].ID
		}
		d, e := s.governance.Execute(r.Context(), governanceActor(r), c)
		if e != nil {
			governanceProblem(w, e)
			return
		}
		status := 200
		if op == "create" || op == "revisions" || op == "corrections" {
			status = 201
		}
		write(w, status, d)
	}
}
func (s *Server) governanceConfigRevisions(kind string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.governanceReady(w) {
			return
		}
		items, total, e := s.governance.List(r.Context(), governanceActor(r), model.GovernanceFilter{Kind: kind, ResourceID: r.PathValue("id"), Limit: 100})
		if e != nil {
			governanceProblem(w, e)
			return
		}
		write(w, 200, map[string]any{"items": items, "total": total})
	}
}
func (s *Server) governanceConfigGet(kind string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.governanceReady(w) {
			return
		}
		d, e := s.governance.Get(r.Context(), governanceActor(r), kind, r.PathValue("revisionId"))
		if e == nil && d.ResourceID != r.PathValue("id") {
			e = model.ErrNotFound
		}
		if e != nil {
			governanceProblem(w, e)
			return
		}
		write(w, 200, d)
	}
}
func (s *Server) governanceActivityRevisions(w http.ResponseWriter, r *http.Request) {
	if !s.governanceReady(w) {
		return
	}
	d, e := s.governance.Get(r.Context(), governanceActor(r), model.GovernanceActivityKind, r.PathValue("id"))
	if e != nil {
		governanceProblem(w, e)
		return
	}
	items, total, e := s.governance.List(r.Context(), governanceActor(r), model.GovernanceFilter{Kind: model.GovernanceActivityKind, ResourceID: d.ResourceID, Limit: 100})
	if e != nil {
		governanceProblem(w, e)
		return
	}
	write(w, 200, map[string]any{"items": items, "total": total})
}
func (s *Server) governanceReportExport(w http.ResponseWriter, r *http.Request) {
	if !s.governanceReady(w) {
		return
	}
	d, e := s.governance.Get(r.Context(), governanceActor(r), model.GovernanceReportKind, r.PathValue("id"))
	if e != nil {
		governanceProblem(w, e)
		return
	}
	b, _ := model.GovernanceBody[model.GovernanceReport](d)
	facts, e := s.governanceReportFacts(r, b)
	if e != nil {
		analysisProblem(w, e)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="alarm-governance-report.txt"`)
	text := fmtGovernanceReport(d, b) + facts
	_, _ = w.Write([]byte(text))
}
func fmtGovernanceReport(d model.GovernanceDocument, b model.GovernanceReport) string {
	return "反复报警治理正式报告\n报告ID：" + d.ID + "\n轮次：" + b.RoundID + "\n固定快照：" + b.AnalysisSnapshotID + "\n事实hash：" + b.FactsHash + "\n本轮评价：" + b.Conclusion + "\n限制：" + strings.Join(b.Limitations, "；") + "\n后续安排：" + b.Followup + "\n后续负责人：" + b.FollowupOwnerUserID + "\n确认人：" + b.ConfirmedBy + "\n本轮完成不代表现场风险解除，生产告警仍按原流程处置。\n"
}
func (s *Server) governanceObservation(w http.ResponseWriter, r *http.Request) {
	if !s.governanceReady(w) {
		return
	}
	a, e := s.resolveGovernanceActor(r.Context(), governanceActor(r))
	if e != nil {
		governanceProblem(w, e)
		return
	}
	if s.governance.Observations == nil {
		problem(w, 503, "观测服务不可用")
		return
	}
	v, e := s.governance.Observations.GetAlarmObservation(r.Context(), a.TenantID, r.PathValue("id"))
	if e == nil && !a.AllDevices && !slices.Contains(a.DeviceIDs, v.DeviceID) {
		e = alarmgovernance.ErrForbidden
	}
	if e != nil {
		governanceProblem(w, e)
		return
	}
	v.Payload = nil
	write(w, 200, v)
}
func (s *Server) governanceObservations(w http.ResponseWriter, r *http.Request) {
	if !s.governanceReady(w) {
		return
	}
	a, e := s.resolveGovernanceActor(r.Context(), governanceActor(r))
	if e != nil {
		governanceProblem(w, e)
		return
	}
	q := r.URL.Query()
	ids := []string{}
	for _, id := range strings.Split(q.Get("deviceIds"), ",") {
		if id != "" {
			ids = append(ids, id)
		}
	}
	if q.Get("deviceId") != "" {
		ids = append(ids, q.Get("deviceId"))
	}
	if len(ids) == 0 {
		write(w, 200, map[string]any{"items": []model.AlarmObservation{}, "nextCursor": ""})
		return
	}
	for _, id := range ids {
		if !a.AllDevices && !slices.Contains(a.DeviceIDs, id) {
			governanceProblem(w, alarmgovernance.ErrForbidden)
			return
		}
	}
	if s.governance.Observations == nil {
		problem(w, 503, "观测服务不可用")
		return
	}
	limit := intval(q.Get("limit"), 20)
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	filter := ports.AlarmObservationFilter{DeviceIDs: ids, ComponentID: q.Get("componentId"), AlarmType: q.Get("alarmType"), OriginKind: q.Get("originKind"), SignalKey: q.Get("signalKey"), AlarmID: q.Get("alarmId"), Start: governanceInt(q.Get("start"), 0), End: governanceInt(q.Get("end"), 0), Cursor: q.Get("cursor"), Limit: limit}
	filter.TimeBasis = q.Get("timeBasis")
	if filter.TimeBasis == "" {
		filter.TimeBasis = "EVENT_AT"
	}
	if filter.TimeBasis != "EVENT_AT" && filter.TimeBasis != "RECEIVED_AT" {
		problem(w, 422, "查询时间口径不支持")
		return
	}
	if len(ids) > s.analysis.Limits.MaxDevices {
		problem(w, 422, "设备数量超过分析限制")
		return
	}
	if filter.Start <= 0 || filter.End <= filter.Start || filter.End-filter.Start > s.analysis.Limits.MaxRange.Milliseconds() {
		problem(w, 422, "查询区间超过分析限制")
		return
	}
	items, e := s.governance.Observations.ListAlarmObservations(r.Context(), a.TenantID, filter)
	if e != nil {
		governanceProblem(w, e)
		return
	}
	cursor := ""
	if len(items) == limit {
		last := items[len(items)-1]
		cursor = fmt.Sprintf("%d:%s", last.TimeAt(filter.TimeBasis), last.ID)
	}
	for i := range items {
		items[i].Payload = nil
	}
	write(w, 200, map[string]any{"items": items, "nextCursor": cursor})
}
func isGovernanceConfig(kind string) bool {
	return kind == model.GovernanceTemplateKind || kind == model.GovernanceSceneKind || kind == model.GovernanceProfileKind
}
func governanceInt(v string, fallback int64) int64 {
	x, e := strconv.ParseInt(v, 10, 64)
	if e != nil {
		return fallback
	}
	return x
}

func (s *Server) authorizeGovernanceSource(ctx context.Context, a alarmgovernance.Actor, d model.GovernanceDocument) error {
	if !a.AllDevices {
		for _, id := range d.DeviceIDs {
			if !slices.Contains(a.DeviceIDs, id) {
				return alarmgovernance.ErrForbidden
			}
		}
	}
	if e := s.governance.AuthorizeProvenance(ctx, a, d, s.governanceSource); e != nil {
		return e
	}
	if d.Kind != model.GovernanceReportKind {
		return nil
	}
	report, e := model.GovernanceBody[model.GovernanceReport](d)
	if e != nil {
		return e
	}
	if s.analysis == nil {
		return alarmgovernance.ErrForbidden
	}
	snap, e := s.analysis.Store.GetAnalysisSnapshot(ctx, a.TenantID, report.AnalysisSnapshotID)
	if e != nil {
		return e
	}
	actor := analytics.Actor{TenantID: a.TenantID, Username: a.Username, Managed: a.Managed, SessionVersion: a.SessionVersion, AccessVersion: a.AccessVersion, Permissions: a.Permissions, DeviceIDs: a.DeviceIDs, AllDevices: a.AllDevices}
	if _, e = s.analysis.Get(ctx, actor, analytics.KindRecurring, snap.RunID); e != nil {
		return e
	}
	if snap.FactsHash != report.FactsHash {
		return model.ErrGovernanceConflict
	}
	return nil
}
