package model

// VerificationRules describe field evidence required by a device template.
// They are independent of business alarm state and of the device's enabled flag.
type VerificationRules struct {
	Mode                 string   `json:"mode"`
	MinMessages          int      `json:"minMessages"`
	WindowSeconds        int      `json:"windowSeconds"`
	MaxGapSeconds        int      `json:"maxGapSeconds"`
	RequiredMessageTypes []string `json:"requiredMessageTypes,omitempty"`
	RequiredProperties   []string `json:"requiredProperties,omitempty"`
	RequiredEvents       []string `json:"requiredEvents,omitempty"`
}

type TemplateCandidate struct {
	Product           Product               `json:"product"`
	ProtocolID        string                `json:"protocolId"`
	Version           string                `json:"version"`
	Profiles          []DeviceAccessProfile `json:"profiles"`
	VerificationRules VerificationRules     `json:"verificationRules"`
}

type VerificationCheck struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	State  string `json:"state"`
	Detail string `json:"detail"`
}

type DeviceVerification struct {
	Status             string              `json:"status"`
	DeviceID           string              `json:"deviceId"`
	ProductID          string              `json:"productId"`
	Fingerprint        string              `json:"fingerprint"`
	ProtocolID         string              `json:"protocolId"`
	Version            string              `json:"version"`
	ProfileID          string              `json:"profileId,omitempty"`
	ProfileFingerprint string              `json:"profileFingerprint,omitempty"`
	Checks             []VerificationCheck `json:"checks"`
	RawMessageIDs      []string            `json:"rawMessageIds"`
	MessageCount       int                 `json:"messageCount"`
	VerifiedAt         int64               `json:"verifiedAt,omitempty"`
	CheckedAt          int64               `json:"checkedAt"`
	Since              int64               `json:"since"`
}

type TemplateRevision struct {
	Revision                 int64               `json:"revision"`
	Fingerprint              string              `json:"fingerprint"`
	AppliedAt                int64               `json:"appliedAt"`
	Candidate                TemplateCandidate   `json:"candidate"`
	Verification             *DeviceVerification `json:"verification,omitempty"`
	AppliedTrialVerification *DeviceVerification `json:"appliedTrialVerification,omitempty"`
}

// TemplatePreparation is kept in a CAS-protected onboarding record. Its READY
// status is valid only while its applied fingerprint still matches live config.
type TemplatePreparation struct {
	Candidate                TemplateCandidate   `json:"candidate"`
	Fingerprint              string              `json:"fingerprint"`
	Status                   string              `json:"status"`
	Verification             *DeviceVerification `json:"verification,omitempty"`
	AppliedTrialVerification *DeviceVerification `json:"appliedTrialVerification,omitempty"`
	TrialProductID           string              `json:"trialProductId,omitempty"`
	TrialFingerprint         string              `json:"trialFingerprint,omitempty"`
	TrialConfigFingerprint   string              `json:"trialConfigFingerprint,omitempty"`
	History                  []TemplateRevision  `json:"history"`
	AppliedAt                int64               `json:"appliedAt"`
}
