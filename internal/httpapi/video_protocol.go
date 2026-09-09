package httpapi

import (
	"encoding/json"
	"iot-platform/internal/fieldprotocol"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"net"
	"net/http"
	"strings"
)

// ONVIF metadata is a read-only preview. Saving camera metadata remains an
// explicit operation through the existing camera API.
func (s *Server) onvifMetadata(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	var p model.DeviceAccessProfile
	if decode(w, r, &p) != nil {
		return
	}
	if strings.TrimSpace(p.EdgeNodeID) == "" || strings.TrimSpace(p.Host) == "" || p.Port < 1 || p.Port > 65535 || p.CredentialRef == "" {
		problem(w, 422, "现场节点、主机、端口和现场凭据引用必填")
		return
	}
	p.TenantID, p.ID, p.ProductID, p.DeviceID = claims(r).TenantID, "onvif-preview", "onvif-preview", "onvif-preview"
	p.TimeoutMs = 8000
	release := model.ProtocolRelease{TenantID: p.TenantID, ProtocolID: "onvif-metadata", Version: "1", Transport: "ONVIF", Config: map[string]any{"reads": []model.PollPoint{{Identifier: "metadata", Address: "device-information"}}}}
	raw, err := s.edgeRead(r.Context(), p, release)
	if err != nil {
		problem(w, 422, err.Error())
		return
	}
	if len(raw) != 1 {
		problem(w, 422, "ONVIF 返回了非预期数量的响应")
		return
	}
	parsed, err := (parser.PollResponseParser{}).ParseWithConfig(raw[0], release.Config)
	if err != nil {
		problem(w, 422, err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	write(w, 200, map[string]any{"source": "edge-read", "metadata": parsed.Properties["metadata"], "raw": raw[0]})
}

func (s *Server) onvifDiscover(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var input struct {
		NodeID           string `json:"edgeNodeId"`
		InterfaceAddress string `json:"interfaceAddress"`
	}
	if decode(w, r, &input) != nil {
		return
	}
	if input.NodeID == "" || net.ParseIP(input.InterfaceAddress).To4() == nil {
		problem(w, 422, "现场节点和已授权的 IPv4 网卡地址必填")
		return
	}
	tenant := claims(r).TenantID
	node, err := s.engine.Repo.GetEdgeNode(r.Context(), tenant, input.NodeID)
	if err != nil || node.Status != "ENABLED" {
		problem(w, 404, "edge node not found")
		return
	}
	p := model.DeviceAccessProfile{TenantID: tenant, ID: "onvif-discovery", ProductID: "onvif-discovery", DeviceID: "onvif-discovery", EdgeNodeID: input.NodeID, Host: input.InterfaceAddress}
	release := model.ProtocolRelease{TenantID: tenant, ProtocolID: "onvif-discovery", Version: "1", Transport: "ONVIF_DISCOVERY"}
	raw, err := s.edgeRead(r.Context(), p, release)
	if err != nil {
		problem(w, 422, err.Error())
		return
	}
	var result model.ONVIFDiscovery
	if len(raw) != 1 || json.Unmarshal(raw[0].Payload, &result) != nil || fieldprotocol.ValidateDiscoveryResult(result) != nil {
		problem(w, 422, "invalid discovery result from node")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	write(w, 200, map[string]any{"source": "edge-discovery", "authenticated": false, "result": result})
}
