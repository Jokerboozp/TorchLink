package repositorytest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// ExternalAlarm verifies event-specific identity, report idempotency, outbox
// consistency and races against the normal user/recovery versioned writes.
func ExternalAlarm(t *testing.T, repo ports.Repository) {
	t.Helper()
	ctx := context.Background()
	report := func(id string) model.Alarm {
		return model.Alarm{TenantID: "external-alarm", ID: "external_" + id, RuleID: "FIRE:external:" + id, DeviceID: "device", TriggerID: "trigger-1", Status: "ACTIVE", AlarmLevel: "LOW", Source: "device", FirstTriggeredAt: 10, LastTriggeredAt: 10, Details: map[string]any{"content": "first"}}
	}
	drain := func() []model.Alarm {
		t.Helper()
		var reports []model.Alarm
		_, err := repo.DrainOutbox(ctx, 100, func(e model.OutboxEvent) error {
			if e.Topic != model.TopicAlarmReported {
				return fmt.Errorf("unexpected topic %s", e.Topic)
			}
			var a model.Alarm
			if err := json.Unmarshal(e.Payload, &a); err != nil {
				return err
			}
			if e.Key != a.ID {
				return fmt.Errorf("outbox identity %s != %s", e.Key, a.ID)
			}
			reports = append(reports, a)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return reports
	}
	t.Run("concurrent creation and version updates each report once", func(t *testing.T) {
		v := report("concurrent")
		concurrent := func(wantCreated bool) {
			t.Helper()
			var wg sync.WaitGroup
			var createdCount, changedCount atomic.Int64
			for range 16 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, created, changed, err := repo.UpsertExternalAlarm(ctx, v)
					if err != nil {
						t.Error(err)
					}
					if created {
						createdCount.Add(1)
					}
					if changed {
						changedCount.Add(1)
					}
				}()
			}
			wg.Wait()
			expectedCreated := int64(0)
			if wantCreated {
				expectedCreated = 1
			}
			if createdCount.Load() != expectedCreated || changedCount.Load() != 1 {
				t.Fatalf("created=%d changed=%d", createdCount.Load(), changedCount.Load())
			}
		}
		concurrent(true)
		first, err := repo.GetAlarm(ctx, v.TenantID, v.ID)
		if err != nil || first.TriggerCount != 1 || first.Version != 1 {
			t.Fatalf("first report: %+v %v", first, err)
		}
		if events := drain(); len(events) != 1 || events[0].TriggerCount != 1 || events[0].TriggerID != v.TriggerID {
			t.Fatalf("first outbox: %+v", events)
		}
		first.Status, first.AckedAt = "ACKED", 15
		if ok, err := repo.UpdateAlarmIf(ctx, first); err != nil || !ok {
			t.Fatalf("acknowledge: %v %v", ok, err)
		}
		v.TriggerID, v.LastTriggeredAt, v.AlarmLevel, v.Confidence = "trigger-2", 20, "CRITICAL", 0.95
		v.Content = "外部事件新版本的用户提醒内容"
		v.Details = map[string]any{"content": "new report"}
		concurrent(false)
		updated, err := repo.GetAlarm(ctx, v.TenantID, v.ID)
		if err != nil || updated.TriggerCount != 2 || updated.Version != 3 || updated.TriggerID != v.TriggerID || updated.LastTriggeredAt != 20 || updated.FirstTriggeredAt != 10 || updated.AlarmLevel != "CRITICAL" || updated.Details["content"] != "new report" || updated.Confidence != 0.95 || updated.Status != "ACKED" || updated.AckedAt != 15 {
			t.Fatalf("new report lost state: %+v %v", updated, err)
		}
		summaries, err := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: v.TenantID, Summary: true})
		if err != nil || len(summaries) != 1 || summaries[0].Content != v.Content || len(summaries[0].Details) != 0 || len(summaries[0].Cameras) != 0 || updated.Content != v.Content {
			t.Fatalf("notification summary dropped external content: %+v %v", summaries, err)
		}
		if events := drain(); len(events) != 1 || events[0].TriggerCount != 2 || events[0].TriggerID != "trigger-2" || events[0].Details["content"] != "new report" || events[0].Status != "ACKED" || events[0].Content != v.Content {
			t.Fatalf("update outbox: %+v", events)
		}
		v.Details["content"] = "duplicate must not change details"
		if _, created, changed, err := repo.UpsertExternalAlarm(ctx, v); err != nil || created || changed {
			t.Fatalf("duplicate accepted: %v %v %v", created, changed, err)
		}
		if events := drain(); len(events) != 0 {
			t.Fatal("duplicate report notified again", events)
		}
		for _, change := range []func(*model.Alarm){func(a *model.Alarm) { a.DeviceID = "another-device" }, func(a *model.Alarm) { a.RuleID = "another:external:rule" }} {
			wrong := v
			wrong.TriggerID = "trigger-for-wrong-identity"
			change(&wrong)
			if _, _, _, err := repo.UpsertExternalAlarm(ctx, wrong); !errors.Is(err, model.ErrInvalidIngress) {
				t.Fatalf("reused ID for another object: %v", err)
			}
		}
		unchanged, err := repo.GetAlarm(ctx, v.TenantID, v.ID)
		if err != nil || unchanged.Version != updated.Version || unchanged.DeviceID != v.DeviceID || unchanged.RuleID != v.RuleID || unchanged.Details["content"] != "new report" || len(drain()) != 0 {
			t.Fatalf("rejected identity changed alarm or outbox: %+v %v", unchanged, err)
		}
		v.TenantID = "external-alarm-other"
		if got, created, changed, err := repo.UpsertExternalAlarm(ctx, v); err != nil || !created || !changed || got.TriggerCount != 1 {
			t.Fatalf("tenant identity leaked: %+v %v %v %v", got, created, changed, err)
		}
		drain()
	})
	for _, status := range []string{"CLOSED", "RECOVERED"} {
		t.Run(status+" wins concurrent incoming versions", func(t *testing.T) {
			for attempt := range 8 {
				v := report(fmt.Sprintf("%s-%d", status, attempt))
				if _, _, _, err := repo.UpsertExternalAlarm(ctx, v); err != nil {
					t.Fatal(err)
				}
				drain()
				start := make(chan struct{})
				var wg sync.WaitGroup
				wg.Add(2)
				go func() {
					defer wg.Done()
					<-start
					incoming := v
					incoming.TriggerID, incoming.LastTriggeredAt = "trigger-race", 20
					if _, _, _, err := repo.UpsertExternalAlarm(ctx, incoming); err != nil {
						t.Error(err)
					}
				}()
				go func() {
					defer wg.Done()
					<-start
					for range 20 {
						current, err := repo.GetAlarm(ctx, v.TenantID, v.ID)
						if err != nil {
							t.Error(err)
							return
						}
						current.Status = status
						current.RecoveredAt, current.ClosedAt = 30, 30
						ok, err := repo.UpdateAlarmIf(ctx, current)
						if err != nil {
							t.Error(err)
							return
						}
						if ok {
							return
						}
					}
					t.Error("terminal transition exhausted CAS retries")
				}()
				close(start)
				wg.Wait()
				before, err := repo.GetAlarm(ctx, v.TenantID, v.ID)
				if err != nil || before.Status != status {
					t.Fatalf("terminal state lost: %+v %v", before, err)
				}
				events := drain()
				if len(events) != before.TriggerCount-1 {
					t.Fatalf("concurrent accepted count=%d outbox=%d", before.TriggerCount, len(events))
				}
				v.TriggerID = "trigger-late"
				got, created, changed, err := repo.UpsertExternalAlarm(ctx, v)
				if err != nil || created || changed || got.Status != status || got.Version != before.Version || got.TriggerCount != before.TriggerCount || got.TriggerID != before.TriggerID || got.RecoveredAt != 30 || got.ClosedAt != 30 {
					t.Fatalf("terminal event reopened or changed: %+v %v %v %v", got, created, changed, err)
				}
				if len(drain()) != 0 {
					t.Fatal("terminal event generated another report")
				}
			}
		})
	}
}
