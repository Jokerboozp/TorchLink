package httpapi

import (
	"encoding/json"
	"github.com/google/uuid"
	"iot-platform/internal/model"
	"iot-platform/internal/protocolworker"
	"net/http"
	"strings"
	"time"
)

func (s *Server) enqueueEdgeCommand(w http.ResponseWriter, r *http.Request, p model.DeviceAccessProfile, command map[string]any) {
	tenant := claims(r).TenantID
	d, err := s.engine.Repo.GetManagedDevice(r.Context(), tenant, r.PathValue("deviceId"))
	if err != nil || d.Status != "ENABLED" || d.ProductID != p.ProductID || (p.DeviceID != "" && p.DeviceID != d.ID) || !p.Enabled || p.Mode != "listener" {
		problem(w, 422, "设备未启用或不属于此接入实例")
		return
	}
	node, err := s.engine.Repo.GetEdgeNode(r.Context(), tenant, p.EdgeNodeID)
	if err != nil || node.Status != "ENABLED" {
		problem(w, 422, "现场节点未启用")
		return
	}
	binding, err := s.engine.Repo.GetProductProtocolBinding(r.Context(), tenant, d.ProductID)
	if err != nil {
		problem(w, 422, "产品未绑定有效协议")
		return
	}
	release, err := s.engine.Repo.GetProtocolRelease(r.Context(), tenant, binding.ProtocolID, binding.Version)
	if err != nil || release.Status != "PUBLISHED" || !listenerSupports(release, p.Network) || !protocolworker.HasCapability(release, "encode") {
		problem(w, 422, "当前协议不支持真实命令编码")
		return
	}
	id := strings.TrimSpace(firstNonBlankString(command["requestId"]))
	if id == "" {
		problem(w, 422, "现场命令须提供 requestId，重试必须复用此值")
		return
	}
	if !protocolSegmentV2.MatchString(id) {
		problem(w, 422, "invalid command requestId")
		return
	}
	delete(command, "requestId")
	now := time.Now().UnixMilli()
	job := model.DeviceCommand{ID: id, TenantID: tenant, DeviceID: d.ID, ProductID: d.ProductID, Type: firstNonBlankString(command["type"]), Data: command, CreatedAt: now, UpdatedAt: now, Execution: &model.EdgeCommandExecution{NodeID: p.EdgeNodeID, ProfileID: p.ID, ConfigurationHash: model.CommandProfileHash(p), ProtocolID: release.ProtocolID, ProtocolVersion: release.Version, ExpiresAt: now + 30000}}
	// Restore an already accepted request even if the node disconnected afterwards.
	if old, err := s.engine.Repo.GetDeviceCommand(r.Context(), tenant, id); err == nil {
		if !old.SameEdgeRequest(job) {
			problem(w, 409, "command ID belongs to a different request")
			return
		}
		write(w, 200, old.ObservedOutcome(now).Public())
		return
	}
	heartbeat, err := s.engine.Repo.GetEdgeHeartbeat(r.Context(), tenant, p.EdgeNodeID)
	supported := false
	for _, cap := range heartbeat.Capabilities {
		if cap == "PROTOCOL_COMMANDS" {
			supported = true
		}
	}
	if err != nil || heartbeat.LastSeenAt < now-30000 || !supported {
		problem(w, 409, "节点未在线或本机尚未启用协议命令")
		return
	}
	saved, created, err := s.engine.Repo.CreateEdgeCommand(r.Context(), job)
	if err != nil {
		problem(w, 429, err.Error())
		return
	}
	if !saved.SameEdgeRequest(job) {
		problem(w, 409, "command ID belongs to a different request")
		return
	}
	if created {
		s.audit(r, "protocol.edge.command.queued", "device", d.ID, map[string]any{"commandId": id, "profileId": p.ID, "nodeId": p.EdgeNodeID, "type": job.Type})
	}
	write(w, 202, saved.ObservedOutcome(now).Public())
}
func (s *Server) edgeCommand(w http.ResponseWriter, r *http.Request) {
	if !s.authenticateEdge(w, r) {
		return
	}
	tenant, node := r.PathValue("tenant"), r.PathValue("node")
	if r.Method == "GET" {
		c, err := s.engine.Repo.ClaimEdgeCommand(r.Context(), tenant, node, uuid.NewString())
		if err != nil {
			problem(w, 503, "claim edge command failed")
			return
		}
		write(w, 200, c)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32768)
	var result model.DeviceCommand
	if decode(w, r, &result) != nil {
		return
	}
	c, err := s.engine.Repo.GetDeviceCommand(r.Context(), tenant, r.PathValue("command"))
	if err != nil || c.Execution == nil || c.Execution.NodeID != node || result.Execution == nil || result.Execution.Token != c.Execution.Token {
		problem(w, 409, "unowned edge command result")
		return
	}
	data, _ := json.Marshal(result.Reply)
	if !result.EdgeResultAllowed() || len(result.LastError) > 512 || len(data) > 8192 {
		problem(w, 422, "invalid edge command result")
		return
	}
	if result.Status == "ACKNOWLEDGED" && (result.Reply["status"] != "acknowledged" || firstNonBlankString(result.Reply["rawMessageId"]) == "" || firstNonBlankString(result.Reply["correlationId"]) == "") {
		problem(w, 422, "protocol acknowledgment lacks raw evidence")
		return
	}
	if (result.Status == "ACKNOWLEDGED" || result.Status == "SENT") && (result.Reply["protocolId"] != c.Execution.ProtocolID || result.Reply["protocolVersion"] != c.Execution.ProtocolVersion) {
		problem(w, 422, "command result protocol version differs from assigned version")
		return
	}

	c.Status = result.Status
	c.LastError = result.LastError
	c.Reply = result.Reply
	if err := s.engine.Repo.FinishEdgeCommand(r.Context(), c); err != nil {
		problem(w, 409, "edge command result was not accepted")
		return
	}
	write(w, 200, map[string]any{"id": c.ID})
}
func (s *Server) deviceCommandStatus(w http.ResponseWriter, r *http.Request) {
	c, err := s.engine.Repo.GetDeviceCommand(r.Context(), claims(r).TenantID, r.PathValue("command"))
	if err != nil || c.DeviceID != r.PathValue("id") {
		problem(w, 404, "device command not found")
		return
	}
	write(w, 200, c.ObservedOutcome(time.Now().UnixMilli()).Public())
}
