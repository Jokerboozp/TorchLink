package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bytes"             /* 执行当前语句并推进处理流程。 */
	"context"           /* 执行当前语句并推进处理流程。 */
	"encoding/json"     /* 执行当前语句并推进处理流程。 */
	"io"                /* 执行当前语句并推进处理流程。 */
	"log/slog"          /* 执行当前语句并推进处理流程。 */
	"mime/multipart"    /* 执行当前语句并推进处理流程。 */
	"net/http"          /* 执行当前语句并推进处理流程。 */
	"net/http/httptest" /* 执行当前语句并推进处理流程。 */
	"testing"           /* 执行当前语句并推进处理流程。 */
	"time"              /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/adapters/memory" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/config"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/core"            /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/metrics"         /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"           /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser"          /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestUploadedProtocolLifecycle(t *testing.T) { /* 定义 TestUploadedProtocolLifecycle 函数。 */
	repo := memory.NewRepository()                                                                                                /* 更新 repo 的值。 */
	log := slog.New(slog.NewTextHandler(io.Discard, nil))                                                                         /* 更新 log 的值。 */
	cfg := config.Load()                                                                                                          /* 更新 cfg 的值。 */
	cfg.JWTSecret = "protocol-generation-test-32-chars"                                                                           /* 更新 cfg.JWTSecret 的值。 */
	api := New(cfg, &core.Engine{Repo: repo, Parsers: parser.NewPlatformRegistry(t.TempDir())}, metrics.New(), log)               /* 更新 api 的值。 */
	server := httptest.NewServer(api.Handler())                                                                                   /* 更新 server 的值。 */
	defer server.Close()                                                                                                          /* 安排函数结束时执行清理。 */
	token, _ := api.auth.Issue("operator", "tenant", "operator", nil, time.Hour)                                                  /* 更新 _ 的值。 */
	viewer, _ := api.auth.Issue("viewer", "tenant", "viewer", nil, time.Hour)                                                     /* 更新 _ 的值。 */
	other, _ := api.auth.Issue("operator", "other", "operator", nil, time.Hour)                                                   /* 更新 _ 的值。 */
	upload := func(kind, filename string, data []byte, transport, format, auth string, status int) model.ProtocolAssistantDraft { /* 更新 upload 的值。 */
		t.Helper()                                                                                                                /* 执行当前语句并推进处理流程。 */
		var body bytes.Buffer                                                                                                     /* 声明 body。 */
		form := multipart.NewWriter(&body)                                                                                        /* 更新 form 的值。 */
		for k, v := range map[string]string{"inputKind": kind, "transport": transport, "payloadFormat": format, "name": "上传生成"} { /* 循环处理当前数据。 */
			_ = form.WriteField(k, v) /* 更新 _ 的值。 */
		} /* 结束当前表达式或代码块。 */
		file, _ := form.CreateFormFile("file", filename)                                            /* 更新 _ 的值。 */
		_, _ = file.Write(data)                                                                     /* 更新 _ 的值。 */
		_ = form.Close()                                                                            /* 更新 _ 的值。 */
		r, _ := http.NewRequest("POST", server.URL+"/api/v1/ai/protocol-assistant/generate", &body) /* 更新 _ 的值。 */
		r.Header.Set("Authorization", "Bearer "+auth)                                               /* 执行当前语句并推进处理流程。 */
		r.Header.Set("Content-Type", form.FormDataContentType())                                    /* 执行当前语句并推进处理流程。 */
		response, err := server.Client().Do(r)                                                      /* 更新 err 的值。 */
		if err != nil {                                                                             /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		defer response.Body.Close()         /* 安排函数结束时执行清理。 */
		raw, _ := io.ReadAll(response.Body) /* 更新 _ 的值。 */
		if response.StatusCode != status {  /* 判断条件并选择处理分支。 */
			t.Fatalf("upload %s status %d: %s", filename, response.StatusCode, raw) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		var draft model.ProtocolAssistantDraft /* 声明 draft。 */
		if status == 200 {                     /* 判断条件并选择处理分支。 */
			if err = json.Unmarshal(raw, &draft); err != nil { /* 判断条件并选择处理分支。 */
				t.Fatal(err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		return draft /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	draft := upload("sample", "report.json", []byte(`{"data":{"temperature":25.5,"smoke":false}}`), "MQTT", "json", token, 200) /* 更新 draft 的值。 */
	if draft.ParserType != "configurable_json_parser" || draft.Preview.Properties["temperature"] != 25.5 {                      /* 判断条件并选择处理分支。 */
		t.Fatal(draft) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	upload("sample", "report.json", []byte(`{"temperature":1}`), "MQTT", "json", viewer, 403)                                                                                 /* 执行当前语句并推进处理流程。 */
	body := map[string]any{"id": "json-generated", "version": "1.0.0", "draft": draft, "payload": map[string]any{"data": map[string]any{"temperature": 31.5, "smoke": true}}} /* 更新 body 的值。 */
	result := requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/ai/protocol-assistant/publish", token, body, 201)                                                   /* 更新 result 的值。 */
	if result["release"].(map[string]any)["status"] != "VALIDATED" || result["standardMessage"].(map[string]any)["properties"].(map[string]any)["temperature"] != 31.5 {      /* 判断条件并选择处理分支。 */
		t.Fatal(result) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/ai/protocol-assistant/publish", token, body, 409)                                                                              /* 执行当前语句并推进处理流程。 */
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v2/protocols/json-generated/releases/1.0.0/publish", other, map[string]any{}, 404)                                                /* 执行当前语句并推进处理流程。 */
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v2/protocols/json-generated/releases/1.0.0/publish", token, map[string]any{}, 200)                                                /* 执行当前语句并推进处理流程。 */
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/products", token, map[string]any{"id": "generated-product", "name": "报文产品", "protocolPackageId": "json-generated@1.0.0"}, 201) /* 执行当前语句并推进处理流程。 */
	if _, err := repo.GetProduct(context.Background(), "other", "generated-product"); err == nil {                                                                                             /* 判断条件并选择处理分支。 */
		t.Fatal("cross tenant product") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	table := []byte("identifier,name,functionCode,address,addressNotation,dataType,scale\ntemperature,温度,3,0,zero_based,uint16,0.1\n")                              /* 更新 table 的值。 */
	draft = upload("point-table", "points.csv", table, "MODBUS_TCP", "hex", token, 200)                                                                             /* 更新 draft 的值。 */
	body = map[string]any{"id": "points-generated", "version": "1.0.0", "draft": draft}                                                                             /* 更新 body 的值。 */
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/ai/protocol-assistant/publish", token, body, 201)                                                   /* 执行当前语句并推进处理流程。 */
	path := server.URL + "/api/v2/protocols/points-generated/releases/1.0.0"                                                                                        /* 更新 path 的值。 */
	requestJSON(t, server.Client(), "POST", path+"/publish", token, map[string]any{}, 422)                                                                          /* 执行当前语句并推进处理流程。 */
	requestJSON(t, server.Client(), "POST", path+"/preview", other, map[string]any{"payload": "bad"}, 404)                                                          /* 执行当前语句并推进处理流程。 */
	requestJSON(t, server.Client(), "POST", path+"/preview", token, map[string]any{"payload": "00 01", "startAddress": 0}, 422)                                     /* 执行当前语句并推进处理流程。 */
	result = requestJSON(t, server.Client(), "POST", path+"/preview", token, map[string]any{"payload": "00 01 00 00 00 05 01 03 02 00 FA", "startAddress": 0}, 200) /* 更新 result 的值。 */
	if result["standardMessage"].(map[string]any)["properties"].(map[string]any)["temperature"] != float64(25) {                                                    /* 判断条件并选择处理分支。 */
		t.Fatal(result) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	requestJSON(t, server.Client(), "POST", path+"/publish", token, map[string]any{}, 200)                                                                                                    /* 执行当前语句并推进处理流程。 */
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/products", token, map[string]any{"id": "points-product", "name": "点表产品", "protocolPackageId": "points-generated@1.0.0"}, 201) /* 执行当前语句并推进处理流程。 */
	excel := upload("point-table", "points.xlsx", protocolAssistantXLSXFixture(t), "MODBUS_TCP", "hex", token, 200)                                                                           /* 更新 excel 的值。 */
	if excel.ParserType != parser.ModbusTCPParserName || len(excel.Fields) != 2 {                                                                                                             /* 判断条件并选择处理分支。 */
		t.Fatal(excel) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	unchecked := model.ProtocolAssistantDraft{Name: "CRC mapping", ParserType: "configurable_hex_parser", Transport: "MQTT", PayloadFormat: "hex", Config: map[string]any{"checksum": "crc16", "fields": []any{map[string]any{"name": "value", "offset": 0, "length": 1, "type": "uint8"}}}} /* 更新 unchecked 的值。 */
	uncheckedBody := map[string]any{"id": "unchecked-crc", "version": "1.0.0", "draft": unchecked, "payload": "01 02 03"}                                                                                                                                                                    /* 更新 uncheckedBody 的值。 */
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/ai/protocol-assistant/publish", token, uncheckedBody, 422)                                                                                                                                                                   /* 执行当前语句并推进处理流程。 */
	delete(uncheckedBody, "payload")                                                                                                                                                                                                                                                         /* 执行当前语句并推进处理流程。 */
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/ai/protocol-assistant/publish", token, uncheckedBody, 201)                                                                                                                                                                   /* 执行当前语句并推进处理流程。 */
	uncheckedPath := server.URL + "/api/v2/protocols/unchecked-crc/releases/1.0.0"                                                                                                                                                                                                           /* 更新 uncheckedPath 的值。 */
	requestJSON(t, server.Client(), "POST", uncheckedPath+"/preview", token, map[string]any{"payload": "01 02 03"}, 422)                                                                                                                                                                     /* 执行当前语句并推进处理流程。 */
	requestJSON(t, server.Client(), "POST", uncheckedPath+"/publish", token, map[string]any{}, 422)                                                                                                                                                                                          /* 执行当前语句并推进处理流程。 */
	for _, path := range []string{"/api/v2/protocol-catalog", "/api/v2/protocol-market", "/api/v2/market-distribution/tenant/catalog"} {                                                                                                                                                     /* 循环处理当前数据。 */
		req, _ := http.NewRequest("GET", server.URL+path, nil) /* 更新 _ 的值。 */
		req.Header.Set("Authorization", "Bearer "+token)       /* 执行当前语句并推进处理流程。 */
		resp, err := server.Client().Do(req)                   /* 更新 err 的值。 */
		if err != nil {                                        /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		resp.Body.Close()           /* 执行当前语句并推进处理流程。 */
		if resp.StatusCode != 404 { /* 判断条件并选择处理分支。 */
			t.Fatalf("removed route %s: %d", path, resp.StatusCode) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
