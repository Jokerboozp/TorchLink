package protocolrunner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// hostileWorker probes what an uploaded worker can reach and reports it.
const hostileWorker = `package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"time"
)

func main() {
	reader := bufio.NewReader(os.Stdin)
	_, _ = reader.ReadString('\n')
	_, dialErr := net.DialTimeout("tcp", "1.1.1.1:443", 2*time.Second)
	env, envErr := os.ReadFile("/proc/1/environ")
	entries, _ := os.ReadDir("/app/data")
	dataErr := fmt.Sprintf("%d entries", len(entries))
	writeErr := os.WriteFile("/usr/local/go/hacked", []byte("x"), 0o600)
	fmt.Printf("{\"dial\":%q,\"environ\":%q,\"envErr\":%q,\"data\":%q,\"write\":%q}\n", fmt.Sprint(dialErr), string(env), fmt.Sprint(envErr), dataErr, fmt.Sprint(writeErr))
}
`

// IOT_TEST_PROTOCOL_RUNNER_SOCKET points at a runner started as in
// compose.yaml (protocol-runner service). The test uploads a prebuilt worker,
// bypassing the build-time import check, to verify runtime isolation.
func TestContainerRunnerIsolatesHostileWorkers(t *testing.T) {
	socket := os.Getenv("IOT_TEST_PROTOCOL_RUNNER_SOCKET")
	if socket == "" {
		t.Skip("IOT_TEST_PROTOCOL_RUNNER_SOCKET is not configured")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(hostileWorker), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module hostile\n\ngo 1.25\n"), 0o600)
	out := filepath.Join(dir, "worker")
	build := exec.Command("go", "build", "-o", out, ".")
	build.Dir, build.Env = dir, append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+runtime.GOARCH)
	if msg, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build hostile worker: %v %s", err, msg)
	}
	worker, _ := os.ReadFile(out)
	digest := sha256.Sum256(worker)
	client := NewClient(socket)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	result, err := client.Invoke(ctx, func() ([]byte, error) { return worker, nil }, hex.EncodeToString(digest[:]), "", 10*time.Second, []byte(`{"operation":"describe"}`))
	if err != nil {
		t.Fatal(err)
	}
	text := string(result)
	t.Log(text)
	for _, forbidden := range []string{"IOT_JWT_SECRET", "IOT_POSTGRES_DSN", "PASSWORD"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("worker reached a secret: %s", text)
		}
	}
	if strings.Contains(text, `"dial":"<nil>"`) {
		t.Fatal("worker reached the network")
	}
	if !strings.Contains(text, `"data":"0 entries"`) {
		t.Fatal("worker reached platform data")
	}
	if strings.Contains(text, `"write":"<nil>"`) {
		t.Fatal("worker wrote to the image file system")
	}
}

func TestContainerRunnerCompilesTemplates(t *testing.T) {
	socket := os.Getenv("IOT_TEST_PROTOCOL_RUNNER_SOCKET")
	if socket == "" {
		t.Skip("IOT_TEST_PROTOCOL_RUNNER_SOCKET is not configured")
	}
	client := NewClient(socket)
	worker, log, err := client.Build(context.Background(), templateFiles(t), ".", "linux-"+runtime.GOARCH)
	if err != nil || len(worker) == 0 {
		t.Fatalf("build in runner container: %v\n%s", err, log)
	}
}
