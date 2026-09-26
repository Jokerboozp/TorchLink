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
	"iot-platform/internal/aitest"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
)

func TestHealthInspectionJobCanBeLoadedWithoutJobID(t *testing.T) {
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseJob := func() { releaseOnce.Do(func() { close(release) }) }
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	engine.AIWorkflows, engine.HarnessTokens = &aitest.Workflows{Answer: func(ports.AIWorkflowRequest) (string, error) { <-release; return "巡检建议已生成", nil }}, aitest.Tokens()
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

// Inspection progress and results live in the repository, so a restarted
// process or another replica sees the same job and the running-job guard.
func TestHealthInspectionJobIsSharedAcrossServerInstances(t *testing.T) {
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseJob := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseJob()
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	engine.AIWorkflows, engine.HarnessTokens = &aitest.Workflows{Answer: func(ports.AIWorkflowRequest) (string, error) { <-release; return "巡检建议已生成", nil }}, aitest.Tokens()
	first := New(config.Config{DevMode: true}, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	second := New(config.Config{DevMode: true}, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	firstServer, secondServer := newTestHTTPServer(first), newTestHTTPServer(second)
	defer firstServer.Close()
	defer secondServer.Close()
	token, err := first.auth.Issue("viewer", "tenant-a", "viewer", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	started := requestJSON(t, firstServer.Client(), http.MethodPost, firstServer.URL+"/api/v1/ai/health-inspection/run", token, map[string]any{}, http.StatusAccepted)
	again := requestJSON(t, secondServer.Client(), http.MethodPost, secondServer.URL+"/api/v1/ai/health-inspection/run", token, map[string]any{}, http.StatusAccepted)
	if again["jobId"] != started["jobId"] {
		t.Fatalf("second instance started a duplicate inspection: first=%v second=%v", started["jobId"], again["jobId"])
	}
	releaseJob()
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		progress := requestJSON(t, secondServer.Client(), http.MethodGet, secondServer.URL+"/api/v1/ai/health-inspection/progress/"+started["jobId"].(string), token, nil, http.StatusOK)
		if progress["status"] == "succeeded" && progress["report"] != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("other instance did not observe the finished job: %v", progress)
		}
	}
	if _, ok := second.recentHealthInspection(context.Background(), "tenant-a"); !ok {
		t.Fatal("PDF download on another instance cannot reuse the finished report")
	}
}

func TestHealthInspectionStaleRunningJobIsMarkedInterrupted(t *testing.T) {
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	api := New(config.Config{DevMode: true}, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := newTestHTTPServer(api)
	defer server.Close()
	token, err := api.auth.Issue("viewer", "tenant-a", "viewer", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	// A job left running by a process that stopped heartbeating.
	old := time.Now().Add(-2 * healthInspectionStaleAfter).UnixMilli()
	if _, err = repo.CreateHealthInspectionJob(context.Background(), model.HealthInspectionJob{ID: "inspection_job_orphan", TenantID: "tenant-a", Status: "running", Progress: 40, StartedAt: old, UpdatedAt: old}); err != nil {
		t.Fatal(err)
	}
	progress := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/ai/health-inspection/progress", token, nil, http.StatusOK)
	if progress["jobId"] != "inspection_job_orphan" || progress["status"] != "failed" || progress["error"] == nil {
		t.Fatalf("orphaned job must be reported as interrupted: %v", progress)
	}
	started := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/health-inspection/run", token, map[string]any{}, http.StatusAccepted)
	if started["jobId"] == "inspection_job_orphan" || started["status"] != "running" {
		t.Fatalf("a new inspection must start after the interrupted one: %v", started)
	}
}
