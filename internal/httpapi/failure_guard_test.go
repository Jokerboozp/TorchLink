package httpapi

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"iot-platform/internal/logctx"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// A literal 500 written with problem() drops the error and the reference the
// operations guide tells operators to search for; handlers use s.failure.
func TestHandlersReportInternalErrorsWithReference(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) < 2 {
				return true
			}
			if fn, ok := call.Fun.(*ast.Ident); !ok || fn.Name != "problem" {
				return true
			}
			switch status := call.Args[1].(type) {
			case *ast.BasicLit:
				if status.Value == "500" {
					t.Errorf("%s: use s.failure for internal errors", fset.Position(call.Pos()))
				}
			case *ast.SelectorExpr:
				if status.Sel.Name == "StatusInternalServerError" {
					t.Errorf("%s: use s.failure for internal errors", fset.Position(call.Pos()))
				}
			}
			return true
		})
	}
}

func TestFailureLogsTheReferenceItReturns(t *testing.T) {
	var logs strings.Builder
	server := &Server{log: slog.New(slog.NewTextHandler(&logs, nil))}
	r := httptest.NewRequest("GET", "/api/v1/things", nil)
	r = r.WithContext(logctx.WithRequestID(r.Context(), "req-42"))
	w := httptest.NewRecorder()
	server.failure(w, r, errors.New("connection refused"), "读取失败")
	if w.Code != 500 || !strings.Contains(w.Body.String(), `"traceId":"req-42"`) || !strings.Contains(w.Body.String(), "读取失败（编号 req-42）") {
		t.Fatalf("unexpected response %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(logs.String(), "reference=req-42") || !strings.Contains(logs.String(), "connection refused") {
		t.Fatalf("the cause was not logged under the reference: %s", logs.String())
	}
}
