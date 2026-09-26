package mcpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
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
	for _, tool := range []string{"query_system_overview", "query_device_latest", "query_alarm_list", "query_property_history", "query_similar_alarms", "query_knowledge_base", "create_rule_draft"} {
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
