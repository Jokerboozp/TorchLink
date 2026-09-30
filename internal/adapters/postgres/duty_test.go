package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func TestDutySQLRollbackCASImmutableAndHalfOpen(t *testing.T) {
	r := testRepository(t)
	ctx := context.Background()
	boom := errors.New("rollback")
	err := r.DutyTransaction(ctx, "t", func(tx ports.DutyTx) error {
		if _, err := tx.Put(model.NewDutyDocument(model.DutyStationKind, "s", model.DutyStation{Name: "room"}), 0); err != nil {
			return err
		}
		if err := tx.AppendEvent(model.DutyBusinessEvent{ID: "fail", Type: "TEST", OccurredAt: 5}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatal(err)
	}
	err = r.DutyRead(ctx, "t", func(tx ports.DutyTx) error {
		if _, err := tx.Get(model.DutyStationKind, "s"); !errors.Is(err, model.ErrNotFound) {
			t.Fatal(err)
		}
		_, n, err := tx.Events(model.DutyFilter{})
		if n != 0 || err != nil {
			t.Fatal(n, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = r.DutyTransaction(ctx, "t", func(tx ports.DutyTx) error {
		_, err := tx.Put(model.NewDutyDocument(model.DutyStationKind, "s", model.DutyStation{Name: "room"}), 0)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	var successes int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
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
		t.Fatal("CAS accepted stale updates", successes)
	}
	err = r.DutyTransaction(ctx, "t", func(tx ports.DutyTx) error {
		d, err := tx.Put(model.NewDutyDocument(model.DutyRevisionKind, "r", model.DutyHandoverRevision{}), 0)
		if err != nil {
			return err
		}
		if _, err = tx.Put(d, 1); err == nil {
			t.Fatal("immutable revision edited")
		}
		for _, e := range []model.DutyBusinessEvent{{ID: "start", Type: "TEST", OccurredAt: 10}, {ID: "end", Type: "TEST", OccurredAt: 20}} {
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
		es, n, err := tx.Events(model.DutyFilter{Start: 10, End: 20})
		if n != 1 || len(es) != 1 || es[0].ID != "start" || err != nil {
			t.Fatal(es, n, err)
		}
		_, err = tx.Put(model.NewDutyDocument(model.DutyStationKind, "x", model.DutyStation{}), 0)
		if !errors.Is(err, model.ErrDutyReadOnly) {
			t.Fatal(err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = r.DutyRead(ctx, "other", func(tx ports.DutyTx) error {
		_, err := tx.Get(model.DutyStationKind, "s")
		if !errors.Is(err, model.ErrNotFound) {
			t.Fatal("cross tenant", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
func TestDutySQLFactAndEventAtomicFailure(t *testing.T) {
	r := testRepository(t)
	ctx := model.WithDutyActor(context.Background(), "operator")
	a := model.Alarm{ID: "a", TenantID: "t", DeviceID: "d", RuleID: "r", Status: "ACTIVE", TriggerCount: 1, TriggerID: "m1", FirstTriggeredAt: 10, LastTriggeredAt: 10}
	a, _, err := r.UpsertAlarm(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	// Force ledger insertion to fail, proving its failure rolls back the fact and outbox.
	_, err = r.pool.Exec(ctx, `CREATE FUNCTION reject_duty_event() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'ledger failed'; END $$; CREATE TRIGGER reject_duty_event BEFORE INSERT ON duty_business_event FOR EACH ROW EXECUTE FUNCTION reject_duty_event()`)
	if err != nil {
		t.Fatal(err)
	}
	a.Status = "ACKED"
	a.AckedAt = 20
	if _, err = r.UpdateAlarmIf(ctx, a); err == nil {
		t.Fatal("failed ledger did not abort alarm update")
	}
	saved, err := r.GetAlarm(ctx, "t", "a")
	if err != nil || saved.Status != "ACTIVE" || saved.AckedAt != 0 {
		t.Fatal("fact changed despite rollback", saved, err)
	}
	if err = r.UpsertDeviceState(ctx, model.DeviceState{TenantID: "t", DeviceID: "d", BusinessStatus: "OFFLINE", LastSeenAt: 20}); err == nil {
		t.Fatal("failed ledger did not abort state insert")
	}
	if _, err = r.GetDeviceState(ctx, "t", "d"); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("state persisted", err)
	}
	if _, err = r.pool.Exec(ctx, `DROP TRIGGER reject_duty_event ON duty_business_event; DROP FUNCTION reject_duty_event()`); err != nil {
		t.Fatal(err)
	}
	a = saved
	a.Status = "ACKED"
	a.AckedAt = 20
	if _, err = r.UpdateAlarmIf(ctx, a); err != nil {
		t.Fatal(err)
	}
	err = r.DutyRead(ctx, "t", func(tx ports.DutyTx) error {
		events, n, err := tx.Events(model.DutyFilter{})
		if err != nil || n != 2 {
			t.Fatal(events, n, err)
		}
		for _, e := range events {
			if e.ActorID != "operator" {
				t.Fatal("actor missing", e)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
func TestDutySQLListsAndSnapshotScoped(t *testing.T) {
	r := testRepository(t)
	ctx := context.Background()
	for _, id := range []string{"d1", "d2"} {
		if err := r.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "t", ID: id, AccessKey: id}); err != nil {
			t.Fatal(err)
		}
		if err := r.UpsertDeviceState(ctx, model.DeviceState{TenantID: "t", DeviceID: id, BusinessStatus: "ONLINE"}); err != nil {
			t.Fatal(err)
		}
	}
	err := r.DutyTransaction(ctx, "t", func(tx ports.DutyTx) error {
		for _, id := range []string{"d1", "d2"} {
			_, err := tx.Put(model.NewDutyDocument(model.DutyRecordKind, id, model.DutyRecord{StationID: "s", RunID: "run", DeviceID: id, AuthorID: "u", OccurredAt: 10}), 0)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = r.DutyRead(ctx, "t", func(tx ports.DutyTx) error {
		ds, n, err := tx.List(model.DutyFilter{Kind: model.DutyRecordKind, RunID: "run", UserID: "u", DeviceIDs: []string{"d1"}, Start: 10, End: 11})
		if err != nil || n != 1 || len(ds) != 1 || ds[0].ID != "d1" {
			t.Fatal(ds, n, err)
		}
		s, err := tx.Snapshot([]string{"d1"})
		if err != nil || len(s.Devices) != 1 || len(s.States) != 1 {
			t.Fatal(s, err)
		}
		s, err = tx.Snapshot([]string{})
		if err != nil || len(s.Devices) != 0 || len(s.States) != 0 {
			t.Fatal(s, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
