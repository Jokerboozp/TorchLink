package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"sort"

	"iot-platform/internal/auth"
	"iot-platform/internal/core"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// Tool permissions are derived from the same routes used by the browser.
func workflowScopes(ctx context.Context) []string {
	p, managed := ctx.Value(permissionsKey{}).(map[string]bool)
	if !managed {
		return auth.HarnessReadScopes()
	}
	required := map[string]bool{
		auth.ScopeQuerySystemOverview:   p["menu:dashboard"],
		auth.ScopeQueryDeviceLatest:     p["menu:devices"],
		auth.ScopeQueryPropertyHistory:  p["menu:devices"],
		auth.ScopeQueryAlarmList:        allowsRoute(p, "GET", "/api/v1/alarms"),
		auth.ScopeQuerySimilarAlarms:    allowsRoute(p, "GET", "/api/v1/alarms"),
		auth.ScopeQueryKnowledgeBase:    p["menu:knowledge"],
		auth.ScopeCreateRuleDraft:       allowsRoute(p, "POST", "/api/v1/ai/rule-draft"),
		auth.ScopeQueryDutySnapshot:     p["menu:duty"] && p["action:duty:ai"],
		auth.ScopeQueryAnalysisSnapshot: p["menu:devices"] && p["menu:dataQuality"] && allowsRoute(p, "POST", "/api/v1/data-quality/runs/:id/ai-jobs"),
	}
	out := []string{}
	for _, scope := range auth.HarnessReadScopes() {
		if required[scope] {
			out = append(out, scope)
		}
	}
	return out
}

// aiRunContext makes the caller the identity of a Harness business run. The
// MCP endpoint re-checks managed users' permissions and device scope on every
// tool call, so the run can never read more than the caller.
func aiRunContext(ctx context.Context, c auth.Claims) context.Context {
	return ports.WithAIRunIdentity(ctx, aiRunIdentity(ctx, c))
}

func aiRunIdentity(ctx context.Context, c auth.Claims) ports.AIRunIdentity {
	return ports.AIRunIdentity{TenantID: c.TenantID, Username: c.Username, ManagedUser: c.TokenUse == "user", SessionVersion: c.SessionVersion, AccessVersion: requestAccessVersion(ctx, c), Scopes: workflowScopes(ctx)}
}

func (s *Server) authorizeAIRun(ctx context.Context, tenantID, workflowID string) (context.Context, error) {
	identity, ok := ports.AIRunIdentityFrom(ctx)
	if !ok || identity.TenantID != tenantID {
		return ctx, errors.New("AI 运行身份与租户不符")
	}
	if !identity.ManagedUser {
		if workflowID == core.WorkflowDataQuality {
			return ctx, s.authorizeAnalysisAI(ctx, identity)
		}
		if workflowID == core.WorkflowDutyHandover {
			return ctx, s.authorizeDutyAI(ctx, tenantID, identity)
		}
		return ctx, nil
	}
	c := auth.Claims{TenantID: tenantID, Username: identity.Username, SessionVersion: identity.SessionVersion}
	r := (&http.Request{}).WithContext(ctx)
	user, permissions, err := s.managedIdentity(r, c)
	if err != nil {
		return ctx, errors.New("账户已停用或会话已失效，请重新登录")
	}
	if workflowID != "" {
		if !businessWorkflowAllowed(permissions, workflowID) {
			return ctx, errors.New("无此智能功能的访问权限")
		}
	} else if !permissions["menu:ai"] || !(permissions["POST /api/v1/ai/chat"] || permissions["POST /api/v1/ai/chat/stream"]) {
		return ctx, errors.New("无智能助手访问权限")
	}
	if identity.AccessVersion == "" || identity.AccessVersion != accessVersion(user, permissions, tenantID) {
		return ctx, errors.New("权限或设备范围已变化，请重新发起 AI 任务")
	}
	ctx = context.WithValue(ctx, deviceScopeKey{}, scopeFor(user, permissions, tenantID))
	ctx = context.WithValue(ctx, permissionsKey{}, permissions)
	identity.Scopes = intersectScopes(identity.Scopes, workflowScopes(ctx))
	if workflowID == core.WorkflowDutyHandover {
		if err := s.authorizeDutyAI(ctx, tenantID, identity); err != nil {
			return ctx, err
		}
	}
	if workflowID == core.WorkflowDataQuality {
		if err := s.authorizeAnalysisAI(ctx, identity); err != nil {
			return ctx, err
		}
	}
	return ports.WithAIRunIdentity(ctx, identity), nil
}

