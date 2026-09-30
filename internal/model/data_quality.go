package model

import "encoding/json"

const (
	DataQualityProfileKind     = "DATA_QUALITY_PROFILE"
	DataQualityBaselineKind    = "DATA_QUALITY_BASELINE"
	DataQualityCalibrationKind = "DATA_QUALITY_CALIBRATION"
	DataQualityAttachmentKind  = "DATA_QUALITY_ATTACHMENT"
)

// Quality configuration uses UTC Unix milliseconds and millisecond durations.
// The storage envelope supplies the immutable revision ID and version.
type QualityProfile struct {
	TargetType        string        `json:"targetType,omitempty"`
	AttributeID       string        `json:"attributeId"`
	ProductID         string        `json:"productId,omitempty"`
	Mode              string        `json:"mode"`
	EffectiveFrom     int64         `json:"effectiveFrom"`
	EffectiveTo       int64         `json:"effectiveTo,omitempty"`
	ScheduleAnchor    int64         `json:"scheduleAnchor,omitempty"`
	PeriodMs          int64         `json:"periodMs"`
	ToleranceMs       int64         `json:"toleranceMs"`
	ValueType         string        `json:"valueType"`
	Required          bool          `json:"required"`
	Unit              string        `json:"unit"`
	UnitConfirmed     bool          `json:"unitConfirmed"`
	RangeConfirmed    bool          `json:"rangeConfirmed"`
	Minimum           *float64      `json:"minimum,omitempty"`
	Maximum           *float64      `json:"maximum,omitempty"`
	Epsilon           *float64      `json:"epsilon,omitempty"`
	StableDurationMs  int64         `json:"stableDurationMs"`
	MaxRate           *float64      `json:"maxRate,omitempty"`
	MaxSequenceGapMs  int64         `json:"maxSequenceGapMs"`
	FutureToleranceMs *int64        `json:"futureToleranceMs,omitempty"`
	ClockCalibrated   bool          `json:"clockCalibrated"`
	MinimumSamples    int           `json:"minimumSamples"`
	DeviationMethod   string        `json:"deviationMethod,omitempty"`
	MADMultiplier     *float64      `json:"madMultiplier,omitempty"`
	AbsoluteDeviation *float64      `json:"absoluteDeviation,omitempty"`
	QuantileLower     float64       `json:"quantileLower"`
	QuantileUpper     float64       `json:"quantileUpper"`
	CUSUM             *QualityCUSUM `json:"cusum,omitempty"`
}
type QualityCUSUM struct {
	Version   string  `json:"version"`
	Allowance float64 `json:"allowance"`
	Threshold float64 `json:"threshold"`
}
type QualityBaselineRequest struct {
	DeviceID             string `json:"deviceId"`
	AttributeID          string `json:"attributeId"`
	ProfileRevisionID    string `json:"profileRevisionId"`
	Start                int64  `json:"start"`
	End                  int64  `json:"end"`
	ValidFrom            int64  `json:"validFrom"`
	ValidUntil           int64  `json:"validUntil"`
	OperatingCondition   string `json:"operatingCondition"`
	ProtocolVersion      string `json:"protocolVersion"`
	ConfigurationVersion string `json:"configurationVersion"`
}
type QualityBaseline struct {
	QualityBaselineRequest
	Unit           string                   `json:"unit"`
	ProfileVersion string                   `json:"profileVersion"`
	SampleCount    int                      `json:"sampleCount"`
	Median         float64                  `json:"median"`
	MAD            float64                  `json:"mad"`
	QuantileLower  float64                  `json:"quantileLower"`
	QuantileUpper  float64                  `json:"quantileUpper"`
	LowerValue     float64                  `json:"lowerValue"`
	UpperValue     float64                  `json:"upperValue"`
	ConfirmedBy    string                   `json:"confirmedBy,omitempty"`
	ConfirmedAt    int64                    `json:"confirmedAt,omitempty"`
	CUSUMVersion   string                   `json:"cusumVersion,omitempty"`
	InputHash      string                   `json:"inputHash"`
	Sources        []AnalysisSourceCoverage `json:"sources"`
}
type QualityCalibration struct {
	DeviceID      string   `json:"deviceId"`
	AttributeID   string   `json:"attributeId"`
	CalibratedAt  int64    `json:"calibratedAt"`
	Basis         string   `json:"basis"`
	ImplementedBy string   `json:"implementedBy"`
	Minimum       *float64 `json:"minimum,omitempty"`
	Maximum       *float64 `json:"maximum,omitempty"`
	Epsilon       *float64 `json:"epsilon,omitempty"`
	Attachments   []string `json:"attachments"`
}
type QualityAttachment struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ContentType string `json:"contentType"`
	Size        int64  `json:"size"`
	ObjectKey   string `json:"-"`
}
type QualityAttachmentRecord struct {
	QualityAttachment
	StorageKey string `json:"storageKey"`
}
type QualityConfigRequest struct {
	ResourceID      string          `json:"resourceId"`
	ExpectedVersion int64           `json:"expectedVersion"`
	Scope           string          `json:"scope"`
	DeviceIDs       []string        `json:"deviceIds"`
	Body            json.RawMessage `json:"body"`
}
type QualityRunParameters struct {
	AttributeIDs        []string `json:"attributeIds"`
	ProfileRevisionIDs  []string `json:"profileRevisionIds"`
	BaselineRevisionIDs []string `json:"baselineRevisionIds,omitempty"`
}
type QualityReviewRequest struct {
	RunID              string `json:"runId"`
	ExpectedRunVersion int64  `json:"expectedRunVersion"`
	Result             string `json:"result"`
	Explanation        string `json:"explanation"`
	CorrectsID         string `json:"correctsId,omitempty"`
	IdempotencyKey     string `json:"idempotencyKey"`
}
