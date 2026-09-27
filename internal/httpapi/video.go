package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/video"
)

// Camera live module HTTP API. Basic camera management stays in server.go and
// works whether or not the module is deployed; these handlers report the
// module state instead of failing other interfaces.

const videoPlayPermission = "POST /api/v1/video/cameras/:id/play-sessions"

var videoHookEvents = map[string]bool{"on_play": true, "on_publish": true, "on_stream_none_reader": true, "on_stream_not_found": true, "on_server_started": true, "on_server_keepalive": true, "on_stream_changed": true, "on_rtp_server_timeout": true}

// SetVideo installs the live module service.
func (s *Server) SetVideo(v *video.Service) { s.video = v }

func (s *Server) videoRoutes() {
	r, e := s.router, s.endpoint
	// Media server callbacks and the reverse proxy's HLS check carry their own
	// credentials (hook secret / play token), not a user JWT.
	r.POST("/api/v1/video/hooks/:event", e(s.videoHook, "event"))
	r.GET("/api/v1/video/media-auth", e(s.videoMediaAuth))

	r.GET("/api/v1/video/status", s.authorize("viewer"), e(s.videoStatus))
	r.PUT("/api/v1/video/module", s.authorize("admin"), e(s.videoSetModule))
	r.GET("/api/v1/video/devices/:deviceId/cameras", s.authorize("viewer"), e(s.videoDeviceCameras, "deviceId"))
	r.GET("/api/v1/video/cameras/:id", s.authorize("viewer"), e(s.videoCameraSummary, "id"))
	r.POST("/api/v1/video/cameras/:id/play-sessions", s.authorize("viewer"), e(s.videoCreateSession, "id"))
	r.POST("/api/v1/video/play-sessions/:id/whep", s.authorize("viewer"), e(s.videoWHEP, "id"))
	r.POST("/api/v1/video/play-sessions/:id/heartbeat", s.authorize("viewer"), e(s.videoHeartbeat, "id"))
	r.DELETE("/api/v1/video/play-sessions/:id", s.authorize("viewer"), e(s.videoStopSession, "id"))

	r.GET("/api/v1/integrations/video/cameras/:id/live", s.authorize("operator"), e(s.videoGetLiveConfig, "id"))
	r.PUT("/api/v1/integrations/video/cameras/:id/live", s.authorize("operator"), e(s.videoSaveLiveConfig, "id"))
	r.POST("/api/v1/integrations/video/cameras/:id/live/test", s.authorize("operator"), e(s.videoTestLive, "id"))
	r.POST("/api/v1/integrations/video/cameras/:id/live/onvif-profiles", s.authorize("operator"), e(s.videoONVIFProfiles, "id"))

	r.GET("/api/v1/integrations/video/gb28181/devices", s.authorize("operator"), e(s.videoListGBDevices))
	r.PUT("/api/v1/integrations/video/gb28181/devices/:deviceId", s.authorize("operator"), e(s.videoSaveGBDevice, "deviceId"))
	r.DELETE("/api/v1/integrations/video/gb28181/devices/:deviceId", s.authorize("operator"), e(s.videoDeleteGBDevice, "deviceId"))
	r.POST("/api/v1/integrations/video/gb28181/devices/:deviceId/refresh", s.authorize("operator"), e(s.videoRefreshGBDevice, "deviceId"))
}

// videoRouteMenu maps live routes. Status and module switch are shared; the
// viewer routes belong to device data, since device-scoped users reach
// cameras from devices and alarms rather than from the camera directory.
func videoRouteMenu(path string) (string, bool) {
	// GB28181 device management belongs to the camera directory; matched here
	// because the generic "/devices" rule would claim it for device data.
	if strings.HasPrefix(path, "/api/v1/integrations/video/gb28181/") {
		return "cameras", true
	}
	if !strings.HasPrefix(path, "/api/v1/video/") {
		return "", false
	}
	if strings.HasPrefix(path, "/api/v1/video/hooks/") || path == "/api/v1/video/media-auth" || path == "/api/v1/video/status" || path == "/api/v1/video/module" {
		return "", true
	}
	return "devices", true
}

