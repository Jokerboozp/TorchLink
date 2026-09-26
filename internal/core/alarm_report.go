package core

import (
	"context"
	"fmt"
	"iot-platform/internal/model"
)

// publishAlarmReport keeps per-report notification separate from the raised
// lifecycle event used by AI. The snapshot uses this report's details and time,
// while retaining the persisted alarm identity and accumulated trigger count.
func (e *Engine) publishAlarmReport(ctx context.Context, saved, report model.Alarm) error {
	report.ID = saved.ID
	report.FirstTriggeredAt = saved.FirstTriggeredAt
	report.TriggerCount = saved.TriggerCount
	report.Status = saved.Status
	if err := e.Bus.Publish(ctx, model.TopicAlarmReported, saved.ID, mustJSON(report)); err != nil {
		return fmt.Errorf("publish device alarm report: %w", err)
	}
	return nil
}
