// Package continuity computes monitoring intervals without production writes.
package continuity

import "iot-platform/internal/model"

const AlgorithmVersion = "monitoring-continuity-v1"

const (
	Available     = "AVAILABLE"
	Unavailable   = "UNAVAILABLE"
	Unknown       = "UNKNOWN"
	Excluded      = "EXCLUDED"
	NotApplicable = "NOT_APPLICABLE"
)

type Range struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
}
type Attribute struct {
	ID        string   `json:"id"`
	ValueType string   `json:"valueType"`
	Minimum   *float64 `json:"minimum,omitempty"`
	Maximum   *float64 `json:"maximum,omitempty"`
}
type Profile struct {
	ID               string              `json:"id"`
	ProductID        string              `json:"productId,omitempty"`
	EffectiveFrom    int64               `json:"effectiveFrom"`
	EffectiveTo      int64               `json:"effectiveTo,omitempty"`
	Mode             string              `json:"mode"`
	Attributes       []Attribute         `json:"attributes"`
	MessageTypes     []model.MessageType `json:"messageTypes"`
	Merge            string              `json:"merge"`
	PeriodMs         int64               `json:"periodMs"`
	ToleranceMs      int64               `json:"toleranceMs"`
	Importance       string              `json:"importance,omitempty"`
	LongGapMs        int64               `json:"longGapMs,omitempty"`
	FrequentGapCount int                 `json:"frequentGapCount,omitempty"`
}
type Observation struct {
	ID string `json:"id"`
	Range
	Reason      string `json:"reason"`
	Basis       string `json:"basis"`
	ConfirmedBy string `json:"confirmedBy"`
}
type Interval struct {
	ID          string `json:"id"`
	DeviceID    string `json:"deviceId"`
	AttributeID string `json:"attributeId,omitempty"`
	ProfileID   string `json:"profileId,omitempty"`
	Track       string `json:"track"`
	Range
	State       string   `json:"state"`
	Reasons     []string `json:"reasons,omitempty"`
	EvidenceIDs []string `json:"evidenceIds,omitempty"`
}
type Metric struct {
	ID          string `json:"id"`
	DeviceID    string `json:"deviceId"`
	AttributeID string `json:"attributeId,omitempty"`
	ProfileID   string `json:"profileId,omitempty"`
	Track       string `json:"track"`
	Range
	WindowMs               int64    `json:"windowMs"`
	PlannedMs              int64    `json:"plannedMs"`
	ExcludedMs             int64    `json:"excludedMs"`
	AvailableMs            int64    `json:"availableMs"`
	UnavailableMs          int64    `json:"unavailableMs"`
	UnknownMs              int64    `json:"unknownMs"`
	NotApplicableMs        int64    `json:"notApplicableMs"`
	KnownAvailability      *float64 `json:"knownAvailability,omitempty"`
	KnownCoverage          *float64 `json:"knownCoverage,omitempty"`
	FullWindowAvailability *float64 `json:"fullWindowAvailability,omitempty"`
	LongestGapMs           int64    `json:"longestGapMs"`
	GapCount               int      `json:"gapCount"`
}
type Finding struct {
	ID        string `json:"id"`
	DeviceID  string `json:"deviceId,omitempty"`
	ProfileID string `json:"profileId,omitempty"`
	Kind      string `json:"kind"`
	Range
	Explanation string         `json:"explanation"`
	EvidenceIDs []string       `json:"evidenceIds"`
	Values      map[string]any `json:"values,omitempty"`
}
type DeviceInput struct {
	DeviceID     string
	Window       Range
	Profiles     []Profile
	Measurements []model.MeasurementFact
	Connection   []model.DeviceStateIntervalFact
	// These ranges are source coverage proved by the reader. Empty coverage
	// never means that an empty measurement slice proves a missing report.
	ReceivedCoverage  []Range
	AvailableCoverage []Range
	Observations      []Observation
	MaximumIntervals  int
}
type DeviceResult struct {
	AlgorithmVersion string     `json:"algorithmVersion"`
	DeviceID         string     `json:"deviceId"`
	Intervals        []Interval `json:"intervals"`
	Metrics          []Metric   `json:"metrics"`
	Findings         []Finding  `json:"findings"`
	Limitations      []string   `json:"limitations"`
}
type DependencyGroup struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	ResourceID string `json:"resourceId"`
	Range
	MemberIDs           []string `json:"memberIds"`
	VisibleDeviceCount  int      `json:"visibleDeviceCount"`
	AnalysisDeviceCount int      `json:"analysisDeviceCount"`
	Concentration       float64  `json:"concentration"`
	HistoryQuality      string   `json:"historyQuality"`
	EvidenceIDs         []string `json:"evidenceIds"`
}
type CommonGapPolicy struct {
	Version          string  `json:"version"`
	MinimumGapCount  int     `json:"minimumGapCount"`
	MinimumOverlapMs int64   `json:"minimumOverlapMs"`
	MinimumJaccard   float64 `json:"minimumJaccard"`
}
