package httpapi

import (
	"errors"
	"iot-platform/internal/model"
	"net/http"
)

func (s *Server) edgeProgramRoutes() {
	s.router.GET("/api/v1/edge-nodes/:id/program", s.authorize("viewer"), s.endpoint(s.edgeProgramTarget, "id"))
	s.router.POST("/api/v1/edge-nodes/:id/program", s.authorize("admin"), s.endpoint(s.edgeProgramTarget, "id"))
	s.router.GET("/api/v1/edge/:tenant/:node/program", s.endpoint(s.edgeProgramAgent, "tenant", "node"))
	s.router.POST("/api/v1/edge/:tenant/:node/program", s.endpoint(s.edgeProgramAgent, "tenant", "node"))
}
func (s *Server) edgeProgramTarget(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	tenant, node := claims(r).TenantID, r.PathValue("id")
	n, err := s.engine.Repo.GetEdgeNode(r.Context(), tenant, node)
	if err != nil {
		problem(w, 404, "edge node not found")
		return
	}
	if r.Method == "GET" {
		v, err := s.engine.Repo.GetEdgeProgram(r.Context(), tenant, node)
		if err != nil {
			problem(w, 503, "load program state")
			return
		}
		write(w, 200, v)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var input struct {
		Version    string `json:"version"`
		Generation int64  `json:"expectedGeneration"`
		Confirmed  bool   `json:"confirmed"`
	}
	if decode(w, r, &input) != nil {
		return
	}
	if !input.Confirmed || input.Generation < 0 || n.Status != "ENABLED" || (input.Version != "" && !protocolSegmentV2.MatchString(input.Version)) {
		problem(w, 422, "请确认启用节点的目标程序版本，版本须为有效标识")
		return
	}
	v, err := s.engine.Repo.SetEdgeProgram(r.Context(), tenant, node, input.Generation, input.Version)
	if err != nil {
		status := 503
		if errors.Is(err, model.ErrEdgeProgramConflict) {
			status = 409
		}
		problem(w, status, err.Error())
		return
	}
	s.audit(r, "edge.program.target", "edge", node, map[string]any{"version": v.TargetVersion, "generation": v.Generation})
	write(w, 200, v)
}
func (s *Server) edgeProgramAgent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.authenticateEdge(w, r) {
		return
	}
	tenant, node := r.PathValue("tenant"), r.PathValue("node")
	if r.Method == "GET" {
		v, err := s.engine.Repo.GetEdgeProgram(r.Context(), tenant, node)
		if err != nil {
			problem(w, 503, "load program target")
			return
		}
		write(w, 200, v)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var status model.EdgeProgramStatus
	if decode(w, r, &status) != nil {
		return
	}
	if len(status.Version) > 128 || len(status.CandidateVersion) > 128 || len(status.LastError) > 512 || len(status.SHA256) > 64 || status.Generation < 0 {
		problem(w, 422, "invalid program status")
		return
	}
	switch status.Phase {
	case "STARTING", "RUNNING", "STAGING", "UPDATING", "ROLLED_BACK", "FAILED":
	default:
		problem(w, 422, "invalid program phase")
		return
	}
	if err := s.engine.Repo.ReportEdgeProgram(r.Context(), tenant, node, status); err != nil {
		problem(w, 503, "save program status")
		return
	}
	write(w, 200, map[string]any{"nodeId": node})
}
