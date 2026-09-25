package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"iot-platform/internal/auth"
	"iot-platform/internal/model"
)

// Tool permissions are derived from the same routes used by the browser.
func workflowScopes(ctx context.Context) []string {
	p, managed := ctx.Value(permissionsKey{}).(map[string]bool)
	if !managed {
		return auth.HarnessReadScopes()
	}
	required := map[string]bool{
		auth.ScopeQuerySystemOverview:  p["menu:dashboard"],
		auth.ScopeQueryDeviceLatest:    p["menu:devices"],
		auth.ScopeQueryPropertyHistory: p["menu:devices"],
		auth.ScopeQueryAlarmList:       allowsRoute(p, "GET", "/api/v1/alarms"),
		auth.ScopeQuerySimilarAlarms:   allowsRoute(p, "GET", "/api/v1/alarms"),
		auth.ScopeQueryKnowledgeBase:   p["menu:knowledge"],
		auth.ScopeCreateRuleDraft:      allowsRoute(p, "POST", "/api/v1/ai/rule-draft"),
	}
	out := []string{}
	for _, scope := range auth.HarnessReadScopes() {
		if required[scope] {
			out = append(out, scope)
		}
	}
	return out
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
