package ports

import (
	"context"
	"iot-platform/internal/model"
)

// DutyStore transactions always target durable primary storage. Callbacks must
// not invoke Repository methods: use Snapshot to read platform facts atomically.
type DutyStore interface {
	DutyTransaction(context.Context, string, func(DutyTx) error) error
	DutyRead(context.Context, string, func(DutyTx) error) error
}
type DutyTx interface {
	Get(kind, id string) (model.DutyDocument, error)
	List(model.DutyFilter) ([]model.DutyDocument, int, error)
	Put(model.DutyDocument, int64) (model.DutyDocument, error)
	Delete(kind, id string, expectedVersion int64) error
	AppendEvent(model.DutyBusinessEvent) error
	Events(model.DutyFilter) ([]model.DutyBusinessEvent, int, error)
	Snapshot(deviceIDs []string) (model.DutySnapshot, error)
}
