package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"iot-platform/internal/externaldata"
	"iot-platform/internal/repositorytest"
)

func TestExternalDataStore(t *testing.T) {
	repositorytest.ExternalData(t, testRepository(t).ExternalDataStore())
}

func TestExternalDataStoreReopen(t *testing.T) {
	ctx := context.Background()
	r := testRepository(t)
	config := r.pool.Config()
	store := r.ExternalDataStore()
	e, err := store.Put(ctx, externaldata.Entry{TenantID: "t", Kind: "record", ID: "durable", Status: "PENDING", Body: json.RawMessage(`{"raw":{"event":"alarm"}}`)}, 0)
	if err != nil {
		t.Fatal(err)
	}
	r.pool.Close()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	reopened := (&Repository{pool: pool}).ExternalDataStore()
	got, err := reopened.Get(ctx, e.TenantID, e.Kind, e.ID)
	if err != nil || got.Revision != e.Revision || got.CreatedAt != e.CreatedAt {
		t.Fatalf("persistent record: %+v %v", got, err)
	}
	claimed, err := reopened.Claim(ctx, "record", "after-restart", time.Now().UnixMilli(), 60000)
	if err != nil || claimed.ID != e.ID || claimed.Revision != e.Revision+1 {
		t.Fatalf("resumed receipt: %+v %v", claimed, err)
	}
}
