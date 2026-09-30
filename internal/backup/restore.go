package backup

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/minio/minio-go/v7"
)

// ErrRestoreTargetUnsafe refuses restores that would write into the live
// business database: a restore check must never overwrite what it verifies.
var ErrRestoreTargetUnsafe = errors.New("restore target must be a separate database from the platform database")

// ErrRestoreNotConfigured means IOT_BACKUP_RESTORE_TARGET_DSN is unset.
var ErrRestoreNotConfigured = errors.New("IOT_BACKUP_RESTORE_TARGET_DSN is not configured")

// restoreTargetSafe compares the resolved host set, port and database name.
func restoreTargetSafe(source, target string) error {
	if strings.TrimSpace(target) == "" {
		return ErrRestoreNotConfigured
	}
	sc, err := pgx.ParseConfig(source)
	if err != nil {
		return errors.New("platform database configuration is invalid")
	}
	tc, err := pgx.ParseConfig(target)
	if err != nil {
		return errors.New("restore target DSN is invalid")
	}
	hosts := func(c *pgx.ConnConfig) map[string]bool {
		out := map[string]bool{fmt.Sprintf("%s:%d", c.Host, c.Port): true}
		for _, f := range c.Fallbacks {
			out[fmt.Sprintf("%s:%d", f.Host, f.Port)] = true
		}
		return out
	}
	if sc.Database == tc.Database {
		for h := range hosts(tc) {
			if hosts(sc)[h] {
				return ErrRestoreTargetUnsafe
			}
		}
	}
	return nil
}

// recordMessageID reads the message identity of a backup record. PostgreSQL
// rows and ClickHouse raw payloads are platform JSON (messageId); ClickHouse
// telemetry rows use message_id.
func recordMessageID(message json.RawMessage) string {
	var v struct {
		MessageID  string `json:"messageId"`
		MessageID2 string `json:"message_id"`
	}
	_ = json.Unmarshal(message, &v)
	if v.MessageID != "" {
		return v.MessageID
	}
	return v.MessageID2
}

// RestoreResult summarises a restore into the independent target.
type RestoreResult struct {
	RestoreID  string                    `json:"restoreId"`
	BackupID   string                    `json:"backupId"`
	Status     string                    `json:"status"`
	Kinds      map[string]RestoreSummary `json:"kinds"`
	Error      string                    `json:"error,omitempty"`
	Components map[string]any            `json:"components,omitempty"`
}

type RestoreSummary struct {
	Expected int64 `json:"expected"`
	Restored int64 `json:"restored"`
	Distinct int64 `json:"distinctMessages"`
	Matches  bool  `json:"matches"`
}

const restoreSchema = `
CREATE TABLE IF NOT EXISTS restored_message (
  restore_id text NOT NULL, kind text NOT NULL, storage text NOT NULL,
  message_id text NOT NULL, body jsonb NOT NULL
);
CREATE INDEX IF NOT EXISTS restored_message_run_idx ON restored_message(restore_id, kind);
CREATE TABLE IF NOT EXISTS restore_run (
  id text PRIMARY KEY, backup_id text NOT NULL, restored_at timestamptz NOT NULL DEFAULT now(), result jsonb NOT NULL
);`

// Restore loads a device-data backup into the configured independent
// database and checks record and distinct-message counts against the
// manifest. It proves the backup can be read back into PostgreSQL; it does
// not replace the live platform database.
func (s *Service) Restore(ctx context.Context, backupID string) (RestoreResult, error) {
	res := RestoreResult{BackupID: backupID, Kinds: map[string]RestoreSummary{}, Components: map[string]any{}}
	if err := validateSegment(backupID, "backup id"); err != nil {
		return res, err
	}
	if err := restoreTargetSafe(s.cfg.PostgresDSN, s.cfg.RestoreTargetDSN); err != nil {
		return res, err
	}
	if !s.mu.TryLock() {
		return res, fmt.Errorf("a backup or restore is already running")
	}
	defer s.mu.Unlock()
	res.RestoreID = "restore_" + time.Now().UTC().Format("20060102T150405.000Z")
	_, _ = s.pool.Exec(ctx, `INSERT INTO backup_task(id,backup_type,status,started_at) VALUES($1,'RESTORE','RUNNING',now())`, res.RestoreID)
	err := s.restore(ctx, &res)
	if res.Status != "PARTIAL" {
		res.Status = "COMPLETED"
	}
	for _, k := range res.Kinds {
		if !k.Matches {
			res.Status = "MISMATCH"
		}
	}
	if err != nil {
		res.Status, res.Error = "FAILED", err.Error()
	}
	details, _ := json.Marshal(res)
	status := map[string]string{"COMPLETED": "COMPLETED", "PARTIAL": "COMPLETED"}[res.Status]
	if status == "" {
		status = "FAILED"
	}
	_, _ = s.pool.Exec(context.WithoutCancel(ctx), `UPDATE backup_task SET status=$2,details=$3,completed_at=now() WHERE id=$1`, res.RestoreID, status, details)
	if err == nil && res.Status == "MISMATCH" {
		err = errors.New("restored record counts differ from the backup manifest")
	}
	return res, err
}

