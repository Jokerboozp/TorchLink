package httpapi

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"iot-platform/internal/devicescope"
	"iot-platform/internal/logctx"
	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// A literal 500 written with problem() drops the error and the reference the
// operations guide tells operators to search for; handlers use s.fail.
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
					t.Errorf("%s: use s.fail for internal errors", fset.Position(call.Pos()))
				}
			case *ast.SelectorExpr:
				if status.Sel.Name == "StatusInternalServerError" {
					t.Errorf("%s: use s.fail for internal errors", fset.Position(call.Pos()))
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

func TestFailMapsErrorCategories(t *testing.T) {
	var logs strings.Builder
	server := &Server{log: slog.New(slog.NewTextHandler(&logs, nil))}
	cases := []struct {
		method string
		err    error
		status int
		detail string
	}{
		{"GET", fmt.Errorf("load device: %w", devicescope.ErrDenied), 404, devicescope.ErrDenied.Error()},
		{"PUT", devicescope.ErrDenied, 403, devicescope.ErrDenied.Error()},
		{"POST", &onboarding.EnrollError{Status: 409, Message: "地址已被占用"}, 409, "地址已被占用"},
		{"POST", fmt.Errorf("save: %w", model.Invalid("巡检周期须为正整数")), 422, "巡检周期须为正整数"},
		{"PUT", fmt.Errorf("save: %w", model.ErrBindingChanged), 409, model.ErrBindingChanged.Error()},
		{"GET", fmt.Errorf("load: %w", model.ErrNotFound), 404, "资源不存在或无访问权限"},
		{"POST", model.ErrBackpressure, 503, model.ErrBackpressure.Error()},
		{"GET", errors.New("dial tcp 10.0.0.5:5432: connection refused"), 500, "读取失败"},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		server.fail(w, httptest.NewRequest(c.method, "/api/v1/things", nil), c.err, "读取失败")
		if w.Code != c.status || !strings.Contains(w.Body.String(), c.detail) {
			t.Errorf("%s %v: got %d %s, want %d with %q", c.method, c.err, w.Code, w.Body.String(), c.status, c.detail)
		}
		if c.status != 500 && strings.Contains(w.Body.String(), "connection refused") {
			t.Errorf("%v leaked its cause", c.err)
		}
	}
	if strings.Contains(logs.String(), "地址已被占用") || !strings.Contains(logs.String(), "connection refused") {
		t.Fatalf("only unexpected errors are logged: %s", logs.String())
	}
}
