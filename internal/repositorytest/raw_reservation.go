package repositorytest

import (
	"context"
	"encoding/json"
	"errors"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"sync"
	"testing"
)

func RawReservation(t *testing.T, repo ports.Repository) {
	t.Helper()
	ctx := context.Background()
	var wg sync.WaitGroup
	results := make(chan model.RawMessage, 16)
	conflicts := make(chan bool, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			v := model.RawMessage{TenantID: "reservation-test", ProductID: "product", DeviceID: "device", MessageID: "one", ProtocolVersion: "first", Payload: json.RawMessage(`{"x":1}`)}
			if i%2 == 1 {
				v.Payload = json.RawMessage(`{"x":2}`)
				v.ProtocolVersion = "other"
			}
			canonical, err := repo.ReserveRawMessage(ctx, v)
			if err != nil {
				if !errors.Is(err, model.ErrRawConflict) {
					t.Error(err)
				}
				conflicts <- true
				return
			}
			results <- canonical
		}(i)
	}
	wg.Wait()
	close(results)
	close(conflicts)
	var first model.RawMessage
	count := 0
	for v := range results {
		if count == 0 {
			first = v
		} else if v.PayloadHash() != first.PayloadHash() || v.ProtocolVersion != first.ProtocolVersion {
			t.Error("conflicting routing snapshots accepted")
		}
		count++
	}
	if count != 8 || len(conflicts) != 8 {
		t.Fatalf("accepted %d conflicting %d", count, len(conflicts))
	}
	retry := first
	retry.ProtocolVersion = "new-version"
	retry.ReceivedAt = 9999
	canonical, err := repo.ReserveRawMessage(ctx, retry)
	if err != nil || canonical.ProtocolVersion != first.ProtocolVersion || canonical.ReceivedAt != first.ReceivedAt {
		t.Fatal("retry changed reserved parser snapshot", err)
	}
}
