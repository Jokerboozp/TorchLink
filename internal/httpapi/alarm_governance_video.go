package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	minio "github.com/minio/minio-go/v7"
	"io"
	"iot-platform/internal/alarmgovernance"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"net/http"
	"net/url"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"
)

const governanceVideoMediaMax = 256 << 20

var errVideoMediaUnavailable = errors.New("historical video media unavailable")

// This optional reader is captured before Repository decorators, whose narrow
// interfaces otherwise hide historical video reads.
func (s *Server) SetGovernanceVideoReader(reader ports.VideoEventReader) {
	s.governanceVideoEvents = reader
}
func (s *Server) governanceVideoReader() ports.VideoEventReader {
	if s.governanceVideoEvents != nil {
		return s.governanceVideoEvents
	}
	reader, _ := s.unscopedRepo().(ports.VideoEventReader)
	return reader
}
func (s *Server) governanceVideoRoutes() {
	s.router.GET("/api/v1/alarm-governance/video-events", s.authorize("viewer"), s.endpoint(s.governanceVideoEventsList))
	s.router.GET("/api/v1/alarm-governance/business-links/:id/media", s.authorize("viewer"), s.endpoint(s.governanceVideoMedia, "id"))
}
func videoPermission(a alarmgovernance.Actor, permission string) bool {
	return slices.Contains(a.Permissions, "*") || slices.Contains(a.Permissions, permission)
}
func videoHistoryPermission(a alarmgovernance.Actor) bool {
	return videoPermission(a, "menu:cameras") || videoPermission(a, "action:cameras:history")
}
func videoSourceMembers(source ports.VideoEventSource) []string {
	ids := []string{}
	for _, id := range []string{source.DeviceID, source.CurrentDeviceID} {
		if id != "" && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}
func authorizeVideoSource(a alarmgovernance.Actor, source ports.VideoEventSource) error {
	for _, permission := range []string{"menu:devices", "menu:alarms", "menu:alarmGovernance"} {
		if !videoPermission(a, permission) {
			return alarmgovernance.ErrForbidden
		}
	}
	if !videoHistoryPermission(a) {
		return alarmgovernance.ErrForbidden
	}
	// An old event without a saved binding cannot be attributed retrospectively
	// using today's camera mapping. Only a whole-tenant reader may inspect it.
	if !a.AllDevices && source.DeviceID == "" {
		return alarmgovernance.ErrForbidden
	}
	for _, id := range videoSourceMembers(source) {
		if !a.AllDevices && !slices.Contains(a.DeviceIDs, id) {
			return alarmgovernance.ErrForbidden
		}
	}
	return nil
}
func videoSourceVersion(source ports.VideoEventSource) int64 {
	v := source.Event
	// Media transfer is asynchronous; its URLs and mutable transfer status do
	// not change the version token of the underlying historical event.
	b, _ := json.Marshal([]any{v.EventID, v.TenantID, v.Source, v.ProjectID, v.CameraID, v.CameraName, v.AlarmType, v.AlarmName, v.AlarmLevel, v.Confidence, v.EventTime, v.ReceivedAt, source.DeviceID, source.CurrentDeviceID})
	h := sha256.Sum256(b)
	// JSON numbers must remain exact in the browser; this is a content token,
	// not a fictitious monotonically increasing source generation.
	n := int64(binary.BigEndian.Uint64(h[:8]) & ((1 << 53) - 1))
	if n == 0 {
		return 1
	}
	return n
}
func videoSourceSummary(source ports.VideoEventSource) string {
	v := source.Event
	name := v.CameraName
	if name == "" {
		name = "历史摄像头事件"
	}
	alarm := v.AlarmName
	if alarm == "" {
		alarm = v.AlarmType
	}
	summary := fmt.Sprintf("%s：%s，%s", name, alarm, time.UnixMilli(v.EventTime).UTC().Format(time.RFC3339))
	if source.DeviceID == "" {
		summary += "；事件发生时设备绑定未知"
	}
	if len([]rune(summary)) > 500 {
		summary = string([]rune(summary)[:500])
	}
	return summary
}
func (s *Server) readGovernanceVideo(ctx context.Context, a alarmgovernance.Actor, id string) (ports.VideoEventSource, error) {
	reader := s.governanceVideoReader()
	if reader == nil {
		return ports.VideoEventSource{}, alarmgovernance.ErrSourceUnavailable
	}
	v, err := reader.GetGovernanceVideoEvent(ctx, a.TenantID, id)
	if err != nil {
		return ports.VideoEventSource{}, alarmgovernance.ErrForbidden
	}
	if err = authorizeVideoSource(a, v); err != nil {
		return ports.VideoEventSource{}, err
	}
	return v, nil
}
func (s *Server) governanceVideoSource(ctx context.Context, a alarmgovernance.Actor, id string) (alarmgovernance.BusinessSource, error) {
	v, err := s.readGovernanceVideo(ctx, a, id)
	if err != nil {
		return alarmgovernance.BusinessSource{}, err
	}
	ids := videoSourceMembers(v)
	if len(ids) == 0 {
		return alarmgovernance.BusinessSource{}, alarmgovernance.ErrForbidden
	}
	return alarmgovernance.BusinessSource{Version: videoSourceVersion(v), DeviceIDs: ids, Summary: videoSourceSummary(v), Status: "AVAILABLE"}, nil
}
func (s *Server) governanceVideoEventsList(w http.ResponseWriter, r *http.Request) {
	if !s.governanceReady(w) {
		return
	}
	a, err := s.resolveGovernanceActor(r.Context(), governanceActor(r))
	if err != nil {
		governanceProblem(w, err)
		return
	}
	for _, permission := range []string{"menu:devices", "menu:alarms", "menu:alarmGovernance"} {
		if !videoPermission(a, permission) {
			governanceProblem(w, alarmgovernance.ErrForbidden)
			return
		}
	}
	if !videoHistoryPermission(a) {
		governanceProblem(w, alarmgovernance.ErrForbidden)
		return
	}
	reader := s.governanceVideoReader()
	if reader == nil {
		governanceProblem(w, alarmgovernance.ErrSourceUnavailable)
		return
	}
	q := r.URL.Query()
	f := ports.VideoEventFilter{AllDevices: a.AllDevices, Start: governanceInt(q.Get("start"), 0), End: governanceInt(q.Get("end"), 0), Cursor: q.Get("cursor"), Limit: intval(q.Get("limit"), 20)}
	if f.Start <= 0 || f.End <= f.Start || f.End-f.Start > 90*24*time.Hour.Milliseconds() || f.Limit < 1 || f.Limit > 100 {
		problem(w, 422, "历史视频事件查询须给出最多90日的有界区间，每页1至100条")
		return
	}
	for _, id := range strings.Split(q.Get("deviceIds"), ",") {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if !a.AllDevices && !slices.Contains(a.DeviceIDs, id) {
			governanceProblem(w, alarmgovernance.ErrForbidden)
			return
		}
		if !slices.Contains(f.DeviceIDs, id) {
			f.DeviceIDs = append(f.DeviceIDs, id)
		}
	}
	if len(f.DeviceIDs) == 0 && !a.AllDevices {
		f.DeviceIDs = a.DeviceIDs
	}
	if len(f.DeviceIDs) > 200 {
		problem(w, 422, "历史视频事件查询设备过多")
		return
	}
	page, err := reader.ListGovernanceVideoEvents(r.Context(), a.TenantID, f)
	if err != nil {
		governanceProblem(w, err)
		return
	}
	// Re-resolve both permissions and mappings after the query. No cached
	// source metadata is released when a camera moved during the read.
	a, err = s.resolveGovernanceActor(r.Context(), a)
	if err != nil {
		governanceProblem(w, err)
		return
	}
	for _, permission := range []string{"menu:devices", "menu:alarms", "menu:alarmGovernance"} {
		if !videoPermission(a, permission) {
			governanceProblem(w, alarmgovernance.ErrForbidden)
			return
		}
	}
	if !videoHistoryPermission(a) {
		governanceProblem(w, alarmgovernance.ErrForbidden)
		return
	}
	items := []map[string]any{}
	for _, source := range page {
		fresh, e := s.readGovernanceVideo(r.Context(), a, source.Event.EventID)
		if e != nil {
			governanceProblem(w, e)
			return
		}
		v := fresh.Event
		quality := "CAPTURED_BINDING"
		if fresh.DeviceID == "" {
			quality = "HISTORICAL_BINDING_UNKNOWN"
		}
		items = append(items, map[string]any{"eventId": v.EventID, "targetKind": "VIDEO_EVENT", "targetVersion": videoSourceVersion(fresh), "deviceIds": videoSourceMembers(fresh), "cameraId": v.CameraID, "cameraName": v.CameraName, "alarmType": v.AlarmType, "alarmName": v.AlarmName, "eventTime": v.EventTime, "receivedAt": v.ReceivedAt, "summary": videoSourceSummary(fresh), "bindingQuality": quality, "media": map[string]any{"snapshot": videoMediaAvailability(v, "snapshot"), "clip": videoMediaAvailability(v, "clip")}})
	}
	cursor := ""
	if len(page) == f.Limit {
		v := page[len(page)-1].Event
		cursor = strconv.FormatInt(v.EventTime, 10) + ":" + v.EventID
	}
	write(w, 200, map[string]any{"items": items, "nextCursor": cursor})
}
func videoMediaURL(v model.VideoAlarmEvent, kind string) string {
	if kind == "snapshot" {
		return v.SnapshotURL
	}
	if kind == "clip" {
		return v.VideoClipURL
	}
	return ""
}
func videoMediaAvailability(v model.VideoAlarmEvent, kind string) map[string]string {
	status := "NO_MEDIA"
	if raw := videoMediaURL(v, kind); raw != "" {
		if _, _, err := videoMediaObject(v, kind); err == nil {
			status = "ARCHIVED_REFERENCE"
		} else {
			status = "UNAVAILABLE_REFERENCE"
		}
	}
	if transfer, _ := v.Raw["mediaTransferStatus"].(string); status != "ARCHIVED_REFERENCE" && transfer == "PENDING" {
		status = "TRANSFER_PENDING"
	} else if status != "ARCHIVED_REFERENCE" && transfer == "FAILED" {
		status = "TRANSFER_FAILED"
	}
	return map[string]string{"status": status, "retention": "OBJECT_EXISTENCE_CHECKED_ON_DOWNLOAD"}
}
func videoMediaSegment(v string) string {
	return strings.NewReplacer("/", "_", "\\", "_", "..", "_").Replace(v)
}
func videoMediaObject(v model.VideoAlarmEvent, kind string) (string, string, error) {
	if kind != "snapshot" && kind != "clip" {
		return "", "", model.ErrGovernanceInvalid
	}
	// The old archiver sanitized identifiers. A changed segment can collide
	// with a different event or camera, so it is never a download authority.
	for _, id := range []string{v.TenantID, v.EventID, v.CameraID} {
		if id == "" || videoMediaSegment(id) != id || strings.ContainsAny(id, "\x00\r\n") {
			return "", "", model.ErrNotFound
		}
	}
	u, err := url.Parse(videoMediaURL(v, kind))
	if err != nil || (u.Scheme != "minio" && u.Scheme != "local") || u.Host != "video-alarm" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", "", model.ErrNotFound
	}
	key := strings.TrimPrefix(u.Path, "/")
	if strings.ContainsAny(key, "\\\x00\r\n") || path.Clean(key) != key {
		return "", "", model.ErrNotFound
	}
	prefix := fmt.Sprintf("%s/%s/%s/%s-%s", videoMediaSegment(v.TenantID), time.UnixMilli(v.EventTime).UTC().Format("2006/01/02"), videoMediaSegment(v.EventID), kind, videoMediaSegment(v.CameraID))
	if !strings.HasPrefix(key, prefix) {
		return "", "", model.ErrNotFound
	}
	ext := strings.TrimPrefix(key, prefix)
	if ext != "" {
		if len(ext) < 2 || len(ext) > 10 || ext[0] != '.' {
			return "", "", model.ErrNotFound
		}
		for _, r := range ext[1:] {
			if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
				return "", "", model.ErrNotFound
			}
		}
	}
	return "video-alarm", key, nil
}
func videoMediaMissing(err error) bool {
	if os.IsNotExist(err) {
		return true
	}
	code := minio.ToErrorResponse(err).Code
	return code == "NoSuchKey" || code == "NoSuchObject" || code == "NotFound"
}
func videoMediaReadProblem(w http.ResponseWriter, err error) {
	if videoMediaMissing(err) {
		problem(w, 410, "历史媒体不存在或已过期，事件元数据仍保留")
		return
	}
	problem(w, 503, "历史媒体归档暂时不可读取，请稍后重试")
}
func (s *Server) governanceVideoMedia(w http.ResponseWriter, r *http.Request) {
	if !s.governanceReady(w) {
		return
	}
	kind := r.URL.Query().Get("kind")
	if kind != "snapshot" && kind != "clip" {
		problem(w, 422, "媒体类型须为snapshot或clip")
		return
	}
	read := func() (ports.VideoEventSource, string, string, error) {
		a, e := s.resolveGovernanceActor(r.Context(), governanceActor(r))
		if e != nil || !videoPermission(a, "action:cameras:download") {
			return ports.VideoEventSource{}, "", "", alarmgovernance.ErrForbidden
		}
		d, e := s.governance.Get(r.Context(), a, model.GovernanceBusinessLinkKind, r.PathValue("id"))
		if e != nil {
			return ports.VideoEventSource{}, "", "", e
		}
		link, e := model.GovernanceBody[model.GovernanceBusinessLink](d)
		if e != nil || link.TargetKind != "VIDEO_EVENT" {
			return ports.VideoEventSource{}, "", "", model.ErrGovernanceInvalid
		}
		v, e := s.readGovernanceVideo(r.Context(), a, link.TargetID)
		if e != nil {
			return v, "", "", e
		}
		bucket, key, e := videoMediaObject(v.Event, kind)
		if errors.Is(e, model.ErrNotFound) {
			e = errVideoMediaUnavailable
		}
		return v, bucket, key, e
	}
	_, bucket, key, err := read()
	if err != nil {
		if errors.Is(err, errVideoMediaUnavailable) {
			problem(w, 410, "历史媒体不存在、已过期或不属于平台归档，事件元数据仍保留")
		} else {
			governanceProblem(w, err)
		}
		return
	}
	if s.engine.Archive == nil {
		problem(w, 503, "历史媒体归档不可用")
		return
	}
	body, err := s.engine.Archive.GetObject(r.Context(), bucket, key)
	if err != nil {
		videoMediaReadProblem(w, err)
		return
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, governanceVideoMediaMax+1))
	if err != nil {
		videoMediaReadProblem(w, err)
		return
	}
	if len(data) > governanceVideoMediaMax {
		problem(w, 413, "历史媒体超出下载大小限制")
		return
	}
	_, freshBucket, freshKey, err := read()
	if err != nil {
		if errors.Is(err, errVideoMediaUnavailable) {
			problem(w, 409, "历史媒体来源已变化，请重新读取")
		} else {
			governanceProblem(w, err)
		}
		return
	}
	if freshBucket != bucket || freshKey != key {
		problem(w, 409, "历史媒体来源已变化，请重新读取")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+kind+path.Ext(key)+"\"")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(200)
	_, _ = w.Write(data)
}
