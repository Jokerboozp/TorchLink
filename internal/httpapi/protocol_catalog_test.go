package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/parser"
	"iot-platform/internal/protocolbuild"
	"iot-platform/internal/protocolcatalog"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAuthenticatedCatalogSourceInstall(t *testing.T) {
	if !protocolbuild.Available() {
		t.Skip("Go compiler unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	var source bytes.Buffer
	zw := zip.NewWriter(&source)
	for name, body := range map[string]string{
		"main.go":            protocolbuild.Template,
		"protocol.json":      `{"id":"catalog-demo","name":"目录示例","version":"1.0.0","transport":"MQTT","payloadFormat":"hex","runtime":"go-protocol-v2"}`,
		"samples/cases.json": protocolSourceCases,
	} {
		f, _ := zw.Create(name)
		f.Write([]byte(body))
	}
	zw.Close()
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	var signed []byte
	var sourceMu sync.RWMutex
	wireSource := source.Bytes()
	sourceServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sourceMu.RLock()
		defer sourceMu.RUnlock()
		if r.URL.Path == "/catalog.json" {
			w.Write(signed)
		} else {
			w.Write(wireSource)
		}
	}))
	defer sourceServer.Close()
	hash := sha256.Sum256(source.Bytes())
	payload := protocolcatalog.Payload{IssuedAt: time.Now().UnixMilli(), ExpiresAt: time.Now().Add(time.Hour).UnixMilli(), Entries: []protocolcatalog.Entry{{ID: "catalog-demo", Version: "1.0.0", Name: "目录示例", Publisher: "Integration Publisher", SourceURL: sourceServer.URL + "/source.zip", SHA256: hex.EncodeToString(hash[:]), Size: int64(source.Len())}}}
	data, _ := json.Marshal(payload)
	signed, _ = json.Marshal(protocolcatalog.Envelope{KeyID: "fixture", Payload: base64.StdEncoding.EncodeToString(data), Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(private, data))})
	root := t.TempDir()
	ca := filepath.Join(root, "catalog-ca.pem")
	os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: sourceServer.Certificate().Raw}), 0600)
	policy, _ := json.Marshal(protocolcatalog.Policy{URL: sourceServer.URL + "/catalog.json", PublicKeys: map[string]string{"fixture": base64.StdEncoding.EncodeToString(public)}, CAFile: ca})
	policyPath := filepath.Join(root, "catalog-policy.json")
	os.WriteFile(policyPath, policy, 0600)
	cfg := config.Load()
	cfg.DataDir = root
	cfg.ProtocolCatalogPolicy = policyPath
	repo := memory.NewRepository()
	archive, err := local.NewArchive(root)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(root), log)
	api := New(cfg, engine, metrics.New(), log)
	assets := http.FileServer(http.Dir(filepath.Join("..", "..", "iot_front", "dist")))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			api.Handler().ServeHTTP(w, r)
		} else {
			assets.ServeHTTP(w, r)
		}
	}))
	defer server.Close()
	call := func(role, method, path string, body any) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(method, path, bytes.NewReader(b))
		token, err := api.auth.Issue("tester", "t", role, nil, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		api.Handler().ServeHTTP(w, req)
		return w
	}
	w := call("viewer", "GET", "/api/v2/protocol-catalog", nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var listing struct {
		Catalog protocolcatalog.Catalog `json:"catalog"`
	}
	if json.Unmarshal(w.Body.Bytes(), &listing) != nil || len(listing.Catalog.Entries) != 1 {
		t.Fatal("catalog missing")
	}
	body := map[string]any{"id": "catalog-demo", "version": "1.0.0", "digest": listing.Catalog.Digest, "confirmed": true}
	path := "/api/v2/protocol-catalog/install"
	if w := call("operator", "POST", path, body); w.Code != 403 {
		t.Fatal("operator installed", w.Code)
	}
	body["confirmed"] = false
	if w := call("admin", "POST", path, body); w.Code != 422 {
		t.Fatal("missing confirmation", w.Code)
	}
	body["confirmed"] = true
	body["digest"] = "old"
	if w := call("admin", "POST", path, body); w.Code != 409 {
		t.Fatal("stale catalog", w.Code)
	}
	body["digest"] = listing.Catalog.Digest
	t.Run("browser", func(t *testing.T) {
		if os.Getenv("IOT_TEST_BROWSER") == "" {
			t.Skip("IOT_TEST_BROWSER is not configured")
		}
		token, _ := api.auth.Issue("browser-test", "t", "admin", nil, time.Minute)
		command := exec.CommandContext(ctx, "node", filepath.Join("..", "..", "iot_front", "tests", "browser", "catalog-check.mjs"))
		command.Env = append(os.Environ(), "IOT_TEST_ORIGIN="+server.URL, "IOT_TEST_TOKEN="+token)
		out, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("browser: %v %s", err, out)
		}
		t.Log(string(out))
	})
	// Browser, when enabled, performs the actual install. Otherwise exercise the
	// identical authenticated handler directly; neither path inserts a fake release.
	if os.Getenv("IOT_TEST_BROWSER") == "" {
		if w := call("admin", "POST", path, body); w.Code != 201 {
			t.Fatal("install", w.Code, w.Body.String())
		}
	}
	release, err := repo.GetProtocolRelease(ctx, "t", "catalog-demo", "1.0.0")
	if err != nil || release.Status != "VALIDATED" {
		t.Fatal("verified release", release, err)
	}
	build, _ := release.Artifact["build"].(map[string]any)
	provenance, _ := build["catalog"].(map[string]any)
	if provenance["sourceSha256"] != payload.Entries[0].SHA256 || provenance["keyId"] != "fixture" {
		t.Fatal("verified provenance missing", build)
	}
	if _, err := repo.GetProtocolRelease(ctx, "other", "catalog-demo", "1.0.0"); err == nil {
		t.Fatal("cross tenant release")
	}
	if w := call("admin", "POST", path, body); w.Code != 409 {
		t.Fatal("duplicate install", w.Code)
	}
	if w := call("operator", "POST", "/api/v2/protocols/catalog-demo/releases/1.0.0/publish", map[string]any{}); w.Code != 200 {
		t.Fatal("explicit publish", w.Code, w.Body.String())
	}
	release, _ = repo.GetProtocolRelease(ctx, "t", "catalog-demo", "1.0.0")
	if release.Status != "PUBLISHED" {
		t.Fatal("publish was not applied")
	}
	var broken bytes.Buffer
	badZip := zip.NewWriter(&broken)
	for name, body := range map[string]string{
		"main.go":            protocolbuild.Template,
		"protocol.json":      `{"id":"catalog-demo","version":"1.1.0","transport":"MQTT","payloadFormat":"hex","runtime":"go-protocol-v2"}`,
		"samples/cases.json": strings.ReplaceAll(protocolSourceCases, `"temperature":42`, `"temperature":999`),
	} {
		f, _ := badZip.Create(name)
		f.Write([]byte(body))
	}
	badZip.Close()
	badHash := sha256.Sum256(broken.Bytes())
	payload.Entries[0].Version = "1.1.0"
	payload.Entries[0].Size = int64(broken.Len())
	payload.Entries[0].SHA256 = hex.EncodeToString(badHash[:])
	badPayload, _ := json.Marshal(payload)
	badEnvelope, _ := json.Marshal(protocolcatalog.Envelope{KeyID: "fixture", Payload: base64.StdEncoding.EncodeToString(badPayload), Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(private, badPayload))})
	sourceMu.Lock()
	wireSource = broken.Bytes()
	signed = badEnvelope
	sourceMu.Unlock()
	badDigest := sha256.Sum256(badPayload)
	body["version"] = "1.1.0"
	body["digest"] = hex.EncodeToString(badDigest[:])
	if w := call("admin", "POST", path, body); w.Code != 422 {
		t.Fatalf("bad sample accepted: %d %s", w.Code, w.Body.String())
	}
	if _, err := repo.GetProtocolRelease(ctx, "t", "catalog-demo", "1.1.0"); err == nil {
		t.Fatal("failed sample created release")
	}
	release, _ = repo.GetProtocolRelease(ctx, "t", "catalog-demo", "1.0.0")
	if release.Status != "PUBLISHED" {
		t.Fatal("failed install changed active release")
	}

}
