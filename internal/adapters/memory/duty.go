package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

var _ ports.DutyStore = (*Repository)(nil)

type dutyTx struct {
	repo      *Repository
	ctx       context.Context
	tenant    string
	readonly  bool
	documents map[string]model.DutyDocument
	events    []model.DutyBusinessEvent
	seq       int64
}

func (r *Repository) DutyTransaction(ctx context.Context, tenant string, fn func(ports.DutyTx) error) error {
	return r.dutyTransaction(ctx, tenant, false, fn)
}
func (r *Repository) DutyRead(ctx context.Context, tenant string, fn func(ports.DutyTx) error) error {
	return r.dutyTransaction(ctx, tenant, true, fn)
}
func (r *Repository) dutyTransaction(ctx context.Context, tenant string, readonly bool, fn func(ports.DutyTx) error) error {
	if tenant == "" {
		return errors.New("duty tenant required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	tx := &dutyTx{repo: r, ctx: ctx, tenant: tenant, readonly: readonly, documents: map[string]model.DutyDocument{}, events: clone(r.dutyEvents), seq: r.dutyEventSeq}
	for k, d := range r.dutyDocuments {
		tx.documents[k] = clone(d)
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !readonly {
		r.dutyDocuments = tx.documents
		r.dutyEvents = tx.events
		r.dutyEventSeq = tx.seq
	}
	return nil
}
func (t *dutyTx) Get(kind, id string) (model.DutyDocument, error) {
	d, ok := t.documents[key(t.tenant, kind, id)]
	if !ok {
		return d, model.ErrNotFound
	}
	return clone(d), nil
}
func (t *dutyTx) List(f model.DutyFilter) ([]model.DutyDocument, int, error) {
	out := []model.DutyDocument{}
	for _, d := range t.documents {
		if d.TenantID == t.tenant && (f.Kind == "" || d.Kind == f.Kind) && matchesDutyDocument(d, f) {
			out = append(out, clone(d))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt == out[j].UpdatedAt {
			return out[i].ID > out[j].ID
		}
		return out[i].UpdatedAt > out[j].UpdatedAt
	})
	limit, offset := dutyPage(f.Limit, f.Offset)
	total := len(out)
	if offset >= total {
		return []model.DutyDocument{}, total, nil
	}
	return out[offset:min(total, offset+limit)], total, nil
}
func (t *dutyTx) Put(d model.DutyDocument, expected int64) (model.DutyDocument, error) {
	if t.readonly {
		return d, model.ErrDutyReadOnly
	}
	if !validDutyKind(d.Kind) || d.ID == "" || !json.Valid(d.Body) {
		return d, errors.New("invalid duty document")
	}
	if d.TenantID != "" && d.TenantID != t.tenant {
		return d, errors.New("duty tenant mismatch")
	}
	k := key(t.tenant, d.Kind, d.ID)
	old, exists := t.documents[k]
	if (exists && old.Version != expected) || (!exists && expected != 0) {
		return d, model.ErrDutyConflict
	}
	if exists && (d.Kind == model.DutyReceiptKind || d.Kind == model.DutyAttachmentKind || d.Kind == model.DutyRevisionKind || d.Kind == model.DutyItemEventKind || d.Kind == model.DutyRecordKind) {
		return d, fmt.Errorf("%s is immutable", d.Kind)
	}
	d.TenantID = t.tenant
	d.Version = expected + 1
	d.UpdatedAt = time.Now().UnixMilli()
	if exists {
		d.CreatedAt = old.CreatedAt
	} else {
		d.CreatedAt = d.UpdatedAt
	}
	t.documents[k] = clone(d)
	return d, nil
}
func (t *dutyTx) Delete(kind, id string, expected int64) error {
	if t.readonly {
		return model.ErrDutyReadOnly
	}
	d, err := t.Get(kind, id)
	if err != nil {
		return err
	}
	if d.Version != expected {
		return model.ErrDutyConflict
	}
	if kind == model.DutyReceiptKind || kind == model.DutyAttachmentKind || kind == model.DutyRevisionKind || kind == model.DutyItemEventKind || kind == model.DutyRecordKind {
		return errors.New("immutable duty document")
	}
	delete(t.documents, key(t.tenant, kind, id))
	return nil
}
func (t *dutyTx) AppendEvent(e model.DutyBusinessEvent) error {
	if t.readonly {
		return model.ErrDutyReadOnly
	}
	if e.ID == "" || e.Type == "" {
		return errors.New("invalid duty event")
	}
	if e.TenantID != "" && e.TenantID != t.tenant {
		return errors.New("duty tenant mismatch")
	}
	e.TenantID = t.tenant
	for _, old := range t.events {
		if old.TenantID == e.TenantID && old.ID == e.ID {
			return nil
		}
	}
	t.seq++
	e.Seq = t.seq
	if e.RecordedAt == 0 {
		e.RecordedAt = time.Now().UnixMilli()
	}
	t.events = append(t.events, clone(e))
	return nil
}
func (t *dutyTx) Events(f model.DutyFilter) ([]model.DutyBusinessEvent, int, error) {
	out := []model.DutyBusinessEvent{}
	for _, e := range t.events {
		if e.TenantID == t.tenant && (f.Kind == "" || e.Type == f.Kind) && (f.StationID == "" || e.StationID == f.StationID) && (f.RunID == "" || e.RunID == f.RunID) && (f.UserID == "" || e.ActorID == f.UserID) && (f.Start == 0 || e.OccurredAt >= f.Start) && (f.End == 0 || e.OccurredAt < f.End) && (f.DeviceIDs == nil || slices.Contains(f.DeviceIDs, e.DeviceID)) {
			out = append(out, clone(e))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].OccurredAt == out[j].OccurredAt {
			return out[i].Seq > out[j].Seq
		}
		return out[i].OccurredAt > out[j].OccurredAt
	})
	limit, offset := dutyPage(f.Limit, f.Offset)
	total := len(out)
	if offset >= total {
		return []model.DutyBusinessEvent{}, total, nil
	}
	return out[offset:min(total, offset+limit)], total, nil
}
func (t *dutyTx) Snapshot(ids []string) (model.DutySnapshot, error) {
	s := model.DutySnapshot{CutoffAt: time.Now().UnixMilli(), EventSeq: t.seq, DeviceIDs: append([]string{}, ids...), Devices: []model.ManagedDevice{}, States: []model.DeviceState{}, Alarms: []model.Alarm{}}
	for _, d := range t.repo.devices {
		if d.TenantID == t.tenant && (ids == nil || slices.Contains(ids, d.ID)) {
			s.Devices = append(s.Devices, clone(d))
		}
	}
	for _, d := range t.repo.states {
		if d.TenantID == t.tenant && (ids == nil || slices.Contains(ids, d.DeviceID)) {
			s.States = append(s.States, clone(d))
		}
	}
	for _, a := range t.repo.alarms {
		if a.TenantID == t.tenant && (a.Status == "ACTIVE" || a.Status == "ACKED") && (ids == nil || slices.Contains(ids, a.DeviceID)) {
			s.Alarms = append(s.Alarms, clone(a))
		}
	}
	sort.Slice(s.Devices, func(i, j int) bool { return s.Devices[i].ID < s.Devices[j].ID })
	sort.Slice(s.States, func(i, j int) bool { return s.States[i].DeviceID < s.States[j].DeviceID })
	sort.Slice(s.Alarms, func(i, j int) bool { return s.Alarms[i].ID < s.Alarms[j].ID })
	return s, nil
}
func validDutyKind(k string) bool {
	return slices.Contains([]string{model.DutyReceiptKind, model.DutyAttachmentKind, model.DutyStationKind, model.DutyTeamKind, model.DutyShiftTemplateKind, model.DutyRosterKind, model.DutyRunKind, model.DutyRecordKind, model.DutyItemKind, model.DutyItemEventKind, model.DutyHandoverKind, model.DutyRevisionKind, model.DutyAIJobKind, model.DutyNotificationKind, model.DutyActionLinkKind}, k)
}
func dutyPage(l, o int) (int, int) {
	if l <= 0 {
		l = 20
	}
	if l > 100 {
		l = 100
	}
	if o < 0 {
		o = 0
	}
	return l, o
}
func matchesDutyDocument(d model.DutyDocument, f model.DutyFilter) bool {
	var b map[string]any
	if json.Unmarshal(d.Body, &b) != nil {
		return false
	}
	match := func(k, v string) bool { return v == "" || b[k] == v }
	if !match("stationId", f.StationID) || !match("runId", f.RunID) || !match("handoverId", f.HandoverID) || !match("status", f.Status) {
		return false
	}
	if f.UserID != "" {
		ok := false
		for _, k := range []string{"userId", "ownerId", "authorId", "requesterId", "leaderId", "supervisorId"} {
			if b[k] == f.UserID {
				ok = true
			}
		}
		if ids, yes := b["memberIds"].([]any); yes {
			for _, id := range ids {
				if id == f.UserID {
					ok = true
				}
			}
		}
		if !ok {
			return false
		}
	}
	at := d.CreatedAt
	for _, k := range []string{"occurredAt", "startAt"} {
		if n, ok := b[k].(float64); ok {
			at = int64(n)
			break
		}
	}
	if (f.Start != 0 && at < f.Start) || (f.End != 0 && at >= f.End) {
		return false
	}
	if f.DeviceIDs != nil {
		ids := []string{}
		if id, ok := b["deviceId"].(string); ok && id != "" {
			ids = append(ids, id)
		}
		if a, ok := b["deviceIds"].([]any); ok {
			for _, id := range a {
				if v, ok := id.(string); ok {
					ids = append(ids, v)
				}
			}
		}
		for _, id := range ids {
			if !slices.Contains(f.DeviceIDs, id) {
				return false
			}
		}
	}
	return true
}
func (r *Repository) appendDutyEventLocked(e model.DutyBusinessEvent) {
	if e.ID == "" {
		return
	}
	for _, old := range r.dutyEvents {
		if old.TenantID == e.TenantID && old.ID == e.ID {
			return
		}
	}
	r.dutyEventSeq++
	e.Seq = r.dutyEventSeq
	r.dutyEvents = append(r.dutyEvents, clone(e))
}

func (r *Repository) DutyTenants(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	set := map[string]bool{}
	for _, d := range r.dutyDocuments {
		set[d.TenantID] = true
	}
	out := []string{}
	for tenant := range set {
		if tenant != "" {
			out = append(out, tenant)
		}
	}
	slices.Sort(out)
	return out, nil
}

func (t *dutyTx) Alarm(id string) (model.Alarm, error) {
	v, ok := t.repo.alarms[key(t.tenant, id)]
	if !ok {
		return v, model.ErrNotFound
	}
	return cloneAlarm(v), nil
}
