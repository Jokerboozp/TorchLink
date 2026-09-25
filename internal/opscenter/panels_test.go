package opscenter

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

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
