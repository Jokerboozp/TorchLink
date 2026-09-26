package protocolruntime

import (
	"context"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/model"
	"testing"
	"time"
)

func TestCoordinatorLossCancelsOldExecution(t *testing.T) {
	repo := memory.NewRepository()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := model.DeviceAccessProfile{TenantID: "tenant", ID: "profile", Enabled: true}
	if err := repo.SaveDeviceAccessProfile(ctx, p); err != nil {
		t.Fatal(err)
	}
	first := NewCoordinator(repo, "first", "http://first")
	second := NewCoordinator(repo, "second", "http://second")
	done := make(chan struct{})
	go func() { first.Run(ctx); close(done) }()
	execution, ok := first.Claim(ctx, p)
	if !ok {
		t.Fatal("first owner rejected")
	}
	if _, ok := second.Claim(ctx, p); ok {
		t.Fatal("duplicate executor")
	}
	if err := first.Validate(execution, p); err != nil {
		t.Fatal(err)
	}
	lease, err := repo.GetExecutionLease(ctx, p.TenantID, "profile/"+p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.ReleaseExecutionLease(ctx, lease); err != nil {
		t.Fatal(err)
	}
	next, ok := second.Claim(ctx, p)
	if !ok {
		t.Fatal("takeover rejected")
	}
	select {
	case <-execution.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("old execution survived lease loss")
	}
	if err := first.Validate(execution, p); err == nil {
		t.Fatal("old execution may ingest")
	}
	if err := second.Validate(next, p); err != nil {
		t.Fatal("new execution rejected", err)
	}
	p.Enabled = false
	if err := repo.SaveDeviceAccessProfile(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := second.Validate(next, p); err == nil {
		t.Fatal("disabled profile may ingest")
	}
	cancel()
	<-done
	second.mu.Lock()
	for _, h := range second.held {
		h.timer.Stop()
		h.cancel()
	}
	second.mu.Unlock()
}
