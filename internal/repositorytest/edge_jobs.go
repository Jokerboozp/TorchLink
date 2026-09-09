package repositorytest

import (
	"context"
	"fmt"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"sync"
	"testing"
	"time"
)

func EdgeReadJobs(t *testing.T, r ports.Repository) {
	t.Helper()
	ctx := context.Background()
	j := model.EdgeReadJob{ID: "diagnostic", TenantID: "jobs-tenant", NodeID: "node", ExpiresAt: time.Now().Add(time.Minute).UnixMilli()}
	if err := r.CreateEdgeReadJob(ctx, j); err != nil {
		t.Fatal(err)
	}
	if foreign, err := r.ClaimEdgeReadJob(ctx, "other", j.NodeID, "foreign"); err != nil || foreign.ID != "" {
		t.Fatal("foreign job", err)
	}
	var wg sync.WaitGroup
	jobs := make(chan model.EdgeReadJob, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			claimed, err := r.ClaimEdgeReadJob(ctx, j.TenantID, j.NodeID, fmt.Sprint(i))
			if err != nil {
				t.Error(err)
			}
			if claimed.ID != "" {
				jobs <- claimed
			}
		}(i)
	}
	wg.Wait()
	close(jobs)
	if len(jobs) != 1 {
		t.Fatalf("claimants=%d", len(jobs))
	}
	owned := <-jobs
	wrong := owned
	wrong.Token = "wrong"
	if err := r.FinishEdgeReadJob(ctx, wrong); err == nil {
		t.Fatal("unowned result accepted")
	}
	owned.Error = "read failed"
	if err := r.FinishEdgeReadJob(ctx, owned); err != nil {
		t.Fatal(err)
	}
	if err := r.FinishEdgeReadJob(ctx, owned); err == nil {
		t.Fatal("completed result overwritten")
	}
	saved, err := r.GetEdgeReadJob(ctx, j.TenantID, j.ID)
	if err != nil || saved.Status != "DONE" || saved.Error != "read failed" {
		t.Fatal(saved, err)
	}
}
