package capacity

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/adapters/postgres"
	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
)

func TestPrepareExplainsRejectedCachedCredentials(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	if err := repo.SaveProduct(ctx, model.Product{TenantID: "t", ID: "p", Status: "ENABLED", Transport: "MQTT", ProtocolPackageID: onboarding.StandardPackageID}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "t", ProductID: "p", ID: "cap-000000", Status: "ENABLED", AccessKey: "new-key", SecretHash: onboarding.Hash("new-secret")}); err != nil {
		t.Fatal(err)
	}
	service := onboarding.New(repo, nil, "", nil)
	var rejected atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/onboarding" {
			var req onboarding.EnrollRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			result, err := service.Enroll(r.Context(), "t", req)
			if err != nil {
				t.Error(err)
				w.WriteHeader(422)
				return
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(result)
			return
		}
		if r.URL.Path != "/api/v1/device-mqtt/token" {
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if _, err := service.Authenticate(r.Context(), r.Header.Get("X-Device-Key"), r.Header.Get("X-Device-Secret")); err != nil {
			rejected.Add(1)
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"detail":"invalid device credentials"}`))
			return
		}
		_, _ = w.Write([]byte(`{"username":"device","token":"test-token","publishTopic":"/up","receiptTopic":"/receipt"}`))
	}))
	defer srv.Close()
	work := t.TempDir()
	path := filepath.Join(work, "fixtures", "t-p-cap.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	var cached []DeviceCredential
	for i := 0; i < 10; i++ {
		cached = append(cached, DeviceCredential{ID: fmt.Sprintf("cap-%06d", i), Key: "old-key", Secret: "old-secret"})
	}
	b, _ := json.Marshal(cached)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	w := NewWorker("local", t.TempDir())
	c := &controller{plan: validPlan(), inv: &Inventory{API: srv.URL}, opt: RunOptions{WorkDir: work, Log: io.Discard}, runID: "cap-test-123456", dir: t.TempDir(), httpc: srv.Client(), agents: []*agentHandle{{target: AgentTarget{Name: "local"}, agent: w}}}
	t.Cleanup(c.release)
	c.plan.Fixtures.Tenant, c.plan.Fixtures.Product, c.plan.Fixtures.DevicePrefix = "t", "p", "cap"
	c.plan.Fixtures.DeviceCount, c.plan.Fixtures.ReuseDevices = 10, true
	c.plan.Load.IngressShare, c.plan.Load.MQTTConnections = map[string]float64{"mqtt": 1}, 10
	err := c.prepare(ctx)
	if err == nil || rejected.Load() != 10 {
		t.Fatalf("expected all 10 cached credentials to be rejected, got error=%v, 401 count=%d", err, rejected.Load())
	}
	if !strings.Contains(err.Error(), "token_credentials_401=10") || !strings.Contains(err.Error(), "reuseDevices: false") || !strings.Contains(err.Error(), "0/10") {
		t.Fatalf("missing status, failure cause or recovery option: %v", err)
	}
	if strings.Contains(err.Error(), cached[0].Key) || strings.Contains(err.Error(), cached[0].Secret) {
		t.Fatal("stale credential diagnostic leaked credentials")
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(b) {
		t.Fatal("rejected cache must be retained for diagnosis")
	}
	// The suggested recovery enrolls fresh devices through the real onboarding
	// service, without rotating or deleting any of the existing devices.
	c.plan.Fixtures.ReuseDevices = false
	fresh, manifest, err := c.fixtures(ctx)
	if err != nil || len(manifest.DevicesCreated) != 10 || manifest.DevicesReused != 0 {
		t.Fatalf("new-device recovery failed: %+v %v", manifest, err)
	}
	for _, d := range fresh {
		if !strings.HasPrefix(d.ID, "cap-123456-") {
			t.Fatalf("new run reused an old ID: %s", d.ID)
		}
		if _, _, _, _, err := mqttToken(ctx, srv.Client(), srv.URL, d); err != nil {
			t.Fatalf("new device credential still rejected: %v", err)
		}
	}
	old, err := repo.GetManagedDevice(ctx, "t", "cap-000000")
	if err != nil || old.AccessKey != "new-key" || old.SecretHash != onboarding.Hash("new-secret") {
		t.Fatal("new-device recovery changed the original device")
	}
}

func TestMQTTTokenFailureClassification(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		body, want string
	}{
		{"credentials", 401, `{"detail":"invalid device credentials"}`, "token_credentials_401"},
		{"product disabled", 401, `{"detail":"device product is disabled"}`, "token_product_disabled_401"},
		{"unknown unauthorized", 401, `{"detail":"secret-value"}`, "token_http_401"},
		{"unavailable", 503, `{"detail":"secret-value"}`, "token_http_503"},
		{"invalid JSON", 200, `broken`, "token_invalid_response"},
		{"missing fields", 200, `{"token":"secret-value"}`, "token_invalid_response"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			_, failure := connectMQTT(context.Background(), srv.Client(), AgentConfig{API: srv.URL}, DeviceCredential{}, "r")
			if failure != tc.want {
				t.Fatalf("got %s, want %s", failure, tc.want)
			}
			_, _, _, _, err := mqttToken(context.Background(), srv.Client(), srv.URL, DeviceCredential{})
			if err == nil || strings.Contains(err.Error(), "secret-value") {
				t.Fatalf("unsafe or missing diagnostic: %v", err)
			}
		})
	}
}

func TestProvisionRejectsExistingUnreadyProduct(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		ready      bool
	}{
		{"enabled", `{"product":{"status":"ENABLED"},"ready":true}`, true},
		{"disabled", `{"product":{"status":"DISABLED"},"ready":false}`, false},
		{"unsupported", `{"product":{"status":"ENABLED"},"ready":false}`, false},
		{"invalid response", `{}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/api/v1/onboarding/preflight" {
					t.Errorf("existing product must not be changed: %s %s", r.Method, r.URL.Path)
				}
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			c := &controller{plan: &Plan{}, inv: &Inventory{API: srv.URL}, httpc: srv.Client()}
			c.plan.Fixtures.Product = "p"
			checks := c.provision(context.Background())
			if len(checks) != 1 || checks[0].OK != tc.ready {
				t.Fatalf("preflight ignored product readiness: %+v", checks)
			}
		})
	}
}

// runCleanup starts a run cleanup and waits for its background job.
func runCleanup(service *Service, id string, req cleanupRequest) (*CleanupJob, error) {
	job, err := service.StartRunCleanup(id, req)
	if err != nil {
		return nil, err
	}
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		current, err := service.CleanupStatus(req.Tenant, job.Environment)
		if err != nil {
			return nil, err
		}
		if current != nil && current.ID == job.ID && current.Status != "RUNNING" {
			return current, nil
		}
	}
	return nil, errors.New("cleanup did not finish")
}

