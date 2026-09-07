package httpapi

import (
	"context"
	"io"
	"log/slog"
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

type progressTestAI struct {
	release <-chan struct{}
}

func (p progressTestAI) AnalyzeAlarm(_ context.Context, alarm model.Alarm, _ []map[string]any, _ []string) (model.AIAnalysis, error) {
	<-p.release
	return model.AIAnalysis{AlarmID: alarm.ID, Summary: "研判完成", RiskLevel: alarm.AlarmLevel, Confidence: .9, Model: "qwen3:1.7b"}, nil
}

func (progressTestAI) Chat(context.Context, string, string) (string, error) { return "", nil }
func (progressTestAI) RuleDraft(context.Context, string, string) (model.AlarmRule, error) {
	return model.AlarmRule{}, nil
}
func (progressTestAI) Health(context.Context) error { return nil }

func TestAIAnalysisJobReportsProgressAndPersistsResult(t *testing.T) {
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	engine.AI = progressTestAI{release: release}
	api := New(config.Config{DevMode: true}, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, _, err = repo.UpsertAlarm(context.Background(), model.Alarm{ID: "alarm-progress", TenantID: "tenant-a", DeviceID: "device-a", AlarmType: "SMOKE_DETECTED", AlarmLevel: "HIGH", Status: "ACTIVE", LastTriggeredAt: time.Now().UnixMilli()})
	if err != nil {
		t.Fatal(err)
	}
	job := api.startAIAnalysisJob("tenant-a", "alarm-progress", "operator")
	if job.Status != "running" || job.Progress != 8 || job.EstimatedRemainingMs <= 0 {
		t.Fatalf("unexpected initial progress: %#v", aiAnalysisJobView(job))
	}
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		api.aiAnalysisMu.RLock()
		current := cloneAIAnalysisJob(api.aiAnalysisJobs[alarmJobKey("tenant-a", "alarm-progress")])
		api.aiAnalysisMu.RUnlock()
		if current != nil && current.Status != "running" {
			if current.Status != "succeeded" || current.Progress != 100 || current.Analysis.Summary != "研判完成" {
				t.Fatalf("unexpected completed progress: %#v", aiAnalysisJobView(current))
			}
			if saved, getErr := repo.GetAIAnalysis(context.Background(), "tenant-a", "alarm-progress"); getErr != nil || saved.Summary != "研判完成" {
				t.Fatalf("analysis was not persisted: %#v err=%v", saved, getErr)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("AI analysis job did not complete")
}
