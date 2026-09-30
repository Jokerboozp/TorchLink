package memory

import (
	"context"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type alarmObservationState struct {
	Observations map[string]model.AlarmObservation
	InputHashes  map[string]string
	Slots        map[string]string
	Conflicts    map[string]model.AlarmObservationConflict
	Attempts     []model.AlarmObservationAttempt
	Signals      map[string]model.AlarmObservation
}

func (r *Repository) observationStateLocked() *alarmObservationState {
	if r.alarmObservations == nil {
		r.alarmObservations = &alarmObservationState{Observations: map[string]model.AlarmObservation{}, InputHashes: map[string]string{}, Slots: map[string]string{}, Conflicts: map[string]model.AlarmObservationConflict{}, Signals: map[string]model.AlarmObservation{}}
	}
	return r.alarmObservations
}
func (r *Repository) recordAlarmObservationLocked(o model.AlarmObservation) (model.AlarmObservation, bool, error) {
	if err := o.Normalize(); err != nil {
		return o, false, err
	}
	s := r.observationStateLocked()
	inputKey := key(o.TenantID, o.InputKey())
	slotKey := key(o.TenantID, o.SlotKey())
	existing := s.Observations[s.Slots[slotKey]]
	reason := ""
	if hash, ok := s.InputHashes[inputKey]; ok && hash != o.SourceInputHash {
		reason = "SOURCE_INPUT_CONFLICT"
	} else if existing.ID != "" && existing.SourceContentHash != o.SourceContentHash {
		reason = "SOURCE_SLOT_CONFLICT"
	}
	if reason != "" {
		c := model.AlarmObservationConflict{ID: "aoc_" + model.ObservationHash([]string{o.ID, o.SourceInputHash, o.SourceContentHash})[:32], TenantID: o.TenantID, ObservationID: existing.ID, Incoming: clone(o), Reason: reason, RecordedAt: o.RecordedAt}
		if _, exists := s.Conflicts[key(o.TenantID, c.ID)]; !exists {
			r.bumpObservationSourceLocked(o, true)
			for _, original := range s.Observations {
				if original.InputKey() == o.InputKey() {
					r.bumpObservationSourceLocked(original, true)
				}
			}
		}
		s.Conflicts[key(o.TenantID, c.ID)] = c
		o.Acceptance = "CONFLICT"
		o.Reason = reason
		return o, false, nil
	}
	if existing.ID != "" {
		s.Attempts = append(s.Attempts, model.AlarmObservationAttempt{ObservationID: existing.ID, Acceptance: o.Acceptance, Reason: o.Reason, AlarmID: o.AlarmID, EvaluationAt: o.EvaluationAt, RecordedAt: o.RecordedAt})
		return clone(existing), false, nil
	}
	if o.AvailableAt == 0 {
		o.AvailableAt = time.Now().UnixMilli()
		o.AvailabilitySource = "MEMORY_COMMIT"
	}
	s.InputHashes[inputKey] = o.SourceInputHash
	s.Slots[slotKey] = key(o.TenantID, o.ID)
	s.Observations[key(o.TenantID, o.ID)] = clone(o)
	r.bumpObservationSourceLocked(o, false)
	return clone(o), true, nil
}
func (r *Repository) SaveAlarmObservation(_ context.Context, o model.AlarmObservation) (model.AlarmObservation, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.recordAlarmObservationLocked(o)
}
func (r *Repository) GetAlarmObservation(_ context.Context, tenant, id string) (model.AlarmObservation, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.alarmObservations != nil {
		if o, ok := r.alarmObservations.Observations[key(tenant, id)]; ok {
			return clone(o), nil
		}
	}
	return model.AlarmObservation{}, model.ErrNotFound
}
func observationMatches(o model.AlarmObservation, tenant string, f ports.AlarmObservationFilter) bool {
	if o.TenantID != tenant || f.DeviceIDs == nil && f.DeviceID == "" {
		return false
	}
	if f.DeviceIDs != nil && !slices.Contains(f.DeviceIDs, o.DeviceID) {
		return false
	}
	if f.DeviceID != "" && f.DeviceID != o.DeviceID || f.ComponentID != "" && f.ComponentID != o.ComponentID || f.AlarmType != "" && f.AlarmType != o.AlarmType || f.OriginKind != "" && f.OriginKind != o.OriginKind || f.SignalKey != "" && f.SignalKey != o.SignalKey || f.AlarmID != "" && f.AlarmID != o.AlarmID {
		return false
	}
	at := o.TimeAt(f.TimeBasis)
	if f.TimeBasis != "" && f.TimeBasis != "EVENT_AT" && f.TimeBasis != "RECEIVED_AT" && f.TimeBasis != "EVALUATION_AT" && f.TimeBasis != "RECORDED_AT" {
		return false
	}
	if f.TimeBasis != "" && f.TimeBasis != "EVENT_AT" && at <= 0 {
		return false
	}
	if f.Start > 0 && at < f.Start || f.End > 0 && at >= f.End {
		return false
	}
	if f.Cursor != "" {
		p := strings.SplitN(f.Cursor, ":", 2)
		if len(p) != 2 {
			return false
		}
		cursorAt, err := strconv.ParseInt(p[0], 10, 64)
		if err != nil || at < cursorAt || at == cursorAt && o.ID <= p[1] {
			return false
		}
	}
	return true
}
func (r *Repository) ListAlarmObservations(_ context.Context, tenant string, f ports.AlarmObservationFilter) ([]model.AlarmObservation, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []model.AlarmObservation{}
	if r.alarmObservations != nil {
		for _, o := range r.alarmObservations.Observations {
			if observationMatches(o, tenant, f) {
				out = append(out, clone(o))
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TimeAt(f.TimeBasis) != out[j].TimeAt(f.TimeBasis) {
			return out[i].TimeAt(f.TimeBasis) < out[j].TimeAt(f.TimeBasis)
		}
		return out[i].ID < out[j].ID
	})
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func (r *Repository) GetAlarmSignalSeed(_ context.Context, tenant string, f ports.AlarmObservationFilter, before int64) (model.AlarmObservation, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	f.Start = 0
	f.End = before
	f.Cursor = ""
	var seed model.AlarmObservation
	if r.alarmObservations != nil {
		for _, o := range r.alarmObservations.Observations {
			if observationMatches(o, tenant, f) && o.Acceptance == "ACCEPTED" && (o.FactKind == "ASSERT" || o.FactKind == "CLEAR") && (o.TimeAt(f.TimeBasis) > seed.TimeAt(f.TimeBasis) || o.TimeAt(f.TimeBasis) == seed.TimeAt(f.TimeBasis) && o.ID > seed.ID) {
				seed = o
			}
		}
	}
	if seed.ID == "" {
		return seed, model.ErrNotFound
	}
	return clone(seed), nil
}
func (r *Repository) RecoverAlarmSignal(ctx context.Context, o model.AlarmObservation, ruleID string) ([]model.Alarm, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.observationStateLocked()
	sigKey := key(o.TenantID, o.DeviceID, o.SignalKey)
	previous := s.Signals[sigKey]
	o.WatermarkAt = previous.SignalWatermarkAt()
	if o.SignalWatermarkAt() < previous.SignalWatermarkAt() || o.SignalWatermarkAt() == previous.SignalWatermarkAt() && previous.FactKind == "ASSERT" {
		o.Acceptance = "REJECTED"
		o.Reason = "STALE_OR_EQUAL_CLEAR"
	}
	for _, a := range r.alarms {
		if a.TenantID == o.TenantID && a.DeviceID == o.DeviceID && a.RuleID == ruleID && (a.Status == "ACTIVE" || a.Status == "ACKED") {
			o.AlarmID = a.ID
			break
		}
	}
	saved, created, err := r.recordAlarmObservationLocked(o)
	if err != nil {
		return nil, err
	}
	out := []model.Alarm{}
	if !created || saved.Acceptance != "ACCEPTED" {
		return out, nil
	}
	s.Signals[sigKey] = saved
	for k, old := range r.alarms {
		if old.TenantID != o.TenantID || old.DeviceID != o.DeviceID || old.RuleID != ruleID || old.Status != "ACTIVE" && old.Status != "ACKED" {
			continue
		}
		a := cloneAlarm(old)
		a.Version++
		a.Status = "RECOVERED"
		a.RecoveredAt = o.EvaluationAt
		if a.RecoveredAt == 0 {
			a.RecoveredAt = o.RecordedAt
		}
		r.alarms[k] = cloneAlarm(a)
		for _, ev := range model.DutyAlarmEvents(ctx, old, a) {
			r.appendDutyEventLocked(ev)
		}
		out = append(out, a)
	}
	return out, nil
}

// Signal state follows production acceptance; it does not infer recovery from
// platform ACK/CLOSE or absence of a report.
func (r *Repository) acceptSignalLocked(o model.AlarmObservation) {
	if o.Acceptance != "ACCEPTED" || o.FactKind != "ASSERT" && o.FactKind != "CLEAR" {
		return
	}
	s := r.observationStateLocked()
	k := key(o.TenantID, o.DeviceID, o.SignalKey)
	previous := s.Signals[k]
	if o.SignalWatermarkAt() > previous.SignalWatermarkAt() || o.SignalWatermarkAt() == previous.SignalWatermarkAt() && o.FactKind == "ASSERT" {
		s.Signals[k] = clone(o)
	}
}

var _ ports.AlarmObservationStore = (*Repository)(nil)
var _ ports.AlarmObservationRecoveryStore = (*Repository)(nil)

func (r *Repository) bumpObservationSourceLocked(o model.AlarmObservation, conservative bool) {
	if r.governanceSources == nil {
		r.governanceSources = map[string]int64{}
	}
	for _, v := range model.ObservationSourceVersions(o, conservative) {
		r.governanceSources[governanceSourceKey(o.TenantID, v)]++
	}
}

func (t *governanceTx) GetAlarmObservation(id string) (model.AlarmObservation, error) {
	o, ok := t.observations[key(t.tenant, id)]
	if !ok {
		return o, model.ErrNotFound
	}
	return clone(o), nil
}
func (t *governanceTx) ListAlarmObservations(f ports.AlarmObservationFilter) ([]model.AlarmObservation, error) {
	out := []model.AlarmObservation{}
	for _, o := range t.observations {
		if observationMatches(o, t.tenant, f) {
			out = append(out, clone(o))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TimeAt(f.TimeBasis) != out[j].TimeAt(f.TimeBasis) {
			return out[i].TimeAt(f.TimeBasis) < out[j].TimeAt(f.TimeBasis)
		}
		return out[i].ID < out[j].ID
	})
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func (t *governanceTx) GetAlarmSignalSeed(f ports.AlarmObservationFilter, before int64) (model.AlarmObservation, error) {
	f.Start = 0
	f.End = before
	f.Cursor = ""
	var seed model.AlarmObservation
	for _, o := range t.observations {
		if observationMatches(o, t.tenant, f) && o.Acceptance == "ACCEPTED" && (o.FactKind == "ASSERT" || o.FactKind == "CLEAR") && (o.TimeAt(f.TimeBasis) > seed.TimeAt(f.TimeBasis) || o.TimeAt(f.TimeBasis) == seed.TimeAt(f.TimeBasis) && o.ID > seed.ID) {
			seed = o
		}
	}
	if seed.ID == "" {
		return seed, model.ErrNotFound
	}
	return clone(seed), nil
}
