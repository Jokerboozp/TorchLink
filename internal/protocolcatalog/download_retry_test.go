package protocolcatalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
)

func TestDownloadRetryClassification(t *testing.T) {
	var status atomic.Int32
	var truncated atomic.Bool
	status.Store(200)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if truncated.Load() {
			w.Header().Set("Content-Length", "100")
			w.Write([]byte("short"))
			return
		}
		w.WriteHeader(int(status.Load()))
		w.Write([]byte("payload"))
	}))
	defer server.Close()
	address, _ := url.Parse(server.URL)
	c := &Client{http: server.Client(), address: address}
	for _, test := range []struct {
		status int
		retry  bool
	}{{408, true}, {429, true}, {500, true}, {503, true}, {401, false}, {403, false}, {404, false}, {302, false}} {
		status.Store(int32(test.status))
		_, err := c.download(context.Background(), server.URL+"/file", 128)
		if err == nil || RetryableDownload(err) != test.retry {
			t.Fatalf("HTTP %d retry=%v error=%v", test.status, RetryableDownload(err), err)
		}
	}
	status.Store(200)
	truncated.Store(true)
	if _, err := c.download(context.Background(), server.URL+"/file", 128); !RetryableDownload(err) {
		t.Fatal("interrupted body not retryable", err)
	}
	truncated.Store(false)
	if _, err := c.download(context.Background(), server.URL+"/file", 2); err == nil || RetryableDownload(err) {
		t.Fatal("oversized data should fail permanently", err)
	}
	c.http = &http.Client{}
	if _, err := c.download(context.Background(), server.URL+"/file", 128); err == nil || RetryableDownload(err) {
		t.Fatal("untrusted TLS must fail permanently", err)
	}
	c.http = server.Client()
	server.Close()
	if _, err := c.download(context.Background(), server.URL+"/file", 128); !RetryableDownload(err) {
		t.Fatal("connection loss not retryable", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.download(ctx, server.URL+"/file", 128); err != context.Canceled || RetryableDownload(err) {
		t.Fatal("cancellation must stop", err)
	}
}
