package capacity

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakePlatform imitates the platform surfaces the controller touches:
// readiness, metrics, onboarding, standard ingest and management queries.
type fakePlatform struct {
	store    *memStore
	token    string
	archived atomic.Int64
	loseN    int64 // accept but never archive every Nth report (0 = never)
	seen     atomic.Int64
	srv      *httptest.Server
	// dropAlarms simulates a rule that never fires.
	dropAlarms bool
	// downFile, while it exists, makes ingest answer 503 (fault injection).
	downFile string
	// products, when non-nil, lists the products that exist; onboarding
	// preflight answers 404 for others (auto-provisioning tests).
	products map[string]bool
	rules    map[string]string
	mu       sync.Mutex
}

func newFakePlatform(t *testing.T, loseN int64, extra ...func(*http.ServeMux, *fakePlatform)) *fakePlatform {
	f := &fakePlatform{store: newMemStore(), token: "operator-token-e2e", loseN: loseN}
	mux := http.NewServeMux()
	authed := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+f.token {
				w.WriteHeader(401)
				return
			}
			h(w, r)
		}
	}
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"status":"ok"}`)) })
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, _ *http.Request) {
		n := f.archived.Load()
		fmt.Fprintf(w, "# TYPE raw_archive_success_total counter\nraw_archive_success_total %d\nparse_success_total %d\nalarm_trigger_total 0\nkafka_lag 0\nmqtt_inbox_pending 0\ngo_memstats_heap_inuse_bytes 4194304\n", n, n)
	})
	ok := func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"items":[]}`)) }
	mux.HandleFunc("GET /api/v1/devices", authed(ok))
	mux.HandleFunc("GET /api/v1/alarms", authed(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"items":[{"id":"alarm-1"}],"total":1}`))
	}))
	mux.HandleFunc("GET /api/v1/onboarding/preflight", authed(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.products != nil && !f.products[r.URL.Query().Get("productId")] {
			w.WriteHeader(404)
			return
		}
		id := r.URL.Query().Get("productId")
		_ = json.NewEncoder(w).Encode(map[string]any{"ready": true, "product": map[string]string{"id": id, "status": "ENABLED", "name": "容量测试标准设备 " + id, "description": "capacity-test 自动创建", "protocolPackageId": "iot-standard@1.0.0"}})
	}))
	mux.HandleFunc("PUT /api/v1/products/{id}", authed(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.products == nil {
			f.products = map[string]bool{}
		}
		f.products[r.PathValue("id")] = true
		_, _ = w.Write([]byte(`{}`))
	}))
	mux.HandleFunc("PUT /api/v1/rules/{id}", authed(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if _, ok := f.rules[r.PathValue("id")]; !ok {
			w.WriteHeader(404)
			return
		}
		b, _ := io.ReadAll(r.Body)
		f.rules[r.PathValue("id")] = string(b)
	}))
	mux.HandleFunc("POST /api/v1/rules", authed(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		b, _ := io.ReadAll(r.Body)
		var v struct{ ID string }
		_ = json.Unmarshal(b, &v)
		if f.rules == nil {
			f.rules = map[string]string{}
		}
		f.rules[v.ID] = string(b)
		w.WriteHeader(201)
	}))
	mux.HandleFunc("POST /api/v1/onboarding", authed(func(w http.ResponseWriter, r *http.Request) {
		var v struct {
			Device struct{ ID string } `json:"device"`
		}
		_ = json.NewDecoder(r.Body).Decode(&v)
		w.WriteHeader(201)
		fmt.Fprintf(w, `{"credential":{"accessKey":"%s-key","secret":"sec-%s"}}`, v.Device.ID, v.Device.ID)
	}))
	mux.HandleFunc("POST /api/v1/device-ingest/standard/{tenant}/{product}/{device}/{kind}", func(w http.ResponseWriter, r *http.Request) {
		device := r.PathValue("device")
		if f.downFile != "" {
			if _, err := os.Stat(f.downFile); err == nil {
				w.WriteHeader(503)
				return
			}
		}
		if r.Header.Get("X-Device-Secret") != "sec-"+device {
			w.WriteHeader(401)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var v struct {
			ID        string `json:"id"`
			Timestamp int64  `json:"timestamp"`
			Data      struct {
				StressAlarm int `json:"stressAlarm"`
			} `json:"data"`
		}
		_ = json.Unmarshal(body, &v)
		raw := StandardRawID(r.PathValue("tenant"), r.PathValue("product"), device, r.PathValue("kind"), v.ID)
		if n := f.seen.Add(1); f.loseN == 0 || n%f.loseN != 0 {
			f.store.archive(raw, payloadHash(body), time.Now().Add(20*time.Millisecond).UnixMilli())
			if !f.dropAlarms {
				f.store.report(device, v.Timestamp, v.Data.StressAlarm == 1)
			}
			f.archived.Add(1)
		}
		w.WriteHeader(202)
		fmt.Fprintf(w, `{"messageId":%q,"created":true,"status":"ACCEPTED"}`, raw)
	})
	for _, fn := range extra {
		fn(mux, f)
	}
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

type e2eEnv struct {
	dir, planPath, secrets, results string
	platform                        *fakePlatform
	faultAllow                      FaultAllowlist
}

// e2eExtra adds plan/inventory/secret lines and platform handlers.
type e2eExtra struct {
	plan, inventory, secrets, fixtures string
	handlers                           func(*http.ServeMux, *fakePlatform)
}

func newE2E(t *testing.T, loseN int64, preset, search string, extra ...e2eExtra) e2eEnv {
	t.Helper()
	dir := t.TempDir()
	var x e2eExtra
	if len(extra) > 0 {
		x = extra[0]
	}
	var hooks []func(*http.ServeMux, *fakePlatform)
	if x.handlers != nil {
		hooks = append(hooks, x.handlers)
	}
	f := newFakePlatform(t, loseN, hooks...)
	agentToken := "agent-token-e2e"
	remote := httptest.NewServer(AgentHandler(NewWorker("remote", filepath.Join(dir, "remote-agent")), agentToken))
	t.Cleanup(remote.Close)
	inv := fmt.Sprintf(`name: e2e
api: %s
metrics:
  - {role: combined, instance: api-1, url: %s/metrics}
agents:
  - {name: local}
  - {name: remote, url: %s}
observers: {postgresSecretRef: pg}
`, f.srv.URL, f.srv.URL, remote.URL) + strings.ReplaceAll(x.inventory, "{{api}}", f.srv.URL)
	plan := fmt.Sprintf(`schemaVersion: 1
name: e2e
preset: %s
seed: 3
target: {inventoryRef: inv.yaml, deployment: test}
credentials: {operatorSecretRef: op, agentSecretRef: ag}
fixtures: {tenant: t1, product: p1, deviceCount: 10, reuseDevices: true, messageBytes: 200}
load:
  ingressShare: {http: 1}
  initialMessagesPerSecond: 20
  queryRequestsPerSecond: 3
  queryMix: {devices: 0.5, alarms: 0.5}
search: {%s, warmup: 0s, drainTimeout: 10s, cooldown: 1s, observeInterval: 1s}
budget: {maximumWallTime: 5m, maximumMessagesPerSecond: 100, maximumEvidenceGiB: 1}
`, preset, search) + x.plan
	plan = strings.Replace(plan, "messageBytes: 200}", "messageBytes: 200"+x.fixtures+"}", 1)
	e := e2eEnv{dir: dir, planPath: filepath.Join(dir, "plan.yaml"), secrets: filepath.Join(dir, "secrets.yaml"), results: filepath.Join(dir, "results"), platform: f}
	for path, body := range map[string]string{filepath.Join(dir, "inv.yaml"): inv, e.planPath: plan, e.secrets: "op: " + f.token + "\nag: " + agentToken + "\npg: postgres://observer:pg-secret-e2e@db/iot\n" + x.secrets} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

func (e e2eEnv) run(ctx context.Context) (string, error) {
	return Run(ctx, RunOptions{PlanPath: e.planPath, SecretsPath: e.secrets, ResultsDir: e.results, SourceCommit: "test", FaultAllow: e.faultAllow,
		NewStore: func(context.Context, string, string) (Store, error) { return e.platform.store, nil }})
}

func readSummary(t *testing.T, dir string) Summary {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s Summary
	if err = json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestStartupFailureRetainsActionableReason(t *testing.T) {
	for _, stage := range []string{"preflight", "prepare"} {
		t.Run(stage, func(t *testing.T) {
			e := newE2E(t, 0, "quick", "rates: [20], measure: 10s")
			original := e.platform.srv.Config.Handler
			detail := "该模板尚未通过首台实机验证"
			e.platform.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if stage == "preflight" && r.URL.Path == "/api/v1/onboarding/preflight" {
					_, _ = w.Write([]byte(`{"product":{"status":"DISABLED"},"ready":false}`))
					return
				}
				if stage == "prepare" && r.URL.Path == "/api/v1/onboarding" {
					w.WriteHeader(http.StatusConflict)
					_ = json.NewEncoder(w).Encode(map[string]string{"detail": detail})
					return
				}
				original.ServeHTTP(w, r)
			})
			if stage == "preflight" {
				detail = "测试产品未启用"
			}
			runID, err := e.run(context.Background())
			if err != nil && stage != "prepare" {
				t.Fatal(err)
			}
			st, err := ReadState(e.results, runID)
			if err != nil || st.Status != StatusFailed || len(st.Completed) != 0 || !strings.Contains(st.Message, detail) {
				t.Fatalf("startup failure must retain its cause without measuring: state=%+v err=%v", st, err)
			}
			info, err := NewService(ServeOptions{ResultsDir: e.results}).runInfo(runID)
			if err != nil || info.Message != st.Message || info.Verdict != VerdictInconclusive {
				t.Fatalf("run API lost startup reason: %+v err=%v", info, err)
			}
			for _, file := range []string{"report.md", "report.html"} {
				body, err := os.ReadFile(filepath.Join(e.results, runID, file))
				if err != nil || !strings.Contains(string(body), detail) {
					t.Fatalf("%s must explain startup failure: %v", file, err)
				}
			}
		})
	}
}

func TestEndToEndQuickRunWithLocalAndRemoteAgents(t *testing.T) {
	t.Parallel()
	e := newE2E(t, 0, "quick", "rates: [20], measure: 10s")
	runID, err := e.run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(e.results, runID)
	s := readSummary(t, dir)
	if s.ExecutionStatus != StatusFinished || s.Verdict != VerdictPassed || !s.EvidenceComplete {
		b, _ := os.ReadFile(filepath.Join(dir, "phases", s.Phases[0].PhaseID+".json"))
		t.Fatalf("%+v\n%s", s, b)
	}
	if s.Integrity.UniqueSent == nil || *s.Integrity.UniqueSent < 190 || *s.Integrity.Missing != 0 || *s.Integrity.UniqueBusinessCompleted != *s.Integrity.UniqueSent {
		t.Fatalf("integrity %+v", s.Integrity)
	}
	var rec PhaseRecord
	b, _ := os.ReadFile(filepath.Join(dir, "phases", s.Phases[0].PhaseID+".json"))
	_ = json.Unmarshal(b, &rec)
	if len(rec.Agents) != 2 || rec.Streams["query"] == nil || rec.Streams["query"].OK < 25 || rec.Integrity.BusinessLatency.N == 0 {
		t.Fatalf("both agents must contribute load and queries: %+v", rec.Agents)
	}
	for _, name := range []string{"report.html", "report.md", "phases.csv", "charts/throughput.svg", "charts/latency.svg", "charts/backlog.svg", "charts/resources.svg", "checksums.txt", "manifest.json", "plan.sanitized.yaml", "ledgers/" + rec.PhaseID + "/remote.jsonl.gz"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Error("missing", name)
		}
	}
	// Report regeneration is deterministic and offline.
	before, _ := os.ReadFile(filepath.Join(dir, "report.html"))
	beforeSummary, _ := os.ReadFile(filepath.Join(dir, "summary.json"))
	e.platform.srv.Close()
	if err = GenerateReport(dir, []string{e.platform.token}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "report.html"))
	afterSummary, _ := os.ReadFile(filepath.Join(dir, "summary.json"))
	if !bytes.Equal(before, after) || !bytes.Equal(beforeSummary, afterSummary) {
		t.Fatal("report regeneration changed the output")
	}
	// Secrets never reach evidence; credentials stay in the private work dir.
	if err = checkNoSecrets(dir, []string{e.platform.token, "agent-token-e2e", "pg-secret-e2e", "sec-cap-000001"}); err != nil {
		t.Fatal(err)
	}
	creds, err := os.ReadFile(filepath.Join(e.results, ".work", "fixtures", "t1-p1-cap.json"))
	if err != nil || !strings.Contains(string(creds), "sec-cap-000001") {
		t.Fatal("reusable fixture credentials must stay in the private work directory", err)
	}
	if strings.Contains(string(before), "—") && !strings.Contains(string(before), "通过") {
		t.Fatal("report lacks a verdict")
	}
}

func TestEndToEndIntegrityFailureStopsSearchWithEvidence(t *testing.T) {
	t.Parallel()
	e := newE2E(t, 25, "capacity", "measure: 10s, repeats: 1, maxSteps: 3")
	runID, err := e.run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(e.results, runID)
	s := readSummary(t, dir)
	r := s.Capacity["mixedBusinessMessagesPerSecond"]
	if s.Verdict != VerdictFailed || s.VerdictReason != ReasonIntegrity || r.Classification != ClassNoPass || *s.Integrity.Missing == 0 {
		t.Fatalf("%+v %+v", s, r)
	}
	if len(s.Bottlenecks) == 0 || s.Bottlenecks[0].Candidate != "数据完整性" {
		t.Fatalf("%+v", s.Bottlenecks)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "verification", s.Phases[0].PhaseID+".json"))
	if !strings.Contains(string(b), `"missing"`) || strings.Contains(string(b), "sec-") {
		t.Fatal(string(b))
	}
}

func TestEndToEndStopProducesPartialReport(t *testing.T) {
	t.Parallel()
	e := newE2E(t, 0, "soak", "rates: [20], measure: 60s, candidateHold: 60s")
	go func() {
		for i := 0; i < 100; i++ {
			time.Sleep(200 * time.Millisecond)
			entries, _ := os.ReadDir(e.results)
			for _, en := range entries {
				if strings.HasPrefix(en.Name(), "cap-") {
					if st, err := ReadState(e.results, en.Name()); err == nil && st.Status == StatusRunning {
						time.Sleep(2 * time.Second)
						_ = RequestStop(e.results, en.Name(), false)
						return
					}
				}
			}
		}
	}()
	start := time.Now()
	runID, err := e.run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 45*time.Second {
		t.Fatal("stop did not end the 60s step early")
	}
	dir := filepath.Join(e.results, runID)
	s := readSummary(t, dir)
	st, _ := ReadState(e.results, runID)
	if st.Status != StatusCancelled || s.Verdict != VerdictInconclusive || s.StopReason == nil || *s.StopReason != ReasonCancel {
		t.Fatalf("state=%s summary=%+v", st.Status, s)
	}
	if len(s.Phases) != 1 || *s.Integrity.Missing != 0 || *s.Integrity.UniqueSent == 0 {
		t.Fatalf("messages sent before the stop are still reconciled: %+v", s.Integrity)
	}
	if _, err = os.Stat(filepath.Join(dir, "report.html")); err != nil {
		t.Fatal(err)
	}
}

// moduleHandlers imitate the business APIs module streams call.
func moduleHandlers(mux *http.ServeMux, f *fakePlatform) {
	knowledgePolls := sync.Map{}
	auth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+f.token {
				w.WriteHeader(401)
				return
			}
			h(w, r)
		}
	}
	reply := func(status int, body string) http.HandlerFunc {
		return auth(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "POST" && (strings.Contains(r.URL.Path, "/ai/") || r.URL.Path == "/api/v1/raw-messages/replay") && !runIDPattern.MatchString(r.Header.Get("X-Capacity-Run-ID")) {
				w.WriteHeader(400)
				return
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		})
	}
	mux.HandleFunc("POST /api/v1/ai/alarm-analysis/{id}/run", reply(202, `{"jobId":"job-1"}`))
	mux.HandleFunc("GET /api/v1/ai/alarm-analysis/{id}/progress/{job}", reply(200, `{"status":"succeeded"}`))
	mux.HandleFunc("POST /api/v1/knowledge/documents", auth(func(w http.ResponseWriter, r *http.Request) {
		if !runIDPattern.MatchString(r.Header.Get("X-Capacity-Run-ID")) {
			w.WriteHeader(400)
			return
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil || r.FormValue("workflowId") != "wf-cap" {
			w.WriteHeader(400)
			return
		}
		defer r.MultipartForm.RemoveAll()
		file, header, err := r.FormFile("file")
		if err != nil {
			w.WriteHeader(400)
			return
		}
		file.Close()
		id := header.Filename
		knowledgePolls.Store(id, &atomic.Int64{})
		w.WriteHeader(202)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": id, "status": "UPLOADED"})
	}))
	mux.HandleFunc("GET /api/v1/knowledge/documents/{id}", auth(func(w http.ResponseWriter, r *http.Request) {
		if !runIDPattern.MatchString(r.Header.Get("X-Capacity-Run-ID")) {
			w.WriteHeader(400)
			return
		}
		id := r.PathValue("id")
		value, exists := knowledgePolls.Load(id)
		if !exists {
			w.WriteHeader(404)
			return
		}
		status := "INDEXING"
		if value.(*atomic.Int64).Add(1) > 1 {
			status = "INDEXED"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"document": map[string]string{"id": id, "status": status}})
	}))
	mux.HandleFunc("POST /api/v1/video/cameras/{id}/play-sessions", reply(201, `{"sessionId":"s-1","hlsUrl":"/live/cam.m3u8"}`))
	mux.HandleFunc("DELETE /api/v1/video/play-sessions/{id}", reply(204, ""))
	mux.HandleFunc("GET /live/cam.m3u8", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:2,\nseg-1.ts\n"))
	})
	mux.HandleFunc("GET /live/seg-1.ts", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(bytes.Repeat([]byte{0x47}, 188)) })
	mux.HandleFunc("GET /api/v1/raw-messages", reply(200, `{"items":[{"messageId":"raw-1"}]}`))
	mux.HandleFunc("POST /api/v1/raw-messages/download", reply(200, "zip-bytes"))
	mux.HandleFunc("POST /api/v1/raw-messages/replay", reply(202, `{"id":"replay-1"}`))
	mux.HandleFunc("GET /api/v1/replays/{id}", reply(200, `{"status":"COMPLETED"}`))
	mux.HandleFunc("POST /api/v1/ai/health-inspection/run", reply(202, `{"jobId":"insp-1"}`))
	mux.HandleFunc("GET /api/v1/ai/health-inspection/progress/{job}", reply(200, `{"status":"succeeded"}`))
	mux.HandleFunc("POST /api/v1/ai/health-inspection/pdf", reply(200, "%PDF-1.4"))
	mux.HandleFunc("GET /api/open/v1/devices", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "open-key-secret-e2e" {
			w.WriteHeader(401)
			return
		}
		_, _ = w.Write([]byte(`{"items":[]}`))
	})
	file := []byte("backup-file")
	sum := sha256.Sum256(file)
	mux.HandleFunc("POST /api/v1/backups", reply(200, `{"id":"b-1","artifacts":[{"filename":"raw-messages.jsonl.gz"}]}`))
	mux.HandleFunc("GET /api/v1/backups/{id}/files/{name}", auth(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Checksum-SHA256", hex.EncodeToString(sum[:]))
		_, _ = w.Write(file)
	}))
	mux.HandleFunc("POST /api/v1/backups/{id}/restore", reply(200, `{"status":"COMPLETED"}`))
}

func TestKnowledgeModuleWaitsForIndexCompletionAndKeepsCleanupOwnership(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name          string
		uploadStatus  int
		states        []string
		timeout       time.Duration
		wantOK        bool
		wantCode      string
		wantPolls     int
		malformed     bool
		missingID     bool
		wrongDetailID bool
	}{
		{name: "async queued indexing completed", uploadStatus: 202, states: []string{"UPLOADED", "INDEXING", "INDEXED"}, wantOK: true, wantCode: "indexed", wantPolls: 3},
		{name: "synchronous compatibility", uploadStatus: 201, wantOK: true, wantCode: "201"},
		{name: "embedding failed", uploadStatus: 202, states: []string{"INDEX_FAILED"}, wantCode: "index_failed", wantPolls: 1},
		{name: "document deleting", uploadStatus: 202, states: []string{"DELETING"}, wantCode: "deleting", wantPolls: 1},
		{name: "index timeout", uploadStatus: 202, states: []string{"INDEXING"}, timeout: 150 * time.Millisecond, wantCode: "timeout", wantPolls: 1},
		{name: "malformed index response", uploadStatus: 202, malformed: true, wantCode: "invalid_index_response", wantPolls: 1},
		{name: "wrong indexed document", uploadStatus: 202, states: []string{"INDEXED"}, wrongDetailID: true, wantCode: "invalid_index_response", wantPolls: 1},
		{name: "accepted upload missing ID", uploadStatus: 202, missingID: true, wantCode: "invalid_upload_response"},
		{name: "rejected upload with known ID", uploadStatus: 422, wantCode: "422"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			const runID = "cap-20260930-120000-abcdef"
			const documentID = "doc-capacity-index"
			var polls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer operator-cap" || r.Header.Get("X-Capacity-Run-ID") != runID {
					http.Error(w, "missing capacity credentials or ownership", http.StatusUnauthorized)
					return
				}
				switch {
				case r.Method == http.MethodPost && r.URL.Path == "/api/v1/knowledge/documents":
					if r.ParseMultipartForm(1<<20) != nil || r.FormValue("workflowId") != "wf-cap" || r.FormValue("category") != "capacity-test" {
						http.Error(w, "invalid capacity knowledge upload", http.StatusBadRequest)
						return
					}
					defer r.MultipartForm.RemoveAll()
					id := documentID
					if test.missingID {
						id = ""
					}
					w.WriteHeader(test.uploadStatus)
					_ = json.NewEncoder(w).Encode(map[string]string{"id": id, "status": "UPLOADED"})
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/knowledge/documents/"+documentID:
					n := int(polls.Add(1))
					if test.malformed {
						_, _ = w.Write([]byte("invalid JSON"))
						return
					}
					state := test.states[min(n-1, len(test.states)-1)]
					id := documentID
					if test.wrongDetailID {
						id = "another-document"
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"document": map[string]string{"id": id, "status": state}})
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			run := &workerRun{
				req:  PrepareRequest{RunID: runID, Config: AgentConfig{API: server.URL, OperatorToken: "operator-cap", Modules: ModuleConfig{KnowledgeWorkflow: "wf-cap", KnowledgeBytes: 2048}}},
				http: &httpClients{query: server.Client()}, modules: &moduleState{},
			}
			timeout := test.timeout
			if timeout == 0 {
				timeout = 5 * time.Second
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			result := (&Worker{}).sendModule(ctx, run, "knowledge", 1)
			if result.ok != test.wantOK || result.code != test.wantCode || int(polls.Load()) != test.wantPolls {
				t.Fatalf("result=%+v polls=%d, want ok=%v code=%s polls=%d", result, polls.Load(), test.wantOK, test.wantCode, test.wantPolls)
			}
			if !test.missingID && (result.resourceKind != "knowledge" || result.resourceID != documentID) {
				t.Fatalf("accepted/failed resource lost cleanup ownership: %+v", result)
			}
			if result.bytes <= 0 || result.attempts != 1 {
				t.Fatalf("operation accounting lost upload bytes or attempts: %+v", result)
			}
		})
	}
}

func TestEndToEndBusinessModulesAndAlarmSequence(t *testing.T) {
	t.Parallel()
	e := newE2E(t, 0, "quick", "rates: [20], measure: 10s", e2eExtra{
		fixtures:  ", alarmFraction: 0.2, alarmRuleId: rule-stress, alarmRecovers: true",
		inventory: "web: {{api}}\n",
		secrets:   "openkey: open-key-secret-e2e\n",
		handlers:  moduleHandlers,
		plan: `modules:
  ai: {enabled: true, mode: mock, runsPerMinute: 120, maxRuns: 4, timeout: 20s, p95: 0s}
  knowledge: {enabled: true, uploadsPerMinute: 60, workflowId: wf-cap, documentBytes: 2048, p95: 0s}
  video: {enabled: true, cameras: [cam-1], sessionsPerMinute: 60, timeout: 5s, p95: 0s}
  backup: {enabled: true, restore: true, timeout: 1m}
  exports: {enabled: true, rawDownloadsPerMinute: 60, replaysPerMinute: 30, inspectionsPerMinute: 30, timeout: 30s, p95: 0s}
  openapi: {enabled: true, keySecretRef: openkey, requestsPerSecond: 2, p95: 0s}
`,
	})
	runID, err := e.run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(e.results, runID)
	s := readSummary(t, dir)
	var rec PhaseRecord
	b, _ := os.ReadFile(filepath.Join(dir, "phases", s.Phases[0].PhaseID+".json"))
	_ = json.Unmarshal(b, &rec)
	if s.Verdict != VerdictPassed {
		t.Fatalf("%+v\n%s", s, b)
	}
	for _, stream := range []string{"ai", "knowledge", "video", "export_raw", "export_replay", "export_inspection", "openapi"} {
		st := rec.Streams[stream]
		if st == nil || st.OK == 0 {
			t.Errorf("module stream %s produced no successful operations: %+v", stream, st)
		}
	}
	if ai := rec.Streams["ai"]; ai != nil && ai.OK > 4 {
		t.Errorf("AI runs exceed the maxRuns budget: %d", ai.OK)
	}
	if len(rec.Operations) != 1 || !rec.Operations[0].OK || rec.Operations[0].Steps["restore"] != "COMPLETED" {
		t.Fatalf("backup operation %+v", rec.Operations)
	}
	if rec.Integrity.AlarmDevicesChecked == 0 || rec.Integrity.AlarmMismatches != 0 {
		t.Fatalf("alarm sequence %+v", rec.Integrity)
	}
	report, _ := os.ReadFile(filepath.Join(dir, "report.md"))
	for _, want := range []string{"harness-mock", "rule-stress"} {
		if !strings.Contains(string(report), want) {
			t.Errorf("report lacks %q", want)
		}
	}
	if err = checkNoSecrets(dir, []string{"open-key-secret-e2e", e.platform.token}); err != nil {
		t.Fatal(err)
	}
	resources := map[string]bool{}
	ledgers, _ := filepath.Glob(filepath.Join(dir, "ledgers", "*", "*.jsonl.gz"))
	for _, path := range ledgers {
		if err = ReadLedger(path, func(_ LedgerHeader, entry LedgerEntry) error {
			if entry.ResourceID != "" {
				resources[entry.ResourceKind] = true
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, kind := range []string{"knowledge", "inspection", "alarm-analysis", "replay"} {
		if !resources[kind] {
			t.Errorf("module %s lacks cleanup ownership in ledger", kind)
		}
	}
}

func TestEndToEndAlarmSequenceMismatchFailsIntegrity(t *testing.T) {
	t.Parallel()
	e := newE2E(t, 0, "quick", "rates: [20], measure: 10s", e2eExtra{
		fixtures: ", alarmFraction: 0.3, alarmRuleId: rule-stress",
		handlers: func(_ *http.ServeMux, f *fakePlatform) { f.dropAlarms = true },
	})
	runID, err := e.run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s := readSummary(t, filepath.Join(e.results, runID))
	var rec PhaseRecord
	b, _ := os.ReadFile(filepath.Join(e.results, runID, "phases", s.Phases[0].PhaseID+".json"))
	_ = json.Unmarshal(b, &rec)
	if s.Verdict != VerdictFailed || s.VerdictReason != ReasonIntegrity || rec.Integrity.AlarmMismatches == 0 || len(rec.Integrity.AlarmSamples) == 0 {
		t.Fatalf("%+v %+v", s, rec.Integrity)
	}
}

func TestEndToEndResilienceInjectsRecoversAndMeasuresRecovery(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fault commands use /bin/sh")
	}
	t.Parallel()
	down := filepath.Join(t.TempDir(), "down")
	e := newE2E(t, 0, "resilience", "rates: [20], measure: 12s", e2eExtra{
		handlers: func(_ *http.ServeMux, f *fakePlatform) { f.downFile = down },
		plan: `faults:
  enabled: true
  maxRecovery: 30s
  actions:
    - {agent: local, action: ingest-down, at: 4s, duration: 3s}
`,
	})
	e.faultAllow = FaultAllowlist{"ingest-down": {Inject: []string{"/bin/sh", "-c", "touch " + down}, Recover: []string{"/bin/sh", "-c", "rm -f " + down}, Timeout: Duration(10 * time.Second)}}
	runID, err := e.run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(e.results, runID)
	s := readSummary(t, dir)
	var rec PhaseRecord
	b, _ := os.ReadFile(filepath.Join(dir, "phases", s.Phases[0].PhaseID+".json"))
	_ = json.Unmarshal(b, &rec)
	if len(rec.Faults) != 1 || !rec.Faults[0].InjectOK || !rec.Faults[0].RecoverOK || rec.Faults[0].RecoverAt-rec.Faults[0].InjectedAt < 2500 {
		t.Fatalf("fault events %+v", rec.Faults)
	}
	if _, err = os.Stat(down); err == nil {
		t.Fatal("fault left injected")
	}
	if rec.Recovery == nil || rec.Recovery.RecoverySeconds == nil || rec.Recovery.BaselinePerSec == nil || *rec.Recovery.DuringPerSec >= *rec.Recovery.BaselinePerSec {
		t.Fatalf("recovery %+v", rec.Recovery)
	}
	// 503s during the fault are recorded, not an SLO failure; nothing confirmed was lost.
	m := rec.MessageTotals()
	if m.Codes["503"] == 0 || s.Verdict != VerdictPassed || s.Capacity["mixedBusinessMessagesPerSecond"].Classification != ClassResilience {
		t.Fatalf("verdict %s codes %v\n%s", s.Verdict, m.Codes, b)
	}
	for _, name := range []string{"charts/recovery.svg"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Error("missing", name)
		}
	}
	md, _ := os.ReadFile(filepath.Join(dir, "report.md"))
	if !strings.Contains(string(md), "local/ingest-down") || !strings.Contains(string(md), "故障恢复预设") {
		t.Fatal(string(md))
	}
}

func TestServiceRunsPlansAgainstTrustedEnvironments(t *testing.T) {
	t.Parallel()
	e := newE2E(t, 0, "quick", "rates: [20], measure: 10s")
	invDir := filepath.Join(e.dir, "environments")
	_ = os.MkdirAll(invDir, 0o750)
	inv, _ := os.ReadFile(filepath.Join(e.dir, "inv.yaml"))
	_ = os.WriteFile(filepath.Join(invDir, "lab.yaml"), inv, 0o600)
	plan, _ := os.ReadFile(e.planPath)
	token := strings.Repeat("s", 40)
	svc := NewService(ServeOptions{InventoryDir: invDir, ResultsDir: e.results, SecretsPath: e.secrets, Token: token,
		NewStore: func(context.Context, string, string) (Store, error) { return e.platform.store, nil }})
	srv := httptest.NewServer(svc.Handler())
	t.Cleanup(srv.Close)
	call := func(method, path string, body any) (int, []byte) {
		var rd io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = bytes.NewReader(b)
		}
		req, _ := http.NewRequest(method, srv.URL+path, rd)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, b
	}
	if resp, _ := http.Get(srv.URL + "/v1/environments"); resp.StatusCode != 401 {
		t.Fatal("environments without token", resp.StatusCode)
	}
	status, body := call("GET", "/v1/environments", nil)
	if status != 200 || !strings.Contains(string(body), `"name":"lab"`) || strings.Contains(string(body), e.platform.srv.URL) {
		t.Fatalf("environments must list names without addresses: %s", body)
	}
	status, body = call("POST", "/v1/plans/validate", map[string]string{"environment": "../etc/passwd", "plan": "schemaVersion: 1\nnme: x\n"})
	if status != 200 || !strings.Contains(string(body), `"valid":false`) || !strings.Contains(string(body), "请选择已登记的测试环境") {
		t.Fatalf("%d %s", status, body)
	}
	status, body = call("POST", "/v1/plans/validate", map[string]string{"environment": "lab", "plan": string(plan)})
	if status != 200 || !strings.Contains(string(body), `"valid":true`) {
		t.Fatalf("%d %s", status, body)
	}
	status, body = call("POST", "/v1/runs", map[string]string{"environment": "lab", "plan": string(plan)})
	if status != 202 {
		t.Fatalf("%d %s", status, body)
	}
	var started struct{ RunID string }
	_ = json.Unmarshal(body, &started)
	if status, _ = call("POST", "/v1/runs", map[string]string{"environment": "lab", "plan": string(plan)}); status != 409 {
		t.Fatal("a second run must be refused while one is active", status)
	}
	var info RunInfo
	for deadline := time.Now().Add(90 * time.Second); ; {
		status, body = call("GET", "/v1/runs/"+started.RunID, nil)
		_ = json.Unmarshal(body, &info)
		if status == 200 && !info.Active && info.Verdict != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("run did not finish: %s", body)
		}
		time.Sleep(300 * time.Millisecond)
	}
	if info.Status != StatusFinished || info.Verdict != VerdictPassed || info.Conclusion == "" || len(info.Reports) < 4 || info.Preset != "quick" {
		t.Fatalf("%+v", info)
	}
	status, body = call("GET", "/v1/runs", nil)
	if status != 200 || !strings.Contains(string(body), started.RunID) {
		t.Fatal(string(body))
	}
	status, body = call("GET", "/v1/runs/"+started.RunID+"/report?format=markdown", nil)
	if status != 200 || !strings.Contains(string(body), "容量测试报告") {
		t.Fatal(status)
	}
	status, body = call("GET", "/v1/runs/"+started.RunID+"/report?format=zip", nil)
	if status != 200 || !bytes.HasPrefix(body, []byte("PK")) {
		t.Fatal("zip", status)
	}
	if status, _ = call("POST", "/v1/runs/cap-20200101-000000-000000/stop", nil); status != 404 {
		t.Fatal("stop of an unknown run", status)
	}
	if status, _ = call("GET", "/v1/runs/..%2f..%2fetc/report", nil); status != 404 {
		t.Fatal("path traversal in run id", status)
	}
}

// nodeExporter imitates node-exporter counters that grow with time.
func nodeExporter(mux *http.ServeMux, _ *fakePlatform) {
	start := time.Now()
	mux.HandleFunc("GET /node/metrics", func(w http.ResponseWriter, _ *http.Request) {
		t := time.Since(start).Seconds()
		fmt.Fprintf(w, "# TYPE node_cpu_seconds_total counter\n")
		for cpu := 0; cpu < 2; cpu++ {
			fmt.Fprintf(w, "node_cpu_seconds_total{cpu=\"%d\",mode=\"idle\"} %f\nnode_cpu_seconds_total{cpu=\"%d\",mode=\"user\"} %f\nnode_cpu_seconds_total{cpu=\"%d\",mode=\"iowait\"} %f\n", cpu, 100+t*0.6, cpu, 50+t*0.3, cpu, 5+t*0.1)
		}
		fmt.Fprintf(w, "node_memory_MemTotal_bytes 8e+09\nnode_memory_MemAvailable_bytes 6e+09\n")
		fmt.Fprintf(w, "node_disk_io_time_seconds_total{device=\"sda\"} %f\nnode_disk_io_time_seconds_total{device=\"loop0\"} %f\n", t*0.25, t)
		fmt.Fprintf(w, "node_filesystem_avail_bytes{device=\"/dev/sda1\",fstype=\"ext4\",mountpoint=\"/\"} 5e+10\nnode_filesystem_size_bytes{device=\"/dev/sda1\",fstype=\"ext4\",mountpoint=\"/\"} 1e+11\n")
		fmt.Fprintf(w, "node_network_receive_bytes_total{device=\"eth0\"} %f\nnode_network_transmit_bytes_total{device=\"eth0\"} %f\nnode_network_receive_bytes_total{device=\"lo\"} %f\n", t*1e6, t*2e6, t*9e9)
	})
}

func TestEndToEndHostChartsAndPNGOutput(t *testing.T) {
	t.Parallel()
	e := newE2E(t, 0, "quick", "rates: [20], measure: 10s", e2eExtra{
		handlers:  nodeExporter,
		inventory: "nodes:\n  - {name: host-a, url: {{api}}/node/metrics}\n",
		plan:      "outputs: {formats: [html, markdown, json, csv, svg, png]}\n",
	})
	runID, err := e.run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(e.results, runID)
	for _, name := range []string{"charts/hosts.svg", "charts/hosts.png", "charts/throughput.png", "charts/latency.png", "observations/nodes.jsonl"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Error("missing", name)
		}
	}
	b, _ := os.ReadFile(filepath.Join(dir, "charts", "throughput.png"))
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil || img.Bounds().Dx() != chartW*2 {
		t.Fatal("png", err)
	}
	rounds, _ := LoadRounds(filepath.Join(dir, "observations", "nodes.jsonl"))
	hosts := HostSeries(rounds)["host-a"]
	var cpu, disk []float64
	for _, p := range hosts {
		if p.CPUValid {
			cpu = append(cpu, p.CPU)
		}
		if p.DiskValid {
			disk = append(disk, p.Disk)
		}
		if p.MemValid && math.Abs(p.Memory-25) > 0.01 {
			t.Fatal("memory used %", p.Memory)
		}
	}
	// user 0.3 of (0.6 idle + 0.3 user + 0.1 iowait) per CPU → 30% busy; sda 25%, loop ignored.
	if len(cpu) == 0 || math.Abs(cpu[len(cpu)-1]-30) > 2 || len(disk) == 0 || math.Abs(disk[len(disk)-1]-25) > 3 {
		t.Fatalf("cpu %v disk %v", cpu, disk)
	}
	if raw, _ := os.ReadFile(filepath.Join(dir, "observations", "nodes.jsonl")); bytes.Contains(raw, []byte("node_cpu_seconds_total")) {
		t.Fatal("node rounds must keep only reduced values")
	}
}

func TestEndToEndResumeReplaysCompletedStepsWithNextGeneration(t *testing.T) {
	t.Parallel()
	e := newE2E(t, 0, "quick", "rates: [10, 20], measure: 10s")
	runID, err := e.run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(e.results, runID)
	first := readSummary(t, dir)
	if len(first.Phases) != 2 {
		t.Fatal(first.Phases)
	}
	// Simulate a controller that died during the second step.
	_ = os.Remove(filepath.Join(dir, "phases", first.Phases[1].PhaseID+".json"))
	_ = os.Remove(filepath.Join(dir, "summary.json"))
	st, _ := ReadState(e.results, runID)
	st.Status, st.Result, st.UpdatedAt = StatusRunning, "", time.Now().Add(-time.Minute).UnixMilli()
	_ = writeJSONAtomic(filepath.Join(dir, "state.json"), st)
	resume := func() (string, error) {
		return Run(context.Background(), RunOptions{PlanPath: e.planPath, SecretsPath: e.secrets, ResultsDir: e.results, ResumeRunID: runID,
			NewStore: func(context.Context, string, string) (Store, error) { return e.platform.store, nil }})
	}
	fresh := st
	fresh.UpdatedAt = time.Now().UnixMilli()
	_ = writeJSONAtomic(filepath.Join(dir, "state.json"), fresh)
	if _, err = resume(); !errors.Is(err, ErrRunStillActive) {
		t.Fatal("a recently updated run must not be resumed", err)
	}
	_ = writeJSONAtomic(filepath.Join(dir, "state.json"), st)
	onboarded := e.platform.seen.Load()
	got, err := resume()
	if err != nil || got != runID {
		t.Fatal(got, err)
	}
	s := readSummary(t, dir)
	after, _ := ReadState(e.results, runID)
	if len(s.Phases) != 2 || s.Phases[0].PhaseID != first.Phases[0].PhaseID || s.Phases[1].PhaseID != first.Phases[1].PhaseID || after.Generation != 2 || after.Status != StatusFinished {
		t.Fatalf("phases %+v state %+v", s.Phases, after)
	}
	if e.platform.seen.Load()-onboarded < 150 || e.platform.seen.Load()-onboarded > 260 {
		t.Fatal("only the interrupted step is sent again", e.platform.seen.Load()-onboarded)
	}
	events, _ := os.ReadFile(filepath.Join(dir, "events.jsonl"))
	if !bytes.Contains(events, []byte(`"type":"resume"`)) || !bytes.Contains(events, []byte("replayed from the interrupted run")) {
		t.Fatal(string(events))
	}
	if _, err = resume(); err == nil || !strings.Contains(err.Error(), "already finished") {
		t.Fatal("a finished run cannot be resumed", err)
	}
}

func TestSelfModuleRunsWithoutInventoryOrSecretsAndProvisionsFixtures(t *testing.T) {
	t.Parallel()
	f := newFakePlatform(t, 0, func(_ *http.ServeMux, f *fakePlatform) { f.products = map[string]bool{} })
	env := map[string]string{
		"IOT_CAPACITY_API_URL":      f.srv.URL,
		"IOT_CAPACITY_METRICS":      "combined@platform-api=" + f.srv.URL + "/metrics",
		"IOT_CAPACITY_POSTGRES_DSN": "postgres://observer:self-pg-secret@db/iot",
	}
	if _, err := SelfEnvironmentFromEnv(func(k string) string { return map[string]string{"IOT_CAPACITY_API_URL": f.srv.URL}[k] }); err == nil {
		t.Fatal("a module environment without metrics and database must be rejected")
	}
	self, err := SelfEnvironmentFromEnv(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	results := t.TempDir()
	token := strings.Repeat("m", 40)
	svc := NewService(ServeOptions{Self: &self, ResultsDir: results, Token: token,
		NewStore: func(context.Context, string, string) (Store, error) { return f.store, nil }})
	srv := httptest.NewServer(svc.Handler())
	t.Cleanup(srv.Close)
	call := func(method, path string, body any) (int, []byte) {
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest(method, srv.URL+path, bytes.NewReader(b))
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		out, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, out
	}
	status, body := call("GET", "/v1/environments", nil)
	if status != 200 || !strings.Contains(string(body), `"name":"self"`) || !strings.Contains(string(body), "本平台") || strings.Contains(string(body), f.srv.URL) {
		t.Fatalf("%d %s", status, body)
	}
	// The page sends no tenant, credentials or product: the platform adds
	// tenant and token, the module provisions the product and rule.
	plan := "schemaVersion: 1\nname: ui-quick\npreset: quick\nfixtures: {deviceCount: 5, reuseDevices: true, messageBytes: 200, alarmFraction: 0.3, autoProvision: true}\nload: {ingressShare: {http: 1}, initialMessagesPerSecond: 10}\nsearch: {rates: [10], measure: 10s, warmup: 0s, drainTimeout: 10s, cooldown: 1s, observeInterval: 1s}\nbudget: {maximumWallTime: 5m, maximumMessagesPerSecond: 100, maximumEvidenceGiB: 1}\n"
	req := map[string]string{"environment": "self", "plan": plan, "tenant": "t1", "operatorToken": f.token}
	if status, body = call("POST", "/v1/plans/validate", req); status != 200 || !strings.Contains(string(body), `"valid":true`) {
		t.Fatalf("%d %s", status, body)
	}
	status, body = call("POST", "/v1/runs", req)
	if status != 202 {
		t.Fatalf("%d %s", status, body)
	}
	var started struct{ RunID string }
	_ = json.Unmarshal(body, &started)
	var info RunInfo
	for deadline := time.Now().Add(90 * time.Second); ; {
		_, body = call("GET", "/v1/runs/"+started.RunID, nil)
		_ = json.Unmarshal(body, &info)
		if !info.Active && info.Verdict != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("run did not finish: %s", body)
		}
		time.Sleep(300 * time.Millisecond)
	}
	dir := filepath.Join(results, started.RunID)
	s := readSummary(t, dir)
	if s.Verdict != VerdictPassed {
		b, _ := os.ReadFile(filepath.Join(dir, "preflight.json"))
		t.Fatalf("%+v\n%s", s, b)
	}
	f.mu.Lock()
	created, rule := f.products[AutoProductID], f.rules[AutoRuleID]
	f.mu.Unlock()
	if !created || !strings.Contains(rule, `"field":"stressAlarm"`) || !strings.Contains(rule, `"productId":"cap-standard"`) {
		t.Fatalf("auto-provisioning: product %v rule %s", created, rule)
	}
	sanitized, _ := os.ReadFile(filepath.Join(dir, "plan.sanitized.yaml"))
	if !strings.Contains(string(sanitized), "tenant: t1") || !strings.Contains(string(sanitized), "alarmRuleId: cap-stress-alarm") {
		t.Fatal(string(sanitized))
	}
	var rec PhaseRecord
	b, _ := os.ReadFile(filepath.Join(dir, "phases", s.Phases[0].PhaseID+".json"))
	_ = json.Unmarshal(b, &rec)
	if rec.Integrity.AlarmDevicesChecked == 0 {
		t.Fatal("the provisioned rule must be reconciled")
	}
	manifest, _ := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if !strings.Contains(string(manifest), "自动创建的产品 cap-standard") {
		t.Fatal("the report must say what was created", string(manifest))
	}
	if err := checkNoSecrets(dir, []string{f.token, "self-pg-secret"}); err != nil {
		t.Fatal(err)
	}
	// Wrong environments are refused in module mode.
	if status, body = call("POST", "/v1/plans/validate", map[string]string{"environment": "other", "plan": plan, "tenant": "t1"}); !strings.Contains(string(body), "请选择已登记的测试环境") {
		t.Fatal(status, string(body))
	}
}

func TestEndToEndAlarmSequenceIgnoresPreviousStepAlarms(t *testing.T) {
	t.Parallel()
	// Sparse alarms leave devices that alarmed in step 1 but not in step 2;
	// their step-1 alarms must not count against step 2.
	e := newE2E(t, 0, "quick", "rates: [20, 20], measure: 10s", e2eExtra{
		fixtures: ", alarmFraction: 0.03, alarmRuleId: rule-stress, alarmRecovers: true",
	})
	runID, err := e.run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s := readSummary(t, filepath.Join(e.results, runID))
	if len(s.Phases) != 2 {
		t.Fatalf("phases %+v", s.Phases)
	}
	for _, ph := range s.Phases {
		var rec PhaseRecord
		b, _ := os.ReadFile(filepath.Join(e.results, runID, "phases", ph.PhaseID+".json"))
		_ = json.Unmarshal(b, &rec)
		if rec.Verdict != VerdictPassed || rec.Integrity.AlarmDevicesChecked == 0 || rec.Integrity.AlarmMismatches != 0 {
			t.Fatalf("%s verdict=%s %+v", ph.PhaseID, rec.Verdict, rec.Integrity)
		}
	}
}