// videoSessionRoute lists follow-up routes of a play session; they are part
// of the watch permission and are not offered as separate permissions.
func videoSessionRoute(path string) bool {
	return strings.HasPrefix(path, "/api/v1/video/play-sessions/")
}

func (s *Server) videoService(w http.ResponseWriter) *video.Service {
	if s.video == nil {
		write(w, http.StatusServiceUnavailable, map[string]any{"status": 503, "code": "VIDEO_NOT_DEPLOYED", "detail": "直播模块未部署"})
		return nil
	}
	return s.video
}

func videoProblem(w http.ResponseWriter, err error) {
	status, code := http.StatusBadGateway, "VIDEO_MEDIA_ERROR"
	var validation video.ValidationError
	switch {
	case errors.Is(err, video.ErrNotDeployed):
		status, code = http.StatusServiceUnavailable, "VIDEO_NOT_DEPLOYED"
	case errors.Is(err, video.ErrModuleDisabled):
		status, code = http.StatusConflict, "VIDEO_DISABLED"
	case errors.Is(err, video.ErrMediaDown):
		status, code = http.StatusServiceUnavailable, "VIDEO_MEDIA_UNAVAILABLE"
	case errors.Is(err, video.ErrNotConfigured):
		status, code = http.StatusNotFound, "VIDEO_NOT_CONFIGURED"
	case errors.Is(err, video.ErrForbidden):
		status, code = http.StatusForbidden, "VIDEO_FORBIDDEN"
	case errors.Is(err, video.ErrSessionGone):
		status, code = http.StatusGone, "VIDEO_SESSION_GONE"
	case errors.Is(err, model.ErrGBDeviceTaken):
		status, code = http.StatusConflict, "VIDEO_GB_DEVICE_TAKEN"
	case errors.Is(err, model.ErrNotFound):
		status, code = http.StatusNotFound, "VIDEO_NOT_FOUND"
	case errors.As(err, &validation):
		status, code = http.StatusUnprocessableEntity, "VIDEO_INVALID"
	case video.IsLimit(err):
		status, code = http.StatusTooManyRequests, "VIDEO_LIMIT"
	}
	write(w, status, map[string]any{"type": "about:blank", "title": http.StatusText(status), "status": status, "code": code, "detail": err.Error()})
}

func (s *Server) videoViewer(r *http.Request) video.Viewer {
	c := claims(r)
	v := video.Viewer{Username: c.Username, Managed: c.TokenUse == "user", SessionVersion: c.SessionVersion}
	if c.ExpiresAt != nil {
		v.ExpiresAt = c.ExpiresAt.Time
	}
	return v
}

// videoAuthorize is the single watch-permission rule, used when a session is
// created and again on every heartbeat and periodic sweep:
//   - built-in role tokens see every device of their tenant;
//   - managed users need the watch permission, an enabled account with an
//     unchanged session, and the camera's device inside their device scope;
//   - a camera without a device is only visible to users who manage the whole
//     camera directory (all devices plus the camera menu).
func (s *Server) videoAuthorize(ctx context.Context, tenant string, viewer video.Viewer, camera model.VideoCameraMapping) error {
	if camera.TenantID != "" && camera.TenantID != tenant {
		return video.ErrForbidden
	}
	if !viewer.Managed {
		return nil
	}
	store, err := s.accessStore()
	if err != nil {
		return err
	}
	state, err := store.LoadAccessState(ctx, tenant)
	if err != nil {
		return err
	}
	for _, u := range state.Users {
		if u.Username != viewer.Username {
			continue
		}
		if !u.Enabled || u.SessionVersion != viewer.SessionVersion {
			return video.ErrForbidden
		}
		perms := effectivePermissions(state, u)
		s.stripOpsPermissions(tenant, perms)
		if !perms["menu:devices"] || !perms[videoPlayPermission] {
			return video.ErrForbidden
		}
		scope := scopeFor(resolveUserDeviceScope(state, u), perms, tenant)
		if camera.DeviceID != "" {
			if scope.All || scope.IDs[camera.DeviceID] {
				return nil
			}
			return video.ErrForbidden
		}
		if scope.All && perms["menu:cameras"] {
			return nil
		}
		return video.ErrForbidden
	}
	return video.ErrForbidden
}

