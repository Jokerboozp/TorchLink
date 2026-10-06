package postgres

import (
	"context"
	"fmt"
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
		exec(`INSERT INTO alarm_record(tenant_id,id,rule_id,device_id,status,level,source,last_triggered_at,body) VALUES('t',$1,$1,'d',$2,'HIGH','iot',$3,'{"attachments":[{"id":"att_1"},{"id":"att_2"}]}')`, row.id, row.status, row.at.UnixMilli())
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
	// Only the purged alarm's attachment files are queued for deletion.
	refs, err := r.PendingObjectCleanups(ctx, 10)
	if err != nil || len(refs) != 2 || refs[0].Bucket != model.AlarmAttachmentBucket || refs[0].Key != model.AlarmAttachmentKey("t", "closed-old", "att_1") && refs[1].Key != model.AlarmAttachmentKey("t", "closed-old", "att_1") {
		t.Fatalf("queued attachment files %+v %v", refs, err)
	}
	if err = r.FinishObjectCleanup(ctx, refs[0].Bucket, refs[0].Key); err != nil {
		t.Fatal(err)
	}
	if refs, _ = r.PendingObjectCleanups(ctx, 10); len(refs) != 1 {
		t.Fatalf("finished cleanup still queued: %+v", refs)
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

func TestBatchedRawMarks(t *testing.T) {
	ctx := context.Background()
	r := testRepository(t)
	r.marks = newRawMarks(r)
	now := time.Now().UnixMilli()
	for _, id := range []string{"m1", "m2"} {
		if _, err := r.pool.Exec(ctx, `INSERT INTO raw_archive_index(tenant_id,product_id,device_id,message_id,object_bucket,object_key,payload_hash,payload_size,received_at,archived_at) VALUES('t','p','d',$1,'postgres','k','h',1,$2,$2)`, id, now); err != nil {
			t.Fatal(err)
		}
	}
	_ = r.MarkRawPublished(ctx, "t", "m1", now+1, "")
	_ = r.MarkRawParseResult(ctx, "t", "m1", now+2, "")
	_ = r.MarkRawParseResult(ctx, "t", "m2", now+3, "bad frame")
	// A failed publish is written at once.
	if err := r.MarkRawPublished(ctx, "t", "m2", 0, "broker down"); err != nil {
		t.Fatal(err)
	}
	r.marks.close()
	var published, parsedAt int64
	var parseError, publishError string
	if err := r.pool.QueryRow(ctx, `SELECT published_at,parse_attempted_at FROM raw_archive_index WHERE message_id='m1'`).Scan(&published, &parsedAt); err != nil || published != now+1 || parsedAt != now+2 {
		t.Fatalf("m1 published=%d parsed=%d %v", published, parsedAt, err)
	}
	if err := r.pool.QueryRow(ctx, `SELECT parse_error,last_publish_error,published_at FROM raw_archive_index WHERE message_id='m2'`).Scan(&parseError, &publishError, &published); err != nil || parseError != "bad frame" || publishError != "broker down" || published != 0 {
		t.Fatalf("m2 %q %q %d %v", parseError, publishError, published, err)
	}
	// Freshly archived unpublished messages are left to the normal path.
	if pending, err := r.ListPendingRawIndexes(ctx, 10); err != nil || len(pending) != 0 {
		t.Fatalf("fresh message offered for republish: %v %v", pending, err)
	}
}

func TestPurgeRangeKeepsUnfinishedTasks(t *testing.T) {
	ctx := context.Background()
	r := testRepository(t)
	old := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	oldMS := old.UnixMilli()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := r.pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO device_command(tenant_id,id,device_id,status,created_at,body) VALUES('t','cmd-old','d','SUCCEEDED',$1,'{}'),('t','cmd-new','d','SENT',$2,'{}')`, oldMS, time.Now().UnixMilli())
	exec(`INSERT INTO onboarding_record(tenant_id,id,owner_id,kind,status,revision,created_at,updated_at,body) VALUES
  ('t','draft','u','device-draft','DRAFT',1,$1,$1,'{}'),
  ('t','done','u','device-batch','COMPLETED',1,$1,$1,'{}'),
  ('t','batch-done-0','u','device-batch-row:done','SUCCEEDED',1,$1,$1,'{}'),
  ('t','partial','u','device-batch','PARTIAL_FAILED',1,$1,$1,'{}'),
  ('t','batch-partial-0','u','device-batch-row:partial','FAILED',1,$1,$1,'{}')`, oldMS)
	exec(`INSERT INTO replay_task(id,tenant_id,status,body,created_at) VALUES('replay-done','t','COMPLETED','{}',$1),('replay-running','t','RUNNING','{}',$1)`, old)
	exec(`INSERT INTO backup_task(id,backup_type,status,started_at) VALUES('b-ok','FULL','COMPLETED',$1),('b-failed','FULL','FAILED',$1),('b-restore','RESTORE','COMPLETED',$1),('b-running','FULL','RUNNING',$1)`, old)
	exec(`INSERT INTO health_inspection_job(tenant_id,id,status,started_at,updated_at,body) VALUES
  ('t','insp-old','succeeded',$1,$1,'{}'),('t','insp-latest','succeeded',$2,$2,'{}'),('t','insp-failed','failed',$1,$1,'{}'),('t','insp-running','running',$1,$1,'{}')`, oldMS, oldMS+1000)

	cutoff := time.Now().Add(-time.Hour)
	purge := func(table string) {
		t.Helper()
		for {
			n, err := r.PurgeRange(ctx, table, time.Time{}, cutoff, 100)
			if err != nil {
				t.Fatal(table, err)
			}
			if n == 0 {
				return
			}
		}
	}
	ids := func(sql string) []string {
		t.Helper()
		rows, err := r.pool.Query(ctx, sql)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		out := []string{}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			out = append(out, id)
		}
		return out
	}
	for table, check := range map[string]struct{ query, want string }{
		model.RetentionDeviceCommands: {`SELECT id FROM device_command ORDER BY id`, "[cmd-new]"},
		// The finished batch goes first, its rows on the following pass.
		model.RetentionOnboarding:        {`SELECT id FROM onboarding_record ORDER BY id`, "[batch-partial-0 draft partial]"},
		model.RetentionReplays:           {`SELECT id FROM replay_task ORDER BY id`, "[replay-running]"},
		model.RetentionBackupTasks:       {`SELECT id FROM backup_task ORDER BY id`, "[b-ok b-running]"},
		model.RetentionHealthInspections: {`SELECT id FROM health_inspection_job ORDER BY id`, "[insp-latest insp-running]"},
	} {
		purge(table)
		if got := fmt.Sprint(ids(check.query)); got != check.want {
			t.Fatalf("%s kept %s, want %s", table, got, check.want)
		}
	}
}
