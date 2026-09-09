package httpapi

import (
	"context"
	"io"
	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/connector"
	"iot-platform/internal/core"
	"iot-platform/internal/edgeagent"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
	"iot-platform/internal/parser"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestEdgeAgentDurableModbusChain(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	sim, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer sim.Close()
	go func() {
		for {
			c, e := sim.Accept()
			if e != nil {
				return
			}
			_ = c.SetDeadline(time.Now().Add(time.Second))
			request := make([]byte, 12)
			if _, e = io.ReadFull(c, request); e == nil {
				_, _ = c.Write([]byte{request[0], request[1], 0, 0, 0, 5, request[6], 3, 2, 0, 42})
			}
			c.Close()
		}
	}()
	repo := memory.NewRepository()
	root := t.TempDir()
	archive, err := local.NewArchive(root)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	parsers := parser.NewPlatformRegistry(root)
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parsers, log)
	if err = engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	svc := onboarding.New(repo, parsers, root, []string{"127.0.0.0/8"})
	q := onboarding.Request{ProductID: "product", ProductName: "边缘产品", DeviceID: "device", Name: "边缘设备", Type: connector.ModbusTCP, PollIntervalSec: 1, Profile: model.DeviceAccessProfile{Host: "127.0.0.1", Port: sim.Addr().(*net.TCPAddr).Port, UnitID: 1, TimeoutMs: 500}, PointTableCSV: "name,functionCode,address,addressNotation,dataType,scale\ntemperature,3,0,zero_based,uint16,1\n"}
	preview, err := svc.Test(ctx, "tenant", q)
	if err != nil || !preview.Success {
		t.Fatal("preview", err)
	}
	q.TestToken = preview.TestToken
	created, err := svc.Create(ctx, "tenant", q)
	if err != nil {
		t.Fatal(err)
	}
	node := model.EdgeNode{TenantID: "tenant", ID: "node", Name: "现场节点", Status: "ENABLED"}
	if err = repo.SaveEdgeNode(ctx, node); err != nil {
		t.Fatal(err)
	}
	secret := "edge-only-test-secret"
	if err = repo.SetEdgeCredential(ctx, "tenant", "node", onboarding.Hash(secret)); err != nil {
		t.Fatal(err)
	}
	profile := *created.Connector.Profile
	profile.EdgeNodeID = "node"
	if err = repo.SaveDeviceAccessProfile(ctx, profile); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.DataDir = root
	server := New(cfg, engine, metrics.New(), log)
	blocked := atomic.Bool{}
	blocked.Store(true)
	assets := http.FileServer(http.Dir(filepath.Join("..", "..", "iot_front", "dist")))
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/edge/tenant/node/raw" && blocked.Load() {
			w.WriteHeader(503)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			server.Handler().ServeHTTP(w, r)
		} else {
			assets.ServeHTTP(w, r)
		}
	}))
	defer upstream.Close()
	for _, path := range []string{"/api/v1/edge/tenant/node/config", "/api/v1/edge/other/node/config"} {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("X-Edge-Secret", "wrong")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != 401 {
			t.Fatal("node authentication bypass", w.Code)
		}
	}
	options := edgeagent.Options{URL: upstream.URL, TenantID: "tenant", NodeID: "node", Secret: secret, DataDir: t.TempDir(), AllowedCIDRs: []string{"127.0.0.0/8"}, AllowInsecureHTTP: true}
	agent, err := edgeagent.New(options, log)
	if err != nil {
		t.Fatal(err)
	}
	firstCtx, stopFirst := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- agent.Run(firstCtx) }()
	wait := func(check func() bool) {
		t.Helper()
		for !check() {
			select {
			case <-ctx.Done():
				t.Fatal("edge chain timeout")
			case <-time.After(20 * time.Millisecond):
			}
		}
	}
	wait(func() bool { return agent.Pending() > 0 })
	stopFirst()
	<-done
	if err = agent.Close(); err != nil {
		t.Fatal(err)
	}
	disk, err := edgeagent.OpenQueue(filepath.Join(options.DataDir, "outbox"), 64<<20, 10000)
	if err != nil {
		t.Fatal(err)
	}
	pending, ok, err := disk.Next()
	if err != nil || !ok {
		t.Fatal("pending raw lost after restart", err)
	}
	disk.Close()
	// A corrupt record sorts first, but must not starve the valid persisted raw.
	corrupt := filepath.Join(options.DataDir, "outbox", strings.Repeat("0", 64)+".json")
	if err := os.WriteFile(corrupt, []byte("{damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.GetRawIndex(ctx, "tenant", pending.MessageID); err == nil {
		t.Fatal("failed delivery was reported as archived")
	}
	blocked.Store(false)
	restarted, err := edgeagent.New(options, log)
	if err != nil {
		t.Fatal(err)
	}
	secondCtx, stopSecond := context.WithCancel(ctx)
	defer stopSecond()
	secondDone := make(chan error, 1)
	go func() { secondDone <- restarted.Run(secondCtx) }()
	defer func() { stopSecond(); <-secondDone; restarted.Close() }()
	wait(func() bool {
		_, err := repo.GetStandardMessageByRaw(ctx, "tenant", pending.MessageID)
		return err == nil
	})
	parsed, err := repo.GetStandardMessageByRaw(ctx, "tenant", pending.MessageID)
	if err != nil || parsed.Properties["temperature"] != float64(42) {
		t.Fatal("recovered raw was not parsed", err)
	}
	wait(func() bool { return restarted.Pending() == 0 })
	wait(func() bool {
		h, err := repo.GetEdgeHeartbeat(ctx, "tenant", "node")
		return err == nil && h.CorruptDepth == 1
	})
	if data, err := os.ReadFile(corrupt + ".corrupt"); err != nil || string(data) != "{damaged" {
		t.Fatal("corrupt evidence lost", err)
	}

	t.Run("browser", func(t *testing.T) {
		if os.Getenv("IOT_TEST_BROWSER") == "" {
			t.Skip("Chrome not configured")
		}
		token, _ := server.auth.Issue("browser-test", "tenant", "admin", nil, time.Minute)
		command := exec.CommandContext(ctx, "node", filepath.Join("..", "..", "iot_front", "tests", "browser", "edge-recovery-check.mjs"))
		command.Env = append(os.Environ(), "IOT_TEST_ORIGIN="+upstream.URL, "IOT_TEST_TOKEN="+token)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("browser: %v %s", err, output)
		} else {
			t.Log(string(output))
		}
	})
	// Explicit credential revocation is checked on every subsequent request.
	if err = repo.SetEdgeCredential(ctx, "tenant", "node", ""); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/api/v1/edge/tenant/node/config", nil)
	req.Header.Set("X-Edge-Secret", secret)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatal("revoked node credential accepted", w.Code)
	}
}
