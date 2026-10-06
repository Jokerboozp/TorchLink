package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"iot-platform/internal/devicescope"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
	"iot-platform/internal/parser"
)

func taskHTTPFixture(t *testing.T) (*Server, *memory.Repository, func(string, string, string, any) *httptest.ResponseRecorder) {
	t.Helper()
	ctx := context.Background()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(devicescope.Wrap(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(t.TempDir()), logger)
	cfg := config.Load()
	cfg.JWTSecret = strings.Repeat("task-http-test-key-", 3)
	cfg.AdminTenants = []string{"admin-tenant"}
	cfg.DeviceHTTPPublicURL = "https://devices.example.test"
	api := New(cfg, engine, metrics.New(), logger)
	state := model.AccessState{Users: []model.PlatformUser{
		{Username: "alice", Enabled: true, SessionVersion: 1, DeviceScope: "all", Permissions: []string{"menu:devices", "POST /api/v1/device-registry"}},
		{Username: "bob", Enabled: true, SessionVersion: 1, DeviceScope: "all", Permissions: []string{"menu:devices", "POST /api/v1/device-registry"}},
		{Username: "template-editor", Enabled: true, SessionVersion: 1, DeviceScope: "all", Permissions: []string{"menu:products", "POST /api/v1/products", "PUT /api/v1/products/:id"}},
	}}
	if _, err = repo.SaveAccessState(ctx, "user-tenant", state); err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveProduct(ctx, model.Product{TenantID: "user-tenant", ID: "product", Name: "模板", Status: "ENABLED", ProtocolPackageID: onboarding.StandardPackageID, Transport: "HTTP"}); err != nil {
		t.Fatal(err)
	}
	fp, err := api.onboarding.TemplateFingerprint(ctx, "user-tenant", "product")
	if err != nil {
		t.Fatal(err)
	}
	rec, prep, err := api.onboarding.TemplateRecord(ctx, "user-tenant", "product")
	if err != nil {
		t.Fatal(err)
	}
	prep.Fingerprint, prep.Status = fp, "READY"
	prep.Verification = &model.DeviceVerification{Status: "VERIFIED", Fingerprint: fp}
	if _, err = api.onboarding.SaveTemplateRecord(ctx, rec, prep); err != nil {
		t.Fatal(err)
	}
	call := func(user, method, path string, body any) *httptest.ResponseRecorder {
		var encoded []byte
		if body != nil {
			encoded, _ = json.Marshal(body)
		}
		r := httptest.NewRequest(method, path, bytes.NewReader(encoded))
		r.Header.Set("Content-Type", "application/json")
		token, _ := api.auth.IssueUser(user, "user-tenant", 1, time.Hour)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		api.Handler().ServeHTTP(w, r)
		return w
	}
	return api, repo, call
}

func TestOnboardingTaskHTTPIsolationAndLivePermissions(t *testing.T) {
	api, repo, call := taskHTTPFixture(t)
	ctx := context.Background()
	draft := onboarding.DeviceDraft{Step: "connection", ProductID: "product", Request: onboarding.EnrollRequest{ProductID: "product"}}
	w := call("alice", "PUT", "/api/v1/onboarding/drafts/one", draft)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = call("bob", "GET", "/api/v1/onboarding/drafts/one", nil); w.Code != 404 {
		t.Fatal("foreign draft", w.Code, w.Body.String())
	}
	if w = call("bob", "GET", "/api/v1/onboarding/drafts", nil); w.Code != 200 || !strings.Contains(w.Body.String(), `"total":0`) {
		t.Fatal(w.Code, w.Body.String())
	}
	q := onboarding.BatchRequest{ID: "batch", ProductID: "product", Connection: onboarding.EnrollConnection{Mode: onboarding.ModeStandard, Transport: "HTTP"}, Rows: []onboarding.BatchInputRow{{Device: onboarding.EnrollDevice{ID: "batch-device", Name: "批量设备"}}}}
	w = call("alice", "POST", "/api/v1/onboarding/batches/preflight", q)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var check onboarding.BatchPreflight
	_ = json.Unmarshal(w.Body.Bytes(), &check)
	q.Fingerprint = check.Fingerprint
	w = call("alice", "POST", "/api/v1/onboarding/batches", q)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	// Managed tenants outside AdminTenants are in the durable global queue.
	jobs, err := repo.ListPendingOnboardingRecords(ctx, onboarding.BatchKind, 20)
	if err != nil || len(jobs) != 1 || jobs[0].TenantID != "user-tenant" {
		t.Fatal(jobs, err)
	}
	if err = api.onboardingTasks().RunBatch(ctx, "user-tenant", "batch", "http-test"); err != nil {
		t.Fatal(err)
	}
	w = call("alice", "GET", "/api/v1/onboarding/batches/batch", nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), "ciphertext") || strings.Contains(w.Body.String(), `"secret"`) {
		t.Fatal("unsafe batch response", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"onboardingStatus":"WAITING_CONFIGURATION"`) || !strings.Contains(w.Body.String(), `"scope":"page"`) {
		t.Fatal("registered device presented as verified", w.Body.String())
	}
	w = call("alice", "POST", "/api/v1/onboarding/batches/batch/credentials", map[string]any{})
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"accessInfo"`) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("field configuration delivery", w.Code, w.Body.String())
	}
	w = call("alice", "GET", "/api/v1/onboarding/batches/batch", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"onboardingStatus":"WAITING_VERIFICATION"`) {
		t.Fatal("delivery skipped verification", w.Code, w.Body.String())
	}
	fp, e := api.onboarding.TemplateFingerprint(ctx, "user-tenant", "product")
	if e != nil {
		t.Fatal(e)
	}
	device, e := repo.GetManagedDevice(ctx, "user-tenant", "batch-device")
	if e != nil {
		t.Fatal(e)
	}
	verifiedAt := max(time.Now().UnixMilli(), device.UpdatedAt)
	evidence := model.DeviceVerification{Status: "VERIFIED", DeviceID: device.ID, ProductID: device.ProductID, Fingerprint: fp, CheckedAt: verifiedAt, VerifiedAt: verifiedAt, RawMessageIDs: []string{"field-original"}}
	body, _ := json.Marshal(evidence)
	if _, e = repo.SaveOnboardingRecord(ctx, model.OnboardingRecord{TenantID: "user-tenant", ID: "verification:" + device.ID, OwnerID: "device", Kind: "device-verification", Status: "VERIFIED", Body: body}, 0); e != nil {
		t.Fatal(e)
	}
	api.onboarding.LoadRaw = func(context.Context, model.RawArchiveIndex) (model.RawMessage, error) {
		t.Error("batch list scanned raw history")
		return model.RawMessage{}, nil
	}
	w = call("alice", "GET", "/api/v1/onboarding/batches/batch", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"onboardingStatus":"VERIFIED"`) || !strings.Contains(w.Body.String(), `"verified":1`) {
		t.Fatal("persisted evidence not joined", w.Code, w.Body.String())
	}
	preparationRecord, preparation, e := api.onboarding.TemplateRecord(ctx, "user-tenant", "product")
	if e != nil {
		t.Fatal(e)
	}
	preparation.AppliedAt = verifiedAt + 1
	preparationRecord, e = api.onboarding.SaveTemplateRecord(ctx, preparationRecord, preparation)
	if e != nil {
		t.Fatal(e)
	}
	w = call("alice", "GET", "/api/v1/onboarding/batches/batch", nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), `"onboardingStatus":"VERIFIED"`) {
		t.Fatal("template reapply revived old evidence", w.Code, w.Body.String())
	}
	preparation.AppliedAt = 0
	if _, e = api.onboarding.SaveTemplateRecord(ctx, preparationRecord, preparation); e != nil {
		t.Fatal(e)
	}
	binding := model.ProductProtocolBinding{TenantID: "user-tenant", ProductID: "product", ProtocolID: parser.StandardProtocolID, Version: "1.0.0", UpdatedAt: verifiedAt + 1}
	if e = repo.SaveProductProtocolBinding(ctx, binding); e != nil {
		t.Fatal(e)
	}
	w = call("alice", "GET", "/api/v1/onboarding/batches/batch", nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), `"onboardingStatus":"VERIFIED"`) {
		t.Fatal("same-version rebinding revived old evidence", w.Code, w.Body.String())
	}
	binding.UpdatedAt = 0
	if e = repo.SaveProductProtocolBinding(ctx, binding); e != nil {
		t.Fatal(e)
	}
	device.UpdatedAt = verifiedAt + 1
	if e = repo.SaveManagedDevice(ctx, device); e != nil {
		t.Fatal(e)
	}
	w = call("alice", "GET", "/api/v1/onboarding/batches/batch", nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), `"onboardingStatus":"VERIFIED"`) {
		t.Fatal("stale device evidence accepted", w.Code, w.Body.String())
	}
	if w = call("bob", "POST", "/api/v1/onboarding/batches/batch/credentials", map[string]any{}); w.Code != 404 {
		t.Fatal("foreign credential", w.Code, w.Body.String())
	}
	state, _ := repo.LoadAccessState(ctx, "user-tenant")
	state.Users[0].DeviceScope = "selected"
	state.Users[0].DeviceIDs = []string{"batch-device"}
	_, _ = repo.SaveAccessState(ctx, "user-tenant", state)
	if w = call("alice", "POST", "/api/v1/onboarding/batches/batch/credentials", map[string]any{}); w.Code != 403 {
		t.Fatal("credentials after scope narrowed", w.Code, w.Body.String())
	}
	if w = call("alice", "GET", "/api/v1/onboarding/drafts/one", nil); w.Code != 403 {
		t.Fatal("draft after scope narrowed", w.Code, w.Body.String())
	}
}

