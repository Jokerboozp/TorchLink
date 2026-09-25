package opscenter

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"iot-platform/internal/adapters/observability"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// fakePrometheus simulates auto-reload: it loads the managed directory when
// asked for rules and fails the reload when a file contains "BROKEN".
type fakePrometheus struct {
	ports.MetricsBackend
	dir        string
	mu         sync.Mutex
	reloadOK   bool
	lastConfig time.Time
	loaded     []model.OpsRuleGroup
	frozen     bool
}

func (f *fakePrometheus) Configured() bool { return true }
func (f *fakePrometheus) FormatQuery(_ context.Context, q string) (string, error) {
	if strings.Contains(q, "((") {
		return "", &ports.OpsUpstreamError{Status: 400, Message: "parse error: unexpected"}
	}
	return q, nil
}

func (f *fakePrometheus) reload() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.frozen {
		return
	}
	entries, _ := os.ReadDir(f.dir)
	groups := []model.OpsRuleGroup{{Name: "iot-platform", File: "/etc/prometheus/alerts.yml", Rules: []model.OpsRule{{Kind: "alert", Name: "IotPlatformDown"}}}}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yml") {
			continue
		}
		content, _ := os.ReadFile(filepath.Join(f.dir, e.Name()))
		if strings.Contains(string(content), "BROKEN") {
			f.reloadOK = false
			return
		}
		var parsed ruleFileYAML
		_ = yaml.Unmarshal(content, &parsed)
		for _, g := range parsed.Groups {
			group := model.OpsRuleGroup{Name: g.Name, File: "/etc/prometheus/rules/" + e.Name()}
			for _, r := range g.Rules {
				name, kind := r.Record, "record"
				if r.Alert != "" {
					name, kind = r.Alert, "alert"
				}
				group.Rules = append(group.Rules, model.OpsRule{Kind: kind, Name: name, Health: "ok"})
			}
			groups = append(groups, group)
		}
	}
	f.reloadOK, f.loaded, f.lastConfig = true, groups, time.Now().Add(time.Second)
}

func (f *fakePrometheus) Runtime(context.Context) (ports.PrometheusRuntime, error) {
	f.reload()
	f.mu.Lock()
	defer f.mu.Unlock()
	return ports.PrometheusRuntime{ReloadSuccess: f.reloadOK, LastConfig: f.lastConfig}, nil
}

func (f *fakePrometheus) RuleGroups(context.Context) ([]model.OpsRuleGroup, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]model.OpsRuleGroup(nil), f.loaded...), nil
}

func newRuleService(t *testing.T) (*Service, *fakePrometheus, string) {
	t.Helper()
	dir := t.TempDir()
	active, state := filepath.Join(dir, "rules"), filepath.Join(dir, "state")
	prom := &fakePrometheus{dir: active, reloadOK: true}
	prom.reload()
	svc := &Service{Metrics: prom, PromRules: observability.NewDirStore(active, state, ".yml", 0o644), Limits: Limits{ReloadTimeout: 2 * time.Second}, PollInterval: 10 * time.Millisecond}
	return svc, prom, active
}

func sampleGroup() RuleGroupInput {
	return RuleGroupInput{Name: "平台延迟", Interval: "30s", Enabled: true, Rules: []RuleInput{
		{Kind: "record", Name: "job:up:sum", Expr: "sum by (job) (up)"},
		{Kind: "alert", Name: "HighLatency", Expr: "job:up:sum < 1", For: "5m", Labels: map[string]string{"severity": "warning"}, Annotations: map[string]string{"summary": "{{ $labels.job }} 延迟 {{ $value | humanize }}"}},
	}}
}

