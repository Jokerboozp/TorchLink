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
	"strconv"
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
	"iot-platform/internal/protocolruntime"
	"iot-platform/internal/protocolworker"
)

func TestGoFunctionsUploadAndListener(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
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
	ingested := make(chan model.RawMessage, 16)
	listeners := protocolruntime.NewListeners(repo, root, func(ctx context.Context, raw model.RawMessage) error {
		_, _, err := engine.IngestRaw(ctx, raw)
		if err == nil {
			ingested <- raw
		}
		return err
	}, log)
	api.SetProtocolListeners(listeners)
	assets := http.FileServer(http.Dir(filepath.Join("..", "..", "iot_front", "dist")))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			api.Handler().ServeHTTP(w, r)
		} else {
			assets.ServeHTTP(w, r)
		}
	}))
	defer server.Close()
	token, _ := api.auth.Issue("operator", "tenant", "operator", nil, time.Hour)
	viewer, _ := api.auth.Issue("viewer", "tenant", "viewer", nil, time.Hour)
	if err := repo.SaveProduct(ctx, model.Product{TenantID: "tenant", ID: "product", Name: "Go 函数产品", Status: "ENABLED"}); err != nil {
		t.Fatal(err)
	}
	upload := func(auth, name string, code []byte, fields map[string]string, want int) model.ProtocolRelease {
		t.Helper()
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		f, _ := form.CreateFormFile("file", name)
		f.Write(code)
		form.WriteField("productId", "product")
		for k, v := range fields {
			form.WriteField(k, v)
		}
		form.Close()
		req := httptest.NewRequest("POST", "/api/v2/protocols/functions/source-releases", &body)
		req.Header.Set("Authorization", "Bearer "+auth)
		req.Header.Set("Content-Type", form.FormDataContentType())
		w := httptest.NewRecorder()
		api.Handler().ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("upload %s: %d want %d: %s", name, w.Code, want, w.Body.String())
		}
		var result struct{ Release model.ProtocolRelease }
		json.Unmarshal(w.Body.Bytes(), &result)
		return result.Release
	}
	// No runtime, JSON samples, metadata or module required for a single Go file.
	upload(viewer, "protocol.go", []byte(protocolbuild.FunctionTemplate), nil, 403)
	first := upload(token, "protocol.go", []byte(protocolbuild.FunctionTemplate), nil, 201)
	if !strings.HasPrefix(first.Version, "auto-") || first.Artifact["runtime"] != protocolworker.Runtime {
		t.Fatalf("release %+v", first)
	}
	raw := model.RawMessage{MessageID: "raw_functions", TenantID: "tenant", ProductID: "product", DeviceID: "device", Protocol: "functions", PayloadFormat: "hex", Payload: json.RawMessage(`"AA012A"`)}
	if _, _, err := engine.IngestRaw(ctx, raw); err != nil {
		t.Fatal(err)
	}
	message, err := repo.GetStandardMessageByRaw(ctx, "tenant", raw.MessageID)
	if err != nil || message.Properties["temperature"] != float64(42) {
		t.Fatalf("message %+v %v", message, err)
	}
	bad := strings.Replace(protocolbuild.FunctionTemplate, "int(data[2])", "99", 1)
	upload(token, "protocol.go", []byte(bad), nil, 422)
	upload(token, "protocol.go", []byte(strings.Replace(protocolbuild.FunctionTemplate, "Samples: []Sample{{", "Samples: []Sample{/*", 1)), nil, 422)
	upload(token, "protocol.go", []byte(protocolbuild.FunctionTemplate), map[string]string{"capabilities": `["decode"]`}, 422)
	// Go samples are mandatory; supplied expected event/tag/time fields must
	// actually be checked rather than silently accepted as metadata.
	upload(token, "protocol.go", []byte(`package main
func Protocol() Definition {return Definition{Decode:func([]byte,Context)(Message,error){return properties(map[string]any{}),nil}}}`), nil, 422)
	for _, field := range []string{`Event:map[string]any{"type":"WRONG"}`, `Tags:map[string]string{"site":"WRONG"}`, `Timestamp:123`} {
		code := `package main
func Protocol() Definition {return Definition{
 Decode:func([]byte,Context)(Message,error){return properties(map[string]any{"temperature":42}),nil},
 Samples:[]Sample{{Data:[]byte{1},Want:Message{MessageType:"PROPERTY_REPORT",` + field + `}}},
}}`
		upload(token, "protocol.go", []byte(code), nil, 422)
	}
	binding, err := repo.GetProductProtocolBinding(ctx, "tenant", "product")
	if err != nil || binding.Version != first.Version {
		t.Fatalf("failed upload replaced binding %+v %v", binding, err)
	}
	// Downloaded template is a complete independently buildable Go project.
	req := httptest.NewRequest("GET", "/api/v2/protocol-source-template?format=go-functions&kind=tcp", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	api.Handler().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	sources, err := protocolbuild.Sources("template.zip", w.Body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	var wrapped bytes.Buffer
	zw := zip.NewWriter(&wrapped)
	for name, data := range sources {
		if strings.HasSuffix(name, ".json") {
			t.Fatal("template requires JSON", name)
		}
		os.WriteFile(filepath.Join(project, name), data, 0600)
		f, _ := zw.Create("my-protocol/" + name)
		f.Write(data)
	}
	zw.Close()
	command := exec.CommandContext(ctx, "go", "test", "./...")
	command.Dir = project
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("standalone template: %v %s", err, output)
	}
	second := upload(token, "project.zip", wrapped.Bytes(), map[string]string{"version": "2.0.0"}, 201)
	if len(second.Capabilities) != 3 {
		t.Fatal(second.Capabilities)
	}
	upload(token, "project.zip", wrapped.Bytes(), map[string]string{"version": "2.0.0"}, 409)
	// Full template's samples exercise ingress, decode, ACK matching and encode.
	// Also prove real sockets still use inventory, archive and StandardMessage.
	free, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := free.Addr().(*net.TCPAddr).Port
	free.Close()
	profile := model.DeviceAccessProfile{ID: "functions-tcp", ProductID: "product", ProtocolID: "functions", ProtocolVersion: second.Version, Mode: "listener", Network: "tcp", Host: "127.0.0.1", Port: port, TimeoutMs: 2000, Enabled: true, AutoRegister: true}
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v2/device-access-profiles", token, profile, 201)
	listeners.Start(ctx)
	var conn net.Conn
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		conn, err = net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), time.Millisecond*100)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	frame := []byte{0xAA, 1, 7, 42, 0xDC}
	conn.Write(frame[:2])
	conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	reply := make([]byte, 5)
	if _, err := conn.Read(reply); err == nil {
		t.Fatal("half frame received ACK")
	}
	conn.Write(append(frame[2:], frame...))
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for i := 0; i < 2; i++ {
		if _, err := io.ReadFull(conn, reply); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(reply, []byte{0xAA, 2, 7, 42, 0xDD}) {
			t.Fatalf("ACK %x", reply)
		}
		select {
		case raw := <-ingested:
			message, err := repo.GetStandardMessageByRaw(ctx, "tenant", raw.MessageID)
			if err != nil || message.DeviceID != "7" || message.Properties["temperature"] != float64(42) {
				t.Fatalf("TCP message %+v %v", message, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("no archived frame")
		}
	}
	// Downlink is encoded by the uploaded Go function, sent on the live socket,
	// then acknowledged only when the matching device response arrives.
	commandDone := make(chan error, 1)
	go func() {
		result, err := listeners.Command(ctx, "tenant", profile.ID, "7", map[string]any{"type": "ping"})
		if err == nil && result["status"] != "acknowledged" {
			err = fmt.Errorf("command status %v", result)
		}
		commandDone <- err
	}()
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(conn, reply); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(reply, []byte{0xAA, 3, 7, 1, 0xB5}) {
		t.Fatalf("downlink %x", reply)
	}
	conn.Write([]byte{0xAA, 2, 7, 1, 0xB4})
	select {
	case err := <-commandDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("command not acknowledged")
	}
	select {
	case raw := <-ingested:
		message, err := repo.GetStandardMessageByRaw(ctx, "tenant", raw.MessageID)
		if err != nil || message.MessageType != model.CommandReply {
			t.Fatalf("ACK message %+v %v", message, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ACK not archived")
	}
	// Invalid checksum must not be acknowledged or archived.
	frame[4] = 0
	conn.Write(frame)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Read(reply); err == nil {
		t.Fatal("bad checksum acknowledged")
	}
	select {
	case <-ingested:
		t.Fatal("bad checksum archived")
	default:
	}
	t.Run("browser", func(t *testing.T) {
		if os.Getenv("IOT_TEST_BROWSER") == "" {
			t.Skip("Chrome not configured")
		}
		source := filepath.Join(t.TempDir(), "protocol.go")
		os.WriteFile(source, []byte(protocolbuild.FunctionTemplate), 0600)
		os.WriteFile(source+".invalid.go", []byte("package main\nfunc Protocol() Definition { invalid }"), 0600)
		command := exec.CommandContext(ctx, "node", filepath.Join("..", "..", "iot_front", "tests", "browser", "go-functions-check.mjs"))
		command.Env = append(os.Environ(), "IOT_TEST_ORIGIN="+server.URL, "IOT_TEST_TOKEN="+token, "IOT_TEST_SOURCE_GO="+source)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("browser: %v %s", err, output)
		} else {
			t.Log(string(output))
		}
	})
	if _, err := repo.GetProtocolRelease(ctx, "tenant", "functions-browser", "invalid-browser"); err == nil {
		t.Fatal("browser compile failure published")
	}
}
