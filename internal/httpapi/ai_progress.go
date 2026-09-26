package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

const (
	aiAnalysisEstimateDefault = 45 * time.Second
	// The running job stores progress at this interval; without a refresh for
	// aiAnalysisStaleAfter its process has stopped.
	aiAnalysisHeartbeat  = 2 * time.Second
	aiAnalysisStaleAfter = 30 * time.Second
)

// 手动研判任务的进度与结果保存在仓储中：服务重启后仍可读取，多个 API 副本看到同一任务；
// 执行仍在发起任务的进程内进行，心跳停止的任务标记为中断。

// startAIAnalysisJob starts or returns the running job of the alarm. The
// knowledge scope is decided from the caller's role before the job leaves the
// request; each scope runs and is polled as a separate job.
func (s *Server) startAIAnalysisJob(ctx context.Context, tenantID, alarmID, actor, knowledgeScope string, identity ports.AIRunIdentity) (model.AlarmAnalysisJob, error) {
	if existing, found, err := s.loadAIAnalysisJob(ctx, tenantID, alarmID, knowledgeScope); err != nil || found && existing.Status == "running" {
		return existing, err
	}
	now := time.Now().UnixMilli()
	job := model.AlarmAnalysisJob{
		ID:                   "ai_job_" + randomHex(10),
		TenantID:             tenantID,
		AlarmID:              alarmID,
		KnowledgeScope:       knowledgeScope,
		Actor:                actor,
		Status:               "running",
		Stage:                "preparing",
		Message:              "正在准备告警上下文",
		Progress:             8,
		EstimatedRemainingMs: s.aiAnalysisEstimate(),
		StartedAt:            now,
		UpdatedAt:            now,
	}
	created, err := s.engine.Repo.CreateAlarmAnalysisJob(ctx, job)
	if err != nil {
		return job, err
	}
	if !created {
		// Another request or replica started the same analysis first.
		existing, found, loadErr := s.loadAIAnalysisJob(ctx, tenantID, alarmID, knowledgeScope)
		if loadErr == nil && !found {
			loadErr = errors.New("AI 研判任务创建冲突，请稍后重试")
		}
		return existing, loadErr
	}
	go s.runAIAnalysisJob(job, identity)
	return job, nil
}

// loadAIAnalysisJob returns the newest job of the alarm and scope. A running
// job whose heartbeat stopped is marked interrupted instead of staying running.
func (s *Server) loadAIAnalysisJob(ctx context.Context, tenantID, alarmID, knowledgeScope string) (model.AlarmAnalysisJob, bool, error) {
	job, err := s.engine.Repo.LatestAlarmAnalysisJob(ctx, tenantID, alarmID, knowledgeScope)
	if errors.Is(err, model.ErrNotFound) {
		return job, false, nil
	}
	if err != nil {
		return job, false, err
	}
	if job.Status == "running" && time.Since(time.UnixMilli(job.UpdatedAt)) > aiAnalysisStaleAfter {
		now := time.Now().UnixMilli()
		job.Status, job.Stage, job.Progress, job.EstimatedRemainingMs = "failed", "failed", 100, 0
		job.Message, job.Error = "AI 研判已中断", "执行任务的服务已重启或失联，请重新发起研判"
		job.UpdatedAt, job.FinishedAt = now, now
		if _, err = s.engine.Repo.UpdateRunningAlarmAnalysisJob(ctx, job); err != nil {
			return job, true, err
		}
		// A concurrent writer may have finished the job first; read the stored state.
		if job, err = s.engine.Repo.LatestAlarmAnalysisJob(ctx, tenantID, alarmID, knowledgeScope); err != nil {
			return job, true, err
		}
	}
	return job, true, nil
}

func (s *Server) runAIAnalysisJob(job model.AlarmAnalysisJob, identity ports.AIRunIdentity) {
	started := time.UnixMilli(job.StartedAt)
	ctx, cancel := context.WithTimeout(ports.WithAIRunIdentity(context.Background(), identity), 3*time.Minute) // 以发起人的身份运行告警研判工作流。
	defer cancel()
	resultCh := make(chan struct {
		analysis model.AIAnalysis
		err      error
	}, 1)
	go func() {
		analysis, err := s.engine.AnalyzeAlarm(ctx, job.TenantID, job.AlarmID, job.KnowledgeScope != model.AIAnalysisScopeNone)
		resultCh <- struct {
			analysis model.AIAnalysis
			err      error
		}{analysis, err}
	}()
	ticker := time.NewTicker(aiAnalysisHeartbeat)
	defer ticker.Stop()
	for {
		select {
		case result := <-resultCh:
			s.updateAIAnalysisEstimate(time.Since(started).Milliseconds())
			s.finishAIAnalysisJob(job, result.analysis, result.err)
			return
		case now := <-ticker.C:
			elapsed := now.Sub(started).Milliseconds()
			estimate := s.aiAnalysisEstimate()
			job.Progress = min(88, 12+int(float64(elapsed)/float64(estimate)*76))
			job.Stage, job.Message = "preparing", "正在准备告警上下文"
			if elapsed >= 1500 {
				job.Stage, job.Message = "calling_model", "正在调用 AI 模型"
			}
			job.EstimatedRemainingMs = maxInt64(0, estimate-elapsed)
			job.UpdatedAt = now.UnixMilli()
			if !s.storeRunningAIAnalysisJob(job) {
				// The stored job was marked interrupted; stop reporting on it.
				return
			}
		}
	}
}

