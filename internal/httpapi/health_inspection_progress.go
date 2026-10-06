package httpapi

import (
	"context"
	"errors"
	"iot-platform/internal/logkey"
	"net/http"
	"strings"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

const (
	healthInspectionEstimateDefault = 45 * time.Second
	// A running job refreshes updatedAt every tick. Without a refresh for this
	// long its process has stopped, so readers mark the job interrupted.
	healthInspectionStaleAfter   = 30 * time.Second
	healthInspectionStoreTimeout = 5 * time.Second
)

// 任务进度与结果保存在仓储中：服务重启后可继续读取，多个 API 副本看到同一任务；执行仍在发起任务的进程内进行。

func (s *Server) runHealthInspection(w http.ResponseWriter, r *http.Request) {
	job, err := s.startHealthInspectionJob(capacityJobContext(r), claims(r).TenantID, claims(r).Username, aiRunIdentity(r.Context(), claims(r)))
	if err != nil {
		s.fail(w, r, err, "")
		return
	}
	write(w, http.StatusAccepted, healthInspectionJobView(job))
}

// identity is the caller; the job keeps it for its Harness advice run.
func (s *Server) startHealthInspectionJob(ctx context.Context, tenantID, actor string, identity ports.AIRunIdentity) (model.HealthInspectionJob, error) {
	if existing, found, err := s.loadHealthInspectionJob(ctx, tenantID); err != nil || found && existing.Status == "running" {
		return existing, err
	}
	now := time.Now()
	estimate := s.inspectionRuns.get()
	job := model.HealthInspectionJob{
		CapacityRunID:        ports.CapacityRunID(ctx),
		ID:                   "inspection_job_" + randomHex(10),
		TenantID:             tenantID,
		Actor:                actor,
		Status:               "running",
		Stage:                "preparing",
		Message:              "正在准备设备健康快照",
		Progress:             8,
		EstimatedRemainingMs: estimate,
		StartedAt:            now.UnixMilli(),
		UpdatedAt:            now.UnixMilli(),
	}
	created, err := s.engine.Repo.CreateHealthInspectionJob(ctx, job)
	if err != nil {
		return job, err
	}
	if !created {
		// Another request or replica started an inspection first; report that one.
		existing, found, loadErr := s.loadHealthInspectionJob(ctx, tenantID)
		if loadErr == nil && !found {
			loadErr = errors.New("智能巡检任务创建冲突，请稍后重试")
		}
		return existing, loadErr
	}
	go s.runHealthInspectionJob(job, identity)
	return job, nil
}

// loadHealthInspectionJob returns the tenant's newest job. A running job whose
// heartbeat stopped is marked interrupted instead of staying running forever.
func (s *Server) loadHealthInspectionJob(ctx context.Context, tenantID string) (model.HealthInspectionJob, bool, error) {
	job, err := s.engine.Repo.LatestHealthInspectionSummary(ctx, tenantID, "")
	if errors.Is(err, model.ErrNotFound) {
		return job, false, nil
	}
	if err != nil {
		return job, false, err
	}
	if job.Status == "running" && time.Since(time.UnixMilli(job.UpdatedAt)) > healthInspectionStaleAfter {
		now := time.Now().UnixMilli()
		job.Status, job.Stage, job.Progress, job.EstimatedRemainingMs = "failed", "failed", 100, 0
		job.Message, job.Error = "智能巡检已中断", "执行任务的服务已重启或失联，请重新开始巡检"
		job.UpdatedAt, job.FinishedAt = now, now
		if _, err = s.engine.Repo.UpdateRunningHealthInspectionJob(ctx, job); err != nil {
			return job, true, err
		}
		// A concurrent writer may have finished the job first; read the stored state.
		if job, err = s.engine.Repo.LatestHealthInspectionSummary(ctx, tenantID, ""); err != nil {
			return job, true, err
		}
	}
	return job, true, nil
}

func (s *Server) runHealthInspectionJob(job model.HealthInspectionJob, identity ports.AIRunIdentity) {
	started := time.UnixMilli(job.StartedAt)
	// A business run may first wait for a free Harness slot; the job must
	// outlive that wait plus the run itself.
	ctx, cancel := context.WithTimeout(ports.WithAIRunIdentity(context.Background(), identity), s.ai.BusinessRunBudget()+30*time.Second)
	defer cancel()
	resultCh := make(chan struct {
		report model.DeviceHealthReport
		err    error
	}, 1)
	go func() {
		report, err := s.ai.InspectDeviceHealth(ctx, job.TenantID)
		resultCh <- struct {
			report model.DeviceHealthReport
			err    error
		}{report, err}
	}()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case result := <-resultCh:
			elapsed := time.Since(started).Milliseconds()
			s.inspectionRuns.observe(elapsed)
			s.finishHealthInspectionJob(job, result.report, result.err)
			return
		case now := <-ticker.C:
			elapsed := now.Sub(started).Milliseconds()
			estimate := s.inspectionRuns.get()
			progress := 12 + int(float64(elapsed)/float64(estimate)*76)
			if progress > 88 {
				progress = 88
			}
			job.Stage, job.Message = "preparing", "正在准备设备健康快照"
			if elapsed >= 1500 {
				job.Stage, job.Message = "calling_model", "正在生成 AI 巡检建议"
			}
			job.Progress = progress
			job.EstimatedRemainingMs = maxInt64(0, estimate-elapsed)
			job.UpdatedAt = now.UnixMilli()
			if !s.storeRunningHealthInspectionJob(job) {
				// The stored job was marked interrupted; stop working on it.
				return
			}
		}
	}
}

