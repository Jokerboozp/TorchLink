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
	"iot-platform/internal/adapters/postgres"
	"iot-platform/internal/config"
	"iot-platform/internal/model"
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
	if _, err = fixture.Exec(ctx, postgres.AlarmGovernanceRestoreSchema()); err != nil {
		t.Fatal("cannot create governance fixture schema", err)
	}
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
		if table == "duty_ai_job" {
			body["status"] = "RUNNING"
			body["leaseOwner"] = "original-worker"
			body["leaseUntil"] = 9999999999999
		}
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
	prepareApplicationFixture(t, ctx, fixture, store, schema)
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
	governanceKey := schema + "/governance-photo.txt"
	governanceOriginal := "现场核实与治理附件测试内容"
	exists, err = store.BucketExists(ctx, governanceAttachmentBucket)
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		if err = store.MakeBucket(ctx, governanceAttachmentBucket, minio.MakeBucketOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = store.PutObject(ctx, governanceAttachmentBucket, governanceKey, strings.NewReader(governanceOriginal), int64(len(governanceOriginal)), minio.PutObjectOptions{ContentType: "text/plain"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		store.RemoveObject(context.Background(), governanceAttachmentBucket, governanceKey, minio.RemoveObjectOptions{})
	})
	for _, query := range []string{
		`INSERT INTO alarm_observation(tenant_id,id,slot_key,device_id,alarm_type,origin_kind,signal_key,event_at,recorded_at,acceptance,fact_kind,content_hash,body) VALUES('fixture-tenant','fixture-obs','fixture-slot','fixture-device','FIRE','DEVICE_DIRECT','device:FIRE',100,200,'ACCEPTED','ASSERT','fixture-hash','{}')`,
		`INSERT INTO alarm_signal_state(tenant_id,device_id,signal_key,event_at,active,observation_id) VALUES('fixture-tenant','fixture-device','device:FIRE',100,true,'fixture-obs')`,
		`INSERT INTO alarm_governance_case(tenant_id,id,version,created_by,created_at,updated_at,device_ids,point_key,status,body) VALUES('fixture-tenant','governance-case',1,'operator',1,1,'{fixture-device}','fixture-point','INVESTIGATING','{}')`,
		`INSERT INTO alarm_governance_round(tenant_id,id,version,created_by,created_at,updated_at,case_id,device_ids,status,body) VALUES('fixture-tenant','governance-round',1,'operator',1,1,'governance-case','{fixture-device}','ACTIVE','{}')`,
		`INSERT INTO alarm_governance_verification(tenant_id,id,version,created_by,created_at,updated_at,case_id,round_id,device_ids,body) VALUES('fixture-tenant','governance-verification',1,'operator',1,1,'governance-case','governance-round','{fixture-device}','{"observationIds":["fixture-obs"]}')`,
		`INSERT INTO alarm_governance_verification_link(tenant_id,id,version,created_by,created_at,updated_at,case_id,round_id,device_ids,body) VALUES('fixture-tenant','governance-verification-link',1,'operator',1,1,'governance-case','governance-round','{fixture-device}','{"verificationId":"governance-verification"}')`,
		`INSERT INTO alarm_governance_source_version(tenant_id,dependency_key,bucket_start,generation) VALUES('fixture-tenant','fixture-device-dependency',0,3)`,
		`INSERT INTO alarm_governance_case_history(tenant_id,resource_id,version,recorded_at,body) VALUES('fixture-tenant','governance-case',1,2,'{"id":"governance-case","body":{"status":"INVESTIGATING"}}')`,
		`INSERT INTO alarm_governance_type_profile(tenant_id,id,version,created_by,created_at,updated_at,body) VALUES('fixture-tenant','governance-profile',1,'operator',1,1,'{}')`,
	} {
		if _, err = fixture.Exec(ctx, query); err != nil {
			t.Fatal("populate governance fixture", err)
		}
	}
	attachment, _ := json.Marshal(map[string]any{"storageKey": governanceKey, "availability": "AVAILABLE", "name": "field-note.txt", "size": len(governanceOriginal)})
	if _, err = fixture.Exec(ctx, `INSERT INTO alarm_governance_attachment(tenant_id,id,version,created_by,created_at,updated_at,case_id,device_ids,body) VALUES('fixture-tenant','governance-attachment',1,'operator',1,1,'governance-case','{fixture-device}',$1)`, attachment); err != nil {
		t.Fatal(err)
	}
	historyAttachment, _ := json.Marshal(map[string]any{"id": "governance-attachment", "body": map[string]any{"storageKey": governanceKey, "availability": "AVAILABLE", "size": len(governanceOriginal)}})
	if _, err = fixture.Exec(ctx, `INSERT INTO alarm_governance_attachment_history(tenant_id,resource_id,version,recorded_at,body) VALUES('fixture-tenant','governance-attachment',1,2,$1)`, historyAttachment); err != nil {
		t.Fatal(err)
	}
	for _, attempt := range []struct{ id, key string }{{"referenced-upload", governanceKey}, {"orphan-upload", schema + "/never-committed-object"}} {
		upload, _ := json.Marshal(map[string]any{"attachmentId": "governance-attachment", "storageKey": attempt.key, "startedAt": 1})
		if _, err = fixture.Exec(ctx, `INSERT INTO alarm_governance_upload_attempt(tenant_id,id,version,created_by,created_at,updated_at,device_ids,status,body) VALUES('fixture-tenant',$1,1,'SYSTEM',1,1,'{fixture-device}','UPLOAD_PENDING',$2)`, attempt.id, upload); err != nil {
			t.Fatal(err)
		}
		history, _ := json.Marshal(map[string]any{"id": attempt.id, "status": "UPLOAD_PENDING", "body": json.RawMessage(upload)})
		if _, err = fixture.Exec(ctx, `INSERT INTO alarm_governance_upload_attempt_history(tenant_id,resource_id,version,recorded_at,body) VALUES('fixture-tenant',$1,1,1,$2)`, attempt.id, history); err != nil {
			t.Fatal(err)
		}
	}
	for _, document := range governanceApplicationDocuments() {
		raw, _ := json.Marshal(document)
		if _, err = fixture.Exec(ctx, `INSERT INTO analysis_document SELECT * FROM jsonb_populate_record(NULL::analysis_document,$1::jsonb)`, raw); err != nil {
			t.Fatal(err)
		}
		if document.Kind == "snapshot" {
			var fixed model.AnalysisSnapshot
			if err = json.Unmarshal(document.Body, &fixed); err != nil {
				t.Fatal(err)
			}
			body, _ := json.Marshal(map[string]any{"analysisSnapshotId": fixed.ID, "factsHash": fixed.FactsHash})
			if _, err = fixture.Exec(ctx, `INSERT INTO alarm_governance_observation_review(tenant_id,id,version,created_by,created_at,updated_at,case_id,round_id,device_ids,status,body) VALUES('fixture-tenant','governance-review',1,'operator',1,1,'governance-case','governance-round','{fixture-device}','CONFIRMED',$1)`, body); err != nil {
				t.Fatal(err)
			}
			body, _ = json.Marshal(map[string]any{"analysisSnapshotId": fixed.ID, "factsHash": fixed.FactsHash, "reviewId": "governance-review"})
			if _, err = fixture.Exec(ctx, `INSERT INTO alarm_governance_report(tenant_id,id,version,created_by,created_at,updated_at,case_id,round_id,device_ids,body) VALUES('fixture-tenant','governance-report',1,'operator',1,1,'governance-case','governance-round','{fixture-device}',$1)`, body); err != nil {
				t.Fatal(err)
			}
		}
	}
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
	if manifest.FormatVersion != 5 || len(manifest.Artifacts) != 16 {
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
	if result.Status != "PARTIAL" {
		t.Fatal("full restore must preserve cold-source partial coverage")
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
	verifyApplicationRestore(t, ctx, s, target, result)
	verifyCorruptApplicationRollback(t, ctx, s, target, manifest, result.RestoreID)
	governanceRestored := result.Components["governance"].(map[string]any)["schema"].(string)
	for _, table := range postgres.AlarmGovernanceDomainRestoreTables() {
		var count int64
		if err = target.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{governanceRestored, table}.Sanitize()).Scan(&count); err != nil || count != snapshotTableCount(manifest, "governance", table) {
			t.Fatal("governance restored table count mismatch", table, count, err)
		}
	}
	var governanceRestoredKey string
	if err = target.QueryRow(ctx, "SELECT body->>'storageKey' FROM "+pgx.Identifier{governanceRestored, "alarm_governance_attachment"}.Sanitize()+" WHERE id='governance-attachment'").Scan(&governanceRestoredKey); err != nil {
		t.Fatal(err)
	}
	for _, attempt := range []struct{ id, key string }{{"referenced-upload", governanceRestoredKey}, {"orphan-upload", ""}} {
		var status, key, historyKey string
		table := pgx.Identifier{governanceRestored, "alarm_governance_upload_attempt"}.Sanitize()
		if err = target.QueryRow(ctx, "SELECT status,body->>'storageKey' FROM "+table+" WHERE id=$1", attempt.id).Scan(&status, &key); err != nil || status != "UPLOAD_RESTORED" || key != attempt.key {
			t.Fatal("restored compensation still owns source object", status, key, err)
		}
		if err = target.QueryRow(ctx, "SELECT body->'body'->>'storageKey' FROM "+pgx.Identifier{governanceRestored, "alarm_governance_upload_attempt_history"}.Sanitize()+" WHERE resource_id=$1", attempt.id).Scan(&historyKey); err != nil || historyKey != attempt.key {
			t.Fatal("restored upload history retains source namespace", err)
		}
	}
	object, err = dr.GetObject(ctx, governanceAttachmentBucket, governanceRestoredKey, minio.GetObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	body, err = io.ReadAll(object)
	object.Close()
	if err != nil || string(body) != governanceOriginal {
		t.Fatal("restored governance attachment mismatch", err)
	}
	t.Cleanup(func() {
		dr.RemoveObject(context.Background(), governanceAttachmentBucket, governanceRestoredKey, minio.RemoveObjectOptions{})
	})
	var generatedVerification string
	if err = target.QueryRow(ctx, "SELECT verification_id FROM "+pgx.Identifier{governanceRestored, "alarm_governance_verification_link"}.Sanitize()).Scan(&generatedVerification); err != nil || generatedVerification != "governance-verification" {
		t.Fatal("generated governance association not restored", err)
	}
	applicationRestored := result.Components["application"].(map[string]any)["schema"].(string)
	var duplicateAnalysis *string
	if err = target.QueryRow(ctx, "SELECT to_regclass($1)::text", governanceRestored+".analysis_document").Scan(&duplicateAnalysis); err != nil || duplicateAnalysis != nil {
		t.Fatal("combined restore duplicated shared analysis table", err)
	}
	var retiredGovernanceAI string
	if err = target.QueryRow(ctx, "SELECT status FROM "+pgx.Identifier{applicationRestored, "analysis_document"}.Sanitize()+" WHERE id='governance-ai'").Scan(&retiredGovernanceAI); err != nil || retiredGovernanceAI != model.AnalysisCancelled {
		t.Fatal("governance model task retained a live lease", err)
	}
	verifyGovernanceCrossReferences(t, ctx, target, result)
	verifyLegacyFullComponents(t, ctx, s, fixture, target, manifest, result.RestoreID)
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
	var retiredDuty string
	if err = target.QueryRow(ctx, "SELECT body->>'status' FROM "+pgx.Identifier{dutyRestored, "duty_ai_job"}.Sanitize()).Scan(&retiredDuty); err != nil || retiredDuty != "CANCELLED" {
		t.Fatal("restored duty model job could resume", err)
	}
	t.Cleanup(func() {
		cleanup, err := pgx.Connect(context.Background(), cfg.RestoreTargetDSN)
		if err == nil {
			defer cleanup.Close(context.Background())
			for _, name := range []string{restored, dutyRestored, governanceRestored} {
				cleanup.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{name}.Sanitize()+" CASCADE")
			}
			for _, id := range []string{dailyRestore.RestoreID, result.RestoreID} {
				cleanup.Exec(context.Background(), "DELETE FROM restored_message WHERE restore_id=$1", id)
				cleanup.Exec(context.Background(), "DELETE FROM restore_run WHERE id=$1", id)
			}
		}
	})
	t.Logf("FULL v5 and daily verified: retained PG and CH artifacts (PARTIAL cold source), knowledge/duty/application/governance graphs, private attachments, fixed finance/quality permissions, retired tasks, upload compensation isolation, numeric legacy hashes, 2 Agent/session files; restoreId=%s schema=%s", result.RestoreID, restored)
}

func verifyGovernanceCrossReferences(t *testing.T, ctx context.Context, target *pgx.Conn, result RestoreResult) {
	t.Helper()
	gov := result.Components["governance"].(map[string]any)["schema"].(string)
	for _, mutation := range []string{
		"DELETE FROM " + pgx.Identifier{gov, "alarm_governance_type_profile"}.Sanitize() + " WHERE id='governance-profile'",
		"UPDATE " + pgx.Identifier{gov, "alarm_governance_case"}.Sanitize() + " SET device_ids=ARRAY['uncovered-device'] WHERE id='governance-case'",
		"UPDATE " + pgx.Identifier{gov, "alarm_governance_report"}.Sanitize() + " SET body=jsonb_set(body,'{factsHash}','\"changed\"') WHERE id='governance-report'",
	} {
		tx, err := target.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, mutation); err != nil {
			tx.Rollback(ctx)
			t.Fatal(err)
		}
		if err = validateRestoredGovernanceAnalysis(ctx, target, &result); err == nil {
			tx.Rollback(ctx)
			t.Fatal("invalid restored governance reference accepted")
		}
		if err = tx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := validateRestoredGovernanceAnalysis(ctx, target, &result); err != nil {
		t.Fatal("unchanged governance reference rejected", err)
	}
}

// Generate each historical v4 composition in an isolated source transaction,
// then restore the actual schema/data/object artifacts into the DR database.
func verifyLegacyFullComponents(t *testing.T, ctx context.Context, s *Service, fixture, target *pgx.Conn, combined Manifest, restoreID string) {
	t.Helper()
	for _, component := range []string{"application", "governance"} {
		t.Run("legacy-v4-"+component, func(t *testing.T) {
			dir := t.TempDir()
			m := Manifest{ID: combined.ID + "-legacy-" + component, Type: "FULL", FormatVersion: 4, Components: map[string]any{}}
			m.Components[component+"Objects"] = combined.Components[component+"Objects"]
			tx, err := fixture.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			predicate := "run_id='governance-run'"
			if component == "governance" {
				predicate = "run_id<>'governance-run'"
			}
			if _, err = tx.Exec(ctx, "DELETE FROM analysis_document WHERE "+predicate); err != nil {
				t.Fatal(err)
			}
			data := filepath.Join(dir, component+"-postgres.jsonl.gz")
			var schema knowledgeSchema
			var counts map[string]int64
			if component == "application" {
				schema, counts, err = exportDataTables(ctx, tx, applicationTables(), data)
			} else {
				schema, counts, err = exportTableSnapshot(ctx, tx, postgres.AlarmGovernanceRestoreTables(), data)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = tx.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			schemaPath := filepath.Join(dir, component+"-schema.json")
			if err = writeJSON(schemaPath, schema); err != nil {
				t.Fatal(err)
			}
			objects, err := s.downloadVerifiedArtifact(ctx, combined, combined.ID, component+"-objects.tar.gz", dir)
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{schemaPath, data, objects} {
				artifact, err := s.uploadAndVerify(ctx, m.ID, path)
				if err != nil {
					t.Fatal(err)
				}
				m.Artifacts = append(m.Artifacts, artifact)
				t.Cleanup(func() {
					s.store.RemoveObject(context.Background(), s.cfg.BackupBucket, artifact.ObjectKey, minio.RemoveObjectOptions{})
				})
			}
			m.Components[component] = map[string]any{"status": "included", "tables": counts}
			res := RestoreResult{BackupID: m.ID, RestoreID: restoreID + "-legacy-" + component, Components: map[string]any{}}
			if err = s.restoreApplication(ctx, target, m, &res); err != nil {
				t.Fatal("legacy application component", err)
			}
			if err = s.restoreGovernance(ctx, target, m, &res); err != nil {
				t.Fatal("legacy governance component", err)
			}
			if err = validateRestoredGovernanceAnalysis(ctx, target, &res); err != nil {
				t.Fatal("legacy governance analysis references", err)
			}
			other := "governance"
			if component == "governance" {
				other = "application"
			}
			if res.Components[other].(map[string]any)["status"] != "not_included" {
				t.Fatal("legacy missing component invented")
			}
			summary := res.Components[component].(map[string]any)
			if summary["status"] != "restored" {
				t.Fatal(summary)
			}
			restored := summary["schema"].(string)
			t.Cleanup(func() {
				target.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{restored}.Sanitize()+" CASCADE")
			})
			if component == "governance" {
				if summary["analysisDocuments"] != "governance" || summary["retiredExecutions"] != 1 {
					t.Fatal("legacy governance tasks not retired", summary)
				}
				var status, key string
				if err = target.QueryRow(ctx, "SELECT status FROM "+pgx.Identifier{restored, "analysis_document"}.Sanitize()+" WHERE id='governance-ai'").Scan(&status); err != nil || status != model.AnalysisCancelled {
					t.Fatal("legacy model lease remains live", err)
				}
				if err = target.QueryRow(ctx, "SELECT body->>'storageKey' FROM "+pgx.Identifier{restored, "alarm_governance_attachment"}.Sanitize()+" WHERE id='governance-attachment'").Scan(&key); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { s.cleanupGovernanceRestoreObjects(context.Background(), []string{key}) })
			} else {
				verifyApplicationRestore(t, ctx, s, target, res)
			}
		})
	}
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
