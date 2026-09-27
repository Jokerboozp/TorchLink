package capacity

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"iot-platform/internal/adapters/postgres"
	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
)

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
	for _, name := range []string{"core-mixed.yaml", "quick-local.yaml"} {
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
		"no load adapter":       func(p *Plan) { p.Modules.AI.Enabled = true },
		"fault injection":       func(p *Plan) { p.Faults.Enabled = true },
		"png":                   func(p *Plan) { p.Outputs.Formats = []string{"png"} },
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
}

func newMemStore() *memStore {
	return &memStore{raws: map[string]RawRecord{}, bodies: map[string]int{}, std: map[string][]StandardRecord{}, telemetry: map[string]int{}}
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
	if _, err = repo.SaveStandardMessageIfAbsent(ctx, msg); err != nil {
		t.Fatal(err)
	}
	if err = repo.MarkStandardMessageProcessed(ctx, "t", "std-1"); err != nil {
		t.Fatal(err)
	}
	if should, _, err := repo.ClaimStandardMessage(ctx, msg); err != nil || should {
		t.Fatal("a processed message must not be claimed again", should, err)
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
