package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"iot-platform/internal/model"
)

// Assistant conversations belong to the signed-in user: every request is
// scoped to its tenant, username and current access version, so another
// user's conversation or one started under an earlier grant reads as absent.

func (s *Server) conversationRoutes() {
	s.router.GET("/api/v1/ai/conversations", s.authorize("viewer"), s.endpoint(s.listAIConversations))
	s.router.GET("/api/v1/ai/conversations/:id", s.authorize("viewer"), s.endpoint(s.getAIConversation, "id"))
	s.router.DELETE("/api/v1/ai/conversations/:id", s.authorize("viewer"), s.endpoint(s.deleteAIConversation, "id"))
}

// aiConversationRoute reports routes that follow the assistant permission
// instead of a separately granted action: users only reach their own data.
func aiConversationRoute(path string) bool {
	return path == "/api/v1/ai/conversations" || strings.HasPrefix(path, "/api/v1/ai/conversations/")
}

func (s *Server) listAIConversations(w http.ResponseWriter, r *http.Request) {
	if s.engine.AIConversations == nil {
		write(w, http.StatusOK, map[string]any{"items": []model.AIConversation{}})
		return
	}
	c := claims(r)
	workflowID := strings.TrimSpace(r.URL.Query().Get("workflowId"))
	if workflowID == "" || len(workflowID) > 128 {
		problem(w, http.StatusUnprocessableEntity, "请指定智能体")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := s.engine.AIConversations.ListAIConversations(r.Context(), c.TenantID, c.Username, workflowID, requestAccessVersion(r.Context(), c), limit)
	if err != nil {
		s.fail(w, r, err, "")
		return
	}
	write(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) getAIConversation(w http.ResponseWriter, r *http.Request) {
	if s.engine.AIConversations == nil {
		problem(w, http.StatusNotFound, "对话不存在")
		return
	}
	c := claims(r)
	conversation, messages, err := s.engine.AIConversations.GetAIConversation(r.Context(), c.TenantID, c.Username, r.PathValue("id"), requestAccessVersion(r.Context(), c), 200)
	if errors.Is(err, model.ErrNotFound) {
		problem(w, http.StatusNotFound, "对话不存在")
		return
	}
	if err != nil {
		s.fail(w, r, err, "")
		return
	}
	write(w, http.StatusOK, map[string]any{"conversation": conversation, "messages": messages})
}

func (s *Server) deleteAIConversation(w http.ResponseWriter, r *http.Request) {
	if s.engine.AIConversations == nil {
		problem(w, http.StatusNotFound, "对话不存在")
		return
	}
	c := claims(r)
	err := s.engine.AIConversations.DeleteAIConversation(r.Context(), c.TenantID, c.Username, r.PathValue("id"))
	if errors.Is(err, model.ErrNotFound) {
		problem(w, http.StatusNotFound, "对话不存在")
		return
	}
	if err != nil {
		s.fail(w, r, err, "")
		return
	}
	s.audit(r, "ai.conversation.delete", "ai-conversation", r.PathValue("id"), nil)
	write(w, http.StatusOK, map[string]any{"deleted": true})
}
