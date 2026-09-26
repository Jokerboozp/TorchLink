package core

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type releaseOutageRepository struct {
	ports.Repository
	marked int
}

func (r *releaseOutageRepository) GetProtocolRelease(context.Context, string, string, string) (model.ProtocolRelease, error) {
	return model.ProtocolRelease{}, errors.New("failed to connect: too many clients already")
}

func (r *releaseOutageRepository) MarkRawParseResult(ctx context.Context, tenant, id string, at int64, parseError string) error {
	r.marked++
	return r.Repository.MarkRawParseResult(ctx, tenant, id, at, parseError)
}

// A database outage while loading the protocol version is retried through the
// bus instead of being stored as a permanent parse failure.
func TestRepositoryOutageDuringParseIsRetriedNotMarkedFailed(t *testing.T) {
	repo := &releaseOutageRepository{Repository: memory.NewRepository()}
	e := newRuleTestEngine(t, repo, &ruleTestClock{now: time.Unix(1000, 0)})
	raw, _ := json.Marshal(model.RawMessage{MessageID: "raw-1", TenantID: "tenant-a", ProductID: "sensor", DeviceID: "device-a", ProtocolID: "vendor", ProtocolVersion: "1.0.0", PayloadFormat: "json", Payload: json.RawMessage(`{"temperature":20}`)})
	if err := e.handleRaw(context.Background(), raw); err == nil {
		t.Fatal("a repository outage must be returned for redelivery")
	}
	if repo.marked != 0 {
		t.Fatalf("the raw message must not be marked as a parse failure, marked=%d", repo.marked)
	}
}
