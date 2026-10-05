package model

// AIUsage is the token usage the model provider reported for a run.
type AIUsage struct {
	InputTokens     int64 `json:"inputTokens"`
	OutputTokens    int64 `json:"outputTokens"`
	CacheReadTokens int64 `json:"cacheReadTokens"`
	ReasoningTokens int64 `json:"reasoningTokens"`
}

// Add accumulates another usage report.
func (u *AIUsage) Add(other AIUsage) {
	u.InputTokens += other.InputTokens
	u.OutputTokens += other.OutputTokens
	u.CacheReadTokens += other.CacheReadTokens
	u.ReasoningTokens += other.ReasoningTokens
}

// AI run statuses recorded in AIRunRecord.Status.
const (
	AIRunSucceeded = "SUCCEEDED"
	AIRunFailed    = "FAILED"
	AIRunStopped   = "STOPPED"
	AIRunTimeout   = "TIMEOUT"
)

// AIRunRecord is the finished record of one Harness run. It holds metadata
// and sizes only, never prompts, answers or credentials.
type AIRunRecord struct {
	RunID         string  `json:"runId"`
	TenantID      string  `json:"tenantId"`
	Actor         string  `json:"actor"`
	WorkflowID    string  `json:"workflowId"`
	PromptVersion string  `json:"promptVersion,omitempty"`
	Model         string  `json:"model,omitempty"`
	InputBytes    int     `json:"inputBytes"`
	OutputBytes   int     `json:"outputBytes"`
	Usage         AIUsage `json:"usage"`
	// UsageReported is false when the Harness did not return token usage.
	UsageReported bool   `json:"usageReported"`
	ToolCalls     int    `json:"toolCalls"`
	DurationMs    int64  `json:"durationMs"`
	Status        string `json:"status"`
	Error         string `json:"error,omitempty"`
	StartedAt     int64  `json:"startedAt"`
	FinishedAt    int64  `json:"finishedAt"`
}

// AIRunUsage sums the runs of one workflow on one report day.
type AIRunUsage struct {
	Day        string  `json:"day"`
	WorkflowID string  `json:"workflowId"`
	Runs       int     `json:"runs"`
	Failed     int     `json:"failed"`
	Usage      AIUsage `json:"usage"`
	ToolCalls  int     `json:"toolCalls"`
	DurationMs int64   `json:"durationMs"`
}
