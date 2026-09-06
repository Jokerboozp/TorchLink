package parser

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"iot-platform/internal/model"
)

func TestExternalGoProtocolParserRunsWorkerAndEnforcesEnvelopeIdentity(t *testing.T) {
	root := t.TempDir()
	worker := buildExternalTestWorker(t, root, `package main
import("encoding/json";"os")
func main(){var raw struct{Payload json.RawMessage `+"`json:\"payload\"`"+`};if json.NewDecoder(os.Stdin).Decode(&raw)!=nil{os.Exit(2)};var body map[string]any;_ = json.Unmarshal(raw.Payload,&body);_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"messageType":"PROPERTY_REPORT","tenantId":"evil","deviceId":"evil","properties":map[string]any{"temperature":body["temperature"]}})}`)
	digest := fileDigest(t, worker)
	r := NewRegistry(ExternalParser{Root: root})
	m, err := r.ParseWithConfig(GoProtocolParserName, map[string]any{"artifact": map[string]any{"path": filepath.Base(worker), "sha256": digest}, "timeoutMs": 10000}, model.RawMessage{
		MessageID: "raw_external", TenantID: "tenant_001", ProductID: "product_1", DeviceID: "device_1", PayloadFormat: "json", ReceivedAt: 1234,
		Payload: json.RawMessage(`{"temperature":42}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.MessageType != model.PropertyReport || m.Properties["temperature"] != float64(42) {
		t.Fatalf("unexpected external result: %#v", m)
	}
	if m.TenantID != "tenant_001" || m.DeviceID != "device_1" || m.Parser != GoProtocolParserName {
		t.Fatalf("worker changed protected identity or parser metadata: %#v", m)
	}
}

func TestExternalGoProtocolParserRejectsChecksumMismatch(t *testing.T) {
	root := t.TempDir()
	worker := buildExternalTestWorker(t, root, `package main
import("encoding/json";"os")
func main(){_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"messageType":"PROPERTY_REPORT"})}`)
	_, err := (ExternalParser{Root: root}).ParseWithConfig(model.RawMessage{}, map[string]any{"artifact": map[string]any{"path": filepath.Base(worker), "sha256": "00"}})
	if err == nil || !containsAny(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}
}

func TestExternalV2DecodeUsesArchivedSessionState(t *testing.T) {
	root := t.TempDir()
	worker := buildExternalTestWorker(t, root, `package main
import("encoding/json";"os")
func main(){var in map[string]any;if json.NewDecoder(os.Stdin).Decode(&in)!=nil{os.Exit(2)};if in["operation"]!="decode" || in["version"]!=float64(2){os.Exit(3)};_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"standardMessage":map[string]any{"messageType":"PROPERTY_REPORT","properties":in["state"]}})}`)
	config := map[string]any{"artifact": map[string]any{"path": filepath.Base(worker), "sha256": fileDigest(t, worker), "runtime": "go-protocol-v2"}}
	raw := model.RawMessage{MessageID: "raw_state", Metadata: map[string]any{"protocolState": map[string]any{"sequence": float64(17)}}}
	// The persisted JSON representation must reproduce the same context.
	archived, _ := json.Marshal(raw)
	var replay model.RawMessage
	_ = json.Unmarshal(archived, &replay)
	for _, input := range []model.RawMessage{raw, replay} {
		message, err := (ExternalParser{Root: root}).ParseWithConfig(input, config)
		if err != nil || message.Properties["sequence"] != float64(17) {
			t.Fatalf("state decode %+v %v", message, err)
		}
	}
}

func buildExternalTestWorker(t *testing.T, root, source string) string {
	t.Helper()
	sourcePath := filepath.Join(root, "worker.go")
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	name := "worker"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(root, name)
	cmd := exec.Command("go", "build", "-trimpath", "-o", path, sourcePath)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build external test worker: %v\n%s", err, output)
	}
	return path
}

func fileDigest(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func containsAny(value string, expected ...string) bool {
	for _, item := range expected {
		if strings.Contains(value, item) {
			return true
		}
	}
	return false
}
