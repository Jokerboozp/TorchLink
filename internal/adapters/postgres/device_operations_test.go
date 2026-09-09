package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"iot-platform/internal/connector"
	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
	"iot-platform/internal/parser"
	"os"
	"testing"
	"time"
)

// Uses only its own temporary schema in an explicitly configured test database.
func TestDeviceOperationsMigrationAndAtomicity(t *testing.T) {
	dsn := os.Getenv("IOT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("IOT_TEST_POSTGRES_DSN is not configured")
	}
	ctx := context.Background()
	admin, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Close()
	name := fmt.Sprintf("operations_test_%d", time.Now().UnixNano())
	ident := pgx.Identifier{name}.Sanitize()
	if _, e = admin.Exec(ctx, "CREATE SCHEMA "+ident); e != nil {
		t.Fatal(e)
	}
	defer func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA "+ident+" CASCADE") }()
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = name
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	r := &Repository{pool: pool}
	if e = r.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	if e = r.Migrate(ctx); e != nil {
		t.Fatal("migration is not repeatable", e)
	}
	verifyOnboardingAndParseMigration(t, r)
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
	if e = r.SaveEdgeNode(ctx, model.EdgeNode{TenantID: "t", ID: "edge", Name: "edge"}); e != nil {
		t.Fatal(e)
	}
	nodes, e := r.ListEdgeNodes(ctx, "other")
	if e != nil || len(nodes) != 0 {
		t.Fatal(nodes, e)
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
	q := onboarding.Request{ProductID: "new-product", ProductName: "test product", DeviceID: "new-device", Name: "test", Type: connector.HTTP, MessageKind: "property", Payload: json.RawMessage(`{"id":"1","timestamp":1000,"data":{"x":1}}`)}
	preview, e := svc.Test(ctx, "t", q)
	if e != nil || !preview.Success {
		t.Fatal(e)
	}
	q.TestToken = preview.TestToken
	first, e := svc.Create(ctx, "t", q)
	if e != nil || first.Reused {
		t.Fatal(e)
	}
	again, e := svc.Create(ctx, "t", q)
	if e != nil || !again.Reused || again.Credential.Secret != "" {
		t.Fatal("persistent idempotency failed", e)
	}
	q.Name = "changed"
	if _, e := svc.Create(ctx, "t", q); e == nil {
		t.Fatal("different request accepted")
	}
}
