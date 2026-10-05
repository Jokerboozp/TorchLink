package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iot-platform/internal/aiprompt"
	"iot-platform/internal/metrics"
	"log/slog"
	"strings"
	"testing"
	"time"

	"iot-platform/internal/adapters/knowledge"
	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/aitest"
	"iot-platform/internal/messagetopics"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
	"iot-platform/internal/sites"
)

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
	// Device state updates of the earlier messages may still be published
	// meanwhile; only parsed-data topics must not grow.
	parsedTopics := func() int {
		n := 0
		for _, published := range realtime.Snapshot() {
			if strings.HasPrefix(published.Topic, "/iot/parsed/") {
				n++
			}
		}
		return n
	}
	before := parsedTopics()
	if _, _, err = e.IngestRaw(ctx, failure); err != nil {
		t.Fatal(err)
	}
	idx, indexErr := repo.GetRawIndex(ctx, failure.TenantID, failure.MessageID)
	if indexErr != nil || idx.ParseError == "" || idx.ParseAttemptedAt == 0 {
		t.Fatal("parse failure not persisted", idx, indexErr)
	}
	if parsedTopics() != before || hasTopic(bus.topics, model.TopicParseFailed) {
		t.Fatalf("parse failure was forwarded: topics=%#v realtime=%#v", bus.topics, realtime.Snapshot())
	}
	// Query topics are evaluated on the actual parsing path, while the
	// business stream still persists and processes every message.
	settings := model.MessageTopicConfig{Topics: []model.MessageTopicRoute{
		{ID: "kafka-rule", Name: "Kafka 自定义消息", Protocol: "kafka", Topic: messagetopics.KafkaPrefix("t1") + "selected-values", Enabled: true},
		{ID: "mqtt-rule", Name: "MQTT 自定义消息", Protocol: "mqtt", Topic: messagetopics.MQTTPrefix("t1") + "selected-values", Enabled: true},
	}}
	var busBefore, mqttBefore int
	query, err := messagetopics.CompileQuerySQL("SELECT deviceId, properties.temperature AS temperature FROM device_reports WHERE properties.temperature >= 25")
	if err != nil {
		t.Fatal(err)
	}
	query.DeviceScope, query.DeviceIDs = "selected", []string{"device_fanout"}
	for i := range settings.Topics {
		settings.Topics[i].Query = &query
		if err := messagetopics.AccumulateQueryExposure(&settings, settings.Topics[i].ID, query); err != nil {
			t.Fatal(err)
		}
	}
	if saved, err := e.MessageTopics.Save(ctx, "t1", settings); err != nil || !saved {
		t.Fatal("save data query", saved, err)
	}
	for _, tc := range []struct {
		id, device, payload string
		want                bool
	}{
		{"below", "device_fanout", `{"properties":{"temperature":23}}`, false},
		{"matched", "device_fanout", `{"properties":{"temperature":26}}`, true},
		{"scope", "other_device", `{"properties":{"temperature":26}}`, false},
		{"invalid", "device_fanout", `[]`, false},
	} {
		normal.MessageID, normal.DeviceID, normal.Payload = "query_"+tc.id, tc.device, json.RawMessage(tc.payload)
		normal.ReceivedAt++
		busBefore, mqttBefore = len(bus.topics), len(realtime.Messages)
		if _, _, err := e.IngestRaw(ctx, normal); err != nil {
			t.Fatal(tc.id, err)
		}
		if got := hasTopic(bus.topics[busBefore:], settings.Topics[0].Topic); got != tc.want {
			t.Fatalf("%s Kafka query match=%t", tc.id, got)
		}
		count := 0
		for _, message := range realtime.Messages[mqttBefore:] {
			if message.Topic == settings.Topics[1].Topic {
				count++
				if string(message.Payload) != `{"deviceId":"device_fanout","temperature":26}` {
					t.Fatalf("query projection=%s", message.Payload)
				}
			}
		}
		if (count == 1) != tc.want || count > 1 {
			t.Fatalf("%s MQTT query deliveries=%d", tc.id, count)
		}
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
	e := New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
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
	if _, err = e.SetAlarmStatus(ctx, "t1", alarm.ID, "CLOSED", "operator"); !errors.Is(err, model.ErrDispositionRequired) {
		t.Fatalf("closing an unverified fire alarm: %v", err)
	}
	if _, err = e.VerifyAlarm(ctx, "t1", alarm.ID, model.AlarmDisposition{Result: model.DispositionFalseAlarm, Notes: "探头积尘"}, "operator"); err != nil {
		t.Fatal(err)
	}
	if _, err = e.SetAlarmStatus(ctx, "t1", alarm.ID, "CLOSED", "operator"); err != nil {
		t.Fatal(err)
	}
	if _, err = e.VerifyAlarm(ctx, "t1", alarm.ID, model.AlarmDisposition{Result: model.DispositionRealFire}, "operator"); err == nil {
		t.Fatal("a closed alarm was verified again")
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

type releaseOutageRepository struct {
	ports.Repository
	marked int
}

func (r *releaseOutageRepository) GetProtocolRelease(context.Context, string, string, string) (model.ProtocolRelease, error) {
	return model.ProtocolRelease{}, errors.New("failed to connect: too many clients already")
}

func (r *releaseOutageRepository) MarkRawParseResult(ctx context.Context, tenant, id string, at int64, parseError string) error {
	r.marked++
	return r.Repository.MarkRawParseResult(ctx, tenant, id, at, parseError)
}

// A database outage while loading the protocol version is retried through the
// bus instead of being stored as a permanent parse failure.
func TestRepositoryOutageDuringParseIsRetriedNotMarkedFailed(t *testing.T) {
	repo := &releaseOutageRepository{Repository: memory.NewRepository()}
	e := newRuleTestEngine(t, repo, &ruleTestClock{now: time.Unix(1000, 0)})
	raw, _ := json.Marshal(model.RawMessage{MessageID: "raw-1", TenantID: "tenant-a", ProductID: "sensor", DeviceID: "device-a", ProtocolID: "vendor", ProtocolVersion: "1.0.0", PayloadFormat: "json", Payload: json.RawMessage(`{"temperature":20}`)})
	if err := e.handleRaw(context.Background(), raw); err == nil {
		t.Fatal("a repository outage must be returned for redelivery")
	}
	if repo.marked != 0 {
		t.Fatalf("the raw message must not be marked as a parse failure, marked=%d", repo.marked)
	}
}

// While paused by backpressure, ingest refuses new messages with a retryable
// error and archives nothing; after resuming it accepts them again.
func TestIngestPausedRefusesThenResumes(t *testing.T) {
	repo := memory.NewRepository()
	e := newRuleTestEngine(t, repo, &ruleTestClock{now: time.Unix(1000, 0)})
	raw := model.RawMessage{MessageID: "raw-paused", TenantID: "tenant-a", ProductID: "sensor", DeviceID: "device-a", Protocol: "json", PayloadFormat: "json", Payload: json.RawMessage(`{"temperature":20}`)}
	e.SetIngestPaused(true)
	if _, _, err := e.IngestRaw(context.Background(), raw); !errors.Is(err, model.ErrBackpressure) {
		t.Fatalf("paused ingest must return ErrBackpressure, got %v", err)
	}
	if _, err := repo.GetRawIndex(context.Background(), raw.TenantID, raw.MessageID); err == nil {
		t.Fatal("a refused message must not be archived")
	}
	e.SetIngestPaused(false)
	if _, _, err := e.IngestRaw(context.Background(), raw); errors.Is(err, model.ErrBackpressure) {
		t.Fatalf("resumed ingest must not refuse: %v", err)
	}
}

func TestLargePropertyReportStillTriggersRules(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	properties := map[string]any{}
	for i := 0; i < 257; i++ {
		properties[fmt.Sprint(i)] = float64(i)
	}
	properties["temperature"] = float64(90)
	if err := repo.SaveRule(ctx, model.AlarmRule{ID: "rule", TenantID: "tenant", Name: "temperature", AlarmType: "FIRE", Level: "HIGH", Enabled: true, Conditions: []model.RuleCondition{{Field: "temperature", Operator: ">", Value: 80}}}); err != nil {
		t.Fatal(err)
	}
	msg := model.StandardMessage{TenantID: "tenant", DeviceID: "device", ProductID: "product", MessageID: "oversized", MessageType: model.PropertyReport, Properties: properties, Timestamp: time.Now().UnixMilli()}
	data, _ := json.Marshal(msg)
	if err := e.handleStandard(ctx, data); err != nil {
		t.Fatal("property processing failed", err)
	}
	claim, err := repo.ClaimStandardMessage(ctx, msg, "check", time.Minute)
	if err != nil || claim.ShouldProcess {
		t.Fatal("message not completed", err)
	}
	alarms, err := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "tenant", DeviceID: "device", Limit: 10})
	if err != nil || len(alarms) != 1 {
		t.Fatal("property report did not trigger rule alarm", alarms, err)
	}
}

