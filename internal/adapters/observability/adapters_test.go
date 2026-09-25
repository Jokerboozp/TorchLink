package observability

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"iot-platform/internal/ports"
)

func TestPrometheusQueryParsesValuesAndTruncates(t *testing.T) {
	var form string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		form = string(body)
		_, _ = w.Write([]byte(`{"status":"success","warnings":["w"],"data":{"resultType":"matrix","result":[
		 {"metric":{"job":"a"},"values":[[1.5,"1"],[3,"NaN"]]},
		 {"metric":{"job":"b"},"values":[[1.5,"+Inf"]]},
		 {"metric":{"job":"c"},"values":[[1.5,"2"]]}]}}`))
	}))
	defer srv.Close()
	p := NewPrometheus(srv.URL, time.Second)
	res, err := p.Query(context.Background(), ports.MetricQuery{Expr: "up", Start: time.Unix(0, 0), End: time.Unix(60, 0), Step: 15 * time.Second, Limit: 2, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Truncated || len(res.Series) != 2 || res.Series[0].Timestamps[0] != 1500 || *res.Series[0].Values[0] != 1 || res.Series[0].Values[1] != nil || res.Series[1].Values[0] != nil {
		t.Fatalf("result = %+v", res)
	}
	if !strings.Contains(form, "limit=3") || !strings.Contains(form, "step=15") || res.Warnings[0] != "w" {
		t.Fatalf("form = %s", form)
	}
}

func TestPrometheusErrorsAreMapped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"status":"error","errorType":"bad_data","error":"1:5: parse error: unexpected"}`))
	}))
	p := NewPrometheus(srv.URL, time.Second)
	_, err := p.FormatQuery(context.Background(), "sum((")
	var upstream *ports.OpsUpstreamError
	if !errors.As(err, &upstream) || upstream.Status != 400 || !strings.Contains(upstream.Message, "parse error") || strings.Contains(upstream.Message, srv.URL) {
		t.Fatalf("err = %#v", err)
	}
	srv.Close()
	if _, err := p.FormatQuery(context.Background(), "up"); !errors.Is(err, ports.ErrOpsUnavailable) {
		t.Fatalf("closed server must be unavailable, got %v", err)
	}
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(300 * time.Millisecond) }))
	defer slow.Close()
	if _, err := NewPrometheus(slow.URL, 50*time.Millisecond).Targets(context.Background()); !errors.Is(err, ports.ErrOpsTimeout) {
		t.Fatalf("slow server must time out, got %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewPrometheus(slow.URL, time.Second).Targets(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled request must report cancellation, got %v", err)
	}
	if _, err := NewPrometheus("", time.Second).Targets(context.Background()); !errors.Is(err, ports.ErrOpsNotConfigured) {
		t.Fatalf("unconfigured backend, got %v", err)
	}
}

func TestLokiQueryUsesCategorizedLabelsAndCursor(t *testing.T) {
	var header string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header = r.Header.Get("X-Loki-Response-Encoding-Flags")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"streams","encodingFlags":["categorize-labels"],"result":[
		 {"stream":{"service_name":"api"},"values":[["300","c",{"structuredMetadata":{"detected_level":"info"},"parsed":{"msg":"c"}}],["100","a"]]},
		 {"stream":{"service_name":"db"},"values":[["200","b"]]}],"stats":{"summary":{"execTime":0.01,"totalLinesProcessed":3}}}}`))
	}))
	defer srv.Close()
	l := NewLoki(srv.URL, "", time.Second)
	res, err := l.Query(context.Background(), ports.LogQuery{Query: `{service_name=~".+"}`, Start: time.Unix(0, 0), End: time.Unix(1, 0), Limit: 3, Direction: "backward"})
	if err != nil {
		t.Fatal(err)
	}
	if header != "categorize-labels" || len(res.Entries) != 3 || res.Entries[0].Line != "c" || res.Entries[0].Metadata["detected_level"] != "info" || res.Entries[0].Labels["service_name"] != "api" {
		t.Fatalf("entries = %+v", res.Entries)
	}
	if !res.Truncated || res.NextCursor != "101" || res.Stats["execTime"] != 0.01 {
		t.Fatalf("cursor = %s truncated = %v stats = %v", res.NextCursor, res.Truncated, res.Stats)
	}
}

func TestLokiRuntimeStateFromMetrics(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("# HELP x\nloki_runtime_config_hash{config=\"loki\",sha256=\"abc\"} 1\nloki_runtime_config_last_reload_successful{config=\"loki\"} 0\n"))
	}))
	defer srv.Close()
	state, err := NewLoki(srv.URL, "", time.Second).RuntimeState(context.Background())
	if err != nil || state.Hash != "abc" || state.Success {
		t.Fatalf("state = %+v err = %v", state, err)
	}
}

