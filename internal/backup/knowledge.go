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
	"regexp"
	"strings"

	"github.com/minio/minio-go/v7"
)

var knowledgeTables = []string{"ai_knowledge_doc", "ai_workflow_knowledge_binding", "ai_knowledge_index_version", "ai_knowledge_chunk"}
var backupIdentifier = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

type knowledgeColumn struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	NotNull bool   `json:"notNull"`
}
type knowledgeTable struct {
	Name       string            `json:"name"`
	Columns    []knowledgeColumn `json:"columns"`
	PrimaryKey []string          `json:"primaryKey"`
}
type knowledgeSchema struct {
	Tables []knowledgeTable `json:"tables"`
}
type knowledgeRow struct {
	Table string          `json:"table"`
	Row   json.RawMessage `json:"row"`
}
type knowledgeObject struct {
	Bucket             string `json:"bucket"`
	Key                string `json:"key"`
	Entry              string `json:"entry"`
	SHA256             string `json:"sha256"`
	Size               int64  `json:"size"`
	ContentType        string `json:"contentType,omitempty"`
	OriginalHashStatus string `json:"originalHashStatus,omitempty"`
}

// Only types used by knowledge tables can be replayed. No SQL, default
// expressions or executable hooks from a backup are evaluated on restore.
var knowledgeSQLTypes = map[string]string{
	"text": "text", "_text": "text[]", "int4": "integer", "int8": "bigint",
	"jsonb": "jsonb", "timestamptz": "timestamptz", "bool": "boolean", "vector": "public.vector",
}

func (s *Service) exportKnowledgeAndAgents(ctx context.Context, dir string, manifest *Manifest) ([]string, error) {
	if strings.TrimSpace(s.cfg.HarnessDataDir) == "" && len(s.cfg.HarnessSnapshotURLs) == 0 {
		manifest.Components["harness"] = map[string]any{"status": "not_configured"}
		return nil, errors.New("FULL backup requires Harness snapshot URLs or a read-only Harness data directory; persistent Agents were not backed up")
	}
	tx, err := s.beginBackupRead(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	dataPath := filepath.Join(dir, "knowledge-postgres.jsonl.gz")
	schema, counts, err := exportDataTables(ctx, tx, knowledgeTables, dataPath)
	if err != nil {
		return nil, fmt.Errorf("knowledge database: %w", err)
	}
	schemaPath := filepath.Join(dir, "knowledge-schema.json")
	if err = writeJSON(schemaPath, schema); err != nil {
		return nil, err
	}
	refs := []knowledgeObject{}
	rows, err := tx.Query(ctx, `SELECT DISTINCT object_bucket,object_key FROM ai_knowledge_doc ORDER BY object_bucket,object_key`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var ref knowledgeObject
		if err = rows.Scan(&ref.Bucket, &ref.Key); err != nil {
			rows.Close()
			return nil, err
		}
		refs = append(refs, ref)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	objectPath := filepath.Join(dir, "knowledge-objects.tar.gz")
	var objectBytes int64
	err = writeGzip(objectPath, func(w io.Writer) error {
		tw := tar.NewWriter(w)
		for i := range refs {
			if err = ctx.Err(); err != nil {
				return err
			}
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
				return errors.New("knowledge original changed during backup")
			}
			ref.SHA256 = hex.EncodeToString(h.Sum(nil))
			objectBytes += n
		}
		index, e := json.Marshal(refs)
		if e != nil {
			return e
		}
		if e = tw.WriteHeader(&tar.Header{Name: "index.json", Mode: 0600, Size: int64(len(index)), Typeflag: tar.TypeReg}); e != nil {
			return e
		}
		if _, e = tw.Write(index); e != nil {
			return e
		}
		return tw.Close()
	})
	if err != nil {
		return nil, fmt.Errorf("knowledge originals: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	harnessPath := filepath.Join(dir, "harness-data.tar.gz")
	files, bytes, instances, err := s.archivePersistentAgents(ctx, harnessPath)
	if err != nil {
		return nil, fmt.Errorf("persistent Agents: %w", err)
	}
	manifest.Components["knowledge"] = map[string]any{"status": "included", "tables": counts}
	manifest.Components["knowledgeObjects"] = map[string]any{"status": "included", "objects": len(refs), "bytes": objectBytes}
	manifest.Components["harness"] = map[string]any{"status": "included", "files": files, "bytes": bytes, "instances": instances}
	return []string{schemaPath, dataPath, objectPath, harnessPath}, nil
}

func writeGzip(path string, write func(io.Writer) error) (err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(f)
	err = write(gz)
	if e := gz.Close(); err == nil {
		err = e
	}
	if e := f.Close(); err == nil {
		err = e
	}
	return err
}

func archiveHarness(ctx context.Context, root, path string) (files, bytes int64, err error) {
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return
	}
	info, err := os.Stat(root)
	if err != nil {
		return
	}
	if !info.IsDir() {
		return 0, 0, errors.New("Harness data path must be a directory")
	}
	err = writeGzip(path, func(w io.Writer) error {
		tw := tar.NewWriter(w)
		err := filepath.WalkDir(root, func(p string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if e = ctx.Err(); e != nil {
				return e
			}
			if p == root {
				return nil
			}
			rel, e := filepath.Rel(root, p)
			if e != nil {
				return e
			}
			rel = filepath.ToSlash(rel)
			if rel != "plugins" && rel != "sessions" && !strings.HasPrefix(rel, "plugins/") && !strings.HasPrefix(rel, "sessions/") {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			info, e := d.Info()
			if e != nil {
				return e
			}
			// Read-only backup must never follow a link outside the volume or
			// copy a Unix socket, FIFO or device into a portable artifact.
			if !info.Mode().IsRegular() && !info.IsDir() {
				return errors.New("Harness data contains a symlink or non-regular file")
			}
			if !info.IsDir() && !validHarnessSnapshotPath(rel) {
				return errors.New("Harness data contains an unsupported Agent or session file")
			}
			header, e := tar.FileInfoHeader(info, "")
			if e != nil {
				return e
			}
			header.Name = filepath.ToSlash(rel)
			if e = tw.WriteHeader(header); e != nil {
				return e
			}
			if info.IsDir() {
				return nil
			}
			f, e := os.Open(p)
			if e != nil {
				return e
			}
			n, e := io.Copy(tw, f)
			f.Close()
			if e != nil {
				return e
			}
			after, e := os.Stat(p)
			if e != nil {
				return e
			}
			if n != info.Size() || !info.ModTime().Equal(after.ModTime()) || info.Size() != after.Size() {
				return errors.New("Harness file changed during backup; retry when the Agent is idle")
			}
			files++
			bytes += n
			return nil
		})
		if err != nil {
			return err
		}
		return tw.Close()
	})
	return
}
