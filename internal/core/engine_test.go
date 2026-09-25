package core /* 声明 core 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"       /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"errors"        /* 执行当前语句并推进处理流程。 */
	"io"            /* 执行当前语句并推进处理流程。 */
	"log/slog"      /* 执行当前语句并推进处理流程。 */
	"testing"       /* 执行当前语句并推进处理流程。 */
	"time"          /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/adapters/knowledge" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/local"     /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/memory"    /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/aitest"
	"iot-platform/internal/model"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports"  /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type recordingBus struct { /* 定义 recordingBus 类型。 */
	*local.Bus          /* 执行当前语句并推进处理流程。 */
	topics     []string /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (b *recordingBus) Publish(ctx context.Context, topic, key string, payload []byte) error { /* 定义 Publish 函数。 */
	b.topics = append(b.topics, topic)             /* 更新 b.topics 的值。 */
	return b.Bus.Publish(ctx, topic, key, payload) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func hasTopic(topics []string, wanted string) bool { /* 定义 hasTopic 函数。 */
	for _, topic := range topics { /* 循环处理当前数据。 */
		if topic == wanted { /* 判断条件并选择处理分支。 */
			return true /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return false /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func TestParsedMessageFanoutRequiresSuccessfulParsing(t *testing.T) { /* 定义 TestParsedMessageFanoutRequiresSuccessfulParsing 函数。 */
	ctx := context.Background()                   /* 更新 ctx 的值。 */
	repo := memory.NewRepository()                /* 更新 repo 的值。 */
	archive, err := local.NewArchive(t.TempDir()) /* 更新 err 的值。 */
	if err != nil {                               /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	bus := &recordingBus{Bus: local.NewBus()}                                                                                       /* 更新 bus 的值。 */
	realtime := local.NewRealtime()                                                                                                 /* 更新 realtime 的值。 */
	e := New(repo, archive, bus, realtime, parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil))) /* 更新 e 的值。 */
	if err = e.Start(ctx); err != nil {                                                                                             /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	normal := model.RawMessage{MessageID: "raw_fanout_normal", TenantID: "t1", ProductID: "json_sensor", DeviceID: "device_fanout", Protocol: "json", PayloadFormat: "json", ReceivedAt: 1000, Payload: json.RawMessage(`{"properties":{"temperature":23}}`)} /* 更新 normal 的值。 */
	if _, _, err = e.IngestRaw(ctx, normal); err != nil {                                                                                                                                                                                                     /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if !hasTopic(bus.topics, model.TopicRaw) || !hasTopic(bus.topics, model.TopicPropertyReport) { /* 判断条件并选择处理分支。 */
		t.Fatalf("normal parsed message did not reach Kafka topics: %#v", bus.topics) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	foundParsedMQTT := false                      /* 更新 foundParsedMQTT 的值。 */
	for _, published := range realtime.Messages { /* 循环处理当前数据。 */
		if published.Topic == "/iot/parsed/t1/json_sensor/device_fanout/PROPERTY_REPORT" { /* 判断条件并选择处理分支。 */
			foundParsedMQTT = true /* 更新 foundParsedMQTT 的值。 */
			break                  /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if !foundParsedMQTT { /* 判断条件并选择处理分支。 */
		t.Fatalf("normal parsed message did not reach MQTT: %#v", realtime.Messages) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	event := model.RawMessage{MessageID: "raw_fanout_event", TenantID: "t1", ProductID: "json_sensor", DeviceID: "device_fanout", Protocol: "json", PayloadFormat: "json", ReceivedAt: 1001, Payload: json.RawMessage(`{"event":{"type":"FAULT"}}`)} /* 更新 event 的值。 */
	if _, _, err = e.IngestRaw(ctx, event); err != nil {                                                                                                                                                                                             /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if !hasTopic(bus.topics, model.TopicEventReport) { /* 判断条件并选择处理分支。 */
		t.Fatalf("event parsed message did not reach Kafka event topic: %#v", bus.topics) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	failure := model.RawMessage{MessageID: "raw_fanout_failure", TenantID: "t1", ProductID: "json_sensor", DeviceID: "device_fanout", Protocol: "json", PayloadFormat: "json", ReceivedAt: 1002, Payload: json.RawMessage(`[]`)} /* 更新 failure 的值。 */
	before := len(realtime.Messages)                                                                                                                                                                                             /* 更新 before 的值。 */
	if _, _, err = e.IngestRaw(ctx, failure); err != nil {                                                                                                                                                                       /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	idx, indexErr := repo.GetRawIndex(ctx, failure.TenantID, failure.MessageID) /* 更新 indexErr 的值。 */
	if indexErr != nil || idx.ParseError == "" || idx.ParseAttemptedAt == 0 {   /* 判断条件并选择处理分支。 */
		t.Fatal("parse failure not persisted", idx, indexErr) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if len(realtime.Messages) != before || hasTopic(bus.topics, model.TopicParseFailed) { /* 判断条件并选择处理分支。 */
		t.Fatalf("parse failure was forwarded: topics=%#v realtime=%#v", bus.topics, realtime.Messages) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestRawToAlarmPipeline(t *testing.T) { /* 定义 TestRawToAlarmPipeline 函数。 */
	ctx := context.Background()                   /* 更新 ctx 的值。 */
	repo := memory.NewRepository()                /* 更新 repo 的值。 */
	archive, err := local.NewArchive(t.TempDir()) /* 更新 err 的值。 */
	if err != nil {                               /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	bus := local.NewBus()                                                                                                           /* 更新 bus 的值。 */
	realtime := local.NewRealtime()                                                                                                 /* 更新 realtime 的值。 */
	e := New(repo, archive, bus, realtime, parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil))) /* 更新 e 的值。 */
	e.AIWorkflows = &aitest.Workflows{Answer: func(ports.AIWorkflowRequest) (string, error) { return analysisAnswer, nil }}
	e.HarnessTokens = aitest.Tokens()
	e.KB = knowledge.NewLocal()         /* 更新 e.KB 的值。 */
	if err = e.Start(ctx); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err = repo.SaveManagedDevice(ctx, model.ManagedDevice{ID: "device_1", TenantID: "t1", ProductID: "json_sensor", Name: "一号烟感", Status: "ENABLED", AccessKey: "device-1-key"}); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err = repo.SaveVideoCameraMapping(ctx, model.VideoCameraMapping{TenantID: "t1", CameraID: "camera-001", CameraName: "一号摄像头", Brand: "海康", CameraPoint: "东侧入口", DeviceID: "device_1", Building: "A", Floor: "1", Room: "大厅", Enabled: true}); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	rule := model.AlarmRule{ID: "r1", TenantID: "t1", Name: "高温烟雾", AlarmType: "FIRE", Level: "HIGH", Enabled: true, Conditions: []model.RuleCondition{{Field: "temperature", Operator: ">", Value: 80}, {Field: "smoke", Operator: "eq", Value: true}}, Actions: []model.RuleAction{{Type: "OPEN_CAMERA", CameraID: "camera-001"}}} /* 更新 rule 的值。 */
	if err = repo.SaveRule(ctx, rule); err != nil {                                                                                                                                                                                                                                                                                  /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	raw := model.RawMessage{MessageID: "raw_1", TenantID: "t1", ProductID: "json_sensor", DeviceID: "device_1", Protocol: "json", PayloadFormat: "json", ReceivedAt: time.Now().UnixMilli(), Payload: json.RawMessage(`{"properties":{"temperature":90,"smoke":true},"tags":{"cityCode":"city","districtCode":"d","buildingId":"b","deviceType":"smoke"}}`)} /* 更新 raw 的值。 */
	if _, created, err := e.IngestRaw(ctx, raw); err != nil || !created {                                                                                                                                                                                                                                                                                    /* 判断条件并选择处理分支。 */
		t.Fatalf("ingest created=%v err=%v", created, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	alarms, err := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "t1", Status: "ACTIVE"}) /* 更新 err 的值。 */
	if err != nil || len(alarms) != 1 {                                                      /* 判断条件并选择处理分支。 */
		t.Fatalf("alarms=%v err=%v", alarms, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if alarms[0].TriggerCount != 1 { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected alarm %#v", alarms[0]) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if alarms[0].DeviceName != "一号烟感" { /* 判断条件并选择处理分支。 */
		t.Fatalf("alarm did not preserve the managed device name: %#v", alarms[0]) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if len(alarms[0].Cameras) != 1 || alarms[0].Cameras[0].CameraID != "camera-001" || alarms[0].Cameras[0].Brand != "海康" { /* 判断条件并选择处理分支。 */
		t.Fatalf("alarm did not resolve associated camera metadata: %#v", alarms[0].Cameras) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	state, err := repo.GetDeviceState(ctx, "t1", "device_1") /* 更新 err 的值。 */
	if err != nil || state.BusinessStatus != "ALARM" {       /* 判断条件并选择处理分支。 */
		t.Fatalf("matching alarm did not update device business status: state=%#v err=%v", state, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, created, err := e.IngestRaw(ctx, raw); err != nil || created { /* 判断条件并选择处理分支。 */
		t.Fatalf("duplicate created=%v err=%v", created, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, err = repo.GetAIAnalysis(ctx, alarms[0].TenantID, alarms[0].ID, model.AIAnalysisScopeNone); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatalf("ai analysis not saved: %v", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if len(realtime.Messages) < 2 { /* 判断条件并选择处理分支。 */
		t.Fatalf("expected state and alarm realtime messages, got %d", len(realtime.Messages)) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	foundAction := false                          /* 更新 foundAction 的值。 */
	for _, published := range realtime.Messages { /* 循环处理当前数据。 */
		if published.Topic != "/iot/ui-action/t1" { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		var event model.UIActionEvent                                    /* 声明 event。 */
		if err = json.Unmarshal(published.Payload, &event); err != nil { /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		foundAction = event.RuleID == "r1" && event.Action.Type == "OPEN_CAMERA" && event.Action.CameraID == "camera-001" /* 更新 foundAction 的值。 */
	} /* 结束当前表达式或代码块。 */
	if !foundAction { /* 判断条件并选择处理分支。 */
		t.Fatalf("expected validated UI action event, messages=%#v", realtime.Messages) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestAnalyzeAlarmPersistsReadableFallbackOnProviderError(t *testing.T) { /* 定义 TestAnalyzeAlarmPersistsReadableFallbackOnProviderError 函数。 */
	ctx := context.Background()                   /* 更新 ctx 的值。 */
	repo := memory.NewRepository()                /* 更新 repo 的值。 */
	archive, err := local.NewArchive(t.TempDir()) /* 更新 err 的值。 */
	if err != nil {                               /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	e := New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil))) /* 更新 e 的值。 */
	e.AIWorkflows = &aitest.Workflows{Answer: func(ports.AIWorkflowRequest) (string, error) { return "", errors.New("provider response invalid") }}
	e.HarnessTokens = aitest.Tokens()
	if _, _, err = repo.UpsertAlarm(ctx, model.Alarm{ID: "alarm-ai-failure", TenantID: "t1", DeviceID: "device-1", AlarmType: "FIRE", AlarmLevel: "HIGH", Status: "ACTIVE", LastTriggeredAt: time.Now().UnixMilli()}); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

	analysis, err := e.AnalyzeAlarm(aitest.Context(ctx), "t1", "alarm-ai-failure", false) /* 更新 err 的值。 */
	if err != nil {                                                                       /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if analysis.Summary != "AI 研判暂时失败，已保留告警供人工研判。" || analysis.Model != "unavailable" || analysis.Error == "" { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected readable fallback: %#v", analysis) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	saved, err := repo.GetAIAnalysis(ctx, "t1", "alarm-ai-failure", model.AIAnalysisScopeNone) /* 更新 err 的值。 */
	if err != nil {                                                                            /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if saved.Summary == "" || saved.Model == "" { /* 判断条件并选择处理分支。 */
		t.Fatalf("saved fallback is not renderable: %#v", saved) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestGatewayAutomaticallyRegistersChildDevice(t *testing.T) { /* 定义 TestGatewayAutomaticallyRegistersChildDevice 函数。 */
	ctx := context.Background()                   /* 更新 ctx 的值。 */
	repo := memory.NewRepository()                /* 更新 repo 的值。 */
	archive, err := local.NewArchive(t.TempDir()) /* 更新 err 的值。 */
	if err != nil {                               /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	e := New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil))) /* 更新 e 的值。 */
	if err = e.Start(ctx); err != nil {                                                                                                                   /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err = repo.SaveProduct(ctx, model.Product{ID: "gateway_product", TenantID: "t1", Name: "网关产品", Category: "gateway", Status: "ENABLED"}); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err = repo.SaveProduct(ctx, model.Product{ID: "sensor_product", TenantID: "t1", Name: "传感器产品", Category: "sensor", Status: "ENABLED"}); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err = repo.SaveManagedDevice(ctx, model.ManagedDevice{ID: "gateway_1", TenantID: "t1", ProductID: "gateway_product", Name: "一号网关", Status: "ENABLED", DeviceRole: "GATEWAY", AccessKey: "dk_gateway", SecretHash: "unused"}); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	raw := model.RawMessage{MessageID: "raw_child_1", TenantID: "t1", ProductID: "sensor_product", DeviceID: "child_1", DeviceName: "一号烟感", GatewayID: "gateway_1", Protocol: "json", PayloadFormat: "json", Payload: json.RawMessage(`{"properties":{"temperature":25.5}}`)} /* 更新 raw 的值。 */
	if _, created, ingestErr := e.IngestRaw(ctx, raw); ingestErr != nil || !created {                                                                                                                                                                                         /* 判断条件并选择处理分支。 */
		t.Fatalf("ingest created=%v err=%v", created, ingestErr) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	child, err := repo.GetManagedDevice(ctx, "t1", "child_1") /* 更新 err 的值。 */
	if err != nil {                                           /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if child.DeviceRole != "CHILD" || child.GatewayID != "gateway_1" || !child.AutoRegistered || child.RegistrationSource != "GATEWAY_AUTO" || child.ProductID != "sensor_product" { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected child registration %#v", child) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestStateChangeIsStoredAsStandardMessage(t *testing.T) { /* 定义 TestStateChangeIsStoredAsStandardMessage 函数。 */
	repo := memory.NewRepository()                /* 更新 repo 的值。 */
	archive, err := local.NewArchive(t.TempDir()) /* 更新 err 的值。 */
	if err != nil {                               /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	bus := local.NewBus()                                                                                                                      /* 更新 bus 的值。 */
	e := New(repo, archive, bus, local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil))) /* 更新 e 的值。 */
	ctx, cancel := context.WithCancel(context.Background())                                                                                    /* 更新 cancel 的值。 */
	defer cancel()                                                                                                                             /* 安排函数结束时执行清理。 */
	if err := e.Start(ctx); err != nil {                                                                                                       /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	raw := model.RawMessage{MessageID: "raw_state_change", TenantID: "t1", ProductID: "json_sensor", DeviceID: "state_device", Protocol: "json", PayloadFormat: "json", Payload: json.RawMessage(`{"businessStatus":"ONLINE"}`)} /* 更新 raw 的值。 */
	if _, _, err := e.IngestRaw(ctx, raw); err != nil {                                                                                                                                                                          /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	deadline := time.Now().Add(time.Second) /* 更新 deadline 的值。 */
	for time.Now().Before(deadline) {       /* 循环处理当前数据。 */
		if message, getErr := repo.GetStandardMessageByRaw(ctx, "t1", raw.MessageID); getErr == nil { /* 判断条件并选择处理分支。 */
			if message.MessageType != model.StateChange { /* 判断条件并选择处理分支。 */
				t.Fatalf("unexpected message type: %s", message.MessageType) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			return /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		time.Sleep(10 * time.Millisecond) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	t.Fatal("state-change standard message was not stored") /* 验证实际结果符合预期。 */
} /* 结束当前表达式或代码块。 */

func TestAlarmCannotBeAcknowledgedTwice(t *testing.T) { /* 定义 TestAlarmCannotBeAcknowledgedTwice 函数。 */
	ctx := context.Background()                   /* 更新 ctx 的值。 */
	repo := memory.NewRepository()                /* 更新 repo 的值。 */
	archive, err := local.NewArchive(t.TempDir()) /* 更新 err 的值。 */
	if err != nil {                               /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	e := New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))       /* 更新 e 的值。 */
	alarm := model.Alarm{ID: "alarm_ack_once", TenantID: "t1", DeviceID: "device_1", AlarmType: "FIRE", AlarmLevel: "HIGH", Status: "ACTIVE", Source: "device"} /* 更新 alarm 的值。 */
	if err = repo.UpsertDeviceState(ctx, model.DeviceState{TenantID: "t1", ProductID: "sensor", DeviceID: "device_1", BusinessStatus: "ONLINE"}); err != nil {  /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, _, err = repo.UpsertAlarm(ctx, alarm); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

	first, err := e.SetAlarmStatus(ctx, "t1", alarm.ID, "ACKED", "operator") /* 更新 err 的值。 */
	if err != nil {                                                          /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if first.Status != "ACKED" { /* 判断条件并选择处理分支。 */
		t.Fatalf("first acknowledgement did not update status: %#v", first) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	state, err := repo.GetDeviceState(ctx, "t1", "device_1") /* 更新 err 的值。 */
	if err != nil || state.BusinessStatus != "ALARM" {       /* 判断条件并选择处理分支。 */
		t.Fatalf("acknowledged alarm did not keep device in ALARM state: state=%#v err=%v", state, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, err = e.SetAlarmStatus(ctx, "t1", alarm.ID, "ACKED", "operator"); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("second acknowledgement should be rejected") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

	persisted, err := repo.GetAlarm(ctx, "t1", alarm.ID) /* 更新 err 的值。 */
	if err != nil {                                      /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if persisted.Status != "ACKED" || persisted.AckedAt != first.AckedAt { /* 判断条件并选择处理分支。 */
		t.Fatalf("second acknowledgement changed the alarm: %#v", persisted) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, err = e.SetAlarmStatus(ctx, "t1", alarm.ID, "CLOSED", "operator"); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	state, err = repo.GetDeviceState(ctx, "t1", "device_1") /* 更新 err 的值。 */
	if err != nil || state.BusinessStatus != "ONLINE" {     /* 判断条件并选择处理分支。 */
		t.Fatalf("closed alarm did not return device to ONLINE state: state=%#v err=%v", state, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestLateMessageCannotRollBackDeviceConnectivity(t *testing.T) { /* 定义 TestLateMessageCannotRollBackDeviceConnectivity 函数。 */
	repo := memory.NewRepository()                                                                                                                      /* 更新 repo 的值。 */
	ctx := context.Background()                                                                                                                         /* 更新 ctx 的值。 */
	state := model.DeviceState{TenantID: "t", DeviceID: "d", ProductID: "p", LastSeenAt: 2000, ConnectionStatus: "CONNECTED", BusinessStatus: "ONLINE"} /* 更新 state 的值。 */
	if err := repo.UpsertDeviceState(ctx, state); err != nil {                                                                                          /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	e := New(repo, nil, local.NewBus(), local.NewRealtime(), nil, nil)                                                                                                                                                              /* 更新 e 的值。 */
	late := model.StandardMessage{TenantID: "t", DeviceID: "d", ProductID: "p", Timestamp: 1000, Parser: parser.StandardParserName, MessageType: model.StateChange, Properties: map[string]any{"connectionStatus": "DISCONNECTED"}} /* 更新 late 的值。 */
	if err := e.touchState(ctx, late); err != nil {                                                                                                                                                                                 /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	actual, _ := repo.GetDeviceState(ctx, "t", "d")                          /* 更新 _ 的值。 */
	if actual.LastSeenAt != 2000 || actual.ConnectionStatus != "CONNECTED" { /* 判断条件并选择处理分支。 */
		t.Fatal("late message rolled back status", actual) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
