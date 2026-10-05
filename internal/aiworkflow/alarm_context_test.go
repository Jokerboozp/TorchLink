package aiworkflow

import (
	"context"
	"encoding/json"
	"fmt"
	"iot-platform/internal/adapters/memory"
	"strings"
	"testing"

	"iot-platform/internal/model"
)

// alarmContextFixture places three devices on one floor and one in another
// building, with a rule, verified history, cameras and a video event.
func alarmContextFixture(t *testing.T) (*testEngine, model.Alarm) {
	t.Helper()
	e, repo, _ := newBusinessEngine(t, nil)
	ctx := context.Background()
	high := 60.0
	if err := repo.SaveProduct(ctx, model.Product{ID: "smoke", TenantID: "t1", Name: "烟温复合探测器", ThingModel: &model.ThingModel{Properties: []model.ThingField{{Identifier: "temperature", Name: "温度", DataType: "number", Unit: "℃", AlarmHigh: &high}}}}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"d1", "d2", "d3", "far"} {
		if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{ID: id, TenantID: "t1", ProductID: "smoke", Name: "设备" + id, AccessKey: "ak-" + id, Status: "ENABLED"}); err != nil {
			t.Fatal(err)
		}
	}
	state := model.SiteState{
		Units:     []model.SiteUnit{{SiteRecord: model.SiteRecord{ID: "u1"}}},
		Buildings: []model.SiteBuilding{{SiteRecord: model.SiteRecord{ID: "b1"}, UnitID: "u1"}, {SiteRecord: model.SiteRecord{ID: "b2"}, UnitID: "u1"}},
		Floors:    []model.SiteFloor{{SiteRecord: model.SiteRecord{ID: "f1"}, BuildingID: "b1"}, {SiteRecord: model.SiteRecord{ID: "f2"}, BuildingID: "b2"}},
		Points: []model.SitePoint{
			{SiteRecord: model.SiteRecord{ID: "p1"}, UnitID: "u1", BuildingID: "b1", FloorID: "f1", DeviceID: "d1", Name: "301 室"},
			{SiteRecord: model.SiteRecord{ID: "p2"}, UnitID: "u1", BuildingID: "b1", FloorID: "f1", DeviceID: "d2", Name: "302 室"},
			{SiteRecord: model.SiteRecord{ID: "p3"}, UnitID: "u1", BuildingID: "b1", FloorID: "f1", DeviceID: "d3", Name: "走廊"},
			{SiteRecord: model.SiteRecord{ID: "p4"}, UnitID: "u1", BuildingID: "b2", FloorID: "f2", DeviceID: "far"},
		},
	}
	if saved, err := repo.SaveSiteState(ctx, "t1", model.SiteState{}, state); err != nil || !saved {
		t.Fatalf("save sites: %v %v", saved, err)
	}
	if err := repo.SaveRule(ctx, model.AlarmRule{ID: "r-temp", TenantID: "t1", ProductID: "smoke", Name: "高温", AlarmType: "HIGH_TEMPERATURE", Level: "HIGH", Enabled: true,
		Conditions: []model.RuleCondition{{Field: "temperature", Operator: ">", Value: 60}}}); err != nil {
		t.Fatal(err)
	}
	now := int64(1_800_000_000_000)
	location := &model.AlarmLocation{UnitID: "u1", BuildingID: "b1", FloorID: "f1", PointID: "p1", PointName: "301 室"}
	alarm := model.Alarm{ID: "a-now", TenantID: "t1", RuleID: "r-temp", DeviceID: "d1", AlarmType: "HIGH_TEMPERATURE", AlarmLevel: "HIGH", Status: "ACTIVE", FirstTriggeredAt: now, LastTriggeredAt: now,
		Location: location, Cameras: []model.CameraSummary{{CameraID: "cam-1", CameraName: "三楼走廊", Enabled: true}},
		Details: map[string]any{"videoEvent": model.VideoAlarmEvent{EventID: "v1", CameraID: "cam-1", AlarmType: "SMOKE", Confidence: 0.8, EventTime: now - 1000, SnapshotURL: "https://secret.example/snap.jpg"}, "temperature": 85}}
	alarms := []model.Alarm{
		alarm,
		{ID: "a-neighbour", TenantID: "t1", RuleID: "r-n", DeviceID: "d2", AlarmType: "SMOKE_DETECTED", AlarmLevel: "HIGH", Status: "ACTIVE", LastTriggeredAt: now - 600_000, Location: &model.AlarmLocation{PointName: "302 室"}},
		{ID: "a-old-neighbour", TenantID: "t1", RuleID: "r-o", DeviceID: "d3", AlarmType: "SMOKE_DETECTED", Status: "CLOSED", LastTriggeredAt: now - 3*3600_000},
		{ID: "a-far", TenantID: "t1", RuleID: "r-f", DeviceID: "far", AlarmType: "SMOKE_DETECTED", Status: "ACTIVE", LastTriggeredAt: now - 60_000},
	}
	for i := range 8 {
		result := model.DispositionFalseAlarm
		if i == 0 {
			result = model.DispositionRealFire
		}
		alarms = append(alarms, model.Alarm{ID: fmt.Sprintf("a-hist-%d", i), TenantID: "t1", RuleID: fmt.Sprintf("r-h%d", i), DeviceID: "d1", AlarmType: "HIGH_TEMPERATURE", Status: "CLOSED",
			LastTriggeredAt: now - int64(i+1)*86_400_000, Disposition: &model.AlarmDisposition{Result: result, VerifiedAt: now}})
	}
	for _, a := range alarms {
		if _, _, err := repo.UpsertAlarm(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	return e, alarm
}

func TestAlarmContextCoversTheSiteRuleHistoryAndCameras(t *testing.T) {
	e, alarm := alarmContextFixture(t)
	c := e.buildAlarmContext(context.Background(), alarm)
	for _, name := range []string{"alarm", "device", "location", "siteAlarms", "rule", "dispositionHistory", "similarAlarms", "cameras"} {
		if _, ok := c.blocks[name]; !ok {
			t.Errorf("missing context block %s", name)
		}
	}
	site := c.blocks["siteAlarms"].(map[string]any)
	items := site["items"].([]map[string]any)
	if site["scope"] != "同楼层" || site["devicesNearby"] != 2 || len(items) != 1 || items[0]["alarmId"] != "a-neighbour" || items[0]["pointName"] != "302 室" {
		t.Fatalf("site alarms must be the same floor within two hours: %v", site)
	}
	if rule := c.blocks["rule"].(map[string]any); rule["name"] != "高温" {
		t.Fatalf("rule %v", rule)
	}
	history := c.blocks["dispositionHistory"].(map[string]any)
	if history["sameTypeAlarms"] != 8 || history["verified"] != 8 || history["byResult"].(map[string]int)[model.DispositionFalseAlarm] != 7 {
		t.Fatalf("disposition history %v", history)
	}
	if similar := c.blocks["similarAlarms"].([]map[string]any); len(similar) != 8 {
		t.Fatalf("similar alarms %d", len(similar))
	}
	payload, _ := json.Marshal(c.payload())
	text := string(payload)
	if strings.Contains(text, "secret.example") || !strings.Contains(text, `"hasSnapshot":true`) || !strings.Contains(text, `"cameraName":"三楼走廊"`) {
		t.Fatalf("camera context must describe media without its addresses: %s", text)
	}
	if !strings.Contains(text, `"temperature":85`) {
		t.Fatalf("small alarm details are kept: %s", text)
	}
	if len(payload) > 24<<10 {
		t.Fatalf("context is %d bytes", len(payload))
	}
}

func TestAlarmContextBudgetsShortenLargeBlocks(t *testing.T) {
	c := alarmContext{blocks: map[string]any{"similarAlarms": func() []map[string]any {
		out := []map[string]any{}
		for i := range 200 {
			out = append(out, map[string]any{"alarmId": fmt.Sprintf("a-%03d", i), "content": strings.Repeat("内容", 20)})
		}
		return out
	}(), "rule": map[string]any{"expression": strings.Repeat("x", 4000)}}}
	c.fitBudgets()
	if similar := c.blocks["similarAlarms"].([]map[string]any); len(similar) == 0 || len(similar) == 200 || jsonSize(similar) > alarmContextBudgets["similarAlarms"] {
		t.Fatalf("list was not shortened to its budget: %d items", len(similar))
	}
	if _, ok := c.blocks["rule"]; ok || len(c.omitted) != 2 {
		t.Fatalf("oversized block kept: omitted=%v", c.omitted)
	}
	if last := c.payload()[len(c.payload())-1]; last["contextType"] != "omitted" {
		t.Fatalf("omissions must be stated: %v", last)
	}
}

func TestAlarmSymptomsNameThresholdsAndTrends(t *testing.T) {
	high := 60.0
	history := []map[string]any{
		{"property": "humidity", "latest": map[string]any{"value": 40.0}, "stats1h": map[string]any{"mean": 40.0}},
		{"property": "temperature", "name": "温度", "unit": "℃", "alarmHigh": high, "latest": map[string]any{"value": 85.0}, "stats1h": map[string]any{"mean": 50.0}},
	}
	if got := alarmSymptoms(history); len(got) != 2 || got[0] != "温度 85℃ 超过告警阈值 持续上升" || got[1] != "humidity 40" {
		t.Fatalf("symptoms %q", got)
	}
}

func TestAlarmContextIncludesDeviceSignals(t *testing.T) {
	e, alarm := alarmContextFixture(t)
	repo := e.Engine.Repo.(*memory.Repository)
	e.Engine.DeviceSignals = repo
	if err := repo.ReplaceDeviceSignals(context.Background(), "t1", []model.DeviceSignal{{TenantID: "t1", DeviceID: "d1", SignalType: model.SignalOutOfRange, Property: "temperature", Strength: 0.4}}); err != nil {
		t.Fatal(err)
	}
	c := e.buildAlarmContext(context.Background(), alarm)
	signals, ok := c.blocks["deviceSignals"].([]map[string]any)
	if !ok || len(signals) != 1 || signals[0]["name"] != "数值超出有效范围" || signals[0]["property"] != "temperature" {
		t.Fatalf("device signals block %v", c.blocks["deviceSignals"])
	}
}
