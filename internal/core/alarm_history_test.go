package core

import (
	"context"
	"testing"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/model"
)

type countingHistoryRepo struct {
	*memory.Repository
	calls int
	rows  map[string][]map[string]any
}

func (r *countingHistoryRepo) PropertyHistory(_ context.Context, _, _, property string, _, _ int64, _ int) ([]map[string]any, error) {
	r.calls++
	return r.rows[property], nil
}

func TestAlarmPropertyHistoryQueriesOncePerPropertyAndCondenses(t *testing.T) {
	end := int64(100 * alarmHistoryDayMs)
	temperature := []map[string]any{}
	for i := 0; i < 120; i++ { // one point per minute for two hours, newest first like the stores
		temperature = append(temperature, map[string]any{"timestamp": end - int64(i)*60*1000, "value": float64(20 + i%10)})
	}
	smoke := []map[string]any{{"timestamp": end, "value": true}, {"timestamp": end - 1000, "value": false}, {"timestamp": end - 2000, "value": true}}
	repo := &countingHistoryRepo{Repository: memory.NewRepository(), rows: map[string][]map[string]any{"temperature": temperature, "smoke": smoke}}
	e := &Engine{Repo: repo}

	history := e.alarmPropertyHistory(context.Background(), model.Alarm{TenantID: "t1", DeviceID: "d1", LastTriggeredAt: end})
	if repo.calls != len(alarmHistoryProperties) {
		t.Fatalf("expected one query per property, got %d", repo.calls)
	}
	if len(history) != 2 {
		t.Fatalf("only properties with data are included, got %d", len(history))
	}
	temp := history[0]
	if recent := temp["recent10m"].([]map[string]any); len(recent) != 11 || recent[len(recent)-1]["timestamp"] != end {
		t.Fatalf("recent points must cover the last 10 minutes in time order: %v", recent)
	}
	stats := temp["stats1h"].(map[string]any)
	if stats["count"] != 61 || stats["min"] != 20.0 || stats["max"] != 29.0 {
		t.Fatalf("unexpected 1h stats: %v", stats)
	}
	if day := temp["stats24h"].(map[string]any); day["count"] != 120 {
		t.Fatalf("unexpected 24h stats: %v", day)
	}
	smokeStats := history[1]["stats24h"].(map[string]any)["valueCounts"].(map[string]int)
	if smokeStats["true"] != 2 || smokeStats["false"] != 1 {
		t.Fatalf("non-numeric values must be counted: %v", smokeStats)
	}

	many := []map[string]any{}
	for i := 0; i < alarmHistoryLimit; i++ {
		many = append(many, map[string]any{"timestamp": end - int64(i)*1000, "value": 1.0})
	}
	summary := summarizePropertyHistory("temperature", many, end)
	if len(summary["recent10m"].([]map[string]any)) != alarmHistoryRecentLimit || summary["truncated"] == nil {
		t.Fatalf("dense history must stay bounded and report truncation: %v", summary["truncated"])
	}
}

func TestInspectionPromptSnapshotIsBounded(t *testing.T) {
	items := []model.DeviceHealthItem{}
	for i := 0; i < inspectionPromptLimit+15; i++ {
		items = append(items, model.DeviceHealthItem{DeviceID: "bad", Severity: "HIGH"})
	}
	for i := 0; i < 500; i++ {
		items = append(items, model.DeviceHealthItem{DeviceID: "ok", Severity: "INFO"})
	}
	snapshot := inspectionPromptSnapshot(1, map[string]int{"total": len(items)}, items)
	if listed := snapshot["devicesNeedingAttention"].([]model.DeviceHealthItem); len(listed) != inspectionPromptLimit {
		t.Fatalf("prompt lists %d devices, want %d", len(listed), inspectionPromptLimit)
	}
	if snapshot["omittedAttentionDevices"] != 15 {
		t.Fatalf("omitted devices must be reported: %v", snapshot["omittedAttentionDevices"])
	}
}
