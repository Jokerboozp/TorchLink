package auth

import (
	"testing"
	"time"
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
