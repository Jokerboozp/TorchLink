package model

import "errors"

var ErrEdgeProgramConflict = errors.New("edge program target generation conflict")

type EdgeProgramStatus struct {
	Phase            string `json:"phase"`
	Version          string `json:"version"`
	CandidateVersion string `json:"candidateVersion,omitempty"`
	Generation       int64  `json:"generation"`
	SHA256           string `json:"sha256,omitempty"`
	LastError        string `json:"lastError,omitempty"`
	LastSeenAt       int64  `json:"lastSeenAt"`
}
type EdgeProgram struct {
	TenantID      string            `json:"tenantId"`
	NodeID        string            `json:"nodeId"`
	Generation    int64             `json:"generation"`
	TargetVersion string            `json:"targetVersion"`
	UpdatedAt     int64             `json:"updatedAt"`
	Status        EdgeProgramStatus `json:"status"`
}