func (s *Server) videoCameraLookup(ctx context.Context, tenant, camera string) (model.VideoCameraMapping, error) {
	return s.unscopedRepo().GetVideoCameraMapping(ctx, tenant, camera)
}

// VideoCameraLookup and VideoAuthorize are handed to the live module so its
// background checks use the same camera data and permission rule as the API.
func (s *Server) VideoCameraLookup(ctx context.Context, tenant, camera string) (model.VideoCameraMapping, error) {
	return s.videoCameraLookup(ctx, tenant, camera)
}

func (s *Server) VideoAuthorize(ctx context.Context, tenant string, viewer video.Viewer, camera model.VideoCameraMapping) error {
	return s.videoAuthorize(ctx, tenant, viewer, camera)
}

func (s *Server) videoStatus(w http.ResponseWriter, r *http.Request) {
	var st video.Status
	if s.video == nil {
		st = video.Status{State: video.StateNotDeployed, Message: "直播模块未部署；摄像头资料与设备关联不受影响。"}
	} else {
		st = s.video.Status(r.Context())
	}
	c := claims(r)
	canWatch := c.TokenUse != "user" || requestAllows(r, http.MethodPost, "/api/v1/video/cameras/:id/play-sessions")
	platformAdmin := c.TokenUse != "user" && c.Role == "admin"
	if !platformAdmin {
		// Runtime counters and profiles are only for the platform administrator.
		st.ActiveSessions, st.ActiveSources, st.ActiveTranscodes, st.UpdatedBy = 0, 0, 0, ""
	}
	write(w, http.StatusOK, map[string]any{"status": st, "canWatch": canWatch, "canManageModule": platformAdmin})
}

// videoSetModule is the platform-wide business switch. Only the built-in
// platform administrator may change it; tenant users cannot toggle a module
// shared by every tenant.
func (s *Server) videoSetModule(w http.ResponseWriter, r *http.Request) {
	c := claims(r)
	if c.TokenUse == "user" || c.Role != "admin" {
		problem(w, http.StatusForbidden, "仅平台内置管理员可以启停直播模块")
		return
	}
	v := s.videoService(w)
	if v == nil {
		return
	}
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	if err := v.SetModule(r.Context(), in.Enabled, c.Username); err != nil {
		videoProblem(w, err)
		return
	}
	s.audit(r, "video.module."+map[bool]string{true: "enable", false: "disable"}[in.Enabled], "video-module", "platform", nil)
	write(w, http.StatusOK, v.Status(r.Context()))
}

type cameraLiveBrief struct {
	model.CameraSummary
	LiveAvailable bool `json:"liveAvailable"`
}

func (s *Server) videoDeviceCameras(w http.ResponseWriter, r *http.Request) {
	tenant := claims(r).TenantID
	deviceID := r.PathValue("deviceId")
	items, err := s.engine.ListCameraSummaries(r.Context(), tenant, deviceID)
	if err != nil {
		problem(w, http.StatusInternalServerError, "读取关联摄像头失败")
		return
	}
	out := make([]cameraLiveBrief, 0, len(items))
	for _, item := range items {
		brief := cameraLiveBrief{CameraSummary: item}
		if s.video != nil && item.Enabled {
			brief.LiveAvailable = s.video.CameraAvailable(r.Context(), tenant, item.CameraID)
		}
		out = append(out, brief)
	}
	write(w, http.StatusOK, map[string]any{"items": out})
}

