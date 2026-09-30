package backup

import (
	"archive/tar"
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
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

func (s *Service) restoreKnowledgeAndAgents(ctx context.Context, target *pgx.Conn, m Manifest, res *RestoreResult) error {
	if m.FormatVersion < 2 || m.Type != "FULL" {
		res.Components["knowledge"] = map[string]any{"status": "not_included", "reason": "this backup contains device messages only"}
		res.Components["knowledgeObjects"] = map[string]any{"status": "not_included"}
		res.Components["harness"] = map[string]any{"status": "not_included"}
		return nil
	}
	if strings.TrimSpace(s.cfg.RestoreHarnessDir) == "" {
		return errors.New("IOT_BACKUP_RESTORE_HARNESS_DIR must name an independent restore directory")
	}
	if err := harnessRestoreTargetSafe(s.cfg.HarnessDataDir, s.cfg.RestoreHarnessDir); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(s.cfg.BackupDir, ".knowledge-restore-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	paths := map[string]string{}
	for _, name := range []string{"knowledge-schema.json", "knowledge-postgres.jsonl.gz", "knowledge-objects.tar.gz", "harness-data.tar.gz"} {
		p, e := s.downloadVerifiedArtifact(ctx, m, res.BackupID, name, stage)
		if e != nil {
			return e
		}
		paths[name] = p
	}
	var schema knowledgeSchema
	body, err := os.ReadFile(paths["knowledge-schema.json"])
	if err != nil {
		return err
	}
	if err = json.Unmarshal(body, &schema); err != nil {
		return err
	}
	if err = validateKnowledgeSchema(schema); err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(res.RestoreID))
	schemaName := "kb_restore_" + hex.EncodeToString(sum[:10])
	tx, err := target.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS vector WITH SCHEMA public"); err != nil {
		return fmt.Errorf("restore pgvector: %w", err)
	}
	if _, err = tx.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schemaName}.Sanitize()); err != nil {
		return err
	}
	for _, table := range schema.Tables {
		columns := []string{}
		for _, c := range table.Columns {
			def := pgx.Identifier{c.Name}.Sanitize() + " " + knowledgeSQLTypes[c.Type]
			if c.NotNull {
				def += " NOT NULL"
			}
			columns = append(columns, def)
		}
		pk := []string{}
		for _, c := range table.PrimaryKey {
			pk = append(pk, pgx.Identifier{c}.Sanitize())
		}
		columns = append(columns, "PRIMARY KEY ("+strings.Join(pk, ",")+")")
		if _, err = tx.Exec(ctx, "CREATE TABLE "+pgx.Identifier{schemaName, table.Name}.Sanitize()+" ("+strings.Join(columns, ",")+")"); err != nil {
			return err
		}
	}
	counts, err := restoreKnowledgeRows(ctx, tx, schemaName, paths["knowledge-postgres.jsonl.gz"])
	if err != nil {
		return err
	}
	for _, table := range knowledgeTables {
		expected := knowledgeTableCount(m, table)
		if counts[table] != expected {
			return fmt.Errorf("restored knowledge count mismatch: %s", table)
		}
	}
	chunk := pgx.Identifier{schemaName, "ai_knowledge_chunk"}.Sanitize()
	for _, fk := range []struct{ column, table string }{{"document_id", "ai_knowledge_doc"}, {"version_id", "ai_knowledge_index_version"}} {
		if _, err = tx.Exec(ctx, "ALTER TABLE "+chunk+" ADD FOREIGN KEY ("+pgx.Identifier{fk.column}.Sanitize()+") REFERENCES "+pgx.Identifier{schemaName, fk.table}.Sanitize()+" (id) ON DELETE CASCADE"); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, "CREATE UNIQUE INDEX ON "+pgx.Identifier{schemaName, "ai_knowledge_index_version"}.Sanitize()+" ((status)) WHERE status='active'"); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, "SELECT id,dimensions FROM "+pgx.Identifier{schemaName, "ai_knowledge_index_version"}.Sanitize())
	if err != nil {
		return err
	}
	type version struct {
		id        string
		dimension int
	}
	versions := []version{}
	for rows.Next() {
		var v version
		if err = rows.Scan(&v.id, &v.dimension); err != nil {
			rows.Close()
			return err
		}
		if v.dimension < 1 || v.dimension > 2000 {
			rows.Close()
			return errors.New("restored knowledge dimensions are invalid")
		}
		versions = append(versions, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, v := range versions {
		// Version ids are bound as SQL string literals; no artifact SQL is run.
		lit := "'" + strings.ReplaceAll(v.id, "'", "''") + "'"
		if _, err = tx.Exec(ctx, fmt.Sprintf("CREATE INDEX ON %s USING hnsw ((embedding::public.vector(%d)) public.vector_cosine_ops) WHERE version_id=%s", chunk, v.dimension, lit)); err != nil {
			return err
		}
	}
	objectCount, err := s.restoreKnowledgeObjects(ctx, tx, schemaName, paths["knowledge-objects.tar.gz"], stage, res.RestoreID)
	if err != nil {
		return err
	}
	if objectCount != componentCount(m, "knowledgeObjects", "objects") {
		return errors.New("knowledge original count mismatch")
	}
	harnessDir := filepath.Join(s.cfg.RestoreHarnessDir, res.RestoreID)
	files, bytes, err := restoreHarnessArchive(ctx, paths["harness-data.tar.gz"], harnessDir)
	if err != nil {
		return err
	}
	if files != componentCount(m, "harness", "files") || bytes != componentCount(m, "harness", "bytes") {
		return errors.New("persistent Agent restore count mismatch")
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	res.Components["knowledge"] = map[string]any{"status": "restored", "schema": schemaName, "tables": counts, "matches": true}
	res.Components["knowledgeObjects"] = map[string]any{"status": "restored", "objects": objectCount, "matches": true}
	res.Components["harness"] = map[string]any{"status": "restored", "files": files, "bytes": bytes, "instances": componentCount(m, "harness", "instances"), "matches": true}
	return nil
}

func validateKnowledgeSchema(schema knowledgeSchema) error {
	if len(schema.Tables) != len(knowledgeTables) {
		return errors.New("knowledge schema is incomplete")
	}
	seen := map[string]bool{}
	for _, t := range schema.Tables {
		allowed := false
		for _, name := range knowledgeTables {
			if t.Name == name {
				allowed = true
			}
		}
		if !allowed || seen[t.Name] || len(t.Columns) == 0 || len(t.PrimaryKey) == 0 {
			return errors.New("invalid knowledge table schema")
		}
		seen[t.Name] = true
		cols := map[string]bool{}
		for _, c := range t.Columns {
			if !backupIdentifier.MatchString(c.Name) || knowledgeSQLTypes[c.Type] == "" || cols[c.Name] {
				return errors.New("invalid knowledge column schema")
			}
			cols[c.Name] = true
		}
		for _, key := range t.PrimaryKey {
			if !cols[key] {
				return errors.New("invalid knowledge primary key")
			}
		}
	}
	return nil
}

func restoreKnowledgeRows(ctx context.Context, tx pgx.Tx, schema, path string) (map[string]int64, error) {
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
	for _, table := range knowledgeTables {
		counts[table] = 0
	}
	dec := json.NewDecoder(gz)
	for {
		var rec knowledgeRow
		err = dec.Decode(&rec)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if _, ok := counts[rec.Table]; !ok || !json.Valid(rec.Row) {
			return nil, errors.New("invalid knowledge backup record")
		}
		table := pgx.Identifier{schema, rec.Table}.Sanitize()
		if _, err = tx.Exec(ctx, "INSERT INTO "+table+" SELECT * FROM jsonb_populate_record(NULL::"+table+",$1::jsonb)", []byte(rec.Row)); err != nil {
			return nil, err
		}
		counts[rec.Table]++
	}
	return counts, nil
}

func (s *Service) downloadVerifiedArtifact(ctx context.Context, m Manifest, id, name, dir string) (string, error) {
	var a *Artifact
	for i := range m.Artifacts {
		if m.Artifacts[i].Filename == name {
			a = &m.Artifacts[i]
		}
	}
	if a == nil || a.ObjectKey != "backup/"+id+"/"+name || a.Size < 0 || len(a.SHA256) != 64 {
		return "", fmt.Errorf("backup artifact missing or invalid: %s", name)
	}
	obj, err := s.store.GetObject(ctx, s.cfg.BackupBucket, a.ObjectKey, minio.GetObjectOptions{})
	if err != nil {
		return "", err
	}
	defer obj.Close()
	p := filepath.Join(dir, name)
	f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), obj)
	closeErr := f.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	if n != a.Size || hex.EncodeToString(h.Sum(nil)) != a.SHA256 {
		return "", fmt.Errorf("checksum mismatch: %s", name)
	}
	return p, nil
}

