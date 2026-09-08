package postgres

import (
	"context"
	"encoding/json"
	"iot-platform/internal/model"
)

func (r *Repository) SaveEdgeNode(ctx context.Context, v model.EdgeNode) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	_, e = r.pool.Exec(ctx, `INSERT INTO edge_node(tenant_id,id,body) VALUES($1,$2,$3) ON CONFLICT(tenant_id,id) DO UPDATE SET body=excluded.body`, v.TenantID, v.ID, b)
	return e
}
func (r *Repository) GetEdgeNode(ctx context.Context, t, id string) (v model.EdgeNode, e error) {
	var b []byte
	e = r.pool.QueryRow(ctx, `SELECT body FROM edge_node WHERE tenant_id=$1 AND id=$2`, t, id).Scan(&b)
	if e == nil {
		e = json.Unmarshal(b, &v)
	}
	return
}
func (r *Repository) ListEdgeNodes(ctx context.Context, t string) ([]model.EdgeNode, error) {
	rows, e := r.pool.Query(ctx, `SELECT body FROM edge_node WHERE tenant_id=$1 ORDER BY id`, t)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []model.EdgeNode{}
	for rows.Next() {
		var v model.EdgeNode
		var b []byte
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		if e = json.Unmarshal(b, &v); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *Repository) ListDeviceStateEvents(ctx context.Context, t, d string, limit, offset int) ([]model.DeviceStateEvent, int, error) {
	var total int
	e := r.pool.QueryRow(ctx, `SELECT count(*) FROM device_state_event WHERE tenant_id=$1 AND device_id=$2`, t, d).Scan(&total)
	if e != nil {
		return nil, 0, e
	}
	rows, e := r.pool.Query(ctx, `SELECT body,(extract(epoch from created_at)*1000)::bigint FROM device_state_event WHERE tenant_id=$1 AND device_id=$2 ORDER BY id DESC LIMIT $3 OFFSET $4`, t, d, limit, offset)
	if e != nil {
		return nil, 0, e
	}
	defer rows.Close()
	out := []model.DeviceStateEvent{}
	for rows.Next() {
		var v model.DeviceStateEvent
		var b []byte
		if e = rows.Scan(&b, &v.RecordedAt); e != nil {
			return nil, 0, e
		}
		if e = json.Unmarshal(b, &v.State); e != nil {
			return nil, 0, e
		}
		out = append(out, v)
	}
	return out, total, rows.Err()
}
func (r *Repository) ListDeviceMessages(ctx context.Context, t, d string, kind model.MessageType, limit, offset int) ([]model.StandardMessage, int, error) {
	var total int
	e := r.pool.QueryRow(ctx, `SELECT count(*) FROM standard_message WHERE tenant_id=$1 AND device_id=$2 AND ($3='' OR message_type=$3)`, t, d, string(kind)).Scan(&total)
	if e != nil {
		return nil, 0, e
	}
	rows, e := r.pool.Query(ctx, `SELECT body FROM standard_message WHERE tenant_id=$1 AND device_id=$2 AND ($3='' OR message_type=$3) ORDER BY ts DESC,message_id DESC LIMIT $4 OFFSET $5`, t, d, string(kind), limit, offset)
	if e != nil {
		return nil, 0, e
	}
	defer rows.Close()
	out := []model.StandardMessage{}
	for rows.Next() {
		var b []byte
		var v model.StandardMessage
		if e = rows.Scan(&b); e != nil {
			return nil, 0, e
		}
		if e = json.Unmarshal(b, &v); e != nil {
			return nil, 0, e
		}
		out = append(out, v)
	}
	return out, total, rows.Err()
}
func (r *Repository) ChangeDeviceCredential(ctx context.Context, t, id, access, hash string, now int64) (d model.ManagedDevice, v model.CredentialRevocation, e error) {
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return
	}
	defer tx.Rollback(ctx)
	var b []byte
	e = tx.QueryRow(ctx, `SELECT body FROM device_registry WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, t, id).Scan(&b)
	if e != nil {
		return
	}
	if e = json.Unmarshal(b, &d); e != nil {
		return
	}
	v = model.CredentialRevocation{ID: "revoke_" + d.ID + "_" + d.AccessKey, TenantID: t, DeviceID: id, Username: d.AccessKey, Status: "PENDING", CreatedAt: now, UpdatedAt: now}
	b, e = json.Marshal(v)
	if e != nil {
		return
	}
	_, e = tx.Exec(ctx, `INSERT INTO device_credential_revocation(tenant_id,id,device_id,status,body) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, t, v.ID, id, v.Status, b)
	if e != nil {
		return
	}
	if access != "" {
		d.AccessKey = access
	}
	d.SecretHash = hash
	d.SecretHint = ""
	d.UpdatedAt = now
	b, e = json.Marshal(d)
	if e != nil {
		return
	}
	_, e = tx.Exec(ctx, `UPDATE device_registry SET access_key=$3,secret_hash=$4,body=$5 WHERE tenant_id=$1 AND id=$2`, t, id, d.AccessKey, hash, b)
	if e != nil {
		return
	}
	e = tx.QueryRow(ctx, `SELECT body FROM device_credential_revocation WHERE tenant_id=$1 AND id=$2`, t, v.ID).Scan(&b)
	if e != nil {
		return
	}
	if e = json.Unmarshal(b, &v); e != nil {
		return
	}
	e = tx.Commit(ctx)
	return
}
func (r *Repository) ListCredentialRevocations(ctx context.Context, t, d string, pending bool) ([]model.CredentialRevocation, error) {
	rows, e := r.pool.Query(ctx, `SELECT body FROM device_credential_revocation WHERE ($1='' OR tenant_id=$1) AND ($2='' OR device_id=$2) AND (NOT $3 OR status!='REVOKED') ORDER BY tenant_id,id`, t, d, pending)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []model.CredentialRevocation{}
	for rows.Next() {
		var b []byte
		var v model.CredentialRevocation
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		if e = json.Unmarshal(b, &v); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *Repository) UpdateCredentialRevocation(ctx context.Context, v model.CredentialRevocation) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	_, e = r.pool.Exec(ctx, `UPDATE device_credential_revocation SET status=$3,body=$4 WHERE tenant_id=$1 AND id=$2 AND status!='REVOKED'`, v.TenantID, v.ID, v.Status, b)
	return e
}
func (r *Repository) CreateDeviceCommand(ctx context.Context, v model.DeviceCommand) (model.DeviceCommand, bool, error) {
	b, e := json.Marshal(v)
	if e != nil {
		return v, false, e
	}
	tag, e := r.pool.Exec(ctx, `INSERT INTO device_command(tenant_id,id,device_id,status,created_at,body) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, v.TenantID, v.ID, v.DeviceID, v.Status, v.CreatedAt, b)
	if e != nil {
		return v, false, e
	}
	created := tag.RowsAffected() == 1
	if !created {
		e = r.pool.QueryRow(ctx, `SELECT body FROM device_command WHERE tenant_id=$1 AND id=$2`, v.TenantID, v.ID).Scan(&b)
		if e == nil {
			e = json.Unmarshal(b, &v)
		}
	}
	return v, created, e
}
func (r *Repository) UpdateDeviceCommandDispatch(ctx context.Context, t, id, status, message string, now int64) error {
	b, e := json.Marshal(map[string]any{"status": status, "lastError": message, "updatedAt": now})
	if e != nil {
		return e
	}
	_, e = r.pool.Exec(ctx, `UPDATE device_command SET status=$3,body=body || $4::jsonb WHERE tenant_id=$1 AND id=$2 AND status='DISPATCHING'`, t, id, status, b)
	return e
}
func (r *Repository) CompleteDeviceCommand(ctx context.Context, t, d, id string, reply map[string]any, now int64) error {
	status := "FAILED"
	if reply["success"] == true {
		status = "SUCCEEDED"
	}
	b, e := json.Marshal(map[string]any{"status": status, "reply": reply, "updatedAt": now})
	if e != nil {
		return e
	}
	_, e = r.pool.Exec(ctx, `UPDATE device_command SET status=$4,body=body || $5::jsonb WHERE tenant_id=$1 AND id=$2 AND device_id=$3 AND status NOT IN ('SUCCEEDED','FAILED')`, t, id, d, status, b)
	return e
}
func (r *Repository) ListDeviceCommands(ctx context.Context, t, d string, limit, offset int) ([]model.DeviceCommand, int, error) {
	var total int
	e := r.pool.QueryRow(ctx, `SELECT count(*) FROM device_command WHERE tenant_id=$1 AND device_id=$2`, t, d).Scan(&total)
	if e != nil {
		return nil, 0, e
	}
	rows, e := r.pool.Query(ctx, `SELECT body FROM device_command WHERE tenant_id=$1 AND device_id=$2 ORDER BY created_at DESC,id DESC LIMIT $3 OFFSET $4`, t, d, limit, offset)
	if e != nil {
		return nil, 0, e
	}
	defer rows.Close()
	out := []model.DeviceCommand{}
	for rows.Next() {
		var b []byte
		var v model.DeviceCommand
		if e = rows.Scan(&b); e != nil {
			return nil, 0, e
		}
		if e = json.Unmarshal(b, &v); e != nil {
			return nil, 0, e
		}
		out = append(out, v)
	}
	return out, total, rows.Err()
}
