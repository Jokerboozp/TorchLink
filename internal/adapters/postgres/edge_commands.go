package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"iot-platform/internal/model"
	"time"
)

func (r *Repository) CreateEdgeCommand(ctx context.Context, c model.DeviceCommand) (model.DeviceCommand, bool, error) {
	if err := c.ValidEdgeQueue(); err != nil {
		return c, false, err
	}
	execution := *c.Execution
	c.Execution = &execution
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return c, false, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,795))`, c.TenantID+"/"+c.Execution.NodeID); err != nil {
		return c, false, err
	}
	var body []byte
	err = tx.QueryRow(ctx, `SELECT body FROM device_command WHERE tenant_id=$1 AND id=$2`, c.TenantID, c.ID).Scan(&body)
	if err == nil {
		var old model.DeviceCommand
		err = json.Unmarshal(body, &old)
		return old, false, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return c, false, err
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM device_command WHERE tenant_id=$1 AND body->'execution'->>'nodeId'=$2 AND status IN ('QUEUED','DISPATCHING') AND (body->'execution'->>'expiresAt')::bigint > (extract(epoch from clock_timestamp())*1000)::bigint`, c.TenantID, c.Execution.NodeID).Scan(&count); err != nil {
		return c, false, err
	}
	if count >= 8 {
		return c, false, errors.New("edge command queue full")
	}
	c.Status = "QUEUED"
	c.Execution.Token = ""
	c.Reply = nil
	c.LastError = ""
	body, err = json.Marshal(c)
	if err != nil {
		return c, false, err
	}
	tag, err := tx.Exec(ctx, `INSERT INTO device_command(tenant_id,id,device_id,status,created_at,body) VALUES($1,$2,$3,'QUEUED',$4,$5) ON CONFLICT DO NOTHING`, c.TenantID, c.ID, c.DeviceID, c.CreatedAt, body)
	if err != nil {
		return c, false, err
	}
	if tag.RowsAffected() == 0 {
		if err = tx.QueryRow(ctx, `SELECT body FROM device_command WHERE tenant_id=$1 AND id=$2`, c.TenantID, c.ID).Scan(&body); err != nil {
			return c, false, err
		}
		var old model.DeviceCommand
		err = json.Unmarshal(body, &old)
		return old, false, err
	}
	return c, true, tx.Commit(ctx)
}
func (r *Repository) GetDeviceCommand(ctx context.Context, tenant, id string) (model.DeviceCommand, error) {
	var c model.DeviceCommand
	var b []byte
	err := r.pool.QueryRow(ctx, `SELECT body FROM device_command WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&b)
	if err == nil {
		err = json.Unmarshal(b, &c)
	}
	return c, err
}
func (r *Repository) ClaimEdgeCommand(ctx context.Context, tenant, node, token string) (model.DeviceCommand, error) {
	var c model.DeviceCommand
	if token == "" {
		return c, errors.New("claim token required")
	}
	var b []byte
	err := r.pool.QueryRow(ctx, `WITH next AS (SELECT tenant_id,id FROM device_command WHERE tenant_id=$1 AND body->'execution'->>'nodeId'=$2 AND status='QUEUED' AND (body->'execution'->>'expiresAt')::bigint > (extract(epoch from clock_timestamp())*1000)::bigint ORDER BY created_at,id FOR UPDATE SKIP LOCKED LIMIT 1)
 UPDATE device_command c SET status='DISPATCHING',body=jsonb_set(c.body,'{execution,token}',to_jsonb($3::text)) || jsonb_build_object('status','DISPATCHING','updatedAt',(extract(epoch from clock_timestamp())*1000)::bigint) FROM next WHERE c.tenant_id=next.tenant_id AND c.id=next.id RETURNING c.body`, tenant, node, token).Scan(&b)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, nil
	}
	if err == nil {
		err = json.Unmarshal(b, &c)
	}
	return c, err
}
func (r *Repository) FinishEdgeCommand(ctx context.Context, c model.DeviceCommand) error {
	if c.Execution == nil || c.Execution.Token == "" || !c.EdgeResultAllowed() {
		return errors.New("invalid command result")
	}
	patch, err := json.Marshal(map[string]any{"status": c.Status, "reply": c.Reply, "lastError": c.LastError, "updatedAt": time.Now().UnixMilli()})
	if err != nil {
		return err
	}
	tag, err := r.pool.Exec(ctx, `UPDATE device_command SET status=CASE WHEN status='DISPATCHING' THEN $5 ELSE status END,body=CASE WHEN status='DISPATCHING' THEN body || $6::jsonb ELSE body END WHERE tenant_id=$1 AND id=$2 AND body->'execution'->>'nodeId'=$3 AND body->'execution'->>'token'=$4 AND created_at > (extract(epoch from clock_timestamp())*1000)::bigint-86400000`, c.TenantID, c.ID, c.Execution.NodeID, c.Execution.Token, c.Status, patch)
	if err == nil && tag.RowsAffected() != 1 {
		return errors.New("unowned or expired edge command result")
	}
	return err
}