func componentCount(m Manifest, name, key string) int64 {
	v, _ := m.Components[name].(map[string]any)
	return manifestRecords(map[string]any{"records": v[key]})
}
func knowledgeTableCount(m Manifest, table string) int64 {
	v, _ := m.Components["knowledge"].(map[string]any)
	tables, _ := v["tables"].(map[string]any)
	return manifestRecords(map[string]any{"records": tables[table]})
}

func (s *Service) restoreKnowledgeObjects(ctx context.Context, tx pgx.Tx, schema, path, stage, restoreID string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return 0, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	entries := map[string]string{}
	var refs []knowledgeObject
	indexFound := false
	for {
		header, e := tr.Next()
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			return 0, e
		}
		if header.Typeflag != tar.TypeReg || header.Size < 0 {
			return 0, errors.New("invalid knowledge object archive")
		}
		if header.Name == "index.json" {
			if indexFound || header.Size > 32<<20 {
				return 0, errors.New("invalid knowledge object index")
			}
			indexFound = true
			if e = json.NewDecoder(io.LimitReader(tr, header.Size)).Decode(&refs); e != nil {
				return 0, e
			}
			continue
		}
		if !regexpObjectEntry(header.Name) || entries[header.Name] != "" {
			return 0, errors.New("invalid knowledge object entry")
		}
		p := filepath.Join(stage, strings.ReplaceAll(header.Name, "/", "_"))
		o, e := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return 0, e
		}
		n, e := io.Copy(o, tr)
		o.Close()
		if e != nil {
			return 0, e
		}
		if n != header.Size {
			return 0, errors.New("truncated knowledge original")
		}
		entries[header.Name] = p
	}
	if !indexFound || len(entries) != len(refs) {
		return 0, errors.New("knowledge object index mismatch")
	}
	if len(refs) == 0 {
		return 0, nil
	}
	if strings.TrimSpace(s.cfg.RestoreMinIOEndpoint) == "" {
		return 0, errors.New("IOT_BACKUP_RESTORE_MINIO_ENDPOINT is required for knowledge original restore")
	}
	if strings.EqualFold(strings.TrimRight(s.cfg.MinIOEndpoint, "/"), strings.TrimRight(s.cfg.RestoreMinIOEndpoint, "/")) {
		return 0, errors.New("knowledge originals must restore to the independent MinIO DR endpoint")
	}
	dr, err := minio.New(s.cfg.RestoreMinIOEndpoint, &minio.Options{Creds: credentials.NewStaticV4(s.cfg.RestoreMinIOAccessKey, s.cfg.RestoreMinIOSecretKey, ""), Secure: s.cfg.RestoreMinIOUseTLS})
	if err != nil {
		return 0, err
	}
	const bucket = "iot-restored-knowledge"
	if err = s.ensureBucket(ctx, dr, bucket); err != nil {
		return 0, err
	}
	seen := map[string]bool{}
	for _, ref := range refs {
		p := entries[ref.Entry]
		if p == "" || seen[ref.Entry] {
			return 0, errors.New("knowledge object reference mismatch")
		}
		seen[ref.Entry] = true
		hash, size, e := hashFile(p)
		if e != nil {
			return 0, e
		}
		if size != ref.Size || hash != ref.SHA256 {
			return 0, errors.New("knowledge original checksum mismatch")
		}
		key := "restore/" + restoreID + "/" + ref.Entry
		if _, e = dr.FPutObject(ctx, bucket, key, p, minio.PutObjectOptions{ContentType: ref.ContentType}); e != nil {
			return 0, e
		}
		obj, e := dr.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
		if e != nil {
			return 0, e
		}
		h := sha256.New()
		n, e := io.Copy(h, obj)
		obj.Close()
		if e != nil {
			return 0, e
		}
		if n != ref.Size || hex.EncodeToString(h.Sum(nil)) != ref.SHA256 {
			return 0, errors.New("restored original verification failed")
		}
		command, e := tx.Exec(ctx, "UPDATE "+pgx.Identifier{schema, "ai_knowledge_doc"}.Sanitize()+" SET object_bucket=$1,object_key=$2 WHERE object_bucket=$3 AND object_key=$4", bucket, key, ref.Bucket, ref.Key)
		if e != nil {
			return 0, e
		}
		if command.RowsAffected() == 0 {
			return 0, errors.New("knowledge original has no restored document reference")
		}
	}
	return int64(len(refs)), nil
}

