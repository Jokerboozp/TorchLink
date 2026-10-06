package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"io"
	"mime"
	"net/http"
	"path"
	"slices"
	"strings"
	"unicode"

	"iot-platform/internal/core"
	"iot-platform/internal/model"
)

const alarmAttachmentPath = "/api/v1/alarms/:id/attachments"

func (s *Server) alarmAttachmentRoutes() {
	s.router.POST(alarmAttachmentPath, s.authorize("operator"), s.endpoint(s.uploadAlarmAttachment, "id"))
	s.router.GET(alarmAttachmentPath+"/:attachmentId", s.authorize("viewer"), s.endpoint(s.alarmAttachment, "id", "attachmentId"))
	s.router.DELETE(alarmAttachmentPath+"/:attachmentId", s.authorize("operator"), s.endpoint(s.deleteAlarmAttachment, "id", "attachmentId"))
}

// attachmentType accepts PNG and JPEG photos, PDF documents and MP4 video,
// judged by content rather than by the file name.
func attachmentType(data []byte) (string, string, bool) {
	if _, format, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
		switch format {
		case "png":
			return "image/png", ".png", true
		case "jpeg":
			return "image/jpeg", ".jpg", true
		}
	}
	if bytes.HasPrefix(data, []byte("%PDF-")) {
		return "application/pdf", ".pdf", true
	}
	if http.DetectContentType(data) == "video/mp4" {
		return "video/mp4", ".mp4", true
	}
	return "", "", false
}

// attachmentName keeps a printable base name of at most 100 characters with
// the extension of the detected type.
func attachmentName(name, ext string) string {
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '/' || r == '\\' {
			return -1
		}
		return r
	}, path.Base(strings.ReplaceAll(name, "\\", "/")))
	name = strings.TrimSpace(strings.TrimSuffix(name, path.Ext(name)))
	if name == "" || name == "." {
		name = "附件"
	}
	if runes := []rune(name); len(runes) > 100 {
		name = string(runes[:100])
	}
	return name + ext
}

func (s *Server) attachmentProblem(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, errDeviceScope):
		problemCode(w, 403, codeDeviceScopeDenied, "无权处置该设备的告警")
	case errors.Is(err, model.ErrNotFound):
		problem(w, 404, "告警不存在")
	case errors.Is(err, model.ErrAttachmentNotFound):
		problem(w, 404, err.Error())
	case errors.Is(err, model.ErrAlarmClosed), errors.Is(err, model.ErrTooManyAttachments):
		problem(w, 422, err.Error())
	default:
		s.failure(w, r, err, "保存附件失败")
	}
}

func (s *Server) uploadAlarmAttachment(w http.ResponseWriter, r *http.Request) {
	if s.engine.Archive == nil {
		problem(w, 503, "对象存储未配置，无法保存附件")
		return
	}
	c, alarmID := claims(r), r.PathValue("id")
	// Check the alarm before reading the upload.
	alarm, err := s.engine.Repo.GetAlarm(r.Context(), c.TenantID, alarmID)
	if err == nil && alarm.Status == "CLOSED" {
		err = model.ErrAlarmClosed
	} else if err == nil && len(alarm.Attachments) >= model.MaxAlarmAttachments {
		err = model.ErrTooManyAttachments
	}
	if err != nil {
		s.attachmentProblem(w, r, err)
		return
	}
	data, filename, ok := readUpload(w, r, model.MaxAlarmAttachmentSize, "附件不能超过 20 MiB")
	if !ok {
		return
	}
	contentType, ext, ok := attachmentType(data)
	if !ok {
		problem(w, 422, "附件须为 PNG、JPEG 图片、PDF 文档或 MP4 视频")
		return
	}
	digest := sha256.Sum256(data)
	att := model.AlarmAttachment{ID: core.NewAlarmAttachmentID(), Name: attachmentName(filename, ext), ContentType: contentType, Size: int64(len(data)), SHA256: hex.EncodeToString(digest[:])}
	key := model.AlarmAttachmentKey(c.TenantID, alarmID, att.ID)
	if _, err = s.engine.Archive.PutObject(r.Context(), model.AlarmAttachmentBucket, key, bytes.NewReader(data), att.Size, contentType); err != nil {
		s.log.ErrorContext(r.Context(), "store alarm attachment failed", "alarmId", alarmID, "error", err)
		problem(w, 502, "附件保存失败，请检查对象存储")
		return
	}
	alarm, err = s.engine.AddAlarmAttachment(r.Context(), c.TenantID, alarmID, att, c.Username)
	if err != nil {
		s.engine.DeleteObjectLater(r.Context(), model.AlarmAttachmentBucket, key)
		s.attachmentProblem(w, r, err)
		return
	}
	write(w, 200, alarm)
}

func (s *Server) findAttachment(w http.ResponseWriter, r *http.Request) (model.AlarmAttachment, bool) {
	alarm, err := s.engine.Repo.GetAlarm(r.Context(), claims(r).TenantID, r.PathValue("id"))
	if err != nil {
		s.attachmentProblem(w, r, err)
		return model.AlarmAttachment{}, false
	}
	index := slices.IndexFunc(alarm.Attachments, func(a model.AlarmAttachment) bool { return a.ID == r.PathValue("attachmentId") })
	if index < 0 {
		s.attachmentProblem(w, r, model.ErrAttachmentNotFound)
		return model.AlarmAttachment{}, false
	}
	return alarm.Attachments[index], true
}

// alarmAttachment serves an attachment to anyone who may read the alarm.
// Images may be shown inline (?inline=1); other files are always downloads.
func (s *Server) alarmAttachment(w http.ResponseWriter, r *http.Request) {
	att, ok := s.findAttachment(w, r)
	if !ok {
		return
	}
	if s.engine.Archive == nil {
		problem(w, 404, "附件文件不存在")
		return
	}
	reader, err := s.engine.Archive.GetObject(r.Context(), model.AlarmAttachmentBucket, model.AlarmAttachmentKey(claims(r).TenantID, r.PathValue("id"), att.ID))
	if err != nil {
		problem(w, 404, "附件文件不存在")
		return
	}
	defer reader.Close()
	disposition := "attachment"
	if r.URL.Query().Get("inline") == "1" && strings.HasPrefix(att.ContentType, "image/") {
		disposition = "inline"
	}
	w.Header().Set("Content-Type", att.ContentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": att.Name}))
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("ETag", `"`+att.SHA256+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	_, _ = io.Copy(w, io.LimitReader(reader, model.MaxAlarmAttachmentSize))
}

func (s *Server) deleteAlarmAttachment(w http.ResponseWriter, r *http.Request) {
	c, alarmID := claims(r), r.PathValue("id")
	att, err := s.engine.RemoveAlarmAttachment(r.Context(), c.TenantID, alarmID, r.PathValue("attachmentId"), c.Username)
	if err != nil {
		s.attachmentProblem(w, r, err)
		return
	}
	s.engine.DeleteObjectLater(r.Context(), model.AlarmAttachmentBucket, model.AlarmAttachmentKey(c.TenantID, alarmID, att.ID))
	write(w, 200, map[string]bool{"success": true})
}
