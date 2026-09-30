package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
)

var ErrGovernanceConflict = errors.New("治理资源或分析输入已改变，请刷新后重试")
var ErrGovernanceInvalid = errors.New("治理请求不符合要求")

const (
	GovernanceTemplateKind         = "template"
	GovernanceSceneKind            = "scene-preset"
	GovernanceProfileKind          = "type-profile"
	GovernanceCaseKind             = "case"
	GovernanceRoundKind            = "round"
	GovernanceAlarmLinkKind        = "alarm-link"
	GovernanceVerificationKind     = "verification"
	GovernanceVerificationLinkKind = "verification-link"
	GovernanceActivityKind         = "activity"
	GovernanceCoverageKind         = "activity-coverage"
	GovernanceCauseKind            = "cause"
	GovernanceMeasureKind          = "measure"
	GovernancePlanKind             = "observation-plan"
	GovernanceReviewKind           = "observation-review"
	GovernanceReportKind           = "report"
	GovernanceEventKind            = "event"
	GovernanceReceiptKind          = "receipt"
	GovernanceAttachmentKind       = "attachment"
	GovernanceBusinessLinkKind     = "business-link"
	GovernanceReminderKind         = "reminder"
	GovernanceUploadAttemptKind    = "upload-attempt"
)

// GovernanceDocument is a typed resource envelope. Relational columns below are
// independently indexed and constrained; Body stores the corresponding type.
type GovernanceDocument struct {
	ID             string          `json:"id"`
	TenantID       string          `json:"tenantId"`
	Kind           string          `json:"kind"`
	Version        int64           `json:"version"`
	CreatedBy      string          `json:"createdBy"`
	CreatedAt      int64           `json:"createdAt"`
	UpdatedAt      int64           `json:"updatedAt"`
	CaseID         string          `json:"caseId,omitempty"`
	RoundID        string          `json:"roundId,omitempty"`
	ParentID       string          `json:"parentId,omitempty"`
	ResourceID     string          `json:"resourceId,omitempty"`
	RevisionNumber int             `json:"revisionNumber,omitempty"`
	Status         string          `json:"status,omitempty"`
	OwnerUserID    string          `json:"ownerUserId,omitempty"`
	DeviceIDs      []string        `json:"deviceIds"`
	PointKey       string          `json:"pointKey,omitempty"`
	OccurredAt     int64           `json:"occurredAt,omitempty"`
	CorrectsID     string          `json:"correctsId,omitempty"`
	Body           json.RawMessage `json:"body"`
}

