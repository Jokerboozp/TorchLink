package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func (s *Server) aiWorkflows(w http.ResponseWriter, r *http.Request) {
	pagination := parseListPagination(r)
	// 知识库页除聊天智能体外，还需要为告警研判智能体上传文档和配置检索策略；Harness 暂不可用时仍可管理这些文档。
	forKnowledge := r.URL.Query().Get("purpose") == "knowledge"
	if s.engine.AIWorkflows == nil {
		items := []ports.AIWorkflowPlugin{}
		if forKnowledge {
			items = append(items, alarmAnalysisWorkflowPlugin())
		}
		items, total := pageItems(items, pagination)
		writeList(w, 200, items, total, pagination, map[string]any{"configured": false, "mode": "local", "healthy": false, "healthMessage": "未配置 AI 工作流服务（Harness），智能助手问答暂不可用"})
		return
	}
	items, err := s.engine.AIWorkflows.ListWorkflows(r.Context())
	if err != nil {
		if s.log != nil {
			s.log.Warn("list AI workflows failed", "error", err)
		}
		writeList(w, 200, []ports.AIWorkflowPlugin{}, 0, pagination, map[string]any{"configured": true, "mode": "harness", "healthy": false, "healthMessage": "AI 工作流服务（Harness）无法连接"})
		return
	}
	if forKnowledge {
		items = knowledgeWorkflowPlugins(items)
	} else {
		items = chatWorkflowPlugins(items)
	}
	items, total := pageItems(items, pagination)
	writeList(w, 200, items, total, pagination, map[string]any{"configured": true, "mode": "harness", "healthy": true, "healthMessage": "AI 工作流服务连接正常"})
}

func alarmAnalysisWorkflowPlugin() ports.AIWorkflowPlugin {
	return ports.AIWorkflowPlugin{ID: model.AlarmAnalysisWorkflowID, Name: "AI 告警研判", Description: "告警详情中的智能研判；仅有知识库权限的角色手动研判时检索本智能体的文档。", Enabled: true, KnowledgeEnabled: true}
}

// knowledgeWorkflowPlugins lists the Agents that own knowledge documents: chat
// Agents plus the alarm analysis Agent.
func knowledgeWorkflowPlugins(items []ports.AIWorkflowPlugin) []ports.AIWorkflowPlugin {
	visible := chatWorkflowPlugins(items)
	for _, item := range items {
		if item.ID == model.AlarmAnalysisWorkflowID {
			return append(visible, item)
		}
	}
	return append(visible, alarmAnalysisWorkflowPlugin())
}

func (s *Server) aiWorkflowManifests(w http.ResponseWriter, r *http.Request) {
	pagination := parseListPagination(r)
	manager, ok := s.engine.AIWorkflows.(ports.AIWorkflowAdminManager)
	if !ok {
		problem(w, http.StatusServiceUnavailable, "AI workflow harness does not support plugin management")
		return
	}
	items, err := manager.ListWorkflowManifests(r.Context())
	if err != nil {
		if s.log != nil {
			s.log.Warn("list AI workflow manifests failed", "error", err)
		}
		problem(w, http.StatusBadGateway, "AI 工作流服务（Harness）无法读取智能体清单")
		return
	}
	items = chatWorkflowManifests(items)
	items, total := pageItems(items, pagination)
	writeList(w, http.StatusOK, items, total, pagination, map[string]any{
		"configured":    true,
		"mode":          "harness",
		"healthy":       true,
		"healthMessage": "AI 工作流服务连接正常",
		"allowedTools":  dynamicAgentTools,
		"builtinIds":    ports.BuiltinAIWorkflowIDs,
	})
}

