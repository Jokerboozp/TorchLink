package core

import (
	"context"
	"fmt"
	"iot-platform/internal/model"
)

// IngestMQTT acknowledges the application's durable archive, independently of
// the publisher's broker PUBACK. If publishing the receipt fails, the durable
// inbox retries this operation with the same raw ID; IngestRaw is idempotent.
func (e *Engine) IngestMQTT(ctx context.Context, raw model.RawMessage) error {
	index, _, err := e.IngestRaw(ctx, raw)
	if err != nil {
		return err
	}
	clientID, _ := raw.Metadata["clientMessageId"].(string)
	if clientID == "" {
		clientID = raw.MessageID
	}
	receipt := map[string]any{"id": clientID, "rawMessageId": index.MessageID, "payloadHash": index.PayloadHash, "status": "archived", "archivedAt": index.ArchivedAt}
	if err = e.Realtime.Publish(ctx, fmt.Sprintf("/iot/down/%s/%s/%s/receipt", raw.TenantID, raw.ProductID, raw.DeviceID), mustJSON(receipt), 1, false); err != nil {
		return err
	}
	if e.Metrics != nil {
		e.Metrics.Inc("mqtt_archive_receipt_total")
	}
	return nil
}
