package httpapi

import (
	"context"
	"errors"
	"net/http"

	"iot-platform/internal/analytics"
	"iot-platform/internal/analytics/dataquality"
	"iot-platform/internal/analytics/monitoring"
	"iot-platform/internal/auth"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"iot-platform/internal/rulelab/lab"
)

func (s *Server) setupAnalytics() {
	store, ok := s.unscopedRepo().(ports.AnalysisStore)
	if !ok {
		store = analytics.NewMemoryStore()
	}
	s.analysis = analytics.NewService(store, s.cfg.Analytics, s.resolveAnalysisActor, func(ctx context.Context, tenant, id string) error {
		_, err := s.unscopedRepo().GetManagedDevice(ctx, tenant, id)
		return err
	})
	s.analysisFacts, _ = s.unscopedRepo().(ports.AnalyticsFactStore)
	s.quality = dataquality.NewService(s.analysis, s.analysisFacts)
	s.quality.RecordLimit = s.analysis.Limits.RecordLimit
	s.quality.Catalog = s.unscopedRepo()
	s.quality.Archive = s.engine.Archive
	if err := s.quality.Register(); err != nil {
		panic(err)
	}
	s.monitoring = monitoring.NewService(s.analysis, s.analysisFacts)
	s.monitoring.RecordLimit = s.analysis.Limits.RecordLimit
	s.monitoring.Catalog = s.unscopedRepo()
	if err := s.monitoring.Register(); err != nil {
		panic(err)
	}
	inputs, _ := s.unscopedRepo().(ports.RuleLabInputStore)
	s.rulelab = lab.NewService(s.analysis, inputs)
	s.rulelab.Facts = s.analysisFacts
	s.rulelab.Catalog = s.unscopedRepo()
	s.rulelab.History = s.unscopedRepo()
	s.rulelab.RecordLimit = s.analysis.Limits.RecordLimit
	s.rulelab.ValidateCandidate = func(ctx context.Context, a analytics.Actor, rule model.AlarmRule) error {
		if rule.TenantID != a.TenantID {
			return analytics.ErrForbidden
		}
		_, _, err := s.engine.ValidateRuleDraft(ctx, rule)
		return err
	}
	if err := s.rulelab.Register(); err != nil {
		panic(err)
	}
}

// SetAnalysisStorage is startup wiring: decorated Repository interfaces do not
// automatically preserve optional durable ports. Call before starting workers.
func (s *Server) SetAnalysisStorage(store ports.AnalysisStore, facts ports.AnalyticsFactStore) {
	s.analysis.Store = store
	s.analysisFacts = facts
	s.quality.Facts = facts
	s.monitoring.Facts = facts
	s.rulelab.Facts = facts
	if inputs, ok := facts.(ports.RuleLabInputStore); ok {
		s.rulelab.Inputs = inputs
	} else if inputs, ok := store.(ports.RuleLabInputStore); ok {
		s.rulelab.Inputs = inputs
	}
}

func (s *Server) resolveAnalysisActor(ctx context.Context, a analytics.Actor) (analytics.Actor, error) {
	if !a.Managed {
		if a.Username != s.cfg.AdminUser || !adminTenantAllowed(s.cfg.AdminTenants, a.TenantID) {
			return analytics.Actor{}, analytics.ErrForbidden
		}
		a.AllDevices = true
		a.Permissions = []string{"*"}
		a.AccessVersion = "admin:" + a.TenantID + ":" + a.Username
		return a, nil
	}
	c := auth.Claims{TenantID: a.TenantID, Username: a.Username, TokenUse: "user", SessionVersion: a.SessionVersion}
	u, p, err := s.managedIdentity((&http.Request{}).WithContext(ctx), c)
	if err != nil {
		return analytics.Actor{}, analytics.ErrForbidden
	}
	scope := scopeFor(u, p, a.TenantID)
	a.DeviceIDs = u.DeviceIDs
	a.AllDevices = scope.All
	a.Permissions = permissionList(p)
	a.AccessVersion = accessVersion(u, p, a.TenantID)
	return a, nil
}

func analysisActor(r *http.Request) analytics.Actor {
	c := claims(r)
	return analytics.Actor{TenantID: c.TenantID, Username: c.Username, Managed: c.TokenUse == "user", SessionVersion: c.SessionVersion}
}

func (s *Server) AnalysisService() *analytics.Service { return s.analysis }
func (s *Server) RunAnalysisWorkers(ctx context.Context) {
	s.analysis.RunWorkers(ctx, s.cfg.InstanceID, s.log)
}

func (s *Server) analysisRoutes() {
	for _, kind := range []string{analytics.KindDataQuality, analytics.KindMonitoring, analytics.KindRuleLab} {
		prefix := analytics.Prefix(kind)
		s.router.GET(prefix+"/runs", s.authorize("viewer"), s.endpoint(s.analysisRuns(kind)))
		s.router.POST(prefix+"/runs", s.authorize("viewer"), s.endpoint(s.analysisCreate(kind)))
		s.router.GET(prefix+"/runs/:id", s.authorize("viewer"), s.endpoint(s.analysisRun(kind), "id"))
		s.router.POST(prefix+"/runs/:id/stop", s.authorize("viewer"), s.endpoint(s.analysisStop(kind), "id"))
		s.router.GET(prefix+"/runs/:id/snapshot", s.authorize("viewer"), s.endpoint(s.analysisSnapshot(kind), "id"))
		s.router.GET(prefix+"/runs/:id/evidence", s.authorize("viewer"), s.endpoint(s.analysisEvidence(kind), "id"))
		collections := []string{"metrics", "findings"}
		if kind == analytics.KindMonitoring {
			collections = append(collections, "intervals", "dependency-groups")
		}
		if kind == analytics.KindRuleLab {
			collections = append(collections, "outcomes", "diffs", "labels")
		}
		for _, collection := range collections {
			s.router.GET(prefix+"/runs/:id/"+collection, s.authorize("viewer"), s.endpoint(s.analysisOutputs(kind, collection), "id"))
		}
	}
}

