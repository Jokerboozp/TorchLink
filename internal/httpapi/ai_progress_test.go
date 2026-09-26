package httpapi

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"iot-platform/internal/adapters/knowledge"
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

func TestAIAnalysisJobReportsProgressAndPersistsResult(t *testing.T) {
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	engine.AIWorkflows, engine.HarnessTokens = &aitest.Workflows{Answer: func(ports.AIWorkflowRequest) (string, error) { <-release; return testAnalysisAnswer, nil }}, aitest.Tokens()
	api := New(config.Config{DevMode: true}, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, _, err = repo.UpsertAlarm(context.Background(), model.Alarm{ID: "alarm-progress", TenantID: "tenant-a", DeviceID: "device-a", AlarmType: "SMOKE_DETECTED", AlarmLevel: "HIGH", Status: "ACTIVE", LastTriggeredAt: time.Now().UnixMilli()})
	if err != nil {
		t.Fatal(err)
	}
	job, err := api.startAIAnalysisJob(context.Background(), "tenant-a", "alarm-progress", "operator", model.AIAnalysisScopeNone, testRunIdentity("operator"))
	if err != nil || job.Status != "running" || job.Progress != 8 || job.EstimatedRemainingMs <= 0 {
		t.Fatalf("unexpected initial progress: %#v", aiAnalysisJobView(job))
	}
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		// A second API instance on the same store stands in for a restart or replica.
		current, found, loadErr := New(config.Config{DevMode: true}, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil))).loadAIAnalysisJob(context.Background(), "tenant-a", "alarm-progress", model.AIAnalysisScopeNone)
		if loadErr != nil || !found {
			t.Fatalf("stored job not readable: found=%v err=%v", found, loadErr)
		}
		if current.Status != "running" {
			if current.Status != "succeeded" || current.Progress != 100 || current.Analysis.Summary != "研判完成" {
				t.Fatalf("unexpected completed progress: %#v", aiAnalysisJobView(current))
			}
			if saved, getErr := repo.GetAIAnalysis(context.Background(), "tenant-a", "alarm-progress", model.AIAnalysisScopeNone); getErr != nil || saved.Summary != "研判完成" {
				t.Fatalf("analysis was not persisted: %#v err=%v", saved, getErr)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("AI analysis job did not complete")
}

func TestAIAnalysisProgressCanBeLoadedWithoutJobID(t *testing.T) {
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	engine.KB = knowledge.NewLocal()
	engine.AIWorkflows, engine.HarnessTokens = &aitest.Workflows{Answer: func(ports.AIWorkflowRequest) (string, error) { <-release; return testAnalysisAnswer, nil }}, aitest.Tokens()
	api := New(config.Config{DevMode: true}, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, _, err = repo.UpsertAlarm(context.Background(), model.Alarm{ID: "alarm-progress-resume", TenantID: "tenant-a", DeviceID: "device-a", AlarmType: "SMOKE_DETECTED", AlarmLevel: "HIGH", Status: "ACTIVE", LastTriggeredAt: time.Now().UnixMilli()})
	if err != nil {
		t.Fatal(err)
	}
	job, err := api.startAIAnalysisJob(context.Background(), "tenant-a", "alarm-progress-resume", "operator", model.AlarmAnalysisWorkflowID, testRunIdentity("operator")) // 未托管的测试令牌拥有知识库权限，进度按同一知识范围查询。
	if err != nil {
		t.Fatal(err)
	}
	server := newTestHTTPServer(api)
	defer server.Close()
	defer close(release)
	viewerToken, err := api.auth.Issue("viewer", "tenant-a", "viewer", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	progress := requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/ai/alarm-analysis/alarm-progress-resume/progress", viewerToken, nil, 200)
	if progress["jobId"] != job.ID || progress["status"] != "running" {
		t.Fatalf("unexpected resumable progress: %#v", progress)
	}
}

// A running job whose process stopped heartbeating is reported as interrupted,
// and a new run can start for that alarm.
func TestStaleAIAnalysisJobIsMarkedInterrupted(t *testing.T) {
	repo := memory.NewRepository()
	engine := &core.Engine{Repo: repo, Clock: ports.RealClock{}, Bus: local.NewBus(), Realtime: local.NewRealtime()}
	api := New(config.Config{DevMode: true}, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	old := time.Now().Add(-time.Minute).UnixMilli()
	stale := model.AlarmAnalysisJob{ID: "ai_job_stale", TenantID: "tenant-a", AlarmID: "alarm-stale", Status: "running", Stage: "calling_model", StartedAt: old, UpdatedAt: old}
	if created, err := repo.CreateAlarmAnalysisJob(context.Background(), stale); err != nil || !created {
		t.Fatalf("seed job: %v %v", created, err)
	}
	job, found, err := api.loadAIAnalysisJob(context.Background(), "tenant-a", "alarm-stale", model.AIAnalysisScopeNone)
	if err != nil || !found || job.Status != "failed" || job.Error == "" {
		t.Fatalf("stale job must be interrupted: %#v found=%v err=%v", job, found, err)
	}
	if created, err := repo.CreateAlarmAnalysisJob(context.Background(), model.AlarmAnalysisJob{ID: "ai_job_new", TenantID: "tenant-a", AlarmID: "alarm-stale", Status: "running"}); err != nil || !created {
		t.Fatalf("a new run must be allowed after the interruption: %v %v", created, err)
	}
}
