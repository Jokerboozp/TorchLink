package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"iot-platform/internal/analytics"
	"iot-platform/internal/analytics/maintenance"
	"iot-platform/internal/duty"
	"iot-platform/internal/model"
	"net/http"
	"net/url"
	"strings"
)

func (s *Server) maintenanceService() *maintenance.Service {
	svc := maintenance.NewService(s.analysis, s.analysisFacts)
	svc.Catalog = s.unscopedRepo()
	svc.Archive = s.engine.Archive
	svc.RecordLimit = s.analysis.Limits.RecordLimit
	response := s.responseService()
	response.EvidenceKind = analytics.KindMaintenance
	svc.ResolveEvidence = response.BusinessEvidence
	svc.Corrective = response
	if s.analysis.AI != nil {
		svc.AI = s.analysis.AI
	}
	return svc
}
func (s *Server) maintenanceRoutes() {
	// Resolve current storage at execution time; test storage replacement and
	// runtime reload cannot leave a registered worker holding the old reader.
	if err := s.analysis.Register(analytics.KindMaintenance, func(ctx context.Context, e *analytics.Execution) error {
		return s.maintenanceService().ProcessObservation(ctx, e)
	}); err != nil {
		panic(err)
	}
	if err := s.analysis.Register(analytics.KindInvestment, func(ctx context.Context, e *analytics.Execution) error {
		return s.maintenanceService().ProcessInvestment(ctx, e)
	}); err != nil {
		panic(err)
	}
	for kind, path := range map[string]string{maintenance.AssetKind: "assets", maintenance.InterventionKind: "maintenance-records", maintenance.ContextKind: "maintenance-contexts", maintenance.AdmissionKind: "maintenance-admissions", maintenance.FaultAssessmentKind: "maintenance-fault-cycles", maintenance.CostKind: "maintenance-costs", maintenance.ScenarioKind: "investment-scenarios"} {
		base := "/api/v1/" + path
		s.router.GET(base, s.authorize("viewer"), s.endpoint(s.maintenanceList(kind)))
		s.router.GET(base+"/:id", s.authorize("viewer"), s.endpoint(s.maintenanceGet(kind), "id"))
		s.router.POST(base, s.authorize("viewer"), s.endpoint(s.maintenanceSave(kind)))
	}
	s.router.GET("/api/v1/maintenance-revisions/:id", s.authorize("viewer"), s.endpoint(s.maintenanceRevision, "id"))
	s.router.POST("/api/v1/maintenance-records/:id/actions", s.authorize("viewer"), s.endpoint(s.maintenanceAction, "id"))
	s.router.POST("/api/v1/maintenance-records/:id/verifications", s.authorize("viewer"), s.endpoint(s.maintenanceVerification, "id"))
	s.router.POST("/api/v1/maintenance-records/:id/observations", s.authorize("viewer"), s.endpoint(s.maintenanceObserve, "id"))
	s.router.GET("/api/v1/maintenance-records/:id/observations", s.authorize("viewer"), s.endpoint(s.maintenanceRelated(analytics.KindMaintenance), "id"))
	s.router.POST("/api/v1/maintenance-fault-cycles/:id/confirm", s.authorize("viewer"), s.endpoint(s.maintenanceConfirmFault, "id"))
	s.router.POST("/api/v1/investment-scenarios/:id/evaluate", s.authorize("viewer"), s.endpoint(s.investmentEvaluate, "id"))
	s.router.GET("/api/v1/investment-scenarios/:id/evaluations", s.authorize("viewer"), s.endpoint(s.maintenanceRelated(analytics.KindInvestment), "id"))
	s.router.POST("/api/v1/investment-scenarios/:id/adjustments", s.authorize("viewer"), s.endpoint(s.investmentAdjust, "id"))
	s.router.POST("/api/v1/investment-scenarios/:id/decisions", s.authorize("viewer"), s.endpoint(s.investmentDecide, "id"))
	s.router.POST("/api/v1/investment-scenarios/:id/actions", s.authorize("viewer"), s.endpoint(s.investmentAction, "id"))
	for _, kind := range []string{analytics.KindMaintenance, analytics.KindInvestment} {
		base := analytics.RunCollection(kind)
		s.router.GET(base, s.authorize("viewer"), s.endpoint(s.analysisRuns(kind)))
		s.router.GET(base+"/:id", s.authorize("viewer"), s.endpoint(s.analysisRun(kind), "id"))
		s.router.GET(base+"/:id/snapshot", s.authorize("viewer"), s.endpoint(s.analysisSnapshot(kind), "id"))
		s.router.POST(base+"/:id/stop", s.authorize("viewer"), s.endpoint(s.analysisStop(kind), "id"))
		s.router.GET(base+"/:id/evidence", s.authorize("viewer"), s.endpoint(s.analysisEvidence(kind), "id"))
		s.router.GET(base+"/:id/export", s.authorize("viewer"), s.endpoint(s.maintenanceExport(kind), "id"))
		s.router.GET(base+"/:id/reviews", s.authorize("viewer"), s.endpoint(s.maintenanceReviews(kind), "id"))
		s.router.POST(base+"/:id/reviews", s.authorize("viewer"), s.endpoint(s.maintenanceReview(kind), "id"))
		collections := []string{"observations", "change-metrics", "findings"}
		if kind == analytics.KindInvestment {
			collections = []string{"investment-priorities", "budget-lines", "findings"}
		}
		for _, collection := range collections {
			s.router.GET(base+"/:id/"+collection, s.authorize("viewer"), s.endpoint(s.analysisOutputs(kind, collection), "id"))
		}
	}
	s.router.POST("/api/v1/maintenance-attachments", s.authorize("viewer"), s.endpoint(s.maintenanceUpload))
	s.router.GET("/api/v1/maintenance-attachments/:id", s.authorize("viewer"), s.endpoint(s.maintenanceDownload, "id"))
	s.router.GET("/api/v1/maintenance-records/:id/duty-links", s.authorize("viewer"), s.endpoint(s.maintenanceDutyLinks, "id"))
	s.router.POST("/api/v1/maintenance-records/:id/duty-links", s.authorize("viewer"), s.endpoint(s.maintenanceDutyLink, "id"))
}
func maintenanceWrite(w http.ResponseWriter, status int, v model.AnalysisConfigRevision, err error) {
	if err != nil {
		analysisProblem(w, err)
		return
	}
	write(w, status, publicResponseConfig(v))
}
func (s *Server) maintenanceList(kind string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		items, total, err := s.maintenanceService().List(r.Context(), analysisActor(r), kind, analysisPage(r))
		if err != nil {
			analysisProblem(w, err)
			return
		}
		for i := range items {
			items[i] = publicResponseConfig(items[i])
		}
		write(w, 200, map[string]any{"items": items, "total": total})
	}
}
func (s *Server) maintenanceGet(kind string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		v, err := s.maintenanceService().Latest(r.Context(), analysisActor(r), kind, r.PathValue("id"))
		maintenanceWrite(w, 200, v, err)
	}
}
func (s *Server) maintenanceRevision(w http.ResponseWriter, r *http.Request) {
	v, err := s.maintenanceService().Revision(r.Context(), analysisActor(r), r.URL.Query().Get("kind"), r.PathValue("id"))
	maintenanceWrite(w, 200, v, err)
}
func (s *Server) maintenanceSave(kind string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		var q model.MaintenanceRevisionRequest
		if decode(w, r, &q) != nil {
			return
		}
		svc := s.maintenanceService()
		var v model.AnalysisConfigRevision
		var err error
		switch kind {
		case maintenance.AssetKind:
			v, err = svc.SaveAsset(r.Context(), analysisActor(r), q)
		case maintenance.InterventionKind:
			v, err = svc.SaveIntervention(r.Context(), analysisActor(r), q)
		case maintenance.ContextKind:
			v, err = svc.SaveContext(r.Context(), analysisActor(r), q)
		case maintenance.AdmissionKind:
			v, err = svc.SaveAdmission(r.Context(), analysisActor(r), q)
		case maintenance.FaultAssessmentKind:
			v, err = svc.SaveFaultAssessment(r.Context(), analysisActor(r), q)
		case maintenance.CostKind:
			v, err = svc.SaveCost(r.Context(), analysisActor(r), q)
		case maintenance.ScenarioKind:
			v, err = svc.SaveScenario(r.Context(), analysisActor(r), q)
		}
		maintenanceWrite(w, 201, v, err)
	}
}
func (s *Server) maintenanceAction(w http.ResponseWriter, r *http.Request) {
	var q model.MaintenanceActionRequest
	if decode(w, r, &q) != nil {
		return
	}
	v, err := s.maintenanceService().InterventionAction(r.Context(), analysisActor(r), r.PathValue("id"), q)
	maintenanceWrite(w, 201, v, err)
}
func (s *Server) maintenanceVerification(w http.ResponseWriter, r *http.Request) {
	var q model.MaintenanceVerificationRequest
	if decode(w, r, &q) != nil {
		return
	}
	v, err := s.maintenanceService().VerifyIntervention(r.Context(), analysisActor(r), r.PathValue("id"), q)
	maintenanceWrite(w, 201, v, err)
}
func (s *Server) maintenanceConfirmFault(w http.ResponseWriter, r *http.Request) {
	var q model.MaintenanceActionRequest
	if decode(w, r, &q) != nil {
		return
	}
	v, err := s.maintenanceService().ConfirmFault(r.Context(), analysisActor(r), r.PathValue("id"), q)
	maintenanceWrite(w, 201, v, err)
}
func (s *Server) maintenanceObserve(w http.ResponseWriter, r *http.Request) {
	var q model.MaintenanceObservationRequest
	if decode(w, r, &q) != nil {
		return
	}
	v, err := s.maintenanceService().CreateObservation(r.Context(), analysisActor(r), r.PathValue("id"), q)
	if err != nil {
		analysisProblem(w, err)
		return
	}
	write(w, 202, v)
}
func (s *Server) investmentEvaluate(w http.ResponseWriter, r *http.Request) {
	var q model.InvestmentEvaluationRequest
	if decode(w, r, &q) != nil {
		return
	}
	v, err := s.maintenanceService().EvaluateScenario(r.Context(), analysisActor(r), r.PathValue("id"), q)
	if err != nil {
		analysisProblem(w, err)
		return
	}
	write(w, 202, v)
}
func (s *Server) investmentAdjust(w http.ResponseWriter, r *http.Request) {
	var q model.InvestmentAdjustmentRequest
	if decode(w, r, &q) != nil {
		return
	}
	v, err := s.maintenanceService().AdjustScenario(r.Context(), analysisActor(r), r.PathValue("id"), q)
	maintenanceWrite(w, 201, v, err)
}
func (s *Server) investmentDecide(w http.ResponseWriter, r *http.Request) {
	var q model.InvestmentDecisionRequest
	if decode(w, r, &q) != nil {
		return
	}
	v, err := s.maintenanceService().DecideScenario(r.Context(), analysisActor(r), r.PathValue("id"), q)
	maintenanceWrite(w, 201, v, err)
}
func (s *Server) investmentAction(w http.ResponseWriter, r *http.Request) {
	var q model.MaintenanceActionRequest
	if decode(w, r, &q) != nil {
		return
	}
	v, err := s.maintenanceService().ScenarioAction(r.Context(), analysisActor(r), r.PathValue("id"), q)
	maintenanceWrite(w, 201, v, err)
}
func (s *Server) maintenanceRelated(kind string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		items, total, err := s.maintenanceService().RelatedRuns(r.Context(), analysisActor(r), kind, r.PathValue("id"), analysisPage(r))
		if err != nil {
			analysisProblem(w, err)
			return
		}
		write(w, 200, map[string]any{"items": items, "total": total})
	}
}
func (s *Server) maintenanceReviews(kind string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		items, total, err := s.maintenanceService().Reviews(r.Context(), analysisActor(r), kind, r.PathValue("id"), analysisPage(r))
		if err != nil {
			analysisProblem(w, err)
			return
		}
		write(w, 200, map[string]any{"items": items, "total": total, "statusPolicy": "人工核实独立，固定指标不变"})
	}
}
func (s *Server) maintenanceReview(kind string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		var q model.MaintenanceReviewRequest
		if decode(w, r, &q) != nil {
			return
		}
		v, err := s.maintenanceService().Review(r.Context(), analysisActor(r), kind, r.PathValue("id"), q)
		if err != nil {
			analysisProblem(w, err)
			return
		}
		write(w, 201, v)
	}
}
func (s *Server) maintenanceExport(kind string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := s.maintenanceService().Export(r.Context(), analysisActor(r), kind, r.PathValue("id"))
		if err != nil {
			analysisProblem(w, err)
			return
		}
		w.Header().Set("Content-Disposition", "attachment; filename=\"maintenance-"+url.PathEscape(r.PathValue("id"))+".json\"")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(200)
		_, _ = w.Write(body)
	}
}
func (s *Server) maintenanceUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maintenance.AttachmentMaxBytes+(1<<20))
	if r.ParseMultipartForm(1<<20) != nil {
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
	data, err := io.ReadAll(io.LimitReader(file, maintenance.AttachmentMaxBytes+1))
	if err != nil {
		analysisProblem(w, err)
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		key = r.FormValue("idempotencyKey")
	}
	v, err := s.maintenanceService().UploadAttachment(r.Context(), analysisActor(r), devices, key, header.Filename, header.Header.Get("Content-Type"), data)
	maintenanceWrite(w, 201, v, err)
}
func (s *Server) maintenanceDownload(w http.ResponseWriter, r *http.Request) {
	v, reader, err := s.maintenanceService().DownloadAttachment(r.Context(), analysisActor(r), r.PathValue("id"))
	if err != nil {
		analysisProblem(w, err)
		return
	}
	defer reader.Close()
	w.Header().Set("Content-Type", v.ContentType)
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(v.Name))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(200)
	_, _ = io.Copy(w, reader)
}
func (s *Server) maintenanceDutyLink(w http.ResponseWriter, r *http.Request) {
	var q struct {
		ExpectedVersion int64  `json:"expectedVersion"`
		RunID           string `json:"runId"`
		OwnerID         string `json:"ownerId"`
		NextAction      string `json:"nextAction"`
	}
	if decode(w, r, &q) != nil {
		return
	}
	v, err := s.maintenanceService().Latest(r.Context(), analysisActor(r), maintenance.InterventionKind, r.PathValue("id"))
	if err != nil {
		analysisProblem(w, err)
		return
	}
	current, err := s.analysis.Current(r.Context(), analysisActor(r))
	if err != nil || !current.Allows(analytics.KindMaintenance, "POST /api/v1/maintenance-records/:id/duty-links", v.DeviceIDs) {
		analysisProblem(w, analytics.ErrForbidden)
		return
	}
	if v.Version != q.ExpectedVersion {
		analysisProblem(w, model.ErrAnalysisConflict)
		return
	}
	var work model.MaintenanceIntervention
	json.Unmarshal(v.Body, &work)
	if work.Status == "CANCELLED" {
		analysisProblem(w, model.ErrAnalysisConflict)
		return
	}
	svc, err := s.dutyService(r.Context())
	if err != nil {
		dutyProblem(w, err)
		return
	}
	link, err := svc.LinkFollowUp(r.Context(), s.dutyActor(r), duty.FollowUpSource{Kind: maintenance.InterventionKind, ID: v.ResourceID, Version: v.Version, DeviceIDs: v.DeviceIDs, Title: work.Name}, q.RunID, q.OwnerID, strings.TrimSpace(q.NextAction))
	if err != nil {
		dutyProblem(w, err)
		return
	}
	write(w, 201, link)
}
func (s *Server) maintenanceDutyLinks(w http.ResponseWriter, r *http.Request) {
	v, err := s.maintenanceService().Latest(r.Context(), analysisActor(r), maintenance.InterventionKind, r.PathValue("id"))
	if err != nil {
		analysisProblem(w, err)
		return
	}
	svc, err := s.dutyService(r.Context())
	if err != nil {
		dutyProblem(w, err)
		return
	}
	links, err := svc.FollowUpLinks(r.Context(), s.dutyActor(r), maintenance.InterventionKind, v.ResourceID)
	if err != nil {
		dutyProblem(w, err)
		return
	}
	var work model.MaintenanceIntervention
	json.Unmarshal(v.Body, &work)
	write(w, 200, map[string]any{"items": links, "sourceVersion": v.Version, "sourceStatus": work.Status, "completionPolicy": "当班跟进完成不替代维修功能验收"})
}
