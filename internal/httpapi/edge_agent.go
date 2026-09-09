package httpapi

import (
	"crypto/hmac"
	"encoding/json"
	"errors"
	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
	"iot-platform/internal/parser"
	"iot-platform/internal/protocolworker"
	"net/http"
	"sort"
	"strings"
	"time"
)

func (s *Server) edgeRoutes() {
	s.onboarding.RemoteRead = s.edgeRead
	s.router.POST("/api/v1/edge-nodes/:id/video-catalog/import", s.authorize("operator"), s.endpoint(s.importVideoCatalog, "id"))
	s.router.GET("/api/v1/edge/:tenant/:node/protocols/:id/:version/artifact", s.endpoint(s.edgeArtifact, "tenant", "node", "id", "version"))
	s.router.GET("/api/v1/edge/:tenant/:node/read-jobs", s.endpoint(s.edgeReadJobs, "tenant", "node"))
	s.router.POST("/api/v1/edge/:tenant/:node/read-jobs/:job", s.endpoint(s.edgeReadJobs, "tenant", "node", "job"))
	s.router.POST("/api/v1/edge-nodes/:id/credentials", s.authorize("admin"), s.endpoint(s.edgeCredential, "id"))
	s.router.DELETE("/api/v1/edge-nodes/:id/credentials", s.authorize("admin"), s.endpoint(s.edgeCredential, "id"))
	s.router.GET("/api/v1/edge-nodes/:id/runtime", s.authorize("viewer"), s.endpoint(s.edgeRuntime, "id"))
	s.router.GET("/api/v1/edge/:tenant/:node/config", s.endpoint(s.edgeConfig, "tenant", "node"))
	s.router.POST("/api/v1/edge/:tenant/:node/heartbeat", s.endpoint(s.edgeHeartbeat, "tenant", "node"))
	s.router.POST("/api/v1/edge/:tenant/:node/raw", s.endpoint(s.edgeRaw, "tenant", "node"))
}
func (s *Server) edgeCredential(w http.ResponseWriter, r *http.Request) {
	tenant, id := claims(r).TenantID, r.PathValue("id")
	if _, err := s.engine.Repo.GetEdgeNode(r.Context(), tenant, id); err != nil {
		problem(w, 404, "edge node not found")
		return
	}
	secret, hash := "", ""
	if r.Method == http.MethodPost {
		v, err := onboarding.Credential()
		if err != nil {
			problem(w, 500, "generate edge credential")
			return
		}
		secret, hash = v.Secret, onboarding.Hash(v.Secret)
	}
	if err := s.engine.Repo.SetEdgeCredential(r.Context(), tenant, id, hash); err != nil {
		problem(w, 500, "save edge credential")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	s.audit(r, "edge.credential.change", "edge", id, nil)
	write(w, 200, map[string]any{"tenantId": tenant, "nodeId": id, "secret": secret, "disabled": hash == ""})
}
func (s *Server) authenticateEdge(w http.ResponseWriter, r *http.Request) bool {
	tenant, node := r.PathValue("tenant"), r.PathValue("node")
	secret := r.Header.Get("X-Edge-Secret")
	edge, err := s.engine.Repo.GetEdgeNode(r.Context(), tenant, node)
	if err != nil || edge.Status != "ENABLED" || secret == "" {
		problem(w, 401, "invalid edge credential")
		return false
	}
	hash, err := s.engine.Repo.GetEdgeCredential(r.Context(), tenant, node)
	if err != nil || hash == "" || !hmac.Equal([]byte(hash), []byte(onboarding.Hash(secret))) {
		problem(w, 401, "invalid edge credential")
		return false
	}
	return true
}
func (s *Server) edgeRuntime(w http.ResponseWriter, r *http.Request) {
	tenant, id := claims(r).TenantID, r.PathValue("id")
	if _, err := s.engine.Repo.GetEdgeNode(r.Context(), tenant, id); err != nil {
		problem(w, 404, "edge node not found")
		return
	}
	h, err := s.engine.Repo.GetEdgeHeartbeat(r.Context(), tenant, id)
	state := "WAITING"
	if err == nil && h.LastSeenAt > 0 {
		state = "OFFLINE"
		if time.Now().UnixMilli()-h.LastSeenAt < 30000 {
			state = "ONLINE"
		}
	}
	write(w, 200, map[string]any{"status": state, "heartbeat": h})
}
func (s *Server) edgeConfig(w http.ResponseWriter, r *http.Request) {
	if !s.authenticateEdge(w, r) {
		return
	}
	tenant, node := r.PathValue("tenant"), r.PathValue("node")
	out := model.EdgeConfiguration{TenantID: tenant, NodeID: node, Tasks: []model.EdgeTask{}, Devices: []model.ManagedDevice{}, Products: []model.Product{}}
	profiles, err := s.engine.Repo.ListDeviceAccessProfiles(r.Context(), tenant)
	if err != nil {
		problem(w, 503, "load edge profiles")
		return
	}
	products := map[string]bool{}
	for _, p := range profiles {
		if !p.Enabled || p.EdgeNodeID != node {
			continue
		}
		if len(out.Tasks) >= 256 {
			problem(w, 422, "edge supports at most 256 profiles")
			return
		}
		id, version := p.ProtocolID, p.ProtocolVersion
		if binding, err := s.engine.Repo.GetProductProtocolBinding(r.Context(), tenant, p.ProductID); err == nil && p.Mode == "listener" {
			id, version = binding.ProtocolID, binding.Version
		}
		release, err := s.engine.Repo.GetProtocolRelease(r.Context(), tenant, id, version)
		if err != nil || release.Status != "PUBLISHED" {
			problem(w, 409, "assigned protocol is unavailable")
			return
		}
		// Only implemented readers and published ingress workers are synchronized.
		if !edgeReadRelease(release) && !(p.Mode == "listener" && edgeWorkerRelease(release)) {
			problem(w, 422, "this edge build does not support the configured protocol reader")
			return
		}
		product, err := s.engine.Repo.GetProduct(r.Context(), tenant, p.ProductID)
		if err != nil || product.Status != "ENABLED" {
			continue
		}
		if !products[product.ID] {
			out.Products = append(out.Products, product)
			products[product.ID] = true
		}
		out.Tasks = append(out.Tasks, model.EdgeTask{Profile: p, Release: release})
	}
	devices, err := s.engine.Repo.ListManagedDevices(r.Context(), tenant)
	if err != nil {
		problem(w, 503, "load edge devices")
		return
	}
	for _, d := range devices {
		for _, task := range out.Tasks {
			if (d.ID == task.Profile.DeviceID || (task.Profile.Mode == "listener" && task.Profile.DeviceID == "")) && d.ProductID == task.Profile.ProductID && d.Status == "ENABLED" {
				d.AccessKey = ""
				out.Devices = append(out.Devices, d)
				break
			}
		}
	}
	sort.Slice(out.Tasks, func(i, j int) bool { return out.Tasks[i].Profile.ID < out.Tasks[j].Profile.ID })
	sort.Slice(out.Products, func(i, j int) bool { return out.Products[i].ID < out.Products[j].ID })
	sort.Slice(out.Devices, func(i, j int) bool { return out.Devices[i].ID < out.Devices[j].ID })
	normalized := out
	normalized.Tasks = append([]model.EdgeTask(nil), out.Tasks...)
	for i := range normalized.Tasks {
		normalized.Tasks[i].Profile = normalized.Tasks[i].Profile.Configuration()
	}
	encoded, _ := json.Marshal(normalized)
	out.Revision = onboarding.Hash(string(encoded))
	out.ExpiresAt = time.Now().Add(24 * time.Hour).UnixMilli()
	w.Header().Set("Cache-Control", "no-store")
	write(w, 200, out)
}
func (s *Server) edgeHeartbeat(w http.ResponseWriter, r *http.Request) {
	if !s.authenticateEdge(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	var h model.EdgeHeartbeat
	if decode(w, r, &h) != nil {
		return
	}
	if len(h.Profiles) > 256 || len(h.LastError) > 512 || h.QueueDepth < 0 || h.QueueDepth > 10000 || h.RejectedDepth < 0 || h.RejectedDepth > 10000 || len(h.Version) > 64 || len(h.Capabilities) > 16 {
		problem(w, 422, "invalid heartbeat limits")
		return
	}
	h.LastSeenAt = time.Now().UnixMilli()
	if err := validateVideoCatalog(h.VideoCatalog, h.LastSeenAt); err != nil {
		problem(w, 422, err.Error())
		return
	}
	for _, p := range h.Profiles {
		if p.TenantID != r.PathValue("tenant") || p.EdgeNodeID != r.PathValue("node") {
			problem(w, 403, "foreign profile")
			return
		}
		current, err := s.engine.Repo.GetDeviceAccessProfile(r.Context(), p.TenantID, p.ID)
		if err != nil || current.EdgeNodeID != p.EdgeNodeID {
			problem(w, 403, "unassigned profile")
			return
		}
	}
	for _, p := range h.Profiles {
		if p.RuntimeStatus != "ONLINE" && p.RuntimeStatus != "ERROR" {
			continue
		}
		if len(p.LastError) > 512 {
			p.LastError = p.LastError[:512]
		}
		if _, err := s.engine.Repo.UpdateDeviceAccessStatus(r.Context(), p, p.RuntimeStatus, p.LastError, min(h.LastSeenAt, max(p.LastSuccessAt, p.LastErrorAt))); err != nil {
			problem(w, 503, "save edge profile status")
			return
		}
	}
	if err := s.engine.Repo.SaveEdgeHeartbeat(r.Context(), r.PathValue("tenant"), r.PathValue("node"), h); err != nil {
		problem(w, 503, "save heartbeat")
		return
	}
	write(w, 200, map[string]any{"receivedAt": h.LastSeenAt})
}
func (s *Server) edgeRaw(w http.ResponseWriter, r *http.Request) {
	if !s.authenticateEdge(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var raw model.RawMessage
	if decode(w, r, &raw) != nil {
		return
	}
	profileID, _ := raw.Metadata["profileId"].(string)
	tenant, node := r.PathValue("tenant"), r.PathValue("node")
	p, err := s.engine.Repo.GetDeviceAccessProfile(r.Context(), tenant, profileID)
	if err != nil || !p.Enabled || p.EdgeNodeID != node || raw.TenantID != tenant || raw.ProductID != p.ProductID || (p.DeviceID != "" && raw.DeviceID != p.DeviceID) {
		problem(w, 403, "raw does not belong to assigned edge profile")
		return
	}
	d, err := s.engine.Repo.GetManagedDevice(r.Context(), tenant, raw.DeviceID)
	if err != nil || d.Status != "ENABLED" || d.ProductID != p.ProductID {
		problem(w, 403, "device is disabled or unavailable")
		return
	}
	product, err := s.engine.Repo.GetProduct(r.Context(), tenant, p.ProductID)
	if err != nil || product.Status != "ENABLED" {
		problem(w, 403, "product is disabled or unavailable")
		return
	}
	if (p.Mode != "listener" && raw.ProtocolID != p.ProtocolID) || raw.ProtocolVersion == "" || len(raw.MessageID) > 128 || strings.TrimSpace(raw.MessageID) == "" {
		problem(w, 422, "invalid raw protocol or message id")
		return
	}
	if p.Mode == "listener" && raw.ProtocolID != p.ProtocolID {
		binding, err := s.engine.Repo.GetProductProtocolBinding(r.Context(), tenant, p.ProductID)
		if err != nil || (raw.ProtocolID != binding.ProtocolID && raw.ProtocolID != binding.PreviousProtocolID) {
			problem(w, 403, "protocol does not belong to assigned listener")
			return
		}
	}
	release, err := s.engine.Repo.GetProtocolRelease(r.Context(), tenant, raw.ProtocolID, raw.ProtocolVersion)
	if err != nil || release.Status != "PUBLISHED" || (!edgeReadRelease(release) && !(p.Mode == "listener" && edgeWorkerRelease(release))) {
		problem(w, 422, "unsupported edge protocol snapshot")
		return
	}
	if raw.ReceivedAt <= 0 || raw.ReceivedAt > time.Now().Add(5*time.Minute).UnixMilli() {
		problem(w, 422, "invalid collection timestamp")
		return
	}
	raw.Protocol, raw.Source, raw.Transport, raw.PayloadFormat, raw.CollectorID = release.ProtocolID, "edge-agent", release.Transport, release.PayloadFormat, node
	if p.Mode == "listener" {
		raw.Transport = strings.ToUpper(p.Network)
	}
	raw.PointTableVersion = release.PointTableVersion
	raw.Headers = nil
	index, created, err := s.engine.IngestRaw(r.Context(), raw)
	if err != nil {
		if errors.Is(err, model.ErrRawConflict) {
			problem(w, 409, "raw message id conflicts with archived payload")
			return
		}
		problem(w, 503, "edge raw ingest was not confirmed")
		return
	}
	write(w, 202, map[string]any{"messageId": index.MessageID, "created": created})
}

func edgeReadRelease(r model.ProtocolRelease) bool {
	return r.ParserType == parser.ModbusTCPParserName || r.ParserType == parser.ModbusRTUParserName || (r.ParserType == parser.PollResponseParserName && (r.Transport == "OPC_UA" || (r.Transport == "SNMP" || r.Transport == "BACNET")))
}

func edgeWorkerRelease(r model.ProtocolRelease) bool {
	return r.ParserType == parser.GoProtocolParserName && r.Artifact["runtime"] == protocolworker.Runtime && protocolworker.HasCapability(r, "ingress") && (r.Transport == "TCP" || r.Transport == "UDP" || r.Transport == "TCP_UDP")
}
