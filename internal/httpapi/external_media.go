package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"iot-platform/internal/core"
	"iot-platform/internal/model"
)

const maxAlarmMediaBytes = 256 << 20

func (s *Server) externalMediaRoutes() {
	s.router.GET("/api/v1/alarms/:id/media/:kind", s.authorize("viewer"), s.endpoint(s.externalAlarmMedia, "id", "kind"))
	s.router.POST("/api/v1/alarms/:id/media/retry", s.authorize("operator"), s.endpoint(s.externalAlarmMediaRetry, "id"))
}

func alarmVideoEvent(a model.Alarm) (model.VideoAlarmEvent, error) {
	var event model.VideoAlarmEvent
	raw, err := json.Marshal(a.Details["videoEvent"])
	if err != nil {
		return event, err
	}
	if err = json.Unmarshal(raw, &event); err != nil {
		return event, err
	}
	if event.EventID == "" || event.TenantID != a.TenantID {
		return event, errors.New("告警没有关联视频事件")
	}
	return event, nil
}

func (s *Server) externalAlarmMedia(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if kind != "snapshot" && kind != "clip" {
		problem(w, 422, "媒体类型须为 snapshot 或 clip")
		return
	}
	a, err := s.engine.Repo.GetAlarm(r.Context(), claims(r).TenantID, r.PathValue("id"))
	if err != nil {
		problem(w, 404, "告警不存在或无访问权限")
		return
	}
	v, err := alarmVideoEvent(a)
	if err != nil {
		problem(w, 404, "告警没有关联视频事件")
		return
	}
	bucket, key, err := core.VideoMediaObjectReference(v, kind)
	if err != nil {
		problem(w, 409, "媒体尚未完成归档，请查看媒体状态或重试")
		return
	}
	if s.engine.Archive == nil {
		problem(w, 503, "媒体存储暂不可用")
		return
	}
	reader, err := s.engine.Archive.GetObject(r.Context(), bucket, key)
	if err != nil {
		problem(w, 404, "媒体文件不可用")
		return
	}
	defer reader.Close()
	prefix := make([]byte, 512)
	n, err := io.ReadFull(reader, prefix)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		problem(w, 404, "媒体文件不可用")
		return
	}
	prefix = prefix[:n]
	contentType := http.DetectContentType(prefix)
	extensions := map[string]string{}
	if kind == "snapshot" {
		extensions = map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp", "image/bmp": ".bmp"}
	} else {
		extensions = map[string]string{"video/mp4": ".mp4", "video/webm": ".webm", "video/mpeg": ".mpeg", "application/ogg": ".ogv"}
	}
	ext, ok := extensions[contentType]
	if !ok {
		problem(w, 415, "媒体内容不是支持的图片或视频格式")
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Disposition", `attachment; filename="`+kind+ext+`"`)
	if seeker, ok := reader.(io.ReadSeeker); ok {
		size, err := seeker.Seek(0, io.SeekEnd)
		if err != nil || size < 0 || size > maxAlarmMediaBytes {
			problem(w, 422, "媒体文件不可用或超过大小限制")
			return
		}
		if _, err = seeker.Seek(0, io.SeekStart); err != nil {
			problem(w, 500, "媒体文件读取失败")
			return
		}
		http.ServeContent(w, r, kind+ext, time.Time{}, seeker)
		return
	}
	if r.Header.Get("Range") != "" {
		problem(w, http.StatusRequestedRangeNotSatisfiable, "当前媒体存储不支持范围读取")
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, io.LimitReader(io.MultiReader(bytes.NewReader(prefix), reader), maxAlarmMediaBytes))
}

func (s *Server) externalAlarmMediaRetry(w http.ResponseWriter, r *http.Request) {
	tenant := claims(r).TenantID
	id := r.PathValue("id")
	a, err := s.engine.Repo.GetAlarm(r.Context(), tenant, id)
	if err != nil {
		problem(w, 404, "告警不存在或无访问权限")
		return
	}
	v, err := alarmVideoEvent(a)
	if err != nil {
		problem(w, 404, "告警没有关联视频事件")
		return
	}
	v, err = s.engine.QueueVideoMediaRetry(r.Context(), tenant, v.EventID)
	if err != nil {
		problem(w, 422, "视频事件不存在或没有可重试的媒体")
		return
	}
	// Only refresh this authorized alarm's exact event; lifecycle/status are
	// unchanged. A concurrent event replacement must not be overwritten.
	for attempt := 0; attempt < 3; attempt++ {
		current, getErr := s.engine.Repo.GetAlarm(r.Context(), tenant, id)
		if getErr != nil {
			break
		}
		linked, decodeErr := alarmVideoEvent(current)
		if decodeErr != nil || linked.EventID != v.EventID {
			break
		}
		current.Details["videoEvent"] = v
		written, updateErr := s.engine.Repo.UpdateAlarmIf(r.Context(), current)
		if updateErr != nil || written {
			break
		}
	}
	s.audit(r, "alarm.media.retry", "alarm", id, nil)
	w.Header().Set("Cache-Control", "no-store")
	write(w, http.StatusAccepted, map[string]any{"queued": true, "eventId": v.EventID, "status": "PENDING"})
}
