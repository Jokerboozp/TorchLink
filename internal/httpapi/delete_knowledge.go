package httpapi

import (
	"net/http"
	"strings"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func (s *Server) deleteKnowledgeDocument(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		problem(w, http.StatusBadRequest, "document id is required")
		return
	}
	tenant := claims(r).TenantID
	docs, err := s.engine.Repo.ListKnowledgeDocs(r.Context(), tenant)
	if err != nil {
		problem(w, http.StatusInternalServerError, "could not load document")
		return
	}
	var doc model.KnowledgeDoc
	for _, item := range docs {
		if item.ID == id {
			doc = item
			break
		}
	}
	if doc.ID == "" {
		problem(w, http.StatusNotFound, "knowledge document not found")
		return
	}
	index, indexed := s.engine.KB.(ports.KnowledgeDocumentDeleter)
	objects, stored := s.engine.Archive.(ports.ObjectDeleter)
	if !indexed || !stored {
		problem(w, http.StatusNotImplemented, "knowledge storage does not support deletion")
		return
	}
	if jobs := s.knowledgeJobs; jobs != nil {
		doc.Status = "DELETING"
		if doc.Metadata == nil {
			doc.Metadata = map[string]any{}
		}
		doc.Metadata["indexStage"] = "deleting"
		updated, updateErr := jobs.UpdateKnowledgeDocument(r.Context(), doc)
		if updateErr != nil {
			problem(w, 500, "无法保存文档删除任务")
			return
		}
		if !updated {
			problem(w, 404, "知识文档不存在")
			return
		}
		s.audit(r, "knowledge.delete", "knowledge-document", id, nil)
		write(w, 202, map[string]any{"deleting": true, "id": id})
		return
	}
	if err = index.DeleteKnowledgeDocument(r.Context(), tenant, id, doc.WorkflowID); err != nil {
		problem(w, http.StatusBadGateway, "could not delete indexed knowledge")
		return
	}
	if err = objects.DeleteObject(r.Context(), doc.ObjectBucket, doc.ObjectKey); err != nil {
		problem(w, http.StatusBadGateway, "could not delete document file")
		return
	}
	if err = s.engine.Repo.DeleteResource(r.Context(), tenant, "knowledge", id); err != nil {
		problem(w, http.StatusInternalServerError, "could not delete document record")
		return
	}
	s.audit(r, "knowledge.delete", "knowledge-document", id, nil)
	write(w, http.StatusOK, map[string]any{"deleted": true, "id": id})
}
