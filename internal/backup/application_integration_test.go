package backup

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	minioadapter "iot-platform/internal/adapters/minio"
	"iot-platform/internal/adapters/postgres"
	"iot-platform/internal/analytics"
	"iot-platform/internal/analytics/dataquality"
	"iot-platform/internal/analytics/response"
	"iot-platform/internal/config"
	"iot-platform/internal/model"
	"iot-platform/internal/rulelab/eval"
	"iot-platform/internal/rulelab/history"
)

func prepareApplicationFixture(t *testing.T, ctx context.Context, fixture *pgx.Conn, store *minio.Client, schema string) {
	t.Helper()
	for _, name := range applicationTables() {
		var exists bool
		if err := fixture.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, schema+"."+name).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists {
			continue
		}
		spec := applicationSpecs[name]
		columns := []string{}
		for _, column := range spec.Columns {
			def := pgx.Identifier{column.Name}.Sanitize() + " " + knowledgeSQLTypes[column.Type]
			if column.NotNull {
				def += " NOT NULL"
			}
			columns = append(columns, def)
		}
		pk := []string{}
		for _, name := range spec.PrimaryKey {
			pk = append(pk, pgx.Identifier{name}.Sanitize())
		}
		columns = append(columns, "PRIMARY KEY("+strings.Join(pk, ",")+")")
		if _, err := fixture.Exec(ctx, "CREATE TABLE "+pgx.Identifier{name}.Sanitize()+"("+strings.Join(columns, ",")+")"); err != nil {
			t.Fatal(err)
		}
	}
	docs := append(applicationTestDocuments(), applicationMaintenanceDocuments()...)
	docs = append(docs, applicationTestConfig("numeric-revision", "DATA_QUALITY_PROFILE", "numeric-profile", json.RawMessage(`{"typed":1e-7,"raw":{"value":0.0000001},"integer":9007199254740993}`))...)
	for i, d := range docs {
		if d.Kind != "snapshot" || d.ID != "fixed-snapshot" {
			continue
		}
		snapshot, _ := decodeApplication[model.AnalysisSnapshot](d)
		snapshot.Statistics = json.RawMessage(`{"typed":1e-7,"fixedSource":{"value":0.0000001},"integer":9007199254740993}`)
		snapshot.FactsHash, snapshot.CreatedAt = "", 0
		facts := []json.RawMessage{}
		for _, fact := range docs {
			if fact.RunID == d.RunID && (fact.Kind == "output" || fact.Kind == "evidence") {
				facts = append(facts, fact.Body)
			}
		}
		snapshot.FactsHash, _ = analytics.AnalysisHash(struct {
			Snapshot model.AnalysisSnapshot
			Facts    []json.RawMessage
		}{snapshot, facts})
		snapshot.CreatedAt = 20
		docs[i].Body, _ = json.Marshal(snapshot)
	}
	for _, kind := range []string{"RESPONSE_ATTACHMENT", "MAINTENANCE_ATTACHMENT", model.DataQualityAttachmentKind} {
		bucket := applicationAttachmentBuckets[kind]
		exists, err := store.BucketExists(ctx, bucket)
		if err != nil {
			t.Fatal(err)
		}
		if !exists {
			if err = store.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
				t.Fatal(err)
			}
		}
		id := "attachment-" + kind
		key := "fixture-tenant/" + schema + "/" + kind
		if kind == model.DataQualityAttachmentKind {
			id += "-" + schema
			key = "fixture-tenant/" + id
		}
		data := []byte("固定私有附件：" + kind)
		digest := sha256.Sum256(data)
		sum := hex.EncodeToString(digest[:])
		if _, err = store.PutObject(ctx, bucket, key, strings.NewReader(string(data)), int64(len(data)), minio.PutObjectOptions{ContentType: "text/plain"}); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			store.RemoveObject(context.Background(), bucket, key, minio.RemoveObjectOptions{})
			if !exists {
				store.RemoveBucket(context.Background(), bucket)
			}
		})
		body := map[string]any{"objectKey": key, "sha256": sum, "size": len(data), "contentType": "text/plain", "name": "private.txt", "executionId": "drill"}
		if kind == model.DataQualityAttachmentKind {
			body = map[string]any{"id": id, "storageKey": key, "size": len(data), "contentType": "text/plain", "name": "legacy.txt"}
		}
		docs = append(docs, applicationTestConfig(id, kind, "file-"+kind, body)...)
	}
	for _, doc := range docs {
		raw, _ := json.Marshal(doc)
		if _, err := fixture.Exec(ctx, "INSERT INTO analysis_document SELECT * FROM jsonb_populate_record(NULL::analysis_document,$1::jsonb)", raw); err != nil {
			t.Fatal("fixture application document", err)
		}
	}
	access := model.AccessState{Revision: 1, Users: []model.PlatformUser{{Username: "operator", Enabled: true, DeviceScope: "CUSTOM", DeviceIDs: []string{"device"}, SessionVersion: 1, Permissions: []string{"menu:devices", "menu:response", "menu:dataQuality", "menu:maintenance", "GET /api/v1/response-attachments/:id", "GET /api/v1/data-quality/calibrations/attachments/:id"}}}}
	body, _ := json.Marshal(access)
	if _, err := fixture.Exec(ctx, "INSERT INTO platform_access(tenant_id,revision,body) VALUES('fixture-tenant',1,$1)", body); err != nil {
		t.Fatal(err)
	}
	rule := model.AlarmRule{ID: "rule", TenantID: "fixture-tenant", Version: 1, Enabled: true}
	body, _ = json.Marshal(rule)
	if _, err := fixture.Exec(ctx, "INSERT INTO alarm_rule(tenant_id,id,product_id,enabled,body,updated_at) VALUES($1,$2,NULL,$3,$4,now())", rule.TenantID, rule.ID, rule.Enabled, body); err != nil {
		t.Fatal(err)
	}
	revision, activation := history.Register(rule, 1234)
	revision.SemanticsVersion = eval.RevisionV2
	body, _ = json.Marshal(revision)
	if _, err := fixture.Exec(ctx, "INSERT INTO alarm_rule_revision(tenant_id,id,rule_id,version,hash,registered_at,body) VALUES($1,$2,$3,$4,$5,$6,$7)", revision.TenantID, revision.ID, revision.RuleID, revision.Version, revision.Hash, revision.RegisteredAt, body); err != nil {
		t.Fatal(err)
	}
	body, _ = json.Marshal(activation)
	if _, err := fixture.Exec(ctx, "INSERT INTO alarm_rule_activation(tenant_id,rule_id,revision_id,version,since_at,deleted,body) VALUES($1,$2,$3,$4,$5,$6,$7)", revision.TenantID, revision.RuleID, revision.ID, revision.Version, activation.Since, false, body); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.Exec(ctx, "INSERT INTO alarm_rule_current_revision(tenant_id,rule_id,revision_id,version,deleted) VALUES($1,$2,$3,$4,false)", revision.TenantID, revision.RuleID, revision.ID, revision.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.Exec(ctx, "INSERT INTO alarm_rule_revision_pending(tenant_id,rule_id,revision_id,device_id,since_at) VALUES($1,$2,$3,'device',100)", revision.TenantID, revision.RuleID, revision.ID); err != nil {
		t.Fatal(err)
	}
	trace := model.RuleEvaluationTrace{RuleTraceBinding: model.RuleTraceBinding{TenantID: "fixture-tenant", MessageID: "parsed", ClaimToken: 7}, DeviceID: "device", MessageTimestamp: 1234, StartedAt: 1234, Rules: []model.AlarmRuleRevision{revision}, Status: "IN_PROGRESS", ReproductionQuality: "INCOMPLETE_TRACE", SemanticsVersion: eval.RevisionV2}
	trace.ID = model.RuleTraceID(trace.RuleTraceBinding)
	trace.RuleSetHash = model.RuleSetHash(trace.Rules)
	body, _ = json.Marshal(trace)
	if _, err := fixture.Exec(ctx, "INSERT INTO rule_evaluation_trace(tenant_id,id,message_id,claim_token,device_id,message_timestamp,started_at,status,body) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)", trace.TenantID, trace.ID, trace.MessageID, trace.ClaimToken, trace.DeviceID, trace.MessageTimestamp, trace.StartedAt, trace.Status, body); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.Exec(ctx, `INSERT INTO duty_action_link(tenant_id,id,version,created_at,updated_at,body) VALUES('fixture-tenant','linked-action',1,10,20,'{"actionId":"action","sourceId":"finding"}')`); err != nil {
		t.Fatal(err)
	}
}

