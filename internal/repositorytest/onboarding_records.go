package repositorytest

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func OnboardingRecords(t *testing.T, repo ports.OnboardingStore) {
	t.Helper()
	ctx := context.Background()
	v := model.OnboardingRecord{TenantID: "records-a", ID: "draft", OwnerID: "alice", Kind: "device-draft", Status: "DRAFT", Body: json.RawMessage(`{"step":"connection"}`)}
	saved, err := repo.SaveOnboardingRecord(ctx, v, 0)
	if err != nil || saved.Revision != 1 || saved.CreatedAt == 0 {
		t.Fatal(saved, err)
	}
	if _, err = repo.SaveOnboardingRecord(ctx, v, 0); !errors.Is(err, model.ErrOnboardingChanged) {
		t.Fatal("duplicate create", err)
	}
	if _, err = repo.GetOnboardingRecord(ctx, "records-b", v.ID); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("cross tenant get", err)
	}
	for _, field := range []string{"owner", "kind"} {
		changed := saved
		if field == "owner" {
			changed.OwnerID = "bob"
		} else {
			changed.Kind = "device-batch"
		}
		if _, err = repo.SaveOnboardingRecord(ctx, changed, 1); !errors.Is(err, model.ErrOnboardingChanged) {
			t.Fatal("identity changed", field, err)
		}
	}
	var won atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			candidate := saved
			candidate.Status = "UPDATED"
			_, e := repo.SaveOnboardingRecord(ctx, candidate, 1)
			if e == nil {
				won.Add(1)
			} else if !errors.Is(e, model.ErrOnboardingChanged) {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if won.Load() != 1 {
		t.Fatal("CAS winners", won.Load())
	}
	got, err := repo.GetOnboardingRecord(ctx, v.TenantID, v.ID)
	if err != nil || got.Revision != 2 || got.CreatedAt != saved.CreatedAt {
		t.Fatal(got, err)
	}
	got.Body[0] = '!'
	again, _ := repo.GetOnboardingRecord(ctx, v.TenantID, v.ID)
	if !json.Valid(again.Body) {
		t.Fatal("mutable body escaped")
	}
	items, total, err := repo.ListOnboardingRecords(ctx, v.TenantID, "bob", "", 20, 0)
	if err != nil || total != 0 || len(items) != 0 {
		t.Fatal("owner filter", items, total, err)
	}
	items, total, err = repo.ListOnboardingRecords(ctx, v.TenantID, "alice", "device-draft", 20, 0)
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatal(items, total, err)
	}
	if _, _, err = repo.ListOnboardingRecords(ctx, "", "", "", 20, 0); err == nil {
		t.Fatal("public listing accepted empty tenant")
	}
	for _, tenant := range []string{"queue-a", "queue-b"} {
		v.TenantID, v.ID, v.Kind, v.Status = tenant, "job", "device-batch", "QUEUED"
		if _, err = repo.SaveOnboardingRecord(ctx, v, 0); err != nil {
			t.Fatal(err)
		}
	}
	jobs, err := repo.ListPendingOnboardingRecords(ctx, "device-batch", 10)
	if err != nil || len(jobs) != 2 {
		t.Fatal("cross tenant worker queue", jobs, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = repo.SaveOnboardingRecord(cancelled, v, 0); err == nil {
		t.Fatal("cancelled write succeeded")
	}
}
