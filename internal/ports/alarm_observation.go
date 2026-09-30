package ports

import (
	"context"
	"iot-platform/internal/model"
)

type AlarmObservationFilter struct {
	TimeBasis                                                        string
	DeviceIDs                                                        []string
	DeviceID, ComponentID, AlarmType, OriginKind, SignalKey, AlarmID string
	Start, End                                                       int64
	Cursor                                                           string
	Limit                                                            int
}

// Reads always require an explicit device scope; an empty set grants no access.
type AlarmObservationStore interface {
	GetAlarmObservation(context.Context, string, string) (model.AlarmObservation, error)
	ListAlarmObservations(context.Context, string, AlarmObservationFilter) ([]model.AlarmObservation, error)
	GetAlarmSignalSeed(context.Context, string, AlarmObservationFilter, int64) (model.AlarmObservation, error)
	SaveAlarmObservation(context.Context, model.AlarmObservation) (model.AlarmObservation, bool, error)
}

// Recovery commits the incoming explicit normal seed and production transition
// in one transaction, including when no active platform alarm exists.
type AlarmObservationRecoveryStore interface {
	RecoverAlarmSignal(context.Context, model.AlarmObservation, string) ([]model.Alarm, error)
}

// Reader participates in the same snapshot as AlarmGovernanceTx. Never call
// outer repository methods from a governance transaction callback.
type AlarmObservationReader interface {
	GetAlarmObservation(string) (model.AlarmObservation, error)
	ListAlarmObservations(AlarmObservationFilter) ([]model.AlarmObservation, error)
	GetAlarmSignalSeed(AlarmObservationFilter, int64) (model.AlarmObservation, error)
}
