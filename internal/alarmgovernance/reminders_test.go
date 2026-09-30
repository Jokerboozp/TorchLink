package alarmgovernance

import (
	"context"
	"errors"
	"fmt"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"sync"
	"testing"
	"time"
)

func overdueFixture(t *testing.T) (*fixture, model.GovernanceDocument, model.GovernanceCase) {
	t.Helper()
	f := setup(t)
	c := f.newCase(t)
	body, _ := model.GovernanceBody[model.GovernanceCase](c)
	measure := f.must(t, command(model.GovernanceMeasureKind, "", body.CurrentRoundID, "create", model.ImprovementMeasure{Content: "进一步检查设备和环境", OwnerUserID: f.a.Username, DueAt: f.now - 1000, Basis: "现场核实计划", RequiresAcceptance: true}, 0))
	return f, measure, body
}
func TestUserRemindersAreIdempotentRecipientScopedAndReadHandled(t *testing.T) {
	f, _, _ := overdueFixture(t)
	n, e := f.s.RefreshUserReminders(f.ctx, f.a)
	if e != nil || n != 1 {
		t.Fatal(n, e)
	}
	n, e = f.s.RefreshUserReminders(f.ctx, f.a)
	if e != nil || n != 0 {
		t.Fatal("duplicate reminder", n, e)
	}
	items, total, e := f.s.UserReminders(f.ctx, f.a, "UNREAD", 20, 0)
	if e != nil || total != 1 || len(items) != 1 {
		t.Fatal(items, total, e)
	}
	v, _ := model.GovernanceBody[model.GovernanceReminder](items[0])
	if v.Category != "MEASURE_OVERDUE" || v.Message == "" || v.UserID != f.a.Username || v.Count != 1 {
		t.Fatal(v)
	}
	other := f.a
	other.Username = "other"
	foreign, total, e := f.s.UserReminders(f.ctx, other, "", 20, 0)
	if e != nil || len(foreign) != 0 || total != 0 {
		t.Fatal("recipient data leaked", foreign, total, e)
	}
	if _, e = f.s.UpdateReminder(f.ctx, other, items[0].ID, "READ", items[0].Version, "foreign"); !errors.Is(e, ErrForbidden) {
		t.Fatal("foreign recipient changed reminder", e)
	}
	first, e := f.s.UpdateReminder(f.ctx, f.a, items[0].ID, "READ", items[0].Version, "read-once")
	if e != nil {
		t.Fatal(e)
	}
	again, e := f.s.UpdateReminder(f.ctx, f.a, items[0].ID, "READ", items[0].Version, "read-once")
	if e != nil || again.Version != first.Version {
		t.Fatal("retry changed version", again, e)
	}
	if _, e = f.s.UpdateReminder(f.ctx, f.a, items[0].ID, "READ", first.Version, "read-once"); !errors.Is(e, model.ErrGovernanceConflict) {
		t.Fatal("same idempotency key with changed body accepted", e)
	}
	handled, e := f.s.UpdateReminder(f.ctx, f.a, items[0].ID, "HANDLED", first.Version, "handle-once")
	if e != nil {
		t.Fatal(e)
	}
	v, _ = model.GovernanceBody[model.GovernanceReminder](handled)
	if v.ReadAt == 0 || v.HandledAt == 0 || v.Status != "HANDLED" {
		t.Fatal(v)
	}
	if _, e = f.s.UpdateReminder(f.ctx, f.a, items[0].ID, "READ", handled.Version, "read-again"); !errors.Is(e, model.ErrGovernanceInvalid) {
		t.Fatal("handled task reopened by read", e)
	}
}
func TestReminderSourcePermissionAndDeviceRevocationAreAppliedBeforeResponse(t *testing.T) {
	f, _, _ := overdueFixture(t)
	if _, e := f.s.RefreshUserReminders(f.ctx, f.a); e != nil {
		t.Fatal(e)
	}
	f.s.AuthorizeSource = func(context.Context, Actor, model.GovernanceDocument) error { return ErrForbidden }
	items, n, e := f.s.UserReminders(f.ctx, f.a, "", 20, 0)
	if e != nil || n != 0 || len(items) != 0 {
		t.Fatal("source revoke leaked task", items, n, e)
	}
	f.s.AuthorizeSource = nil
	f.s.Resolve = func(_ context.Context, a Actor) (Actor, error) {
		a.AllDevices = false
		a.DeviceIDs = []string{}
		return a, nil
	}
	items, n, e = f.s.UserReminders(f.ctx, f.a, "", 20, 0)
	if e != nil || n != 0 || len(items) != 0 {
		t.Fatal("empty scope leaked task", items, n, e)
	}
}
func TestReminderWriteRechecksRecordPermissionInTransaction(t *testing.T) {
	f, _, _ := overdueFixture(t)
	if _, e := f.s.RefreshUserReminders(f.ctx, f.a); e != nil {
		t.Fatal(e)
	}
	items, _, e := f.s.UserReminders(f.ctx, f.a, "", 20, 0)
	if e != nil {
		t.Fatal(e)
	}
	f.s.ResolveTx = func(_ ports.AlarmGovernanceTx, a Actor) (Actor, error) {
		a.Permissions = []string{"menu:alarmGovernance", "menu:devices", "menu:alarms"}
		return a, nil
	}
	if _, e = f.s.UpdateReminder(f.ctx, f.a, items[0].ID, "HANDLED", items[0].Version, "revoked"); !errors.Is(e, ErrForbidden) {
		t.Fatal("revoked action committed", e)
	}
}
func TestPendingAcceptanceReplacesStaleOverdueReminder(t *testing.T) {
	f, measure, _ := overdueFixture(t)
	if _, e := f.s.RefreshUserReminders(f.ctx, f.a); e != nil {
		t.Fatal(e)
	}
	f.must(t, command(measure.Kind, measure.ID, "", "implement", model.ImprovementMeasure{Implementation: "检查并完成实施"}, measure.Version))
	n, e := f.s.RefreshUserReminders(f.ctx, f.a)
	if e != nil || n != 1 {
		t.Fatal(n, e)
	}
	items, n, e := f.s.UserReminders(f.ctx, f.a, "", 20, 0)
	if e != nil || n != 1 {
		t.Fatal(items, n, e)
	}
	v, _ := model.GovernanceBody[model.GovernanceReminder](items[0])
	if v.Category != "ACCEPTANCE_PENDING" {
		t.Fatal(v)
	}
}
func TestRoundEndingInvalidatesReminderWithoutChangingTaskVersion(t *testing.T) {
	f, _, c := overdueFixture(t)
	if _, err := f.s.RefreshUserReminders(f.ctx, f.a); err != nil {
		t.Fatal(err)
	}
	items, _, err := f.s.UserReminders(f.ctx, f.a, "", 20, 0)
	if err != nil || len(items) != 1 {
		t.Fatal(items, err)
	}
	if err = f.repo.GovernanceTransaction(f.ctx, f.a.TenantID, func(tx ports.AlarmGovernanceTx) error {
		d, round, e := read[model.GovernanceRound](tx, model.GovernanceRoundKind, c.CurrentRoundID)
		if e != nil {
			return e
		}
		round.Status = "TERMINATED"
		d.Status = round.Status
		_, e = put(tx, d, round, d.Version)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	visible, n, err := f.s.UserReminders(f.ctx, f.a, "", 20, 0)
	if err != nil || n != 0 || len(visible) != 0 {
		t.Fatal("ended round retained obsolete task", visible, n, err)
	}
	if _, err = f.s.UpdateReminder(f.ctx, f.a, items[0].ID, "HANDLED", items[0].Version, "ended"); !errors.Is(err, model.ErrGovernanceConflict) {
		t.Fatal("obsolete task was handled", err)
	}
}
func TestConcurrentReminderRefreshCreatesOnePerUserTaskScope(t *testing.T) {
	f, _, _ := overdueFixture(t)
	var wg sync.WaitGroup
	counts := make(chan int, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); n, e := f.s.RefreshUserReminders(f.ctx, f.a); counts <- n; errs <- e }()
	}
	wg.Wait()
	close(counts)
	close(errs)
	sum := 0
	for n := range counts {
		sum += n
	}
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	if sum != 1 {
		t.Fatal("concurrent refresh duplicated task", sum)
	}
}

