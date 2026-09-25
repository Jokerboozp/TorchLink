package onboarding /* 声明 onboarding 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context" /* 执行当前语句并推进处理流程。 */

	"encoding/json"               /* 执行当前语句并推进处理流程。 */
	"errors"                      /* 执行当前语句并推进处理流程。 */
	"fmt"                         /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"reflect"                     /* 执行当前语句并推进处理流程。 */
	"time"                        /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

// ValidateThingModel keeps the extension small and independent of codecs.
func ValidateThingModel(m *model.ThingModel) error { /* 定义 ValidateThingModel 函数。 */
	if m == nil { /* 判断条件并选择处理分支。 */
		return nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	fields := func(fs []model.ThingField) error { /* 更新 fields 的值。 */
		if len(fs) > 256 { /* 判断条件并选择处理分支。 */
			return errors.New("too many fields") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		seen := map[string]bool{} /* 更新 seen 的值。 */
		for _, f := range fs {    /* 循环处理当前数据。 */
			if !segment.MatchString(f.Identifier) || seen[f.Identifier] { /* 判断条件并选择处理分支。 */
				return errors.New("invalid or duplicate field identifier") /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			seen[f.Identifier] = true /* 更新 seen[f.Identifier] 的值。 */
			switch f.DataType {       /* 根据条件选择处理路径。 */
			case "string", "number", "integer", "boolean", "object", "array": /* 处理当前分支。 */
			default: /* 处理当前分支。 */
				return fmt.Errorf("unsupported dataType for %s", f.Identifier) /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		return nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if e := fields(m.Properties); e != nil { /* 判断条件并选择处理分支。 */
		return e /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for _, ops := range [][]model.ThingOperation{m.Events, m.Commands} { /* 循环处理当前数据。 */
		if len(ops) > 128 { /* 判断条件并选择处理分支。 */
			return errors.New("too many operations") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		seen := map[string]bool{} /* 更新 seen 的值。 */
		for _, op := range ops {  /* 循环处理当前数据。 */
			if !segment.MatchString(op.Identifier) || seen[op.Identifier] { /* 判断条件并选择处理分支。 */
				return errors.New("invalid or duplicate operation identifier") /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			seen[op.Identifier] = true            /* 更新 seen[op.Identifier] 的值。 */
			if e := fields(op.Fields); e != nil { /* 判断条件并选择处理分支。 */
				return e /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

var ErrCredentialUnsupported = errors.New("该设备通过协议连接或主设备接入，无需平台凭据") /* 声明 ErrCredentialUnsupported。 */

func (s *Service) ChangeCredential(ctx context.Context, t, id string, rotate bool) (model.DeviceCredential, model.CredentialRevocation, error) { /* 定义 ChangeCredential 函数。 */
	d, err := s.Repo.GetManagedDevice(ctx, t, id) /* 更新 err 的值。 */
	if err != nil {                               /* 判断条件并选择处理分支。 */
		return model.DeviceCredential{}, model.CredentialRevocation{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	p, err := s.Repo.GetProduct(ctx, t, d.ProductID) /* 更新 err 的值。 */
	if err != nil {                                  /* 判断条件并选择处理分支。 */
		return model.DeviceCredential{}, model.CredentialRevocation{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !d.UsesPlatformCredentials(p) { /* 判断条件并选择处理分支。 */
		return model.DeviceCredential{}, model.CredentialRevocation{}, ErrCredentialUnsupported /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	c := model.DeviceCredential{} /* 更新 c 的值。 */
	hash := ""                    /* 更新 hash 的值。 */
	if rotate {                   /* 判断条件并选择处理分支。 */
		var e error         /* 声明 e。 */
		c, e = Credential() /* 更新 e 的值。 */
		if e != nil {       /* 判断条件并选择处理分支。 */
			return c, model.CredentialRevocation{}, e /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		hash = Hash(c.Secret) /* 更新 hash 的值。 */
	} /* 结束当前表达式或代码块。 */
	_, v, e := s.Repo.ChangeDeviceCredential(ctx, t, id, c.AccessKey, hash, time.Now().UnixMilli()) /* 更新 e 的值。 */
	if e != nil {                                                                                   /* 判断条件并选择处理分支。 */
		return model.DeviceCredential{}, v, e /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	v = s.revoke(ctx, v) /* 更新 v 的值。 */
	return c, v, nil     /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (s *Service) revoke(ctx context.Context, v model.CredentialRevocation) model.CredentialRevocation { /* 定义 revoke 函数。 */
	if v.Status == "REVOKED" { /* 判断条件并选择处理分支。 */
		return v /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	v.LastError = "broker administration is not configured" /* 更新 v.LastError 的值。 */
	if s.RevokeUsername != nil {                            /* 判断条件并选择处理分支。 */
		limited, cancel := context.WithTimeout(ctx, 10*time.Second) /* 更新 cancel 的值。 */
		e := s.RevokeUsername(limited, v.Username)                  /* 更新 e 的值。 */
		cancel()                                                    /* 执行当前语句并推进处理流程。 */
		if e == nil {                                               /* 判断条件并选择处理分支。 */
			v.Status = "REVOKED" /* 更新 v.Status 的值。 */
			v.LastError = ""     /* 更新 v.LastError 的值。 */
		} else { /* 结束当前表达式或代码块。 */
			v.LastError = "broker revocation failed; retry pending" /* 更新 v.LastError 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	v.UpdatedAt = time.Now().UnixMilli()                          /* 更新 v.UpdatedAt 的值。 */
	if e := s.Repo.UpdateCredentialRevocation(ctx, v); e != nil { /* 判断条件并选择处理分支。 */
		v.Status = "PENDING"                                     /* 更新 v.Status 的值。 */
		v.LastError = "revocation status could not be persisted" /* 更新 v.LastError 的值。 */
	} /* 结束当前表达式或代码块。 */
	return v /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (s *Service) RetryRevocations(ctx context.Context) { /* 定义 RetryRevocations 函数。 */
	tick := time.NewTicker(30 * time.Second) /* 更新 tick 的值。 */
	defer tick.Stop()                        /* 安排函数结束时执行清理。 */
	for {                                    /* 循环处理当前数据。 */
		if s.RevokeUsername != nil { /* 判断条件并选择处理分支。 */
			items, e := s.Repo.ListCredentialRevocations(ctx, "", "", true) /* 更新 e 的值。 */
			if e == nil {                                                   /* 判断条件并选择处理分支。 */
				for _, v := range items { /* 循环处理当前数据。 */
					if ctx.Err() != nil { /* 判断条件并选择处理分支。 */
						return /* 返回当前处理结果。 */
					} /* 结束当前表达式或代码块。 */
					s.revoke(ctx, v) /* 执行当前语句并推进处理流程。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		select { /* 根据条件选择处理路径。 */
		case <-ctx.Done(): /* 处理当前分支。 */
			return /* 返回当前处理结果。 */
		case <-tick.C: /* 处理当前分支。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (s *Service) SendCommand(ctx context.Context, t, d string, q model.DeviceCommand) (model.DeviceCommand, error) { /* 定义 SendCommand 函数。 */
	if !q.Confirmed { /* 判断条件并选择处理分支。 */
		return q, errors.New("manual command confirmation is required") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !segment.MatchString(q.ID) || !segment.MatchString(q.Type) || q.Data == nil { /* 判断条件并选择处理分支。 */
		return q, errors.New("valid command id, type and data object are required") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	device, e := s.Repo.GetManagedDevice(ctx, t, d)                                                      /* 更新 e 的值。 */
	if e != nil || device.Status != "ENABLED" || device.SecretHash == "" || device.Connector != "MQTT" { /* 判断条件并选择处理分支。 */
		return q, errors.New("enabled standard MQTT device required") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	p, e := s.Repo.GetProduct(ctx, t, device.ProductID) /* 更新 e 的值。 */
	if e != nil || p.Status != "ENABLED" {              /* 判断条件并选择处理分支。 */
		return q, errors.New("product disabled") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if p.ThingModel != nil && len(p.ThingModel.Commands) > 0 { /* 判断条件并选择处理分支。 */
		var op *model.ThingOperation           /* 声明 op。 */
		for i := range p.ThingModel.Commands { /* 循环处理当前数据。 */
			if p.ThingModel.Commands[i].Identifier == q.Type { /* 判断条件并选择处理分支。 */
				op = &p.ThingModel.Commands[i] /* 更新 op 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		if op == nil { /* 判断条件并选择处理分支。 */
			return q, errors.New("command is not defined in product thing model") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		for _, f := range op.Fields { /* 循环处理当前数据。 */
			v, ok := q.Data[f.Identifier] /* 更新 ok 的值。 */
			if !ok {                      /* 判断条件并选择处理分支。 */
				if f.Required { /* 判断条件并选择处理分支。 */
					return q, fmt.Errorf("%s is required", f.Identifier) /* 返回当前处理结果。 */
				} /* 结束当前表达式或代码块。 */
				continue /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			valid := false      /* 更新 valid 的值。 */
			switch f.DataType { /* 根据条件选择处理路径。 */
			case "string": /* 处理当前分支。 */
				_, valid = v.(string) /* 更新 valid 的值。 */
			case "boolean": /* 处理当前分支。 */
				_, valid = v.(bool) /* 更新 valid 的值。 */
			case "number": /* 处理当前分支。 */
				_, valid = v.(float64) /* 更新 valid 的值。 */
			case "integer": /* 处理当前分支。 */
				n, ok := v.(float64)                 /* 更新 ok 的值。 */
				valid = ok && n == float64(int64(n)) /* 更新 valid 的值。 */
			case "object": /* 处理当前分支。 */
				_, valid = v.(map[string]any) /* 更新 valid 的值。 */
			case "array": /* 处理当前分支。 */
				_, valid = v.([]any) /* 更新 valid 的值。 */
			} /* 结束当前表达式或代码块。 */
			if !valid { /* 判断条件并选择处理分支。 */
				return q, fmt.Errorf("invalid %s", f.Identifier) /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if s.PublishCommand == nil { /* 判断条件并选择处理分支。 */
		return q, errors.New("MQTT publisher is unavailable") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	now := time.Now().UnixMilli()                                                                                                                                               /* 更新 now 的值。 */
	q = model.DeviceCommand{ID: q.ID, TenantID: t, DeviceID: d, ProductID: device.ProductID, Type: q.Type, Data: q.Data, Status: "DISPATCHING", CreatedAt: now, UpdatedAt: now} /* 更新 q 的值。 */
	payload, e := json.Marshal(map[string]any{"id": q.ID, "version": "1.0", "timestamp": now, "command": q.Type, "params": q.Data, "type": q.Type, "data": q.Data})             /* 更新 e 的值。 */
	if e != nil || len(payload) > 64<<10 {                                                                                                                                      /* 判断条件并选择处理分支。 */
		return q, errors.New("command exceeds 64 KiB or contains invalid JSON") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	saved, created, e := s.Repo.CreateDeviceCommand(ctx, q) /* 更新 e 的值。 */
	if e != nil {                                           /* 判断条件并选择处理分支。 */
		return q, e /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !created { /* 判断条件并选择处理分支。 */
		if saved.Status == "QUEUED" || saved.DeviceID != d || saved.Type != q.Type || !reflect.DeepEqual(saved.Data, q.Data) { /* 判断条件并选择处理分支。 */
			return model.DeviceCommand{}, errors.New("command id is already used by a different request") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return saved.ObservedOutcome(time.Now().UnixMilli()), nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	limited, cancel := context.WithTimeout(ctx, 5*time.Second)                                                          /* 更新 cancel 的值。 */
	defer cancel()                                                                                                      /* 安排函数结束时执行清理。 */
	e = s.PublishCommand(limited, fmt.Sprintf("/iot/down/%s/%s/%s/command", t, device.ProductID, d), payload, 1, false) /* 更新 e 的值。 */
	status, message := "SENT", ""                                                                                       /* 更新 message 的值。 */
	if e != nil {                                                                                                       /* 判断条件并选择处理分支。 */
		status, message = "UNKNOWN", "publish outcome unknown; inspect device before sending a new command" /* 更新 message 的值。 */
	} /* 结束当前表达式或代码块。 */
	if err := s.Repo.UpdateDeviceCommandDispatch(ctx, t, q.ID, status, message, time.Now().UnixMilli()); err != nil { /* 判断条件并选择处理分支。 */
		return q, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	q.Status = status     /* 更新 q.Status 的值。 */
	q.LastError = message /* 更新 q.LastError 的值。 */
	return q, nil         /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
