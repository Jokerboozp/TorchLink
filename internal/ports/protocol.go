package ports

import (
	"context"

	"iot-platform/internal/model"
)

// ProtocolStore keeps protocol packages, definitions, releases, point tables,
// template bindings and access profiles.
type ProtocolStore interface {
	SaveProtocolPackage(context.Context, model.ProtocolPackage) error
	GetProtocolPackage(context.Context, string, string) (model.ProtocolPackage, error)
	ListProtocolPackages(context.Context, string) ([]model.ProtocolPackage, error)
	ListProtocolPackagesPage(context.Context, string, int, int) ([]model.ProtocolPackage, int, error)
	SaveProtocolDefinition(context.Context, model.ProtocolDefinition) error
	GetProtocolDefinition(context.Context, string, string) (model.ProtocolDefinition, error)
	ListProtocolDefinitions(context.Context, string) ([]model.ProtocolDefinition, error)
	CreateProtocolRelease(context.Context, model.ProtocolRelease) error
	GetProtocolRelease(context.Context, string, string, string) (model.ProtocolRelease, error)
	ListProtocolReleases(context.Context, string, string) ([]model.ProtocolRelease, error)
	UpdateProtocolReleaseStatus(context.Context, string, string, string, string, int64) error
	CreatePointTableRelease(context.Context, model.PointTableRelease) error
	GetPointTableRelease(context.Context, string, string, string) (model.PointTableRelease, error)
	SaveProductProtocolBinding(context.Context, model.ProductProtocolBinding) error
	GetProductProtocolBinding(context.Context, string, string) (model.ProductProtocolBinding, error)
	SaveDeviceAccessProfile(context.Context, model.DeviceAccessProfile, ...model.AccessProfileSaveOptions) error
	UpdateDeviceAccessStatus(context.Context, model.DeviceAccessProfile, string, string, int64) (bool, error)
	GetDeviceAccessProfile(context.Context, string, string) (model.DeviceAccessProfile, error)
	ListDeviceAccessProfiles(context.Context, string) ([]model.DeviceAccessProfile, error)
	// SwitchProductProtocol writes a template's protocol binding, protocol reference
	// and compatibility package in one transaction.
	SwitchProductProtocol(context.Context, model.ProtocolSwitch) error
}
