package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bytes"             /* 执行当前语句并推进处理流程。 */
	"encoding/json"     /* 执行当前语句并推进处理流程。 */
	"io"                /* 执行当前语句并推进处理流程。 */
	"log/slog"          /* 执行当前语句并推进处理流程。 */
	"mime/multipart"    /* 执行当前语句并推进处理流程。 */
	"net/http"          /* 执行当前语句并推进处理流程。 */
	"net/http/httptest" /* 执行当前语句并推进处理流程。 */
	"strings"           /* 执行当前语句并推进处理流程。 */
	"testing"           /* 执行当前语句并推进处理流程。 */
	"time"              /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/adapters/knowledge" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/local"     /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/memory"    /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/config"             /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/core"               /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/metrics"            /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser"             /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestKnowledgeUploadAndTenantScopedList(t *testing.T) { /* 定义 TestKnowledgeUploadAndTenantScopedList 函数。 */
	repo := memory.NewRepository()                /* 更新 repo 的值。 */
	archive, err := local.NewArchive(t.TempDir()) /* 更新 err 的值。 */
	if err != nil {                               /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil))) /* 更新 engine 的值。 */
	engine.KB = knowledge.NewLocal()                                                                                                                                                  /* 更新 engine.KB 的值。 */
	cfg := config.Load()                                                                                                                                                              /* 更新 cfg 的值。 */
	cfg.JWTSecret = "test-secret-at-least-32-characters"                                                                                                                              /* 更新 cfg.JWTSecret 的值。 */
	api := New(cfg, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))                                                                                            /* 更新 api 的值。 */
	server := httptest.NewServer(api.Handler())                                                                                                                                       /* 更新 server 的值。 */
	defer server.Close()                                                                                                                                                              /* 安排函数结束时执行清理。 */

	operatorToken, err := api.auth.Issue("operator", "tenant-a", "operator", nil, time.Hour) /* 检查错误并决定后续处理。 */
	if err != nil {                                                                          /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	viewerToken, err := api.auth.Issue("viewer", "tenant-a", "viewer", nil, time.Hour) /* 检查错误并决定后续处理。 */
	if err != nil {                                                                    /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	otherTenantToken, err := api.auth.Issue("viewer", "tenant-b", "viewer", nil, time.Hour) /* 检查错误并决定后续处理。 */
	if err != nil {                                                                         /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

	var body bytes.Buffer                                                 /* 声明 body。 */
	form := multipart.NewWriter(&body)                                    /* 更新 form 的值。 */
	if err = form.WriteField("workflowId", "ops-assistant"); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err = form.WriteField("productId", "fire-smoke"); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err = form.WriteField("category", "alarm-sop"); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err = form.WriteField("tags", "smoke,certified"); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	file, err := form.CreateFormFile("file", "fire-sop.txt") /* 更新 err 的值。 */
	if err != nil {                                          /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, err = io.Copy(file, strings.NewReader("高温烟雾告警处置：先核对设备状态，再通知现场人员复核。")); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err = form.Close(); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/knowledge/documents", &body) /* 更新 err 的值。 */
	if err != nil {                                                                               /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	req.Header.Set("Authorization", "Bearer "+operatorToken)   /* 执行当前语句并推进处理流程。 */
	req.Header.Set("Content-Type", form.FormDataContentType()) /* 执行当前语句并推进处理流程。 */
	resp, err := server.Client().Do(req)                       /* 更新 err 的值。 */
	if err != nil {                                            /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	defer resp.Body.Close()                                             /* 安排函数结束时执行清理。 */
	var uploaded map[string]any                                         /* 声明 uploaded。 */
	if err = json.NewDecoder(resp.Body).Decode(&uploaded); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if resp.StatusCode != http.StatusCreated || uploaded["status"] != "INDEXED" || uploaded["filename"] != "fire-sop.txt" { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected upload response status=%d body=%#v", resp.StatusCode, uploaded) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

	listed := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/knowledge/documents", viewerToken, nil, http.StatusOK) /* 更新 listed 的值。 */
	if listed["count"] != float64(1) || listed["persistentIndex"] != false || listed["indexMode"] != "local-memory" {                    /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected knowledge list %#v", listed) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	items := listed["items"].([]any)                                                                                                                                                           /* 更新 items 的值。 */
	item := items[0].(map[string]any)                                                                                                                                                          /* 更新 item 的值。 */
	if item["tenantId"] != "tenant-a" || item["workflowId"] != "ops-assistant" || item["productId"] != "fire-smoke" || item["category"] != "alarm-sop" || item["filename"] != "fire-sop.txt" { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected knowledge item %#v", item) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	tags := item["tags"].([]any)                                        /* 更新 tags 的值。 */
	if len(tags) != 2 || tags[0] != "smoke" || tags[1] != "certified" { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected knowledge tags %#v", tags) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	detail := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/knowledge/documents/"+item["id"].(string), viewerToken, nil, http.StatusOK) /* 更新 detail 的值。 */
	indexDetails := detail["index"].(map[string]any)                                                                                                          /* 更新 indexDetails 的值。 */
	if indexDetails["mode"] != "local-memory" || indexDetails["chunkCount"] != float64(1) {                                                                   /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected knowledge index details %#v", detail) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	chunking := indexDetails["chunking"].(map[string]any)                         /* 更新 chunking 的值。 */
	if chunking["size"] != float64(1200) || chunking["overlap"] != float64(200) { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected chunking policy %#v", chunking) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	detailChunks := detail["chunks"].([]any)                                                                                                                               /* 更新 detailChunks 的值。 */
	if len(detailChunks) != 1 || detailChunks[0].(map[string]any)["content"] != "高温烟雾告警处置：先核对设备状态，再通知现场人员复核。" || detailChunks[0].(map[string]any)["vectorized"] != false { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected knowledge chunks %#v", detailChunks) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

	isolated := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/knowledge/documents", otherTenantToken, nil, http.StatusOK) /* 更新 isolated 的值。 */
	if isolated["count"] != float64(0) {                                                                                                        /* 判断条件并选择处理分支。 */
		t.Fatalf("knowledge documents leaked across tenants: %#v", isolated) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/knowledge/documents/"+item["id"].(string), otherTenantToken, nil, http.StatusNotFound) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