func regexpObjectEntry(name string) bool {
	if !strings.HasPrefix(name, "objects/") || len(name) != len("objects/")+12 {
		return false
	}
	for _, c := range name[len("objects/"):] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func harnessRestoreTargetSafe(source, target string) error {
	if source == "" {
		return nil
	}
	a, err := resolvedBackupPath(source)
	if err != nil {
		return err
	}
	b, err := resolvedBackupPath(target)
	if err != nil {
		return err
	}
	for _, pair := range [][2]string{{a, b}, {b, a}} {
		r, e := filepath.Rel(pair[0], pair[1])
		if e == nil && (r == "." || r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator))) {
			return errors.New("Harness restore directory must be separate from the running Agent data directory")
		}
	}
	return nil
}

// Resolve the nearest existing ancestor as well: a new destination under a
// symlink must not bypass the separation check merely because it does not exist.
func resolvedBackupPath(path string) (string, error) {
	p, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	var suffix []string
	for {
		resolved, e := filepath.EvalSymlinks(p)
		if e == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return resolved, nil
		}
		if !errors.Is(e, os.ErrNotExist) {
			return "", e
		}
		parent := filepath.Dir(p)
		if parent == p {
			return "", e
		}
		suffix = append(suffix, filepath.Base(p))
		p = parent
	}
}

