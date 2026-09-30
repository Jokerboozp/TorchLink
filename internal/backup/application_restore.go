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
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (s *Service) restoreApplication(ctx context.Context, target *pgx.Conn, m Manifest, res *RestoreResult) error {
	if m.FormatVersion < 4 || m.Type != "FULL" {
		res.Components["application"] = map[string]any{"status": "not_included", "reason": "this backup predates fixed analysis snapshots or contains device messages only"}
		return nil
	}
	stage, err := os.MkdirTemp(s.cfg.BackupDir, ".application-restore-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	paths := map[string]string{}
	for _, name := range []string{"application-schema.json", "application-postgres.jsonl.gz", "application-objects.tar.gz"} {
		path, e := s.downloadVerifiedArtifact(ctx, m, res.BackupID, name, stage)
		if e != nil {
			return e
		}
		paths[name] = path
	}
	raw, err := os.ReadFile(paths["application-schema.json"])
	if err != nil {
		return err
	}
	var schema knowledgeSchema
	if err = json.Unmarshal(raw, &schema); err != nil {
		return err
	}
	if err = validateApplicationSchema(schema); err != nil {
		return err
	}
	hash := sha256.Sum256([]byte(res.RestoreID))
	schemaName := "application_restore_" + hex.EncodeToString(hash[:10])
	tx, err := target.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = createApplicationTables(ctx, tx, schemaName, schema); err != nil {
		return err
	}
	counts, documents, err := restoreApplicationRows(ctx, tx, schemaName, paths["application-postgres.jsonl.gz"])
	if err != nil {
		return err
	}
	for _, table := range applicationTables() {
		if counts[table] != applicationTableCount(m, table) {
			return fmt.Errorf("restored application count mismatch: %s", table)
		}
	}
	if err = validateApplicationDocuments(documents); err != nil {
		return err
	}
	if err = validateApplicationHistory(ctx, tx, schemaName); err != nil {
		return err
	}
	retired, err := retireApplicationExecutions(ctx, tx, schemaName, res.RestoreID, documents)
	if err != nil {
		return err
	}
	if err = createApplicationIndexes(ctx, tx, schemaName); err != nil {
		return err
	}
	objects, err := s.restoreApplicationObjects(ctx, tx, schemaName, paths["application-objects.tar.gz"], stage, res.RestoreID)
	if err != nil {
		return err
	}
	if objects != componentCount(m, "applicationObjects", "objects") {
		return errors.New("application attachment count mismatch")
	}
	coldRecords := componentCount(m, "rawMessages", "clickhouse") + componentCount(m, "parsedMessages", "clickhouse")
	var legacyRaw int64
	if err = tx.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{schemaName, "raw_archive_index"}.Sanitize()+" WHERE object_bucket NOT IN ('postgres','clickhouse')").Scan(&legacyRaw); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	partial := coldRecords > 0 || legacyRaw > 0
	limitations := []string{"仅恢复备份时仍保留的 PostgreSQL 来源；已过期或采集前缺失的原始证据保持未知"}
	if coldRecords > 0 {
		limitations = append(limitations, "ClickHouse 消息仅恢复为校验制品；冷源查询未还原")
	}
	if legacyRaw > 0 {
		limitations = append(limitations, "旧版原始报文对象仅保留索引；原件未包含在本备份内")
	}
	res.Components["application"] = map[string]any{"status": "restored", "schema": schemaName, "tables": counts, "matches": true, "retiredExecutions": retired, "partial": partial, "limitations": limitations, "mode": "read-only historical load; pending processing clocks retired", "hashVerification": "ORIGINAL_SHA256_EXACT_NUMERIC_LEXEMES", "sourceCoverage": map[string]any{"postgres": map[string]any{"status": "RETAINED_ONLY", "complete": false}, "clickhouse": map[string]any{"status": "ARTIFACT_STAGING_ONLY", "complete": false, "records": coldRecords}, "legacyRawObjects": map[string]any{"status": "NOT_INCLUDED", "complete": false, "records": legacyRaw}}}
	if partial {
		res.Status = "PARTIAL"
	}
	res.Components["applicationObjects"] = map[string]any{"status": "restored", "objects": objects, "matches": true, "mapping": "immutable source keys resolved through private restored-object overlays", "originalHashUnknown": componentCount(m, "applicationObjects", "originalHashUnknown")}
	return nil
}
func createApplicationTables(ctx context.Context, tx pgx.Tx, schemaName string, schema knowledgeSchema) error {
	if _, err := tx.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schemaName}.Sanitize()); err != nil {
		return err
	}
	for _, table := range schema.Tables {
		cols := []string{}
		for _, col := range table.Columns {
			def := pgx.Identifier{col.Name}.Sanitize() + " " + knowledgeSQLTypes[col.Type]
			if col.NotNull {
				def += " NOT NULL"
			}
			cols = append(cols, def)
		}
		pk := []string{}
		for _, col := range table.PrimaryKey {
			pk = append(pk, pgx.Identifier{col}.Sanitize())
		}
		cols = append(cols, "PRIMARY KEY("+strings.Join(pk, ",")+")")
		if _, err := tx.Exec(ctx, "CREATE TABLE "+pgx.Identifier{schemaName, table.Name}.Sanitize()+"("+strings.Join(cols, ",")+")"); err != nil {
			return err
		}
	}
	return nil
}
func applicationTableCount(m Manifest, table string) int64 {
	v, _ := m.Components["application"].(map[string]any)
	switch values := v["tables"].(type) {
	case map[string]any:
		return manifestRecords(map[string]any{"records": values[table]})
	case map[string]int64:
		return values[table]
	}
	return 0
}
func restoreApplicationRows(ctx context.Context, tx pgx.Tx, schema, path string) (map[string]int64, []applicationDocument, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, nil, err
	}
	defer gz.Close()
	dec := json.NewDecoder(gz)
	counts := map[string]int64{}
	documents := []applicationDocument{}
	for _, table := range applicationTables() {
		counts[table] = 0
	}
	for {
		var row knowledgeRow
		err = dec.Decode(&row)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		spec, ok := applicationSpecs[row.Table]
		if !ok || !json.Valid(row.Row) {
			return nil, nil, errors.New("invalid application backup row")
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(row.Row, &fields) != nil || len(fields) != len(spec.Columns) {
			return nil, nil, errors.New("application row column count mismatch")
		}
		for key := range fields {
			if _, ok = spec.Columns[key]; !ok {
				return nil, nil, errors.New("invalid application row column")
			}
		}
		ident := pgx.Identifier{schema, row.Table}.Sanitize()
		if _, err = tx.Exec(ctx, "INSERT INTO "+ident+" SELECT * FROM jsonb_populate_record(NULL::"+ident+",$1::jsonb)", []byte(row.Row)); err != nil {
			return nil, nil, err
		}
		counts[row.Table]++
		if row.Table == "analysis_document" {
			var d applicationDocument
			if json.Unmarshal(row.Row, &d) != nil {
				return nil, nil, errors.New("invalid analysis document")
			}
			documents = append(documents, d)
		}
	}
	return counts, documents, nil
}
func createApplicationIndexes(ctx context.Context, tx pgx.Tx, schema string) error {
	indexes := map[string][]string{
		"analysis_document":   {"(tenant_id,kind,run_id,created_at DESC,id)", "(tenant_id,kind,application_kind,status)", "(tenant_id,kind,resource_id)"},
		"alarm_rule_revision": {"UNIQUE (tenant_id,rule_id,version)"}, "rule_evaluation_trace": {"UNIQUE (tenant_id,message_id,claim_token)", "(tenant_id,device_id,message_timestamp,id)"},
		"analytics_configuration_event": {"UNIQUE (tenant_id,source,resource_id,resource_version)", "(tenant_id,device_id,occurred_at,seq)"},
		"raw_archive_index":             {"(tenant_id,device_id,received_at,message_id)"}, "raw_message_log": {"(tenant_id,device_id,received_at,message_id)"}, "standard_message": {"(tenant_id,device_id,ts,message_id)"},
		"measurement_availability": {"(tenant_id,available_at,message_id)"}, "alarm_record": {"(tenant_id,device_id,last_triggered_at,id)"},
	}
	for table, defs := range indexes {
		for _, def := range defs {
			prefix := "CREATE INDEX ON "
			if strings.HasPrefix(def, "UNIQUE ") {
				prefix = "CREATE UNIQUE INDEX ON "
				def = strings.TrimPrefix(def, "UNIQUE ")
			}
			if _, err := tx.Exec(ctx, prefix+pgx.Identifier{schema, table}.Sanitize()+def); err != nil {
				return err
			}
		}
	}
	// New configuration/state events may be explicitly recorded by an isolated
	// reader fixture without colliding with the preserved source sequence.
	for table, column := range map[string]string{"analytics_configuration_event": "seq", "device_state_event": "id"} {
		ident := pgx.Identifier{schema, table}.Sanitize()
		seq := pgx.Identifier{schema, table + "_restore_seq"}.Sanitize()
		col := pgx.Identifier{column}.Sanitize()
		for _, statement := range []string{"CREATE SEQUENCE " + seq, "ALTER TABLE " + ident + " ALTER COLUMN " + col + " SET DEFAULT nextval('" + seq + "'::regclass)", "SELECT setval('" + seq + "'::regclass,COALESCE((SELECT max(" + col + ") FROM " + ident + "),1),(SELECT count(*)>0 FROM " + ident + "))"} {
			if _, err := tx.Exec(ctx, statement); err != nil {
				return err
			}
		}
	}
	return nil
}
