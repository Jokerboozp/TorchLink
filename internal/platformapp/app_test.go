package platformapp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	clickhouseadapter "iot-platform/internal/adapters/clickhouse"
	"iot-platform/internal/adapters/memory"
	redisadapter "iot-platform/internal/adapters/redis"
	"iot-platform/internal/ports"
	"iot-platform/internal/protocolrunner"
)

// Online and cluster deployments decorate the primary repository with
// ClickHouse and Redis. Run captures the dynamic Agent store before that, so
// every API replica reconciles the Harness instances to the shared manifests.
func TestAIWorkflowManifestStoreSurvivesRepositoryDecorators(t *testing.T) {
	primary := memory.NewRepository()
	var repo ports.Repository = primary
	manifestStore, _ := repo.(ports.AIWorkflowManifestStore)
	repo = &clickhouseadapter.Repository{Repository: repo}
	repo = redisadapter.New(repo, redisadapter.NewClient(redisadapter.Options{Addr: "127.0.0.1:0"}))

	if _, ok := repo.(ports.AIWorkflowManifestStore); ok {
		t.Fatal("decorators now expose the manifest store; the early capture in Run can be simplified")
	}
	if manifestStore == nil {
		t.Fatal("manifest store was not captured from the primary repository")
	}
	ctx := context.Background()
	if err := manifestStore.SaveAIWorkflowManifest(ctx, ports.StoredAIWorkflowManifest{ID: "agent-a"}); err != nil {
		t.Fatal(err)
	}
	stored, err := primary.ListAIWorkflowManifests(ctx)
	if err != nil || len(stored) != 1 || stored[0].ID != "agent-a" {
		t.Fatalf("manifest did not reach the primary repository: %+v, %v", stored, err)
	}
}

// The container probe passes only for a live endpoint on the configured port,
// including the wildcard listen address used in containers.
func TestHealthcheckExitCode(t *testing.T) {
	live := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health/live" {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer live.Close()
	port := live.Listener.Addr().String()[strings.LastIndex(live.Listener.Addr().String(), ":"):]
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	down.Close()
	for addr, want := range map[string]int{port: 0, "0.0.0.0" + port: 0, down.Listener.Addr().String(): 1} {
		if got := healthcheck(addr); got != want {
			t.Fatalf("healthcheck(%q) = %d, want %d", addr, got, want)
		}
	}
}

func TestRunnerHealthcheckProbesTheSocket(t *testing.T) {
	// Unix socket paths are limited to about 100 bytes.
	dir, err := os.MkdirTemp("/tmp", "runner-hc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "runner.sock")
	if code := runnerHealthcheck(socket); code != 1 {
		t.Fatalf("missing runner reported healthy: %d", code)
	}
	if code := runnerHealthcheck(""); code != 1 {
		t.Fatalf("unset socket reported healthy: %d", code)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = (&protocolrunner.Server{Dir: filepath.Join(dir, "work")}).Serve(ctx, socket) }()
	for i := 0; runnerHealthcheck(socket) != 0; i++ {
		if i == 100 {
			t.Fatal("running runner reported unhealthy")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
