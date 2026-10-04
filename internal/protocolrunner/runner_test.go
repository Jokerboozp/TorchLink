package protocolrunner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"iot-platform/internal/parser"
	"iot-platform/internal/protocolbuild"
)

func startRunner(t *testing.T) *Client {
	t.Helper()
	if !protocolbuild.Available() {
		t.Skip("Go toolchain unavailable")
	}
	// Unix socket paths are limited to about 100 bytes.
	dir, err := os.MkdirTemp("/tmp", "runner-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "runner.sock")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	server := &Server{Dir: filepath.Join(dir, "work")}
	go func() { _ = server.Serve(ctx, socket) }()
	client := NewClient(socket)
	for i := 0; i < 50; i++ {
		if client.Health(ctx) == nil {
			return client
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("runner did not start")
	return nil
}

func templateFiles(t *testing.T) map[string][]byte {
	t.Helper()
	data, err := protocolbuild.FunctionTemplateZIP()
	if err != nil {
		t.Fatal(err)
	}
	files, err := protocolbuild.Sources("protocol.zip", data)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = protocolbuild.PrepareFunctions(files); err != nil {
		t.Fatal(err)
	}
	return files
}

func TestRunnerBuildsAndExecutesUploadedProtocols(t *testing.T) {
	client := startRunner(t)
	protocolbuild.SetBuilder(client)
	parser.SetExecutor(client)
	t.Cleanup(func() { protocolbuild.SetBuilder(nil); parser.SetExecutor(nil) })
	ctx := context.Background()
	worker, _, err := protocolbuild.Build(ctx, t.TempDir(), templateFiles(t), ".")
	if err != nil {
		t.Fatal(err)
	}
	// DescribeFunctions runs the worker through the parser, so it reaches the
	// runner, which receives the binary on first use.
	description, err := protocolbuild.DescribeFunctions(ctx, worker)
	if err != nil || len(description["metadata"]) == 0 {
		t.Fatalf("describe through runner: %v %v", description, err)
	}
	digest := sha256.Sum256(worker)
	if _, err = client.Invoke(ctx, func() ([]byte, error) { return worker[:10], nil }, hex.EncodeToString(digest[:]), "", time.Second, []byte(`{"operation":"describe"}`)); err != nil {
		t.Fatalf("cached worker must be reused without a new upload: %v", err)
	}
	if _, err = client.Invoke(ctx, func() ([]byte, error) { return []byte("tampered"), nil }, strings.Repeat("0", 64), "", time.Second, []byte(`{}`)); err == nil || !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("a binary not matching its hash must be refused: %v", err)
	}
}

func TestRunnerRejectsSystemAccessImports(t *testing.T) {
	client := startRunner(t)
	for name, source := range map[string]string{
		"exec":     `package main; import "os/exec"; func main() { _ = exec.Command("sh") }`,
		"network":  `package main; import "net"; func main() { _, _ = net.Dial("tcp", "x:1") }`,
		"syscall":  `package main; import "syscall"; func main() { _ = syscall.Getpid() }`,
		"unsafe":   `package main; import "unsafe"; func main() { _ = unsafe.Sizeof(0) }`,
		"assembly": "package main\nfunc f()\nfunc main() { f() }\n",
	} {
		files := map[string][]byte{"main.go": []byte(source)}
		if name == "assembly" {
			files["f_amd64.s"], files["f_arm64.s"] = []byte("TEXT ·f(SB),0,$0\n\tRET\n"), []byte("TEXT ·f(SB),0,$0\n\tRET\n")
		}
		_, _, err := client.Build(context.Background(), files, ".", runtime.GOOS+"-"+runtime.GOARCH)
		if err == nil || !(strings.Contains(err.Error(), "不能导入") || strings.Contains(err.Error(), "汇编")) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	// The runner's own build keeps working for ordinary code.
	if _, _, err := client.Build(context.Background(), map[string][]byte{"main.go": []byte(`package main; import ("fmt"; "os"); func main() { fmt.Fprintln(os.Stdout, "ok") }`)}, ".", runtime.GOOS+"-"+runtime.GOARCH); err != nil {
		t.Fatal(err)
	}
}
