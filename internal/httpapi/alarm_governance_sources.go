package httpapi

import (
	"context"
	"iot-platform/internal/alarmgovernance"
	"iot-platform/internal/duty"
	"iot-platform/internal/model"
	"net/http"
	"slices"
	"strings"
)

func (s *Server) governanceSource(ctx context.Context, a alarmgovernance.Actor, kind, id string) (alarmgovernance.BusinessSource, error) {
	fresh, e := s.resolveGovernanceActor(ctx, a)
	if e != nil {
		return alarmgovernance.BusinessSource{}, e
	}
	a = fresh
	if kind == "VIDEO_EVENT" {
		return s.governanceVideoSource(ctx, a, id)
	}
	if kind != "DUTY_ITEM" && kind != "DUTY_RECORD" {
		return alarmgovernance.BusinessSource{}, alarmgovernance.ErrSourceUnavailable
	}
	if !slices.Contains(a.Permissions, "*") && !slices.Contains(a.Permissions, "menu:duty") {
		return alarmgovernance.BusinessSource{}, alarmgovernance.ErrForbidden
	}
	svc, e := s.dutyService(ctx)
	if e != nil {
		return alarmgovernance.BusinessSource{}, e
	}
	actor := duty.Actor{TenantID: a.TenantID, Username: a.Username, Enabled: true, Permissions: a.Permissions, AllDevices: a.AllDevices, AllowedDeviceIDs: a.DeviceIDs, SessionVersion: a.SessionVersion, ManagedUser: a.Managed, AccessVersion: a.AccessVersion}
	k := model.DutyRecordKind
	if kind == "DUTY_ITEM" {
		k = model.DutyItemKind
	}
	doc, e := svc.Get(ctx, actor, k, id)
	if e != nil {
		return alarmgovernance.BusinessSource{}, alarmgovernance.ErrForbidden
	}
	ids := []string{}
	summary := ""
	if k == model.DutyItemKind {
		v, e := model.DutyBody[model.DutyItem](doc)
		if e != nil {
			return alarmgovernance.BusinessSource{}, e
		}
		if v.DeviceID != "" {
			ids = []string{v.DeviceID}
		}
		summary = v.Title
	} else {
		v, e := model.DutyBody[model.DutyRecord](doc)
		if e != nil {
			return alarmgovernance.BusinessSource{}, e
		}
		if v.DeviceID != "" {
			ids = []string{v.DeviceID}
		}
		summary = v.Content
	}
	if len(ids) == 0 {
		return alarmgovernance.BusinessSource{}, alarmgovernance.ErrForbidden
	}
	if len(summary) > 1000 {
		summary = summary[:1000]
	}
	return alarmgovernance.BusinessSource{Version: doc.Version, DeviceIDs: ids, Summary: summary, Status: "AVAILABLE"}, nil
}
func (s *Server) governanceSourceRoutes() {
	s.governanceVideoRoutes()
	base := "/api/v1/alarm-governance"
	s.router.GET(base+"/cases/:id/business-links", s.authorize("viewer"), s.endpoint(s.governanceList(model.GovernanceBusinessLinkKind, "case"), "id"))
	s.router.POST(base+"/cases/:id/business-links", s.authorize("viewer"), s.endpoint(s.governanceBusinessLink, "id"))
	s.router.POST(base+"/business-links/:id/corrections", s.authorize("viewer"), s.endpoint(s.governanceBusinessLink, "id"))
}
func (s *Server) governanceBusinessLink(w http.ResponseWriter, r *http.Request) {
	if !s.governanceReady(w) {
		return
	}
	var q struct {
		model.GovernanceBusinessLink
		IdempotencyKey string `json:"idempotencyKey"`
	}
	if decode(w, r, &q) != nil {
		return
	}
	caseID := r.PathValue("id")
	if strings.HasSuffix(r.URL.Path, "/corrections") {
		prior, e := s.governance.Get(r.Context(), governanceActor(r), model.GovernanceBusinessLinkKind, caseID)
		if e != nil {
			governanceProblem(w, e)
			return
		}
		caseID = prior.CaseID
		q.CorrectsID = prior.ID
	}
	d, e := s.governance.CreateBusinessLink(r.Context(), governanceActor(r), caseID, q.GovernanceBusinessLink, q.IdempotencyKey, s.governanceSource)
	if e != nil {
		governanceProblem(w, e)
		return
	}
	write(w, 201, d)
}
