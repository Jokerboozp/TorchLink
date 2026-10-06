package onboarding

import (
	"context"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// Store is the part of the repository the onboarding service uses: templates
// and onboarding records, device registrations and credentials, the protocol
// and access profile a template refers to, evidence from raw and parsed
// messages, command dispatch for verification, and execution leases for
// background tasks.
type Store interface {
	ports.ProductStore
	ports.OnboardingStore
	ports.DeviceRegistry
	ports.DeviceCredentials
	CreateDeviceCommand(context.Context, model.DeviceCommand) (model.DeviceCommand, bool, error)
	UpdateDeviceCommandDispatch(context.Context, string, string, string, string, int64) error
	GetProtocolPackage(context.Context, string, string) (model.ProtocolPackage, error)
	CreateProtocolRelease(context.Context, model.ProtocolRelease) error
	GetProtocolRelease(context.Context, string, string, string) (model.ProtocolRelease, error)
	GetProductProtocolBinding(context.Context, string, string) (model.ProductProtocolBinding, error)
	GetDeviceAccessProfile(context.Context, string, string) (model.DeviceAccessProfile, error)
	ListDeviceAccessProfiles(context.Context, string) ([]model.DeviceAccessProfile, error)
	GetStandardMessageByRaw(context.Context, string, string) (model.StandardMessage, error)
	ListRawIndexes(context.Context, ports.RawFilter) ([]model.RawArchiveIndex, error)
	AcquireExecutionLease(context.Context, string, string, string, string, time.Duration) (model.ExecutionLease, bool, error)
	ReleaseExecutionLease(context.Context, model.ExecutionLease) error
}
