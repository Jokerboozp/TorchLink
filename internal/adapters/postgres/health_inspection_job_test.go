package postgres

import (
	"context"
	"testing"

	"iot-platform/internal/model"
)

// Set IOT_TEST_POSTGRES_DSN to run against a disposable PostgreSQL database.
func TestHealthInspectionJobStore(t *testing.T) {
	ctx := context.Background()
	r := testRepository(t)

	running := model.HealthInspectionJob{ID: "job-1", TenantID: "tenant-a", Status: "running", Progress: 8, StartedAt: 1000, UpdatedAt: 1000}
	if created, createErr := r.CreateHealthInspectionJob(ctx, running); createErr != nil || !created {
		t.Fatalf("first running job not created: created=%v err=%v", created, createErr)
	}
	if created, createErr := r.CreateHealthInspectionJob(ctx, model.HealthInspectionJob{ID: "job-2", TenantID: "tenant-a", Status: "running", StartedAt: 2000, UpdatedAt: 2000}); createErr != nil || created {
		t.Fatalf("a second running job for the tenant must be refused: created=%v err=%v", created, createErr)
	}
	if created, createErr := r.CreateHealthInspectionJob(ctx, model.HealthInspectionJob{ID: "job-b", TenantID: "tenant-b", Status: "running", StartedAt: 2000, UpdatedAt: 2000}); createErr != nil || !created {
		t.Fatalf("other tenants are independent: created=%v err=%v", created, createErr)
	}

	running.Progress, running.UpdatedAt = 50, 1500
	if updated, updateErr := r.UpdateRunningHealthInspectionJob(ctx, running); updateErr != nil || !updated {
		t.Fatalf("progress not saved: updated=%v err=%v", updated, updateErr)
	}
	finished := running
	finished.Status, finished.FinishedAt, finished.Report = "succeeded", 1800, model.DeviceHealthReport{Summary: "正常"}
	if updated, updateErr := r.UpdateRunningHealthInspectionJob(ctx, finished); updateErr != nil || !updated {
		t.Fatalf("finish not saved: updated=%v err=%v", updated, updateErr)
	}
	running.Status = "running"
	if updated, _ := r.UpdateRunningHealthInspectionJob(ctx, running); updated {
		t.Fatal("a finished job must not be revived by a late progress update")
	}
	latest, err := r.LatestHealthInspectionJob(ctx, "tenant-a", "succeeded")
	if err != nil || latest.ID != "job-1" || latest.Report.Summary != "正常" {
		t.Fatalf("latest succeeded job: %#v err=%v", latest, err)
	}
	if created, createErr := r.CreateHealthInspectionJob(ctx, model.HealthInspectionJob{ID: "job-3", TenantID: "tenant-a", Status: "running", StartedAt: 3000, UpdatedAt: 3000}); createErr != nil || !created {
		t.Fatalf("a new job must start once the previous one finished: created=%v err=%v", created, createErr)
	}
	if latest, err = r.LatestHealthInspectionJob(ctx, "tenant-a", ""); err != nil || latest.ID != "job-3" {
		t.Fatalf("latest job of any status: %#v err=%v", latest, err)
	}
	if _, err = r.LatestHealthInspectionJob(ctx, "tenant-c", ""); err != ErrNotFound {
		t.Fatalf("missing tenant must report not found, got %v", err)
	}
}
