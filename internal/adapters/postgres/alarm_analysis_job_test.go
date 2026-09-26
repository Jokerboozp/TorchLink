package postgres

import (
	"context"
	"testing"

	"iot-platform/internal/model"
)

func TestAlarmAnalysisJobStore(t *testing.T) {
	ctx := context.Background()
	r := testRepository(t)

	running := model.AlarmAnalysisJob{ID: "job-1", TenantID: "t1", AlarmID: "a1", Status: "running", StartedAt: 1000, UpdatedAt: 1000}
	if created, createErr := r.CreateAlarmAnalysisJob(ctx, running); createErr != nil || !created {
		t.Fatalf("create: %v %v", created, createErr)
	}
	// One running job per alarm and scope; another scope runs independently.
	if created, _ := r.CreateAlarmAnalysisJob(ctx, model.AlarmAnalysisJob{ID: "job-2", TenantID: "t1", AlarmID: "a1", Status: "running", StartedAt: 2000}); created {
		t.Fatal("a second running job for the same alarm and scope was created")
	}
	if created, createErr := r.CreateAlarmAnalysisJob(ctx, model.AlarmAnalysisJob{ID: "job-k", TenantID: "t1", AlarmID: "a1", KnowledgeScope: model.AlarmAnalysisWorkflowID, Status: "running", StartedAt: 2000}); createErr != nil || !created {
		t.Fatalf("knowledge scope must run separately: %v %v", created, createErr)
	}
	running.Status, running.Progress, running.UpdatedAt = "succeeded", 100, 3000
	running.Analysis = model.AIAnalysis{Summary: "研判完成"}
	if updated, updateErr := r.UpdateRunningAlarmAnalysisJob(ctx, running); updateErr != nil || !updated {
		t.Fatalf("finish: %v %v", updated, updateErr)
	}
	if updated, _ := r.UpdateRunningAlarmAnalysisJob(ctx, running); updated {
		t.Fatal("a finished job must not be updated again")
	}
	latest, err := r.LatestAlarmAnalysisJob(ctx, "t1", "a1", "")
	if err != nil || latest.ID != "job-1" || latest.Analysis.Summary != "研判完成" {
		t.Fatalf("latest: %#v %v", latest, err)
	}
	// A new run replaces the finished job of the same alarm and scope.
	if created, createErr := r.CreateAlarmAnalysisJob(ctx, model.AlarmAnalysisJob{ID: "job-3", TenantID: "t1", AlarmID: "a1", Status: "running", StartedAt: 4000, UpdatedAt: 4000}); createErr != nil || !created {
		t.Fatalf("rerun: %v %v", created, createErr)
	}
	var rows int
	if err = r.pool.QueryRow(ctx, `SELECT count(*) FROM alarm_analysis_job WHERE tenant_id='t1' AND alarm_id='a1' AND knowledge_scope=''`).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("finished jobs must be replaced: rows=%d err=%v", rows, err)
	}
	if _, err = r.LatestAlarmAnalysisJob(ctx, "t1", "missing", ""); err != ErrNotFound {
		t.Fatalf("missing job: %v", err)
	}
}
