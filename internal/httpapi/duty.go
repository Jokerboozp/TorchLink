package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"

	"iot-platform/internal/auth"
	"iot-platform/internal/duty"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

var dutyResources = map[string]string{
	"stations": model.DutyStationKind, "teams": model.DutyTeamKind,
	"shift-templates": model.DutyShiftTemplateKind, "rosters": model.DutyRosterKind,
	"runs": model.DutyRunKind, "records": model.DutyRecordKind, "items": model.DutyItemKind,
	"handovers": model.DutyHandoverKind, "revisions": model.DutyRevisionKind,
	"ai-jobs": model.DutyAIJobKind, "notifications": model.DutyNotificationKind,
}

func (s *Server) dutyRoutes() {
	for resource, kind := range dutyResources {
		base := "/api/v1/duty/" + resource
		s.router.GET(base, s.authorize("viewer"), s.endpoint(s.dutyList(kind)))
		s.router.GET(base+"/:id", s.authorize("viewer"), s.endpoint(s.dutyGet(kind), "id"))
		if slices.Contains([]string{model.DutyStationKind, model.DutyTeamKind, model.DutyShiftTemplateKind, model.DutyRosterKind, model.DutyRecordKind, model.DutyItemKind, model.DutyHandoverKind}, kind) {
			s.router.POST(base, s.authorize("viewer"), s.endpoint(s.dutyCommand(kind, "create")))
			if kind != model.DutyRecordKind {
				s.router.PUT(base+"/:id", s.authorize("viewer"), s.endpoint(s.dutyCommand(kind, "update"), "id"))
			}
			if slices.Contains([]string{model.DutyStationKind, model.DutyTeamKind, model.DutyShiftTemplateKind}, kind) {
				s.router.DELETE(base+"/:id", s.authorize("viewer"), s.endpoint(s.dutyCommand(kind, "delete"), "id"))
			}
		}
	}
	for _, op := range []string{"generate", "import-preview", "import"} {
		s.router.POST("/api/v1/duty/rosters/"+op, s.authorize("viewer"), s.endpoint(s.dutyCommand(model.DutyRosterKind, op)))
	}
	for _, op := range []string{"validate", "publish", "cancel"} {
		s.router.POST("/api/v1/duty/rosters/:id/"+op, s.authorize("viewer"), s.endpoint(s.dutyCommand(model.DutyRosterKind, op), "id"))
	}
	for _, op := range []string{"arrive", "open"} {
		s.router.POST("/api/v1/duty/runs/"+op, s.authorize("viewer"), s.endpoint(s.dutyCommand(model.DutyRunKind, op)))
	}
	for _, op := range []string{"substitute", "end"} {
		s.router.POST("/api/v1/duty/runs/:id/"+op, s.authorize("viewer"), s.endpoint(s.dutyCommand(model.DutyRunKind, op), "id"))
	}
	s.router.GET("/api/v1/duty/runs/current", s.authorize("viewer"), s.endpoint(s.dutyCurrent))
	s.router.GET("/api/v1/duty/runs/:id/records", s.authorize("viewer"), s.endpoint(s.dutyRunRecords, "id"))
	s.router.POST("/api/v1/duty/runs/:id/records", s.authorize("viewer"), s.endpoint(s.dutyRunRecord, "id"))
	s.router.POST("/api/v1/duty/records/:id/correct", s.authorize("viewer"), s.endpoint(s.dutyCommand(model.DutyRecordKind, "correct"), "id"))
	for _, op := range []string{"submit", "return", "accept", "void", "amend"} {
		s.router.POST("/api/v1/duty/handovers/:id/"+op, s.authorize("viewer"), s.endpoint(s.dutyCommand(model.DutyHandoverKind, op), "id"))
	}
	s.router.GET("/api/v1/duty/handovers/:id/revisions", s.authorize("viewer"), s.endpoint(s.dutyHandoverRevisions, "id"))
	s.router.POST("/api/v1/duty/handovers/:id/revisions", s.authorize("viewer"), s.endpoint(s.dutyCommand(model.DutyHandoverKind, "update"), "id"))
	s.router.GET("/api/v1/duty/handovers/:id/delta", s.authorize("viewer"), s.endpoint(s.dutyDelta, "id"))
	s.router.GET("/api/v1/duty/items/:id/events", s.authorize("viewer"), s.endpoint(s.dutyItemEvents, "id"))
	for _, op := range []string{"assign", "status", "transfer", "complete", "cancel"} {
		s.router.POST("/api/v1/duty/items/:id/"+op, s.authorize("viewer"), s.endpoint(s.dutyCommand(model.DutyItemKind, op), "id"))
	}
	s.router.GET("/api/v1/duty/options", s.authorize("viewer"), s.endpoint(s.dutyOptions))
	s.router.GET("/api/v1/duty/events", s.authorize("viewer"), s.endpoint(s.dutyEvents))
	s.router.POST("/api/v1/duty/notifications/:id/read", s.authorize("viewer"), s.endpoint(s.dutyCommand(model.DutyNotificationKind, "read"), "id"))
	s.dutyAIRoutes()
	s.dutyExportRoutes()
}

