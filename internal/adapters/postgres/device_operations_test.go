package postgres

import (
	"context"
	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
	"iot-platform/internal/repositorytest"
	"testing"
)

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
