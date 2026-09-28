package ports

import (
	"context"
	"errors"
	"time"
)

// ErrAIWorkflowBusy means the Harness is running as many workflows as it
// allows; the caller may wait and retry.
var ErrAIWorkflowBusy = errors.New("AI 工作流服务繁忙")

// ErrAIWorkflowRunsActive means provider changes must wait for running workflows.
var ErrAIWorkflowRunsActive = errors.New("AI 工作流正在运行，暂不能切换模型")

var ErrAIWorkflowRunNotFound = errors.New("AI 工作流已结束或不存在")
var ErrAIWorkflowManagementUnavailable = errors.New("Harness 尚未支持运行管理，请更新依赖机的 Harness 镜像")
var ErrAIWorkflowStopped = errors.New("AI 工作流已被管理员强制停止")

// AIWorkflowRun contains operational metadata only, never prompts or credentials.
type AIWorkflowRun struct {
	RunID        string `json:"runId"`
	TenantID     string `json:"tenantId"`
	Actor        string `json:"actor"`
	WorkflowID   string `json:"workflowId"`
	WorkflowName string `json:"workflowName"`
	Model        string `json:"model"`
	Status       string `json:"status"`
	StartedAt    int64  `json:"startedAt"`
}

type AIWorkflowRunManager interface {
	ListWorkflowRuns(context.Context, string) ([]AIWorkflowRun, error)
	StopWorkflowRun(context.Context, string, string) error
}

// AIRunIdentity is the account a Harness business run acts for. Browser users
// keep ManagedUser so the MCP endpoint re-checks their current permissions and
// device scope on every tool call; other accounts, such as the configured
// administrator, have only the tool scopes listed here.
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
