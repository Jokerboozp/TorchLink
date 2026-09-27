package capacity

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
	mux.HandleFunc("GET /api/v1/onboarding/preflight", authed(ok))
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
	return Run(ctx, RunOptions{PlanPath: e.planPath, SecretsPath: e.secrets, ResultsDir: e.results, SourceCommit: "test",
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
		return auth(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		})
	}
	mux.HandleFunc("POST /api/v1/ai/alarm-analysis/{id}/run", reply(202, `{"jobId":"job-1"}`))
	mux.HandleFunc("GET /api/v1/ai/alarm-analysis/{id}/progress/{job}", reply(200, `{"status":"succeeded"}`))
	mux.HandleFunc("POST /api/v1/knowledge/documents", auth(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil || r.FormValue("workflowId") != "wf-cap" {
			w.WriteHeader(400)
			return
		}
		w.WriteHeader(201)
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
