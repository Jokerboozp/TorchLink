package ports

import (
	"context"
	"iot-platform/internal/model"
)

type AccessStore interface {
	LoadAccessState(context.Context, string) (model.AccessState, error)
	SaveAccessState(context.Context, string, model.AccessState) (bool, error)
	AccessRevisionReader
}

// AccessRevisionReader reads only the tenant's access revision, so a cached
// state can be reused until any user, role or key changes.
type AccessRevisionReader interface {
	AccessRevision(context.Context, string) (int64, error)
}
