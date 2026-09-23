package auth /* 声明 auth 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"testing" /* 执行当前语句并推进处理流程。 */
	"time"    /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestIssueHarnessCreatesRestrictedShortLivedToken(t *testing.T) { /* 定义 TestIssueHarnessCreatesRestrictedShortLivedToken 函数。 */
	manager := New("test-secret-at-least-32-characters")                                    /* 更新 manager 的值。 */
	scopes := HarnessReadScopes()                                                           /* 更新 scopes 的值。 */
	token, err := manager.IssueHarness("alice", "tenant-a", "run-1", scopes, 2*time.Minute) /* 更新 err 的值。 */
	if err != nil {                                                                         /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	claims, err := manager.Parse(token) /* 更新 err 的值。 */
	if err != nil {                     /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if claims.TokenUse != "harness" || claims.RunID != "run-1" || claims.TenantID != "tenant-a" { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected harness claims: %#v", claims) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if !claims.HasAudience(HarnessAudience) || len(claims.ACL) != 0 { /* 判断条件并选择处理分支。 */
		t.Fatalf("harness audience/ACL is unsafe: %#v", claims) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	for _, scope := range scopes { /* 循环处理当前数据。 */
		if !claims.HasScope(scope) { /* 判断条件并选择处理分支。 */
			t.Fatalf("missing exact scope %q", scope) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	remaining := time.Until(claims.ExpiresAt.Time)                         /* 更新 remaining 的值。 */
	if remaining <= time.Minute || remaining > 2*time.Minute+time.Second { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected harness TTL: %s", remaining) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
