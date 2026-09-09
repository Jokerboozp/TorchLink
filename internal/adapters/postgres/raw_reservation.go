package postgres

import (
	"context"
	"encoding/json"
	"iot-platform/internal/model"
)

// The reservation contains only immutable routing/parser metadata, not a second
// payload copy. It prevents conflicting writers before either tier archives data.
func (r *Repository) ReserveRawMessage(ctx context.Context, v model.RawMessage) (model.RawMessage, error) {
	metadata := v
	metadata.Payload = nil
	body, err := json.Marshal(metadata)
	if err != nil {
		return v, err
	}
	tag, err := r.pool.Exec(ctx, `INSERT INTO raw_ingest_reservation(tenant_id,message_id,payload_hash,metadata) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, v.TenantID, v.MessageID, v.PayloadHash(), body)
	if err != nil {
		return v, err
	}
	if tag.RowsAffected() == 1 {
		return v, nil
	}
	var hash string
	if err = r.pool.QueryRow(ctx, `SELECT payload_hash,metadata FROM raw_ingest_reservation WHERE tenant_id=$1 AND message_id=$2`, v.TenantID, v.MessageID).Scan(&hash, &body); err != nil {
		return v, err
	}
	if err = json.Unmarshal(body, &metadata); err != nil {
		return v, err
	}
	if hash != v.PayloadHash() || metadata.ProductID != v.ProductID || metadata.DeviceID != v.DeviceID {
		return v, model.ErrRawConflict
	}
	metadata.Payload = v.Payload
	return metadata, nil
}
