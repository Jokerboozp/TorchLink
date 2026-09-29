package parser

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"iot-platform/internal/model"
)

func TestExternalGoProtocolParserRunsWorkerAndEnforcesEnvelopeIdentity(t *testing.T) {
	root := t.TempDir()
	worker := buildExternalTestWorker(t, root, `package main
import("encoding/json";"os")
func main(){var request struct{Version int; Operation string; Raw struct{Payload json.RawMessage `+"`json:\"payload\"`"+`}};if json.NewDecoder(os.Stdin).Decode(&request)!=nil || request.Version!=2 || request.Operation!="decode"{os.Exit(2)};var body map[string]any;_ = json.Unmarshal(request.Raw.Payload,&body);_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"standardMessage":map[string]any{"messageType":"PROPERTY_REPORT","tenantId":"evil","deviceId":"evil","properties":map[string]any{"temperature":body["temperature"]}}})}`)
	digest := fileDigest(t, worker)
	r := NewRegistry(ExternalParser{Root: root})
	m, err := r.ParseWithConfig(GoProtocolParserName, map[string]any{"artifact": map[string]any{"path": filepath.Base(worker), "sha256": digest, "runtime": "go-protocol-v2"}, "timeoutMs": 10000}, model.RawMessage{
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
	_, err := (ExternalParser{Root: root}).ParseWithConfig(model.RawMessage{}, map[string]any{"artifact": map[string]any{"path": filepath.Base(worker), "sha256": "00", "runtime": "go-protocol-v2"}})
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

func TestExternalRejectsLegacyContract(t *testing.T) {
	for _, runtime := range []string{"", "go-json-lines-v1"} {
		_, err := (ExternalParser{}).ParseWithConfig(model.RawMessage{}, map[string]any{"artifact": map[string]any{"path": "worker", "runtime": runtime}})
		if err == nil || !strings.Contains(err.Error(), "runtime must be go-protocol-v2") {
			t.Fatalf("legacy runtime %q: %v", runtime, err)
		}
	}
	if _, err := decodeExternalMessage([]byte(`{"messageType":"PROPERTY_REPORT"}`)); err == nil {
		t.Fatal("unwrapped legacy response accepted")
	}
}

func TestArtifactVerificationCacheDetectsReplacedBinary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worker")
	original := []byte("worker-binary-v1")
	if err := os.WriteFile(path, original, 0o755); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(original)
	artifact := map[string]any{"sha256": hex.EncodeToString(sum[:])}
	for i := 0; i < 2; i++ {
		if err := verifyExternalArtifact(path, artifact); err != nil {
			t.Fatalf("verification %d: %v", i, err)
		}
	}
	// Same size, different content and modification time: the cached result
	// must not be trusted.
	if err := os.WriteFile(path, []byte("worker-binary-v2"), 0o755); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	if err := verifyExternalArtifact(path, artifact); err == nil {
		t.Fatal("replaced artifact passed checksum verification")
	}
	// A different expected hash for the same path is always re-checked.
	if err := verifyExternalArtifact(path, map[string]any{"sha256": "00"}); err == nil {
		t.Fatal("mismatching expected hash was accepted")
	}
}

// residentTestWorker answers in both modes. The payload "cmd" field makes it
// crash, hang or print an extra line; the answer carries the process ID.
const residentTestWorker = `package main
import("bufio";"encoding/json";"os";"time")
func answer(line []byte) map[string]any {
	var in struct{RequestID string ` + "`json:\"requestId\"`" + `; Raw struct{Payload struct{Cmd string ` + "`json:\"cmd\"`" + `} ` + "`json:\"payload\"`" + `} ` + "`json:\"raw\"`" + `}
	_ = json.Unmarshal(line, &in)
	switch in.Raw.Payload.Cmd {
	case "crash": os.Exit(3)
	case "hang": time.Sleep(time.Hour)
	case "stray": os.Stdout.WriteString("{\"requestId\":\"stray\"}\n")
	}
	out := map[string]any{"standardMessage": map[string]any{"messageType": "PROPERTY_REPORT", "properties": map[string]any{"pid": os.Getpid()}}}
	if in.RequestID != "" { out["requestId"] = in.RequestID }
	return out
}
func main() {
	enc := json.NewEncoder(os.Stdout)
	if os.Getenv("IOT_PROTOCOL_WORKER_MODE") != "serve" {
		var line json.RawMessage
		_ = json.NewDecoder(os.Stdin).Decode(&line)
		_ = enc.Encode(answer(line))
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() { _ = enc.Encode(answer(scanner.Bytes())) }
}`

// singleShotWorker answers one request and exits, like a Worker without serve support.
const singleShotWorker = `package main
import("encoding/json";"os")
func main(){var in map[string]any;_ = json.NewDecoder(os.Stdin).Decode(&in);_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"requestId":in["requestId"],"standardMessage":map[string]any{"messageType":"PROPERTY_REPORT"}})}`

func residentConfig(t *testing.T, source string, serve bool) (ExternalParser, map[string]any) {
	t.Helper()
	root := t.TempDir()
	// Registered after TempDir, so it runs before the directory is removed.
	t.Cleanup(func() { StopResidentWorkers(root) })
	worker := buildExternalTestWorker(t, root, source)
	artifact := map[string]any{"path": filepath.Base(worker), "sha256": fileDigest(t, worker), "runtime": "go-protocol-v2"}
	if serve {
		artifact["workerMode"] = WorkerModeServe
	}
	return ExternalParser{Root: root}, map[string]any{"artifact": artifact, "timeoutMs": 2000}
}

func residentRaw(cmd string) model.RawMessage {
	return model.RawMessage{MessageID: "raw_1", TenantID: "t1", ProductID: "p1", DeviceID: "d1", PayloadFormat: "json", Payload: json.RawMessage(fmt.Sprintf(`{"cmd":%q}`, cmd))}
}

func residentPID(t *testing.T, p ExternalParser, config map[string]any, cmd string) (string, error) {
	t.Helper()
	message, err := p.ParseWithContext(context.Background(), residentRaw(cmd), config)
	if err != nil {
		return "", err
	}
	return fmt.Sprint(message.Properties["pid"]), nil
}

// A serve Worker is reused between calls and replaced after a crash, a timeout
// or an answer that does not belong to the request.
func TestResidentWorkerIsReusedAndReplacedAfterFailures(t *testing.T) {
	p, config := residentConfig(t, residentTestWorker, true)
	first, err := residentPID(t, p, config, "")
	if err != nil {
		t.Fatal(err)
	}
	if again, err := residentPID(t, p, config, ""); err != nil || again != first {
		t.Fatalf("resident worker was not reused: %s then %s err=%v", first, again, err)
	}
	for _, cmd := range []string{"crash", "hang", "stray"} {
		if _, err := residentPID(t, p, config, cmd); err == nil {
			t.Fatalf("%s must fail the call", cmd)
		}
		next, err := residentPID(t, p, config, "")
		if err != nil {
			t.Fatalf("worker was not restarted after %s: %v", cmd, err)
		}
		if next == first {
			t.Fatalf("failed process was reused after %s", cmd)
		}
		first = next
	}
}

func TestResidentWorkersAreBoundedPerArtifact(t *testing.T) {
	p, config := residentConfig(t, residentTestWorker, true)
	var mu sync.Mutex
	pids := map[string]bool{}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pid, err := residentPID(t, p, config, "")
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			pids[pid] = true
			mu.Unlock()
		}()
	}
	wg.Wait()
	if len(pids) == 0 || len(pids) > residentPerArtifact {
		t.Fatalf("%d processes served one artifact, limit %d", len(pids), residentPerArtifact)
	}
}

// Publication keeps a Worker resident only when it passes the probe.
func TestProbeServeAcceptsOnlyServingWorkers(t *testing.T) {
	p, config := residentConfig(t, residentTestWorker, false)
	request := DecodeRequest(residentRaw(""))
	if err := p.ProbeServe(context.Background(), config, request); err == nil {
		t.Fatal("the probe must compare the pid-bearing answers and reject them")
	}
	stable := strings.Replace(residentTestWorker, `"pid": os.Getpid()`, `"pid": 1`, 1)
	p, config = residentConfig(t, stable, false)
	if err := p.ProbeServe(context.Background(), config, request); err != nil {
		t.Fatalf("serving worker rejected: %v", err)
	}
	p, config = residentConfig(t, singleShotWorker, false)
	if err := p.ProbeServe(context.Background(), config, request); err == nil {
		t.Fatal("a single-shot worker must not be marked resident")
	}
}
