package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bytes"             /* 执行当前语句并推进处理流程。 */
	"encoding/json"     /* 执行当前语句并推进处理流程。 */
	"net/http"          /* 执行当前语句并推进处理流程。 */
	"net/http/httptest" /* 执行当前语句并推进处理流程。 */
	"testing"           /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/auth"   /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/config" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/core"   /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestBuiltinAdminCannotMintTokenForUnconfiguredTenant(t *testing.T) { /* 定义 TestBuiltinAdminCannotMintTokenForUnconfiguredTenant 函数。 */
	cfg := config.Config{AdminUser: "admin", AdminPassword: "admin123", JWTSecret: "test-secret-at-least-32-characters"}     /* 更新 cfg 的值。 */
	s := &Server{cfg: cfg, auth: auth.New(cfg.JWTSecret), engine: &core.Engine{}}                                            /* 更新 s 的值。 */
	body, err := json.Marshal(map[string]string{"username": "admin", "password": "admin123", "tenantId": "tenant-attacker"}) /* 更新 err 的值。 */
	if err != nil {                                                                                                          /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body)) /* 更新 req 的值。 */
	req.Header.Set("Content-Type", "application/json")                                       /* 执行当前语句并推进处理流程。 */
	resp := httptest.NewRecorder()                                                           /* 更新 resp 的值。 */

	s.login(resp, req) /* 执行当前语句并推进处理流程。 */

	if resp.Code != http.StatusForbidden { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected status for unconfigured admin tenant: got=%d body=%s", resp.Code, resp.Body.String()) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if bytes.Contains(resp.Body.Bytes(), []byte(`"accessToken"`)) { /* 判断条件并选择处理分支。 */
		t.Fatal("unconfigured admin tenant response contains an access token") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestBuiltinAdminCanUseConfiguredTenant(t *testing.T) { /* 定义 TestBuiltinAdminCanUseConfiguredTenant 函数。 */
	cfg := config.Config{AdminUser: "admin", AdminPassword: "admin123", AdminTenants: []string{"tenant-allowed"}, JWTSecret: "test-secret-at-least-32-characters"} /* 更新 cfg 的值。 */
	s := &Server{cfg: cfg, auth: auth.New(cfg.JWTSecret), engine: &core.Engine{}}                                                                                  /* 更新 s 的值。 */
	body, err := json.Marshal(map[string]string{"username": "admin", "password": "admin123", "tenantId": "tenant-allowed"})                                        /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body)) /* 更新 req 的值。 */
	req.Header.Set("Content-Type", "application/json")                                       /* 执行当前语句并推进处理流程。 */
	resp := httptest.NewRecorder()                                                           /* 更新 resp 的值。 */
	s.login(resp, req)                                                                       /* 执行当前语句并推进处理流程。 */
	if resp.Code != http.StatusOK {                                                          /* 判断条件并选择处理分支。 */
		t.Fatalf("configured admin tenant was rejected: status=%d body=%s", resp.Code, resp.Body.String()) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	var result struct { /* 声明 result。 */
		AccessToken string `json:"accessToken"` /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	claims, err := s.auth.Parse(result.AccessToken) /* 更新 err 的值。 */
	if err != nil {                                 /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if claims.TenantID != "tenant-allowed" || claims.Role != "admin" { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected configured admin claims: tenant=%q role=%q", claims.TenantID, claims.Role) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