func TestGrafanaQueryDataAndConflicts(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/api/ds/query":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"results":{"A":{"status":200,"frames":[{"schema":{"refId":"A","fields":[{"name":"Time","type":"time"},{"name":"up","type":"number","labels":{"job":"x"},"config":{"displayNameFromDS":"x","unit":"s","secret":"y"}}]},"data":{"values":[[1000],[1]]}}]},"B":{"status":400,"error":"parse error"}}}`))
		case "/api/dashboards/db":
			w.WriteHeader(http.StatusPreconditionFailed)
			_, _ = w.Write([]byte(`{"message":"The dashboard has been changed by someone else","status":"version-mismatch"}`))
		case "/api/datasources/uid/p/health":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"status":"ERROR","message":"connection refused"}`))
		}
	}))
	defer srv.Close()
	g := NewGrafana(srv.URL, "sa-token", "", "", time.Second)
	frames, errs, err := g.QueryData(context.Background(), ports.PanelQueryRequest{From: time.Unix(0, 0), To: time.Unix(60, 0), Queries: []map[string]any{{"refId": "A"}, {"refId": "B"}}})
	if err != nil || errs["B"] != "parse error" || len(frames["A"]) != 1 || frames["A"][0].Fields[1].Labels["job"] != "x" {
		t.Fatalf("frames = %+v errs = %v err = %v", frames, errs, err)
	}
	if _, leaked := frames["A"][0].Fields[1].Config["secret"]; leaked || auth != "Bearer sa-token" {
		t.Fatalf("config not filtered or auth missing: %+v %s", frames["A"][0].Fields[1].Config, auth)
	}
	if _, err := g.SaveDashboard(context.Background(), map[string]any{"title": "x"}, "", "", false); !errors.Is(err, ports.ErrOpsConflict) {
		t.Fatalf("412 must map to conflict, got %v", err)
	}
	status, message, err := g.TestDataSource(context.Background(), "p")
	if err != nil || status != "ERROR" || message != "connection refused" {
		t.Fatalf("health = %s %s %v", status, message, err)
	}
}

func TestAlertmanagerReloadErrorAndGeneratorURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/-/reload":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("failed to reload config: undefined receiver \"x\" used in route"))
		case "/api/v2/alerts":
			_, _ = w.Write([]byte(`[{"fingerprint":"f","labels":{"alertname":"A"},"annotations":{},"startsAt":"2026-01-01T00:00:00Z","status":{"state":"active"},"receivers":[{"name":"ops"}],"generatorURL":"http://prometheus-internal:9090/graph?g0.expr=up+%3D%3D+0&g0.tab=1"}]`))
		}
	}))
	defer srv.Close()
	a := NewAlertmanager(srv.URL, time.Second)
	err := a.Reload(context.Background())
	var upstream *ports.OpsUpstreamError
	if !errors.As(err, &upstream) || !strings.Contains(upstream.Message, "undefined receiver") {
		t.Fatalf("reload err = %v", err)
	}
	alerts, err := a.Alerts(context.Background(), ports.AlertFilter{Active: true})
	if err != nil || alerts[0].Expr != "up == 0" || alerts[0].Receivers[0] != "ops" {
		t.Fatalf("alerts = %+v err = %v", alerts, err)
	}
	b, _ := json.Marshal(alerts[0])
	if strings.Contains(string(b), "prometheus-internal") {
		t.Fatal("internal generator URL must not be exposed")
	}
}

func TestDirStoreMovesBetweenEnabledAndDisabledAndArchives(t *testing.T) {
	dir := t.TempDir()
	s := NewDirStore(dir+"/active", dir+"/state", ".yml", 0o644)
	if err := s.Write("g-1", []byte("a"), true); err != nil {
		t.Fatal(err)
	}
	if err := s.Write("g-1", []byte("b"), false); err != nil {
		t.Fatal(err)
	}
	files, _ := s.List()
	if len(files) != 1 || files[0].Enabled || string(files[0].Content) != "b" {
		t.Fatalf("files = %+v", files)
	}
	if err := s.Write("../x", []byte("x"), true); err == nil {
		t.Fatal("path traversal must be rejected")
	}
	if err := s.Remove("g-1"); err != nil {
		t.Fatal(err)
	}
	history, _ := filepathGlob(dir + "/state/history/g-1/*.yml")
	if len(history) < 2 {
		t.Fatalf("previous versions must be archived, got %v", history)
	}
}

func TestLokiPushBatchesByLevel(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	p := NewLokiPush(srv.URL, "", "platform-api")
	_, _ = p.Write([]byte(`{"time":"x","level":"ERROR","msg":"boom"}` + "\n"))
	_, _ = p.Write([]byte(`{"time":"x","level":"INFO","msg":"ok"}` + "\n"))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	p.Close(ctx)
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 1 || !strings.Contains(bodies[0], `"level":"error"`) || !strings.Contains(bodies[0], `"service_name":"platform-api"`) || !strings.Contains(bodies[0], `boom`) {
		t.Fatalf("pushed = %v", bodies)
	}
}

func filepathGlob(pattern string) ([]string, error) { return filepath.Glob(pattern) }
