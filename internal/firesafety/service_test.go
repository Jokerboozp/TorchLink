package firesafety

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/model"
)

type fixture struct {
	s            *Service
	ctx          context.Context
	now          int64
	station      model.FireStation
	people       []model.FirePersonnel
	equipment    model.FireEquipment
	shift        model.DutyShift
	extinguisher model.Extinguisher
}

func raw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func call(t *testing.T, s *Service, action, id string, body any) any {
	t.Helper()
	v, err := s.Apply(context.Background(), "tenant-a", "operator-a", action, id, raw(body))
	if err != nil {
		t.Fatalf("%s: %v", action, err)
	}
	return v
}
func wantError(t *testing.T, s *Service, actor, action, id string, body any, want error) {
	t.Helper()
	_, err := s.Apply(context.Background(), "tenant-a", actor, action, id, raw(body))
	if !errors.Is(err, want) {
		t.Fatalf("%s: got %v, want %v", action, err, want)
	}
}
func newFixture(t *testing.T, people int) fixture {
	t.Helper()
	f := fixture{ctx: context.Background(), now: time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC).UnixMilli()}
	f.s = New(memory.NewRepository())
	f.s.now = func() time.Time { return time.UnixMilli(f.now) }
	f.station = call(t, f.s, "saveStation", "", model.FireStation{Code: "station-1", Name: "一号消防站", Type: "micro", Enabled: true}).(model.FireStation)
	for i := 0; i < people; i++ {
		f.people = append(f.people, call(t, f.s, "savePersonnel", "", model.FirePersonnel{Name: "值班员", StationID: f.station.ID, Enabled: true}).(model.FirePersonnel))
	}
	f.equipment = call(t, f.s, "saveEquipment", "", model.FireEquipment{StationID: f.station.ID, Name: "水带", Category: "救援器材", Unit: "件", Quantity: 2, Status: "ready"}).(model.FireEquipment)
	f.shift = call(t, f.s, "saveShift", "", model.DutyShift{Name: "夜班", StartTime: "22:00", EndTime: "06:00"}).(model.DutyShift)
	f.extinguisher = call(t, f.s, "saveExtinguisher", "", model.Extinguisher{Code: "EXT-1", StationID: f.station.ID, Location: "一层门口", Type: "dry_powder", ManufacturedOn: "2020-01-01", ServiceDueOn: "2027-01-01", RetireOn: "2030-01-01", InspectionCycleDays: 30, Status: "active"}).(model.Extinguisher)
	return f
}
func (f fixture) assignment(person string, start, end int64) model.DutyAssignment {
	return model.DutyAssignment{StationID: f.station.ID, ShiftID: f.shift.ID, PersonnelIDs: []string{person}, StartAt: start, EndAt: end}
}
func (f fixture) dispatch(person string, quantity int) model.FireDispatch {
	v := model.FireDispatch{StationID: f.station.ID, Title: "应急出勤", Type: "fire", Location: "演练现场", PersonnelIDs: []string{person}, StartedAt: f.now}
	if quantity > 0 {
		v.Equipment = []model.FireEquipmentUsage{{EquipmentID: f.equipment.ID, Quantity: quantity}}
	}
	return v
}

// standardChecks returns every standard item, failing the named ones.
func standardChecks(failed ...string) []model.FireInspectionCheck {
	out := []model.FireInspectionCheck{}
	for _, name := range StandardChecks {
		out = append(out, model.FireInspectionCheck{Name: name, Passed: !slices.Contains(failed, name)})
	}
	return out
}
func (f fixture) inspection() model.FireInspection {
	return model.FireInspection{ExtinguisherID: f.extinguisher.ID, AssigneeID: f.people[0].ID, DueAt: f.now + 86400000}
}

