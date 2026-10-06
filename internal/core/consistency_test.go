package core

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func quietEngine(repo ports.Repository) *Engine {
	return New(repo, nil, local.NewBus(), local.NewRealtime(), nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestStandardMessageWaitsForLiveClaimThenTakesOver(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	e := quietEngine(repo)
	msg := model.StandardMessage{TenantID: "t", DeviceID: "d", ProductID: "p", MessageID: "m", Timestamp: 1000, MessageType: model.PropertyReport, Properties: map[string]any{"v": 1}}
	// Another worker (for example the previous partition owner) holds it.
	ghost, err := repo.ClaimStandardMessage(ctx, msg, "previous-owner", 300*time.Millisecond)
	if err != nil || !ghost.ShouldProcess {
		t.Fatal(ghost, err)
	}
	start := time.Now()
	if err = e.handleStandard(ctx, mustJSON(msg)); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < 250*time.Millisecond {
		t.Fatal("processed while another worker still held the claim")
	}
	if state, err := repo.GetDeviceState(ctx, "t", "d"); err != nil || state.LastMessageID != "m" {
		t.Fatal("taken-over message was not processed", state, err)
	}
	// The previous owner's late completion is fenced.
	if err = repo.MarkStandardMessageProcessed(ctx, "t", "m", ghost.Token); err == nil {
		t.Fatal("stale owner recorded completion")
	}
}

// racingAlarmRepo lets a concurrent trigger land between a read and its write.
type racingAlarmRepo struct {
	*memory.Repository
	once sync.Once
}

func (r *racingAlarmRepo) GetAlarm(ctx context.Context, tenant, id string) (model.Alarm, error) {
	a, err := r.Repository.GetAlarm(ctx, tenant, id)
	r.once.Do(func() {
		_, _, _ = r.Repository.UpsertAlarm(ctx, model.Alarm{ID: "dup", TenantID: tenant, DeviceID: a.DeviceID, RuleID: a.RuleID, Status: "ACTIVE", LastTriggeredAt: 99, TriggerID: "concurrent"})
	})
	return a, err
}

func TestAlarmAcknowledgeRetriesOnConcurrentTrigger(t *testing.T) {
	ctx := context.Background()
	repo := &racingAlarmRepo{Repository: memory.NewRepository()}
	e := quietEngine(repo)
	if _, _, err := repo.Repository.UpsertAlarm(ctx, model.Alarm{ID: "a", TenantID: "t", DeviceID: "d", RuleID: "r", Status: "ACTIVE", TriggerCount: 1, LastTriggeredAt: 1}); err != nil {
		t.Fatal(err)
	}
	acked, err := e.SetAlarmStatus(ctx, "t", "a", "ACKED", "operator")
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := repo.Repository.GetAlarm(ctx, "t", "a")
	if acked.Status != "ACKED" || stored.Status != "ACKED" || stored.TriggerCount != 2 || stored.LastTriggeredAt != 99 {
		t.Fatalf("acknowledgement overwrote the concurrent trigger: %+v", stored)
	}
}

// staleListRepo returns a device listing taken before a new report arrived.
type staleListRepo struct {
	*memory.Repository
	afterList func()
}

func (r *staleListRepo) ListOfflineDue(ctx context.Context, now int64, limit int) ([]model.DeviceState, error) {
	states, err := r.Repository.ListOfflineDue(ctx, now, limit)
	r.afterList()
	return states, err
}

func TestOfflineScanDoesNotOverwriteNewerReport(t *testing.T) {
	ctx := context.Background()
	base := memory.NewRepository()
	now := time.Now().UnixMilli()
	old := model.DeviceState{TenantID: "t", DeviceID: "d", ProductID: "p", BusinessStatus: "ONLINE", DataStatus: "ACTIVE", LastSeenAt: now - 3_600_000, ReportIntervalSec: 60, OfflineToleranceSec: 60}
	if err := base.UpsertDeviceState(ctx, old); err != nil {
		t.Fatal(err)
	}
	repo := &staleListRepo{Repository: base, afterList: func() {
		fresh := old
		fresh.LastSeenAt = now
		_ = base.UpsertDeviceState(ctx, fresh)
	}}
	e := quietEngine(repo)
	if err := e.ScanOffline(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := base.GetDeviceState(ctx, "t", "d"); got.BusinessStatus != "ONLINE" || got.LastSeenAt != now {
		t.Fatalf("offline scan overwrote a newer report: %+v", got)
	}
	// Without the concurrent report the device is marked offline.
	repo.afterList = func() {}
	if err := base.UpsertDeviceState(ctx, old); err != nil {
		t.Fatal(err)
	}
	if err := e.ScanOffline(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := base.GetDeviceState(ctx, "t", "d"); got.BusinessStatus != "OFFLINE" || got.DataStatus != "SILENT" {
		t.Fatalf("silent device not marked offline: %+v", got)
	}
}

func TestSingletonJobRunsOnOneProcessAndFailsOver(t *testing.T) {
	oldTTL, oldRenew := singletonTTL, singletonRenew
	singletonTTL, singletonRenew = time.Second, 100*time.Millisecond
	defer func() { singletonTTL, singletonRenew = oldTTL, oldRenew }()
	// Virtual time: the lease expiry and renewals run without real waiting.
	synctest.Test(t, func(t *testing.T) {
		repo := memory.NewRepository()
		a, b := quietEngine(repo), quietEngine(repo)
		a.SetIdentity("a")
		b.SetIdentity("b")
		var ranA, ranB atomic.Int64
		ctxA, stopA := context.WithCancel(context.Background())
		ctxB, stopB := context.WithCancel(context.Background())
		defer stopB()
		job := func(n *atomic.Int64) func(context.Context) error {
			return func(context.Context) error { n.Add(1); return nil }
		}
		a.RunSingleton(ctxA, "job", 20*time.Millisecond, job(&ranA))
		time.Sleep(50 * time.Millisecond)
		b.RunSingleton(ctxB, "job", 20*time.Millisecond, job(&ranB))
		time.Sleep(400 * time.Millisecond)
		synctest.Wait()
		if ranA.Load() == 0 || ranB.Load() != 0 {
			t.Fatalf("both processes ran the singleton: a=%d b=%d", ranA.Load(), ranB.Load())
		}
		stopA()
		// The standby takes over once the stopped holder's lease expires.
		time.Sleep(singletonTTL + 2*singletonRenew)
		synctest.Wait()
		if ranB.Load() == 0 {
			t.Fatal("standby did not take over after the holder stopped")
		}
	})
}

func TestProcessorOnlyEngineConsumesBusinessStream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	repo := memory.NewRepository()
	bus := local.NewBus()
	e := New(repo, nil, bus, local.NewRealtime(), nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := e.StartWith(ctx, Components{Processor: true}); err != nil {
		t.Fatal(err)
	}
	msg := model.StandardMessage{TenantID: "t", DeviceID: "d", ProductID: "p", MessageID: "m", Timestamp: 1, MessageType: model.PropertyReport, Properties: map[string]any{"v": 1}}
	// Legacy external topics are no longer consumed internally.
	_ = bus.Publish(ctx, model.TopicPropertyReport, model.DeviceKey("t", "d"), mustJSON(msg))
	if _, err := repo.GetDeviceState(ctx, "t", "d"); err == nil {
		t.Fatal("external topic was processed as business input")
	}
	if err := bus.Publish(ctx, model.TopicDeviceBusiness, model.DeviceKey("t", "d"), mustJSON(msg)); err != nil {
		t.Fatal(err)
	}
	if state, err := repo.GetDeviceState(ctx, "t", "d"); err != nil || state.LastMessageID != "m" {
		t.Fatal("business stream not processed", err)
	}
}

func TestReplayWithoutHeartbeatIsReportedInterrupted(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	e := quietEngine(repo)
	now := time.Now()
	running := model.ReplayRequest{ID: "r1", TenantID: "t", Mode: "DRY_RUN", Status: "RUNNING", CreatedAt: now.Add(-5 * time.Minute).UnixMilli(), HeartbeatAt: now.Add(-2 * time.Minute).UnixMilli(), Owner: "gone"}
	if err := repo.SaveReplay(ctx, running); err != nil {
		t.Fatal(err)
	}
	if got := e.RefreshReplay(ctx, running); got.Status != "INTERRUPTED" || got.CompletedAt == 0 {
		t.Fatalf("stale replay not interrupted: %+v", got)
	}
	if stored, _ := repo.GetReplay(ctx, "r1"); stored.Status != "INTERRUPTED" {
		t.Fatal("interruption not persisted")
	}
	alive := running
	alive.ID, alive.HeartbeatAt = "r2", now.UnixMilli()
	if got := e.RefreshReplay(ctx, alive); got.Status != "RUNNING" {
		t.Fatal("live replay interrupted")
	}
}
