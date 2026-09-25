package parser

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"iot-platform/internal/model"
)

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
