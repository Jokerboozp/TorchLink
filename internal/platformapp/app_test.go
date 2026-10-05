package platformapp

import (
	"context"
	"testing"

	clickhouseadapter "iot-platform/internal/adapters/clickhouse"
	"iot-platform/internal/adapters/memory"
	redisadapter "iot-platform/internal/adapters/redis"
	"iot-platform/internal/ports"
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