func TestOnboardingTemplateDraftHTTPUsesTemplatePermission(t *testing.T) {
	_, _, call := taskHTTPFixture(t)
	q := onboarding.DeviceDraft{Step: "preparation:0", Request: onboarding.EnrollRequest{NewProduct: &onboarding.NewProduct{Metadata: map[string]any{"preparationDraft": map[string]any{"product": map[string]any{"name": "新模板"}}}}}}
	w := call("template-editor", "PUT", "/api/v1/onboarding/drafts/preparation", q)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = call("template-editor", "GET", "/api/v1/onboarding/drafts/preparation", nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	q.Step = "connection"
	if w = call("template-editor", "PUT", "/api/v1/onboarding/drafts/device", q); w.Code != 403 {
		t.Fatal("template permission registered device draft", w.Code, w.Body.String())
	}
	if w = call("template-editor", "GET", "/api/v1/onboarding/batches", nil); w.Code != 403 {
		t.Fatal("template permission read batch", w.Code, w.Body.String())
	}
}

func TestOnboardingBatchEvidenceRequiresCurrentDeviceConnection(t *testing.T) {
	api, repo, _ := taskHTTPFixture(t)
	ctx := context.Background()
	profile := model.DeviceAccessProfile{TenantID: "user-tenant", ID: "dial", ProductID: "product", DeviceID: "dial-device", Mode: "listener", ConnectionMode: "dial", Network: "tcp", Host: "192.0.2.1", Port: 9000, Enabled: true}
	if err := repo.SaveDeviceAccessProfile(ctx, profile); err != nil {
		t.Fatal(err)
	}
	device := model.ManagedDevice{TenantID: "user-tenant", ID: "dial-device", ProductID: "product", Name: "主动连接设备", Connector: "TCP", ConnectorProfileID: profile.ID, Status: "ENABLED"}
	if err := repo.SaveManagedDevice(ctx, device); err != nil {
		t.Fatal(err)
	}
	fingerprint, err := api.onboarding.TemplateFingerprint(ctx, "user-tenant", "product")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	evidence := model.DeviceVerification{Status: "VERIFIED", DeviceID: device.ID, ProductID: device.ProductID, Fingerprint: fingerprint, CheckedAt: now, VerifiedAt: now}
	body, _ := json.Marshal(evidence)
	record, err := repo.SaveOnboardingRecord(ctx, model.OnboardingRecord{TenantID: "user-tenant", ID: "verification:" + device.ID, OwnerID: "device", Kind: "device-verification", Status: "VERIFIED", Body: body}, 0)
	if err != nil {
		t.Fatal(err)
	}
	check := func(want string) {
		t.Helper()
		rows := []onboarding.BatchPublicRow{{DeviceID: device.ID, Status: "SUCCEEDED", Mode: "dial"}}
		if _, err := api.enrichOnboardingRows(ctx, "user-tenant", "product", rows); err != nil || rows[0].OnboardingStatus != want {
			t.Fatal(rows, err)
		}
	}
	check("WAITING_VERIFICATION")
	evidence.ProfileID, evidence.ProfileFingerprint = profile.ID, profile.ConfigurationFingerprint()
	record.Body, _ = json.Marshal(evidence)
	if _, err = repo.SaveOnboardingRecord(ctx, record, record.Revision); err != nil {
		t.Fatal(err)
	}
	check("VERIFIED")
	profile.Host = "192.0.2.2"
	if err = repo.SaveDeviceAccessProfile(ctx, profile); err != nil {
		t.Fatal(err)
	}
	current, err := api.onboarding.TemplateFingerprint(ctx, "user-tenant", "product")
	if err != nil || current != fingerprint {
		t.Fatal("per-device connection unexpectedly changed shared template fingerprint", current, err)
	}
	check("WAITING_VERIFICATION")
}