func TestDutyConflictSwapAndHistory(t *testing.T) {
	f := newFixture(t, 3)
	start, end := f.now+3600000, f.now+9*3600000
	a := call(t, f.s, "saveAssignment", "", f.assignment(f.people[0].ID, start, end)).(model.DutyAssignment)
	// Actual intervals are authoritative; the shift template need not match them.
	if a.StartAt != start || a.EndAt != end {
		t.Fatal("assignment did not preserve the actual interval")
	}
	wantError(t, f.s, "operator-a", "saveAssignment", "", f.assignment(f.people[0].ID, start+1000, end+1000), ErrConflict)
	adjacent := call(t, f.s, "saveAssignment", "", f.assignment(f.people[0].ID, end, end+3600000)).(model.DutyAssignment)
	call(t, f.s, "deleteAssignment", adjacent.ID, map[string]any{"version": adjacent.Version})
	wantError(t, f.s, "operator-a", "deleteShift", f.shift.ID, map[string]any{"version": f.shift.Version}, ErrConflict)
	bad := f.assignment(f.people[1].ID, start, start)
	wantError(t, f.s, "operator-a", "saveAssignment", "", bad, ErrValidation)
	bad.EndAt = start + 49*3600000
	wantError(t, f.s, "operator-a", "saveAssignment", "", bad, ErrValidation)

	swap := call(t, f.s, "createSwap", "", model.DutySwap{AssignmentID: a.ID, FromPersonnelID: f.people[0].ID, ToPersonnelID: f.people[1].ID, Reason: "调整值班"}).(model.DutySwap)
	wantError(t, f.s, "operator-a", "reviewSwap", swap.ID, map[string]any{"version": swap.Version, "approved": true}, ErrForbidden)
	// A request does not reserve the substitute: approval rechecks new conflicts.
	busy := call(t, f.s, "saveAssignment", "", f.assignment(f.people[1].ID, start, end)).(model.DutyAssignment)
	wantError(t, f.s, "reviewer", "reviewSwap", swap.ID, map[string]any{"version": swap.Version, "approved": true}, ErrConflict)
	call(t, f.s, "deleteAssignment", busy.ID, map[string]any{"version": busy.Version})
	approved, err := f.s.Apply(f.ctx, "tenant-a", "reviewer", "reviewSwap", swap.ID, raw(map[string]any{"version": swap.Version, "approved": true, "note": "同意"}))
	if err != nil || approved.(model.DutySwap).Status != "approved" {
		t.Fatal(approved, err)
	}
	state, err := f.s.Snapshot(f.ctx, "tenant-a")
	if err != nil || len(state.Assignments) != 1 || state.Assignments[0].PersonnelIDs[0] != f.people[1].ID || state.Assignments[0].Version != 2 {
		t.Fatal(state.Assignments, err)
	}
	wantError(t, f.s, "reviewer", "reviewSwap", swap.ID, map[string]any{"version": int64(2), "approved": true}, ErrConflict)
	wantError(t, f.s, "operator-a", "saveAssignment", a.ID, a, ErrConflict)
	wantError(t, f.s, "operator-a", "deleteAssignment", a.ID, map[string]any{"version": 2}, ErrConflict)
	wantError(t, f.s, "operator-a", "deletePersonnel", f.people[0].ID, map[string]any{"version": f.people[0].Version}, ErrConflict)
	p := f.people[1]
	p.Enabled = false
	wantError(t, f.s, "operator-a", "savePersonnel", p.ID, p, ErrConflict)
}

