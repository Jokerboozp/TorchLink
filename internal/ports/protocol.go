package ports

import (
	"context"

	"iot-platform/internal/model"
)

// ProtocolStore keeps protocol packages, definitions, releases, point tables,
// template bindings and access profiles. Callers that need only part of it
// depend on the narrower interfaces below.
type ProtocolStore interface {
	ProtocolPackageStore
	ProtocolDefinitionStore
	ProtocolReleaseStore
	PointTableStore
	ProtocolBindingStore
	AccessProfileStore
}

// ProtocolPackageStore keeps the protocol packages templates refer to.
type ProtocolPackageStore interface {
	SaveProtocolPackage(context.Context, model.ProtocolPackage) error
	GetProtocolPackage(context.Context, string, string) (model.ProtocolPackage, error)
	ListProtocolPackages(context.Context, string) ([]model.ProtocolPackage, error)
	ListProtocolPackagesPage(context.Context, string, int, int) ([]model.ProtocolPackage, int, error)
}

// ProtocolDefinitionStore keeps protocol definitions.
type ProtocolDefinitionStore interface {
	SaveProtocolDefinition(context.Context, model.ProtocolDefinition) error
	GetProtocolDefinition(context.Context, string, string) (model.ProtocolDefinition, error)
	ListProtocolDefinitions(context.Context, string) ([]model.ProtocolDefinition, error)
}

// ProtocolReleaseStore keeps immutable protocol releases and their status.
type ProtocolReleaseStore interface {
	CreateProtocolRelease(context.Context, model.ProtocolRelease) error
	GetProtocolRelease(context.Context, string, string, string) (model.ProtocolRelease, error)
	ListProtocolReleases(context.Context, string, string) ([]model.ProtocolRelease, error)
	UpdateProtocolReleaseStatus(context.Context, string, string, string, string, int64) error
}

// PointTableStore keeps point table releases.
type PointTableStore interface {
	CreatePointTableRelease(context.Context, model.PointTableRelease) error
	GetPointTableRelease(context.Context, string, string, string) (model.PointTableRelease, error)
}

// ProtocolBindingStore keeps which protocol release each template uses.
type ProtocolBindingStore interface {
	SaveProductProtocolBinding(context.Context, model.ProductProtocolBinding) error
	GetProductProtocolBinding(context.Context, string, string) (model.ProductProtocolBinding, error)
	// GetProductProtocolBindingsByIDs returns the bindings that exist, keyed by product ID.
	GetProductProtocolBindingsByIDs(context.Context, string, []string) (map[string]model.ProductProtocolBinding, error)
	// SwitchProductProtocol writes a template's protocol binding, protocol reference
	// and compatibility package in one transaction.
	SwitchProductProtocol(context.Context, model.ProtocolSwitch) error
}

// AccessProfileStore keeps device access profiles (platform connections).
type AccessProfileStore interface {
	SaveDeviceAccessProfile(context.Context, model.DeviceAccessProfile, ...model.AccessProfileSaveOptions) error
	UpdateDeviceAccessStatus(context.Context, model.DeviceAccessProfile, string, string, int64) (bool, error)
	GetDeviceAccessProfile(context.Context, string, string) (model.DeviceAccessProfile, error)
	ListDeviceAccessProfiles(context.Context, string) ([]model.DeviceAccessProfile, error)
}