func TestSaveRuleGroupWritesLoadsAndLists(t *testing.T) {
	svc, _, active := newRuleService(t)
	group, err := svc.SaveRuleGroup(context.Background(), SourcePrometheus, "", sampleGroup(), "ops@tenant_ops")
	if err != nil {
		t.Fatal(err)
	}
	if !group.Managed || group.UpdatedBy != "ops@tenant_ops" || len(group.Rules) != 2 {
		t.Fatalf("group = %+v", group)
	}
	files, _ := os.ReadDir(active)
	if len(files) != 1 {
		t.Fatalf("files = %v", files)
	}
	groups, err := svc.RuleGroups(context.Background(), SourcePrometheus)
	if err != nil || len(groups) != 2 || groups[0].Managed || !groups[1].Loaded || groups[1].Rules[0].Health != "ok" {
		t.Fatalf("groups = %+v err = %v", groups, err)
	}
	if _, err := svc.SaveRuleGroup(context.Background(), SourcePrometheus, "", sampleGroup(), "x"); err == nil {
		t.Fatal("duplicate group name must be rejected")
	}
	builtin := sampleGroup()
	builtin.Name = "iot-platform"
	if _, err := svc.SaveRuleGroup(context.Background(), SourcePrometheus, "", builtin, "x"); err == nil {
		t.Fatal("name of a deployment group must be rejected")
	}
}

func TestSaveRuleGroupValidation(t *testing.T) {
	svc, _, _ := newRuleService(t)
	cases := []func(*RuleGroupInput){
		func(g *RuleGroupInput) { g.Rules[0].Expr = "sum((" },
		func(g *RuleGroupInput) { g.Rules[1].For = "5 minutes" },
		func(g *RuleGroupInput) { g.Rules[1].Annotations = map[string]string{"summary": "{{ .Labels "} },
		func(g *RuleGroupInput) { g.Rules[1].Name = "高延迟" },
		func(g *RuleGroupInput) { g.Rules[0].Labels = map[string]string{"__name__": "x"} },
		func(g *RuleGroupInput) { g.Name = "a/b" },
	}
	for i, mutate := range cases {
		in := sampleGroup()
		mutate(&in)
		_, err := svc.SaveRuleGroup(context.Background(), SourcePrometheus, "", in, "x")
		var v *ValidationError
		if !errors.As(err, &v) {
			t.Fatalf("case %d: expected validation error, got %v", i, err)
		}
	}
}