func TestDispatchOccupancyReturnAndHistory(t *testing.T) {
	f := newFixture(t, 2)
	d := call(t, f.s, "createDispatch", "", f.dispatch(f.people[0].ID, 2)).(model.FireDispatch)
	wantError(t, f.s, "operator-a", "createDispatch", "", f.dispatch(f.people[1].ID, 1), ErrConflict)
	wantError(t, f.s, "operator-a", "createDispatch", "", f.dispatch(f.people[0].ID, 0), ErrConflict)
	e := f.equipment
	e.Quantity = 1
	wantError(t, f.s, "operator-a", "saveEquipment", e.ID, e, ErrConflict)
	e.Quantity, e.Status = 2, "maintenance"
	wantError(t, f.s, "operator-a", "saveEquipment", e.ID, e, ErrConflict)
	p := f.people[0]
	p.Enabled = false
	wantError(t, f.s, "operator-a", "savePersonnel", p.ID, p, ErrConflict)
	s := f.station
	s.Enabled = false
	wantError(t, f.s, "operator-a", "saveStation", s.ID, s, ErrConflict)
	wantError(t, f.s, "operator-a", "returnDispatch", d.ID, map[string]any{"version": d.Version}, ErrValidation)
	wantError(t, f.s, "operator-a", "returnDispatch", d.ID, map[string]any{"version": d.Version, "summary": "已完成", "returnedAt": f.now - 1}, ErrValidation)
	d = call(t, f.s, "returnDispatch", d.ID, map[string]any{"version": d.Version, "summary": "已完成"}).(model.FireDispatch)
	if d.Status != "returned" || d.ReturnedBy != "operator-a" || d.ReturnedAt != f.now {
		t.Fatal(d)
	}
	wantError(t, f.s, "operator-a", "returnDispatch", d.ID, map[string]any{"version": d.Version, "summary": "重复"}, ErrConflict)
	call(t, f.s, "createDispatch", "", f.dispatch(f.people[0].ID, 2))
	wantError(t, f.s, "operator-a", "deleteEquipment", f.equipment.ID, map[string]any{"version": f.equipment.Version}, ErrConflict)
}

func TestInspectionRectificationReviewCycle(t *testing.T) {
	f := newFixture(t, 1)
	in := f.inspection()
	in.Status, in.Result, in.InspectedBy = "completed", "pass", "forged"
	task := call(t, f.s, "createInspection", "", in).(model.FireInspection)
	if task.Status != "pending" || task.Result != "" || task.InspectedBy != "" {
		t.Fatal("creation trusted execution fields", task)
	}
	wantError(t, f.s, "operator-a", "createInspection", "", f.inspection(), ErrConflict)
	e := f.extinguisher
	e.Status = "retired"
	wantError(t, f.s, "operator-a", "saveExtinguisher", e.ID, e, ErrConflict)
	p := f.people[0]
	p.Enabled = false
	wantError(t, f.s, "operator-a", "savePersonnel", p.ID, p, ErrConflict)
	s := f.station
	s.Enabled = false
	wantError(t, f.s, "operator-a", "saveStation", s.ID, s, ErrConflict)
	wantError(t, f.s, "operator-a", "inspect", task.ID, map[string]any{"version": task.Version, "checks": []any{}}, ErrValidation)
	wantError(t, f.s, "operator-a", "inspect", task.ID, map[string]any{"version": task.Version, "checks": append(standardChecks(), model.FireInspectionCheck{Name: "外观及筒体", Passed: true})}, ErrValidation)
	wantError(t, f.s, "operator-a", "inspect", task.ID, map[string]any{"version": task.Version, "checks": standardChecks("压力指示")}, ErrValidation)
	// Standard items cannot be renamed or omitted; custom items may be added.
	wantError(t, f.s, "operator-a", "inspect", task.ID, map[string]any{"version": task.Version, "checks": []model.FireInspectionCheck{{Name: "外观", Passed: true}}}, ErrValidation)
	task = call(t, f.s, "inspect", task.ID, map[string]any{"version": task.Version, "checks": append(standardChecks("压力指示"), model.FireInspectionCheck{Name: "箱体", Passed: true}), "findings": "压力不足", "result": "pass"}).(model.FireInspection)
	if task.Result != "fail" || task.Status != "rectifying" {
		t.Fatal("result was not calculated", task)
	}
	task = call(t, f.s, "rectify", task.ID, map[string]any{"version": task.Version, "action": "更换压力表"}).(model.FireInspection)
	wantError(t, f.s, "operator-a", "reviewInspection", task.ID, map[string]any{"version": task.Version, "approved": true}, ErrForbidden)
	wantError(t, f.s, "reviewer", "reviewInspection", task.ID, map[string]any{"version": task.Version, "approved": false}, ErrValidation)
	value, err := f.s.Apply(f.ctx, "tenant-a", "reviewer", "reviewInspection", task.ID, raw(map[string]any{"version": task.Version, "approved": false, "note": "压力仍不足"}))
	if err != nil {
		t.Fatal(err)
	}
	task = value.(model.FireInspection)
	if task.Status != "rectifying" || task.Rectifications[0].Status != "rejected" {
		t.Fatal(task)
	}
	task = call(t, f.s, "rectify", task.ID, map[string]any{"version": task.Version, "action": "替换合格灭火器"}).(model.FireInspection)
	value, err = f.s.Apply(f.ctx, "tenant-a", "reviewer", "reviewInspection", task.ID, raw(map[string]any{"version": task.Version, "approved": true}))
	if err != nil {
		t.Fatal(err)
	}
	task = value.(model.FireInspection)
	if task.Status != "completed" || task.Result != "fail" || len(task.Rectifications) != 2 || task.Rectifications[0].ReviewNote != "压力仍不足" || task.Rectifications[1].Status != "approved" {
		t.Fatal("lost rectification history", task)
	}
	wantError(t, f.s, "reviewer", "reviewInspection", task.ID, map[string]any{"version": task.Version, "approved": true}, ErrConflict)
	wantError(t, f.s, "operator-a", "deleteExtinguisher", e.ID, map[string]any{"version": e.Version}, ErrConflict)
	next := call(t, f.s, "createInspection", "", f.inspection()).(model.FireInspection)
	wantError(t, f.s, "operator-a", "cancelInspection", next.ID, map[string]any{"version": next.Version}, ErrValidation)
	next = call(t, f.s, "cancelInspection", next.ID, map[string]any{"version": next.Version, "reason": "重复计划"}).(model.FireInspection)
	if next.Status != "cancelled" {
		t.Fatal(next)
	}
	wantError(t, f.s, "operator-a", "cancelInspection", next.ID, map[string]any{"version": next.Version, "reason": "重复"}, ErrConflict)
	call(t, f.s, "saveExtinguisher", e.ID, e)
	wantError(t, f.s, "operator-a", "createInspection", "", f.inspection(), ErrConflict)
}

