package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"  /* 执行当前语句并推进处理流程。 */
	"net/http" /* 执行当前语句并推进处理流程。 */
	"strings"  /* 执行当前语句并推进处理流程。 */
	"time"     /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

const ( /* 执行当前语句并推进处理流程。 */
	aiAnalysisEstimateDefault = 45 * time.Second /* 更新 aiAnalysisEstimateDefault 的值。 */
	aiAnalysisJobTTL          = 15 * time.Minute /* 更新 aiAnalysisJobTTL 的值。 */
) /* 结束当前表达式或代码块。 */

type aiAnalysisJob struct { /* 定义 aiAnalysisJob 类型。 */
	ID                   string           /* 执行当前语句并推进处理流程。 */
	TenantID             string           /* 执行当前语句并推进处理流程。 */
	AlarmID              string           /* 执行当前语句并推进处理流程。 */
	Actor                string           /* 执行当前语句并推进处理流程。 */
	Status               string           /* 执行当前语句并推进处理流程。 */
	Stage                string           /* 执行当前语句并推进处理流程。 */
	Message              string           /* 执行当前语句并推进处理流程。 */
	Progress             int              /* 执行当前语句并推进处理流程。 */
	EstimatedRemainingMs int64            /* 执行当前语句并推进处理流程。 */
	StartedAt            int64            /* 执行当前语句并推进处理流程。 */
	UpdatedAt            int64            /* 执行当前语句并推进处理流程。 */
	FinishedAt           int64            /* 执行当前语句并推进处理流程。 */
	Analysis             model.AIAnalysis /* 执行当前语句并推进处理流程。 */
	Error                string           /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) startAIAnalysisJob(tenantID, alarmID, actor string) *aiAnalysisJob { /* 定义 startAIAnalysisJob 函数。 */
	now := time.Now()                     /* 更新 now 的值。 */
	key := alarmJobKey(tenantID, alarmID) /* 更新 key 的值。 */
	s.aiAnalysisMu.Lock()                 /* 执行当前语句并推进处理流程。 */
	defer s.aiAnalysisMu.Unlock()         /* 安排函数结束时执行清理。 */
	if s.aiAnalysisJobs == nil {          /* 判断条件并选择处理分支。 */
		s.aiAnalysisJobs = make(map[string]*aiAnalysisJob) /* 更新 s.aiAnalysisJobs 的值。 */
	} /* 结束当前表达式或代码块。 */
	for existingKey, job := range s.aiAnalysisJobs { /* 循环处理当前数据。 */
		if job.Status != "running" && now.Sub(time.UnixMilli(job.UpdatedAt)) > aiAnalysisJobTTL { /* 判断条件并选择处理分支。 */
			delete(s.aiAnalysisJobs, existingKey) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if existing := s.aiAnalysisJobs[key]; existing != nil && existing.Status == "running" { /* 判断条件并选择处理分支。 */
		return cloneAIAnalysisJob(existing) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	estimate := s.aiAnalysisEstimateMs /* 更新 estimate 的值。 */
	if estimate <= 0 {                 /* 判断条件并选择处理分支。 */
		estimate = aiAnalysisEstimateDefault.Milliseconds() /* 更新 estimate 的值。 */
	} /* 结束当前表达式或代码块。 */
	job := &aiAnalysisJob{ /* 更新 job 的值。 */
		ID:                   "ai_job_" + randomHex(10), /* 执行当前语句并推进处理流程。 */
		TenantID:             tenantID,                  /* 执行当前语句并推进处理流程。 */
		AlarmID:              alarmID,                   /* 执行当前语句并推进处理流程。 */
		Actor:                actor,                     /* 执行当前语句并推进处理流程。 */
		Status:               "running",                 /* 执行当前语句并推进处理流程。 */
		Stage:                "preparing",               /* 执行当前语句并推进处理流程。 */
		Message:              "正在准备告警上下文",               /* 执行当前语句并推进处理流程。 */
		Progress:             8,                         /* 执行当前语句并推进处理流程。 */
		EstimatedRemainingMs: estimate,                  /* 执行当前语句并推进处理流程。 */
		StartedAt:            now.UnixMilli(),           /* 执行当前语句并推进处理流程。 */
		UpdatedAt:            now.UnixMilli(),           /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	s.aiAnalysisJobs[key] = job     /* 更新 s.aiAnalysisJobs[key] 的值。 */
	go s.runAIAnalysisJob(key, job) /* 执行当前语句并推进处理流程。 */
	return cloneAIAnalysisJob(job)  /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func alarmJobKey(tenantID, alarmID string) string { return tenantID + "\x00" + alarmID } /* 定义 alarmJobKey 函数。 */

func cloneAIAnalysisJob(job *aiAnalysisJob) *aiAnalysisJob { /* 定义 cloneAIAnalysisJob 函数。 */
	if job == nil { /* 判断条件并选择处理分支。 */
		return nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	copy := *job                                                                           /* 更新 copy 的值。 */
	copy.Analysis.PossibleReasons = append([]string(nil), job.Analysis.PossibleReasons...) /* 更新 copy.Analysis.PossibleReasons 的值。 */
	copy.Analysis.Suggestions = append([]string(nil), job.Analysis.Suggestions...)         /* 更新 copy.Analysis.Suggestions 的值。 */
	return &copy                                                                           /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// runAIAnalysisJob performs the actual work. It is split from the starter so
// tests and the progress updater can keep the tenant/alarm identity explicit.
func (s *Server) runAIAnalysisJob(key string, job *aiAnalysisJob) { /* 定义 runAIAnalysisJob 函数。 */
	started := time.UnixMilli(job.StartedAt)                                /* 更新 started 的值。 */
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute) /* 更新 cancel 的值。 */
	defer cancel()                                                          /* 安排函数结束时执行清理。 */
	resultCh := make(chan struct {                                          /* 更新 resultCh 的值。 */
		analysis model.AIAnalysis /* 执行当前语句并推进处理流程。 */
		err      error            /* 执行当前语句并推进处理流程。 */
	}, 1) /* 结束当前表达式或代码块。 */
	go func() { /* 执行当前语句并推进处理流程。 */
		analysis, err := s.engine.AnalyzeAlarm(ctx, job.TenantID, job.AlarmID) /* 更新 err 的值。 */
		resultCh <- struct {                                                   /* 执行当前语句并推进处理流程。 */
			analysis model.AIAnalysis /* 执行当前语句并推进处理流程。 */
			err      error            /* 执行当前语句并推进处理流程。 */
		}{analysis, err} /* 结束当前表达式或代码块。 */
	}() /* 结束当前表达式或代码块。 */
	ticker := time.NewTicker(500 * time.Millisecond) /* 更新 ticker 的值。 */
	defer ticker.Stop()                              /* 安排函数结束时执行清理。 */
	for {                                            /* 循环处理当前数据。 */
		select { /* 根据条件选择处理路径。 */
		case result := <-resultCh: /* 处理当前分支。 */
			elapsed := time.Since(started).Milliseconds() /* 更新 elapsed 的值。 */
			s.updateAIAnalysisEstimate(elapsed)           /* 执行当前语句并推进处理流程。 */
			if result.err != nil {                        /* 判断条件并选择处理分支。 */
				s.finishAIAnalysisJob(key, job.ID, result.analysis, result.err) /* 执行当前语句并推进处理流程。 */
			} else { /* 结束当前表达式或代码块。 */
				s.finishAIAnalysisJob(key, job.ID, result.analysis, nil) /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			return /* 返回当前处理结果。 */
		case now := <-ticker.C: /* 处理当前分支。 */
			elapsed := now.Sub(started).Milliseconds() /* 更新 elapsed 的值。 */
			s.aiAnalysisMu.RLock()                     /* 执行当前语句并推进处理流程。 */
			estimate := s.aiAnalysisEstimateMs         /* 更新 estimate 的值。 */
			s.aiAnalysisMu.RUnlock()                   /* 执行当前语句并推进处理流程。 */
			if estimate <= 0 {                         /* 判断条件并选择处理分支。 */
				estimate = aiAnalysisEstimateDefault.Milliseconds() /* 更新 estimate 的值。 */
			} /* 结束当前表达式或代码块。 */
			progress := 12 + int(float64(elapsed)/float64(estimate)*76) /* 更新 progress 的值。 */
			if progress > 88 {                                          /* 判断条件并选择处理分支。 */
				progress = 88 /* 更新 progress 的值。 */
			} /* 结束当前表达式或代码块。 */
			stage, message := "preparing", "正在准备告警上下文" /* 更新 message 的值。 */
			if elapsed >= 1500 {                       /* 判断条件并选择处理分支。 */
				stage, message = "calling_model", "正在调用 AI 模型" /* 更新 message 的值。 */
			} /* 结束当前表达式或代码块。 */
			s.updateAIAnalysisJob(key, job.ID, func(current *aiAnalysisJob) { /* 执行当前语句并推进处理流程。 */
				current.Stage = stage                                        /* 更新 current.Stage 的值。 */
				current.Message = message                                    /* 更新 current.Message 的值。 */
				current.Progress = progress                                  /* 更新 current.Progress 的值。 */
				current.EstimatedRemainingMs = maxInt64(0, estimate-elapsed) /* 更新 current.EstimatedRemainingMs 的值。 */
			}) /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) updateAIAnalysisJob(key, jobID string, update func(*aiAnalysisJob)) { /* 定义 updateAIAnalysisJob 函数。 */
	s.aiAnalysisMu.Lock()                                         /* 执行当前语句并推进处理流程。 */
	defer s.aiAnalysisMu.Unlock()                                 /* 安排函数结束时执行清理。 */
	job := s.aiAnalysisJobs[key]                                  /* 更新 job 的值。 */
	if job == nil || job.ID != jobID || job.Status != "running" { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	update(job)                            /* 执行当前语句并推进处理流程。 */
	job.UpdatedAt = time.Now().UnixMilli() /* 更新 job.UpdatedAt 的值。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) finishAIAnalysisJob(key, jobID string, analysis model.AIAnalysis, err error) { /* 定义 finishAIAnalysisJob 函数。 */
	s.aiAnalysisMu.Lock()              /* 执行当前语句并推进处理流程。 */
	job := s.aiAnalysisJobs[key]       /* 更新 job 的值。 */
	if job == nil || job.ID != jobID { /* 判断条件并选择处理分支。 */
		s.aiAnalysisMu.Unlock() /* 执行当前语句并推进处理流程。 */
		return                  /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	finished := time.Now()                /* 更新 finished 的值。 */
	job.UpdatedAt = finished.UnixMilli()  /* 更新 job.UpdatedAt 的值。 */
	job.FinishedAt = finished.UnixMilli() /* 更新 job.FinishedAt 的值。 */
	job.EstimatedRemainingMs = 0          /* 更新 job.EstimatedRemainingMs 的值。 */
	job.Analysis = analysis               /* 更新 job.Analysis 的值。 */
	if err != nil {                       /* 判断条件并选择处理分支。 */
		job.Status = "failed"                      /* 更新 job.Status 的值。 */
		job.Stage = "failed"                       /* 更新 job.Stage 的值。 */
		job.Progress = 100                         /* 更新 job.Progress 的值。 */
		job.Message = "AI 研判失败"                    /* 更新 job.Message 的值。 */
		job.Error = strings.TrimSpace(err.Error()) /* 更新 job.Error 的值。 */
	} else { /* 结束当前表达式或代码块。 */
		job.Status = "succeeded" /* 更新 job.Status 的值。 */
		job.Stage = "completed"  /* 更新 job.Stage 的值。 */
		job.Progress = 100       /* 更新 job.Progress 的值。 */
		job.Message = "AI 研判已完成" /* 更新 job.Message 的值。 */
	} /* 结束当前表达式或代码块。 */
	copy := cloneAIAnalysisJob(job) /* 更新 copy 的值。 */
	s.aiAnalysisMu.Unlock()         /* 执行当前语句并推进处理流程。 */

	if err == nil { /* 判断条件并选择处理分支。 */
		_ = s.engine.Repo.SaveAudit(context.Background(), model.AuditLog{ /* 更新 _ 的值。 */
			ID:         "audit_" + randomHex(10),                         /* 执行当前语句并推进处理流程。 */
			TenantID:   copy.TenantID,                                    /* 执行当前语句并推进处理流程。 */
			Actor:      copy.Actor,                                       /* 执行当前语句并推进处理流程。 */
			Action:     "ai.alarm-analysis.run",                          /* 执行当前语句并推进处理流程。 */
			TargetType: "alarm",                                          /* 执行当前语句并推进处理流程。 */
			TargetID:   copy.AlarmID,                                     /* 执行当前语句并推进处理流程。 */
			Details:    map[string]any{"manual": true, "jobId": copy.ID}, /* 执行当前语句并推进处理流程。 */
			CreatedAt:  copy.FinishedAt,                                  /* 执行当前语句并推进处理流程。 */
		}) /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

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
	key := alarmJobKey(claims(r).TenantID, r.PathValue("alarmId"))      /* 更新 key 的值。 */
	requestedJobID := strings.TrimSpace(r.PathValue("jobId"))           /* 更新 requestedJobID 的值。 */
	s.aiAnalysisMu.RLock()                                              /* 执行当前语句并推进处理流程。 */
	job := cloneAIAnalysisJob(s.aiAnalysisJobs[key])                    /* 更新 job 的值。 */
	s.aiAnalysisMu.RUnlock()                                            /* 执行当前语句并推进处理流程。 */
	if job == nil || requestedJobID != "" && job.ID != requestedJobID { /* 判断条件并选择处理分支。 */
		problem(w, http.StatusNotFound, "AI 研判任务不存在或已过期") /* 执行当前语句并推进处理流程。 */
		return                                            /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	write(w, http.StatusOK, aiAnalysisJobView(job)) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func aiAnalysisJobView(job *aiAnalysisJob) map[string]any { /* 定义 aiAnalysisJobView 函数。 */
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