func TestRejectedRuleChangeIsRolledBack(t *testing.T) {
	svc, prom, active := newRuleService(t)
	if _, err := svc.SaveRuleGroup(context.Background(), SourcePrometheus, "", sampleGroup(), "x"); err != nil {
		t.Fatal(err)
	}
	name := ruleFileName("平台延迟")
	before, _ := os.ReadFile(filepath.Join(active, name+".yml"))
	update := sampleGroup()
	update.Rules[1].Annotations = map[string]string{"summary": "BROKEN"}
	_, err := svc.SaveRuleGroup(context.Background(), SourcePrometheus, "平台延迟", update, "y")
	var apply *ApplyError
	if !errors.As(err, &apply) || !apply.RolledBack {
		t.Fatalf("expected rolled back apply error, got %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(active, name+".yml"))
	if strings.Contains(string(after), "BROKEN") || string(stripHeader(after)) != string(stripHeader(before)) {
		t.Fatalf("rollback did not restore content:\n%s", after)
	}
	// The rollback rewrites the header so auto-reload runs again and recovers.
	if _, err := prom.Runtime(context.Background()); err != nil || !prom.reloadOK {
		t.Fatal("prometheus should reload successfully after rollback")
	}
}

func TestUnconfirmedRuleChangeTimesOutAndRollsBack(t *testing.T) {
	svc, prom, active := newRuleService(t)
	prom.frozen = true
	_, err := svc.SaveRuleGroup(context.Background(), SourcePrometheus, "", sampleGroup(), "x")
	var apply *ApplyError
	if !errors.As(err, &apply) || !apply.RolledBack || !strings.Contains(apply.Detail, "auto-reload") {
		t.Fatalf("expected timeout rollback, got %v", err)
	}
	files, _ := os.ReadDir(active)
	for _, f := range files {
		if !f.IsDir() {
			t.Fatalf("new file must be removed on rollback, found %s", f.Name())
		}
	}
}

func TestDisableAndDeleteRuleGroup(t *testing.T) {
	svc, _, active := newRuleService(t)
	saved, err := svc.SaveRuleGroup(context.Background(), SourcePrometheus, "", sampleGroup(), "x")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SetRuleGroupEnabled(context.Background(), SourcePrometheus, "平台延迟", false, "stale", "x"); !errors.Is(err, ports.ErrOpsConflict) {
		t.Fatalf("stale revision must conflict, got %v", err)
	}
	if err := svc.SetRuleGroupEnabled(context.Background(), SourcePrometheus, "平台延迟", false, saved.Revision, "x"); err != nil {
		t.Fatal(err)
	}
	groups, _ := svc.RuleGroups(context.Background(), SourcePrometheus)
	if len(groups) != 2 || groups[1].Enabled || groups[1].Loaded {
		t.Fatalf("disabled group = %+v", groups)
	}
	if entries, _ := filepath.Glob(filepath.Join(active, "*.yml")); len(entries) != 0 {
		t.Fatalf("disabled group must leave the loaded directory: %v", entries)
	}
	if err := svc.DeleteRuleGroup(context.Background(), SourcePrometheus, "平台延迟", "", "x"); err != nil {
		t.Fatal(err)
	}
	groups, _ = svc.RuleGroups(context.Background(), SourcePrometheus)
	if len(groups) != 1 {
		t.Fatalf("groups after delete = %+v", groups)
	}
}

func TestLokiRejectsRecordingRules(t *testing.T) {
	svc := &Service{Logs: &fakeLoki{}, LokiRules: observability.NewDirStore(t.TempDir(), t.TempDir(), ".yaml", 0o644)}
	_, err := svc.SaveRuleGroup(context.Background(), SourceLoki, "", sampleGroup(), "x")
	var v *ValidationError
	if !errors.As(err, &v) || !strings.Contains(v.Message, "remote write") {
		t.Fatalf("expected recording rule rejection, got %v", err)
	}
}

type fakeLoki struct {
	ports.LogsBackend
	runtime ports.LokiRuntimeState
	limits  ports.LokiLimits
	onRead  func()
}

// Query mimics Loki's instant endpoint: log selectors are refused with 400.
func (f *fakeLoki) Query(_ context.Context, q ports.LogQuery) (model.LogQueryResult, error) {
	if q.Instant && strings.HasPrefix(strings.TrimSpace(q.Query), "{") {
		return model.LogQueryResult{}, &ports.OpsUpstreamError{Status: 400, Message: "log queries are not supported as an instant query type"}
	}
	return model.LogQueryResult{ResultType: "vector"}, nil
}

func (f *fakeLoki) Configured() bool                                        { return true }
func (f *fakeLoki) FormatQuery(_ context.Context, q string) (string, error) { return q, nil }
func (f *fakeLoki) Limits(context.Context) (ports.LokiLimits, error)        { return f.limits, nil }
func (f *fakeLoki) RuntimeState(context.Context) (ports.LokiRuntimeState, error) {
	if f.onRead != nil {
		f.onRead()
	}
	return f.runtime, nil
}

func TestSaveRetentionPreservesOtherOverridesAndConfirmsReload(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "runtime.yaml")
	if err := os.WriteFile(file, []byte("overrides:\n  fake:\n    ingestion_rate_mb: 8\n  other:\n    retention_period: 48h\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	loki := &fakeLoki{runtime: ports.LokiRuntimeState{Hash: "a", Success: true}, limits: ports.LokiLimits{RetentionEnabled: true}}
	loki.onRead = func() {
		content, _ := os.ReadFile(file)
		if strings.Contains(string(content), "168h") {
			loki.runtime.Hash = "b"
		}
	}
	svc := &Service{Logs: loki, LokiRuntime: observability.NewFileStore(file, filepath.Join(dir, "state"), 0o644), Limits: Limits{ReloadTimeout: time.Second}, PollInterval: 10 * time.Millisecond}
	in := RetentionInput{Period: "168h"}
	in.Streams = append(in.Streams, struct {
		Matchers []Matcher `json:"matchers"`
		Selector string    `json:"selector"`
		Priority int       `json:"priority"`
		Period   string    `json:"period"`
	}{Matchers: []Matcher{{Name: "service_name", Op: "=", Value: "platform-api"}}, Priority: 1, Period: "72h"})
	settings, err := svc.SaveRetention(context.Background(), "", in, "ops@t")
	if err != nil {
		t.Fatal(err)
	}
	content, _ := os.ReadFile(file)
	if !strings.Contains(string(content), "ingestion_rate_mb: 8") || !strings.Contains(string(content), "other:") || settings.Period != "168h" || len(settings.Streams) != 1 || settings.Streams[0].Selector != `{service_name="platform-api"}` {
		t.Fatalf("settings = %+v\n%s", settings, content)
	}
	loki.runtime.Success = false
	loki.onRead = nil
	if _, err := svc.SaveRetention(context.Background(), "", RetentionInput{Period: "240h", Revision: settings.Revision}, "ops@t"); err == nil {
		t.Fatal("rejected runtime config must fail")
	}
	restored, _ := os.ReadFile(file)
	if !strings.Contains(string(restored), "168h") {
		t.Fatalf("rejected change must be rolled back:\n%s", restored)
	}
	if _, err := svc.SaveRetention(context.Background(), "", RetentionInput{Period: "1h"}, "x"); err == nil {
		t.Fatal("retention below 24h must be rejected")
	}
}

// Saving a disabled group leaves the loaded directory unchanged, so real
// Prometheus never reloads; the save must not wait for a reload or roll back.
func TestSavingDisabledGroupDoesNotWaitForReload(t *testing.T) {
	svc, prom, active := newRuleService(t)
	prom.frozen = true
	prom.lastConfig = time.Now().Add(-time.Hour)
	in := sampleGroup()
	in.Enabled = false
	saved, err := svc.SaveRuleGroup(context.Background(), SourcePrometheus, "", in, "x")
	if err != nil {
		t.Fatalf("saving a disabled group failed: %v", err)
	}
	in.Revision = saved.Revision
	in.Interval = "1m"
	if _, err := svc.SaveRuleGroup(context.Background(), SourcePrometheus, in.Name, in, "x"); err != nil {
		t.Fatalf("editing a disabled group failed: %v", err)
	}
	if entries, _ := filepath.Glob(filepath.Join(active, "*.yml")); len(entries) != 0 {
		t.Fatalf("disabled group must stay out of the loaded directory: %v", entries)
	}
	groups, _ := svc.RuleGroups(context.Background(), SourcePrometheus)
	if len(groups) != 2 || groups[1].Enabled || groups[1].Interval != "1m" {
		t.Fatalf("groups = %+v", groups)
	}
}

func TestLokiRulesMustBeSampleQueries(t *testing.T) {
	svc := &Service{Logs: &fakeLoki{}, LokiRules: observability.NewDirStore(t.TempDir(), t.TempDir(), ".yaml", 0o644)}
	in := RuleGroupInput{Name: "logs", Enabled: false, Rules: []RuleInput{{Kind: "alert", Name: "Bad", Expr: `{service_name="api"} |= "error"`}}}
	_, err := svc.SaveRuleGroup(context.Background(), SourceLoki, "", in, "x")
	var v *ValidationError
	if !errors.As(err, &v) || !strings.Contains(v.Message, "统计查询") {
		t.Fatalf("expected log selector rejection, got %v", err)
	}
	in.Rules[0].Expr = `sum(count_over_time({service_name="api"} |= "error" [5m])) > 0`
	if _, err := svc.SaveRuleGroup(context.Background(), SourceLoki, "", in, "x"); err != nil {
		t.Fatalf("sample query rejected: %v", err)
	}
}
