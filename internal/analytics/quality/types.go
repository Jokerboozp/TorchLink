// Package quality computes immutable, deterministic measurement-quality facts.
// It has no repository, model-provider, alarm, or device-control dependency.
package quality

import "time"

const AlgorithmVersion = "data-quality-v1"
const MinimumStatisticalSamples = 30

type Mode string

const (
	Periodic Mode = "periodic"
	Event    Mode = "event"
)

// State describes whether the particular calculation has enough evidence. It
// is independent of whether that calculation found an issue.
type State string

const (
	Assessed      State = "assessed"
	Unknown       State = "unknown"
	NotApplicable State = "not_applicable"
	Partial       State = "partial"
)

type Window struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// Coverage is supplied by the fact reader, never inferred from an empty slice.
// Missing, expired, denied, and failed sources retain their separate Reasons.
// Its Window includes actual tolerance-padding coverage for periodic metrics;
// merely reading the main window yields partial boundary completeness.
type Coverage struct {
	State         State    `json:"state"`
	Window        Window   `json:"window"`
	Reasons       []string `json:"reasons,omitempty"`
	SourceVersion string   `json:"sourceVersion,omitempty"`
}

type CUSUMConfig struct {
	Version string `json:"version"`
	// Allowance and Threshold are in measurement units, not standard deviations.
	Allowance float64 `json:"allowance"`
	Threshold float64 `json:"threshold"`
}

// Profile is an immutable published configuration. Zero physical thresholds
// do not imply a universal default; pointer fields distinguish missing values.
type Profile struct {
	ID                string         `json:"id"`
	Version           string         `json:"version"`
	Mode              Mode           `json:"mode"`
	EffectiveFrom     time.Time      `json:"effectiveFrom"`
	EffectiveTo       time.Time      `json:"effectiveTo,omitempty"`
	ScheduleAnchor    time.Time      `json:"scheduleAnchor,omitempty"`
	Period            time.Duration  `json:"period"`
	Tolerance         time.Duration  `json:"tolerance"`
	ValueType         string         `json:"valueType"`
	Required          bool           `json:"required"`
	Unit              string         `json:"unit"`
	UnitConfirmed     bool           `json:"unitConfirmed"`
	RangeConfirmed    bool           `json:"rangeConfirmed"`
	Minimum           *float64       `json:"minimum,omitempty"`
	Maximum           *float64       `json:"maximum,omitempty"`
	Epsilon           *float64       `json:"epsilon,omitempty"`
	StableDuration    time.Duration  `json:"stableDuration"`
	MaxRate           *float64       `json:"maxRate,omitempty"`
	MaxSequenceGap    time.Duration  `json:"maxSequenceGap"`
	FutureTolerance   *time.Duration `json:"futureTolerance,omitempty"`
	ClockCalibrated   bool           `json:"clockCalibrated"`
	MinimumSamples    int            `json:"minimumSamples"`
	DeviationMethod   string         `json:"deviationMethod,omitempty"` // mad or quantile
	MADMultiplier     *float64       `json:"madMultiplier,omitempty"`
	AbsoluteDeviation *float64       `json:"absoluteDeviation,omitempty"`
	QuantileLower     float64        `json:"quantileLower"`
	QuantileUpper     float64        `json:"quantileUpper"`
	CUSUM             *CUSUMConfig   `json:"cusum,omitempty"`
}

// Sample retains all three clocks. Analyze never substitutes one for another.
// The caller reads the union of event-time and reception-time windows with
// tolerance padding. Padding records can cover a slot, but do not inflate the
// main-window format or sequence denominators.
type Sample struct {
	ID                   string    `json:"id"`
	MessageID            string    `json:"messageId"`
	RawMessageID         string    `json:"rawMessageId,omitempty"`
	EventAt              time.Time `json:"eventAt"`
	ReceivedAt           time.Time `json:"receivedAt"`
	AvailableAt          time.Time `json:"availableAt,omitempty"`
	Value                any       `json:"value"`
	Unit                 string    `json:"unit"`
	ProtocolVersion      string    `json:"protocolVersion"`
	ConfigurationVersion string    `json:"configurationVersion"`
	OperatingCondition   string    `json:"operatingCondition"`
}