func analysisProblem(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, analytics.ErrForbidden):
		problem(w, 403, err.Error())
	case errors.Is(err, model.ErrNotFound):
		problem(w, 404, "分析任务或结果不存在")
	case errors.Is(err, model.ErrAnalysisConflict), errors.Is(err, model.ErrAnalysisLeaseLost):
		problem(w, 409, err.Error())
	case errors.Is(err, model.ErrAnalysisQueueFull):
		problem(w, 429, "分析队列已满，请稍后重试")
	case errors.Is(err, model.ErrAnalysisInvalid):
		problem(w, 422, err.Error())
	case errors.Is(err, analytics.ErrUnsupported):
		problem(w, 503, err.Error())
	default:
		problem(w, 500, "分析操作失败")
	}
}

func analysisPage(r *http.Request) model.AnalysisFilter {
	return model.AnalysisFilter{Limit: intval(r.URL.Query().Get("limit"), 20), Offset: intval(r.URL.Query().Get("offset"), 0)}
}
func publicAnalysisRun(run model.AnalysisRun) model.AnalysisRun {
	run.LeaseOwner = ""
	run.LeaseToken = 0
	run.LeaseExpiresAt = 0
	run.Checkpoint = nil
	run.PermissionsVersion = ""
	run.CreatorSessionVersion = 0
	return run
}

func (s *Server) analysisCreate(kind string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		var q analytics.CreateRequest
		if decode(w, r, &q) != nil {
			return
		}
		if q.IdempotencyKey == "" {
			q.IdempotencyKey = r.Header.Get("Idempotency-Key")
		}
		algorithm := "v1"
		if kind == analytics.KindDataQuality {
			if err := s.quality.ValidateCreate(r.Context(), analysisActor(r), &q); err != nil {
				analysisProblem(w, err)
				return
			}
			algorithm = dataquality.AlgorithmVersion
		}
		if kind == analytics.KindMonitoring {
			if err := s.monitoring.ValidateCreate(r.Context(), analysisActor(r), &q); err != nil {
				analysisProblem(w, err)
				return
			}
			algorithm = monitoring.AlgorithmVersion
		}
		if kind == analytics.KindRuleLab {
			if err := s.rulelab.ValidateCreate(r.Context(), analysisActor(r), &q); err != nil {
				analysisProblem(w, err)
				return
			}
			algorithm = lab.AlgorithmVersion
		}
		run, err := s.analysis.Create(r.Context(), analysisActor(r), kind, algorithm, q)
		if err != nil {
			analysisProblem(w, err)
			return
		}
		write(w, 202, publicAnalysisRun(run))
	}
}
func (s *Server) analysisRuns(kind string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		items, total, err := s.analysis.List(r.Context(), analysisActor(r), kind, analysisPage(r))
		if err != nil {
			analysisProblem(w, err)
			return
		}
		for i := range items {
			items[i] = publicAnalysisRun(items[i])
		}
		write(w, 200, map[string]any{"items": items, "total": total})
	}
}
func (s *Server) analysisRun(kind string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		run, err := s.analysis.Get(r.Context(), analysisActor(r), kind, r.PathValue("id"))
		if err != nil {
			analysisProblem(w, err)
			return
		}
		write(w, 200, publicAnalysisRun(run))
	}
}
func (s *Server) analysisStop(kind string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			ExpectedVersion int64 `json:"expectedVersion"`
		}
		if decode(w, r, &q) != nil {
			return
		}
		if q.ExpectedVersion < 1 {
			problem(w, 422, "expectedVersion 必须为当前版本")
			return
		}
		run, err := s.analysis.Stop(r.Context(), analysisActor(r), kind, r.PathValue("id"), q.ExpectedVersion)
		if err != nil {
			analysisProblem(w, err)
			return
		}
		write(w, 200, publicAnalysisRun(run))
	}
}
func (s *Server) analysisSnapshot(kind string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		snapshot, err := s.analysis.Snapshot(r.Context(), analysisActor(r), kind, r.PathValue("id"))
		if err != nil {
			analysisProblem(w, err)
			return
		}
		write(w, 200, snapshot)
	}
}
func (s *Server) analysisOutputs(kind, collection string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		f := analysisPage(r)
		f.Kind = collection
		items, total, err := s.analysis.Outputs(r.Context(), analysisActor(r), kind, r.PathValue("id"), f)
		if err != nil {
			analysisProblem(w, err)
			return
		}
		write(w, 200, map[string]any{"items": items, "total": total})
	}
}
func (s *Server) analysisEvidence(kind string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		items, total, err := s.analysis.Evidence(r.Context(), analysisActor(r), kind, r.PathValue("id"), analysisPage(r))
		if err != nil {
			analysisProblem(w, err)
			return
		}
		write(w, 200, map[string]any{"items": items, "total": total})
	}
}