// videoCameraSummary returns safe metadata for one camera the viewer may
// watch (used by OPEN_CAMERA). Knowing a camera ID grants nothing: the same
// device-scope rule as playback applies.
func (s *Server) videoCameraSummary(w http.ResponseWriter, r *http.Request) {
	tenant := claims(r).TenantID
	camera, err := s.videoCameraLookup(r.Context(), tenant, r.PathValue("id"))
	if err != nil || s.videoAuthorize(r.Context(), tenant, s.videoViewer(r), camera) != nil || !s.videoDeviceVisible(r, camera) {
		problem(w, http.StatusNotFound, "摄像头不存在或无权查看")
		return
	}
	brief := cameraLiveBrief{CameraSummary: model.CameraSummary{CameraID: camera.CameraID, Brand: camera.Brand, CameraName: camera.CameraName, CameraPoint: camera.CameraPoint, DeviceID: camera.DeviceID, Building: camera.Building, Floor: camera.Floor, Room: camera.Room, Enabled: camera.Enabled}}
	if s.video != nil && camera.Enabled {
		brief.LiveAvailable = s.video.CameraAvailable(r.Context(), tenant, camera.CameraID)
	}
	write(w, http.StatusOK, brief)
}

// videoDeviceVisible applies the request's own device scope as well, so a
// built-in token and a managed user are both limited by the request context.
func (s *Server) videoDeviceVisible(r *http.Request, camera model.VideoCameraMapping) bool {
	if camera.DeviceID != "" {
		return deviceAllowed(r.Context(), claims(r).TenantID, camera.DeviceID)
	}
	return !limited(r.Context())
}

func (s *Server) videoCreateSession(w http.ResponseWriter, r *http.Request) {
	v := s.videoService(w)
	if v == nil {
		return
	}
	var req video.PlayRequest
	if decode(w, r, &req) != nil {
		return
	}
	tenant := claims(r).TenantID
	camera, err := s.videoCameraLookup(r.Context(), tenant, r.PathValue("id"))
	if err != nil || !s.videoDeviceVisible(r, camera) {
		videoProblem(w, video.ErrForbidden)
		return
	}
	grant, err := v.CreateSession(r.Context(), tenant, s.videoViewer(r), camera.CameraID, req)
	if err != nil {
		videoProblem(w, err)
		return
	}
	s.audit(r, "video.play.start", "video-camera", camera.CameraID, map[string]any{"session": grant.SessionID, "stream": grant.Stream, "profile": grant.Profile})
	write(w, http.StatusCreated, grant)
}