func TestCleanupRunDataCachesAndRetry(t *testing.T) {
	for _, tc := range []struct {
		shared bool
		count  int
	}{{false, 2}, {true, 2}, {false, 1001}} {
		shared := tc.shared
		t.Run(fmt.Sprintf("shared=%v/devices=%d", shared, tc.count), func(t *testing.T) {
			root := t.TempDir()
			id := "cap-20260930-120000-abcdef"
			other := "cap-20260930-110000-123456"
			var fail atomic.Bool
			fail.Store(true)
			var requests []model.CapacityCleanupBatch
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Capacity-Service-Token") != "svc" || r.Header.Get("Authorization") != "Bearer operator" {
					t.Error("missing trusted cleanup identity")
				}
				var q model.CapacityCleanupBatch
				if json.NewDecoder(r.Body).Decode(&q) != nil {
					t.Error("invalid cleanup batch")
				}
				requests = append(requests, q)
				if fail.Load() {
					w.WriteHeader(503)
					return
				}
				_ = json.NewEncoder(w).Encode(model.CapacityCleanupCounts{Devices: int64(len(q.Devices))})
			}))
			defer api.Close()
			service := NewService(ServeOptions{ResultsDir: root, Token: "svc", Self: &SelfEnvironment{API: api.URL, PostgresDSN: "unused", Metrics: []MetricsTarget{{Role: "combined", Instance: "a", URL: api.URL + "/metrics"}}}})
			p := validPlan()
			p.Fixtures.DeviceCount = tc.count
			p.Fixtures.ReuseDevices = true
			ds := make([]string, tc.count)
			for i := range ds {
				ds[i] = fmt.Sprintf("cap-%06d", i)
			}
			makeRun := func(run string, devices []string) {
				t.Helper()
				dir := filepath.Join(root, run)
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				b, _, _ := p.Sanitized()
				if err := os.WriteFile(filepath.Join(dir, "plan.sanitized.yaml"), b, 0600); err != nil {
					t.Fatal(err)
				}
				_ = writeJSONAtomic(filepath.Join(dir, "state.json"), RunState{RunID: run, Status: StatusFailed})
				_ = writeJSONAtomic(filepath.Join(dir, "manifest.json"), Manifest{RunID: run, Tenant: p.Fixtures.Tenant, Product: p.Fixtures.Product, Devices: devices})
				_ = writeJSONAtomic(filepath.Join(dir, "cleanup-context.json"), cleanupContext{Environment: "self", APIHash: apiHash(api.URL)})
			}
			makeRun(id, ds)
			if shared {
				makeRun(other, ds[:1])
			}
			cache := filepath.Join(root, ".work", "fixtures", "t1-p1-cap.json")
			_ = os.MkdirAll(filepath.Dir(cache), 0700)
			have := []DeviceCredential{{ID: "other-cache-device", Key: "other", Secret: "keep"}}
			for _, id := range ds {
				have = append(have, DeviceCredential{ID: id, Key: "test-key", Secret: "test-secret"})
			}
			b, _ := json.Marshal(have)
			_ = os.WriteFile(cache, b, 0600)
			preview, err := service.RunCleanupPreview(id, "t1")
			if err != nil {
				t.Fatal(err)
			}
			wantRemove := tc.count
			if shared {
				wantRemove--
			}
			if preview.Devices != int64(wantRemove) || preview.SharedDevices != tc.count-wantRemove {
				t.Fatalf("unexpected preview %+v", preview)
			}
			req := cleanupRequest{Tenant: "t1", OperatorToken: "operator"}
			if _, err = runCleanup(service, id, cleanupRequest{Tenant: "t2", OperatorToken: "operator"}); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("cross-tenant cleanup allowed", err)
			}
			service.active = "starting"
			if _, err = runCleanup(service, id, req); !errors.Is(err, ErrRunActive) {
				t.Fatal("active cleanup allowed", err)
			}
			service.active = ""
			if job, err := runCleanup(service, id, req); err != nil || job.Status != "FAILED" {
				t.Fatal("failed storage cleanup reported success", job, err)
			}
			after, _ := os.ReadFile(cache)
			if string(after) != string(b) {
				t.Fatal("failure discarded retry credentials")
			}
			if _, err = os.Stat(filepath.Join(root, id, "state.json")); err != nil {
				t.Fatal("failure discarded run records")
			}
			if info, _ := service.runInfo(id); info.CleanupError == "" || info.Cleaning {
				t.Fatalf("failed cleanup is not reported in the run list: %+v", info)
			}
			fail.Store(false)
			job, err := runCleanup(service, id, req)
			if err != nil || job.Status != "SUCCEEDED" || job.Counts.Devices != int64(wantRemove) {
				t.Fatalf("cleanup %+v %v", job, err)
			}
			if _, err = os.Stat(filepath.Join(root, id)); !os.IsNotExist(err) {
				t.Fatal("run artifacts remain", err)
			}
			after, _ = os.ReadFile(cache)
			var kept []DeviceCredential
			_ = json.Unmarshal(after, &kept)
			if len(kept) != len(have)-wantRemove {
				t.Fatalf("wrong cached credentials kept: %d", len(kept))
			}
			for _, d := range kept {
				if d.ID == ds[1] || (!shared && d.ID == ds[0]) {
					t.Fatal("deleted fixture credential remains")
				}
			}
			if shared {
				if _, err = os.Stat(filepath.Join(root, other, "state.json")); err != nil {
					t.Fatal("other run was removed", err)
				}
			}
			for _, q := range requests {
				if len(q.Devices) > 500 || q.RunID != id {
					t.Fatal("unbounded or unowned cleanup batch", q.RunID)
				}
				if q.RemoveProduct && shared {
					t.Fatal("shared product was removed")
				}
				for _, d := range q.Devices {
					if shared && d == ds[0] {
						t.Fatal("shared device was sent for deletion")
					}
				}
			}
		})
	}
}

func TestServiceRunsArePagedNewestFirst(t *testing.T) {
	root := t.TempDir()
	service := NewService(ServeOptions{ResultsDir: root})
	for i := range 3 {
		id := fmt.Sprintf("cap-20260930-12000%d-abcdef", i)
		_ = os.Mkdir(filepath.Join(root, id), 0700)
		_ = writeJSONAtomic(filepath.Join(root, id, "state.json"), RunState{RunID: id, Status: StatusFinished})
	}
	items, total := service.runs(2, 2)
	if total != 3 || len(items) != 1 || items[0].RunID != "cap-20260930-120000-abcdef" {
		t.Fatalf("page 2: %d %+v", total, items)
	}
	if items, _ = service.runs(3, 2); len(items) != 0 {
		t.Fatalf("page past the end: %+v", items)
	}
}

