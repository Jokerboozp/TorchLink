package analytics

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func (s *AIService) RunWorkers(ctx context.Context, instance string, log *slog.Logger) {
	if log == nil {
		log = slog.Default()
	}
	if s.Runner == nil {
		return
	}
	store, err := s.store()
	if err != nil {
		return
	}
	var wg sync.WaitGroup
	for i := 0; i < max(1, s.Workers); i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			owner := fmt.Sprintf("%s/analysis-ai/%d", instance, i)
			ticker := time.NewTicker(s.Poll)
			defer ticker.Stop()
			for {
				if ctx.Err() != nil {
					return
				}
				job, err := store.ClaimAnalysisAIRevision(ctx, owner, s.Lease, s.Timeout, s.workflowIDs())
				if err == nil {
					s.executeAI(ctx, job, log)
					continue
				}
				if !errors.Is(err, model.ErrNotFound) && ctx.Err() == nil {
					log.Warn("analysis AI claim failed", "error", err)
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}(i)
	}
	wg.Wait()
}

func (s *AIService) executeAI(parent context.Context, job model.AnalysisAIRevision, log *slog.Logger) {
	ctx, cancel := context.WithDeadline(parent, time.UnixMilli(job.Deadline))
	defer cancel()
	store, err := s.store()
	if err != nil {
		return
	}
	identity := AIIdentity(job)
	ctx = ports.WithAIRunIdentity(ctx, identity)
	if _, err = s.ValidateBinding(ctx, identity); err != nil {
		s.cancelJob(parent, job)
		return
	}
	input, err := s.BuildInput(ctx, job)
	if err != nil {
		_, _ = store.FinishAnalysisAIRevision(parent, job.TenantID, job.ID, job.LeaseToken, model.AnalysisAIResult{}, "", err.Error())
		return
	}
	resultCh := make(chan struct {
		run ports.AIWorkflowResult
		err error
	}, 1)
	go func() {
		var run ports.AIWorkflowResult
		var err error
		defer func() {
			if recover() != nil {
				err = errors.New("AI workflow execution failed")
			}
			resultCh <- struct {
				run ports.AIWorkflowResult
				err error
			}{run, err}
		}()
		run, err = s.Runner(ctx, job, input)
	}()
	ticker := time.NewTicker(max(s.Lease/3, 10*time.Millisecond))
	defer ticker.Stop()
	stopExternal := func() {
		if s.StopRunner != nil {
			stopCtx, done := context.WithTimeout(context.Background(), 3*time.Second)
			defer done()
			_ = s.StopRunner(stopCtx, job.TenantID, job.HarnessRunID)
		}
	}
	finishFailure := func(failure string) {
		finishCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		_, _ = store.FinishAnalysisAIRevision(finishCtx, job.TenantID, job.ID, job.LeaseToken, model.AnalysisAIResult{}, "", failure)
	}
	for {
		select {
		case <-parent.Done():
			stopExternal()
			return // On restart the expired call is FAILED, never replayed.
		case <-ctx.Done():
			stopExternal()
			finishFailure("AI 工作流超时，外部结果未确认为成功")
			return
		case outcome := <-resultCh:
			if _, err = s.ValidateBinding(ctx, identity); err != nil {
				s.cancelJob(parent, job)
				return
			}
			if outcome.err != nil {
				finishFailure(outcome.err.Error())
				return
			}
			if outcome.run.RunID != job.HarnessRunID {
				finishFailure("AI 结果不属于本次签名运行")
				return
			}
			current, err := s.Facts.Store.GetAnalysisAIRevision(ctx, job.TenantID, job.ID)
			if err != nil {
				return
			}
			coverage, err := s.coverage(ctx, current)
			if err != nil {
				finishFailure(err.Error())
				return
			}
			result, err := DecodeAnalysisAIResult(outcome.run.Answer, current.SentFactIDs, current.DeviceIDs, coverage)
			if err != nil {
				finishFailure("AI 输出结构、对象范围或事实引用无效")
				return
			}
			if _, err = s.ValidateBinding(ctx, identity); err != nil {
				s.cancelJob(parent, job)
				return
			}
			if _, err = store.FinishAnalysisAIRevision(ctx, job.TenantID, job.ID, job.LeaseToken, result, outcome.run.Model, ""); err != nil {
				if errors.Is(err, model.ErrAnalysisInvalid) || errors.Is(err, ErrForbidden) {
					finishFailure("AI 输出的对象与引用事实不匹配")
				} else if !errors.Is(err, model.ErrAnalysisLeaseLost) {
					log.Warn("analysis AI result not saved", "jobId", job.ID, "error", err)
				}
			}
			return
		case <-ticker.C:
			if _, err = s.ValidateBinding(ctx, identity); err != nil {
				cancel()
				stopExternal()
				s.cancelJob(parent, job)
				return
			}
			if _, err = store.RenewAnalysisAILease(ctx, job.TenantID, job.ID, job.LeaseToken, s.Lease); err != nil {
				cancel()
				stopExternal()
				return
			}
		}
	}
}
