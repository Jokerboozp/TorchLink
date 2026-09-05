package protocolbuild

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
)

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
