package memory

import (
	"context"
	"encoding/json"
	"testing"

	"iot-platform/internal/externaldata"
	"iot-platform/internal/repositorytest"
)

func TestExternalDataStore(t *testing.T) {
	repositorytest.ExternalData(t, NewRepository().ExternalDataStore())
}

func TestExternalDataStoreBodyIsolation(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository()
	store := repo.ExternalDataStore()
	e, err := store.Put(ctx, externaldata.Entry{TenantID: "t", Kind: "source", ID: "one", Body: json.RawMessage(`{"a":1}`)}, 0)
	if err != nil {
		t.Fatal(err)
	}
	e.Body[0] = 'x'
	got, err := repo.ExternalDataStore().Get(ctx, "t", "source", "one")
	if err != nil || !json.Valid(got.Body) {
		t.Fatalf("body mutation leaked: %s %v", got.Body, err)
	}
	got.Body[0] = 'x'
	got, err = store.Get(ctx, "t", "source", "one")
	if err != nil || !json.Valid(got.Body) {
		t.Fatalf("get mutation leaked: %s %v", got.Body, err)
	}
}
