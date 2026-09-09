package protocolbuild

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

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
