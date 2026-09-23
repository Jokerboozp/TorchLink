package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"iot-platform/internal/protocolworker" /* 执行当前语句并推进处理流程。 */
	"net/http/httptest"                    /* 执行当前语句并推进处理流程。 */
	"net/url"                              /* 执行当前语句并推进处理流程。 */
	"testing"                              /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestSourceManifestUsesPackageMetadataAndExplicitOverrides(t *testing.T) { /* 定义 TestSourceManifestUsesPackageMetadataAndExplicitOverrides 函数。 */
	req := httptest.NewRequest("POST", "/", nil)                                                                                                                                                                                                            /* 更新 req 的值。 */
	files := map[string][]byte{"protocol.json": []byte(`{"id":"package","name":"Vendor","version":"1.0.0","runtime":"go-protocol-v2","transport":"TCP_UDP","payloadFormat":"hex","capabilities":["decode","ingress","encode"],"entrypoint":"cmd/worker"}`)} /* 更新 files 的值。 */
	m, entry, err := sourceProtocolManifest(req, "package", files)                                                                                                                                                                                          /* 更新 err 的值。 */
	if err != nil || m.Runtime != protocolworker.Runtime || m.Version != "1.0.0" || m.Transport != "TCP_UDP" || entry != "cmd/worker" {                                                                                                                     /* 判断条件并选择处理分支。 */
		t.Fatalf("metadata %+v %s %v", m, entry, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	req.Form = url.Values{"version": {"1.1.0"}, "transport": {"TCP"}} /* 更新 req.Form 的值。 */
	m, _, err = sourceProtocolManifest(req, "package", files)         /* 更新 err 的值。 */
	if err != nil || m.Version != "1.1.0" || m.Transport != "TCP" {   /* 判断条件并选择处理分支。 */
		t.Fatalf("override %+v %v", m, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, _, err = sourceProtocolManifest(req, "other", files); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("mismatched source id accepted") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	req.Form.Set("runtime", "go-json-lines-v1")                                /* 执行当前语句并推进处理流程。 */
	if _, _, err = sourceProtocolManifest(req, "package", files); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("v1 accepted ingress/encode") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	req.Form.Set("capabilities", `["decode"]`)                                 /* 执行当前语句并推进处理流程。 */
	if _, _, err = sourceProtocolManifest(req, "package", files); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("decode-only v1 accepted") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	req.Form = url.Values{"version": {"1.0.0"}}             /* 更新 req.Form 的值。 */
	m, _, err = sourceProtocolManifest(req, "package", nil) /* 检查错误并决定后续处理。 */
	if err != nil || m.Runtime != protocolworker.Runtime {  /* 判断条件并选择处理分支。 */
		t.Fatalf("current runtime default: %+v %v", m, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

} /* 结束当前表达式或代码块。 */
