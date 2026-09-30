package analytics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"iot-platform/internal/model"
)

func batchRevision(id, kind, resource string) model.AnalysisConfigRevision {
	return model.AnalysisConfigRevision{ID: id, TenantID: "t", Kind: kind, ResourceID: resource, Scope: "SHARED", Creator: "operator", DeviceIDs: []string{"d1"}, Body: json.RawMessage(`{"physical":"one"}`)}
}

func TestConfigBatchRollsBackEveryPointerAndSerializesSlots(t *testing.T) {
	ctx, store := context.Background(), NewMemoryStore()
	first, err := store.PutAnalysisConfigs(ctx, []model.AnalysisConfigRevision{batchRevision("slot1", "ASSET_SLOT", "device1"), batchRevision("asset1", "ASSET_INSTANCE", "physical1")}, []int64{0, 0})
	if err != nil || len(first) != 2 {
		t.Fatal(first, err)
	}
	_, err = store.PutAnalysisConfigs(ctx, []model.AnalysisConfigRevision{batchRevision("slot-orphan", "ASSET_SLOT", "device1"), batchRevision("asset-stale", "ASSET_INSTANCE", "physical1")}, []int64{1, 0})
	if !errors.Is(err, model.ErrAnalysisConflict) {
		t.Fatal(err)
	}
	if _, err = store.GetAnalysisConfig(ctx, "t", "slot-orphan"); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("partial first immutable revision survived", err)
	}
	var success atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, e := store.PutAnalysisConfigs(ctx, []model.AnalysisConfigRevision{batchRevision(fmt.Sprintf("slot-%d", i), "ASSET_SLOT", "device1"), batchRevision(fmt.Sprintf("asset-%d", i), "ASSET_INSTANCE", fmt.Sprintf("physical-%d", i))}, []int64{1, 0})
			if e == nil {
				success.Add(1)
			} else if !errors.Is(e, model.ErrAnalysisConflict) {
				t.Error(e)
			}
		}(i)
	}
	wg.Wait()
	if success.Load() != 1 {
		t.Fatal("multiple overlapping slot claims", success.Load())
	}
	items, total, err := store.ListAnalysisConfigs(ctx, "t", model.AnalysisFilter{Kind: "ASSET_INSTANCE", Limit: 100})
	if err != nil || len(items) != 2 || total != 2 {
		t.Fatal("loser asset revisions leaked", items, total, err)
	}
	duplicate := batchRevision("duplicate", "ASSET_SLOT", "device1")
	if _, err = store.PutAnalysisConfigs(ctx, []model.AnalysisConfigRevision{duplicate, duplicate}, []int64{2, 2}); !errors.Is(err, model.ErrAnalysisInvalid) {
		t.Fatal(err)
	}
	foreign := batchRevision("foreign", "ASSET_INSTANCE", "physical2")
	foreign.TenantID = "other"
	if _, err = store.PutAnalysisConfigs(ctx, []model.AnalysisConfigRevision{batchRevision("slot-next", "ASSET_SLOT", "device1"), foreign}, []int64{2, 0}); !errors.Is(err, model.ErrAnalysisInvalid) {
		t.Fatal(err)
	}
}
