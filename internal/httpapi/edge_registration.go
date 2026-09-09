package httpapi

import (
	"errors"
	"iot-platform/internal/model"
	"net/http"
)

func (s *Server) edgeRegisterDevice(w http.ResponseWriter, r *http.Request) {
	if !s.authenticateEdge(w, r) {
		return
	}
	tenant, node := r.PathValue("tenant"), r.PathValue("node")
	if !s.onboarding.Allow("edge-register/" + tenant + "/" + node) {
		problem(w, 429, "edge registration rate exceeded")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var input struct {
		ProfileID         string `json:"profileId"`
		DeviceID          string `json:"deviceId"`
		Name              string `json:"name"`
		ConfigurationHash string `json:"configurationHash"`
	}
	if decode(w, r, &input) != nil {
		return
	}
	p, err := s.engine.Repo.GetDeviceAccessProfile(r.Context(), tenant, input.ProfileID)
	if err != nil || p.EdgeNodeID != node || !p.Enabled || !p.AutoRegister || p.Mode != "listener" || input.ConfigurationHash != model.CommandProfileHash(p) {
		problem(w, 403, "automatic registration does not belong to current assigned profile")
		return
	}
	if !model.ValidProtocolDeviceID(input.DeviceID) || len(input.Name) > 256 {
		problem(w, 422, "invalid protocol device identity")
		return
	}
	id, version := p.ProtocolID, p.ProtocolVersion
	if binding, err := s.engine.Repo.GetProductProtocolBinding(r.Context(), tenant, p.ProductID); err == nil {
		id, version = binding.ProtocolID, binding.Version
	}
	release, err := s.engine.Repo.GetProtocolRelease(r.Context(), tenant, id, version)
	if err != nil || release.Status != "PUBLISHED" || !edgeWorkerRelease(release) {
		problem(w, 409, "assigned ingress protocol is unavailable")
		return
	}
	device, created, err := s.engine.Repo.RegisterProtocolDevice(r.Context(), p, input.DeviceID, input.Name)
	if err != nil {
		if errors.Is(err, model.ErrProtocolRegistration) {
			problem(w, 409, err.Error())
		} else {
			problem(w, 503, "device registration was not confirmed")
		}
		return
	}
	device.AccessKey, device.SecretHash = "", ""
	w.Header().Set("Cache-Control", "no-store")
	write(w, 200, map[string]any{"device": device, "created": created})
}
