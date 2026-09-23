package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"  /* 执行当前语句并推进处理流程。 */
	"net/http" /* 执行当前语句并推进处理流程。 */
	"strings"  /* 执行当前语句并推进处理流程。 */
	"time"     /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

const ( /* 执行当前语句并推进处理流程。 */
	healthInspectionEstimateDefault = 45 * time.Second /* 更新 healthInspectionEstimateDefault 的值。 */
	healthInspectionJobTTL          = 15 * time.Minute /* 更新 healthInspectionJobTTL 的值。 */
) /* 结束当前表达式或代码块。 */

type healthInspectionJob struct { /* 定义 healthInspectionJob 类型。 */
	ID                   string                   /* 执行当前语句并推进处理流程。 */
	TenantID             string                   /* 执行当前语句并推进处理流程。 */
	Actor                string                   /* 执行当前语句并推进处理流程。 */
	Status               string                   /* 执行当前语句并推进处理流程。 */
	Stage                string                   /* 执行当前语句并推进处理流程。 */
	Message              string                   /* 执行当前语句并推进处理流程。 */
	Progress             int                      /* 执行当前语句并推进处理流程。 */
	EstimatedRemainingMs int64                    /* 执行当前语句并推进处理流程。 */
	StartedAt            int64                    /* 执行当前语句并推进处理流程。 */
	UpdatedAt            int64                    /* 执行当前语句并推进处理流程。 */
	FinishedAt           int64                    /* 执行当前语句并推进处理流程。 */
	Report               model.DeviceHealthReport /* 执行当前语句并推进处理流程。 */
	Error                string                   /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) runHealthInspection(w http.ResponseWriter, r *http.Request) { /* 定义 runHealthInspection 函数。 */
	job := s.startHealthInspectionJob(claims(r).TenantID, claims(r).Username) /* 更新 job 的值。 */
	write(w, http.StatusAccepted, healthInspectionJobView(job))               /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) startHealthInspectionJob(tenantID, actor string) *healthInspectionJob { /* 定义 startHealthInspectionJob 函数。 */
	now := time.Now()                       /* 更新 now 的值。 */
	s.healthInspectionJobsMu.Lock()         /* 执行当前语句并推进处理流程。 */
	defer s.healthInspectionJobsMu.Unlock() /* 安排函数结束时执行清理。 */
	if s.healthInspectionJobs == nil {      /* 判断条件并选择处理分支。 */
		s.healthInspectionJobs = make(map[string]*healthInspectionJob) /* 更新 s.healthInspectionJobs 的值。 */
	} /* 结束当前表达式或代码块。 */
	for key, job := range s.healthInspectionJobs { /* 循环处理当前数据。 */
		if job.Status != "running" && now.Sub(time.UnixMilli(job.UpdatedAt)) > healthInspectionJobTTL { /* 判断条件并选择处理分支。 */
			delete(s.healthInspectionJobs, key) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if existing := s.healthInspectionJobs[tenantID]; existing != nil && existing.Status == "running" { /* 判断条件并选择处理分支。 */
		return cloneHealthInspectionJob(existing) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	estimate := s.healthInspectionEstimateMs /* 更新 estimate 的值。 */
	if estimate <= 0 {                       /* 判断条件并选择处理分支。 */
		estimate = healthInspectionEstimateDefault.Milliseconds() /* 更新 estimate 的值。 */
	} /* 结束当前表达式或代码块。 */
	job := &healthInspectionJob{ /* 更新 job 的值。 */
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
	s.healthInspectionJobs[tenantID] = job     /* 更新 s.healthInspectionJobs[tenantID] 的值。 */
	go s.runHealthInspectionJob(tenantID, job) /* 执行当前语句并推进处理流程。 */
	return cloneHealthInspectionJob(job)       /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func cloneHealthInspectionJob(job *healthInspectionJob) *healthInspectionJob { /* 定义 cloneHealthInspectionJob 函数。 */
	if job == nil { /* 判断条件并选择处理分支。 */
		return nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	copy := *job                                                      /* 更新 copy 的值。 */
	copy.Report.Counts = make(map[string]int, len(job.Report.Counts)) /* 更新 copy.Report.Counts 的值。 */
	for key, value := range job.Report.Counts {                       /* 循环处理当前数据。 */
		copy.Report.Counts[key] = value /* 更新 copy.Report.Counts[key] 的值。 */
	} /* 结束当前表达式或代码块。 */
	copy.Report.Items = make([]model.DeviceHealthItem, len(job.Report.Items)) /* 更新 copy.Report.Items 的值。 */
	for index, item := range job.Report.Items {                               /* 循环处理当前数据。 */
		copy.Report.Items[index] = item                                             /* 更新 copy.Report.Items[index] 的值。 */
		copy.Report.Items[index].Findings = append([]string(nil), item.Findings...) /* 更新 copy.Report.Items[index].Findings 的值。 */
	} /* 结束当前表达式或代码块。 */
	copy.Report.Warnings = append([]string(nil), job.Report.Warnings...) /* 更新 copy.Report.Warnings 的值。 */
	return &copy                                                         /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) runHealthInspectionJob(key string, job *healthInspectionJob) { /* 定义 runHealthInspectionJob 函数。 */
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
			if result.err != nil {                        /* 判断条件并选择处理分支。 */
				s.finishHealthInspectionJob(key, job.ID, result.report, result.err) /* 执行当前语句并推进处理流程。 */
				return                                                              /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			s.rememberHealthInspection(job.TenantID, result.report)      /* 执行当前语句并推进处理流程。 */
			s.finishHealthInspectionJob(key, job.ID, result.report, nil) /* 执行当前语句并推进处理流程。 */
			return                                                       /* 返回当前处理结果。 */
		case now := <-ticker.C: /* 处理当前分支。 */
			elapsed := now.Sub(started).Milliseconds() /* 更新 elapsed 的值。 */
			s.healthInspectionJobsMu.RLock()           /* 执行当前语句并推进处理流程。 */
			estimate := s.healthInspectionEstimateMs   /* 更新 estimate 的值。 */
			s.healthInspectionJobsMu.RUnlock()         /* 执行当前语句并推进处理流程。 */
			if estimate <= 0 {                         /* 判断条件并选择处理分支。 */
				estimate = healthInspectionEstimateDefault.Milliseconds() /* 更新 estimate 的值。 */
			} /* 结束当前表达式或代码块。 */
			progress := 12 + int(float64(elapsed)/float64(estimate)*76) /* 更新 progress 的值。 */
			if progress > 88 {                                          /* 判断条件并选择处理分支。 */
				progress = 88 /* 更新 progress 的值。 */
			} /* 结束当前表达式或代码块。 */
			stage, message := "preparing", "正在准备设备健康快照" /* 更新 message 的值。 */
			if elapsed >= 1500 {                        /* 判断条件并选择处理分支。 */
				stage, message = "calling_model", "正在生成 AI 巡检建议" /* 更新 message 的值。 */
			} /* 结束当前表达式或代码块。 */
			s.updateHealthInspectionJob(key, job.ID, func(current *healthInspectionJob) { /* 执行当前语句并推进处理流程。 */
				current.Stage = stage                                        /* 更新 current.Stage 的值。 */
				current.Message = message                                    /* 更新 current.Message 的值。 */
				current.Progress = progress                                  /* 更新 current.Progress 的值。 */
				current.EstimatedRemainingMs = maxInt64(0, estimate-elapsed) /* 更新 current.EstimatedRemainingMs 的值。 */
			}) /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) updateHealthInspectionJob(key, jobID string, update func(*healthInspectionJob)) { /* 定义 updateHealthInspectionJob 函数。 */
	s.healthInspectionJobsMu.Lock()                               /* 执行当前语句并推进处理流程。 */
	defer s.healthInspectionJobsMu.Unlock()                       /* 安排函数结束时执行清理。 */
	job := s.healthInspectionJobs[key]                            /* 更新 job 的值。 */
	if job == nil || job.ID != jobID || job.Status != "running" { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	update(job)                            /* 执行当前语句并推进处理流程。 */
	job.UpdatedAt = time.Now().UnixMilli() /* 更新 job.UpdatedAt 的值。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) finishHealthInspectionJob(key, jobID string, report model.DeviceHealthReport, err error) { /* 定义 finishHealthInspectionJob 函数。 */
	s.healthInspectionJobsMu.Lock()    /* 执行当前语句并推进处理流程。 */
	job := s.healthInspectionJobs[key] /* 更新 job 的值。 */
	if job == nil || job.ID != jobID { /* 判断条件并选择处理分支。 */
		s.healthInspectionJobsMu.Unlock() /* 执行当前语句并推进处理流程。 */
		return                            /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	finished := time.Now()                /* 更新 finished 的值。 */
	job.UpdatedAt = finished.UnixMilli()  /* 更新 job.UpdatedAt 的值。 */
	job.FinishedAt = finished.UnixMilli() /* 更新 job.FinishedAt 的值。 */
	job.EstimatedRemainingMs = 0          /* 更新 job.EstimatedRemainingMs 的值。 */
	job.Report = report                   /* 更新 job.Report 的值。 */
	if err != nil {                       /* 判断条件并选择处理分支。 */
		job.Status = "failed"                      /* 更新 job.Status 的值。 */
		job.Stage = "failed"                       /* 更新 job.Stage 的值。 */
		job.Progress = 100                         /* 更新 job.Progress 的值。 */
		job.Message = "智能巡检失败"                     /* 更新 job.Message 的值。 */
		job.Error = strings.TrimSpace(err.Error()) /* 更新 job.Error 的值。 */
	} else { /* 结束当前表达式或代码块。 */
		job.Status = "succeeded" /* 更新 job.Status 的值。 */
		job.Stage = "completed"  /* 更新 job.Stage 的值。 */
		job.Progress = 100       /* 更新 job.Progress 的值。 */
		job.Message = "智能巡检已完成"  /* 更新 job.Message 的值。 */
	} /* 结束当前表达式或代码块。 */
	s.healthInspectionJobsMu.Unlock() /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) updateHealthInspectionEstimate(elapsed int64) { /* 定义 updateHealthInspectionEstimate 函数。 */
	if elapsed <= 0 { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	s.healthInspectionJobsMu.Lock()         /* 执行当前语句并推进处理流程。 */
	defer s.healthInspectionJobsMu.Unlock() /* 安排函数结束时执行清理。 */
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
	tenantID := claims(r).TenantID                                      /* 更新 tenantID 的值。 */
	requestedJobID := strings.TrimSpace(r.PathValue("jobId"))           /* 更新 requestedJobID 的值。 */
	s.healthInspectionJobsMu.RLock()                                    /* 执行当前语句并推进处理流程。 */
	job := cloneHealthInspectionJob(s.healthInspectionJobs[tenantID])   /* 更新 job 的值。 */
	s.healthInspectionJobsMu.RUnlock()                                  /* 执行当前语句并推进处理流程。 */
	if job == nil || requestedJobID != "" && job.ID != requestedJobID { /* 判断条件并选择处理分支。 */
		problem(w, http.StatusNotFound, "智能巡检任务不存在或已过期") /* 执行当前语句并推进处理流程。 */
		return                                           /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	write(w, http.StatusOK, healthInspectionJobView(job)) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) currentHealthInspectionJob(tenantID string) *healthInspectionJob { /* 定义 currentHealthInspectionJob 函数。 */
	s.healthInspectionJobsMu.RLock()                                  /* 执行当前语句并推进处理流程。 */
	job := cloneHealthInspectionJob(s.healthInspectionJobs[tenantID]) /* 更新 job 的值。 */
	s.healthInspectionJobsMu.RUnlock()                                /* 执行当前语句并推进处理流程。 */
	return job                                                        /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func healthInspectionJobView(job *healthInspectionJob) map[string]any { /* 定义 healthInspectionJobView 函数。 */
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
