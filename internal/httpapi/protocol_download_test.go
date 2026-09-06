package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/protocolbuild"
)

type protocolDownloadFixtureV2 struct {
	api    *Server
	server *httptest.Server
	repo   *memory.Repository
	root   string
	token  string
}

func newProtocolDownloadFixtureV2(t *testing.T) protocolDownloadFixtureV2 {
	t.Helper()
	root := t.TempDir()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(root)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(), log)
	engine.Metrics = metrics.New()
	cfg := config.Load()
	cfg.DataDir = root
	cfg.JWTSecret = "protocol-download-test-secret-at-least-32-characters"
	api := New(cfg, engine, engine.Metrics.(*metrics.Registry), log)
	server := httptest.NewServer(api.Handler())
	t.Cleanup(server.Close)
	token, err := api.auth.Issue("protocol-developer", "tenant_001", "operator", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return protocolDownloadFixtureV2{api: api, server: server, repo: repo, root: root, token: token}
}

func protocolDownloadZipV2(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var body bytes.Buffer
	writer := zip.NewWriter(&body)
	for name, content := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = entry.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return body.Bytes()
}

func (f protocolDownloadFixtureV2) save(t *testing.T, version string, entries map[string][]byte, mutate func(map[string]any)) ([]byte, string) {
	t.Helper()
	data := protocolDownloadZipV2(t, entries)
	relative := filepath.Join("protocol-releases", "tenant_001", "vendor-fire", version, "package.zip")
	filename := filepath.Join(f.root, relative)
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, data, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	artifact := map[string]any{"packagePath": filepath.ToSlash(relative), "packageSha256": hex.EncodeToString(digest[:])}
	if mutate != nil {
		mutate(artifact)
	}
	if err := f.repo.CreateProtocolRelease(context.Background(), model.ProtocolRelease{TenantID: "tenant_001", ProtocolID: "vendor-fire", Version: version, ParserType: parser.GoProtocolParserName, Status: "PUBLISHED", Artifact: artifact}); err != nil {
		t.Fatal(err)
	}
	return data, filename
}

func (f protocolDownloadFixtureV2) get(t *testing.T, token, version, kind string, status int) ([]byte, http.Header) {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, f.server.URL+"/api/v2/protocols/vendor-fire/releases/"+version+"/"+kind, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := f.server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != status {
		t.Fatalf("%s %s status=%d want=%d body=%s", version, kind, response.StatusCode, status, body)
	}
	return body, response.Header
}

func TestProtocolReleaseDownloadsPreserveOriginalSourceAndPackage(t *testing.T) {
	f := newProtocolDownloadFixtureV2(t)
	project := protocolDownloadZipV2(t, map[string][]byte{
		"go.mod":             []byte("module example.com/vendor-fire\n\ngo 1.25.0\n"),
		"main.go":            []byte(protocolbuild.Template),
		"samples/cases.json": []byte(protocolSourceCases),
	})
	for _, test := range []struct {
		name, version, extension, contentType string
		source                                []byte
	}{
		{name: "single-file", version: "1.0.0", extension: ".go", contentType: "text/plain; charset=utf-8", source: []byte(protocolbuild.Template)},
		{name: "standalone-project", version: "1.1.0", extension: ".zip", contentType: "application/zip", source: project},
	} {
		t.Run(test.name, func(t *testing.T) {
			digest := sha256.Sum256(test.source)
			archive, _ := f.save(t, test.version, map[string][]byte{"manifest.yaml": []byte("schemaVersion: 1\n"), "bin/worker": []byte("worker"), "source/upload" + test.extension: test.source}, func(artifact map[string]any) {
				artifact["build"] = map[string]any{"kind": "go-source", "sourceSha256": hex.EncodeToString(digest[:])}
			})
			body, headers := f.get(t, f.token, test.version, "source", 200)
			if !bytes.Equal(body, test.source) {
				t.Fatal("download changed the uploaded source bytes")
			}
			if headers.Get("Content-Type") != test.contentType || !strings.Contains(headers.Get("Content-Disposition"), "vendor-fire-"+test.version+"-source"+test.extension) || headers.Get("X-Content-SHA256") != hex.EncodeToString(digest[:]) || headers.Get("Cache-Control") != "no-store" {
				t.Fatalf("unexpected source download headers: %v", headers)
			}
			files, err := protocolbuild.Sources("download"+test.extension, body)
			if err != nil || !bytes.Equal(files["main.go"], []byte(protocolbuild.Template)) {
				t.Fatalf("download cannot be uploaded again as Go source: %v", err)
			}
			body, headers = f.get(t, f.token, test.version, "package", 200)
			if !bytes.Equal(body, archive) || headers.Get("Content-Type") != "application/zip" || !strings.Contains(headers.Get("Content-Disposition"), "-package.zip") {
				t.Fatal("package download did not preserve the complete immutable artifact")
			}
		})
	}
	// A compiled-only release remains exportable, with a clear source error.
	archive, _ := f.save(t, "2.0.0", map[string][]byte{"bin/worker": []byte("worker")}, nil)
	body, _ := f.get(t, f.token, "2.0.0", "source", 404)
	if !strings.Contains(string(body), "未保存原始 Go 源码") {
		t.Fatalf("missing-source explanation: %s", body)
	}
	body, _ = f.get(t, f.token, "2.0.0", "package", 200)
	if !bytes.Equal(body, archive) {
		t.Fatal("compiled-only package download differs")
	}
}

