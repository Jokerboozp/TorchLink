package protocolbuild

import (
	"archive/zip"
	"bytes"
	"context"
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"iot-platform/internal/model"
)

// TestMain shares one Go build cache across the package's builds, each of
// which uses its own temporary data directory.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "protocolbuild-cache-")
	if err != nil {
		panic(err)
	}
	sharedCacheDir = dir
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func TestSourceZIPRejectsUnsafePaths(t *testing.T) {
	for _, name := range []string{"../escape.go", "/root.go", "C:/root.go", `dir\file.go`, "a/../file.go", "a.go:stream", "trailing./file.go"} {
		t.Run(name, func(t *testing.T) {
			var b bytes.Buffer
			z := zip.NewWriter(&b)
			f, _ := z.Create(name)
			_, _ = f.Write([]byte("package main"))
			_ = z.Close()
			if _, err := Sources("source.zip", b.Bytes()); err == nil {
				t.Fatal("unsafe path accepted")
			}
		})
	}
	for _, symlink := range []bool{false, true} {
		var b bytes.Buffer
		z := zip.NewWriter(&b)
		if symlink {
			h := &zip.FileHeader{Name: "main.go"}
			h.SetMode(os.ModeSymlink | 0o777)
			f, _ := z.CreateHeader(h)
			_, _ = f.Write([]byte("../secret"))
		} else {
			for _, name := range []string{"main.go", "MAIN.go"} {
				f, _ := z.Create(name)
				_, _ = f.Write([]byte("package main"))
			}
		}
		_ = z.Close()
		if _, err := Sources("source.zip", b.Bytes()); err == nil {
			t.Fatalf("symlink=%v accepted", symlink)
		}
	}
}

func TestBuildRejectsOutsideReplacementAndCancellation(t *testing.T) {
	if !Available() {
		t.Skip("Go compiler unavailable")
	}
	for _, target := range []string{"../outside", "/etc", "C:/secrets"} {
		files := map[string][]byte{"go.mod": []byte("module example.com/protocol\n\ngo 1.25\nreplace example.com/lib => " + target + "\n")}
		_, _, err := Build(context.Background(), t.TempDir(), files, ".")
		if err == nil || !strings.Contains(err.Error(), "replace") {
			t.Fatalf("target=%s err=%v", target, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := Build(ctx, t.TempDir(), map[string][]byte{"main.go": []byte(Template)}, "."); err == nil {
		t.Fatal("canceled build succeeded")
	}
}

func TestCrossPlatformCompilerProducesActualTargets(t *testing.T) {
	if !Available() {
		t.Skip("Go compiler unavailable")
	}
	root := t.TempDir()
	source := map[string][]byte{"main.go": []byte("package main\nfunc main() {}")}
	for _, target := range model.ProtocolPlatforms() {
		t.Run(target, func(t *testing.T) {
			data, log, err := BuildForPlatform(context.Background(), root, source, ".", target)
			if err != nil {
				t.Fatalf("%v %s", err, log)
			}
			switch {
			case strings.HasPrefix(target, "linux"):
				binary, err := elf.NewFile(bytes.NewReader(data))
				if err != nil {
					t.Fatal(err)
				}
				defer binary.Close()
				want := elf.EM_X86_64
				if strings.HasSuffix(target, "arm64") {
					want = elf.EM_AARCH64
				}
				if binary.Machine != want {
					t.Fatal(binary.Machine)
				}
			case strings.HasPrefix(target, "windows"):
				binary, err := pe.NewFile(bytes.NewReader(data))
				if err != nil {
					t.Fatal(err)
				}
				defer binary.Close()
				want := uint16(pe.IMAGE_FILE_MACHINE_AMD64)
				if strings.HasSuffix(target, "arm64") {
					want = pe.IMAGE_FILE_MACHINE_ARM64
				}
				if binary.Machine != want {
					t.Fatal(binary.Machine)
				}
			case strings.HasPrefix(target, "darwin"):
				binary, err := macho.NewFile(bytes.NewReader(data))
				if err != nil {
					t.Fatal(err)
				}
				defer binary.Close()
				want := macho.CpuAmd64
				if strings.HasSuffix(target, "arm64") {
					want = macho.CpuArm64
				}
				if binary.Cpu != want {
					t.Fatal(binary.Cpu)
				}
			}
		})
	}
	if _, _, err := BuildForPlatform(context.Background(), root, source, ".", "linux/arm64;echo invalid"); err == nil {
		t.Fatal("invalid target accepted")
	}
}

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