func verifyApplicationRestore(t *testing.T, ctx context.Context, s *Service, target *pgx.Conn, result RestoreResult) {
	t.Helper()
	if result.Components["applicationObjects"].(map[string]any)["originalHashUnknown"].(int64) != 1 {
		t.Fatal("legacy original upload digest unknown was hidden")
	}
	component := result.Components["application"].(map[string]any)
	schema := component["schema"].(string)
	t.Cleanup(func() { target.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE") })
	cfg, err := pgx.ParseConfig(s.cfg.RestoreTargetDSN)
	if err != nil {
		t.Fatal("restore configuration invalid")
	}
	cfg.RuntimeParams["search_path"] = schema + ",public"
	restoreDSN := cfg.ConnString()
	if strings.HasPrefix(restoreDSN, "postgres://") || strings.HasPrefix(restoreDSN, "postgresql://") {
		u, _ := url.Parse(restoreDSN)
		query := u.Query()
		query.Set("search_path", schema+",public")
		u.RawQuery = query.Encode()
		restoreDSN = u.String()
	} else {
		restoreDSN += " search_path='" + schema + ",public'"
	}
	probe, err := pgx.Connect(ctx, restoreDSN)
	if err != nil {
		t.Fatal("restored schema probe failed")
	}
	var actualSchema string
	err = probe.QueryRow(ctx, "SELECT current_schema()").Scan(&actualSchema)
	probe.Close(context.Background())
	if err != nil || actualSchema != schema {
		t.Fatal("restored reader did not select the isolated schema", actualSchema, err)
	}
	repo, err := postgres.OpenRestoredReader(ctx, restoreDSN, schema)
	if err != nil {
		t.Fatal("restored repository unavailable", err)
	}
	t.Cleanup(func() { repo.Close() })
	if run, err := repo.GetAnalysisRun(ctx, "fixture-tenant", "pending-run"); err != nil || run.Status != model.AnalysisCancelled || run.LeaseToken != 12 || run.LeaseOwner != "" || run.LeaseExpiresAt != 0 {
		t.Fatalf("pending execution unsafe: %+v %v", run, err)
	}
	if job, err := repo.GetAnalysisAIRevision(ctx, "fixture-tenant", "pending-ai"); err != nil || job.Status != model.AnalysisCancelled || job.LeaseToken != 6 || job.Deadline != 0 || job.LeaseOwner != "" {
		t.Fatalf("pending external result can resume: %+v %v", job, err)
	}
	if _, err := repo.CommitAnalysisBatch(ctx, "fixture-tenant", "pending-run", 11, model.AnalysisBatch{ID: "late", Status: model.AnalysisSucceeded}); err == nil {
		t.Fatal("restored reader accepted an old worker's write")
	}
	if job, err := repo.GetAnalysisAIRevision(ctx, "fixture-tenant", "completed-ai"); err != nil || job.Status != model.AnalysisSucceeded || !strings.Contains(string(job.Interpretation), "固定事实解读") {
		t.Fatal("completed AI interpretation altered", err)
	}
	var pending, actions, traceCount int
	if err := target.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{schema, "alarm_rule_revision_pending"}.Sanitize()).Scan(&pending); err != nil || pending != 0 {
		t.Fatal("processing clocks crossed restore boundary", err)
	}
	if err := target.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{schema, "duty_action_link"}.Sanitize()).Scan(&actions); err != nil || actions != 1 {
		t.Fatal("corrective action link missing", err)
	}
	if err := target.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{schema, "rule_evaluation_trace"}.Sanitize()+" WHERE status='IN_PROGRESS' AND body->>'reproductionQuality'='INCOMPLETE_TRACE'").Scan(&traceCount); err != nil || traceCount != 1 {
		t.Fatal("incomplete trace was promoted", err)
	}
	verifyApplicationRuleHistoryCorruption(t, ctx, target, schema)
	archive, err := minioadapter.New(s.cfg.RestoreMinIOEndpoint, s.cfg.RestoreMinIOAccessKey, s.cfg.RestoreMinIOSecretKey, s.cfg.RestoreMinIOUseTLS)
	if err != nil {
		t.Fatal(err)
	}
	resolve := func(ctx context.Context, a analytics.Actor) (analytics.Actor, error) {
		state, err := repo.LoadAccessState(ctx, a.TenantID)
		if err != nil {
			return a, err
		}
		for _, user := range state.Users {
			if user.Username == a.Username && user.Enabled {
				a.Permissions = user.Permissions
				a.DeviceIDs = user.DeviceIDs
				a.AllDevices = user.DeviceScope == "ALL"
				a.SessionVersion = user.SessionVersion
				return a, nil
			}
		}
		return a, analytics.ErrForbidden
	}
	actor := analytics.Actor{TenantID: "fixture-tenant", Username: "operator", Managed: true, SessionVersion: 1}
	analysis := analytics.NewService(repo, config.AnalyticsConfig{}, resolve, nil)
	for _, kind := range []string{analytics.KindMaintenance, analytics.KindInvestment} {
		id := "financial-observation"
		if kind == analytics.KindInvestment {
			id = "financial-investment"
		}
		if _, err := analysis.Get(ctx, actor, kind, id); !errors.Is(err, analytics.ErrForbidden) {
			t.Fatal("restored financial history lost its independent permission", err)
		}
	}
	if fixed, err := repo.GetAnalysisConfig(ctx, actor.TenantID, "numeric-revision"); err != nil || !analytics.AnalysisHashMatchesJSONB(fixed.Hash, fixed.Body) {
		t.Fatal("mixed numeric legacy digest changed across actual restore", err)
	}
	responseService := response.NewService(analysis, nil)
	responseService.Archive = archive
	revision, err := repo.GetAnalysisConfig(ctx, actor.TenantID, "attachment-RESPONSE_ATTACHMENT")
	if err != nil {
		t.Fatal(err)
	}
	originalHash := revision.Hash
	var original model.ResponseAttachment
	_ = json.Unmarshal(revision.Body, &original)
	metadata, reader, err := responseService.DownloadAttachment(ctx, actor, revision.ID)
	if err != nil {
		t.Fatal("authorized restored attachment read failed", err)
	}
	body, err := io.ReadAll(reader)
	reader.Close()
	if err != nil || string(body) != "固定私有附件：RESPONSE_ATTACHMENT" || metadata.ObjectKey != "" {
		t.Fatal("restored attachment content or secrecy changed", err)
	}
	fixed, err := repo.GetAnalysisConfig(ctx, actor.TenantID, revision.ID)
	if err != nil || fixed.Hash != originalHash || !sameApplicationHash(fixed.Body, revision.Body) {
		t.Fatal("immutable attachment hash changed", err)
	}
	for _, kind := range []string{"RESPONSE_ATTACHMENT", "MAINTENANCE_ATTACHMENT", model.DataQualityAttachmentKind} {
		id := "attachment-" + kind
		if kind == model.DataQualityAttachmentKind {
			page, _, err := repo.ListAnalysisConfigs(ctx, actor.TenantID, model.AnalysisFilter{Kind: kind, Limit: 20})
			if err != nil || len(page) != 1 {
				t.Fatal("legacy calibration attachment missing", err)
			}
			id = page[0].ID
		}
		v, err := repo.GetAnalysisConfig(ctx, actor.TenantID, id)
		if err != nil {
			t.Fatal(err)
		}
		var b struct {
			ObjectKey  string `json:"objectKey"`
			StorageKey string `json:"storageKey"`
		}
		_ = json.Unmarshal(v.Body, &b)
		if kind == model.DataQualityAttachmentKind {
			b.ObjectKey = b.StorageKey
			quality := dataquality.NewService(analysis, nil)
			quality.Archive = archive
			metadata, reader, err := quality.DownloadAttachment(ctx, actor, v.ID)
			if err != nil {
				t.Fatal("legacy calibration overlay read", err)
			}
			bytes, err := io.ReadAll(reader)
			reader.Close()
			if err != nil || string(bytes) != "固定私有附件："+kind || metadata.SHA256 != "" {
				t.Fatal("legacy upload hash fabricated or bytes lost", err)
			}
			fixed, err := repo.GetAnalysisConfig(ctx, actor.TenantID, v.ID)
			if err != nil || fixed.Hash != v.Hash || !sameApplicationHash(fixed.Body, v.Body) {
				t.Fatal("legacy calibration immutable body changed", err)
			}
		}
		location, err := repo.GetRestoredObjectLocation(ctx, actor.TenantID, applicationAttachmentBuckets[kind], b.ObjectKey)
		if err != nil {
			t.Fatal("object mapping unavailable", err)
		}
		if location.Key != b.ObjectKey || !strings.HasPrefix(location.Bucket, "iot-application-restore-") {
			t.Fatal("original immutable key was replaced")
		}
		t.Cleanup(func() {
			objects, err := minio.New(s.cfg.RestoreMinIOEndpoint, &minio.Options{Creds: credentials.NewStaticV4(s.cfg.RestoreMinIOAccessKey, s.cfg.RestoreMinIOSecretKey, ""), Secure: s.cfg.RestoreMinIOUseTLS})
			if err == nil {
				objects.RemoveObject(context.Background(), location.Bucket, location.Key, minio.RemoveObjectOptions{})
				objects.RemoveBucket(context.Background(), location.Bucket)
			}
		})
	}
	if _, err := repo.SaveAccessState(ctx, actor.TenantID, model.AccessState{}); err == nil {
		t.Fatal("restored historical reader allowed a business write")
	}
	// A current permission edit must affect already restored history immediately.
	state, err := repo.LoadAccessState(ctx, actor.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	state.Users[0].Permissions = []string{"menu:devices", "menu:response"}
	rawAccess, _ := json.Marshal(state)
	changed, err := target.Exec(ctx, "UPDATE "+pgx.Identifier{schema, "platform_access"}.Sanitize()+" SET body=$2,revision=revision+1 WHERE tenant_id=$1", actor.TenantID, rawAccess)
	if err != nil || changed.RowsAffected() != 1 {
		t.Fatal("isolated permission update", err)
	}
	if _, reader, err := responseService.DownloadAttachment(ctx, actor, revision.ID); !errors.Is(err, analytics.ErrForbidden) || reader != nil {
		t.Fatal("restored object exposed after current revocation", err)
	}
	var sourceStatus string
	if err := s.pool.QueryRow(ctx, `SELECT status FROM analysis_document WHERE tenant_id='fixture-tenant' AND kind='ai' AND id='pending-ai'`).Scan(&sourceStatus); err != nil || sourceStatus != model.AnalysisRunning {
		t.Fatal("restore mutated source execution", err)
	}
}

