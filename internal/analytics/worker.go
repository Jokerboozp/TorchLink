package analytics

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"iot-platform/internal/model"
)

// Execution exposes only analysis commits. A processor cannot obtain the
// production Bus, alarm store or device-control ports from this object.
type Execution struct {
	service *Service
	Run     model.AnalysisRun
	current model.AnalysisRun
	mu      sync.Mutex
}

func runActor(r model.AnalysisRun) Actor {
	return Actor{TenantID: r.TenantID, Username: r.Creator, Managed: r.CreatorManaged, SessionVersion: r.CreatorSessionVersion, AccessVersion: r.PermissionsVersion}
}

func (s *Service) authorizeWorker(ctx context.Context, r model.AnalysisRun) error {
	a, err := s.authorize(ctx, runActor(r), r.Kind, RunCreationOperation(r.Kind, r.CreationOperation), r.DeviceIDs)
	if err != nil || a.AccessVersion != r.PermissionsVersion || !a.AllowsRequired(r.RequiredPermissions) {
		return ErrForbidden
	}
	return nil
}

func (e *Execution) Commit(ctx context.Context, b model.AnalysisBatch) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.service.authorizeWorker(ctx, e.current); err != nil {
		return err
	}
	if len(b.Outputs)+len(b.Evidence) > e.service.Limits.BatchSize {
		return fmt.Errorf("%w: batch exceeds configured limit", model.ErrAnalysisInvalid)
	}
	r, err := e.service.Store.CommitAnalysisBatch(ctx, e.current.TenantID, e.current.ID, e.current.LeaseToken, b)
	if err == nil {
		e.current = r
	}
	return err
}

func (s *Service) processor(kind string) Processor {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.processors[kind]
}
func (s *Service) kinds() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.processors))
	for k := range s.processors {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func (s *Service) RunWorkers(ctx context.Context, instance string, log *slog.Logger) {
	if log == nil {
		log = slog.Default()
	}
	var wg sync.WaitGroup
	for i := 0; i < s.Limits.Workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s.workerLoop(ctx, fmt.Sprintf("%s/analytics/%d/%s", instance, i, uuid.NewString()), log)
		}(i)
	}
	wg.Wait()
}

func (s *Service) workerLoop(ctx context.Context, owner string, log *slog.Logger) {
	ticker := time.NewTicker(s.Limits.Poll)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		r, err := s.Store.ClaimAnalysisRun(ctx, owner, s.Limits.Lease, s.kinds())
		if err == nil {
			s.execute(ctx, r, log)
			continue
		}
		if !errors.Is(err, model.ErrNotFound) && ctx.Err() == nil {
			log.Warn("analysis task claim failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) execute(parent context.Context, r model.AnalysisRun, log *slog.Logger) {
	deadline := time.Now().Add(s.Limits.RunTimeout)
	if r.StartedAt > 0 {
		deadline = time.UnixMilli(r.StartedAt).Add(s.Limits.RunTimeout)
	}
	ctx, cancel := context.WithDeadline(parent, deadline)
	defer cancel()
	e := &Execution{service: s, Run: r, current: r}
	if parent.Err() != nil {
		return
	}
	// A reclaimed run keeps its original deadline. Authorize against the live
	// parent before invoking an already-expired processor context, allowing
	// application timeout handlers to preserve fixed partial facts. The shared
	// failure path still handles processors without a partial-result policy.
	if err := s.authorizeWorker(parent, r); err != nil {
		s.invalidate(parent, r)
		return
	}
	done := make(chan struct{})
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		tick := time.NewTicker(s.Limits.Lease / 3)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-tick.C:
				e.mu.Lock()
				current := e.current
				if modelAnalysisTerminal(current.Status) {
					e.mu.Unlock()
					return
				}
				err := s.authorizeWorker(ctx, current)
				if err == nil {
					var updated model.AnalysisRun
					updated, err = s.Store.RenewAnalysisLease(ctx, current.TenantID, current.ID, current.LeaseToken, s.Limits.Lease)
					if err == nil {
						e.current = updated
					}
				}
				e.mu.Unlock()
				if err != nil {
					if errors.Is(err, ErrForbidden) {
						s.invalidate(parent, current)
					}
					cancel()
					return
				}
			}
		}
	}()
	p := s.processor(r.Kind)
	var err error
	if p == nil {
		err = ErrUnsupported
	} else {
		err = invokeAnalysisProcessor(ctx, p, e)
	}
	close(done)
	<-heartbeatDone
	e.mu.Lock()
	current := e.current
	e.mu.Unlock()
	if errors.Is(err, ErrForbidden) {
		s.invalidate(parent, current)
		return
	}
	if parent.Err() != nil {
		return
	} // Keep the lease/checkpoint for takeover on restart.
	if modelAnalysisTerminal(current.Status) {
		return
	}
	if err == nil {
		err = errors.New("analysis processor returned without final snapshot")
	}
	if errors.Is(err, model.ErrAnalysisLeaseLost) {
		return
	}
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	_, saveErr := s.Store.CommitAnalysisBatch(parent, current.TenantID, current.ID, current.LeaseToken, model.AnalysisBatch{ID: "failure/" + uuid.NewString(), Checkpoint: current.Checkpoint, Processed: current.Processed, Status: model.AnalysisFailed, Stage: "FAILED", Error: err.Error()})
	if saveErr != nil && !errors.Is(saveErr, model.ErrAnalysisLeaseLost) {
		log.Warn("analysis failure could not be saved", "runId", current.ID, "error", saveErr)
	}
}

func invokeAnalysisProcessor(ctx context.Context, p Processor, e *Execution) (err error) {
	defer func() {
		if failure := recover(); failure != nil {
			err = fmt.Errorf("analysis processor panic: %v", failure)
		}
	}()
	return p(ctx, e)
}

func modelAnalysisTerminal(status string) bool {
	return status == model.AnalysisSucceeded || status == model.AnalysisPartial || status == model.AnalysisFailed || status == model.AnalysisCancelled
}

func (s *Service) invalidate(ctx context.Context, r model.AnalysisRun) {
	for i := 0; i < 3; i++ {
		current, err := s.Store.GetAnalysisRun(ctx, r.TenantID, r.ID)
		if err != nil || modelAnalysisTerminal(current.Status) {
			return
		}
		if _, err = s.Store.StopAnalysisRun(ctx, r.TenantID, r.ID, current.Version); !errors.Is(err, model.ErrAnalysisConflict) {
			return
		}
	}
}
