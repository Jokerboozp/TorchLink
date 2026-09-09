package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	"testing"
	"time"
)

func TestEdgeWorkerDownloadTCPUDPAndVersionSwitch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
	upstream := httptest.NewServer(api.Handler())
	defer upstream.Close()
	source := filepath.Join(t.TempDir(), "worker.go")
	code := `package main
import("os";"encoding/json";"encoding/hex")
func main(){var q struct{Operation,Data string}; json.NewDecoder(os.Stdin).Decode(&q); out:=map[string]any{}; if q.Operation=="ingress" {b,_:=hex.DecodeString(q.Data); if len(b)<2{out["needMore"]=true}else{out["consumed"]=2;out["deviceId"]="device";out["reply"]="AC"}}else{out["standardMessage"]=map[string]any{"messageType":"PROPERTY_REPORT","properties":map[string]any{"temperature":42}}};json.NewEncoder(os.Stdout).Encode(out)}
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
		if err := repo.CreateProtocolRelease(ctx, model.ProtocolRelease{TenantID: "tenant", ProtocolID: "worker", Version: version, Status: "PUBLISHED", Transport: "TCP_UDP", PayloadFormat: "hex", ParserType: parser.GoProtocolParserName, Capabilities: []string{"ingress", "decode"}, Artifact: artifact, Config: map[string]any{"artifact": artifact}}); err != nil {
			t.Fatal(err)
		}
	}
	for _, err := range []error{repo.SaveProduct(ctx, model.Product{TenantID: "tenant", ID: "product", Status: "ENABLED"}), repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "tenant", ProductID: "product", ID: "device", Status: "ENABLED"}), repo.SaveEdgeNode(ctx, model.EdgeNode{TenantID: "tenant", ID: "edge", Status: "ENABLED"}), repo.SetEdgeCredential(ctx, "tenant", "edge", onboarding.Hash("test-node-secret")), repo.SaveProductProtocolBinding(ctx, model.ProductProtocolBinding{TenantID: "tenant", ProductID: "product", ProtocolID: "worker", Version: "1.0.0"})} {
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
		if err := repo.SaveDeviceAccessProfile(ctx, model.DeviceAccessProfile{TenantID: "tenant", ID: network, ProductID: "product", ProtocolID: "worker", ProtocolVersion: "1.0.0", Mode: "listener", Network: network, Host: "127.0.0.1", Port: ports[network], Enabled: true, EdgeNodeID: "edge", TimeoutMs: 2000}); err != nil {
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
	agent, err := edgeagent.New(edgeagent.Options{URL: upstream.URL, TenantID: "tenant", NodeID: "edge", Secret: "test-node-secret", DataDir: t.TempDir(), AllowedCIDRs: []string{"127.0.0.0/8"}, AllowInsecureHTTP: true, AllowGoWorkers: true, AllowedListenAddresses: []string{"127.0.0.1"}}, log)
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
