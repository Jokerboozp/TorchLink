package duty

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type fixture struct {
	t                                       *testing.T
	ctx                                     context.Context
	repo                                    *memory.Repository
	s                                       *Service
	admin, day, night                       Actor
	actors                                  map[string]Actor
	now                                     time.Time
	station, dayRoster, nightRoster, dayRun model.DutyDocument
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t, ctx: context.Background(), repo: memory.NewRepository(), now: time.Now().Add(-time.Hour)}
	f.admin = Actor{TenantID: "tenant", Username: "admin", Admin: true, Enabled: true, AllDevices: true, Permissions: []string{"*"}}
	f.day = Actor{TenantID: "tenant", Username: "day", Enabled: true, AllowedDeviceIDs: []string{"d1"}, Permissions: []string{"menu:duty", "action:duty:participate", "action:duty:record", "action:duty:handover", "action:duty:accept", "action:duty:item", "action:duty:ai"}}
	f.night = f.day
	f.night.Username = "night"
	f.actors = map[string]Actor{"admin": f.admin, "day": f.day, "night": f.night}
	f.s = New(f.repo, func(_ context.Context, tenant, user string) (Actor, error) {
		a, ok := f.actors[user]
		if !ok {
			return Actor{}, ErrForbidden
		}
		a.TenantID = tenant
		return a, nil
	})
	f.s.Now = func() time.Time { return f.now }
	for _, id := range []string{"d1", "d2"} {
		if e := f.repo.SaveManagedDevice(f.ctx, model.ManagedDevice{ID: id, TenantID: "tenant", Name: id, AccessKey: id}); e != nil {
			t.Fatal(e)
		}
	}
	f.station = f.doc(f.admin, model.DutyStationKind, "create", "", 0, Station{Name: "消防值班室", SupervisorID: "admin", DeviceIDs: []string{"d1"}, RequiredPeople: 1, Enabled: true, Timezone: "Asia/Shanghai"})
	f.dayRoster = f.doc(f.admin, model.DutyRosterKind, "create", "", 0, Roster{StationID: f.station.ID, StartAt: f.now.UnixMilli(), EndAt: f.now.Add(time.Hour).UnixMilli(), MemberIDs: []string{"day"}, LeaderID: "day"})
	f.dayRoster = f.doc(f.admin, model.DutyRosterKind, "publish", f.dayRoster.ID, f.dayRoster.Version, nil)
	f.nightRoster = f.doc(f.admin, model.DutyRosterKind, "create", "", 0, Roster{StationID: f.station.ID, StartAt: f.now.Add(time.Hour).UnixMilli(), EndAt: f.now.Add(2 * time.Hour).UnixMilli(), MemberIDs: []string{"night"}, LeaderID: "night"})
	f.nightRoster = f.doc(f.admin, model.DutyRosterKind, "publish", f.nightRoster.ID, f.nightRoster.Version, nil)
	f.doc(f.day, model.DutyRunKind, "arrive", "", 0, map[string]any{"rosterId": f.dayRoster.ID})
	f.dayRun = f.doc(f.admin, model.DutyRunKind, "open", "", 0, map[string]any{"rosterId": f.dayRoster.ID, "reason": "首次开班"})
	return f
}
func asDoc(t *testing.T, v any) model.DutyDocument {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	var doc model.DutyDocument
	if e = json.Unmarshal(b, &doc); e != nil {
		t.Fatal(e)
	}
	if doc.ID == "" {
		t.Fatalf("expected document, got %s", b)
	}
	return doc
}
func (f *fixture) cmd(a Actor, k, op, id string, version int64, body any) (any, error) {
	raw, _ := json.Marshal(body)
	return f.s.Execute(f.ctx, a, Command{Kind: k, Operation: op, ID: id, ExpectedVersion: version, IdempotencyKey: fmt.Sprintf("%s-%d", op, time.Now().UnixNano()), Body: raw})
}
func (f *fixture) doc(a Actor, k, op, id string, version int64, body any) model.DutyDocument {
	f.t.Helper()
	v, e := f.cmd(a, k, op, id, version, body)
	if e != nil {
		f.t.Fatal(k, op, e)
	}
	return asDoc(f.t, v)
}
func body[T any](t *testing.T, doc model.DutyDocument) T {
	t.Helper()
	v, e := model.DutyBody[T](doc)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func (f *fixture) revision(h model.DutyDocument) Revision {
	f.t.Helper()
	v := body[Handover](f.t, h)
	d, e := f.s.Get(f.ctx, f.day, model.DutyRevisionKind, v.CurrentRevisionID)
	if e != nil {
		f.t.Fatal(e)
	}
	return body[Revision](f.t, d)
}
func (f *fixture) handover() model.DutyDocument {
	f.t.Helper()
	return f.doc(f.day, model.DutyHandoverKind, "create", "", 0, HandoverEdit{RunID: f.dayRun.ID, NextRosterID: f.nightRoster.ID, HumanNotes: "21点联系维修，保留观察"})
}
func (f *fixture) submit(h model.DutyDocument) model.DutyDocument {
	f.t.Helper()
	v := body[Handover](f.t, h)
	rev := f.revision(h)
	return f.doc(f.day, model.DutyHandoverKind, "submit", h.ID, h.Version, HandoverEdit{RevisionID: v.CurrentRevisionID, SnapshotHash: rev.SnapshotHash})
}

func TestHandoverAtomicAcceptanceCarryAndSnapshot(t *testing.T) {
	f := newFixture(t)
	f.now = f.now.Add(30 * time.Minute)
	_, _, e := f.repo.UpsertAlarm(f.ctx, model.Alarm{ID: "alarm1", TenantID: "tenant", DeviceID: "d1", Status: "ACKED", AlarmType: "FAULT", AlarmLevel: "HIGH", FirstTriggeredAt: f.now.Add(-2 * time.Hour).UnixMilli(), LastTriggeredAt: f.now.Add(-2 * time.Hour).UnixMilli(), AckedAt: f.now.UnixMilli(), TriggerCount: 1})
	if e != nil {
		t.Fatal(e)
	}
	record := f.doc(f.day, model.DutyRecordKind, "create", "", 0, Record{RunID: f.dayRun.ID, DeviceID: "d1", AlarmID: "alarm1", Content: "已联系维修，原因尚未确认"})
	item := f.doc(f.day, model.DutyItemKind, "create", "", 0, Item{RunID: f.dayRun.ID, DeviceID: "d1", AlarmID: "alarm1", Title: "现场复核", NextAction: "核实维修到场", OwnerID: "day"})
	h := f.handover()
	rev := f.revision(h)
	if len(rev.Snapshot.Alarms) != 1 || rev.Snapshot.Alarms[0].Status != "ACKED" || len(rev.Items) != 1 || len(rev.Records) != 1 {
		t.Fatalf("incomplete revision %+v", rev)
	}
	h = f.submit(h)
	f.doc(f.night, model.DutyRunKind, "arrive", "", 0, map[string]any{"rosterId": f.nightRoster.ID})
	delta, e := f.cmd(f.night, model.DutyHandoverKind, "delta", h.ID, 0, nil)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(delta)
	var d struct {
		SnapshotHash string `json:"snapshotHash"`
	}
	_ = json.Unmarshal(raw, &d)
	accepted := f.doc(f.night, model.DutyHandoverKind, "accept", h.ID, h.Version, HandoverEdit{RevisionID: body[Handover](t, h).CurrentRevisionID, SnapshotHash: d.SnapshotHash})
	a := body[Handover](t, accepted)
	if a.Status != "ACCEPTED" || a.Acceptance.UserID != "night" || a.AcceptanceRevisionID == "" {
		t.Fatalf("bad acceptance %+v", a)
	}
	old, e := f.s.Get(f.ctx, f.admin, model.DutyRunKind, f.dayRun.ID)
	if e != nil {
		t.Fatal(e)
	}
	newRun, e := f.s.Get(f.ctx, f.night, model.DutyRunKind, a.NextRunID)
	if e != nil {
		t.Fatal(e)
	}
	r1, r2 := body[Run](t, old), body[Run](t, newRun)
	if r1.Status != "ENDED" || r2.Status != "ACTIVE" || r1.EndedAt != r2.StartedAt {
		t.Fatal(r1, r2)
	}
	updated, e := f.s.Get(f.ctx, f.night, model.DutyItemKind, item.ID)
	if e != nil {
		t.Fatal(e)
	}
	it := body[Item](t, updated)
	if it.OwnerID != "night" || it.RunID != a.NextRunID || it.Status != "OPEN" {
		t.Fatal(it)
	}
	signed, e := f.s.Get(f.ctx, f.night, model.DutyRevisionKind, a.Submission.RevisionID)
	if e != nil {
		t.Fatal(e)
	}
	frozen := body[Revision](t, signed)
	if frozen.Records[0].ID != record.ID || body[Item](t, frozen.Items[0]).OwnerID != "day" {
		t.Fatal("original facts overwritten")
	}
	_, e = f.cmd(f.night, model.DutyItemKind, "complete", item.ID, updated.Version, map[string]any{"result": "现场确认线路正常"})
	if e != nil {
		t.Fatal(e)
	}
	alarm, e := f.repo.GetAlarm(f.ctx, "tenant", "alarm1")
	if e != nil || alarm.Status != "ACKED" {
		t.Fatal("item completion changed alarm", alarm, e)
	}
}

func TestHandoverConflictNewEventsAndRepeatedSigning(t *testing.T) {
	f := newFixture(t)
	h := f.handover()
	h = f.submit(h)
	f.doc(f.night, model.DutyRunKind, "arrive", "", 0, map[string]any{"rosterId": f.nightRoster.ID})
	rev := f.revision(h)
	_, _, e := f.repo.UpsertAlarm(f.ctx, model.Alarm{ID: "new-alarm", TenantID: "tenant", DeviceID: "d1", Status: "ACTIVE", AlarmType: "FIRE", FirstTriggeredAt: f.now.UnixMilli(), LastTriggeredAt: f.now.UnixMilli(), TriggerCount: 1})
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.cmd(f.night, model.DutyHandoverKind, "accept", h.ID, h.Version, HandoverEdit{RevisionID: body[Handover](t, h).CurrentRevisionID, SnapshotHash: rev.SnapshotHash})
	if !errors.Is(e, ErrConflict) {
		t.Fatalf("expected fresh facts conflict: %v", e)
	}
	active, e := f.s.Get(f.ctx, f.admin, model.DutyRunKind, f.dayRun.ID)
	if e != nil || body[Run](t, active).Status != "ACTIVE" {
		t.Fatal("failed acceptance changed actual run", e)
	}
	delta, e := f.cmd(f.night, model.DutyHandoverKind, "delta", h.ID, 0, nil)
	if e != nil {
		t.Fatal(e)
	}
	bytes, _ := json.Marshal(delta)
	var d struct {
		SnapshotHash string `json:"snapshotHash"`
		Changed      bool   `json:"changed"`
	}
	_ = json.Unmarshal(bytes, &d)
	if !d.Changed {
		t.Fatal("new alarm missing from delta")
	}
	req := HandoverEdit{RevisionID: body[Handover](t, h).CurrentRevisionID, SnapshotHash: d.SnapshotHash}
	raw, _ := json.Marshal(req)
	cmd := Command{Kind: model.DutyHandoverKind, ID: h.ID, Operation: "accept", ExpectedVersion: h.Version, IdempotencyKey: "sign-once", Body: raw}
	once, e := f.s.Execute(f.ctx, f.night, cmd)
	if e != nil {
		t.Fatal(e)
	}
	twice, e := f.s.Execute(f.ctx, f.night, cmd)
	if e != nil {
		t.Fatal(e)
	}
	if asDoc(t, once).Version != asDoc(t, twice).Version {
		t.Fatal("duplicate signature mutated")
	}
	cmd.Body = []byte(`{"snapshotHash":"different"}`)
	_, e = f.s.Execute(f.ctx, f.night, cmd)
	if !errors.Is(e, ErrConflict) {
		t.Fatal("idempotency key reused with changed payload", e)
	}
}

func TestScopeAndMembershipAreEnforcedForEveryReadAndMutation(t *testing.T) {
	f := newFixture(t)
	h := f.handover()
	outsider := f.day
	outsider.Username = "outsider"
	f.actors[outsider.Username] = outsider
	if _, e := f.s.Get(f.ctx, outsider, model.DutyHandoverKind, h.ID); !errors.Is(e, ErrForbidden) {
		t.Fatal("outside participant accessed handover", e)
	}
	revoked := f.day
	revoked.AllowedDeviceIDs = []string{}
	for _, kind := range []string{model.DutyHandoverKind, model.DutyRunKind} {
		id := h.ID
		if kind == model.DutyRunKind {
			id = f.dayRun.ID
		}
		if _, e := f.s.Get(f.ctx, revoked, kind, id); !errors.Is(e, ErrForbidden) {
			t.Fatal("scope revocation did not deny entire document", e)
		}
	}
	result, e := f.s.Query(f.ctx, revoked, model.DutyFilter{Kind: model.DutyRevisionKind})
	if e != nil || result.Total != 0 {
		t.Fatal("revoked user sees AI/human revision", result, e)
	}
	_, e = f.cmd(f.day, model.DutyRecordKind, "create", "", 0, Record{RunID: f.dayRun.ID, DeviceID: "d2", Content: "cross-device"})
	if !errors.Is(e, ErrForbidden) {
		t.Fatal("cross-device record accepted", e)
	}
	otherTenant := f.admin
	otherTenant.TenantID = "other"
	if _, e = f.s.Get(f.ctx, otherTenant, model.DutyStationKind, f.station.ID); !errors.Is(e, model.ErrNotFound) {
		t.Fatal("cross-tenant station", e)
	}
}

func TestAIJobImmutableHumanNotesLeaseAndVersionGuard(t *testing.T) {
	f := newFixture(t)
	f.doc(f.day, model.DutyRecordKind, "create", "", 0, Record{RunID: f.dayRun.ID, Content: "人工备注来源"})
	h := f.handover()
	job := f.doc(f.day, model.DutyHandoverKind, "start-ai", h.ID, h.Version, HandoverEdit{})
	claimed, ok, e := f.s.ClaimJob(f.ctx, "tenant", job.ID, "worker1", time.Minute)
	if e != nil || !ok {
		t.Fatal(ok, e)
	}
	if _, ok, e = f.s.ClaimJob(f.ctx, "tenant", job.ID, "worker2", time.Minute); e != nil || ok {
		t.Fatal("duplicate lease", ok, e)
	}
	j := body[AIJob](t, claimed)
	rev := f.revision(h)
	result := &model.DutyAIResult{Summary: "人工已记录，继续现场核实", Highlights: []model.DutyAIStatement{{Text: "已联系维修", EvidenceIDs: []string{rev.Records[0].ID}}}, InputCount: 1, TotalCount: 1}
	done, e := f.s.FinishJob(f.ctx, "tenant", job.ID, "worker1", result, "test-model", "run-1", "")
	if e != nil {
		t.Fatal(e)
	}
	if body[AIJob](t, done).Status != "SUCCEEDED" {
		t.Fatal(done)
	}
	newH, e := f.s.Get(f.ctx, f.day, model.DutyHandoverKind, h.ID)
	if e != nil {
		t.Fatal(e)
	}
	newRev := f.revision(newH)
	if newRev.HumanNotes != rev.HumanNotes || newRev.AI == nil || newRev.Number <= rev.Number {
		t.Fatal("AI overwrote human notes or old version", newRev)
	}
	original, e := f.s.Get(f.ctx, f.day, model.DutyRevisionKind, j.RevisionID)
	if e != nil || body[Revision](t, original).AI != nil {
		t.Fatal("AI modified immutable origin", e)
	}
	job = f.doc(f.day, model.DutyHandoverKind, "start-ai", newH.ID, newH.Version, HandoverEdit{})
	_, ok, e = f.s.ClaimJob(f.ctx, "tenant", job.ID, "worker3", time.Minute)
	if e != nil || !ok {
		t.Fatal(e)
	}
	f.doc(f.day, model.DutyHandoverKind, "update", newH.ID, newH.Version, HandoverEdit{HumanNotes: "值班人员新修改"})
	done, e = f.s.FinishJob(f.ctx, "tenant", job.ID, "worker3", result, "test", "run2", "")
	if e != nil || body[AIJob](t, done).Status != "INTERRUPTED" {
		t.Fatal("stale AI replaced newer human version", e, done)
	}
}

func TestRecordCorrectionAndInvalidAttachmentKeepOriginal(t *testing.T) {
	f := newFixture(t)
	r := f.doc(f.day, model.DutyRecordKind, "create", "", 0, Record{RunID: f.dayRun.ID, Content: "错误备注"})
	corrected := f.doc(f.day, model.DutyRecordKind, "correct", r.ID, r.Version, Record{Content: "更正后备注", CorrectionReason: "补充电话核实"})
	v := body[Record](t, corrected)
	if v.CorrectsID != r.ID || v.AuthorID != "day" {
		t.Fatal(v)
	}
	original, e := f.s.Get(f.ctx, f.day, model.DutyRecordKind, r.ID)
	if e != nil || body[Record](t, original).Content != "错误备注" {
		t.Fatal("correction overwrote original", e)
	}
	_, e = f.cmd(f.day, model.DutyRecordKind, "create", "", 0, Record{RunID: f.dayRun.ID, Content: "假附件", Attachments: []model.DutyAttachment{{ID: "unknown", ObjectKey: "arbitrary"}}})
	if e == nil {
		t.Fatal("arbitrary attachment accepted")
	}
}

func TestRosterCrossMidnightImportAndConflict(t *testing.T) {
	f := newFixture(t)
	tmpl := f.doc(f.admin, model.DutyShiftTemplateKind, "create", "", 0, ShiftTemplate{Name: "夜班", StartTime: "20:00", EndTime: "08:00", EndDayOffset: 1})
	date := time.Now().AddDate(0, 0, 2).Format("2006-01-02")
	req := ImportRequest{CSV: "stationId,date,templateId,memberIds,leaderId,sourceId\n" + f.station.ID + "," + date + "," + tmpl.ID + ",night,night,old-1\n"}
	previewAny, e := f.cmd(f.admin, model.DutyRosterKind, "import-preview", "", 0, req)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(previewAny)
	var preview ImportPreview
	_ = json.Unmarshal(raw, &preview)
	if len(preview.Problems) > 0 || len(preview.Rows) != 1 || preview.Rows[0].EndAt-preview.Rows[0].StartAt != 12*time.Hour.Milliseconds() {
		t.Fatalf("bad cross-midnight preview %+v", preview)
	}
	req.Digest = preview.Digest
	v, e := f.cmd(f.admin, model.DutyRosterKind, "import", "", 0, req)
	if e != nil {
		t.Fatal(e)
	}
	var list Result
	b, _ := json.Marshal(v)
	_ = json.Unmarshal(b, &list)
	if list.Total != 1 {
		t.Fatal(list)
	}
	_, e = f.cmd(f.admin, model.DutyRosterKind, "import", "", 0, req)
	if e == nil {
		t.Fatal("duplicate source import accepted")
	}
	_, e = f.cmd(f.admin, model.DutyRosterKind, "create", "", 0, Roster{StationID: f.station.ID, StartAt: body[Roster](t, f.dayRoster).StartAt, EndAt: body[Roster](t, f.dayRoster).EndAt, MemberIDs: []string{"night"}, LeaderID: "night"})
	if e == nil {
		t.Fatal("same station overlap accepted")
	}
	_, e = f.cmd(f.admin, model.DutyRosterKind, "update", f.dayRoster.ID, f.dayRoster.Version, body[Roster](t, f.dayRoster))
	if e == nil {
		t.Fatal("active roster overwritten")
	}
}

func TestTransientDeviceEventsRemainVisibleAndSignedEventsAreComplete(t *testing.T) {
	f := newFixture(t)
	h := f.handover()
	f.repo.DutyTransaction(f.ctx, "tenant", func(tx ports.DutyTx) error {
		for i := 0; i < 2050; i++ {
			if e := tx.AppendEvent(model.DutyBusinessEvent{ID: fmt.Sprintf("platform-%d", i), Type: "DEVICE_STATUS_CHANGED", Source: "device", DeviceID: "d1", OccurredAt: f.now.UnixMilli(), RecordedAt: time.Now().UnixMilli()}); e != nil {
				return e
			}
		}
		return nil
	})
	delta, e := f.cmd(f.night, model.DutyHandoverKind, "delta", h.ID, 0, nil)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(delta)
	var d struct {
		Changed bool `json:"changed"`
	}
	_ = json.Unmarshal(b, &d)
	if !d.Changed {
		t.Fatal("temporary transitions hidden because final state unchanged")
	}
	updated := f.doc(f.day, model.DutyHandoverKind, "update", h.ID, h.Version, HandoverEdit{HumanNotes: "完整更新"})
	rev := f.revision(updated)
	if len(rev.Events) < 2050 || rev.EventTotal != len(rev.Events) {
		t.Fatal("signed event collection truncated", rev.EventTotal, len(rev.Events))
	}
}

func TestAIStopLeaseRecoveryAndEvidenceRejection(t *testing.T) {
	f := newFixture(t)
	h := f.handover()
	job := f.doc(f.day, model.DutyHandoverKind, "start-ai", h.ID, h.Version, HandoverEdit{})
	running, ok, e := f.s.ClaimJob(f.ctx, "tenant", job.ID, "old-worker", time.Minute)
	if e != nil || !ok {
		t.Fatal(e)
	}
	stopped := f.doc(f.day, model.DutyAIJobKind, "stop", job.ID, running.Version, nil)
	if body[AIJob](t, stopped).Status != "STOP_REQUESTED" {
		t.Fatal(stopped)
	}
	if ok, e = f.s.HeartbeatJob(f.ctx, "tenant", job.ID, "old-worker", "生成中", time.Minute); e != nil || ok {
		t.Fatal(ok, e)
	}
	doc, e := f.s.Get(f.ctx, f.day, model.DutyAIJobKind, job.ID)
	if e != nil || body[AIJob](t, doc).Status != "CANCELLED" {
		t.Fatal(doc, e)
	}
	job = f.doc(f.day, model.DutyHandoverKind, "start-ai", h.ID, h.Version, HandoverEdit{})
	_, ok, e = f.s.ClaimJob(f.ctx, "tenant", job.ID, "old-worker", time.Minute)
	if e != nil || !ok {
		t.Fatal(e)
	}
	f.now = f.now.Add(2 * time.Minute)
	_, ok, e = f.s.ClaimJob(f.ctx, "tenant", job.ID, "new-worker", time.Minute)
	if e != nil || !ok {
		t.Fatal("expired lease not recovered", e)
	}
	if _, e = f.s.FinishJob(f.ctx, "tenant", job.ID, "old-worker", &model.DutyAIResult{Summary: "过时结果"}, "", "", ""); !errors.Is(e, ErrConflict) {
		t.Fatal("stale owner completed newer lease", e)
	}
	failed, e := f.s.FinishJob(f.ctx, "tenant", job.ID, "new-worker", &model.DutyAIResult{Summary: "未知内容", Highlights: []model.DutyAIStatement{{Text: "无来源", EvidenceIDs: []string{"missing-evidence"}}}}, "", "", "")
	if e != nil || body[AIJob](t, failed).Status != "FAILED" {
		t.Fatal("invalid evidence accepted", failed, e)
	}
}

func TestLateEventAmendmentAndRemindersArePersistentIdempotent(t *testing.T) {
	f := newFixture(t)
	f.now = f.now.Add(time.Minute)
	item := f.doc(f.day, model.DutyItemKind, "create", "", 0, Item{RunID: f.dayRun.ID, Title: "逾期维护", NextAction: "联系维修", DueAt: f.now.Add(-time.Minute).UnixMilli()})
	h := f.submit(f.handover())
	f.doc(f.night, model.DutyRunKind, "arrive", "", 0, map[string]any{"rosterId": f.nightRoster.ID})
	delta, e := f.cmd(f.night, model.DutyHandoverKind, "delta", h.ID, 0, nil)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(delta)
	var d struct {
		SnapshotHash string `json:"snapshotHash"`
	}
	_ = json.Unmarshal(raw, &d)
	accepted := f.doc(f.night, model.DutyHandoverKind, "accept", h.ID, h.Version, HandoverEdit{RevisionID: body[Handover](t, h).CurrentRevisionID, SnapshotHash: d.SnapshotHash})
	signed := body[Handover](t, accepted)
	if e = f.repo.DutyTransaction(f.ctx, "tenant", func(tx ports.DutyTx) error {
		return tx.AppendEvent(model.DutyBusinessEvent{ID: "late-event", Source: "device", Type: "DEVICE_OFFLINE", DeviceID: "d1", OccurredAt: f.now.Add(-time.Millisecond).UnixMilli(), RecordedAt: time.Now().UnixMilli()})
	}); e != nil {
		t.Fatal(e)
	}
	if e = f.s.RefreshReminders(f.ctx, "tenant"); e != nil {
		t.Fatal(e)
	}
	if e = f.s.RefreshReminders(f.ctx, "tenant"); e != nil {
		t.Fatal(e)
	}
	updated, e := f.s.Get(f.ctx, f.night, model.DutyHandoverKind, h.ID)
	if e != nil {
		t.Fatal(e)
	}
	a := body[Handover](t, updated)
	if len(a.Amendments) != 1 || a.Acceptance.RevisionID != signed.Acceptance.RevisionID || a.AcceptanceRevisionID != signed.AcceptanceRevisionID {
		t.Fatal("late amendment changed frozen signature or duplicated", a)
	}
	notes, e := f.s.Query(f.ctx, f.night, model.DutyFilter{Kind: model.DutyNotificationKind, Limit: 100})
	if e != nil {
		t.Fatal(e)
	}
	late, overdue := 0, 0
	for _, doc := range notes.Items {
		n := body[Notice](t, doc)
		if n.Type == "LATE_EVENT" {
			late++
		}
		if n.Type == "ITEM_OVERDUE" {
			overdue++
		}
		if n.Type == "ITEM_OVERDUE" && n.ResourceID != item.ID {
			t.Fatal("notification resource must remain navigable item ID")
		}
	}
	if late != 1 || overdue != 1 {
		t.Fatal("reminders missing or duplicated", late, overdue)
	}
	rev, e := f.s.Get(f.ctx, f.night, model.DutyRevisionKind, signed.AcceptanceRevisionID)
	if e != nil {
		t.Fatal(e)
	}
	for _, ev := range body[Revision](t, rev).Events {
		if ev.ID == "late-event" {
			t.Fatal("late event inserted into frozen signature")
		}
	}
}

func TestSubstituteAndExceptionalEndPreserveRoster(t *testing.T) {
	f := newFixture(t)
	roster := body[Roster](t, f.dayRoster)
	run := f.doc(f.admin, model.DutyRunKind, "substitute", f.dayRun.ID, f.dayRun.Version, map[string]any{"userId": "day", "replacementId": "night", "reason": "临时代班"})
	v := body[Run](t, run)
	if v.LeaderID != "night" || len(v.Attendance) != 2 || v.Attendance[0].LeftAt == 0 {
		t.Fatal(v)
	}
	plan, e := f.s.Get(f.ctx, f.admin, model.DutyRosterKind, f.dayRoster.ID)
	if e != nil || body[Roster](t, plan).LeaderID != roster.LeaderID {
		t.Fatal("actual substitute overwrote plan", e)
	}
	ended := f.doc(f.admin, model.DutyRunKind, "end", run.ID, run.Version, map[string]any{"reason": "实际空岗，等待主管安排"})
	if body[Run](t, ended).Status != "ENDED" {
		t.Fatal(ended)
	}
	if _, e = f.cmd(f.night, model.DutyRecordKind, "create", "", 0, Record{RunID: run.ID, Content: "假在岗记录"}); e == nil {
		t.Fatal("ended shift accepted normal records")
	}
	current, e := f.cmd(f.night, model.DutyRunKind, "current", "", 0, nil)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(current)
	var c struct {
		Items []model.DutyDocument `json:"items"`
	}
	_ = json.Unmarshal(raw, &c)
	if len(c.Items) != 0 {
		t.Fatal("ended shift remains in current", c)
	}
}

func TestIdempotentResponsesCannotBypassRevokedScope(t *testing.T) {
	f := newFixture(t)
	raw, _ := json.Marshal(Record{RunID: f.dayRun.ID, DeviceID: "d1", Content: "原授权记录"})
	cmd := Command{Kind: model.DutyRecordKind, Operation: "create", IdempotencyKey: "record-retry", Body: raw}
	if _, e := f.s.Execute(f.ctx, f.day, cmd); e != nil {
		t.Fatal(e)
	}
	revoked := f.day
	revoked.AllowedDeviceIDs = []string{}
	if _, e := f.s.Execute(f.ctx, revoked, cmd); !errors.Is(e, ErrForbidden) {
		t.Fatal("idempotency receipt leaked revoked device", e)
	}
}

func TestCSVActualDatesDoNotRequireTemplates(t *testing.T) {
	f := newFixture(t)
	from := time.Now().Truncate(time.Second).Add(48 * time.Hour).In(time.FixedZone("CST", 8*3600))
	to := from.Add(12 * time.Hour)
	csv := fmt.Sprintf("stationId,startAt,endAt,memberIds,leaderId,sourceId\n%s,%s,%s,night,night,legacy-dates\n", f.station.ID, from.Format(time.RFC3339), to.Format(time.RFC3339))
	result, e := f.cmd(f.admin, model.DutyRosterKind, "import-preview", "", 0, ImportRequest{CSV: csv})
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(result)
	var preview ImportPreview
	_ = json.Unmarshal(raw, &preview)
	if len(preview.Rows) != 1 || len(preview.Problems) > 0 || preview.Rows[0].StartAt != from.UnixMilli() {
		t.Fatal("actual dates import required artificial template", preview)
	}
}

func TestReturnedResubmissionPreservesEveryConfirmation(t *testing.T) {
	f := newFixture(t)
	h := f.submit(f.handover())
	first := body[Handover](t, h).Submission
	returned := f.doc(f.night, model.DutyHandoverKind, "return", h.ID, h.Version, HandoverEdit{Reason: "请补充跟进安排"})
	changed := f.doc(f.day, model.DutyHandoverKind, "update", h.ID, returned.Version, HandoverEdit{HumanNotes: "已补充跟进安排"})
	resubmitted := f.submit(changed)
	v := body[Handover](t, resubmitted)
	if len(v.Confirmations) != 2 || v.Confirmations[0].RevisionID != first.RevisionID || v.Confirmations[0].UserID != "day" || v.Confirmations[0].Type != "SUBMISSION" || v.Confirmations[1].RevisionID == first.RevisionID {
		t.Fatal("old signature lost during resubmission", v)
	}
	original, e := f.s.Get(f.ctx, f.day, model.DutyRevisionKind, first.RevisionID)
	if e != nil || body[Revision](t, original).HumanNotes == "已补充跟进安排" {
		t.Fatal("returned revision overwritten", e)
	}
}

func TestStationOldScopeAndWholeRunNarrativeCannotBeBypassed(t *testing.T) {
	f := newFixture(t)
	other := f.doc(f.admin, model.DutyStationKind, "create", "", 0, Station{Name: "另一值班室", SupervisorID: "admin", DeviceIDs: []string{"d2"}, RequiredPeople: 1, Enabled: true, Timezone: "Asia/Shanghai"})
	limited := f.day
	limited.Permissions = append(limited.Permissions, "action:duty:settings")
	if _, e := f.cmd(limited, model.DutyStationKind, "delete", other.ID, other.Version, nil); !errors.Is(e, ErrForbidden) {
		t.Fatal("limited settings user deleted outside station", e)
	}
	if _, e := f.cmd(limited, model.DutyStationKind, "update", other.ID, other.Version, Station{Name: "越权修改", SupervisorID: "day", DeviceIDs: []string{"d1"}, RequiredPeople: 1, Enabled: true}); !errors.Is(e, ErrForbidden) {
		t.Fatal("limited settings user replaced outside station scope", e)
	}
	if e := f.repo.DutyTransaction(f.ctx, "tenant", func(tx ports.DutyTx) error {
		doc, r, e := read[Run](tx, model.DutyRunKind, f.dayRun.ID)
		if e != nil {
			return e
		}
		r.DeviceIDs = []string{"d1", "d2"}
		_, e = put(tx, doc, r, doc.Version)
		return e
	}); e != nil {
		t.Fatal(e)
	}
	all := f.day
	all.AllowedDeviceIDs = []string{"d1", "d2"}
	f.actors[all.Username] = all
	record := f.doc(all, model.DutyRecordKind, "create", "", 0, Record{RunID: f.dayRun.ID, DeviceID: "d1", Content: "同班另一设备的现场情况也写入本条记录"})
	if _, e := f.s.Get(f.ctx, f.day, model.DutyRecordKind, record.ID); !errors.Is(e, ErrForbidden) {
		t.Fatal("single record deviceId narrowed original whole-run disclosure", e)
	}
	result, e := f.cmd(f.day, "event", "query", "", 0, model.DutyFilter{})
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(result)
	if bytes.Contains(raw, []byte(record.ID)) {
		t.Fatal("raw duty event bypassed complete narrative scope")
	}
}

func TestCancelledNextRosterCannotReceiveAndKnowledgeFlagNeedsPermission(t *testing.T) {
	f := newFixture(t)
	h := f.submit(f.handover())
	f.commandCancel(t, f.nightRoster)
	delta, e := f.cmd(f.night, model.DutyHandoverKind, "delta", h.ID, 0, nil)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(delta)
	var d struct {
		SnapshotHash string `json:"snapshotHash"`
	}
	_ = json.Unmarshal(raw, &d)
	if _, e = f.cmd(f.night, model.DutyHandoverKind, "accept", h.ID, h.Version, HandoverEdit{RevisionID: body[Handover](t, h).CurrentRevisionID, SnapshotHash: d.SnapshotHash}); e == nil {
		t.Fatal("cancelled next roster received responsibility")
	}
	returned := f.doc(f.night, model.DutyHandoverKind, "return", h.ID, h.Version, HandoverEdit{Reason: "班次已取消"})
	if _, e = f.cmd(f.day, model.DutyHandoverKind, "start-ai", h.ID, returned.Version, HandoverEdit{UseKnowledge: true}); e == nil {
		t.Fatal("knowledge flag silently accepted without permission")
	}
}
func (f *fixture) commandCancel(t *testing.T, roster model.DutyDocument) {
	t.Helper()
	f.doc(f.admin, model.DutyRosterKind, "cancel", roster.ID, roster.Version, map[string]any{"reason": "调整排班"})
}
