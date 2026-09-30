package analytics

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/model"
)

func TestRestoredObjectMappingPreservesImmutableKeyAndFailsClosed(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sourceBucket, key := "iot-response-attachments", "t/drill/file"
	_, err = archive.PutObject(ctx, sourceBucket, key, strings.NewReader("live"), 4, "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	location := model.AnalysisRestoredObjectLocation{TenantID: "t", RestoreID: "restore-one", SourceBucket: sourceBucket, SourceKey: key, Bucket: "iot-application-restore-test", Key: key, SHA256: strings.Repeat("a", 64), Size: 8}
	_, err = archive.PutObject(ctx, location.Bucket, key, strings.NewReader("restored"), 8, "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	put := func(v model.AnalysisRestoredObjectLocation) {
		t.Helper()
		err := store.backend.Transaction(ctx, "t", func(tx StorageTx) error {
			d, _ := bodyDocument("restored-object", RestoredObjectID(sourceBucket, key), "t", v)
			old, err := tx.Get(d.Kind, d.ID)
			expected := int64(0)
			if err == nil {
				expected = old.Version
			} else if !errors.Is(err, model.ErrNotFound) {
				return err
			}
			return save(tx, d, expected)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	read := func() string {
		t.Helper()
		reader, err := OpenAnalysisObject(ctx, store, archive, "t", sourceBucket, key)
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Close()
		b, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if read() != "live" {
		t.Fatal("missing overlay changed ordinary source read")
	}
	put(location)
	if read() != "restored" {
		t.Fatal("mapping did not route actual archive read")
	}
	if _, err := store.GetRestoredObjectLocation(ctx, "other", sourceBucket, key); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("cross tenant object mapping exposed", err)
	}
	location.Key = "other/key"
	put(location)
	if reader, err := OpenAnalysisObject(ctx, store, archive, "t", sourceBucket, key); !errors.Is(err, model.ErrAnalysisInvalid) || reader != nil {
		t.Fatal("corrupt overlay fell back to live source", err)
	}
}
