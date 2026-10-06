package postgres

import (
	"context"
	"testing"
	"time"

	"iot-platform/internal/model"
)

// TestPartitionUpgradeKeepsRowsAndIDs upgrades a database that already holds
// rows, then checks that the rows stay readable, message IDs stay unique
// across partitions, and retention can drop expired months.
func TestPartitionUpgradeKeepsRowsAndIDs(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	all, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	before := []migration{}
	for _, m := range all {
		if m.version < 10 {
			before = append(before, m)
		}
	}
	if err = migrateWith(ctx, pool, before); err != nil {
		t.Fatal(err)
	}
	r := &Repository{pool: pool}
	old := time.Now().Add(-48 * time.Hour).UnixMilli()
	raw := model.RawArchiveIndex{TenantID: "t", ProductID: "p", DeviceID: "d", MessageID: "old-raw", ObjectBucket: "b", ObjectKey: "k", PayloadHash: "h", PayloadSize: 1, ReceivedAt: old, ArchivedAt: old, PublishedAt: old}
	if created, err := r.SaveRawIndex(ctx, raw); err != nil || !created {
		t.Fatalf("legacy raw index: %v %v", created, err)
	}
	msg := model.StandardMessage{TenantID: "t", MessageID: "old-msg", RawMessageID: "old-raw", ProductID: "p", DeviceID: "d", MessageType: model.PropertyReport, Timestamp: old, Properties: map[string]any{"v": 1}}
	// The statement the previous release used.
	if _, err = pool.Exec(ctx, `INSERT INTO standard_message(tenant_id,message_id,raw_message_id,product_id,device_id,message_type,ts,body,claim_owner,claim_token,claim_expires_at,attempts)
VALUES('t','old-msg','old-raw','p','d','PROPERTY_REPORT',$1,'{"messageId":"old-msg","tenantId":"t"}','worker',1,`+nowMS+`+60000,1)`, old); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO device_state_event(tenant_id,device_id,business_status,body) VALUES('t','d','ONLINE','{}')`); err != nil {
		t.Fatal(err)
	}
	if err = r.SaveAudit(ctx, model.AuditLog{ID: "a-old", TenantID: "t", Actor: "x", Action: "y", TargetType: "z", TargetID: "1", CreatedAt: old}); err != nil {
		t.Fatal(err)
	}

	if err = r.Migrate(ctx); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if err = r.Migrate(ctx); err != nil {
		t.Fatalf("repeated startup after the switch: %v", err)
	}
	for _, table := range []string{"raw_archive_index", "raw_message_log", "standard_message", "device_state_event", "audit_log"} {
		if partitioned, err := r.isPartitioned(ctx, table); err != nil || !partitioned {
			t.Fatalf("%s not partitioned: %v", table, err)
		}
		var duplicateIndexes int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM pg_indexes WHERE tablename=$1 AND schemaname=current_schema()`, table).Scan(&duplicateIndexes); err != nil {
			t.Fatal(err)
		}
		var legacyIndexes int
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM pg_indexes WHERE tablename=$1 AND schemaname=current_schema()`, table+"_legacy").Scan(&legacyIndexes)
		if duplicateIndexes != legacyIndexes {
			rows, _ := pool.Query(ctx, `SELECT tablename, indexname, indexdef FROM pg_indexes WHERE tablename = ANY($1::text[]) AND schemaname=current_schema() ORDER BY 1,2`, []string{table, table + "_legacy"})
			for rows.Next() {
				var a, b, c string
				_ = rows.Scan(&a, &b, &c)
				t.Log(a, b, c)
			}
			rows.Close()
			t.Fatalf("%s: parent has %d indexes, legacy partition %d; the baseline must not add more", table, duplicateIndexes, legacyIndexes)
		}
	}

	// Old rows stay readable through the parent.
	if got, err := r.GetRawIndex(ctx, "t", "old-raw"); err != nil || got.ReceivedAt != old {
		t.Fatalf("legacy raw index after upgrade: %+v %v", got, err)
	}
	if got, err := r.GetStandardMessageByRaw(ctx, "t", "old-raw"); err != nil || got.MessageID != "old-msg" {
		t.Fatalf("legacy standard message: %+v %v", got, err)
	}
	// A redelivered legacy message is neither inserted again nor claimed by
	// another worker while its lease runs.
	if claim, err := r.ClaimStandardMessage(ctx, msg, "other", time.Minute); err != nil || claim.Created || claim.ShouldProcess {
		t.Fatalf("legacy ID duplicated: %+v %v", claim, err)
	}
	if created, err := r.SaveRawIndex(ctx, model.RawArchiveIndex{TenantID: "t", ProductID: "p", DeviceID: "d", MessageID: "old-raw", ObjectBucket: "b", ObjectKey: "k", PayloadHash: "h", ReceivedAt: time.Now().UnixMilli(), ArchivedAt: time.Now().UnixMilli()}); err != nil || created {
		t.Fatalf("legacy raw ID inserted again into a new month: %v %v", created, err)
	}
	// New messages stay unique after their key expired from the key table.
	fresh := msg
	fresh.MessageID, fresh.RawMessageID = "new-msg", "new-raw"
	if claim, err := r.ClaimStandardMessage(ctx, fresh, "worker", time.Minute); err != nil || !claim.Created {
		t.Fatalf("new message: %+v %v", claim, err)
	}
	if _, err = pool.Exec(ctx, `DELETE FROM standard_message_key`); err != nil {
		t.Fatal(err)
	}
	if inserted, err := r.SaveStandardMessageIfAbsent(ctx, fresh); err != nil || inserted {
		t.Fatalf("expired key let the ID in again: %v %v", inserted, err)
	}
	var rows int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM standard_message WHERE message_id IN ('old-msg','new-msg')`).Scan(&rows); err != nil || rows != 2 {
		t.Fatalf("standard rows %d %v", rows, err)
	}
	// The serial sequence belongs to the parent, so dropping the emptied
	// legacy partition keeps state events insertable.
	if _, err = pool.Exec(ctx, `DELETE FROM device_state_event_legacy`); err != nil {
		t.Fatal(err)
	}
	if dropped, err := r.DropEmptyLegacy(ctx, "device_state_event"); err != nil || !dropped {
		t.Fatalf("drop empty legacy: %v %v", dropped, err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO device_state_event(tenant_id,device_id,business_status,body) VALUES('t','d','OFFLINE','{}')`); err != nil {
		t.Fatalf("insert after legacy drop: %v", err)
	}
	if dropped, err := r.DropEmptyLegacy(ctx, "raw_archive_index"); err != nil || dropped {
		t.Fatalf("non-empty legacy dropped: %v %v", dropped, err)
	}

	// Months ahead are created, and an expired month holding an unpublished
	// raw message is kept until it is published.
	var cutoverMS int64
	if err = pool.QueryRow(ctx, `SELECT cutover_ms FROM partition_cutover WHERE table_name='raw_archive_index'`).Scan(&cutoverMS); err != nil {
		t.Fatal(err)
	}
	cutover := time.UnixMilli(cutoverMS).UTC()
	if err = r.EnsurePartitions(ctx, cutover.AddDate(0, 3, 0)); err != nil {
		t.Fatal(err)
	}
	spec, _ := partitionSpec("raw_archive_index")
	var exists bool
	if err = pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, spec.monthName(cutover.AddDate(0, 6, 0))).Scan(&exists); err != nil || !exists {
		t.Fatalf("upcoming partition missing: %v", err)
	}
	pending := model.RawArchiveIndex{TenantID: "t", ProductID: "p", DeviceID: "d", MessageID: "future", ObjectBucket: "b", ObjectKey: "k", PayloadHash: "h", ReceivedAt: cutover.Add(time.Hour).UnixMilli(), ArchivedAt: cutover.UnixMilli()}
	if created, err := r.SaveRawIndex(ctx, pending); err != nil || !created {
		t.Fatalf("raw in cutover month: %v %v", created, err)
	}
	expired, err := r.ExpiredPartitions(ctx, "raw_archive_index", cutover.AddDate(0, 1, 0))
	if err != nil || len(expired) != 1 || expired[0].Name != spec.monthName(cutover) {
		t.Fatalf("expired partitions %+v %v", expired, err)
	}
	if dropped, err := r.DropPartition(ctx, "raw_archive_index", expired[0].Name); err != nil || dropped {
		t.Fatalf("partition with an unpublished message dropped: %v %v", dropped, err)
	}
	if err = r.MarkRawPublished(ctx, "t", "future", 0, cutover.UnixMilli(), ""); err != nil {
		t.Fatal(err)
	}
	if dropped, err := r.DropPartition(ctx, "raw_archive_index", expired[0].Name); err != nil || !dropped {
		t.Fatalf("drop expired partition: %v %v", dropped, err)
	}
	// Row purge still works on the remaining partitions.
	if n, err := r.PurgeRange(ctx, model.RetentionRawIndex, time.Time{}, time.Now(), 10); err != nil || n != 1 {
		t.Fatalf("purge legacy row: %d %v", n, err)
	}
}

// The pending-publish partial index covers every partition, including ones
// created after the migration, so the publish retry never scans whole tables.
func TestPendingPublishIndexCoversEveryPartition(t *testing.T) {
	ctx := context.Background()
	r := testRepository(t)
	if err := r.EnsurePartitions(ctx, time.Now().AddDate(0, 2, 0)); err != nil {
		t.Fatal(err)
	}
	var valid bool
	if err := r.pool.QueryRow(ctx, `SELECT indisvalid FROM pg_index WHERE indexrelid=to_regclass('raw_archive_index_pending_idx')`).Scan(&valid); err != nil || !valid {
		t.Fatalf("parent pending index missing or invalid: valid=%v err=%v", valid, err)
	}
	leaves, err := r.leafTables(ctx, "raw_archive_index")
	if err != nil || len(leaves) < 2 {
		t.Fatalf("expected partitions, got %v err=%v", leaves, err)
	}
	for _, leaf := range leaves {
		var covered bool
		if err = r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_index i WHERE i.indrelid=to_regclass($1) AND pg_get_expr(i.indpred, i.indrelid) LIKE '%published_at = 0%')`, leaf).Scan(&covered); err != nil || !covered {
			t.Fatalf("partition %s has no pending-publish index (err=%v)", leaf, err)
		}
	}
}
