package model

import "slices"

// Sites describe where devices are installed: a fire safety unit (the
// managed organisation or premises), its buildings, their floors with an
// optional floor plan image, and points that place a device or one of its
// components on a floor. A device's unit is taken from its device-level
// point (ComponentID empty); unit grants in user and role device scopes use
// that relation.

// SiteRecord carries identity and optimistic concurrency for site records.
type SiteRecord struct {
	ID        string `json:"id"`
	Version   int64  `json:"version"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
	UpdatedBy string `json:"updatedBy,omitempty"`
}

type SiteUnit struct {
	SiteRecord
	Name    string `json:"name"`
	Code    string `json:"code,omitempty"`
	Address string `json:"address,omitempty"`
	Contact string `json:"contact,omitempty"`
	Phone   string `json:"phone,omitempty"`
	Notes   string `json:"notes,omitempty"`
}

type SiteBuilding struct {
	SiteRecord
	UnitID      string `json:"unitId"`
	Name        string `json:"name"`
	Address     string `json:"address,omitempty"`
	AboveFloors int    `json:"aboveFloors,omitempty"`
	BelowFloors int    `json:"belowFloors,omitempty"`
	Notes       string `json:"notes,omitempty"`
}

// FloorPlan is the floor plan image kept in object storage under a key
// derived from the tenant, floor and SHA256.
type FloorPlan struct {
	ContentType string `json:"contentType"`
	SHA256      string `json:"sha256"`
	Size        int64  `json:"size"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	UploadedAt  int64  `json:"uploadedAt"`
}

type SiteFloor struct {
	SiteRecord
	BuildingID string `json:"buildingId"`
	Name       string `json:"name"`
	// Level orders floors; negative values are basements.
	Level int        `json:"level"`
	Plan  *FloorPlan `json:"plan,omitempty"`
}

// SitePoint places a device, or one component of it, in a unit and
// optionally on a floor plan. X and Y are fractions of the plan's width and
// height, so a replaced image of the same layout keeps the markers.
type SitePoint struct {
	SiteRecord
	UnitID      string   `json:"unitId"`
	BuildingID  string   `json:"buildingId,omitempty"`
	FloorID     string   `json:"floorId,omitempty"`
	DeviceID    string   `json:"deviceId"`
	ComponentID string   `json:"componentId,omitempty"`
	Name        string   `json:"name,omitempty"`
	X           *float64 `json:"x,omitempty"`
	Y           *float64 `json:"y,omitempty"`
}

// SiteState is one tenant's sites, saved with optimistic concurrency like
// the fire safety state.
type SiteState struct {
	Revision  int64          `json:"revision"`
	Units     []SiteUnit     `json:"units"`
	Buildings []SiteBuilding `json:"buildings"`
	Floors    []SiteFloor    `json:"floors"`
	Points    []SitePoint    `json:"points"`
}

// Clone copies the record lists so a change can be applied without touching
// a shared snapshot; records are replaced, never modified in place.
func (s SiteState) Clone() SiteState {
	s.Units = slices.Clone(s.Units)
	s.Buildings = slices.Clone(s.Buildings)
	s.Floors = slices.Clone(s.Floors)
	s.Points = slices.Clone(s.Points)
	return s
}

// SiteChange is one record to write or delete; Value is nil for a delete.
type SiteChange struct {
	Kind, ID string
	Value    any
}

// SiteChanges lists the records that differ between base and next. Records
// are compared by value, so a record copied unchanged from base is skipped.
func SiteChanges(base, next SiteState) []SiteChange {
	var out []SiteChange
	out = diffSites(out, "unit", base.Units, next.Units, func(v SiteUnit) string { return v.ID })
	out = diffSites(out, "building", base.Buildings, next.Buildings, func(v SiteBuilding) string { return v.ID })
	out = diffSites(out, "floor", base.Floors, next.Floors, func(v SiteFloor) string { return v.ID })
	return diffSites(out, "point", base.Points, next.Points, func(v SitePoint) string { return v.ID })
}

func diffSites[T comparable](out []SiteChange, kind string, base, next []T, id func(T) string) []SiteChange {
	old := make(map[string]T, len(base))
	for _, v := range base {
		old[id(v)] = v
	}
	for _, v := range next {
		key := id(v)
		previous, exists := old[key]
		delete(old, key)
		if !exists || previous != v {
			out = append(out, SiteChange{Kind: kind, ID: key, Value: v})
		}
	}
	for key := range old {
		out = append(out, SiteChange{Kind: kind, ID: key})
	}
	return out
}

// AlarmLocation is the site position copied into an alarm when it is
// raised, so later changes to sites do not rewrite alarm history.
type AlarmLocation struct {
	UnitID       string   `json:"unitId"`
	UnitName     string   `json:"unitName"`
	BuildingID   string   `json:"buildingId,omitempty"`
	BuildingName string   `json:"buildingName,omitempty"`
	FloorID      string   `json:"floorId,omitempty"`
	FloorName    string   `json:"floorName,omitempty"`
	PointID      string   `json:"pointId"`
	PointName    string   `json:"pointName,omitempty"`
	X            *float64 `json:"x,omitempty"`
	Y            *float64 `json:"y,omitempty"`
	// Current marks a position looked up when reading an alarm that was
	// raised before its device was placed.
	Current bool `json:"current,omitempty"`
}
