package ports

import (
	"context"

	"iot-platform/internal/model"
)

// CapacityRawObjectCleaner verifies the actual archive content before removing
// a legacy raw object. A zero object offset is also valid for the first record
// of a shared batch and does not establish exclusive ownership.
type CapacityRawObjectCleaner interface {
	DeleteCapacityRawObject(context.Context, string, model.CapacityCleanupBatch, model.RawArchiveIndex) error
}
