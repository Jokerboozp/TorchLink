package onboarding /* 声明 onboarding 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                               /* 执行当前语句并推进处理流程。 */
	"encoding/json"                         /* 执行当前语句并推进处理流程。 */
	"errors"                                /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/memory" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"           /* 执行当前语句并推进处理流程。 */
	"strings"                               /* 执行当前语句并推进处理流程。 */
	"sync"                                  /* 执行当前语句并推进处理流程。 */
	"sync/atomic"                           /* 执行当前语句并推进处理流程。 */
	"testing"                               /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func operationService(t *testing.T) *Service { /* 定义 operationService 函数。 */
	t.Helper()                                                                                       /* 执行当前语句并推进处理流程。 */
	r := memory.NewRepository()                                                                      /* 更新 r 的值。 */
	ctx := context.Background()                                                                      /* 更新 ctx 的值。 */
	if e := r.SaveProduct(ctx, model.Product{TenantID: "t", ID: "p", Status: "ENABLED"}); e != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if e := r.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "t", ID: "d", ProductID: "p", Status: "ENABLED", AccessKey: "old", SecretHash: Hash("secret"), Tags: map[string]string{"connector": "MQTT"}}); e != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	return New(r, nil, "", nil) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func TestCommandConcurrencyAndEarlyReply(t *testing.T) { /* 定义 TestCommandConcurrencyAndEarlyReply 函数。 */
	s := operationService(t)                                                                              /* 更新 s 的值。 */
	ctx := context.Background()                                                                           /* 更新 ctx 的值。 */
	var sent atomic.Int32                                                                                 /* 声明 sent。 */
	s.PublishCommand = func(ctx context.Context, topic string, b []byte, qos byte, retained bool) error { /* 更新 s.PublishCommand 的值。 */
		sent.Add(1)                                         /* 执行当前语句并推进处理流程。 */
		if topic != "/iot/down/t/p/d/command" || retained { /* 判断条件并选择处理分支。 */
			t.Error("wrong command routing") /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		var v map[string]any              /* 声明 v。 */
		if json.Unmarshal(b, &v) != nil { /* 判断条件并选择处理分支。 */
			t.Error("invalid envelope") /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		return s.Repo.CompleteDeviceCommand(ctx, "t", "d", "cmd1", map[string]any{"success": true}, 2) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	q := model.DeviceCommand{Confirmed: true, ID: "cmd1", Type: "reboot", Data: map[string]any{}} /* 更新 q 的值。 */
	var wg sync.WaitGroup                                                                         /* 声明 wg。 */
	for i := 0; i < 20; i++ {                                                                     /* 循环处理当前数据。 */
		wg.Add(1)   /* 执行当前语句并推进处理流程。 */
		go func() { /* 执行当前语句并推进处理流程。 */
			defer wg.Done()                                        /* 安排函数结束时执行清理。 */
			if _, e := s.SendCommand(ctx, "t", "d", q); e != nil { /* 判断条件并选择处理分支。 */
				t.Error(e) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		}() /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	wg.Wait()             /* 执行当前语句并推进处理流程。 */
	if sent.Load() != 1 { /* 判断条件并选择处理分支。 */
		t.Fatal("duplicate physical dispatch", sent.Load()) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	v, _, e := s.Repo.ListDeviceCommands(ctx, "t", "d", 20, 0) /* 更新 e 的值。 */
	if e != nil || len(v) != 1 || v[0].Status != "SUCCEEDED" { /* 判断条件并选择处理分支。 */
		t.Fatal(v, e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	q.Data = map[string]any{"different": true}            /* 更新 q.Data 的值。 */
	if _, e = s.SendCommand(ctx, "t", "d", q); e == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("conflicting id accepted") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, e = s.SendCommand(ctx, "other", "d", q); e == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("cross-tenant command accepted") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
func TestCommandUnknownIsNotRetried(t *testing.T) { /* 定义 TestCommandUnknownIsNotRetried 函数。 */
	s := operationService(t)                                                                                                 /* 更新 s 的值。 */
	n := 0                                                                                                                   /* 更新 n 的值。 */
	s.PublishCommand = func(context.Context, string, []byte, byte, bool) error { n++; return errors.New("connection lost") } /* 更新 s.PublishCommand 的值。 */
	q := model.DeviceCommand{Confirmed: true, ID: "cmd1", Type: "open", Data: map[string]any{}}                              /* 更新 q 的值。 */
	for i := 0; i < 2; i++ {                                                                                                 /* 循环处理当前数据。 */
		v, e := s.SendCommand(context.Background(), "t", "d", q) /* 更新 e 的值。 */
		if e != nil || v.Status != "UNKNOWN" {                   /* 判断条件并选择处理分支。 */
			t.Fatal(v, e) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if n != 1 { /* 判断条件并选择处理分支。 */
		t.Fatal(n) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
func TestCredentialOutboxAndRecovery(t *testing.T) { /* 定义 TestCredentialOutboxAndRecovery 函数。 */
	s := operationService(t)                                                        /* 更新 s 的值。 */
	ctx := context.Background()                                                     /* 更新 ctx 的值。 */
	c, v, e := s.ChangeCredential(ctx, "t", "d", true)                              /* 更新 e 的值。 */
	if e != nil || v.Status != "PENDING" || v.Username != "old" || c.Secret == "" { /* 判断条件并选择处理分支。 */
		t.Fatal(v, e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, e = s.Authenticate(ctx, "old", "secret"); e == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("old credential accepted") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, e = s.Authenticate(ctx, c.AccessKey, c.Secret); e != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	s.RevokeUsername = func(_ context.Context, user string) error { /* 更新 s.RevokeUsername 的值。 */
		if user != "old" { /* 判断条件并选择处理分支。 */
			t.Fatal(user) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		return nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	v = s.revoke(ctx, v)       /* 更新 v 的值。 */
	if v.Status != "REVOKED" { /* 判断条件并选择处理分支。 */
		t.Fatal(v) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	s.RevokeUsername = nil                             /* 更新 s.RevokeUsername 的值。 */
	_, v, e = s.ChangeCredential(ctx, "t", "d", false) /* 更新 e 的值。 */
	if e != nil || v.Username != c.AccessKey {         /* 判断条件并选择处理分支。 */
		t.Fatal(v, e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, e = s.Authenticate(ctx, c.AccessKey, c.Secret); e == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("disabled credential accepted") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	data, _ := json.Marshal(v)                    /* 更新 _ 的值。 */
	if strings.Contains(string(data), c.Secret) { /* 判断条件并选择处理分支。 */
		t.Fatal("secret exposed") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	items, _ := s.Repo.ListCredentialRevocations(ctx, "t", "d", false) /* 更新 _ 的值。 */
	if len(items) != 2 {                                               /* 判断条件并选择处理分支。 */
		t.Fatal(items) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
func TestThingModelAndRemovedEdgeValidation(t *testing.T) { /* 定义 TestThingModelAndRemovedEdgeValidation 函数。 */
	s := operationService(t)                                                                                                                                              /* 更新 s 的值。 */
	ctx := context.Background()                                                                                                                                           /* 更新 ctx 的值。 */
	m := &model.ThingModel{Commands: []model.ThingOperation{{Identifier: "set", Fields: []model.ThingField{{Identifier: "value", DataType: "integer", Required: true}}}}} /* 更新 m 的值。 */
	if e := ValidateThingModel(m); e != nil {                                                                                                                             /* 判断条件并选择处理分支。 */
		t.Fatal(e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	m.Properties = []model.ThingField{{Identifier: "a", DataType: "bad"}} /* 更新 m.Properties 的值。 */
	if ValidateThingModel(m) == nil {                                     /* 判断条件并选择处理分支。 */
		t.Fatal("invalid model accepted") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	m.Properties = nil                                                                                                                                  /* 更新 m.Properties 的值。 */
	s.Repo.SaveProduct(ctx, model.Product{TenantID: "t", ID: "p", Status: "ENABLED", ThingModel: m})                                                    /* 执行当前语句并推进处理流程。 */
	s.PublishCommand = func(context.Context, string, []byte, byte, bool) error { return nil }                                                           /* 检查错误并决定后续处理。 */
	if _, e := s.SendCommand(ctx, "t", "d", model.DeviceCommand{Confirmed: true, ID: "c", Type: "set", Data: map[string]any{"value": 1.2}}); e == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("invalid integer accepted") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	_, _, e := s.plan(ctx, "t", Request{ProductID: "p", DeviceID: "new", Name: "new", Profile: model.DeviceAccessProfile{EdgeNodeID: "edge"}}) /* 更新 e 的值。 */
	if e == nil {                                                                                                                              /* 判断条件并选择处理分支。 */
		t.Fatal("removed edge assignment accepted") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestCommandRequiresManualConfirmation(t *testing.T) { /* 定义 TestCommandRequiresManualConfirmation 函数。 */
	s := operationService(t)                                                     /* 更新 s 的值。 */
	s.PublishCommand = func(context.Context, string, []byte, byte, bool) error { /* 更新 s.PublishCommand 的值。 */
		t.Fatal("unconfirmed command was dispatched") /* 验证实际结果符合预期。 */
		return nil                                    /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if _, e := s.SendCommand(context.Background(), "t", "d", model.DeviceCommand{ID: "unconfirmed", Type: "reset", Data: map[string]any{}}); e == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("unconfirmed command accepted") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
