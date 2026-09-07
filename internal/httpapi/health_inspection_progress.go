package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"iot-platform/internal/model"
)

const (
	healthInspectionEstimateDefault = 45 * time.Second
	healthInspectionJobTTL          = 15 * time.Minute
)

type healthInspectionJob struct {
	ID                   string
	TenantID             string
	Actor                string
	Status               string
	Stage                string
	Message              string
	Progress             int
	EstimatedRemainingMs int64
	StartedAt            int64
	UpdatedAt            int64
	FinishedAt           int64
	Report               model.DeviceHealthReport
	Error                string
}

func (s *Server) runHealthInspection(w http.ResponseWriter, r *http.Request) {
	job := s.startHealthInspectionJob(claims(r).TenantID, claims(r).Username)
	write(w, http.StatusAccepted, healthInspectionJobView(job))
}

func (s *Server) startHealthInspectionJob(tenantID, actor string) *healthInspectionJob {
	now := time.Now()
	s.healthInspectionJobsMu.Lock()
	defer s.healthInspectionJobsMu.Unlock()
	if s.healthInspectionJobs == nil {
		s.healthInspectionJobs = make(map[string]*healthInspectionJob)
	}
	for key, job := range s.healthInspectionJobs {
		if job.Status != "running" && now.Sub(time.UnixMilli(job.UpdatedAt)) > healthInspectionJobTTL {
			delete(s.healthInspectionJobs, key)
		}
	}
	if existing := s.healthInspectionJobs[tenantID]; existing != nil && existing.Status == "running" {
		return cloneHealthInspectionJob(existing)
	}
	estimate := s.healthInspectionEstimateMs
	if estimate <= 0 {
		estimate = healthInspectionEstimateDefault.Milliseconds()
	}
	job := &healthInspectionJob{
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
	s.healthInspectionJobs[tenantID] = job
	go s.runHealthInspectionJob(tenantID, job)
	return cloneHealthInspectionJob(job)
}

func cloneHealthInspectionJob(job *healthInspectionJob) *healthInspectionJob {
	if job == nil {
		return nil
	}
	copy := *job
	copy.Report.Counts = make(map[string]int, len(job.Report.Counts))
	for key, value := range job.Report.Counts {
		copy.Report.Counts[key] = value
	}
	copy.Report.Items = make([]model.DeviceHealthItem, len(job.Report.Items))
	for index, item := range job.Report.Items {
		copy.Report.Items[index] = item
		copy.Report.Items[index].Findings = append([]string(nil), item.Findings...)
	}
	copy.Report.Warnings = append([]string(nil), job.Report.Warnings...)
	return &copy
}

func (s *Server) runHealthInspectionJob(key string, job *healthInspectionJob) {
	started := time.UnixMilli(job.StartedAt)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	resultCh := make(chan struct {
		report model.DeviceHealthReport
		err    error
	}, 1)
	go func() {
		report, err := s.engine.InspectDeviceHealth(ctx, job.TenantID)
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
			s.updateHealthInspectionEstimate(elapsed)
			if result.err != nil {
				s.finishHealthInspectionJob(key, job.ID, result.report, result.err)
				return
			}
			s.rememberHealthInspection(job.TenantID, result.report)
			s.finishHealthInspectionJob(key, job.ID, result.report, nil)
			return
		case now := <-ticker.C:
			elapsed := now.Sub(started).Milliseconds()
			s.healthInspectionJobsMu.RLock()
			estimate := s.healthInspectionEstimateMs
			s.healthInspectionJobsMu.RUnlock()
			if estimate <= 0 {
				estimate = healthInspectionEstimateDefault.Milliseconds()
			}
			progress := 12 + int(float64(elapsed)/float64(estimate)*76)
			if progress > 88 {
				progress = 88
			}
			stage, message := "preparing", "正在准备设备健康快照"
			if elapsed >= 1500 {
				stage, message = "calling_model", "正在生成 AI 巡检建议"
			}
			s.updateHealthInspectionJob(key, job.ID, func(current *healthInspectionJob) {
				current.Stage = stage
				current.Message = message
				current.Progress = progress
				current.EstimatedRemainingMs = maxInt64(0, estimate-elapsed)
			})
		}
	}
}

func (s *Server) updateHealthInspectionJob(key, jobID string, update func(*healthInspectionJob)) {
	s.healthInspectionJobsMu.Lock()
	defer s.healthInspectionJobsMu.Unlock()
	job := s.healthInspectionJobs[key]
	if job == nil || job.ID != jobID || job.Status != "running" {
		return
	}
	update(job)
	job.UpdatedAt = time.Now().UnixMilli()
}

func (s *Server) finishHealthInspectionJob(key, jobID string, report model.DeviceHealthReport, err error) {
	s.healthInspectionJobsMu.Lock()
	job := s.healthInspectionJobs[key]
	if job == nil || job.ID != jobID {
		s.healthInspectionJobsMu.Unlock()
		return
	}
	finished := time.Now()
	job.UpdatedAt = finished.UnixMilli()
	job.FinishedAt = finished.UnixMilli()
	job.EstimatedRemainingMs = 0
	job.Report = report
	if err != nil {
		job.Status = "failed"
		job.Stage = "failed"
		job.Progress = 100
		job.Message = "智能巡检失败"
		job.Error = strings.TrimSpace(err.Error())
	} else {
		job.Status = "succeeded"
		job.Stage = "completed"
		job.Progress = 100
		job.Message = "智能巡检已完成"
	}
	s.healthInspectionJobsMu.Unlock()
}

func (s *Server) updateHealthInspectionEstimate(elapsed int64) {
	if elapsed <= 0 {
		return
	}
	s.healthInspectionJobsMu.Lock()
	defer s.healthInspectionJobsMu.Unlock()
	current := s.healthInspectionEstimateMs
	if current <= 0 {
		current = healthInspectionEstimateDefault.Milliseconds()
	}
	updated := (current*3 + elapsed) / 4
	if updated < 5000 {
		updated = 5000
	}
	if updated > 180000 {
		updated = 180000
	}
	s.healthInspectionEstimateMs = updated
}

func (s *Server) healthInspectionProgress(w http.ResponseWriter, r *http.Request) {
	tenantID := claims(r).TenantID
	requestedJobID := strings.TrimSpace(r.PathValue("jobId"))
	s.healthInspectionJobsMu.RLock()
	job := cloneHealthInspectionJob(s.healthInspectionJobs[tenantID])
	s.healthInspectionJobsMu.RUnlock()
	if job == nil || requestedJobID != "" && job.ID != requestedJobID {
		problem(w, http.StatusNotFound, "智能巡检任务不存在或已过期")
		return
	}
	write(w, http.StatusOK, healthInspectionJobView(job))
}

func (s *Server) currentHealthInspectionJob(tenantID string) *healthInspectionJob {
	s.healthInspectionJobsMu.RLock()
	job := cloneHealthInspectionJob(s.healthInspectionJobs[tenantID])
	s.healthInspectionJobsMu.RUnlock()
	return job
}

func healthInspectionJobView(job *healthInspectionJob) map[string]any {
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
