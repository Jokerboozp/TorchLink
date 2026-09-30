package httpapi

import (
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"iot-platform/internal/core"
	"iot-platform/internal/duty"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

const dutyAttachmentBucket = "iot-duty-attachments"
const dutyAttachmentMax = 16 << 20

func (s *Server) dutyExportRoutes() {
	s.router.POST("/api/v1/duty/runs/:id/attachments", s.authorize("viewer"), s.endpoint(s.dutyUploadAttachment, "id"))
	s.router.GET("/api/v1/duty/attachments/:id", s.authorize("viewer"), s.endpoint(s.dutyDownloadAttachment, "id"))
	s.router.GET("/api/v1/duty/revisions/:id/pdf", s.authorize("viewer"), s.endpoint(s.dutyPDF, "id"))
	s.router.GET("/api/v1/duty/revisions/:id/events", s.authorize("viewer"), s.endpoint(s.dutyRevisionEvents, "id"))
	s.router.GET("/api/v1/duty/revisions/:id/events.csv", s.authorize("viewer"), s.endpoint(s.dutyEventsCSV, "id"))
}

func (s *Server) dutyUploadAttachment(w http.ResponseWriter, r *http.Request) {
	svc, err := s.dutyService(r.Context())
	if err != nil {
		dutyProblem(w, err)
		return
	}
	a := s.dutyActor(r)
	doc, err := svc.Get(r.Context(), a, model.DutyRunKind, r.PathValue("id"))
	if err != nil {
		dutyProblem(w, err)
		return
	}
	run, err := model.DutyBody[model.DutyRun](doc)
	if err != nil {
		dutyProblem(w, err)
		return
	}
	if run.Status != "ACTIVE" || !slices.Contains(run.MemberIDs, a.Username) {
		dutyProblem(w, duty.ErrForbidden)
		return
	}
	if s.engine.Archive == nil {
		problem(w, 503, "附件存储不可用")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, dutyAttachmentMax+(1<<20))
	if err = r.ParseMultipartForm(1 << 20); err != nil {
		problem(w, 413, "附件最大16MiB")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, h, err := r.FormFile("file")
	if err != nil {
		problem(w, 422, "请选择附件文件")
		return
	}
	defer file.Close()
	if h.Size <= 0 || h.Size > dutyAttachmentMax {
		problem(w, 413, "附件大小须在1字节至16MiB之间")
		return
	}
	name := filepath.Base(strings.ReplaceAll(h.Filename, "\\", "/"))
	name = strings.Map(func(c rune) rune {
		if c < 32 || c == 127 {
			return -1
		}
		return c
	}, name)
	if name == "." || name == "" || len(name) > 240 {
		problem(w, 422, "附件文件名无效")
		return
	}
	id := "duty_attachment_" + randomHex(16)
	key := a.TenantID + "/" + doc.ID + "/" + id
	contentType := h.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if _, err = s.engine.Archive.PutObject(r.Context(), dutyAttachmentBucket, key, file, h.Size, contentType); err != nil {
		problem(w, 503, "附件保存失败")
		return
	}
	attachment := model.DutyAttachment{ID: id, Name: name, ObjectKey: key, ContentType: contentType, Size: h.Size}
	err = svc.Store.DutyTransaction(r.Context(), a.TenantID, func(tx ports.DutyTx) error {
		latest, e := tx.Get(model.DutyRunKind, doc.ID)
		if e != nil {
			return e
		}
		v, e := model.DutyBody[model.DutyRun](latest)
		if e != nil {
			return e
		}
		if v.Status != "ACTIVE" || !slices.Contains(v.MemberIDs, a.Username) {
			return duty.ErrConflict
		}
		_, e = tx.Put(model.NewDutyDocument(model.DutyAttachmentKind, id, model.DutyAttachmentRecord{StationID: run.StationID, RunID: doc.ID, AuthorID: a.Username, Attachment: attachment}), 0)
		return e
	})
	if err != nil {
		if del, ok := s.engine.Archive.(ports.ObjectDeleter); ok {
			_ = del.DeleteObject(r.Context(), dutyAttachmentBucket, key)
		}
		dutyProblem(w, err)
		return
	}
	write(w, 201, attachment)
}

func (s *Server) dutyDownloadAttachment(w http.ResponseWriter, r *http.Request) {
	svc, err := s.dutyService(r.Context())
	if err != nil {
		dutyProblem(w, err)
		return
	}
	doc, err := svc.Get(r.Context(), s.dutyActor(r), model.DutyAttachmentKind, r.PathValue("id"))
	if err != nil {
		dutyProblem(w, err)
		return
	}
	v, err := model.DutyBody[model.DutyAttachmentRecord](doc)
	if err != nil {
		dutyProblem(w, err)
		return
	}
	if s.engine.Archive == nil {
		problem(w, 503, "附件存储不可用")
		return
	}
	reader, err := s.engine.Archive.GetObject(r.Context(), dutyAttachmentBucket, v.Attachment.ObjectKey)
	if err != nil {
		problem(w, 404, "附件原件不可用")
		return
	}
	defer reader.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename*=UTF-8''%s", url.PathEscape(v.Attachment.Name)))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.Copy(w, reader)
}

func (s *Server) dutyExportRevision(r *http.Request) (model.DutyDocument, model.DutyHandoverRevision, error) {
	svc, err := s.dutyService(r.Context())
	if err != nil {
		return model.DutyDocument{}, model.DutyHandoverRevision{}, err
	}
	doc, err := svc.Get(r.Context(), s.dutyActor(r), model.DutyRevisionKind, r.PathValue("id"))
	if err != nil {
		return doc, model.DutyHandoverRevision{}, err
	}
	v, err := model.DutyBody[model.DutyHandoverRevision](doc)
	return doc, v, err
}
func (s *Server) dutyRevisionEvents(w http.ResponseWriter, r *http.Request) {
	_, v, err := s.dutyExportRevision(r)
	if err != nil {
		dutyProblem(w, err)
		return
	}
	limit, offset := dutyEventPage(r)
	write(w, 200, map[string]any{"items": pageSlice(v.Events, limit, offset), "total": len(v.Events)})
}
func dutyEventPage(r *http.Request) (int, int) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}
func (s *Server) dutyEventsCSV(w http.ResponseWriter, r *http.Request) {
	doc, v, err := s.dutyExportRevision(r)
	if err != nil {
		dutyProblem(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"handover-%s-events.csv\"", doc.ID))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, "\ufeff")
	writer := csv.NewWriter(w)
	defer writer.Flush()
	_ = writer.Write([]string{"事件ID", "类型", "设备ID", "告警ID", "操作人", "发生时间", "记录时间", "内容"})
	for _, ev := range v.Events {
		row := []string{ev.ID, ev.Type, ev.DeviceID, ev.AlarmID, ev.ActorID, strconv.FormatInt(ev.OccurredAt, 10), strconv.FormatInt(ev.RecordedAt, 10), string(ev.Body)}
		for i, value := range row {
			row[i] = safeDutyCSV(value)
		}
		if writer.Write(row) != nil {
			return
		}
	}
}
func safeDutyCSV(s string) string {
	if strings.ContainsAny(strings.TrimLeft(s, " \t\r\n")[:min(len(strings.TrimLeft(s, " \t\r\n")), 1)], "=+-@") || strings.HasPrefix(s, "\t") || strings.HasPrefix(s, "\r") {
		return "'" + s
	}
	return s
}

func (s *Server) dutyPDF(w http.ResponseWriter, r *http.Request) {
	doc, v, err := s.dutyExportRevision(r)
	if err != nil {
		dutyProblem(w, err)
		return
	}
	svc, err := s.dutyService(r.Context())
	if err != nil {
		dutyProblem(w, err)
		return
	}
	hdoc, err := svc.Get(r.Context(), s.dutyActor(r), model.DutyHandoverKind, v.HandoverID)
	if err != nil {
		dutyProblem(w, err)
		return
	}
	h, _ := model.DutyBody[model.DutyHandover](hdoc)
	data, err := core.RenderDutyHandoverPDF(doc, v, h)
	if err != nil {
		dutyProblem(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"handover-%s-v%d.pdf\"", v.HandoverID, v.Number))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}
