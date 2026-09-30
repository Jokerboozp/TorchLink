package ports

import (
	"context"
	"iot-platform/internal/model"
)

// AnalyticsFactsRead freezes the PostgreSQL sources in one repeatable-read
// transaction. All pages for a run must be consumed inside the callback. A
// ClickHouse decorator records a separate cutoff; it is not a cross-DB snapshot.
type AnalyticsFactStore interface {
	AnalyticsFactsRead(context.Context, string, func(AnalyticsFactReader) error) error
}
type AnalyticsFactReader interface {
	QueryMeasurementSeries(model.FactQuery) (model.FactPage[model.MeasurementFact], error)
	ListRawParseOutcomes(model.FactQuery) (model.FactPage[model.RawParseOutcomeFact], error)
	ListAlarmReportEvents(model.FactQuery) (model.FactPage[model.BusinessEventFact], error)
	ListAlarmLifecycleEvents(model.FactQuery) (model.FactPage[model.BusinessEventFact], error)
	ListDeviceStateIntervals(model.FactQuery) (model.FactPage[model.DeviceStateIntervalFact], error)
	ListHandlingRecords(model.FactQuery) (model.FactPage[model.HandlingRecordFact], error)
	GetDependencySnapshot(model.FactQuery) (model.FactPage[model.DependencyFact], error)
	GetConfigurationHistory(model.FactQuery) (model.FactPage[model.ConfigurationFact], error)
}

// MeasurementAvailabilityRecorder records a storage acknowledgement after the
// underlying write succeeded. The immutable time is conservative: it never
// claims data was readable before acknowledgement. Lost receipts stay unknown.
type MeasurementAvailabilityRecorder interface {
	RecordMeasurementAvailability(context.Context, string, string, string) error
}
