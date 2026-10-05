package httpapi

import (
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"
	"testing"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
)

// The registered routes are pinned so reorganizing route registration cannot
// add, drop or change a route unnoticed. Set IOT_UPDATE_ROUTES=1 to rewrite
// testdata/routes.txt after an intended route change.
func TestRegisteredRoutesMatchSnapshot(t *testing.T) {
	api := New(config.Config{DevMode: true}, &core.Engine{Repo: memory.NewRepository()}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	routes := []string{}
	for _, r := range api.router.Routes() {
		routes = append(routes, r.Method+" "+r.Path)
	}
	sort.Strings(routes)
	got := strings.Join(routes, "\n") + "\n"
	if os.Getenv("IOT_UPDATE_ROUTES") == "1" {
		if err := os.WriteFile("testdata/routes.txt", []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile("testdata/routes.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("registered routes changed; run with IOT_UPDATE_ROUTES=1 if intended\n%s", diffLines(string(want), got))
	}
}

func diffLines(want, got string) string {
	wantSet, gotSet := map[string]bool{}, map[string]bool{}
	for _, l := range strings.Split(want, "\n") {
		wantSet[l] = true
	}
	for _, l := range strings.Split(got, "\n") {
		gotSet[l] = true
	}
	var b strings.Builder
	for l := range wantSet {
		if !gotSet[l] {
			b.WriteString("- " + l + "\n")
		}
	}
	for l := range gotSet {
		if !wantSet[l] {
			b.WriteString("+ " + l + "\n")
		}
	}
	return b.String()
}