func TestInspectionPassAndStateVersionIsolation(t *testing.T) {
	f := newFixture(t, 1)
	task := call(t, f.s, "createInspection", "", f.inspection()).(model.FireInspection)
	task = call(t, f.s, "inspect", task.ID, map[string]any{"version": task.Version, "checks": standardChecks()}).(model.FireInspection)
	if task.Status != "completed" || task.Result != "pass" {
		t.Fatal(task)
	}
	wantError(t, f.s, "operator-a", "inspect", task.ID, map[string]any{"version": task.Version, "checks": standardChecks()}, ErrConflict)
	other, err := f.s.Snapshot(f.ctx, "tenant-b")
	if err != nil || len(other.Stations) != 0 || other.Stations == nil || other.Revision != 0 {
		t.Fatal(other, err)
	}
	_, err = f.s.Apply(f.ctx, "tenant-b", "operator-a", "savePersonnel", "", raw(model.FirePersonnel{Name: "越权人员", StationID: f.station.ID, Enabled: true}))
	if !errors.Is(err, ErrValidation) {
		t.Fatal("cross-tenant station accepted", err)
	}
	_, err = f.s.Apply(f.ctx, "tenant-b", "operator-a", "deleteStation", f.station.ID, raw(map[string]any{"version": 1}))
	if !errors.Is(err, ErrNotFound) {
		t.Fatal("cross-tenant delete accepted", err)
	}
	v := f.station
	v.Name = "更新名称"
	updated := call(t, f.s, "saveStation", v.ID, v).(model.FireStation)
	if updated.Version != 2 || updated.CreatedAt != f.station.CreatedAt {
		t.Fatal(updated)
	}
	before, _ := f.s.Snapshot(f.ctx, "tenant-a")
	wantError(t, f.s, "operator-a", "saveStation", v.ID, v, ErrConflict)
	after, _ := f.s.Snapshot(f.ctx, "tenant-a")
	if after.Revision != before.Revision || after.Stations[0].Name != "更新名称" {
		t.Fatal("stale update committed", after)
	}
	ctx, cancel := context.WithCancel(f.ctx)
	cancel()
	_, err = f.s.Apply(ctx, "tenant-a", "operator-a", "saveStation", "", raw(v))
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestConcurrentBusinessClaimsRevalidateCAS(t *testing.T) {
	for _, test := range []struct {
		name, action string
		maximum      int
	}{{"inspection", "createInspection", 1}, {"assignment", "saveAssignment", 1}, {"dispatch", "createDispatch", 2}} {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t, 12)
			var successes atomic.Int32
			var wg sync.WaitGroup
			for i := 0; i < 12; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					var body any
					switch test.name {
					case "inspection":
						body = f.inspection()
					case "assignment":
						body = f.assignment(f.people[0].ID, f.now+1000, f.now+3600000)
					case "dispatch":
						body = f.dispatch(f.people[i].ID, 1)
					}
					_, err := f.s.Apply(f.ctx, "tenant-a", "operator-a", test.action, "", raw(body))
					if err == nil {
						successes.Add(1)
					} else if !errors.Is(err, ErrConflict) {
						t.Errorf("unexpected concurrent error: %v", err)
					}
				}(i)
			}
			wg.Wait()
			if int(successes.Load()) != test.maximum {
				t.Fatalf("%d successful claims, want %d", successes.Load(), test.maximum)
			}
			state, err := f.s.Snapshot(f.ctx, "tenant-a")
			if err != nil {
				t.Fatal(err)
			}
			if test.name == "dispatch" && occupiedEquipment(&state, f.equipment.ID) != 2 {
				t.Fatal("inventory oversubscribed", state.Dispatches)
			}
		})
	}
}

