package ports

import (
	"context"

	"iot-platform/internal/model"
)

// VideoStore persists the optional camera live module. It is separate from
// Repository so the module can be absent without touching core storage.
// Camera deletion removes live configuration and credentials inside the
// repository's own DeleteResource transaction.
type VideoStore interface {
	GetVideoModuleState(context.Context) (model.VideoModuleState, error)
	SaveVideoModuleState(context.Context, model.VideoModuleState) error
	GetCameraLiveConfig(context.Context, string, string) (model.CameraLiveConfig, error)
	ListCameraLiveConfigs(context.Context, string, []string) (map[string]model.CameraLiveConfig, error)
	SaveCameraLiveConfig(context.Context, model.CameraLiveConfig) error
	GetCameraCredential(context.Context, string, string) (model.CameraCredential, error)
	SaveCameraCredential(context.Context, model.CameraCredential) error
	DeleteCameraCredential(context.Context, string, string) error
	SaveVideoPlaySession(context.Context, model.VideoPlaySession) error
	ListActiveVideoPlaySessions(context.Context, int64) ([]model.VideoPlaySession, error)
	PruneVideoPlaySessions(context.Context, int64) error
	// GB28181 devices. Device IDs are unique across tenants; GetGBDevice looks
	// a device up by its SIP identity alone.
	ListGBDevices(context.Context, string) ([]model.GBDevice, error)
	GetGBDevice(context.Context, string) (model.GBDevice, error)
	SaveGBDevice(context.Context, model.GBDevice) error
	SaveGBDeviceState(context.Context, string, model.GBDeviceState) error
	DeleteGBDevice(context.Context, string, string) error
}
