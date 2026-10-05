package ports

import (
	"context"

	"iot-platform/internal/model"
)

// DeviceStore keeps device registrations, credentials, commands and protocol
// registrations of devices.
type DeviceStore interface {
	RegisterProtocolDevice(context.Context, model.DeviceAccessProfile, string, string) (model.ManagedDevice, bool, error)
	RegisterProtocolChild(context.Context, model.DeviceAccessProfile, string, model.ChildIdentity) (model.ManagedDevice, bool, error)
	ListManagedDeviceChildren(context.Context, string, string, int, int) ([]model.ManagedDevice, int, error)
	ChangeDeviceCredential(context.Context, string, string, string, string, int64) (model.ManagedDevice, model.CredentialRevocation, error)
	ListCredentialRevocations(context.Context, string, string, bool) ([]model.CredentialRevocation, error)
	UpdateCredentialRevocation(context.Context, model.CredentialRevocation) error
	GetDeviceCommand(context.Context, string, string) (model.DeviceCommand, error)
	CreateDeviceCommand(context.Context, model.DeviceCommand) (model.DeviceCommand, bool, error)
	UpdateDeviceCommandDispatch(context.Context, string, string, string, string, int64) error
	CompleteDeviceCommand(context.Context, string, string, string, map[string]any, int64) error
	ListDeviceCommands(context.Context, string, string, int, int) ([]model.DeviceCommand, int, error)
	SaveManagedDevice(context.Context, model.ManagedDevice) error
	GetManagedDevice(context.Context, string, string) (model.ManagedDevice, error)
	GetManagedDeviceByAccessKey(context.Context, string) (model.ManagedDevice, error)
	ListManagedDevices(context.Context, string) ([]model.ManagedDevice, error)
	ListManagedDevicesPage(context.Context, string, int, int) ([]model.ManagedDevice, int, error)
	CountManagedDeviceChildren(context.Context, string, []string) (map[string]int, error)
	// A nil child scope means all; a non-nil empty scope means none.
	CountManagedDeviceChildrenForDevices(context.Context, string, []string, []string) (map[string]int, error)
	// ListManagedDevicesFiltered filters before pagination and returns the filtered total.
	ListManagedDevicesFiltered(context.Context, DeviceFilter, int, int) ([]model.ManagedDevice, int, error)
}
