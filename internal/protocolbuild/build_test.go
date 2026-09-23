package protocolbuild /* 声明 protocolbuild 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"archive/zip"                 /* 执行当前语句并推进处理流程。 */
	"bytes"                       /* 执行当前语句并推进处理流程。 */
	"context"                     /* 执行当前语句并推进处理流程。 */
	"debug/elf"                   /* 执行当前语句并推进处理流程。 */
	"debug/macho"                 /* 执行当前语句并推进处理流程。 */
	"debug/pe"                    /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"os"                          /* 执行当前语句并推进处理流程。 */
	"strings"                     /* 执行当前语句并推进处理流程。 */
	"testing"                     /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestSourceZIPRejectsUnsafePaths(t *testing.T) { /* 定义 TestSourceZIPRejectsUnsafePaths 函数。 */
	for _, name := range []string{"../escape.go", "/root.go", "C:/root.go", `dir\file.go`, "a/../file.go", "a.go:stream", "trailing./file.go"} { /* 循环处理当前数据。 */
		t.Run(name, func(t *testing.T) { /* 执行当前语句并推进处理流程。 */
			var b bytes.Buffer                                          /* 声明 b。 */
			z := zip.NewWriter(&b)                                      /* 更新 z 的值。 */
			f, _ := z.Create(name)                                      /* 更新 _ 的值。 */
			_, _ = f.Write([]byte("package main"))                      /* 更新 _ 的值。 */
			_ = z.Close()                                               /* 更新 _ 的值。 */
			if _, err := Sources("source.zip", b.Bytes()); err == nil { /* 判断条件并选择处理分支。 */
				t.Fatal("unsafe path accepted") /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		}) /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	for _, symlink := range []bool{false, true} { /* 循环处理当前数据。 */
		var b bytes.Buffer     /* 声明 b。 */
		z := zip.NewWriter(&b) /* 更新 z 的值。 */
		if symlink {           /* 判断条件并选择处理分支。 */
			h := &zip.FileHeader{Name: "main.go"} /* 更新 h 的值。 */
			h.SetMode(os.ModeSymlink | 0o777)     /* 执行当前语句并推进处理流程。 */
			f, _ := z.CreateHeader(h)             /* 更新 _ 的值。 */
			_, _ = f.Write([]byte("../secret"))   /* 更新 _ 的值。 */
		} else { /* 结束当前表达式或代码块。 */
			for _, name := range []string{"main.go", "MAIN.go"} { /* 循环处理当前数据。 */
				f, _ := z.Create(name)                 /* 更新 _ 的值。 */
				_, _ = f.Write([]byte("package main")) /* 更新 _ 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		_ = z.Close()                                               /* 更新 _ 的值。 */
		if _, err := Sources("source.zip", b.Bytes()); err == nil { /* 判断条件并选择处理分支。 */
			t.Fatalf("symlink=%v accepted", symlink) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestBuildRejectsOutsideReplacementAndCancellation(t *testing.T) { /* 定义 TestBuildRejectsOutsideReplacementAndCancellation 函数。 */
	if !Available() { /* 判断条件并选择处理分支。 */
		t.Skip("Go compiler unavailable") /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	for _, target := range []string{"../outside", "/etc", "C:/secrets"} { /* 循环处理当前数据。 */
		files := map[string][]byte{"go.mod": []byte("module example.com/protocol\n\ngo 1.25\nreplace example.com/lib => " + target + "\n")} /* 更新 files 的值。 */
		_, _, err := Build(context.Background(), t.TempDir(), files, ".")                                                                   /* 更新 err 的值。 */
		if err == nil || !strings.Contains(err.Error(), "replace") {                                                                        /* 判断条件并选择处理分支。 */
			t.Fatalf("target=%s err=%v", target, err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	ctx, cancel := context.WithCancel(context.Background())                                                    /* 更新 cancel 的值。 */
	cancel()                                                                                                   /* 执行当前语句并推进处理流程。 */
	if _, _, err := Build(ctx, t.TempDir(), map[string][]byte{"main.go": []byte(Template)}, "."); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("canceled build succeeded") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestCrossPlatformCompilerProducesActualTargets(t *testing.T) { /* 定义 TestCrossPlatformCompilerProducesActualTargets 函数。 */
	if !Available() { /* 判断条件并选择处理分支。 */
		t.Skip("Go compiler unavailable") /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	root := t.TempDir()                                                            /* 更新 root 的值。 */
	source := map[string][]byte{"main.go": []byte("package main\nfunc main() {}")} /* 更新 source 的值。 */
	for _, target := range model.ProtocolPlatforms() {                             /* 循环处理当前数据。 */
		t.Run(target, func(t *testing.T) { /* 执行当前语句并推进处理流程。 */
			data, log, err := BuildForPlatform(context.Background(), root, source, ".", target) /* 更新 err 的值。 */
			if err != nil {                                                                     /* 判断条件并选择处理分支。 */
				t.Fatalf("%v %s", err, log) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			switch { /* 根据条件选择处理路径。 */
			case strings.HasPrefix(target, "linux"): /* 处理当前分支。 */
				binary, err := elf.NewFile(bytes.NewReader(data)) /* 更新 err 的值。 */
				if err != nil {                                   /* 判断条件并选择处理分支。 */
					t.Fatal(err) /* 验证实际结果符合预期。 */
				} /* 结束当前表达式或代码块。 */
				defer binary.Close()                    /* 安排函数结束时执行清理。 */
				want := elf.EM_X86_64                   /* 更新 want 的值。 */
				if strings.HasSuffix(target, "arm64") { /* 判断条件并选择处理分支。 */
					want = elf.EM_AARCH64 /* 更新 want 的值。 */
				} /* 结束当前表达式或代码块。 */
				if binary.Machine != want { /* 判断条件并选择处理分支。 */
					t.Fatal(binary.Machine) /* 验证实际结果符合预期。 */
				} /* 结束当前表达式或代码块。 */
			case strings.HasPrefix(target, "windows"): /* 处理当前分支。 */
				binary, err := pe.NewFile(bytes.NewReader(data)) /* 更新 err 的值。 */
				if err != nil {                                  /* 判断条件并选择处理分支。 */
					t.Fatal(err) /* 验证实际结果符合预期。 */
				} /* 结束当前表达式或代码块。 */
				defer binary.Close()                        /* 安排函数结束时执行清理。 */
				want := uint16(pe.IMAGE_FILE_MACHINE_AMD64) /* 更新 want 的值。 */
				if strings.HasSuffix(target, "arm64") {     /* 判断条件并选择处理分支。 */
					want = pe.IMAGE_FILE_MACHINE_ARM64 /* 更新 want 的值。 */
				} /* 结束当前表达式或代码块。 */
				if binary.Machine != want { /* 判断条件并选择处理分支。 */
					t.Fatal(binary.Machine) /* 验证实际结果符合预期。 */
				} /* 结束当前表达式或代码块。 */
			case strings.HasPrefix(target, "darwin"): /* 处理当前分支。 */
				binary, err := macho.NewFile(bytes.NewReader(data)) /* 更新 err 的值。 */
				if err != nil {                                     /* 判断条件并选择处理分支。 */
					t.Fatal(err) /* 验证实际结果符合预期。 */
				} /* 结束当前表达式或代码块。 */
				defer binary.Close()                    /* 安排函数结束时执行清理。 */
				want := macho.CpuAmd64                  /* 更新 want 的值。 */
				if strings.HasSuffix(target, "arm64") { /* 判断条件并选择处理分支。 */
					want = macho.CpuArm64 /* 更新 want 的值。 */
				} /* 结束当前表达式或代码块。 */
				if binary.Cpu != want { /* 判断条件并选择处理分支。 */
					t.Fatal(binary.Cpu) /* 验证实际结果符合预期。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
		}) /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if _, _, err := BuildForPlatform(context.Background(), root, source, ".", "linux/arm64;echo invalid"); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("invalid target accepted") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
