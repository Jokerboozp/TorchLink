package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"iot-platform/internal/model"
)

const (
	aiAnalysisEstimateDefault = 45 * time.Second
	aiAnalysisJobTTL          = 15 * time.Minute
)

type aiAnalysisJob struct {
	ID                   string
	TenantID             string
	AlarmID              string
	Actor                string
	Status               string
	Stage                string
	Message              string
	Progress             int
	EstimatedRemainingMs int64
	StartedAt            int64
	UpdatedAt            int64
	FinishedAt           int64
	Analysis             model.AIAnalysis
	Error                string
}

func (s *Server) startAIAnalysisJob(tenantID, alarmID, actor string) *aiAnalysisJob {
	now := time.Now()
	key := alarmJobKey(tenantID, alarmID)
	s.aiAnalysisMu.Lock()
	defer s.aiAnalysisMu.Unlock()
	if s.aiAnalysisJobs == nil {
		s.aiAnalysisJobs = make(map[string]*aiAnalysisJob)
	}
	for existingKey, job := range s.aiAnalysisJobs {
		if job.Status != "running" && now.Sub(time.UnixMilli(job.UpdatedAt)) > aiAnalysisJobTTL {
			delete(s.aiAnalysisJobs, existingKey)
		}
	}
	if existing := s.aiAnalysisJobs[key]; existing != nil && existing.Status == "running" {
		return cloneAIAnalysisJob(existing)
	}
	estimate := s.aiAnalysisEstimateMs
	if estimate <= 0 {
		estimate = aiAnalysisEstimateDefault.Milliseconds()
	}
	job := &aiAnalysisJob{
		ID:                   "ai_job_" + randomHex(10),
		TenantID:             tenantID,
		AlarmID:              alarmID,
		Actor:                actor,
		Status:               "running",
		Stage:                "preparing",
		Message:              "正在准备告警上下文",
		Progress:             8,
		EstimatedRemainingMs: estimate,
		StartedAt:            now.UnixMilli(),
		UpdatedAt:            now.UnixMilli(),
	}
	s.aiAnalysisJobs[key] = job
	go s.runAIAnalysisJob(key, job)
	return cloneAIAnalysisJob(job)
}

func alarmJobKey(tenantID, alarmID string) string { return tenantID + "\x00" + alarmID }

func cloneAIAnalysisJob(job *aiAnalysisJob) *aiAnalysisJob {
	if job == nil {
		return nil
	}
	copy := *job
	copy.Analysis.PossibleReasons = append([]string(nil), job.Analysis.PossibleReasons...)
	copy.Analysis.Suggestions = append([]string(nil), job.Analysis.Suggestions...)
	return &copy
}

// runAIAnalysisJob performs the actual work. It is split from the starter so
// tests and the progress updater can keep the tenant/alarm identity explicit.
func (s *Server) runAIAnalysisJob(key string, job *aiAnalysisJob) {
	started := time.UnixMilli(job.StartedAt)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	resultCh := make(chan struct {
		analysis model.AIAnalysis
		err      error
	}, 1)
	go func() {
		analysis, err := s.engine.AnalyzeAlarm(ctx, job.TenantID, job.AlarmID)
		resultCh <- struct {
			analysis model.AIAnalysis
			err      error
		}{analysis, err}
	}()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case result := <-resultCh:
			elapsed := time.Since(started).Milliseconds()
			s.updateAIAnalysisEstimate(elapsed)
			if result.err != nil {
				s.finishAIAnalysisJob(key, job.ID, result.analysis, result.err)
			} else {
				s.finishAIAnalysisJob(key, job.ID, result.analysis, nil)
			}
			return
		case now := <-ticker.C:
			elapsed := now.Sub(started).Milliseconds()
			s.aiAnalysisMu.RLock()
			estimate := s.aiAnalysisEstimateMs
			s.aiAnalysisMu.RUnlock()
			if estimate <= 0 {
				estimate = aiAnalysisEstimateDefault.Milliseconds()
			}
			progress := 12 + int(float64(elapsed)/float64(estimate)*76)
			if progress > 88 {
				progress = 88
			}
			stage, message := "preparing", "正在准备告警上下文"
			if elapsed >= 1500 {
				stage, message = "calling_model", "正在调用 AI 模型"
			}
			s.updateAIAnalysisJob(key, job.ID, func(current *aiAnalysisJob) {
				current.Stage = stage
				current.Message = message
				current.Progress = progress
				current.EstimatedRemainingMs = maxInt64(0, estimate-elapsed)
			})
		}
	}
}

func (s *Server) updateAIAnalysisJob(key, jobID string, update func(*aiAnalysisJob)) {
	s.aiAnalysisMu.Lock()
	defer s.aiAnalysisMu.Unlock()
	job := s.aiAnalysisJobs[key]
	if job == nil || job.ID != jobID || job.Status != "running" {
		return
	}
	update(job)
	job.UpdatedAt = time.Now().UnixMilli()
}

func (s *Server) finishAIAnalysisJob(key, jobID string, analysis model.AIAnalysis, err error) {
	s.aiAnalysisMu.Lock()
	job := s.aiAnalysisJobs[key]
	if job == nil || job.ID != jobID {
		s.aiAnalysisMu.Unlock()
		return
	}
	finished := time.Now()
	job.UpdatedAt = finished.UnixMilli()
	job.FinishedAt = finished.UnixMilli()
	job.EstimatedRemainingMs = 0
	job.Analysis = analysis
	if err != nil {
		job.Status = "failed"
		job.Stage = "failed"
		job.Progress = 100
		job.Message = "AI 研判失败"
		job.Error = strings.TrimSpace(err.Error())
	} else {
		job.Status = "succeeded"
		job.Stage = "completed"
		job.Progress = 100
		job.Message = "AI 研判已完成"
	}
	copy := cloneAIAnalysisJob(job)
	s.aiAnalysisMu.Unlock()

	if err == nil {
		_ = s.engine.Repo.SaveAudit(context.Background(), model.AuditLog{
			ID:         "audit_" + randomHex(10),
			TenantID:   copy.TenantID,
			Actor:      copy.Actor,
			Action:     "ai.alarm-analysis.run",
			TargetType: "alarm",
			TargetID:   copy.AlarmID,
			Details:    map[string]any{"manual": true, "jobId": copy.ID},
			CreatedAt:  copy.FinishedAt,
		})
	}
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
	key := alarmJobKey(claims(r).TenantID, r.PathValue("alarmId"))
	requestedJobID := strings.TrimSpace(r.PathValue("jobId"))
	s.aiAnalysisMu.RLock()
	job := cloneAIAnalysisJob(s.aiAnalysisJobs[key])
	s.aiAnalysisMu.RUnlock()
	if job == nil || requestedJobID != "" && job.ID != requestedJobID {
		problem(w, http.StatusNotFound, "AI 研判任务不存在或已过期")
		return
	}
	write(w, http.StatusOK, aiAnalysisJobView(job))
}

func aiAnalysisJobView(job *aiAnalysisJob) map[string]any {
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
