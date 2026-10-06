package model

import (
	"crypto/rand"
	"encoding/hex"
)

// NewAuditID returns a new audit record ID: "audit_" and 20 random hex digits.
// Engine.RecordAudit assigns one to records saved without an ID.
func NewAuditID() string {
	b := make([]byte, 10)
	_, _ = rand.Read(b)
	return "audit_" + hex.EncodeToString(b)
}
