package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
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
	"iot-platform/internal/onboarding"
	"iot-platform/internal/parser"
	"iot-platform/internal/protocolbuild"
	"iot-platform/internal/protocolcatalog"
	"iot-platform/internal/protocolmarket"
)

func TestPrivateProtocolMarketReviewAndRemoteInstall(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	root := t.TempDir()
	policyPath := filepath.Join(root, "market-policy.json")
	makeAPI := func(dir, catalog, market string) (*Server, *memory.Repository) {
		t.Helper()
		repo := memory.NewRepository()
		archive, err := local.NewArchive(dir)
		if err != nil {
			t.Fatal(err)
		}
		log := slog.New(slog.NewTextHandler(io.Discard, nil))
		engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(dir), log)
		if err = engine.Start(ctx); err != nil {
			t.Fatal(err)
		}
		cfg := config.Load()
		cfg.DataDir = dir
		cfg.ProtocolCatalogPolicy = catalog
		cfg.ProtocolMarketPolicy = market
		return New(cfg, engine, metrics.New(), log), repo
	}
	publisher, repo := makeAPI(root, "", policyPath)
	server := httptest.NewTLSServer(publisher.Handler())
	defer server.Close()
	keyPath := filepath.Join(root, "signing.key")
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	if err := os.WriteFile(keyPath, []byte(base64.StdEncoding.EncodeToString(private)), 0600); err != nil {
		t.Fatal(err)
	}
	secret := "fixture-private-catalog-reader-credential-0001"
	policy := protocolmarket.Policy{PublicOrigin: server.URL, Organizations: map[string]protocolmarket.Organization{"tenant": {Publisher: "验证组织", KeyID: "org-key", PrivateKeyFile: keyPath, ReaderTokenHashes: []string{onboarding.Hash(secret)}}, "other": {Publisher: "其他组织", KeyID: "org-key", PrivateKeyFile: keyPath, ReaderTokenHashes: []string{onboarding.Hash("different-organization-reader-credential")}}}}
	savePolicy := func() {
		data, _ := json.Marshal(policy)
		if err := os.WriteFile(policyPath, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	savePolicy()
	ca := filepath.Join(root, "ca.pem")
	os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600)
	tokenFile := filepath.Join(root, "reader.token")
	os.WriteFile(tokenFile, []byte(secret), 0600)
	consumerPolicy := protocolcatalog.Policy{URL: server.URL + "/api/v2/market-distribution/tenant/catalog", PublicKeys: map[string]string{"org-key": base64.StdEncoding.EncodeToString(public)}, CAFile: ca, TokenFile: tokenFile}
	client, err := protocolcatalog.New(consumerPolicy)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	initial, err := client.Fetch(ctx)
	if err != nil || len(initial.Entries) != 0 {
		t.Fatal("empty private catalog", err)
	}
	call := func(api *Server, tenant, user, role, method, path string, body any, want int) *httptest.ResponseRecorder {
		t.Helper()
		encoded, _ := json.Marshal(body)
		req := httptest.NewRequest(method, path, bytes.NewReader(encoded))
		token, _ := api.auth.Issue(user, tenant, role, nil, time.Minute)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		api.Handler().ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("%s: %d want %d: %s", path, w.Code, want, w.Body.String())
		}
		return w
	}
	upload := func(version string) {
		t.Helper()
		var project bytes.Buffer
		z := zip.NewWriter(&project)
		metadata, _ := json.Marshal(map[string]any{"id": "market-demo", "version": version, "name": "市场示例", "transport": "MQTT", "payloadFormat": "hex", "runtime": "go-json-lines-v1"})
		for name, data := range map[string][]byte{"main.go": []byte(protocolbuild.Template), "protocol.json": metadata, "samples/cases.json": []byte(protocolSourceCases)} {
			f, _ := z.Create(name)
			f.Write(data)
		}
		z.Close()
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		f, _ := form.CreateFormFile("file", "source.zip")
		f.Write(project.Bytes())
		form.WriteField("publish", "false")
		form.Close()
		req := httptest.NewRequest("POST", "/api/v2/protocols/market-demo/source-releases", &body)
		token, _ := publisher.auth.Issue("publisher", "tenant", "operator", nil, time.Minute)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", form.FormDataContentType())
		w := httptest.NewRecorder()
		publisher.Handler().ServeHTTP(w, req)
		if w.Code != 201 {
			t.Fatal("actual source validation", w.Code, w.Body.String())
		}
	}
	upload("1.0.0")
	submission := map[string]any{"name": "市场示例", "description": "已校验的测试协议", "license": "组织内部使用", "tags": []string{"消防"}, "confirmed": true}
	path := "/api/v2/protocol-market/market-demo/1.0.0"
	call(publisher, "tenant", "reader", "viewer", "POST", path, submission, 403)
	call(publisher, "other", "publisher", "operator", "POST", path, submission, 404)
	call(publisher, "tenant", "publisher", "operator", "POST", path, map[string]any{"confirmed": false}, 422)
	call(publisher, "tenant", "publisher", "operator", "POST", path, submission, 201)
	call(publisher, "tenant", "publisher", "operator", "POST", path, submission, 409)
	pending, err := client.Fetch(ctx)
	if err != nil || len(pending.Entries) != 0 {
		t.Fatal("unreviewed source distributed", err)
	}
	decision := map[string]any{"decision": "APPROVED", "reason": "核实源码与许可，实际样例通过", "generation": 1, "confirmed": true}
	call(publisher, "tenant", "publisher", "admin", "POST", path+"/review", decision, 422)
	call(publisher, "tenant", "reviewer", "operator", "POST", path+"/review", decision, 403)
	call(publisher, "other", "reviewer", "admin", "POST", path+"/review", decision, 404)
	call(publisher, "tenant", "reviewer", "admin", "POST", path+"/review", decision, 200)
	call(publisher, "tenant", "reviewer", "admin", "POST", path+"/review", decision, 409)
	catalog, err := client.Fetch(ctx)
	if err != nil || len(catalog.Entries) != 1 || catalog.Entries[0].Publisher != "验证组织" {
		t.Fatal("approved catalog", err)
	}
	again, err := client.Fetch(ctx)
	if err != nil || again.Digest != catalog.Digest {
		t.Fatal("unchanged catalog digest is unstable", err)
	}
	data, err := client.Source(ctx, catalog.Entries[0])
	if err != nil || len(data) == 0 {
		t.Fatal("authenticated source", err)
	}
	// Reader credentials are organization-scoped and never replaced by user JWTs.
	for _, wrong := range []protocolcatalog.Policy{{URL: consumerPolicy.URL, PublicKeys: consumerPolicy.PublicKeys, CAFile: ca}, {URL: strings.Replace(consumerPolicy.URL, "/tenant/", "/other/", 1), PublicKeys: consumerPolicy.PublicKeys, CAFile: ca, TokenFile: tokenFile}, {URL: consumerPolicy.URL, PublicKeys: consumerPolicy.PublicKeys, TokenFile: tokenFile}} {
		reader, err := protocolcatalog.New(wrong)
		if err != nil {
			t.Fatal(err)
		}
		_, err = reader.Fetch(ctx)
		reader.Close()
		if err == nil {
			t.Fatal("missing credential, foreign tenant or untrusted CA accepted")
		}
	}
	consumerRoot := t.TempDir()
	consumerPolicyPath := filepath.Join(consumerRoot, "catalog-policy.json")
	encoded, _ := json.Marshal(consumerPolicy)
	os.WriteFile(consumerPolicyPath, encoded, 0600)
	consumer, consumerRepo := makeAPI(consumerRoot, consumerPolicyPath, "")
	install := map[string]any{"id": "market-demo", "version": "1.0.0", "digest": catalog.Digest, "confirmed": true}
	call(consumer, "consumer", "installer", "admin", "POST", "/api/v2/protocol-catalog/install", install, 201)
	installed, err := consumerRepo.GetProtocolRelease(ctx, "consumer", "market-demo", "1.0.0")
	if err != nil || installed.Status != "VALIDATED" || artifactTestCountV2(installed.Artifact) != 1 {
		t.Fatal("remote source was not actually validated", err)
	}
	parsed, err := (parser.ExternalParser{Root: consumerRoot}).ParseWithConfig(model.RawMessage{Payload: json.RawMessage(`"AA 01 2A"`)}, installed.Config)
	if err != nil || parsed.Properties["temperature"] != float64(42) {
		t.Fatal("installed worker actual execution", err)
	}
	// A local policy rotation immediately rejects the old machine credential.
	org := policy.Organizations["tenant"]
	org.ReaderTokenHashes = []string{onboarding.Hash("replacement-private-catalog-reader-credential")}
	policy.Organizations["tenant"] = org
	savePolicy()
	if _, err = client.Source(ctx, catalog.Entries[0]); err == nil {
		t.Fatal("rotated reader still downloads")
	}
	org.ReaderTokenHashes = []string{onboarding.Hash(secret)}
	policy.Organizations["tenant"] = org
	savePolicy()
	call(publisher, "tenant", "reviewer", "admin", "POST", path+"/review", map[string]any{"decision": "WITHDRAWN", "reason": "停止新分发", "generation": 2, "confirmed": true}, 200)
	withdrawn, err := client.Fetch(ctx)
	if err != nil || len(withdrawn.Entries) != 0 || withdrawn.Digest == catalog.Digest {
		t.Fatal("withdrawal not reflected", err)
	}
	if _, err = client.Source(ctx, catalog.Entries[0]); err == nil {
		t.Fatal("withdrawn version still downloadable")
	}
	existing, err := consumerRepo.GetProtocolRelease(ctx, "consumer", "market-demo", "1.0.0")
	if err != nil || existing.Artifact["sha256"] != installed.Artifact["sha256"] {
		t.Fatal("withdrawal damaged installed version")
	}
	t.Run("browser", func(t *testing.T) {
		if os.Getenv("IOT_TEST_BROWSER") == "" {
			t.Skip("Chrome not configured")
		}
		upload("1.1.0")
		assets := http.FileServer(http.Dir(filepath.Join("..", "..", "iot_front", "dist")))
		browserServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				publisher.Handler().ServeHTTP(w, r)
			} else {
				assets.ServeHTTP(w, r)
			}
		}))
		defer browserServer.Close()
		author, _ := publisher.auth.Issue("browser-publisher", "tenant", "admin", nil, time.Minute)
		reviewer, _ := publisher.auth.Issue("browser-reviewer", "tenant", "admin", nil, time.Minute)
		command := exec.CommandContext(ctx, "node", filepath.Join("..", "..", "iot_front", "tests", "browser", "market-check.mjs"))
		command.Env = append(os.Environ(), "IOT_TEST_ORIGIN="+browserServer.URL, "IOT_TEST_TOKEN="+author, "IOT_TEST_REVIEW_TOKEN="+reviewer)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("browser: %v %s", err, output)
		}
		t.Log(string(output))
		entry, err := repo.GetProtocolMarket(ctx, "tenant", "market-demo", "1.1.0")
		if err != nil || entry.Status != "APPROVED" || entry.Reviews[0].Actor != "browser-reviewer" {
			t.Fatal("browser independent review missing", err)
		}
		signed, err := client.Fetch(ctx)
		if err != nil || len(signed.Entries) != 1 || signed.Entries[0].Version != "1.1.0" {
			t.Fatal("browser approval not in signed catalog", err)
		}
	})
}
