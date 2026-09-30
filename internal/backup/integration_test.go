package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"iot-platform/internal/config"
)

// Uses real PostgreSQL and both object stores, but only writes fixtures in a
// unique source schema and the independently configured restore database.
func TestFullKnowledgeAndAgentRestoreIntegration(t *testing.T) {
	envFile := os.Getenv("IOT_BACKUP_FULL_RESTORE_TEST_ENV")
	if envFile != "" {
		if err := config.LoadEnvFile(envFile); err != nil {
			t.Fatal("cannot load private integration configuration")
		}
	}
	if os.Getenv("IOT_BACKUP_FULL_RESTORE_TEST") != "1" && envFile == "" {
		t.Skip("set IOT_BACKUP_FULL_RESTORE_TEST_ENV for the isolated full restore drill")
	}
	source := os.Getenv("IOT_TEST_POSTGRES_DSN")
	if source == "" {
		t.Fatal("IOT_TEST_POSTGRES_DSN must identify the independent fixture database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, source)
	if err != nil {
		t.Fatal("fixture database configuration invalid")
	}
	t.Cleanup(pool.Close)
	schema := "backup_fixture_" + time.Now().UTC().Format("20060102150405")
	if _, err = pool.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal("cannot create isolated fixture schema")
	}
	t.Cleanup(func() { pool.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE") })
	for _, table := range append(append([]string{}, knowledgeTables...), "raw_message_log", "standard_message", "backup_task") {
		if _, err = pool.Exec(ctx, "CREATE TABLE "+pgx.Identifier{schema, table}.Sanitize()+" (LIKE "+pgx.Identifier{"public", table}.Sanitize()+" INCLUDING ALL)"); err != nil {
			t.Fatalf("cannot create fixture table %s: %v", table, err)
		}
	}
	// Duty schema is isolated too; it does not require changing live source tables.
	for _, table := range dutyTables {
		ident := pgx.Identifier{schema, table}.Sanitize()
		definition := `tenant_id text NOT NULL,id text NOT NULL,version bigint NOT NULL,created_at bigint NOT NULL,updated_at bigint NOT NULL,body jsonb NOT NULL,PRIMARY KEY(tenant_id,id)`
		if table == "duty_business_event" {
			definition = `seq bigserial PRIMARY KEY,tenant_id text NOT NULL,id text NOT NULL,event_type text NOT NULL,station_id text NOT NULL,run_id text NOT NULL,device_id text NOT NULL,actor_id text NOT NULL,occurred_at bigint NOT NULL,recorded_at bigint NOT NULL,body jsonb NOT NULL,UNIQUE(tenant_id,id)`
		}
		if _, err = pool.Exec(ctx, "CREATE TABLE "+ident+"("+definition+")"); err != nil {
			t.Fatalf("cannot create duty fixture table %s: %v", table, err)
		}
	}
	connConfig, err := pgx.ParseConfig(source)
	if err != nil {
		t.Fatal("invalid source configuration")
	}
	connConfig.RuntimeParams["search_path"] = schema + ",public"
	fixture, err := pgx.ConnectConfig(ctx, connConfig)
	if err != nil {
		t.Fatal("fixture connection failed")
	}
	defer fixture.Close(context.Background())
	const original = "消防控制器离线时检查网络与供电。"
	objectBucket := "iot-backup-fixture-" + time.Now().UTC().Format("20060102150405")
	store, err := minio.New(os.Getenv("IOT_MINIO_ENDPOINT"), &minio.Options{Creds: credentials.NewStaticV4(os.Getenv("IOT_MINIO_ACCESS_KEY"), os.Getenv("IOT_MINIO_SECRET_KEY"), ""), Secure: os.Getenv("IOT_MINIO_USE_TLS") == "true"})
	if err != nil {
		t.Fatal("source object store configuration invalid")
	}
	if err = store.MakeBucket(ctx, objectBucket, minio.MakeBucketOptions{}); err != nil {
		t.Fatal("fixture original bucket creation failed")
	}
	t.Cleanup(func() {
		store.RemoveObject(context.Background(), objectBucket, "manual.txt", minio.RemoveObjectOptions{})
		store.RemoveBucket(context.Background(), objectBucket)
	})
	if _, err = store.PutObject(ctx, objectBucket, "manual.txt", strings.NewReader(original), int64(len(original)), minio.PutObjectOptions{ContentType: "text/plain"}); err != nil {
		t.Fatal("fixture original upload failed")
	}
	for _, query := range []string{
		`INSERT INTO ai_knowledge_doc(id,tenant_id,workflow_id,object_bucket,object_key,filename,status) VALUES('document','fixture-tenant','fire-operator',$1,'manual.txt','manual.txt','READY')`,
		`INSERT INTO ai_workflow_knowledge_binding(tenant_id,workflow_id,body) VALUES('fixture-tenant','fire-operator','{"workflowId":"fire-operator","documentIds":["document"]}')`,
		`INSERT INTO ai_knowledge_index_version(id,signature,provider,model,dimensions,preprocessing,status) VALUES('version','fixture-signature','fixture','fixture-embedding',3,'fixture-v1','active')`,
		`INSERT INTO ai_knowledge_chunk(version_id,tenant_id,workflow_id,document_id,chunk_id,chunk_index,start_char,end_char,character_count,content,dimensions,embedding) VALUES('version','fixture-tenant','fire-operator','document','chunk',0,0,2,2,'测试',3,'[1,0,0]')`,
	} {
		args := []any{}
		if strings.Contains(query, "$1") {
			args = append(args, objectBucket)
		}
		if _, err = fixture.Exec(ctx, query, args...); err != nil {
			t.Fatal("cannot populate knowledge fixture", err)
		}
	}
	// A signed handover, carried item, record attachment, pending job and receipt
	// exercise all duty artifacts without accessing live business rows.
	dutyKey := schema + "/handover-photo.txt"
	dutyOriginal := "现场检查照片的测试内容"
	exists, err := store.BucketExists(ctx, dutyAttachmentBucket)
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		if err = store.MakeBucket(ctx, dutyAttachmentBucket, minio.MakeBucketOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = store.PutObject(ctx, dutyAttachmentBucket, dutyKey, strings.NewReader(dutyOriginal), int64(len(dutyOriginal)), minio.PutObjectOptions{ContentType: "text/plain"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		store.RemoveObject(context.Background(), dutyAttachmentBucket, dutyKey, minio.RemoveObjectOptions{})
		if !exists {
			store.RemoveBucket(context.Background(), dutyAttachmentBucket)
		}
	})
	for _, table := range dutyTables {
		body := map[string]any{"stationId": "station", "runId": "run", "handoverId": "handover", "status": "ACCEPTED"}
		if table == "duty_attachment" {
			body["attachment"] = map[string]any{"id": "attachment", "objectKey": dutyKey, "name": "handover-photo.txt", "size": len(dutyOriginal)}
		}
		if table == "duty_record" || table == "duty_handover_revision" {
			body["attachments"] = []any{map[string]any{"id": "attachment", "objectKey": dutyKey}}
		}
		b, _ := json.Marshal(body)
		ident := pgx.Identifier{schema, table}.Sanitize()
		query := "INSERT INTO " + ident + "(tenant_id,id,version,created_at,updated_at,body) VALUES('fixture-tenant',$1,1,10,20,$2)"
		if table == "duty_business_event" {
			query = "INSERT INTO " + ident + "(tenant_id,id,event_type,station_id,run_id,device_id,actor_id,occurred_at,recorded_at,body) VALUES('fixture-tenant',$1,'HANDOVER_ACCEPTED','station','run','','operator',10,20,$2)"
		}
		if _, err = fixture.Exec(ctx, query, table, b); err != nil {
			t.Fatalf("populate duty fixture %s: %v", table, err)
		}
	}
	day := time.Now().AddDate(0, 0, -1)
	var rawBody = []byte(`{"messageId":"backup-fixture-raw"}`)
	if _, err = fixture.Exec(ctx, `INSERT INTO raw_message_log(tenant_id,message_id,product_id,device_id,payload_hash,payload_size,received_at,stored_at,body) VALUES('fixture-tenant','raw','product','device','hash',1,$2,$2,$1)`, rawBody, day.UnixMilli()); err != nil {
		t.Fatal("cannot populate raw fixture", err)
	}
	if _, err = fixture.Exec(ctx, `INSERT INTO standard_message(tenant_id,message_id,raw_message_id,product_id,device_id,message_type,ts,body) VALUES('fixture-tenant','parsed','raw','product','device','PROPERTY_REPORT',$1,'{"messageId":"backup-fixture-parsed"}')`, day.UnixMilli()); err != nil {
		t.Fatal("cannot populate parsed fixture", err)
	}
	// Real PostgreSQL and fixture HTTP exports exercise both message storage
	// paths without reading live ClickHouse device data.
	ch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Query().Get("query"), "iot_raw_message") {
			json.NewEncoder(w).Encode(map[string]string{"body": `{"messageId":"backup-fixture-ch-raw"}`})
		} else {
			json.NewEncoder(w).Encode(map[string]string{"message_id": "backup-fixture-ch-parsed"})
		}
	}))
	defer ch.Close()
	snapshot := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-IOT-Harness-Token") != "fixture-token" {
			w.WriteHeader(401)
			return
		}
		json.NewEncoder(w).Encode(snapshotFixture(map[string][]byte{"plugins/fire-operator.json": []byte(`{"id":"fire-operator","name":"巡检助手"}`), "sessions/project/session/session.jsonl": []byte("{\"message\":\"测试\"}\n")}))
	}))
	defer snapshot.Close()
	backupPoolConfig, err := pgxpool.ParseConfig(source)
	if err != nil {
		t.Fatal("backup fixture configuration invalid")
	}
	backupPoolConfig.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	backupPool, err := pgxpool.NewWithConfig(ctx, backupPoolConfig)
	if err != nil {
		t.Fatal("backup fixture connection failed")
	}
	defer backupPool.Close()
	cfg := Config{PostgresDSN: source, BackupDir: t.TempDir(), BackupBucket: objectBucket, MinIOEndpoint: os.Getenv("IOT_MINIO_ENDPOINT"), MinIOAccessKey: os.Getenv("IOT_MINIO_ACCESS_KEY"), MinIOSecretKey: os.Getenv("IOT_MINIO_SECRET_KEY"), MinIOUseTLS: os.Getenv("IOT_MINIO_USE_TLS") == "true", ClickHouseURL: ch.URL, BackupTimezone: "Asia/Shanghai", HarnessSnapshotURLs: []string{snapshot.URL + "/v1/backup/snapshot"}, HarnessToken: "fixture-token", RestoreTargetDSN: os.Getenv("IOT_BACKUP_RESTORE_TARGET_DSN"), RestoreHarnessDir: t.TempDir(), RestoreMinIOEndpoint: os.Getenv("IOT_BACKUP_RESTORE_MINIO_ENDPOINT"), RestoreMinIOAccessKey: os.Getenv("IOT_BACKUP_RESTORE_MINIO_ACCESS_KEY"), RestoreMinIOSecretKey: os.Getenv("IOT_BACKUP_RESTORE_MINIO_SECRET_KEY"), RestoreMinIOUseTLS: os.Getenv("IOT_BACKUP_RESTORE_MINIO_USE_TLS") == "true"}
	s := &Service{cfg: cfg, pool: backupPool, store: store}
	manifest, err := s.Run(ctx, "FULL")
	if err != nil {
		t.Fatal("FULL fixture backup failed", err)
	}
	t.Cleanup(func() {
		for _, a := range manifest.Artifacts {
			store.RemoveObject(context.Background(), objectBucket, a.ObjectKey, minio.RemoveObjectOptions{})
		}
	})
	if manifest.FormatVersion != 3 || len(manifest.Artifacts) != 10 {
		t.Fatal("FULL composition incomplete")
	}
	if _, err = s.Verify(ctx, manifest.ID); err != nil {
		t.Fatal("fixture artifact verification failed", err)
	}
	daily, err := s.RunDaily(ctx, day)
	if err != nil {
		t.Fatal("daily fixture backup failed", err)
	}
	t.Cleanup(func() {
		for _, a := range daily.Artifacts {
			store.RemoveObject(context.Background(), objectBucket, a.ObjectKey, minio.RemoveObjectOptions{})
		}
	})
	if daily.Type != "DEVICE_DAILY" || len(daily.Artifacts) != 3 || daily.Components["knowledge"] != nil {
		t.Fatal("daily scope must contain raw and parsed messages only")
	}
	if _, err = s.Verify(ctx, daily.ID); err != nil {
		t.Fatal("daily artifact verification failed", err)
	}
	dailyRestore, err := s.Restore(ctx, daily.ID)
	if err != nil || dailyRestore.Status != "COMPLETED" {
		t.Fatal("daily message restore failed", err)
	}
	for _, kind := range []string{"rawMessages", "parsedMessages"} {
		if summary := dailyRestore.Kinds[kind]; summary.Restored != 2 || !summary.Matches {
			t.Fatal("daily PostgreSQL/ClickHouse fixture restore incomplete", kind)
		}
	}
	result, err := s.Restore(ctx, manifest.ID)
	if err != nil {
		t.Fatal("isolated full restore failed", err)
	}
	if result.Status != "COMPLETED" {
		t.Fatal("full restore not completed")
	}
	target, err := pgx.Connect(ctx, cfg.RestoreTargetDSN)
	if err != nil {
		t.Fatal("cannot verify restore target")
	}
	defer target.Close(context.Background())
	restored := result.Components["knowledge"].(map[string]any)["schema"].(string)
	for _, table := range knowledgeTables {
		var count int
		if err = target.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{restored, table}.Sanitize()).Scan(&count); err != nil || count != 1 {
			t.Fatalf("restored table %s count=%d err=%v", table, count, err)
		}
	}
	var content string
	if err = target.QueryRow(ctx, "SELECT content FROM "+pgx.Identifier{restored, "ai_knowledge_chunk"}.Sanitize()+" WHERE version_id='version' ORDER BY embedding::public.vector(3) OPERATOR(public.<=>) '[1,0,0]'::public.vector(3) LIMIT 1").Scan(&content); err != nil || content != "测试" {
		t.Fatal("restored vector query failed", err)
	}
	var bucket, key string
	if err = target.QueryRow(ctx, "SELECT object_bucket,object_key FROM "+pgx.Identifier{restored, "ai_knowledge_doc"}.Sanitize()+" WHERE id='document'").Scan(&bucket, &key); err != nil {
		t.Fatal("restored document reference unavailable")
	}
	dr, err := minio.New(cfg.RestoreMinIOEndpoint, &minio.Options{Creds: credentials.NewStaticV4(cfg.RestoreMinIOAccessKey, cfg.RestoreMinIOSecretKey, ""), Secure: cfg.RestoreMinIOUseTLS})
	if err != nil {
		t.Fatal("DR connection invalid")
	}
	object, err := dr.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		t.Fatal("restored original unavailable")
	}
	body, err := io.ReadAll(object)
	object.Close()
	if err != nil || !bytes.Equal(body, []byte(original)) {
		t.Fatal("restored original content mismatch")
	}
	if _, err = os.Stat(filepath.Join(cfg.RestoreHarnessDir, result.RestoreID, "instances/000/plugins/fire-operator.json")); err != nil {
		t.Fatal("dynamic Agent manifest missing")
	}
	dutyRestored := result.Components["duty"].(map[string]any)["schema"].(string)
	for _, table := range dutyTables {
		var count int
		if err = target.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{dutyRestored, table}.Sanitize()).Scan(&count); err != nil || count != 1 {
			t.Fatalf("restored duty %s count=%d err=%v", table, count, err)
		}
	}
	var attachmentKey string
	if err = target.QueryRow(ctx, "SELECT body->'attachment'->>'objectKey' FROM "+pgx.Identifier{dutyRestored, "duty_attachment"}.Sanitize()).Scan(&attachmentKey); err != nil {
		t.Fatal(err)
	}
	object, err = dr.GetObject(ctx, dutyAttachmentBucket, attachmentKey, minio.GetObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	body, err = io.ReadAll(object)
	object.Close()
	if err != nil || string(body) != dutyOriginal {
		t.Fatal("restored duty attachment mismatch", err)
	}
	var embeddedKey string
	if err = target.QueryRow(ctx, "SELECT body->'attachments'->0->>'objectKey' FROM "+pgx.Identifier{dutyRestored, "duty_handover_revision"}.Sanitize()).Scan(&embeddedKey); err != nil || embeddedKey != attachmentKey {
		t.Fatal("snapshot attachment reference mismatch", err)
	}
	t.Cleanup(func() {
		dr.RemoveObject(context.Background(), dutyAttachmentBucket, attachmentKey, minio.RemoveObjectOptions{})
	})
	// An older FULL v2 can still restore without requiring duty artifacts.
	old := manifest
	old.ID = manifest.ID
	old.FormatVersion = 2
	oldResult := RestoreResult{BackupID: manifest.ID, RestoreID: result.RestoreID + "-legacy", Components: map[string]any{}}
	if err = s.restoreDuty(ctx, target, old, &oldResult); err != nil || oldResult.Components["duty"].(map[string]any)["status"] != "not_included" {
		t.Fatal("old full backup rejected", err)
	}
	var sourceCount int
	if err = fixture.QueryRow(ctx, "SELECT count(*) FROM ai_knowledge_doc").Scan(&sourceCount); err != nil || sourceCount != 1 {
		t.Fatal("source knowledge changed during restore")
	}
	t.Logf("FULL v3 and daily verified and restored: PostgreSQL/ClickHouse messages, 4 knowledge tables, duty tables, original and duty attachment, 2 Agent/session files; restoreId=%s schema=%s", result.RestoreID, restored)
}

func TestLiveHarnessSnapshotRestoreIntegration(t *testing.T) {
	if os.Getenv("IOT_BACKUP_LIVE_HARNESS_TEST") != "1" {
		t.Skip("set IOT_BACKUP_LIVE_HARNESS_TEST=1 after deploying the snapshot endpoint")
	}
	s := &Service{cfg: Config{HarnessSnapshotURLs: strings.Split(os.Getenv("IOT_BACKUP_HARNESS_SNAPSHOT_URLS"), ","), HarnessToken: os.Getenv("IOT_AI_HARNESS_TOKEN")}}
	archive := filepath.Join(t.TempDir(), "agents.tar.gz")
	files, size, instances, err := s.archivePersistentAgents(context.Background(), archive)
	if err != nil {
		t.Fatal("live Harness snapshot failed", err)
	}
	f, b, err := restoreHarnessArchive(context.Background(), archive, filepath.Join(t.TempDir(), "isolated-agents"))
	if err != nil || f != files || b != size {
		t.Fatal("live snapshot isolated restore mismatch", err)
	}
	t.Logf("Live snapshot restored in isolated directory: instances=%d files=%d bytes=%d", instances, files, size)
}
