package alarmgovernance

import (
	"context"
	"github.com/google/uuid"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"slices"
	"strings"
)

var ErrSourceUnavailable = invalid("该来源尚未接通受控读取适配")

type BusinessSource struct {
	Version   int64
	DeviceIDs []string
	Summary   string
	Status    string
}
type SourceResolver func(context.Context, Actor, string, string) (BusinessSource, error)

func (s *Service) CreateBusinessLink(ctx context.Context, a Actor, caseID string, q model.GovernanceBusinessLink, key string, resolve SourceResolver) (out model.GovernanceDocument, err error) {
	a, err = s.actor(ctx, a, "record")
	if err != nil {
		return
	}
	if len(key) == 0 || len(key) > 200 || strings.TrimSpace(q.Relation) == "" || resolve == nil {
		return out, invalid("关联来源、关系和幂等键必填")
	}
	d, err := s.Get(ctx, a, model.GovernanceCaseKind, caseID)
	if err != nil {
		return out, err
	}
	source, err := resolve(ctx, a, q.TargetKind, q.TargetID)
	if err != nil {
		return out, err
	}
	if len(source.DeviceIDs) == 0 || !a.covers(source.DeviceIDs) || len(d.DeviceIDs) != 1 || !slices.Contains(source.DeviceIDs, d.DeviceIDs[0]) {
		return out, ErrForbidden
	}
	if q.TargetVersion != source.Version {
		return out, model.ErrGovernanceConflict
	}
	q.CaseID = caseID
	q.DeviceIDs = source.DeviceIDs
	q.Summary = source.Summary
	q.SyncStatus = source.Status
	id := "glink_" + model.GovernanceHash([]string{a.Username, caseID, key})[:32]
	err = s.Store.GovernanceTransaction(ctx, a.TenantID, func(tx ports.AlarmGovernanceTx) error {
		if s.ResolveTx != nil {
			fresh, e := s.ResolveTx(tx, a)
			if e != nil || !fresh.menu() || !fresh.can("record") || !sourceMenus(fresh, model.GovernanceBusinessLinkKind) || fresh.AccessVersion != a.AccessVersion || !fresh.covers(source.DeviceIDs) {
				return ErrForbidden
			}
			a = fresh
		}
		if prior, e := tx.Get(model.GovernanceBusinessLinkKind, id); e == nil {
			old, e := model.GovernanceBody[model.GovernanceBusinessLink](prior)
			if e != nil {
				return e
			}
			if model.GovernanceHash(old) != model.GovernanceHash(q) {
				return model.ErrGovernanceConflict
			}
			out = prior
			return nil
		}
		parent, e := tx.Get(model.GovernanceCaseKind, caseID)
		if e != nil {
			return e
		}
		if e = s.scope(parent, a); e != nil {
			return e
		}
		if q.CorrectsID != "" {
			prev, e := tx.Get(model.GovernanceBusinessLinkKind, q.CorrectsID)
			if e != nil {
				return e
			}
			if prev.CaseID != caseID || q.CorrectionReason == "" {
				return invalid("更正来源与理由不合法")
			}
		}
		out, e = put(tx, model.GovernanceDocument{Kind: model.GovernanceBusinessLinkKind, ID: id, CaseID: caseID, DeviceIDs: source.DeviceIDs, CorrectsID: q.CorrectsID, CreatedBy: a.Username, OccurredAt: s.Now().UnixMilli()}, q, 0)
		if e != nil {
			return e
		}
		if e = s.bumpCase(tx, caseID); e != nil {
			return e
		}
		event := model.GovernanceEvent{CaseID: caseID, ResourceID: id, ResourceVersion: out.Version, Actor: a.Username, Action: "business-link:create", OccurredAt: s.Now().UnixMilli(), RecordedAt: s.Now().UnixMilli()}
		_, e = put(tx, model.GovernanceDocument{Kind: model.GovernanceEventKind, ID: uuid.NewString(), CaseID: caseID, DeviceIDs: source.DeviceIDs, CreatedBy: a.Username, OccurredAt: event.OccurredAt}, event, 0)
		return e
	})
	if err == nil {
		_, err = resolve(ctx, a, q.TargetKind, q.TargetID)
	}
	return
}

// AuthorizeProvenance checks entire source membership and the source's own
// reader. Owning a case never grants access to duty, video or rule evidence.
func (s *Service) AuthorizeProvenance(ctx context.Context, a Actor, d model.GovernanceDocument, resolve SourceResolver) error {
	if d.Kind == model.GovernanceBusinessLinkKind {
		v, e := model.GovernanceBody[model.GovernanceBusinessLink](d)
		if e != nil {
			return e
		}
		if !a.covers(v.DeviceIDs) {
			return ErrForbidden
		}
		_, e = resolve(ctx, a, v.TargetKind, v.TargetID)
		return e
	}
	if d.Kind == model.GovernanceReportKind {
		report, e := model.GovernanceBody[model.GovernanceReport](d)
		if e != nil {
			return e
		}
		for _, resource := range report.Resources {
			if resource.Kind == model.GovernanceBusinessLinkKind {
				if e = s.AuthorizeProvenance(ctx, a, resource, resolve); e != nil {
					return e
				}
			}
		}
		return nil
	}
	caseID := d.CaseID
	if d.Kind == model.GovernanceCaseKind {
		caseID = d.ID
	}
	if caseID == "" {
		return nil
	}
	var links []model.GovernanceDocument
	err := s.Store.GovernanceRead(ctx, a.TenantID, func(tx ports.AlarmGovernanceTx) error {
		f := model.GovernanceFilter{Kind: model.GovernanceBusinessLinkKind, CaseID: caseID, AllDevices: true, Limit: 100}
		for {
			page, n, e := tx.List(f)
			if e != nil {
				return e
			}
			links = append(links, page...)
			f.Offset += len(page)
			if f.Offset >= n {
				break
			}
			if f.Offset > 10000 {
				return invalid("关联来源过多")
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	superseded := map[string]bool{}
	for _, l := range links {
		if l.CorrectsID != "" {
			superseded[l.CorrectsID] = true
		}
	}
	for _, l := range links {
		if superseded[l.ID] {
			continue
		}
		if !a.covers(l.DeviceIDs) {
			return ErrForbidden
		}
		if err = s.AuthorizeProvenance(ctx, a, l, resolve); err != nil {
			return err
		}
	}
	return nil
}
