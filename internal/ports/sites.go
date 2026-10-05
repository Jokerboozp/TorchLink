package ports

import (
	"context"

	"iot-platform/internal/model"
)

// SiteStore persists a tenant's units, buildings, floors and points with
// optimistic concurrency, like FireSafetyStore.
type SiteStore interface {
	LoadSiteState(context.Context, string) (model.SiteState, error)
	// SaveSiteState stores next when the tenant is still at base.Revision,
	// writing only the records that differ from base; base must be the
	// stored state of that revision.
	SaveSiteState(ctx context.Context, tenant string, base, next model.SiteState) (bool, error)
	SiteRevision(context.Context, string) (int64, error)
}

// AlarmLocator finds the site position of a device or component.
type AlarmLocator interface {
	AlarmLocation(ctx context.Context, tenant, deviceID, componentID string) *model.AlarmLocation
}

// NearbyDeviceLocator lists the devices placed near a location (same floor,
// or same building without a floor), excluding one device.
type NearbyDeviceLocator interface {
	DevicesNear(ctx context.Context, tenant string, loc model.AlarmLocation, exclude string) []string
}
