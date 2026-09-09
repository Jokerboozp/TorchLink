package httpapi

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/edgeagent"
	"iot-platform/internal/edgeupgrade"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
	"iot-platform/internal/parser"
	"iot-platform/internal/protocolcatalog"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestEdgeProgramAuthenticatedProcessUpgradeAndRollback(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("Go compiler unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	root := t.TempDir()
	build := func(version string) string {
		t.Helper()
		path := filepath.Join(root, version)
		if runtime.GOOS == "windows" {
			path += ".exe"
		}
		command := exec.CommandContext(ctx, "go", "build", "-ldflags", "-s -w -X iot-platform/internal/edgeagent.Version="+version, "-o", path, "./cmd/iot-edge-agent")
		command.Dir = filepath.Join("..", "..")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("real agent build: %v %s", err, output)
		}
		return path
	}
	v1, v2 := build("v1"), build("v2")
	newBinary, err := os.ReadFile(v2)
	if err != nil {
		t.Fatal(err)
	}
	if len(newBinary) > protocolcatalog.MaxSource {
		t.Fatal("native agent exceeds signed artifact size limit")
	}
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	var signed []byte
	var tampered atomic.Bool
	var downloadDown atomic.Bool
	var interruptedDownloads atomic.Int32
	catalog := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/agent" && downloadDown.Load() {
			interruptedDownloads.Add(1)
			w.WriteHeader(503)
			return
		}
		if r.URL.Path == "/catalog.json" {
			w.Write(signed)
			return
		}
		if tampered.Load() {
			w.Write([]byte("corrupt binary"))
			return
		}
		w.Write(newBinary)
	}))
	defer catalog.Close()
	hash := sha256.Sum256(newBinary)
	hashText := hex.EncodeToString(hash[:])
	payload := protocolcatalog.Payload{IssuedAt: time.Now().UnixMilli(), ExpiresAt: time.Now().Add(time.Hour).UnixMilli()}
	for _, version := range []string{"v2", "v3"} {
		payload.Entries = append(payload.Entries, protocolcatalog.Entry{Kind: "edge-agent", Platform: runtime.GOOS + "/" + runtime.GOARCH, ID: "iot-edge-agent", Version: version, Name: "Edge Agent", SourceURL: catalog.URL + "/agent", Size: int64(len(newBinary)), SHA256: hashText})
	}
	data, _ := json.Marshal(payload)
	signed, _ = json.Marshal(protocolcatalog.Envelope{KeyID: "test", Payload: base64.StdEncoding.EncodeToString(data), Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(private, data))})
	ca := filepath.Join(root, "ca.pem")
	if err = os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: catalog.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	policy, _ := json.Marshal(protocolcatalog.Policy{URL: catalog.URL + "/catalog.json", PublicKeys: map[string]string{"test": base64.StdEncoding.EncodeToString(public)}, CAFile: ca})
	policyPath := filepath.Join(root, "policy.json")
	if err = os.WriteFile(policyPath, policy, 0600); err != nil {
		t.Fatal(err)
	}
	repo := memory.NewRepository()
	archive, err := local.NewArchive(filepath.Join(root, "platform"))
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(root), log)
	cfg := config.Load()
	cfg.DataDir = root
	api := New(cfg, engine, metrics.New(), log)
	node := model.EdgeNode{TenantID: "t", ID: "node", Name: "升级测试节点", Status: "ENABLED"}
	if err = repo.SaveEdgeNode(ctx, node); err != nil {
		t.Fatal(err)
	}
	const secret = "isolated-upgrade-test-secret"
	if err = repo.SetEdgeCredential(ctx, "t", "node", onboarding.Hash(secret)); err != nil {
		t.Fatal(err)
	}
	assets := http.FileServer(http.Dir(filepath.Join("..", "..", "iot_front", "dist")))
	var controlDown atomic.Bool
	var configDown atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/raw") || (controlDown.Load() && strings.HasSuffix(r.URL.Path, "/program")) || (configDown.Load() && strings.HasSuffix(r.URL.Path, "/config")) {
			w.WriteHeader(503)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			api.Handler().ServeHTTP(w, r)
		} else {
			assets.ServeHTTP(w, r)
		}
	}))
	defer server.Close()
	call := func(tenant, role string, body any) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		r := httptest.NewRequest("POST", "/api/v1/edge-nodes/node/program", bytes.NewReader(b))
		token, _ := api.auth.Issue("test", tenant, role, nil, time.Minute)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		api.Handler().ServeHTTP(w, r)
		return w
	}
	input := map[string]any{"version": "v2", "expectedGeneration": 0, "confirmed": true}
	for _, identity := range [][3]string{{"t", "viewer", "403"}, {"other", "admin", "404"}} {
		if w := call(identity[0], identity[1], input); fmt.Sprint(w.Code) != identity[2] {
			t.Fatal("target authorization", w.Code)
		}
	}
	input["confirmed"] = false
	if w := call("t", "admin", input); w.Code != 422 {
		t.Fatal("confirmation bypass", w.Code)
	}
	input["confirmed"] = true
	agentData := filepath.Join(root, "agent-data")
	queue, err := edgeagent.OpenQueue(filepath.Join(agentData, "outbox"), 64<<20, 10000)
	if err != nil {
		t.Fatal(err)
	}
	if err = queue.Put(model.RawMessage{TenantID: "t", MessageID: "durable-before-upgrade", Payload: json.RawMessage(`{"value":42}`)}); err != nil {
		t.Fatal(err)
	}
	queue.Close()
	env := filepath.Join(root, "agent.env")
	body := fmt.Sprintf("IOT_EDGE_PLATFORM_URL=%s\nIOT_EDGE_TENANT_ID=t\nIOT_EDGE_NODE_ID=node\nIOT_EDGE_SECRET=%s\nIOT_EDGE_DATA_DIR=%s\nIOT_EDGE_ALLOWED_CIDRS=127.0.0.0/8\nIOT_EDGE_ALLOW_HTTP=true\n", server.URL, secret, agentData)
	if err = os.WriteFile(env, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	options := edgeupgrade.Options{URL: server.URL, TenantID: "t", NodeID: "node", Secret: secret, DataDir: filepath.Join(root, "programs"), BootstrapBinary: v1, AgentEnvFile: env, CatalogPolicy: policyPath, AllowHTTP: true, PollInterval: 50 * time.Millisecond, ReadyTimeout: 2 * time.Second}
	t.Run("cached-configuration-is-not-readiness", func(t *testing.T) {
		req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/api/v1/edge/t/node/config", nil)
		req.Header.Set("X-Edge-Secret", secret)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		cached, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil || res.StatusCode != 200 {
			t.Fatal("authenticated configuration", res.StatusCode, err)
		}
		if err = os.WriteFile(filepath.Join(agentData, "configuration.json"), cached, 0600); err != nil {
			t.Fatal(err)
		}
		configDown.Store(true)
		defer configDown.Store(false)
		isolated := options
		isolated.DataDir = filepath.Join(root, "readiness-test")
		isolated.ReadyTimeout = 600 * time.Millisecond
		probe, err := edgeupgrade.New(isolated)
		if err != nil {
			t.Fatal(err)
		}
		defer probe.Close()
		probeCtx, stop := context.WithTimeout(ctx, 3*time.Second)
		defer stop()
		err = probe.Run(probeCtx)
		if err == nil || probeCtx.Err() != nil || !strings.Contains(err.Error(), "did not confirm configuration") {
			t.Fatal("cached configuration produced false ready", err)
		}
	})
	launcher, err := edgeupgrade.New(options)
	if err != nil {
		t.Fatal(err)
	}
	if duplicate, err := edgeupgrade.New(options); err == nil {
		duplicate.Close()
		t.Fatal("second launcher acquired same process state")
	}
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- launcher.Run(runCtx) }()
	defer func() {
		stop()
		select {
		case <-done:
		case <-time.After(8 * time.Second):
			t.Error("launcher did not stop")
		}
		launcher.Close()
	}()
	var minimumSeen int64
	wait := func(phase, version string, generation int64) {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			v, _ := repo.GetEdgeProgram(ctx, "t", "node")
			if v.Status.Phase == phase && v.Status.Version == version && v.Status.Generation == generation && v.Status.LastSeenAt >= minimumSeen {
				return
			}
			select {
			case err := <-done:
				done <- err
				t.Fatalf("launcher exited: %v, state %+v", err, v)
			case <-time.After(20 * time.Millisecond):
			}
		}
		v, _ := repo.GetEdgeProgram(ctx, "t", "node")
		t.Fatalf("program status timeout: %+v", v)
	}
	wait("RUNNING", "v1", 0)
	heartbeat, err := repo.GetEdgeHeartbeat(ctx, "t", "node")
	if err != nil || heartbeat.Version != "v1" || heartbeat.QueueDepth != 1 {
		t.Fatal("real child readiness/queue", heartbeat, err)
	}
	tampered.Store(true)
	if w := call("t", "admin", input); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	wait("FAILED", "v1", 1)
	if w := call("t", "admin", input); w.Code != 409 {
		t.Fatal("stale update accepted", w.Code)
	}
	tampered.Store(false)
	downloadDown.Store(true)
	input["expectedGeneration"] = 1
	if w := call("t", "admin", input); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	deadline := time.Now().Add(5 * time.Second)
	for interruptedDownloads.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if interruptedDownloads.Load() == 0 {
		t.Fatal("network interruption not exercised")
	}
	wait("STAGING", "v1", 2)
	current, _ := repo.GetEdgeHeartbeat(ctx, "t", "node")
	if current.Version != "v1" {
		t.Fatal("working agent stopped during download failure")
	}
	downloadDown.Store(false)
	// Same target generation must recover automatically, without reissuing it.
	wait("RUNNING", "v2", 2)
	heartbeat, err = repo.GetEdgeHeartbeat(ctx, "t", "node")
	if err != nil || heartbeat.Version != "v2" || heartbeat.QueueDepth != 1 {
		t.Fatal("candidate heartbeat/queue", heartbeat, err)
	}
	// v3 deliberately contains the real v2 executable: it authenticates but
	// cannot claim the requested version, so startup must roll back to v2.
	input["version"], input["expectedGeneration"] = "v3", 2
	if w := call("t", "admin", input); w.Code != 200 {
		t.Fatal(w.Code)
	}
	wait("ROLLED_BACK", "v2", 3)
	controlDown.Store(true)
	time.Sleep(180 * time.Millisecond)
	controlDown.Store(false)
	wait("ROLLED_BACK", "v2", 3)
	// Restart the independent launcher from its real durable journal. The
	// failed target generation must not execute again or forget the actual v2.
	stop()
	<-done
	launcher.Close()
	previous, _ := repo.GetEdgeProgram(ctx, "t", "node")
	minimumSeen = previous.Status.LastSeenAt + 1
	launcher, err = edgeupgrade.New(options)
	if err != nil {
		t.Fatal(err)
	}
	runCtx, stop = context.WithCancel(ctx)
	done = make(chan error, 1)
	go func() { done <- launcher.Run(runCtx) }()
	wait("ROLLED_BACK", "v2", 3)
	// Tampering with an installed program after shutdown must fail the local
	// journal hash check and resume the retained v1, not execute the new bytes.
	stop()
	<-done
	launcher.Close()
	installed := filepath.Join(options.DataDir, "agent-"+hashText)
	if runtime.GOOS == "windows" {
		installed += ".exe"
	}
	if err = os.WriteFile(installed, []byte("corrupt installed program"), 0700); err != nil {
		t.Fatal(err)
	}
	previous, _ = repo.GetEdgeProgram(ctx, "t", "node")
	minimumSeen = previous.Status.LastSeenAt + 1
	launcher, err = edgeupgrade.New(options)
	if err != nil {
		t.Fatal(err)
	}
	runCtx, stop = context.WithCancel(ctx)
	done = make(chan error, 1)
	go func() { done <- launcher.Run(runCtx) }()
	wait("ROLLED_BACK", "v1", 3)
	t.Run("browser", func(t *testing.T) {
		if os.Getenv("IOT_TEST_BROWSER") == "" {
			t.Skip("IOT_TEST_BROWSER is not configured")
		}
		token, _ := api.auth.Issue("browser-test", "t", "admin", nil, time.Minute)
		command := exec.CommandContext(ctx, "node", filepath.Join("..", "..", "iot_front", "tests", "browser", "edge-program-check.mjs"))
		command.Env = append(os.Environ(), "IOT_TEST_ORIGIN="+server.URL, "IOT_TEST_TOKEN="+token)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("browser: %v %s", err, output)
		} else {
			t.Log(string(output))
		}
	})
	node.Status = "DISABLED"
	if err = repo.SaveEdgeNode(ctx, node); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, edgeupgrade.ErrUnauthorized) {
			t.Fatal("revocation", err)
		}
		done <- err
	case <-time.After(5 * time.Second):
		t.Fatal("revoked node kept running")
	}
	// The actual child has released its outbox lock, and the pending record
	// remains durable across both the upgrade and failed candidate rollback.
	reopened, err := edgeagent.OpenQueue(filepath.Join(agentData, "outbox"), 64<<20, 10000)
	if err != nil {
		t.Fatal("child still owns queue", err)
	}
	defer reopened.Close()
	raw, ok, err := reopened.Next()
	if err != nil || !ok || raw.MessageID != "durable-before-upgrade" {
		t.Fatal("durable record lost", raw, err)
	}
}
