package protocolbuild

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	external "iot-platform/internal/parser"
)

//go:embed functiontemplates/platform.go.txt
var FunctionAdapter string

//go:embed functiontemplates/protocol.go.txt
var FunctionTemplate string

//go:embed functiontemplates/tcp.go.txt
var FunctionTCPTemplate string

//go:embed functiontemplates/platform_test.go.txt
var FunctionTestTemplate string

// PrepareFunctions recognizes the explicit Go entry point. Existing main-based
// packages continue through the original build path without rewriting source.
func PrepareFunctions(files map[string][]byte) (bool, error) {
	// ZIP tools commonly include one enclosing folder. Flatten only a recognized
	// function project; legacy project entrypoint semantics stay unchanged.
	candidate := files
	prefix := ""
	for name := range files {
		parts := strings.SplitN(name, "/", 2)
		if len(parts) != 2 {
			prefix = ""
			break
		}
		if prefix == "" {
			prefix = parts[0] + "/"
		} else if !strings.HasPrefix(name, prefix) {
			prefix = ""
			break
		}
	}
	if prefix != "" {
		candidate = map[string][]byte{}
		for name, data := range files {
			candidate[strings.TrimPrefix(name, prefix)] = data
		}
	}
	found := false
	for name, data := range candidate {
		if path.Dir(name) != "." || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, data, 0)
		if err != nil {
			return false, fmt.Errorf("%s: %w", name, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Name.Name != "Protocol" || fn.Type.Params.NumFields() != 0 || fn.Type.Results.NumFields() != 1 {
				continue
			}
			if result, ok := fn.Type.Results.List[0].Type.(*ast.Ident); ok && result.Name == "Definition" {
				found = true
			}
		}
	}
	if !found {
		return false, nil
	}
	if prefix != "" {
		for name := range files {
			delete(files, name)
		}
		for name, data := range candidate {
			files[name] = data
		}
	}
	const adapter = "zz_platform.go"
	if data, ok := files[adapter]; ok {
		if !bytes.Equal(data, []byte(FunctionAdapter)) {
			return false, fmt.Errorf("%s 是平台适配文件，请使用原始模板，业务代码放在其他 Go 文件", adapter)
		}
	} else {
		files[adapter] = []byte(FunctionAdapter)
	}
	return true, nil
}

func FunctionTemplateZIP(kind ...string) ([]byte, error) {
	source := FunctionTemplate
	if len(kind) > 0 && kind[0] == "tcp" {
		source = FunctionTCPTemplate
	}
	var data bytes.Buffer
	z := zip.NewWriter(&data)
	for _, file := range []struct{ name, content string }{{"go.mod", "module device-protocol\n\ngo 1.25.0\n"}, {"protocol.go", source}, {"zz_platform.go", FunctionAdapter}, {"zz_platform_test.go", FunctionTestTemplate}} {
		w, err := z.Create(file.name)
		if err != nil {
			return nil, err
		}
		if _, err = w.Write([]byte(file.content)); err != nil {
			return nil, err
		}
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return data.Bytes(), nil
}

// DescribeFunctions executes the compiled program with the existing worker
// timeout, bounded output, minimal environment, and verified binary hash.
func DescribeFunctions(ctx context.Context, worker []byte) (map[string]json.RawMessage, error) {
	root, err := os.MkdirTemp("", "protocol-describe-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(root)
	name := "worker"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err = os.WriteFile(filepath.Join(root, name), worker, 0700); err != nil {
		return nil, err
	}
	digest := sha256.Sum256(worker)
	output, err := (external.ExternalParser{Root: root}).Invoke(ctx, map[string]any{"artifact": map[string]any{"path": name, "sha256": hex.EncodeToString(digest[:])}}, map[string]any{"operation": "describe"})
	if err != nil {
		return nil, err
	}
	var result map[string]json.RawMessage
	if err = json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("读取 Go 配置失败: %w", err)
	}
	if failure := result["error"]; len(failure) > 0 {
		return nil, fmt.Errorf("读取 Go 配置失败: %s", failure)
	}
	if len(result["metadata"]) == 0 || len(result["cases"]) == 0 {
		return nil, fmt.Errorf("Go 协议未返回配置或样例")
	}
	return result, nil
}
