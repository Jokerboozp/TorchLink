package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"iot-platform/internal/aioutput"
	"iot-platform/internal/auth"
	"iot-platform/internal/core"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func New(engine *core.Engine) http.Handler {
	return newServer(engine, false, "/mcp")
}

// NewHarness exposes a deliberately smaller, safe MCP surface for the AI
// sidecar. Authentication, audience and token-use checks live at the HTTP
// boundary; each tool additionally enforces its exact scope here.
func NewHarness(engine *core.Engine) http.Handler {
	return newServer(engine, true, "/mcp/harness")
}

func newServer(engine *core.Engine, harness bool, endpoint string) http.Handler {
	s := server.NewMCPServer("iot-platform-tools", "1.0.0", server.WithToolCapabilities(false), server.WithInstructions("查询消防物联网数据，或生成并保存待人工确认的禁用规则草稿；不能执行 SQL、控制设备或自动启用规则。"), server.WithRecovery())
	s.AddTool(mcp.NewTool("query_system_overview", mcp.WithDescription("统计当前租户的系统状态、产品、设备、在线状态、告警、规则、摄像头和知识库数量")), func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		tenant, err := tenantForTool(ctx, auth.ScopeQuerySystemOverview, harness)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		v, err := buildSystemOverview(ctx, engine, tenant)
		return auditedResult(ctx, engine, "query_system_overview", map[string]any{}, v, err)
	})
	s.AddTool(mcp.NewTool("query_device_latest", mcp.WithDescription("查询当前租户内某设备的最新在线和业务状态"), mcp.WithString("deviceId", mcp.Required(), mcp.Description("设备 ID"))), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		tenant, err := tenantForTool(ctx, auth.ScopeQueryDeviceLatest, harness)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		v, err := engine.Repo.GetDeviceState(ctx, tenant, req.GetString("deviceId", ""))
		return auditedResult(ctx, engine, "query_device_latest", map[string]any{"deviceId": req.GetString("deviceId", "")}, v, err)
	})
	s.AddTool(mcp.NewTool("query_alarm_list", mcp.WithDescription("按状态、等级、设备和时间范围查询当前租户告警"), mcp.WithString("deviceId"), mcp.WithString("status"), mcp.WithString("level"), mcp.WithNumber("start"), mcp.WithNumber("end"), mcp.WithNumber("limit")), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		tenant, err := tenantForTool(ctx, auth.ScopeQueryAlarmList, harness)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		limit := req.GetInt("limit", 100)
		if harness {
			limit = boundedLimit(limit, 100, 100)
		}
		v, err := engine.Repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: tenant, DeviceID: req.GetString("deviceId", ""), Status: req.GetString("status", ""), Level: req.GetString("level", ""), Start: int64(req.GetInt("start", 0)), End: int64(req.GetInt("end", 0)), Limit: limit})
		return auditedResult(ctx, engine, "query_alarm_list", map[string]any{"deviceId": req.GetString("deviceId", ""), "status": req.GetString("status", ""), "level": req.GetString("level", "")}, v, err)
	})
	s.AddTool(mcp.NewTool("query_property_history", mcp.WithDescription("查询当前租户内设备属性历史趋势"), mcp.WithString("deviceId", mcp.Required()), mcp.WithString("propertyCode", mcp.Required()), mcp.WithNumber("start", mcp.Required()), mcp.WithNumber("end", mcp.Required()), mcp.WithNumber("limit")), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		tenant, err := tenantForTool(ctx, auth.ScopeQueryPropertyHistory, harness)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		limit := req.GetInt("limit", 1000)
		if harness {
			limit = boundedLimit(limit, 200, 500)
		}
		v, err := engine.Repo.PropertyHistory(ctx, tenant, req.GetString("deviceId", ""), req.GetString("propertyCode", ""), int64(req.GetInt("start", 0)), int64(req.GetInt("end", int(time.Now().UnixMilli()))), limit)
		return auditedResult(ctx, engine, "query_property_history", map[string]any{"deviceId": req.GetString("deviceId", ""), "propertyCode": req.GetString("propertyCode", "")}, v, err)
	})
	s.AddTool(mcp.NewTool("query_similar_alarms", mcp.WithDescription("查询同设备、同类型的历史告警"), mcp.WithString("deviceId", mcp.Required()), mcp.WithNumber("limit")), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		tenant, err := tenantForTool(ctx, auth.ScopeQuerySimilarAlarms, harness)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		limit := req.GetInt("limit", 20)
		if harness {
			limit = boundedLimit(limit, 20, 50)
		}
		v, err := engine.Repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: tenant, DeviceID: req.GetString("deviceId", ""), Limit: limit})
		return auditedResult(ctx, engine, "query_similar_alarms", map[string]any{"deviceId": req.GetString("deviceId", "")}, v, err)
	})
	s.AddTool(mcp.NewTool("query_knowledge_base", mcp.WithDescription("按当前 Agent 直接绑定的知识文档检索设备手册、SOP 与维修知识"), mcp.WithString("question", mcp.Required()), mcp.WithString("workflowId", mcp.Required()), mcp.WithNumber("limit"), mcp.WithNumber("minScore")), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		tenant, err := tenantForTool(ctx, auth.ScopeQueryKnowledgeBase, harness)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if engine.KB == nil {
			return mcp.NewToolResultError("knowledge base disabled"), nil
		}
		limit := req.GetInt("limit", 5)
		if harness {
			limit = boundedLimit(limit, 5, 20)
		}
		workflowID := req.GetString("workflowId", "")
		minScore := req.GetFloat("minScore", 0)
		if c, ok := auth.ClaimsFromContext(ctx); ok && c.TokenUse == "harness" && c.Knowledge != nil {
			workflowID = c.Knowledge.WorkflowID
			if c.Knowledge.TopK > 0 && limit > c.Knowledge.TopK {
				limit = c.Knowledge.TopK
			}
			if minScore < c.Knowledge.MinScore {
				minScore = c.Knowledge.MinScore
			}
		}
		input := map[string]any{"question": req.GetString("question", ""), "workflowId": workflowID, "limit": limit, "minScore": minScore}
		if filtered, ok := engine.KB.(ports.FilteredKnowledgeBase); ok {
			v, searchErr := filtered.SearchKnowledge(ctx, ports.KnowledgeSearchRequest{TenantID: tenant, WorkflowID: workflowID, Question: req.GetString("question", ""), Limit: limit, MinScore: minScore})
			return auditedResult(ctx, engine, "query_knowledge_base", input, v, searchErr)
		}
		return auditedResult(ctx, engine, "query_knowledge_base", input, nil, fmt.Errorf("workflow-bound knowledge search is not supported by the configured index"))
	})
	// The calling Agent already is the model: it writes the rule JSON itself and
	// this tool only normalises, validates and saves it as a disabled draft, so
	// no second model run is started inside a Harness run.
	s.AddTool(mcp.NewTool("create_rule_draft", mcp.WithDescription("把你按规定格式写好的规则 JSON 保存为禁用草稿；不会启用或执行，必须由用户人工确认。格式要求："+aioutput.RuleDraftInstructions), mcp.WithString("ruleJson", mcp.Required(), mcp.Description("规则 JSON 对象文本")), mcp.WithString("inputText", mcp.Description("用户原始需求，用于审计"))), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		tenant, err := tenantForTool(ctx, auth.ScopeCreateRuleDraft, harness)
		if !harness {
			tenant, err = tenantFrom(ctx)
		}
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		ruleJSON := strings.TrimSpace(req.GetString("ruleJson", ""))
		inputText := strings.TrimSpace(req.GetString("inputText", ""))
		if ruleJSON == "" {
			return mcp.NewToolResultError("ruleJson is required"), nil
		}
		if len(ruleJSON) > 65536 {
			return mcp.NewToolResultError("ruleJson exceeds 65536 bytes"), nil
		}
		v, draftErr := aioutput.DecodeRuleDraft(ruleJSON)
		if draftErr == nil {
			v.TenantID, v.Enabled, v.Expression = tenant, false, ""
			if v.ID == "" {
				v.ID = fmt.Sprintf("rule_draft_%d", time.Now().UnixNano())
			}
			if v.Match == "" {
				v.Match = "all"
			}
			if v.Version == 0 {
				v.Version = 1
			}
			_, _, draftErr = engine.ValidateRuleDraft(ctx, v)
			if draftErr == nil {
				now := time.Now().UnixMilli()
				v.CreatedAt, v.UpdatedAt = now, now
				draftErr = engine.Repo.SaveRule(ctx, v)
				engine.RulesChanged(tenant)
			}
		}
		output := map[string]any{"kind": "ruleDraft", "draft": v, "persisted": draftErr == nil, "requiresHumanApproval": true}
		return auditedResult(ctx, engine, "create_rule_draft", map[string]any{"inputText": inputText}, output, draftErr)
	})
	options := []server.StreamableHTTPOption{
		server.WithStateLess(true),
		server.WithEndpointPath(endpoint),
	}
	if harness {
		// The Harness sidecar reaches the local API through Docker Desktop's
		// host.docker.internal bridge. The Go server sees that connection as
		// loopback, while the Host header is host.docker.internal:port. Keep
		// the MCP endpoint's JWT/audience/scope checks as the security boundary
		// and disable only the transport's localhost Host-header check here.
		options = append(options, server.WithDisableLocalhostProtection(true))
	}
	return server.NewStreamableHTTPServer(s, options...)
}

