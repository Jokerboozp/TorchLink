package model

import (
	"encoding/json"
	"errors"
)

var ErrOnboardingChanged = errors.New("接入记录已发生变化，请刷新后重试")

// OnboardingRecord is a versioned, tenant-owned control-plane record. Body must
// never contain plaintext device credentials. Kind and OwnerID are immutable.
type OnboardingRecord struct {
	TenantID  string          `json:"tenantId"`
	ID        string          `json:"id"`
	OwnerID   string          `json:"ownerId"`
	Kind      string          `json:"kind"`
	Status    string          `json:"status"`
	Revision  int64           `json:"revision"`
	CreatedAt int64           `json:"createdAt"`
	UpdatedAt int64           `json:"updatedAt"`
	Body      json.RawMessage `json:"body"`
}