func (s *Service) restore(ctx context.Context, res *RestoreResult) error {
	object, err := s.store.GetObject(ctx, s.cfg.BackupBucket, "backup/"+res.BackupID+"/manifest.json", minio.GetObjectOptions{})
	if err != nil {
		return err
	}
	var manifest Manifest
	err = json.NewDecoder(object).Decode(&manifest)
	object.Close()
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	if manifest.FormatVersion > 4 {
		return fmt.Errorf("unsupported backup manifest version %d", manifest.FormatVersion)
	}
	if manifest.ID != res.BackupID {
		return errors.New("backup manifest identity mismatch")
	}
	if err = os.MkdirAll(s.cfg.BackupDir, 0700); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(s.cfg.BackupDir, ".message-restore-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	target, err := pgx.Connect(ctx, s.cfg.RestoreTargetDSN)
	if err != nil {
		return errors.New("restore target database is unavailable")
	}
	defer target.Close(context.WithoutCancel(ctx))
	if err = restoreConnectedTargetSafe(ctx, s.pool, target); err != nil {
		return err
	}
	if _, err = target.Exec(ctx, restoreSchema); err != nil {
		return err
	}
	kinds := map[string]string{"raw-messages.jsonl.gz": "rawMessages", "parsed-messages.jsonl.gz": "parsedMessages"}
	names := make([]string, 0, len(kinds))
	for n := range kinds {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, filename := range names {
		kind := kinds[filename]
		var artifact *Artifact
		for i := range manifest.Artifacts {
			if manifest.Artifacts[i].Filename == filename {
				artifact = &manifest.Artifacts[i]
			}
		}
		if artifact == nil {
			return fmt.Errorf("backup has no %s", filename)
		}
		summary := RestoreSummary{Expected: manifestRecords(manifest.Components[kind])}
		file, err := s.downloadVerifiedArtifact(ctx, manifest, res.BackupID, filename, stage)
		if err != nil {
			return err
		}
		obj, err := os.Open(file)
		if err != nil {
			return err
		}
		n, err := copyRecords(ctx, target, res.RestoreID, kind, obj)
		obj.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", filename, err)
		}
		summary.Restored = n
		if err = target.QueryRow(ctx, `SELECT count(DISTINCT message_id) FROM restored_message WHERE restore_id=$1 AND kind=$2`, res.RestoreID, kind).Scan(&summary.Distinct); err != nil {
			return err
		}
		// A backup may hold one message in both stores (PostgreSQL and
		// ClickHouse representations); distinct IDs never exceed records.
		summary.Matches = summary.Restored == summary.Expected && (summary.Distinct > 0) == (summary.Expected > 0) && summary.Distinct <= summary.Restored
		res.Kinds[kind] = summary
	}
	if err = s.restoreKnowledgeAndAgents(ctx, target, manifest, res); err != nil {
		return err
	}
	if err = s.restoreDuty(ctx, target, manifest, res); err != nil {
		return err
	}
	if err = s.restoreApplication(ctx, target, manifest, res); err != nil {
		return err
	}
	if res.Status != "PARTIAL" {
		res.Status = "COMPLETED"
	}
	for _, summary := range res.Kinds {
		if !summary.Matches {
			res.Status = "MISMATCH"
		}
	}
	result, _ := json.Marshal(res)
	_, err = target.Exec(ctx, `INSERT INTO restore_run(id,backup_id,result) VALUES($1,$2,$3)`, res.RestoreID, res.BackupID, result)
	return err
}

func manifestRecords(v any) int64 {
	m, _ := v.(map[string]any)
	switch n := m["records"].(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	}
	return 0
}

// copyRecords streams gzip JSONL backup records into the target with COPY.
func copyRecords(ctx context.Context, target *pgx.Conn, restoreID, kind string, r io.Reader) (int64, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return 0, err
	}
	defer gz.Close()
	dec := json.NewDecoder(gz)
	var total int64
	batch := make([][]any, 0, 1000)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		_, err := target.CopyFrom(ctx, pgx.Identifier{"restored_message"}, []string{"restore_id", "kind", "storage", "message_id", "body"}, pgx.CopyFromRows(batch))
		batch = batch[:0]
		return err
	}
	for {
		var rec rawLogRecord
		if err = dec.Decode(&rec); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return total, fmt.Errorf("corrupt record %d: %w", total+1, err)
		}
		batch = append(batch, []any{restoreID, kind, rec.Storage, recordMessageID(rec.Message), []byte(rec.Message)})
		total++
		if len(batch) == cap(batch) {
			if err = flush(); err != nil {
				return total, err
			}
		}
	}
	return total, flush()
}
