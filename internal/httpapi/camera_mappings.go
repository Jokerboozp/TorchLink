package httpapi

import (
	"net/http"
	"strings"
	"time"

	"iot-platform/internal/model"
)

func (s *Server) videoCameras(w http.ResponseWriter, r *http.Request) {
	pagination := parseListPagination(r)
	items, total, err := s.engine.Repo.ListVideoCameraMappingsPage(r.Context(), claims(r).TenantID, pagination.PageSize, pagination.Offset)
	if err != nil {
		s.fail(w, r, err, "")
		return
	}
	for index := range items {
		// The platform stores camera metadata only. Live stream lookup and
		// playback stay in the external video platform, so never return legacy
		// stream or vendor credential fields from this endpoint.
		items[index].IngestMode = ""
		items[index].ProjectID = ""
		items[index].CityCode = ""
		items[index].DistrictCode = ""
		items[index].AreaID = ""
		items[index].RelatedDeviceIDs = nil
		items[index].RelatedFloorIDs = nil
		items[index].RelatedRoomIDs = nil
		items[index].VideoPlatformID = ""
		items[index].ClearLegacyStreamFields()
		items[index].StreamConfigured = false
		items[index].PreviewEligible = false
	}
	writeList(w, 200, s.attachLiveSummaries(r, items), total, pagination, nil)
}

func (s *Server) videoRelations(w http.ResponseWriter, r *http.Request) {
	relationType := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("relationType")))
	targetID := strings.TrimSpace(r.URL.Query().Get("targetId"))
	if relationType != "device" || targetID == "" {
		problem(w, http.StatusUnprocessableEntity, "only device relation is supported and targetId is required")
		return
	}
	relations, err := s.engine.Repo.ListVideoCameraRelationsByTarget(r.Context(), claims(r).TenantID, relationType, targetID)
	if err != nil {
		s.fail(w, r, err, "")
		return
	}
	write(w, http.StatusOK, map[string]any{"items": relations, "relationType": relationType, "targetId": targetID})
}

func (s *Server) saveVideoCamera(w http.ResponseWriter, r *http.Request) {
	var v model.VideoCameraMapping
	if decode(w, r, &v) != nil {
		return
	}
	c := claims(r)
	v.TenantID = c.TenantID
	if id := r.PathValue("id"); id != "" {
		v.CameraID = id
	}
	v.CameraID = strings.TrimSpace(v.CameraID)
	v.CameraName = strings.TrimSpace(v.CameraName)
	if v.CameraID == "" || v.CameraName == "" {
		problem(w, 422, "cameraId and cameraName are required")
		return
	}
	// Accept one legacy relatedDeviceIds value during migration, but reject
	// multiple values so the camera -> device cardinality is unambiguous.
	legacyDeviceIDs := cleanStringList(v.RelatedDeviceIDs, 128, 128)
	if len(legacyDeviceIDs) > 1 {
		problem(w, 422, "a camera can be associated with at most one device")
		return
	}
	v.DeviceID = strings.TrimSpace(v.DeviceID)
	if v.DeviceID == "" && len(legacyDeviceIDs) == 1 {
		v.DeviceID = legacyDeviceIDs[0]
	}
	if v.DeviceID != "" {
		if _, deviceErr := s.engine.Repo.GetManagedDevice(r.Context(), c.TenantID, v.DeviceID); deviceErr != nil {
			problem(w, 422, "deviceId is not registered in the current tenant")
			return
		}
	}
	v.Brand = strings.TrimSpace(v.Brand)
	v.CameraPoint = strings.TrimSpace(v.CameraPoint)
	v.Building = strings.TrimSpace(v.Building)
	v.Floor = strings.TrimSpace(v.Floor)
	v.Room = strings.TrimSpace(v.Room)
	// Clear legacy relation and stream fields on every save. The video
	// platform remains the source of truth for live playback.
	v.RelatedDeviceIDs = nil
	v.RelatedFloorIDs = nil
	v.RelatedRoomIDs = nil
	v.IngestMode = ""
	v.ProjectID = ""
	v.CityCode = ""
	v.DistrictCode = ""
	v.AreaID = ""
	v.VideoPlatformID = ""
	var previousCamera *model.VideoCameraMapping
	if previous, err := s.engine.Repo.GetVideoCameraMapping(r.Context(), c.TenantID, v.CameraID); err == nil {
		previousCamera = &previous
		if strings.HasPrefix(previous.VideoPlatformID, "gb28181/") {
			v.VideoPlatformID = previous.VideoPlatformID
		}
	}
	v.ClearLegacyStreamFields()
	v.UpdatedAt = time.Now().UnixMilli()
	if err := s.engine.Repo.SaveVideoCameraMapping(r.Context(), v); err != nil {
		s.fail(w, r, err, "")
		return
	}
	// Live configuration is stored separately and is never touched here; only
	// playback that depended on the old association is ended.
	s.videoCameraChanged(previousCamera, v)
	s.audit(r, "video.camera.save", "video-camera", v.CameraID, map[string]any{"deviceId": v.DeviceID, "enabled": v.Enabled})
	write(w, map[bool]int{true: 200, false: 201}[r.Method == http.MethodPut], v)
}
