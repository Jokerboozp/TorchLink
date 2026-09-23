package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"crypto/hmac"   /* 执行当前语句并推进处理流程。 */
	"crypto/sha256" /* 执行当前语句并推进处理流程。 */
	"encoding/hex"  /* 执行当前语句并推进处理流程。 */
	"testing"       /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestVideoSignature(t *testing.T) { /* 定义 TestVideoSignature 函数。 */
	body := []byte(`{"eventId":"1"}`)                       /* 更新 body 的值。 */
	mac := hmac.New(sha256.New, []byte("secret"))           /* 更新 mac 的值。 */
	_, _ = mac.Write([]byte("123"))                         /* 更新 _ 的值。 */
	_, _ = mac.Write(body)                                  /* 更新 _ 的值。 */
	signature := hex.EncodeToString(mac.Sum(nil))           /* 更新 signature 的值。 */
	if !verifySignature("secret", "123", body, signature) { /* 判断条件并选择处理分支。 */
		t.Fatal("valid signature rejected") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if verifySignature("secret", "123", body, "bad") { /* 判断条件并选择处理分支。 */
		t.Fatal("bad signature accepted") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestAIProviderURLFormat(t *testing.T) { /* 定义 TestAIProviderURLFormat 函数。 */
	tests := []struct { /* 更新 tests 的值。 */
		name, target string /* 执行当前语句并推进处理流程。 */
		want         bool   /* 执行当前语句并推进处理流程。 */
	}{ /* 结束当前表达式或代码块。 */
		{name: "official API", target: "https://api.deepseek.com/v1", want: true},                      /* 执行当前语句并推进处理流程。 */
		{name: "default HTTPS port", target: "https://api.deepseek.com:443", want: true},               /* 执行当前语句并推进处理流程。 */
		{name: "exact Ollama port", target: "http://localhost:11434/api", want: true},                  /* 执行当前语句并推进处理流程。 */
		{name: "custom local port", target: "http://localhost:8080", want: true},                       /* 执行当前语句并推进处理流程。 */
		{name: "remote LAN model", target: "http://192.168.10.20:9000/v1", want: true},                 /* 执行当前语句并推进处理流程。 */
		{name: "custom cloud model", target: "https://models.example.com/v1", want: true},              /* 执行当前语句并推进处理流程。 */
		{name: "IPv6 model", target: "http://[::1]:11434", want: true},                                 /* 执行当前语句并推进处理流程。 */
		{name: "userinfo rejected", target: "https://token@api.deepseek.com", want: false},             /* 执行当前语句并推进处理流程。 */
		{name: "query rejected", target: "https://models.example.com?key=secret", want: false},         /* 执行当前语句并推进处理流程。 */
		{name: "fragment rejected", target: "https://models.example.com/#v1", want: false},             /* 执行当前语句并推进处理流程。 */
		{name: "markdown rejected", target: "[http://ollama:11434](http://ollama:11434)", want: false}, /* 执行当前语句并推进处理流程。 */
		{name: "unsupported scheme", target: "file:///tmp/provider", want: false},                      /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	for _, tt := range tests { /* 循环处理当前数据。 */
		t.Run(tt.name, func(t *testing.T) { /* 执行当前语句并推进处理流程。 */
			if got := validateAIProviderURL(tt.target) == nil; got != tt.want { /* 判断条件并选择处理分支。 */
				t.Fatalf("valid provider URL(%q)=%v want=%v", tt.target, got, tt.want) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		}) /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