func (s *Server) saveAIWorkflow(w http.ResponseWriter, r *http.Request) {
	manager, ok := s.engine.AIWorkflows.(ports.AIWorkflowManager)
	if !ok {
		problem(w, http.StatusServiceUnavailable, "AI workflow harness does not support dynamic agents")
		return
	}
	var manifest ports.AIWorkflowManifest
	if decode(w, r, &manifest) != nil {
		return
	}
	if err := validateAIWorkflowManifest(manifest); err != nil {
		problem(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	defer s.lockAISync()()
	plugin, err := manager.SaveWorkflow(r.Context(), manifest)
	err = s.recordAIWorkflowChange(r.Context(), ports.StoredAIWorkflowManifest{ID: manifest.ID, Manifest: manifest}, err)
	if err != nil {
		if s.log != nil {
			s.log.Warn("save dynamic AI workflow failed", "workflow", manifest.ID, "error", err)
		}
		problem(w, http.StatusBadGateway, "AI workflow harness rejected the agent manifest")
		return
	}
	s.audit(r, "ai.workflow.agent.save", "ai-workflow", plugin.ID, map[string]any{"name": plugin.Name, "version": plugin.Version, "enabled": plugin.Enabled, "capabilities": len(plugin.Capabilities), "knowledgeEnabled": plugin.KnowledgeEnabled})
	write(w, http.StatusCreated, plugin)
}

func (s *Server) updateAIWorkflow(w http.ResponseWriter, r *http.Request) {
	manager, ok := s.engine.AIWorkflows.(ports.AIWorkflowManager)
	if !ok {
		problem(w, http.StatusServiceUnavailable, "AI workflow harness does not support dynamic agents")
		return
	}
	var manifest ports.AIWorkflowManifest
	if decode(w, r, &manifest) != nil {
		return
	}
	workflowID := strings.TrimSpace(r.PathValue("id"))
	if workflowID == "" || workflowID != manifest.ID {
		problem(w, http.StatusUnprocessableEntity, "workflow path id must match the manifest id")
		return
	}
	if err := validateAIWorkflowManifest(manifest); err != nil {
		problem(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if catalog, supportsCatalog := s.engine.AIWorkflows.(ports.AIWorkflowAdminManager); supportsCatalog {
		items, err := catalog.ListWorkflowManifests(r.Context())
		if err != nil {
			if s.log != nil {
				s.log.Warn("check AI workflow before update failed", "workflow", manifest.ID, "error", err)
			}
			problem(w, http.StatusBadGateway, "AI 工作流服务（Harness）无法读取智能体清单")
			return
		}
		found := false
		for _, item := range items {
			if item.ID == manifest.ID {
				found = true
				break
			}
		}
		if !found {
			problem(w, http.StatusNotFound, "workflow plugin was not found")
			return
		}
	}
	defer s.lockAISync()()
	plugin, err := manager.SaveWorkflow(r.Context(), manifest)
	err = s.recordAIWorkflowChange(r.Context(), ports.StoredAIWorkflowManifest{ID: manifest.ID, Manifest: manifest}, err)
	if err != nil {
		if s.log != nil {
			s.log.Warn("update dynamic AI workflow failed", "workflow", manifest.ID, "error", err)
		}
		problem(w, http.StatusBadGateway, "AI workflow harness rejected the agent manifest")
		return
	}
	s.audit(r, "ai.workflow.agent.update", "ai-workflow", plugin.ID, map[string]any{"name": plugin.Name, "version": plugin.Version, "enabled": plugin.Enabled, "capabilities": len(plugin.Capabilities), "knowledgeEnabled": plugin.KnowledgeEnabled})
	write(w, http.StatusOK, plugin)
}

func (s *Server) deleteAIWorkflow(w http.ResponseWriter, r *http.Request) {
	manager, ok := s.engine.AIWorkflows.(ports.AIWorkflowAdminManager)
	if !ok {
		problem(w, http.StatusServiceUnavailable, "AI workflow harness does not support plugin management")
		return
	}
	workflowID := strings.TrimSpace(r.PathValue("id"))
	if !validWorkflowIdentifier(workflowID) {
		problem(w, http.StatusUnprocessableEntity, "workflow id has an invalid format")
		return
	}
	if ports.IsBuiltinAIWorkflow(workflowID) {
		problem(w, http.StatusConflict, "built-in Agent ids cannot be deleted")
		return
	}
	defer s.lockAISync()()
	err := manager.DeleteWorkflow(r.Context(), workflowID)
	if err = s.recordAIWorkflowChange(r.Context(), ports.StoredAIWorkflowManifest{ID: workflowID, Manifest: ports.AIWorkflowManifest{ID: workflowID}, Deleted: true}, err); err != nil {
		if s.log != nil {
			s.log.Warn("delete dynamic AI workflow failed", "workflow", workflowID, "error", err)
		}
		problem(w, http.StatusBadGateway, "AI workflow harness rejected the delete request")
		return
	}
	s.audit(r, "ai.workflow.agent.delete", "ai-workflow", workflowID, nil)
	write(w, http.StatusOK, map[string]any{"deleted": true, "id": workflowID})
}

// recordAIWorkflowChange stores a dynamic Agent change that at least one
// Harness instance accepted; reconciliation brings the other instances along.
// Without a store, any instance failure is reported to the caller.
func (s *Server) recordAIWorkflowChange(ctx context.Context, change ports.StoredAIWorkflowManifest, err error) error {
	if err != nil && (s.aiManifestStore == nil || !errors.Is(err, ports.ErrAIWorkflowPartial)) {
		return err
	}
	if s.aiManifestStore != nil {
		if storeErr := s.aiManifestStore.SaveAIWorkflowManifest(ctx, change); storeErr != nil {
			return errors.Join(err, storeErr)
		}
	}
	if err != nil && s.log != nil {
		s.log.Warn("AI workflow change reached only some Harness instances; reconciliation completes it", "workflow", change.ID, "error", err)
	}
	return nil
}

// dynamicAgentTools is the read-only tool whitelist for administrator-defined
// chat Agents; the Agent editor offers exactly these.
var dynamicAgentTools = []string{
	"mcp__iot__query_system_overview", "mcp__iot__query_device_latest", "mcp__iot__query_alarm_list", "mcp__iot__query_alarm_detail",
	"mcp__iot__query_property_history", "mcp__iot__query_similar_alarms", "mcp__iot__query_knowledge_base", "mcp__iot__create_rule_draft",
}

func validateAIWorkflowManifest(manifest ports.AIWorkflowManifest) error {
	if manifest.SchemaVersion != 1 || !validWorkflowIdentifier(manifest.ID) {
		return errors.New("schemaVersion must be 1 and id must contain only letters, numbers, dot, underscore, colon or hyphen")
	}
	if ports.IsBuiltinAIWorkflow(manifest.ID) {
		return errors.New("built-in Agent ids cannot be overwritten")
	}
	if !boundedText(manifest.Name, 128) || !boundedText(manifest.Description, 1024) || !boundedText(manifest.Version, 64) || !boundedText(manifest.Persona, 16384) || !validWorkflowModel(manifest.DefaultModel) {
		return errors.New("name, description, version, persona and defaultModel are required and exceed no field limits")
	}
	if manifest.MaxTokens < 1 || manifest.MaxTokens > 8192 || len(manifest.Capabilities) < 1 || len(manifest.Capabilities) > 32 || len(manifest.AllowedTools) < 1 || len(manifest.AllowedTools) > 6 {
		return errors.New("maxTokens must be 1..8192 and capabilities/allowedTools must be non-empty")
	}
	allowed := map[string]struct{}{}
	for _, tool := range dynamicAgentTools {
		allowed[tool] = struct{}{}
	}
	seen := map[string]struct{}{}
	capabilities := map[string]struct{}{}
	for _, capability := range manifest.Capabilities {
		if !boundedText(capability, 64) {
			return errors.New("each capability must contain 1..64 characters")
		}
		if _, duplicate := capabilities[capability]; duplicate {
			return fmt.Errorf("duplicate capability %q", capability)
		}
		capabilities[capability] = struct{}{}
	}
	for _, tool := range manifest.AllowedTools {
		if _, ok := allowed[tool]; !ok {
			return fmt.Errorf("tool %q is outside the read-only Agent whitelist", tool)
		}
		if _, duplicate := seen[tool]; duplicate {
			return fmt.Errorf("duplicate tool %q", tool)
		}
		seen[tool] = struct{}{}
	}
	return nil
}

func validWorkflowIdentifier(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for index, char := range value {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || index > 0 && strings.ContainsRune("._:-", char) {
			continue
		}
		return false
	}
	return true
}

func validWorkflowModel(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for index, char := range value {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || index > 0 && strings.ContainsRune("._:/-", char) {
			continue
		}
		return false
	}
	return true
}

func boundedText(value string, maximum int) bool {
	length := len([]rune(strings.TrimSpace(value)))
	return length > 0 && length <= maximum
}