func TestAlarmReportsIncludeRepeatedTriggers(t *testing.T) {
	for _, kind := range []string{"direct", "rule", "component"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			repo, bus := memory.NewRepository(), local.NewBus()
			e := New(repo, nil, bus, local.NewRealtime(), nil, nil)
			// New alarms copy the device's or component's site position.
			placement := e.Locator.(*sites.Service)
			unit, err := placement.SaveUnit(ctx, "t", "admin", model.SiteUnit{Name: "示例单位"})
			if err != nil {
				t.Fatal(err)
			}
			for component, name := range map[string]string{"": "消防主机", "c": "一楼探测器"} {
				if _, err = placement.SavePoint(ctx, "t", "admin", model.SitePoint{UnitID: unit.ID, DeviceID: "d", ComponentID: component, Name: name}); err != nil {
					t.Fatal(err)
				}
			}
			var reports []model.Alarm
			if err := bus.Subscribe(ctx, model.TopicAlarmReported, "test", func(_ context.Context, b []byte) error {
				var a model.Alarm
				if err := json.Unmarshal(b, &a); err != nil {
					return err
				}
				reports = append(reports, a)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if kind == "rule" {
				if err := repo.SaveRule(ctx, model.AlarmRule{ID: "r", TenantID: "t", Name: "烟雾规则", Enabled: true, AlarmType: "FIRE", Level: "HIGH", Conditions: []model.RuleCondition{{Field: "smoke", Operator: "eq", Value: true}}}); err != nil {
					t.Fatal(err)
				}
			}
			send := func(id string, at int64, active bool, description string) {
				t.Helper()
				msg := model.StandardMessage{TenantID: "t", ProductID: "p", DeviceID: "d", MessageID: id, Timestamp: at, MessageType: model.AlarmReport, Properties: map[string]any{"smoke": active}, Event: map[string]any{"alarmType": "FIRE", "description": description}}
				if !active {
					msg.MessageType = model.PropertyReport
				}
				if kind == "component" {
					msg.MessageType = model.StateChange
					msg.Event["components"] = []model.ComponentStatus{{ID: "c", Name: "探测器", Location: "一楼", Alarms: map[string]bool{"FIRE": active}}}
				}
				if err := e.handleStandard(ctx, mustJSON(msg)); err != nil {
					t.Fatal(err)
				}
			}
			send("m1", 1000, true, "首次报警")
			send("m2", 2000, true, "再次报警")
			send("m2", 2000, true, "再次报警")
			if kind == "component" {
				send("stale", 500, true, "乱序旧报文")
			}
			if len(reports) != 2 {
				t.Fatalf("want 2 report events, got %d", len(reports))
			}
			if reports[0].ID != reports[1].ID || reports[0].TriggerID != "m1" || reports[1].TriggerID != "m2" {
				t.Fatalf("incorrect report identity: %+v", reports)
			}
			want := map[bool]string{false: "消防主机", true: "一楼探测器"}[kind == "component"]
			if l := reports[0].Location; l == nil || l.PointName != want || l.UnitName != "示例单位" {
				t.Fatalf("alarm location %+v, want point %s", l, want)
			}
			message := reports[1].Details["message"].(map[string]any)
			if message["event"].(map[string]any)["description"] != "再次报警" {
				t.Fatal("retained old alarm details")
			}
			alarms, err := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "t", Status: "ACTIVE"})
			if err != nil || len(alarms) != 1 || alarms[0].TriggerCount != 2 {
				t.Fatalf("changed alarm aggregation: %+v %v", alarms, err)
			}
			// The alarm keeps the description of its first trigger.
			wantContent := map[bool]string{false: "首次报警", true: "探测器（一楼）"}[kind == "component"]
			if alarms[0].Content != wantContent || reports[0].Content != wantContent {
				t.Fatalf("alarm content %q / %q, want %q", alarms[0].Content, reports[0].Content, wantContent)
			}
			send("normal", 3000, false, "恢复")
			if len(reports) != 2 {
				t.Fatal("recovery must not send an alarm receipt")
			}
		})
	}
}

