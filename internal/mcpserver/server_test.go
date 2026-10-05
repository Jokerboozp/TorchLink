package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/auth"
	"iot-platform/internal/core"
	"iot-platform/internal/model"
)

func TestHarnessToolSurfaceIsReadOnlyAndScopeChecked(t *testing.T) {
	handler := NewHarness(&core.Engine{})
	claims := auth.Claims{
		Username: "alice",
		TenantID: "tenant-a",
		TokenUse: "harness",
		RunID:    "run-1",
		Scopes:   []string{auth.ScopeQueryDeviceLatest},
		RegisteredClaims: jwt.RegisteredClaims{
			Audience: jwt.ClaimStrings{auth.HarnessAudience},
		},
	}

	listBody := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
	list := httptest.NewRequest(http.MethodPost, "http://localhost/mcp/harness", strings.NewReader(listBody))
	list.Header.Set("Content-Type", "application/json")
	list = list.WithContext(auth.ContextWithClaims(context.Background(), claims))
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, list)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("tools/list status=%d body=%s", listResponse.Code, listResponse.Body.String())
	}
	body := listResponse.Body.String()
	for _, tool := range []string{"query_system_overview", "query_device_latest", "query_alarm_list", "query_alarm_detail", "query_property_history", "query_similar_alarms", "query_knowledge_base", "create_rule_draft"} {
		if !strings.Contains(body, `"name":"`+tool+`"`) {
			t.Fatalf("missing read tool %q: %s", tool, body)
		}
	}

	callBody := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"query_alarm_list","arguments":{}}}`
	call := httptest.NewRequest(http.MethodPost, "http://localhost/mcp/harness", strings.NewReader(callBody))
	call.Header.Set("Content-Type", "application/json")
	call = call.WithContext(auth.ContextWithClaims(context.Background(), claims))
	callResponse := httptest.NewRecorder()
	handler.ServeHTTP(callResponse, call)
	if callResponse.Code != http.StatusOK || !strings.Contains(callResponse.Body.String(), "not authorized") {
		t.Fatalf("out-of-scope tool call was not rejected: status=%d body=%s", callResponse.Code, callResponse.Body.String())
	}
}

func TestHarnessCreateRuleDraftPersistsDisabledRule(t *testing.T) {
	repo := memory.NewRepository()
	engine := &core.Engine{Repo: repo}
	handler := NewHarness(engine)
	claims := auth.Claims{Username: "alice", TenantID: "tenant-a", TokenUse: "harness", RunID: "run-draft", Scopes: []string{auth.ScopeCreateRuleDraft}, RegisteredClaims: jwt.RegisteredClaims{Audience: jwt.ClaimStrings{auth.HarnessAudience}}}
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"create_rule_draft","arguments":{"inputText":"如果 temperature 超过 80，打开告警中心","ruleJson":"{\"name\":\"高温\",\"alarmType\":\"HIGH_TEMPERATURE\",\"level\":\"HIGH\",\"match\":\"all\",\"conditions\":[{\"field\":\"temperature\",\"operator\":\"gt\",\"value\":80}],\"actions\":[{\"type\":\"OPEN_PAGE\",\"page\":\"alarms\"}]}"}}}`
	req := httptest.NewRequest(http.MethodPost, "http://localhost/mcp/harness", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(auth.ContextWithClaims(context.Background(), claims))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `\"persisted\":true`) {
		t.Fatalf("draft was not returned as persisted: status=%d body=%s", response.Code, response.Body.String())
	}
	rules, err := repo.ListRules(context.Background(), "tenant-a")
	if err != nil || len(rules) != 1 || rules[0].Enabled || rules[0].Actions[0].Page != "alarms" {
		t.Fatalf("disabled draft was not saved to tenant rules: rules=%#v err=%v", rules, err)
	}
}

