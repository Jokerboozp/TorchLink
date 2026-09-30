package httpapi

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"time"

	"iot-platform/internal/auth"
	"iot-platform/internal/core"
	"iot-platform/internal/duty"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func (s *Server) dutyAIRoutes() {
	s.router.POST("/api/v1/duty/handovers/:id/start-ai", s.authorize("viewer"), s.endpoint(s.dutyCommand(model.DutyHandoverKind, "start-ai"), "id"))
	s.router.POST("/api/v1/duty/handovers/:id/ai-jobs", s.authorize("viewer"), s.endpoint(s.dutyCommand(model.DutyHandoverKind, "start-ai"), "id"))
	s.router.POST("/api/v1/duty/ai-jobs/:id/stop", s.authorize("viewer"), s.endpoint(s.dutyCommand(model.DutyAIJobKind, "stop"), "id"))
}

// RunDutyWorkers runs in management processes, where Harness clients exist.
// Persistent claims coordinate API replicas; shutdown cancels inference and
// leaves a recoverable lease rather than an in-memory-only running flag.
func (s *Server) RunDutyWorkers(ctx context.Context) {
	store, ok := s.unscopedRepo().(ports.DutyStore)
	if !ok {
		return
	}
	lister, ok := s.unscopedRepo().(ports.DutyTenantLister)
	if !ok {
		return
	}
	worker := duty.New(store, nil)
	owner := "duty_worker_" + randomHex(16)
	slots := make(chan struct{}, 2)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	scan := func() {
		tenants, err := lister.DutyTenants(ctx)
		if err != nil {
			return
		}
		for _, tenant := range tenants {
			if ctx.Err() != nil {
				return
			}
			_ = worker.RefreshReminders(ctx, tenant)
			var jobs []model.DutyDocument
			err = store.DutyRead(ctx, tenant, func(tx ports.DutyTx) error {
				for _, status := range []string{"QUEUED", "RUNNING", "STOP_REQUESTED"} {
					for offset := 0; ; {
						batch, total, e := tx.List(model.DutyFilter{Kind: model.DutyAIJobKind, Status: status, Limit: 100, Offset: offset})
						if e != nil {
							return e
						}
						jobs = append(jobs, batch...)
						offset += len(batch)
						if len(batch) == 0 || offset >= total {
							break
						}
					}
				}
				return nil
			})
			if err != nil {
				continue
			}
			for _, doc := range jobs {
				j, e := model.DutyBody[model.DutyAIJob](doc)
				if e != nil {
					continue
				}
				if j.Status != "QUEUED" && !((j.Status == "RUNNING" || j.Status == "STOP_REQUESTED") && j.LeaseUntil < time.Now().UnixMilli()) {
					continue
				}
				select {
				case slots <- struct{}{}:
				default:
					return
				}
				claimed, yes, e := worker.ClaimJob(ctx, tenant, doc.ID, owner+":"+randomHex(8), 30*time.Second)
				if e != nil || !yes {
					<-slots
					continue
				}
				go func(tenant string, doc model.DutyDocument) {
					defer func() { <-slots }()
					s.runDutyAIJob(ctx, tenant, doc)
				}(tenant, claimed)
			}
		}
	}
	scan()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			scan()
		}
	}
}

func (s *Server) dutyJobContext(ctx context.Context, tenant, jobID, claimedOwner string) (context.Context, *duty.Service, model.DutyAIJob, model.DutyHandoverRevision, error) {
	store, ok := s.unscopedRepo().(ports.DutyStore)
	if !ok {
		return ctx, nil, model.DutyAIJob{}, model.DutyHandoverRevision{}, errors.New("值班仓储不可用")
	}
	var job model.DutyAIJob
	err := store.DutyRead(ctx, tenant, func(tx ports.DutyTx) error {
		doc, e := tx.Get(model.DutyAIJobKind, jobID)
		if e != nil {
			return e
		}
		job, e = model.DutyBody[model.DutyAIJob](doc)
		return e
	})
	if err != nil {
		return ctx, nil, job, model.DutyHandoverRevision{}, err
	}
	if claimedOwner == "" || job.LeaseOwner != claimedOwner || job.Status != "RUNNING" || job.LeaseUntil <= time.Now().UnixMilli() {
		return ctx, nil, job, model.DutyHandoverRevision{}, duty.ErrConflict
	}
	identity := ports.AIRunIdentity{TenantID: tenant, Username: job.RequesterID, ManagedUser: job.ManagedUser, SessionVersion: job.SessionVersion, AccessVersion: job.AccessVersion, DutyRevisionID: job.RevisionID, DutyJobID: jobID, DutyLeaseOwner: job.LeaseOwner, Scopes: []string{auth.ScopeQueryDutySnapshot}}
	if job.UseKnowledge {
		identity.Scopes = append(identity.Scopes, auth.ScopeQueryKnowledgeBase)
	}
	ctx = ports.WithAIRunIdentity(ctx, identity)
	c := auth.Claims{TenantID: tenant, Username: job.RequesterID, Role: "admin", SessionVersion: job.SessionVersion}
	if job.ManagedUser {
		c.TokenUse = "user"
	}
	ctx = auth.ContextWithClaims(ctx, c)
	ctx = context.WithValue(ctx, claimsKey, c)
	ctx, err = s.authorizeAIRun(ctx, tenant, core.WorkflowDutyHandover)
	if err != nil {
		return ctx, nil, job, model.DutyHandoverRevision{}, err
	}
	svc, err := s.dutyService(ctx)
	if err != nil {
		return ctx, nil, job, model.DutyHandoverRevision{}, err
	}
	r := (&http.Request{}).WithContext(ctx)
	doc, err := svc.Get(ctx, s.dutyActor(r), model.DutyRevisionKind, job.RevisionID)
	if err != nil {
		return ctx, svc, job, model.DutyHandoverRevision{}, err
	}
	rev, err := model.DutyBody[model.DutyHandoverRevision](doc)
	return ctx, svc, job, rev, err
}

