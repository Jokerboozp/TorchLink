package opscenter

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"iot-platform/internal/adapters/observability"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// overviewPrometheus counts target listings and can block them to simulate a
// slow Prometheus.
type overviewPrometheus struct {
	ports.MetricsBackend
	targetCalls atomic.Int32
	release     chan struct{}
}

func (f *overviewPrometheus) Configured() bool { return true }
func (f *overviewPrometheus) Targets(ctx context.Context) ([]model.ScrapeTarget, error) {
	f.targetCalls.Add(1)
	if f.release != nil {
		select {
		case <-f.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return []model.ScrapeTarget{{Job: "iot-platform", Health: "up"}, {Job: "node", Health: "down", LastError: "connection refused"}}, nil
}
func (f *overviewPrometheus) Query(context.Context, ports.MetricQuery) (model.MetricQueryResult, error) {
	v := 2.0
	return model.MetricQueryResult{Series: []model.MetricSeries{{Values: []*float64{&v}}}}, nil
}

func TestOverviewKPIsReturnsOnlyTheRequestedGroup(t *testing.T) {
	s := &Service{Metrics: &overviewPrometheus{}}
	group, err := s.OverviewKPIs(context.Background(), "host")
	if err != nil {
		t.Fatal(err)
	}
	if len(group.KPIs) != 3 {
		t.Fatalf("host group returned %d KPIs, want 3", len(group.KPIs))
	}
	for _, k := range group.KPIs {
		if k.Group != "host" || k.Status != "scrape_failed" {
			t.Fatalf("unexpected KPI %s group=%s status=%s", k.ID, k.Group, k.Status)
		}
	}
	if job := group.Jobs["node"]; job.Total != 1 || job.Up != 0 || job.LastError == "" {
		t.Fatalf("job health not reported: %+v", group.Jobs)
	}
	var validation *ValidationError
	if _, err := s.OverviewKPIs(context.Background(), "unknown"); !errors.As(err, &validation) {
		t.Fatalf("unknown group should be a validation error, got %v", err)
	}
}

// The overview page loads every KPI group in parallel; they must share one
// Prometheus target listing instead of issuing one each.
func TestOverviewKPIGroupsShareOneTargetListing(t *testing.T) {
	prom := &overviewPrometheus{release: make(chan struct{})}
	s := &Service{Metrics: prom}
	var wg sync.WaitGroup
	for _, group := range []string{"platform", "backup", "host", "observability"} {
		wg.Add(1)
		go func(group string) {
			defer wg.Done()
			if _, err := s.OverviewKPIs(context.Background(), group); err != nil {
				t.Error(err)
			}
		}(group)
	}
	time.Sleep(50 * time.Millisecond)
	close(prom.release)
	wg.Wait()
	if _, err := s.OverviewKPIs(context.Background(), "platform"); err != nil {
		t.Fatal(err)
	}
	if calls := prom.targetCalls.Load(); calls != 1 {
		t.Fatalf("target listing called %d times, want 1", calls)
	}
}

// A caller that gives up must not cancel the shared listing for others.
func TestOverviewTargetsSurviveFirstCallerCancel(t *testing.T) {
	prom := &overviewPrometheus{release: make(chan struct{})}
	s := &Service{Metrics: prom}
	first, cancel := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() { _, err := s.overviewTargets(first); firstDone <- err }()
	time.Sleep(20 * time.Millisecond)
	secondDone := make(chan error, 1)
	go func() { _, err := s.overviewTargets(context.Background()); secondDone <- err }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	if err := <-firstDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("first caller: %v", err)
	}
	close(prom.release)
	if err := <-secondDone; err != nil {
		t.Fatalf("second caller should get the shared result: %v", err)
	}
}

func TestComponentRejectsUnknownID(t *testing.T) {
	s := &Service{}
	if status, ok := s.Component(context.Background(), "loki"); !ok || status.State != "unconfigured" {
		t.Fatalf("loki without backend: ok=%v status=%+v", ok, status)
	}
	if _, ok := s.Component(context.Background(), "../metrics"); ok {
		t.Fatal("unknown component accepted")
	}
}

func TestFormatVariableMatchesGrafanaPrometheusEscaping(t *testing.T) {
	single := &varState{values: []string{`a'b\c`}, def: map[string]any{}}
	if got := formatVariable(single, "", "prometheus"); got != `a\\'b\\c` {
		t.Fatalf("single = %s", got)
	}
	multi := &varState{multi: true, values: []string{"a.b", "c|d"}, def: map[string]any{}}
	if got := formatVariable(multi, "", "prometheus"); got != `(a\\.b|c\\|d)` {
		t.Fatalf("multi = %s", got)
	}
	all := &varState{multi: true, all: true, allRaw: ".+", values: []string{"a", "b"}, def: map[string]any{"includeAll": true}}
	if got := formatVariable(all, "", "prometheus"); got != ".+" {
		t.Fatalf("all with allValue = %s", got)
	}
	if got := formatVariable(multi, "pipe", "prometheus"); got != "a.b|c|d" {
		t.Fatalf("pipe = %s", got)
	}
}

func TestInterpolateBuiltinsAndSyntaxes(t *testing.T) {
	from := time.Unix(0, 0)
	to := from.Add(time.Hour)
	vars := map[string]*varState{"job": {values: []string{"api"}, def: map[string]any{}}}
	builtins := builtinValues(from, to, 30*time.Second, 15*time.Second)
	got := interpolate(`rate(x{job="$job",a="${job}",b="[[job]]"}[$__rate_interval]) $__range $unknown`, vars, builtins, "prometheus")
	want := `rate(x{job="api",a="api",b="api"}[1m]) 1h $unknown`
	if got != want {
		t.Fatalf("interpolate = %s\nwant          %s", got, want)
	}
}

func TestCalculateIntervalRespectsMinimum(t *testing.T) {
	from := time.Unix(0, 0)
	if got := calculateInterval(from, from.Add(time.Hour), 800, 15*time.Second); got != 15*time.Second {
		t.Fatalf("interval = %v", got)
	}
	if got := calculateInterval(from, from.Add(7*24*time.Hour), 800, 15*time.Second); got != 15*time.Minute {
		t.Fatalf("interval = %v", got)
	}
}

func TestApplyVariableRegexAndSort(t *testing.T) {
	options := applyVariableRegex([]string{"node:9100", "api:8080", "node:9100"}, `/^(?P<text>[a-z]+):(?P<value>\d+)$/`)
	if len(options) != 2 || options[0].Text != "node" || options[0].Value != "9100" {
		t.Fatalf("options = %+v", options)
	}
	sortOptions(options, float64(1))
	if options[0].Text != "api" {
		t.Fatalf("sorted = %+v", options)
	}
}

func testIndex() (map[string]model.OpsDataSource, model.OpsDataSource) {
	prom := model.OpsDataSource{UID: "prom", Name: "Prometheus", Type: "prometheus", Supported: true, IsDefault: true}
	loki := model.OpsDataSource{UID: "loki", Name: "Loki", Type: "loki", Supported: true}
	es := model.OpsDataSource{UID: "es", Name: "ES", Type: "elasticsearch"}
	index := map[string]model.OpsDataSource{}
	for _, ds := range []model.OpsDataSource{prom, loki, es} {
		index[ds.UID], index["name:"+ds.Name] = ds, ds
	}
	return index, prom
}

func parseDash(t *testing.T, raw string) map[string]any {
	t.Helper()
	var dash map[string]any
	if err := json.Unmarshal([]byte(raw), &dash); err != nil {
		t.Fatal(err)
	}
	return dash
}

func TestAnalyzeDashboardReportsUnsupportedParts(t *testing.T) {
	index, def := testIndex()
	dash := parseDash(t, `{"title":"x","panels":[
	 {"id":1,"type":"timeseries","title":"ok","targets":[{"refId":"A","expr":"up"}]},
	 {"id":2,"type":"stat","title":"transform","targets":[{"refId":"A","expr":"up"}],"transformations":[{"id":"organize"}]},
	 {"id":3,"type":"piechart","title":"pie","targets":[]},
	 {"id":4,"type":"row","title":"r","collapsed":true,"panels":[{"id":5,"type":"table","title":"es","datasource":{"type":"elasticsearch","uid":"es"},"targets":[{"refId":"A"}]}]}
	],"templating":{"list":[{"name":"q","type":"adhoc"}]},"annotations":{"list":[{"name":"deploys","enable":true}]}}`)
	report := AnalyzeDashboard(dash, index, def)
	status := map[any]string{}
	for _, p := range report.Panels {
		status[p.ID] = p.Status
	}
	if status[float64(1)] != "supported" || status[float64(2)] != "partial" || status[float64(3)] != "unsupported" || status[float64(5)] != "unsupported" {
		t.Fatalf("panel statuses = %v", status)
	}
	if report.Level != "limited" || report.Variables[0].Status != "unsupported" || len(report.Notes) != 1 {
		t.Fatalf("report = %+v", report)
	}
}

func TestResolveDataSourceThroughVariable(t *testing.T) {
	index, def := testIndex()
	vars := map[string]map[string]any{"ds": {"name": "ds", "type": "datasource", "query": "loki", "current": map[string]any{"value": "loki"}}}
	ds, err := resolveDataSource(map[string]any{"type": "loki", "uid": "${ds}"}, nil, index, def, vars)
	if err != nil || ds.UID != "loki" {
		t.Fatalf("ds = %+v err = %v", ds, err)
	}
	if _, err := resolveDataSource(map[string]any{"uid": "-- Mixed --"}, nil, index, def, nil); err == nil {
		t.Fatal("mixed panel data source needs per-target data sources")
	}
}

type fakeGrafana struct {
	ports.DashboardsBackend
	dash     map[string]any
	queries  []map[string]any
	resource []string
}

func (f *fakeGrafana) Configured() bool { return true }
func (f *fakeGrafana) DataSources(context.Context) ([]model.OpsDataSource, error) {
	index, _ := testIndex()
	out := []model.OpsDataSource{}
	for k, ds := range index {
		if !strings.HasPrefix(k, "name:") {
			out = append(out, ds)
		}
	}
	return out, nil
}
func (f *fakeGrafana) GetDashboard(context.Context, string) (map[string]any, map[string]any, error) {
	return deepCopy(f.dash), map[string]any{}, nil
}
func (f *fakeGrafana) QueryData(_ context.Context, q ports.PanelQueryRequest) (map[string][]model.DataFrame, map[string]string, error) {
	f.queries = append(f.queries, q.Queries...)
	return map[string][]model.DataFrame{"A": {{RefID: "A", Fields: []model.DataField{{Name: "Time", Type: "time", Values: []any{1.0}}}}}}, nil, nil
}
func (f *fakeGrafana) DataSourceResource(_ context.Context, uid, resource string, params map[string][]string) (json.RawMessage, error) {
	f.resource = append(f.resource, resource+"?"+url.Values(params).Encode())
	return json.RawMessage(`{"status":"success","data":["api","worker"]}`), nil
}

func TestPanelDataValidatesVariableSelectionsAndInterpolates(t *testing.T) {
	cacheMu.Lock()
	cache = map[string]cacheEntry{}
	cacheMu.Unlock()
	g := &fakeGrafana{dash: parseDash(t, `{"uid":"d1","title":"x","templating":{"list":[
	  {"name":"job","type":"query","datasource":{"type":"prometheus","uid":"prom"},"query":"label_values(up, job)","multi":true,"includeAll":true,"current":{"value":["$__all"]}}]},
	 "panels":[{"id":7,"type":"timeseries","targets":[{"refId":"A","expr":"sum(rate(x{job=~\"$job\"}[$__rate_interval]))","legendFormat":"{{job}}"}]}]}`)}
	svc := &Service{Dashboards: g, Limits: Limits{MaxMetricRange: 24 * time.Hour, MaxSeries: 10, MaxLogLines: 100}, Now: func() time.Time { return time.Unix(10000, 0) }}
	from, to := time.Unix(10000-3600, 0).UnixMilli(), time.Unix(10000, 0).UnixMilli()
	data, err := svc.PanelData(context.Background(), "d1", 7, PanelDataInput{From: from, To: to, Vars: map[string][]string{"job": {"api"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.queries) != 1 || g.queries[0]["expr"] != `sum(rate(x{job=~"api"}[1m]))` {
		t.Fatalf("queries = %+v", g.queries)
	}
	if len(data.Frames["A"]) != 1 || !strings.HasPrefix(g.resource[0], "api/v1/label/job/values?") {
		t.Fatalf("data = %+v resources = %v", data, g.resource)
	}
	_, err = svc.PanelData(context.Background(), "d1", 7, PanelDataInput{From: from, To: to, Vars: map[string][]string{"job": {`api"}) or vector(1`}}})
	var v *ValidationError
	if !asValidation(err, &v) {
		t.Fatalf("injected selection must be rejected, got %v", err)
	}
	g.queries = nil
	if _, err := svc.PanelData(context.Background(), "d1", 7, PanelDataInput{From: from, To: to, Vars: map[string][]string{"job": {"$__all"}}}); err != nil {
		t.Fatal(err)
	}
	if g.queries[0]["expr"] != `sum(rate(x{job=~"(api|worker)"}[1m]))` {
		t.Fatalf("all expr = %v", g.queries[0]["expr"])
	}
}

func asValidation(err error, target **ValidationError) bool {
	v, ok := err.(*ValidationError)
	if ok {
		*target = v
	}
	return ok
}

func TestReplaceInputsAndExternalExportRoundTrip(t *testing.T) {
	dash := parseDash(t, `{"title":"x","panels":[{"id":1,"type":"stat","datasource":{"type":"prometheus","uid":"${DS_PROM}"},"targets":[{"refId":"A","expr":"up","datasource":"${DS_PROM}"}]}]}`)
	out := replaceInputs(dash, map[string]string{"DS_PROM": "prom"}).(map[string]any)
	panel := obj(arr(out["panels"])[0])
	if obj(panel["datasource"])["uid"] != "prom" || obj(arr(panel["targets"])[0])["datasource"] != "prom" {
		t.Fatalf("panel = %+v", panel)
	}
}

type defaultDashboardGrafana struct {
	ports.DashboardsBackend
	dashboards map[string]map[string]any
	sources    []model.OpsDataSource
	getError   error
	saveError  error
	saves      int
}

func (f *defaultDashboardGrafana) Configured() bool { return true }
func (f *defaultDashboardGrafana) GetDashboard(_ context.Context, uid string) (map[string]any, map[string]any, error) {
	if f.getError != nil {
		return nil, nil, f.getError
	}
	if dash, ok := f.dashboards[uid]; ok {
		return deepCopy(dash), nil, nil
	}
	return nil, nil, ports.ErrOpsNotFound
}
func (f *defaultDashboardGrafana) DataSources(context.Context) ([]model.OpsDataSource, error) {
	return f.sources, nil
}
func (f *defaultDashboardGrafana) SearchDashboards(_ context.Context, q ports.DashboardSearch) ([]model.OpsDashboardSummary, error) {
	var hits []model.OpsDashboardSummary
	for _, uid := range q.UIDs {
		if dash, ok := f.dashboards[uid]; ok {
			hits = append(hits, model.OpsDashboardSummary{UID: uid, Title: strv(dash["title"])})
		}
	}
	return hits, nil
}
func (f *defaultDashboardGrafana) SaveDashboard(_ context.Context, dash map[string]any, _, _ string, overwrite bool) (ports.DashboardSaveResult, error) {
	f.saves++
	if overwrite {
		return ports.DashboardSaveResult{}, errors.New("defaults must be imported without overwriting existing dashboards")
	}
	uid := strv(dash["uid"])
	if f.saveError != nil {
		return ports.DashboardSaveResult{}, f.saveError
	}
	if _, ok := f.dashboards[uid]; ok {
		return ports.DashboardSaveResult{}, ports.ErrOpsConflict
	}
	f.dashboards[uid] = deepCopy(dash)
	return ports.DashboardSaveResult{UID: uid, Version: 1}, nil
}

func defaultDashboardService() (*Service, *defaultDashboardGrafana) {
	cacheMu.Lock()
	cache = map[string]cacheEntry{}
	cacheMu.Unlock()
	index, _ := testIndex()
	g := &defaultDashboardGrafana{
		dashboards: map[string]map[string]any{},
		sources:    []model.OpsDataSource{index["prom"], index["loki"]},
	}
	return &Service{Dashboards: g}, g
}

func TestDefaultDashboardsCreateAndPreserveEdits(t *testing.T) {
	s, g := defaultDashboardService()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	s.RunDefaultDashboards(ctx)
	if ctx.Err() != nil {
		t.Fatalf("initialization did not finish: %v", ctx.Err())
	}
	if len(g.dashboards) != len(templateCatalog) || g.saves != len(templateCatalog) {
		t.Fatalf("created %d dashboards with %d saves", len(g.dashboards), g.saves)
	}
	index, def := testIndex()
	for uid, dash := range g.dashboards {
		if report := AnalyzeDashboard(dash, index, def); report.Level != "full" {
			t.Fatalf("%s unsupported: %+v", uid, report)
		}
		dash["title"], dash["version"], dash["panels"] = "用户修改", float64(7), []any{}
	}
	before := canonicalJSON(g.dashboards)
	if err := s.ensureDefaultDashboards(ctx); err != nil {
		t.Fatal(err)
	}
	if g.saves != len(templateCatalog) || canonicalJSON(g.dashboards) != before {
		t.Fatal("repeated startup changed an existing dashboard")
	}
	delete(g.dashboards, "torchlink-host")
	if err := s.ensureDefaultDashboards(ctx); err != nil || g.saves != len(templateCatalog)+1 || len(g.dashboards) != len(templateCatalog) {
		t.Fatalf("missing dashboard was not restored: saves=%d err=%v", g.saves, err)
	}
}

func TestDefaultDashboardsRetryMissingDataSourceIndependently(t *testing.T) {
	s, g := defaultDashboardService()
	g.sources = g.sources[1:] // Loki is ready before Prometheus.
	if err := s.ensureDefaultDashboards(context.Background()); err == nil {
		t.Fatal("missing Prometheus should keep initialization pending")
	}
	if len(g.dashboards) != 1 || g.dashboards["torchlink-logs"] == nil {
		t.Fatal("missing Prometheus should not block the logs dashboard or create broken queries")
	}
	index, _ := testIndex()
	g.sources = append(g.sources, index["prom"])
	if err := s.ensureDefaultDashboards(context.Background()); err != nil || g.saves != len(templateCatalog) {
		t.Fatalf("retry did not complete all dashboards: saves=%d err=%v", g.saves, err)
	}
}

func TestDefaultDashboardsPropagateErrors(t *testing.T) {
	for _, tc := range []struct {
		name                string
		getError, saveError error
		wantSaves           int
	}{
		{"read unavailable", ports.ErrOpsUnavailable, nil, 0},
		{"write conflict", nil, ports.ErrOpsConflict, len(templateCatalog)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, g := defaultDashboardService()
			g.getError, g.saveError = tc.getError, tc.saveError
			err := s.ensureDefaultDashboards(context.Background())
			wantErr := tc.getError
			if wantErr == nil {
				wantErr = tc.saveError
			}
			if !errors.Is(err, wantErr) {
				t.Fatalf("upstream failure was lost: %v", err)
			}
			if g.saves != tc.wantSaves || len(g.dashboards) != 0 {
				t.Fatal("upstream failure changed dashboard state")
			}
		})
	}
}

func TestSelectorQuotesValuesSoTheyCannotEscapeTheMatcher(t *testing.T) {
	sel, err := Selector("up", []Matcher{{Name: "job", Op: "=", Value: `x"} or vector(1) or up{a="`}})
	if err != nil {
		t.Fatal(err)
	}
	want := `up{job="x\"} or vector(1) or up{a=\""}`
	if sel != want {
		t.Fatalf("selector = %s, want %s", sel, want)
	}
}

func TestSelectorRejectsInvalidNamesAndRegex(t *testing.T) {
	cases := []struct {
		metric string
		m      Matcher
	}{
		{"up", Matcher{Name: "job}", Op: "=", Value: "a"}},
		{"up) or (x", Matcher{Name: "job", Op: "=", Value: "a"}},
		{"up", Matcher{Name: "job", Op: "=~", Value: "(unclosed"}},
		{"up", Matcher{Name: "job", Op: "==", Value: "a"}},
	}
	for _, c := range cases {
		if _, err := Selector(c.metric, []Matcher{c.m}); err == nil {
			t.Fatalf("expected error for %+v", c)
		}
	}
}

func TestLogFilterBuildsEscapedLogQL(t *testing.T) {
	q, err := LogFilter{Services: []string{"platform-api", "backup.service"}, Levels: []string{"error"}, Keyword: `boom" | line_format "x`, Exclude: "health"}.LogQL()
	if err != nil {
		t.Fatal(err)
	}
	want := `{service_name=~"backup\\.service|platform-api",level="error"} |= "boom\" | line_format \"x" != "health"`
	if q != want {
		t.Fatalf("logql = %s\nwant    %s", q, want)
	}
	all, _ := LogFilter{}.LogQL()
	if all != `{service_name=~".+"}` {
		t.Fatalf("empty filter = %s", all)
	}
	if _, err := (LogFilter{Levels: []string{"verbose"}}).LogQL(); err == nil {
		t.Fatal("unknown level must be rejected")
	}
	if _, err := (LogFilter{Keyword: "(", Regex: true}).LogQL(); err == nil {
		t.Fatal("invalid regex keyword must be rejected")
	}
}

func TestExploreQueryWhitelist(t *testing.T) {
	q, err := ExploreQuery("http_requests_total", []Matcher{{Name: "code", Op: "=~", Value: "5.."}}, "sum_rate", "5m", "job,instance")
	if err != nil {
		t.Fatal(err)
	}
	if q != `sum by (job,instance)(rate(http_requests_total{code=~"5.."}[5m]))` {
		t.Fatalf("query = %s", q)
	}
	for _, bad := range [][3]string{{"up", "label_replace", "5m"}, {"up", "rate", "7m"}} {
		if _, err := ExploreQuery(bad[0], nil, bad[1], bad[2], ""); err == nil {
			t.Fatalf("expected rejection for %v", bad)
		}
	}
	if _, err := ExploreQuery("up", nil, "sum", "5m", "job) or (x"); err == nil {
		t.Fatal("invalid grouping label must be rejected")
	}
}

func TestStreamSelectorSortsAndQuotes(t *testing.T) {
	sel, err := StreamSelector(map[string]string{"service_name": "api", "level": `in"fo`})
	if err != nil {
		t.Fatal(err)
	}
	if sel != `{level="in\"fo",service_name="api"}` {
		t.Fatalf("selector = %s", sel)
	}
}

func TestTimeRangeLimits(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, _, err := TimeRange(now.Add(-48*time.Hour), now, 24*time.Hour, now); err == nil {
		t.Fatal("range above maximum must be rejected")
	}
	var v *ValidationError
	if _, _, err := TimeRange(now, now.Add(-time.Hour), 24*time.Hour, now); !errors.As(err, &v) {
		t.Fatal("reversed range must be a validation error")
	}
	start, end, err := TimeRange(time.Time{}, time.Time{}, 24*time.Hour, now)
	if err != nil || !end.Equal(now) || end.Sub(start) != time.Hour {
		t.Fatalf("default range = %v %v %v", start, end, err)
	}
}

func TestStepStaysWithinPointLimit(t *testing.T) {
	start := time.Unix(0, 0)
	end := start.Add(31 * 24 * time.Hour)
	step := Step(start, end, time.Second, 20000)
	if points := end.Sub(start) / step; points > 11000 {
		t.Fatalf("step %v yields %d points", step, points)
	}
}

func TestCheckQueryText(t *testing.T) {
	if err := checkQueryText("q", strings.Repeat("a", maxQueryLength+1)); err == nil {
		t.Fatal("oversized query must be rejected")
	}
	if err := checkQueryText("q", "  "); err == nil {
		t.Fatal("empty query must be rejected")
	}
}

func TestParseSelectorRoundTrip(t *testing.T) {
	want := []Matcher{{Name: "service_name", Op: "=", Value: `a"b}`}, {Name: "level", Op: "=~", Value: "error|warn"}, {Name: "env", Op: "!=", Value: "dev"}}
	selector, err := Selector("", want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseSelector(selector)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseSelector(%s) = %v, %v", selector, got, err)
	}
	if got, err := ParseSelector("{ app = `x` , job!~\"y.*\" }"); err != nil || len(got) != 2 || got[0].Value != "x" || got[1].Op != "!~" {
		t.Fatalf("spaced selector = %v, %v", got, err)
	}
	for _, bad := range []string{`app="x"`, `{app="x"} or {}`, `{app='x'}`, `{1app="x"}`, `{app="x" job="y"}`, `{app=~"("}`, `{app="x"`} {
		if _, err := ParseSelector(bad); err == nil {
			t.Errorf("ParseSelector(%q) should fail", bad)
		}
	}
}

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
