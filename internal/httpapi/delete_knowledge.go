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
