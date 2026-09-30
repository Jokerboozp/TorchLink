package memory

import (
	"context"
	"errors"
	"sync"
	"testing"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func TestDutyTransactionsRollbackIsolationAndVersion(t *testing.T) {
	r := NewRepository()
	ctx := context.Background()
	boom := errors.New("fail after document and event")
	err := r.DutyTransaction(ctx, "t", func(tx ports.DutyTx) error {
		if _, err := tx.Put(model.NewDutyDocument(model.DutyStationKind, "s", model.DutyStation{Name: "消防室"}), 0); err != nil {
			return err
		}
		if err := tx.AppendEvent(model.DutyBusinessEvent{ID: "e", Type: "TEST", OccurredAt: 10}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatal(err)
	}
	if err = r.DutyRead(ctx, "t", func(tx ports.DutyTx) error {
		if _, err := tx.Get(model.DutyStationKind, "s"); !errors.Is(err, model.ErrNotFound) {
			t.Fatal(err)
		}
		events, total, err := tx.Events(model.DutyFilter{})
		if err != nil || len(events) != 0 || total != 0 {
			t.Fatal(events, total, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err = r.DutyTransaction(ctx, "t", func(tx ports.DutyTx) error {
		_, err := tx.Put(model.NewDutyDocument(model.DutyStationKind, "s", model.DutyStation{Name: "消防室"}), 0)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var successes int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := r.DutyTransaction(ctx, "t", func(tx ports.DutyTx) error {
				d, err := tx.Get(model.DutyStationKind, "s")
				if err != nil {
					return err
				}
				_, err = tx.Put(d, 1)
				return err
			})
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				successes++
			} else if !errors.Is(err, model.ErrDutyConflict) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if successes != 1 {
		t.Fatal("concurrent stale updates accepted", successes)
	}
	if err = r.DutyRead(ctx, "other", func(tx ports.DutyTx) error {
		_, err := tx.Get(model.DutyStationKind, "s")
		if !errors.Is(err, model.ErrNotFound) {
			t.Fatal("cross tenant", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err = r.DutyRead(ctx, "t", func(tx ports.DutyTx) error {
		_, err := tx.Put(model.NewDutyDocument(model.DutyStationKind, "forbidden", model.DutyStation{}), 0)
		if !errors.Is(err, model.ErrDutyReadOnly) {
			t.Fatal(err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
func TestDutyImmutableRecordsHalfOpenEventsAndScopedSnapshot(t *testing.T) {
	r := NewRepository()
	ctx := context.Background()
	for _, tenant := range []string{"t", "other"} {
		if err := r.UpsertDeviceState(ctx, model.DeviceState{TenantID: tenant, DeviceID: "d1", BusinessStatus: "OFFLINE", LastSeenAt: 20}); err != nil {
			t.Fatal(err)
		}
	}
	err := r.DutyTransaction(ctx, "t", func(tx ports.DutyTx) error {
		d, err := tx.Put(model.NewDutyDocument(model.DutyRevisionKind, "rev", model.DutyHandoverRevision{}), 0)
		if err != nil {
			return err
		}
		if _, err = tx.Put(d, d.Version); err == nil {
			t.Fatal("revision edited")
		}
		for _, e := range []model.DutyBusinessEvent{{ID: "10", Type: "TEST", OccurredAt: 10}, {ID: "20", Type: "TEST", OccurredAt: 20}} {
			if err = tx.AppendEvent(e); err != nil {
				return err
			}
			if err = tx.AppendEvent(e); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = r.DutyRead(ctx, "t", func(tx ports.DutyTx) error {
		events, n, err := tx.Events(model.DutyFilter{Kind: "TEST", Start: 10, End: 20})
		if err != nil || n != 1 || len(events) != 1 || events[0].ID != "10" {
			t.Fatal(events, n, err)
		}
		none, err := tx.Snapshot([]string{})
		if err != nil || len(none.States) != 0 {
			t.Fatal(none, err)
		}
		some, err := tx.Snapshot([]string{"d1"})
		if err != nil || len(some.States) != 1 || some.States[0].TenantID != "t" {
			t.Fatal(some, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
func TestDutyReliableAlarmLifecycleAndNoEventOnFailedCAS(t *testing.T) {
	r := NewRepository()
	ctx := model.WithDutyActor(context.Background(), "u")
	a := model.Alarm{ID: "a", TenantID: "t", DeviceID: "d", RuleID: "r", Status: "ACTIVE", TriggerID: "m1", FirstTriggeredAt: 10, LastTriggeredAt: 10, TriggerCount: 1}
	a, _, err := r.UpsertAlarm(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = r.UpsertAlarm(ctx, a); err != nil {
		t.Fatal(err)
	}
	a.TriggerID = "m2"
	a.LastTriggeredAt = 20
	a, _, err = r.UpsertAlarm(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	stale := a
	a.Status = "ACKED"
	a.AckedAt = 30
	ok, err := r.UpdateAlarmIf(ctx, a)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	stale.Status = "CLOSED"
	stale.ClosedAt = 40
	ok, err = r.UpdateAlarmIf(ctx, stale)
	if err != nil || ok {
		t.Fatal("stale update", ok, err)
	}
	a, err = r.GetAlarm(ctx, "t", "a")
	if err != nil {
		t.Fatal(err)
	}
	a.Status = "RECOVERED"
	a.RecoveredAt = 50
	if err = r.UpdateAlarm(ctx, a); err != nil {
		t.Fatal(err)
	}
	a.Status = "CLOSED"
	a.ClosedAt = 60
	if err = r.UpdateAlarm(ctx, a); err != nil {
		t.Fatal(err)
	}
	err = r.DutyRead(ctx, "t", func(tx ports.DutyTx) error {
		events, n, err := tx.Events(model.DutyFilter{Start: 10, End: 70})
		if err != nil || n != 5 {
			t.Fatal(events, n, err)
		}
		types := map[string]bool{}
		for _, e := range events {
			types[e.Type] = true
			if e.ActorID != "u" {
				t.Fatal("actor missing", e)
			}
		}
		for _, kind := range []string{"ALARM_CREATED", "ALARM_REPORTED", "ALARM_ACKNOWLEDGED", "ALARM_RECOVERED", "ALARM_CLOSED"} {
			if !types[kind] {
				t.Fatal("missing lifecycle", kind)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDutyDeviceTimeoutEvidenceUsesRealPlatformStatuses(t *testing.T) {
	r := NewRepository()
	ctx := context.Background()
	v := model.DeviceState{TenantID: "t", DeviceID: "d", BusinessStatus: "ONLINE", ConnectionStatus: "CONNECTED", DataStatus: "ACTIVE", LastSeenAt: 10}
	if err := r.UpsertDeviceState(ctx, v); err != nil {
		t.Fatal(err)
	}
	v.BusinessStatus = "SUSPECTED_OFFLINE"
	v.DataStatus = "SILENT"
	v.OfflineAt = 20
	if err := r.UpsertDeviceState(ctx, v); err != nil {
		t.Fatal(err)
	}
	v.BusinessStatus = "ONLINE"
	v.DataStatus = "ACTIVE"
	v.LastSeenAt = 30
	if err := r.UpsertDeviceState(ctx, v); err != nil {
		t.Fatal(err)
	}
	if err := r.DutyRead(ctx, "t", func(tx ports.DutyTx) error {
		events, n, err := tx.Events(model.DutyFilter{Start: 20, End: 31})
		if err != nil || n != 2 {
			t.Fatal(events, n, err)
		}
		if events[0].Type != "DEVICE_ONLINE" || events[0].OccurredAt != 30 || events[1].Type != "DEVICE_SUSPECTED_OFFLINE" {
			t.Fatal(events)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