type Baseline struct {
	ID                   string    `json:"id"`
	ProfileVersion       string    `json:"profileVersion"`
	ProtocolVersion      string    `json:"protocolVersion"`
	ConfigurationVersion string    `json:"configurationVersion"`
	Unit                 string    `json:"unit"`
	OperatingCondition   string    `json:"operatingCondition"`
	Window               Window    `json:"window"`
	ValidFrom            time.Time `json:"validFrom"`
	ValidUntil           time.Time `json:"validUntil"`
	SampleCount          int       `json:"sampleCount"`
	Median               float64   `json:"median"`
	MAD                  float64   `json:"mad"`
	QuantileLower        float64   `json:"quantileLower"`
	QuantileUpper        float64   `json:"quantileUpper"`
	LowerValue           float64   `json:"lowerValue"`
	UpperValue           float64   `json:"upperValue"`
	ConfirmedBy          string    `json:"confirmedBy,omitempty"`
	ConfirmedAt          time.Time `json:"confirmedAt,omitempty"`
	CUSUMVersion         string    `json:"cusumVersion,omitempty"`
}

type BaselineRequest struct {
	Baseline Baseline
	Profile  Profile
	Samples  []Sample
}

type ParseStatus string

const (
	ParseNotAttempted ParseStatus = "not_attempted"
	ParseFailed       ParseStatus = "failed"
	ParseSucceeded    ParseStatus = "succeeded"
	ParseUnknown      ParseStatus = "unknown"
)

type ParseAttempt struct {
	ID     string      `json:"id"`
	Status ParseStatus `json:"status"`
	At     time.Time   `json:"at"`
}

// Attempts are immutable attempt records. A last status alone cannot prove an
// attempt-by-attempt failure ratio. The API's filled Parsed boolean is not input.
type ParseOutcome struct {
	RawMessageID               string         `json:"rawMessageId"`
	ReceivedAt                 time.Time      `json:"receivedAt"`
	Archived                   bool           `json:"archived"`
	Attempted                  bool           `json:"attempted"`
	LastStatus                 ParseStatus    `json:"lastStatus"`
	SuccessfulStandardMessages int            `json:"successfulStandardMessages"`
	Attempts                   []ParseAttempt `json:"attempts,omitempty"`
}

type Input struct {
	DeviceID         string         `json:"deviceId"`
	AttributeID      string         `json:"attributeId"`
	Window           Window         `json:"window"`
	Profiles         []Profile      `json:"profiles"`
	Samples          []Sample       `json:"samples"`
	Baselines        []Baseline     `json:"baselines,omitempty"`
	Coverage         Coverage       `json:"coverage"`
	ParseOutcomes    []ParseOutcome `json:"parseOutcomes,omitempty"`
	ParseCoverage    Coverage       `json:"parseCoverage"`
	AttemptsComplete bool           `json:"attemptsComplete"`
	// Protects against accidental unbounded materialization of slot arrays.
	// The application should additionally bound database reads and input size.
	MaximumSlots int `json:"maximumSlots,omitempty"`
}

type Ratio struct {
	State       State    `json:"state"`
	Numerator   int      `json:"numerator"`
	Denominator int      `json:"denominator"`
	Value       *float64 `json:"value,omitempty"`
	Reasons     []string `json:"reasons,omitempty"`
}

