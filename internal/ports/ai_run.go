package ports

import (
	"context"
	"errors"
	"time"

	"iot-platform/internal/model"
)

// AIRequestError rejects an AI request for a reason the user can act on
// (a permission, an invalid question, a missing knowledge grant). Status is
// the HTTP status to answer; Message is shown to the user as is.
type AIRequestError struct {
	Status  int
	Message string
}

func (e *AIRequestError) Error() string { return e.Message }

// AIRejected builds an AIRequestError.
func AIRejected(status int, message string) error {
	return &AIRequestError{Status: status, Message: message}
}

// ErrAIWorkflowBusy means the Harness is running as many workflows as it
// allows; the caller may wait and retry.
var ErrAIWorkflowBusy = errors.New("AI 工作流服务繁忙")

// ErrAIWorkflowRunsActive means provider changes must wait for running workflows.
var ErrAIWorkflowRunsActive = errors.New("AI 工作流正在运行，暂不能切换模型")

// ErrAIWorkflowPartial means a catalog change reached some Harness instances
// but not all; with a manifest store the remaining instances are reconciled.
var ErrAIWorkflowPartial = errors.New("AI 工作流变更只同步到部分 Harness 实例")

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
	TenantID       string
	Username       string
	ManagedUser    bool
	SessionVersion int64
	// AccessVersion binds preloaded evidence to the permissions and device scope
	// at request time. A changed grant invalidates the entire pending prompt.
	AccessVersion string
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

// HarnessTokenIssuer signs the short-lived MCP credential of a Harness run:
// business runs are bound to their workflow, chat runs to the assistant.
type HarnessTokenIssuer interface {
	IssueBusinessRunToken(tenantID string, identity AIRunIdentity, runID, workflowID string, scopes []string, knowledge *AIKnowledgeRunScope, ttl time.Duration) (string, error)
	IssueChatRunToken(tenantID string, identity AIRunIdentity, runID string, scopes []string, knowledge *AIKnowledgeRunScope, ttl time.Duration) (string, error)
}

// AIRunFilter selects finished AI runs; Start and End bound StartedAt in
// milliseconds and are ignored when zero.
type AIRunFilter struct {
	TenantID, WorkflowID, Status string
	Start, End                   int64
	Limit, Offset                int
}

// AIRunStore keeps the finished record of each AI run for cost and quality
// review. It is separate from Repository and obtained from the unwrapped store.
type AIRunStore interface {
	SaveAIRun(context.Context, model.AIRunRecord) error
	// ListAIRuns returns runs newest first and the filtered total.
	ListAIRuns(context.Context, AIRunFilter) ([]model.AIRunRecord, int, error)
	// AIRunUsage sums runs per report day and workflow, oldest day first.
	AIRunUsage(context.Context, AIRunFilter) ([]model.AIRunUsage, error)
}

// ErrAIConversationAccessChanged means a turn targets a conversation started
// under a different access version; the turn is not stored there.
var ErrAIConversationAccessChanged = errors.New("权限或设备范围已变化，请新建对话")

// AIConversationStore keeps assistant conversations per tenant and user. Every
// method is scoped by tenant, actor and access version; another user's or an
// outdated conversation reads as model.ErrNotFound.
type AIConversationStore interface {
	// AppendAIConversationTurn creates the conversation if needed and appends
	// messages after its last one.
	AppendAIConversationTurn(ctx context.Context, conversation model.AIConversation, messages []model.AIConversationMessage) error
	// ListAIConversations returns the newest conversations first.
	ListAIConversations(ctx context.Context, tenantID, actor, workflowID, accessVersion string, limit int) ([]model.AIConversation, error)
	// GetAIConversation returns the conversation and its last limit messages in order.
	GetAIConversation(ctx context.Context, tenantID, actor, id, accessVersion string, limit int) (model.AIConversation, []model.AIConversationMessage, error)
	DeleteAIConversation(ctx context.Context, tenantID, actor, id string) error
}