func TestAlarmContentFallsBackAndIsBounded(t *testing.T) {
	msg := model.StandardMessage{Event: map[string]any{"content": "  "}, Raw: map[string]any{"payload": map[string]any{"alarm": map[string]any{"alarmDesc": "3 层烟感报警"}}}}
	if got := alarmContent(msg, "规则说明"); got != "3 层烟感报警" {
		t.Fatalf("nested description: %q", got)
	}
	if got := alarmContent(model.StandardMessage{}, "", "温度超限"); got != "温度超限" {
		t.Fatalf("fallback: %q", got)
	}
	long := model.StandardMessage{Event: map[string]any{"content": strings.Repeat("火", maxAlarmContent+10)}}
	if got := []rune(alarmContent(long)); len(got) != maxAlarmContent {
		t.Fatalf("content not bounded: %d", len(got))
	}
}

type failingReportBus struct {
	*local.Bus
	failed bool
}

func (b *failingReportBus) Publish(ctx context.Context, topic, key string, payload []byte) error {
	if b.failed && topic == model.TopicAlarmReported {
		return errors.New("report stream unavailable")
	}
	return b.Bus.Publish(ctx, topic, key, payload)
}

func TestAlarmReportSurvivesPublishFailure(t *testing.T) {
	ctx := context.Background()
	bus := &failingReportBus{Bus: local.NewBus(), failed: true}
	e := New(memory.NewRepository(), nil, bus, local.NewRealtime(), nil, nil)
	reports := 0
	_ = bus.Subscribe(ctx, model.TopicAlarmReported, "test", func(context.Context, []byte) error { reports++; return nil })
	if _, _, err := e.raiseDirectAlarm(ctx, model.StandardMessage{TenantID: "t", DeviceID: "d", MessageID: "m", MessageType: model.AlarmReport}); err != nil {
		t.Fatal(err)
	}
	bus.failed = false
	e.flushOutbox(ctx)
	e.flushOutbox(ctx)
	if reports != 1 {
		t.Fatalf("want the committed report published once after recovery, got %d", reports)
	}
}

