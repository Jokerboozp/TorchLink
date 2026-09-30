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

// Authorization shares the governing transaction; writes lock the access row
// so revocation and a formal confirmation cannot pass each other unnoticed.
type GovernanceAuthorizationReader interface {
	GovernanceAccessState() (model.AccessState, error)
}

// Historical input is immutable successful parsing, read in the governing
// snapshot. It cannot prove production acceptance or reconstruct rule decisions.
type GovernanceHistoricalReader interface {
	ListGovernanceHistoricalMessages(AlarmObservationFilter) ([]model.StandardMessage, error)
	GetGovernanceHistoricalMessage(id string) (model.StandardMessage, error)
}

// Archive history has a separate cutoff from the PostgreSQL snapshot. It may
// preserve only telemetry, so coverage and original acceptance remain explicit.
type GovernanceHistoricalArchiveReader interface {
	ListGovernanceHistoricalArchiveMessages(context.Context, string, AlarmObservationFilter) (model.FactPage[model.StandardMessage], error)
}

// Only management maintenance uses this enumeration; it grants no user scope.
type GovernanceTenantLister interface {
	GovernanceTenants(context.Context) ([]string, error)
}
