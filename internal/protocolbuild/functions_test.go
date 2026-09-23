package protocolbuild /* 声明 protocolbuild 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bytes"         /* 执行当前语句并推进处理流程。 */
	"context"       /* 执行当前语句并推进处理流程。 */
	"os"            /* 执行当前语句并推进处理流程。 */
	"os/exec"       /* 执行当前语句并推进处理流程。 */
	"path/filepath" /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */
	"testing"       /* 执行当前语句并推进处理流程。 */
	"time"          /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestPrepareFunctionsCompatibility(t *testing.T) { /* 定义 TestPrepareFunctionsCompatibility 函数。 */
	for _, legacy := range []map[string][]byte{ /* 循环处理当前数据。 */
		{"main.go": []byte(Template)},                                                                  /* 执行当前语句并推进处理流程。 */
		{"project/cmd/worker/main.go": []byte("package main\nfunc main() {}")},                         /* 执行当前语句并推进处理流程。 */
		{"main.go": []byte("package main\nfunc Protocol() string{return \"legacy\"}\nfunc main() {}")}, /* 执行当前语句并推进处理流程。 */
	} { /* 结束当前表达式或代码块。 */
		before := len(legacy)                          /* 更新 before 的值。 */
		ok, err := PrepareFunctions(legacy)            /* 更新 err 的值。 */
		if err != nil || ok || len(legacy) != before { /* 判断条件并选择处理分支。 */
			t.Fatalf("legacy modified: %v %v", ok, err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	files := map[string][]byte{"project/protocol.go": []byte(FunctionTemplate)}                                                 /* 更新 files 的值。 */
	ok, err := PrepareFunctions(files)                                                                                          /* 更新 err 的值。 */
	if err != nil || !ok || !bytes.Equal(files["protocol.go"], []byte(FunctionTemplate)) || len(files["zz_platform.go"]) == 0 { /* 判断条件并选择处理分支。 */
		t.Fatalf("wrapped project %v %v", ok, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	files["zz_platform.go"] = []byte("package main")   /* 执行当前语句并推进处理流程。 */
	if _, err := PrepareFunctions(files); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("modified adapter accepted") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestFunctionDescriptionLimits(t *testing.T) { /* 定义 TestFunctionDescriptionLimits 函数。 */
	files := map[string][]byte{"protocol.go": []byte(strings.Replace(FunctionTemplate, "return Definition{", "for {}\nreturn Definition{", 1))} /* 更新 files 的值。 */
	if _, err := PrepareFunctions(files); err != nil {                                                                                          /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	worker, log, err := Build(context.Background(), t.TempDir(), files, ".") /* 更新 err 的值。 */
	if err != nil {                                                          /* 判断条件并选择处理分支。 */
		t.Fatal(err, log) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond) /* 更新 cancel 的值。 */
	defer cancel()                                                                 /* 安排函数结束时执行清理。 */
	started := time.Now()                                                          /* 更新 started 的值。 */
	if _, err := DescribeFunctions(ctx, worker); err == nil {                      /* 判断条件并选择处理分支。 */
		t.Fatal("nonterminating configuration succeeded") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if time.Since(started) > 3*time.Second { /* 判断条件并选择处理分支。 */
		t.Fatal("description ignored cancellation") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestDownloadedTemplatesRunRealSamplesLocally(t *testing.T) { /* 定义 TestDownloadedTemplatesRunRealSamplesLocally 函数。 */
	for _, kind := range []string{"", "tcp"} { /* 循环处理当前数据。 */
		t.Run("template-"+kind, func(t *testing.T) { /* 执行当前语句并推进处理流程。 */
			data, err := FunctionTemplateZIP(kind) /* 更新 err 的值。 */
			if err != nil {                        /* 判断条件并选择处理分支。 */
				t.Fatal(err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			files, err := Sources("template.zip", data) /* 更新 err 的值。 */
			if err != nil {                             /* 判断条件并选择处理分支。 */
				t.Fatal(err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			root := t.TempDir()             /* 更新 root 的值。 */
			for name, data := range files { /* 循环处理当前数据。 */
				if err := os.WriteFile(filepath.Join(root, name), data, 0600); err != nil { /* 判断条件并选择处理分支。 */
					t.Fatal(err) /* 验证实际结果符合预期。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
			run := func(wantPass bool) { /* 更新 run 的值。 */
				t.Helper()                                                               /* 执行当前语句并推进处理流程。 */
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second) /* 更新 cancel 的值。 */
				defer cancel()                                                           /* 安排函数结束时执行清理。 */
				cmd := exec.CommandContext(ctx, "go", "test", "-count=1", "./...")       /* 更新 cmd 的值。 */
				cmd.Dir = root                                                           /* 更新 cmd.Dir 的值。 */
				output, err := cmd.CombinedOutput()                                      /* 更新 err 的值。 */
				if (err == nil) != wantPass || ctx.Err() != nil {                        /* 判断条件并选择处理分支。 */
					t.Fatalf("want pass=%v: %v %s", wantPass, err, output) /* 验证实际结果符合预期。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
			run(true)                                                                      /* 执行当前语句并推进处理流程。 */
			code := strings.Replace(string(files["protocol.go"]), "int(data[2])", "99", 1) /* 更新 code 的值。 */
			if kind == "tcp" {                                                             /* 判断条件并选择处理分支。 */
				code = strings.Replace(string(files["protocol.go"]), "int(data[3])", "99", 1) /* 更新 code 的值。 */
			} /* 结束当前表达式或代码块。 */
			os.WriteFile(filepath.Join(root, "protocol.go"), []byte(code), 0600) /* 执行当前语句并推进处理流程。 */
			run(false)                                                           /* 执行当前语句并推进处理流程。 */
		}) /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