func TestComponentAlarmLifecycleAndRecoveryIsolation(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	archive, _ := local.NewArchive(t.TempDir())
	e := New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	send := func(id string, at int64, components ...model.ComponentStatus) {
		t.Helper()
		m := model.StandardMessage{MessageID: id, TenantID: "t", ProductID: "p", DeviceID: "controller", MessageType: model.StateChange, Timestamp: at, Event: map[string]any{"components": components}}
		b, _ := json.Marshal(m)
		if err := e.handleStandard(ctx, b); err != nil {
			t.Fatal(err)
		}
	}
	c := func(id string, fire, fault bool) model.ComponentStatus {
		return model.ComponentStatus{ID: id, Name: "探测器", Location: "二楼走廊", Alarms: map[string]bool{"FIRE": fire, "DEVICE_FAULT": fault}}
	}
	active := func(n int) {
		t.Helper()
		alarms, err := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "t", DeviceID: "controller", Status: "ACTIVE", Limit: 100})
		if err != nil || len(alarms) != n {
			t.Fatalf("active=%+v err=%v want=%d", alarms, err, n)
		}
	}
	send("m1", 1000, c("A", false, false), c("B", true, true))
	active(2)
	send("normal-A", 2000, c("A", false, false))
	active(2)
	// Registration/connection status cannot recover either component.
	b, _ := json.Marshal(model.StandardMessage{MessageID: "online", TenantID: "t", ProductID: "p", DeviceID: "controller", MessageType: model.StateChange, Timestamp: 2500, Properties: map[string]any{"connectionStatus": "CONNECTED"}})
	if err := e.handleStandard(ctx, b); err != nil {
		t.Fatal(err)
	}
	active(2)
	// Recovery of one category must leave the other category active.
	send("recover-fire", 3000, model.ComponentStatus{ID: "B", Alarms: map[string]bool{"FIRE": false}})
	active(1)
	send("old-alarm", 1500, c("B", true, true))
	active(1)
	send("same-time-alarm", 3000, model.ComponentStatus{ID: "B", Alarms: map[string]bool{"FIRE": true}})
	active(2)
	send("same-time-normal", 3000, c("B", false, false))
	active(1)
	send("recover-all", 4000, c("B", false, false))
	active(0)
	send("late-replay", 1000, c("B", true, true))
	active(0)
	send("new-cycle", 5000, c("B", true, false))
	active(1)
	send("new-cycle", 5000, c("B", true, false))
	active(1)
	alarms, _ := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "t", Status: "ACTIVE", Limit: 100})
	a := alarms[0]
	if a.ComponentID != "B" || a.ComponentLocation != "二楼走廊" || a.TriggerCount != 1 {
		t.Fatalf("bad component alarm %+v", a)
	}
	a.Status = "ACKED"
	if err := repo.UpdateAlarm(ctx, a); err != nil {
		t.Fatal(err)
	}
	send("acked-recovery", 6000, c("B", false, false))
	active(0)
	got, _ := repo.GetAlarm(ctx, "t", a.ID)
	if got.Status != "RECOVERED" {
		t.Fatal(got)
	}
	// More than the old 100-row recovery limit, each addressed independently.
	for batch := 0; batch < 2; batch++ {
		items := []model.ComponentStatus{}
		for i := 0; i < 80; i++ {
			items = append(items, c(fmt.Sprintf("many-%d-%d", batch, i), true, false))
		}
		send(fmt.Sprint("many", batch), 7000, items...)
	}
	for batch := 0; batch < 2; batch++ {
		items := []model.ComponentStatus{}
		for i := 0; i < 80; i++ {
			items = append(items, c(fmt.Sprintf("many-%d-%d", batch, i), false, false))
		}
		send(fmt.Sprint("clear", batch), 8000, items...)
	}
	active(0)
}

