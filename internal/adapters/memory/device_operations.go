package memory /* 声明 memory 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                     /* 执行当前语句并推进处理流程。 */
	"errors"                      /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"sort"                        /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func (r *Repository) ListDeviceStateEvents(_ context.Context, t, d string, limit, offset int) ([]model.DeviceStateEvent, int, error) { /* 定义 ListDeviceStateEvents 函数。 */
	r.mu.RLock()                                   /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()                           /* 安排函数结束时执行清理。 */
	out := []model.DeviceStateEvent{}              /* 更新 out 的值。 */
	for i := len(r.stateEvents) - 1; i >= 0; i-- { /* 循环处理当前数据。 */
		v := r.stateEvents[i]                               /* 更新 v 的值。 */
		if v.State.TenantID == t && v.State.DeviceID == d { /* 判断条件并选择处理分支。 */
			out = append(out, clone(v)) /* 更新 out 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return page(out, offset, limit), len(out), nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListDeviceMessages(_ context.Context, t, d string, kind model.MessageType, limit, offset int) ([]model.StandardMessage, int, error) { /* 定义 ListDeviceMessages 函数。 */
	r.mu.RLock()                     /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()             /* 安排函数结束时执行清理。 */
	out := []model.StandardMessage{} /* 更新 out 的值。 */
	for _, v := range r.standard {   /* 循环处理当前数据。 */
		if v.TenantID == t && v.DeviceID == d && (kind == "" || v.MessageType == kind) { /* 判断条件并选择处理分支。 */
			out = append(out, clone(v)) /* 更新 out 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	sort.Slice(out, func(i, j int) bool { /* 执行当前语句并推进处理流程。 */
		if out[i].Timestamp == out[j].Timestamp { /* 判断条件并选择处理分支。 */
			return out[i].MessageID > out[j].MessageID /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return out[i].Timestamp > out[j].Timestamp /* 返回当前处理结果。 */
	}) /* 结束当前表达式或代码块。 */
	return page(out, offset, limit), len(out), nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// The credential change and its broker revocation intent share the same lock.
func (r *Repository) ChangeDeviceCredential(_ context.Context, t, id, access, hash string, now int64) (model.ManagedDevice, model.CredentialRevocation, error) { /* 定义 ChangeDeviceCredential 函数。 */
	r.mu.Lock()                    /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()            /* 安排函数结束时执行清理。 */
	d, ok := r.devices[key(t, id)] /* 更新 ok 的值。 */
	if !ok {                       /* 判断条件并选择处理分支。 */
		return d, model.CredentialRevocation{}, ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	v := model.CredentialRevocation{ID: "revoke_" + d.ID + "_" + d.AccessKey, TenantID: t, DeviceID: id, Username: d.AccessKey, Status: "PENDING", CreatedAt: now, UpdatedAt: now} /* 更新 v 的值。 */
	if access != "" {                                                                                                                                                              /* 判断条件并选择处理分支。 */
		for _, other := range r.devices { /* 循环处理当前数据。 */
			if other.AccessKey == access { /* 判断条件并选择处理分支。 */
				return d, v, errors.New("duplicate access key") /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		d.AccessKey = access /* 更新 d.AccessKey 的值。 */
	} /* 结束当前表达式或代码块。 */
	d.SecretHash = hash       /* 更新 d.SecretHash 的值。 */
	d.SecretHint = ""         /* 更新 d.SecretHint 的值。 */
	d.UpdatedAt = now         /* 更新 d.UpdatedAt 的值。 */
	if r.revocations == nil { /* 判断条件并选择处理分支。 */
		r.revocations = map[string]model.CredentialRevocation{} /* 更新 r.revocations 的值。 */
	} /* 结束当前表达式或代码块。 */
	if old, exists := r.revocations[key(t, v.ID)]; exists { /* 判断条件并选择处理分支。 */
		v = old /* 更新 v 的值。 */
	} else { /* 结束当前表达式或代码块。 */
		r.revocations[key(t, v.ID)] = v /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	r.devices[key(t, id)] = cloneManaged(d) /* 执行当前语句并推进处理流程。 */
	return cloneManaged(d), v, nil          /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListCredentialRevocations(_ context.Context, t, d string, pending bool) ([]model.CredentialRevocation, error) { /* 定义 ListCredentialRevocations 函数。 */
	r.mu.RLock()                          /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()                  /* 安排函数结束时执行清理。 */
	out := []model.CredentialRevocation{} /* 更新 out 的值。 */
	for _, v := range r.revocations {     /* 循环处理当前数据。 */
		if (t == "" || v.TenantID == t) && (d == "" || v.DeviceID == d) && (!pending || v.Status != "REVOKED") { /* 判断条件并选择处理分支。 */
			out = append(out, clone(v)) /* 更新 out 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt }) /* 执行当前语句并推进处理流程。 */
	return out, nil                                                                     /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) UpdateCredentialRevocation(_ context.Context, v model.CredentialRevocation) error { /* 定义 UpdateCredentialRevocation 函数。 */
	r.mu.Lock()                 /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()         /* 安排函数结束时执行清理。 */
	k := key(v.TenantID, v.ID)  /* 更新 k 的值。 */
	old, ok := r.revocations[k] /* 更新 ok 的值。 */
	if !ok {                    /* 判断条件并选择处理分支。 */
		return ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if old.Status == "REVOKED" { /* 判断条件并选择处理分支。 */
		return nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	r.revocations[k] = clone(v) /* 更新 r.revocations[k] 的值。 */
	return nil                  /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) CreateDeviceCommand(_ context.Context, v model.DeviceCommand) (model.DeviceCommand, bool, error) { /* 定义 CreateDeviceCommand 函数。 */
	r.mu.Lock()            /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()    /* 安排函数结束时执行清理。 */
	if r.commands == nil { /* 判断条件并选择处理分支。 */
		r.commands = map[string]model.DeviceCommand{} /* 更新 r.commands 的值。 */
	} /* 结束当前表达式或代码块。 */
	k := key(v.TenantID, v.ID)        /* 更新 k 的值。 */
	if old, ok := r.commands[k]; ok { /* 判断条件并选择处理分支。 */
		return clone(old), false, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	r.commands[k] = clone(v)   /* 更新 r.commands[k] 的值。 */
	return clone(v), true, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) UpdateDeviceCommandDispatch(_ context.Context, t, id, status, message string, now int64) error { /* 定义 UpdateDeviceCommandDispatch 函数。 */
	r.mu.Lock()            /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()    /* 安排函数结束时执行清理。 */
	k := key(t, id)        /* 更新 k 的值。 */
	v, ok := r.commands[k] /* 更新 ok 的值。 */
	if !ok {               /* 判断条件并选择处理分支。 */
		return ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if v.Status == "DISPATCHING" { /* 判断条件并选择处理分支。 */
		v.Status = status     /* 更新 v.Status 的值。 */
		v.LastError = message /* 更新 v.LastError 的值。 */
		v.UpdatedAt = now     /* 更新 v.UpdatedAt 的值。 */
		r.commands[k] = v     /* 更新 r.commands[k] 的值。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) CompleteDeviceCommand(_ context.Context, t, d, id string, reply map[string]any, now int64) error { /* 定义 CompleteDeviceCommand 函数。 */
	r.mu.Lock()                 /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()         /* 安排函数结束时执行清理。 */
	k := key(t, id)             /* 更新 k 的值。 */
	v, ok := r.commands[k]      /* 更新 ok 的值。 */
	if !ok || v.DeviceID != d { /* 判断条件并选择处理分支。 */
		return nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if v.Status == "SUCCEEDED" || v.Status == "FAILED" { /* 判断条件并选择处理分支。 */
		return nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	v.Status = "FAILED"           /* 更新 v.Status 的值。 */
	if reply["success"] == true { /* 判断条件并选择处理分支。 */
		v.Status = "SUCCEEDED" /* 更新 v.Status 的值。 */
	} /* 结束当前表达式或代码块。 */
	v.Reply = clone(reply) /* 更新 v.Reply 的值。 */
	v.UpdatedAt = now      /* 更新 v.UpdatedAt 的值。 */
	r.commands[k] = v      /* 更新 r.commands[k] 的值。 */
	return nil             /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListDeviceCommands(_ context.Context, t, d string, limit, offset int) ([]model.DeviceCommand, int, error) { /* 定义 ListDeviceCommands 函数。 */
	r.mu.RLock()                   /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()           /* 安排函数结束时执行清理。 */
	out := []model.DeviceCommand{} /* 更新 out 的值。 */
	for _, v := range r.commands { /* 循环处理当前数据。 */
		if v.TenantID == t && v.DeviceID == d { /* 判断条件并选择处理分支。 */
			out = append(out, clone(v)) /* 更新 out 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	sort.Slice(out, func(i, j int) bool { /* 执行当前语句并推进处理流程。 */
		if out[i].CreatedAt == out[j].CreatedAt { /* 判断条件并选择处理分支。 */
			return out[i].ID > out[j].ID /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return out[i].CreatedAt > out[j].CreatedAt /* 返回当前处理结果。 */
	}) /* 结束当前表达式或代码块。 */
	return page(out, offset, limit), len(out), nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *Repository) GetDeviceCommand(_ context.Context, tenant, id string) (model.DeviceCommand, error) { /* 定义 GetDeviceCommand 函数。 */
	r.mu.RLock()                         /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()                 /* 安排函数结束时执行清理。 */
	c, ok := r.commands[key(tenant, id)] /* 更新 ok 的值。 */
	if !ok {                             /* 判断条件并选择处理分支。 */
		return c, ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return clone(c), nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
