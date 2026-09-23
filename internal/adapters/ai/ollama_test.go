package aiadapter /* 声明 aiadapter 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"           /* 执行当前语句并推进处理流程。 */
	"net/http"          /* 执行当前语句并推进处理流程。 */
	"net/http/httptest" /* 执行当前语句并推进处理流程。 */
	"sync/atomic"       /* 执行当前语句并推进处理流程。 */
	"testing"           /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestOllamaRejectsUnsafeURLAndCrossOriginRedirect(t *testing.T) { /* 定义 TestOllamaRejectsUnsafeURLAndCrossOriginRedirect 函数。 */
	normalized, err := NewOllama("http://localhost:11434/v1", "model") /* 更新 err 的值。 */
	if err != nil {                                                    /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if normalized.baseURL != "http://localhost:11434" { /* 判断条件并选择处理分支。 */
		t.Fatalf("Ollama /v1 suffix was not normalized: %q", normalized.baseURL) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	for _, rawURL := range []string{"not-a-url", "https://secret@example.com", "https://example.com?token=secret"} { /* 循环处理当前数据。 */
		if _, err := NewOllama(rawURL, "model"); err == nil { /* 判断条件并选择处理分支。 */
			t.Fatalf("unsafe Ollama URL %q was accepted", rawURL) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */

	var redirectedRequests atomic.Int32                                                      /* 声明 redirectedRequests。 */
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { /* 更新 target 的值。 */
		redirectedRequests.Add(1) /* 执行当前语句并推进处理流程。 */
	})) /* 结束当前表达式或代码块。 */
	defer target.Close()                                                                             /* 安排函数结束时执行清理。 */
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { /* 更新 redirector 的值。 */
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect) /* 执行当前语句并推进处理流程。 */
	})) /* 结束当前表达式或代码块。 */
	defer redirector.Close()                          /* 安排函数结束时执行清理。 */
	client, err := NewOllama(redirector.URL, "model") /* 更新 err 的值。 */
	if err != nil {                                   /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, err = client.Chat(context.Background(), "tenant", "hello"); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("cross-origin Ollama redirect was followed") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if redirectedRequests.Load() != 0 { /* 判断条件并选择处理分支。 */
		t.Fatalf("redirect target received %d requests", redirectedRequests.Load()) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
