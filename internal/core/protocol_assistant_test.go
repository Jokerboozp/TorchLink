package core /* 声明 core 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"  /* 执行当前语句并推进处理流程。 */
	"io"       /* 执行当前语句并推进处理流程。 */
	"log/slog" /* 执行当前语句并推进处理流程。 */
	"testing"  /* 执行当前语句并推进处理流程。 */

	aiadapter "iot-platform/internal/adapters/ai" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/local"        /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/memory"       /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/aitest"
	"iot-platform/internal/model"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports"
) /* 结束当前表达式或代码块。 */

type protocolAssistantAI struct{} /* 定义 protocolAssistantAI 类型。 */

func (protocolAssistantAI) AnalyzeAlarm(context.Context, model.Alarm, []map[string]any, []string) (model.AIAnalysis, error) { /* 定义 AnalyzeAlarm 函数。 */
	return model.AIAnalysis{}, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (protocolAssistantAI) Chat(context.Context, string, string) (string, error) { /* 定义 Chat 函数。 */
	return "巡检建议", nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (protocolAssistantAI) RuleDraft(context.Context, string, string) (model.AlarmRule, error) { /* 定义 RuleDraft 函数。 */
	return model.AlarmRule{}, nil /* 返回当前处理结果。 */
}                                                        /* 结束当前表达式或代码块。 */
func (protocolAssistantAI) Health(context.Context) error { return nil } /* 定义 Health 函数。 */
func (protocolAssistantAI) GenerateJSON(context.Context, string, string, string) (string, error) { /* 定义 GenerateJSON 函数。 */
	return `{"name":"测试 Go 协议","protocol":"test-modbus","transport":"MODBUS_TCP","payloadFormat":"hex","parserType":"modbus_coil_parser","messageType":"PROPERTY_REPORT","config":{"frame":"tcp","startAddress":0,"functionCode":1,"fields":[{"name":"smoke","coilAddress":0}]},"fields":[{"name":"smoke","label":"烟雾","type":"boolean","coilAddress":0,"dataType":"BOOL"}]}`, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func TestProtocolAssistantBuildAndPreview(t *testing.T) { /* 定义 TestProtocolAssistantBuildAndPreview 函数。 */
	draft := model.ProtocolAssistantDraft{ParserType: parser.ModbusCoilParserName, Protocol: "modbus", Transport: "MODBUS_TCP", PayloadFormat: "hex", MessageType: model.PropertyReport, Config: map[string]any{"frame": "tcp", "startAddress": 0, "functionCode": 1, "fields": []any{map[string]any{"name": "smoke", "coilAddress": 0}}}} /* 更新 draft 的值。 */
	message, err := PreviewProtocolAssistant(draft, "tenant-test", "00 01 00 00 00 04 01 01 01 01")                                                                                                                                                                                                                                        /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                                                                                                                                                        /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if message.Properties["smoke"] != true || message.Parser != parser.ModbusCoilParserName { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected protocol preview %#v", message) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestProtocolAssistantRejectsInvalidGoMapping(t *testing.T) { /* 定义 TestProtocolAssistantRejectsInvalidGoMapping 函数。 */
	err := parser.ValidateModbusCoilConfig(map[string]any{"fields": []any{map[string]any{"name": "x", "coilAddress": -1}}}) /* 更新 err 的值。 */
	if err == nil {                                                                                                         /* 判断条件并选择处理分支。 */
		t.Fatal("invalid coil address was accepted") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestGenerateProtocolAssistant(t *testing.T) { /* 定义 TestGenerateProtocolAssistant 函数。 */
	repo := memory.NewRepository()                /* 更新 repo 的值。 */
	archive, err := local.NewArchive(t.TempDir()) /* 更新 err 的值。 */
	if err != nil {                               /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	engine := New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.ModbusCoilParser{}), slog.New(slog.NewTextHandler(io.Discard, nil))) /* 更新 engine 的值。 */
	engine.AIWorkflows = &aitest.Workflows{Answer: func(ports.AIWorkflowRequest) (string, error) {
		return protocolAssistantAI{}.GenerateJSON(context.Background(), "", "", "")
	}}
	engine.HarnessTokens = aitest.Tokens()
	draft, err := engine.GenerateProtocolAssistant(aitest.Context(context.Background()), "tenant-test", ProtocolAssistantInput{PointTable: "温度：第 2 字节，单位 0.1 度", SamplePayload: "01 2A"}) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                       /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if draft.Source != "" || draft.ParserType != parser.ModbusCoilParserName || len(draft.Fields) != 1 || draft.Preview != nil { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected generated draft %#v", draft) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestInspectDeviceHealth(t *testing.T) { /* 定义 TestInspectDeviceHealth 函数。 */
	repo := memory.NewRepository()                /* 更新 repo 的值。 */
	archive, err := local.NewArchive(t.TempDir()) /* 更新 err 的值。 */
	if err != nil {                               /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	engine := New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil))) /* 更新 engine 的值。 */
	engine.AI = aiadapter.NoopAI{}                                                                                                                             /* 更新 engine.AI 的值。 */
	ctx := context.Background()                                                                                                                                /* 更新 ctx 的值。 */
	if err = repo.SaveManagedDevice(ctx, model.ManagedDevice{ID: "device-health", TenantID: "tenant-test", ProductID: "sensor", Name: "测试设备"}); err != nil {   /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err = repo.UpsertDeviceState(ctx, model.DeviceState{TenantID: "tenant-test", ProductID: "sensor", DeviceID: "device-health", BusinessStatus: "OFFLINE", DataStatus: "SILENT", LastSeenAt: 1}); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, _, err = repo.UpsertAlarm(ctx, model.Alarm{ID: "alarm-health", TenantID: "tenant-test", DeviceID: "device-health", Status: "ACTIVE", AlarmLevel: "HIGH"}); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	report, err := engine.InspectDeviceHealth(ctx, "tenant-test") /* 更新 err 的值。 */
	if err != nil {                                               /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if report.Counts["total"] != 1 || report.Counts["offline"] != 1 || report.Items[0].ActiveAlarmCount != 1 { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected health report %#v", report) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
