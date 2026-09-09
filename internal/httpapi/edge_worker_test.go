package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestEdgeWorkerDownloadTCPUDPAndVersionSwitch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
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
	var registrationBlocked atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if registrationBlocked.Load() && strings.HasSuffix(r.URL.Path, "/devices/register") {
			w.WriteHeader(503)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			api.Handler().ServeHTTP(w, r)
		} else {
			assets.ServeHTTP(w, r)
		}
	}))
	defer upstream.Close()
	source := filepath.Join(t.TempDir(), "worker.go")
	code := `package main
import("os";"encoding/json";"encoding/hex")
func main(){var q struct{Operation,Data string}; json.NewDecoder(os.Stdin).Decode(&q); out:=map[string]any{}; if q.Operation=="ingress" {b,_:=hex.DecodeString(q.Data); if len(b)<2{out["needMore"]=true}else{out["consumed"]=2;out["deviceId"]="device";if b[0]==0xD0 {out["deviceId"]="new-device"};if b[0]==0xD1 {out["deviceId"]="new-udp"};out["reply"]="AC"; if b[0]==0xB0 {out["correlationId"]="fixture-command"}}}else if q.Operation=="encode" {out["reply"]="CAFE";out["correlationId"]="fixture-command"}else{out["standardMessage"]=map[string]any{"messageType":"PROPERTY_REPORT","properties":map[string]any{"temperature":42}}};json.NewEncoder(os.Stdout).Encode(out)}
`
	if err := os.WriteFile(source, []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	worker := filepath.Join(t.TempDir(), "worker")
	if runtime.GOOS == "windows" {
		worker += ".exe"
	}
	if out, err := exec.CommandContext(ctx, "go", "build", "-o", worker, source).CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	data, err := os.ReadFile(worker)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	var firstPath string
	for _, version := range []string{"1.0.0", "1.1.0"} {
		relative := filepath.ToSlash(filepath.Join("protocol-releases", "tenant", "worker", version, "artifact"))
		path := filepath.Join(root, relative)
		if firstPath == "" {
			firstPath = path
		}
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0700); err != nil {
			t.Fatal(err)
		}
		artifact := map[string]any{"path": relative, "sha256": hex.EncodeToString(sum[:]), "platform": runtime.GOOS + "/" + runtime.GOARCH, "runtime": "go-protocol-v2"}
		if err := repo.CreateProtocolRelease(ctx, model.ProtocolRelease{TenantID: "tenant", ProtocolID: "worker", Version: version, Status: "PUBLISHED", Transport: "TCP_UDP", PayloadFormat: "hex", ParserType: parser.GoProtocolParserName, Capabilities: []string{"ingress", "decode", "encode"}, Artifact: artifact, Config: map[string]any{"artifact": artifact}}); err != nil {
			t.Fatal(err)
		}
	}
	for _, err := range []error{repo.SaveProduct(ctx, model.Product{TenantID: "tenant", ID: "product", Name: "现场协议产品", Status: "ENABLED"}), repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "tenant", ProductID: "product", ID: "device", Status: "ENABLED"}), repo.SaveEdgeNode(ctx, model.EdgeNode{TenantID: "tenant", ID: "edge", Name: "现场节点", Status: "ENABLED"}), repo.SetEdgeCredential(ctx, "tenant", "edge", onboarding.Hash("test-node-secret")), repo.SaveProductProtocolBinding(ctx, model.ProductProtocolBinding{TenantID: "tenant", ProductID: "product", ProtocolID: "worker", Version: "1.0.0"})} {
		if err != nil {
			t.Fatal(err)
		}
	}
	ports := map[string]int{}
	for _, network := range []string{"tcp", "udp"} {
		if network == "tcp" {
			l, e := net.Listen("tcp", "127.0.0.1:0")
			if e != nil {
				t.Fatal(e)
			}
			ports[network] = l.Addr().(*net.TCPAddr).Port
			l.Close()
		} else {
			l, e := net.ListenPacket("udp", "127.0.0.1:0")
			if e != nil {
				t.Fatal(e)
			}
			ports[network] = l.LocalAddr().(*net.UDPAddr).Port
			l.Close()
		}
		if err := repo.SaveDeviceAccessProfile(ctx, model.DeviceAccessProfile{TenantID: "tenant", ID: network, ProductID: "product", ProtocolID: "worker", ProtocolVersion: "1.0.0", Mode: "listener", Network: network, Host: "127.0.0.1", Port: ports[network], Enabled: true, AutoRegister: true, EdgeNodeID: "edge", TimeoutMs: 2000}); err != nil {
			t.Fatal(err)
		}
	}
	profile, err := repo.GetDeviceAccessProfile(ctx, "tenant", "tcp")
	if err != nil {
		t.Fatal(err)
	}
	preview, err := api.onboarding.Test(ctx, "tenant", onboarding.Request{ProductID: "product", DeviceID: "device", Name: "Worker device", Type: connector.TCP, Profile: profile, ExistingProfileID: profile.ID, Payload: json.RawMessage(`"0102"`)})
	if err != nil || !preview.Success || preview.Source != "sample" || strings.Contains(preview.Message, "本机端口可绑定") {
		t.Fatalf("edge preview: %+v %v", preview, err)
	}
	get := func(version, secret string) int {
		request, _ := http.NewRequestWithContext(ctx, "GET", upstream.URL+"/api/v1/edge/tenant/edge/protocols/worker/"+version+"/artifact", nil)
		request.Header.Set("X-Edge-Secret", secret)
		response, e := http.DefaultClient.Do(request)
		if e != nil {
			t.Fatal(e)
		}
		defer response.Body.Close()
		io.Copy(io.Discard, response.Body)
		return response.StatusCode
	}
	if status := get("1.0.0", "wrong"); status != 401 {
		t.Fatal("unauthenticated download", status)
	}
	if status := get("1.1.0", "test-node-secret"); status != 403 {
		t.Fatal("unassigned version download", status)
	}
	if err := os.WriteFile(firstPath, []byte("corrupt"), 0700); err != nil {
		t.Fatal(err)
	}
	if status := get("1.0.0", "test-node-secret"); status != 409 {
		t.Fatal("corrupt artifact download", status)
	}
	if err := os.WriteFile(firstPath, data, 0700); err != nil {
		t.Fatal(err)
	}
	agent, err := edgeagent.New(edgeagent.Options{URL: upstream.URL, TenantID: "tenant", NodeID: "edge", Secret: "test-node-secret", DataDir: t.TempDir(), AllowedCIDRs: []string{"127.0.0.0/8"}, AllowInsecureHTTP: true, AllowGoWorkers: true, AllowAutoRegister: true, AllowCommands: true, AllowedListenAddresses: []string{"127.0.0.1"}}, log)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- agent.Run(ctx) }()
	defer func() { cancel(); <-done; agent.Close() }()
	var tcp net.Conn
	for tcp == nil {
		tcp, err = net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(ports["tcp"])), 100*time.Millisecond)
		if err != nil {
			select {
			case <-ctx.Done():
				t.Fatal("listener not ready")
			case <-time.After(20 * time.Millisecond):
			}
		}
	}
	defer tcp.Close()
	tcp.SetDeadline(time.Now().Add(2 * time.Second))
	tcp.Write([]byte{1})
	time.Sleep(30 * time.Millisecond)
	tcp.Write([]byte{2, 3, 4})
	ack := make([]byte, 2)
	if _, err := io.ReadFull(tcp, ack); err != nil || !bytes.Equal(ack, []byte{0xac, 0xac}) {
		t.Fatalf("split and joined frames: %x %v", ack, err)
	}
	udp, err := net.Dial("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(ports["udp"])))
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	udp.SetDeadline(time.Now().Add(2 * time.Second))
	udp.Write([]byte{1, 2})
	if n, err := udp.Read(ack); err != nil || n != 1 || ack[0] != 0xac {
		t.Fatalf("UDP: %x %v", ack, err)
	}

	t.Run("automatic-registration", func(t *testing.T) {
		registrationBlocked.Store(true)
		first, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(ports["tcp"])), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		first.SetDeadline(time.Now().Add(time.Second))
		first.Write([]byte{0xD0, 2})
		reply := make([]byte, 1)
		if n, err := first.Read(reply); err == nil || n != 0 {
			t.Fatal("unconfirmed registration acknowledged", n, err)
		}
		first.Close()
		if _, err := repo.GetManagedDevice(ctx, "tenant", "new-device"); err == nil {
			t.Fatal("registration succeeded during platform failure")
		}
		registrationBlocked.Store(false)
		second, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(ports["tcp"])), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		defer second.Close()
		second.SetDeadline(time.Now().Add(3 * time.Second))
		second.Write([]byte{0xD0, 2})
		if _, err := io.ReadFull(second, reply); err != nil || reply[0] != 0xac {
			t.Fatal("confirmed automatic registration reply", reply, err)
		}
		registered, err := repo.GetManagedDevice(ctx, "tenant", "new-device")
		if err != nil || !registered.AutoRegistered || registered.ProductID != "product" || registered.SecretHash != "" || registered.AccessKey == "" {
			t.Fatal("registered inventory", registered, err)
		}
		third, err := net.Dial("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(ports["udp"])))
		if err != nil {
			t.Fatal(err)
		}
		defer third.Close()
		third.SetDeadline(time.Now().Add(3 * time.Second))
		third.Write([]byte{0xD1, 2})
		if _, err := third.Read(reply); err != nil || reply[0] != 0xac {
			t.Fatal("second auto device", reply, err)
		}
		for _, id := range []string{"new-device", "new-udp"} {
			for {
				m, e := repo.GetLatestMessage(ctx, "tenant", id)
				if e == nil && m.Properties["temperature"] == float64(42) {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal("auto-registered raw/standard missing")
				case <-time.After(20 * time.Millisecond):
				}
			}
		}
		// Registration authority is bound to node identity and the current profile.
		for _, tc := range []struct {
			node, secret, hash string
			status             int
		}{{"edge", "wrong", model.CommandProfileHash(profile), 401}, {"other", "test-node-secret", model.CommandProfileHash(profile), 401}, {"edge", "test-node-secret", "stale", 403}} {
			data, _ := json.Marshal(map[string]any{"profileId": "tcp", "deviceId": "forbidden", "configurationHash": tc.hash})
			req := httptest.NewRequest("POST", "/api/v1/edge/tenant/"+tc.node+"/devices/register", bytes.NewReader(data))
			req.Header.Set("X-Edge-Secret", tc.secret)
			w := httptest.NewRecorder()
			api.Handler().ServeHTTP(w, req)
			if w.Code != tc.status {
				t.Fatal("registration authorization", w.Code, w.Body.String())
			}
		}
	})
	callCommand := func(role, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("POST", "/api/v2/device-access-profiles/tcp/devices/device/commands", strings.NewReader(body))
		token, err := api.auth.Issue("command-test", "tenant", role, nil, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		api.Handler().ServeHTTP(w, r)
		return w
	}
	commandBody := `{"type":"test","requestId":"edge-real-command","confirmed":true}`
	if w := callCommand("viewer", commandBody); w.Code != 403 {
		t.Fatal("viewer command", w.Code)
	}
	if w := callCommand("operator", `{"type":"test","requestId":"unconfirmed"}`); w.Code != 422 {
		t.Fatal("missing confirmation", w.Code)
	}
	if w := callCommand("operator", commandBody); w.Code != 202 {
		t.Fatalf("queue command: %d %s", w.Code, w.Body.String())
	}
	tcp.SetDeadline(time.Now().Add(5 * time.Second))
	wire := make([]byte, 2)
	if _, err := io.ReadFull(tcp, wire); err != nil || !bytes.Equal(wire, []byte{0xca, 0xfe}) {
		t.Fatalf("actual edge command bytes: %x %v", wire, err)
	}
	before, _ := repo.GetDeviceCommand(ctx, "tenant", "edge-real-command")
	if before.Status != "DISPATCHING" {
		t.Fatal("claim reported device acknowledgment before actual reply", before.Status)
	}
	tcp.Write([]byte{0xb0, 0x01})
	if _, err := io.ReadFull(tcp, ack[:1]); err != nil || ack[0] != 0xac {
		t.Fatal("command reply ingress", err)
	}
	for {
		finished, err := repo.GetDeviceCommand(ctx, "tenant", "edge-real-command")
		if err == nil && finished.Status == "ACKNOWLEDGED" {
			if finished.Reply["rawMessageId"] == "" || finished.Reply["correlationId"] != "fixture-command" {
				t.Fatal("missing real acknowledgment evidence")
			}
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("command result missing", finished, err)
		case <-time.After(20 * time.Millisecond):
		}
	}
	if w := callCommand("operator", commandBody); w.Code != 200 || bytes.Contains(w.Body.Bytes(), []byte(`"token"`)) {
		t.Fatal("retry or public token", w.Code, w.Body.String())
	}
	if w := callCommand("operator", `{"type":"other","requestId":"edge-real-command","confirmed":true}`); w.Code != 409 {
		t.Fatal("conflicting request ID", w.Code)
	}
	tcp.SetReadDeadline(time.Now().Add(1200 * time.Millisecond))
	if _, err := tcp.Read(wire); err == nil {
		t.Fatal("completed command was automatically sent again")
	}

	t.Run("command-browser", func(t *testing.T) {
		if os.Getenv("IOT_TEST_BROWSER") == "" {
			t.Skip("IOT_TEST_BROWSER is not configured")
		}
		token, err := api.auth.Issue("browser-test", "tenant", "admin", nil, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		deviceDone := make(chan error, 1)
		go func() {
			tcp.SetDeadline(time.Now().Add(20 * time.Second))
			frame := make([]byte, 2)
			if _, err := io.ReadFull(tcp, frame); err != nil {
				deviceDone <- err
				return
			}
			if !bytes.Equal(frame, []byte{0xca, 0xfe}) {
				deviceDone <- fmt.Errorf("unexpected command: %x", frame)
				return
			}
			if _, err := tcp.Write([]byte{0xb0, 0x02}); err != nil {
				deviceDone <- err
				return
			}
			_, err := io.ReadFull(tcp, frame[:1])
			deviceDone <- err
		}()
		command := exec.CommandContext(ctx, "node", filepath.Join("..", "..", "iot_front", "tests", "browser", "edge-command-check.mjs"))
		command.Env = append(os.Environ(), "IOT_TEST_ORIGIN="+upstream.URL, "IOT_TEST_TOKEN="+token)
		output, err := command.CombinedOutput()
		if err != nil {
			tcp.SetReadDeadline(time.Now())
			<-deviceDone
			t.Fatalf("browser: %v %s", err, output)
		}
		if err := <-deviceDone; err != nil {
			t.Fatal("real device response", err)
		}
		t.Log(string(output))
	})

	if err := repo.SaveProductProtocolBinding(ctx, model.ProductProtocolBinding{TenantID: "tenant", ProductID: "product", ProtocolID: "worker", Version: "1.1.0", PreviousVersion: "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	// The same TCP connection survives the node's five-second configuration poll.
	for {
		tcp.SetDeadline(time.Now().Add(time.Second))
		if _, err := tcp.Write([]byte{5, 6}); err != nil {
			t.Fatal(err)
		}
		if _, err := io.ReadFull(tcp, ack[:1]); err != nil {
			t.Fatal("connection lost during update", err)
		}
		messages, _, err := repo.ListDeviceMessages(ctx, "tenant", "device", model.PropertyReport, 100, 0)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, m := range messages {
			index, e := repo.GetRawIndex(ctx, "tenant", m.RawMessageID)
			raw, rawErr := engine.GetRaw(ctx, index)
			if e == nil && rawErr == nil && raw.ProtocolVersion == "1.1.0" {
				if m.Properties["temperature"] != float64(42) {
					t.Fatal("parse failed")
				}
				found = true
			}
		}
		if found {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("updated version not ingested")
		case <-time.After(200 * time.Millisecond):
		}
	}
}
