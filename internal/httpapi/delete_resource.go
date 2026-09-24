package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"iot-platform/internal/model"
)

func (s *Server) deletionRoutes() {
	s.router.DELETE("/api/v1/device-registry/:id", s.authorize("operator"), s.endpoint(s.deleteResource("device"), "id"))
	s.router.DELETE("/api/v1/products/:id", s.authorize("operator"), s.endpoint(s.deleteResource("product"), "id"))
	s.router.DELETE("/api/v2/device-access-profiles/:id", s.authorize("operator"), s.endpoint(s.deleteResource("profile"), "id"))
	s.router.DELETE("/api/v1/integrations/video/cameras/:id", s.authorize("operator"), s.endpoint(s.deleteResource("camera"), "id"))
	s.router.DELETE("/api/v2/protocols/:id", s.authorize("operator"), s.endpoint(s.deleteResource("protocol"), "id"))
	s.router.DELETE("/api/v1/alarms/:id", s.authorize("admin"), s.endpoint(s.deleteResource("alarm"), "id"))
	s.router.DELETE("/api/v1/knowledge/documents/:id", s.authorize("operator"), s.endpoint(s.deleteKnowledgeDocument, "id"))
	s.router.DELETE("/api/v1/backups/:id", s.authorize("admin"), s.endpoint(s.deleteBackup, "id"))
}

func (s *Server) deleteResource(kind string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.PathValue("id"))
		if id == "" {
			problem(w, http.StatusBadRequest, "resource id is required")
			return
		}
		if kind == "protocol" && (!protocolSegmentV2.MatchString(id) || id == "." || id == "..") {
			problem(w, http.StatusBadRequest, "invalid protocol id")
			return
		}
		err := s.engine.Repo.DeleteResource(r.Context(), claims(r).TenantID, kind, id)
		switch {
		case errors.Is(err, model.ErrNotFound):
			problem(w, http.StatusNotFound, "resource not found")
		case errors.Is(err, model.ErrResourceInUse):
			problem(w, http.StatusConflict, "resource is still referenced; remove its associations first")
		case err != nil:
			problem(w, http.StatusInternalServerError, "delete failed")
		default:
			if kind == "protocol" {
				if err := s.removeProtocolArtifacts(claims(r).TenantID, id); err != nil {
					problem(w, http.StatusInternalServerError, "protocol deleted but artifact cleanup failed")
					return
				}
			}
			s.audit(r, kind+".delete", kind, id, nil)
			write(w, http.StatusOK, map[string]any{"deleted": true, "id": id})
		}
	}
}

func (s *Server) removeProtocolArtifacts(tenant, id string) error {
	if tenant == "" || tenant == "." || tenant == ".." || filepath.Base(tenant) != tenant || strings.ContainsAny(tenant, `/\`) {
		return fmt.Errorf("invalid tenant")
	}
	root, err := filepath.Abs(filepath.Join(s.cfg.DataDir, "protocol-releases"))
	if err != nil {
		return err
	}
	target, err := filepath.Abs(filepath.Join(root, tenant, id))
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(root, target)
	if err != nil || rel != filepath.Join(tenant, id) {
		return fmt.Errorf("protocol artifact path leaves data directory")
	}
	return os.RemoveAll(target)
}
