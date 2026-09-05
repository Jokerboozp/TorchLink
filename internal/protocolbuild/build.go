// Package protocolbuild compiles ordinary Go programs for the protocol runtime.
// It never runs build scripts, go generate, tests, or dependency downloads.
package protocolbuild

import (
	"archive/zip"
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/mod/modfile"
)

//go:embed template.go.txt
var Template string

const MaxSource = 32 << 20

// Slots bounds compiler concurrency across tenants in this API process.
var Slots = make(chan struct{}, 1)

func Available() bool { _, err := exec.LookPath("go"); return err == nil }

func Sources(filename string, data []byte) (map[string][]byte, error) {
	if len(data) == 0 || len(data) > MaxSource {
		return nil, errors.New("源码文件须为 1 字节至 32 MiB")
	}
	if strings.EqualFold(filepath.Ext(filename), ".go") {
		return map[string][]byte{"main.go": data}, nil
	}
	if !strings.EqualFold(filepath.Ext(filename), ".zip") {
		return nil, errors.New("请上传 .go 文件或完整 Go 项目 ZIP")
	}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, errors.New("源码 ZIP 格式无效")
	}
	if len(z.File) == 0 || len(z.File) > 4096 {
		return nil, errors.New("源码 ZIP 须包含 1 至 4096 个条目")
	}
	files := map[string][]byte{}
	seen := map[string]bool{}
	var expanded int64
	for _, f := range z.File {
		name := strings.TrimSuffix(f.Name, "/")
		if !localPath(name) || f.Mode()&os.ModeSymlink != 0 || (!f.FileInfo().IsDir() && !f.Mode().IsRegular()) {
			return nil, fmt.Errorf("源码 ZIP 路径不安全: %q", f.Name)
		}
		key := strings.ToLower(name)
		if seen[key] {
			return nil, fmt.Errorf("源码 ZIP 存在重复路径: %q", name)
		}
		seen[key] = true
		if f.FileInfo().IsDir() {
			continue
		}
		if f.UncompressedSize64 > MaxSource {
			return nil, fmt.Errorf("源码文件过大: %q", name)
		}
		expanded += int64(f.UncompressedSize64)
		if expanded > 128<<20 {
			return nil, errors.New("源码 ZIP 展开后不能超过 128 MiB")
		}
		r, err := f.Open()
		if err != nil {
			return nil, err
		}
		content, readErr := io.ReadAll(io.LimitReader(r, MaxSource+1))
		closeErr := r.Close()
		if readErr != nil || closeErr != nil || len(content) > MaxSource {
			return nil, fmt.Errorf("读取源码失败: %q", name)
		}
		files[name] = content
	}
	return files, nil
}

func localPath(name string) bool {
	if name == "" || name == "." || path.Clean(name) != name || strings.ContainsAny(name, "\\:\x00") || !filepath.IsLocal(filepath.FromSlash(name)) {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." || strings.TrimRight(part, ". ") != part {
			return false
		}
	}
	return true
}

