package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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

// Readiness is a fixture here; production readiness is covered below using
// real credential ingress, raw archival and the parser's persisted result.
func readyTemplateFixture(t *testing.T, s *Server, tenant, id string) {
	t.Helper()
	ctx := context.Background()
	candidate, err := s.onboarding.CurrentCandidate(ctx, tenant, id)
	if err != nil {
		t.Fatal(err)
	}
	fp := s.onboarding.CandidateFingerprint(candidate)
	rec, prep, err := s.onboarding.TemplateRecord(ctx, tenant, id)
	if err != nil {
		t.Fatal(err)
	}
	prep.Candidate = candidate
	prep.Status = "READY"
	prep.Fingerprint = fp
	prep.Verification = &model.DeviceVerification{Status: "VERIFIED", Fingerprint: fp, ProductID: id}
	if _, err = s.onboarding.SaveTemplateRecord(ctx, rec, prep); err != nil {
		t.Fatal(err)
	}
}

func TestTemplatePreparationActualIngressAndIsolatedUpgrade(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(t.TempDir()), log)
	cfg := config.Load()
	cfg.JWTSecret = "template-preparation-test-only-key"
	cfg.AdminTenants = []string{"tenant"}
	cfg.DeviceHTTPPublicURL = "https://field.example.test"
	api := New(cfg, engine, metrics.New(), log)
	if err = engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	token, _ := api.auth.Issue("tester", "tenant", "admin", nil, time.Hour)
	call := func(method, path string, body any, want int) map[string]any {
		t.Helper()
		data, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, bytes.NewReader(data))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		api.Handler().ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s got %d want %d: %s", method, path, w.Code, want, w.Body.String())
		}
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	p := model.Product{ID: "sensor", Name: "事件烟感", Status: "DRAFT", ProtocolPackageID: onboarding.StandardPackageID, Transport: "HTTP"}
	call("POST", "/api/v1/products", p, 201)
	prep := call("GET", "/api/v1/products/sensor/preparation", nil, 200)
	if prep["reusable"] != false {
		t.Fatal("new template unexpectedly reusable")
	}
	p.Status = "ENABLED"
	candidate := model.TemplateCandidate{Product: p, ProtocolID: parser.StandardProtocolID, Version: "1.0.0", Profiles: []model.DeviceAccessProfile{}, VerificationRules: model.VerificationRules{Mode: "low_frequency", MinMessages: 1, WindowSeconds: 86400}}
	prep = call("PUT", "/api/v1/products/sensor/preparation", map[string]any{"revision": 0, "candidate": candidate}, 200)
	prep = call("POST", "/api/v1/products/sensor/preparation/apply", map[string]any{"revision": prep["revision"]}, 200)
	if prep["status"] != "AWAITING_VALIDATION" {
		t.Fatal(prep)
	}
	enroll := func(product, id string, trial bool, status int) map[string]any {
		return call("POST", "/api/v1/onboarding", onboarding.EnrollRequest{RequestID: "req-" + id, ProductID: product, Trial: trial, Device: onboarding.EnrollDevice{ID: id, Name: id}, Connection: onboarding.EnrollConnection{Mode: "standard"}}, status)
	}
	enroll("sensor", "first", false, 409)
	first := enroll("sensor", "first", true, 201)
	before := call("POST", "/api/v1/products/sensor/verification", map[string]any{"deviceId": "first"}, 200)
	if before["status"] != "WAITING" {
		t.Fatal("saved device counted as verified")
	}
	send := func(product, device, id string, result map[string]any) {
		t.Helper()
		cred := result["credential"].(map[string]any)
		data, _ := json.Marshal(map[string]any{"id": id, "timestamp": time.Now().UnixMilli(), "data": map[string]any{"temperature": 25}})
		r := httptest.NewRequest(http.MethodPost, "/api/v1/device-ingest/standard/tenant/"+product+"/"+device+"/property", bytes.NewReader(data))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Device-Key", cred["accessKey"].(string))
		r.Header.Set("X-Device-Secret", cred["secret"].(string))
		w := httptest.NewRecorder()
		api.Handler().ServeHTTP(w, r)
		if w.Code != 202 {
			t.Fatalf("ingress %d %s", w.Code, w.Body.String())
		}
	}
	awaitVerified := func(product, device string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			v := call("POST", "/api/v1/products/"+product+"/verification", map[string]any{"deviceId": device}, 200)
			if v["status"] == "VERIFIED" {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("no field verification: %+v", v)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	send("sensor", "first", "first-raw", first)
	awaitVerified("sensor", "first")
	prep = call("GET", "/api/v1/products/sensor/preparation", nil, 200)
	if prep["reusable"] != true {
		t.Fatal(prep)
	}
	enroll("sensor", "ordinary", false, 201)
	// Candidate changes do not silently replace the still-usable live config.
	candidate.VerificationRules.MinMessages = 2
	prep = call("PUT", "/api/v1/products/sensor/preparation", map[string]any{"revision": prep["revision"], "candidate": candidate}, 200)
	if prep["reusable"] != true {
		t.Fatal("editing candidate interrupted live template")
	}
	call("POST", "/api/v1/products/sensor/preparation/apply", map[string]any{"revision": prep["revision"]}, 409)
	trial := call("POST", "/api/v1/products/sensor/preparation/trial", map[string]any{"revision": prep["revision"]}, 201)
	trialID := trial["trialProductId"].(string)
	prep = call("GET", "/api/v1/products/sensor/preparation", nil, 200)
	call("POST", "/api/v1/products/sensor/preparation/apply", map[string]any{"revision": prep["revision"]}, 409)
	trialDevice := enroll(trialID, "isolated", true, 201)
	send(trialID, "isolated", "trial-1", trialDevice)
	send(trialID, "isolated", "trial-2", trialDevice)
	awaitVerified(trialID, "isolated")
	original, _ := repo.GetProduct(ctx, "tenant", "sensor")
	if original.VerificationRules.MinMessages != 1 {
		t.Fatal("trial changed production")
	}
	prep = call("POST", "/api/v1/products/sensor/preparation/apply", map[string]any{"revision": prep["revision"]}, 200)
	if prep["reusable"] != false || prep["status"] != "AWAITING_VALIDATION" {
		t.Fatal("trial evidence was reused as production evidence", prep)
	}
	_, record, e := api.onboarding.TemplateRecord(ctx, "tenant", "sensor")
	if e != nil || record.AppliedTrialVerification == nil || record.AppliedTrialVerification.DeviceID != "isolated" || record.AppliedTrialVerification.MessageCount != 2 {
		t.Fatal("isolated trial evidence was not retained", e)
	}
	previous := record.History[len(record.History)-1]
	if previous.Verification == nil || previous.Verification.DeviceID != "first" || previous.Verification.Fingerprint != previous.Fingerprint || len(previous.Verification.RawMessageIDs) != 1 {
		t.Fatal("previous revision lost its acceptance evidence")
	}
	original, _ = repo.GetProduct(ctx, "tenant", "sensor")
	if original.VerificationRules.MinMessages != 2 {
		t.Fatal("explicit apply missing")
	}
	call("POST", "/api/v1/products/sensor/verification", map[string]any{"deviceId": "isolated"}, 422)
	send("sensor", "first", "after-1", first)
	send("sensor", "first", "after-2", first)
	awaitVerified("sensor", "first")
	if d, e := repo.GetManagedDevice(ctx, "tenant", "first"); e != nil || d.CreatedAt == 0 {
		t.Fatal("first device recreated or lost", e)
	}
}
