package ports

import (
	"context"
	"iot-platform/internal/model"
)

// DeviceID is the server-captured event binding; CurrentDeviceID is checked
// separately so moving a camera cannot grant access to another device's past.
type VideoEventSource struct {
	Event                     model.VideoAlarmEvent
	DeviceID, CurrentDeviceID string
}
type VideoEventFilter struct {
	DeviceIDs  []string
	AllDevices bool
	Start, End int64
	Cursor     string
	Limit      int
}
type VideoEventReader interface {
	GetGovernanceVideoEvent(context.Context, string, string) (VideoEventSource, error)
	ListGovernanceVideoEvents(context.Context, string, VideoEventFilter) ([]VideoEventSource, error)
}