// businessWorkflowAllowed checks the feature permission behind a business run
// token; chat tokens keep requiring the assistant permission instead.
func businessWorkflowAllowed(p map[string]bool, workflow string) bool {
	switch workflow {
	case core.WorkflowDataQuality:
		return p["menu:devices"] && p["menu:dataQuality"] && allowsRoute(p, "POST", "/api/v1/data-quality/runs/:id/ai-jobs")
	case core.WorkflowDutyHandover:
		return p["menu:duty"] && p["action:duty:ai"]
	case core.WorkflowAlarmAnalysis:
		return allowsRoute(p, "POST", "/api/v1/ai/alarm-analysis/:alarmId/run")
	case core.WorkflowHealthInspection:
		return allowsRoute(p, "POST", "/api/v1/ai/health-inspection/run") || allowsRoute(p, "POST", "/api/v1/ai/health-inspection") || allowsRoute(p, "POST", "/api/v1/ai/health-inspection/pdf")
	case core.WorkflowOpsReport:
		return allowsRoute(p, "POST", "/api/v1/ai/reports")
	case core.WorkflowProtocolAssist:
		return allowsRoute(p, "POST", "/api/v1/ai/protocol-assistant/generate")
	case core.WorkflowRuleDraft:
		return allowsRoute(p, "POST", "/api/v1/ai/rule-draft")
	}
	return false
}

// canQueryKnowledge applies the same role rule as Agent chat: only roles that
// may use the knowledge-base tool receive knowledge-based answers.
func canQueryKnowledge(ctx context.Context) bool {
	return len(intersectScopes(workflowScopes(ctx), []string{auth.ScopeQueryKnowledgeBase})) > 0
}

// alarmAnalysisRunScope is the variant produced when the caller runs analysis.
func alarmAnalysisRunScope(ctx context.Context) string {
	if canQueryKnowledge(ctx) {
		return model.AlarmAnalysisWorkflowID
	}
	return model.AIAnalysisScopeNone
}

// alarmAnalysisViewScopes lists the stored variants the caller's role may read.
func alarmAnalysisViewScopes(ctx context.Context) []string {
	if canQueryKnowledge(ctx) {
		return []string{model.AIAnalysisScopeNone, model.AlarmAnalysisWorkflowID, model.AIAnalysisScopeLegacyTenant}
	}
	return []string{model.AIAnalysisScopeNone}
}

func intersectScopes(issued, current []string) []string {
	out := []string{}
	for _, value := range issued {
		for _, allowed := range current {
			if value == allowed {
				out = append(out, value)
				break
			}
		}
	}
	return out
}

func accessVersion(user model.PlatformUser, permissions map[string]bool, tenant string) string {
	scope := scopeFor(user, permissions, tenant)
	return scopeAccessVersion(scope, permissions, user.SessionVersion)
}

func requestAccessVersion(ctx context.Context, claims auth.Claims) string {
	if claims.TokenUse != "user" {
		return ""
	}
	scope, _ := requestScope(ctx)
	permissions, _ := ctx.Value(permissionsKey{}).(map[string]bool)
	return scopeAccessVersion(scope, permissions, claims.SessionVersion)
}

func scopeAccessVersion(scope deviceScope, permissions map[string]bool, version int64) string {
	ids := make([]string, 0, len(scope.IDs))
	for id, allowed := range scope.IDs {
		if allowed {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	payload, _ := json.Marshal([]any{scope.Tenant, scope.All, ids, permissionList(permissions), version})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
