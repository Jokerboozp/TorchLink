package ports

import (
	"context"
	"errors"
	"time"
)

// ErrAIWorkflowBusy means the Harness is running as many workflows as it
// allows; the caller may wait and retry.
var ErrAIWorkflowBusy = errors.New("AI 工作流服务繁忙")

// AIRunIdentity is the account a Harness business run acts for. Browser users
// keep ManagedUser so the MCP endpoint re-checks their current permissions and
// device scope on every tool call; system runs have no user and only the tool
// scopes listed here.
type AIRunIdentity struct {
	Username       string
	ManagedUser    bool
	SessionVersion int64
	// Scopes are the MCP tool scopes the caller may use.
	Scopes []string
}

type aiRunIdentityKey struct{}

// WithAIRunIdentity attaches the caller's identity to a business AI run.
func WithAIRunIdentity(ctx context.Context, identity AIRunIdentity) context.Context {
	return context.WithValue(ctx, aiRunIdentityKey{}, identity)
}

// AIRunIdentityFrom returns the identity; business runs fail without one rather
// than silently acting as the system.
func AIRunIdentityFrom(ctx context.Context) (AIRunIdentity, bool) {
	identity, ok := ctx.Value(aiRunIdentityKey{}).(AIRunIdentity)
	return identity, ok && identity.Username != ""
}

// SystemAIRunIdentity is used for runs without a user, such as automatic alarm
// analysis. It can use only the listed tool scopes.
func SystemAIRunIdentity(purpose string, scopes ...string) AIRunIdentity {
	return AIRunIdentity{Username: "system:" + purpose, Scopes: append([]string(nil), scopes...)}
}

// MCPToolScope is the token scope that allows one platform MCP tool.
func MCPToolScope(tool string) string { return "mcp:tool:" + tool }

// AIKnowledgeRunScope limits the knowledge tool to one Agent's documents.
type AIKnowledgeRunScope struct {
	WorkflowID string
	TopK       int
	MinScore   float64
}

// HarnessTokenIssuer signs the short-lived MCP credential of a business run.
type HarnessTokenIssuer interface {
	IssueBusinessRunToken(tenantID string, identity AIRunIdentity, runID, workflowID string, scopes []string, knowledge *AIKnowledgeRunScope, ttl time.Duration) (string, error)
}
