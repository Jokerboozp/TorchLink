package postgres

import (
	"context"
	"testing"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/notify/notifytest"
)

func TestPurgeRangeKeepsGuardedRows(t *testing.T) {
	ctx := context.Background()
	r := testRepository(t)
	old := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	recent := time.Now()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := r.pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	for i, row := range []struct {
		id        string
		ts        time.Time
		processed bool
	}{{"old-done", old, true}, {"old-pending", old, false}, {"new-done", recent, true}} {
		processed := int64(0)
		if row.processed {
			processed = row.ts.UnixMilli()
		}
		exec(`INSERT INTO standard_message(tenant_id,message_id,raw_message_id,product_id,device_id,message_type,ts,body,processed_at) VALUES('t',$1,$1,'p','d','PROPERTY_REPORT',$2,'{}',$3)`, row.id, int64(i), processed)
	}
	for _, row := range []struct {
		id, status string
		at         time.Time
	}{{"closed-old", "CLOSED", old}, {"active-old", "ACTIVE", old}, {"closed-new", "CLOSED", recent}} {
		exec(`INSERT INTO alarm_record(tenant_id,id,rule_id,device_id,status,level,source,last_triggered_at,body) VALUES('t',$1,$1,'d',$2,'HIGH','iot',$3,'{}')`, row.id, row.status, row.at.UnixMilli())
	}
	exec(`INSERT INTO raw_archive_index(tenant_id,product_id,device_id,message_id,object_bucket,object_key,payload_hash,payload_size,received_at,archived_at,published_at) VALUES
  ('t','p','d','published','postgres','k','h',1,$1,$1,$1),('t','p','d','unpublished','postgres','k','h',1,$1,$1,0)`, old.UnixMilli())
	exec(`INSERT INTO raw_ingest_reservation(tenant_id,message_id,payload_hash,metadata,created_at) VALUES('t','old','h','{}',$1),('t','new','h','{}',now())`, old)

	cutoff := recent.Add(-time.Hour)
	for table, want := range map[string]int64{
		model.RetentionStandardMessages: 1,
		model.RetentionAlarms:           1,
		model.RetentionRawIndex:         1,
		model.RetentionReservations:     1,
	} {
		got, err := r.PurgeRange(ctx, table, time.Time{}, cutoff, 100)
		if err != nil || got != want {
			t.Fatalf("%s deleted=%d err=%v", table, got, err)
		}
	}
	count := func(sql string) int {
		var n int
		if err := r.pool.QueryRow(ctx, sql).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if count(`SELECT count(*) FROM standard_message WHERE message_id IN ('old-pending','new-done')`) != 2 {
		t.Fatal("unprocessed or recent standard messages were purged")
	}
	if count(`SELECT count(*) FROM alarm_record WHERE id IN ('active-old','closed-new')`) != 2 {
		t.Fatal("active or recent alarms were purged")
	}
	if count(`SELECT count(*) FROM raw_archive_index WHERE message_id='unpublished'`) != 1 {
		t.Fatal("unpublished raw messages must be kept for the publish retry")
	}
	// A lower bound restricts a purge to one day.
	exec(`INSERT INTO audit_log(id,tenant_id,actor,action,target_type,target_id,created_at) VALUES('a1','t','x','y','z','1',$1),('a2','t','x','y','z','2',$2)`, old.UnixMilli(), old.AddDate(0, 0, 2).UnixMilli())
	if got, err := r.PurgeRange(ctx, model.RetentionAudit, old.AddDate(0, 0, 1), cutoff, 100); err != nil || got != 1 {
		t.Fatalf("bounded purge deleted=%d err=%v", got, err)
	}
	if oldest, ok, err := r.OldestRetained(ctx, model.RetentionAudit); err != nil || !ok || !oldest.Equal(old) {
		t.Fatalf("oldest=%v ok=%v err=%v", oldest, ok, err)
	}
	if _, err := r.PurgeRange(ctx, "device_registry", time.Time{}, cutoff, 1); err == nil {
		t.Fatal("tables outside the retention policy must be refused")
	}
}

func TestBackupWindows(t *testing.T) {
	ctx := context.Background()
	r := testRepository(t)
	day := time.Date(2026, 9, 30, 0, 0, 0, 0, time.FixedZone("CST", 8*3600))
	if _, err := r.pool.Exec(ctx, `INSERT INTO backup_task(id,backup_type,status,details,started_at) VALUES
 ('d','DEVICE_DAILY','COMPLETED',jsonb_build_object('components',jsonb_build_object('start',$1::text,'end',$2::text)),now()),
 ('f','FULL','COMPLETED','{}',$3),
 ('x','DEVICE_DAILY','FAILED','{}',now())`, day.Format(time.RFC3339Nano), day.AddDate(0, 0, 1).Format(time.RFC3339Nano), day); err != nil {
		t.Fatal(err)
	}
	windows, err := r.BackupWindows(ctx)
	if err != nil || len(windows) != 2 {
		t.Fatalf("windows=%v err=%v", windows, err)
	}
	daily, full := false, false
	for _, w := range windows {
		daily = daily || (w.Start.Equal(day) && w.End.Equal(day.AddDate(0, 0, 1)))
		full = full || (w.Start.IsZero() && w.End.Equal(day))
	}
	if !daily || !full {
		t.Fatalf("windows=%v", windows)
	}
}

func TestNotificationStoreContract(t *testing.T) {
	notifytest.StoreContract(t, testRepository(t).NotificationStore())
}

func TestOfflineDueFollowsDeviceStateWrites(t *testing.T) {
	ctx := context.Background()
	r := testRepository(t)
	now := time.Now().UnixMilli()
	states := []model.DeviceState{
		{TenantID: "t", DeviceID: "due", LastSeenAt: now - 600_000, ReportIntervalSec: 60, OfflineToleranceSec: 60, DataStatus: "ACTIVE", BusinessStatus: "ONLINE"},
		{TenantID: "t", DeviceID: "fresh", LastSeenAt: now, ReportIntervalSec: 60, OfflineToleranceSec: 60, DataStatus: "ACTIVE", BusinessStatus: "ONLINE"},
		{TenantID: "t", DeviceID: "never", DataStatus: "UNKNOWN"},
	}
	done := model.DeviceState{TenantID: "t", DeviceID: "offline", LastSeenAt: now - 600_000, ReportIntervalSec: 60, OfflineToleranceSec: 60, DataStatus: "SILENT", BusinessStatus: "OFFLINE"}
	done.OfflineAt = done.OfflineDeadline()
	states = append(states, done)
	for _, s := range states {
		if err := r.UpsertDeviceState(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	due, err := r.ListOfflineDue(ctx, now, 10)
	if err != nil || len(due) != 1 || due[0].DeviceID != "due" || due[0].Version == 0 {
		t.Fatalf("due=%+v err=%v", due, err)
	}
	// The backfill migration computes the same value as the Go writes.
	if _, err = r.pool.Exec(ctx, `UPDATE device_state SET offline_check_at = -1`); err != nil {
		t.Fatal(err)
	}
	backfill, err := migrationFiles.ReadFile("migrations/0004_device_state_offline_backfill.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.pool.Exec(ctx, string(backfill)); err != nil {
		t.Fatal(err)
	}
	for _, s := range states {
		var got int64
		if err = r.pool.QueryRow(ctx, `SELECT offline_check_at FROM device_state WHERE device_id=$1`, s.DeviceID).Scan(&got); err != nil || got != s.OfflineCheckAt() {
			t.Fatalf("%s backfill=%d want=%d err=%v", s.DeviceID, got, s.OfflineCheckAt(), err)
		}
	}
}
