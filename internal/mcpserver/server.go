package mcpserver /* 声明 mcpserver 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"       /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */
	"net/http"      /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */
	"time"          /* 执行当前语句并推进处理流程。 */

	"github.com/mark3labs/mcp-go/mcp"    /* 执行当前语句并推进处理流程。 */
	"github.com/mark3labs/mcp-go/server" /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/aioutput"
	"iot-platform/internal/auth"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/core"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func New(engine *core.Engine) http.Handler { /* 定义 New 函数。 */
	return newServer(engine, false, "/mcp") /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// NewHarness exposes a deliberately smaller, safe MCP surface for the AI
// sidecar. Authentication, audience and token-use checks live at the HTTP
// boundary; each tool additionally enforces its exact scope here.
func NewHarness(engine *core.Engine) http.Handler { /* 定义 NewHarness 函数。 */
	return newServer(engine, true, "/mcp/harness") /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func newServer(engine *core.Engine, harness bool, endpoint string) http.Handler { /* 定义 newServer 函数。 */
	s := server.NewMCPServer("iot-platform-tools", "1.0.0", server.WithToolCapabilities(false), server.WithInstructions("查询消防物联网数据，或生成并保存待人工确认的禁用规则草稿；不能执行 SQL、控制设备或自动启用规则。"), server.WithRecovery()) /* 更新 s 的值。 */
	s.AddTool(mcp.NewTool("query_system_overview", mcp.WithDescription("统计当前租户的系统状态、产品、设备、在线状态、告警、规则、摄像头和知识库数量")), func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {    /* 执行当前语句并推进处理流程。 */
		tenant, err := tenantForTool(ctx, auth.ScopeQuerySystemOverview, harness) /* 更新 err 的值。 */
		if err != nil {                                                           /* 判断条件并选择处理分支。 */
			return mcp.NewToolResultError(err.Error()), nil /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		v, err := buildSystemOverview(ctx, engine, tenant)                                   /* 更新 err 的值。 */
		return auditedResult(ctx, engine, "query_system_overview", map[string]any{}, v, err) /* 返回当前处理结果。 */
	}) /* 结束当前表达式或代码块。 */
	s.AddTool(mcp.NewTool("query_device_latest", mcp.WithDescription("查询当前租户内某设备的最新在线和业务状态"), mcp.WithString("deviceId", mcp.Required(), mcp.Description("设备 ID"))), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) { /* 执行当前语句并推进处理流程。 */
		tenant, err := tenantForTool(ctx, auth.ScopeQueryDeviceLatest, harness) /* 更新 err 的值。 */
		if err != nil {                                                         /* 判断条件并选择处理分支。 */
			return mcp.NewToolResultError(err.Error()), nil /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		v, err := engine.Repo.GetDeviceState(ctx, tenant, req.GetString("deviceId", ""))                                            /* 更新 err 的值。 */
		return auditedResult(ctx, engine, "query_device_latest", map[string]any{"deviceId": req.GetString("deviceId", "")}, v, err) /* 返回当前处理结果。 */
	}) /* 结束当前表达式或代码块。 */
	s.AddTool(mcp.NewTool("query_alarm_list", mcp.WithDescription("按状态、等级、设备和时间范围查询当前租户告警"), mcp.WithString("deviceId"), mcp.WithString("status"), mcp.WithString("level"), mcp.WithNumber("start"), mcp.WithNumber("end"), mcp.WithNumber("limit")), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) { /* 执行当前语句并推进处理流程。 */
		tenant, err := tenantForTool(ctx, auth.ScopeQueryAlarmList, harness) /* 更新 err 的值。 */
		if err != nil {                                                      /* 判断条件并选择处理分支。 */
			return mcp.NewToolResultError(err.Error()), nil /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		limit := req.GetInt("limit", 100) /* 更新 limit 的值。 */
		if harness {                      /* 判断条件并选择处理分支。 */
			limit = boundedLimit(limit, 100, 100) /* 更新 limit 的值。 */
		} /* 结束当前表达式或代码块。 */
		v, err := engine.Repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: tenant, DeviceID: req.GetString("deviceId", ""), Status: req.GetString("status", ""), Level: req.GetString("level", ""), Start: int64(req.GetInt("start", 0)), End: int64(req.GetInt("end", 0)), Limit: limit}) /* 更新 err 的值。 */
		return auditedResult(ctx, engine, "query_alarm_list", map[string]any{"deviceId": req.GetString("deviceId", ""), "status": req.GetString("status", ""), "level": req.GetString("level", "")}, v, err)                                                                              /* 返回当前处理结果。 */
	}) /* 结束当前表达式或代码块。 */
	s.AddTool(mcp.NewTool("query_property_history", mcp.WithDescription("查询当前租户内设备属性历史趋势"), mcp.WithString("deviceId", mcp.Required()), mcp.WithString("propertyCode", mcp.Required()), mcp.WithNumber("start", mcp.Required()), mcp.WithNumber("end", mcp.Required()), mcp.WithNumber("limit")), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) { /* 执行当前语句并推进处理流程。 */
		tenant, err := tenantForTool(ctx, auth.ScopeQueryPropertyHistory, harness) /* 更新 err 的值。 */
		if err != nil {                                                            /* 判断条件并选择处理分支。 */
			return mcp.NewToolResultError(err.Error()), nil /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		limit := req.GetInt("limit", 1000) /* 更新 limit 的值。 */
		if harness {                       /* 判断条件并选择处理分支。 */
			limit = boundedLimit(limit, 200, 500) /* 更新 limit 的值。 */
		} /* 结束当前表达式或代码块。 */
		v, err := engine.Repo.PropertyHistory(ctx, tenant, req.GetString("deviceId", ""), req.GetString("propertyCode", ""), int64(req.GetInt("start", 0)), int64(req.GetInt("end", int(time.Now().UnixMilli()))), limit) /* 更新 err 的值。 */
		return auditedResult(ctx, engine, "query_property_history", map[string]any{"deviceId": req.GetString("deviceId", ""), "propertyCode": req.GetString("propertyCode", "")}, v, err)                                 /* 返回当前处理结果。 */
	}) /* 结束当前表达式或代码块。 */
	s.AddTool(mcp.NewTool("query_similar_alarms", mcp.WithDescription("查询同设备、同类型的历史告警"), mcp.WithString("deviceId", mcp.Required()), mcp.WithNumber("limit")), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) { /* 执行当前语句并推进处理流程。 */
		tenant, err := tenantForTool(ctx, auth.ScopeQuerySimilarAlarms, harness) /* 更新 err 的值。 */
		if err != nil {                                                          /* 判断条件并选择处理分支。 */
			return mcp.NewToolResultError(err.Error()), nil /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		limit := req.GetInt("limit", 20) /* 更新 limit 的值。 */
		if harness {                     /* 判断条件并选择处理分支。 */
			limit = boundedLimit(limit, 20, 50) /* 更新 limit 的值。 */
		} /* 结束当前表达式或代码块。 */
		v, err := engine.Repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: tenant, DeviceID: req.GetString("deviceId", ""), Limit: limit}) /* 更新 err 的值。 */
		return auditedResult(ctx, engine, "query_similar_alarms", map[string]any{"deviceId": req.GetString("deviceId", "")}, v, err)      /* 返回当前处理结果。 */
	}) /* 结束当前表达式或代码块。 */
	s.AddTool(mcp.NewTool("query_knowledge_base", mcp.WithDescription("按当前 Agent 直接绑定的知识文档检索设备手册、SOP 与维修知识"), mcp.WithString("question", mcp.Required()), mcp.WithString("workflowId", mcp.Required()), mcp.WithNumber("limit"), mcp.WithNumber("minScore")), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) { /* 执行当前语句并推进处理流程。 */
		tenant, err := tenantForTool(ctx, auth.ScopeQueryKnowledgeBase, harness) /* 更新 err 的值。 */
		if err != nil {                                                          /* 判断条件并选择处理分支。 */
			return mcp.NewToolResultError(err.Error()), nil /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if engine.KB == nil { /* 判断条件并选择处理分支。 */
			return mcp.NewToolResultError("knowledge base disabled"), nil /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		limit := req.GetInt("limit", 5) /* 更新 limit 的值。 */
		if harness {                    /* 判断条件并选择处理分支。 */
			limit = boundedLimit(limit, 5, 20) /* 更新 limit 的值。 */
		} /* 结束当前表达式或代码块。 */
		workflowID := req.GetString("workflowId", "")                                                  /* 更新 workflowID 的值。 */
		minScore := req.GetFloat("minScore", 0)                                                        /* 更新 minScore 的值。 */
		if c, ok := auth.ClaimsFromContext(ctx); ok && c.TokenUse == "harness" && c.Knowledge != nil { /* 判断条件并选择处理分支。 */
			workflowID = c.Knowledge.WorkflowID                   /* 更新 workflowID 的值。 */
			if c.Knowledge.TopK > 0 && limit > c.Knowledge.TopK { /* 判断条件并选择处理分支。 */
				limit = c.Knowledge.TopK /* 更新 limit 的值。 */
			} /* 结束当前表达式或代码块。 */
			if minScore < c.Knowledge.MinScore { /* 判断条件并选择处理分支。 */
				minScore = c.Knowledge.MinScore /* 更新 minScore 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		input := map[string]any{"question": req.GetString("question", ""), "workflowId": workflowID, "limit": limit, "minScore": minScore} /* 更新 input 的值。 */
		if filtered, ok := engine.KB.(ports.FilteredKnowledgeBase); ok {                                                                   /* 判断条件并选择处理分支。 */
			v, searchErr := filtered.SearchKnowledge(ctx, ports.KnowledgeSearchRequest{TenantID: tenant, WorkflowID: workflowID, Question: req.GetString("question", ""), Limit: limit, MinScore: minScore}) /* 更新 searchErr 的值。 */
			return auditedResult(ctx, engine, "query_knowledge_base", input, v, searchErr)                                                                                                                   /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return auditedResult(ctx, engine, "query_knowledge_base", input, nil, fmt.Errorf("workflow-bound knowledge search is not supported by the configured index")) /* 返回当前处理结果。 */
	}) /* 结束当前表达式或代码块。 */
	// The calling Agent already is the model: it writes the rule JSON itself and
	// this tool only normalises, validates and saves it as a disabled draft, so
	// no second model run is started inside a Harness run.
	s.AddTool(mcp.NewTool("create_rule_draft", mcp.WithDescription("把你按规定格式写好的规则 JSON 保存为禁用草稿；不会启用或执行，必须由用户人工确认。格式要求："+aioutput.RuleDraftInstructions), mcp.WithString("ruleJson", mcp.Required(), mcp.Description("规则 JSON 对象文本")), mcp.WithString("inputText", mcp.Description("用户原始需求，用于审计"))), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		tenant, err := tenantForTool(ctx, auth.ScopeCreateRuleDraft, harness) /* 更新 err 的值。 */
		if !harness {                                                         /* 判断条件并选择处理分支。 */
			tenant, err = tenantFrom(ctx) /* 更新 err 的值。 */
		} /* 结束当前表达式或代码块。 */
		if err != nil { /* 判断条件并选择处理分支。 */
			return mcp.NewToolResultError(err.Error()), nil /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
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
			if v.ID == "" { /* 判断条件并选择处理分支。 */
				v.ID = fmt.Sprintf("rule_draft_%d", time.Now().UnixNano()) /* 更新 v.ID 的值。 */
			} /* 结束当前表达式或代码块。 */
			if v.Match == "" { /* 判断条件并选择处理分支。 */
				v.Match = "all" /* 更新 v.Match 的值。 */
			} /* 结束当前表达式或代码块。 */
			if v.Version == 0 { /* 判断条件并选择处理分支。 */
				v.Version = 1 /* 更新 v.Version 的值。 */
			} /* 结束当前表达式或代码块。 */
			_, _, draftErr = engine.ValidateRuleDraft(ctx, v) /* 更新 draftErr 的值。 */
			if draftErr == nil {                              /* 判断条件并选择处理分支。 */
				now := time.Now().UnixMilli()           /* 更新 now 的值。 */
				v.CreatedAt, v.UpdatedAt = now, now     /* 更新 v.UpdatedAt 的值。 */
				draftErr = engine.Repo.SaveRule(ctx, v) /* 更新 draftErr 的值。 */
				engine.RulesChanged(tenant)
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		output := map[string]any{"kind": "ruleDraft", "draft": v, "persisted": draftErr == nil, "requiresHumanApproval": true} /* 更新 output 的值。 */
		return auditedResult(ctx, engine, "create_rule_draft", map[string]any{"inputText": inputText}, output, draftErr)       /* 返回当前处理结果。 */
	}) /* 结束当前表达式或代码块。 */
	options := []server.StreamableHTTPOption{ /* 更新 options 的值。 */
		server.WithStateLess(true),        /* 执行当前语句并推进处理流程。 */
		server.WithEndpointPath(endpoint), /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if harness { /* 判断条件并选择处理分支。 */
		// The Harness sidecar reaches the local API through Docker Desktop's
		// host.docker.internal bridge. The Go server sees that connection as
		// loopback, while the Host header is host.docker.internal:port. Keep
		// the MCP endpoint's JWT/audience/scope checks as the security boundary
		// and disable only the transport's localhost Host-header check here.
		options = append(options, server.WithDisableLocalhostProtection(true)) /* 更新 options 的值。 */
	} /* 结束当前表达式或代码块。 */
	return server.NewStreamableHTTPServer(s, options...) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func buildSystemOverview(ctx context.Context, engine *core.Engine, tenant string) (map[string]any, error) { /* 定义 buildSystemOverview 函数。 */
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

	productStatus, productCategory := map[string]int{}, map[string]int{} /* 更新 productCategory 的值。 */
	for _, item := range products {                                      /* 循环处理当前数据。 */
		increment(productStatus, item.Status)     /* 执行当前语句并推进处理流程。 */
		increment(productCategory, item.Category) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	protocolStatus := map[string]int{} /* 更新 protocolStatus 的值。 */
	for _, item := range protocols {   /* 循环处理当前数据。 */
		increment(protocolStatus, item.Status) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	deviceStatus, deviceRole := map[string]int{}, map[string]int{} /* 更新 deviceRole 的值。 */
	registered := make(map[string]struct{}, len(devices))          /* 更新 registered 的值。 */
	autoRegistered := 0                                            /* 更新 autoRegistered 的值。 */
	for _, item := range devices {                                 /* 循环处理当前数据。 */
		registered[item.ID] = struct{}{}       /* 更新 registered[item.ID] 的值。 */
		increment(deviceStatus, item.Status)   /* 执行当前语句并推进处理流程。 */
		increment(deviceRole, item.DeviceRole) /* 执行当前语句并推进处理流程。 */
		if item.AutoRegistered {               /* 判断条件并选择处理分支。 */
			autoRegistered++ /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	connectionStatus, dataStatus, businessStatus := map[string]int{}, map[string]int{}, map[string]int{} /* 更新 businessStatus 的值。 */
	reported, discovered, latestSeenAt := 0, 0, int64(0)                                                 /* 更新 latestSeenAt 的值。 */
	for _, item := range states {                                                                        /* 循环处理当前数据。 */
		if _, ok := registered[item.DeviceID]; !ok { /* 判断条件并选择处理分支。 */
			discovered++ /* 执行当前语句并推进处理流程。 */
			continue     /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		reported++                                         /* 执行当前语句并推进处理流程。 */
		increment(connectionStatus, item.ConnectionStatus) /* 执行当前语句并推进处理流程。 */
		increment(dataStatus, item.DataStatus)             /* 执行当前语句并推进处理流程。 */
		increment(businessStatus, item.BusinessStatus)     /* 执行当前语句并推进处理流程。 */
		if item.LastSeenAt > latestSeenAt {                /* 判断条件并选择处理分支。 */
			latestSeenAt = item.LastSeenAt /* 更新 latestSeenAt 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	ruleEnabled := 0             /* 更新 ruleEnabled 的值。 */
	for _, item := range rules { /* 循环处理当前数据。 */
		if item.Enabled { /* 判断条件并选择处理分支。 */
			ruleEnabled++ /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	alarmStatus, alarmLevel, alarmSource := map[string]int{}, map[string]int{}, map[string]int{} /* 更新 alarmSource 的值。 */
	active, highActive, recent24h := 0, 0, 0                                                     /* 更新 recent24h 的值。 */
	cutoff := time.Now().Add(-24 * time.Hour).UnixMilli()                                        /* 更新 cutoff 的值。 */
	for _, item := range alarms {                                                                /* 循环处理当前数据。 */
		increment(alarmStatus, item.Status)    /* 执行当前语句并推进处理流程。 */
		increment(alarmLevel, item.AlarmLevel) /* 执行当前语句并推进处理流程。 */
		increment(alarmSource, item.Source)    /* 执行当前语句并推进处理流程。 */
		if item.Status == "ACTIVE" {           /* 判断条件并选择处理分支。 */
			active++                                                                                          /* 执行当前语句并推进处理流程。 */
			if item.AlarmLevel == "HIGH" || item.AlarmLevel == "CRITICAL" || item.AlarmLevel == "EMERGENCY" { /* 判断条件并选择处理分支。 */
				highActive++ /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		if item.LastTriggeredAt >= cutoff { /* 判断条件并选择处理分支。 */
			recent24h++ /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	cameraEnabled, linkedDevices := 0, 0 /* 更新 linkedDevices 的值。 */
	for _, item := range cameras {       /* 循环处理当前数据。 */
		if item.Enabled { /* 判断条件并选择处理分支。 */
			cameraEnabled++ /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if item.Enabled && strings.TrimSpace(item.DeviceID) != "" { /* 判断条件并选择处理分支。 */
			linkedDevices++ /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	indexedDocs, chunks := 0, 0      /* 更新 chunks 的值。 */
	for _, item := range documents { /* 循环处理当前数据。 */
		if item.Status == "INDEXED" { /* 判断条件并选择处理分支。 */
			indexedDocs++ /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		switch value := item.Metadata["chunks"].(type) { /* 根据条件选择处理路径。 */
		case int: /* 处理当前分支。 */
			chunks += value /* 更新 chunks 的值。 */
		case float64: /* 处理当前分支。 */
			chunks += int(value) /* 更新 chunks 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	components := map[string]string{ /* 更新 components 的值。 */
		"repository": componentHealth(ctx, engine.Repo),        /* 执行当前语句并推进处理流程。 */
		"archive":    componentHealth(ctx, engine.Archive),     /* 执行当前语句并推进处理流程。 */
		"eventBus":   componentHealth(ctx, engine.Bus),         /* 执行当前语句并推进处理流程。 */
		"realtime":   componentHealth(ctx, engine.Realtime),    /* 执行当前语句并推进处理流程。 */
		"knowledge":  componentHealth(ctx, engine.KB),          /* 执行当前语句并推进处理流程。 */
		"aiWorkflow": componentHealth(ctx, engine.AIWorkflows), /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	status := "RUNNING"                   /* 更新 status 的值。 */
	for name, value := range components { /* 循环处理当前数据。 */
		if name != "knowledge" && name != "aiWorkflow" && value != "HEALTHY" { /* 判断条件并选择处理分支。 */
			status = "DEGRADED" /* 更新 status 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	result := map[string]any{ /* 返回当前处理结果。 */
		"tenantId": tenant, "generatedAt": time.Now().UnixMilli(), "systemStatus": status, "components": components, /* 执行当前语句并推进处理流程。 */
		"products":         map[string]any{"total": len(products), "byStatus": productStatus, "byCategory": productCategory},                                                                                                                                                                                                                                                                    /* 执行当前语句并推进处理流程。 */
		"protocolPackages": map[string]any{"total": len(protocols), "byStatus": protocolStatus},                                                                                                                                                                                                                                                                                                 /* 执行当前语句并推进处理流程。 */
		"devices":          map[string]any{"total": len(devices), "byStatus": deviceStatus, "byRole": deviceRole, "autoRegistered": autoRegistered, "reported": reported, "neverReported": max(0, len(devices)-reported), "discoveredUnregistered": discovered, "connectionStatus": connectionStatus, "dataStatus": dataStatus, "businessStatus": businessStatus, "latestSeenAt": latestSeenAt}, /* 执行当前语句并推进处理流程。 */
		"alarms":           map[string]any{"loaded": len(alarms), "truncated": len(alarms) == 10000, "active": active, "highRiskActive": highActive, "triggeredLast24h": recent24h, "byStatus": alarmStatus, "byLevel": alarmLevel, "bySource": alarmSource},                                                                                                                                    /* 执行当前语句并推进处理流程。 */
		"rules":            map[string]any{"total": len(rules), "enabled": ruleEnabled, "disabled": len(rules) - ruleEnabled},                                                                                                                                                                                                                                                                   /* 执行当前语句并推进处理流程。 */
		"cameras":          map[string]any{"total": len(cameras), "enabled": cameraEnabled, "linkedDevices": linkedDevices},                                                                                                                                                                                                                                                                     /* 执行当前语句并推进处理流程。 */
		"knowledge":        map[string]any{"documents": len(documents), "indexed": indexedDocs, "chunks": chunks},                                                                                                                                                                                                                                                                               /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	for field, menu := range map[string]string{"products": "products", "protocolPackages": "protocols", "rules": "rules", "cameras": "cameras", "knowledge": "knowledge"} {
		if !can(menu) {
			delete(result, field)
		}
	}
	return result, nil

} /* 结束当前表达式或代码块。 */

type healthChecker interface{ Health(context.Context) error } /* 定义 healthChecker 类型。 */

func componentHealth(ctx context.Context, component healthChecker) string { /* 定义 componentHealth 函数。 */
	if component == nil { /* 判断条件并选择处理分支。 */
		return "DISABLED" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	healthCtx, cancel := context.WithTimeout(ctx, 2*time.Second) /* 更新 cancel 的值。 */
	defer cancel()                                               /* 安排函数结束时执行清理。 */
	if err := component.Health(healthCtx); err != nil {          /* 判断条件并选择处理分支。 */
		return "UNAVAILABLE" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return "HEALTHY" /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func increment(counts map[string]int, value string) { /* 定义 increment 函数。 */
	value = strings.TrimSpace(value) /* 更新 value 的值。 */
	if value == "" {                 /* 判断条件并选择处理分支。 */
		value = "UNKNOWN" /* 更新 value 的值。 */
	} /* 结束当前表达式或代码块。 */
	counts[value]++ /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func tenantForTool(ctx context.Context, scope string, harness bool) (string, error) { /* 定义 tenantForTool 函数。 */
	c, ok := auth.ClaimsFromContext(ctx) /* 更新 ok 的值。 */
	if !ok || c.TenantID == "" {         /* 判断条件并选择处理分支。 */
		return "", fmt.Errorf("authenticated tenant context is required") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if harness && (c.TokenUse != "harness" || !c.HasAudience(auth.HarnessAudience) || !c.HasScope(scope)) { /* 判断条件并选择处理分支。 */
		return "", fmt.Errorf("harness token is not authorized for %s", scope) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return c.TenantID, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func boundedLimit(value, fallback, maximum int) int { /* 定义 boundedLimit 函数。 */
	if value <= 0 { /* 判断条件并选择处理分支。 */
		return fallback /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if value > maximum { /* 判断条件并选择处理分支。 */
		return maximum /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return value /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func tenantFrom(ctx context.Context) (string, error) { /* 定义 tenantFrom 函数。 */
	c, ok := auth.ClaimsFromContext(ctx) /* 更新 ok 的值。 */
	if !ok || c.TenantID == "" {         /* 判断条件并选择处理分支。 */
		return "", fmt.Errorf("authenticated tenant context is required") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return c.TenantID, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func result(v any, err error) (*mcp.CallToolResult, error) { /* 定义 result 函数。 */
	if err != nil { /* 判断条件并选择处理分支。 */
		return mcp.NewToolResultError(err.Error()), nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	b, _ := json.Marshal(v)                      /* 更新 _ 的值。 */
	return mcp.NewToolResultText(string(b)), nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func auditedResult(ctx context.Context, engine *core.Engine, tool string, input map[string]any, v any, err error) (*mcp.CallToolResult, error) { /* 定义 auditedResult 函数。 */
	c, _ := auth.ClaimsFromContext(ctx)                      /* 更新 _ 的值。 */
	traceID := fmt.Sprintf("tool_%d", time.Now().UnixNano()) /* 更新 traceID 的值。 */
	if c.RunID != "" {                                       /* 判断条件并选择处理分支。 */
		traceID = c.RunID /* 更新 traceID 的值。 */
	} /* 结束当前表达式或代码块。 */
	entry := model.AIToolCallLog{ID: traceID, TenantID: c.TenantID, Actor: c.Username, Tool: tool, Input: input, Output: v, Success: err == nil, CreatedAt: time.Now().UnixMilli()} /* 检查错误并决定后续处理。 */
	if err != nil {                                                                                                                                                                 /* 判断条件并选择处理分支。 */
		entry.Error = err.Error() /* 更新 entry.Error 的值。 */
	} /* 结束当前表达式或代码块。 */
	auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second) /* 更新 cancel 的值。 */
	defer cancel()                                                                     /* 安排函数结束时执行清理。 */
	_ = engine.Repo.SaveAIToolCall(auditCtx, entry)                                    /* 更新 _ 的值。 */
	return result(v, err)                                                              /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
