package platformapp

import (
	"context"
	"testing"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/devicescope"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// telemetryLayer stands for ClickHouse: a decorator that computes telemetry
// statistics itself and hides the base repository's other capabilities.
type telemetryLayer struct{ ports.Repository }

func (l telemetryLayer) Unwrap() ports.Repository { return l.Repository }

func (telemetryLayer) DevicePropertyStats(context.Context, string, int64, int64, []model.PropertyRange) ([]model.DevicePropertyStat, error) {
	return nil, nil
}

func (telemetryLayer) DeviceReportStats(context.Context, string, int64, int64) ([]model.DeviceReportStat, error) {
	return nil, nil
}

func TestStoresComeFromTheLayerThatProvidesThem(t *testing.T) {
	base := memory.NewRepository()
	got := storesOf(devicescope.Wrap(telemetryLayer{base}))
	if got.aiRunStore != ports.AIRunStore(base) || got.videoStore != ports.VideoStore(base) {
		t.Fatal("business stores must come from the base repository below the decorators")
	}
	if _, ok := got.telemetryStats.(telemetryLayer); !ok {
		t.Fatalf("telemetry statistics come from the outermost layer computing them, got %T", got.telemetryStats)
	}
}