func (s *Server) videoWHEP(w http.ResponseWriter, r *http.Request) {
	v := s.videoService(w)
	if v == nil {
		return
	}
	offer, err := io.ReadAll(io.LimitReader(r.Body, 64<<10+1))
	if err != nil {
		problem(w, http.StatusBadRequest, "读取 SDP 失败")
		return
	}
	answer, err := v.WHEP(r.Context(), claims(r).TenantID, s.videoViewer(r), r.PathValue("id"), string(offer))
	if err != nil {
		videoProblem(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/sdp")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusCreated)
	_, _ = io.WriteString(w, answer)
}

func (s *Server) videoHeartbeat(w http.ResponseWriter, r *http.Request) {
	v := s.videoService(w)
	if v == nil {
		return
	}
	var in struct {
		Rendering bool `json:"rendering"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	result, err := v.Heartbeat(r.Context(), claims(r).TenantID, s.videoViewer(r), r.PathValue("id"), in.Rendering)
	if err != nil {
		videoProblem(w, err)
		return
	}
	write(w, http.StatusOK, result)
}

func (s *Server) videoStopSession(w http.ResponseWriter, r *http.Request) {
	v := s.videoService(w)
	if v == nil {
		return
	}
	if err := v.Stop(claims(r).TenantID, s.videoViewer(r), r.PathValue("id")); err != nil && !errors.Is(err, video.ErrSessionGone) {
		videoProblem(w, err)
		return
	}
	write(w, http.StatusOK, map[string]bool{"stopped": true})
}

func (s *Server) videoLiveCamera(w http.ResponseWriter, r *http.Request) (model.VideoCameraMapping, bool) {
	camera, err := s.videoCameraLookup(r.Context(), claims(r).TenantID, r.PathValue("id"))
	if err != nil {
		problem(w, http.StatusNotFound, "摄像头不存在")
		return camera, false
	}
	return camera, true
}

func (s *Server) videoGetLiveConfig(w http.ResponseWriter, r *http.Request) {
	v := s.videoService(w)
	if v == nil {
		return
	}
	if _, ok := s.videoLiveCamera(w, r); !ok {
		return
	}
	cfg, err := v.GetConfig(r.Context(), claims(r).TenantID, r.PathValue("id"))
	if err != nil {
		videoProblem(w, err)
		return
	}
	write(w, http.StatusOK, cfg)
}

func (s *Server) videoSaveLiveConfig(w http.ResponseWriter, r *http.Request) {
	v := s.videoService(w)
	if v == nil {
		return
	}
	camera, ok := s.videoLiveCamera(w, r)
	if !ok {
		return
	}
	var in video.LiveConfigInput
	if decode(w, r, &in) != nil {
		return
	}
	cfg, err := v.SaveConfig(r.Context(), claims(r).TenantID, camera.CameraID, in)
	if err != nil {
		videoProblem(w, err)
		return
	}
	s.audit(r, "video.camera.live.save", "video-camera", camera.CameraID, map[string]any{"enabled": cfg.Enabled, "accessMode": cfg.AccessMode, "transcodeMode": cfg.TranscodeMode, "passwordChanged": in.Password != "" || in.ClearPassword})
	write(w, http.StatusOK, cfg)
}

func (s *Server) videoTestLive(w http.ResponseWriter, r *http.Request) {
	v := s.videoService(w)
	if v == nil {
		return
	}
	camera, ok := s.videoLiveCamera(w, r)
	if !ok {
		return
	}
	var in video.LiveConfigInput
	var input *video.LiveConfigInput
	if r.ContentLength != 0 {
		if decode(w, r, &in) != nil {
			return
		}
		input = &in
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	result, err := v.Test(ctx, claims(r).TenantID, camera.CameraID, input)
	if err != nil {
		videoProblem(w, err)
		return
	}
	s.audit(r, "video.camera.live.test", "video-camera", camera.CameraID, map[string]any{"status": result.Status})
	write(w, http.StatusOK, result)
}

func (s *Server) videoONVIFProfiles(w http.ResponseWriter, r *http.Request) {
	v := s.videoService(w)
	if v == nil {
		return
	}
	camera, ok := s.videoLiveCamera(w, r)
	if !ok {
		return
	}
	var in video.LiveConfigInput
	if decode(w, r, &in) != nil {
		return
	}
	profiles, err := v.ONVIFProfiles(r.Context(), claims(r).TenantID, camera.CameraID, in)
	if err != nil {
		var validation video.ValidationError
		if errors.As(err, &validation) || errors.Is(err, video.ErrNotDeployed) {
			videoProblem(w, err)
			return
		}
		// Device-side failures are results for the form, not API errors.
		write(w, http.StatusOK, map[string]any{"items": []any{}, "error": err.Error()})
		return
	}
	write(w, http.StatusOK, map[string]any{"items": profiles})
}

func (s *Server) videoHook(w http.ResponseWriter, r *http.Request) {
	event := r.PathValue("event")
	if s.video == nil || !videoHookEvents[event] || !s.video.VerifyHookSecret(r.URL.Query().Get("secret")) {
		write(w, http.StatusForbidden, map[string]any{"code": -1, "msg": "forbidden"})
		return
	}
	var body video.HookBody
	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		write(w, http.StatusBadRequest, map[string]any{"code": -1, "msg": "invalid body"})
		return
	}
	// Media server hook bodies carry many extra fields; read only known ones.
	if err := json.Unmarshal(data, &body); err != nil {
		write(w, http.StatusBadRequest, map[string]any{"code": -1, "msg": "invalid body"})
		return
	}
	write(w, http.StatusOK, s.video.Hook(r.Context(), event, body))
}

func (s *Server) videoMediaAuth(w http.ResponseWriter, r *http.Request) {
	if s.video != nil && s.video.MediaAuth(r.Context(), r.Header.Get("X-Original-URI")) {
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.WriteHeader(http.StatusForbidden)
}

// videoCameraChanged ends playback when a saved camera's authorization
// inputs changed: another device, or the camera was disabled.
func (s *Server) videoCameraChanged(previous *model.VideoCameraMapping, current model.VideoCameraMapping) {
	if s.video == nil || previous == nil {
		return
	}
	if previous.DeviceID != current.DeviceID || (previous.Enabled && !current.Enabled) {
		s.video.CameraChanged(current.TenantID, current.CameraID, "camera association changed")
	}
}

// liveCameraItem adds safe live state to the camera list.
type liveCameraItem struct {
	model.VideoCameraMapping
	Live map[string]any `json:"live,omitempty"`
}

func (s *Server) attachLiveSummaries(r *http.Request, items []model.VideoCameraMapping) any {
	if s.video == nil || len(items) == 0 {
		return items
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.CameraID)
	}
	live := s.video.LiveSummaries(r.Context(), claims(r).TenantID, ids)
	out := make([]liveCameraItem, 0, len(items))
	for _, item := range items {
		out = append(out, liveCameraItem{VideoCameraMapping: item, Live: live[item.CameraID]})
	}
	return out
}

// GB28181 devices register to the platform's SIP server. Device IDs are
// global SIP identities; every handler stays within the caller's tenant.

func (s *Server) videoListGBDevices(w http.ResponseWriter, r *http.Request) {
	v := s.videoService(w)
	if v == nil {
		return
	}
	items, err := v.ListGBDevices(r.Context(), claims(r).TenantID)
	if err != nil {
		videoProblem(w, err)
		return
	}
	write(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) videoSaveGBDevice(w http.ResponseWriter, r *http.Request) {
	v := s.videoService(w)
	if v == nil {
		return
	}
	var in video.GBDeviceInput
	if decode(w, r, &in) != nil {
		return
	}
	d, err := v.SaveGBDevice(r.Context(), claims(r).TenantID, r.PathValue("deviceId"), in)
	if err != nil {
		videoProblem(w, err)
		return
	}
	s.audit(r, "video.gb28181.device.save", "video-gb-device", d.DeviceID, map[string]any{"enabled": d.Enabled, "streamTransport": d.StreamTransport, "passwordChanged": in.Password != ""})
	write(w, http.StatusOK, d)
}

func (s *Server) videoDeleteGBDevice(w http.ResponseWriter, r *http.Request) {
	v := s.videoService(w)
	if v == nil {
		return
	}
	id := r.PathValue("deviceId")
	if err := v.DeleteGBDevice(r.Context(), claims(r).TenantID, id); err != nil {
		videoProblem(w, err)
		return
	}
	s.audit(r, "video.gb28181.device.delete", "video-gb-device", id, nil)
	write(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (s *Server) videoRefreshGBDevice(w http.ResponseWriter, r *http.Request) {
	v := s.videoService(w)
	if v == nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := v.RefreshGBDevice(ctx, claims(r).TenantID, r.PathValue("deviceId")); err != nil {
		videoProblem(w, err)
		return
	}
	write(w, http.StatusAccepted, map[string]bool{"requested": true})
}
