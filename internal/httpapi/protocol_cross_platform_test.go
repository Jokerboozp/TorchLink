package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/edgeagent"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
	"iot-platform/internal/parser"
)

const crossPlatformWorker = `package main
import("os";"encoding/json";"encoding/hex";"runtime")
func main(){var q struct{Operation,Data string};json.NewDecoder(os.Stdin).Decode(&q);out:=map[string]any{};if q.Operation=="ingress"{b,_:=hex.DecodeString(q.Data);if len(b)<2{out["needMore"]=true}else{out["consumed"]=2;out["deviceId"]="device";out["reply"]="AC"}}else{temperature:=42;_ = runtime.GOOS;out["standardMessage"]=map[string]any{"messageType":"PROPERTY_REPORT","properties":map[string]any{"temperature":temperature}}};json.NewEncoder(os.Stdout).Encode(out)}
`

func TestCrossPlatformSourceEdgeActualExecution(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	root := t.TempDir()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(root)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(root), log)
	if err := engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.DataDir = root
	api := New(cfg, engine, metrics.New(), log)
	assets := http.FileServer(http.Dir(filepath.Join("..", "..", "iot_front", "dist")))
	var downloadsBlocked atomic.Bool
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if downloadsBlocked.Load() && (strings.HasSuffix(r.URL.Path, "/artifact") || strings.HasSuffix(r.URL.Path, "/samples")) {
				w.WriteHeader(503)
				return
			}
			api.Handler().ServeHTTP(w, r)
		} else {
			assets.ServeHTTP(w, r)
		}
	}))
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	server.Listener = listener
	server.Start()
	defer server.Close()
	base := "http://127.0.0.1:" + strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	token, _ := api.auth.Issue("publisher", "tenant", "operator", nil, time.Hour)
	for _, err := range []error{repo.SaveProduct(ctx, model.Product{TenantID: "tenant", ID: "product", Name: "Cross platform", Status: "ENABLED"}), repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "tenant", ProductID: "product", ID: "device", Status: "ENABLED"}), repo.SaveEdgeNode(ctx, model.EdgeNode{TenantID: "tenant", ID: "edge", Status: "ENABLED"}), repo.SetEdgeCredential(ctx, "tenant", "edge", onboarding.Hash("cross-test-secret"))} {
		if err != nil {
			t.Fatal(err)
		}
	}
	browserSource := filepath.Join(t.TempDir(), "source.zip")
	upload := func(version, code, targets string, want int) model.ProtocolRelease {
		t.Helper()
		var project bytes.Buffer
		z := zip.NewWriter(&project)
		metadata := fmt.Sprintf(`{"id":"cross","version":%q,"runtime":"go-protocol-v2","transport":"TCP","payloadFormat":"hex","capabilities":["decode","ingress"]}`, version)
		for name, content := range map[string]string{"protocol.json": metadata, "main.go": code, "samples/cases.json": `[{"input":{"payload":"0102"},"expectedMessageType":"PROPERTY_REPORT","expectedProperties":{"temperature":42}}]`, "samples/operations.json": `[{"name":"frame","request":{"operation":"ingress","data":"0102"},"expected":{"consumed":2,"deviceId":"device","reply":"AC"}}]`} {
			f, _ := z.Create(name)
			io.WriteString(f, content)
		}
		if version == "target-fail" {
			f, _ := z.Create("broken_linux.go")
			io.WriteString(f, "//go:build linux\n\npackage main\nvar invalid=doesNotExist\n")
		}
		z.Close()
		if version == "1.0.0" {
			if err := os.WriteFile(browserSource, project.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
		}
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		file, _ := form.CreateFormFile("file", "project.zip")
		file.Write(project.Bytes())
		form.WriteField("publish", "true")
		form.WriteField("productId", "product")
		form.WriteField("targetPlatforms", targets)
		form.Close()
		req := httptest.NewRequest("POST", "/api/v2/protocols/cross/source-releases", &body)
		req.Header.Set("Content-Type", form.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		api.Handler().ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("upload %s: %d %s", version, w.Code, w.Body.String())
		}
		release, _ := repo.GetProtocolRelease(ctx, "tenant", "cross", version)
		return release
	}
	upload("bad", crossPlatformWorker, `["unrecognized-target"]`, 422)
	release := upload("1.0.0", crossPlatformWorker, `["linux-arm64"]`, 201)
	native := runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS != "linux" {
		upload("target-fail", crossPlatformWorker, `["linux-arm64"]`, 422)
		if _, err := repo.GetProtocolRelease(ctx, "tenant", "cross", "target-fail"); err == nil {
			t.Fatal("failed target published")
		}
		binding, _ := repo.GetProductProtocolBinding(ctx, "tenant", "product")
		if binding.Version != "1.0.0" {
			t.Fatal("failed target switched binding")
		}
	}
	selected, err := model.SelectProtocolArtifact(release.Artifact, "linux/arm64")
	if err != nil {
		t.Fatal(err)
	}
	if native != "linux-arm64" && (selected["validation"] != "COMPILED" || selected["testCases"] != float64(0)) {
		t.Fatal("foreign build incorrectly marked executed", selected)
	}
	nodeCall := func(path, secret string, want int) []byte {
		t.Helper()
		req, _ := http.NewRequestWithContext(ctx, "GET", base+"/api/v1/edge/tenant/edge/protocols/cross/1.0.0/"+path, nil)
		req.Header.Set("X-Edge-Secret", secret)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != want {
			t.Fatalf("node download %d want %d: %s", resp.StatusCode, want, data)
		}
		return data
	}
	// The persisted release is real publication output, including hyphen notation.
	portListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := portListener.Addr().(*net.TCPAddr).Port
	portListener.Close()
	profile := model.DeviceAccessProfile{TenantID: "tenant", ID: "cross-profile", ProductID: "product", DeviceID: "device", ProtocolID: "cross", ProtocolVersion: "1.0.0", Mode: "listener", Network: "tcp", Host: "127.0.0.1", Port: port, Enabled: true, EdgeNodeID: "edge", TimeoutMs: 2000}
	if err = repo.SaveDeviceAccessProfile(ctx, profile); err != nil {
		t.Fatal(err)
	}
	nodeCall("artifact?platform=linux-arm64", "wrong", 401)
	nodeCall("artifact?platform=linux-arm64&platform=darwin-arm64", "cross-test-secret", 422)
	nodeCall("artifact?platform=unknown", "cross-test-secret", 409)
	foreign := nodeCall("artifact?platform=linux-arm64", "cross-test-secret", 200)
	if !protocolDownloadHashMatchesV2(foreign, selected["sha256"].(string)) {
		t.Fatal("wrong selected binary")
	}
	nodeCall("samples", "wrong", 401)
	samples := nodeCall("samples", "cross-test-secret", 200)
	if !protocolDownloadHashMatchesV2(samples, release.Artifact["samplesSha256"].(string)) {
		t.Fatal("wrong suite")
	}
	samplePath := filepath.Join(root, release.Artifact["samplesPath"].(string))
	if err = os.WriteFile(samplePath, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	nodeCall("samples", "cross-test-secret", 409)
	if err = os.WriteFile(samplePath, samples, 0600); err != nil {
		t.Fatal(err)
	}
	nodeDir := t.TempDir()
	runLocal := func(t *testing.T) {
		nodeCtx, stop := context.WithCancel(ctx)
		defer stop()
		agent, err := edgeagent.New(edgeagent.Options{URL: base, TenantID: "tenant", NodeID: "edge", Secret: "cross-test-secret", DataDir: nodeDir, AllowedCIDRs: []string{"127.0.0.0/8"}, AllowInsecureHTTP: true, AllowGoWorkers: true, AllowedListenAddresses: []string{"127.0.0.1"}}, log)
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- agent.Run(nodeCtx) }()
		defer func() { stop(); <-done; agent.Close() }()
		previous, _ := repo.GetLatestMessage(ctx, "tenant", "device")
		assertCrossPlatformWire(t, ctx, net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		awaitCrossPlatformReport(t, ctx, repo, previous.RawMessageID)
	}
	t.Run("native-source-to-edge", runLocal)
	t.Run("restart-with-cached-suite", func(t *testing.T) { downloadsBlocked.Store(true); defer downloadsBlocked.Store(false); runLocal(t) })
	t.Run("linux-arm64-process", func(t *testing.T) {
		commandJSON, shared := os.Getenv("IOT_TEST_EDGE_PROCESS_COMMAND"), os.Getenv("IOT_TEST_EDGE_SHARED_DIR")
		if commandJSON == "" || shared == "" {
			t.Skip("explicit shared directory and Linux ARM64 process runner not configured")
		}
		var command []string
		if json.Unmarshal([]byte(commandJSON), &command) != nil || len(command) == 0 {
			t.Fatal("invalid process runner")
		}
		work, err := os.MkdirTemp(shared, "cross-edge-")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(work)
		binary := filepath.Join(work, "iot-edge-agent")
		build := exec.CommandContext(ctx, "go", "build", "-o", binary, "./cmd/iot-edge-agent")
		build.Dir = filepath.Join("..", "..")
		build.Env = append(os.Environ(), "GOOS=linux", "GOARCH=arm64", "CGO_ENABLED=0")
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build agent: %v %s", err, out)
		}
		profile.Host = "127.0.0.1"
		if err = repo.SaveDeviceAccessProfile(ctx, profile); err != nil {
			t.Fatal(err)
		}
		env := fmt.Sprintf("IOT_EDGE_PLATFORM_URL=http://host.orb.internal:%d\nIOT_EDGE_TENANT_ID=tenant\nIOT_EDGE_NODE_ID=edge\nIOT_EDGE_SECRET=cross-test-secret\nIOT_EDGE_DATA_DIR=%s\nIOT_EDGE_ALLOWED_CIDRS=127.0.0.0/8\nIOT_EDGE_ALLOW_HTTP=true\nIOT_EDGE_ALLOW_GO_WORKERS=true\nIOT_EDGE_LISTEN_ADDRESSES=127.0.0.1\n", listener.Addr().(*net.TCPAddr).Port, filepath.Join(work, "node"))
		envPath := filepath.Join(work, "edge.env")
		if err = os.WriteFile(envPath, []byte(env), 0600); err != nil {
			t.Fatal(err)
		}
		nodeCtx, stop := context.WithCancel(ctx)
		defer stop()
		args := append(append([]string{}, command[1:]...), binary, "--env-file", envPath)
		proc := exec.CommandContext(nodeCtx, command[0], args...)
		var output bytes.Buffer
		proc.Stdout, proc.Stderr = &output, &output
		if err = proc.Start(); err != nil {
			t.Fatal(err)
		}
		defer func() {
			stop()
			proc.Wait()
			t.Log("Linux process output:", output.String())
			h, _ := repo.GetEdgeHeartbeat(ctx, "tenant", "edge")
			t.Logf("Last heartbeat: %+v", h)
		}()
		remoteWire := func() {
			t.Helper()
			probe := fmt.Sprintf("import socket,time\nend=time.monotonic()+20\nwhile True:\n try:\n  s=socket.create_connection(('127.0.0.1',%d),1);s.settimeout(2);s.sendall(bytes([1,2]));r=s.recv(1);s.close();assert r==bytes([172]);print('AC');break\n except Exception:\n  if time.monotonic()>end: raise\n  time.sleep(.1)\n", port)
			args := append(append([]string{}, command[1:]...), "python3", "-c", probe)
			request := exec.CommandContext(nodeCtx, command[0], args...)
			result, err := request.CombinedOutput()
			if err != nil {
				t.Fatalf("Linux loopback device: %v %s", err, result)
			}
		}
		previous, _ := repo.GetLatestMessage(ctx, "tenant", "device")
		remoteWire()
		awaitCrossPlatformReport(t, ctx, repo, previous.RawMessageID)
		// Destination-specific failure passes host validation, but must never replace
		// the already-running Linux worker. This is an actual conditional Go build.
		broken := strings.Replace(crossPlatformWorker, "temperature:=42;", "temperature:=42;if runtime.GOOS==\"linux\" {temperature=99};", 1)
		if runtime.GOOS != "linux" {
			upload("1.1.0", broken, `["linux-arm64"]`, 201)
			deadline := time.Now().Add(20 * time.Second)
			for {
				h, _ := repo.GetEdgeHeartbeat(ctx, "tenant", "edge")
				if strings.Contains(h.LastError, "samples failed") {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("destination failure not reported", h.LastError)
				}
				time.Sleep(100 * time.Millisecond)
			}
			previous, _ := repo.GetLatestMessage(ctx, "tenant", "device")
			remoteWire()
			awaitCrossPlatformReport(t, ctx, repo, previous.RawMessageID)
			latest, _ := repo.GetLatestMessage(ctx, "tenant", "device")
			rawIndex, err := repo.GetRawIndex(ctx, "tenant", latest.RawMessageID)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := engine.GetRaw(ctx, rawIndex)
			if err != nil || raw.ProtocolVersion != "1.0.0" {
				t.Fatal("old Linux frame snapshot lost", raw.ProtocolVersion, err)
			}
			h, _ := repo.GetEdgeHeartbeat(ctx, "tenant", "edge")
			if h.ConfigRevision == "" {
				t.Fatal("previous configuration lost")
			}
		}
	})
	t.Run("browser", func(t *testing.T) {
		if os.Getenv("IOT_TEST_BROWSER") == "" {
			t.Skip("Chrome not configured")
		}
		token, _ := api.auth.Issue("browser-test", "tenant", "admin", nil, time.Minute*2)
		command := exec.CommandContext(ctx, "node", filepath.Join("..", "..", "iot_front", "tests", "browser", "cross-platform-check.mjs"))
		command.Env = append(os.Environ(), "IOT_TEST_ORIGIN="+base, "IOT_TEST_TOKEN="+token, "IOT_TEST_SOURCE_ZIP="+browserSource)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("browser: %v %s", err, output)
		}
		t.Log(string(output))
		release, err := repo.GetProtocolRelease(ctx, "tenant", "cross", "2.0.0")
		if err != nil {
			t.Fatal(err)
		}
		variant, err := model.SelectProtocolArtifact(release.Artifact, "windows-arm64")
		if err != nil || variant["validation"] != "COMPILED" {
			t.Fatal("browser target missing", err)
		}
	})
}

func assertCrossPlatformWire(t *testing.T, ctx context.Context, address string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		connection, err := net.DialTimeout("tcp", address, 200*time.Millisecond)
		if err == nil {
			connection.SetDeadline(time.Now().Add(2 * time.Second))
			connection.Write([]byte{1, 2})
			reply := make([]byte, 1)
			_, err = io.ReadFull(connection, reply)
			connection.Close()
			if err == nil && reply[0] == 0xac {
				return
			}
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			t.Fatalf("actual listener did not acknowledge at %s: %v", address, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
func awaitCrossPlatformReport(t *testing.T, ctx context.Context, repo *memory.Repository, previous ...string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		message, err := repo.GetLatestMessage(ctx, "tenant", "device")
		if err == nil && message.Properties["temperature"] == float64(42) && (len(previous) == 0 || message.RawMessageID != previous[0]) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("actual Raw/Standard missing", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