func TestCleanupRejectsActiveRunAndUnsafePaths(t *testing.T) {
	root := t.TempDir()
	service := NewService(ServeOptions{ResultsDir: root})
	if _, err := service.runScope("../outside", "t"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	id := "cap-20260930-120000-abcdef"
	_ = os.Mkdir(filepath.Join(root, id), 0700)
	_ = writeJSONAtomic(filepath.Join(root, id, "state.json"), RunState{RunID: id, Status: StatusRunning})
	if _, err := service.runScope(id, "t"); !errors.Is(err, ErrRunActive) {
		t.Fatal("running record was cleanable", err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err == nil {
		if _, err = safeCleanupPath(root, "link", "file"); err == nil {
			t.Fatal("symlink path was allowed")
		}
	}
}

func TestRemoteAgentCleanupRequiresAuthAndIdleWorker(t *testing.T) {
	root := t.TempDir()
	id := "cap-20260930-120000-abcdef"
	other := "cap-20260930-120001-abcdef"
	for _, run := range []string{id, other} {
		if err := os.Mkdir(filepath.Join(root, run), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, run, "ledger"), []byte("evidence"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	w := NewWorker("remote", root)
	srv := httptest.NewServer(AgentHandler(w, "agent-secret"))
	defer srv.Close()
	ctx := context.Background()
	if err := NewRemoteAgent("remote", srv.URL, "forged").Cleanup(ctx, id); err == nil {
		t.Fatal("unauthenticated cleanup allowed")
	}
	a := NewRemoteAgent("remote", srv.URL, "agent-secret")
	w.run = &workerRun{req: PrepareRequest{RunID: id}, leaseUntil: time.Now().Add(time.Minute)}
	if err := a.Cleanup(ctx, id); !errors.Is(err, ErrAgentBusy) {
		t.Fatal("leased worker cleanup allowed", err)
	}
	w.mu.Lock()
	w.run = nil
	w.mu.Unlock()
	if err := a.Cleanup(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, id)); !os.IsNotExist(err) {
		t.Fatal("agent artifacts retained", err)
	}
	if _, err := os.Stat(filepath.Join(root, other, "ledger")); err != nil {
		t.Fatal("other run changed", err)
	}
	if err := a.Cleanup(ctx, id); err != nil {
		t.Fatal("idempotent retry failed", err)
	}
	if err := w.Cleanup(ctx, "../outside"); err == nil {
		t.Fatal("unsafe worker cleanup path allowed")
	}
	// An expired, idle lease from a lost controller can also be cleaned.
	_, cancel := context.WithCancel(context.Background())
	w.mu.Lock()
	w.run = &workerRun{req: PrepareRequest{RunID: id}, leaseUntil: time.Now().Add(-time.Second), cancel: cancel}
	w.mu.Unlock()
	if err := a.Cleanup(ctx, id); err != nil {
		t.Fatal("expired idle lease cannot be cleaned", err)
	}
}

func validPlan() *Plan {
	p, err := ParsePlan([]byte(`
schemaVersion: 1
name: t
seed: 7
credentials: {operatorSecretRef: op}
fixtures: {tenant: t1, product: p1, deviceCount: 100}
load:
  ingressShare: {http: 0.5, mqtt: 0.5}
  initialMessagesPerSecond: 100
  mqttConnections: 50
search: {measure: 10s, warmup: 0s}
budget: {maximumWallTime: 1h, maximumMessagesPerSecond: 800, maximumEvidenceGiB: 1}
`))
	if err != nil {
		panic(err)
	}
	return p
}

func TestPlanValidationRejectsUnsupportedOrInconsistentPlans(t *testing.T) {
	if err := validPlan().Validate(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"core-mixed.yaml", "quick-local.yaml", "full-system.yaml", "resilience.yaml"} {
		p, err := LoadPlan(filepath.Join("..", "..", "cmd", "capacity-test", "examples", name))
		if err != nil || p.Validate() != nil {
			t.Fatalf("example %s must stay valid: %v %v", name, err, p.Validate())
		}
	}
	if _, err := ParsePlan([]byte("schemaVersion: 1\nnme: typo\n")); err == nil {
		t.Fatal("unknown field accepted")
	}
	cases := map[string]func(p *Plan){
		"sum to 1":              func(p *Plan) { p.Load.IngressShare["http"] = 0.7 },
		"resilience":            func(p *Plan) { p.Preset = PresetResilience },
		"hard maxRuns budget":   func(p *Plan) { p.Modules.AI.Enabled = true; p.Modules.AI.Mode = "mock" },
		"only by) preset":       func(p *Plan) { p.Faults.Enabled = true },
		"is unknown":            func(p *Plan) { p.Outputs.Formats = []string{"gif"} },
		"perDeviceMaxPerSecond": func(p *Plan) { p.Load.InitialMessagesPerSecond = 3000; p.Budget.MaximumMessagesPerSecond = 5000 },
		"maximumMessagesPerSec": func(p *Plan) { p.Load.InitialMessagesPerSecond = 900 },
		"allowed query": func(p *Plan) {
			p.Load.QueryRequestsPerSecond = 1
			p.Load.QueryMix = map[string]float64{"/api/v1/users": 1}
		},
	}
	for want, mutate := range cases {
		p := validPlan()
		mutate(p)
		if err := p.Validate(); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v", want, err)
		}
	}
	// 50 MQTT devices × 20/s at a 0.5 share → 2000 msg/s; HTTP likewise.
	if got := validPlan().MaxRateForDevices(); got != 2000 {
		t.Fatal("per-device cap", got)
	}
}

func TestStandardRawIDMatchesPlatformDerivation(t *testing.T) {
	body := []byte(`{"id":"abc-1","timestamp":1,"data":{"temperature":1}}`)
	raw, err := onboarding.StandardRaw("tenant_1", "prod", "dev-1", "property", "HTTP", body)
	if err != nil {
		t.Fatal(err)
	}
	if got := StandardRawID("tenant_1", "prod", "dev-1", "property", "abc-1"); got != raw.MessageID {
		t.Fatalf("ledger raw ID %s drifted from platform %s", got, raw.MessageID)
	}
	if raw.PayloadHash() != payloadHash(body) {
		t.Fatal("payload hash must equal the platform archive hash")
	}
}

func TestReportBodyIsDeterministicAndSized(t *testing.T) {
	a := reportBody("id-1", 1000, 6, 1024, true, 7, 3)
	b := reportBody("id-1", 1000, 6, 1024, true, 7, 3)
	if string(a) != string(b) || len(a) < 1000 || len(a) > 1030 {
		t.Fatalf("len=%d same=%v", len(a), string(a) == string(b))
	}
	var v struct {
		ID   string         `json:"id"`
		Data map[string]any `json:"data"`
	}
	if json.Unmarshal(a, &v) != nil || v.ID != "id-1" || v.Data["stressAlarm"] != float64(1) {
		t.Fatal(string(a))
	}
}

func TestHistogramMergesAcrossAgentsAndHidesUnsupportedPercentiles(t *testing.T) {
	var a, b Histogram
	for i := 1; i <= 500; i++ {
		a.Observe(float64(i))
		b.Observe(float64(i + 500))
	}
	a.Merge(b)
	p95, _ := a.Quantile(0.95)
	if a.N != 1000 || p95 < 950 || p95 > 950*1.1 {
		t.Fatalf("n=%d p95=%v", a.N, p95)
	}
	var small Histogram
	for i := 0; i < 19; i++ {
		small.Observe(10)
	}
	pc := small.Percentiles()
	if pc.P50MS == nil || pc.P95MS != nil || pc.P99MS != nil {
		t.Fatal("percentiles must be null when samples cannot support them")
	}
}

func TestParsePrometheusAndPerInstanceAggregation(t *testing.T) {
	m, err := ParsePrometheus(strings.NewReader("# TYPE a counter\na 5\nb{x=\"1 2\"} 3.5\nbad\n"))
	if err != nil || m["a"] != 5 || m[`b{x="1 2"}`] != 3.5 {
		t.Fatal(m, err)
	}
	s := func(inst string, ok bool, vals map[string]float64) InstanceSample {
		return InstanceSample{Instance: inst, OK: ok, Values: vals}
	}
	rounds := []Round{
		{At: 0, Instances: []InstanceSample{s("a", true, map[string]float64{"c_total": 100, "kafka_lag": 40, "mqtt_inbox_pending": 1}), s("b", true, map[string]float64{"c_total": 10, "kafka_lag": 40, "mqtt_inbox_pending": 2})}},
		{At: 1000, Instances: []InstanceSample{s("a", true, map[string]float64{"c_total": 150, "kafka_lag": 30, "mqtt_inbox_pending": 1}), s("b", false, nil)}},
		// Instance a restarted: 20 counts since restart.
		{At: 2000, Instances: []InstanceSample{s("a", true, map[string]float64{"c_total": 20, "kafka_lag": 20, "mqtt_inbox_pending": 0}), s("b", true, map[string]float64{"c_total": 15, "kafka_lag": 20, "mqtt_inbox_pending": 0})}},
	}
	if v, ok := CounterIncrease(rounds, "c_total"); !ok || v != 50+20+5 {
		t.Fatal("counter increase must add per-instance deltas across restarts", v, ok)
	}
	if _, ok := CounterIncrease(rounds[:1], "c_total"); ok {
		t.Fatal("a single reading is not a measurement")
	}
	lag := GaugeSeries(rounds, "kafka_lag")
	if lag[0].Value != 40 || !lag[1].Valid {
		t.Fatal("group-wide lag is de-duplicated with max, not summed", lag)
	}
	inbox := GaugeSeries(rounds, "mqtt_inbox_pending")
	if inbox[0].Value != 3 || inbox[1].Valid {
		t.Fatal("per-process gauges are summed and a failed scrape is a gap, not zero", inbox)
	}
	if slope, _, ok := Trend(lag); !ok || math.Abs(slope+10) > 1e-9 {
		t.Fatal("trend", slope, ok)
	}
}

func stats(sent, ok uint64, latency float64, codes map[string]uint64) *StreamStats {
	s := &StreamStats{Scheduled: sent, Sent: sent, OK: ok, Fail: sent - ok, Attempts: sent, Codes: codes}
	for i := uint64(0); i < sent; i++ {
		s.Lateness.Observe(1)
		if i < ok {
			s.Latency.Observe(latency)
		}
	}
	return s
}

func passingRecord() PhaseRecord {
	slope := 0.0
	var biz Histogram
	for i := 0; i < 200; i++ {
		biz.Observe(300)
	}
	return PhaseRecord{PhaseID: "p01", MeasureSeconds: 60, Streams: map[string]*StreamStats{"http": stats(1000, 1000, 20, map[string]uint64{"202": 1000})},
		Pipeline:  Pipeline{BacklogSlopePerSec: &slope, BacklogPoints: 12, MetricsValid: true},
		Integrity: Integrity{VerificationMode: "full_id", UniqueSent: 1000, UniqueBusinessDone: 1000, BusinessLatency: biz, BusinessLatencyValid: true, States: map[string]uint64{}}}
}

func TestJudgeSeparatesServicePolicyGeneratorAndIntegrityOutcomes(t *testing.T) {
	p := validPlan()
	r := passingRecord()
	Judge(p, &r)
	if r.Verdict != VerdictPassed {
		t.Fatal(r.Checks)
	}
	// Fast 429 rejections look healthy on latency but are a policy limit.
	r = passingRecord()
	r.Streams["http"] = stats(1000, 500, 1, map[string]uint64{"202": 500, "429": 500})
	Judge(p, &r)
	if r.Verdict != VerdictFailed || r.StopReason != ReasonPolicy {
		t.Fatal(r.Verdict, r.StopReason)
	}
	r = passingRecord()
	growth := 5.0
	r.Pipeline.BacklogSlopePerSec = &growth
	Judge(p, &r)
	if r.Verdict != VerdictFailed || r.StopReason != ReasonService {
		t.Fatal("steady backlog growth must fail", r.Checks)
	}
	r = passingRecord()
	r.Streams["http"].NotSent = 100
	Judge(p, &r)
	if r.Verdict != VerdictInconclusive || r.StopReason != ReasonGenerator {
		t.Fatal("generator shortfall is not a service result", r.Verdict, r.StopReason)
	}
	r = passingRecord()
	r.Integrity.Missing, r.Integrity.Unaccounted = 1, 1
	Judge(p, &r)
	if r.Verdict != VerdictFailed || r.StopReason != ReasonIntegrity {
		t.Fatal(r.Verdict, r.StopReason)
	}
	r = passingRecord()
	r.Pipeline.BacklogSlopePerSec = nil
	Judge(p, &r)
	if r.Verdict != VerdictInconclusive || r.StopReason != ReasonObservability {
		t.Fatal("missing backlog metrics must not pass", r.Verdict)
	}
}

// fakeRunner simulates a system with a capacity edge.
type fakeRunner struct {
	limit      float64
	steps      []float64
	budget     int
	verdictFor func(rate float64, n int, kind string) string
}

func (f *fakeRunner) RunStep(_ context.Context, rate float64, kind string, hold time.Duration) (PhaseRecord, error) {
	f.steps = append(f.steps, rate)
	v := VerdictPassed
	if f.verdictFor != nil {
		v = f.verdictFor(rate, len(f.steps), kind)
	} else if rate > f.limit {
		v = VerdictFailed
	}
	r := PhaseRecord{Kind: kind, TargetMessagesPerSec: rate, Verdict: v, MeasureSeconds: hold.Seconds()}
	switch v {
	case VerdictFailed:
		r.StopReason = ReasonService
	case VerdictInconclusive:
		r.StopReason = ReasonGenerator
	}
	return r, nil
}

func (f *fakeRunner) Affordable(time.Duration) bool { return f.budget == 0 || len(f.steps) < f.budget }

func TestSearchFindsBoundedIntervalAndConfirmsCandidate(t *testing.T) {
	p := validPlan()
	p.Budget.MaximumMessagesPerSecond = 1e6
	p.Load.PerDeviceMaxPerSecond = 1e6
	f := &fakeRunner{limit: 1000}
	res, err := Search(context.Background(), p, f)
	if err != nil {
		t.Fatal(err)
	}
	if res.Classification != ClassBounded || res.LowerPassedBound == nil || *res.LowerPassedBound > 1000 || *res.UpperFailedBound <= 1000 {
		t.Fatalf("%+v steps=%v", res, f.steps)
	}
	if (*res.UpperFailedBound-*res.LowerPassedBound)/(*res.LowerPassedBound) > p.Search.BoundaryRelativeWidth {
		t.Fatal("interval wider than requested", res)
	}
	if res.RecommendedOperatingValue == nil || *res.RecommendedOperatingValue != math.Floor(*res.LowerPassedBound*0.7) || res.RecommendationBasis != "healthy_only" {
		t.Fatal("recommendation", res.RecommendedOperatingValue)
	}
	if res.FailureMode != ReasonService {
		t.Fatal(res.FailureMode)
	}
}

func TestSearchReportsLowerBoundUnstableNoPassAndStops(t *testing.T) {
	p := validPlan()
	// Budget cap without a failure: "at least L", never a boundary.
	f := &fakeRunner{limit: 1e9}
	res, _ := Search(context.Background(), p, f)
	if res.Classification != ClassLowerOnly || *res.LowerPassedBound != 800 || res.UpperFailedBound != nil || res.StopReason != ReasonBudget {
		t.Fatalf("%+v %v", res, f.steps)
	}
	// A candidate that fails on repeat is unstable and gets no recommendation.
	p.Budget.MaximumMessagesPerSecond, p.Load.PerDeviceMaxPerSecond = 1e6, 1e6
	confirms := 0
	f = &fakeRunner{verdictFor: func(rate float64, _ int, kind string) string {
		if rate > 500 {
			return VerdictFailed
		}
		if kind == "confirm" {
			confirms++
			if confirms == 2 {
				return VerdictFailed
			}
		}
		return VerdictPassed
	}}
	res, _ = Search(context.Background(), p, f)
	// The failing candidate becomes the upper bound; the lower bound drops to
	// the highest pass strictly below it.
	if !res.Unstable || res.RecommendedOperatingValue != nil || *res.LowerPassedBound >= *res.UpperFailedBound || *res.UpperFailedBound != f.steps[len(f.steps)-1] {
		t.Fatalf("%+v %v", res, f.steps)
	}
	f = &fakeRunner{limit: 10}
	if res, _ = Search(context.Background(), p, f); res.Classification != ClassNoPass || *res.UpperFailedBound != 100 {
		t.Fatalf("%+v", res)
	}
	// The first step passes, the next fails, then the first rate fails on
	// retest: an unstable result, not a first-step failure.
	f = &fakeRunner{verdictFor: func(_ float64, n int, _ string) string {
		if n == 1 {
			return VerdictPassed
		}
		return VerdictFailed
	}}
	res, _ = Search(context.Background(), p, f)
	if res.Classification != ClassUnstable || !res.Unstable || res.LowerPassedBound != nil || *res.UpperFailedBound != 100 || res.RecommendedOperatingValue != nil {
		t.Fatalf("%+v %v", res, f.steps)
	}
	f = &fakeRunner{verdictFor: func(_ float64, n int, _ string) string {
		if n == 2 {
			return VerdictInconclusive
		}
		return VerdictPassed
	}}
	if res, _ = Search(context.Background(), p, f); res.StopReason != ReasonGenerator || len(f.steps) != 2 || res.Classification != ClassLowerOnly {
		t.Fatalf("generator limit must stop the search: %+v %v", res, f.steps)
	}
	f = &fakeRunner{limit: 1e9, budget: 3}
	if res, _ = Search(context.Background(), p, f); res.StopReason != ReasonBudget || len(f.steps) != 3 {
		t.Fatalf("budget: %+v %v", res, f.steps)
	}
}

// ingestServer accepts standard reports like the platform does.
func ingestServer(t *testing.T, delay time.Duration) (*httptest.Server, *atomic.Int64) {
	var n atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		time.Sleep(delay)
		w.WriteHeader(202)
		_, _ = w.Write([]byte(`{"messageId":"x"}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

func TestWorkerLeaseExpiryStopsSendingAndGenerationsAreFenced(t *testing.T) {
	srv, sent := ingestServer(t, 0)
	w := NewWorker("a", t.TempDir())
	ctx := context.Background()
	cfg := AgentConfig{API: srv.URL, Tenant: "t", Product: "p", RequestTimeout: Duration(time.Second), MaxInflight: 64, Fields: 2}
	prep := PrepareRequest{RunID: "run-1", Generation: 2, Lease: Duration(400 * time.Millisecond), Config: cfg, HTTPDevices: []DeviceCredential{{ID: "d1", Key: "k", Secret: "s"}}}
	if _, err := w.Prepare(ctx, prep); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Prepare(ctx, PrepareRequest{RunID: "run-2", Generation: 1, Config: cfg}); !errors.Is(err, ErrAgentBusy) {
		t.Fatal("a leased agent must refuse another run", err)
	}
	if _, err := w.Heartbeat(ctx, RunRef{RunID: "run-1", Generation: 1}); !errors.Is(err, ErrStaleGeneration) {
		t.Fatal("older generation accepted", err)
	}
	a := PhaseAssignment{RunID: "run-1", Generation: 2, PhaseID: "p1", StartAt: time.Now().UnixMilli(), Measure: Duration(30 * time.Second), Rates: map[string]float64{"http": 200}}
	if err := w.StartPhase(ctx, a); err != nil {
		t.Fatal(err)
	}
	// No heartbeats: the lease expires after 400ms and sending stops.
	deadline := time.Now().Add(5 * time.Second)
	var res AgentPhaseResult
	var err error
	for time.Now().Before(deadline) {
		if res, err = w.PhaseResult(ctx, RunRef{RunID: "run-1", Generation: 2}, "p1"); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil || !res.LeaseExpired || !res.Interrupted {
		t.Fatalf("lease expiry did not stop the phase: %+v %v", res, err)
	}
	before := sent.Load()
	time.Sleep(300 * time.Millisecond)
	if sent.Load() != before || before > 400 {
		t.Fatal("agent kept sending after its lease expired", before, sent.Load())
	}
	if _, err = w.Heartbeat(ctx, RunRef{RunID: "run-1", Generation: 2}); !errors.Is(err, ErrStaleGeneration) {
		t.Fatal("an expired lease must not be revived by a late heartbeat", err)
	}
	// After expiry another controller may take the agent.
	if _, err = w.Prepare(ctx, PrepareRequest{RunID: "run-2", Generation: 1, Config: cfg}); err != nil {
		t.Fatal(err)
	}
}

func TestWorkerOpenLoopRecordsGeneratorShortfall(t *testing.T) {
	srv, _ := ingestServer(t, 300*time.Millisecond)
	dir := t.TempDir()
	w := NewWorker("a", dir)
	ctx := context.Background()
	cfg := AgentConfig{API: srv.URL, Tenant: "t", Product: "p", RequestTimeout: Duration(2 * time.Second), MaxInflight: 5, Fields: 2}
	if _, err := w.Prepare(ctx, PrepareRequest{RunID: "r", Generation: 1, Lease: Duration(time.Minute), Config: cfg, HTTPDevices: []DeviceCredential{{ID: "d1"}, {ID: "d2"}}}); err != nil {
		t.Fatal(err)
	}
	a := PhaseAssignment{RunID: "r", Generation: 1, PhaseID: "p", StartAt: time.Now().UnixMilli(), Measure: Duration(time.Second), Rates: map[string]float64{"http": 100}}
	if err := w.StartPhase(ctx, a); err != nil {
		t.Fatal(err)
	}
	var res AgentPhaseResult
	for i := 0; i < 100; i++ {
		var err error
		if res, err = w.PhaseResult(ctx, RunRef{RunID: "r", Generation: 1}, "p"); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	s := res.Streams["http"]
	// 100/s with 5 in flight at 300ms each can start ~17/s: the rest is a
	// generator shortfall recorded as not sent, not a slower schedule.
	if s == nil || s.Scheduled < 95 || s.NotSent < 60 || s.Sent+s.NotSent != s.Scheduled {
		t.Fatalf("%+v", s)
	}
	var entries, notSent int
	var devices = map[string]bool{}
	err := ReadLedger(filepath.Join(dir, "r", "p.jsonl.gz"), func(h LedgerHeader, e LedgerEntry) error {
		entries++
		if e.Result == "not_sent" {
			notSent++
		} else {
			devices[e.Device] = true
			if e.RawID != StandardRawID("t", "p", e.Device, "property", e.ClientID) && e.RawID != "x" {
				t.Error("raw id", e.RawID)
			}
		}
		if h.RunID != "r" || h.PhaseID != "p" {
			t.Error("header", h)
		}
		return nil
	})
	if err != nil || uint64(entries) != s.Scheduled || uint64(notSent) != s.NotSent || len(devices) != 2 {
		t.Fatal(err, entries, notSent, devices)
	}
}

type memStore struct {
	mu        sync.Mutex
	raws      map[string]RawRecord
	bodies    map[string]int
	std       map[string][]StandardRecord
	telemetry map[string]int
	offset    time.Duration
	// alarm state per device as a rule firing on stressAlarm=1 and
	// recovering on stressAlarm=0 would leave it (order independent).
	alarmLast map[string][2]int64 // device -> {latest report ms, alarm 0/1}
	alarmAt   map[string]int64
}

func newMemStore() *memStore {
	return &memStore{raws: map[string]RawRecord{}, bodies: map[string]int{}, std: map[string][]StandardRecord{}, telemetry: map[string]int{}, alarmLast: map[string][2]int64{}, alarmAt: map[string]int64{}}
}

func pick[V any](m map[string]V, ids []string) map[string]V {
	out := map[string]V{}
	for _, id := range ids {
		if v, ok := m[id]; ok {
			out[id] = v
		}
	}
	return out
}

func (s *memStore) RawIndexes(_ context.Context, _ string, ids []string) (map[string]RawRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return pick(s.raws, ids), nil
}
func (s *memStore) RawBodies(_ context.Context, _ string, ids []string) (map[string]int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return pick(s.bodies, ids), nil
}
func (s *memStore) Standards(_ context.Context, _ string, ids []string) (map[string][]StandardRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return pick(s.std, ids), nil
}
func (s *memStore) TelemetryConfigured() bool { return true }
func (s *memStore) Telemetry(_ context.Context, _ string, ids []string) (map[string]int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return pick(s.telemetry, ids), nil
}
func (s *memStore) ClockOffset(context.Context) (time.Duration, time.Duration, error) {
	return s.offset, time.Millisecond, nil
}
func (s *memStore) Close() {}
func (s *memStore) RuleAlarms(_ context.Context, _, _ string, devices []string, since int64) (map[string][]AlarmRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string][]AlarmRecord{}
	for _, d := range devices {
		if at, ok := s.alarmAt[d]; ok && at >= since {
			status := "RECOVERED"
			if s.alarmLast[d][1] == 1 {
				status = "ACTIVE"
			}
			out[d] = append(out[d], AlarmRecord{Status: status, LastTriggeredAt: at})
		}
	}
	return out, nil
}

// report applies a device report to the simulated rule.
func (s *memStore) report(device string, tsMS int64, alarm bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if last, ok := s.alarmLast[device]; !ok || tsMS >= last[0] {
		s.alarmLast[device] = [2]int64{tsMS, int64(boolInt(alarm))}
	}
	if alarm && tsMS > s.alarmAt[device] {
		s.alarmAt[device] = tsMS
	}
}

// archive records a message the way the platform pipeline would.
func (s *memStore) archive(rawID, hash string, processedAt int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.raws[rawID] = RawRecord{PayloadHash: hash, Published: true}
	s.bodies[rawID] = 1
	s.std[rawID] = []StandardRecord{{MessageID: "std-" + rawID, Type: "PROPERTY_REPORT", HasProperties: true, ProcessedAt: processedAt}}
	if processedAt > 0 {
		s.telemetry["std-"+rawID] = 1
	}
}

func TestVerifierClassifiesEveryMessageByRawID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "l.jsonl.gz")
	lw, err := NewLedgerWriter(path, LedgerHeader{RunID: "r", PhaseID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	entry := func(seq uint64, raw string, ok bool, result string) LedgerEntry {
		return LedgerEntry{Seq: seq, Stream: "http", Measured: true, RawID: raw, Hash: "h" + raw, OK: ok, Result: result, Scheduled: now.UnixMicro()}
	}
	for _, e := range []LedgerEntry{
		entry(1, "ok", true, "202"), entry(2, "lost", true, "202"), entry(3, "timeout-written", false, "timeout"),
		entry(4, "timeout-absent", false, "timeout"), entry(5, "rejected", false, "429"), entry(6, "conflict", true, "202"),
		entry(7, "slow", true, "202"), {Seq: 8, Stream: "http", Result: "not_sent"}, {Seq: 9, Stream: "tcp", OK: true, Result: "ack"},
	} {
		lw.Write(e)
	}
	if _, err = lw.Close(); err != nil {
		t.Fatal(err)
	}
	store := newMemStore()
	done := now.Add(400 * time.Millisecond).UnixMilli()
	store.archive("ok", "hok", done)
	store.archive("timeout-written", "htimeout-written", done)
	store.archive("conflict", "different", done)
	store.archive("slow", "hslow", 0)
	v := NewVerifier(store, "t")
	if err = v.Track(path, "p", 0); err != nil {
		t.Fatal(err)
	}
	if err = v.Pass(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if v.Open("p") != 4 {
		t.Fatal("lost, both unknown-response cases and slow stay pending before the final pass", v.Open("p"))
	}
	if err = v.Pass(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	in := v.PhaseIntegrity("p", 0)
	want := map[string]uint64{StateConfirmed: 2, StateMissing: 1, StateUnknown: 1, StateRejected: 1, StateConflict: 1, StatePending: 1}
	for k, n := range want {
		if in.States[k] != n {
			t.Fatalf("%s=%d want %d: %+v", k, in.States[k], n, in.States)
		}
	}
	if in.UniqueSent != 7 || in.Missing != 2 || in.Unknown != 1 || in.Pending != 1 || in.TCPAckOnly != 1 {
		t.Fatalf("%+v", in)
	}
	p95, _ := in.BusinessLatency.Quantile(0.95)
	if in.BusinessLatency.N != 2 || p95 < 350 || p95 > 500 {
		t.Fatal("business latency from scheduled send to processed_at", in.BusinessLatency.N, p95)
	}
	for _, sample := range in.Samples {
		if strings.Contains(sample, "timeout-absent") && len("timeout-absent") > 16 {
			t.Fatal("samples must not print full IDs", sample)
		}
	}
}

type receiptMessage struct {
	mqtt.Message
	body []byte
}

func (m receiptMessage) Payload() []byte { return m.body }

type receiptToken struct {
	mqtt.Token
	done chan struct{}
}

func (t receiptToken) Done() <-chan struct{} { return t.done }
func (t receiptToken) Error() error          { return nil }

type receiptClient struct {
	mqtt.Client
	calls   int
	sent    [][]byte
	tracker *MQTTReceipts
}

func (c *receiptClient) Publish(_ string, _ byte, _ bool, payload interface{}) mqtt.Token {
	body := payload.([]byte)
	c.calls++
	c.sent = append(c.sent, append([]byte{}, body...))
	hash := fmt.Sprintf("%x", sha256.Sum256(body))
	if c.calls == 1 {
		hash = "wrong-hash"
	}
	v, _ := json.Marshal(map[string]string{"id": "r", "status": "archived", "payloadHash": hash, "rawMessageId": "raw-r"})
	c.tracker.Receive(nil, receiptMessage{body: v})
	done := make(chan struct{})
	close(done)
	return receiptToken{done: done}
}

func TestMQTTReceiptsRetryIdenticalBytesAndValidateHash(t *testing.T) {
	r := NewMQTTReceipts()
	c := &receiptClient{tracker: r}
	body := []byte(`{"id":"r","timestamp":1}`)
	res := r.Publish(context.Background(), c, "topic", body, 1, 5*time.Millisecond, 1)
	if !res.OK || res.Code != "archived_retry" || res.Attempts != 2 || res.RawID != "raw-r" || c.calls != 2 || string(c.sent[0]) != string(c.sent[1]) || r.Pending() != 0 {
		t.Fatalf("%+v calls=%d", res, c.calls)
	}
}

func TestSecretsComeFromEnvOrPrivateFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.yaml")
	if err := os.WriteFile(path, []byte("op: file-token\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSecrets(path); err == nil && os.Getenv("OS") != "Windows_NT" {
		t.Fatal("group/world readable secrets file accepted")
	}
	_ = os.Chmod(path, 0o600)
	s, err := LoadSecrets(path)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := s.Get("op"); v != "file-token" {
		t.Fatal(v)
	}
	t.Setenv(SecretEnvName("op"), "env-token")
	if v, _ := s.Get("op"); v != "env-token" {
		t.Fatal("environment overrides the file", v)
	}
	if _, err = s.Get("missing"); err == nil || strings.Contains(err.Error(), "token") {
		t.Fatal(err)
	}
}

// Set IOT_TEST_POSTGRES_DSN to check the reconciliation queries and the
// processed_at completion time against the real schema in a temporary schema.
func TestPGStoreQueriesAgainstMigratedSchema(t *testing.T) {
	dsn := os.Getenv("IOT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("IOT_TEST_POSTGRES_DSN is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("connect test database")
	}
	defer admin.Close()
	schema := fmt.Sprintf("capacity_%d", time.Now().UnixNano())
	ident := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+ident); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA "+ident+" CASCADE") }()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("test requires a PostgreSQL URL")
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	repo, err := postgres.New(ctx, u.String())
	if err != nil {
		t.Fatal("initialize isolated repository")
	}
	defer repo.Close()
	exec := func(sql string, args ...any) {
		if _, err := admin.Exec(ctx, strings.ReplaceAll(sql, "S.", ident+"."), args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO S.raw_archive_index(tenant_id,product_id,device_id,message_id,object_bucket,object_key,payload_hash,payload_size,received_at,archived_at,published_at) VALUES('t','p','d','raw-1','','','h1',1,1,1,5),('t','p','d','raw-2','','','h2',1,1,1,0)`)
	exec(`INSERT INTO S.raw_message_log(tenant_id,message_id,product_id,device_id,payload_hash,payload_size,received_at,stored_at,body) VALUES('t','raw-1','p','d','h1',1,1,1,'{}')`)
	before := time.Now().UnixMilli()
	msg := model.StandardMessage{TenantID: "t", MessageID: "std-1", RawMessageID: "raw-1", ProductID: "p", DeviceID: "d", MessageType: "PROPERTY_REPORT", Properties: map[string]any{"temperature": 1}}
	claim, err := repo.ClaimStandardMessage(ctx, msg, "worker-a", time.Minute)
	if err != nil || !claim.ShouldProcess {
		t.Fatal(claim, err)
	}
	if err = repo.MarkStandardMessageProcessed(ctx, "t", "std-1", claim.Token); err != nil {
		t.Fatal(err)
	}
	if again, err := repo.ClaimStandardMessage(ctx, msg, "worker-b", time.Minute); err != nil || again.ShouldProcess || again.Busy {
		t.Fatal("a processed message must not be claimed again", again, err)
	}
	store, err := NewPGCHStore(ctx, u.String(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	raws, err := store.RawIndexes(ctx, "t", []string{"raw-1", "raw-2", "absent"})
	if err != nil || len(raws) != 2 || !raws["raw-1"].Published || raws["raw-2"].Published || raws["raw-1"].PayloadHash != "h1" {
		t.Fatal(raws, err)
	}
	bodies, err := store.RawBodies(ctx, "t", []string{"raw-1", "raw-2"})
	if err != nil || bodies["raw-1"] != 1 || bodies["raw-2"] != 0 {
		t.Fatal(bodies, err)
	}
	stds, err := store.Standards(ctx, "t", []string{"raw-1", "raw-2"})
	if err != nil || len(stds["raw-1"]) != 1 || !stds["raw-1"][0].HasProperties {
		t.Fatal(stds, err)
	}
	if done := stds["raw-1"][0].ProcessedAt; done < before-60_000 || done > time.Now().UnixMilli()+60_000 {
		t.Fatal("processed_at must be the completion time in Unix ms", done)
	}
	if _, unc, err := store.ClockOffset(ctx); err != nil || unc <= 0 {
		t.Fatal(unc, err)
	}
}

func TestFaultAllowlistIsLocalStrictAndRecoveredOnRelease(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fault commands use /bin/sh")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "faults.yaml")
	mark := filepath.Join(dir, "injected")
	body := "actions:\n  mark:\n    inject: [/bin/sh, -c, 'touch " + mark + "']\n    recover: [/bin/sh, -c, 'rm -f " + mark + "']\n"
	if err := os.WriteFile(path, []byte(body), 0o666); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(path, 0o666)
	if _, err := LoadFaultAllowlist(path); err == nil {
		t.Fatal("group/world writable allowlist accepted")
	}
	_ = os.Chmod(path, 0o600)
	allow, err := LoadFaultAllowlist(path)
	if err != nil || allow["mark"].Timeout.D() != time.Minute {
		t.Fatal(err, allow)
	}
	bad := filepath.Join(dir, "bad.yaml")
	_ = os.WriteFile(bad, []byte("actions:\n  x: {inject: [], recover: [true]}\n"), 0o600)
	if _, err = LoadFaultAllowlist(bad); err == nil {
		t.Fatal("empty inject accepted")
	}
	w := NewWorker("a", filepath.Join(dir, "w"))
	w.SetFaults(allow)
	if st, _ := w.Status(context.Background()); len(st.Faults) != 1 || st.Faults[0] != "mark" {
		t.Fatal("status must list allowlisted names", st.Faults)
	}
	ctx := context.Background()
	if _, err = w.Fault(ctx, FaultRequest{RunID: "r", Generation: 1, Action: "mark", Op: FaultInject}); !errors.Is(err, ErrStaleGeneration) {
		t.Fatal("fault outside a leased run", err)
	}
	if _, err = w.Prepare(ctx, PrepareRequest{RunID: "r", Generation: 1, Config: AgentConfig{API: "http://127.0.0.1:1", RequestTimeout: Duration(time.Second)}}); err != nil {
		t.Fatal(err)
	}
	if _, err = w.Fault(ctx, FaultRequest{RunID: "r", Generation: 1, Action: "rm -rf /", Op: FaultInject}); !errors.Is(err, ErrUnknownFault) {
		t.Fatal("unlisted action must be refused", err)
	}
	res, err := w.Fault(ctx, FaultRequest{RunID: "r", Generation: 1, Action: "mark", Op: FaultInject})
	if err != nil || !res.OK {
		t.Fatal(err, res)
	}
	if _, err = os.Stat(mark); err != nil {
		t.Fatal("inject did not run")
	}
	// Releasing the run undoes a fault the controller never recovered.
	if err = w.Release(ctx, RunRef{RunID: "r", Generation: 1}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err = os.Stat(mark); os.IsNotExist(err) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("release did not recover the injected fault")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestComputeRecoveryFindsReturnToBaseline(t *testing.T) {
	// 1 round/s: 10/s before, 0/s during the fault (t=5..8), catch-up after.
	var rounds []Round
	total := 0.0
	lag := 0.0
	for i := 0; i <= 20; i++ {
		at := int64(i * 1000)
		switch {
		case i > 5 && i <= 8:
			lag += 10
		case i > 8 && lag > 0:
			total += 20
			lag -= 10
		default:
			total += 10
		}
		if i == 0 {
			total = 0
		}
		rounds = append(rounds, Round{At: at, Instances: []InstanceSample{{Instance: "a", OK: true, Values: map[string]float64{"parse_success_total": total, "kafka_lag": lag}}}})
	}
	info := computeRecovery(rounds, []FaultEvent{{InjectedAt: 5000, RecoverAt: 8000, InjectOK: true, RecoverOK: true}}, 0, 20000)
	if info.BaselinePerSec == nil || *info.BaselinePerSec != 10 || info.DuringPerSec == nil || *info.DuringPerSec != 0 || info.RecoverySeconds == nil {
		t.Fatalf("%+v", info)
	}
	if *info.RecoverySeconds < 1 || *info.RecoverySeconds > 6 {
		t.Fatalf("recovery seconds %v", *info.RecoverySeconds)
	}
	if info := computeRecovery(rounds, []FaultEvent{{InjectedAt: 500, RecoverAt: 8000}}, 0, 20000); info.RecoverySeconds != nil || info.BaselinePerSec != nil {
		t.Fatal("no baseline before the fault must not produce a recovery time", info)
	}
}

func TestCompareComputesScalingOnlyForSameWorkload(t *testing.T) {
	dir := t.TempDir()
	write := func(id string, instances int, lower float64, mutate func(*Plan)) {
		p := validPlan()
		p.Preset = PresetCapacity
		if mutate != nil {
			mutate(p)
		}
		b, _, _ := p.Sanitized()
		run := filepath.Join(dir, id)
		_ = os.MkdirAll(run, 0o750)
		_ = os.WriteFile(filepath.Join(run, "plan.sanitized.yaml"), b, 0o640)
		targets := make([]MetricsTarget, instances)
		for i := range targets {
			targets[i] = MetricsTarget{Role: "processor", Instance: fmt.Sprint("p", i)}
		}
		_ = writeJSONAtomic(filepath.Join(run, "environment.json"), map[string]any{"deployment": "cluster", "metricsTargets": targets})
		up := lower * 1.5
		_ = writeJSONAtomic(filepath.Join(run, "summary.json"), Summary{RunID: id, Verdict: VerdictPassed, EvidenceComplete: true,
			Capacity: map[string]SearchResult{"mixedBusinessMessagesPerSecond": {Classification: ClassBounded, LowerPassedBound: &lower, UpperFailedBound: &up}}})
	}
	write("cap-20260101-000000-000001", 1, 100, nil)
	write("cap-20260101-000000-000002", 2, 190, nil)
	write("cap-20260101-000000-000003", 3, 240, nil)
	write("cap-20260101-000000-000004", 2, 300, func(p *Plan) { p.Fixtures.MessageBytes = 2048 })
	c, err := Compare(dir, []string{"cap-20260101-000000-000003", "cap-20260101-000000-000001", "cap-20260101-000000-000002"}, nil)
	if err != nil || !c.Comparable || len(c.Scaling) != 3 {
		t.Fatal(err, c)
	}
	if c.Scaling[0].Instances != 1 || c.Scaling[1].Efficiency != 0.95 || c.Scaling[2].Efficiency != 0.8 {
		t.Fatalf("%+v", c.Scaling)
	}
	out := filepath.Join(dir, "cmp")
	if err = WriteComparison(out, c); err != nil {
		t.Fatal(err)
	}
	md, _ := os.ReadFile(filepath.Join(out, "compare.md"))
	if _, err = os.Stat(filepath.Join(out, "scaling.svg")); err != nil || !strings.Contains(string(md), "95%") {
		t.Fatal(err, string(md))
	}
	c, err = Compare(dir, []string{"cap-20260101-000000-000001", "cap-20260101-000000-000004"}, map[string]int{"cap-20260101-000000-000004": 4})
	if err != nil || c.Comparable || len(c.Scaling) != 0 || c.Runs[1].Instances != 4 || !strings.Contains(strings.Join(c.Reasons, ";"), "负载组合") {
		t.Fatal(err, c)
	}
	if _, err = Compare(dir, []string{"cap-20260101-000000-000001", "../etc"}, nil); err == nil {
		t.Fatal("invalid run id accepted")
	}
}
