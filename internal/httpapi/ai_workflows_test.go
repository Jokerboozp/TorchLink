package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
)

func TestProtocolAssistantExcelUploadDoesNotRequireAI(t *testing.T) {
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.ModbusCoilParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	engine.Metrics = metrics.New()
	if err = engine.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.DevMode = true
	cfg.JWTSecret = "test-secret-at-least-32-characters"
	api := New(cfg, engine, engine.Metrics.(*metrics.Registry), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	login := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/auth/login", "", map[string]any{"username": "admin", "password": "admin123", "tenantId": "tenant_001"}, http.StatusOK)
	token := login["accessToken"].(string)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("name", "Excel 火花探测器")
	_ = writer.WriteField("transport", "MODBUS_TCP")
	_ = writer.WriteField("payloadFormat", "hex")
	file, _ := writer.CreateFormFile("file", "变量地址表.xlsx")
	_, _ = file.Write(protocolAssistantXLSXFixture(t))
	_ = writer.Close()
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/ai/protocol-assistant/generate", &body)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var draft model.ProtocolAssistantDraft
	if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&draft) != nil {
		t.Fatalf("Excel generate status=%d", response.StatusCode)
	}
	if draft.ParserType != parser.ModbusCoilParserName || len(draft.Fields) != 2 || draft.Source != "" {
		t.Fatalf("unexpected Excel draft %#v", draft)
	}
}

