package httpapi

import (
	"encoding/json"
	"io"
	"iot-platform/internal/alarmgovernance"
	"iot-platform/internal/analytics"
	"iot-platform/internal/model"
	"net/http"
	"net/url"
)

func (s *Server) governanceEvidenceRoutes() {
	base := "/api/v1/alarm-governance"
	for _, resource := range []string{"cases", "verifications", "activities"} {
		kind := governanceResources[resource]
		s.router.GET(base+"/"+resource+"/:id/attachments", s.authorize("viewer"), s.endpoint(s.governanceAttachmentList(kind), "id"))
		s.router.POST(base+"/"+resource+"/:id/attachments", s.authorize("viewer"), s.endpoint(s.governanceUploadAttachment(kind), "id"))
	}
	s.router.GET(base+"/attachments/:id/download", s.authorize("viewer"), s.endpoint(s.governanceDownloadAttachment, "id"))
	s.router.DELETE(base+"/attachments/:id", s.authorize("viewer"), s.endpoint(s.governanceWithdrawAttachment, "id"))
	s.router.GET(base+"/candidates", s.authorize("viewer"), s.endpoint(s.governanceCandidates))
}
func (s *Server) governanceAttachmentList(kind string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.governanceReady(w) {
			return
		}
		a := governanceActor(r)
		if _, e := s.governance.Get(r.Context(), a, kind, r.PathValue("id")); e != nil {
			governanceProblem(w, e)
			return
		}
		items, n, e := s.governance.List(r.Context(), a, model.GovernanceFilter{Kind: model.GovernanceAttachmentKind, ParentID: r.PathValue("id"), Limit: 100})
		if e != nil {
			governanceProblem(w, e)
			return
		}
		for i := range items {
			v, _ := model.GovernanceBody[model.GovernanceAttachment](items[i])
			v.StorageKey = ""
			items[i].Body, _ = json.Marshal(v)
		}
		write(w, 200, map[string]any{"items": items, "total": n})
	}
}
func (s *Server) governanceUploadAttachment(kind string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.governanceReady(w) {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, alarmgovernance.AttachmentMax+(1<<20))
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			problem(w, 413, "附件最大16MiB")
			return
		}
		if r.MultipartForm != nil {
			defer r.MultipartForm.RemoveAll()
		}
		f, h, err := r.FormFile("file")
		if err != nil {
			problem(w, 422, "请选择附件")
			return
		}
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, alarmgovernance.AttachmentMax+1))
		if err != nil {
			problem(w, 422, "无法读取附件")
			return
		}
		doc, err := s.governance.UploadAttachment(r.Context(), governanceActor(r), s.engine.Archive, kind, r.PathValue("id"), h.Filename, r.FormValue("idempotencyKey"), data)
		if err != nil {
			governanceProblem(w, err)
			return
		}
		v, _ := model.GovernanceBody[model.GovernanceAttachment](doc)
		v.StorageKey = ""
		doc.Body, _ = json.Marshal(v)
		write(w, 201, doc)
	}
}
func (s *Server) governanceDownloadAttachment(w http.ResponseWriter, r *http.Request) {
	if !s.governanceReady(w) {
		return
	}
	v, data, err := s.governance.DownloadAttachment(r.Context(), governanceActor(r), s.engine.Archive, r.PathValue("id"))
	if err != nil {
		governanceProblem(w, err)
		return
	}
	w.Header().Set("Content-Type", v.MIME)
	w.Header().Set("Content-Disposition", `attachment; filename*=UTF-8''`+url.PathEscape(v.Name))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(200)
	_, _ = w.Write(data)
}
func (s *Server) governanceWithdrawAttachment(w http.ResponseWriter, r *http.Request) {
	if !s.governanceReady(w) {
		return
	}
	var q struct {
		ExpectedVersion int64  `json:"expectedVersion"`
		Reason          string `json:"reason"`
	}
	if decode(w, r, &q) != nil {
		return
	}
	d, e := s.governance.WithdrawAttachment(r.Context(), governanceActor(r), r.PathValue("id"), q.ExpectedVersion, q.Reason)
	if e != nil {
		governanceProblem(w, e)
		return
	}
	v, _ := model.GovernanceBody[model.GovernanceAttachment](d)
	v.StorageKey = ""
	d.Body, _ = json.Marshal(v)
	write(w, 200, d)
}
func (s *Server) governanceCandidates(w http.ResponseWriter, r *http.Request) {
	run := r.URL.Query().Get("runId")
	if run == "" {
		problem(w, 422, "请选择已完成分析任务")
		return
	}
	f := analysisPage(r)
	f.Kind = "findings"
	v, n, e := s.analysis.Outputs(r.Context(), analysisActor(r), analytics.KindRecurring, run, f)
	if e != nil {
		analysisProblem(w, e)
		return
	}
	write(w, 200, map[string]any{"items": v, "total": n})
}
