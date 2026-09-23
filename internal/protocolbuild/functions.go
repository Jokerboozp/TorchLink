package protocolbuild /* 声明 protocolbuild 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"archive/zip"   /* 执行当前语句并推进处理流程。 */
	"bytes"         /* 执行当前语句并推进处理流程。 */
	"context"       /* 执行当前语句并推进处理流程。 */
	"crypto/sha256" /* 执行当前语句并推进处理流程。 */
	_ "embed"       /* 执行当前语句并推进处理流程。 */
	"encoding/hex"  /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */
	"go/ast"        /* 执行当前语句并推进处理流程。 */
	"go/parser"     /* 执行当前语句并推进处理流程。 */
	"go/token"      /* 执行当前语句并推进处理流程。 */
	"os"            /* 执行当前语句并推进处理流程。 */
	"path"          /* 执行当前语句并推进处理流程。 */
	"path/filepath" /* 执行当前语句并推进处理流程。 */
	"runtime"       /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */

	external "iot-platform/internal/parser" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

//go:embed functiontemplates/platform.go.txt
var FunctionAdapter string /* 声明 FunctionAdapter。 */

//go:embed functiontemplates/protocol.go.txt
var FunctionTemplate string /* 声明 FunctionTemplate。 */

//go:embed functiontemplates/tcp.go.txt
var FunctionTCPTemplate string /* 声明 FunctionTCPTemplate。 */

//go:embed functiontemplates/platform_test.go.txt
var FunctionTestTemplate string /* 声明 FunctionTestTemplate。 */

// PrepareFunctions recognizes the explicit Go entry point. Existing main-based
// packages continue through the original build path without rewriting source.
func PrepareFunctions(files map[string][]byte) (bool, error) { /* 定义 PrepareFunctions 函数。 */
	// ZIP tools commonly include one enclosing folder. Flatten only a recognized
	// function project; complete Go projects keep their configured entrypoint.
	candidate := files        /* 更新 candidate 的值。 */
	prefix := ""              /* 更新 prefix 的值。 */
	for name := range files { /* 循环处理当前数据。 */
		parts := strings.SplitN(name, "/", 2) /* 更新 parts 的值。 */
		if len(parts) != 2 {                  /* 判断条件并选择处理分支。 */
			prefix = "" /* 更新 prefix 的值。 */
			break       /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if prefix == "" { /* 判断条件并选择处理分支。 */
			prefix = parts[0] + "/" /* 更新 prefix 的值。 */
		} else if !strings.HasPrefix(name, prefix) { /* 结束当前表达式或代码块。 */
			prefix = "" /* 更新 prefix 的值。 */
			break       /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if prefix != "" { /* 判断条件并选择处理分支。 */
		candidate = map[string][]byte{} /* 更新 candidate 的值。 */
		for name, data := range files { /* 循环处理当前数据。 */
			candidate[strings.TrimPrefix(name, prefix)] = data /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	found := false                      /* 更新 found 的值。 */
	for name, data := range candidate { /* 循环处理当前数据。 */
		if path.Dir(name) != "." || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		file, err := parser.ParseFile(token.NewFileSet(), name, data, 0) /* 更新 err 的值。 */
		if err != nil {                                                  /* 判断条件并选择处理分支。 */
			return false, fmt.Errorf("%s: %w", name, err) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		for _, decl := range file.Decls { /* 循环处理当前数据。 */
			fn, ok := decl.(*ast.FuncDecl)                                                                                                  /* 更新 ok 的值。 */
			if !ok || fn.Recv != nil || fn.Name.Name != "Protocol" || fn.Type.Params.NumFields() != 0 || fn.Type.Results.NumFields() != 1 { /* 判断条件并选择处理分支。 */
				continue /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			if result, ok := fn.Type.Results.List[0].Type.(*ast.Ident); ok && result.Name == "Definition" { /* 判断条件并选择处理分支。 */
				found = true /* 更新 found 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if !found { /* 判断条件并选择处理分支。 */
		return false, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if prefix != "" { /* 判断条件并选择处理分支。 */
		for name := range files { /* 循环处理当前数据。 */
			delete(files, name) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		for name, data := range candidate { /* 循环处理当前数据。 */
			files[name] = data /* 更新 files[name] 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	const adapter = "zz_platform.go"    /* 声明 adapter。 */
	if data, ok := files[adapter]; ok { /* 判断条件并选择处理分支。 */
		if !bytes.Equal(data, []byte(FunctionAdapter)) { /* 判断条件并选择处理分支。 */
			return false, fmt.Errorf("%s 是平台适配文件，请使用原始模板，业务代码放在其他 Go 文件", adapter) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} else { /* 结束当前表达式或代码块。 */
		files[adapter] = []byte(FunctionAdapter) /* 更新 files[adapter] 的值。 */
	} /* 结束当前表达式或代码块。 */
	return true, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func FunctionTemplateZIP(kind ...string) ([]byte, error) { /* 定义 FunctionTemplateZIP 函数。 */
	source := FunctionTemplate             /* 更新 source 的值。 */
	if len(kind) > 0 && kind[0] == "tcp" { /* 判断条件并选择处理分支。 */
		source = FunctionTCPTemplate /* 更新 source 的值。 */
	} /* 结束当前表达式或代码块。 */
	var data bytes.Buffer                                                                                                                                                                                                     /* 声明 data。 */
	z := zip.NewWriter(&data)                                                                                                                                                                                                 /* 更新 z 的值。 */
	for _, file := range []struct{ name, content string }{{"go.mod", "module device-protocol\n\ngo 1.25.0\n"}, {"protocol.go", source}, {"zz_platform.go", FunctionAdapter}, {"zz_platform_test.go", FunctionTestTemplate}} { /* 循环处理当前数据。 */
		w, err := z.Create(file.name) /* 更新 err 的值。 */
		if err != nil {               /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if _, err = w.Write([]byte(file.content)); err != nil { /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if err := z.Close(); err != nil { /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return data.Bytes(), nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// DescribeFunctions executes the compiled program with the existing worker
// timeout, bounded output, minimal environment, and verified binary hash.
func DescribeFunctions(ctx context.Context, worker []byte) (map[string]json.RawMessage, error) { /* 定义 DescribeFunctions 函数。 */
	root, err := os.MkdirTemp("", "protocol-describe-") /* 更新 err 的值。 */
	if err != nil {                                     /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer os.RemoveAll(root)       /* 安排函数结束时执行清理。 */
	name := "worker"               /* 更新 name 的值。 */
	if runtime.GOOS == "windows" { /* 判断条件并选择处理分支。 */
		name += ".exe" /* 更新 name 的值。 */
	} /* 结束当前表达式或代码块。 */
	if err = os.WriteFile(filepath.Join(root, name), worker, 0700); err != nil { /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	digest := sha256.Sum256(worker)                                                                                                                                                                              /* 更新 digest 的值。 */
	output, err := (external.ExternalParser{Root: root}).Invoke(ctx, map[string]any{"artifact": map[string]any{"path": name, "sha256": hex.EncodeToString(digest[:])}}, map[string]any{"operation": "describe"}) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                              /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var result map[string]json.RawMessage                  /* 声明 result。 */
	if err = json.Unmarshal(output, &result); err != nil { /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("读取 Go 配置失败: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if failure := result["error"]; len(failure) > 0 { /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("读取 Go 配置失败: %s", failure) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(result["metadata"]) == 0 || len(result["cases"]) == 0 { /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("Go 协议未返回配置或样例") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return result, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
