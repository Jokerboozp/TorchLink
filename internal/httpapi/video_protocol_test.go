package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/edgeagent"
	"iot-platform/internal/fieldprotocol"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
	"iot-platform/internal/parser"
	"iot-platform/internal/testonvif"
	"log/slog"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEdgeONVIFMetadataWithAuthentication(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	camera := testonvif.Start(t, true, false)
	repo := memory.NewRepository()
	root := t.TempDir()
	archive, err := local.NewArchive(root)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(root), log)
	cfg := config.Load()
	cfg.DataDir = root
	server := New(cfg, engine, metrics.New(), log)
	upstream := httptest.NewServer(server.Handler())
	defer upstream.Close()
	if err := repo.SaveEdgeNode(ctx, model.EdgeNode{TenantID: "t", ID: "edge", Status: "ENABLED"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetEdgeCredential(ctx, "t", "edge", onboarding.Hash("test-node-secret")); err != nil {
		t.Fatal(err)
	}
	credentialPath := filepath.Join(t.TempDir(), "credentials.json")
	b, _ := json.Marshal(map[string]fieldprotocol.Credential{"camera": {Username: "operator", Password: testonvif.Password, TLSCAFile: camera.CAFile}})
	if err := os.WriteFile(credentialPath, b, 0600); err != nil {
		t.Fatal(err)
	}
	agent, err := edgeagent.New(edgeagent.Options{URL: upstream.URL, TenantID: "t", NodeID: "edge", Secret: "test-node-secret", DataDir: t.TempDir(), CredentialFile: credentialPath, AllowedCIDRs: []string{"127.0.0.0/8"}, AllowInsecureHTTP: true}, log)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- agent.Run(ctx) }()
	defer func() { cancel(); <-done; agent.Close() }()
	for {
		h, e := repo.GetEdgeHeartbeat(ctx, "t", "edge")
		if e == nil && h.LastSeenAt > 0 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
	p := model.DeviceAccessProfile{EdgeNodeID: "edge", Host: "127.0.0.1", Port: camera.Server.Listener.Addr().(*net.TCPAddr).Port, CredentialRef: "camera"}
	call := func(tenant, role string, profile model.DeviceAccessProfile) *httptest.ResponseRecorder {
		token, err := server.auth.Issue("tester", tenant, role, nil, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(profile)
		req := httptest.NewRequest("POST", "/api/v1/integrations/video/onvif/test", bytes.NewReader(b))
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		return w
	}
	if w := call("t", "viewer", p); w.Code != 403 {
		t.Fatalf("viewer: %d", w.Code)
	}
	if w := call("other", "operator", p); w.Code != 422 {
		t.Fatalf("foreign tenant: %d", w.Code)
	}
	bad := p
	bad.CredentialRef = "missing"
	if w := call("t", "operator", bad); w.Code != 422 {
		t.Fatalf("missing credential: %d", w.Code)
	}
	w := call("t", "operator", p)
	if w.Code != 200 {
		t.Fatalf("read: %d %s", w.Code, w.Body.String())
	}
	var result struct {
		Source   string
		Metadata map[string]any
	}
	if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Source != "edge-read" || result.Metadata["serialNumber"] != "CAM-42" {
		t.Fatalf("invalid metadata: %s", w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), []byte(testonvif.Password)) {
		t.Fatal("credential leaked")
	}
	if _, err := repo.GetManagedDevice(ctx, "t", "onvif-preview"); err == nil {
		t.Fatal("preview created device")
	}
}
