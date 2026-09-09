package httpapi

import (
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"net/http"
	"strings"
)

// ONVIF discovery is a read-only preview. Saving camera metadata remains an
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