func buildSystemOverview(ctx context.Context, engine *core.Engine, tenant string) (map[string]any, error) {
	claims, _ := auth.ClaimsFromContext(ctx)
	can := func(menu string) bool {
		if !claims.ManagedUser {
			return true
		}
		for _, permission := range claims.Permissions {
			if permission == "menu:"+menu {
				return true
			}
		}
		return false
	}
	var err error
	var products []model.Product
	if can("products") {
		products, err = engine.Repo.ListProducts(ctx, tenant)
		if err != nil {
			return nil, err
		}
	}
	var protocols []model.ProtocolPackage
	if can("protocols") {
		protocols, err = engine.Repo.ListProtocolPackages(ctx, tenant)
		if err != nil {
			return nil, err
		}
	}
	var devices []model.ManagedDevice
	if can("devices") {
		devices, err = engine.Repo.ListManagedDevices(ctx, tenant)
		if err != nil {
			return nil, err
		}
	}
	var states []model.DeviceState
	if can("devices") {
		states, err = engine.Repo.ListDeviceStates(ctx, tenant)
		if err != nil {
			return nil, err
		}
	}
	var rules []model.AlarmRule
	if can("rules") {
		rules, err = engine.Repo.ListRules(ctx, tenant)
		if err != nil {
			return nil, err
		}
	}
	var alarms []model.Alarm
	if can("devices") {
		alarms, err = engine.Repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: tenant, Limit: 10000})
		if err != nil {
			return nil, err
		}
	}
	var cameras []model.VideoCameraMapping
	if can("cameras") {
		cameras, err = engine.Repo.ListVideoCameraMappings(ctx, tenant)
		if err != nil {
			return nil, err
		}
	}
	var documents []model.KnowledgeDoc
	if can("knowledge") {
		documents, err = engine.Repo.ListKnowledgeDocs(ctx, tenant)
		if err != nil {
			return nil, err
		}
	}

	productStatus, productCategory := map[string]int{}, map[string]int{}
	for _, item := range products {
		increment(productStatus, item.Status)
		increment(productCategory, item.Category)
	}
	protocolStatus := map[string]int{}
	for _, item := range protocols {
		increment(protocolStatus, item.Status)
	}
	deviceStatus, deviceRole := map[string]int{}, map[string]int{}
	registered := make(map[string]struct{}, len(devices))
	autoRegistered := 0
	for _, item := range devices {
		registered[item.ID] = struct{}{}
		increment(deviceStatus, item.Status)
		increment(deviceRole, item.DeviceRole)
		if item.AutoRegistered {
			autoRegistered++
		}
	}
	connectionStatus, dataStatus, businessStatus := map[string]int{}, map[string]int{}, map[string]int{}
	reported, discovered, latestSeenAt := 0, 0, int64(0)
	for _, item := range states {
		if _, ok := registered[item.DeviceID]; !ok {
			discovered++
			continue
		}
		reported++
		increment(connectionStatus, item.ConnectionStatus)
		increment(dataStatus, item.DataStatus)
		increment(businessStatus, item.BusinessStatus)
		if item.LastSeenAt > latestSeenAt {
			latestSeenAt = item.LastSeenAt
		}
	}
	ruleEnabled := 0
	for _, item := range rules {
		if item.Enabled {
			ruleEnabled++
		}
	}
	alarmStatus, alarmLevel, alarmSource := map[string]int{}, map[string]int{}, map[string]int{}
	active, highActive, recent24h := 0, 0, 0
	cutoff := time.Now().Add(-24 * time.Hour).UnixMilli()
	for _, item := range alarms {
		increment(alarmStatus, item.Status)
		increment(alarmLevel, item.AlarmLevel)
		increment(alarmSource, item.Source)
		if item.Status == "ACTIVE" {
			active++
			if item.AlarmLevel == "HIGH" || item.AlarmLevel == "CRITICAL" || item.AlarmLevel == "EMERGENCY" {
				highActive++
			}
		}
		if item.LastTriggeredAt >= cutoff {
			recent24h++
		}
	}
	cameraEnabled, linkedDevices := 0, 0
	for _, item := range cameras {
		if item.Enabled {
			cameraEnabled++
		}
		if item.Enabled && strings.TrimSpace(item.DeviceID) != "" {
			linkedDevices++
		}
	}
	indexedDocs, chunks := 0, 0
	for _, item := range documents {
		if item.Status == "INDEXED" {
			indexedDocs++
		}
		switch value := item.Metadata["chunks"].(type) {
		case int:
			chunks += value
		case float64:
			chunks += int(value)
		}
	}
	components := map[string]string{
		"repository": componentHealth(ctx, engine.Repo),
		"archive":    componentHealth(ctx, engine.Archive),
		"eventBus":   componentHealth(ctx, engine.Bus),
		"realtime":   componentHealth(ctx, engine.Realtime),
		"knowledge":  componentHealth(ctx, engine.KB),
		"aiWorkflow": componentHealth(ctx, engine.AIWorkflows),
	}
	status := "RUNNING"
	for name, value := range components {
		if name != "knowledge" && name != "aiWorkflow" && value != "HEALTHY" {
			status = "DEGRADED"
		}
	}
	result := map[string]any{
		"tenantId": tenant, "generatedAt": time.Now().UnixMilli(), "systemStatus": status, "components": components,
		"products":         map[string]any{"total": len(products), "byStatus": productStatus, "byCategory": productCategory},
		"protocolPackages": map[string]any{"total": len(protocols), "byStatus": protocolStatus},
		"devices":          map[string]any{"total": len(devices), "byStatus": deviceStatus, "byRole": deviceRole, "autoRegistered": autoRegistered, "reported": reported, "neverReported": max(0, len(devices)-reported), "discoveredUnregistered": discovered, "connectionStatus": connectionStatus, "dataStatus": dataStatus, "businessStatus": businessStatus, "latestSeenAt": latestSeenAt},
		"alarms":           map[string]any{"loaded": len(alarms), "truncated": len(alarms) == 10000, "active": active, "highRiskActive": highActive, "triggeredLast24h": recent24h, "byStatus": alarmStatus, "byLevel": alarmLevel, "bySource": alarmSource},
		"rules":            map[string]any{"total": len(rules), "enabled": ruleEnabled, "disabled": len(rules) - ruleEnabled},
		"cameras":          map[string]any{"total": len(cameras), "enabled": cameraEnabled, "linkedDevices": linkedDevices},
		"knowledge":        map[string]any{"documents": len(documents), "indexed": indexedDocs, "chunks": chunks},
	}
	for field, menu := range map[string]string{"products": "products", "protocolPackages": "protocols", "rules": "rules", "cameras": "cameras", "knowledge": "knowledge"} {
		if !can(menu) {
			delete(result, field)
		}
	}
	return result, nil

}

