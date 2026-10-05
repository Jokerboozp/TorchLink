package repositorytest

import (
	"context"
	"errors"
	"testing"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// AIConversations checks that conversations stay with their tenant, user and
// access version, append in order and can be deleted only by their owner.
func AIConversations(t *testing.T, store ports.AIConversationStore) {
	t.Helper()
	ctx := context.Background()
	turn := func(c model.AIConversation, at int64, question, answer string) error {
		c.UpdatedAt = at
		return store.AppendAIConversationTurn(ctx, c, []model.AIConversationMessage{
			{Role: "user", Text: question, Status: model.AIRunSucceeded, CreatedAt: at},
			{Role: "assistant", Text: answer, RunID: "run-" + question, Status: model.AIRunSucceeded, CreatedAt: at},
		})
	}
	first := model.AIConversation{ID: "c1", TenantID: "t1", Actor: "alice", WorkflowID: "ops", Title: "设备", AccessVersion: "v1"}
	second := model.AIConversation{ID: "c2", TenantID: "t1", Actor: "alice", WorkflowID: "ops", Title: "告警", AccessVersion: "v1"}
	for _, step := range []struct {
		c      model.AIConversation
		at     int64
		q, ans string
	}{{first, 1000, "q1", "a1"}, {second, 2000, "q2", "a2"}, {first, 3000, "q3", "a3"}} {
		if err := turn(step.c, step.at, step.q, step.ans); err != nil {
			t.Fatal(err)
		}
	}
	// Same id for another user or tenant is a separate conversation.
	for _, other := range []model.AIConversation{{ID: "c1", TenantID: "t1", Actor: "bob", WorkflowID: "ops", AccessVersion: "v1"}, {ID: "c1", TenantID: "t2", Actor: "alice", WorkflowID: "ops", AccessVersion: "v1"}} {
		if err := turn(other, 4000, "other", "other"); err != nil {
			t.Fatal(err)
		}
	}
	list, err := store.ListAIConversations(ctx, "t1", "alice", "ops", "v1", 10)
	if err != nil || len(list) != 2 || list[0].ID != "c1" || list[0].MessageCount != 4 || list[0].Title != "设备" || list[0].CreatedAt != 1000 || list[0].UpdatedAt != 3000 {
		t.Fatalf("conversations %+v err=%v", list, err)
	}
	c, messages, err := store.GetAIConversation(ctx, "t1", "alice", "c1", "v1", 3)
	if err != nil || c.ID != "c1" || len(messages) != 3 || messages[0].Text != "a1" || messages[2].Text != "a3" || messages[2].Seq != 4 || messages[2].RunID != "run-q3" {
		t.Fatalf("conversation %+v messages %+v err=%v", c, messages, err)
	}
	// A changed grant hides earlier conversations and refuses to extend them.
	if _, _, err = store.GetAIConversation(ctx, "t1", "alice", "c1", "v2", 10); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("outdated access version read: %v", err)
	}
	if list, _ = store.ListAIConversations(ctx, "t1", "alice", "ops", "v2", 10); len(list) != 0 {
		t.Fatalf("outdated conversations listed: %+v", list)
	}
	first.AccessVersion = "v2"
	if err = turn(first, 5000, "q4", "a4"); !errors.Is(err, ports.ErrAIConversationAccessChanged) {
		t.Fatalf("turn appended across access versions: %v", err)
	}
	if err = store.DeleteAIConversation(ctx, "t1", "bob", "c2"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("another user deleted the conversation: %v", err)
	}
	if err = store.DeleteAIConversation(ctx, "t1", "alice", "c2"); err != nil {
		t.Fatal(err)
	}
	if list, _ = store.ListAIConversations(ctx, "t1", "alice", "ops", "v1", 10); len(list) != 1 || list[0].ID != "c1" {
		t.Fatalf("deleted conversation still listed: %+v", list)
	}
}
