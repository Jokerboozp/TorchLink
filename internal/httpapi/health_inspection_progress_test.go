package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"testing"
	"time"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
)

type progressInspectionAI struct {
	release <-chan struct{}
}

func (p progressInspectionAI) AnalyzeAlarm(context.Context, model.Alarm, []map[string]any, []string) (model.AIAnalysis, error) {
	return model.AIAnalysis{}, nil
}

func (p progressInspectionAI) Chat(context.Context, string, string) (string, error) {
	<-p.release
	return "巡检建议已生成", nil
}

func (progressInspectionAI) RuleDraft(context.Context, string, string) (model.AlarmRule, error) {
	return model.AlarmRule{}, nil
}

func (progressInspectionAI) Health(context.Context) error { return nil }

func TestHealthInspectionJobCanBeLoadedWithoutJobID(t *testing.T) {
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseJob := func() { releaseOnce.Do(func() { close(release) }) }
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	engine.AI = progressInspectionAI{release: release}
	api := New(config.Config{DevMode: true}, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := newTestHTTPServer(api)
	defer server.Close()
	defer releaseJob()
	token, err := api.auth.Issue("viewer", "tenant-a", "viewer", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	started := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/health-inspection/run", token, map[string]any{}, http.StatusAccepted)
	if started["status"] != "running" || started["progress"] != float64(8) {
		t.Fatalf("unexpected initial inspection progress: %#v", started)
	}
	progress := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/ai/health-inspection/progress", token, nil, http.StatusOK)
	if progress["jobId"] != started["jobId"] || progress["status"] != "running" {
		t.Fatalf("unexpected resumable inspection progress: %#v", progress)
	}
	releaseJob()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		progress = requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/ai/health-inspection/progress", token, nil, http.StatusOK)
		if progress["status"] == "succeeded" {
			if progress["progress"] != float64(100) {
				t.Fatalf("unexpected completed inspection progress: %#v", progress)
			}
			if progress["report"] == nil {
				t.Fatalf("completed inspection did not include report: %#v", progress)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("health inspection job did not complete")
}