func TestResourceValidationAndStationRelationships(t *testing.T) {
	f := newFixture(t, 1)
	wantError(t, f.s, "operator-a", "saveStation", "", f.station, ErrConflict)
	s := f.station
	s.Longitude = 181
	wantError(t, f.s, "operator-a", "saveStation", s.ID, s, ErrValidation)
	s = f.station
	s.Type = "unknown"
	wantError(t, f.s, "operator-a", "saveStation", s.ID, s, ErrValidation)
	wantError(t, f.s, "operator-a", "deleteStation", f.station.ID, map[string]any{"version": 1}, ErrConflict)
	e := f.extinguisher
	e.ManufacturedOn = "2026-02-30"
	wantError(t, f.s, "operator-a", "saveExtinguisher", e.ID, e, ErrValidation)
	e = f.extinguisher
	e.ServiceDueOn = "2019-01-01"
	wantError(t, f.s, "operator-a", "saveExtinguisher", e.ID, e, ErrValidation)
	e = f.extinguisher
	e.InspectionCycleDays = 0
	wantError(t, f.s, "operator-a", "saveExtinguisher", e.ID, e, ErrValidation)
	e = f.extinguisher
	e.StationID = ""
	wantError(t, f.s, "operator-a", "saveExtinguisher", e.ID, e, ErrValidation)
	e = f.extinguisher
	e.ManufacturedOn = time.UnixMilli(f.now).In(time.Local).AddDate(0, 0, 1).Format("2006-01-02")
	wantError(t, f.s, "operator-a", "saveExtinguisher", e.ID, e, ErrValidation)
	call(t, f.s, "deleteExtinguisher", f.extinguisher.ID, map[string]any{"version": 1})
	call(t, f.s, "deleteEquipment", f.equipment.ID, map[string]any{"version": 1})
	call(t, f.s, "deletePersonnel", f.people[0].ID, map[string]any{"version": 1})
	call(t, f.s, "deleteShift", f.shift.ID, map[string]any{"version": 1})
	call(t, f.s, "deleteStation", f.station.ID, map[string]any{"version": 1})
	state, err := f.s.Snapshot(f.ctx, "tenant-a")
	if err != nil || len(state.Stations)+len(state.Personnel)+len(state.Equipment)+len(state.Shifts)+len(state.Extinguishers) != 0 {
		t.Fatal(state, err)
	}
}

