package local

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"iot-platform/internal/model"
)

func TestArchiveRoundTripAndKeysStayInsideBuckets(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	a, err := NewArchive(filepath.Join(root, "archive"))
	if err != nil {
		t.Fatal(err)
	}
	raw := model.RawMessage{MessageID: "m/../1", TenantID: "t", ProductID: "p", DeviceID: "d", Protocol: "json", PayloadFormat: "json", ReceivedAt: 1, Payload: []byte(`{"a":1}`)}
	idx, err := a.PutRaw(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(idx.ObjectKey, "..") {
		t.Fatalf("raw key keeps a parent segment: %s", idx.ObjectKey)
	}
	if got, err := a.GetRaw(ctx, idx); err != nil || string(got.Payload) != `{"a":1}` {
		t.Fatalf("raw round trip %+v %v", got, err)
	}
	if _, err = a.PutObject(ctx, "files", "doc/a.txt", strings.NewReader("hello"), 5, "text/plain"); err != nil {
		t.Fatal(err)
	}
	r, err := a.GetObject(ctx, "files", "doc/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r)
	r.Close()
	if string(b) != "hello" {
		t.Fatalf("object round trip %q", b)
	}

	secret := filepath.Join(root, "secret.txt")
	if err = os.WriteFile(secret, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"../../secret.txt", "../files2/x", "a/../../../secret.txt", ""} {
		if _, err := a.GetObject(ctx, "files", key); err == nil {
			t.Fatalf("read outside the bucket with %q", key)
		}
		if _, err := a.PutObject(ctx, "files", key, strings.NewReader("x"), 1, ""); err == nil {
			t.Fatalf("wrote outside the bucket with %q", key)
		}
		if err := a.DeleteObject(ctx, "files", key); err == nil {
			t.Fatalf("deleted outside the bucket with %q", key)
		}
		if _, err := a.GetRaw(ctx, model.RawArchiveIndex{ObjectBucket: "iot-raw-archive", ObjectKey: key}); err == nil {
			t.Fatalf("raw read outside the bucket with %q", key)
		}
	}
	if b, _ := os.ReadFile(secret); string(b) != "outside" {
		t.Fatal("file outside the archive changed")
	}
}
