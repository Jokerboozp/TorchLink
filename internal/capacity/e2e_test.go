package capacity

import (
	"bytes"
	"context"
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
}

func newFakePlatform(t *testing.T, loseN int64) *fakePlatform {
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
	mux.HandleFunc("GET /api/v1/alarms", authed(ok))
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
			ID string `json:"id"`
		}
		_ = json.Unmarshal(body, &v)
		raw := StandardRawID(r.PathValue("tenant"), r.PathValue("product"), device, r.PathValue("kind"), v.ID)
		if n := f.seen.Add(1); f.loseN == 0 || n%f.loseN != 0 {
			f.store.archive(raw, payloadHash(body), time.Now().Add(20*time.Millisecond).UnixMilli())
			f.archived.Add(1)
		}
		w.WriteHeader(202)
		fmt.Fprintf(w, `{"messageId":%q,"created":true,"status":"ACCEPTED"}`, raw)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

type e2eEnv struct {
	dir, planPath, secrets, results string
	platform                        *fakePlatform
}

func newE2E(t *testing.T, loseN int64, preset, search string) e2eEnv {
	t.Helper()
	dir := t.TempDir()
	f := newFakePlatform(t, loseN)
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
`, f.srv.URL, f.srv.URL, remote.URL)
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
`, preset, search)
	e := e2eEnv{dir: dir, planPath: filepath.Join(dir, "plan.yaml"), secrets: filepath.Join(dir, "secrets.yaml"), results: filepath.Join(dir, "results"), platform: f}
	for path, body := range map[string]string{filepath.Join(dir, "inv.yaml"): inv, e.planPath: plan, e.secrets: "op: " + f.token + "\nag: " + agentToken + "\npg: postgres://observer:pg-secret-e2e@db/iot\n"} {
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
