package mcpserver /* 声明 mcpserver 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"           /* 执行当前语句并推进处理流程。 */
	"errors"            /* 执行当前语句并推进处理流程。 */
	"net/http"          /* 执行当前语句并推进处理流程。 */
	"net/http/httptest" /* 执行当前语句并推进处理流程。 */
	"strings"           /* 执行当前语句并推进处理流程。 */
	"testing"           /* 执行当前语句并推进处理流程。 */
	"time"              /* 执行当前语句并推进处理流程。 */

	"github.com/golang-jwt/jwt/v5" /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/adapters/memory" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/auth"            /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/core"            /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"           /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type draftAI struct{} /* 定义 draftAI 类型。 */

func (draftAI) AnalyzeAlarm(context.Context, model.Alarm, []map[string]any, []string) (model.AIAnalysis, error) { /* 定义 AnalyzeAlarm 函数。 */
	return model.AIAnalysis{}, errors.New("not used") /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (draftAI) Chat(context.Context, string, string) (string, error) { /* 定义 Chat 函数。 */
	return "", errors.New("not used") /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (draftAI) RuleDraft(context.Context, string, string) (model.AlarmRule, error) { /* 定义 RuleDraft 函数。 */
	return model.AlarmRule{Name: "高温打开告警中心", AlarmType: "HIGH_TEMPERATURE", Level: "HIGH", Conditions: []model.RuleCondition{{Field: "temperature", Operator: "gt", Value: float64(80)}}, Actions: []model.RuleAction{{Type: "OPEN_PAGE", Page: "alarms"}}}, nil /* 返回当前处理结果。 */
}                                            /* 结束当前表达式或代码块。 */
func (draftAI) Health(context.Context) error { return nil } /* 定义 Health 函数。 */

func TestHarnessToolSurfaceIsReadOnlyAndScopeChecked(t *testing.T) { /* 定义 TestHarnessToolSurfaceIsReadOnlyAndScopeChecked 函数。 */
	handler := NewHarness(&core.Engine{}) /* 更新 handler 的值。 */
	claims := auth.Claims{                /* 更新 claims 的值。 */
		Username: "alice",                               /* 执行当前语句并推进处理流程。 */
		TenantID: "tenant-a",                            /* 执行当前语句并推进处理流程。 */
		TokenUse: "harness",                             /* 执行当前语句并推进处理流程。 */
		RunID:    "run-1",                               /* 执行当前语句并推进处理流程。 */
		Scopes:   []string{auth.ScopeQueryDeviceLatest}, /* 执行当前语句并推进处理流程。 */
		RegisteredClaims: jwt.RegisteredClaims{ /* 执行当前语句并推进处理流程。 */
			Audience: jwt.ClaimStrings{auth.HarnessAudience}, /* 执行当前语句并推进处理流程。 */
		}, /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */

	listBody := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`                                  /* 更新 listBody 的值。 */
	list := httptest.NewRequest(http.MethodPost, "http://localhost/mcp/harness", strings.NewReader(listBody)) /* 更新 list 的值。 */
	list.Header.Set("Content-Type", "application/json")                                                       /* 执行当前语句并推进处理流程。 */
	list = list.WithContext(auth.ContextWithClaims(context.Background(), claims))                             /* 更新 list 的值。 */
	listResponse := httptest.NewRecorder()                                                                    /* 更新 listResponse 的值。 */
	handler.ServeHTTP(listResponse, list)                                                                     /* 执行当前语句并推进处理流程。 */
	if listResponse.Code != http.StatusOK {                                                                   /* 判断条件并选择处理分支。 */
		t.Fatalf("tools/list status=%d body=%s", listResponse.Code, listResponse.Body.String()) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	body := listResponse.Body.String()                                                                                                                                                                 /* 更新 body 的值。 */
	for _, tool := range []string{"query_system_overview", "query_device_latest", "query_alarm_list", "query_property_history", "query_similar_alarms", "query_knowledge_base", "create_rule_draft"} { /* 循环处理当前数据。 */
		if !strings.Contains(body, `"name":"`+tool+`"`) { /* 判断条件并选择处理分支。 */
			t.Fatalf("missing read tool %q: %s", tool, body) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */

	callBody := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"query_alarm_list","arguments":{}}}` /* 更新 callBody 的值。 */
	call := httptest.NewRequest(http.MethodPost, "http://localhost/mcp/harness", strings.NewReader(callBody))        /* 更新 call 的值。 */
	call.Header.Set("Content-Type", "application/json")                                                              /* 执行当前语句并推进处理流程。 */
	call = call.WithContext(auth.ContextWithClaims(context.Background(), claims))                                    /* 更新 call 的值。 */
	callResponse := httptest.NewRecorder()                                                                           /* 更新 callResponse 的值。 */
	handler.ServeHTTP(callResponse, call)                                                                            /* 执行当前语句并推进处理流程。 */
	if callResponse.Code != http.StatusOK || !strings.Contains(callResponse.Body.String(), "not authorized") {       /* 判断条件并选择处理分支。 */
		t.Fatalf("out-of-scope tool call was not rejected: status=%d body=%s", callResponse.Code, callResponse.Body.String()) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestHarnessCreateRuleDraftPersistsDisabledRule(t *testing.T) { /* 定义 TestHarnessCreateRuleDraftPersistsDisabledRule 函数。 */
	repo := memory.NewRepository()                                                                                                                                                                                                                 /* 更新 repo 的值。 */
	engine := &core.Engine{Repo: repo, AI: draftAI{}}                                                                                                                                                                                              /* 更新 engine 的值。 */
	handler := NewHarness(engine)                                                                                                                                                                                                                  /* 更新 handler 的值。 */
	claims := auth.Claims{Username: "alice", TenantID: "tenant-a", TokenUse: "harness", RunID: "run-draft", Scopes: []string{auth.ScopeCreateRuleDraft}, RegisteredClaims: jwt.RegisteredClaims{Audience: jwt.ClaimStrings{auth.HarnessAudience}}} /* 更新 claims 的值。 */
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"create_rule_draft","arguments":{"inputText":"如果 temperature 超过 80，打开告警中心"}}}`                                                                                         /* 更新 body 的值。 */
	req := httptest.NewRequest(http.MethodPost, "http://localhost/mcp/harness", strings.NewReader(body))                                                                                                                                           /* 更新 req 的值。 */
	req.Header.Set("Content-Type", "application/json")                                                                                                                                                                                             /* 执行当前语句并推进处理流程。 */
	req = req.WithContext(auth.ContextWithClaims(context.Background(), claims))                                                                                                                                                                    /* 更新 req 的值。 */
	response := httptest.NewRecorder()                                                                                                                                                                                                             /* 更新 response 的值。 */
	handler.ServeHTTP(response, req)                                                                                                                                                                                                               /* 执行当前语句并推进处理流程。 */
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `\"persisted\":true`) {                                                                                                                                         /* 判断条件并选择处理分支。 */
		t.Fatalf("draft was not returned as persisted: status=%d body=%s", response.Code, response.Body.String()) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	rules, err := repo.ListRules(context.Background(), "tenant-a")                                 /* 更新 err 的值。 */
	if err != nil || len(rules) != 1 || rules[0].Enabled || rules[0].Actions[0].Page != "alarms" { /* 判断条件并选择处理分支。 */
		t.Fatalf("disabled draft was not saved to tenant rules: rules=%#v err=%v", rules, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestSystemOverviewAggregatesTenantStatistics(t *testing.T) { /* 定义 TestSystemOverviewAggregatesTenantStatistics 函数。 */
	repo := memory.NewRepository()                                                                                                        /* 更新 repo 的值。 */
	ctx := context.Background()                                                                                                           /* 更新 ctx 的值。 */
	if err := repo.SaveProduct(ctx, model.Product{ID: "smoke", TenantID: "tenant-a", Status: "ENABLED", Category: "smoke"}); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{ID: "device-1", TenantID: "tenant-a", ProductID: "smoke", Status: "ENABLED", DeviceRole: "DIRECT"}); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err := repo.UpsertDeviceState(ctx, model.DeviceState{TenantID: "tenant-a", ProductID: "smoke", DeviceID: "device-1", ConnectionStatus: "ONLINE", DataStatus: "FRESH", BusinessStatus: "ONLINE", LastSeenAt: time.Now().UnixMilli()}); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err := repo.SaveRule(ctx, model.AlarmRule{ID: "rule-1", TenantID: "tenant-a", Enabled: true}); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, _, err := repo.UpsertAlarm(ctx, model.Alarm{ID: "alarm-1", TenantID: "tenant-a", DeviceID: "device-1", Status: "ACTIVE", AlarmLevel: "HIGH", Source: "iot", LastTriggeredAt: time.Now().UnixMilli()}); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	overview, err := buildSystemOverview(ctx, &core.Engine{Repo: repo}, "tenant-a") /* 更新 err 的值。 */
	if err != nil {                                                                 /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if overview["systemStatus"] != "DEGRADED" { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected component status: %#v", overview["components"]) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	devices := overview["devices"].(map[string]any)                                                                                            /* 更新 devices 的值。 */
	alarms := overview["alarms"].(map[string]any)                                                                                              /* 更新 alarms 的值。 */
	products := overview["products"].(map[string]any)                                                                                          /* 更新 products 的值。 */
	if devices["total"] != 1 || devices["reported"] != 1 || products["total"] != 1 || alarms["active"] != 1 || alarms["highRiskActive"] != 1 { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected overview: %#v", overview) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestBoundedLimit(t *testing.T) { /* 定义 TestBoundedLimit 函数。 */
	for _, test := range []struct{ value, fallback, maximum, want int }{ /* 循环处理当前数据。 */
		{0, 20, 50, 20}, {-1, 20, 50, 20}, {10, 20, 50, 10}, {1000, 20, 50, 50}, /* 执行当前语句并推进处理流程。 */
	} { /* 结束当前表达式或代码块。 */
		if got := boundedLimit(test.value, test.fallback, test.maximum); got != test.want { /* 判断条件并选择处理分支。 */
			t.Fatalf("boundedLimit(%d, %d, %d)=%d want=%d", test.value, test.fallback, test.maximum, got, test.want) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
