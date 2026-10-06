package ports

import (
	"context"

	"iot-platform/internal/model"
)

// DeviceStore keeps device registrations, credentials, commands and protocol
// registrations of devices. Callers that need only part of it depend on the
// narrower interfaces below.
type DeviceStore interface {
	DeviceRegistry
	DeviceChildren
	ProtocolDeviceRegistration
	DeviceCredentials
	DeviceCommands
}

// DeviceRegistry reads and writes device registrations.
type DeviceRegistry interface {
	SaveManagedDevice(context.Context, model.ManagedDevice) error
	GetManagedDevice(context.Context, string, string) (model.ManagedDevice, error)
	GetManagedDeviceByAccessKey(context.Context, string) (model.ManagedDevice, error)
	ListManagedDevices(context.Context, string) ([]model.ManagedDevice, error)
	ListManagedDevicesPage(context.Context, string, int, int) ([]model.ManagedDevice, int, error)
	// ListManagedDevicesFiltered filters before pagination and returns the filtered total.
	ListManagedDevicesFiltered(context.Context, DeviceFilter, int, int) ([]model.ManagedDevice, int, error)
}

// DeviceChildren lists and counts the child devices of gateways.
type DeviceChildren interface {
	ListManagedDeviceChildren(context.Context, string, string, int, int) ([]model.ManagedDevice, int, error)
	// ListManagedDeviceChildrenForDevices pages a parent's children among
	// deviceIDs (a user's device grant); an empty list matches nothing.
	ListManagedDeviceChildrenForDevices(ctx context.Context, tenant, parent string, deviceIDs []string, limit, offset int) ([]model.ManagedDevice, int, error)
	CountManagedDeviceChildren(context.Context, string, []string) (map[string]int, error)
	// A nil child scope means all; a non-nil empty scope means none.
	CountManagedDeviceChildrenForDevices(context.Context, string, []string, []string) (map[string]int, error)
}

// ProtocolDeviceRegistration registers devices and children first seen on a
// protocol connection.
type ProtocolDeviceRegistration interface {
	RegisterProtocolDevice(context.Context, model.DeviceAccessProfile, string, string) (model.ManagedDevice, bool, error)
	RegisterProtocolChild(context.Context, model.DeviceAccessProfile, string, model.ChildIdentity) (model.ManagedDevice, bool, error)
}

// DeviceCredentials changes device credentials and tracks the revocation of
// the old ones at the message broker.
type DeviceCredentials interface {
	ChangeDeviceCredential(context.Context, string, string, string, string, int64) (model.ManagedDevice, model.CredentialRevocation, error)
	ListCredentialRevocations(context.Context, string, string, bool) ([]model.CredentialRevocation, error)
	UpdateCredentialRevocation(context.Context, model.CredentialRevocation) error
}

// DeviceCommands keeps commands sent to devices and their outcome.
type DeviceCommands interface {
	GetDeviceCommand(context.Context, string, string) (model.DeviceCommand, error)
	CreateDeviceCommand(context.Context, model.DeviceCommand) (model.DeviceCommand, bool, error)
	UpdateDeviceCommandDispatch(context.Context, string, string, string, string, int64) error
	CompleteDeviceCommand(context.Context, string, string, string, map[string]any, int64) error
	ListDeviceCommands(context.Context, string, string, int, int) ([]model.DeviceCommand, int, error)
}
