package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"archive/zip"       /* 执行当前语句并推进处理流程。 */
	"bytes"             /* 执行当前语句并推进处理流程。 */
	"context"           /* 执行当前语句并推进处理流程。 */
	"encoding/json"     /* 执行当前语句并推进处理流程。 */
	"io"                /* 执行当前语句并推进处理流程。 */
	"log/slog"          /* 执行当前语句并推进处理流程。 */
	"mime/multipart"    /* 执行当前语句并推进处理流程。 */
	"net/http"          /* 执行当前语句并推进处理流程。 */
	"net/http/httptest" /* 执行当前语句并推进处理流程。 */
	"strings"           /* 执行当前语句并推进处理流程。 */
	"testing"           /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/adapters/local"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/memory" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/config"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/core"            /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/metrics"         /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"           /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser"          /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestProtocolAssistantExcelUploadDoesNotRequireAI(t *testing.T) { /* 定义 TestProtocolAssistantExcelUploadDoesNotRequireAI 函数。 */
	repo := memory.NewRepository()                /* 更新 repo 的值。 */
	archive, err := local.NewArchive(t.TempDir()) /* 更新 err 的值。 */
	if err != nil {                               /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.ModbusCoilParser{}), slog.New(slog.NewTextHandler(io.Discard, nil))) /* 更新 engine 的值。 */
	engine.Metrics = metrics.New()                                                                                                                                                          /* 更新 engine.Metrics 的值。 */
	if err = engine.Start(context.Background()); err != nil {                                                                                                                               /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	cfg := config.Load()                                                                                                                                                                                 /* 更新 cfg 的值。 */
	cfg.DevMode = true                                                                                                                                                                                   /* 更新 cfg.DevMode 的值。 */
	cfg.JWTSecret = "test-secret-at-least-32-characters"                                                                                                                                                 /* 更新 cfg.JWTSecret 的值。 */
	api := New(cfg, engine, engine.Metrics.(*metrics.Registry), slog.New(slog.NewTextHandler(io.Discard, nil)))                                                                                          /* 更新 api 的值。 */
	server := httptest.NewServer(api.Handler())                                                                                                                                                          /* 更新 server 的值。 */
	defer server.Close()                                                                                                                                                                                 /* 安排函数结束时执行清理。 */
	login := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/auth/login", "", map[string]any{"username": "admin", "password": "admin123", "tenantId": "tenant_001"}, http.StatusOK) /* 更新 login 的值。 */
	token := login["accessToken"].(string)                                                                                                                                                               /* 更新 token 的值。 */
	var body bytes.Buffer                                                                                                                                                                                /* 声明 body。 */
	writer := multipart.NewWriter(&body)                                                                                                                                                                 /* 更新 writer 的值。 */
	_ = writer.WriteField("name", "Excel 火花探测器")                                                                                                                                                         /* 更新 _ 的值。 */
	_ = writer.WriteField("transport", "MODBUS_TCP")                                                                                                                                                     /* 更新 _ 的值。 */
	_ = writer.WriteField("payloadFormat", "hex")                                                                                                                                                        /* 更新 _ 的值。 */
	file, _ := writer.CreateFormFile("file", "变量地址表.xlsx")                                                                                                                                               /* 更新 _ 的值。 */
	_, _ = file.Write(protocolAssistantXLSXFixture(t))                                                                                                                                                   /* 更新 _ 的值。 */
	_ = writer.Close()                                                                                                                                                                                   /* 更新 _ 的值。 */
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/ai/protocol-assistant/generate", &body)                                                                                           /* 更新 _ 的值。 */
	request.Header.Set("Authorization", "Bearer "+token)                                                                                                                                                 /* 执行当前语句并推进处理流程。 */
	request.Header.Set("Content-Type", writer.FormDataContentType())                                                                                                                                     /* 执行当前语句并推进处理流程。 */
	response, err := server.Client().Do(request)                                                                                                                                                         /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                      /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	defer response.Body.Close()                                                                       /* 安排函数结束时执行清理。 */
	var draft model.ProtocolAssistantDraft                                                            /* 声明 draft。 */
	if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&draft) != nil { /* 判断条件并选择处理分支。 */
		t.Fatalf("Excel generate status=%d", response.StatusCode) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if draft.ParserType != parser.ModbusCoilParserName || len(draft.Fields) != 2 || draft.Source != "" { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected Excel draft %#v", draft) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func protocolAssistantXLSXFixture(t *testing.T) []byte { /* 定义 protocolAssistantXLSXFixture 函数。 */
	t.Helper()                                       /* 执行当前语句并推进处理流程。 */
	var data bytes.Buffer                            /* 声明 data。 */
	zw := zip.NewWriter(&data)                       /* 更新 zw 的值。 */
	shared, err := zw.Create("xl/sharedStrings.xml") /* 更新 err 的值。 */
	if err != nil {                                  /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	_, _ = shared.Write([]byte(`<sst><si><t>序号</t></si><si><t>变量名称</t></si><si><t>PLC 线圈地址</t></si><si><t>Modbus地址（十进制）</t></si><si><t>数据类型</t></si><si><t>无报出状态</t></si><si><t>报出状态</t></si><si><t>备注</t></si><si><t>通讯心跳测试</t></si><si><t>M100</t></si><si><t>BOOL</t></si><si><t>火花探测组1报警</t></si><si><t>M3001</t></si></sst>`)) /* 更新 _ 的值。 */
	sheet, err := zw.Create("xl/worksheets/sheet1.xml")                                                                                                                                                                                                                                                                             /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                                                                                                                                                 /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	_, _ = sheet.Write([]byte(`<worksheet><sheetData><row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c><c r="C1" t="s"><v>2</v></c><c r="D1" t="s"><v>3</v></c><c r="E1" t="s"><v>4</v></c><c r="F1" t="s"><v>5</v></c><c r="G1" t="s"><v>6</v></c><c r="H1" t="s"><v>7</v></c></row><row r="2"><c r="A2"><v>1</v></c><c r="B2" t="s"><v>8</v></c><c r="C2" t="s"><v>9</v></c><c r="D2"><v>100</v></c><c r="E2" t="s"><v>10</v></c><c r="F2"><v>0</v></c><c r="G2"><v>1</v></c></row><row r="3"><c r="A3"><v>2</v></c><c r="B3" t="s"><v>11</v></c><c r="C3" t="s"><v>12</v></c><c r="D3"><v>3001</v></c><c r="E3" t="s"><v>10</v></c><c r="F3"><v>0</v></c><c r="G3"><v>1</v></c></row></sheetData></worksheet>`)) /* 更新 _ 的值。 */
	if err := zw.Close(); err != nil {                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	return data.Bytes() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

type protocolEndpointAI struct{} /* 定义 protocolEndpointAI 类型。 */

func (protocolEndpointAI) AnalyzeAlarm(context.Context, model.Alarm, []map[string]any, []string) (model.AIAnalysis, error) { /* 定义 AnalyzeAlarm 函数。 */
	return model.AIAnalysis{}, nil /* 返回当前处理结果。 */
}                                                                               /* 结束当前表达式或代码块。 */
func (protocolEndpointAI) Chat(context.Context, string, string) (string, error) { return "ok", nil } /* 定义 Chat 函数。 */
func (protocolEndpointAI) RuleDraft(context.Context, string, string) (model.AlarmRule, error) { /* 定义 RuleDraft 函数。 */
	return model.AlarmRule{ /* 返回当前处理结果。 */
		Name:        "AI 高温烟雾规则",            /* 执行当前语句并推进处理流程。 */
		Description: "温度持续过高且烟雾信号出现时提示人工处置", /* 执行当前语句并推进处理流程。 */
		AlarmType:   "FIRE_RISK",            /* 执行当前语句并推进处理流程。 */
		Level:       "HIGH",                 /* 执行当前语句并推进处理流程。 */
		Match:       "all",                  /* 执行当前语句并推进处理流程。 */
		Conditions: []model.RuleCondition{ /* 执行当前语句并推进处理流程。 */
			{Field: "properties.temperature", Operator: ">", Value: 80}, /* 执行当前语句并推进处理流程。 */
			{Field: "properties.smoke", Operator: "eq", Value: true},    /* 执行当前语句并推进处理流程。 */
		}, /* 结束当前表达式或代码块。 */
		Recovery: []model.RuleCondition{{Field: "properties.temperature", Operator: "lt", Value: 70}}, /* 执行当前语句并推进处理流程。 */
		Actions:  []model.RuleAction{{Type: "OPEN_PAGE", Page: "alarms"}},                             /* 执行当前语句并推进处理流程。 */
	}, nil /* 结束当前表达式或代码块。 */
}                                                       /* 结束当前表达式或代码块。 */
func (protocolEndpointAI) Health(context.Context) error { return nil } /* 定义 Health 函数。 */
func (protocolEndpointAI) GenerateJSON(context.Context, string, string, string) (string, error) { /* 定义 GenerateJSON 函数。 */
	return `{"name":"端点测试协议","protocol":"endpoint-modbus","transport":"MODBUS_TCP","payloadFormat":"hex","parserType":"modbus_coil_parser","messageType":"PROPERTY_REPORT","config":{"frame":"tcp","startAddress":0,"functionCode":1,"fields":[{"name":"smoke","coilAddress":0}]},"fields":[{"name":"smoke","label":"烟雾","type":"boolean","coilAddress":0,"dataType":"BOOL"}]}`, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func TestAIRuleDraftReturnsAnnotatedJSONAndCommentedGengine(t *testing.T) { /* 定义 TestAIRuleDraftReturnsAnnotatedJSONAndCommentedGengine 函数。 */
	repo := memory.NewRepository()                /* 更新 repo 的值。 */
	archive, err := local.NewArchive(t.TempDir()) /* 更新 err 的值。 */
	if err != nil {                               /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil))) /* 更新 engine 的值。 */
	installEndpointWorkflows(engine)
	engine.Metrics = metrics.New()                            /* 更新 engine.Metrics 的值。 */
	if err = engine.Start(context.Background()); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	cfg := config.Load()                                                                                                                                                                                 /* 更新 cfg 的值。 */
	cfg.DevMode = true                                                                                                                                                                                   /* 更新 cfg.DevMode 的值。 */
	cfg.JWTSecret = "test-secret-at-least-32-characters"                                                                                                                                                 /* 更新 cfg.JWTSecret 的值。 */
	api := New(cfg, engine, engine.Metrics.(*metrics.Registry), slog.New(slog.NewTextHandler(io.Discard, nil)))                                                                                          /* 更新 api 的值。 */
	server := httptest.NewServer(api.Handler())                                                                                                                                                          /* 更新 server 的值。 */
	defer server.Close()                                                                                                                                                                                 /* 安排函数结束时执行清理。 */
	login := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/auth/login", "", map[string]any{"username": "admin", "password": "admin123", "tenantId": "tenant_001"}, http.StatusOK) /* 更新 login 的值。 */
	token := login["accessToken"].(string)                                                                                                                                                               /* 更新 token 的值。 */
	result := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/rule-draft", token, map[string]any{"text": "温度超过 80 且烟雾出现"}, http.StatusOK)                                        /* 更新 result 的值。 */

	draft := result["draft"].(map[string]any)                                                         /* 更新 draft 的值。 */
	if draft["enabled"] != false || draft["expression"] != nil || draft["tenantId"] != "tenant_001" { /* 判断条件并选择处理分支。 */
		t.Fatalf("AI rule draft was not kept as a safe tenant draft: %#v", draft) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	presentation := result["presentation"].(map[string]any)                                        /* 更新 presentation 的值。 */
	var executableJSON map[string]any                                                              /* 声明 executableJSON。 */
	if err := json.Unmarshal([]byte(presentation["json"].(string)), &executableJSON); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatalf("presentation JSON is not executable JSON: %v", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if presentation["gengine"].(string) == "" || !strings.HasPrefix(strings.TrimSpace(presentation["genginePlaceholder"].(string)), "//") { /* 判断条件并选择处理分支。 */
		t.Fatalf("Gengine presentation is not an explicitly commented alternative: %#v", presentation) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	descriptions := presentation["fieldDescriptions"].([]any)                                                  /* 更新 descriptions 的值。 */
	needed := map[string]bool{"conditions[].field": false, "recovery[].value": false, "actions[].page": false} /* 更新 needed 的值。 */
	for _, item := range descriptions {                                                                        /* 循环处理当前数据。 */
		field := item.(map[string]any)["field"].(string) /* 更新 field 的值。 */
		if _, ok := needed[field]; ok {                  /* 判断条件并选择处理分支。 */
			needed[field] = true /* 更新 needed[field] 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	for field, found := range needed { /* 循环处理当前数据。 */
		if !found { /* 判断条件并选择处理分支。 */
			t.Fatalf("missing nested field description %q", field) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestProtocolAssistantEndpoints(t *testing.T) { /* 定义 TestProtocolAssistantEndpoints 函数。 */
	repo := memory.NewRepository()                /* 更新 repo 的值。 */
	archive, err := local.NewArchive(t.TempDir()) /* 更新 err 的值。 */
	if err != nil {                               /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.ModbusCoilParser{}), slog.New(slog.NewTextHandler(io.Discard, nil))) /* 更新 engine 的值。 */
	installEndpointWorkflows(engine)
	engine.Metrics = metrics.New()                            /* 更新 engine.Metrics 的值。 */
	if err = engine.Start(context.Background()); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	cfg := config.Load()                                                                                                                                                                                 /* 更新 cfg 的值。 */
	cfg.DevMode = true                                                                                                                                                                                   /* 更新 cfg.DevMode 的值。 */
	cfg.JWTSecret = "test-secret-at-least-32-characters"                                                                                                                                                 /* 更新 cfg.JWTSecret 的值。 */
	api := New(cfg, engine, engine.Metrics.(*metrics.Registry), slog.New(slog.NewTextHandler(io.Discard, nil)))                                                                                          /* 更新 api 的值。 */
	server := httptest.NewServer(api.Handler())                                                                                                                                                          /* 更新 server 的值。 */
	defer server.Close()                                                                                                                                                                                 /* 安排函数结束时执行清理。 */
	login := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/auth/login", "", map[string]any{"username": "admin", "password": "admin123", "tenantId": "tenant_001"}, http.StatusOK) /* 更新 login 的值。 */
	token := login["accessToken"].(string)                                                                                                                                                               /* 更新 token 的值。 */
	var body bytes.Buffer                                                                                                                                                                                /* 声明 body。 */
	writer := multipart.NewWriter(&body)                                                                                                                                                                 /* 更新 writer 的值。 */
	_ = writer.WriteField("pointTable", "smoke = M0")                                                                                                                                                    /* 更新 _ 的值。 */
	_ = writer.WriteField("transport", "MODBUS_TCP")                                                                                                                                                     /* 更新 _ 的值。 */
	_ = writer.WriteField("samplePayload", "00 01 00 00 00 04 01 01 01 01")                                                                                                                              /* 更新 _ 的值。 */
	file, _ := writer.CreateFormFile("file", "protocol.csv")                                                                                                                                             /* 更新 _ 的值。 */
	_, _ = file.Write([]byte("address,name\n0,smoke\n"))                                                                                                                                                 /* 更新 _ 的值。 */
	_ = writer.Close()                                                                                                                                                                                   /* 更新 _ 的值。 */
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/ai/protocol-assistant/generate", &body)                                                                                           /* 更新 _ 的值。 */
	request.Header.Set("Authorization", "Bearer "+token)                                                                                                                                                 /* 执行当前语句并推进处理流程。 */
	request.Header.Set("Content-Type", writer.FormDataContentType())                                                                                                                                     /* 执行当前语句并推进处理流程。 */
	response, err := server.Client().Do(request)                                                                                                                                                         /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                      /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	var draft model.ProtocolAssistantDraft                                                            /* 声明 draft。 */
	if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&draft) != nil { /* 判断条件并选择处理分支。 */
		response.Body.Close()                                                  /* 执行当前语句并推进处理流程。 */
		t.Fatalf("generate protocol assistant status=%d", response.StatusCode) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	response.Body.Close()                                                                                /* 执行当前语句并推进处理流程。 */
	if draft.Source != "" || draft.ParserType != parser.ModbusCoilParserName || len(draft.Fields) != 1 { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected generated draft %#v", draft) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	draft.Fields[0].Name = "smoke_alarm"                                                                                                                                                                                                      /* 更新 draft.Fields[0].Name 的值。 */
	draft.Config["fields"] = []any{map[string]any{"name": "smoke_alarm", "coilAddress": 0}}                                                                                                                                                   /* 执行当前语句并推进处理流程。 */
	preview := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/protocol-assistant/preview", token, map[string]any{"draft": draft, "payload": "00 01 00 00 00 04 01 01 01 01", "payloadFormat": "hex"}, http.StatusOK) /* 更新 preview 的值。 */
	if preview["success"] != true || preview["standardMessage"].(map[string]any)["properties"].(map[string]any)["smoke_alarm"] != true {                                                                                                      /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected preview %#v", preview) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/protocol-assistant/publish", token, map[string]any{"draft": draft, "status": "PUBLISHED"}, http.StatusUnprocessableEntity)                                                                  /* 执行当前语句并推进处理流程。 */
	published := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/protocol-assistant/publish", token, map[string]any{"draft": draft, "payload": "00 01 00 00 00 04 01 01 01 01", "payloadFormat": "hex", "status": "DRAFT"}, http.StatusCreated) /* 更新 published 的值。 */
	pkg := published["package"].(map[string]any)                                                                                                                                                                                                                        /* 更新 pkg 的值。 */
	if pkg["parserType"] != parser.ModbusCoilParserName || pkg["status"] != "DRAFT" {                                                                                                                                                                                   /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected published package %#v", pkg) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, ok := pkg["config"].(map[string]any)["fields"]; !ok { /* 判断条件并选择处理分支。 */
		t.Fatalf("published package did not persist edited field: %#v", pkg) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
