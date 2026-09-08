package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/connector"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
	"iot-platform/internal/parser"
	"iot-platform/internal/protocolbuild"
	"iot-platform/internal/protocolruntime"
)

// Uses the separately maintained source package and real child processes,
// HTTP publication, sockets and the normal archive/parser path in one host.
func TestGoProtocolListenerSourceHotSwitch(t *testing.T) {
	if !protocolbuild.Available() {
		t.Skip("Go compiler unavailable")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	root := t.TempDir()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(root)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(root), log)
	engine.Metrics = metrics.New()
	if err = engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.DataDir = root
	cfg.JWTSecret = "listener-test-secret-at-least-32-characters"
	api := New(cfg, engine, engine.Metrics.(*metrics.Registry), log)
	ingested := make(chan model.RawMessage, 32)
	listeners := protocolruntime.NewListeners(repo, root, func(ctx context.Context, raw model.RawMessage) error {
		_, _, err := engine.IngestRaw(ctx, raw)
		if err == nil {
			ingested <- raw
		}
		return err
	}, log)
	api.SetProtocolListeners(listeners)
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	token, _ := api.auth.Issue("tester", "tenant_001", "operator", nil, time.Hour)
	_ = repo.SaveProduct(ctx, model.Product{TenantID: "tenant_001", ID: "gb-product", Name: "GB", Status: "ENABLED"})
	packageRoot := filepath.Join("..", "..", "protocol-packages", "gb26875-dahua")
	fixture, err := os.ReadFile(filepath.Join(packageRoot, "samples", "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var samples []protocolPackageCaseV2
	if err = json.Unmarshal(fixture, &samples); err != nil {
		t.Fatal(err)
	}
	var frameHex string
	_ = json.Unmarshal(samples[0].Input.Payload, &frameHex)
	frame, _ := hex.DecodeString(frameHex)
	upload := func(version string, bad bool, status int) {
		t.Helper()
		var packed bytes.Buffer
		zw := zip.NewWriter(&packed)
		err := filepath.WalkDir(packageRoot, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			name, _ := filepath.Rel(packageRoot, path)
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if name == "protocol.json" {
				data = bytes.ReplaceAll(data, []byte("1.0.0"), []byte(version))
			}
			if bad && filepath.ToSlash(name) == "samples/operations.json" {
				data = []byte(`[]`)
			}
			if version == "1.1.0" && filepath.ToSlash(name) == "gb26875/codec.go" {
				data = bytes.ReplaceAll(data, []byte(`"Dahua"`), []byte(`"Dahua-v2"`))
			}
			f, err := zw.Create(filepath.ToSlash(name))
			if err == nil {
				_, err = f.Write(data)
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		_ = zw.Close()
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		f, _ := form.CreateFormFile("file", "gb.zip")
		_, _ = f.Write(packed.Bytes())
		_ = form.WriteField("productId", "gb-product")
		_ = form.WriteField("publish", "true")
		_ = form.Close()
		req, _ := http.NewRequest("POST", server.URL+"/api/v2/protocols/gb26875-dahua/source-releases", &body)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", form.FormDataContentType())
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != status {
			t.Fatalf("upload %s: %d %s", version, resp.StatusCode, data)
		}
	}
	upload("1.0.0", false, 201)
	free, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := free.Addr().(*net.TCPAddr).Port
	_ = free.Close()
	profile := model.DeviceAccessProfile{ID: "gb-tcp", ProductID: "gb-product", ProtocolID: "gb26875-dahua", ProtocolVersion: "1.0.0", Mode: "listener", Network: "tcp", Host: "127.0.0.1", Port: port, TimeoutMs: 2000, Enabled: true, AutoRegister: true}
	for _, kind := range []connector.Type{connector.TCP, connector.UDP} {
		preview, err := api.onboarding.Test(ctx, "tenant_001", onboarding.Request{ProductID: "gb-product", DeviceID: "gb26875_123456789012", Name: "GB preview", Type: kind, Profile: profile, Payload: samples[0].Input.Payload})
		if err != nil || !preview.Success || len(preview.StandardMessages) != 1 || preview.TestToken == "" {
			t.Fatalf("%s onboarding preview: %+v %v", kind, preview, err)
		}
	}
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v2/device-access-profiles", token, profile, 201)
	udpFree, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	udpPort := udpFree.LocalAddr().(*net.UDPAddr).Port
	_ = udpFree.Close()
	udpProfile := profile
	udpProfile.ID = "gb-udp"
	udpProfile.Network = "udp"
	udpProfile.Port = udpPort
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v2/device-access-profiles", token, udpProfile, 201)
	listeners.Start(ctx)
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	var conn net.Conn
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		conn, err = net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// Add a managed device through the unified service while reusing the live
	// listener. The existing runtime below must accept it without re-registration.
	onboardRequest := onboarding.Request{ProductID: "gb-product", DeviceID: "gb26875_123456789012", Name: "GB onboarded", Type: connector.TCP, Profile: profile, ExistingProfileID: profile.ID, Payload: samples[0].Input.Payload}
	onboardPreview, onboardErr := api.onboarding.Test(ctx, "tenant_001", onboardRequest)
	if onboardErr != nil || !onboardPreview.Success {
		t.Fatalf("reuse listener preview: %+v %v", onboardPreview, onboardErr)
	}
	onboardRequest.TestToken = onboardPreview.TestToken
	if _, err = api.onboarding.Create(ctx, "tenant_001", onboardRequest); err != nil {
		t.Fatal("reuse listener onboarding", err)
	}
	profilesAfter, _ := repo.ListDeviceAccessProfiles(ctx, "tenant_001")
	if len(profilesAfter) != 2 {
		t.Fatal("onboarding duplicated the existing listener")
	}
	readFrame := func(c net.Conn) []byte {
		t.Helper()
		_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
		head := make([]byte, 27)
		if _, err := io.ReadFull(c, head); err != nil {
			t.Fatal(err)
		}
		length := int(head[24]) + int(head[25])*256
		tail := make([]byte, length+3)
		if _, err := io.ReadFull(c, tail); err != nil {
			t.Fatal(err)
		}
		return append(head, tail...)
	}
	check := func(version, vendor string) {
		t.Helper()
		select {
		case raw := <-ingested:
			if raw.ProtocolVersion != version || raw.DeviceID != "gb26875_123456789012" {
				t.Fatalf("raw %+v", raw)
			}
			msg, err := repo.GetStandardMessageByRaw(ctx, "tenant_001", raw.MessageID)
			if err != nil || msg.Tags["terminalVendor"] != vendor {
				t.Fatalf("parsed %+v %v", msg, err)
			}
			stored, err := repo.GetRawIndex(ctx, "tenant_001", raw.MessageID)
			if err != nil {
				t.Fatal(err)
			}
			archived, err := engine.GetRaw(ctx, stored)
			if err != nil || archived.ProtocolVersion != version {
				t.Fatalf("archive %+v %v", archived, err)
			}
			if raw.Metadata["protocolState"] != nil && archived.Metadata["protocolState"] == nil {
				t.Fatal("session state was not archived for replay")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("no ingested frame")
		}
	}
	_, _ = conn.Write(frame[:12])
	time.Sleep(50 * time.Millisecond)
	select {
	case <-ingested:
		t.Fatal("partial frame was ingested")
	default:
	}
	_, _ = conn.Write(append(append([]byte{}, frame[12:]...), frame...))
	for i := 0; i < 2; i++ {
		ack := readFrame(conn)
		if ack[26] != 3 {
			t.Fatalf("not ACK: %X", ack)
		}
		check("1.0.0", "Dahua")
	}
	// A real command must wait for a matching reply, without acknowledging ACKs.
	commandDone := make(chan error, 1)
	go func() {
		result, err := listeners.Command(ctx, "tenant_001", "gb-tcp", "gb26875_123456789012", map[string]any{"type": "time-sync"})
		if err == nil && result["status"] != "acknowledged" {
			err = io.ErrUnexpectedEOF
		}
		commandDone <- err
	}()
	command := readFrame(conn)
	ack := append([]byte{}, command[:27]...)
	copy(ack[12:18], frame[12:18])
	copy(ack[18:24], frame[18:24])
	ack[24], ack[25], ack[26] = 0, 0, 3
	var sum byte
	for _, b := range ack[2:] {
		sum += b
	}
	ack = append(ack, sum, '#', '#')
	_, _ = conn.Write(ack)
	check("1.0.0", "Dahua")
	if err = <-commandDone; err != nil {
		t.Fatal(err)
	}
	udp, err := net.Dial("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(udpPort)))
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	_, _ = udp.Write(frame)
	_ = udp.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 1024)
	if n, err := udp.Read(buf); err != nil || n != 30 || buf[26] != 3 {
		t.Fatalf("UDP ACK %d %v", n, err)
	}
	check("1.0.0", "Dahua")
	upload("1.1.0", false, 201)
	_, _ = conn.Write(frame)
	_ = readFrame(conn)
	check("1.1.0", "Dahua-v2")
	upload("1.2.0", true, 422)
	_, _ = conn.Write(frame)
	_ = readFrame(conn)
	check("1.1.0", "Dahua-v2")
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v2/products/gb-product/protocol-binding/rollback", token, map[string]any{}, 200)
	_, _ = conn.Write(frame)
	_ = readFrame(conn)
	check("1.0.0", "Dahua")
	// A corrupt frame receives no ACK and cannot enter the archive.
	_, _ = conn.Write(append([]byte("noise"), frame...))
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if n, err := conn.Read(buf); n != 0 || err == nil || strings.Contains(err.Error(), "timeout") {
		t.Fatalf("invalid frame not rejected: %d %v", n, err)
	}
	select {
	case <-ingested:
		t.Fatal("corrupt frame ingested")
	default:
	}
}
