// Package devicescope limits what a request may read and change to the
// devices its user is granted. The scope travels in the request context; the
// Repository wrapper applies it to every device, state, alarm and raw-message
// query, and passes calls through unchanged when no scope is present, so
// background ingest and maintenance keep reading the whole tenant.
package devicescope

import (
	"context"
	"errors"
	"slices"

	"iot-platform/internal/model"
	"iot-platform/internal/sites"
)

// UnitIndex resolves unit grants: the unit each placed device belongs to and
// the devices of each unit. One index per tenant and site revision is shared
// by every request, so unit grants cost no per-user copies.
type UnitIndex struct {
	unitOf  map[string]string
	devices map[string][]string
}

// NewUnitIndex indexes the device placements of a tenant's sites.
func NewUnitIndex(state model.SiteState) *UnitIndex {
	index := &UnitIndex{unitOf: sites.DeviceUnits(state), devices: map[string][]string{}}
	for device, unit := range index.unitOf {
		index.devices[unit] = append(index.devices[unit], device)
	}
	for _, list := range index.devices {
		slices.Sort(list)
	}
	return index
}

// With returns ctx carrying scope v.
func With(ctx context.Context, v Scope) context.Context { return context.WithValue(ctx, scopeKey{}, v) }

type scopeKey struct{}

// Scope is the devices a request may use: all of them, the devices
// granted one by one, and the devices placed in granted units. Unit grants
// are resolved through the tenant's shared site index instead of being
// copied into every user's device list.
type Scope struct {
	Tenant string
	All    bool
	IDs    map[string]bool
	Units  map[string]bool
	Sites  *UnitIndex
}

// Has reports whether the scope covers device id.
func (v Scope) Has(id string) bool {
	if v.All || v.IDs[id] {
		return true
	}
	if len(v.Units) == 0 || v.Sites == nil {
		return false
	}
	unit, placed := v.Sites.unitOf[id]
	return placed && v.Units[unit]
}

// DeviceIDs lists the granted devices, including those placed in granted
// units, sorted; it is empty for a scope covering all devices.
func (v Scope) DeviceIDs() []string {
	ids := make([]string, 0, len(v.IDs))
	for id, allowed := range v.IDs {
		if allowed {
			ids = append(ids, id)
		}
	}
	if v.Sites != nil {
		for unit := range v.Units {
			ids = append(ids, v.Sites.devices[unit]...)
		}
	}
	slices.Sort(ids)
	return slices.Compact(ids)
}

// FromContext returns the scope of the request, if any.
func FromContext(ctx context.Context) (Scope, bool) {
	v, ok := ctx.Value(scopeKey{}).(Scope)
	return v, ok
}

// Allowed reports whether ctx may use device id of tenant; without a scope
// every device is allowed.
func Allowed(ctx context.Context, tenant, id string) bool {
	v, ok := FromContext(ctx)
	return !ok || (tenant == v.Tenant && v.Has(id))
}

// Limited reports whether ctx carries a scope that does not cover all devices.
func Limited(ctx context.Context) bool { v, ok := FromContext(ctx); return ok && !v.All }

// ErrDenied hides devices outside the scope as if they did not exist.
var ErrDenied = errors.New("设备不存在或无访问权限")
