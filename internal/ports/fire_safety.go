package ports

import (
	"context"

	"iot-platform/internal/model"
)

// FireSafetyStore persists one tenant aggregate with optimistic concurrency.
// An absent state has revision zero. Save compares the supplied revision and
// increments it on success; false means another writer changed the state.
type FireSafetyStore interface {
	LoadFireSafetyState(context.Context, string) (model.FireSafetyState, error)
	SaveFireSafetyState(context.Context, string, model.FireSafetyState) (bool, error)
	// FireSafetyRevision reads only the revision, so readers can reuse a
	// cached state until it changes.
	FireSafetyRevision(context.Context, string) (int64, error)
}
