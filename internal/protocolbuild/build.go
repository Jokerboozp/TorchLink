// Package protocolbuild compiles ordinary Go programs for the protocol runtime.
// It never runs build scripts, go generate, tests, or dependency downloads.
package protocolbuild /* 声明 protocolbuild 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"archive/zip"   /* 执行当前语句并推进处理流程。 */
	"bytes"         /* 执行当前语句并推进处理流程。 */
	"context"       /* 执行当前语句并推进处理流程。 */
	_ "embed"       /* 执行当前语句并推进处理流程。 */
	"errors"        /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */
	"io"            /* 执行当前语句并推进处理流程。 */
	"os"            /* 执行当前语句并推进处理流程。 */
	"os/exec"       /* 执行当前语句并推进处理流程。 */
	"path"          /* 执行当前语句并推进处理流程。 */
	"path/filepath" /* 执行当前语句并推进处理流程。 */
	"runtime"       /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */
	"time"          /* 执行当前语句并推进处理流程。 */

	"golang.org/x/mod/modfile"    /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

//go:embed template.go.txt
var Template string /* 声明 Template。 */

const MaxSource = 32 << 20 /* 声明 MaxSource。 */

// Slots bounds compiler concurrency across tenants in this API process.
var Slots = make(chan struct{}, 1) /* 声明 Slots。 */

func Available() bool { _, err := exec.LookPath("go"); return err == nil } /* 定义 Available 函数。 */