func (s *Server) runDutyAIJob(parent context.Context, tenant string, doc model.DutyDocument) {
	job, _ := model.DutyBody[model.DutyAIJob](doc)
	claimedOwner := job.LeaseOwner
	ctx, cancel := context.WithTimeout(parent, 4*time.Minute)
	defer cancel()
	store := s.unscopedRepo().(ports.DutyStore)
	worker := duty.New(store, nil)
	ctx, _, job, revision, err := s.dutyJobContext(ctx, tenant, doc.ID, claimedOwner)
	if err != nil {
		_, _ = worker.FinishJob(parent, tenant, doc.ID, claimedOwner, nil, "", "", err.Error())
		return
	}
	ch := make(chan struct {
		result model.DutyAIResult
		run    ports.AIWorkflowResult
		err    error
	}, 1)
	go func() {
		result, run, e := s.engine.RunDutyHandover(ctx, tenant, job.RevisionID, revision, job.InputLimit, job.UseKnowledge)
		ch <- struct {
			result model.DutyAIResult
			run    ports.AIWorkflowResult
			err    error
		}{result, run, e}
	}()
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-parent.Done():
			return
		case result := <-ch:
			if parent.Err() != nil {
				return
			}
			failure := ""
			if result.err != nil {
				failure = result.err.Error()
			} else if _, e := s.authorizeAIRun(ctx, tenant, core.WorkflowDutyHandover); e != nil {
				failure = e.Error()
			}
			finishCtx, done := context.WithTimeout(context.Background(), 10*time.Second)
			_, _ = worker.FinishJob(finishCtx, tenant, doc.ID, claimedOwner, &result.result, result.run.Model, result.run.RunID, failure)
			done()
			return
		case <-ticker.C:
			ok, e := worker.HeartbeatJob(parent, tenant, doc.ID, claimedOwner, "AI 整理交接", 30*time.Second)
			if e != nil || !ok {
				cancel()
				return
			}
			if _, e = s.authorizeAIRun(ctx, tenant, core.WorkflowDutyHandover); e != nil {
				cancel()
				_, _ = worker.FinishJob(parent, tenant, doc.ID, claimedOwner, nil, "", "", e.Error())
				return
			}
		}
	}
}

func (s *Server) authorizeDutyAI(ctx context.Context, tenant string, identity ports.AIRunIdentity) error {
	if identity.DutyJobID == "" || identity.DutyRevisionID == "" {
		return errors.New("缺少值班任务绑定")
	}
	if !identity.ManagedUser && (identity.Username != s.cfg.AdminUser || !adminTenantAllowed(s.cfg.AdminTenants, tenant)) {
		return duty.ErrForbidden
	}
	svc, err := s.dutyService(ctx)
	if err != nil {
		return err
	}
	r := (&http.Request{}).WithContext(ctx)
	a := s.dutyActor(r)
	doc, err := svc.Get(ctx, a, model.DutyAIJobKind, identity.DutyJobID)
	if err != nil {
		return err
	}
	j, err := model.DutyBody[model.DutyAIJob](doc)
	if err != nil {
		return err
	}
	if identity.DutyLeaseOwner == "" || j.LeaseOwner != identity.DutyLeaseOwner || j.RequesterID != identity.Username || j.RevisionID != identity.DutyRevisionID || j.Status != "RUNNING" || j.LeaseUntil < time.Now().UnixMilli() {
		return duty.ErrForbidden
	}
	hdoc, err := svc.Get(ctx, a, model.DutyHandoverKind, j.HandoverID)
	if err != nil {
		return err
	}
	h, err := model.DutyBody[model.DutyHandover](hdoc)
	if err != nil {
		return err
	}
	if h.CurrentRevisionID != j.RevisionID || (h.Status != "DRAFT" && h.Status != "RETURNED") {
		return duty.ErrConflict
	}
	if j.ManagedUser && j.AccessVersion != requestAccessVersion(ctx, auth.Claims{TokenUse: "user", SessionVersion: j.SessionVersion}) {
		return duty.ErrForbidden
	}
	if !a.AllDevices {
		for _, id := range j.DeviceIDs {
			if !slices.Contains(a.AllowedDeviceIDs, id) {
				return duty.ErrForbidden
			}
		}
	}
	return nil
}

func (s *Server) authorizeDutyHarness(ctx context.Context, c auth.Claims) error {
	store, ok := s.unscopedRepo().(ports.DutyStore)
	if !ok {
		return duty.ErrForbidden
	}
	var j model.DutyAIJob
	if err := store.DutyRead(ctx, c.TenantID, func(tx ports.DutyTx) error {
		doc, e := tx.Get(model.DutyAIJobKind, c.DutyJobID)
		if e != nil {
			return e
		}
		j, e = model.DutyBody[model.DutyAIJob](doc)
		return e
	}); err != nil {
		return duty.ErrForbidden
	}
	return s.authorizeDutyAI(ctx, c.TenantID, ports.AIRunIdentity{TenantID: c.TenantID, Username: c.Username, ManagedUser: c.ManagedUser, SessionVersion: c.SessionVersion, AccessVersion: j.AccessVersion, DutyJobID: c.DutyJobID, DutyRevisionID: c.DutyRevisionID, DutyLeaseOwner: c.DutyLeaseOwner})
}
