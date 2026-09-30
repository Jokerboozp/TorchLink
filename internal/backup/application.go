package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/jackc/pgx/v5"
)

// Shared by application and duty artifacts. Data, schema and primary keys all
// come from the same repeatable-read transaction, with supported types only.
func exportDataTables(ctx context.Context, tx pgx.Tx, tables []string, path string) (schema knowledgeSchema, counts map[string]int64, err error) {
	counts = map[string]int64{}
	err = writeGzip(path, func(w io.Writer) error {
		encoder := json.NewEncoder(w)
		for _, name := range tables {
			table := knowledgeTable{Name: name}
			rows, e := tx.Query(ctx, `SELECT a.attname,t.typname,a.attnotnull FROM pg_attribute a JOIN pg_type t ON t.oid=a.atttypid WHERE a.attrelid=to_regclass($1) AND a.attnum>0 AND NOT a.attisdropped ORDER BY a.attnum`, name)
			if e != nil {
				return e
			}
			for rows.Next() {
				var column knowledgeColumn
				if e = rows.Scan(&column.Name, &column.Type, &column.NotNull); e != nil {
					rows.Close()
					return e
				}
				if !backupIdentifier.MatchString(column.Name) || knowledgeSQLTypes[column.Type] == "" {
					rows.Close()
					return errors.New("unsupported backup column")
				}
				table.Columns = append(table.Columns, column)
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return e
			}
			if len(table.Columns) == 0 {
				return fmt.Errorf("backup table missing: %s", name)
			}
			if e = tx.QueryRow(ctx, `SELECT array_agg(a.attname ORDER BY k.ordinality) FROM pg_constraint c CROSS JOIN LATERAL unnest(c.conkey) WITH ORDINALITY k(attnum,ordinality) JOIN pg_attribute a ON a.attrelid=c.conrelid AND a.attnum=k.attnum WHERE c.conrelid=to_regclass($1) AND c.contype='p'`, name).Scan(&table.PrimaryKey); e != nil {
				return e
			}
			schema.Tables = append(schema.Tables, table)
			// Stable primary-key order makes the artifact audit deterministic.
			order := ""
			for _, column := range table.PrimaryKey {
				if order != "" {
					order += ","
				}
				order += pgx.Identifier{column}.Sanitize()
			}
			rows, e = tx.Query(ctx, "SELECT to_jsonb(t) FROM "+pgx.Identifier{name}.Sanitize()+" t ORDER BY "+order)
			if e != nil {
				return e
			}
			counts[name] = 0
			for rows.Next() {
				var body []byte
				if e = rows.Scan(&body); e == nil {
					e = encoder.Encode(knowledgeRow{Table: name, Row: body})
				}
				if e != nil {
					rows.Close()
					return e
				}
				counts[name]++
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return e
			}
		}
		return nil
	})
	return
}

func (s *Service) exportApplication(ctx context.Context, dir string, manifest *Manifest) ([]string, error) {
	tx, err := s.beginBackupRead(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	schemaPath, dataPath, objectPath := filepath.Join(dir, "application-schema.json"), filepath.Join(dir, "application-postgres.jsonl.gz"), filepath.Join(dir, "application-objects.tar.gz")
	schema, counts, err := exportDataTables(ctx, tx, applicationTables(), dataPath)
	if err != nil {
		return nil, err
	}
	if err = validateApplicationSchema(schema); err != nil {
		return nil, err
	}
	if err = writeJSON(schemaPath, schema); err != nil {
		return nil, err
	}
	objects, bytes, unknownHashes, err := s.exportApplicationObjects(ctx, tx, objectPath)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	manifest.Components["application"] = map[string]any{"status": "included", "tables": counts, "scope": "immutable analysis/configuration, permissions, rule history/traces and retained PostgreSQL read sources"}
	manifest.Components["applicationObjects"] = map[string]any{"status": "included", "objects": objects, "bytes": bytes, "originalHashUnknown": unknownHashes}
	return []string{schemaPath, dataPath, objectPath}, nil
}
