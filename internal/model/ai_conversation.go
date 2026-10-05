package model

// AIConversation is one assistant conversation of one user with one Agent.
// It is visible only to its tenant and user under the access version it was
// started with; a changed grant starts new conversations, like the browser
// cache, so earlier answers never outlive a narrower permission.
type AIConversation struct {
	ID            string `json:"id"`
	TenantID      string `json:"tenantId"`
	Actor         string `json:"-"`
	WorkflowID    string `json:"workflowId"`
	Title         string `json:"title"`
	AccessVersion string `json:"-"`
	MessageCount  int    `json:"messageCount"`
	CreatedAt     int64  `json:"createdAt"`
	UpdatedAt     int64  `json:"updatedAt"`
}

// AIConversationMessage is one stored turn half: the question or the answer.
type AIConversationMessage struct {
	Seq       int    `json:"seq"`
	Role      string `json:"role"`
	Text      string `json:"text"`
	RunID     string `json:"runId,omitempty"`
	Status    string `json:"status"`
	CreatedAt int64  `json:"createdAt"`
}
