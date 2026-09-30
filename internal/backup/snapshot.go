package backup

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/minio/minio-go/v7"
	"io"
	"slices"
	"strings"
)

// Two independently shipped v4 formats used separate component names. v5
// requires both components, while a v4 package may legitimately contain only
// one. A partial component is always corrupt, never silently downgraded.
func snapshotComponentIncluded(m Manifest, component string, artifacts []string) (bool, error) {
	entry, _ := m.Components[component].(map[string]any)
	included := entry["status"] == "included"
	found := map[string]bool{}
	for _, artifact := range m.Artifacts {
		if !slices.Contains(artifacts, artifact.Filename) {
			continue
		}
		if found[artifact.Filename] {
			return false, fmt.Errorf("duplicate %s backup artifact", component)
		}
		found[artifact.Filename] = true
	}
	if !included && len(found) == 0 && m.FormatVersion < 5 {
		return false, nil
	}
	if !included || len(found) != len(artifacts) {
		return false, fmt.Errorf("%s backup component incomplete", component)
	}
	return true, nil
}

func exportTableSnapshot(ctx context.Context, tx pgx.Tx, tables []string, dataPath string) (knowledgeSchema, map[string]int64, error) {
	schema := knowledgeSchema{}
	counts := map[string]int64{}
	var err error
	err = writeGzip(dataPath, func(w io.Writer) error {
		enc := json.NewEncoder(w)
		for _, table := range tables {
			t := knowledgeTable{Name: table}
			rows, e := tx.Query(ctx, `SELECT a.attname,t.typname,a.attnotnull FROM pg_attribute a JOIN pg_type t ON t.oid=a.atttypid WHERE a.attrelid=to_regclass($1) AND a.attnum>0 AND NOT a.attisdropped AND a.attgenerated='' ORDER BY a.attnum`, table)
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
					return fmt.Errorf("unsupported snapshot column in %s", table)
				}
				t.Columns = append(t.Columns, c)
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return e
			}
			if len(t.Columns) == 0 {
				return fmt.Errorf("snapshot table %s missing", table)
			}
			if e = tx.QueryRow(ctx, `SELECT array_agg(a.attname ORDER BY k.ordinality) FROM pg_constraint c CROSS JOIN LATERAL unnest(c.conkey) WITH ORDINALITY k(attnum,ordinality) JOIN pg_attribute a ON a.attrelid=c.conrelid AND a.attnum=k.attnum WHERE c.conrelid=to_regclass($1) AND c.contype='p'`, table).Scan(&t.PrimaryKey); e != nil {
				return e
			}
			schema.Tables = append(schema.Tables, t)
			columnNames := []string{}
			for _, col := range t.Columns {
				columnNames = append(columnNames, pgx.Identifier{col.Name}.Sanitize())
			}
			rows, e = tx.Query(ctx, "SELECT to_jsonb(t) FROM (SELECT "+strings.Join(columnNames, ",")+" FROM "+pgx.Identifier{table}.Sanitize()+") t")
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

	return schema, counts, err
}

func (s *Service) exportSnapshotObjects(ctx context.Context, refs []knowledgeObject, objectPath string) (int64, error) {
	var bytes int64
	var err error
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
				return errors.New("snapshot object changed during backup")
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

	return bytes, err
}