// storeRunningHealthInspectionJob saves a progress heartbeat. A transient store
// error keeps the job running; it is only abandoned once marked interrupted.
func (s *Server) storeRunningHealthInspectionJob(job model.HealthInspectionJob) bool {
	ctx, cancel := context.WithTimeout(context.Background(), healthInspectionStoreTimeout)
	defer cancel()
	updated, err := s.engine.Repo.UpdateRunningHealthInspectionJob(ctx, job)
	if err != nil {
		if s.log != nil {
			s.log.Warn("save health inspection progress failed", logkey.Tenant, job.TenantID, "job", job.ID, "error", err)
		}
		return true
	}
	return updated
}

func (s *Server) finishHealthInspectionJob(job model.HealthInspectionJob, report model.DeviceHealthReport, err error) {
	finished := time.Now().UnixMilli()
	job.UpdatedAt, job.FinishedAt, job.EstimatedRemainingMs, job.Progress = finished, finished, 0, 100
	job.Report = report
	if err != nil {
		job.Status, job.Stage, job.Message = "failed", "failed", "智能巡检失败"
		job.Error = strings.TrimSpace(err.Error())
	} else {
		job.Status, job.Stage, job.Message = "succeeded", "completed", "智能巡检已完成"
	}
	s.storeRunningHealthInspectionJob(job)
}

func (s *Server) healthInspectionProgress(w http.ResponseWriter, r *http.Request) {
	requestedJobID := strings.TrimSpace(r.PathValue("jobId"))
	job, found, err := s.loadHealthInspectionJob(r.Context(), claims(r).TenantID)
	if err != nil {
		s.fail(w, r, err, "")
		return
	}
	if !found || requestedJobID != "" && job.ID != requestedJobID {
		problem(w, http.StatusNotFound, "智能巡检任务不存在或已过期")
		return
	}
	write(w, http.StatusOK, healthInspectionJobView(job))
}

func healthInspectionJobView(job model.HealthInspectionJob) map[string]any {
	view := map[string]any{
		"jobId":                job.ID,
		"status":               job.Status,
		"stage":                job.Stage,
		"message":              job.Message,
		"progress":             job.Progress,
		"estimatedRemainingMs": job.EstimatedRemainingMs,
		"startedAt":            job.StartedAt,
		"updatedAt":            job.UpdatedAt,
		"finishedAt":           job.FinishedAt,
	}
	if job.Status == "succeeded" {
		view["report"] = job.Report
	}
	if job.Error != "" {
		view["error"] = job.Error
	}
	return view
}
