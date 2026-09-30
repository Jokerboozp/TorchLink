package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"iot-platform/internal/analytics/dataquality"
	"iot-platform/internal/model"
)

func (s *Server) dataQualityRoutes() {
	for _, resource := range []string{"profiles", "baselines", "calibrations"} {
		base := "/api/v1/data-quality/" + resource
		s.router.GET(base, s.authorize("viewer"), s.endpoint(s.dataQualityConfigs(resource)))
		s.router.POST(base, s.authorize("viewer"), s.endpoint(s.dataQualitySave(resource)))
	}
	s.router.POST("/api/v1/data-quality/profiles/publish", s.authorize("viewer"), s.endpoint(s.dataQualitySave("profiles")))
	s.router.POST("/api/v1/data-quality/baselines/:id/confirm", s.authorize("viewer"), s.endpoint(s.dataQualityConfirmBaseline, "id"))
	s.router.POST("/api/v1/data-quality/findings/:id/reviews", s.authorize("viewer"), s.endpoint(s.dataQualityReview, "id"))
	s.router.GET("/api/v1/data-quality/findings/:id/reviews", s.authorize("viewer"), s.endpoint(s.dataQualityReviews, "id"))
	s.router.GET("/api/v1/data-quality/runs/:id/reviews", s.authorize("viewer"), s.endpoint(s.dataQualityRunReviews, "id"))
	s.router.GET("/api/v1/data-quality/runs/:id/series", s.authorize("viewer"), s.endpoint(s.dataQualitySeries, "id"))
	s.router.GET("/api/v1/data-quality/runs/:id/export", s.authorize("viewer"), s.endpoint(s.dataQualityExport, "id"))
	s.router.POST("/api/v1/data-quality/calibrations/attachments", s.authorize("viewer"), s.endpoint(s.dataQualityUpload))
	s.router.GET("/api/v1/data-quality/calibrations/attachments/:id", s.authorize("viewer"), s.endpoint(s.dataQualityAttachment, "id"))
}
func (s *Server) dataQualityConfigs(resource string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		items, total, err := s.quality.ListConfigs(r.Context(), analysisActor(r), resource, analysisPage(r))
		if err != nil {
			analysisProblem(w, err)
			return
		}
		write(w, 200, map[string]any{"items": items, "total": total})
	}
}
func (s *Server) dataQualitySave(resource string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		var q model.QualityConfigRequest
		if decode(w, r, &q) != nil {
			return
		}
		if strings.HasSuffix(r.URL.Path, "/publish") {
			q.Scope = "shared"
		}
		var revision model.AnalysisConfigRevision
		var err error
		switch resource {
		case "profiles":
			revision, err = s.quality.SaveProfile(r.Context(), analysisActor(r), q)
		case "baselines":
			revision, err = s.quality.BuildBaseline(r.Context(), analysisActor(r), q)
		case "calibrations":
			revision, err = s.quality.SaveCalibration(r.Context(), analysisActor(r), q)
		}
		if err != nil {
			analysisProblem(w, err)
			return
		}
		write(w, 201, revision)
	}
}
func (s *Server) dataQualityConfirmBaseline(w http.ResponseWriter, r *http.Request) {
	var q struct {
		ExpectedVersion int64 `json:"expectedVersion"`
	}
	if decode(w, r, &q) != nil {
		return
	}
	revision, err := s.quality.ConfirmBaseline(r.Context(), analysisActor(r), r.PathValue("id"), q.ExpectedVersion)
	if err != nil {
		analysisProblem(w, err)
		return
	}
	write(w, 201, revision)
}
func (s *Server) dataQualityReview(w http.ResponseWriter, r *http.Request) {
	var q model.QualityReviewRequest
	if decode(w, r, &q) != nil {
		return
	}
	revision, err := s.quality.Review(r.Context(), analysisActor(r), r.PathValue("id"), q)
	if err != nil {
		analysisProblem(w, err)
		return
	}
	write(w, 201, revision)
}
func (s *Server) dataQualityReviews(w http.ResponseWriter, r *http.Request) {
	items, total, err := s.quality.Reviews(r.Context(), analysisActor(r), r.URL.Query().Get("runId"), r.PathValue("id"), analysisPage(r))
	if err != nil {
		analysisProblem(w, err)
		return
	}
	write(w, 200, map[string]any{"items": items, "total": total})
}
func (s *Server) dataQualityRunReviews(w http.ResponseWriter, r *http.Request) {
	items, total, err := s.quality.Reviews(r.Context(), analysisActor(r), r.PathValue("id"), "", analysisPage(r))
	if err != nil {
		analysisProblem(w, err)
		return
	}
	write(w, 200, map[string]any{"items": items, "total": total})
}
func (s *Server) dataQualitySeries(w http.ResponseWriter, r *http.Request) {
	data, err := s.quality.Series(r.Context(), analysisActor(r), r.PathValue("id"), r.URL.Query().Get("deviceId"), r.URL.Query().Get("attributeId"), intval(r.URL.Query().Get("limit"), 1000))
	if err != nil {
		analysisProblem(w, err)
		return
	}
	write(w, 200, data)
}
func (s *Server) dataQualityExport(w http.ResponseWriter, r *http.Request) {
	data, err := s.quality.Export(r.Context(), analysisActor(r), r.PathValue("id"))
	if err != nil {
		analysisProblem(w, err)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="data-quality-`+r.PathValue("id")+`.json"`)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write(data)
}
func (s *Server) dataQualityUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, dataquality.AttachmentMax+(1<<20))
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		problem(w, 413, "附件最大16MiB")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	var devices []string
	if json.Unmarshal([]byte(r.FormValue("deviceIdsJSON")), &devices) != nil {
		problem(w, 422, "deviceIdsJSON须为明确设备集合")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		problem(w, 422, "请选择附件")
		return
	}
	defer file.Close()
	name := filepath.Base(strings.ReplaceAll(header.Filename, "\\", "/"))
	name = strings.Map(func(c rune) rune {
		if c < 32 || c == 127 {
			return -1
		}
		return c
	}, name)
	if name == "" || name == "." || len(name) > 240 {
		problem(w, 422, "附件文件名无效")
		return
	}
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	attachment, err := s.quality.UploadAttachment(r.Context(), analysisActor(r), devices, r.FormValue("scope"), name, contentType, header.Size, file)
	if err != nil {
		analysisProblem(w, err)
		return
	}
	write(w, 201, attachment)
}
func (s *Server) dataQualityAttachment(w http.ResponseWriter, r *http.Request) {
	attachment, reader, err := s.quality.DownloadAttachment(r.Context(), analysisActor(r), r.PathValue("id"))
	if err != nil {
		analysisProblem(w, err)
		return
	}
	defer reader.Close()
	w.Header().Set("Content-Type", attachment.ContentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename*=UTF-8''%s", url.PathEscape(attachment.Name)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(200)
	_, _ = io.Copy(w, reader)
}
