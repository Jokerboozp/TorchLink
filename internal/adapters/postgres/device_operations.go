package postgres /* 声明 postgres 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                     /* 执行当前语句并推进处理流程。 */
	"encoding/json"               /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func (r *Repository) ListDeviceStateEvents(ctx context.Context, t, d string, limit, offset int) ([]model.DeviceStateEvent, int, error) { /* 定义 ListDeviceStateEvents 函数。 */
	var total int                                                                                                               /* 声明 total。 */
	e := r.pool.QueryRow(ctx, `SELECT count(*) FROM device_state_event WHERE tenant_id=$1 AND device_id=$2`, t, d).Scan(&total) /* 更新 e 的值。 */
	if e != nil {                                                                                                               /* 判断条件并选择处理分支。 */
		return nil, 0, e /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	rows, e := r.pool.Query(ctx, `SELECT body,(extract(epoch from created_at)*1000)::bigint FROM device_state_event WHERE tenant_id=$1 AND device_id=$2 ORDER BY id DESC LIMIT $3 OFFSET $4`, t, d, limit, offset) /* 更新 e 的值。 */
	if e != nil {                                                                                                                                                                                                  /* 判断条件并选择处理分支。 */
		return nil, 0, e /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer rows.Close()                /* 安排函数结束时执行清理。 */
	out := []model.DeviceStateEvent{} /* 更新 out 的值。 */
	for rows.Next() {                 /* 循环处理当前数据。 */
		var v model.DeviceStateEvent                    /* 声明 v。 */
		var b []byte                                    /* 声明 b。 */
		if e = rows.Scan(&b, &v.RecordedAt); e != nil { /* 判断条件并选择处理分支。 */
			return nil, 0, e /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if e = json.Unmarshal(b, &v.State); e != nil { /* 判断条件并选择处理分支。 */
			return nil, 0, e /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		out = append(out, v) /* 更新 out 的值。 */
	} /* 结束当前表达式或代码块。 */
	return out, total, rows.Err() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListDeviceMessages(ctx context.Context, t, d string, kind model.MessageType, limit, offset int) ([]model.StandardMessage, int, error) { /* 定义 ListDeviceMessages 函数。 */
	var total int                                                                                                                                                          /* 声明 total。 */
	e := r.pool.QueryRow(ctx, `SELECT count(*) FROM standard_message WHERE tenant_id=$1 AND device_id=$2 AND ($3='' OR message_type=$3)`, t, d, string(kind)).Scan(&total) /* 更新 e 的值。 */
	if e != nil {                                                                                                                                                          /* 判断条件并选择处理分支。 */
		return nil, 0, e /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	rows, e := r.pool.Query(ctx, `SELECT body FROM standard_message WHERE tenant_id=$1 AND device_id=$2 AND ($3='' OR message_type=$3) ORDER BY ts DESC,message_id DESC LIMIT $4 OFFSET $5`, t, d, string(kind), limit, offset) /* 更新 e 的值。 */
	if e != nil {                                                                                                                                                                                                               /* 判断条件并选择处理分支。 */
		return nil, 0, e /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer rows.Close()               /* 安排函数结束时执行清理。 */
	out := []model.StandardMessage{} /* 更新 out 的值。 */
	for rows.Next() {                /* 循环处理当前数据。 */
		var b []byte                     /* 声明 b。 */
		var v model.StandardMessage      /* 声明 v。 */
		if e = rows.Scan(&b); e != nil { /* 判断条件并选择处理分支。 */
			return nil, 0, e /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if e = json.Unmarshal(b, &v); e != nil { /* 判断条件并选择处理分支。 */
			return nil, 0, e /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		out = append(out, v) /* 更新 out 的值。 */
	} /* 结束当前表达式或代码块。 */
	return out, total, rows.Err() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ChangeDeviceCredential(ctx context.Context, t, id, access, hash string, now int64) (d model.ManagedDevice, v model.CredentialRevocation, e error) { /* 定义 ChangeDeviceCredential 函数。 */
	tx, e := r.pool.Begin(ctx) /* 更新 e 的值。 */
	if e != nil {              /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer tx.Rollback(ctx)                                                                                           /* 安排函数结束时执行清理。 */
	var b []byte                                                                                                     /* 声明 b。 */
	e = tx.QueryRow(ctx, `SELECT body FROM device_registry WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, t, id).Scan(&b) /* 更新 e 的值。 */
	if e != nil {                                                                                                    /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if e = json.Unmarshal(b, &d); e != nil { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	v = model.CredentialRevocation{ID: "revoke_" + d.ID + "_" + d.AccessKey, TenantID: t, DeviceID: id, Username: d.AccessKey, Status: "PENDING", CreatedAt: now, UpdatedAt: now} /* 更新 v 的值。 */
	b, e = json.Marshal(v)                                                                                                                                                        /* 更新 e 的值。 */
	if e != nil {                                                                                                                                                                 /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	_, e = tx.Exec(ctx, `INSERT INTO device_credential_revocation(tenant_id,id,device_id,status,body) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, t, v.ID, id, v.Status, b) /* 更新 e 的值。 */
	if e != nil {                                                                                                                                                               /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if access != "" { /* 判断条件并选择处理分支。 */
		d.AccessKey = access /* 更新 d.AccessKey 的值。 */
	} /* 结束当前表达式或代码块。 */
	d.SecretHash = hash    /* 更新 d.SecretHash 的值。 */
	d.SecretHint = ""      /* 更新 d.SecretHint 的值。 */
	d.UpdatedAt = now      /* 更新 d.UpdatedAt 的值。 */
	b, e = json.Marshal(d) /* 更新 e 的值。 */
	if e != nil {          /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	_, e = tx.Exec(ctx, `UPDATE device_registry SET access_key=$3,secret_hash=$4,body=$5 WHERE tenant_id=$1 AND id=$2`, t, id, d.AccessKey, hash, b) /* 更新 e 的值。 */
	if e != nil {                                                                                                                                    /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	e = tx.QueryRow(ctx, `SELECT body FROM device_credential_revocation WHERE tenant_id=$1 AND id=$2`, t, v.ID).Scan(&b) /* 更新 e 的值。 */
	if e != nil {                                                                                                        /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if e = json.Unmarshal(b, &v); e != nil { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	e = tx.Commit(ctx) /* 更新 e 的值。 */
	return             /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListCredentialRevocations(ctx context.Context, t, d string, pending bool) ([]model.CredentialRevocation, error) { /* 定义 ListCredentialRevocations 函数。 */
	rows, e := r.pool.Query(ctx, `SELECT body FROM device_credential_revocation WHERE ($1='' OR tenant_id=$1) AND ($2='' OR device_id=$2) AND (NOT $3 OR status!='REVOKED') ORDER BY tenant_id,id`, t, d, pending) /* 更新 e 的值。 */
	if e != nil {                                                                                                                                                                                                  /* 判断条件并选择处理分支。 */
		return nil, e /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer rows.Close()                    /* 安排函数结束时执行清理。 */
	out := []model.CredentialRevocation{} /* 更新 out 的值。 */
	for rows.Next() {                     /* 循环处理当前数据。 */
		var b []byte                     /* 声明 b。 */
		var v model.CredentialRevocation /* 声明 v。 */
		if e = rows.Scan(&b); e != nil { /* 判断条件并选择处理分支。 */
			return nil, e /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if e = json.Unmarshal(b, &v); e != nil { /* 判断条件并选择处理分支。 */
			return nil, e /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		out = append(out, v) /* 更新 out 的值。 */
	} /* 结束当前表达式或代码块。 */
	return out, rows.Err() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) UpdateCredentialRevocation(ctx context.Context, v model.CredentialRevocation) error { /* 定义 UpdateCredentialRevocation 函数。 */
	b, e := json.Marshal(v) /* 更新 e 的值。 */
	if e != nil {           /* 判断条件并选择处理分支。 */
		return e /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	_, e = r.pool.Exec(ctx, `UPDATE device_credential_revocation SET status=$3,body=$4 WHERE tenant_id=$1 AND id=$2 AND status!='REVOKED'`, v.TenantID, v.ID, v.Status, b) /* 更新 e 的值。 */
	return e                                                                                                                                                               /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) CreateDeviceCommand(ctx context.Context, v model.DeviceCommand) (model.DeviceCommand, bool, error) { /* 定义 CreateDeviceCommand 函数。 */
	b, e := json.Marshal(v) /* 更新 e 的值。 */
	if e != nil {           /* 判断条件并选择处理分支。 */
		return v, false, e /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	tag, e := r.pool.Exec(ctx, `INSERT INTO device_command(tenant_id,id,device_id,status,created_at,body) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, v.TenantID, v.ID, v.DeviceID, v.Status, v.CreatedAt, b) /* 更新 e 的值。 */
	if e != nil {                                                                                                                                                                                                    /* 判断条件并选择处理分支。 */
		return v, false, e /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	created := tag.RowsAffected() == 1 /* 更新 created 的值。 */
	if !created {                      /* 判断条件并选择处理分支。 */
		e = r.pool.QueryRow(ctx, `SELECT body FROM device_command WHERE tenant_id=$1 AND id=$2`, v.TenantID, v.ID).Scan(&b) /* 更新 e 的值。 */
		if e == nil {                                                                                                       /* 判断条件并选择处理分支。 */
			e = json.Unmarshal(b, &v) /* 更新 e 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return v, created, e /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) UpdateDeviceCommandDispatch(ctx context.Context, t, id, status, message string, now int64) error { /* 定义 UpdateDeviceCommandDispatch 函数。 */
	b, e := json.Marshal(map[string]any{"status": status, "lastError": message, "updatedAt": now}) /* 更新 e 的值。 */
	if e != nil {                                                                                  /* 判断条件并选择处理分支。 */
		return e /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	_, e = r.pool.Exec(ctx, `UPDATE device_command SET status=$3,body=body || $4::jsonb WHERE tenant_id=$1 AND id=$2 AND status='DISPATCHING'`, t, id, status, b) /* 更新 e 的值。 */
	return e                                                                                                                                                      /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) CompleteDeviceCommand(ctx context.Context, t, d, id string, reply map[string]any, now int64) error { /* 定义 CompleteDeviceCommand 函数。 */
	status := "FAILED"            /* 更新 status 的值。 */
	if reply["success"] == true { /* 判断条件并选择处理分支。 */
		status = "SUCCEEDED" /* 更新 status 的值。 */
	} /* 结束当前表达式或代码块。 */
	b, e := json.Marshal(map[string]any{"status": status, "reply": reply, "updatedAt": now}) /* 更新 e 的值。 */
	if e != nil {                                                                            /* 判断条件并选择处理分支。 */
		return e /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	_, e = r.pool.Exec(ctx, `UPDATE device_command SET status=$4,body=body || $5::jsonb WHERE tenant_id=$1 AND id=$2 AND device_id=$3 AND status NOT IN ('SUCCEEDED','FAILED')`, t, id, d, status, b) /* 更新 e 的值。 */
	return e                                                                                                                                                                                          /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListDeviceCommands(ctx context.Context, t, d string, limit, offset int) ([]model.DeviceCommand, int, error) { /* 定义 ListDeviceCommands 函数。 */
	var total int                                                                                                           /* 声明 total。 */
	e := r.pool.QueryRow(ctx, `SELECT count(*) FROM device_command WHERE tenant_id=$1 AND device_id=$2`, t, d).Scan(&total) /* 更新 e 的值。 */
	if e != nil {                                                                                                           /* 判断条件并选择处理分支。 */
		return nil, 0, e /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	rows, e := r.pool.Query(ctx, `SELECT body FROM device_command WHERE tenant_id=$1 AND device_id=$2 ORDER BY created_at DESC,id DESC LIMIT $3 OFFSET $4`, t, d, limit, offset) /* 更新 e 的值。 */
	if e != nil {                                                                                                                                                                /* 判断条件并选择处理分支。 */
		return nil, 0, e /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer rows.Close()             /* 安排函数结束时执行清理。 */
	out := []model.DeviceCommand{} /* 更新 out 的值。 */
	for rows.Next() {              /* 循环处理当前数据。 */
		var b []byte                     /* 声明 b。 */
		var v model.DeviceCommand        /* 声明 v。 */
		if e = rows.Scan(&b); e != nil { /* 判断条件并选择处理分支。 */
			return nil, 0, e /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if e = json.Unmarshal(b, &v); e != nil { /* 判断条件并选择处理分支。 */
			return nil, 0, e /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		out = append(out, v) /* 更新 out 的值。 */
	} /* 结束当前表达式或代码块。 */
	return out, total, rows.Err() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *Repository) GetDeviceCommand(ctx context.Context, tenant, id string) (model.DeviceCommand, error) { /* 定义 GetDeviceCommand 函数。 */
	var c model.DeviceCommand                                                                                        /* 声明 c。 */
	var b []byte                                                                                                     /* 声明 b。 */
	err := r.pool.QueryRow(ctx, `SELECT body FROM device_command WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&b) /* 更新 err 的值。 */
	if err == nil {                                                                                                  /* 判断条件并选择处理分支。 */
		err = json.Unmarshal(b, &c) /* 更新 err 的值。 */
	} /* 结束当前表达式或代码块。 */
	return c, err /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
