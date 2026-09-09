package protocolcatalog

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestSignedHTTPSCatalogAndSourceBoundaries(t *testing.T) {
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	source := []byte("verified-source")
	hash := sha256.Sum256(source)
	var mu sync.Mutex
	var envelope []byte
	var mode string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if mode == "redirect" {
			http.Redirect(w, r, "https://untrusted.invalid/source.zip", 302)
			return
		}
		if mode == "unavailable" {
			w.WriteHeader(503)
			return
		}
		if r.URL.Path == "/catalog.json" {
			w.Write(envelope)
			return
		}
		if mode == "tampered" {
			w.Write([]byte("tampered-source"))
			return
		}
		w.Write(source)
	}))
	defer server.Close()
	ca := filepath.Join(t.TempDir(), "ca.pem")
	os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600)
	policy := Policy{URL: server.URL + "/catalog.json", PublicKeys: map[string]string{"trusted": base64.StdEncoding.EncodeToString(public)}, CAFile: ca}
	client, err := New(policy)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	payload := Payload{IssuedAt: time.Now().UnixMilli(), ExpiresAt: time.Now().Add(time.Hour).UnixMilli(), Entries: []Entry{{ID: "example", Version: "1.0.0", Name: "Example", SourceURL: server.URL + "/source.zip", Size: int64(len(source)), SHA256: hex.EncodeToString(hash[:])}}}
	set := func(p Payload, key string, signing ed25519.PrivateKey) {
		b, _ := json.Marshal(p)
		e, _ := json.Marshal(Envelope{KeyID: key, Payload: base64.StdEncoding.EncodeToString(b), Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(signing, b))})
		mu.Lock()
		envelope = e
		mode = ""
		mu.Unlock()
	}
	set(payload, "trusted", private)
	catalog, err := client.Fetch(context.Background())
	if err != nil || catalog.KeyID != "trusted" || len(catalog.Entries) != 1 {
		t.Fatal(catalog, err)
	}
	data, err := client.Source(context.Background(), catalog.Entries[0])
	if err != nil || string(data) != string(source) {
		t.Fatal("source", err)
	}
	for _, value := range []string{"tampered", "redirect", "unavailable"} {
		mu.Lock()
		mode = value
		mu.Unlock()
		if _, err := client.Source(context.Background(), catalog.Entries[0]); err == nil {
			t.Fatal("accepted source", value)
		}
	}
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	set(payload, "trusted", other)
	if _, err := client.Fetch(context.Background()); err == nil {
		t.Fatal("wrong signature accepted")
	}
	set(payload, "untrusted", private)
	if _, err := client.Fetch(context.Background()); err == nil {
		t.Fatal("unknown key accepted")
	}
	expired := payload
	expired.ExpiresAt = time.Now().Add(-time.Minute).UnixMilli()
	set(expired, "trusted", private)
	if _, err := client.Fetch(context.Background()); err == nil {
		t.Fatal("expired catalog accepted")
	}
	foreign := payload
	foreign.Entries = append([]Entry{}, payload.Entries...)
	foreign.Entries[0].SourceURL = "https://other.invalid/source.zip"
	set(foreign, "trusted", private)
	if _, err := client.Fetch(context.Background()); err == nil {
		t.Fatal("foreign source accepted")
	}
	set(payload, "trusted", private)
	policy.CAFile = ""
	untrusted, err := New(policy)
	if err != nil {
		t.Fatal(err)
	}
	defer untrusted.Close()
	if _, err := untrusted.Fetch(context.Background()); err == nil {
		t.Fatal("untrusted TLS accepted")
	}
	policy.URL = "http://127.0.0.1/catalog.json"
	if _, err := New(policy); err == nil {
		t.Fatal("plain HTTP accepted")
	}
}