func restoreHarnessArchive(ctx context.Context, path, target string) (files, bytes int64, err error) {
	if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return
	}
	if err = os.Mkdir(target, 0700); err != nil {
		return
	}
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		header, e := tr.Next()
		if errors.Is(e, io.EOF) {
			return files, bytes, nil
		}
		if e != nil {
			return files, bytes, e
		}
		if e = ctx.Err(); e != nil {
			return files, bytes, e
		}
		if header.Name == "" || strings.ContainsAny(header.Name, "\\\x00") || filepath.IsAbs(header.Name) {
			return files, bytes, errors.New("invalid persistent Agent archive path")
		}
		clean := filepath.Clean(filepath.FromSlash(header.Name))
		if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return files, bytes, errors.New("persistent Agent archive path escapes restore directory")
		}
		p := filepath.Join(target, clean)
		switch header.Typeflag {
		case tar.TypeDir:
			if e = os.MkdirAll(p, 0700); e != nil {
				return files, bytes, e
			}
		case tar.TypeReg:
			if header.Size < 0 {
				return files, bytes, errors.New("invalid persistent Agent file size")
			}
			if e = os.MkdirAll(filepath.Dir(p), 0700); e != nil {
				return files, bytes, e
			}
			o, e := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, os.FileMode(header.Mode)&0700)
			if e != nil {
				return files, bytes, e
			}
			n, e := io.Copy(o, tr)
			o.Close()
			if e != nil {
				return files, bytes, e
			}
			if n != header.Size {
				return files, bytes, errors.New("truncated persistent Agent file")
			}
			files++
			bytes += n
		default:
			return files, bytes, errors.New("persistent Agent archive contains a symlink or non-regular file")
		}
	}
}