func TestInsufficientReviewAndCompletedNewReportOnlyCreateHumanFollowup(t *testing.T) {
	f := setup(t)
	doc := f.newCase(t)
	c, _ := model.GovernanceBody[model.GovernanceCase](doc)
	ended := f.now - 500
	if err := f.repo.GovernanceTransaction(f.ctx, f.a.TenantID, func(tx ports.AlarmGovernanceTx) error {
		current, body, e := read[model.GovernanceCase](tx, model.GovernanceCaseKind, doc.ID)
		if e != nil {
			return e
		}
		body.Status = "COMPLETED"
		current.Status = body.Status
		if _, e = put(tx, current, body, current.Version); e != nil {
			return e
		}
		rd, round, e := read[model.GovernanceRound](tx, model.GovernanceRoundKind, c.CurrentRoundID)
		if e != nil {
			return e
		}
		round.Status = "COMPLETED"
		round.EndedAt = &ended
		rd.Status = round.Status
		if _, e = put(tx, rd, round, rd.Version); e != nil {
			return e
		}
		_, e = put(tx, model.GovernanceDocument{Kind: model.GovernanceReviewKind, ID: "insufficient-review", CaseID: doc.ID, RoundID: c.CurrentRoundID, DeviceIDs: doc.DeviceIDs, Status: "CONFIRMED", CreatedBy: f.a.Username}, model.ObservationReview{RoundID: c.CurrentRoundID, Status: "CONFIRMED", Conclusion: "INSUFFICIENT_DATA", FollowupOwnerUserID: f.a.Username}, 0)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	// More than the 200-row source page ensures refresh advances its cursor.
	for i := 0; i < 201; i++ {
		report := f.obs
		report.ID = ""
		report.SourceEventID = fmt.Sprintf("after-completion-%03d", i)
		report.SourceContentHash = ""
		report.EvaluationAt = 0 // direct reports need no rule evaluation clock
		report.ReceivedAt = f.now - 1000
		report.EventAt = f.now - 1000
		report.RecordedAt = f.now - 90
		if _, _, err := f.repo.SaveAlarmObservation(f.ctx, report); err != nil {
			t.Fatal(err)
		}
	}
	n, err := f.s.RefreshUserReminders(f.ctx, f.a)
	if err != nil || n != 2 {
		t.Fatal(n, err)
	}
	cats := map[string]bool{}
	var aggregate model.GovernanceDocument
	for offset := 0; offset < 2; offset += 100 {
		items, total, err := f.s.UserReminders(f.ctx, f.a, "", 100, offset)
		if err != nil || total != 2 {
			t.Fatal(items, total, err)
		}
		for _, d := range items {
			v, _ := model.GovernanceBody[model.GovernanceReminder](d)
			cats[v.Category] = true
			if v.Category == "POST_COMPLETION_REPORT" {
				if v.Count != 201 || len(v.ObservationIDs) != 201 || v.FactsHash == "" {
					t.Fatal("continuous reports were not aggregated", v)
				}
				aggregate = d
			}
		}
	}
	if !cats["OBSERVATION_INSUFFICIENT"] || !cats["POST_COMPLETION_REPORT"] {
		t.Fatal(cats)
	}
	handled, err := f.s.UpdateReminder(f.ctx, f.a, aggregate.ID, "HANDLED", aggregate.Version, "old-fixed-member-scope")
	if err != nil {
		t.Fatal(err)
	}
	if n, err = f.s.RefreshUserReminders(f.ctx, f.a); err != nil || n != 0 {
		t.Fatal("same facts created another reminder", n, err)
	}
	late := f.obs
	late.ID, late.SourceEventID, late.SourceContentHash = "", "new-late-record", ""
	late.EventAt, late.ReceivedAt, late.EvaluationAt, late.RecordedAt = f.now-1000, f.now-1000, 0, f.now-80
	if _, _, err = f.repo.SaveAlarmObservation(f.ctx, late); err != nil {
		t.Fatal(err)
	}
	if n, err = f.s.RefreshUserReminders(f.ctx, f.a); err != nil || n != 0 {
		t.Fatal("new facts duplicated current task", n, err)
	}
	items, total, err := f.s.UserReminders(f.ctx, f.a, "", 20, 0)
	if err != nil || total != 2 {
		t.Fatal(items, total, err)
	}
	for _, d := range items {
		if d.ID != handled.ID {
			continue
		}
		v, _ := model.GovernanceBody[model.GovernanceReminder](d)
		if v.Count != 202 || v.Status != "UNREAD" || d.Version != handled.Version+1 || v.HandledAt != 0 {
			t.Fatal("new fixed members did not need fresh attention", v, d.Version)
		}
	}
	// An earlier read can authorize after a newer refresh has committed. Its
	// fixed member set must not overwrite the newer aggregate with fewer facts.
	type oldRefreshKey struct{}
	started, release, oldDone := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	var once sync.Once
	f.s.AuthorizeSource = func(ctx context.Context, _ Actor, d model.GovernanceDocument) error {
		if ctx.Value(oldRefreshKey{}) == true && d.Kind == model.GovernanceCaseKind {
			once.Do(func() { close(started); <-release })
		}
		return nil
	}
	go func() {
		_, e := f.s.RefreshUserReminders(context.WithValue(f.ctx, oldRefreshKey{}, true), f.a)
		oldDone <- e
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("older source read did not reach authorization barrier")
	}
	late.ID, late.SourceEventID, late.SourceContentHash, late.RecordedAt = "", "newest-late-record", "", f.now-70
	if _, _, err = f.repo.SaveAlarmObservation(f.ctx, late); err != nil {
		close(release)
		t.Fatal(err)
	}
	if _, err = f.s.RefreshUserReminders(f.ctx, f.a); err != nil {
		close(release)
		t.Fatal(err)
	}
	close(release)
	if err = <-oldDone; err != nil {
		t.Fatal(err)
	}
	f.s.AuthorizeSource = nil
	items, total, err = f.s.UserReminders(f.ctx, f.a, "", 20, 0)
	if err != nil || total != 2 {
		t.Fatal(items, total, err)
	}
	for _, d := range items {
		v, _ := model.GovernanceBody[model.GovernanceReminder](d)
		if d.ID == aggregate.ID && v.Count != 203 {
			t.Fatal("older snapshot rolled back fixed member aggregate", v)
		}
	}
	unchanged, err := f.s.Get(f.ctx, f.a, model.GovernanceCaseKind, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := model.GovernanceBody[model.GovernanceCase](unchanged)
	if after.Status != "COMPLETED" || after.CurrentRoundID != c.CurrentRoundID {
		t.Fatal("reminder reopened case or changed round", after)
	}
	alarms, _ := f.repo.ListAlarms(f.ctx, ports.AlarmFilter{TenantID: f.a.TenantID})
	if len(alarms) != 0 {
		t.Fatal("governance reminder created production alarm", alarms)
	}
}