func TestProtocolReleaseDownloadsEnforceAuthorizationAndTenant(t *testing.T) {
	f := newProtocolDownloadFixtureV2(t)
	f.save(t, "1.0.0", map[string][]byte{"source/upload.go": []byte(protocolbuild.Template)}, nil)
	viewer, _ := f.api.auth.Issue("reader", "tenant_001", "viewer", nil, time.Hour)
	otherTenant, _ := f.api.auth.Issue("other", "tenant_002", "operator", nil, time.Hour)
	for _, kind := range []string{"source", "package"} {
		f.get(t, "", "1.0.0", kind, 401)
		f.get(t, viewer, "1.0.0", kind, 403)
		f.get(t, otherTenant, "1.0.0", kind, 404)
		f.get(t, f.token, "9.9.9", kind, 404)
	}
}

func TestProtocolReleaseDownloadsRejectWrongPathsAndCorruption(t *testing.T) {
	f := newProtocolDownloadFixtureV2(t)
	for index, wrongPath := range []string{
		"../outside.zip",
		filepath.Join(f.root, "outside.zip"),
		"protocol-releases/tenant_002/vendor-fire/1.0.0/package.zip",
		"protocol-releases/tenant_001/vendor-fire/1.0.0/package.zip",
	} {
		version := []string{"2.0.0", "2.1.0", "2.2.0", "2.3.0"}[index]
		f.save(t, version, map[string][]byte{"source/upload.go": []byte(protocolbuild.Template)}, func(artifact map[string]any) { artifact["packagePath"] = wrongPath })
		for _, kind := range []string{"source", "package"} {
			f.get(t, f.token, version, kind, 409)
		}
	}
	_, filename := f.save(t, "3.0.0", map[string][]byte{"source/upload.go": []byte(protocolbuild.Template)}, nil)
	if err := os.WriteFile(filename, []byte("tampered artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"source", "package"} {
		body, _ := f.get(t, f.token, "3.0.0", kind, 409)
		if !strings.Contains(string(body), "SHA-256") || strings.Contains(string(body), "tampered artifact") {
			t.Fatalf("corrupt artifact response: %s", body)
		}
	}
	f.save(t, "3.1.0", map[string][]byte{"source/upload.go": []byte(protocolbuild.Template)}, func(artifact map[string]any) {
		artifact["build"] = map[string]any{"sourceSha256": strings.Repeat("0", 64)}
	})
	f.get(t, f.token, "3.1.0", "source", 409)
	f.save(t, "3.2.0", map[string][]byte{"source/upload.go": []byte(protocolbuild.Template), "source/upload.zip": []byte("ambiguous")}, nil)
	f.get(t, f.token, "3.2.0", "source", 409)
}