func (s *Server) dutyService(ctx context.Context) (*duty.Service, error) {
	store, ok := s.unscopedRepo().(ports.DutyStore)
	if !ok {
		return nil, errors.New("值班持久化仓储不可用")
	}
	// Resolve accounts before entering a duty transaction. The memory adapter
	// shares a mutex with access storage, and PostgreSQL facts must stay in tx.
	var state model.AccessState
	if access, ok := s.unscopedRepo().(ports.AccessStore); ok {
		var err error
		state, err = access.LoadAccessState(ctx, claimsContextTenant(ctx))
		if err != nil {
			return nil, err
		}
	}
	resolve := func(_ context.Context, tenant, username string) (duty.Actor, error) {
		if username == s.cfg.AdminUser && adminTenantAllowed(s.cfg.AdminTenants, tenant) {
			return duty.Actor{TenantID: tenant, Username: username, Admin: true, Enabled: true, AllDevices: true, Permissions: []string{"*"}}, nil
		}
		for _, u := range state.Users {
			if u.Username != username {
				continue
			}
			u = resolveUserDeviceScope(state, u)
			p := effectivePermissions(state, u)
			scope := scopeFor(u, p, tenant)
			return duty.Actor{TenantID: tenant, Username: username, Enabled: u.Enabled, Permissions: permissionList(p), AllDevices: scope.All, AllowedDeviceIDs: u.DeviceIDs, SessionVersion: u.SessionVersion, AccessVersion: accessVersion(u, p, tenant), ManagedUser: true}, nil
		}
		return duty.Actor{}, duty.ErrForbidden
	}
	return duty.New(store, resolve), nil
}

func claimsContextTenant(ctx context.Context) string {
	if c, ok := auth.ClaimsFromContext(ctx); ok {
		return c.TenantID
	}
	if c, ok := ctx.Value(claimsKey).(auth.Claims); ok {
		return c.TenantID
	}
	if identity, ok := ports.AIRunIdentityFrom(ctx); ok {
		return identity.TenantID
	}
	return ""
}

func (s *Server) dutyActor(r *http.Request) duty.Actor {
	c := claims(r)
	a := duty.Actor{TenantID: c.TenantID, Username: c.Username, Enabled: true}
	if c.TokenUse != "user" && !c.ManagedUser {
		trusted := c.Username == s.cfg.AdminUser && adminTenantAllowed(s.cfg.AdminTenants, c.TenantID) && (c.Role == "admin" || c.TokenUse == "harness" && c.Workflow == "duty-handover")
		a.Enabled = trusted
		if trusted {
			a.Admin, a.AllDevices, a.Permissions = true, true, []string{"*"}
		}
		return a
	}
	p, _ := r.Context().Value(permissionsKey{}).(map[string]bool)
	a.Permissions = permissionList(p)
	a.ManagedUser, a.SessionVersion, a.AccessVersion = true, c.SessionVersion, requestAccessVersion(r.Context(), c)
	scope, _ := requestScope(r.Context())
	a.AllDevices = scope.All
	for id, allowed := range scope.IDs {
		if allowed {
			a.AllowedDeviceIDs = append(a.AllowedDeviceIDs, id)
		}
	}
	slices.Sort(a.AllowedDeviceIDs)
	return a
}

