package model

// Device health signal types, computed from reported data without a model.
const (
	// SignalStuckValue: a numeric property did not change across the window.
	SignalStuckValue = "STUCK_VALUE"
	// SignalReportDrift: the average report interval is far from the
	// configured reporting interval.
	SignalReportDrift = "REPORT_DRIFT"
	// SignalOutOfRange: a share of values lies outside the thing model's
	// valid range.
	SignalOutOfRange = "OUT_OF_RANGE"
	// SignalPeerOutlier: a property's average differs strongly from the other
	// devices of the same template.
	SignalPeerOutlier = "PEER_OUTLIER"
)

// DeviceSignal is one health signal of a device. Property is empty for
// device-level signals such as report drift. Strength is 0..1.
type DeviceSignal struct {
	TenantID    string         `json:"tenantId"`
	DeviceID    string         `json:"deviceId"`
	ProductID   string         `json:"productId"`
	SignalType  string         `json:"signalType"`
	Property    string         `json:"property,omitempty"`
	Strength    float64        `json:"strength"`
	WindowStart int64          `json:"windowStart"`
	WindowEnd   int64          `json:"windowEnd"`
	Evidence    map[string]any `json:"evidence,omitempty"`
	UpdatedAt   int64          `json:"updatedAt"`
}

// PropertyRange is a thing-model valid range used when counting
// out-of-range values; a nil bound is open.
type PropertyRange struct {
	ProductID string
	Property  string
	Min, Max  *float64
}

// DevicePropertyStat summarises one numeric property of one device over a
// window.
type DevicePropertyStat struct {
	DeviceID   string  `json:"deviceId"`
	ProductID  string  `json:"productId"`
	Property   string  `json:"property"`
	Count      int64   `json:"count"`
	Min        float64 `json:"min"`
	Max        float64 `json:"max"`
	Mean       float64 `json:"mean"`
	StdDev     float64 `json:"stdDev"`
	OutOfRange int64   `json:"outOfRange"`
}

// DeviceReportStat counts one device's reports over a window.
type DeviceReportStat struct {
	DeviceID  string `json:"deviceId"`
	ProductID string `json:"productId"`
	Count     int64  `json:"count"`
	FirstAt   int64  `json:"firstAt"`
	LastAt    int64  `json:"lastAt"`
}