func TestSystemOverviewAggregatesTenantStatistics(t *testing.T) {
	repo := memory.NewRepository()
	ctx := context.Background()
	if err := repo.SaveProduct(ctx, model.Product{ID: "smoke", TenantID: "tenant-a", Status: "ENABLED", Category: "smoke"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{ID: "device-1", TenantID: "tenant-a", ProductID: "smoke", Status: "ENABLED", DeviceRole: "DIRECT"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertDeviceState(ctx, model.DeviceState{TenantID: "tenant-a", ProductID: "smoke", DeviceID: "device-1", ConnectionStatus: "ONLINE", DataStatus: "FRESH", BusinessStatus: "ONLINE", LastSeenAt: time.Now().UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveRule(ctx, model.AlarmRule{ID: "rule-1", TenantID: "tenant-a", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.UpsertAlarm(ctx, model.Alarm{ID: "alarm-1", TenantID: "tenant-a", DeviceID: "device-1", Status: "ACTIVE", AlarmLevel: "HIGH", Source: "iot", LastTriggeredAt: time.Now().UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	overview, err := buildSystemOverview(ctx, &core.Engine{Repo: repo}, "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	if overview["systemStatus"] != "DEGRADED" {
		t.Fatalf("unexpected component status: %#v", overview["components"])
	}
	devices := overview["devices"].(map[string]any)
	alarms := overview["alarms"].(map[string]any)
	products := overview["products"].(map[string]any)
	if devices["total"] != 1 || devices["reported"] != 1 || products["total"] != 1 || alarms["active"] != 1 || alarms["highRiskActive"] != 1 {
		t.Fatalf("unexpected overview: %#v", overview)
	}
}

func TestBoundedLimit(t *testing.T) {
	for _, test := range []struct{ value, fallback, maximum, want int }{
		{0, 20, 50, 20}, {-1, 20, 50, 20}, {10, 20, 50, 10}, {1000, 20, 50, 50},
	} {
		if got := boundedLimit(test.value, test.fallback, test.maximum); got != test.want {
			t.Fatalf("boundedLimit(%d, %d, %d)=%d want=%d", test.value, test.fallback, test.maximum, got, test.want)
		}
	}
}

// Alarm tools return small pages of summaries with the total and next offset;
// the detail tool returns one full alarm.
func TestAlarmToolsReturnSummaryPages(t *testing.T) {
	repo := memory.NewRepository()
	ctx := context.Background()
	for i := range 30 {
		alarmType := "SMOKE_DETECTED"
		if i%3 == 0 {
			alarmType = "HIGH_TEMPERATURE"
		}
		a := model.Alarm{ID: fmt.Sprintf("alarm-%02d", i), RuleID: fmt.Sprintf("rule-%02d", i), TenantID: "tenant-a", DeviceID: "device-1", AlarmType: alarmType, Status: "ACTIVE", LastTriggeredAt: int64(1000 + i),
			Details: map[string]any{"telemetry": strings.Repeat("x", 100)}, Cameras: []model.CameraSummary{{CameraID: "cam"}}}
		if _, _, err := repo.UpsertAlarm(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	handler := NewHarness(&core.Engine{Repo: repo})
	claims := auth.Claims{Username: "alice", TenantID: "tenant-a", TokenUse: "harness", RunID: "run-page", Scopes: auth.HarnessReadScopes(), RegisteredClaims: jwt.RegisteredClaims{Audience: jwt.ClaimStrings{auth.HarnessAudience}}}
	call := func(name string, args map[string]any) map[string]any {
		t.Helper()
		params, _ := json.Marshal(map[string]any{"name": name, "arguments": args})
		req := httptest.NewRequest(http.MethodPost, "http://localhost/mcp/harness", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":`+string(params)+`}`))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(auth.ContextWithClaims(context.Background(), claims))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		var envelope struct {
			Result struct {
				Content []struct{ Text string } `json:"content"`
				IsError bool                    `json:"isError"`
			} `json:"result"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || envelope.Result.IsError || len(envelope.Result.Content) == 0 {
			t.Fatalf("%s failed: %s", name, response.Body.String())
		}
		out := map[string]any{}
		if err := json.Unmarshal([]byte(envelope.Result.Content[0].Text), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	page := call("query_alarm_list", map[string]any{})
	items := page["items"].([]any)
	if len(items) != 20 || page["total"] != float64(30) || page["nextOffset"] != float64(20) {
		t.Fatalf("default page: %d items total=%v next=%v", len(items), page["total"], page["nextOffset"])
	}
	if first := items[0].(map[string]any); first["details"] != nil || first["cameras"] != nil || first["alarmId"] != "alarm-29" {
		t.Fatalf("list must return newest summaries only: %v", first)
	}
	if last := call("query_alarm_list", map[string]any{"limit": 500, "offset": 20}); len(last["items"].([]any)) != 10 || last["nextOffset"] != float64(-1) {
		t.Fatalf("last page: %v", last)
	}
	similar := call("query_similar_alarms", map[string]any{"deviceId": "device-1", "alarmType": "high_temperature", "limit": 4, "offset": 4})
	if similar["total"] != float64(10) || len(similar["items"].([]any)) != 4 || similar["nextOffset"] != float64(8) {
		t.Fatalf("similar page: %v", similar)
	}
	for _, item := range similar["items"].([]any) {
		if item.(map[string]any)["alarmType"] != "HIGH_TEMPERATURE" {
			t.Fatalf("type filter ignored: %v", item)
		}
	}
	if detail := call("query_alarm_detail", map[string]any{"alarmId": "alarm-03"}); detail["details"] == nil || detail["alarmId"] != "alarm-03" {
		t.Fatalf("detail: %v", detail)
	}
}

// Outside a Harness run the browser endpoint carries no per-action
// permissions, so only the built-in administrator may save rule drafts.
func TestBrowserCreateRuleDraftRequiresBuiltinAdmin(t *testing.T) {
	repo := memory.NewRepository()
	handler := New(&core.Engine{Repo: repo})
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"create_rule_draft","arguments":{"ruleJson":"{\"name\":\"高温\",\"alarmType\":\"HIGH_TEMPERATURE\",\"level\":\"HIGH\",\"conditions\":[{\"field\":\"temperature\",\"operator\":\"gt\",\"value\":80}]}"}}}`
	req := httptest.NewRequest(http.MethodPost, "http://localhost/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(auth.ContextWithClaims(context.Background(), auth.Claims{Username: "alice", TenantID: "tenant-a", Role: "operator", TokenUse: "user"}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "built-in administrator") {
		t.Fatalf("managed user draft was not rejected: status=%d body=%s", response.Code, response.Body.String())
	}
	if rules, err := repo.ListRules(context.Background(), "tenant-a"); err != nil || len(rules) != 0 {
		t.Fatalf("draft was saved: rules=%#v err=%v", rules, err)
	}
}

// Tool call logs keep small results whole and summarise large ones.
func TestAuditOutputSummarisesLargeResults(t *testing.T) {
	small := map[string]any{"kind": "ruleDraft"}
	if got := auditOutput(small); !reflect.DeepEqual(got, small) {
		t.Fatalf("small result changed: %#v", got)
	}
	items := make([]map[string]string, 200)
	for i := range items {
		items[i] = map[string]string{"text": strings.Repeat("知", 20)}
	}
	got, ok := auditOutput(map[string]any{"items": items, "total": 900}).(map[string]any)
	if !ok || got["truncated"] != true || got["items"] != 200 || string(got["total"].(json.RawMessage)) != "900" {
		t.Fatalf("large result not summarised: %#v", got)
	}
}
