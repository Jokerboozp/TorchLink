package backup

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/jackc/pgx/v5"
	"github.com/minio/minio-go/v7"
)

const dutyAttachmentBucket = "iot-duty-attachments"

var dutyTables = []string{"duty_receipt", "duty_station", "duty_team", "duty_shift_template", "duty_roster", "duty_run", "duty_record", "duty_item", "duty_item_event", "duty_handover", "duty_handover_revision", "duty_ai_job", "duty_notification", "duty_attachment", "duty_business_event"}

// Duty uses separate artifacts from knowledge, so previous FULL backups retain
// their original schema and restore contract.
func (s *Service) exportDuty(ctx context.Context, dir string, m *Manifest) ([]string, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	schema := knowledgeSchema{}
	counts := map[string]int64{}
	dataPath := filepath.Join(dir, "duty-postgres.jsonl.gz")
	err = writeGzip(dataPath, func(w io.Writer) error {
		enc := json.NewEncoder(w)
		for _, table := range dutyTables {
			t := knowledgeTable{Name: table}
			rows, e := tx.Query(ctx, `SELECT a.attname,t.typname,a.attnotnull FROM pg_attribute a JOIN pg_type t ON t.oid=a.atttypid WHERE a.attrelid=to_regclass($1) AND a.attnum>0 AND NOT a.attisdropped ORDER BY a.attnum`, table)
			if e != nil {
				return e
			}
			for rows.Next() {
				var c knowledgeColumn
				if e = rows.Scan(&c.Name, &c.Type, &c.NotNull); e != nil {
					rows.Close()
					return e
				}
				if !backupIdentifier.MatchString(c.Name) || knowledgeSQLTypes[c.Type] == "" {
					rows.Close()
					return fmt.Errorf("unsupported duty column in %s", table)
				}
				t.Columns = append(t.Columns, c)
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return e
			}
			if len(t.Columns) == 0 {
				return fmt.Errorf("duty table %s missing", table)
			}
			if e = tx.QueryRow(ctx, `SELECT array_agg(a.attname ORDER BY k.ordinality) FROM pg_constraint c CROSS JOIN LATERAL unnest(c.conkey) WITH ORDINALITY k(attnum,ordinality) JOIN pg_attribute a ON a.attrelid=c.conrelid AND a.attnum=k.attnum WHERE c.conrelid=to_regclass($1) AND c.contype='p'`, table).Scan(&t.PrimaryKey); e != nil {
				return e
			}
			schema.Tables = append(schema.Tables, t)
			rows, e = tx.Query(ctx, "SELECT to_jsonb(t) FROM "+pgx.Identifier{table}.Sanitize()+" t")
			if e != nil {
				return e
			}
			counts[table] = 0
			for rows.Next() {
				var b []byte
				if e = rows.Scan(&b); e == nil {
					e = enc.Encode(knowledgeRow{Table: table, Row: b})
				}
				if e != nil {
					rows.Close()
					return e
				}
				counts[table]++
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("duty database: %w", err)
	}
	schemaPath := filepath.Join(dir, "duty-schema.json")
	if err = writeJSON(schemaPath, schema); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT body->'attachment'->>'objectKey' FROM duty_attachment ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	refs := []knowledgeObject{}
	for rows.Next() {
		var key string
		if err = rows.Scan(&key); err != nil {
			rows.Close()
			return nil, err
		}
		if key == "" {
			rows.Close()
			return nil, errors.New("invalid duty attachment reference")
		}
		refs = append(refs, knowledgeObject{Bucket: dutyAttachmentBucket, Key: key})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	objectPath := filepath.Join(dir, "duty-objects.tar.gz")
	var bytes int64
	err = writeGzip(objectPath, func(w io.Writer) error {
		tw := tar.NewWriter(w)
		for i := range refs {
			ref := &refs[i]
			ref.Entry = fmt.Sprintf("objects/%012d", i)
			obj, e := s.store.GetObject(ctx, ref.Bucket, ref.Key, minio.GetObjectOptions{})
			if e != nil {
				return e
			}
			info, e := obj.Stat()
			if e != nil {
				obj.Close()
				return e
			}
			ref.Size = info.Size
			ref.ContentType = info.ContentType
			if e = tw.WriteHeader(&tar.Header{Name: ref.Entry, Mode: 0600, Size: ref.Size, Typeflag: tar.TypeReg}); e != nil {
				obj.Close()
				return e
			}
			h := sha256.New()
			n, e := io.Copy(io.MultiWriter(tw, h), obj)
			obj.Close()
			if e != nil {
				return e
			}
			if n != ref.Size {
				return errors.New("duty attachment changed during backup")
			}
			ref.SHA256 = hex.EncodeToString(h.Sum(nil))
			bytes += n
		}
		b, e := json.Marshal(refs)
		if e != nil {
			return e
		}
		if e = tw.WriteHeader(&tar.Header{Name: "index.json", Mode: 0600, Size: int64(len(b)), Typeflag: tar.TypeReg}); e != nil {
			return e
		}
		if _, e = tw.Write(b); e != nil {
			return e
		}
		return tw.Close()
	})
	if err != nil {
		return nil, fmt.Errorf("duty attachments: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	m.Components["duty"] = map[string]any{"status": "included", "tables": counts}
	m.Components["dutyObjects"] = map[string]any{"status": "included", "objects": len(refs), "bytes": bytes}
	return []string{schemaPath, dataPath, objectPath}, nil
}