func protocolAssistantXLSXFixture(t *testing.T) []byte {
	t.Helper()
	var data bytes.Buffer
	zw := zip.NewWriter(&data)
	shared, err := zw.Create("xl/sharedStrings.xml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = shared.Write([]byte(`<sst><si><t>序号</t></si><si><t>变量名称</t></si><si><t>PLC 线圈地址</t></si><si><t>Modbus地址（十进制）</t></si><si><t>数据类型</t></si><si><t>无报出状态</t></si><si><t>报出状态</t></si><si><t>备注</t></si><si><t>通讯心跳测试</t></si><si><t>M100</t></si><si><t>BOOL</t></si><si><t>火花探测组1报警</t></si><si><t>M3001</t></si></sst>`))
	sheet, err := zw.Create("xl/worksheets/sheet1.xml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = sheet.Write([]byte(`<worksheet><sheetData><row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c><c r="C1" t="s"><v>2</v></c><c r="D1" t="s"><v>3</v></c><c r="E1" t="s"><v>4</v></c><c r="F1" t="s"><v>5</v></c><c r="G1" t="s"><v>6</v></c><c r="H1" t="s"><v>7</v></c></row><row r="2"><c r="A2"><v>1</v></c><c r="B2" t="s"><v>8</v></c><c r="C2" t="s"><v>9</v></c><c r="D2"><v>100</v></c><c r="E2" t="s"><v>10</v></c><c r="F2"><v>0</v></c><c r="G2"><v>1</v></c></row><row r="3"><c r="A3"><v>2</v></c><c r="B3" t="s"><v>11</v></c><c r="C3" t="s"><v>12</v></c><c r="D3"><v>3001</v></c><c r="E3" t="s"><v>10</v></c><c r="F3"><v>0</v></c><c r="G3"><v>1</v></c></row></sheetData></worksheet>`))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

type protocolEndpointAI struct{}

func (protocolEndpointAI) AnalyzeAlarm(context.Context, model.Alarm, []map[string]any, []string) (model.AIAnalysis, error) {
	return model.AIAnalysis{}, nil
}
func (protocolEndpointAI) Chat(context.Context, string, string) (string, error) { return "ok", nil }
func (protocolEndpointAI) RuleDraft(context.Context, string, string) (model.AlarmRule, error) {
	return model.AlarmRule{
		Name:        "AI 高温烟雾规则",
		Description: "温度持续过高且烟雾信号出现时提示人工处置",
		AlarmType:   "FIRE_RISK",
		Level:       "HIGH",
		Match:       "all",
		Conditions: []model.RuleCondition{
			{Field: "properties.temperature", Operator: ">", Value: 80},
			{Field: "properties.smoke", Operator: "eq", Value: true},
		},
		Recovery: []model.RuleCondition{{Field: "properties.temperature", Operator: "lt", Value: 70}},
		Actions:  []model.RuleAction{{Type: "OPEN_PAGE", Page: "alarms"}},
	}, nil
}
func (protocolEndpointAI) Health(context.Context) error { return nil }
func (protocolEndpointAI) GenerateJSON(context.Context, string, string, string) (string, error) {
	return `{"name":"端点测试协议","protocol":"endpoint-modbus","transport":"MODBUS_TCP","payloadFormat":"hex","parserType":"modbus_coil_parser","messageType":"PROPERTY_REPORT","config":{"frame":"tcp","startAddress":0,"functionCode":1,"fields":[{"name":"smoke","coilAddress":0}]},"fields":[{"name":"smoke","label":"烟雾","type":"boolean","coilAddress":0,"dataType":"BOOL"}]}`, nil
}

func TestAIRuleDraftReturnsAnnotatedJSONAndCommentedGengine(t *testing.T) {
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	engine.AI = protocolEndpointAI{}
	engine.Metrics = metrics.New()
	if err = engine.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.DevMode = true
	cfg.JWTSecret = "test-secret-at-least-32-characters"
	api := New(cfg, engine, engine.Metrics.(*metrics.Registry), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	login := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/auth/login", "", map[string]any{"username": "admin", "password": "admin123", "tenantId": "tenant_001"}, http.StatusOK)
	token := login["accessToken"].(string)
	result := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/rule-draft", token, map[string]any{"text": "温度超过 80 且烟雾出现"}, http.StatusOK)

	draft := result["draft"].(map[string]any)
	if draft["enabled"] != false || draft["expression"] != nil || draft["tenantId"] != "tenant_001" {
		t.Fatalf("AI rule draft was not kept as a safe tenant draft: %#v", draft)
	}
	presentation := result["presentation"].(map[string]any)
	var executableJSON map[string]any
	if err := json.Unmarshal([]byte(presentation["json"].(string)), &executableJSON); err != nil {
		t.Fatalf("presentation JSON is not executable JSON: %v", err)
	}
	if presentation["gengine"].(string) == "" || !strings.HasPrefix(strings.TrimSpace(presentation["genginePlaceholder"].(string)), "//") {
		t.Fatalf("Gengine presentation is not an explicitly commented alternative: %#v", presentation)
	}
	descriptions := presentation["fieldDescriptions"].([]any)
	needed := map[string]bool{"conditions[].field": false, "recovery[].value": false, "actions[].page": false}
	for _, item := range descriptions {
		field := item.(map[string]any)["field"].(string)
		if _, ok := needed[field]; ok {
			needed[field] = true
		}
	}
	for field, found := range needed {
		if !found {
			t.Fatalf("missing nested field description %q", field)
		}
	}
}

func TestProtocolAssistantEndpoints(t *testing.T) {
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.ModbusCoilParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	engine.AI = protocolEndpointAI{}
	engine.Metrics = metrics.New()
	if err = engine.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.DevMode = true
	cfg.JWTSecret = "test-secret-at-least-32-characters"
	api := New(cfg, engine, engine.Metrics.(*metrics.Registry), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	login := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/auth/login", "", map[string]any{"username": "admin", "password": "admin123", "tenantId": "tenant_001"}, http.StatusOK)
	token := login["accessToken"].(string)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("pointTable", "smoke = M0")
	_ = writer.WriteField("transport", "MODBUS_TCP")
	_ = writer.WriteField("samplePayload", "00 01 00 00 00 04 01 01 01 01")
	file, _ := writer.CreateFormFile("file", "protocol.csv")
	_, _ = file.Write([]byte("address,name\n0,smoke\n"))
	_ = writer.Close()
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/ai/protocol-assistant/generate", &body)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var draft model.ProtocolAssistantDraft
	if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&draft) != nil {
		response.Body.Close()
		t.Fatalf("generate protocol assistant status=%d", response.StatusCode)
	}
	response.Body.Close()
	if draft.Source != "" || draft.ParserType != parser.ModbusCoilParserName || len(draft.Fields) != 1 {
		t.Fatalf("unexpected generated draft %#v", draft)
	}
	draft.Fields[0].Name = "smoke_alarm"
	draft.Config["fields"] = []any{map[string]any{"name": "smoke_alarm", "coilAddress": 0}}
	preview := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/protocol-assistant/preview", token, map[string]any{"draft": draft, "payload": "00 01 00 00 00 04 01 01 01 01", "payloadFormat": "hex"}, http.StatusOK)
	if preview["success"] != true || preview["standardMessage"].(map[string]any)["properties"].(map[string]any)["smoke_alarm"] != true {
		t.Fatalf("unexpected preview %#v", preview)
	}
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/protocol-assistant/publish", token, map[string]any{"draft": draft, "status": "PUBLISHED"}, http.StatusUnprocessableEntity)
	published := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/protocol-assistant/publish", token, map[string]any{"draft": draft, "payload": "00 01 00 00 00 04 01 01 01 01", "payloadFormat": "hex", "status": "DRAFT"}, http.StatusCreated)
	pkg := published["package"].(map[string]any)
	if pkg["parserType"] != parser.ModbusCoilParserName || pkg["status"] != "DRAFT" {
		t.Fatalf("unexpected published package %#v", pkg)
	}
	if _, ok := pkg["config"].(map[string]any)["fields"]; !ok {
		t.Fatalf("published package did not persist edited field: %#v", pkg)
	}
}
