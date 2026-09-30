package ports

import (
	"context"
	"iot-platform/internal/model"
)

// Reads use one repeatable snapshot. Callbacks must not call Repository methods
// because adapters may share locks. Transactions use primary durable storage.
type AlarmGovernanceStore interface {
	GovernanceTransaction(context.Context, string, func(AlarmGovernanceTx) error) error
	GovernanceRead(context.Context, string, func(AlarmGovernanceTx) error) error
}
type AlarmGovernanceTx interface {
	Get(kind, id string) (model.GovernanceDocument, error)
	List(model.GovernanceFilter) ([]model.GovernanceDocument, int, error)
	Put(model.GovernanceDocument, int64) (model.GovernanceDocument, error)
	SourceVersions([]model.GovernanceSourceVersion) ([]model.GovernanceSourceVersion, error)
	BumpSourceVersions([]model.GovernanceSourceVersion) error
}
