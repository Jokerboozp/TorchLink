package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"  /* 执行当前语句并推进处理流程。 */
	"errors"   /* 执行当前语句并推进处理流程。 */
	"net/http" /* 执行当前语句并推进处理流程。 */
	"strings"  /* 执行当前语句并推进处理流程。 */
	"time"     /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

const ( /* 执行当前语句并推进处理流程。 */
	healthInspectionEstimateDefault = 45 * time.Second /* 更新 healthInspectionEstimateDefault 的值。 */
	// A running job refreshes updatedAt every tick. Without a refresh for this
	// long its process has stopped, so readers mark the job interrupted.
	healthInspectionStaleAfter   = 30 * time.Second
	healthInspectionStoreTimeout = 5 * time.Second
)

// 任务进度与结果保存在仓储中：服务重启后可继续读取，多个 API 副本看到同一任务；执行仍在发起任务的进程内进行。

func (s *Server) runHealthInspection(w http.ResponseWriter, r *http.Request) { /* 定义 runHealthInspection 函数。 */
	job, err := s.startHealthInspectionJob(r.Context(), claims(r).TenantID, claims(r).Username) /* 更新 job 的值。 */
	if err != nil {
		problem(w, http.StatusInternalServerError, err.Error())
		return
	}
	write(w, http.StatusAccepted, healthInspectionJobView(job)) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) startHealthInspectionJob(ctx context.Context, tenantID, actor string) (model.HealthInspectionJob, error) { /* 定义 startHealthInspectionJob 函数。 */
	if existing, found, err := s.loadHealthInspectionJob(ctx, tenantID); err != nil || found && existing.Status == "running" {
		return existing, err
	}
	now := time.Now()                        /* 更新 now 的值。 */
	estimate := s.healthInspectionEstimate() /* 更新 estimate 的值。 */
	job := model.HealthInspectionJob{        /* 更新 job 的值。 */
		ID:                   "inspection_job_" + randomHex(10), /* 执行当前语句并推进处理流程。 */
		TenantID:             tenantID,                          /* 执行当前语句并推进处理流程。 */
		Actor:                actor,                             /* 执行当前语句并推进处理流程。 */
		Status:               "running",                         /* 执行当前语句并推进处理流程。 */
		Stage:                "preparing",                       /* 执行当前语句并推进处理流程。 */
		Message:              "正在准备设备健康快照",                      /* 执行当前语句并推进处理流程。 */
		Progress:             8,                                 /* 执行当前语句并推进处理流程。 */
		EstimatedRemainingMs: estimate,                          /* 执行当前语句并推进处理流程。 */
		StartedAt:            now.UnixMilli(),                   /* 执行当前语句并推进处理流程。 */
		UpdatedAt:            now.UnixMilli(),                   /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
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
	go s.runHealthInspectionJob(job) /* 执行当前语句并推进处理流程。 */
	return job, nil                  /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// loadHealthInspectionJob returns the tenant's newest job. A running job whose
// heartbeat stopped is marked interrupted instead of staying running forever.
func (s *Server) loadHealthInspectionJob(ctx context.Context, tenantID string) (model.HealthInspectionJob, bool, error) {
	job, err := s.engine.Repo.LatestHealthInspectionJob(ctx, tenantID, "")
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
		if job, err = s.engine.Repo.LatestHealthInspectionJob(ctx, tenantID, ""); err != nil {
			return job, true, err
		}
	}
	return job, true, nil
}

func (s *Server) runHealthInspectionJob(job model.HealthInspectionJob) { /* 定义 runHealthInspectionJob 函数。 */
	started := time.UnixMilli(job.StartedAt)                                /* 更新 started 的值。 */
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute) /* 更新 cancel 的值。 */
	defer cancel()                                                          /* 安排函数结束时执行清理。 */
	resultCh := make(chan struct {                                          /* 更新 resultCh 的值。 */
		report model.DeviceHealthReport /* 执行当前语句并推进处理流程。 */
		err    error                    /* 执行当前语句并推进处理流程。 */
	}, 1) /* 结束当前表达式或代码块。 */
	go func() { /* 执行当前语句并推进处理流程。 */
		report, err := s.engine.InspectDeviceHealth(ctx, job.TenantID) /* 更新 err 的值。 */
		resultCh <- struct {                                           /* 执行当前语句并推进处理流程。 */
			report model.DeviceHealthReport /* 执行当前语句并推进处理流程。 */
			err    error                    /* 执行当前语句并推进处理流程。 */
		}{report, err} /* 结束当前表达式或代码块。 */
	}() /* 结束当前表达式或代码块。 */
	ticker := time.NewTicker(500 * time.Millisecond) /* 更新 ticker 的值。 */
	defer ticker.Stop()                              /* 安排函数结束时执行清理。 */
	for {                                            /* 循环处理当前数据。 */
		select { /* 根据条件选择处理路径。 */
		case result := <-resultCh: /* 处理当前分支。 */
			elapsed := time.Since(started).Milliseconds() /* 更新 elapsed 的值。 */
			s.updateHealthInspectionEstimate(elapsed)     /* 执行当前语句并推进处理流程。 */
			s.finishHealthInspectionJob(job, result.report, result.err)
			return /* 返回当前处理结果。 */
		case now := <-ticker.C: /* 处理当前分支。 */
			elapsed := now.Sub(started).Milliseconds()                  /* 更新 elapsed 的值。 */
			estimate := s.healthInspectionEstimate()                    /* 更新 estimate 的值。 */
			progress := 12 + int(float64(elapsed)/float64(estimate)*76) /* 更新 progress 的值。 */
			if progress > 88 {                                          /* 判断条件并选择处理分支。 */
				progress = 88 /* 更新 progress 的值。 */
			} /* 结束当前表达式或代码块。 */
			job.Stage, job.Message = "preparing", "正在准备设备健康快照" /* 更新 message 的值。 */
			if elapsed >= 1500 {                               /* 判断条件并选择处理分支。 */
				job.Stage, job.Message = "calling_model", "正在生成 AI 巡检建议" /* 更新 message 的值。 */
			} /* 结束当前表达式或代码块。 */
			job.Progress = progress
			job.EstimatedRemainingMs = maxInt64(0, estimate-elapsed)
			job.UpdatedAt = now.UnixMilli()
			if !s.storeRunningHealthInspectionJob(job) {
				// The stored job was marked interrupted; stop working on it.
				return
			}
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

// storeRunningHealthInspectionJob saves a progress heartbeat. A transient store
// error keeps the job running; it is only abandoned once marked interrupted.
func (s *Server) storeRunningHealthInspectionJob(job model.HealthInspectionJob) bool {
	ctx, cancel := context.WithTimeout(context.Background(), healthInspectionStoreTimeout)
	defer cancel()
	updated, err := s.engine.Repo.UpdateRunningHealthInspectionJob(ctx, job)
	if err != nil {
		if s.log != nil {
			s.log.Warn("save health inspection progress failed", "tenant", job.TenantID, "job", job.ID, "error", err)
		}
		return true
	}
	return updated
}

func (s *Server) finishHealthInspectionJob(job model.HealthInspectionJob, report model.DeviceHealthReport, err error) { /* 定义 finishHealthInspectionJob 函数。 */
	finished := time.Now().UnixMilli()
	job.UpdatedAt, job.FinishedAt, job.EstimatedRemainingMs, job.Progress = finished, finished, 0, 100
	job.Report = report
	if err != nil { /* 判断条件并选择处理分支。 */
		job.Status, job.Stage, job.Message = "failed", "failed", "智能巡检失败"
		job.Error = strings.TrimSpace(err.Error()) /* 更新 job.Error 的值。 */
	} else { /* 结束当前表达式或代码块。 */
		job.Status, job.Stage, job.Message = "succeeded", "completed", "智能巡检已完成"
	} /* 结束当前表达式或代码块。 */
	s.storeRunningHealthInspectionJob(job)
} /* 结束当前表达式或代码块。 */

func (s *Server) healthInspectionEstimate() int64 {
	s.healthInspectionMu.RLock()
	defer s.healthInspectionMu.RUnlock()
	if s.healthInspectionEstimateMs <= 0 {
		return healthInspectionEstimateDefault.Milliseconds()
	}
	return s.healthInspectionEstimateMs
}

func (s *Server) updateHealthInspectionEstimate(elapsed int64) { /* 定义 updateHealthInspectionEstimate 函数。 */
	if elapsed <= 0 { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	s.healthInspectionMu.Lock()             /* 执行当前语句并推进处理流程。 */
	defer s.healthInspectionMu.Unlock()     /* 安排函数结束时执行清理。 */
	current := s.healthInspectionEstimateMs /* 更新 current 的值。 */
	if current <= 0 {                       /* 判断条件并选择处理分支。 */
		current = healthInspectionEstimateDefault.Milliseconds() /* 更新 current 的值。 */
	} /* 结束当前表达式或代码块。 */
	updated := (current*3 + elapsed) / 4 /* 更新 updated 的值。 */
	if updated < 5000 {                  /* 判断条件并选择处理分支。 */
		updated = 5000 /* 更新 updated 的值。 */
	} /* 结束当前表达式或代码块。 */
	if updated > 180000 { /* 判断条件并选择处理分支。 */
		updated = 180000 /* 更新 updated 的值。 */
	} /* 结束当前表达式或代码块。 */
	s.healthInspectionEstimateMs = updated /* 更新 s.healthInspectionEstimateMs 的值。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) healthInspectionProgress(w http.ResponseWriter, r *http.Request) { /* 定义 healthInspectionProgress 函数。 */
	requestedJobID := strings.TrimSpace(r.PathValue("jobId"))                     /* 更新 requestedJobID 的值。 */
	job, found, err := s.loadHealthInspectionJob(r.Context(), claims(r).TenantID) /* 更新 job 的值。 */
	if err != nil {
		problem(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found || requestedJobID != "" && job.ID != requestedJobID { /* 判断条件并选择处理分支。 */
		problem(w, http.StatusNotFound, "智能巡检任务不存在或已过期") /* 执行当前语句并推进处理流程。 */
		return                                           /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	write(w, http.StatusOK, healthInspectionJobView(job)) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func healthInspectionJobView(job model.HealthInspectionJob) map[string]any { /* 定义 healthInspectionJobView 函数。 */
	view := map[string]any{ /* 更新 view 的值。 */
		"jobId":                job.ID,                   /* 执行当前语句并推进处理流程。 */
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
		view["report"] = job.Report /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if job.Error != "" { /* 判断条件并选择处理分支。 */
		view["error"] = job.Error /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	return view /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