// Build uses an isolated source directory and a minimal environment. This is
// process separation, not an OS sandbox; uploads are trusted operator code.
func Build(ctx context.Context, dataDir string, files map[string][]byte, entry string) ([]byte, string, error) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		return nil, "", errors.New("服务器缺少 Go 编译器，请部署包含 Go 工具链的 API 镜像")
	}
	entry = strings.TrimPrefix(strings.TrimSpace(entry), "./")
	if entry == "" {
		entry = "."
	}
	if entry != "." && !localPath(entry) {
		return nil, "", errors.New("编译入口必须是源码项目内的目录，例如 . 或 cmd/worker")
	}
	for name, data := range files {
		if path.Base(name) != "go.mod" {
			continue
		}
		mod, err := modfile.Parse(name, data, nil)
		if err != nil {
			return nil, "", fmt.Errorf("%s 无效: %w", name, err)
		}
		for _, replace := range mod.Replace {
			if replace.New.Version == "" {
				target := replace.New.Path
				resolved := path.Join(path.Dir(name), target)
				if strings.ContainsAny(target, "\\:") || path.IsAbs(target) || (resolved != "." && !localPath(resolved)) {
					return nil, "", fmt.Errorf("%s 的 replace 不能引用源码包以外的目录", name)
				}
			}
		}
	}
	root, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, "", err
	}
	buildRoot := filepath.Join(root, "protocol-builds")
	if err = os.MkdirAll(buildRoot, 0o700); err != nil {
		return nil, "", err
	}
	work, err := os.MkdirTemp(buildRoot, "build-")
	if err != nil {
		return nil, "", err
	}
	defer os.RemoveAll(work) // work is created by MkdirTemp under the absolute build root.
	sourceDir := filepath.Join(work, "src")
	if err = os.Mkdir(sourceDir, 0o700); err != nil {
		return nil, "", err
	}
	for name, data := range files {
		if !localPath(name) {
			return nil, "", fmt.Errorf("源码路径无效: %q", name)
		}
		file := filepath.Join(sourceDir, filepath.FromSlash(name))
		if err = os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			return nil, "", err
		}
		if err = os.WriteFile(file, data, 0o600); err != nil {
			return nil, "", err
		}
	}
	if _, exists := files["go.mod"]; !exists {
		if err = os.WriteFile(filepath.Join(sourceDir, "go.mod"), []byte("module uploaded-protocol\n\ngo 1.25.0\n"), 0o600); err != nil {
			return nil, "", err
		}
	}
	cache := filepath.Join(buildRoot, "cache")
	for _, dir := range []string{cache, filepath.Join(work, "tmp"), filepath.Join(work, "modcache")} {
		if err = os.MkdirAll(dir, 0o700); err != nil {
			return nil, "", err
		}
	}
	output := filepath.Join(work, "worker")
	if runtime.GOOS == "windows" {
		output += ".exe"
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, goBin, "build", "-mod=vendor", "-buildvcs=false", "-trimpath", "-p=2", "-ldflags=-s -w", "-o", output, "./"+entry)
	cmd.Dir = sourceDir
	cmd.Env = []string{"GOENV=off", "GOWORK=off", "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off", "CGO_ENABLED=0", "GOOS=" + runtime.GOOS, "GOARCH=" + runtime.GOARCH, "GOCACHE=" + cache, "GOMODCACHE=" + filepath.Join(work, "modcache"), "GOTMPDIR=" + filepath.Join(work, "tmp"), "HOME=" + work, "USERPROFILE=" + work, "GOMAXPROCS=2"}
	for _, name := range []string{"PATH", "SystemRoot", "WINDIR", "TEMP", "TMP"} {
		if value := os.Getenv(name); value != "" {
			cmd.Env = append(cmd.Env, name+"="+value)
		}
	}
	log := &limitedLog{}
	cmd.Stdout, cmd.Stderr = log, log
	cmd.WaitDelay = time.Second
	err = cmd.Run()
	if ctx.Err() != nil {
		return nil, log.String(), fmt.Errorf("Go 编译取消或超过 120 秒: %w", ctx.Err())
	}
	if err != nil {
		return nil, log.String(), errors.New("Go 编译失败，请根据编译日志修正源码；第三方依赖请随 ZIP 提供 vendor 目录")
	}
	f, err := os.Open(output)
	if err != nil {
		return nil, log.String(), err
	}
	defer f.Close()
	worker, err := io.ReadAll(io.LimitReader(f, (64<<20)+1))
	if err != nil {
		return nil, log.String(), err
	}
	if len(worker) == 0 || len(worker) > 64<<20 {
		return nil, log.String(), errors.New("编译结果须为 1 字节至 64 MiB")
	}
	return worker, log.String(), nil
}

type limitedLog struct{ bytes.Buffer }

func (b *limitedLog) Write(p []byte) (int, error) {
	n := len(p)
	remaining := (64 << 10) - b.Len()
	if remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		_, _ = b.Buffer.Write(p)
	}
	return n, nil
}