func TestInvalidComponentsAndPlainStateCannotClearFire(t *testing.T) {
	for _, value := range []any{nil, []any{}, []any{map[string]any{"id": "x", "alarms": map[string]any{"FIRE": nil}}}, []any{map[string]any{"id": "x", "alarms": map[string]any{"FIRE": "false"}}}, []model.ComponentStatus{{ID: "x", Alarms: map[string]bool{"FIRE": true}}, {ID: "x", Alarms: map[string]bool{"FIRE": false}}}} {
		if _, err := model.MessageComponents(model.StandardMessage{MessageType: model.AlarmReport, Timestamp: 1000, Event: map[string]any{"components": value}}); err == nil {
			t.Fatalf("accepted invalid %#v", value)
		}
	}
	if directAlarmCleared(model.StandardMessage{MessageType: model.StateChange, Properties: map[string]any{"connectionStatus": "CONNECTED"}}) {
		t.Fatal("connection cleared alarm")
	}
	if directAlarmTypeCleared(model.StandardMessage{Properties: map[string]any{"fault": false}}, "FIRE") {
		t.Fatal("fault recovery cleared fire")
	}
}

type countedStateRepo struct {
	*memory.Repository
	reads, writes, alarmReads int
}

func (r *countedStateRepo) GetDeviceStateFresh(ctx context.Context, t, d string) (model.DeviceState, error) {
	r.reads++
	return r.Repository.GetDeviceStateFresh(ctx, t, d)
}
func (r *countedStateRepo) UpsertDeviceStateIf(ctx context.Context, v model.DeviceState) (bool, error) {
	r.writes++
	return r.Repository.UpsertDeviceStateIf(ctx, v)
}
func (r *countedStateRepo) LoadDeviceStateWithAlarms(ctx context.Context, t, d string) (model.DeviceState, bool, error) {
	r.reads++
	return r.Repository.LoadDeviceStateWithAlarms(ctx, t, d)
}
func (r *countedStateRepo) CompleteStandardMessage(ctx context.Context, state *model.DeviceState, t, id string, token int64) (bool, error) {
	if state != nil {
		r.writes++
	}
	return r.Repository.CompleteStandardMessage(ctx, state, t, id, token)
}
func (r *countedStateRepo) HasOpenAlarm(ctx context.Context, t, d string) (bool, error) {
	r.alarmReads++
	return r.Repository.HasOpenAlarm(ctx, t, d)
}
func (r *countedStateRepo) ListAlarms(ctx context.Context, f ports.AlarmFilter) ([]model.Alarm, error) {
	r.alarmReads++
	return r.Repository.ListAlarms(ctx, f)
}
func TestStandardMessageWritesFinalStateOnce(t *testing.T) {
	repo := &countedStateRepo{Repository: memory.NewRepository()}
	e := New(repo, nil, local.NewBus(), local.NewRealtime(), nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	msg := model.StandardMessage{TenantID: "t", DeviceID: "d", ProductID: "p", MessageID: "m", Timestamp: 1000, MessageType: model.PropertyReport, Properties: map[string]any{"temperature": 22}}
	if err := e.handleStandard(context.Background(), mustJSON(msg)); err != nil {
		t.Fatal(err)
	}
	if repo.writes != 1 || repo.reads > 1 || repo.alarmReads != 0 {
		t.Fatalf("per-message operations: state writes=%d reads=%d alarm list reads=%d", repo.writes, repo.reads, repo.alarmReads)
	}
	if err := e.handleStandard(context.Background(), mustJSON(msg)); err != nil {
		t.Fatal(err)
	}
	if repo.writes != 1 {
		t.Fatal("duplicate rewrote processed state")
	}
}

type flakyReceiptPublisher struct {
	ports.RealtimePublisher
	calls int
	body  []byte
}

func (p *flakyReceiptPublisher) Publish(_ context.Context, topic string, body []byte, qos byte, retained bool) error {
	if topic != "/iot/down/t/p/d/receipt" || qos != 1 || retained {
		return fmt.Errorf("unexpected receipt route: %s", topic)
	}
	p.calls++
	p.body = append([]byte{}, body...)
	if p.calls == 1 {
		return errors.New("receipt connection lost")
	}
	return nil
}
func TestMQTTArchiveReceiptRetryAndConflict(t *testing.T) {
	e, repo, _ := newBusinessEngine(t, nil)
	p := &flakyReceiptPublisher{RealtimePublisher: local.NewRealtime()}
	e.Realtime = p
	raw := model.RawMessage{MessageID: "raw-r", TenantID: "t", ProductID: "p", DeviceID: "d", Protocol: "json", PayloadFormat: "json", ReceivedAt: 1, Payload: json.RawMessage(`{"id":"device-r","properties":{"value":1}}`), Metadata: map[string]any{"clientMessageId": "device-r"}}
	if err := e.IngestMQTT(context.Background(), raw); err == nil {
		t.Fatal("missing receipt must retry")
	}
	if _, err := repo.GetRawIndex(context.Background(), "t", "raw-r"); err != nil {
		t.Fatal("archive must precede receipt", err)
	}
	if err := e.IngestMQTT(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	var receipt struct{ ID, Status, PayloadHash, RawMessageID string }
	if err := json.Unmarshal(p.body, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.ID != "device-r" || receipt.Status != "archived" || receipt.PayloadHash != raw.PayloadHash() || receipt.RawMessageID != "raw-r" {
		t.Fatal(receipt)
	}
	raw.Payload = json.RawMessage(`{"id":"device-r","properties":{"value":2}}`)
	if err := e.IngestMQTT(context.Background(), raw); err == nil {
		t.Fatal("same archive ID changed payload was accepted")
	}
	if p.calls != 2 {
		t.Fatal("conflict emitted a receipt", p.calls)
	}
}

// countingRepo counts the lookups the hot path should avoid.
type countingRepo struct {
	*memory.Repository
	rawIndexReads, productReads int
}

func (r *countingRepo) GetRawIndex(ctx context.Context, tenant, id string) (model.RawArchiveIndex, error) {
	r.rawIndexReads++
	return r.Repository.GetRawIndex(ctx, tenant, id)
}

func (r *countingRepo) GetProduct(ctx context.Context, tenant, id string) (model.Product, error) {
	r.productReads++
	return r.Repository.GetProduct(ctx, tenant, id)
}

func TestHotPathSkipsRedundantLookups(t *testing.T) {
	ctx := context.Background()
	repo := &countingRepo{Repository: memory.NewRepository()}
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err = e.Start(ctx); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		raw := model.RawMessage{MessageID: fmt.Sprintf("hot-%d", i), TenantID: "t", ProductID: "json_sensor", DeviceID: "d", Protocol: "json", PayloadFormat: "json", ReceivedAt: int64(1000 + i), Payload: json.RawMessage(`{"properties":{"t":1}}`)}
		if _, created, err := e.IngestRaw(ctx, raw); err != nil || !created {
			t.Fatalf("ingest %d: %v %v", i, created, err)
		}
	}
	if repo.rawIndexReads != 0 {
		t.Fatalf("new messages read the archive index %d times", repo.rawIndexReads)
	}
	if repo.productReads > 1 {
		t.Fatalf("parsing read the product %d times for one product", repo.productReads)
	}
	// A retransmission is still recognised through the index.
	dup := model.RawMessage{MessageID: "hot-0", TenantID: "t", ProductID: "json_sensor", DeviceID: "d", Protocol: "json", PayloadFormat: "json", ReceivedAt: 1000, Payload: json.RawMessage(`{"properties":{"t":1}}`)}
	if _, created, err := e.IngestRaw(ctx, dup); err != nil || created || repo.rawIndexReads != 1 {
		t.Fatalf("duplicate created=%v err=%v reads=%d", created, err, repo.rawIndexReads)
	}
	conflict := dup
	conflict.Payload = json.RawMessage(`{"properties":{"t":2}}`)
	if _, _, err := e.IngestRaw(ctx, conflict); !errors.Is(err, model.ErrRawConflict) {
		t.Fatalf("conflicting retransmission: %v", err)
	}
}

type flakyDeleter struct {
	*local.Archive
	fail bool
}

func (a *flakyDeleter) DeleteObject(ctx context.Context, bucket, key string) error {
	if a.fail {
		return errors.New("object storage unavailable")
	}
	return a.Archive.DeleteObject(ctx, bucket, key)
}

// A file whose delete fails stays queued until the cleanup job removes it.
func TestDeleteObjectLaterQueuesFailedDeletes(t *testing.T) {
	ctx := context.Background()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store := &flakyDeleter{Archive: archive, fail: true}
	repo := memory.NewRepository()
	e := New(repo, store, local.NewBus(), local.NewRealtime(), nil, nil)
	if _, err = archive.PutObject(ctx, "b", "k", strings.NewReader("x"), 1, "text/plain"); err != nil {
		t.Fatal(err)
	}
	e.DeleteObjectLater(ctx, "b", "k")
	if err = e.CleanupObjectsOnce(ctx); err == nil {
		t.Fatal("a failing delete must be reported")
	}
	if refs, _ := repo.PendingObjectCleanups(ctx, 10); len(refs) != 1 {
		t.Fatalf("failed delete not queued: %+v", refs)
	}
	store.fail = false
	if err = e.CleanupObjectsOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if refs, _ := repo.PendingObjectCleanups(ctx, 10); len(refs) != 0 {
		t.Fatalf("cleanup left %+v", refs)
	}
	if _, err = archive.GetObject(ctx, "b", "k"); err == nil {
		t.Fatal("file not deleted")
	}
}

// Parsed values are checked against the product's thing model: mismatches are
// tagged and counted, and the message is still forwarded. Products without a
// thing model are not checked.
func TestThingModelQualityIsMarkedNotRejected(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bus := &recordingBus{Bus: local.NewBus()}
	registry := metrics.New()
	e := New(repo, archive, bus, local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	e.Metrics = registry
	minimum, maximum := -20.0, 120.0
	if err = repo.SaveProduct(ctx, model.Product{ID: "json_modeled", TenantID: "t1", ThingModel: &model.ThingModel{Properties: []model.ThingField{
		{Identifier: "temperature", DataType: "number", Min: &minimum, Max: &maximum},
		{Identifier: "count", DataType: "integer"},
		{Identifier: "smoke", DataType: "boolean"},
	}}}); err != nil {
		t.Fatal(err)
	}
	var parsed []model.StandardMessage
	if err = e.Bus.Subscribe(ctx, model.TopicDeviceBusiness, "quality-test", func(_ context.Context, b []byte) error {
		var msg model.StandardMessage
		_ = json.Unmarshal(b, &msg)
		parsed = append(parsed, msg)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err = e.Start(ctx); err != nil {
		t.Fatal(err)
	}
	for i, tc := range []struct {
		product, payload, want string
	}{
		{"json_modeled", `{"properties":{"temperature":23,"count":2,"smoke":false}}`, ""},
		{"json_modeled", `{"properties":{"temperature":300,"count":2.5,"smoke":"yes","extra":1}}`, "range:temperature,type:count,type:smoke,unknown:extra"},
		{"json_plain", `{"properties":{"temperature":300}}`, ""},
	} {
		raw := model.RawMessage{MessageID: fmt.Sprintf("raw_quality_%d", i), TenantID: "t1", ProductID: tc.product, DeviceID: "device_quality", Protocol: "json", PayloadFormat: "json", ReceivedAt: int64(1000 + i), Payload: json.RawMessage(tc.payload)}
		if _, _, err = e.IngestRaw(ctx, raw); err != nil {
			t.Fatal(err)
		}
		var got *model.StandardMessage
		for j := range parsed {
			if parsed[j].RawMessageID == raw.MessageID {
				got = &parsed[j]
			}
		}
		if got == nil {
			t.Fatalf("%s was not forwarded", raw.MessageID)
		}
		if got.Tags[QualityTag] != tc.want {
			t.Fatalf("%s quality=%q want %q", raw.MessageID, got.Tags[QualityTag], tc.want)
		}
	}
	if out := registry.Prometheus(); !strings.Contains(out, `parse_quality_total{reason="type"} 2`) || !strings.Contains(out, `parse_quality_total{reason="range"} 1`) {
		t.Fatalf("quality metrics missing:\n%s", out)
	}
}

// Verification copies the newest successful AI analysis; values sent with the
// verification request are ignored.
func TestVerifyAlarmSnapshotsTheAIAnalysis(t *testing.T) {
	ctx := context.Background()
	e, repo, _ := newBusinessEngine(t, nil)
	for _, id := range []string{"a-analysed", "a-failed", "a-none"} {
		if _, _, err := repo.UpsertAlarm(ctx, model.Alarm{ID: id, TenantID: "t1", RuleID: "r-" + id, DeviceID: "d1", AlarmType: "FIRE", Status: "ACTIVE", FirstTriggeredAt: 1, LastTriggeredAt: 1}); err != nil {
			t.Fatal(err)
		}
	}
	for _, analysis := range []model.AIAnalysis{
		{TenantID: "t1", AlarmID: "a-analysed", RiskLevel: "LOW", PromptVersion: "old", CreatedAt: 10},
		{TenantID: "t1", AlarmID: "a-analysed", KnowledgeScope: model.AlarmAnalysisWorkflowID, RiskLevel: "HIGH", PromptVersion: aiprompt.AlarmAnalysisVersion, CreatedAt: 20},
		{TenantID: "t1", AlarmID: "a-failed", RiskLevel: "HIGH", PromptVersion: aiprompt.AlarmFallbackVersion, CreatedAt: 20, Error: "timeout"},
	} {
		if err := repo.SaveAIAnalysis(ctx, analysis); err != nil {
			t.Fatal(err)
		}
	}
	forged := model.AlarmDisposition{Result: model.DispositionRealFire, AIRiskLevel: "CRITICAL", AIPromptVersion: "forged", AIAnalysisAt: 99}
	for id, want := range map[string]model.AlarmDisposition{
		"a-analysed": {AIRiskLevel: "HIGH", AIPromptVersion: aiprompt.AlarmAnalysisVersion, AIAnalysisAt: 20},
		"a-failed":   {},
		"a-none":     {},
	} {
		a, err := e.VerifyAlarm(ctx, "t1", id, forged, "operator")
		if err != nil {
			t.Fatal(err)
		}
		d := a.Disposition
		if d.AIRiskLevel != want.AIRiskLevel || d.AIPromptVersion != want.AIPromptVersion || d.AIAnalysisAt != want.AIAnalysisAt {
			t.Fatalf("%s snapshot %+v", id, d)
		}
	}
}

// New device states take their reporting timing from the device, then the
// template, then the defaults; ApplyDeviceTiming moves existing states to a
// changed configuration and their offline check time with it.
func TestDeviceReportingTimingComesFromTemplateAndDevice(t *testing.T) {
	ctx := context.Background()
	e, repo, _ := newBusinessEngine(t, nil)
	if err := e.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveProduct(ctx, model.Product{ID: "json_hydrant", TenantID: "t1", ReportIntervalSec: 3600, OfflineToleranceSec: 600}); err != nil {
		t.Fatal(err)
	}
	for _, d := range []model.ManagedDevice{
		{ID: "hydrant-1", TenantID: "t1", ProductID: "json_hydrant", AccessKey: "ak-h1"},
		{ID: "hydrant-2", TenantID: "t1", ProductID: "json_hydrant", AccessKey: "ak-h2", ReportIntervalSec: 60},
		{ID: "plain-1", TenantID: "t1", ProductID: "json_plain", AccessKey: "ak-p1"},
	} {
		if err := repo.SaveManagedDevice(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	for i, d := range []struct{ product, device string }{{"json_hydrant", "hydrant-1"}, {"json_hydrant", "hydrant-2"}, {"json_plain", "plain-1"}} {
		// Current timestamps keep the engine's offline scan from marking the
		// devices offline while the test runs.
		raw := model.RawMessage{MessageID: fmt.Sprintf("raw_timing_%d", i), TenantID: "t1", ProductID: d.product, DeviceID: d.device, Protocol: "json", PayloadFormat: "json", ReceivedAt: time.Now().UnixMilli(), Payload: json.RawMessage(`{"properties":{"pressure":0.3}}`)}
		if _, _, err := e.IngestRaw(ctx, raw); err != nil {
			t.Fatal(err)
		}
	}
	check := func(device string, interval, tolerance int64) {
		t.Helper()
		state, err := repo.GetDeviceState(ctx, "t1", device)
		if err != nil || state.ReportIntervalSec != interval || state.OfflineToleranceSec != tolerance {
			t.Fatalf("%s timing %d/%d err=%v, want %d/%d", device, state.ReportIntervalSec, state.OfflineToleranceSec, err, interval, tolerance)
		}
		if state.OfflineDeadline() != state.LastSeenAt+(interval+tolerance)*1000 {
			t.Fatalf("%s offline deadline %d", device, state.OfflineDeadline())
		}
	}
	check("hydrant-1", 3600, 600)
	check("hydrant-2", 60, 600)
	check("plain-1", model.DefaultReportIntervalSec, model.DefaultOfflineToleranceSec)
	product, _ := repo.GetProduct(ctx, "t1", "json_hydrant")
	product.ReportIntervalSec = 1800
	if err := repo.SaveProduct(ctx, product); err != nil {
		t.Fatal(err)
	}
	if changed, err := e.ApplyDeviceTiming(ctx, "t1", "json_hydrant", ""); err != nil || changed != 1 {
		t.Fatalf("changed=%d err=%v", changed, err)
	}
	check("hydrant-1", 1800, 600)
	check("hydrant-2", 60, 600)
	if err := model.ValidateDeviceTiming(5, 0); err == nil {
		t.Fatal("a 5 second interval was accepted")
	}
}
