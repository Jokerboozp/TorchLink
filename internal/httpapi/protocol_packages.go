package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/parser"
)

func (s *Server) protocolPackages(w http.ResponseWriter, r *http.Request) {
	pagination := parseListPagination(r)
	items, total, err := s.engine.Repo.ListProtocolPackagesPage(r.Context(), claims(r).TenantID, pagination.PageSize, pagination.Offset)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeList(w, 200, items, total, pagination, map[string]any{"parserTypes": parser.ManagedParserTypes()})
}

func (s *Server) saveProtocolPackage(w http.ResponseWriter, r *http.Request) {
	var v model.ProtocolPackage
	if decode(w, r, &v) != nil {
		return
	}
	c := claims(r)
	v.TenantID = c.TenantID
	if id := r.PathValue("id"); id != "" {
		v.ID = id
	}
	if v.ID == "" {
		v.ID = "protocol_" + randomHex(6)
	}
	if v.Name == "" || v.ParserType == "" {
		problem(w, 422, "name and parserType are required")
		return
	}
	if v.ParserType == parser.GoProtocolParserName {
		problem(w, 422, "Go 协议请通过源码编译、样例验证和版本发布接口管理")
		return
	}
	if !parser.ManagedParserType(v.ParserType) {
		problem(w, 422, "专用解析器仅供已有绑定及历史回放；新增或更新协议请上传 Go 源码包")
		return
	}
	if v.Version == "" {
		v.Version = "1.0.0"
	}
	if v.Protocol == "" {
		v.Protocol = "json"
	}
	if v.Transport == "" {
		v.Transport = "MQTT"
	}
	if v.PayloadFormat == "" {
		v.PayloadFormat = "json"
	}
	if v.Status == "" {
		v.Status = "DRAFT"
	}
	if v.Status != "DRAFT" && v.Status != "PUBLISHED" && v.Status != "DISABLED" {
		problem(w, 422, "status must be DRAFT, PUBLISHED or DISABLED")
		return
	}
	now := time.Now().UnixMilli()
	if old, getErr := s.engine.Repo.GetProtocolPackage(r.Context(), c.TenantID, v.ID); getErr == nil {
		v.CreatedAt = old.CreatedAt
		if v.Config == nil {
			v.Config = map[string]any{}
		}
		if _, hasArtifact := v.Config["artifact"]; !hasArtifact {
			if artifact, exists := old.Config["artifact"]; exists {
				v.Config["artifact"] = artifact
			}
		}
	}
	if v.CreatedAt == 0 {
		v.CreatedAt = now
	}
	v.UpdatedAt = now
	if err := s.engine.Repo.SaveProtocolPackage(r.Context(), v); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit(r, "protocol.save", "protocolPackage", v.ID, map[string]any{"version": v.Version, "status": v.Status})
	write(w, 201, v)
}

func (s *Server) testProtocolPackage(w http.ResponseWriter, r *http.Request) {
	pkg, err := s.engine.Repo.GetProtocolPackage(r.Context(), claims(r).TenantID, r.PathValue("id"))
	if err != nil {
		problem(w, 404, "protocol package not found")
		return
	}
	var in struct {
		ProductID string          `json:"productId"`
		DeviceID  string          `json:"deviceId"`
		Payload   json.RawMessage `json:"payload"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	if len(in.Payload) == 0 {
		problem(w, 422, "payload is required")
		return
	}
	if in.ProductID == "" {
		in.ProductID = "protocol_test"
	}
	if in.DeviceID == "" {
		in.DeviceID = "device_test"
	}
	raw := model.RawMessage{MessageID: "raw_test_" + randomHex(6), TenantID: pkg.TenantID, ProductID: in.ProductID, DeviceID: in.DeviceID, Protocol: pkg.Protocol, Transport: pkg.Transport, PayloadFormat: pkg.PayloadFormat, Payload: in.Payload, ReceivedAt: time.Now().UnixMilli()}
	msg, err := s.engine.Parsers.ParseWithConfig(pkg.ParserType, pkg.Config, raw)
	if err != nil {
		write(w, 200, map[string]any{"success": false, "error": err.Error(), "raw": raw})
		return
	}
	write(w, 200, map[string]any{"success": true, "standardMessage": msg})
}
