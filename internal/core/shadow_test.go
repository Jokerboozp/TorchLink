package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
	"log/slog"
	"testing"
	"time"
)

func TestShadowProjectionLimitDoesNotBlockMessageProcessing(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	properties := map[string]any{}
	for i := 0; i < 257; i++ {
		properties[fmt.Sprint(i)] = float64(i)
	}
	properties["temperature"] = float64(90)
	if err := repo.SaveRule(ctx, model.AlarmRule{ID: "rule", TenantID: "tenant", Name: "temperature", AlarmType: "FIRE", Level: "HIGH", Enabled: true, Conditions: []model.RuleCondition{{Field: "temperature", Operator: ">", Value: 80}}}); err != nil {
		t.Fatal(err)
	}
	msg := model.StandardMessage{TenantID: "tenant", DeviceID: "device", ProductID: "product", MessageID: "oversized", MessageType: model.PropertyReport, Properties: properties, Timestamp: time.Now().UnixMilli()}
	data, _ := json.Marshal(msg)
	if err := e.handleStandard(ctx, data); err != nil {
		t.Fatal("projection blocked normal processing", err)
	}
	shouldProcess, _, err := repo.ClaimStandardMessage(ctx, msg)
	if err != nil || shouldProcess {
		t.Fatal("message not completed", err)
	}
	s, err := repo.GetDeviceShadow(ctx, "tenant", "device")
	if err != nil || s.LastError == "" || s.ErrorMessageID != "oversized" || len(s.Reported) != 0 {
		t.Fatal("projection error not visible", s, err)
	}
	alarms, err := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "tenant", DeviceID: "device", Limit: 10})
	if err != nil || len(alarms) != 1 {
		t.Fatal("projection suppressed rule alarm", alarms, err)
	}
}