func GovernanceBody[T any](d GovernanceDocument) (v T, err error) {
	err = json.Unmarshal(d.Body, &v)
	return
}
func GovernanceHash(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

type GovernanceFilter struct {
	Kind        string
	CaseID      string
	RoundID     string
	ParentID    string
	ResourceID  string
	CorrectsID  string
	Status      string
	OwnerUserID string
	DeviceIDs   []string
	AllDevices  bool
	Start       int64
	End         int64
	Limit       int
	Offset      int
}
type GovernancePoint struct {
	DeviceID    string `json:"deviceId"`
	ComponentID string `json:"componentId,omitempty"`
	AlarmType   string `json:"alarmType"`
	OriginKind  string `json:"originKind"`
	SignalKey   string `json:"signalKey"`
}

func (p GovernancePoint) Key(sourceKind string) string {
	b, _ := json.Marshal([]string{p.DeviceID, p.ComponentID, p.AlarmType, p.OriginKind, p.SignalKey, sourceKind})
	return string(b)
}

type GovernanceOption struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}
type GovernanceField struct {
	ID                string             `json:"id"`
	Label             string             `json:"label"`
	Control           string             `json:"control"`
	Required          bool               `json:"required"`
	Protected         bool               `json:"protected"`
	Options           []GovernanceOption `json:"options,omitempty"`
	Min               *float64           `json:"min,omitempty"`
	Max               *float64           `json:"max,omitempty"`
	SourceFieldPath   string             `json:"sourceFieldPath,omitempty"`
	SourceDeviceID    string             `json:"sourceDeviceId,omitempty"`
	SourceMessageID   string             `json:"sourceMessageId,omitempty"`
	SourceType        string             `json:"sourceType,omitempty"`
	SourceValueHash   string             `json:"sourceValueHash,omitempty"`
	SourceConfirmedBy string             `json:"sourceConfirmedBy,omitempty"`
	SourceConfirmedAt int64              `json:"sourceConfirmedAt,omitempty"`
}
type GovernanceConfiguration struct {
	ResourceID           string             `json:"resourceId"`
	Name                 string             `json:"name"`
	RevisionNumber       int                `json:"revisionNumber"`
	Status               string             `json:"status"`
	SchemaVersion        string             `json:"schemaVersion"`
	Builtin              bool               `json:"builtin"`
	Hash                 string             `json:"hash"`
	Fields               []GovernanceField  `json:"fields"`
	ApplicableAlarmTypes []string           `json:"applicableAlarmTypes,omitempty"`
	ActivityOptions      []GovernanceOption `json:"activityOptions,omitempty"`
	EnvironmentOptions   []GovernanceOption `json:"environmentOptions,omitempty"`
	FacilityQuestions    []GovernanceField  `json:"facilityQuestions,omitempty"`
	RequiredEvidence     []string           `json:"requiredEvidence,omitempty"`
	DeviceIDs            []string           `json:"deviceIds"`
	ProductID            string             `json:"productId,omitempty"`
	AlarmType            string             `json:"alarmType,omitempty"`
	OriginKind           string             `json:"originKind,omitempty"`
	SignalKey            string             `json:"signalKey,omitempty"`
	CycleMethod          string             `json:"cycleMethod,omitempty"`
	TimeBasis            string             `json:"timeBasis,omitempty"`
	RecoverySemantics    string             `json:"recoverySemantics,omitempty"`
	ReferenceBasis       string             `json:"referenceBasis,omitempty"`
	PublishedBy          string             `json:"publishedBy,omitempty"`
	PublishedAt          int64              `json:"publishedAt,omitempty"`
}
type GovernanceConfigurationRefs struct {
	TemplateRevisionID    string `json:"templateRevisionId"`
	ScenePresetRevisionID string `json:"scenePresetRevisionId"`
	TypeProfileRevisionID string `json:"typeProfileRevisionId"`
	TemplateHash          string `json:"templateHash"`
	ScenePresetHash       string `json:"scenePresetHash"`
	TypeProfileHash       string `json:"typeProfileHash"`
}
type GovernanceCase struct {
	IdentityBasis string `json:"identityBasis"`
	GovernancePoint
	GovernanceConfigurationRefs
	Title           string   `json:"title"`
	Location        string   `json:"location"`
	OwnerUserID     string   `json:"ownerUserId"`
	Status          string   `json:"status"`
	CurrentRoundID  string   `json:"currentRoundId"`
	DataRevision    int64    `json:"dataRevision"`
	ObservationIDs  []string `json:"observationIds"`
	AssetInstanceID string   `json:"assetInstanceId"`
	IdentityQuality string   `json:"identityQuality"`
}
type GovernanceRound struct {
	IdentityBasis string `json:"identityBasis"`
	GovernanceConfigurationRefs
	CaseID           string `json:"caseId"`
	Number           int    `json:"number"`
	PreviousRoundID  string `json:"previousRoundId,omitempty"`
	Status           string `json:"status"`
	StartedAt        int64  `json:"startedAt"`
	EndedAt          *int64 `json:"endedAt,omitempty"`
	ReportID         string `json:"reportId,omitempty"`
	EndReason        string `json:"endReason,omitempty"`
	AssetInstanceID  string `json:"assetInstanceId"`
	IdentityQuality  string `json:"identityQuality"`
	LocationSnapshot string `json:"locationSnapshot"`
}
type GovernanceAlarmLink struct {
	RoundID          string   `json:"roundId"`
	ObservationIDs   []string `json:"observationIds"`
	CycleRevisionIDs []string `json:"cycleRevisionIds,omitempty"`
	Reason           string   `json:"reason"`
	ConfirmedBy      string   `json:"confirmedBy"`
	CorrectsID       string   `json:"correctsId,omitempty"`
	CorrectionReason string   `json:"correctionReason,omitempty"`
	Removed          bool     `json:"removed"`
}
type FieldVerification struct {
	Complete bool `json:"complete"`
	GovernancePoint
	GovernanceConfigurationRefs
	ObservationIDs     []string       `json:"observationIds"`
	VerificationMethod string         `json:"verificationMethod"`
	VerifiedAt         *int64         `json:"verifiedAt"`
	RecordedAt         int64          `json:"recordedAt"`
	FieldResult        string         `json:"fieldResult"`
	ActivityRelation   string         `json:"activityRelation"`
	CheckScope         string         `json:"checkScope"`
	FieldValues        map[string]any `json:"fieldValues"`
	ActivityIDs        []string       `json:"activityIds"`
	Description        string         `json:"description"`
	AttachmentIDs      []string       `json:"attachmentIds"`
	Status             string         `json:"status"`
	ConfirmedBy        string         `json:"confirmedBy,omitempty"`
	ConfirmedAt        int64          `json:"confirmedAt,omitempty"`
	CorrectsID         string         `json:"correctsId,omitempty"`
	CorrectionReason   string         `json:"correctionReason,omitempty"`
}
type VerificationRoundLink struct {
	RoundID             string   `json:"roundId"`
	VerificationID      string   `json:"verificationId"`
	VerificationVersion int64    `json:"verificationVersion"`
	ObservationIDs      []string `json:"observationIds"`
	Reason              string   `json:"reason"`
	CorrectsID          string   `json:"correctsId,omitempty"`
	CorrectionReason    string   `json:"correctionReason,omitempty"`
}
type FieldActivityRevision struct {
	TemplateHash          string            `json:"templateHash"`
	ScenePresetHash       string            `json:"scenePresetHash"`
	ActivityID            string            `json:"activityId"`
	Location              string            `json:"location"`
	DeviceIDs             []string          `json:"deviceIds"`
	ActivityType          string            `json:"activityType"`
	Unit                  string            `json:"unit"`
	Actual                bool              `json:"actual"`
	StartAt               *int64            `json:"startAt"`
	EndAt                 *int64            `json:"endAt"`
	TimeQuality           string            `json:"timeQuality"`
	Source                string            `json:"source"`
	Status                string            `json:"status"`
	Conditions            map[string]string `json:"conditions"`
	Description           string            `json:"description"`
	TemplateRevisionID    string            `json:"templateRevisionId"`
	ScenePresetRevisionID string            `json:"scenePresetRevisionId"`
	RevisionNumber        int               `json:"revisionNumber"`
	CorrectsID            string            `json:"correctsId,omitempty"`
	CorrectionReason      string            `json:"correctionReason,omitempty"`
}
type ActivityCoverage struct {
	CoverageID       string   `json:"coverageId"`
	Location         string   `json:"location"`
	DeviceIDs        []string `json:"deviceIds"`
	ActivityType     string   `json:"activityType"`
	StartAt          int64    `json:"startAt"`
	EndAt            int64    `json:"endAt"`
	Coverage         string   `json:"coverage"`
	Basis            string   `json:"basis"`
	DeclaredBy       string   `json:"declaredBy"`
	RevisionNumber   int      `json:"revisionNumber"`
	CorrectsID       string   `json:"correctsId,omitempty"`
	CorrectionReason string   `json:"correctionReason,omitempty"`
}
type CauseAssessment struct {
	RoundID             string   `json:"roundId"`
	Cause               string   `json:"cause"`
	ObservationIDs      []string `json:"observationIds"`
	VerificationIDs     []string `json:"verificationIds"`
	SupportEvidenceIDs  []string `json:"supportEvidenceIds"`
	ConflictEvidenceIDs []string `json:"conflictEvidenceIds"`
	Status              string   `json:"status"`
	ConfirmationBasis   string   `json:"confirmationBasis"`
	ConfirmedBy         string   `json:"confirmedBy"`
	CorrectsID          string   `json:"correctsId,omitempty"`
	CorrectionReason    string   `json:"correctionReason,omitempty"`
}
type ImprovementMeasure struct {
	RoundID            string `json:"roundId"`
	Content            string `json:"content"`
	OwnerUserID        string `json:"ownerUserId"`
	DueAt              int64  `json:"dueAt"`
	Status             string `json:"status"`
	Required           bool   `json:"required"`
	RequiresAcceptance bool   `json:"requiresAcceptance"`
	Basis              string `json:"basis"`
	Implementation     string `json:"implementation"`
	ImplementedBy      string `json:"implementedBy"`
	ImplementedAt      int64  `json:"implementedAt"`
	Acceptance         string `json:"acceptance"`
	AcceptedBy         string `json:"acceptedBy"`
	AcceptedAt         int64  `json:"acceptedAt"`
	CancellationReason string `json:"cancellationReason"`
	Followup           string `json:"followup"`
	CorrectsID         string `json:"correctsId,omitempty"`
	CorrectionReason   string `json:"correctionReason,omitempty"`
}
type ObservationPlan struct {
	BeforeConditions       string                         `json:"beforeConditions"`
	AfterConditions        string                         `json:"afterConditions"`
	ActivityType           string                         `json:"activityType"`
	MonitoringIntervals    []GovernanceMonitoringInterval `json:"monitoringIntervals"`
	BeforeConditionsHash   string                         `json:"beforeConditionsHash"`
	AfterConditionsHash    string                         `json:"afterConditionsHash"`
	TimeBasis              string                         `json:"timeBasis"`
	RoundID                string                         `json:"roundId"`
	MeasureVersions        map[string]int64               `json:"measureVersions"`
	BeforeStart            int64                          `json:"beforeStart"`
	BeforeEnd              int64                          `json:"beforeEnd"`
	AfterStart             int64                          `json:"afterStart"`
	AfterEnd               int64                          `json:"afterEnd"`
	ActivityUnit           string                         `json:"activityUnit"`
	AdmissionConditions    []string                       `json:"admissionConditions"`
	Exclusions             []string                       `json:"exclusions"`
	MinimumMonitoringHours float64                        `json:"minimumMonitoringHours"`
	MinimumActivities      int                            `json:"minimumActivities"`
	CoverageRequirement    string                         `json:"coverageRequirement"`
	ReviewerUserID         string                         `json:"reviewerUserId"`
	Status                 string                         `json:"status"`
}
type GovernanceMonitoringInterval struct {
	DeviceID    string `json:"deviceId"`
	Start       int64  `json:"start"`
	End         int64  `json:"end"`
	Kind        string `json:"kind"`
	Basis       string `json:"basis"`
	ConfirmedBy string `json:"confirmedBy"`
	ConfirmedAt int64  `json:"confirmedAt"`
}
type GovernanceSourceVersion struct {
	DependencyKey string `json:"dependencyKey"`
	BucketStart   int64  `json:"bucketStart"`
	Generation    int64  `json:"generation"`
}
type ObservationReview struct {
	RoundID              string                    `json:"roundId"`
	PlanID               string                    `json:"planId"`
	PlanVersion          int64                     `json:"planVersion"`
	AnalysisSnapshotID   string                    `json:"analysisSnapshotId"`
	FactsHash            string                    `json:"factsHash"`
	DataRevision         int64                     `json:"dataRevision"`
	SourceRevisionVector []GovernanceSourceVersion `json:"sourceRevisionVector"`
	Conclusion           string                    `json:"conclusion"`
	Limitations          []string                  `json:"limitations"`
	Followup             string                    `json:"followup"`
	FollowupOwnerUserID  string                    `json:"followupOwnerUserId"`
	Status               string                    `json:"status"`
	ConfirmedBy          string                    `json:"confirmedBy"`
	ConfirmedAt          int64                     `json:"confirmedAt"`
	CorrectsID           string                    `json:"correctsId,omitempty"`
	CorrectionReason     string                    `json:"correctionReason,omitempty"`
}
type GovernanceReport struct {
	CaseID               string                    `json:"caseId"`
	RoundID              string                    `json:"roundId"`
	ReviewID             string                    `json:"reviewId"`
	ReviewVersion        int64                     `json:"reviewVersion"`
	AnalysisSnapshotID   string                    `json:"analysisSnapshotId"`
	FactsHash            string                    `json:"factsHash"`
	DataRevision         int64                     `json:"dataRevision"`
	SourceRevisionVector []GovernanceSourceVersion `json:"sourceRevisionVector"`
	Conclusion           string                    `json:"conclusion"`
	Limitations          []string                  `json:"limitations"`
	Followup             string                    `json:"followup"`
	FollowupOwnerUserID  string                    `json:"followupOwnerUserId"`
	ConfirmedBy          string                    `json:"confirmedBy"`
	ConfirmedAt          int64                     `json:"confirmedAt"`
	Resources            []GovernanceDocument      `json:"resources"`
}
type GovernanceEvent struct {
	CaseID          string `json:"caseId"`
	RoundID         string `json:"roundId"`
	ResourceID      string `json:"resourceId"`
	ResourceVersion int64  `json:"resourceVersion"`
	Action          string `json:"action"`
	Actor           string `json:"actor"`
	OccurredAt      int64  `json:"occurredAt"`
	RecordedAt      int64  `json:"recordedAt"`
	Reason          string `json:"reason"`
}