func TestCrossStationInspectionAndHistoricalDutyReferences(t *testing.T) {
	f := newFixture(t, 1)
	other := call(t, f.s, "saveStation", "", model.FireStation{Code: "station-2", Name: "二号消防站", Type: "professional", Enabled: true}).(model.FireStation)
	otherExtinguisher := f.extinguisher
	otherExtinguisher.Code, otherExtinguisher.StationID = "EXT-2", other.ID
	otherExtinguisher = call(t, f.s, "saveExtinguisher", "", otherExtinguisher).(model.Extinguisher)
	// The inspector's home station remains active even for another station's task.
	task := f.inspection()
	task.ExtinguisherID = otherExtinguisher.ID
	created := call(t, f.s, "createInspection", "", task).(model.FireInspection)
	s := f.station
	s.Enabled = false
	wantError(t, f.s, "operator-a", "saveStation", s.ID, s, ErrConflict)
	call(t, f.s, "cancelInspection", created.ID, map[string]any{"version": created.Version, "reason": "调整计划"})
	start, end := f.now-24*3600000, f.now-20*3600000
	call(t, f.s, "saveAssignment", "", f.assignment(f.people[0].ID, start, end))
	p := f.people[0]
	p.StationID = other.ID
	p = call(t, f.s, "savePersonnel", p.ID, p).(model.FirePersonnel)
	historicalOverlap := f.assignment(p.ID, start+1000, end+1000)
	historicalOverlap.StationID = other.ID
	wantError(t, f.s, "operator-a", "saveAssignment", "", historicalOverlap, ErrConflict)
	// Adjacent intervals at the new station remain valid.
	historicalOverlap.StartAt, historicalOverlap.EndAt = end, end+3600000
	call(t, f.s, "saveAssignment", "", historicalOverlap)
	wantError(t, f.s, "operator-a", "deletePersonnel", p.ID, map[string]any{"version": p.Version}, ErrConflict)
}

func TestBatchAssignmentsCommitAllOrReportEveryConflict(t *testing.T) {
	f := newFixture(t, 2)
	day := int64(24 * time.Hour / time.Millisecond)
	start := f.now + day
	batch := func(days int, groups ...[]string) map[string]any {
		items := []model.DutyAssignment{}
		for i := 0; i < days; i++ {
			items = append(items, model.DutyAssignment{StationID: f.station.ID, ShiftID: f.shift.ID, PersonnelIDs: groups[i%len(groups)], StartAt: start + int64(i)*day, EndAt: start + int64(i)*day + 8*3600000})
		}
		return map[string]any{"assignments": items}
	}
	call(t, f.s, "saveAssignment", "", f.assignment(f.people[0].ID, start+2*day, start+2*day+3600000))
	_, err := f.s.Apply(f.ctx, "tenant-a", "operator-a", "createAssignments", "", raw(batch(4, []string{f.people[0].ID}, []string{f.people[1].ID})))
	if !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "未保存任何排班") {
		t.Fatalf("conflicting batch was not rejected: %v", err)
	}
	state, _ := f.s.Snapshot(f.ctx, "tenant-a")
	if len(state.Assignments) != 1 {
		t.Fatal("rejected batch saved assignments", state.Assignments)
	}
	result := call(t, f.s, "createAssignments", "", batch(2, []string{f.people[0].ID}, []string{f.people[1].ID})).(map[string]any)
	if result["created"] != 2 {
		t.Fatal(result)
	}
	// Overlap inside the same batch is a conflict too.
	overlap := batch(1, []string{f.people[1].ID})
	overlap["assignments"] = append(overlap["assignments"].([]model.DutyAssignment), overlap["assignments"].([]model.DutyAssignment)...)
	wantError(t, f.s, "operator-a", "createAssignments", "", overlap, ErrConflict)
	wantError(t, f.s, "operator-a", "createAssignments", "", map[string]any{"assignments": []model.DutyAssignment{}}, ErrValidation)
	wantError(t, f.s, "operator-a", "createAssignments", "", batch(63, []string{f.people[0].ID}), ErrValidation)
}

