package model

// Physical dates are supported by recorded evidence. The platform device's
// registration date is never copied into an asset's commissioning date.
type AssetInstance struct {
	Name           string                      `json:"name"`
	DeviceID       string                      `json:"deviceId"`
	ComponentID    string                      `json:"componentId,omitempty"`
	PhysicalID     string                      `json:"physicalId"`
	ManufacturedAt *int64                      `json:"manufacturedAt,omitempty"`
	InstalledAt    *int64                      `json:"installedAt,omitempty"`
	CommissionedAt *int64                      `json:"commissionedAt,omitempty"`
	RetiredAt      *int64                      `json:"retiredAt,omitempty"`
	EffectiveStart int64                       `json:"effectiveStart"`
	EffectiveEnd   *int64                      `json:"effectiveEnd,omitempty"`
	BoundaryStatus string                      `json:"boundaryStatus"`
	Importance     int                         `json:"importance"`
	Evidence       []ResponseEvidenceReference `json:"evidence"`
	Requests       []ResponseRequestReceipt    `json:"requests"`
}
type MaintenanceIntervention struct {
	Name               string                    `json:"name"`
	AssetRevisionID    string                    `json:"assetRevisionId"`
	Type               string                    `json:"type"`
	Status             string                    `json:"status"`
	StartedAt          int64                     `json:"startedAt,omitempty"`
	EndedAt            int64                     `json:"endedAt,omitempty"`
	Reason             string                    `json:"reason"`
	Actions            []string                  `json:"actions"`
	People             []string                  `json:"people"`
	Parts              []string                  `json:"parts"`
	FaultCycleIDs      []string                  `json:"faultCycleIds"`
	CorrectiveActionID string                    `json:"correctiveActionId,omitempty"`
	Verifications      []MaintenanceVerification `json:"verifications"`
	History            []ResponseActionEvent     `json:"history"`
	Requests           []ResponseRequestReceipt  `json:"requests"`
}
type MaintenanceVerification struct {
	ID            string                      `json:"id"`
	RequiredItems []string                    `json:"requiredItems"`
	CheckedItems  []string                    `json:"checkedItems"`
	Result        string                      `json:"result"`
	Explanation   string                      `json:"explanation"`
	Verifier      string                      `json:"verifier"`
	RecordedAt    int64                       `json:"recordedAt"`
	Evidence      []ResponseEvidenceReference `json:"evidence"`
}
type OperatingContext struct {
	Kind        string                      `json:"kind"`
	Start       int64                       `json:"start"`
	End         int64                       `json:"end"`
	Intensity   string                      `json:"intensity,omitempty"`
	Reason      string                      `json:"reason"`
	Confirmed   bool                        `json:"confirmed"`
	Confirmer   string                      `json:"confirmer,omitempty"`
	ConfirmedAt int64                       `json:"confirmedAt,omitempty"`
	Evidence    []ResponseEvidenceReference `json:"evidence"`
	SourceIDs   []string                    `json:"sourceIds,omitempty"`
	Requests    []ResponseRequestReceipt    `json:"requests"`
}
type MaintenanceAdmission struct {
	Version                    string  `json:"version"`
	MinimumEffectiveHours      float64 `json:"minimumEffectiveHours"`
	MinimumObservationCoverage float64 `json:"minimumObservationCoverage"`
	MinimumFaultSourceCoverage float64 `json:"minimumFaultSourceCoverage"`
	MinimumConfirmedCycles     int     `json:"minimumConfirmedCycles"`
	PostMaintenanceWaitMs      int64   `json:"postMaintenanceWaitMs"`
	ComparableContextRequired  bool    `json:"comparableContextRequired"`
}
type MaintenanceObservationParameters struct {
	InterventionRevisionID string                `json:"interventionRevisionId"`
	ComparisonType         string                `json:"comparisonType"`
	BeforeAssetRevisionID  string                `json:"beforeAssetRevisionId"`
	AfterAssetRevisionID   string                `json:"afterAssetRevisionId"`
	SwitchAt               int64                 `json:"switchAt,omitempty"`
	Before                 FactRange             `json:"before"`
	After                  FactRange             `json:"after"`
	ContextRevisionIDs     []string              `json:"contextRevisionIds"`
	QualityRunIDs          []string              `json:"qualityRunIds"`
	Admission              *MaintenanceAdmission `json:"admission,omitempty"`
	AdmissionRevisionID    string                `json:"admissionRevisionId,omitempty"`
}
type MaintenanceSideMetrics struct {
	WindowMs               int64                  `json:"windowMs"`
	AssetWindowMs          int64                  `json:"assetWindowMs"`
	ExcludedMs             int64                  `json:"excludedMs"`
	EffectiveMs            int64                  `json:"effectiveMs"`
	EffectiveHours         float64                `json:"effectiveHours"`
	ObservationCoverage    *float64               `json:"observationCoverage,omitempty"`
	ConfirmedFaultCycles   int                    `json:"confirmedFaultCycles"`
	FaultSourceKnownMs     int64                  `json:"faultSourceKnownMs"`
	FaultSourceUnknownMs   int64                  `json:"faultSourceUnknownMs"`
	FaultSourceCoverage    *float64               `json:"faultSourceCoverage,omitempty"`
	FaultsPer1000Hours     *float64               `json:"faultsPer1000Hours,omitempty"`
	OnlineMs               int64                  `json:"onlineMs"`
	OfflineMs              int64                  `json:"offlineMs"`
	StateUnknownMs         int64                  `json:"stateUnknownMs"`
	KnownOfflineRatio      *float64               `json:"knownOfflineRatio,omitempty"`
	StateCoverage          *float64               `json:"stateCoverage,omitempty"`
	FullWindowOfflineRatio *float64               `json:"fullWindowOfflineRatio,omitempty"`
	Exclusions             []MaintenanceExclusion `json:"exclusions"`
	Limitations            []string               `json:"limitations"`
}
type MaintenanceExclusion struct {
	Kind             string `json:"kind"`
	Start            int64  `json:"start"`
	End              int64  `json:"end"`
	SourceRevisionID string `json:"sourceRevisionId"`
}
type MaintenanceObservation struct {
	Parameters   MaintenanceObservationParameters `json:"parameters"`
	Before       MaintenanceSideMetrics           `json:"before"`
	After        MaintenanceSideMetrics           `json:"after"`
	Status       string                           `json:"status"`
	Verification string                           `json:"verification"`
	Confounders  []string                         `json:"confounders"`
	Limitations  []string                         `json:"limitations"`
}
type MaintenanceCost struct {
	SourceKind      string                      `json:"sourceKind"`
	SourceID        string                      `json:"sourceId"`
	Type            string                      `json:"type"`
	Currency        string                      `json:"currency"`
	Material        *string                     `json:"material,omitempty"`
	Labor           *string                     `json:"labor,omitempty"`
	ExternalService *string                     `json:"externalService,omitempty"`
	LaborHours      *string                     `json:"laborHours,omitempty"`
	OccurredAt      int64                       `json:"occurredAt"`
	PlanningStart   int64                       `json:"planningStart"`
	PlanningEnd     int64                       `json:"planningEnd"`
	Basis           string                      `json:"basis"`
	Evidence        []ResponseEvidenceReference `json:"evidence"`
	Requests        []ResponseRequestReceipt    `json:"requests"`
}
type InvestmentCandidate struct {
	ID                       string   `json:"id"`
	AssetRevisionID          string   `json:"assetRevisionId"`
	AssetID                  string   `json:"assetId"`
	Action                   string   `json:"action"`
	RequiredTier             int      `json:"requiredTier"`
	TierBasis                string   `json:"tierBasis"`
	UnresolvedVerifiedDefect bool     `json:"unresolvedVerifiedDefect"`
	DefectBasisIDs           []string `json:"defectBasisIds"`
	Importance               int      `json:"importance"`
	ObservationRunID         string   `json:"observationRunId,omitempty"`
	Comparable               bool     `json:"comparable"`
	MetricBasis              string   `json:"metricBasis,omitempty"`
	ConfirmedFaultRate       *float64 `json:"confirmedFaultRate,omitempty"`
	KnownOfflineMs           *int64   `json:"knownOfflineMs,omitempty"`
	QuoteRevisionID          string   `json:"quoteRevisionId,omitempty"`
	Constraints              []string `json:"constraints"`
}
type InvestmentScenario struct {
	UseFinance          bool                     `json:"useFinance"`
	RequiredPermissions []string                 `json:"requiredPermissions,omitempty"`
	Name                string                   `json:"name"`
	Status              string                   `json:"status"`
	Currency            string                   `json:"currency"`
	Budget              *string                  `json:"budget,omitempty"`
	PlanningStart       int64                    `json:"planningStart"`
	PlanningEnd         int64                    `json:"planningEnd"`
	PolicyVersion       string                   `json:"policyVersion"`
	Candidates          []InvestmentCandidate    `json:"candidates"`
	Adjustments         []InvestmentAdjustment   `json:"adjustments"`
	Decisions           []InvestmentDecision     `json:"decisions"`
	Requests            []ResponseRequestReceipt `json:"requests"`
}
type InvestmentAdjustment struct {
	CandidateOrder []string `json:"candidateOrder"`
	Reason         string   `json:"reason"`
	Actor          string   `json:"actor"`
	RecordedAt     int64    `json:"recordedAt"`
}
type InvestmentDecision struct {
	EvaluationRunID      string   `json:"evaluationRunId"`
	FactsHash            string   `json:"factsHash"`
	SelectedCandidateIDs []string `json:"selectedCandidateIds"`
	Reason               string   `json:"reason"`
	Actor                string   `json:"actor"`
	RecordedAt           int64    `json:"recordedAt"`
}
