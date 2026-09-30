package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"slices"
	"sort"
	"time"
)

var _ ports.AlarmGovernanceStore = (*Repository)(nil)

type governanceTx struct {
	history      map[string]model.GovernanceDocument
	accessState  model.AccessState
	tenant       string
	readonly     bool
	documents    map[string]model.GovernanceDocument
	sources      map[string]int64
	observations map[string]model.AlarmObservation
	standard     map[string]model.StandardMessage
}

func (r *Repository) GovernanceTransaction(ctx context.Context, tenant string, fn func(ports.AlarmGovernanceTx) error) error {
	return r.governanceTransaction(ctx, tenant, false, fn)
}
func (r *Repository) GovernanceRead(ctx context.Context, tenant string, fn func(ports.AlarmGovernanceTx) error) error {
	return r.governanceTransaction(ctx, tenant, true, fn)
}
func (r *Repository) governanceTransaction(ctx context.Context, tenant string, read bool, fn func(ports.AlarmGovernanceTx) error) error {
	if tenant == "" {
		return model.ErrGovernanceInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	t := &governanceTx{history: map[string]model.GovernanceDocument{}, tenant: tenant, readonly: read, documents: map[string]model.GovernanceDocument{}, sources: map[string]int64{}}
	if b := r.accessStates[tenant]; len(b) > 0 {
		if err := json.Unmarshal(b, &t.accessState); err != nil {
			return err
		}
	}
	t.observations = map[string]model.AlarmObservation{}
	t.standard = map[string]model.StandardMessage{}
	for k, v := range r.standard {
		if r.standardProcessed[k] {
			t.standard[k] = clone(v)
		}
	}
	if r.alarmObservations != nil {
		for k, v := range r.alarmObservations.Observations {
			t.observations[k] = clone(v)
		}
	}
	for k, v := range r.governanceHistory {
		t.history[k] = clone(v)
	}
	for k, v := range r.governanceDocuments {
		t.documents[k] = clone(v)
	}
	for k, v := range r.governanceSources {
		t.sources[k] = v
	}
	if err := fn(t); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !read {
		r.governanceDocuments = t.documents
		r.governanceHistory = t.history
		r.governanceSources = t.sources
	}
	return nil
}
func (t *governanceTx) GovernanceAccessState() (model.AccessState, error) {
	return clone(t.accessState), nil
}
func (t *governanceTx) Get(kind, id string) (model.GovernanceDocument, error) {
	v, ok := t.documents[key(t.tenant, kind, id)]
	if !ok {
		return v, model.ErrNotFound
	}
	return clone(v), nil
}
func (t *governanceTx) List(f model.GovernanceFilter) ([]model.GovernanceDocument, int, error) {
	out := []model.GovernanceDocument{}
	for _, d := range t.documents {
		if f.CorrectsID != "" && d.CorrectsID != f.CorrectsID {
			continue
		}
		if d.TenantID != t.tenant || d.Kind != f.Kind || f.CaseID != "" && d.CaseID != f.CaseID || f.RoundID != "" && d.RoundID != f.RoundID || f.ParentID != "" && d.ParentID != f.ParentID || f.ResourceID != "" && d.ResourceID != f.ResourceID || f.Status != "" && d.Status != f.Status || f.OwnerUserID != "" && d.OwnerUserID != f.OwnerUserID || f.Start > 0 && d.OccurredAt < f.Start || f.End > 0 && d.OccurredAt >= f.End {
			continue
		}
		if !f.AllDevices {
			if len(d.DeviceIDs) == 0 && !slices.Contains([]string{model.GovernanceTemplateKind, model.GovernanceSceneKind, model.GovernanceProfileKind}, d.Kind) {
				continue
			}
			allowed := true
			for _, id := range d.DeviceIDs {
				if !slices.Contains(f.DeviceIDs, id) {
					allowed = false
				}
			}
			if !allowed {
				continue
			}
		}
		out = append(out, clone(d))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt == out[j].UpdatedAt {
			return out[i].ID > out[j].ID
		}
		return out[i].UpdatedAt > out[j].UpdatedAt
	})
	total := len(out)
	limit, offset := dutyPage(f.Limit, f.Offset)
	if offset >= total {
		return []model.GovernanceDocument{}, total, nil
	}
	return out[offset:min(total, offset+limit)], total, nil
}
func (t *governanceTx) Put(d model.GovernanceDocument, expected int64) (model.GovernanceDocument, error) {
	if t.readonly || d.ID == "" || !json.Valid(d.Body) || d.TenantID != "" && d.TenantID != t.tenant {
		return d, model.ErrGovernanceInvalid
	}
	old, exists := t.documents[key(t.tenant, d.Kind, d.ID)]
	if exists && old.Version != expected || !exists && expected != 0 {
		return d, model.ErrGovernanceConflict
	}
	if exists && model.GovernanceImmutableAfterConfirmation(old) {
		return d, model.ErrGovernanceConflict
	}
	if exists && !model.GovernanceConfigurationMutationAllowed(old, d) {
		return d, model.ErrGovernanceConflict
	}
	if exists && slices.Contains([]string{model.GovernanceEventKind, model.GovernanceReceiptKind, model.GovernanceReportKind, model.GovernanceAlarmLinkKind, model.GovernanceVerificationLinkKind, model.GovernanceCoverageKind}, d.Kind) {
		return d, model.ErrGovernanceConflict
	}
	for _, v := range t.documents {
		if v.TenantID != t.tenant || v.ID == d.ID || v.Kind != d.Kind {
			continue
		}
		if d.Kind == model.GovernanceCaseKind && d.PointKey == v.PointKey && !slices.Contains([]string{"COMPLETED", "CANCELLED"}, d.Status) && !slices.Contains([]string{"COMPLETED", "CANCELLED"}, v.Status) {
			return d, model.ErrGovernanceConflict
		}
		if d.ResourceID != "" && d.ResourceID == v.ResourceID && d.RevisionNumber == v.RevisionNumber {
			return d, model.ErrGovernanceConflict
		}
		if d.Kind == model.GovernanceRoundKind && d.CaseID == v.CaseID && (d.RevisionNumber == v.RevisionNumber || d.Status == "ACTIVE" && v.Status == "ACTIVE") {
			return d, model.ErrGovernanceConflict
		}
	}
	for _, rel := range []struct{ kind, id string }{{model.GovernanceCaseKind, d.CaseID}, {model.GovernanceRoundKind, d.RoundID}} {
		if rel.id != "" {
			if _, ok := t.documents[key(t.tenant, rel.kind, rel.id)]; !ok {
				return d, model.ErrGovernanceInvalid
			}
		}
	}
	d.TenantID = t.tenant
	d.Version = expected + 1
	d.UpdatedAt = time.Now().UnixMilli()
	d.CreatedAt = d.UpdatedAt
	if exists {
		d.CreatedAt = old.CreatedAt
		d.CreatedBy = old.CreatedBy
	}
	t.documents[key(t.tenant, d.Kind, d.ID)] = clone(d)
	t.history[key(t.tenant, d.Kind, d.ID, fmt.Sprint(d.Version))] = clone(d)
	return d, nil
}
func governanceSourceKey(tenant string, v model.GovernanceSourceVersion) string {
	return key(tenant, v.DependencyKey, fmt.Sprint(v.BucketStart))
}
func (t *governanceTx) SourceVersions(keys []model.GovernanceSourceVersion) ([]model.GovernanceSourceVersion, error) {
	out := append([]model.GovernanceSourceVersion{}, keys...)
	for i := range out {
		k := governanceSourceKey(t.tenant, out[i])
		out[i].Generation = t.sources[k]
		if !t.readonly {
			t.sources[k] = out[i].Generation
		}
	}
	return out, nil
}
func (t *governanceTx) BumpSourceVersions(keys []model.GovernanceSourceVersion) error {
	if t.readonly {
		return model.ErrGovernanceInvalid
	}
	for _, v := range keys {
		t.sources[governanceSourceKey(t.tenant, v)]++
	}
	return nil
}
