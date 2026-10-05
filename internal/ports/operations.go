package ports

import (
	"context"
	"time"

	"iot-platform/internal/model"
)

// OperationsStore keeps replays, audit logs, execution leases and dashboard
// counts.
type OperationsStore interface {
	DashboardCounts(context.Context, string, int64, int64) ([]model.DashboardCount, error)
	DashboardCountsForDevices(context.Context, string, int64, int64, []string) ([]model.DashboardCount, error)
	AcquireExecutionLease(context.Context, string, string, string, string, time.Duration) (model.ExecutionLease, bool, error)
	GetExecutionLease(context.Context, string, string) (model.ExecutionLease, error)
	ReleaseExecutionLease(context.Context, model.ExecutionLease) error
	SaveReplay(context.Context, model.ReplayRequest) error
	UpdateReplay(context.Context, model.ReplayRequest) error
	GetReplay(context.Context, string) (model.ReplayRequest, error)
	SaveAudit(context.Context, model.AuditLog) error
}
