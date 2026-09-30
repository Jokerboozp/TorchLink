package backup

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"iot-platform/internal/adapters/postgres"
)

func TestGovernanceSnapshotSchemaRejectsUnknownTablesAndCode(t *testing.T) {
	schema := knowledgeSchema{}
	for _, name := range postgres.AlarmGovernanceRestoreTables() {
		schema.Tables = append(schema.Tables, knowledgeTable{Name: name, Columns: []knowledgeColumn{{Name: "id", Type: "text", NotNull: true}}, PrimaryKey: []string{"id"}})
	}
	if err := validateGovernanceSchema(schema); err != nil {
		t.Fatal(err)
	}
	schema.Tables[0].Name = "alarm_observation; DROP SCHEMA public CASCADE"
	if err := validateGovernanceSchema(schema); err == nil {
		t.Fatal("untrusted DDL table accepted")
	}
	schema.Tables[0].Name = postgres.AlarmGovernanceRestoreTables()[0]
	schema.Tables[0].Columns[0].Type = "text default shell()"
	if err := validateGovernanceSchema(schema); err == nil {
		t.Fatal("executable type accepted")
	}
}
func TestGovernanceObjectArchiveChecksBucketHashAndMetadata(t *testing.T) {
	payload := []byte("field evidence")
	hash := sha256.Sum256(payload)
	for _, bucket := range []string{governanceAttachmentBucket, dutyAttachmentBucket} {
		dir := t.TempDir()
		path := filepath.Join(dir, "governance.tar.gz")
		refs := []knowledgeObject{{Bucket: bucket, Key: "tenant/object", Entry: "objects/000000000000", Size: int64(len(payload)), SHA256: hex.EncodeToString(hash[:])}}
		if err := writeGzip(path, func(w io.Writer) error {
			tw := tar.NewWriter(w)
			if err := tw.WriteHeader(&tar.Header{Name: refs[0].Entry, Typeflag: tar.TypeReg, Mode: 0600, Size: int64(len(payload))}); err != nil {
				return err
			}
			if _, err := tw.Write(payload); err != nil {
				return err
			}
			b, _ := json.Marshal(refs)
			if err := tw.WriteHeader(&tar.Header{Name: "index.json", Typeflag: tar.TypeReg, Mode: 0600, Size: int64(len(b))}); err != nil {
				return err
			}
			if _, err := tw.Write(b); err != nil {
				return err
			}
			return tw.Close()
		}); err != nil {
			t.Fatal(err)
		}
		_, got, err := readSnapshotObjects(path, dir, governanceAttachmentBucket)
		if bucket == governanceAttachmentBucket {
			if err != nil || len(got) != 1 {
				t.Fatal(got, err)
			}
		} else if err == nil {
			t.Fatal("another bucket accepted")
		}
	}
}
func TestGovernanceSnapshotPostgresRoundtripPreservesGeneratedRelationsAndTasks(t *testing.T) {
	dsn := os.Getenv("IOT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("IOT_TEST_POSTGRES_DSN is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	sourceSchema := fmt.Sprintf("governance_backup_%d", time.Now().UnixNano())
	targetSchema := sourceSchema + "_restored"
	defer pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+pgx.Identifier{sourceSchema}.Sanitize()+" CASCADE")
	defer pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+pgx.Identifier{targetSchema}.Sanitize()+" CASCADE")
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{sourceSchema}.Sanitize()+";SET LOCAL search_path="+pgx.Identifier{sourceSchema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, postgres.AlarmGovernanceRestoreSchema()); err != nil {
		t.Fatal(err)
	}
	fixtures := []string{
		`INSERT INTO alarm_observation(tenant_id,id,slot_key,device_id,alarm_type,origin_kind,signal_key,event_at,recorded_at,acceptance,fact_kind,content_hash,body) VALUES('tenant','obs','slot','device','FIRE','COMPONENT_STATE','component:c:FIRE',100,200,'ACCEPTED','ASSERT','hash','{"id":"obs"}')`,
		`INSERT INTO alarm_signal_state(tenant_id,device_id,signal_key,event_at,active,observation_id) VALUES('tenant','device','component:c:FIRE',100,true,'obs')`,
		`INSERT INTO alarm_observation_attempt(tenant_id,observation_id,recorded_at,body) VALUES('tenant','obs',300,'{"acceptance":"ACCEPTED"}')`,
		`INSERT INTO alarm_governance_case(tenant_id,id,version,created_by,created_at,updated_at,device_ids,point_key,status,body) VALUES('tenant','case',1,'operator',1,1,'{device}','point','INVESTIGATING','{}')`,
		`INSERT INTO alarm_governance_round(tenant_id,id,version,created_by,created_at,updated_at,case_id,device_ids,status,body) VALUES('tenant','round',1,'operator',1,1,'case','{device}','ACTIVE','{}')`,
		`INSERT INTO alarm_governance_verification(tenant_id,id,version,created_by,created_at,updated_at,case_id,round_id,device_ids,body) VALUES('tenant','verification',1,'operator',1,1,'case','round','{device}','{"observationIds":["obs"]}')`,
		`INSERT INTO alarm_governance_verification_link(tenant_id,id,version,created_by,created_at,updated_at,case_id,round_id,device_ids,body) VALUES('tenant','link',1,'operator',1,1,'case','round','{device}','{"verificationId":"verification"}')`,
		`INSERT INTO alarm_governance_source_version(tenant_id,dependency_key,bucket_start,generation) VALUES('tenant','device-dependency',0,3)`,
		`INSERT INTO alarm_governance_case_history(tenant_id,resource_id,version,recorded_at,body) VALUES('tenant','case',1,2,'{"id":"case","body":{"status":"INVESTIGATING"}}')`,
	}
	for _, query := range fixtures {
		if _, err = tx.Exec(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	for _, kind := range []string{"run", "snapshot", "config", "ai"} {
		if _, err = tx.Exec(ctx, `INSERT INTO analysis_document(tenant_id,kind,id,application_kind,version,body) VALUES('tenant',$1,$1,'recurring-alarm-governance',1,'{"status":"RUNNING","frozenFacts":"immutable"}')`, kind); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(t.TempDir(), "snapshot.jsonl.gz")
	schema, counts, err := exportTableSnapshot(ctx, tx, postgres.AlarmGovernanceRestoreTables(), path)
	if err != nil {
		t.Fatal(err)
	}
	if counts["analysis_document"] != 4 || counts["alarm_governance_verification_link"] != 1 || counts["alarm_governance_case_history"] != 1 {
		t.Fatal(counts)
	}
	if _, err = tx.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{targetSchema}.Sanitize()+";SET LOCAL search_path="+pgx.Identifier{targetSchema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, postgres.AlarmGovernanceRestoreSchema()); err != nil {
		t.Fatal(err)
	}
	if err = verifySnapshotSchema(ctx, tx, schema); err != nil {
		t.Fatal(err)
	}
	restored, err := restoreSnapshotRows(ctx, tx, targetSchema, path, schema)
	if err != nil {
		t.Fatal(err)
	}
	for table, n := range counts {
		if restored[table] != n {
			t.Fatal(table, n, restored[table])
		}
	}
	if err = validateGovernanceReferences(ctx, tx); err != nil {
		t.Fatal(err)
	}
	var generated string
	if err = tx.QueryRow(ctx, `SELECT verification_id FROM alarm_governance_verification_link WHERE id='link'`).Scan(&generated); err != nil || generated != "verification" {
		t.Fatal("generated FK relation not recovered", generated, err)
	}
	var preserved string
	if err = tx.QueryRow(ctx, `SELECT body->>'frozenFacts' FROM analysis_document WHERE kind='snapshot'`).Scan(&preserved); err != nil || preserved != "immutable" {
		t.Fatal("analysis snapshot lost", preserved, err)
	}
	if _, err = tx.Exec(ctx, `SET CONSTRAINTS ALL IMMEDIATE`); err != nil {
		t.Fatal("restored typed relationships invalid", err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}
