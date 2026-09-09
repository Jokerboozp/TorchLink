package core

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	aiadapter "iot-platform/internal/adapters/ai"
	"iot-platform/internal/adapters/knowledge"
	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
)

type failingAlarmAI struct{}

func (failingAlarmAI) AnalyzeAlarm(context.Context, model.Alarm, []map[string]any, []string) (model.AIAnalysis, error) {
	return model.AIAnalysis{}, errors.New("provider response invalid")
}
func (failingAlarmAI) Chat(context.Context, string, string) (string, error) {
	return "", errors.New("provider response invalid")
}
func (failingAlarmAI) RuleDraft(context.Context, string, string) (model.AlarmRule, error) {
	return model.AlarmRule{}, errors.New("provider response invalid")
}
func (failingAlarmAI) Health(context.Context) error { return errors.New("provider response invalid") }

type recordingBus struct {
	*local.Bus
	topics []string
}

func (b *recordingBus) Publish(ctx context.Context, topic, key string, payload []byte) error {
	b.topics = append(b.topics, topic)
	return b.Bus.Publish(ctx, topic, key, payload)
}

func hasTopic(topics []string, wanted string) bool {
	for _, topic := range topics {
		if topic == wanted {
			return true
		}
	}
	return false
}

func TestParsedMessageFanoutRequiresSuccessfulParsing(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bus := &recordingBus{Bus: local.NewBus()}
	realtime := local.NewRealtime()
	e := New(repo, archive, bus, realtime, parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err = e.Start(ctx); err != nil {
		t.Fatal(err)
	}
	normal := model.RawMessage{MessageID: "raw_fanout_normal", TenantID: "t1", ProductID: "json_sensor", DeviceID: "device_fanout", Protocol: "json", PayloadFormat: "json", ReceivedAt: 1000, Payload: json.RawMessage(`{"properties":{"temperature":23}}`)}
	if _, _, err = e.IngestRaw(ctx, normal); err != nil {
		t.Fatal(err)
	}
	if !hasTopic(bus.topics, model.TopicRaw) || !hasTopic(bus.topics, model.TopicPropertyReport) {
		t.Fatalf("normal parsed message did not reach Kafka topics: %#v", bus.topics)
	}
	foundParsedMQTT := false
	for _, published := range realtime.Messages {
		if published.Topic == "/iot/parsed/t1/json_sensor/device_fanout/PROPERTY_REPORT" {
			foundParsedMQTT = true
			break
		}
	}
	if !foundParsedMQTT {
		t.Fatalf("normal parsed message did not reach MQTT: %#v", realtime.Messages)
	}
	event := model.RawMessage{MessageID: "raw_fanout_event", TenantID: "t1", ProductID: "json_sensor", DeviceID: "device_fanout", Protocol: "json", PayloadFormat: "json", ReceivedAt: 1001, Payload: json.RawMessage(`{"event":{"type":"FAULT"}}`)}
	if _, _, err = e.IngestRaw(ctx, event); err != nil {
		t.Fatal(err)
	}
	if !hasTopic(bus.topics, model.TopicEventReport) {
		t.Fatalf("event parsed message did not reach Kafka event topic: %#v", bus.topics)
	}
	failure := model.RawMessage{MessageID: "raw_fanout_failure", TenantID: "t1", ProductID: "json_sensor", DeviceID: "device_fanout", Protocol: "json", PayloadFormat: "json", ReceivedAt: 1002, Payload: json.RawMessage(`[]`)}
	before := len(realtime.Messages)
	if _, _, err = e.IngestRaw(ctx, failure); err != nil {
		t.Fatal(err)
	}
	idx, indexErr := repo.GetRawIndex(ctx, failure.TenantID, failure.MessageID)
	if indexErr != nil || idx.ParseError == "" || idx.ParseAttemptedAt == 0 {
		t.Fatal("parse failure not persisted", idx, indexErr)
	}
	if len(realtime.Messages) != before || hasTopic(bus.topics, model.TopicParseFailed) {
		t.Fatalf("parse failure was forwarded: topics=%#v realtime=%#v", bus.topics, realtime.Messages)
	}
}

func TestRawToAlarmPipeline(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bus := local.NewBus()
	realtime := local.NewRealtime()
	e := New(repo, archive, bus, realtime, parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	e.AI = aiadapter.NoopAI{}
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
	if _, err = repo.GetAIAnalysis(ctx, alarms[0].TenantID, alarms[0].ID); err != nil {
		t.Fatalf("ai analysis not saved: %v", err)
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
	e := New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	e.AI = failingAlarmAI{}
	if _, _, err = repo.UpsertAlarm(ctx, model.Alarm{ID: "alarm-ai-failure", TenantID: "t1", DeviceID: "device-1", AlarmType: "FIRE", AlarmLevel: "HIGH", Status: "ACTIVE", LastTriggeredAt: time.Now().UnixMilli()}); err != nil {
		t.Fatal(err)
	}

	analysis, err := e.AnalyzeAlarm(ctx, "t1", "alarm-ai-failure")
	if err != nil {
		t.Fatal(err)
	}
	if analysis.Summary != "AI 研判暂时失败，已保留告警供人工研判。" || analysis.Model != "unavailable" || analysis.Error == "" {
		t.Fatalf("unexpected readable fallback: %#v", analysis)
	}
	saved, err := repo.GetAIAnalysis(ctx, "t1", "alarm-ai-failure")
	if err != nil {
		t.Fatal(err)
	}
	if saved.Summary == "" || saved.Model == "" {
		t.Fatalf("saved fallback is not renderable: %#v", saved)
	}
}

func TestGatewayAutomaticallyRegistersChildDevice(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err = e.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveProduct(ctx, model.Product{ID: "gateway_product", TenantID: "t1", Name: "网关产品", Category: "gateway", Status: "ENABLED"}); err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveProduct(ctx, model.Product{ID: "sensor_product", TenantID: "t1", Name: "传感器产品", Category: "sensor", Status: "ENABLED"}); err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveManagedDevice(ctx, model.ManagedDevice{ID: "gateway_1", TenantID: "t1", ProductID: "gateway_product", Name: "一号网关", Status: "ENABLED", DeviceRole: "GATEWAY", AccessKey: "dk_gateway", SecretHash: "unused"}); err != nil {
		t.Fatal(err)
	}
	raw := model.RawMessage{MessageID: "raw_child_1", TenantID: "t1", ProductID: "sensor_product", DeviceID: "child_1", DeviceName: "一号烟感", GatewayID: "gateway_1", Protocol: "json", PayloadFormat: "json", Payload: json.RawMessage(`{"properties":{"temperature":25.5}}`)}
	if _, created, ingestErr := e.IngestRaw(ctx, raw); ingestErr != nil || !created {
		t.Fatalf("ingest created=%v err=%v", created, ingestErr)
	}
	child, err := repo.GetManagedDevice(ctx, "t1", "child_1")
	if err != nil {
		t.Fatal(err)
	}
	if child.DeviceRole != "CHILD" || child.GatewayID != "gateway_1" || !child.AutoRegistered || child.RegistrationSource != "GATEWAY_AUTO" || child.ProductID != "sensor_product" {
		t.Fatalf("unexpected child registration %#v", child)
	}
}

func TestStateChangeIsStoredAsStandardMessage(t *testing.T) {
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bus := local.NewBus()
	e := New(repo, archive, bus, local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := e.Start(ctx); err != nil {
		t.Fatal(err)
	}
	raw := model.RawMessage{MessageID: "raw_state_change", TenantID: "t1", ProductID: "json_sensor", DeviceID: "state_device", Protocol: "json", PayloadFormat: "json", Payload: json.RawMessage(`{"businessStatus":"ONLINE"}`)}
	if _, _, err := e.IngestRaw(ctx, raw); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if message, getErr := repo.GetStandardMessageByRaw(ctx, "t1", raw.MessageID); getErr == nil {
			if message.MessageType != model.StateChange {
				t.Fatalf("unexpected message type: %s", message.MessageType)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("state-change standard message was not stored")
}

func TestAlarmCannotBeAcknowledgedTwice(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	alarm := model.Alarm{ID: "alarm_ack_once", TenantID: "t1", DeviceID: "device_1", AlarmType: "FIRE", AlarmLevel: "HIGH", Status: "ACTIVE", Source: "device"}
	if err = repo.UpsertDeviceState(ctx, model.DeviceState{TenantID: "t1", ProductID: "sensor", DeviceID: "device_1", BusinessStatus: "ONLINE"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = repo.UpsertAlarm(ctx, alarm); err != nil {
		t.Fatal(err)
	}

	first, err := e.SetAlarmStatus(ctx, "t1", alarm.ID, "ACKED", "operator")
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != "ACKED" {
		t.Fatalf("first acknowledgement did not update status: %#v", first)
	}
	state, err := repo.GetDeviceState(ctx, "t1", "device_1")
	if err != nil || state.BusinessStatus != "ALARM" {
		t.Fatalf("acknowledged alarm did not keep device in ALARM state: state=%#v err=%v", state, err)
	}
	if _, err = e.SetAlarmStatus(ctx, "t1", alarm.ID, "ACKED", "operator"); err == nil {
		t.Fatal("second acknowledgement should be rejected")
	}

	persisted, err := repo.GetAlarm(ctx, "t1", alarm.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != "ACKED" || persisted.AckedAt != first.AckedAt {
		t.Fatalf("second acknowledgement changed the alarm: %#v", persisted)
	}
	if _, err = e.SetAlarmStatus(ctx, "t1", alarm.ID, "CLOSED", "operator"); err != nil {
		t.Fatal(err)
	}
	state, err = repo.GetDeviceState(ctx, "t1", "device_1")
	if err != nil || state.BusinessStatus != "ONLINE" {
		t.Fatalf("closed alarm did not return device to ONLINE state: state=%#v err=%v", state, err)
	}
}

func TestLateMessageCannotRollBackDeviceConnectivity(t *testing.T) {
	repo := memory.NewRepository()
	ctx := context.Background()
	state := model.DeviceState{TenantID: "t", DeviceID: "d", ProductID: "p", LastSeenAt: 2000, ConnectionStatus: "CONNECTED", BusinessStatus: "ONLINE"}
	if err := repo.UpsertDeviceState(ctx, state); err != nil {
		t.Fatal(err)
	}
	e := New(repo, nil, local.NewBus(), local.NewRealtime(), nil, nil)
	late := model.StandardMessage{TenantID: "t", DeviceID: "d", ProductID: "p", Timestamp: 1000, Parser: parser.StandardParserName, MessageType: model.StateChange, Properties: map[string]any{"connectionStatus": "DISCONNECTED"}}
	if err := e.touchState(ctx, late); err != nil {
		t.Fatal(err)
	}
	actual, _ := repo.GetDeviceState(ctx, "t", "d")
	if actual.LastSeenAt != 2000 || actual.ConnectionStatus != "CONNECTED" {
		t.Fatal("late message rolled back status", actual)
	}
}
