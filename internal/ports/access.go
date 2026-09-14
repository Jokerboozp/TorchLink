package ports

import (
	"context"
	"iot-platform/internal/model"
)

type AccessStore interface {
	LoadAccessState(context.Context, string) (model.AccessState, error)
	SaveAccessState(context.Context, string, model.AccessState) (bool, error)
}
