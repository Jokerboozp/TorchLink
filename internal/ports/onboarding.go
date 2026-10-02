package ports

import (
	"context"
	"iot-platform/internal/model"
)

type OnboardingStore interface {
	GetOnboardingRecord(context.Context, string, string) (model.OnboardingRecord, error)
	// expectedRevision zero creates; positive revisions update by compare-and-swap.
	SaveOnboardingRecord(context.Context, model.OnboardingRecord, int64) (model.OnboardingRecord, error)
	// Empty owner/kind filters are for trusted internal callers. Tenant is mandatory.
	ListOnboardingRecords(context.Context, string, string, string, int, int) ([]model.OnboardingRecord, int, error)
	// ListPendingOnboardingRecords is an internal worker queue across tenants;
	// callers must reauthorize each record's persisted owner before execution.
	ListPendingOnboardingRecords(context.Context, string, int) ([]model.OnboardingRecord, error)
}