type healthChecker interface{ Health(context.Context) error }

func componentHealth(ctx context.Context, component healthChecker) string {
	if component == nil {
		return "DISABLED"
	}
	healthCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := component.Health(healthCtx); err != nil {
		return "UNAVAILABLE"
	}
	return "HEALTHY"
}

func increment(counts map[string]int, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "UNKNOWN"
	}
	counts[value]++
}

func tenantForTool(ctx context.Context, scope string, harness bool) (string, error) {
	c, ok := auth.ClaimsFromContext(ctx)
	if !ok || c.TenantID == "" {
		return "", fmt.Errorf("authenticated tenant context is required")
	}
	if harness && (c.TokenUse != "harness" || !c.HasAudience(auth.HarnessAudience) || !c.HasScope(scope)) {
		return "", fmt.Errorf("harness token is not authorized for %s", scope)
	}
	return c.TenantID, nil
}

func boundedLimit(value, fallback, maximum int) int {
	if value <= 0 {
		return fallback
	}
	if value > maximum {
		return maximum
	}
	return value
}

func tenantFrom(ctx context.Context) (string, error) {
	c, ok := auth.ClaimsFromContext(ctx)
	if !ok || c.TenantID == "" {
		return "", fmt.Errorf("authenticated tenant context is required")
	}
	return c.TenantID, nil
}
func result(v any, err error) (*mcp.CallToolResult, error) {
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	b, _ := json.Marshal(v)
	return mcp.NewToolResultText(string(b)), nil
}
func auditedResult(ctx context.Context, engine *core.Engine, tool string, input map[string]any, v any, err error) (*mcp.CallToolResult, error) {
	c, _ := auth.ClaimsFromContext(ctx)
	traceID := fmt.Sprintf("tool_%d", time.Now().UnixNano())
	if c.RunID != "" {
		traceID = c.RunID
	}
	entry := model.AIToolCallLog{ID: traceID, TenantID: c.TenantID, Actor: c.Username, Tool: tool, Input: input, Output: v, Success: err == nil, CreatedAt: time.Now().UnixMilli()}
	if err != nil {
		entry.Error = err.Error()
	}
	auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	_ = engine.Repo.SaveAIToolCall(auditCtx, entry)
	return result(v, err)
}
