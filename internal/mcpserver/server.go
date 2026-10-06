package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iot-platform/internal/aiprompt"
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
	s.AddTool(mcp.NewTool("query_alarm_list", mcp.WithDescription("按状态、等级、设备和时间范围分页查询当前租户告警摘要（不含遥测明细）；需要明细时用 query_alarm_detail"), mcp.WithString("deviceId"), mcp.WithString("status"), mcp.WithString("level"), mcp.WithNumber("start"), mcp.WithNumber("end"), mcp.WithNumber("limit", mcp.Description("默认 20，最多 50")), mcp.WithNumber("offset")), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		tenant, err := tenantForTool(ctx, auth.ScopeQueryAlarmList, harness)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		filter := ports.AlarmFilter{TenantID: tenant, DeviceID: req.GetString("deviceId", ""), Status: req.GetString("status", ""), Level: req.GetString("level", ""), Start: int64(req.GetInt("start", 0)), End: int64(req.GetInt("end", 0))}
		v, err := alarmPage(ctx, engine, filter, "", req)
		return auditedResult(ctx, engine, "query_alarm_list", map[string]any{"deviceId": filter.DeviceID, "status": filter.Status, "level": filter.Level}, v, err)
	})
	s.AddTool(mcp.NewTool("query_alarm_detail", mcp.WithDescription("按告警 ID 查询当前租户单条告警的完整信息，包括遥测明细、位置、摄像头与核实结论"), mcp.WithString("alarmId", mcp.Required(), mcp.Description("告警 ID"))), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		tenant, err := tenantForTool(ctx, auth.ScopeQueryAlarmDetail, harness)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		id := req.GetString("alarmId", "")
		// The scoped repository reports an alarm of an ungranted device the
		// same way as a missing one.
		v, err := engine.Repo.GetAlarm(ctx, tenant, id)
		if err != nil {
			err = errors.New("告警不存在或无访问权限")
		}
		return auditedResult(ctx, engine, "query_alarm_detail", map[string]any{"alarmId": id}, v, err)
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
	s.AddTool(mcp.NewTool("query_similar_alarms", mcp.WithDescription("查询同设备的历史告警摘要，可按告警类型过滤；按时间倒序分页"), mcp.WithString("deviceId", mcp.Required()), mcp.WithString("alarmType", mcp.Description("告警类型，如 SMOKE_DETECTED；为空时不限类型")), mcp.WithNumber("limit", mcp.Description("默认 20，最多 50")), mcp.WithNumber("offset")), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		tenant, err := tenantForTool(ctx, auth.ScopeQuerySimilarAlarms, harness)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		alarmType := strings.ToUpper(strings.TrimSpace(req.GetString("alarmType", "")))
		v, err := alarmPage(ctx, engine, ports.AlarmFilter{TenantID: tenant, DeviceID: req.GetString("deviceId", "")}, alarmType, req)
		return auditedResult(ctx, engine, "query_similar_alarms", map[string]any{"deviceId": req.GetString("deviceId", ""), "alarmType": alarmType}, v, err)
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
	s.AddTool(mcp.NewTool("create_rule_draft", mcp.WithDescription("把你按规定格式写好的规则 JSON 保存为禁用草稿；不会启用或执行，必须由用户人工确认。格式要求："+aiprompt.RuleDraftInstructions), mcp.WithString("ruleJson", mcp.Required(), mcp.Description("规则 JSON 对象文本")), mcp.WithString("inputText", mcp.Description("用户原始需求，用于审计"))), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		tenant, err := tenantForTool(ctx, auth.ScopeCreateRuleDraft, harness)
		if !harness {
			// Browser sessions carry no per-action permissions here; only the
			// built-in administrator may save drafts outside a Harness run.
			tenant, err = builtinAdminTenant(ctx)
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
	// Devices and alarms are counted in the store so the overview does not
	// grow with the tenant's fleet or alarm history.
	devices := model.NewDeviceOverview()
	alarms := model.NewAlarmOverview()
	if can("devices") {
		devices, err = engine.Repo.DeviceOverviewCounts(ctx, tenant, false, nil)
		if err != nil {
			return nil, err
		}
		alarms, err = engine.Repo.AlarmOverviewCounts(ctx, ports.AlarmFilter{TenantID: tenant}, time.Now().Add(-24*time.Hour).UnixMilli())
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
	var cameras []model.VideoCameraMapping
	if can("cameras") {
		cameras, err = engine.Repo.ListVideoCameraMappings(ctx, tenant)
		if err != nil {
			return nil, err
		}
	}
	var knowledgeSummary model.KnowledgeSummary
	if can("knowledge") {
		knowledgeSummary, err = engine.Repo.KnowledgeDocSummary(ctx, tenant)
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
	ruleEnabled := 0
	for _, item := range rules {
		if item.Enabled {
			ruleEnabled++
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
		"devices":          map[string]any{"total": devices.Total, "byStatus": devices.ByStatus, "byRole": devices.ByRole, "autoRegistered": devices.AutoRegistered, "reported": devices.Reported, "neverReported": max(0, devices.Total-devices.Reported), "discoveredUnregistered": devices.DiscoveredUnregistered, "connectionStatus": devices.ConnectionStatus, "dataStatus": devices.DataStatus, "businessStatus": devices.BusinessStatus, "latestSeenAt": devices.LatestSeenAt},
		"alarms":           map[string]any{"total": alarms.Total, "active": alarms.Active, "highRiskActive": alarms.HighRiskActive, "triggeredLast24h": alarms.Recent, "byStatus": alarms.ByStatus, "byLevel": alarms.ByLevel, "bySource": alarms.BySource},
		"rules":            map[string]any{"total": len(rules), "enabled": ruleEnabled, "disabled": len(rules) - ruleEnabled},
		"cameras":          map[string]any{"total": len(cameras), "enabled": cameraEnabled, "linkedDevices": linkedDevices},
		"knowledge":        map[string]any{"documents": knowledgeSummary.Documents, "indexed": knowledgeSummary.Indexed, "chunks": knowledgeSummary.Chunks},
	}
	for field, menu := range map[string]string{"products": "products", "protocolPackages": "protocols", "rules": "rules", "cameras": "cameras", "knowledge": "knowledge"} {
		if !can(menu) {
			delete(result, field)
		}
	}
	return result, nil

}

// alarmPageLimit and alarmPageMax bound the alarm summaries one tool call
// returns, keeping tool results small for the model's context.
const (
	alarmPageLimit = 20
	alarmPageMax   = 50
)

// alarmPage returns one page of alarm summaries (telemetry details and camera
// payloads omitted) with the filtered total and the next offset, or -1 when
// there are no more. alarmType, when set, filters the alarm type.
func alarmPage(ctx context.Context, engine *core.Engine, filter ports.AlarmFilter, alarmType string, req mcp.CallToolRequest) (map[string]any, error) {
	limit := boundedLimit(req.GetInt("limit", alarmPageLimit), alarmPageLimit, alarmPageMax)
	offset := max(req.GetInt("offset", 0), 0)
	filter.Summary = true
	items := []model.Alarm{}
	total := 0
	filter.AlarmType = alarmType
	if alarmType != "" && filter.Start == 0 {
		// Type lookups compare history; without an explicit window they read
		// the last 90 days, not a device's whole alarm table.
		filter.Start = time.Now().Add(-90 * 24 * time.Hour).UnixMilli()
	}
	filter.Limit, filter.Offset = limit, offset
	page, err := engine.Repo.ListAlarms(ctx, filter)
	if err != nil {
		return nil, err
	}
	if total, err = engine.Repo.CountAlarms(ctx, filter); err != nil {
		return nil, err
	}
	items = page
	next := offset + len(items)
	if next >= total {
		next = -1
	}
	return map[string]any{"items": items, "total": total, "nextOffset": next}, nil
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

func builtinAdminTenant(ctx context.Context) (string, error) {
	c, ok := auth.ClaimsFromContext(ctx)
	if !ok || c.TenantID == "" || c.TokenUse != "" || c.Role != "admin" {
		return "", fmt.Errorf("rule drafts outside a Harness run require the built-in administrator")
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

// auditOutputLimit bounds what a tool call log keeps of the tool result. Small
// results such as a saved rule draft stay whole; telemetry and knowledge
// passages are summarised, since the model already received them and the log
// only needs to show what was returned.
const auditOutputLimit = 4 << 10

func auditOutput(v any) any {
	encoded, err := json.Marshal(v)
	if err != nil || len(encoded) <= auditOutputLimit {
		return v
	}
	summary := map[string]any{"truncated": true, "bytes": len(encoded)}
	var list []json.RawMessage
	if json.Unmarshal(encoded, &list) == nil {
		summary["items"] = len(list)
	} else {
		var object map[string]json.RawMessage
		if json.Unmarshal(encoded, &object) == nil {
			if json.Unmarshal(object["items"], &list) == nil {
				summary["items"] = len(list)
			}
			if total, ok := object["total"]; ok {
				summary["total"] = total
			}
		}
	}
	return summary
}

func auditedResult(ctx context.Context, engine *core.Engine, tool string, input map[string]any, v any, err error) (*mcp.CallToolResult, error) {
	c, _ := auth.ClaimsFromContext(ctx)
	traceID := fmt.Sprintf("tool_%d", time.Now().UnixNano())
	if c.RunID != "" {
		traceID = c.RunID
	}
	entry := model.AIToolCallLog{ID: traceID, TenantID: c.TenantID, Actor: c.Username, Tool: tool, Input: input, Output: auditOutput(v), Success: err == nil, CreatedAt: time.Now().UnixMilli()}
	if err != nil {
		entry.Error = err.Error()
	}
	auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	_ = engine.Repo.SaveAIToolCall(auditCtx, entry)
	return result(v, err)
}
