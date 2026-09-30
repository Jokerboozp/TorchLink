package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"iot-platform/internal/analytics"
	"iot-platform/internal/analytics/response"
	"iot-platform/internal/duty"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func (s *Server) responseService() *response.Service {
	svc := response.NewService(s.analysis, s.analysisFacts)
	svc.Catalog, svc.Archive = s.unscopedRepo(), s.engine.Archive
	svc.ValidateStaff = s.validateResponseStaff
	svc.ResolveEvidence = svc.BusinessEvidence
	if s.analysis.AI != nil {
		svc.AI = s.analysis.AI
	}
	return svc
}

func (s *Server) validateResponseStaff(ctx context.Context, tenant, username string, devices []string) error {
	if username == s.cfg.AdminUser && adminTenantAllowed(s.cfg.AdminTenants, tenant) {
		return nil
	}
	store, ok := s.unscopedRepo().(ports.AccessStore)
	if !ok {
		return analytics.ErrUnsupported
	}
	state, err := store.LoadAccessState(ctx, tenant)
	if err != nil {
		return err
	}
	for _, user := range state.Users {
		if user.Username != username || !user.Enabled {
			continue
		}
		user = resolveUserDeviceScope(state, user)
		p := effectivePermissions(state, user)
		if !p["menu:response"] {
			return analytics.ErrForbidden
		}
		scope := scopeFor(user, p, tenant)
		for _, id := range devices {
			if !scope.All && !scope.IDs[id] {
				return analytics.ErrForbidden
			}
		}
		return nil
	}
	return analytics.ErrForbidden
}

func (s *Server) responseRoutes() {
	s.responseLookupRoutes()
	s.followUpRoutes()
	if err := s.responseService().Register(); err != nil {
		panic(err)
	}
	for path, kind := range map[string]string{"response-procedures": response.ProcedureKind, "drills": response.ExecutionKind, "response-cases": response.ExecutionKind, "corrective-actions": response.CorrectiveKind} {
		base := "/api/v1/" + path
		s.router.GET(base, s.authorize("viewer"), s.endpoint(s.responseList(kind, path)))
		s.router.GET(base+"/:id", s.authorize("viewer"), s.endpoint(s.responseGet(kind), "id"))
		s.router.POST(base, s.authorize("viewer"), s.endpoint(s.responseSave(path)))
	}
	s.router.GET("/api/v1/response-runs/:id", s.authorize("viewer"), s.endpoint(s.responseGet(response.ExecutionKind), "id"))
	s.router.PUT("/api/v1/drills/:id", s.authorize("viewer"), s.endpoint(s.responseUpdatePlan, "id"))
	s.router.GET("/api/v1/response-revisions/:id", s.authorize("viewer"), s.endpoint(s.responseRevision, "id"))
	s.router.POST("/api/v1/response-procedures/:id/publish", s.authorize("viewer"), s.endpoint(s.responsePublish, "id"))
	for _, prefix := range []string{"drills", "response-runs"} {
		s.router.POST("/api/v1/"+prefix+"/:id/actions", s.authorize("viewer"), s.endpoint(s.responseAction, "id"))
	}
	s.router.POST("/api/v1/response-runs/:id/milestones", s.authorize("viewer"), s.endpoint(s.responseMilestone(false), "id"))
	s.router.POST("/api/v1/response-runs/:id/corrections", s.authorize("viewer"), s.endpoint(s.responseMilestone(true), "id"))
	s.router.POST("/api/v1/response-runs/:id/system-milestones", s.authorize("viewer"), s.endpoint(s.responseSystemMilestone, "id"))
	s.router.POST("/api/v1/drills/:id/simulation-events", s.authorize("viewer"), s.endpoint(s.responseSimulation, "id"))
	s.router.POST("/api/v1/response-runs/:id/sample-associations", s.authorize("viewer"), s.endpoint(s.responseSampleAssociation, "id"))
	s.router.POST("/api/v1/response-runs/:id/evaluations", s.authorize("viewer"), s.endpoint(s.responseEvaluation, "id"))
	s.router.POST("/api/v1/response-runs/:id/reviews/:revisionId/confirm", s.authorize("viewer"), s.endpoint(s.responseConfirm, "id", "revisionId"))
	s.router.POST("/api/v1/response-runs/:id/improvements", s.authorize("viewer"), s.endpoint(s.responseImprovement, "id"))
	s.router.POST("/api/v1/corrective-actions/:id/actions", s.authorize("viewer"), s.endpoint(s.correctiveAction, "id"))
	s.router.POST("/api/v1/corrective-actions/:id/verifications", s.authorize("viewer"), s.endpoint(s.correctiveVerification, "id"))
	s.router.POST("/api/v1/corrective-actions/:id/duty-links", s.authorize("viewer"), s.endpoint(s.correctiveDutyLink, "id"))
	s.router.GET("/api/v1/corrective-actions/:id/duty-links", s.authorize("viewer"), s.endpoint(s.correctiveDutyLinks, "id"))
	s.router.POST("/api/v1/response-runs/:id/attachments", s.authorize("viewer"), s.endpoint(s.responseUpload, "id"))
	s.router.GET("/api/v1/response-attachments/:id", s.authorize("viewer"), s.endpoint(s.responseDownload, "id"))
	s.router.GET("/api/v1/response-evaluations/:id/export", s.authorize("viewer"), s.endpoint(s.responseExport, "id"))
	base := "/api/v1/response-evaluations"
	s.router.GET(base, s.authorize("viewer"), s.endpoint(s.analysisRuns(analytics.KindResponse)))
	s.router.GET(base+"/:id", s.authorize("viewer"), s.endpoint(s.analysisRun(analytics.KindResponse), "id"))
	s.router.GET(base+"/:id/snapshot", s.authorize("viewer"), s.endpoint(s.analysisSnapshot(analytics.KindResponse), "id"))
	s.router.POST(base+"/:id/stop", s.authorize("viewer"), s.endpoint(s.analysisStop(analytics.KindResponse), "id"))
	for _, collection := range []string{"metrics", "findings"} {
		s.router.GET(base+"/:id/"+collection, s.authorize("viewer"), s.endpoint(s.analysisOutputs(analytics.KindResponse, collection), "id"))
	}
	s.router.GET(base+"/:id/evidence", s.authorize("viewer"), s.endpoint(s.analysisEvidence(analytics.KindResponse), "id"))
	// AI routes are registered only after the response workflow is available.
}

