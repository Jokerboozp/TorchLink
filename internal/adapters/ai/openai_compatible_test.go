package aiadapter /* 声明 aiadapter 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"           /* 执行当前语句并推进处理流程。 */
	"encoding/json"     /* 执行当前语句并推进处理流程。 */
	"io"                /* 执行当前语句并推进处理流程。 */
	"net/http"          /* 执行当前语句并推进处理流程。 */
	"net/http/httptest" /* 执行当前语句并推进处理流程。 */
	"strings"           /* 执行当前语句并推进处理流程。 */
	"sync/atomic"       /* 执行当前语句并推进处理流程。 */
	"testing"           /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/aioutput"
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestOpenAICompatibleProvider(t *testing.T) { /* 定义 TestOpenAICompatibleProvider 函数。 */
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { /* 更新 server 的值。 */
		if r.Header.Get("Authorization") != "Bearer test-key" { /* 判断条件并选择处理分支。 */
			t.Fatalf("unexpected authorization header %q", r.Header.Get("Authorization")) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		switch r.URL.Path { /* 根据条件选择处理路径。 */
		case "/models": /* 处理当前分支。 */
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}}) /* 更新 _ 的值。 */
		case "/chat/completions": /* 处理当前分支。 */
			var request openAIChatRequest                                    /* 声明 request。 */
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil { /* 判断条件并选择处理分支。 */
				t.Fatal(err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			if request.Model != "test-model" || len(request.Messages) != 2 { /* 判断条件并选择处理分支。 */
				t.Fatalf("unexpected request %#v", request) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "chat_test", "choices": []any{map[string]any{"message": map[string]any{"content": "插件连接成功"}}}}) /* 更新 _ 的值。 */
		default: /* 处理当前分支。 */
			http.NotFound(w, r) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	})) /* 结束当前表达式或代码块。 */
	defer server.Close() /* 安排函数结束时执行清理。 */

	client, err := NewOpenAICompatible("test", "Test Provider", server.URL, "test-model", "test-key") /* 更新 err 的值。 */
	if err != nil {                                                                                   /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err := client.Health(context.Background()); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	answer, err := client.Chat(context.Background(), "tenant", "hello") /* 更新 err 的值。 */
	if err != nil || answer != "插件连接成功" {                               /* 判断条件并选择处理分支。 */
		t.Fatalf("answer=%q err=%v", answer, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestOpenAICompatibleAnalyzeAlarmAcceptsStringConfidence(t *testing.T) { /* 定义 TestOpenAICompatibleAnalyzeAlarmAcceptsStringConfidence 函数。 */
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { /* 更新 server 的值。 */
		if r.URL.Path != "/chat/completions" { /* 判断条件并选择处理分支。 */
			http.NotFound(w, r) /* 执行当前语句并推进处理流程。 */
			return              /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		_ = json.NewEncoder(w).Encode(map[string]any{ /* 更新 _ 的值。 */
			"choices": []any{map[string]any{"message": map[string]any{"content": `{"summary":"温度持续升高，需现场复核。","possibleReasons":["传感器异常"],"suggestions":["现场复核设备"],"riskLevel":"HIGH","confidence":"0.86"}`}}}, /* 执行当前语句并推进处理流程。 */
		}) /* 结束当前表达式或代码块。 */
	})) /* 结束当前表达式或代码块。 */
	defer server.Close() /* 安排函数结束时执行清理。 */

	client, err := NewOpenAICompatible("deepseek", "DeepSeek", server.URL, "test-model", "test-key") /* 更新 err 的值。 */
	if err != nil {                                                                                  /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	analysis, err := client.AnalyzeAlarm(context.Background(), model.Alarm{ID: "alarm-1", AlarmLevel: "HIGH"}, nil, nil) /* 检查错误并决定后续处理。 */
	if err != nil {                                                                                                      /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if analysis.Summary == "" || analysis.Model != "test-model" || analysis.Confidence != 0.86 { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected decoded alarm analysis: %#v", analysis) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestOpenAICompatibleAnalyzeAlarmKeepsSummaryWhenConfidenceIsMalformed(t *testing.T) { /* 定义 TestOpenAICompatibleAnalyzeAlarmKeepsSummaryWhenConfidenceIsMalformed 函数。 */
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { /* 更新 server 的值。 */
		if r.URL.Path != "/chat/completions" { /* 判断条件并选择处理分支。 */
			http.NotFound(w, r) /* 执行当前语句并推进处理流程。 */
			return              /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		_ = json.NewEncoder(w).Encode(map[string]any{ /* 更新 _ 的值。 */
			"choices": []any{map[string]any{"message": map[string]any{"content": `{"summary":"告警事实已核对，请人工复核现场。","possibleReasons":["传感器异常"],"suggestions":["现场复核设备"],"riskLevel":"HIGH","confidence":"较高"}`}}}, /* 执行当前语句并推进处理流程。 */
		}) /* 结束当前表达式或代码块。 */
	})) /* 结束当前表达式或代码块。 */
	defer server.Close() /* 安排函数结束时执行清理。 */

	client, err := NewOpenAICompatible("deepseek", "DeepSeek", server.URL, "test-model", "test-key") /* 更新 err 的值。 */
	if err != nil {                                                                                  /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	analysis, err := client.AnalyzeAlarm(context.Background(), model.Alarm{ID: "alarm-2", AlarmLevel: "HIGH"}, nil, nil) /* 检查错误并决定后续处理。 */
	if err != nil {                                                                                                      /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if analysis.Summary == "" || analysis.Confidence != 0 { /* 判断条件并选择处理分支。 */
		t.Fatalf("malformed confidence should not discard summary: %#v", analysis) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestOpenAICompatibleBoundsAndSanitizesProviderErrors(t *testing.T) { /* 定义 TestOpenAICompatibleBoundsAndSanitizesProviderErrors 函数。 */
	leaky := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { /* 更新 leaky 的值。 */
		w.WriteHeader(http.StatusUnauthorized)                             /* 执行当前语句并推进处理流程。 */
		_, _ = io.WriteString(w, "Authorization: Bearer super-secret-key") /* 更新 _ 的值。 */
	})) /* 结束当前表达式或代码块。 */
	defer leaky.Close()                                                                        /* 安排函数结束时执行清理。 */
	client, err := NewOpenAICompatible("test", "Test", leaky.URL, "model", "super-secret-key") /* 更新 err 的值。 */
	if err != nil {                                                                            /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	_, err = client.Chat(context.Background(), "tenant", "hello")        /* 更新 err 的值。 */
	if err == nil || strings.Contains(err.Error(), "super-secret-key") { /* 判断条件并选择处理分支。 */
		t.Fatalf("provider error was not sanitized: %v", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

	oversized := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { /* 更新 oversized 的值。 */
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"`)             /* 更新 _ 的值。 */
		_, _ = io.WriteString(w, strings.Repeat("x", maxAIProviderResponseBytes+1)) /* 更新 _ 的值。 */
		_, _ = io.WriteString(w, `"}}]}`)                                           /* 更新 _ 的值。 */
	})) /* 结束当前表达式或代码块。 */
	defer oversized.Close()                                                          /* 安排函数结束时执行清理。 */
	client, err = NewOpenAICompatible("test", "Test", oversized.URL, "model", "key") /* 更新 err 的值。 */
	if err != nil {                                                                  /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, err = client.Chat(context.Background(), "tenant", "hello"); err == nil || !strings.Contains(err.Error(), "size limit") { /* 判断条件并选择处理分支。 */
		t.Fatalf("oversized provider response was accepted: %v", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestOpenAICompatibleRejectsUserinfoAndCrossOriginRedirect(t *testing.T) { /* 定义 TestOpenAICompatibleRejectsUserinfoAndCrossOriginRedirect 函数。 */
	if _, err := NewOpenAICompatible("test", "Test", "https://secret@example.com", "model", "key"); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("provider URL with userinfo was accepted") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, err := NewOpenAICompatible("test", "Test", "https://example.com?token=secret", "model", "key"); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("provider URL with query credentials was accepted") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

	var redirectedRequests atomic.Int32                                                      /* 声明 redirectedRequests。 */
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { /* 更新 target 的值。 */
		redirectedRequests.Add(1) /* 执行当前语句并推进处理流程。 */
	})) /* 结束当前表达式或代码块。 */
	defer target.Close()                                                                             /* 安排函数结束时执行清理。 */
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { /* 更新 redirector 的值。 */
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect) /* 执行当前语句并推进处理流程。 */
	})) /* 结束当前表达式或代码块。 */
	defer redirector.Close() /* 安排函数结束时执行清理。 */

	client, err := NewOpenAICompatible("test", "Test", redirector.URL, "model", "key") /* 更新 err 的值。 */
	if err != nil {                                                                    /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, err = client.Chat(context.Background(), "tenant", "hello"); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("cross-origin redirect was followed") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if redirectedRequests.Load() != 0 { /* 判断条件并选择处理分支。 */
		t.Fatalf("redirect target received %d requests", redirectedRequests.Load()) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestOpenAICompatibleLimitsSameOriginRedirects(t *testing.T) { /* 定义 TestOpenAICompatibleLimitsSameOriginRedirects 函数。 */
	var requests atomic.Int32                                                                    /* 声明 requests。 */
	var serverURL string                                                                         /* 声明 serverURL。 */
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { /* 更新 server 的值。 */
		requests.Add(1)                                                         /* 执行当前语句并推进处理流程。 */
		http.Redirect(w, r, serverURL+r.URL.Path, http.StatusTemporaryRedirect) /* 执行当前语句并推进处理流程。 */
	})) /* 结束当前表达式或代码块。 */
	serverURL = server.URL                                                         /* 更新 serverURL 的值。 */
	defer server.Close()                                                           /* 安排函数结束时执行清理。 */
	client, err := NewOpenAICompatible("test", "Test", server.URL, "model", "key") /* 更新 err 的值。 */
	if err != nil {                                                                /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, err = client.Chat(context.Background(), "tenant", "hello"); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("same-origin redirect loop was not rejected") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if got := requests.Load(); got > 10 { /* 判断条件并选择处理分支。 */
		t.Fatalf("redirect loop made %d requests", got) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestDecodeRuleDraftNormalizesObjectShapedModelOutput(t *testing.T) { /* 定义 TestDecodeRuleDraftNormalizesObjectShapedModelOutput 函数。 */
	rule, err := aioutput.DecodeRuleDraft(`{"name":"smoke_detector_high_alarm","alarmType":"smoke","level":"high","match":{"deviceType":"smoke_detector"},"conditions":{"smoke":true},"durationSeconds":0,"recovery":{"event":"smoke_clear"},"actions":{"type":"open_camera","cameraId":"camera-001"}}`) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                                                                                                                      /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if rule.AlarmType != "SMOKE_DETECTED" || rule.Level != "HIGH" || rule.Match != "all" { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected normalized rule: %#v", rule) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if len(rule.Conditions) != 1 || rule.Conditions[0].Field != "smoke" || rule.Conditions[0].Operator != "eq" || rule.Conditions[0].Value != true { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected conditions: %#v", rule.Conditions) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if len(rule.Recovery) != 1 || rule.Recovery[0].Field != "event" || rule.Recovery[0].Value != "smoke_clear" { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected recovery: %#v", rule.Recovery) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if len(rule.Actions) != 1 || rule.Actions[0].Type != "OPEN_CAMERA" || rule.Actions[0].CameraID != "camera-001" { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected actions: %#v", rule.Actions) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestProviderRegistry(t *testing.T) { /* 定义 TestProviderRegistry 函数。 */
	registry := NewProviderRegistry() /* 更新 registry 的值。 */
	items := registry.List()          /* 更新 items 的值。 */
	if len(items) < 4 {               /* 判断条件并选择处理分支。 */
		t.Fatalf("expected built-in provider plugins, got %#v", items) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	for _, item := range items { /* 循环处理当前数据。 */
		if item.ID != "disabled" && !item.Enabled { /* 判断条件并选择处理分支。 */
			t.Fatalf("provider %q is not selectable in the test sandbox: %#v", item.ID, item) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if _, err := registry.Create(ports.AIPluginConfig{Provider: "deepseek"}); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("DeepSeek plugin accepted a missing API key") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	client, err := registry.Create(ports.AIPluginConfig{}) /* 更新 err 的值。 */
	if err != nil {                                        /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if info := client.(ports.AIInspectable).ProviderInfo(); info.ID != "disabled" || info.Enabled { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected fallback plugin %#v", info) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
