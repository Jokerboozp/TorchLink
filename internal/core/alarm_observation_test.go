package core

import (
	"context"
	"io"
	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
	"log/slog"
	"testing"
)

type observationWrappedRepository struct {
	ports.Repository
	receipts int
}

func (r *observationWrappedRepository) GetRawIndex(ctx context.Context, tenant, id string) (model.RawArchiveIndex, error) {
	r.receipts++
	return r.Repository.GetRawIndex(ctx, tenant, id)
}
func TestAlarmObservationRecoverySurvivesRepositoryDecorators(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	wrapped := &observationWrappedRepository{Repository: repo}
	archive, _ := local.NewArchive(t.TempDir())
	e := New(wrapped, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	normal := model.StandardMessage{TenantID: "t", ProductID: "p", DeviceID: "d", MessageID: "normal", MessageType: model.StateChange, Timestamp: 100, Properties: map[string]any{"fireAlarm": false}}
	if err := e.handleStandard(ctx, mustJSON(normal)); err != nil {
		t.Fatal("normal seed failed through decorator", err)
	}
	facts, err := repo.ListAlarmObservations(ctx, "t", ports.AlarmObservationFilter{DeviceIDs: []string{"d"}})
	if err != nil || len(facts) != 1 || facts[0].FactKind != "CLEAR" {
		t.Fatal(facts, err)
	}
	alarm := normal
	alarm.MessageID = "alarm"
	alarm.Timestamp = 200
	alarm.MessageType = model.AlarmReport
	alarm.Properties = map[string]any{"fireAlarm": true}
	if err = e.handleStandard(ctx, mustJSON(alarm)); err != nil {
		t.Fatal(err)
	}
	normal.MessageID = "clear"
	normal.Timestamp = 300
	if err = e.handleStandard(ctx, mustJSON(normal)); err != nil {
		t.Fatal("recovery failed through decorator", err)
	}
	alarms, _ := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "t", DeviceID: "d"})
	if len(alarms) != 1 || alarms[0].Status != "RECOVERED" {
		t.Fatal(alarms)
	}
}
func TestAlarmObservationReusesOneRawReceiptForComponentSlots(t *testing.T) {
	repo := memory.NewRepository()
	wrapped := &observationWrappedRepository{Repository: repo}
	archive, _ := local.NewArchive(t.TempDir())
	e := New(wrapped, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	msg := model.StandardMessage{TenantID: "t", ProductID: "p", DeviceID: "d", MessageID: "components", RawMessageID: "raw", Timestamp: 100, MessageType: model.StateChange, Event: map[string]any{"components": []model.ComponentStatus{{ID: "one", Alarms: map[string]bool{"FIRE": true, "FAULT": false}}, {ID: "two", Alarms: map[string]bool{"FIRE": false, "FAULT": true}}}}}
	if err := e.handleStandard(context.Background(), mustJSON(msg)); err != nil {
		t.Fatal(err)
	}
	if wrapped.receipts != 1 {
		t.Fatalf("four component slots read raw receipt %d times", wrapped.receipts)
	}
}
