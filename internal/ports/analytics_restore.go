package ports

import (
	"context"
	"iot-platform/internal/model"
)

// No public setter exists. Only isolated backup restoration inserts overlays.
type AnalysisRestoredObjectStore interface {
	GetRestoredObjectLocation(context.Context, string, string, string) (model.AnalysisRestoredObjectLocation, error)
}