func verifyApplicationRuleHistoryCorruption(t *testing.T, ctx context.Context, target *pgx.Conn, schema string) {
	t.Helper()
	insertRevision := func(tx pgx.Tx, rule model.AlarmRule) {
		t.Helper()
		revision, activation := history.Register(rule, 2000)
		revision.SemanticsVersion = eval.RevisionV2
		body, _ := json.Marshal(revision)
		if _, err := tx.Exec(ctx, "INSERT INTO "+pgx.Identifier{schema, "alarm_rule_revision"}.Sanitize()+"(tenant_id,id,rule_id,version,hash,registered_at,body) VALUES($1,$2,$3,$4,$5,$6,$7)", revision.TenantID, revision.ID, revision.RuleID, revision.Version, revision.Hash, revision.RegisteredAt, body); err != nil {
			t.Fatal(err)
		}
		body, _ = json.Marshal(activation)
		if _, err := tx.Exec(ctx, "INSERT INTO "+pgx.Identifier{schema, "alarm_rule_activation"}.Sanitize()+"(tenant_id,rule_id,revision_id,version,since_at,deleted,body) VALUES($1,$2,$3,$4,$5,false,$6)", revision.TenantID, revision.RuleID, revision.ID, revision.Version, activation.Since, body); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"stale-current-pointer", "changed-current-rule", "activation-body-version", "missing-current-pointer", "missing-intermediate-version"} {
		t.Run(name, func(t *testing.T) {
			tx, err := target.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(context.Background())
			switch name {
			case "stale-current-pointer", "missing-intermediate-version":
				rule := model.AlarmRule{ID: "rule", TenantID: "fixture-tenant", Version: 2, Enabled: true, Name: "changed"}
				if name == "missing-intermediate-version" {
					rule.Version = 3
				}
				insertRevision(tx, rule)
				body, _ := json.Marshal(rule)
				if _, err = tx.Exec(ctx, "UPDATE "+pgx.Identifier{schema, "alarm_rule"}.Sanitize()+" SET body=$1 WHERE tenant_id=$2 AND id=$3", body, rule.TenantID, rule.ID); err != nil {
					t.Fatal(err)
				}
				if name == "missing-intermediate-version" {
					if _, err = tx.Exec(ctx, "UPDATE "+pgx.Identifier{schema, "alarm_rule_current_revision"}.Sanitize()+" SET revision_id=$1,version=$2 WHERE tenant_id=$3 AND rule_id=$4", model.RuleRevisionID(rule), rule.Version, rule.TenantID, rule.ID); err != nil {
						t.Fatal(err)
					}
				}
			case "changed-current-rule":
				_, err = tx.Exec(ctx, "UPDATE "+pgx.Identifier{schema, "alarm_rule"}.Sanitize()+" SET body=jsonb_set(body,'{name}','\"changed\"') WHERE tenant_id='fixture-tenant' AND id='rule'")
			case "activation-body-version":
				_, err = tx.Exec(ctx, "UPDATE "+pgx.Identifier{schema, "alarm_rule_activation"}.Sanitize()+" SET body=jsonb_set(body,'{version}','99') WHERE tenant_id='fixture-tenant' AND rule_id='rule'")
			case "missing-current-pointer":
				_, err = tx.Exec(ctx, "DELETE FROM "+pgx.Identifier{schema, "alarm_rule_current_revision"}.Sanitize()+" WHERE tenant_id='fixture-tenant' AND rule_id='rule'")
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = validateApplicationHistory(ctx, tx, schema); err == nil {
				t.Fatal("inconsistent rule history accepted")
			}
		})
	}
	tx, err := target.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	for _, version := range []int{0, 5} {
		rule := model.AlarmRule{ID: fmt.Sprintf("known-current-%d", version), TenantID: "fixture-tenant", Version: version, Enabled: true}
		insertRevision(tx, rule)
		body, _ := json.Marshal(rule)
		if _, err = tx.Exec(ctx, "INSERT INTO "+pgx.Identifier{schema, "alarm_rule"}.Sanitize()+"(tenant_id,id,product_id,enabled,body,updated_at) VALUES($1,$2,NULL,true,$3,now())", rule.TenantID, rule.ID, body); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, "INSERT INTO "+pgx.Identifier{schema, "alarm_rule_current_revision"}.Sanitize()+"(tenant_id,rule_id,revision_id,version,deleted) VALUES($1,$2,$3,$4,false)", rule.TenantID, rule.ID, model.RuleRevisionID(rule), rule.Version); err != nil {
			t.Fatal(err)
		}
	}
	if err = validateApplicationHistory(ctx, tx, schema); err != nil {
		t.Fatal("initial registration invented a missing historical version", err)
	}
}

