package memory

import (
	"context"
	"sort"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type memoryConversation struct {
	conversation model.AIConversation
	messages     []model.AIConversationMessage
}

func (r *Repository) AppendAIConversationTurn(_ context.Context, c model.AIConversation, messages []model.AIConversationMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.aiConversations == nil {
		r.aiConversations = map[string]*memoryConversation{}
	}
	k := key(c.TenantID, c.Actor, c.ID)
	stored, ok := r.aiConversations[k]
	if !ok {
		c.MessageCount, c.CreatedAt = 0, c.UpdatedAt
		stored = &memoryConversation{conversation: c}
		r.aiConversations[k] = stored
	}
	if stored.conversation.AccessVersion != c.AccessVersion {
		return ports.ErrAIConversationAccessChanged
	}
	for _, m := range messages {
		stored.conversation.MessageCount++
		m.Seq = stored.conversation.MessageCount
		stored.messages = append(stored.messages, m)
	}
	stored.conversation.UpdatedAt = c.UpdatedAt
	return nil
}

func (r *Repository) ListAIConversations(_ context.Context, tenantID, actor, workflowID, accessVersion string, limit int) ([]model.AIConversation, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []model.AIConversation{}
	for _, stored := range r.aiConversations {
		c := stored.conversation
		if c.TenantID == tenantID && c.Actor == actor && c.WorkflowID == workflowID && c.AccessVersion == accessVersion {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt != out[j].UpdatedAt {
			return out[i].UpdatedAt > out[j].UpdatedAt
		}
		return out[i].ID > out[j].ID
	})
	return out[:min(len(out), limit)], nil
}

func (r *Repository) GetAIConversation(_ context.Context, tenantID, actor, id, accessVersion string, limit int) (model.AIConversation, []model.AIConversationMessage, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	stored, ok := r.aiConversations[key(tenantID, actor, id)]
	if !ok || stored.conversation.AccessVersion != accessVersion {
		return model.AIConversation{}, nil, model.ErrNotFound
	}
	messages := stored.messages[max(0, len(stored.messages)-limit):]
	return stored.conversation, append([]model.AIConversationMessage(nil), messages...), nil
}

func (r *Repository) DeleteAIConversation(_ context.Context, tenantID, actor, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(tenantID, actor, id)
	if _, ok := r.aiConversations[k]; !ok {
		return model.ErrNotFound
	}
	delete(r.aiConversations, k)
	return nil
}

var _ ports.AIConversationStore = (*Repository)(nil)
