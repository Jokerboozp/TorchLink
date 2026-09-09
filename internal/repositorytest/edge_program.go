package repositorytest

import (
	"context"
	"errors"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"sync"
	"sync/atomic"
	"testing"
)

func EdgeProgram(t *testing.T, r ports.Repository) {
	t.Helper()
	ctx := context.Background()
	const tenant, node = "program-tenant", "node"
	var wg sync.WaitGroup
	var winners atomic.Int32
	for i := 0; i < 16; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, err := r.SetEdgeProgram(ctx, tenant, node, 0, "v2")
			if err == nil {
				winners.Add(1)
			} else if !errors.Is(err, model.ErrEdgeProgramConflict) {
				t.Error(err)
			}
		}()
		go func() {
			defer wg.Done()
			if err := r.ReportEdgeProgram(ctx, tenant, node, model.EdgeProgramStatus{Phase: "RUNNING", Version: "v1", Generation: 0}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	v, err := r.GetEdgeProgram(ctx, tenant, node)
	if err != nil || winners.Load() != 1 || v.Generation != 1 || v.TargetVersion != "v2" || v.Status.Version != "v1" || v.Status.LastSeenAt == 0 {
		t.Fatal("concurrent target/status lost", v, err)
	}
	other, err := r.GetEdgeProgram(ctx, "other", node)
	if err != nil || other.Generation != 0 || other.Status.Version != "" {
		t.Fatal("foreign state", other, err)
	}
	v, err = r.SetEdgeProgram(ctx, tenant, node, 1, "")
	if err != nil || v.Generation != 2 || v.TargetVersion != "" || v.Status.Version != "v1" {
		t.Fatal("target cancellation lost status", v, err)
	}
}
