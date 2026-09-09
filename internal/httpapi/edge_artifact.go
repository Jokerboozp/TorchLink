package httpapi

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (s *Server) edgeArtifact(w http.ResponseWriter, r *http.Request) {
	if !s.authenticateEdge(w, r) {
		return
	}
	tenant, node, id, version := r.PathValue("tenant"), r.PathValue("node"), r.PathValue("id"), r.PathValue("version")
	for _, value := range []string{tenant, id, version} {
		if !protocolSegmentV2.MatchString(value) {
			problem(w, 422, "invalid protocol identity")
			return
		}
	}
	profiles, err := s.engine.Repo.ListDeviceAccessProfiles(r.Context(), tenant)
	if err != nil {
		problem(w, 503, "load assigned protocols")
		return
	}
	assigned := false
	for _, p := range profiles {
		if !p.Enabled || p.EdgeNodeID != node || p.Mode != "listener" {
			continue
		}
		binding, err := s.engine.Repo.GetProductProtocolBinding(r.Context(), tenant, p.ProductID)
		if err == nil && binding.ProtocolID == id && binding.Version == version {
			assigned = true
			break
		}
	}
	if !assigned {
		problem(w, 403, "protocol is not assigned to this node")
		return
	}
	release, err := s.engine.Repo.GetProtocolRelease(r.Context(), tenant, id, version)
	if err != nil || release.Status != "PUBLISHED" || !edgeWorkerRelease(release) {
		problem(w, 404, "published worker unavailable")
		return
	}
	relative, _ := release.Artifact["path"].(string)
	prefix := filepath.ToSlash(filepath.Join("protocol-releases", tenant, id, version)) + "/"
	if !strings.HasPrefix(filepath.ToSlash(relative), prefix) || filepath.ToSlash(filepath.Clean(relative)) != relative {
		problem(w, 409, "invalid worker path")
		return
	}
	root, err := os.OpenRoot(s.cfg.DataDir)
	if err != nil {
		problem(w, 503, "open worker directory")
		return
	}
	defer root.Close()
	file, err := root.Open(relative)
	if err != nil {
		problem(w, 404, "worker file unavailable")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > 64<<20 {
		problem(w, 409, "invalid worker file")
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, (64<<20)+1))
	expected, _ := release.Artifact["sha256"].(string)
	if err != nil || len(data) > 64<<20 || !protocolDownloadHashMatchesV2(data, expected) {
		problem(w, 409, "worker checksum verification failed")
		return
	}
	writeProtocolDownloadV2(w, data, "protocol-worker", "application/octet-stream")
}