func TestBatchInspectionsSkipOpenAndRetiredAssets(t *testing.T) {
	f := newFixture(t, 1)
	second := call(t, f.s, "saveExtinguisher", "", model.Extinguisher{Code: "EXT-2", StationID: f.station.ID, Location: "二层", Type: "co2", ManufacturedOn: "2020-01-01", ServiceDueOn: "2027-01-01", RetireOn: "2030-01-01", InspectionCycleDays: 30, Status: "active"}).(model.Extinguisher)
	retired := call(t, f.s, "saveExtinguisher", "", model.Extinguisher{Code: "EXT-3", StationID: f.station.ID, Location: "三层", Type: "co2", ManufacturedOn: "2020-01-01", ServiceDueOn: "2027-01-01", RetireOn: "2030-01-01", InspectionCycleDays: 30, Status: "retired"}).(model.Extinguisher)
	call(t, f.s, "createInspection", "", f.inspection())
	body := map[string]any{"extinguisherIds": []string{f.extinguisher.ID, second.ID, retired.ID}, "assigneeId": f.people[0].ID, "dueAt": f.now + 86400000}
	result := call(t, f.s, "createInspections", "", body).(map[string]any)
	if result["created"] != 1 || len(result["items"].([]model.FireInspection)) != 1 || result["items"].([]model.FireInspection)[0].ExtinguisherID != second.ID {
		t.Fatal("batch did not create exactly the eligible task", result)
	}
	encoded, _ := json.Marshal(result["skipped"])
	if !strings.Contains(string(encoded), "EXT-1") || !strings.Contains(string(encoded), "EXT-3") {
		t.Fatal("skipped assets were not reported", string(encoded))
	}
	wantError(t, f.s, "operator-a", "createInspections", "", body, ErrConflict)
	fresh := call(t, f.s, "saveExtinguisher", "", model.Extinguisher{Code: "EXT-4", StationID: f.station.ID, Location: "四层", Type: "co2", ManufacturedOn: "2020-01-01", ServiceDueOn: "2027-01-01", RetireOn: "2030-01-01", InspectionCycleDays: 30, Status: "active"}).(model.Extinguisher)
	body["assigneeId"] = "missing"
	body["extinguisherIds"] = []string{fresh.ID}
	wantError(t, f.s, "operator-a", "createInspections", "", body, ErrValidation)
}

type overlapStore struct{ *memory.Repository }

func (s overlapStore) SaveFireSafetyFrom(ctx context.Context, tenant string, _, next model.FireSafetyState) (bool, error) {
	return s.SaveFireSafetyState(ctx, tenant, next)
}

func (overlapStore) SaveFireSafetyState(context.Context, string, model.FireSafetyState) (bool, error) {
	return false, model.ErrDutyOverlap
}

func TestStorageOverlapIsReportedAsConflict(t *testing.T) {
	s := New(overlapStore{memory.NewRepository()})
	_, err := s.Apply(context.Background(), "t", "admin", "saveStation", "", json.RawMessage(`{"code":"S1","name":"一站","type":"micro","enabled":true}`))
	if !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "重叠排班") {
		t.Fatalf("storage overlap: %v", err)
	}
}
