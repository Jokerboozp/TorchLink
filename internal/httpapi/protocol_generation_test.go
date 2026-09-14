package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
)

func TestUploadedProtocolLifecycle(t *testing.T) {
	repo := memory.NewRepository()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Load()
	cfg.JWTSecret = "protocol-generation-test-32-chars"
	api := New(cfg, &core.Engine{Repo: repo, Parsers: parser.NewPlatformRegistry(t.TempDir())}, metrics.New(), log)
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	token, _ := api.auth.Issue("operator", "tenant", "operator", nil, time.Hour)
	viewer, _ := api.auth.Issue("viewer", "tenant", "viewer", nil, time.Hour)
	other, _ := api.auth.Issue("operator", "other", "operator", nil, time.Hour)
	upload := func(kind, filename string, data []byte, transport, format, auth string, status int) model.ProtocolAssistantDraft {
		t.Helper()
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		for k, v := range map[string]string{"inputKind": kind, "transport": transport, "payloadFormat": format, "name": "上传生成"} {
			_ = form.WriteField(k, v)
		}
		file, _ := form.CreateFormFile("file", filename)
		_, _ = file.Write(data)
		_ = form.Close()
		r, _ := http.NewRequest("POST", server.URL+"/api/v1/ai/protocol-assistant/generate", &body)
		r.Header.Set("Authorization", "Bearer "+auth)
		r.Header.Set("Content-Type", form.FormDataContentType())
		response, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		raw, _ := io.ReadAll(response.Body)
		if response.StatusCode != status {
			t.Fatalf("upload %s status %d: %s", filename, response.StatusCode, raw)
		}
		var draft model.ProtocolAssistantDraft
		if status == 200 {
			if err = json.Unmarshal(raw, &draft); err != nil {
				t.Fatal(err)
			}
		}
		return draft
	}
	draft := upload("sample", "report.json", []byte(`{"data":{"temperature":25.5,"smoke":false}}`), "MQTT", "json", token, 200)
	if draft.ParserType != "configurable_json_parser" || draft.Preview.Properties["temperature"] != 25.5 {
		t.Fatal(draft)
	}
	upload("sample", "report.json", []byte(`{"temperature":1}`), "MQTT", "json", viewer, 403)
	body := map[string]any{"id": "json-generated", "version": "1.0.0", "draft": draft, "payload": map[string]any{"data": map[string]any{"temperature": 31.5, "smoke": true}}}
	result := requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/ai/protocol-assistant/publish", token, body, 201)
	if result["release"].(map[string]any)["status"] != "VALIDATED" || result["standardMessage"].(map[string]any)["properties"].(map[string]any)["temperature"] != 31.5 {
		t.Fatal(result)
	}
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/ai/protocol-assistant/publish", token, body, 409)
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v2/protocols/json-generated/releases/1.0.0/publish", other, map[string]any{}, 404)
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v2/protocols/json-generated/releases/1.0.0/publish", token, map[string]any{}, 200)
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/products", token, map[string]any{"id": "generated-product", "name": "报文产品", "protocolPackageId": "json-generated@1.0.0"}, 201)
	if _, err := repo.GetProduct(context.Background(), "other", "generated-product"); err == nil {
		t.Fatal("cross tenant product")
	}
	table := []byte("identifier,name,functionCode,address,addressNotation,dataType,scale\ntemperature,温度,3,0,zero_based,uint16,0.1\n")
	draft = upload("point-table", "points.csv", table, "MODBUS_TCP", "hex", token, 200)
	body = map[string]any{"id": "points-generated", "version": "1.0.0", "draft": draft}
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/ai/protocol-assistant/publish", token, body, 201)
	path := server.URL + "/api/v2/protocols/points-generated/releases/1.0.0"
	requestJSON(t, server.Client(), "POST", path+"/publish", token, map[string]any{}, 422)
	requestJSON(t, server.Client(), "POST", path+"/preview", other, map[string]any{"payload": "bad"}, 404)
	requestJSON(t, server.Client(), "POST", path+"/preview", token, map[string]any{"payload": "00 01", "startAddress": 0}, 422)
	result = requestJSON(t, server.Client(), "POST", path+"/preview", token, map[string]any{"payload": "00 01 00 00 00 05 01 03 02 00 FA", "startAddress": 0}, 200)
	if result["standardMessage"].(map[string]any)["properties"].(map[string]any)["temperature"] != float64(25) {
		t.Fatal(result)
	}
	requestJSON(t, server.Client(), "POST", path+"/publish", token, map[string]any{}, 200)
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/products", token, map[string]any{"id": "points-product", "name": "点表产品", "protocolPackageId": "points-generated@1.0.0"}, 201)
	excel := upload("point-table", "points.xlsx", protocolAssistantXLSXFixture(t), "MODBUS_TCP", "hex", token, 200)
	if excel.ParserType != parser.ModbusTCPParserName || len(excel.Fields) != 2 {
		t.Fatal(excel)
	}
	for _, path := range []string{"/api/v2/protocol-catalog", "/api/v2/protocol-market", "/api/v2/market-distribution/tenant/catalog"} {
		req, _ := http.NewRequest("GET", server.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Fatalf("removed route %s: %d", path, resp.StatusCode)
		}
	}
}
