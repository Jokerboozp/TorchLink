package model

import "errors"

// FireSafetyState groups low-volume management records in one tenant aggregate.
// Revision makes relationship checks and mutations atomic across API replicas.
type FireSafetyState struct {
	Revision      int64            `json:"revision"`
	Stations      []FireStation    `json:"stations"`
	Personnel     []FirePersonnel  `json:"personnel"`
	Equipment     []FireEquipment  `json:"equipment"`
	Dispatches    []FireDispatch   `json:"dispatches"`
	Shifts        []DutyShift      `json:"shifts"`
	Assignments   []DutyAssignment `json:"assignments"`
	Swaps         []DutySwap       `json:"swaps"`
	Extinguishers []Extinguisher   `json:"extinguishers"`
	Inspections   []FireInspection `json:"inspections"`
}

type FireRecord struct {
	ID        string `json:"id"`
	Version   int64  `json:"version"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

type FireStation struct {
	FireRecord
	Code      string  `json:"code"`
	Name      string  `json:"name"`
	Type      string  `json:"type"` // micro, professional, volunteer
	Address   string  `json:"address"`
	Longitude float64 `json:"longitude"`
	Latitude  float64 `json:"latitude"`
	Contact   string  `json:"contact"`
	Phone     string  `json:"phone"`
	Enabled   bool    `json:"enabled"`
	Notes     string  `json:"notes"`
}

// Personnel includes people without a platform login. It never grants API access.
type FirePersonnel struct {
	FireRecord
	Name      string `json:"name"`
	Phone     string `json:"phone"`
	StationID string `json:"stationId"`
	Position  string `json:"position"`
	Enabled   bool   `json:"enabled"`
	Notes     string `json:"notes"`
}

type FireEquipment struct {
	FireRecord
	StationID string `json:"stationId"`
	Name      string `json:"name"`
	Category  string `json:"category"`
	Quantity  int    `json:"quantity"`
	Unit      string `json:"unit"`
	Status    string `json:"status"` // ready, maintenance, retired
	Notes     string `json:"notes"`
}

type FireEquipmentUsage struct {
	EquipmentID string `json:"equipmentId"`
	Quantity    int    `json:"quantity"`
}

type FireDispatch struct {
	FireRecord
	StationID    string               `json:"stationId"`
	Title        string               `json:"title"`
	Type         string               `json:"type"` // fire, rescue, drill, other
	Location     string               `json:"location"`
	PersonnelIDs []string             `json:"personnelIds"`
	Equipment    []FireEquipmentUsage `json:"equipment"`
	Status       string               `json:"status"` // dispatched, returned
	StartedAt    int64                `json:"startedAt"`
	ReturnedAt   int64                `json:"returnedAt"`
	Summary      string               `json:"summary"`
	CreatedBy    string               `json:"createdBy"`
	ReturnedBy   string               `json:"returnedBy"`
}

type DutyShift struct {
	FireRecord
	Name      string `json:"name"`
	StartTime string `json:"startTime"` // HH:mm, end <= start means overnight
	EndTime   string `json:"endTime"`
}

type DutyAssignment struct {
	FireRecord
	StationID    string   `json:"stationId"`
	ShiftID      string   `json:"shiftId"`
	PersonnelIDs []string `json:"personnelIds"`
	StartAt      int64    `json:"startAt"`
	EndAt        int64    `json:"endAt"`
	Notes        string   `json:"notes"`
}

type DutySwap struct {
	FireRecord
	AssignmentID    string `json:"assignmentId"`
	FromPersonnelID string `json:"fromPersonnelId"`
	ToPersonnelID   string `json:"toPersonnelId"`
	Reason          string `json:"reason"`
	Status          string `json:"status"` // pending, approved, rejected
	RequestedBy     string `json:"requestedBy"`
	ReviewedBy      string `json:"reviewedBy"`
	ReviewNote      string `json:"reviewNote"`
	ReviewedAt      int64  `json:"reviewedAt"`
}

type Extinguisher struct {
	FireRecord
	Code                string `json:"code"`
	StationID           string `json:"stationId"`
	Location            string `json:"location"`
	Type                string `json:"type"` // dry_powder, co2, foam, water, other
	Specification       string `json:"specification"`
	Manufacturer        string `json:"manufacturer"`
	SerialNumber        string `json:"serialNumber"`
	ManufacturedOn      string `json:"manufacturedOn"` // YYYY-MM-DD
	ServiceDueOn        string `json:"serviceDueOn"`
	RetireOn            string `json:"retireOn"`
	InspectionCycleDays int    `json:"inspectionCycleDays"`
	Status              string `json:"status"` // active, maintenance, retired
	Notes               string `json:"notes"`
}

type FireInspectionCheck struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
}

type FireRectification struct {
	Action      string `json:"action"`
	SubmittedBy string `json:"submittedBy"`
	SubmittedAt int64  `json:"submittedAt"`
	Status      string `json:"status"` // pending, approved, rejected
	ReviewedBy  string `json:"reviewedBy"`
	ReviewedAt  int64  `json:"reviewedAt"`
	ReviewNote  string `json:"reviewNote"`
}

type FireInspection struct {
	FireRecord
	ExtinguisherID string                `json:"extinguisherId"`
	AssigneeID     string                `json:"assigneeId"`
	DueAt          int64                 `json:"dueAt"`
	Status         string                `json:"status"` // pending, rectifying, reviewing, completed, cancelled
	Notes          string                `json:"notes"`
	Checks         []FireInspectionCheck `json:"checks"`
	Result         string                `json:"result"` // pass, fail; calculated from checks
	InspectedAt    int64                 `json:"inspectedAt"`
	InspectedBy    string                `json:"inspectedBy"`
	Findings       string                `json:"findings"`
	Rectifications []FireRectification   `json:"rectifications"`
	CreatedBy      string                `json:"createdBy"`
	CancelReason   string                `json:"cancelReason"`
}

// ErrDutyOverlap is returned by storage that enforces non-overlapping duty
// per person when a save would break it.
var ErrDutyOverlap = errors.New("duty assignments overlap for one person")
