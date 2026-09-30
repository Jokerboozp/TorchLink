package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func factRepository(t *testing.T) *Repository {
	t.Helper()
	repo := testRepository(t)
	if _, err := repo.pool.Exec(context.Background(), AnalyticsFactSchema()); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.pool.Exec(context.Background(), `UPDATE analytics_source_collection SET collection_started_at=0`); err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestAnalyticsFactMeasurementsPaginationAvailabilityAndSnapshot(t *testing.T) {
	repo := factRepository(t)
	ctx := context.Background()
	received := time.Now().UnixMilli() - 5000
	for _, m := range []model.StandardMessage{{TenantID: "t", MessageID: "m1", RawMessageID: "r1", DeviceID: "a", ProductID: "p", MessageType: model.PropertyReport, Timestamp: 100, Properties: map[string]any{"pressure": 1, "temperature": 2}}, {TenantID: "t", MessageID: "m2", RawMessageID: "r2", DeviceID: "a", ProductID: "p", MessageType: model.PropertyReport, Timestamp: 100, Properties: map[string]any{"pressure": 3}}, {TenantID: "t", MessageID: "unknown", RawMessageID: "r3", DeviceID: "a", ProductID: "p", MessageType: model.PropertyReport, Timestamp: 110, Properties: map[string]any{"pressure": 4}}, {TenantID: "other", MessageID: "hidden", RawMessageID: "other", DeviceID: "a", ProductID: "p", Timestamp: 100, Properties: map[string]any{"pressure": 9}}} {
		b, _ := json.Marshal(m)
		if _, err := repo.pool.Exec(ctx, `INSERT INTO standard_message(tenant_id,message_id,raw_message_id,product_id,device_id,message_type,ts,properties,body) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, m.TenantID, m.MessageID, m.RawMessageID, m.ProductID, m.DeviceID, m.MessageType, m.Timestamp, m.Properties, b); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.SaveRawIndex(ctx, model.RawArchiveIndex{TenantID: m.TenantID, MessageID: m.RawMessageID, ProductID: m.ProductID, DeviceID: m.DeviceID, ReceivedAt: received, ArchivedAt: received + 1}); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.RecordMeasurementAvailability(ctx, "t", "m1", "postgres_standard_commit"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.pool.Exec(ctx, `UPDATE standard_message SET processed_at=123 WHERE tenant_id='t' AND message_id='m2'`); err != nil {
		t.Fatal(err)
	}
	var first int64
	if err := repo.pool.QueryRow(ctx, `SELECT available_at FROM measurement_availability WHERE tenant_id='t' AND message_id='m1'`).Scan(&first); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordMeasurementAvailability(ctx, "t", "m1", "postgres_standard_commit"); err != nil {
		t.Fatal(err)
	}
	var again int64
	if err := repo.pool.QueryRow(ctx, `SELECT available_at FROM measurement_availability WHERE tenant_id='t' AND message_id='m1'`).Scan(&again); err != nil || again != first {
		t.Fatal("first receipt changed", first, again, err)
	}
	var cursor string
	if err := repo.AnalyticsFactsRead(ctx, "t", func(reader ports.AnalyticsFactReader) error {
		q := model.FactQuery{DeviceIDs: []string{"a"}, Start: 90, End: 120, Limit: 1}
		seen := []model.MeasurementFact{}
		for {
			page, err := reader.QueryMeasurementSeries(q)
			if err != nil {
				return err
			}
			if len(page.Items) != 1 {
				t.Fatalf("page: %+v", page)
			}
			seen = append(seen, page.Items...)
			if cursor == "" {
				cursor = page.Cursor
				// A concurrently committed source row is not admitted to later pages in
				// the already established repeatable-read snapshot.
				m := model.StandardMessage{TenantID: "t", MessageID: "late", RawMessageID: "late", DeviceID: "a", ProductID: "p", Timestamp: 105, Properties: map[string]any{"pressure": 5}}
				if _, err = repo.SaveStandardMessageIfAbsent(ctx, m); err != nil {
					return err
				}
			}
			if !page.HasMore {
				break
			}
			q.Cursor = page.Cursor
			q.SourceVersion = page.Source.SourceVersion
		}
		if len(seen) != 4 || seen[0].ID != "m1:pressure" || seen[1].ID != "m1:temperature" || seen[2].AvailableAt != 123 || seen[2].AvailableAtSource != "historical_processed_at" || seen[3].AvailableAt != 0 || seen[3].HistoricalReconstructionQuality != "UNKNOWN" || seen[0].ReceivedAt != received || seen[0].AvailableAt < received {
			t.Fatalf("measurement facts: %+v", seen)
		}
		q.Cursor = cursor
		q.DeviceIDs = []string{"b"}
		if _, err := reader.QueryMeasurementSeries(q); err == nil {
			t.Fatal("cursor admitted changed scope")
		}
		q = model.FactQuery{DeviceIDs: []string{"a"}, Start: 90, End: 120, Properties: []string{"pressure"}}
		p, err := reader.QueryMeasurementSeries(q)
		if err == nil && (len(p.Items) != 3 || p.Source.Complete) {
			t.Fatal("unknown availability was asserted complete", p)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
func TestAnalyticsFactsPersistentLifecycleRawOutcomesAndState(t *testing.T) {
	repo := factRepository(t)
	ctx := context.Background()
	for _, idx := range []model.RawArchiveIndex{{TenantID: "t", MessageID: "unattempted", DeviceID: "a", ProductID: "p", ReceivedAt: 10, ArchivedAt: 11}, {TenantID: "t", MessageID: "failed", DeviceID: "a", ProductID: "p", ReceivedAt: 12, ArchivedAt: 13}} {
		if _, err := repo.SaveRawIndex(ctx, idx); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.MarkRawParseResult(ctx, "t", "failed", 14, "invalid frame"); err != nil {
		t.Fatal(err)
	}
	if err := repo.DutyTransaction(ctx, "t", func(tx ports.DutyTx) error {
		for i, kind := range []string{"ALARM_CREATED", "ALARM_REPORTED", "ALARM_ACKNOWLEDGED", "ALARM_RECOVERED", "ALARM_CLOSED"} {
			if err := tx.AppendEvent(model.DutyBusinessEvent{ID: kind, TenantID: "t", DeviceID: "a", Type: kind, Source: "alarm", ResourceID: "alarm", ResourceVersion: int64(i + 1), OccurredAt: int64(10 + i*5), RecordedAt: 40, Body: json.RawMessage(`{}`)}); err != nil {
				return err
			}
		}
		body, _ := json.Marshal(model.DeviceState{TenantID: "t", DeviceID: "a", ConnectionStatus: "DISCONNECTED"})
		return tx.AppendEvent(model.DutyBusinessEvent{ID: "seed", TenantID: "t", DeviceID: "a", Type: "DEVICE_STATUS_CHANGED", Source: "device", ResourceID: "a", ResourceVersion: 1, OccurredAt: 1, RecordedAt: 2, Body: body})
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.AnalyticsFactsRead(ctx, "t", func(reader ports.AnalyticsFactReader) error {
		q := model.FactQuery{DeviceIDs: []string{"a", "b"}, Start: 5, End: 50}
		reports, err := reader.ListAlarmReportEvents(q)
		if err != nil {
			return err
		}
		if len(reports.Items) != 2 || reports.Items[1].Type != "ALARM_REPORTED" {
			t.Fatal(reports)
		}
		lifecycle, err := reader.ListAlarmLifecycleEvents(q)
		if err != nil {
			return err
		}
		if len(lifecycle.Items) != 3 || lifecycle.Items[0].Type != "ALARM_ACKNOWLEDGED" || lifecycle.Items[0].ActorKind != "AUTOMATIC" {
			t.Fatal(lifecycle)
		}
		raw, err := reader.ListRawParseOutcomes(q)
		if err != nil {
			return err
		}
		if len(raw.Items) != 2 || raw.Items[0].Outcome != "ARCHIVED_NOT_ATTEMPTED" || raw.Items[1].Outcome != "LAST_ATTEMPT_FAILED" {
			t.Fatal(raw)
		}
		states, err := reader.ListDeviceStateIntervals(q)
		if err != nil {
			return err
		}
		if len(states.Items) != 2 || states.Items[0].State.ConnectionStatus != "DISCONNECTED" || states.Items[1].Quality != "UNKNOWN" || states.Source.Complete {
			t.Fatal(states)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
func TestAnalyticsConfigurationHistoryAndHiddenParent(t *testing.T) {
	repo := factRepository(t)
	ctx := context.Background()
	before := time.Now().UnixMilli() - 100
	device := model.ManagedDevice{TenantID: "t", ID: "child", ProductID: "p", Status: "ENABLED", AccessKey: "test-key", GatewayID: "hidden", ConnectorProfileID: "profile"}
	if err := repo.SaveManagedDevice(ctx, device); err != nil {
		t.Fatal(err)
	}
	device.GatewayID = "visible"
	if err := repo.SaveManagedDevice(ctx, device); err != nil {
		t.Fatal(err)
	}
	if err := repo.AnalyticsFactsRead(ctx, "t", func(reader ports.AnalyticsFactReader) error {
		q := model.FactQuery{DeviceIDs: []string{"child"}, Start: before, End: time.Now().UnixMilli() + 100}
		page, err := reader.GetConfigurationHistory(q)
		if err != nil {
			return err
		}
		if len(page.Items) != 2 {
			t.Fatalf("expected two immutable revisions: %+v", page)
		}
		for _, v := range page.Items {
			if strings.Contains(string(v.Body), "hidden") || strings.Contains(string(v.Body), "visible") || strings.Contains(string(v.Body), "test-key") {
				t.Fatal("unauthorized parent or credentials leaked", string(v.Body))
			}
		}
		dependencies, err := reader.GetDependencySnapshot(q)
		if err != nil {
			return err
		}
		if dependencies.Complete || !strings.Contains(strings.Join(dependencies.Source.Limitations, ";"), "DEPENDENCY_WINDOW_SEED_MISSING") {
			t.Fatal("missing window seed was declared historical coverage", dependencies.Source)
		}
		for _, v := range dependencies.Items {
			if v.Kind == "parent-device" {
				t.Fatal("unauthorized parent exposed", v)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestAnalyticsStateIntervalsIncludeLegacyTransitionsAndPaginate(t *testing.T) {
	repo := factRepository(t)
	ctx := context.Background()
	if _, err := repo.pool.Exec(ctx, `UPDATE analytics_source_collection SET collection_started_at=1000`); err != nil {
		t.Fatal(err)
	}
	for _, at := range []int64{80, 120, 140, 160, 180, 200} {
		state := model.DeviceState{TenantID: "t", DeviceID: "a", ConnectionStatus: "CONNECTED"}
		if at == 80 || at == 140 || at == 160 {
			state.ConnectionStatus = "DISCONNECTED"
		}
		b, _ := json.Marshal(state)
		if _, err := repo.pool.Exec(ctx, `INSERT INTO device_state_event(tenant_id,device_id,business_status,body,created_at) VALUES('t','a','ONLINE',$1,to_timestamp($2::double precision/1000))`, b, at); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.DutyTransaction(ctx, "t", func(tx ports.DutyTx) error {
		for _, at := range []int64{50, 100, 140} {
			state := model.DeviceState{TenantID: "t", DeviceID: "a", ConnectionStatus: "DISCONNECTED"}
			if at == 140 {
				state.ConnectionStatus = "CONNECTED"
			}
			b, _ := json.Marshal(state)
			if err := tx.AppendEvent(model.DutyBusinessEvent{ID: fmt.Sprintf("reliable:%d", at), Type: "DEVICE_STATUS_CHANGED", Source: "device", DeviceID: "a", ResourceID: "a", OccurredAt: at, RecordedAt: at, Body: b}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.AnalyticsFactsRead(ctx, "t", func(reader ports.AnalyticsFactReader) error {
		q := model.FactQuery{DeviceIDs: []string{"a"}, Start: 100, End: 200, Limit: 2}
		all := []model.DeviceStateIntervalFact{}
		for {
			page, err := reader.ListDeviceStateIntervals(q)
			if err != nil {
				return err
			}
			if len(page.Items) > 2 {
				t.Fatal("unbounded interval batch", page)
			}
			all = append(all, page.Items...)
			if !page.HasMore {
				break
			}
			q.Cursor = page.Cursor
			q.SourceVersion = page.Source.SourceVersion
		}
		if len(all) != 5 || all[0].Start != 100 || all[0].State.ConnectionStatus != "DISCONNECTED" || all[1].Start != 120 || all[1].Quality != "UNKNOWN" || all[2].Start != 140 || all[2].State.ConnectionStatus != "CONNECTED" || all[2].Quality != "KNOWN" || all[4].End != 200 {
			t.Fatalf("full legacy and reliable interval pages: %+v", all)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
func TestAnalyticsCollectorDependencyReceiptsAndFlatPagination(t *testing.T) {
	repo := factRepository(t)
	ctx := context.Background()
	before := time.Now().UnixMilli() - 10
	if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "t", ID: "child", ProductID: "p", AccessKey: "key", Status: "ENABLED", GatewayID: "parent", ConnectorProfileID: "profile"}); err != nil {
		t.Fatal(err)
	}
	raw := model.RawMessage{TenantID: "t", MessageID: "collector-raw", DeviceID: "child", ProductID: "p", ReceivedAt: time.Now().UnixMilli(), CollectorID: "collector", ProtocolVersion: "v2", PointTableVersion: "points", Payload: json.RawMessage(`{}`)}
	if _, err := repo.ReserveRawMessage(ctx, raw); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SaveRawIndex(ctx, model.RawArchiveIndex{TenantID: raw.TenantID, MessageID: raw.MessageID, DeviceID: raw.DeviceID, ProductID: raw.ProductID, ReceivedAt: raw.ReceivedAt, ArchivedAt: raw.ReceivedAt + 1, ObjectBucket: "clickhouse"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.AnalyticsFactsRead(ctx, "t", func(reader ports.AnalyticsFactReader) error {
		q := model.FactQuery{DeviceIDs: []string{"child", "parent"}, Start: before, End: time.Now().UnixMilli() + 10, Limit: 1}
		all := []model.DependencyFact{}
		for {
			page, err := reader.GetDependencySnapshot(q)
			if err != nil {
				return err
			}
			if len(page.Items) > 1 {
				t.Fatal("dependency page exceeds limit", page)
			}
			all = append(all, page.Items...)
			if !page.HasMore {
				break
			}
			q.Cursor = page.Cursor
		}
		if len(all) != 3 {
			t.Fatalf("expected independently evidenced parent/profile/collector: %+v", all)
		}
		q.Cursor = ""
		raws, err := reader.ListRawParseOutcomes(q)
		if err != nil {
			return err
		}
		if len(raws.Items) != 1 || raws.Items[0].ProtocolVersion != "v2" || raws.Items[0].PointTableVersion != "points" {
			t.Fatal("reservation metadata absent for ClickHouse archive", raws)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
