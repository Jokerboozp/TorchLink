package backup

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestDutyObjectArchiveRejectsMismatchAndUnsafeEntries(t *testing.T) {
	for _, tc := range []struct {
		name, entry string
		refs        []knowledgeObject
	}{{"unsafe", "../escape", nil}, {"missing-index", "objects/000000000000", nil}, {"wrong-hash", "objects/000000000000", []knowledgeObject{{Bucket: dutyAttachmentBucket, Key: "photo", Entry: "objects/000000000000", Size: 4, SHA256: "wrong"}}}} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "objects.tar.gz")
			if err := writeGzip(path, func(w io.Writer) error {
				tw := tar.NewWriter(w)
				if err := tw.WriteHeader(&tar.Header{Name: tc.entry, Mode: 0600, Size: 4, Typeflag: tar.TypeReg}); err != nil {
					return err
				}
				if _, err := tw.Write([]byte("test")); err != nil {
					return err
				}
				if tc.refs != nil {
					b, _ := json.Marshal(tc.refs)
					if err := tw.WriteHeader(&tar.Header{Name: "index.json", Mode: 0600, Size: int64(len(b)), Typeflag: tar.TypeReg}); err != nil {
						return err
					}
					if _, err := tw.Write(b); err != nil {
						return err
					}
				}
				return tw.Close()
			}); err != nil {
				t.Fatal(err)
			}
			if _, _, err := readDutyObjects(path, dir); err == nil {
				t.Fatal("invalid archive accepted")
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "escape")); err == nil {
				t.Fatal("escaped extraction")
			}
		})
	}
}
func TestDutyObjectReferencesRewrittenWithoutChangingUnrelatedText(t *testing.T) {
	b := []byte(`{"attachment":{"objectKey":"old"},"records":[{"body":{"attachments":[{"objectKey":"old"}]}}],"humanNotes":"old","other":"old","largeCounter":9007199254740993}`)
	out, changed, err := rewriteDutyObjectKeys(b, map[string]string{"old": "restore/new"})
	if err != nil || !changed {
		t.Fatal(err)
	}
	if !bytes.Contains(out, []byte("9007199254740993")) {
		t.Fatal("historical numeric precision changed", string(out))
	}
	var v map[string]any
	if err = json.Unmarshal(out, &v); err != nil {
		t.Fatal(err)
	}
	if v["humanNotes"] != "old" || v["other"] != "old" || v["attachment"].(map[string]any)["objectKey"] != "restore/new" {
		t.Fatal(string(out))
	}
	nested := v["records"].([]any)[0].(map[string]any)["body"].(map[string]any)["attachments"].([]any)[0].(map[string]any)
	if nested["objectKey"] != "restore/new" {
		t.Fatal("nested revision reference lost")
	}
}
func TestDutySchemaRejectsAddedExecutableOrForeignColumns(t *testing.T) {
	schema := knowledgeSchema{}
	for _, name := range dutyTables {
		cols := []knowledgeColumn{{Name: "tenant_id", Type: "text", NotNull: true}, {Name: "id", Type: "text", NotNull: true}, {Name: "version", Type: "int8", NotNull: true}, {Name: "created_at", Type: "int8", NotNull: true}, {Name: "updated_at", Type: "int8", NotNull: true}, {Name: "body", Type: "jsonb", NotNull: true}}
		pk := []string{"tenant_id", "id"}
		if name == "duty_business_event" {
			cols = []knowledgeColumn{}
			for _, name := range []string{"seq", "tenant_id", "id", "event_type", "station_id", "run_id", "device_id", "actor_id", "occurred_at", "recorded_at", "body"} {
				typ := "text"
				if name == "seq" || name == "occurred_at" || name == "recorded_at" {
					typ = "int8"
				}
				if name == "body" {
					typ = "jsonb"
				}
				cols = append(cols, knowledgeColumn{Name: name, Type: typ, NotNull: true})
			}
			pk = []string{"seq"}
		}
		schema.Tables = append(schema.Tables, knowledgeTable{Name: name, Columns: cols, PrimaryKey: pk})
	}
	if err := validateDutySchema(schema); err != nil {
		t.Fatal(err)
	}
	schema.Tables[0].Columns[0].Name = "bad;drop table users"
	if err := validateDutySchema(schema); err == nil {
		t.Fatal("unsafe schema accepted")
	}
}
