package video /* 声明 video 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bytes"             /* 执行当前语句并推进处理流程。 */
	"crypto/hmac"       /* 执行当前语句并推进处理流程。 */
	"crypto/md5"        /* 执行当前语句并推进处理流程。 */
	"crypto/sha256"     /* 执行当前语句并推进处理流程。 */
	"encoding/base64"   /* 执行当前语句并推进处理流程。 */
	"encoding/json"     /* 执行当前语句并推进处理流程。 */
	"net/http"          /* 执行当前语句并推进处理流程。 */
	"net/http/httptest" /* 执行当前语句并推进处理流程。 */
	"strings"           /* 执行当前语句并推进处理流程。 */
	"testing"           /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestApplyHikvisionSignature(t *testing.T) { /* 定义 TestApplyHikvisionSignature 函数。 */
	body := []byte(`{"cameraIndexCode":"camera-001"}`)                                                                                                 /* 更新 body 的值。 */
	req := httptest.NewRequest(http.MethodPost, "https://hikcentral.example.internal/artemis/api/video/v2/cameras/previewURLs", bytes.NewReader(body)) /* 更新 req 的值。 */
	applyHikvisionSignature(req, body, "app-key", "app-secret", "Thu, 01 Jan 2026 00:00:00 GMT")                                                       /* 执行当前语句并推进处理流程。 */
	if req.Header.Get("X-Ca-Key") != "app-key" || req.Header.Get("X-Ca-Signature-Headers") != "x-ca-key,x-ca-nonce,x-ca-timestamp" {                   /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected signing headers: %#v", req.Header) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	nonce := req.Header.Get("X-Ca-Nonce")                                         /* 更新 nonce 的值。 */
	timestamp := req.Header.Get("X-Ca-Timestamp")                                 /* 更新 timestamp 的值。 */
	if nonce == "" || timestamp == "" || req.Header.Get("X-Ca-Signature") == "" { /* 判断条件并选择处理分支。 */
		t.Fatalf("missing generated signing values: %#v", req.Header) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	digest := md5.Sum(body)                                                                                    /* 更新 digest 的值。 */
	if got, want := req.Header.Get("Content-MD5"), base64.StdEncoding.EncodeToString(digest[:]); got != want { /* 判断条件并选择处理分支。 */
		t.Fatalf("Content-MD5 = %q, want %q", got, want) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	stringToSign := strings.Join([]string{ /* 更新 stringToSign 的值。 */
		http.MethodPost, /* 执行当前语句并推进处理流程。 */
		"*/*",           /* 执行当前语句并推进处理流程。 */
		base64.StdEncoding.EncodeToString(digest[:]), /* 执行当前语句并推进处理流程。 */
		"application/json",                           /* 执行当前语句并推进处理流程。 */
		"Thu, 01 Jan 2026 00:00:00 GMT",              /* 执行当前语句并推进处理流程。 */
		"x-ca-key:app-key",                           /* 执行当前语句并推进处理流程。 */
		"x-ca-nonce:" + nonce,                        /* 执行当前语句并推进处理流程。 */
		"x-ca-timestamp:" + timestamp,                /* 执行当前语句并推进处理流程。 */
		req.URL.RequestURI(),                         /* 执行当前语句并推进处理流程。 */
	}, "\n") /* 结束当前表达式或代码块。 */
	hash := hmac.New(sha256.New, []byte("app-secret"))                 /* 更新 hash 的值。 */
	_, _ = hash.Write([]byte(stringToSign))                            /* 更新 _ 的值。 */
	wantSignature := base64.StdEncoding.EncodeToString(hash.Sum(nil))  /* 更新 wantSignature 的值。 */
	if got := req.Header.Get("X-Ca-Signature"); got != wantSignature { /* 判断条件并选择处理分支。 */
		t.Fatalf("signature = %q, want %q", got, wantSignature) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestParseHikvisionTimestamp(t *testing.T) { /* 定义 TestParseHikvisionTimestamp 函数。 */
	checks := []struct { /* 更新 checks 的值。 */
		name string /* 执行当前语句并推进处理流程。 */
		raw  string /* 执行当前语句并推进处理流程。 */
		want int64  /* 执行当前语句并推进处理流程。 */
	}{ /* 结束当前表达式或代码块。 */
		{name: "unix seconds", raw: `1740000000`, want: 1740000000000},             /* 执行当前语句并推进处理流程。 */
		{name: "unix milliseconds", raw: `1740000000000`, want: 1740000000000},     /* 执行当前语句并推进处理流程。 */
		{name: "rfc3339", raw: `"2025-02-20T10:00:00+08:00"`, want: 1740016800000}, /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	for _, check := range checks { /* 循环处理当前数据。 */
		t.Run(check.name, func(t *testing.T) { /* 执行当前语句并推进处理流程。 */
			got := parseHikvisionTimestamp(json.RawMessage(check.raw)) /* 更新 got 的值。 */
			if got != check.want {                                     /* 判断条件并选择处理分支。 */
				t.Fatalf("parseHikvisionTimestamp(%s) = %d, want %d", check.raw, got, check.want) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		}) /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestHikvisionPreviewURLNormalizesOfficialAPIPath(t *testing.T) { /* 定义 TestHikvisionPreviewURLNormalizesOfficialAPIPath 函数。 */
	got, err := hikvisionPreviewURL("https://hikcentral.example.internal") /* 更新 err 的值。 */
	if err != nil {                                                        /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	want := "https://hikcentral.example.internal/artemis/api/video/v2/cameras/previewURLs" /* 更新 want 的值。 */
	if got != want {                                                                       /* 判断条件并选择处理分支。 */
		t.Fatalf("preview URL = %q, want %q", got, want) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestHikvisionPreviewURLPreservesExplicitAPIPath(t *testing.T) { /* 定义 TestHikvisionPreviewURLPreservesExplicitAPIPath 函数。 */
	got, err := hikvisionPreviewURL("https://hikcentral.example.internal/artemis/api/video/v2/cameras/previewURLs") /* 更新 err 的值。 */
	if err != nil {                                                                                                 /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	want := "https://hikcentral.example.internal/artemis/api/video/v2/cameras/previewURLs" /* 更新 want 的值。 */
	if got != want {                                                                       /* 判断条件并选择处理分支。 */
		t.Fatalf("preview URL = %q, want %q", got, want) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
