package rawstore

import (
	"context"
	"fmt"
	"testing"

	"iot-platform/internal/model"
)

type recordingDatabase struct {
	name     string
	messages map[string]model.RawMessage
}

func newRecordingDatabase(name string) *recordingDatabase {
	return &recordingDatabase{name: name, messages: map[string]model.RawMessage{}}
}

func (d *recordingDatabase) SaveRawMessage(_ context.Context, value model.RawMessage) error {
	d.messages[value.MessageID] = value
	return nil
}

func (d *recordingDatabase) GetRawMessage(_ context.Context, _, messageID string) (model.RawMessage, error) {
	value, ok := d.messages[messageID]
	if !ok {
		return model.RawMessage{}, fmt.Errorf("%s: raw message not found", d.name)
	}
	return value, nil
}

type recordingResolver struct {
	states map[string]model.DeviceState
}

func (r *recordingResolver) GetDeviceState(_ context.Context, _, deviceID string) (model.DeviceState, error) {
	state, ok := r.states[deviceID]
	if !ok {
		return model.DeviceState{}, fmt.Errorf("device state not found")
	}
	return state, nil
}

func rawMessage(id, device string, receivedAt int64) model.RawMessage {
	return model.RawMessage{
		MessageID:     id,
		TenantID:      "tenant-1",
		ProductID:     "product-1",
		DeviceID:      device,
		ReceivedAt:    receivedAt,
		PayloadFormat: "json",
		Payload:       []byte(`{"value":1}`),
	}
}

func TestStoreRoutesByConfiguredAndObservedFrequency(t *testing.T) {
	postgres := newRecordingDatabase(PostgreSQLBucket)
	clickhouse := newRecordingDatabase(ClickHouseBucket)
	resolver := &recordingResolver{states: map[string]model.DeviceState{
		"slow-device": {ReportIntervalSec: 300, LastSeenAt: 1_000_000},
		"fast-device": {ReportIntervalSec: 10, LastSeenAt: 1_000_000},
	}}
	store := New(Config{
		PostgreSQL:               postgres,
		ClickHouse:               clickhouse,
		Resolver:                 resolver,
		HighFrequencyIntervalSec: 60,
	})

	slowIndex, err := store.PutRaw(context.Background(), rawMessage("slow-1", "slow-device", 1_100_000))
	if err != nil {
		t.Fatalf("store slow message: %v", err)
	}
	if slowIndex.ObjectBucket != PostgreSQLBucket {
		t.Fatalf("slow message bucket = %q, want %q", slowIndex.ObjectBucket, PostgreSQLBucket)
	}

	fastIndex, err := store.PutRaw(context.Background(), rawMessage("fast-1", "fast-device", 1_100_000))
	if err != nil {
		t.Fatalf("store configured fast message: %v", err)
	}
	if fastIndex.ObjectBucket != ClickHouseBucket {
		t.Fatalf("configured fast message bucket = %q, want %q", fastIndex.ObjectBucket, ClickHouseBucket)
	}

	firstObserved, err := store.PutRaw(context.Background(), rawMessage("observed-1", "unknown-device", 2_000_000))
	if err != nil {
		t.Fatalf("store first observed message: %v", err)
	}
	if firstObserved.ObjectBucket != PostgreSQLBucket {
		t.Fatalf("first observed message bucket = %q, want %q", firstObserved.ObjectBucket, PostgreSQLBucket)
	}
	secondObserved, err := store.PutRaw(context.Background(), rawMessage("observed-2", "unknown-device", 2_001_000))
	if err != nil {
		t.Fatalf("store second observed message: %v", err)
	}
	if secondObserved.ObjectBucket != ClickHouseBucket {
		t.Fatalf("second observed message bucket = %q, want %q", secondObserved.ObjectBucket, ClickHouseBucket)
	}

	if _, err := store.GetRaw(context.Background(), secondObserved); err != nil {
		t.Fatalf("read clickhouse-tier raw message: %v", err)
	}
	if len(postgres.messages) != 2 {
		t.Fatalf("postgres stored %d messages, want 2", len(postgres.messages))
	}
	if len(clickhouse.messages) != 2 {
		t.Fatalf("clickhouse stored %d messages, want 2", len(clickhouse.messages))
	}
}

func TestStoreFallsBackToAvailableDatabase(t *testing.T) {
	clickhouse := newRecordingDatabase(ClickHouseBucket)
	store := New(Config{ClickHouse: clickhouse, HighFrequencyIntervalSec: 60})

	index, err := store.PutRaw(context.Background(), rawMessage("fallback-1", "device-1", 3_000_000))
	if err != nil {
		t.Fatalf("store with one database: %v", err)
	}
	if index.ObjectBucket != ClickHouseBucket {
		t.Fatalf("fallback bucket = %q, want %q", index.ObjectBucket, ClickHouseBucket)
	}
}

func TestStoreRejectsMissingDatabases(t *testing.T) {
	store := New(Config{})
	if _, err := store.PutRaw(context.Background(), rawMessage("missing-1", "device-1", 4_000_000)); err == nil {
		t.Fatal("expected missing database error")
	}
}
