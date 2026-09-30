package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

func (s *Service) restoreDuty(ctx context.Context, target *pgx.Conn, m Manifest, res *RestoreResult) error {
	if m.FormatVersion < 3 || m.Type != "FULL" {
		res.Components["duty"] = map[string]any{"status": "not_included", "reason": "this backup predates duty management or contains device messages only"}
		res.Components["dutyObjects"] = map[string]any{"status": "not_included"}
		return nil
	}
	stage, err := os.MkdirTemp(s.cfg.BackupDir, ".duty-restore-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	paths := map[string]string{}
	for _, name := range []string{"duty-schema.json", "duty-postgres.jsonl.gz", "duty-objects.tar.gz"} {
		path, e := s.downloadVerifiedArtifact(ctx, m, res.BackupID, name, stage)
		if e != nil {
			return e
		}
		paths[name] = path
	}
	var schema knowledgeSchema
	b, err := os.ReadFile(paths["duty-schema.json"])
	if err != nil {
		return err
	}
	if err = json.Unmarshal(b, &schema); err != nil {
		return err
	}
	if err = validateDutySchema(schema); err != nil {
		return err
	}
	hash := sha256.Sum256([]byte(res.RestoreID))
	schemaName := "duty_restore_" + hex.EncodeToString(hash[:10])
	tx, err := target.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schemaName}.Sanitize()); err != nil {
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
		if _, err = tx.Exec(ctx, "CREATE TABLE "+pgx.Identifier{schemaName, table.Name}.Sanitize()+"("+strings.Join(cols, ",")+")"); err != nil {
			return err
		}
	}
	counts, err := restoreDutyRows(ctx, tx, schemaName, paths["duty-postgres.jsonl.gz"])
	if err != nil {
		return err
	}
	for _, table := range dutyTables {
		if counts[table] != dutyTableCount(m, table) {
			return fmt.Errorf("restored duty count mismatch: %s", table)
		}
	}
	for _, table := range dutyTables {
		ident := pgx.Identifier{schemaName, table}.Sanitize()
		if table == "duty_business_event" {
			if _, err = tx.Exec(ctx, "CREATE UNIQUE INDEX ON "+ident+"(tenant_id,id)"); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, "CREATE INDEX ON "+ident+"(tenant_id,occurred_at DESC,seq DESC)"); err != nil {
				return err
			}
			sequence := pgx.Identifier{schemaName, "duty_business_event_seq"}.Sanitize()
			if _, err = tx.Exec(ctx, "CREATE SEQUENCE "+sequence); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, "ALTER TABLE "+ident+" ALTER COLUMN seq SET DEFAULT nextval('"+sequence+"'::regclass)"); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, "SELECT setval('"+sequence+"'::regclass,COALESCE((SELECT max(seq) FROM "+ident+"),1),(SELECT count(*)>0 FROM "+ident+"))"); err != nil {
				return err
			}
			continue
		}
		if _, err = tx.Exec(ctx, "CREATE INDEX ON "+ident+"(tenant_id,updated_at DESC,id DESC)"); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "CREATE INDEX ON "+ident+"(tenant_id,(body->>'stationId'),(body->>'status'))"); err != nil {
			return err
		}
	}
	objectCount, err := s.restoreDutyObjects(ctx, tx, schemaName, paths["duty-objects.tar.gz"], stage, res.RestoreID)
	if err != nil {
		return err
	}
	if objectCount != componentCount(m, "dutyObjects", "objects") {
		return errors.New("duty attachment count mismatch")
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	res.Components["duty"] = map[string]any{"status": "restored", "schema": schemaName, "tables": counts, "matches": true}
	res.Components["dutyObjects"] = map[string]any{"status": "restored", "objects": objectCount, "matches": true, "bucket": dutyAttachmentBucket}
	return nil
}
func validateDutySchema(schema knowledgeSchema) error {
	if len(schema.Tables) != len(dutyTables) {
		return errors.New("duty schema is incomplete")
	}
	seen := map[string]bool{}
	base := map[string]string{"tenant_id": "text", "id": "text", "version": "int8", "created_at": "int8", "updated_at": "int8", "body": "jsonb"}
	event := map[string]string{"seq": "int8", "tenant_id": "text", "id": "text", "event_type": "text", "station_id": "text", "run_id": "text", "device_id": "text", "actor_id": "text", "occurred_at": "int8", "recorded_at": "int8", "body": "jsonb"}
	for _, table := range schema.Tables {
		if !slices.Contains(dutyTables, table.Name) || seen[table.Name] {
			return errors.New("invalid duty table schema")
		}
		seen[table.Name] = true
		expected := base
		pk := []string{"tenant_id", "id"}
		if table.Name == "duty_business_event" {
			expected = event
			pk = []string{"seq"}
		}
		if len(table.Columns) != len(expected) || !slices.Equal(table.PrimaryKey, pk) {
			return errors.New("invalid duty table columns or primary key")
		}
		cols := map[string]bool{}
		for _, col := range table.Columns {
			if expected[col.Name] != col.Type || !col.NotNull || cols[col.Name] {
				return errors.New("invalid duty column schema")
			}
			cols[col.Name] = true
		}
	}
	return nil
}
func dutyTableCount(m Manifest, table string) int64 {
	v, _ := m.Components["duty"].(map[string]any)
	switch tables := v["tables"].(type) {
	case map[string]any:
		return manifestRecords(map[string]any{"records": tables[table]})
	case map[string]int64:
		return tables[table]
	}
	return 0
}
func restoreDutyRows(ctx context.Context, tx pgx.Tx, schema, path string) (map[string]int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	counts := map[string]int64{}
	for _, table := range dutyTables {
		counts[table] = 0
	}
	dec := json.NewDecoder(gz)
	for {
		var row knowledgeRow
		err = dec.Decode(&row)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if _, ok := counts[row.Table]; !ok || !json.Valid(row.Row) {
			return nil, errors.New("invalid duty backup row")
		}
		ident := pgx.Identifier{schema, row.Table}.Sanitize()
		if _, err = tx.Exec(ctx, "INSERT INTO "+ident+" SELECT * FROM jsonb_populate_record(NULL::"+ident+",$1::jsonb)", []byte(row.Row)); err != nil {
			return nil, err
		}
		counts[row.Table]++
	}
	return counts, nil
}
func readDutyObjects(path, stage string) (map[string]string, []knowledgeObject, error) {
	return readSnapshotObjects(path, stage, dutyAttachmentBucket)
}
func readSnapshotObjects(path, stage, bucket string) (map[string]string, []knowledgeObject, error) {
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
	tr := tar.NewReader(gz)
	entries := map[string]string{}
	refs := []knowledgeObject{}
	found := false
	for {
		h, e := tr.Next()
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			return nil, nil, e
		}
		if h.Typeflag != tar.TypeReg || h.Size < 0 {
			return nil, nil, errors.New("invalid duty object archive")
		}
		if h.Name == "index.json" {
			if found || h.Size > 32<<20 {
				return nil, nil, errors.New("invalid duty object index")
			}
			found = true
			if e = json.NewDecoder(io.LimitReader(tr, h.Size)).Decode(&refs); e != nil {
				return nil, nil, e
			}
			continue
		}
		if !regexpObjectEntry(h.Name) || entries[h.Name] != "" {
			return nil, nil, errors.New("invalid duty object entry")
		}
		p := filepath.Join(stage, strings.ReplaceAll(h.Name, "/", "_"))
		o, e := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return nil, nil, e
		}
		n, e := io.Copy(o, tr)
		closeErr := o.Close()
		if e != nil {
			return nil, nil, e
		}
		if closeErr != nil {
			return nil, nil, closeErr
		}
		if n != h.Size {
			return nil, nil, errors.New("truncated duty attachment")
		}
		entries[h.Name] = p
	}
	if !found || len(entries) != len(refs) {
		return nil, nil, errors.New("duty object index mismatch")
	}
	seen := map[string]bool{}
	keys := map[string]bool{}
	for _, ref := range refs {
		if entries[ref.Entry] == "" || seen[ref.Entry] || ref.Bucket != bucket || ref.Key == "" || keys[ref.Key] {
			return nil, nil, errors.New("invalid duty object reference")
		}
		seen[ref.Entry] = true
		keys[ref.Key] = true
		hash, size, e := hashFile(entries[ref.Entry])
		if e != nil {
			return nil, nil, e
		}
		if size != ref.Size || hash != ref.SHA256 {
			return nil, nil, errors.New("duty attachment checksum mismatch")
		}
	}
	return entries, refs, nil
}
func (s *Service) restoreDutyObjects(ctx context.Context, tx pgx.Tx, schema, path, stage, restoreID string) (int64, error) {
	entries, refs, err := readDutyObjects(path, stage)
	if err != nil {
		return 0, err
	}
	var storedRefs int
	if err = tx.QueryRow(ctx, "SELECT count(DISTINCT body->'attachment'->>'objectKey') FROM "+pgx.Identifier{schema, "duty_attachment"}.Sanitize()).Scan(&storedRefs); err != nil {
		return 0, err
	}
	if storedRefs != len(refs) {
		return 0, errors.New("duty attachment references do not match object archive")
	}
	if len(refs) == 0 {
		return 0, nil
	}
	if strings.TrimSpace(s.cfg.RestoreMinIOEndpoint) == "" || strings.EqualFold(strings.TrimRight(s.cfg.MinIOEndpoint, "/"), strings.TrimRight(s.cfg.RestoreMinIOEndpoint, "/")) {
		return 0, errors.New("duty attachments require an independent MinIO restore endpoint")
	}
	dr, err := minio.New(s.cfg.RestoreMinIOEndpoint, &minio.Options{Creds: credentials.NewStaticV4(s.cfg.RestoreMinIOAccessKey, s.cfg.RestoreMinIOSecretKey, ""), Secure: s.cfg.RestoreMinIOUseTLS})
	if err != nil {
		return 0, err
	}
	if err = s.ensureBucket(ctx, dr, dutyAttachmentBucket); err != nil {
		return 0, err
	}
	remap := map[string]string{}
	for _, ref := range refs {
		var n int
		if err = tx.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{schema, "duty_attachment"}.Sanitize()+" WHERE body->'attachment'->>'objectKey'=$1", ref.Key).Scan(&n); err != nil {
			return 0, err
		}
		if n == 0 {
			return 0, errors.New("duty attachment has no metadata reference")
		}
		key := "restore/" + restoreID + "/" + ref.Entry
		if _, err = dr.FPutObject(ctx, dutyAttachmentBucket, key, entries[ref.Entry], minio.PutObjectOptions{ContentType: ref.ContentType}); err != nil {
			return 0, err
		}
		obj, e := dr.GetObject(ctx, dutyAttachmentBucket, key, minio.GetObjectOptions{})
		if e != nil {
			return 0, e
		}
		h := sha256.New()
		size, e := io.Copy(h, obj)
		obj.Close()
		if e != nil {
			return 0, e
		}
		if size != ref.Size || hex.EncodeToString(h.Sum(nil)) != ref.SHA256 {
			return 0, errors.New("restored duty attachment verification failed")
		}
		remap[ref.Key] = key
	}
	for _, table := range dutyTables {
		ident := pgx.Identifier{schema, table}.Sanitize()
		rows, e := tx.Query(ctx, "SELECT tenant_id,id,body FROM "+ident)
		if e != nil {
			return 0, e
		}
		type changed struct {
			tenant, id string
			body       []byte
		}
		changes := []changed{}
		for rows.Next() {
			var v changed
			if e = rows.Scan(&v.tenant, &v.id, &v.body); e != nil {
				rows.Close()
				return 0, e
			}
			b, yes, e := rewriteDutyObjectKeys(v.body, remap)
			if e != nil {
				rows.Close()
				return 0, e
			}
			if yes {
				v.body = b
				changes = append(changes, v)
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return 0, e
		}
		for _, v := range changes {
			if _, e = tx.Exec(ctx, "UPDATE "+ident+" SET body=$3 WHERE tenant_id=$1 AND id=$2", v.tenant, v.id, v.body); e != nil {
				return 0, e
			}
		}
	}
	return int64(len(refs)), nil
}
func rewriteDutyObjectKeys(body []byte, remap map[string]string) ([]byte, bool, error) {
	return rewriteSnapshotObjectKeys(body, remap, "objectKey")
}
func rewriteSnapshotObjectKeys(body []byte, remap map[string]string, field string) ([]byte, bool, error) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, false, err
	}
	changed := false
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, child := range x {
				if k == field {
					if old, ok := child.(string); ok {
						if next, found := remap[old]; found {
							x[k] = next
							changed = true
						}
					}
				}
				walk(child)
			}
		case []any:
			for _, child := range x {
				walk(child)
			}
		}
	}
	walk(value)
	if !changed {
		return body, false, nil
	}
	b, err := json.Marshal(value)
	return b, true, err
}
