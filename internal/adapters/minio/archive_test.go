package minioadapter

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"

	"iot-platform/internal/model"
)

// TestArchiveRoundTrip runs against a real S3-compatible store (RustFS or
// MinIO) given by IOT_TEST_MINIO_ENDPOINT, IOT_TEST_MINIO_ACCESS_KEY and
// IOT_TEST_MINIO_SECRET_KEY; it writes only to a throwaway bucket.
func TestArchiveRoundTrip(t *testing.T) {
	endpoint := os.Getenv("IOT_TEST_MINIO_ENDPOINT")
	if endpoint == "" {
		t.Skip("IOT_TEST_MINIO_ENDPOINT not configured")
	}
	a, err := New(endpoint, os.Getenv("IOT_TEST_MINIO_ACCESS_KEY"), os.Getenv("IOT_TEST_MINIO_SECRET_KEY"), os.Getenv("IOT_TEST_MINIO_TLS") == "true")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	suffix := make([]byte, 4)
	_, _ = rand.Read(suffix)
	bucket := "iot-test-" + hex.EncodeToString(suffix)
	t.Cleanup(func() {
		ctx := context.Background()
		for object := range a.client.ListObjects(ctx, bucket, minio.ListObjectsOptions{Recursive: true}) {
			_ = a.client.RemoveObject(ctx, bucket, object.Key, minio.RemoveObjectOptions{})
		}
		_ = a.client.RemoveBucket(ctx, bucket)
	})
	if err := a.Health(ctx); err != nil {
		t.Fatal(err)
	}
	// An unknown size is buffered; the stored object reads back unchanged.
	if _, err := a.PutObject(ctx, bucket, "dir/plain.txt", bytes.NewBufferString("你好，存储"), -1, "text/plain"); err != nil {
		t.Fatal(err)
	}
	r, err := a.GetObject(ctx, bucket, "dir/plain.txt")
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil || string(got) != "你好，存储" {
		t.Fatalf("read back %q, %v", got, err)
	}
	// Legacy raw batches are gzip JSON lines found by offset or message ID.
	var batch bytes.Buffer
	gz := gzip.NewWriter(&batch)
	for _, id := range []string{"m1", "m2"} {
		_ = json.NewEncoder(gz).Encode(model.RawMessage{TenantID: "t", DeviceID: "d", MessageID: id, Payload: json.RawMessage(`{"v":1}`)})
	}
	_ = gz.Close()
	if _, err := a.PutObject(ctx, bucket, "raw/batch.jsonl.gz", &batch, int64(batch.Len()), "application/gzip"); err != nil {
		t.Fatal(err)
	}
	raw, err := a.GetRaw(ctx, model.RawArchiveIndex{ObjectBucket: bucket, ObjectKey: "raw/batch.jsonl.gz", ObjectOffset: 5, MessageID: "m2"})
	if err != nil || raw.MessageID != "m2" {
		t.Fatalf("GetRaw = %+v, %v", raw, err)
	}
	if err := a.DeleteObject(ctx, bucket, "dir/plain.txt"); err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteObject(ctx, bucket, "dir/plain.txt"); err != nil {
		t.Fatalf("deleting a missing object = %v, want nil", err)
	}
	if err := a.DeleteObject(ctx, bucket+"-missing", "x"); err != nil {
		t.Fatalf("deleting from a missing bucket = %v, want nil", err)
	}
}
