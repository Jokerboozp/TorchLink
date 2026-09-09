package protocolcatalog

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

func TestPrivateReaderFileAndOriginBoundary(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "reader.token")
	policy := Policy{URL: "https://catalog.example.test/catalog", PublicKeys: map[string]string{"key": base64.StdEncoding.EncodeToString(make([]byte, 32))}, TokenFile: path}
	if _, err := New(policy); err == nil {
		t.Fatal("missing credential file accepted")
	}
	for _, secret := range []string{"", "short", strings.Repeat("a", 32) + "\n" + strings.Repeat("b", 32), strings.Repeat("a", 4097)} {
		os.WriteFile(path, []byte(secret), 0600)
		if _, err := New(policy); err == nil {
			t.Fatal("invalid reader credential accepted")
		}
	}
	os.WriteFile(path, []byte(strings.Repeat("a", 32)), 0600)
	if runtime.GOOS != "windows" {
		os.Chmod(path, 0644)
		if _, err := New(policy); err == nil {
			t.Fatal("public reader credential file accepted")
		}
		os.Chmod(path, 0600)
	}
	client, err := New(policy)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	var requests atomic.Int32
	foreign := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1) }))
	defer foreign.Close()
	if _, err = client.download(context.Background(), foreign.URL+"/steal-token", 10); err == nil {
		t.Fatal("cross-origin request accepted")
	}
	if requests.Load() != 0 {
		t.Fatal("private reader credential could reach foreign origin")
	}
}
