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

func EdgeCommands(t *testing.T, r ports.Repository) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UnixMilli()
	c := model.DeviceCommand{ID: "edge-command-1", TenantID: "edge-command-tenant", DeviceID: "device", ProductID: "product", Type: "test", Data: map[string]any{"type": "test"}, CreatedAt: now, Execution: &model.EdgeCommandExecution{NodeID: "node", ProfileID: "profile", ExpiresAt: now + 60000}}
	var wg sync.WaitGroup
	created := make(chan bool, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, first, err := r.CreateEdgeCommand(ctx, c)
			if err != nil {
				t.Error(err)
			}
			if first {
				created <- true
			}
		}()
	}
	wg.Wait()
	if len(created) != 1 {
		t.Fatal("create count", len(created))
	}
	for _, identity := range [][2]string{{"other", "node"}, {c.TenantID, "other"}} {
		if v, err := r.ClaimEdgeCommand(ctx, identity[0], identity[1], "wrong"); err != nil || v.ID != "" {
			t.Fatal("foreign claim", v, err)
		}
	}
	claims := make(chan model.DeviceCommand, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			v, err := r.ClaimEdgeCommand(ctx, c.TenantID, "node", fmt.Sprintf("claim-%d", i))
			if err != nil {
				t.Error(err)
			}
			if v.ID != "" {
				claims <- v
			}
		}(i)
	}
	wg.Wait()
	if len(claims) != 1 {
		t.Fatal("claim count", len(claims))
	}
	owned := <-claims
	if owned.Public().Execution.Token != "" || owned.Execution.Token == "" {
		t.Fatal("public token exposed or original modified")
	}
	if v, err := r.ClaimEdgeCommand(ctx, c.TenantID, "node", "restart"); err != nil || v.ID != "" {
		t.Fatal("claimed command redelivered", err)
	}
	wrong := owned
	execution := *owned.Execution
	execution.Token = "wrong"
	wrong.Execution = &execution
	wrong.Status = "ACKNOWLEDGED"
	if r.FinishEdgeCommand(ctx, wrong) == nil {
		t.Fatal("wrong token accepted")
	}
	owned.Status = "SUCCEEDED"
	if r.FinishEdgeCommand(ctx, owned) == nil {
		t.Fatal("transport result claimed execution success")
	}
	owned.Status = "ACKNOWLEDGED"
	owned.Reply = map[string]any{"rawMessageId": "actual-reply"}
	if err := r.FinishEdgeCommand(ctx, owned); err != nil {
		t.Fatal(err)
	}
	owned.Status = "UNKNOWN"
	if err := r.FinishEdgeCommand(ctx, owned); err != nil {
		t.Fatal("idempotent finish", err)
	}
	saved, err := r.GetDeviceCommand(ctx, c.TenantID, c.ID)
	if err != nil || saved.Status != "ACKNOWLEDGED" {
		t.Fatal("duplicate result overwrote outcome", saved, err)
	}
	if _, err := r.GetDeviceCommand(ctx, "other", c.ID); err == nil {
		t.Fatal("foreign history exposed")
	}
	for i := 0; i < 8; i++ {
		next := c
		next.ID = fmt.Sprintf("edge-capacity-%d", i)
		if _, first, err := r.CreateEdgeCommand(ctx, next); err != nil || !first {
			t.Fatal("queue", i, err)
		}
	}
	next := c
	next.ID = "overflow"
	if _, _, err := r.CreateEdgeCommand(ctx, next); err == nil {
		t.Fatal("unbounded queue")
	}
	expired := c
	expired.ID = "expired-other-node"
	e := *c.Execution
	e.NodeID = "expired"
	e.ExpiresAt = now - 1
	expired.Execution = &e
	expired.Status = "QUEUED"
	if _, _, err := r.CreateDeviceCommand(ctx, expired); err != nil {
		t.Fatal(err)
	}
	if v, err := r.ClaimEdgeCommand(ctx, c.TenantID, "expired", "too-late"); err != nil || v.ID != "" {
		t.Fatal("expired command dispatched")
	}
	if expired.ObservedOutcome(now).Status != "EXPIRED" {
		t.Fatal("expired queue not visible")
	}
}
