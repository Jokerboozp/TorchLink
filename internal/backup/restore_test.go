package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestRestoreRefusesTheLiveDatabase(t *testing.T) {
	live := "postgres://iot:pw@db1:5432/iot?sslmode=disable"
	for _, target := range []string{live, "postgres://other:pw@db1:5432/iot", "postgres://iot:pw@db2:5432,db1:5432/iot"} {
		if err := restoreTargetSafe(live, target); !errors.Is(err, ErrRestoreTargetUnsafe) {
			t.Fatalf("%s accepted: %v", target, err)
		}
	}
	if err := restoreTargetSafe(live, ""); !errors.Is(err, ErrRestoreNotConfigured) {
		t.Fatal(err)
	}
	for _, target := range []string{"postgres://iot:pw@db1:5432/iot_restore", "postgres://iot:pw@restore-db:5432/iot"} {
		if err := restoreTargetSafe(live, target); err != nil {
			t.Fatalf("%s refused: %v", target, err)
		}
	}
}

func TestPersistentAgentRestoreSeparatesLivePathsAndRejectsArchiveTraversal(t *testing.T) {
	base := t.TempDir()
	live := filepath.Join(base, "live")
	if err := os.Mkdir(live, 0700); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{live, filepath.Join(live, "new"), base} {
		if err := harnessRestoreTargetSafe(live, target); err == nil {
			t.Fatal("accepted a running Harness directory")
		}
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(live, link); err == nil {
		if err = harnessRestoreTargetSafe(live, filepath.Join(link, "new")); err == nil {
			t.Fatal("new path beneath symlink bypassed live-directory boundary")
		}
	}
	if err := harnessRestoreTargetSafe(live, filepath.Join(base, "restore")); err != nil {
		t.Fatal(err)
	}
	for _, header := range []*tar.Header{
		{Name: "../outside", Mode: 0600, Typeflag: tar.TypeReg, Size: 1},
		{Name: "/absolute", Mode: 0600, Typeflag: tar.TypeReg, Size: 1},
		{Name: "plugins/link", Linkname: "/etc", Typeflag: tar.TypeSymlink},
		{Name: "plugins/socket", Typeflag: tar.TypeFifo},
	} {
		archive := filepath.Join(t.TempDir(), "malicious.tar.gz")
		err := writeGzip(archive, func(w io.Writer) error {
			tw := tar.NewWriter(w)
			if e := tw.WriteHeader(header); e != nil {
				return e
			}
			if header.Typeflag == tar.TypeReg {
				if _, e := tw.Write([]byte("x")); e != nil {
					return e
				}
			}
			return tw.Close()
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err = restoreHarnessArchive(context.Background(), archive, filepath.Join(t.TempDir(), "target")); err == nil {
			t.Fatalf("accepted unsafe archive entry %q", header.Name)
		}
	}
}

func TestKnowledgeRestoreNeverExecutesArtifactSQL(t *testing.T) {
	valid := knowledgeSchema{}
	for _, table := range knowledgeTables {
		valid.Tables = append(valid.Tables, knowledgeTable{Name: table, Columns: []knowledgeColumn{{Name: "id", Type: "text", NotNull: true}}, PrimaryKey: []string{"id"}})
	}
	if err := validateKnowledgeSchema(valid); err != nil {
		t.Fatal(err)
	}
	valid.Tables[0].Columns[0].Type = "text); DROP SCHEMA public CASCADE; --"
	if err := validateKnowledgeSchema(valid); err == nil {
		t.Fatal("accepted SQL as a column type")
	}
	valid.Tables[0].Columns[0].Type = "text"
	valid.Tables[0].Name = "platform_access"
	if err := validateKnowledgeSchema(valid); err == nil {
		t.Fatal("accepted table outside knowledge scope")
	}
}

func TestRestoreReadsEveryRecordIdentity(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	enc := json.NewEncoder(gz)
	for _, rec := range []rawLogRecord{
		{Storage: "postgres", Message: json.RawMessage(`{"messageId":"raw-1"}`)},
		{Storage: "clickhouse", Message: json.RawMessage(`{"message_id":"std-1","ts":"x"}`)},
	} {
		_ = enc.Encode(rec)
	}
	_ = gz.Close()
	r, _ := gzip.NewReader(&buf)
	dec := json.NewDecoder(r)
	var ids []string
	for dec.More() {
		var rec rawLogRecord
		if err := dec.Decode(&rec); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, recordMessageID(rec.Message))
	}
	if len(ids) != 2 || ids[0] != "raw-1" || ids[1] != "std-1" {
		t.Fatal(ids)
	}
	if manifestRecords(map[string]any{"records": float64(42)}) != 42 || manifestRecords(nil) != 0 {
		t.Fatal("manifest records")
	}
}
