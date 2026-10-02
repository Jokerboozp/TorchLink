package local

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"iot-platform/internal/model"
)

func localCapacityRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func localCapacityScope() (model.CapacityCleanupBatch, model.RawMessage) {
	raw := model.RawMessage{
		TenantID: "test-tenant", ProductID: "test-product", DeviceID: "test-device", MessageID: "test-raw",
		ReceivedAt: 1790812800000, Protocol: "json", PayloadFormat: "JSON", Payload: json.RawMessage(`{"test":true}`),
	}
	q := model.CapacityCleanupBatch{Product: raw.ProductID, Devices: []string{raw.DeviceID}, RawIDs: []string{raw.MessageID}}
	return q, raw
}

func localCapacityGzip(t *testing.T, records ...model.RawMessage) []byte {
	t.Helper()
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	for _, raw := range records {
		if err := json.NewEncoder(gz).Encode(raw); err != nil {
			t.Fatal(err)
		}
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func localCapacityWrite(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o640); err != nil {
		t.Fatal(err)
	}
}

func localCapacityAssertPreserved(t *testing.T, path string, content []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("protected object missing: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatal("protected object content changed")
	}
}

func TestCapacityRawObjectPutRawAndMissingRetry(t *testing.T) {
	for _, authorization := range []string{"ledger", "exclusive-device"} {
		t.Run(authorization, func(t *testing.T) {
			root := localCapacityRoot(t)
			archive, err := NewArchive(root)
			if err != nil {
				t.Fatal(err)
			}
			q, raw := localCapacityScope()
			if authorization == "exclusive-device" {
				q.RawIDs = nil
				q.RemoveDevices = []string{raw.DeviceID}
			}
			idx, err := archive.PutRaw(context.Background(), raw)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, idx.ObjectBucket, filepath.FromSlash(idx.ObjectKey))
			for attempt := 0; attempt < 2; attempt++ {
				if err := archive.DeleteCapacityRawObject(context.Background(), raw.TenantID, q, idx); err != nil {
					t.Fatalf("attempt %d: %v", attempt, err)
				}
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					t.Fatalf("object remains after attempt %d: %v", attempt, err)
				}
			}
		})
	}
}

func TestCapacityRawObjectSharedFirstRecordPreserved(t *testing.T) {
	root := localCapacityRoot(t)
	archive, err := NewArchive(root)
	if err != nil {
		t.Fatal(err)
	}
	q, raw := localCapacityScope()
	idx, err := archive.PutRaw(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	business := raw
	business.DeviceID, business.MessageID = "business-device", "business-raw"
	content := localCapacityGzip(t, raw, business)
	path := filepath.Join(root, idx.ObjectBucket, filepath.FromSlash(idx.ObjectKey))
	localCapacityWrite(t, path, content)
	if err := archive.DeleteCapacityRawObject(context.Background(), raw.TenantID, q, idx); !errors.Is(err, model.ErrResourceInUse) {
		t.Fatalf("shared first record accepted: %v", err)
	}
	localCapacityAssertPreserved(t, path, content)
}

func TestCapacityRawObjectIdentityAndAuthorizationPreserved(t *testing.T) {
	for _, mismatch := range []string{"content-tenant", "content-product", "content-device", "content-message", "index-tenant", "unlisted-device", "unlisted-message", "batch-offset"} {
		t.Run(mismatch, func(t *testing.T) {
			root := localCapacityRoot(t)
			archive, err := NewArchive(root)
			if err != nil {
				t.Fatal(err)
			}
			q, raw := localCapacityScope()
			idx, err := archive.PutRaw(context.Background(), raw)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, idx.ObjectBucket, filepath.FromSlash(idx.ObjectKey))
			switch mismatch {
			case "content-tenant":
				raw.TenantID = "other-tenant"
			case "content-product":
				raw.ProductID = "business-product"
			case "content-device":
				raw.DeviceID = "business-device"
			case "content-message":
				raw.MessageID = "business-raw"
			case "index-tenant":
				idx.TenantID = "other-tenant"
			case "unlisted-device":
				q.Devices = nil
			case "unlisted-message":
				q.RawIDs = nil
			case "batch-offset":
				idx.ObjectOffset = 1
			}
			content := localCapacityGzip(t, raw)
			localCapacityWrite(t, path, content)
			if err := archive.DeleteCapacityRawObject(context.Background(), "test-tenant", q, idx); !errors.Is(err, model.ErrResourceInUse) {
				t.Fatalf("protected identity accepted: %v", err)
			}
			localCapacityAssertPreserved(t, path, content)
		})
	}
}

