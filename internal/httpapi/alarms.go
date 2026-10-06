package httpapi

import (
	"net/http"
	"strings"

	"iot-platform/internal/ports"
	"iot-platform/internal/sites"
)

func (s *Server) alarms(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	pagination := parseListPagination(r)
	filter := ports.AlarmFilter{TenantID: claims(r).TenantID, DeviceID: q.Get("deviceId"), Status: q.Get("status"), Level: q.Get("level"), Source: q.Get("source"), Start: i64(q.Get("start")), End: i64(q.Get("end")), Limit: pagination.PageSize, Offset: pagination.Offset}
	items, err := s.engine.Repo.ListAlarms(r.Context(), filter)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	total, err := s.engine.Repo.CountAlarms(r.Context(), filter)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	deviceIDs := make([]string, 0, len(items))
	for _, item := range items {
		deviceIDs = append(deviceIDs, item.DeviceID)
	}
	cameras, err := s.engine.ListCameraSummariesForDevices(r.Context(), claims(r).TenantID, deviceIDs)
	if err == nil {
		for index := range items {
			if linked := cameras[items[index].DeviceID]; len(linked) > 0 || items[index].Source != "video" {
				items[index].Cameras = linked
			}
		}
	} else {
		for index := range items {
			if summaries, getErr := s.engine.ListCameraSummaries(r.Context(), items[index].TenantID, items[index].DeviceID); getErr == nil {
				items[index].Cameras = summaries
			}
		}
	}
	writeList(w, 200, items, total, pagination, nil)
}

func (s *Server) alarm(w http.ResponseWriter, r *http.Request) {
	v, err := s.engine.Repo.GetAlarm(r.Context(), claims(r).TenantID, r.PathValue("id"))
	if err != nil {
		problem(w, 404, "alarm not found")
		return
	}
	if cameras, cameraErr := s.engine.ListCameraSummaries(r.Context(), v.TenantID, v.DeviceID); cameraErr == nil && (len(cameras) > 0 || v.Source != "video") {
		// Video analysis alarms name the camera itself as their source; keep
		// the camera summary recorded with the alarm when no device is linked.
		v.Cameras = cameras
	}
	if v.Location == nil {
		if state, err := s.sites.Snapshot(r.Context(), v.TenantID); err == nil {
			if v.Location = sites.Locate(state, v.DeviceID, v.ComponentID); v.Location != nil {
				v.Location.Current = true
			}
		}
	}
	write(w, 200, v)
}

func (s *Server) alarmAction(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Action string `json:"action"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	v, err := s.engine.SetAlarmStatus(r.Context(), claims(r).TenantID, r.PathValue("id"), strings.ToUpper(in.Action), claims(r).Username)
	if err != nil {
		problem(w, 422, err.Error())
		return
	}
	write(w, 200, v)
}