func verifyCorruptApplicationRollback(t *testing.T, ctx context.Context, s *Service, target *pgx.Conn, manifest Manifest, restoreID string) {
	t.Helper()
	dir := t.TempDir()
	bad := manifest
	bad.ID = manifest.ID + "-corrupt"
	bad.Artifacts = nil
	for _, name := range []string{"application-schema.json", "application-postgres.jsonl.gz", "application-objects.tar.gz"} {
		original := filepath.Join(s.cfg.BackupDir, manifest.ID, name)
		destination := filepath.Join(dir, name)
		if name == "application-postgres.jsonl.gz" {
			file, err := os.Open(original)
			if err != nil {
				t.Fatal(err)
			}
			gz, err := gzip.NewReader(file)
			if err != nil {
				t.Fatal(err)
			}
			dec := json.NewDecoder(gz)
			err = writeGzip(destination, func(w io.Writer) error {
				enc := json.NewEncoder(w)
				for {
					var row knowledgeRow
					if err := dec.Decode(&row); errors.Is(err, io.EOF) {
						return nil
					} else if err != nil {
						return err
					}
					if row.Table == "analysis_document" {
						var document applicationDocument
						if err := json.Unmarshal(row.Row, &document); err != nil {
							return err
						}
						if document.Kind == "config" && document.ID == "procedure-revision" {
							v, _ := decodeApplication[model.AnalysisConfigRevision](document)
							v.Body = json.RawMessage(`{"name":"tampered"}`)
							document.Body, _ = json.Marshal(v)
							row.Row, _ = json.Marshal(document)
						}
					}
					if err := enc.Encode(row); err != nil {
						return err
					}
				}
			})
			gz.Close()
			file.Close()
			if err != nil {
				t.Fatal(err)
			}
		} else {
			body, err := os.ReadFile(original)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(destination, body, 0600); err != nil {
				t.Fatal(err)
			}
		}
		artifact, err := s.uploadAndVerify(ctx, bad.ID, destination)
		if err != nil {
			t.Fatal(err)
		}
		bad.Artifacts = append(bad.Artifacts, artifact)
		t.Cleanup(func() {
			s.store.RemoveObject(context.Background(), s.cfg.BackupBucket, artifact.ObjectKey, minio.RemoveObjectOptions{})
		})
	}
	result := RestoreResult{RestoreID: restoreID + "-corrupt", BackupID: bad.ID, Components: map[string]any{}}
	if err := s.restoreApplication(ctx, target, bad, &result); err == nil {
		t.Fatal("hash-corrupt application snapshot was restored")
	}
	hash := sha256.Sum256([]byte(result.RestoreID))
	schema := "application_restore_" + hex.EncodeToString(hash[:10])
	var absent bool
	if err := target.QueryRow(ctx, `SELECT to_regnamespace($1) IS NULL`, schema).Scan(&absent); err != nil || !absent {
		t.Fatal("failed application restore left readable data", err)
	}
}