func TestCapacityRawObjectUnsafePathsPreserved(t *testing.T) {
	for _, unsafe := range []string{"traversal", "absolute-key", "bucket-traversal", "bucket-slash", "bucket-backslash", "absolute-bucket", "empty-bucket", "directory"} {
		t.Run(unsafe, func(t *testing.T) {
			base := localCapacityRoot(t)
			root := filepath.Join(base, "archive")
			archive, err := NewArchive(root)
			if err != nil {
				t.Fatal(err)
			}
			q, raw := localCapacityScope()
			idx := model.RawArchiveIndex{TenantID: raw.TenantID, ProductID: raw.ProductID, DeviceID: raw.DeviceID, MessageID: raw.MessageID, ObjectBucket: "iot-raw-archive", ObjectKey: "object.gz"}
			content := localCapacityGzip(t, raw)
			external := filepath.Join(base, "external", "object.gz")
			localCapacityWrite(t, external, content)
			path := filepath.Join(root, idx.ObjectBucket, idx.ObjectKey)
			switch unsafe {
			case "traversal":
				idx.ObjectKey = "../../external/object.gz"
				path = external
			case "absolute-key":
				idx.ObjectKey = external
				path = external
			case "bucket-traversal":
				idx.ObjectBucket = "../external"
				path = external
			case "bucket-slash":
				idx.ObjectBucket = "nested/bucket"
				path = filepath.Join(root, "nested", "bucket", idx.ObjectKey)
			case "bucket-backslash":
				idx.ObjectBucket = `nested\bucket`
				path = filepath.Join(root, idx.ObjectBucket, idx.ObjectKey)
			case "absolute-bucket":
				idx.ObjectBucket = filepath.Dir(external)
				path = external
			case "empty-bucket":
				idx.ObjectBucket = ""
				path = filepath.Join(root, idx.ObjectKey)
			}
			if unsafe == "directory" {
				if err := os.MkdirAll(path, 0o750); err != nil {
					t.Fatal(err)
				}
			} else {
				localCapacityWrite(t, path, content)
			}
			if err := archive.DeleteCapacityRawObject(context.Background(), raw.TenantID, q, idx); err == nil {
				t.Fatal("unsafe path accepted")
			}
			localCapacityAssertPreserved(t, external, content)
			if unsafe == "directory" {
				if info, err := os.Lstat(path); err != nil || !info.IsDir() {
					t.Fatalf("nonregular target changed: %v", err)
				}
			} else {
				localCapacityAssertPreserved(t, path, content)
			}
		})
	}
}

func TestCapacityRawObjectSymlinksPreserved(t *testing.T) {
	for _, position := range []string{"root", "bucket", "intermediate", "final"} {
		t.Run(position, func(t *testing.T) {
			base := localCapacityRoot(t)
			root := filepath.Join(base, "archive")
			q, raw := localCapacityScope()
			idx := model.RawArchiveIndex{TenantID: raw.TenantID, ProductID: raw.ProductID, DeviceID: raw.DeviceID, MessageID: raw.MessageID, ObjectBucket: "iot-raw-archive", ObjectKey: "object.gz"}
			content := localCapacityGzip(t, raw)
			externalDir := filepath.Join(base, "external")
			external := filepath.Join(externalDir, "object.gz")
			localCapacityWrite(t, external, content)
			link := root
			target := externalDir
			switch position {
			case "root":
				external = filepath.Join(externalDir, idx.ObjectBucket, idx.ObjectKey)
				localCapacityWrite(t, external, content)
			case "bucket":
				link = filepath.Join(root, idx.ObjectBucket)
			case "intermediate":
				idx.ObjectKey = "linked/object.gz"
				link = filepath.Join(root, idx.ObjectBucket, "linked")
			case "final":
				link = filepath.Join(root, idx.ObjectBucket, idx.ObjectKey)
				target = external
			}
			if err := os.MkdirAll(filepath.Dir(link), 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			archive, err := NewArchive(root)
			if err != nil {
				t.Fatal(err)
			}
			if err := archive.DeleteCapacityRawObject(context.Background(), raw.TenantID, q, idx); err == nil {
				t.Fatal("symlink path accepted")
			}
			localCapacityAssertPreserved(t, external, content)
			if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
				t.Fatalf("symlink changed: %v", err)
			}
		})
	}
}
