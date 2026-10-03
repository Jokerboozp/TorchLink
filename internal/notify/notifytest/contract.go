// Package notifytest holds the behaviour every notify.Store must share.
package notifytest

import (
	"context"
	"errors"
	"testing"

	"iot-platform/internal/notify"
)

func StoreContract(t *testing.T, store notify.Store) {
	t.Helper()
	ctx := context.Background()
	sealed := "v1:sealed"
	c, err := store.SaveChannel(ctx, notify.Channel{ID: "c1", TenantID: "t", Name: "钉钉", Type: notify.ChannelDingTalk, Enabled: true, Config: notify.ChannelConfig{URLHint: "https://oapi.dingtalk.com"}}, &sealed)
	if err != nil || c.Version != 1 || !c.SecretSet {
		t.Fatalf("create channel %+v %v", c, err)
	}
	if _, err = store.SaveChannel(ctx, notify.Channel{ID: "c1", TenantID: "t", Name: "dup", Type: notify.ChannelDingTalk}, nil); !errors.Is(err, notify.ErrConflict) {
		t.Fatalf("duplicate create: %v", err)
	}
	c.Name = "钉钉值班群"
	if c, err = store.SaveChannel(ctx, c, nil); err != nil || c.Version != 2 {
		t.Fatalf("update keeps secret: %+v %v", c, err)
	}
	if got, secret, err := store.GetChannel(ctx, "t", "c1"); err != nil || secret != sealed || got.Name != "钉钉值班群" || !got.SecretSet {
		t.Fatalf("get %+v %q %v", got, secret, err)
	}
	if _, _, err := store.GetChannel(ctx, "other", "c1"); !errors.Is(err, notify.ErrNotFound) {
		t.Fatal("channel visible to another tenant")
	}
	stale := c
	stale.Version = 1
	if _, err = store.SaveChannel(ctx, stale, nil); !errors.Is(err, notify.ErrConflict) {
		t.Fatal("stale update accepted")
	}
	p, err := store.SavePolicy(ctx, notify.Policy{ID: "p1", TenantID: "t", Name: "火警", Enabled: true, Stages: []notify.Stage{{ChannelIDs: []string{"c1"}}}})
	if err != nil || p.Version != 1 {
		t.Fatalf("policy %+v %v", p, err)
	}
	if list, _ := store.ListPolicies(ctx, "t"); len(list) != 1 || len(list[0].Stages) != 1 {
		t.Fatalf("policies %+v", list)
	}
	tasks := []notify.Task{
		{TenantID: "t", AlarmID: "a", PolicyID: "p1", Stage: 0, Kind: notify.KindTrigger, ChannelID: "c1", Status: notify.StatusPending, NextAt: 100, CreatedAt: 100},
		{TenantID: "t", AlarmID: "a", PolicyID: "p1", Stage: 1, Kind: notify.KindTrigger, ChannelID: "c1", Status: notify.StatusPending, NextAt: 500, CreatedAt: 100},
	}
	if n, err := store.EnqueueTasks(ctx, tasks); err != nil || n != 2 {
		t.Fatalf("enqueue %d %v", n, err)
	}
	if n, err := store.EnqueueTasks(ctx, tasks); err != nil || n != 0 {
		t.Fatalf("redelivery queued again: %d %v", n, err)
	}
	claimed, err := store.ClaimDueTasks(ctx, 200, 1000, 10)
	if err != nil || len(claimed) != 1 || claimed[0].Stage != 0 {
		t.Fatalf("claim %+v %v", claimed, err)
	}
	if again, _ := store.ClaimDueTasks(ctx, 300, 1000, 10); len(again) != 0 {
		t.Fatal("a leased task was claimed twice")
	}
	// An expired lease (crashed sender) is claimed again.
	if again, _ := store.ClaimDueTasks(ctx, 1300, 1000, 10); len(again) != 2 {
		t.Fatalf("expired lease or due escalation not claimed: %+v", again)
	}
	claimed[0].Status, claimed[0].SentAt, claimed[0].Recipients = notify.StatusSent, 1300, []string{"值班员"}
	if err = store.FinishTask(ctx, claimed[0]); err != nil {
		t.Fatal(err)
	}
	timeline, err := store.ListAlarmTasks(ctx, "t", "a")
	if err != nil || len(timeline) != 2 || timeline[0].Status != notify.StatusSent || timeline[0].Recipients[0] != "值班员" {
		t.Fatalf("timeline %+v %v", timeline, err)
	}
	if other, _ := store.ListAlarmTasks(ctx, "other", "a"); len(other) != 0 {
		t.Fatal("timeline visible to another tenant")
	}
	if err = store.DeleteChannel(ctx, "t", "c1"); err != nil {
		t.Fatal(err)
	}
	if err = store.DeletePolicy(ctx, "t", "missing"); !errors.Is(err, notify.ErrNotFound) {
		t.Fatalf("delete missing: %v", err)
	}
}
