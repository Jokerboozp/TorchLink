package httpapi

import (
	"errors"
	"iot-platform/internal/model"
	"net/http"
	"regexp"
	"time"
)

var gbDeviceID = regexp.MustCompile(`^[0-9]{20}$`)

func validateVideoCatalog(devices []model.EdgeVideoDevice, now int64) error {
	if len(devices) > 256 {
		return errors.New("video catalog supports at most 256 registered systems")
	}
	seen := map[string]bool{}
	count := 0
	for _, d := range devices {
		if !gbDeviceID.MatchString(d.DeviceID) || seen[d.DeviceID] || len(d.Name) > 256 || len(d.Manufacturer) > 128 || len(d.Model) > 128 || len(d.Firmware) > 128 || len(d.LastError) > 512 || d.LastSeenAt < 0 || d.LastSeenAt > now+300000 || d.CatalogAt < 0 || d.CatalogAt > now+300000 {
			return errors.New("invalid video catalog device")
		}
		seen[d.DeviceID] = true
		channels := map[string]bool{}
		for _, c := range d.Channels {
			count++
			if count > 1000 || !gbDeviceID.MatchString(c.DeviceID) || channels[c.DeviceID] || len(c.Name) > 256 || len(c.Manufacturer) > 128 || len(c.Model) > 128 || len(c.Address) > 512 || len(c.Owner) > 256 || len(c.CivilCode) > 64 || (c.ParentID != "" && !gbDeviceID.MatchString(c.ParentID)) || len(c.Status) > 32 {
				return errors.New("invalid or oversized video catalog")
			}
			channels[c.DeviceID] = true
		}
	}
	return nil
}

func (s *Server) importVideoCatalog(w http.ResponseWriter, r *http.Request) {
	tenant, node := claims(r).TenantID, r.PathValue("id")
	edge, err := s.engine.Repo.GetEdgeNode(r.Context(), tenant, node)
	if err != nil || edge.Status != "ENABLED" {
		problem(w, 404, "enabled edge node not found")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var request struct {
		DeviceID string `json:"deviceId"`
		CameraID string `json:"cameraId"`
	}
	if decode(w, r, &request) != nil {
		return
	}
	h, err := s.engine.Repo.GetEdgeHeartbeat(r.Context(), tenant, node)
	now := time.Now().UnixMilli()
	if err != nil || now-h.LastSeenAt > 30000 {
		problem(w, 409, "video node heartbeat is stale")
		return
	}
	for _, d := range h.VideoCatalog {
		if d.DeviceID != request.DeviceID || !d.Registered || d.CatalogAt == 0 || now-d.CatalogAt > 120000 {
			continue
		}
		for _, c := range d.Channels {
			if c.DeviceID != request.CameraID || c.Parental != 0 {
				continue
			}
			name := c.Name
			if name == "" {
				name = c.DeviceID
			}
			mapping := model.VideoCameraMapping{TenantID: tenant, CameraID: c.DeviceID, CameraName: name, Brand: c.Manufacturer, CameraPoint: c.Address, VideoPlatformID: "gb28181/" + node + "/" + d.DeviceID, Enabled: true, UpdatedAt: now}
			created, err := s.engine.Repo.CreateCatalogCamera(r.Context(), mapping)
			if err != nil {
				problem(w, 503, "save catalog camera")
				return
			}
			if !created {
				previous, err := s.engine.Repo.GetVideoCameraMapping(r.Context(), tenant, c.DeviceID)
				if err != nil || previous.VideoPlatformID != mapping.VideoPlatformID {
					problem(w, 409, "camera ID already belongs to another source; existing camera was preserved")
					return
				}
				mapping = previous
			}
			s.audit(r, "video.catalog.import", "video-camera", mapping.CameraID, map[string]any{"nodeId": node, "created": created})
			status := 200
			if created {
				status = 201
			}
			write(w, status, map[string]any{"camera": mapping, "created": created})
			return
		}
	}
	problem(w, 422, "camera is not in a recent authenticated catalog")
}