func publicResponseConfig(v model.AnalysisConfigRevision) model.AnalysisConfigRevision {
	var body any
	if json.Unmarshal(v.Body, &body) != nil {
		return v
	}
	var visit func(any)
	visit = func(value any) {
		switch data := value.(type) {
		case map[string]any:
			delete(data, "requests")
			delete(data, "objectKey")
			for _, child := range data {
				visit(child)
			}
		case []any:
			for _, child := range data {
				visit(child)
			}
		}
	}
	visit(body)
	v.Body, _ = json.Marshal(body)
	return v
}
func responseWrite(w http.ResponseWriter, status int, v model.AnalysisConfigRevision, err error) {
	if err != nil {
		analysisProblem(w, err)
		return
	}
	write(w, status, publicResponseConfig(v))
}
func (s *Server) responseList(kind, path string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		items, total, err := s.responseService().List(r.Context(), analysisActor(r), kind, analysisPage(r))
		if err != nil {
			analysisProblem(w, err)
			return
		}
		if kind == response.ExecutionKind {
			// Source filtering precedes pagination, so unrelated cases never affect
			// the visible drill count or create empty pages.
			filter := analysisPage(r)
			filter.Limit = 100
			filter.Offset = 0
			all := []model.AnalysisConfigRevision{}
			for {
				page, count, e := s.responseService().List(r.Context(), analysisActor(r), kind, filter)
				if e != nil {
					analysisProblem(w, e)
					return
				}
				for _, v := range page {
					var exec model.ResponseExecution
					_ = json.Unmarshal(v.Body, &exec)
					if (path == "drills" && exec.Source == "DRILL") || (path == "response-cases" && exec.Source == "REAL_CASE") {
						all = append(all, v)
					}
				}
				filter.Offset += len(page)
				if filter.Offset >= count {
					break
				}
				if filter.Offset > 10000 || len(page) == 0 {
					analysisProblem(w, model.ErrAnalysisInvalid)
					return
				}
			}
			total = len(all)
			pagination := analysisPage(r)
			limit, offset := analytics.NormalizeAnalysisPage(pagination.Limit, pagination.Offset)
			if offset >= total {
				items = []model.AnalysisConfigRevision{}
			} else {
				items = all[offset:min(total, offset+limit)]
			}
		}
		for i := range items {
			items[i] = publicResponseConfig(items[i])
		}
		write(w, 200, map[string]any{"items": items, "total": total})
	}
}
func (s *Server) responseGet(kind string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		v, e := s.responseService().Latest(r.Context(), analysisActor(r), kind, r.PathValue("id"))
		responseWrite(w, 200, v, e)
	}
}
func (s *Server) responseRevision(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	v, e := s.responseService().Revision(r.Context(), analysisActor(r), kind, r.PathValue("id"))
	responseWrite(w, 200, v, e)
}
func (s *Server) responseSave(path string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		var q response.RevisionRequest
		if decode(w, r, &q) != nil {
			return
		}
		if q.IdempotencyKey == "" {
			q.IdempotencyKey = r.Header.Get("Idempotency-Key")
		}
		svc := s.responseService()
		var v model.AnalysisConfigRevision
		var err error
		switch path {
		case "response-procedures":
			v, err = svc.SaveProcedure(r.Context(), analysisActor(r), q)
		case "drills":
			v, err = svc.CreateExecution(r.Context(), analysisActor(r), q, "DRILL")
		case "response-cases":
			v, err = svc.CreateExecution(r.Context(), analysisActor(r), q, "REAL_CASE")
		case "corrective-actions":
			v, err = svc.CreateCorrective(r.Context(), analysisActor(r), q)
		}
		responseWrite(w, 201, v, err)
	}
}
func (s *Server) responsePublish(w http.ResponseWriter, r *http.Request) {
	var q response.ActionRequest
	if decode(w, r, &q) != nil {
		return
	}
	v, e := s.responseService().PublishProcedure(r.Context(), analysisActor(r), r.PathValue("id"), q)
	responseWrite(w, 201, v, e)
}
func (s *Server) responseAction(w http.ResponseWriter, r *http.Request) {
	var q response.ActionRequest
	if decode(w, r, &q) != nil {
		return
	}
	v, e := s.responseService().ExecutionAction(r.Context(), analysisActor(r), r.PathValue("id"), q)
	responseWrite(w, 201, v, e)
}
func (s *Server) responseMilestone(correction bool) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		var q response.MilestoneRequest
		if decode(w, r, &q) != nil {
			return
		}
		if correction != (q.CorrectsID != "") {
			analysisProblem(w, model.ErrAnalysisInvalid)
			return
		}
		v, e := s.responseService().AppendMilestone(r.Context(), analysisActor(r), r.PathValue("id"), q)
		responseWrite(w, 201, v, e)
	}
}
func (s *Server) responseSystemMilestone(w http.ResponseWriter, r *http.Request) {
	var q response.SystemMilestoneRequest
	if decode(w, r, &q) != nil {
		return
	}
	v, e := s.responseService().AppendSystemMilestone(r.Context(), analysisActor(r), r.PathValue("id"), q)
	responseWrite(w, 201, v, e)
}
func (s *Server) responseSimulation(w http.ResponseWriter, r *http.Request) {
	var q response.SimulationRequest
	if decode(w, r, &q) != nil {
		return
	}
	v, e := s.responseService().AppendSimulation(r.Context(), analysisActor(r), r.PathValue("id"), q)
	responseWrite(w, 201, v, e)
}
func (s *Server) responseEvaluation(w http.ResponseWriter, r *http.Request) {
	var q analytics.CreateRequest
	if decode(w, r, &q) != nil {
		return
	}
	svc := s.responseService()
	if err := svc.ValidateCreate(r.Context(), analysisActor(r), &q); err != nil {
		analysisProblem(w, err)
		return
	}
	var p response.EvaluationParameters
	_ = json.Unmarshal(q.Parameters, &p)
	input, err := svc.Revision(r.Context(), analysisActor(r), response.ExecutionKind, p.ExecutionRevisionID)
	if err != nil {
		analysisProblem(w, err)
		return
	}
	if input.ResourceID != r.PathValue("id") {
		analysisProblem(w, model.ErrNotFound)
		return
	}
	run, err := s.analysis.Create(r.Context(), analysisActor(r), analytics.KindResponse, response.AlgorithmVersion, q)
	if err != nil {
		analysisProblem(w, err)
		return
	}
	write(w, 202, publicAnalysisRun(run))
}
func (s *Server) responseConfirm(w http.ResponseWriter, r *http.Request) {
	var q response.ConfirmRequest
	if decode(w, r, &q) != nil {
		return
	}
	v, e := s.responseService().ConfirmReview(r.Context(), analysisActor(r), r.PathValue("id"), r.PathValue("revisionId"), q)
	responseWrite(w, 201, v, e)
}
func (s *Server) responseImprovement(w http.ResponseWriter, r *http.Request) {
	var q response.RevisionRequest
	if decode(w, r, &q) != nil {
		return
	}
	var body response.CorrectiveBody
	if json.Unmarshal(q.Body, &body) != nil {
		analysisProblem(w, model.ErrAnalysisInvalid)
		return
	}
	run, err := s.analysis.Get(r.Context(), analysisActor(r), analytics.KindResponse, body.ReviewRevisionID)
	if err != nil {
		analysisProblem(w, err)
		return
	}
	var p response.EvaluationParameters
	_ = json.Unmarshal(run.Parameters, &p)
	input, err := s.responseService().Revision(r.Context(), analysisActor(r), response.ExecutionKind, p.ExecutionRevisionID)
	if err != nil {
		analysisProblem(w, err)
		return
	}
	if input.ResourceID != r.PathValue("id") {
		analysisProblem(w, model.ErrNotFound)
		return
	}
	v, e := s.responseService().CreateCorrective(r.Context(), analysisActor(r), q)
	responseWrite(w, 201, v, e)
}
func (s *Server) correctiveAction(w http.ResponseWriter, r *http.Request) {
	var q response.CorrectiveRequest
	if decode(w, r, &q) != nil {
		return
	}
	v, e := s.responseService().CorrectiveAction(r.Context(), analysisActor(r), r.PathValue("id"), q)
	responseWrite(w, 201, v, e)
}
func (s *Server) correctiveVerification(w http.ResponseWriter, r *http.Request) {
	var q response.VerificationRequest
	if decode(w, r, &q) != nil {
		return
	}
	v, e := s.responseService().VerifyCorrective(r.Context(), analysisActor(r), r.PathValue("id"), q)
	responseWrite(w, 201, v, e)
}