func dutyProblem(w http.ResponseWriter, err error) {
	var validation *duty.ValidationError
	switch {
	case errors.Is(err, duty.ErrForbidden):
		problem(w, 403, err.Error())
	case errors.Is(err, duty.ErrNotFound), errors.Is(err, model.ErrNotFound):
		problem(w, 404, err.Error())
	case errors.Is(err, duty.ErrConflict), errors.Is(err, model.ErrDutyConflict):
		problem(w, 409, err.Error())
	case errors.As(err, &validation):
		problem(w, 422, err.Error())
	default:
		problem(w, 500, err.Error())
	}
}

func dutyFilter(r *http.Request, kind string) model.DutyFilter {
	q := r.URL.Query()
	p := parseListPagination(r)
	return model.DutyFilter{Kind: kind, StationID: q.Get("stationId"), RunID: q.Get("runId"), HandoverID: q.Get("handoverId"), Status: q.Get("status"), UserID: q.Get("userId"), Start: i64(q.Get("start")), End: i64(q.Get("end")), Limit: p.PageSize, Offset: p.Offset}
}

func (s *Server) dutyList(kind string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		svc, err := s.dutyService(r.Context())
		if err != nil {
			dutyProblem(w, err)
			return
		}
		result, err := svc.Query(r.Context(), s.dutyActor(r), dutyFilter(r, kind))
		if err != nil {
			dutyProblem(w, err)
			return
		}
		writeList(w, 200, result.Items, result.Total, parseListPagination(r), nil)
	}
}
func (s *Server) dutyGet(kind string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		svc, err := s.dutyService(r.Context())
		if err != nil {
			dutyProblem(w, err)
			return
		}
		v, err := svc.Get(r.Context(), s.dutyActor(r), kind, r.PathValue("id"))
		if err != nil {
			dutyProblem(w, err)
			return
		}
		write(w, 200, v)
	}
}
func (s *Server) dutyCommand(kind, operation string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		cmd := duty.Command{Kind: kind, ID: r.PathValue("id"), Operation: operation}
		if r.ContentLength != 0 && decode(w, r, &cmd) != nil {
			return
		}
		// Routing, account and tenant are always server-authoritative.
		cmd.Kind, cmd.ID, cmd.Operation = kind, r.PathValue("id"), operation
		if cmd.IdempotencyKey == "" {
			cmd.IdempotencyKey = r.Header.Get("Idempotency-Key")
		}
		svc, err := s.dutyService(r.Context())
		if err != nil {
			dutyProblem(w, err)
			return
		}
		v, err := svc.Execute(r.Context(), s.dutyActor(r), cmd)
		if err != nil {
			dutyProblem(w, err)
			return
		}
		write(w, 200, v)
	}
}
func (s *Server) dutyRunRecords(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	q.Set("runId", r.PathValue("id"))
	r.URL.RawQuery = q.Encode()
	s.dutyList(model.DutyRecordKind)(w, r)
}
func (s *Server) dutyRunRecord(w http.ResponseWriter, r *http.Request) {
	var cmd duty.Command
	if decode(w, r, &cmd) != nil {
		return
	}
	var body map[string]any
	if json.Unmarshal(cmd.Body, &body) != nil {
		problem(w, 400, "记录正文无效")
		return
	}
	body["runId"] = r.PathValue("id")
	cmd.Body, _ = json.Marshal(body)
	cmd.Kind, cmd.ID, cmd.Operation = model.DutyRecordKind, "", "create"
	svc, err := s.dutyService(r.Context())
	if err != nil {
		dutyProblem(w, err)
		return
	}
	v, err := svc.Execute(r.Context(), s.dutyActor(r), cmd)
	if err != nil {
		dutyProblem(w, err)
		return
	}
	write(w, 201, v)
}
func (s *Server) dutyHandoverRevisions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	q.Set("handoverId", r.PathValue("id"))
	r.URL.RawQuery = q.Encode()
	s.dutyList(model.DutyRevisionKind)(w, r)
}
func (s *Server) dutyItemEvents(w http.ResponseWriter, r *http.Request) {
	svc, err := s.dutyService(r.Context())
	if err != nil {
		dutyProblem(w, err)
		return
	}
	v, err := svc.Execute(r.Context(), s.dutyActor(r), duty.Command{Kind: model.DutyItemKind, ID: r.PathValue("id"), Operation: "events"})
	if err != nil {
		dutyProblem(w, err)
		return
	}
	write(w, 200, v)
}
func (s *Server) dutyCurrent(w http.ResponseWriter, r *http.Request) {
	svc, err := s.dutyService(r.Context())
	if err != nil {
		dutyProblem(w, err)
		return
	}
	v, err := svc.Execute(r.Context(), s.dutyActor(r), duty.Command{Kind: model.DutyRunKind, Operation: "current"})
	if err != nil {
		dutyProblem(w, err)
		return
	}
	write(w, 200, v)
}
func (s *Server) dutyDelta(w http.ResponseWriter, r *http.Request) {
	svc, err := s.dutyService(r.Context())
	if err != nil {
		dutyProblem(w, err)
		return
	}
	v, err := svc.Execute(r.Context(), s.dutyActor(r), duty.Command{Kind: model.DutyHandoverKind, ID: r.PathValue("id"), Operation: "delta"})
	if err != nil {
		dutyProblem(w, err)
		return
	}
	write(w, 200, v)
}
func (s *Server) dutyEvents(w http.ResponseWriter, r *http.Request) {
	svc, err := s.dutyService(r.Context())
	if err != nil {
		dutyProblem(w, err)
		return
	}
	body, _ := json.Marshal(dutyFilter(r, ""))
	v, err := svc.Execute(r.Context(), s.dutyActor(r), duty.Command{Kind: "event", Operation: "query", Body: body})
	if err != nil {
		dutyProblem(w, err)
		return
	}
	write(w, 200, v)
}
func (s *Server) dutyOptions(w http.ResponseWriter, r *http.Request) {
	c := claims(r)
	actor := s.dutyActor(r)
	users := []map[string]any{}
	manage := actor.Admin || slices.Contains(actor.Permissions, "action:duty:settings") || slices.Contains(actor.Permissions, "action:duty:roster")
	if access, ok := s.unscopedRepo().(ports.AccessStore); ok {
		state, err := access.LoadAccessState(r.Context(), c.TenantID)
		if err != nil {
			dutyProblem(w, err)
			return
		}
		for _, u := range state.Users {
			if !manage && u.Username != c.Username {
				continue
			}
			users = append(users, map[string]any{"username": u.Username, "displayName": u.DisplayName, "enabled": u.Enabled})
		}
	}
	if manage && !slices.ContainsFunc(users, func(u map[string]any) bool { return u["username"] == s.cfg.AdminUser }) {
		users = append(users, map[string]any{"username": s.cfg.AdminUser, "displayName": "平台管理员", "enabled": true})
	}
	devices, err := s.engine.Repo.ListManagedDevices(r.Context(), c.TenantID)
	if err != nil {
		dutyProblem(w, err)
		return
	}
	options := []map[string]any{}
	for _, d := range devices {
		options = append(options, map[string]any{"id": d.ID, "name": d.Name, "productId": d.ProductID, "gatewayId": d.GatewayID})
	}
	write(w, 200, map[string]any{"users": users, "devices": options, "permissions": actor.Permissions})
}
