package ports

import (
	"context"

	"iot-platform/internal/model"
)

// VideoEventStore keeps video alarm events received from video platforms.
type VideoEventStore interface {
	SaveVideoEvent(context.Context, model.VideoAlarmEvent) (bool, error)
	GetVideoEvent(context.Context, string, string) (model.VideoAlarmEvent, error)
	UpdateVideoEvent(context.Context, model.VideoAlarmEvent) error
	ListPendingVideoEvents(context.Context, int) ([]model.VideoAlarmEvent, error)
}
