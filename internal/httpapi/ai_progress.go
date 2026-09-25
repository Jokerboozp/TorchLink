package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context" /* 执行当前语句并推进处理流程。 */
	"errors"
	"net/http" /* 执行当前语句并推进处理流程。 */
	"strings"  /* 执行当前语句并推进处理流程。 */
	"time"     /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports"
) /* 结束当前表达式或代码块。 */

const ( /* 执行当前语句并推进处理流程。 */
	aiAnalysisEstimateDefault = 45 * time.Second /* 更新 aiAnalysisEstimateDefault 的值。 */
	// The running job stores progress at this interval; without a refresh for
	// aiAnalysisStaleAfter its process has stopped.
	aiAnalysisHeartbeat  = 2 * time.Second
	aiAnalysisStaleAfter = 30 * time.Second
) /* 结束当前表达式或代码块。 */

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

func (s *Server) updateAIAnalysisEstimate(elapsed int64) { /* 定义 updateAIAnalysisEstimate 函数。 */
	if elapsed <= 0 { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	s.aiAnalysisMu.Lock()             /* 执行当前语句并推进处理流程。 */
	defer s.aiAnalysisMu.Unlock()     /* 安排函数结束时执行清理。 */
	current := s.aiAnalysisEstimateMs /* 更新 current 的值。 */
	if current <= 0 {                 /* 判断条件并选择处理分支。 */
		current = aiAnalysisEstimateDefault.Milliseconds() /* 更新 current 的值。 */
	} /* 结束当前表达式或代码块。 */
	updated := (current*3 + elapsed) / 4 /* 更新 updated 的值。 */
	if updated < 5000 {                  /* 判断条件并选择处理分支。 */
		updated = 5000 /* 更新 updated 的值。 */
	} /* 结束当前表达式或代码块。 */
	if updated > 180000 { /* 判断条件并选择处理分支。 */
		updated = 180000 /* 更新 updated 的值。 */
	} /* 结束当前表达式或代码块。 */
	s.aiAnalysisEstimateMs = updated /* 更新 s.aiAnalysisEstimateMs 的值。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) aiAlarmAnalysisProgress(w http.ResponseWriter, r *http.Request) { /* 定义 aiAlarmAnalysisProgress 函数。 */
	// 只能查看与本人角色相同知识范围的任务。
	job, found, err := s.loadAIAnalysisJob(r.Context(), claims(r).TenantID, r.PathValue("alarmId"), alarmAnalysisRunScope(r.Context()))
	if err != nil {
		problem(w, http.StatusServiceUnavailable, "读取 AI 研判任务失败")
		return
	}
	requestedJobID := strings.TrimSpace(r.PathValue("jobId"))
	if !found || requestedJobID != "" && job.ID != requestedJobID {
		problem(w, http.StatusNotFound, "AI 研判任务不存在或已过期") /* 执行当前语句并推进处理流程。 */
		return                                            /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	write(w, http.StatusOK, aiAnalysisJobView(job)) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func aiAnalysisJobView(job model.AlarmAnalysisJob) map[string]any { /* 定义 aiAnalysisJobView 函数。 */
	view := map[string]any{ /* 更新 view 的值。 */
		"jobId":                job.ID,                   /* 执行当前语句并推进处理流程。 */
		"alarmId":              job.AlarmID,              /* 执行当前语句并推进处理流程。 */
		"status":               job.Status,               /* 执行当前语句并推进处理流程。 */
		"stage":                job.Stage,                /* 执行当前语句并推进处理流程。 */
		"message":              job.Message,              /* 执行当前语句并推进处理流程。 */
		"progress":             job.Progress,             /* 执行当前语句并推进处理流程。 */
		"estimatedRemainingMs": job.EstimatedRemainingMs, /* 执行当前语句并推进处理流程。 */
		"startedAt":            job.StartedAt,            /* 执行当前语句并推进处理流程。 */
		"updatedAt":            job.UpdatedAt,            /* 执行当前语句并推进处理流程。 */
		"finishedAt":           job.FinishedAt,           /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if job.Status == "succeeded" { /* 判断条件并选择处理分支。 */
		view["analysis"] = job.Analysis /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if job.Error != "" { /* 判断条件并选择处理分支。 */
		view["error"] = job.Error /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	return view /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func maxInt64(left, right int64) int64 { /* 定义 maxInt64 函数。 */
	if left > right { /* 判断条件并选择处理分支。 */
		return left /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return right /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