type Statistics struct {
	Count  int     `json:"count"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
	Median float64 `json:"median"`
	MAD    float64 `json:"mad"`
	P05    float64 `json:"p05"`
	P25    float64 `json:"p25"`
	P75    float64 `json:"p75"`
	P95    float64 `json:"p95"`
}

type Slot struct {
	At                      time.Time `json:"at"`
	Covered                 bool      `json:"covered"`
	RepresentativeID        string    `json:"representativeId,omitempty"`
	RepresentativeMessageID string    `json:"representativeMessageId,omitempty"`
	Samples                 int       `json:"samples"`
}

type Completeness struct {
	Missing    Ratio  `json:"missing"`
	Expected   int    `json:"expected"`
	Covered    int    `json:"covered"`
	Duplicates int    `json:"duplicates"`
	OutOfSlot  int    `json:"outOfSlot"`
	Slots      []Slot `json:"slots,omitempty"`
}

type TimeQuality struct {
	OutOfOrder      Ratio       `json:"outOfOrder"`
	Rollback        Ratio       `json:"rollback"`
	Future          Ratio       `json:"future"`
	MissingClocks   int         `json:"missingClocks"`
	Difference      *Statistics `json:"differenceSeconds,omitempty"`
	ClockCalibrated bool        `json:"clockCalibrated"`
	Limitations     []string    `json:"limitations,omitempty"`
}

type SequenceMetric struct {
	Stable        Ratio       `json:"stable"`
	Rate          Ratio       `json:"rate"`
	Deviation     Ratio       `json:"deviation"`
	Drift         Ratio       `json:"drift"`
	Statistics    *Statistics `json:"statistics,omitempty"`
	Segments      []Segment   `json:"segments,omitempty"`
	SameTime      int         `json:"sameTime"`
	LongGaps      int         `json:"longGaps"`
	VersionBreaks int         `json:"versionBreaks"`
}

type Segment struct {
	Window               Window      `json:"window"`
	Unit                 string      `json:"unit"`
	ProtocolVersion      string      `json:"protocolVersion"`
	ConfigurationVersion string      `json:"configurationVersion"`
	OperatingCondition   string      `json:"operatingCondition"`
	SampleCount          int         `json:"sampleCount"`
	Statistics           *Statistics `json:"statistics,omitempty"`
}

type WindowMetric struct {
	ID                    string         `json:"id"`
	ProfileID             string         `json:"profileId"`
	ProfileVersion        string         `json:"profileVersion"`
	Window                Window         `json:"window"`
	State                 State          `json:"state"`
	Reasons               []string       `json:"reasons,omitempty"`
	InspectedSamples      int            `json:"inspectedSamples"`
	ValidSamples          int            `json:"validSamples"`
	Format                Ratio          `json:"format"`
	Range                 Ratio          `json:"range"`
	EventCompleteness     Completeness   `json:"eventCompleteness"`
	ReceptionCompleteness Completeness   `json:"receptionCompleteness"`
	Time                  TimeQuality    `json:"time"`
	Sequence              SequenceMetric `json:"sequence"`
}

type Evidence struct {
	ID                   string    `json:"id"`
	SampleID             string    `json:"sampleId"`
	MessageID            string    `json:"messageId"`
	RawMessageID         string    `json:"rawMessageId,omitempty"`
	EventAt              time.Time `json:"eventAt"`
	ReceivedAt           time.Time `json:"receivedAt"`
	AvailableAt          time.Time `json:"availableAt,omitempty"`
	ProtocolVersion      string    `json:"protocolVersion"`
	ConfigurationVersion string    `json:"configurationVersion"`
	Value                any       `json:"value"`
}

type Finding struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	MetricID    string   `json:"metricId"`
	Window      Window   `json:"window"`
	Level       string   `json:"level"` // issue or verification_required; never fire risk
	Threshold   *float64 `json:"threshold,omitempty"`
	Value       *float64 `json:"value,omitempty"`
	BaselineID  string   `json:"baselineId,omitempty"`
	EvidenceIDs []string `json:"evidenceIds,omitempty"`
	Explanation string   `json:"explanation"`
}

type ParseMetric struct {
	State                      State    `json:"state"`
	ObservedRaw                int      `json:"observedRaw"`
	Archived                   int      `json:"archived"`
	Attempted                  int      `json:"attempted"`
	NotAttempted               int      `json:"notAttempted"`
	LastFailed                 int      `json:"lastFailed"`
	LastSucceeded              int      `json:"lastSucceeded"`
	UnknownLast                int      `json:"unknownLast"`
	SuccessfulStandardMessages int      `json:"successfulStandardMessages"`
	LastFailure                Ratio    `json:"lastFailure"`
	AttemptFailure             Ratio    `json:"attemptFailure"`
	Reasons                    []string `json:"reasons,omitempty"`
}

type Result struct {
	AlgorithmVersion string         `json:"algorithmVersion"`
	DeviceID         string         `json:"deviceId"`
	AttributeID      string         `json:"attributeId"`
	Window           Window         `json:"window"`
	State            State          `json:"state"`
	Metrics          []WindowMetric `json:"metrics"`
	Findings         []Finding      `json:"findings"`
	Evidence         []Evidence     `json:"evidence"`
	Parse            ParseMetric    `json:"parse"`
	Unconfigured     []Window       `json:"unconfigured,omitempty"`
	Limitations      []string       `json:"limitations,omitempty"`
}
