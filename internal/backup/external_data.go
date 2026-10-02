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
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Preserve database column names, including the worker owner which is omitted
// from public API JSON. Credentials in body remain encrypted by the service.
type externalDataBackupRow struct {
	TenantID   string          `json:"tenant_id"`
	Kind       string          `json:"kind"`
	ID         string          `json:"id"`
	SourceID   string          `json:"source_id"`
	EndpointID string          `json:"endpoint_id"`
	Status     string          `json:"status"`
	DueAt      int64           `json:"due_at"`
	LeaseUntil int64           `json:"lease_until"`
	Owner      string          `json:"owner"`
	Revision   int64           `json:"revision"`
	CreatedAt  int64           `json:"created_at"`
	UpdatedAt  int64           `json:"updated_at"`
	Body       json.RawMessage `json:"body"`
}

const externalDataTableDefinition = ` (
 tenant_id text NOT NULL, kind text NOT NULL, id text NOT NULL,
 source_id text NOT NULL DEFAULT '', endpoint_id text NOT NULL DEFAULT '',
 status text NOT NULL DEFAULT '', due_at bigint NOT NULL DEFAULT 0,
 lease_until bigint NOT NULL DEFAULT 0, owner text NOT NULL DEFAULT '',
 revision bigint NOT NULL CHECK (revision > 0),
 created_at bigint NOT NULL, updated_at bigint NOT NULL, body jsonb NOT NULL,
 PRIMARY KEY (tenant_id,kind,id)
)`

func (s *Service) exportExternalData(ctx context.Context, dir string, m *Manifest) (string, error) {
	path := filepath.Join(dir, "external-data.jsonl.gz")
	var count int64
	err := writeGzip(path, func(w io.Writer) error {
		// One cursor reads a consistent statement snapshot, without loading all
		// raw receipts into memory or exposing encrypted configuration in logs.
		rows, err := s.pool.Query(ctx, `SELECT to_jsonb(e) FROM external_data_entry e ORDER BY tenant_id,kind,id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		enc := json.NewEncoder(w)
		for rows.Next() {
			var row json.RawMessage
			if err = rows.Scan(&row); err != nil {
				return err
			}
			if err = enc.Encode(row); err != nil {
				return err
			}
			count++
		}
		return rows.Err()
	})
	if err != nil {
		return "", fmt.Errorf("external data backup: %w", err)
	}
	m.Components["externalData"] = map[string]any{"status": "included", "records": count}
	return path, nil
}

func (s *Service) restoreExternalData(ctx context.Context, target *pgx.Conn, m Manifest, res *RestoreResult, stage string) error {
	v, present := m.Components["externalData"]
	if !present {
		res.Components["externalData"] = map[string]any{"status": "not_included"}
		return nil
	}
	component, ok := v.(map[string]any)
	if !ok || component["status"] != "included" {
		return errors.New("invalid external data backup component")
	}
	path, err := s.downloadVerifiedArtifact(ctx, m, res.BackupID, "external-data.jsonl.gz", stage)
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(res.RestoreID))
	schemaName := "external_restore_" + hex.EncodeToString(sum[:10])
	tx, err := target.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schemaName}.Sanitize()); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "CREATE TABLE "+pgx.Identifier{schemaName, "external_data_entry"}.Sanitize()+externalDataTableDefinition); err != nil {
		return err
	}
	count, err := restoreExternalDataRows(ctx, tx, schemaName, path)
	if err != nil {
		return err
	}
	if count != manifestRecords(component) {
		return errors.New("restored external data count differs from the backup manifest")
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	res.Components["externalData"] = map[string]any{"status": "restored", "schema": schemaName, "records": count, "matches": true}
	return nil
}

func restoreExternalDataRows(ctx context.Context, tx pgx.Tx, schema, path string) (int64, error) {
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
	dec := json.NewDecoder(gz)
	var count int64
	batch := make([][]any, 0, 500)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		_, err := tx.CopyFrom(ctx, pgx.Identifier{schema, "external_data_entry"}, []string{"tenant_id", "kind", "id", "source_id", "endpoint_id", "status", "due_at", "lease_until", "owner", "revision", "created_at", "updated_at", "body"}, pgx.CopyFromRows(batch))
		batch = batch[:0]
		return err
	}
	for {
		var row externalDataBackupRow
		if err = dec.Decode(&row); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return count, fmt.Errorf("corrupt external data record %d", count+1)
		}
		if strings.TrimSpace(row.TenantID) == "" || strings.TrimSpace(row.Kind) == "" || strings.TrimSpace(row.ID) == "" || row.Revision <= 0 || !json.Valid(row.Body) {
			return count, fmt.Errorf("invalid external data record %d", count+1)
		}
		batch = append(batch, []any{row.TenantID, row.Kind, row.ID, row.SourceID, row.EndpointID, row.Status, row.DueAt, row.LeaseUntil, row.Owner, row.Revision, row.CreatedAt, row.UpdatedAt, []byte(row.Body)})
		count++
		if len(batch) == cap(batch) {
			if err = flush(); err != nil {
				return count, err
			}
		}
	}
	return count, flush()
}
