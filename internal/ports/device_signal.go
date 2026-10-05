package ports

import (
	"context"

	"iot-platform/internal/model"
)

// DeviceSignalStore keeps the latest health signals of each tenant's devices.
type DeviceSignalStore interface {
	// ReplaceDeviceSignals atomically replaces all signals of tenant.
	ReplaceDeviceSignals(ctx context.Context, tenant string, signals []model.DeviceSignal) error
	// ListDeviceSignals returns signals, strongest first; nil deviceIDs lists
	// the whole tenant.
	ListDeviceSignals(ctx context.Context, tenant string, deviceIDs []string, limit int) ([]model.DeviceSignal, error)
	// SignalTenants lists the tenants that have device states.
	SignalTenants(ctx context.Context) ([]string, error)
}

// DeviceTelemetryStats aggregates reported numeric properties and report
// counts per device over [start, end] in the store, so health signals do not
// read individual messages. With ClickHouse, property statistics come from
// its telemetry table and report counts from PostgreSQL.
type DeviceTelemetryStats interface {
	DevicePropertyStats(ctx context.Context, tenant string, start, end int64, ranges []model.PropertyRange) ([]model.DevicePropertyStat, error)
	DeviceReportStats(ctx context.Context, tenant string, start, end int64) ([]model.DeviceReportStat, error)
}
