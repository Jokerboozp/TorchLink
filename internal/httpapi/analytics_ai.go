package httpapi

import (
	"context"
	"iot-platform/internal/analytics"
	"iot-platform/internal/auth"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"net/http"
)

func (s *Server) setupAnalysisAI() {
	s.analysis.AI = analytics.NewAIService(s.analysis, s.engine.RunAnalysisWorkflow)
	s.analysis.AI.Workers = s.cfg.Analytics.WithDefaults().Workers
	if manager, ok := s.engine.AIWorkflows.(ports.AIWorkflowRunManager); ok {
		s.analysis.AI.StopRunner = manager.StopWorkflowRun
	}
	s.analysis.AI.ValidateSnapshot = func(ctx context.Context, r model.AnalysisRun) error {
		if r.Kind == analytics.KindRecurring {
			return s.recurring.ValidateSnapshot(ctx, r)
		}
		return nil
	}
	s.engine.AnalysisAI = s.analysis.AI
	s.analysis.AI.PrepareCandidate = s.rulelab.PrepareAICandidate
	s.rulelab.AI = s.analysis.AI
}
func (s *Server) RunAnalysisAIWorkers(ctx context.Context) {
	if s.analysis.AI != nil {
		s.analysis.AI.RunWorkers(ctx, s.cfg.InstanceID, s.log)
	}
}
func (s *Server) analysisAIRoutes() {
	for _, kind := range []string{analytics.KindDataQuality, analytics.KindMonitoring, analytics.KindRecurring} {
		prefix := analytics.Prefix(kind)
		s.router.POST(prefix+"/runs/:id/ai-jobs", s.authorize("viewer"), s.endpoint(s.analysisAICreate(kind), "id"))
		s.router.GET(prefix+"/runs/:id/ai-jobs", s.authorize("viewer"), s.endpoint(s.analysisAIList(kind), "id"))
		s.router.GET(prefix+"/runs/:id/ai-jobs/:jobId", s.authorize("viewer"), s.endpoint(s.analysisAIGet(kind), "id", "jobId"))
		s.router.POST(prefix+"/runs/:id/ai-jobs/:jobId/stop", s.authorize("viewer"), s.endpoint(s.analysisAIStop(kind), "id", "jobId"))
	}
	s.ruleLabAIRoutes()
	s.responseAIRoutes()
	s.investmentAIRoutes()
	p := analytics.RunCollection(analytics.KindMaintenance) + "/:id/ai-jobs"
	s.router.POST(p, s.authorize("viewer"), s.endpoint(s.analysisAICreate(analytics.KindMaintenance), "id"))
	s.router.GET(p, s.authorize("viewer"), s.endpoint(s.analysisAIList(analytics.KindMaintenance), "id"))
	s.router.GET(p+"/:jobId", s.authorize("viewer"), s.endpoint(s.analysisAIGet(analytics.KindMaintenance), "id", "jobId"))
	s.router.POST(p+"/:jobId/stop", s.authorize("viewer"), s.endpoint(s.analysisAIStop(analytics.KindMaintenance), "id", "jobId"))
}
func publicAnalysisAI(v model.AnalysisAIRevision) model.AnalysisAIRevision {
	v.LeaseOwner = ""
	v.LeaseToken = 0
	v.LeaseExpiresAt = 0
	v.PermissionVersion = ""
	v.CreatorSessionVersion = 0
	return v
}
func (s *Server) analysisAICreate(kind string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.analysis.AI == nil {
			analysisProblem(w, analytics.ErrUnsupported)
			return
		}
		var q analytics.CreateAIRequest
		if decode(w, r, &q) != nil {
			return
		}
		if q.IdempotencyKey == "" {
			q.IdempotencyKey = r.Header.Get("Idempotency-Key")
		}
		job, err := s.analysis.AI.Create(r.Context(), analysisActor(r), kind, r.PathValue("id"), q)
		if err != nil {
			analysisProblem(w, err)
			return
		}
		write(w, 202, publicAnalysisAI(job))
	}
}
func (s *Server) analysisAIList(kind string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.analysis.AI == nil {
			analysisProblem(w, analytics.ErrUnsupported)
			return
		}
		items, total, err := s.analysis.AI.List(r.Context(), analysisActor(r), kind, r.PathValue("id"), analysisPage(r))
		if err != nil {
			analysisProblem(w, err)
			return
		}
		for i := range items {
			items[i] = publicAnalysisAI(items[i])
		}
		write(w, 200, map[string]any{"items": items, "total": total})
	}
}
func (s *Server) analysisAIGet(kind string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.analysis.AI == nil {
			analysisProblem(w, analytics.ErrUnsupported)
			return
		}
		job, err := s.analysis.AI.Get(r.Context(), analysisActor(r), kind, r.PathValue("id"), r.PathValue("jobId"))
		if err != nil {
			analysisProblem(w, err)
			return
		}
		write(w, 200, publicAnalysisAI(job))
	}
}
func (s *Server) analysisAIStop(kind string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.analysis.AI == nil {
			analysisProblem(w, analytics.ErrUnsupported)
			return
		}
		var q struct {
			ExpectedVersion int64 `json:"expectedVersion"`
		}
		if decode(w, r, &q) != nil {
			return
		}
		if q.ExpectedVersion <= 0 {
			analysisProblem(w, model.ErrAnalysisInvalid)
			return
		}
		job, err := s.analysis.AI.Stop(r.Context(), analysisActor(r), kind, r.PathValue("id"), r.PathValue("jobId"), q.ExpectedVersion)
		if err != nil {
			analysisProblem(w, err)
			return
		}
		write(w, 200, publicAnalysisAI(job))
	}
}
func (s *Server) authorizeAnalysisAI(ctx context.Context, identity ports.AIRunIdentity) error {
	if s.analysis.AI == nil {
		return analytics.ErrForbidden
	}
	_, err := s.analysis.AI.ValidateBinding(ctx, identity)
	return err
}
func analysisHarnessIdentity(c auth.Claims) ports.AIRunIdentity {
	return ports.AIRunIdentity{TenantID: c.TenantID, Username: c.Username, ManagedUser: c.ManagedUser, SessionVersion: c.SessionVersion, AccessVersion: c.AnalysisAccessVersion, AnalysisRunID: c.AnalysisRunID, AnalysisSnapshotID: c.AnalysisSnapshotID, AnalysisSnapshotVersion: c.AnalysisSnapshotVersion, AnalysisJobID: c.AnalysisJobID, AnalysisLeaseToken: c.AnalysisLeaseToken, AnalysisHarnessRunID: c.RunID, AnalysisWorkflowID: c.Workflow}
}
func (s *Server) authorizeAnalysisHarness(ctx context.Context, c auth.Claims) error {
	if !analytics.IsAnalysisWorkflow(c.Workflow) {
		return analytics.ErrForbidden
	}
	return s.authorizeAnalysisAI(ctx, analysisHarnessIdentity(c))
}
