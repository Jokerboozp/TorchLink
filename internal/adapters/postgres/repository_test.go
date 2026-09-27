package postgres

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
	"iot-platform/internal/protocolruntime"
	"iot-platform/internal/repositorytest"
)

func TestComponentAlarmAtomicWatermarkAndRestart(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	if err := repo.Migrate(ctx); err != nil {
		t.Fatal("migration not idempotent", err)
	}
	a := model.Alarm{ID: "a", TenantID: "t", DeviceID: "d", ComponentID: "c", ComponentName: "探测器", ComponentLocation: "三楼", RuleID: "device-report:FIRE:component:c", AlarmType: "FIRE", AlarmLevel: "HIGH", Status: "ACTIVE", Source: "device", FirstTriggeredAt: 1000, LastTriggeredAt: 1000, TriggerCount: 1, TriggerID: "m1"}
	state := model.ComponentAlarmState{Timestamp: 1000, MessageID: "m1", Active: true}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			saved, event, err := repo.ApplyComponentAlarm(ctx, a, state)
			if err != nil || saved.ID != "a" || event != "raised" || saved.TriggerCount != 1 {
				t.Errorf("concurrent receive: %+v %s %v", saved, event, err)
			}
		}()
	}
	wg.Wait()
	// Another component under the same controller stays active.
	b := a
	b.ID = "b"
	b.ComponentID = "other"
	b.RuleID = "device-report:FIRE:component:other"
	if _, _, err := repo.ApplyComponentAlarm(ctx, b, state); err != nil {
		t.Fatal(err)
	}
	a.LastTriggeredAt = 2000
	state = model.ComponentAlarmState{Timestamp: 2000, MessageID: "m2", Active: false}
	saved, event, err := repo.ApplyComponentAlarm(ctx, a, state)
	if err != nil || saved.Status != "RECOVERED" || event != "recovered" {
		t.Fatal(saved, event, err)
	}
	state = model.ComponentAlarmState{Timestamp: 1500, MessageID: "old", Active: true}
	saved, event, err = repo.ApplyComponentAlarm(ctx, a, state)
	if err != nil || saved.Status != "RECOVERED" || event != "" {
		t.Fatal("stale resurrected alarm", saved, event, err)
	}
	active, err := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "t", DeviceID: "d", Status: "ACTIVE", Limit: 100})
	if err != nil || len(active) != 1 || active[0].ComponentID != "other" {
		t.Fatal(active, err)
	}
	// Same IDs in another tenant are independent.
	a.TenantID = "other-tenant"
	a.ID = "a"
	state = model.ComponentAlarmState{Timestamp: 1000, MessageID: "m1", Active: true}
	if _, event, err = repo.ApplyComponentAlarm(ctx, a, state); err != nil || event != "raised" {
		t.Fatal(event, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	a.TenantID = "cancelled"
	if _, _, err = repo.ApplyComponentAlarm(cancelled, a, state); err == nil {
		t.Fatal("cancelled transaction succeeded")
	}
	var count int
	if err = repo.pool.QueryRow(ctx, `SELECT count(*) FROM component_alarm_state WHERE tenant_id='cancelled'`).Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	// Only accepted active reports were committed to the outbox: a, b and the
	// other tenant's a; concurrent duplicates, recovery and stale reports were not.
	var keys []string
	if _, err = repo.DrainOutbox(ctx, 10, func(v model.OutboxEvent) error { keys = append(keys, v.Key); return nil }); err != nil || strings.Join(keys, ",") != "a,b,a" {
		t.Fatal(keys, err)
	}
}

func TestAlarmOutboxAndDeviceSetFilter(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	a := model.Alarm{ID: "a", TenantID: "t", DeviceID: "d1", RuleID: "r", TriggerID: "m1", Status: "ACTIVE", Source: "device", LastTriggeredAt: 1}
	for _, trigger := range []string{"m1", "m1", "m2"} {
		a.TriggerID = trigger
		if _, _, err := repo.UpsertAlarm(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	a.ID, a.DeviceID = "b", "d2"
	if _, _, err := repo.UpsertAlarm(ctx, a); err != nil {
		t.Fatal(err)
	}
	var triggers []string
	collect := func(v model.OutboxEvent) error {
		var report model.Alarm
		_ = json.Unmarshal(v.Payload, &report)
		triggers = append(triggers, report.ID+":"+report.TriggerID)
		return nil
	}
	if n, err := repo.DrainOutbox(ctx, 10, func(model.OutboxEvent) error { return errors.New("bus down") }); n != 0 || err == nil {
		t.Fatal("failed publish removed events", n, err)
	}
	if n, err := repo.DrainOutbox(ctx, 10, collect); n != 3 || err != nil || strings.Join(triggers, ",") != "a:m1,a:m2,b:m2" {
		t.Fatal(triggers, n, err)
	}
	if n, _ := repo.DrainOutbox(ctx, 10, collect); n != 0 {
		t.Fatal("published events retained")
	}
	for _, tc := range []struct {
		ids  []string
		want int
	}{{nil, 2}, {[]string{"d2", "missing"}, 1}, {[]string{}, 0}} {
		f := ports.AlarmFilter{TenantID: "t", DeviceIDs: tc.ids}
		items, err := repo.ListAlarms(ctx, f)
		total, countErr := repo.CountAlarms(ctx, f)
		if err != nil || countErr != nil || len(items) != tc.want || total != tc.want {
			t.Fatal(tc.ids, items, total, err, countErr)
		}
	}
}

// Uses only its own temporary schema in an explicitly configured test database.
func TestDeviceOperationsMigrationAndAtomicity(t *testing.T) {
	ctx := context.Background()
	r := testRepository(t)
	e := r.Migrate(ctx)
	if e != nil {
		t.Fatal("migration is not repeatable", e)
	}
	var created bool
	if e = r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema=current_schema() AND table_name IN ('edge_node','edge_read_job','edge_program'))`).Scan(&created); e != nil || created {
		t.Fatal("fresh schema still creates edge tables", e)
	}
	// Upgrading existing installations must leave historical node records intact.
	if _, e = r.pool.Exec(ctx, `CREATE TABLE edge_node(tenant_id text,id text,body jsonb); INSERT INTO edge_node VALUES ('legacy','node','{"status":"DISABLED"}')`); e != nil {
		t.Fatal(e)
	}
	if e := r.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	var preserved int
	if e = r.pool.QueryRow(ctx, `SELECT count(*) FROM edge_node WHERE tenant_id='legacy' AND id='node'`).Scan(&preserved); e != nil || preserved != 1 {
		t.Fatal("historical node data was changed", e)
	}
	verifyRetiredDeviceTablesUntouched(t, r)
	verifyOnboardingAndParseMigration(t, r)
	repositorytest.AccessStatus(t, r)
	repositorytest.ExecutionLease(t, r)
	repositorytest.RawReservation(t, r)
	repositorytest.ProtocolRegistration(t, r)
	repositorytest.ProtocolChildren(t, r)
	d := model.ManagedDevice{TenantID: "t", ID: "d", ProductID: "p", Status: "ENABLED", AccessKey: "key", SecretHash: "hash"}
	if e = r.SaveManagedDevice(ctx, d); e != nil {
		t.Fatal(e)
	}
	other := d
	other.ID = "other"
	other.AccessKey = "taken"
	if e = r.SaveManagedDevice(ctx, other); e != nil {
		t.Fatal(e)
	}
	if _, _, e = r.ChangeDeviceCredential(ctx, "t", "d", "taken", "newhash", 1); e == nil {
		t.Fatal("duplicate credential accepted")
	}
	pending, e := r.ListCredentialRevocations(ctx, "t", "d", false)
	if e != nil || len(pending) != 0 {
		t.Fatal("transaction leaked revoke", pending, e)
	}
	saved, e := r.GetManagedDevice(ctx, "t", "d")
	if e != nil || saved.SecretHash != "hash" {
		t.Fatal("transaction changed original", saved, e)
	}
	saved, revoke, e := r.ChangeDeviceCredential(ctx, "t", "d", "newkey", "newhash", 2)
	if e != nil || saved.AccessKey != "newkey" || revoke.Username != "key" {
		t.Fatal(saved, revoke, e)
	}
	pending, e = r.ListCredentialRevocations(ctx, "t", "d", true)
	if e != nil || len(pending) != 1 {
		t.Fatal(pending, e)
	}
	q := model.DeviceCommand{TenantID: "t", DeviceID: "d", ProductID: "p", ID: "c1", Type: "set", Data: map[string]any{}, Status: "DISPATCHING", CreatedAt: 1}
	if _, created, e := r.CreateDeviceCommand(ctx, q); e != nil || !created {
		t.Fatal(created, e)
	}
	if _, created, e := r.CreateDeviceCommand(ctx, q); e != nil || created {
		t.Fatal(created, e)
	}
	if e = r.CompleteDeviceCommand(ctx, "t", "other", "c1", map[string]any{"success": true}, 2); e != nil {
		t.Fatal(e)
	}
	commands, _, e := r.ListDeviceCommands(ctx, "t", "d", 20, 0)
	if e != nil || commands[0].Status != "DISPATCHING" {
		t.Fatal(commands, e)
	}
	if e = r.CompleteDeviceCommand(ctx, "t", "d", "c1", map[string]any{"success": true}, 3); e != nil {
		t.Fatal(e)
	}
	if e = r.UpdateDeviceCommandDispatch(ctx, "t", "c1", "SENT", "", 4); e != nil {
		t.Fatal(e)
	}
	commands, _, e = r.ListDeviceCommands(ctx, "t", "d", 20, 0)
	if e != nil || commands[0].Status != "SUCCEEDED" {
		t.Fatal(commands, e)
	}
	if e = r.SaveDeviceStateEvent(ctx, model.DeviceState{TenantID: "t", DeviceID: "d", ConnectionStatus: "CONNECTED"}); e != nil {
		t.Fatal(e)
	}
	events, total, e := r.ListDeviceStateEvents(ctx, "t", "d", 20, 0)
	if e != nil || total != 1 || events[0].RecordedAt <= 0 {
		t.Fatal(events, total, e)
	}
	if e = r.SaveStandardMessage(ctx, model.StandardMessage{TenantID: "t", DeviceID: "d", MessageID: "m1", MessageType: model.EventReport, Timestamp: 1}); e != nil {
		t.Fatal(e)
	}
	messages, total, e := r.ListDeviceMessages(ctx, "t", "d", model.EventReport, 20, 0)
	if e != nil || total != 1 || len(messages) != 1 {
		t.Fatal(messages, total, e)
	}

}

func verifyOnboardingAndParseMigration(t *testing.T, r *Repository) {
	t.Helper()
	ctx := context.Background()
	idx := model.RawArchiveIndex{TenantID: "t", MessageID: "raw-parse", ProductID: "p", DeviceID: "d", ArchivedAt: 1}
	if _, e := r.SaveRawIndex(ctx, idx); e != nil {
		t.Fatal(e)
	}
	if e := r.MarkRawParseResult(ctx, "t", idx.MessageID, 123, "invalid frame"); e != nil {
		t.Fatal(e)
	}
	if e := r.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	saved, e := r.GetRawIndex(ctx, "t", idx.MessageID)
	if e != nil || saved.ParseError != "invalid frame" || saved.ParseAttemptedAt != 123 {
		t.Fatal("parse migration lost evidence", saved, e)
	}
	if e := r.MarkRawParseResult(ctx, "other", idx.MessageID, 456, ""); e != nil {
		t.Fatal(e)
	}
	saved, _ = r.GetRawIndex(ctx, "t", idx.MessageID)
	if saved.ParseError == "" {
		t.Fatal("cross tenant update")
	}
	svc := onboarding.New(r, parser.NewPlatformRegistry(t.TempDir()), t.TempDir(), nil)
	q := onboarding.EnrollRequest{RequestID: "req-1", NewProduct: &onboarding.NewProduct{ID: "new-product", Name: "test product", ProtocolPackageID: onboarding.StandardPackageID, Transport: "HTTP"}, Device: onboarding.EnrollDevice{ID: "new-device", Name: "test"}, Connection: onboarding.EnrollConnection{Mode: onboarding.ModeStandard}}
	first, e := svc.Enroll(ctx, "t", q)
	if e != nil || first.Reused || first.Credential.Secret == "" {
		t.Fatal(e)
	}
	again, e := svc.Enroll(ctx, "t", q)
	if e != nil || !again.Reused || again.Credential.Secret != "" {
		t.Fatal("persistent idempotency failed", e)
	}
	filtered, total, e := r.ListManagedDevicesFiltered(ctx, ports.DeviceFilter{TenantID: "t", Role: "DIRECT", RestrictProducts: true, ProductIDs: []string{"new-product"}, Query: "NEW-dev", Status: "ENABLED", Runtime: "NEVER_SEEN"}, 10, 0)
	if e != nil || total != 1 || len(filtered) != 1 || filtered[0].ID != "new-device" || filtered[0].SecretHash == "" {
		t.Fatal("filtered device list", filtered, total, e)
	}
	if _, total, e = r.ListManagedDevicesFiltered(ctx, ports.DeviceFilter{TenantID: "t", Query: "new_dev"}, 10, 0); e != nil || total != 0 {
		t.Fatal("keyword wildcard was not escaped", total, e)
	}
	q.Device.Name = "changed"
	if _, e := svc.Enroll(ctx, "t", q); e == nil {
		t.Fatal("different request accepted")
	}
}

func TestAlarmAnalysisJobStore(t *testing.T) {
	ctx := context.Background()
	r := testRepository(t)

	running := model.AlarmAnalysisJob{ID: "job-1", TenantID: "t1", AlarmID: "a1", Status: "running", StartedAt: 1000, UpdatedAt: 1000}
	if created, createErr := r.CreateAlarmAnalysisJob(ctx, running); createErr != nil || !created {
		t.Fatalf("create: %v %v", created, createErr)
	}
	// One running job per alarm and scope; another scope runs independently.
	if created, _ := r.CreateAlarmAnalysisJob(ctx, model.AlarmAnalysisJob{ID: "job-2", TenantID: "t1", AlarmID: "a1", Status: "running", StartedAt: 2000}); created {
		t.Fatal("a second running job for the same alarm and scope was created")
	}
	if created, createErr := r.CreateAlarmAnalysisJob(ctx, model.AlarmAnalysisJob{ID: "job-k", TenantID: "t1", AlarmID: "a1", KnowledgeScope: model.AlarmAnalysisWorkflowID, Status: "running", StartedAt: 2000}); createErr != nil || !created {
		t.Fatalf("knowledge scope must run separately: %v %v", created, createErr)
	}
	running.Status, running.Progress, running.UpdatedAt = "succeeded", 100, 3000
	running.Analysis = model.AIAnalysis{Summary: "研判完成"}
	if updated, updateErr := r.UpdateRunningAlarmAnalysisJob(ctx, running); updateErr != nil || !updated {
		t.Fatalf("finish: %v %v", updated, updateErr)
	}
	if updated, _ := r.UpdateRunningAlarmAnalysisJob(ctx, running); updated {
		t.Fatal("a finished job must not be updated again")
	}
	latest, err := r.LatestAlarmAnalysisJob(ctx, "t1", "a1", "")
	if err != nil || latest.ID != "job-1" || latest.Analysis.Summary != "研判完成" {
		t.Fatalf("latest: %#v %v", latest, err)
	}
	// A new run replaces the finished job of the same alarm and scope.
	if created, createErr := r.CreateAlarmAnalysisJob(ctx, model.AlarmAnalysisJob{ID: "job-3", TenantID: "t1", AlarmID: "a1", Status: "running", StartedAt: 4000, UpdatedAt: 4000}); createErr != nil || !created {
		t.Fatalf("rerun: %v %v", created, createErr)
	}
	var rows int
	if err = r.pool.QueryRow(ctx, `SELECT count(*) FROM alarm_analysis_job WHERE tenant_id='t1' AND alarm_id='a1' AND knowledge_scope=''`).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("finished jobs must be replaced: rows=%d err=%v", rows, err)
	}
	if _, err = r.LatestAlarmAnalysisJob(ctx, "t1", "missing", ""); err != ErrNotFound {
		t.Fatalf("missing job: %v", err)
	}
}

// Set IOT_TEST_POSTGRES_DSN to run against a disposable PostgreSQL database.
func TestHealthInspectionJobStore(t *testing.T) {
	ctx := context.Background()
	r := testRepository(t)

	running := model.HealthInspectionJob{ID: "job-1", TenantID: "tenant-a", Status: "running", Progress: 8, StartedAt: 1000, UpdatedAt: 1000}
	if created, createErr := r.CreateHealthInspectionJob(ctx, running); createErr != nil || !created {
		t.Fatalf("first running job not created: created=%v err=%v", created, createErr)
	}
	if created, createErr := r.CreateHealthInspectionJob(ctx, model.HealthInspectionJob{ID: "job-2", TenantID: "tenant-a", Status: "running", StartedAt: 2000, UpdatedAt: 2000}); createErr != nil || created {
		t.Fatalf("a second running job for the tenant must be refused: created=%v err=%v", created, createErr)
	}
	if created, createErr := r.CreateHealthInspectionJob(ctx, model.HealthInspectionJob{ID: "job-b", TenantID: "tenant-b", Status: "running", StartedAt: 2000, UpdatedAt: 2000}); createErr != nil || !created {
		t.Fatalf("other tenants are independent: created=%v err=%v", created, createErr)
	}

	running.Progress, running.UpdatedAt = 50, 1500
	if updated, updateErr := r.UpdateRunningHealthInspectionJob(ctx, running); updateErr != nil || !updated {
		t.Fatalf("progress not saved: updated=%v err=%v", updated, updateErr)
	}
	finished := running
	finished.Status, finished.FinishedAt, finished.Report = "succeeded", 1800, model.DeviceHealthReport{Summary: "正常"}
	if updated, updateErr := r.UpdateRunningHealthInspectionJob(ctx, finished); updateErr != nil || !updated {
		t.Fatalf("finish not saved: updated=%v err=%v", updated, updateErr)
	}
	running.Status = "running"
	if updated, _ := r.UpdateRunningHealthInspectionJob(ctx, running); updated {
		t.Fatal("a finished job must not be revived by a late progress update")
	}
	latest, err := r.LatestHealthInspectionJob(ctx, "tenant-a", "succeeded")
	if err != nil || latest.ID != "job-1" || latest.Report.Summary != "正常" {
		t.Fatalf("latest succeeded job: %#v err=%v", latest, err)
	}
	if created, createErr := r.CreateHealthInspectionJob(ctx, model.HealthInspectionJob{ID: "job-3", TenantID: "tenant-a", Status: "running", StartedAt: 3000, UpdatedAt: 3000}); createErr != nil || !created {
		t.Fatalf("a new job must start once the previous one finished: created=%v err=%v", created, createErr)
	}
	if latest, err = r.LatestHealthInspectionJob(ctx, "tenant-a", ""); err != nil || latest.ID != "job-3" {
		t.Fatalf("latest job of any status: %#v err=%v", latest, err)
	}
	if _, err = r.LatestHealthInspectionJob(ctx, "tenant-c", ""); err != ErrNotFound {
		t.Fatalf("missing tenant must report not found, got %v", err)
	}
}

// Plain enrollments into one product run in parallel, while two tenants that
// reserve the same listener port at once still get exactly one reservation.
func TestOnboardingParallelEnrollmentAndExclusivePort(t *testing.T) {
	r := testRepository(t)
	ctx := context.Background()
	if err := r.SaveProduct(ctx, model.Product{TenantID: "tenant-a", ID: "sensor", Name: "sensor", Status: "ENABLED"}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 40)
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			d := model.ManagedDevice{TenantID: "tenant-a", ID: fmt.Sprintf("device-%d", i), ProductID: "sensor", Name: "d", Status: "ENABLED", AccessKey: fmt.Sprintf("dk_%d", i)}
			errs <- r.SaveOnboarding(ctx, model.OnboardingBundle{Device: d})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("parallel enrollment failed: %v", err)
		}
	}

	results := make(chan error, 2)
	for _, tenant := range []string{"tenant-b", "tenant-c"} {
		if err := r.SaveProduct(ctx, model.Product{TenantID: tenant, ID: "gateway", Name: "gateway", Status: "ENABLED"}); err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func(tenant string) {
			defer wg.Done()
			results <- r.SaveOnboarding(ctx, model.OnboardingBundle{
				Device:  model.ManagedDevice{TenantID: tenant, ID: "gw", ProductID: "gateway", Name: "gw", Status: "ENABLED", AccessKey: "dk_" + tenant},
				Binding: &model.ProductProtocolBinding{TenantID: tenant, ProductID: "gateway", ProtocolID: "gb", Version: "1.0.0"},
				Profile: &model.DeviceAccessProfile{TenantID: tenant, ID: "listener-" + tenant, ProductID: "gateway", ProtocolID: "gb", ProtocolVersion: "1.0.0", Mode: "listener", Network: "tcp", Port: 26999, Enabled: true},
			})
		}(tenant)
	}
	wg.Wait()
	close(results)
	succeeded, reserved := 0, 0
	for err := range results {
		switch {
		case err == nil:
			succeeded++
		case strings.Contains(err.Error(), "listener port is already reserved"):
			reserved++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if succeeded != 1 || reserved != 1 {
		t.Fatalf("one listener must win the port: succeeded=%d reserved=%d", succeeded, reserved)
	}
}

// Uses only its own temporary schema in an explicitly configured test database.
func TestSwitchProductProtocolDetectsChangedBinding(t *testing.T) {
	ctx := context.Background()
	r := testRepository(t)
	var err error

	product := model.Product{TenantID: "t", ID: "p", Name: "模板", Status: "ENABLED", ProtocolPackageID: "fire@1"}
	switchTo := func(version string, expected *model.ProductProtocolBinding) error {
		next := product
		next.ProtocolPackageID = "fire@" + version
		return r.SwitchProductProtocol(ctx, model.ProtocolSwitch{
			Product:  next,
			Package:  model.ProtocolPackage{TenantID: "t", ID: next.ProtocolPackageID, Protocol: "fire", Version: version, Status: "PUBLISHED", ParserType: "go-protocol-v2"},
			Binding:  model.ProductProtocolBinding{TenantID: "t", ProductID: "p", ProtocolID: "fire", Version: version},
			Expected: expected,
		})
	}
	if err = switchTo("1", nil); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("missing template: %v", err)
	}
	if err = r.SaveProduct(ctx, product); err != nil {
		t.Fatal(err)
	}
	if err = switchTo("1", nil); err != nil {
		t.Fatal(err)
	}
	if err = switchTo("2", nil); !errors.Is(err, model.ErrBindingChanged) {
		t.Fatalf("a first bind raced with another bind: %v", err)
	}
	if err = switchTo("2", &model.ProductProtocolBinding{ProtocolID: "fire", Version: "0"}); !errors.Is(err, model.ErrBindingChanged) {
		t.Fatalf("stale binding accepted: %v", err)
	}
	if saved, _ := r.GetProduct(ctx, "t", "p"); saved.ProtocolPackageID != "fire@1" {
		t.Fatalf("a rejected switch changed the template: %+v", saved)
	}
	if err = switchTo("2", &model.ProductProtocolBinding{ProtocolID: "fire", Version: "1"}); err != nil {
		t.Fatal(err)
	}
	saved, _ := r.GetProduct(ctx, "t", "p")
	binding, _ := r.GetProductProtocolBinding(ctx, "t", "p")
	pkg, err := r.GetProtocolPackage(ctx, "t", "fire@2")
	if saved.ProtocolPackageID != "fire@2" || binding.Version != "2" || err != nil || pkg.Version != "2" {
		t.Fatalf("switch not written together: %+v %+v %+v %v", saved, binding, pkg, err)
	}
}

func TestCountManagedDeviceChildrenQueryMatchesDeviceRegistrySchema(t *testing.T) {
	query := strings.ToLower(countManagedDeviceChildrenSQL)
	if !strings.Contains(query, "body->>'gatewayid'") || strings.Contains(query, "device_registry.gateway_id") || strings.Contains(query, " and gateway_id") {
		t.Fatalf("device child count query must read gatewayId from body jsonb, not a physical gateway_id column: %s", countManagedDeviceChildrenSQL)
	}
}

func TestDeviceFilterSQLBindsEveryValue(t *testing.T) {
	where, args := deviceFilterSQL(ports.DeviceFilter{TenantID: "t", Role: "GATEWAY", RestrictProducts: true, Query: `50%_a\b`, Status: "ENABLED", Runtime: "NEVER_SEEN"})
	if len(args) != 6 || !strings.Contains(where, "$6") || strings.Contains(where, "$7") {
		t.Fatalf("placeholders do not match arguments: %s %v", where, args)
	}
	if products, ok := args[2].([]string); !ok || products == nil || len(products) != 0 {
		t.Fatalf("restricted empty product list must match nothing, got %#v", args[2])
	}
	if args[3] != `%50\%\_a\\b%` || !strings.Contains(where, `ESCAPE '\'`) {
		t.Fatalf("keyword wildcards must be escaped: %v", args[3])
	}
	for _, column := range []string{"d.body->>'deviceRole'", "d.body->>'name'", "s.business_status", "d.status", "d.product_id"} {
		if !strings.Contains(where, column) {
			t.Fatalf("missing %s in %s", column, where)
		}
	}
	if strings.Contains(where, "category") {
		t.Fatalf("roles must come from the stored device, not the template: %s", where)
	}
	if where, args = deviceFilterSQL(ports.DeviceFilter{TenantID: "t"}); where != "d.tenant_id=$1" || len(args) != 1 {
		t.Fatalf("empty filter: %s %v", where, args)
	}
}

func TestRawFiltersPostgres(t *testing.T) {
	repositorytest.RawFilters(t, testRepository(t))
}

// Only the filters that are set reach the SQL, so PostgreSQL cannot fall back
// to a generic plan that scans every alarm.
func TestAlarmFilterSQLUsesOnlySetFilters(t *testing.T) {
	where, args := alarmFilterSQL(ports.AlarmFilter{TenantID: "tenant", Status: "ACTIVE", DeviceIDs: []string{"a", "b"}})
	if where != " WHERE tenant_id=$1 AND status=$2 AND device_id=ANY($3)" || len(args) != 3 {
		t.Fatalf("where=%q args=%v", where, args)
	}
	if strings.Contains(where, "''") {
		t.Fatal("catch-all conditions must not be generated")
	}
	if where, args = alarmFilterSQL(ports.AlarmFilter{}); where != "" || len(args) != 0 {
		t.Fatalf("an empty filter must not add conditions: %q %v", where, args)
	}
	if where, _ = alarmFilterSQL(ports.AlarmFilter{TenantID: "tenant", DeviceIDs: []string{}}); !strings.Contains(where, "device_id=ANY($2)") {
		t.Fatalf("an empty grant must still restrict: %q", where)
	}
}

func TestSchedulerProcessHelper(t *testing.T) {
	dsn := os.Getenv("IOT_TEST_SCHEDULER_DSN")
	if dsn == "" {
		t.Skip("scheduler subprocess only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	r, err := New(ctx, dsn)
	if err != nil {
		t.Fatal("connect scheduler repository")
	}
	defer r.Close()
	owner := os.Getenv("IOT_TEST_SCHEDULER_OWNER")
	c := protocolruntime.NewCoordinator(r, owner, "http://127.0.0.1:8082")
	reader := protocolruntime.New(r, func(ctx context.Context, raw model.RawMessage) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		fmt.Println("SCHEDULER_READ", owner)
		return nil
	}, slog.New(slog.NewTextHandler(io.Discard, nil)), "127.0.0.0/8")
	reader.SetCoordinator(c)
	reader.Start(ctx)
	c.Run(ctx)
}

func TestDistributedCollectionProcessFailover(t *testing.T) {
	dsn := os.Getenv("IOT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("IOT_TEST_POSTGRES_DSN is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("connect test database")
	}
	defer admin.Close()
	schema := fmt.Sprintf("scheduler_%d", time.Now().UnixNano())
	ident := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+ident); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA "+ident+" CASCADE") }()
	u, err := url.Parse(dsn)
	if err != nil || !(u.Scheme == "postgres" || u.Scheme == "postgresql") {
		t.Fatal("test requires a PostgreSQL URL")
	}
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	testDSN := u.String()
	r, err := New(ctx, testDSN)
	if err != nil {
		t.Fatal("initialize isolated repository")
	}
	defer r.Close()
	if err := r.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	sim, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer sim.Close()
	go func() {
		for {
			conn, err := sim.Accept()
			if err != nil {
				return
			}
			conn.SetDeadline(time.Now().Add(time.Second))
			request := make([]byte, 12)
			if _, err := io.ReadFull(conn, request); err == nil {
				conn.Write([]byte{request[0], request[1], 0, 0, 0, 5, request[6], 3, 2, 0, 42})
			}
			conn.Close()
		}
	}()
	release := model.ProtocolRelease{TenantID: "tenant", ProtocolID: "modbus", Version: "1", Transport: "MODBUS_TCP", Status: "PUBLISHED", Config: map[string]any{"blocks": []model.ModbusReadBlock{{ID: "read", FunctionCode: 3, StartAddress: 0, Quantity: 1, PollIntervalSec: 1}}}}
	if err := r.CreateProtocolRelease(ctx, release); err != nil {
		t.Fatal(err)
	}
	profile := model.DeviceAccessProfile{TenantID: "tenant", ID: "profile", ProductID: "product", DeviceID: "device", ProtocolID: "modbus", ProtocolVersion: "1", Mode: "poll", Host: "127.0.0.1", Port: sim.Addr().(*net.TCPAddr).Port, UnitID: 1, TimeoutMs: 500, Enabled: true}
	if err := r.SaveDeviceAccessProfile(ctx, profile); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	reads := make(chan string, 64)
	start := func(owner string) *exec.Cmd {
		command := exec.CommandContext(ctx, executable, "-test.run=^TestSchedulerProcessHelper$", "-test.v")
		command.Env = append(os.Environ(), "IOT_TEST_SCHEDULER_DSN="+testDSN, "IOT_TEST_SCHEDULER_OWNER="+owner)
		stdout, err := command.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		command.Stderr = io.Discard
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		go func() {
			scanner := bufio.NewScanner(stdout)
			for scanner.Scan() {
				if strings.HasPrefix(scanner.Text(), "SCHEDULER_READ ") {
					select {
					case reads <- strings.TrimPrefix(scanner.Text(), "SCHEDULER_READ "):
					case <-ctx.Done():
						return
					}
				}
			}
		}()
		return command
	}
	first := start("first")
	defer func() { first.Process.Kill(); first.Wait() }()
	select {
	case owner := <-reads:
		if owner != "first" {
			t.Fatal(owner)
		}
	case <-ctx.Done():
		t.Fatal("first scheduler did not collect")
	}
	lease, err := r.GetExecutionLease(ctx, "tenant", "profile/profile")
	if err != nil || lease.Owner != "first" {
		t.Fatal("first lease", err)
	}
	second := start("second")
	defer func() { second.Process.Kill(); second.Wait() }()
	for i := 0; i < 2; i++ {
		select {
		case owner := <-reads:
			if owner != "first" {
				t.Fatal("duplicate active scheduler", owner)
			}
		case <-ctx.Done():
			t.Fatal("collection stopped before failure")
		}
	}
	if err := first.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	for {
		select {
		case owner := <-reads:
			if owner != "second" {
				continue
			}
			next, err := r.GetExecutionLease(ctx, "tenant", "profile/profile")
			if err != nil || next.Owner != "second" || next.Token <= lease.Token {
				t.Fatal("takeover did not fence old token", next, err)
			}
			if err := r.ReleaseExecutionLease(ctx, lease); err != nil {
				t.Fatal(err)
			}
			current, err := r.GetExecutionLease(ctx, "tenant", "profile/profile")
			if err != nil || current.Owner != "second" || current.ExpiresAt <= time.Now().UnixMilli() {
				t.Fatal("old owner released active successor", current, err)
			}
			return
		case <-ctx.Done():
			t.Fatal("surviving scheduler did not take over after process loss")
		}
	}
}

// testPool opens a temporary schema in the database named by
// IOT_TEST_POSTGRES_DSN and drops it when the test ends.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("IOT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("IOT_TEST_POSTGRES_DSN is not configured")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("test_%d", time.Now().UnixNano())
	ident := pgx.Identifier{name}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+ident); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = name
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+ident+" CASCADE")
		admin.Close()
	})
	return pool
}

// testRepository returns a migrated repository in a temporary schema.
func testRepository(t *testing.T) *Repository {
	t.Helper()
	r := &Repository{pool: testPool(t)}
	if err := r.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return r
}
