package auth

import (
	"strings"
	"testing"
	"time"

	"iot-platform/internal/ports"
)

func TestIssueHarnessCreatesRestrictedShortLivedToken(t *testing.T) {
	manager := New("test-secret-at-least-32-characters")
	scopes := HarnessReadScopes()
	token, err := manager.IssueHarness("alice", "tenant-a", "run-1", scopes, 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := manager.Parse(token)
	if err != nil {
		t.Fatal(err)
	}
	if claims.TokenUse != "harness" || claims.RunID != "run-1" || claims.TenantID != "tenant-a" {
		t.Fatalf("unexpected harness claims: %#v", claims)
	}
	if !claims.HasAudience(HarnessAudience) || len(claims.ACL) != 0 {
		t.Fatalf("harness audience/ACL is unsafe: %#v", claims)
	}
	for _, scope := range scopes {
		if !claims.HasScope(scope) {
			t.Fatalf("missing exact scope %q", scope)
		}
	}
	remaining := time.Until(claims.ExpiresAt.Time)
	if remaining <= time.Minute || remaining > 2*time.Minute+time.Second {
		t.Fatalf("unexpected harness TTL: %s", remaining)
	}
}

// Business runs name tools through ports.MCPToolScope; it must produce the
// same scopes the MCP endpoint checks.
func TestPortsToolScopesMatchHarnessScopes(t *testing.T) {
	for _, scope := range HarnessReadScopes() {
		tool := strings.TrimPrefix(scope, "mcp:tool:")
		if ports.MCPToolScope(tool) != scope {
			t.Fatalf("ports.MCPToolScope(%q) = %q, want %q", tool, ports.MCPToolScope(tool), scope)
		}
	}
}

func TestAnalysisWorkflowTokenCannotChangeWorkflowOrHarnessRun(t *testing.T) {
	manager := New("test-secret-at-least-32-characters")
	identity := ports.AIRunIdentity{Username: "alice", ManagedUser: true, SessionVersion: 7, AccessVersion: "access-v2", AnalysisRunID: "facts", AnalysisSnapshotID: "snapshot", AnalysisSnapshotVersion: 3, AnalysisJobID: "job", AnalysisLeaseToken: 11, AnalysisHarnessRunID: "analysis_ai_run", AnalysisWorkflowID: "monitoring-continuity-reviewer"}
	for _, binding := range []struct{ workflow, run string }{{"data-quality-analyst", identity.AnalysisHarnessRunID}, {identity.AnalysisWorkflowID, "another-run"}} {
		if _, err := manager.IssueBusinessRunToken("tenant-a", identity, binding.run, binding.workflow, []string{ports.MCPToolScope("query_analysis_snapshot")}, nil, time.Minute); err == nil {
			t.Fatal("mismatched analysis binding was signed", binding)
		}
	}
	token, err := manager.IssueBusinessRunToken("tenant-a", identity, identity.AnalysisHarnessRunID, identity.AnalysisWorkflowID, []string{ports.MCPToolScope("query_analysis_snapshot")}, nil, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := manager.Parse(token)
	if err != nil || claims.Workflow != identity.AnalysisWorkflowID || claims.RunID != identity.AnalysisHarnessRunID || claims.AnalysisJobID != identity.AnalysisJobID || claims.AnalysisLeaseToken != identity.AnalysisLeaseToken || claims.AnalysisSnapshotVersion != identity.AnalysisSnapshotVersion || claims.AnalysisAccessVersion != identity.AccessVersion || claims.SessionVersion != 7 || !claims.ManagedUser || claims.TenantID != "tenant-a" {
		t.Fatal("signed analysis proof changed", claims, err)
	}
}