func (s *Server) correctiveDutyLink(w http.ResponseWriter, r *http.Request) {
	var q struct {
		ExpectedVersion int64  `json:"expectedVersion"`
		RunID           string `json:"runId"`
		OwnerID         string `json:"ownerId"`
		NextAction      string `json:"nextAction"`
	}
	if decode(w, r, &q) != nil {
		return
	}
	v, err := s.responseService().Latest(r.Context(), analysisActor(r), response.CorrectiveKind, r.PathValue("id"))
	if err != nil {
		analysisProblem(w, err)
		return
	}
	if v.Version != q.ExpectedVersion {
		analysisProblem(w, model.ErrAnalysisConflict)
		return
	}
	var action model.CorrectiveAction
	_ = json.Unmarshal(v.Body, &action)
	if slices.Contains([]string{"DONE", "CANCELLED"}, action.Status) {
		analysisProblem(w, model.ErrAnalysisConflict)
		return
	}
	current, err := s.analysis.Current(r.Context(), analysisActor(r))
	if err != nil || !current.Allows(analytics.KindResponse, "POST /api/v1/corrective-actions/:id/duty-links", v.DeviceIDs) {
		analysisProblem(w, analytics.ErrForbidden)
		return
	}
	svc, err := s.dutyService(r.Context())
	if err != nil {
		dutyProblem(w, err)
		return
	}
	link, err := svc.LinkFollowUp(r.Context(), s.dutyActor(r), duty.FollowUpSource{Kind: response.CorrectiveKind, ID: v.ResourceID, Version: v.Version, DeviceIDs: v.DeviceIDs, Title: action.Title, DueAt: action.DueAt}, q.RunID, q.OwnerID, q.NextAction)
	if err != nil {
		dutyProblem(w, err)
		return
	}
	write(w, 201, link)
}
func (s *Server) correctiveDutyLinks(w http.ResponseWriter, r *http.Request) {
	v, err := s.responseService().Latest(r.Context(), analysisActor(r), response.CorrectiveKind, r.PathValue("id"))
	if err != nil {
		analysisProblem(w, err)
		return
	}
	svc, err := s.dutyService(r.Context())
	if err != nil {
		dutyProblem(w, err)
		return
	}
	links, err := svc.FollowUpLinks(r.Context(), s.dutyActor(r), response.CorrectiveKind, v.ResourceID)
	if err != nil {
		dutyProblem(w, err)
		return
	}
	var action model.CorrectiveAction
	_ = json.Unmarshal(v.Body, &action)
	write(w, 200, map[string]any{"items": links, "sourceVersion": v.Version, "sourceStatus": action.Status, "sourceOwner": action.Owner, "completionPolicy": "当班事项完成不替代主业务验收"})
}
func (s *Server) responseUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, response.AttachmentMaxBytes+(1<<20))
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		problem(w, 413, "附件最大16MiB")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		problem(w, 422, "请选择附件")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, response.AttachmentMaxBytes+1))
	if err != nil {
		analysisProblem(w, err)
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		key = r.FormValue("idempotencyKey")
	}
	v, e := s.responseService().UploadAttachment(r.Context(), analysisActor(r), r.PathValue("id"), key, header.Filename, header.Header.Get("Content-Type"), data)
	responseWrite(w, 201, v, e)
}
func (s *Server) responseDownload(w http.ResponseWriter, r *http.Request) {
	v, reader, err := s.responseService().DownloadAttachment(r.Context(), analysisActor(r), r.PathValue("id"))
	if err != nil {
		analysisProblem(w, err)
		return
	}
	defer reader.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename*=UTF-8''%s", url.PathEscape(v.Name)))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.Copy(w, reader)
}
func (s *Server) responseExport(w http.ResponseWriter, r *http.Request) {
	data, err := s.responseService().Export(r.Context(), analysisActor(r), r.PathValue("id"))
	if err != nil {
		analysisProblem(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="response-`+strings.ReplaceAll(r.PathValue("id"), `"`, "")+`.json"`)
	w.WriteHeader(200)
	_, _ = w.Write(data)
}

func (s *Server) responseUpdatePlan(w http.ResponseWriter, r *http.Request) {
	var q response.RevisionRequest
	if decode(w, r, &q) != nil {
		return
	}
	q.ResourceID = r.PathValue("id")
	value, err := s.responseService().UpdatePlan(r.Context(), analysisActor(r), q.ResourceID, q)
	if err != nil {
		analysisProblem(w, err)
		return
	}
	write(w, 200, publicResponseConfig(value))
}
func (s *Server) responseSampleAssociation(w http.ResponseWriter, r *http.Request) {
	var q response.SampleAssociationRequest
	if decode(w, r, &q) != nil {
		return
	}
	value, err := s.responseService().AssociateSample(r.Context(), analysisActor(r), r.PathValue("id"), q)
	if err != nil {
		analysisProblem(w, err)
		return
	}
	write(w, 201, publicResponseConfig(value))
}
