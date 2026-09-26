package protocolbuild

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPrepareFunctionsCompatibility(t *testing.T) {
	for _, legacy := range []map[string][]byte{
		{"main.go": []byte(Template)},
		{"project/cmd/worker/main.go": []byte("package main\nfunc main() {}")},
		{"main.go": []byte("package main\nfunc Protocol() string{return \"legacy\"}\nfunc main() {}")},
	} {
		before := len(legacy)
		ok, err := PrepareFunctions(legacy)
		if err != nil || ok || len(legacy) != before {
			t.Fatalf("legacy modified: %v %v", ok, err)
		}
	}
	files := map[string][]byte{"project/protocol.go": []byte(FunctionTemplate)}
	ok, err := PrepareFunctions(files)
	if err != nil || !ok || !bytes.Equal(files["protocol.go"], []byte(FunctionTemplate)) || len(files["zz_platform.go"]) == 0 {
		t.Fatalf("wrapped project %v %v", ok, err)
	}
	files["zz_platform.go"] = []byte("package main")
	if _, err := PrepareFunctions(files); err == nil {
		t.Fatal("modified adapter accepted")
	}
}

func TestFunctionDescriptionLimits(t *testing.T) {
	files := map[string][]byte{"protocol.go": []byte(strings.Replace(FunctionTemplate, "return Definition{", "for {}\nreturn Definition{", 1))}
	if _, err := PrepareFunctions(files); err != nil {
		t.Fatal(err)
	}
	worker, log, err := Build(context.Background(), t.TempDir(), files, ".")
	if err != nil {
		t.Fatal(err, log)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := DescribeFunctions(ctx, worker); err == nil {
		t.Fatal("nonterminating configuration succeeded")
	}
	if time.Since(started) > 3*time.Second {
		t.Fatal("description ignored cancellation")
	}
}

func TestDownloadedTemplatesRunRealSamplesLocally(t *testing.T) {
	for _, kind := range []string{"", "tcp"} {
		t.Run("template-"+kind, func(t *testing.T) {
			data, err := FunctionTemplateZIP(kind)
			if err != nil {
				t.Fatal(err)
			}
			files, err := Sources("template.zip", data)
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			for name, data := range files {
				if err := os.WriteFile(filepath.Join(root, name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			run := func(wantPass bool) {
				t.Helper()
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, "go", "test", "-count=1", "./...")
				cmd.Dir = root
				output, err := cmd.CombinedOutput()
				if (err == nil) != wantPass || ctx.Err() != nil {
					t.Fatalf("want pass=%v: %v %s", wantPass, err, output)
				}
			}
			run(true)
			code := strings.Replace(string(files["protocol.go"]), "int(data[2])", "99", 1)
			if kind == "tcp" {
				code = strings.Replace(string(files["protocol.go"]), "int(data[3])", "99", 1)
			}
			os.WriteFile(filepath.Join(root, "protocol.go"), []byte(code), 0600)
			run(false)
		})
	}
}
