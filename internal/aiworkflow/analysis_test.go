package aiworkflow

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"iot-platform/internal/core"
	"log/slog"
	"testing"
	"time"

	"iot-platform/internal/adapters/knowledge"
	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/aitest"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
)

func TestRawToAlarmPipeline(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bus := local.NewBus()
	realtime := local.NewRealtime()
	e := wrap(core.New(repo, archive, bus, realtime, parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil))))
	e.AIWorkflows = &aitest.Workflows{Answer: func(ports.AIWorkflowRequest) (string, error) { return analysisAnswer, nil }}
	e.HarnessTokens = aitest.Tokens()
	e.KB = knowledge.NewLocal()
	if err = e.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveManagedDevice(ctx, model.ManagedDevice{ID: "device_1", TenantID: "t1", ProductID: "json_sensor", Name: "一号烟感", Status: "ENABLED", AccessKey: "device-1-key"}); err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveVideoCameraMapping(ctx, model.VideoCameraMapping{TenantID: "t1", CameraID: "camera-001", CameraName: "一号摄像头", Brand: "海康", CameraPoint: "东侧入口", DeviceID: "device_1", Building: "A", Floor: "1", Room: "大厅", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	rule := model.AlarmRule{ID: "r1", TenantID: "t1", Name: "高温烟雾", AlarmType: "FIRE", Level: "HIGH", Enabled: true, Conditions: []model.RuleCondition{{Field: "temperature", Operator: ">", Value: 80}, {Field: "smoke", Operator: "eq", Value: true}}, Actions: []model.RuleAction{{Type: "OPEN_CAMERA", CameraID: "camera-001"}}}
	if err = repo.SaveRule(ctx, rule); err != nil {
		t.Fatal(err)
	}
	raw := model.RawMessage{MessageID: "raw_1", TenantID: "t1", ProductID: "json_sensor", DeviceID: "device_1", Protocol: "json", PayloadFormat: "json", ReceivedAt: time.Now().UnixMilli(), Payload: json.RawMessage(`{"properties":{"temperature":90,"smoke":true},"tags":{"cityCode":"city","districtCode":"d","buildingId":"b","deviceType":"smoke"}}`)}
	if _, created, err := e.IngestRaw(ctx, raw); err != nil || !created {
		t.Fatalf("ingest created=%v err=%v", created, err)
	}
	alarms, err := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "t1", Status: "ACTIVE"})
	if err != nil || len(alarms) != 1 {
		t.Fatalf("alarms=%v err=%v", alarms, err)
	}
	if alarms[0].TriggerCount != 1 {
		t.Fatalf("unexpected alarm %#v", alarms[0])
	}
	if alarms[0].DeviceName != "一号烟感" {
		t.Fatalf("alarm did not preserve the managed device name: %#v", alarms[0])
	}
	if len(alarms[0].Cameras) != 1 || alarms[0].Cameras[0].CameraID != "camera-001" || alarms[0].Cameras[0].Brand != "海康" {
		t.Fatalf("alarm did not resolve associated camera metadata: %#v", alarms[0].Cameras)
	}
	state, err := repo.GetDeviceState(ctx, "t1", "device_1")
	if err != nil || state.BusinessStatus != "ALARM" {
		t.Fatalf("matching alarm did not update device business status: state=%#v err=%v", state, err)
	}
	if _, created, err := e.IngestRaw(ctx, raw); err != nil || created {
		t.Fatalf("duplicate created=%v err=%v", created, err)
	}
	if _, err = repo.GetAIAnalysis(ctx, alarms[0].TenantID, alarms[0].ID, model.AIAnalysisScopeNone); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("alarm ingestion must not create an analysis: %v", err)
	}
	if requests := e.AIWorkflows.(*aitest.Workflows).Requests(); len(requests) != 0 {
		t.Fatalf("alarm ingestion must not call a model: %#v", requests)
	}
	if _, err = e.AnalyzeAlarm(aitest.Context(ctx), alarms[0].TenantID, alarms[0].ID, false); err != nil {
		t.Fatal(err)
	}
	if saved, err := repo.GetAIAnalysis(ctx, alarms[0].TenantID, alarms[0].ID, model.AIAnalysisScopeNone); err != nil || saved.Status != "succeeded" {
		t.Fatalf("manual analysis not saved: %#v err=%v", saved, err)
	}
	if len(realtime.Messages) < 2 {
		t.Fatalf("expected state and alarm realtime messages, got %d", len(realtime.Messages))
	}
	foundAction := false
	for _, published := range realtime.Messages {
		if published.Topic != "/iot/ui-action/t1" {
			continue
		}
		var event model.UIActionEvent
		if err = json.Unmarshal(published.Payload, &event); err != nil {
			t.Fatal(err)
		}
		foundAction = event.RuleID == "r1" && event.Action.Type == "OPEN_CAMERA" && event.Action.CameraID == "camera-001"
	}
	if !foundAction {
		t.Fatalf("expected validated UI action event, messages=%#v", realtime.Messages)
	}
}

func TestAnalyzeAlarmPersistsReadableFallbackOnProviderError(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := wrap(core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil))))
	e.AIWorkflows = &aitest.Workflows{Answer: func(ports.AIWorkflowRequest) (string, error) { return "", errors.New("provider response invalid") }}
	e.HarnessTokens = aitest.Tokens()
	if _, _, err = repo.UpsertAlarm(ctx, model.Alarm{ID: "alarm-ai-failure", TenantID: "t1", DeviceID: "device-1", AlarmType: "FIRE", AlarmLevel: "HIGH", Status: "ACTIVE", LastTriggeredAt: time.Now().UnixMilli()}); err != nil {
		t.Fatal(err)
	}

	analysis, err := e.AnalyzeAlarm(aitest.Context(ctx), "t1", "alarm-ai-failure", false)
	if err != nil {
		t.Fatal(err)
	}
	if analysis.Summary != "AI 研判暂时失败，已保留告警供人工研判。" || analysis.Model != "unavailable" || analysis.Error == "" {
		t.Fatalf("unexpected readable fallback: %#v", analysis)
	}
	saved, err := repo.GetAIAnalysis(ctx, "t1", "alarm-ai-failure", model.AIAnalysisScopeNone)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Summary == "" || saved.Model == "" {
		t.Fatalf("saved fallback is not renderable: %#v", saved)
	}
}
