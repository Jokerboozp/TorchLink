package ports

import (
	"context"

	"iot-platform/internal/model"
)

// DeviceStateStore keeps device runtime states and their change events.
type DeviceStateStore interface {
	ListDeviceStateEvents(context.Context, string, string, int, int) ([]model.DeviceStateEvent, int, error)
	UpsertDeviceState(context.Context, model.DeviceState) error
	GetDeviceState(context.Context, string, string) (model.DeviceState, error)
	// GetDeviceStateFresh reads the stored row with its Version, bypassing caches.
	GetDeviceStateFresh(context.Context, string, string) (model.DeviceState, error)
	// UpsertDeviceStateIf writes only if the stored version equals v.Version
	// (0 = insert if absent) and reports whether it wrote.
	UpsertDeviceStateIf(context.Context, model.DeviceState) (bool, error)
	GetDeviceStatesByIDs(context.Context, string, []string) (map[string]model.DeviceState, error)
	// ListOfflineDue returns up to limit states of every tenant whose
	// OfflineCheckAt is set and before now.
	ListOfflineDue(ctx context.Context, now int64, limit int) ([]model.DeviceState, error)
	ListDeviceStates(context.Context, string) ([]model.DeviceState, error)
	ListDeviceStatesPage(context.Context, string, int, int) ([]model.DeviceState, int, error)
	ListDeviceStatesForDevicesPage(context.Context, string, []string, int, int) ([]model.DeviceState, int, error)
	ListUnregisteredDeviceStatesPage(context.Context, string, int, int) ([]model.DeviceState, int, error)
	CountDeviceStates(context.Context, string, bool) (int, int, error)
	SaveDeviceStateEvent(context.Context, model.DeviceState) error
	// LoadDeviceStateWithAlarms reads a device's state (model.ErrNotFound
	// when absent) and whether it has an open alarm, in one round trip.
	LoadDeviceStateWithAlarms(context.Context, string, string) (model.DeviceState, bool, error)
}
