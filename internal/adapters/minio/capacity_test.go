package minioadapter

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"iot-platform/internal/adapters/rawstore"
	"iot-platform/internal/model"
)

type capacityObjectFixture struct {
	mu      sync.Mutex
	content []byte
	code    string
	reads   int
	deletes int
}

func newCapacityObjectFixture(t *testing.T, content []byte, code string) (*Archive, *capacityObjectFixture) {
	t.Helper()
	fixture := &capacityObjectFixture{content: content, code: code}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fixture.mu.Lock()
		defer fixture.mu.Unlock()
		if r.URL.Path != "/raw-archive/object.gz" {
			t.Errorf("unexpected object path: %s", r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		switch r.Method {
		case http.MethodGet:
			fixture.reads++
			if fixture.code != "" {
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprintf(w, "<Error><Code>%s</Code><Message>missing test object</Message></Error>", fixture.code)
				return
			}
			w.Header().Set("Content-Length", fmt.Sprint(len(fixture.content)))
			w.Header().Set("Content-Type", "application/gzip")
			w.Header().Set("ETag", `"fixture"`)
			w.Header().Set("Last-Modified", "Thu, 01 Oct 2026 00:00:00 GMT")
			_, _ = w.Write(fixture.content)
		case http.MethodDelete:
			fixture.deletes++
			fixture.content, fixture.code = nil, "NoSuchKey"
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected S3 method: %s", r.Method)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	t.Cleanup(server.Close)
	client, err := minio.New(strings.TrimPrefix(server.URL, "http://"), &minio.Options{Creds: credentials.NewStaticV4("test-access", "test-secret", ""), Region: "us-east-1"})
	if err != nil {
		t.Fatal(err)
	}
	return &Archive{client: client}, fixture
}

func capacityObjectGzip(t *testing.T, records ...model.RawMessage) []byte {
	t.Helper()
	var data bytes.Buffer
	gz := gzip.NewWriter(&data)
	for _, record := range records {
		if err := json.NewEncoder(gz).Encode(record); err != nil {
			t.Fatal(err)
		}
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func capacityObjectScope() (model.CapacityCleanupBatch, model.RawArchiveIndex, model.RawMessage) {
	q := model.CapacityCleanupBatch{Product: "fixture", Devices: []string{"test-device"}, RawIDs: []string{"raw-test"}}
	idx := model.RawArchiveIndex{TenantID: "t", ProductID: "fixture", DeviceID: "test-device", MessageID: "raw-test", ObjectBucket: "raw-archive", ObjectKey: "object.gz", ObjectOffset: 0}
	raw := model.RawMessage{TenantID: idx.TenantID, ProductID: idx.ProductID, DeviceID: idx.DeviceID, MessageID: idx.MessageID, Payload: json.RawMessage(`{"test":true}`)}
	return q, idx, raw
}

func TestCapacityRawObjectSharedFirstRecordPreserved(t *testing.T) {
	q, idx, raw := capacityObjectScope()
	business := raw
	business.DeviceID, business.MessageID = "business", "raw-business"
	content := capacityObjectGzip(t, raw, business)
	archive, fixture := newCapacityObjectFixture(t, content, "")
	if err := archive.DeleteCapacityRawObject(context.Background(), "t", q, idx); !errors.Is(err, model.ErrResourceInUse) {
		t.Fatalf("shared first record accepted: %v", err)
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if fixture.deletes != 0 || !bytes.Equal(fixture.content, content) {
		t.Fatal("shared object was modified")
	}
}

func TestCapacityRawObjectExactSingleAndMissingRetry(t *testing.T) {
	for _, exclusive := range []bool{false, true} {
		t.Run(fmt.Sprint(exclusive), func(t *testing.T) {
			q, idx, raw := capacityObjectScope()
			if exclusive {
				q.RawIDs = nil
				q.RemoveDevices = []string{idx.DeviceID}
			}
			archive, fixture := newCapacityObjectFixture(t, capacityObjectGzip(t, raw), "")
			for attempt := 0; attempt < 2; attempt++ {
				if err := archive.DeleteCapacityRawObject(context.Background(), "t", q, idx); err != nil {
					t.Fatalf("attempt %d: %v", attempt, err)
				}
			}
			fixture.mu.Lock()
			defer fixture.mu.Unlock()
			if fixture.deletes != 1 || fixture.reads != 2 {
				t.Fatalf("unexpected retry requests: reads=%d deletes=%d", fixture.reads, fixture.deletes)
			}
		})
	}
}

func TestCapacityRawObjectRejectsIdentityAndUnauthorizedScope(t *testing.T) {
	for _, mismatch := range []string{"content-tenant", "content-product", "content-device", "content-message", "index-tenant", "unlisted-device", "unlisted-message", "batch-offset"} {
		t.Run(mismatch, func(t *testing.T) {
			q, idx, raw := capacityObjectScope()
			switch mismatch {
			case "content-tenant":
				raw.TenantID = "other-tenant"
			case "content-product":
				raw.ProductID = "business"
			case "content-device":
				raw.DeviceID = "business"
			case "content-message":
				raw.MessageID = "raw-business"
			case "index-tenant":
				idx.TenantID = "other-tenant"
			case "unlisted-device":
				q.Devices = nil
			case "unlisted-message":
				q.RawIDs = nil
			case "batch-offset":
				idx.ObjectOffset = 1
			}
			archive, fixture := newCapacityObjectFixture(t, capacityObjectGzip(t, raw), "")
			if err := archive.DeleteCapacityRawObject(context.Background(), "t", q, idx); !errors.Is(err, model.ErrResourceInUse) {
				t.Fatalf("mismatch accepted: %v", err)
			}
			fixture.mu.Lock()
			defer fixture.mu.Unlock()
			if fixture.deletes != 0 {
				t.Fatal("protected object deleted")
			}
		})
	}
}

func TestCapacityRawObjectMalformedOversizedAndMissingBucketPreserved(t *testing.T) {
	q, idx, raw := capacityObjectScope()
	corrupt := capacityObjectGzip(t, raw)
	corrupt[len(corrupt)-1] ^= 1
	large := raw
	large.Payload, _ = json.Marshal(strings.Repeat("a", rawstore.CapacityRawObjectLimit))
	for _, test := range []struct {
		name    string
		content []byte
		code    string
	}{
		{"bad-gzip", []byte("invalid gzip"), ""},
		{"bad-checksum", corrupt, ""},
		{"too-large", capacityObjectGzip(t, large), ""},
		{"missing-bucket", nil, "NoSuchBucket"},
		{"second-gzip-member", append(capacityObjectGzip(t, raw), capacityObjectGzip(t, raw)...), ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			archive, fixture := newCapacityObjectFixture(t, test.content, test.code)
			if err := archive.DeleteCapacityRawObject(context.Background(), "t", q, idx); err == nil {
				t.Fatal("invalid object accepted")
			}
			fixture.mu.Lock()
			defer fixture.mu.Unlock()
			if fixture.deletes != 0 {
				t.Fatal("invalid object deleted")
			}
		})
	}
}