// storeRunningAIAnalysisJob saves a progress heartbeat. A transient store error
// keeps the job running; it is only abandoned once marked interrupted.
func (s *Server) storeRunningAIAnalysisJob(job model.AlarmAnalysisJob) bool {
	ctx, cancel := context.WithTimeout(context.Background(), healthInspectionStoreTimeout)
	defer cancel()
	updated, err := s.engine.Repo.UpdateRunningAlarmAnalysisJob(ctx, job)
	if err != nil {
		if s.log != nil {
			s.log.Warn("save alarm analysis progress failed", "tenant", job.TenantID, "alarm", job.AlarmID, "job", job.ID, "error", err)
		}
		return true
	}
	return updated
}

func (s *Server) finishAIAnalysisJob(job model.AlarmAnalysisJob, analysis model.AIAnalysis, err error) {
	finished := time.Now().UnixMilli()
	job.UpdatedAt, job.FinishedAt, job.EstimatedRemainingMs, job.Progress = finished, finished, 0, 100
	job.Analysis = analysis
	if err != nil {
		job.Status, job.Stage, job.Message = "failed", "failed", "AI 研判失败"
		job.Error = strings.TrimSpace(err.Error())
	} else {
		job.Status, job.Stage, job.Message = "succeeded", "completed", "AI 研判已完成"
	}
	s.storeRunningAIAnalysisJob(job)
	if err == nil {
		_ = s.engine.Repo.SaveAudit(context.Background(), model.AuditLog{
			ID:         "audit_" + randomHex(10),
			TenantID:   job.TenantID,
			Actor:      job.Actor,
			Action:     "ai.alarm-analysis.run",
			TargetType: "alarm",
			TargetID:   job.AlarmID,
			Details:    map[string]any{"manual": true, "jobId": job.ID, "knowledgeScope": job.KnowledgeScope},
			CreatedAt:  job.FinishedAt,
		})
	}
}

func (s *Server) aiAnalysisEstimate() int64 {
	s.aiAnalysisMu.RLock()
	defer s.aiAnalysisMu.RUnlock()
	if s.aiAnalysisEstimateMs <= 0 {
		return aiAnalysisEstimateDefault.Milliseconds()
	}
	return s.aiAnalysisEstimateMs
}

func (s *Server) updateAIAnalysisEstimate(elapsed int64) {
	if elapsed <= 0 {
		return
	}
	s.aiAnalysisMu.Lock()
	defer s.aiAnalysisMu.Unlock()
	current := s.aiAnalysisEstimateMs
	if current <= 0 {
		current = aiAnalysisEstimateDefault.Milliseconds()
	}
	updated := (current*3 + elapsed) / 4
	if updated < 5000 {
		updated = 5000
	}
	if updated > 180000 {
		updated = 180000
	}
	s.aiAnalysisEstimateMs = updated
}

func (s *Server) aiAlarmAnalysisProgress(w http.ResponseWriter, r *http.Request) {
	// 只能查看与本人角色相同知识范围的任务。
	job, found, err := s.loadAIAnalysisJob(r.Context(), claims(r).TenantID, r.PathValue("alarmId"), alarmAnalysisRunScope(r.Context()))
	if err != nil {
		problem(w, http.StatusServiceUnavailable, "读取 AI 研判任务失败")
		return
	}
	requestedJobID := strings.TrimSpace(r.PathValue("jobId"))
	if !found || requestedJobID != "" && job.ID != requestedJobID {
		problem(w, http.StatusNotFound, "AI 研判任务不存在或已过期")
		return
	}
	write(w, http.StatusOK, aiAnalysisJobView(job))
}

func aiAnalysisJobView(job model.AlarmAnalysisJob) map[string]any {
	view := map[string]any{
		"jobId":                job.ID,
		"alarmId":              job.AlarmID,
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
		view["analysis"] = job.Analysis
	}
	if job.Error != "" {
		view["error"] = job.Error
	}
	return view
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