func Sources(filename string, data []byte) (map[string][]byte, error) { /* 定义 Sources 函数。 */
	if len(data) == 0 || len(data) > MaxSource { /* 判断条件并选择处理分支。 */
		return nil, errors.New("源码文件须为 1 字节至 32 MiB") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if strings.EqualFold(filepath.Ext(filename), ".go") { /* 判断条件并选择处理分支。 */
		return map[string][]byte{"main.go": data}, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !strings.EqualFold(filepath.Ext(filename), ".zip") { /* 判断条件并选择处理分支。 */
		return nil, errors.New("请上传 .go 文件或完整 Go 项目 ZIP") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data))) /* 更新 err 的值。 */
	if err != nil {                                                  /* 判断条件并选择处理分支。 */
		return nil, errors.New("源码 ZIP 格式无效") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(z.File) == 0 || len(z.File) > 4096 { /* 判断条件并选择处理分支。 */
		return nil, errors.New("源码 ZIP 须包含 1 至 4096 个条目") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	files := map[string][]byte{} /* 更新 files 的值。 */
	seen := map[string]bool{}    /* 更新 seen 的值。 */
	var expanded int64           /* 声明 expanded。 */
	for _, f := range z.File {   /* 循环处理当前数据。 */
		name := strings.TrimSuffix(f.Name, "/")                                                                   /* 更新 name 的值。 */
		if !localPath(name) || f.Mode()&os.ModeSymlink != 0 || (!f.FileInfo().IsDir() && !f.Mode().IsRegular()) { /* 判断条件并选择处理分支。 */
			return nil, fmt.Errorf("源码 ZIP 路径不安全: %q", f.Name) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		key := strings.ToLower(name) /* 更新 key 的值。 */
		if seen[key] {               /* 判断条件并选择处理分支。 */
			return nil, fmt.Errorf("源码 ZIP 存在重复路径: %q", name) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		seen[key] = true          /* 更新 seen[key] 的值。 */
		if f.FileInfo().IsDir() { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if f.UncompressedSize64 > MaxSource { /* 判断条件并选择处理分支。 */
			return nil, fmt.Errorf("源码文件过大: %q", name) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		expanded += int64(f.UncompressedSize64) /* 更新 expanded 的值。 */
		if expanded > 128<<20 {                 /* 判断条件并选择处理分支。 */
			return nil, errors.New("源码 ZIP 展开后不能超过 128 MiB") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		r, err := f.Open() /* 更新 err 的值。 */
		if err != nil {    /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		content, readErr := io.ReadAll(io.LimitReader(r, MaxSource+1))     /* 更新 readErr 的值。 */
		closeErr := r.Close()                                              /* 更新 closeErr 的值。 */
		if readErr != nil || closeErr != nil || len(content) > MaxSource { /* 判断条件并选择处理分支。 */
			return nil, fmt.Errorf("读取源码失败: %q", name) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		files[name] = content /* 更新 files[name] 的值。 */
	} /* 结束当前表达式或代码块。 */
	return files, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func localPath(name string) bool { /* 定义 localPath 函数。 */
	if name == "" || name == "." || path.Clean(name) != name || strings.ContainsAny(name, "\\:\x00") || !filepath.IsLocal(filepath.FromSlash(name)) { /* 判断条件并选择处理分支。 */
		return false /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for _, part := range strings.Split(name, "/") { /* 循环处理当前数据。 */
		if part == ".." || strings.TrimRight(part, ". ") != part { /* 判断条件并选择处理分支。 */
			return false /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return true /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// Build uses an isolated source directory and a minimal environment. This is
// process separation, not an OS sandbox; uploads are trusted operator code.
func Build(ctx context.Context, dataDir string, files map[string][]byte, entry string) ([]byte, string, error) { /* 定义 Build 函数。 */
	return BuildForPlatform(ctx, dataDir, files, entry, runtime.GOOS+"-"+runtime.GOARCH) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// BuildForPlatform cross-compiles only explicitly supported CGO-free targets.
// A foreign binary still requires sample execution on its destination node.
func BuildForPlatform(ctx context.Context, dataDir string, files map[string][]byte, entry, platform string) ([]byte, string, error) { /* 定义 BuildForPlatform 函数。 */
	platform = model.ProtocolPlatform(platform) /* 更新 platform 的值。 */
	if platform == "" {                         /* 判断条件并选择处理分支。 */
		return nil, "", errors.New("unsupported protocol target platform") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	target := strings.Split(platform, "-") /* 更新 target 的值。 */
	goBin, err := exec.LookPath("go")      /* 更新 err 的值。 */
	if err != nil {                        /* 判断条件并选择处理分支。 */
		return nil, "", errors.New("服务器缺少 Go 编译器，请部署包含 Go 工具链的 API 镜像") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	entry = strings.TrimPrefix(strings.TrimSpace(entry), "./") /* 更新 entry 的值。 */
	if entry == "" {                                           /* 判断条件并选择处理分支。 */
		entry = "." /* 更新 entry 的值。 */
	} /* 结束当前表达式或代码块。 */
	if entry != "." && !localPath(entry) { /* 判断条件并选择处理分支。 */
		return nil, "", errors.New("编译入口必须是源码项目内的目录，例如 . 或 cmd/worker") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for name, data := range files { /* 循环处理当前数据。 */
		if path.Base(name) != "go.mod" { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		mod, err := modfile.Parse(name, data, nil) /* 检查错误并决定后续处理。 */
		if err != nil {                            /* 判断条件并选择处理分支。 */
			return nil, "", fmt.Errorf("%s 无效: %w", name, err) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		for _, replace := range mod.Replace { /* 循环处理当前数据。 */
			if replace.New.Version == "" { /* 判断条件并选择处理分支。 */
				target := replace.New.Path                                                                                 /* 更新 target 的值。 */
				resolved := path.Join(path.Dir(name), target)                                                              /* 更新 resolved 的值。 */
				if strings.ContainsAny(target, "\\:") || path.IsAbs(target) || (resolved != "." && !localPath(resolved)) { /* 判断条件并选择处理分支。 */
					return nil, "", fmt.Errorf("%s 的 replace 不能引用源码包以外的目录", name) /* 返回当前处理结果。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	root, err := filepath.Abs(dataDir) /* 更新 err 的值。 */
	if err != nil {                    /* 判断条件并选择处理分支。 */
		return nil, "", err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	buildRoot := filepath.Join(root, "protocol-builds")  /* 更新 buildRoot 的值。 */
	if err = os.MkdirAll(buildRoot, 0o700); err != nil { /* 判断条件并选择处理分支。 */
		return nil, "", err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	work, err := os.MkdirTemp(buildRoot, "build-") /* 更新 err 的值。 */
	if err != nil {                                /* 判断条件并选择处理分支。 */
		return nil, "", err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer os.RemoveAll(work)                          /* 安排函数结束时执行清理。 */ // work is created by MkdirTemp under the absolute build root.
	sourceDir := filepath.Join(work, "src")           /* 更新 sourceDir 的值。 */
	if err = os.Mkdir(sourceDir, 0o700); err != nil { /* 判断条件并选择处理分支。 */
		return nil, "", err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for name, data := range files { /* 循环处理当前数据。 */
		if !localPath(name) { /* 判断条件并选择处理分支。 */
			return nil, "", fmt.Errorf("源码路径无效: %q", name) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		file := filepath.Join(sourceDir, filepath.FromSlash(name))    /* 更新 file 的值。 */
		if err = os.MkdirAll(filepath.Dir(file), 0o700); err != nil { /* 判断条件并选择处理分支。 */
			return nil, "", err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if err = os.WriteFile(file, data, 0o600); err != nil { /* 判断条件并选择处理分支。 */
			return nil, "", err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if _, exists := files["go.mod"]; !exists { /* 判断条件并选择处理分支。 */
		if err = os.WriteFile(filepath.Join(sourceDir, "go.mod"), []byte("module uploaded-protocol\n\ngo 1.25.0\n"), 0o600); err != nil { /* 判断条件并选择处理分支。 */
			return nil, "", err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	cache := filepath.Join(buildRoot, "cache")                                                         /* 更新 cache 的值。 */
	for _, dir := range []string{cache, filepath.Join(work, "tmp"), filepath.Join(work, "modcache")} { /* 循环处理当前数据。 */
		if err = os.MkdirAll(dir, 0o700); err != nil { /* 判断条件并选择处理分支。 */
			return nil, "", err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	output := filepath.Join(work, "worker") /* 更新 output 的值。 */
	if target[0] == "windows" {             /* 判断条件并选择处理分支。 */
		output += ".exe" /* 更新 output 的值。 */
	} /* 结束当前表达式或代码块。 */
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)                                                                                                                                                                                                                                                                              /* 更新 cancel 的值。 */
	defer cancel()                                                                                                                                                                                                                                                                                                                      /* 安排函数结束时执行清理。 */
	cmd := exec.CommandContext(ctx, goBin, "build", "-mod=vendor", "-buildvcs=false", "-trimpath", "-p=2", "-ldflags=-s -w", "-o", output, "./"+entry)                                                                                                                                                                                  /* 更新 cmd 的值。 */
	cmd.Dir = sourceDir                                                                                                                                                                                                                                                                                                                 /* 更新 cmd.Dir 的值。 */
	cmd.Env = []string{"GOENV=off", "GOWORK=off", "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off", "CGO_ENABLED=0", "GOOS=" + target[0], "GOARCH=" + target[1], "GOCACHE=" + cache, "GOMODCACHE=" + filepath.Join(work, "modcache"), "GOTMPDIR=" + filepath.Join(work, "tmp"), "HOME=" + work, "USERPROFILE=" + work, "GOMAXPROCS=2"} /* 更新 cmd.Env 的值。 */
	for _, name := range []string{"PATH", "SystemRoot", "WINDIR", "TEMP", "TMP"} {                                                                                                                                                                                                                                                      /* 循环处理当前数据。 */
		if value := os.Getenv(name); value != "" { /* 判断条件并选择处理分支。 */
			cmd.Env = append(cmd.Env, name+"="+value) /* 更新 cmd.Env 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	log := &limitedLog{}              /* 更新 log 的值。 */
	cmd.Stdout, cmd.Stderr = log, log /* 更新 cmd.Stderr 的值。 */
	cmd.WaitDelay = time.Second       /* 更新 cmd.WaitDelay 的值。 */
	err = cmd.Run()                   /* 更新 err 的值。 */
	if ctx.Err() != nil {             /* 判断条件并选择处理分支。 */
		return nil, log.String(), fmt.Errorf("Go 编译取消或超过 120 秒: %w", ctx.Err()) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err != nil { /* 判断条件并选择处理分支。 */
		return nil, log.String(), errors.New("Go 编译失败，请根据编译日志修正源码；第三方依赖请随 ZIP 提供 vendor 目录") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	f, err := os.Open(output) /* 更新 err 的值。 */
	if err != nil {           /* 判断条件并选择处理分支。 */
		return nil, log.String(), err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer f.Close()                                          /* 安排函数结束时执行清理。 */
	worker, err := io.ReadAll(io.LimitReader(f, (64<<20)+1)) /* 更新 err 的值。 */
	if err != nil {                                          /* 判断条件并选择处理分支。 */
		return nil, log.String(), err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(worker) == 0 || len(worker) > 64<<20 { /* 判断条件并选择处理分支。 */
		return nil, log.String(), errors.New("编译结果须为 1 字节至 64 MiB") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return worker, log.String(), nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

type limitedLog struct{ bytes.Buffer } /* 定义 limitedLog 类型。 */

func (b *limitedLog) Write(p []byte) (int, error) { /* 定义 Write 函数。 */
	n := len(p)                       /* 更新 n 的值。 */
	remaining := (64 << 10) - b.Len() /* 更新 remaining 的值。 */
	if remaining > 0 {                /* 判断条件并选择处理分支。 */
		if len(p) > remaining { /* 判断条件并选择处理分支。 */
			p = p[:remaining] /* 更新 p 的值。 */
		} /* 结束当前表达式或代码块。 */
		_, _ = b.Buffer.Write(p) /* 更新 _ 的值。 */
	} /* 结束当前表达式或代码块。 */
	return n, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
