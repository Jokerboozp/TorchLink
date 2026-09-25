package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bytes"                                 /* 执行当前语句并推进处理流程。 */
	"context"                               /* 执行当前语句并推进处理流程。 */
	"encoding/json"                         /* 执行当前语句并推进处理流程。 */
	"io"                                    /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/local"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/memory" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/config"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/core"            /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/metrics"         /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"           /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/onboarding"      /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser"          /* 执行当前语句并推进处理流程。 */
	"log/slog"                              /* 执行当前语句并推进处理流程。 */
	"net/http/httptest"                     /* 执行当前语句并推进处理流程。 */
	"testing"                               /* 执行当前语句并推进处理流程。 */
	"time"                                  /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestDeviceOperationsHTTPAndRawReply(t *testing.T) { /* 定义 TestDeviceOperationsHTTPAndRawReply 函数。 */
	ctx, cancel := context.WithCancel(context.Background()) /* 更新 cancel 的值。 */
	defer cancel()                                          /* 安排函数结束时执行清理。 */
	repo := memory.NewRepository()                          /* 更新 repo 的值。 */
	root := t.TempDir()                                     /* 更新 root 的值。 */
	archive, e := local.NewArchive(root)                    /* 更新 e 的值。 */
	if e != nil {                                           /* 判断条件并选择处理分支。 */
		t.Fatal(e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	log := slog.New(slog.NewTextHandler(io.Discard, nil))                                                         /* 更新 log 的值。 */
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(root), log) /* 更新 engine 的值。 */
	if e = engine.Start(ctx); e != nil {                                                                          /* 判断条件并选择处理分支。 */
		t.Fatal(e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	cfg := config.Load()                                                                                                                                                                       /* 更新 cfg 的值。 */
	cfg.JWTSecret = "operations-test-key-32-characters"                                                                                                                                        /* 更新 cfg.JWTSecret 的值。 */
	srv := New(cfg, engine, metrics.New(), log)                                                                                                                                                /* 更新 srv 的值。 */
	sent := 0                                                                                                                                                                                  /* 更新 sent 的值。 */
	srv.SetDeviceOperations(func(context.Context, string, []byte, byte, bool) error { sent++; return nil }, nil)                                                                               /* 检查错误并决定后续处理。 */
	repo.SaveProduct(ctx, model.Product{TenantID: "t", ID: "p", Status: "ENABLED"})                                                                                                            /* 执行当前语句并推进处理流程。 */
	repo.CreateProtocolRelease(ctx, model.ProtocolRelease{TenantID: "t", ProtocolID: parser.StandardProtocolID, Version: "1.0.0", Status: "PUBLISHED", ParserType: parser.StandardParserName}) /* 执行当前语句并推进处理流程。 */
	repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "t", ID: "d", ProductID: "p", Status: "ENABLED", AccessKey: "key", SecretHash: onboarding.Hash("secret"), Connector: "MQTT"})    /* 执行当前语句并推进处理流程。 */
	call := func(method, path, body, tenant, role string) *httptest.ResponseRecorder {                                                                                                         /* 更新 call 的值。 */
		token, _ := srv.auth.Issue("test", tenant, role, nil, time.Hour)    /* 更新 _ 的值。 */
		r := httptest.NewRequest(method, path, bytes.NewBufferString(body)) /* 更新 r 的值。 */
		r.Header.Set("Authorization", "Bearer "+token)                      /* 执行当前语句并推进处理流程。 */
		r.Header.Set("X-Device-Key", "key")                                 /* 执行当前语句并推进处理流程。 */
		r.Header.Set("X-Device-Secret", "secret")                           /* 执行当前语句并推进处理流程。 */
		r.Header.Set("Content-Type", "application/json")                    /* 执行当前语句并推进处理流程。 */
		w := httptest.NewRecorder()                                         /* 更新 w 的值。 */
		srv.Handler().ServeHTTP(w, r)                                       /* 执行当前语句并推进处理流程。 */
		return w                                                            /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if w := call("POST", "/api/v1/device-registry/d/commands", `{"confirmed":true,"id":"c1","type":"set","data":{"value":1}}`, "t", "viewer"); w.Code != 403 { /* 判断条件并选择处理分支。 */
		t.Fatal(w.Code, w.Body.String()) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if w := call("POST", "/api/v1/device-registry/d/commands", `{"id":"unconfirmed","type":"reset","data":{}}`, "t", "operator"); w.Code != 422 || sent != 0 { /* 判断条件并选择处理分支。 */
		t.Fatal("unconfirmed command dispatched", w.Code) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	for i := 0; i < 2; i++ { /* 循环处理当前数据。 */
		if w := call("POST", "/api/v1/device-registry/d/commands", `{"confirmed":true,"id":"c1","type":"set","data":{"value":1}}`, "t", "operator"); w.Code != 202 { /* 判断条件并选择处理分支。 */
			t.Fatal(w.Code, w.Body.String()) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if sent != 1 { /* 判断条件并选择处理分支。 */
		t.Fatal(sent) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if w := call("GET", "/api/v1/device-registry/d/history", "", "other", "admin"); w.Code != 404 { /* 判断条件并选择处理分支。 */
		t.Fatal(w.Code, w.Body.String()) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	reply := `{"id":"reply1","timestamp":1788850000001,"data":{"commandId":"c1","success":true,"result":"ok"}}` /* 更新 reply 的值。 */
	w := call("POST", "/api/v1/device-ingest/standard/t/p/d/command-reply", reply, "t", "admin")                /* 更新 w 的值。 */
	if w.Code != 202 {                                                                                          /* 判断条件并选择处理分支。 */
		t.Fatal(w.Code, w.Body.String()) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	var accepted struct { /* 声明 accepted。 */
		MessageID string `json:"messageId"` /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	json.Unmarshal(w.Body.Bytes(), &accepted)   /* 执行当前语句并推进处理流程。 */
	deadline := time.Now().Add(5 * time.Second) /* 更新 deadline 的值。 */
	for {                                       /* 循环处理当前数据。 */
		v, _, err := repo.ListDeviceCommands(ctx, "t", "d", 20, 0) /* 更新 err 的值。 */
		if err != nil {                                            /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		if v[0].Status == "SUCCEEDED" { /* 判断条件并选择处理分支。 */
			break /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if time.Now().After(deadline) { /* 判断条件并选择处理分支。 */
			t.Fatal("reply not applied", v) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		time.Sleep(10 * time.Millisecond) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if _, e = repo.GetRawIndex(ctx, "t", accepted.MessageID); e != nil { /* 判断条件并选择处理分支。 */
		t.Fatal("reply bypassed archive", e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	m, e := repo.GetStandardMessageByRaw(ctx, "t", accepted.MessageID) /* 更新 e 的值。 */
	if e != nil || m.MessageType != model.CommandReply {               /* 判断条件并选择处理分支。 */
		t.Fatal(m, e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if e = engine.ReportConnection(ctx, "t", "p", "d", true, 3); e != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if e = engine.ReportConnection(ctx, "t", "p", "d", false, 4); e != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	events, _, e := repo.ListDeviceStateEvents(ctx, "t", "d", 20, 0)                                                                          /* 更新 e 的值。 */
	if e != nil || len(events) < 2 || events[0].State.ConnectionStatus != "DISCONNECTED" || events[1].State.ConnectionStatus != "CONNECTED" { /* 判断条件并选择处理分支。 */
		t.Fatal(events, e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	for _, path := range []string{"/api/v1/edge-nodes", "/api/v1/edge-nodes/old/credentials", "/api/v1/edge-nodes/old/runtime", "/api/v1/edge-nodes/old/program", "/api/v1/edge/t/old/config", "/api/v1/edge/t/old/raw", "/api/v1/edge/t/old/heartbeat", "/api/v1/integrations/video/onvif/test", "/api/v1/integrations/video/onvif/discover"} { /* 循环处理当前数据。 */
		for _, method := range []string{"GET", "POST"} { /* 循环处理当前数据。 */
			if w = call(method, path, `{}`, "t", "admin"); w.Code != 404 { /* 判断条件并选择处理分支。 */
				t.Fatal("removed node route is still exposed", path, w.Code) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	w = call("DELETE", "/api/v1/device-registry/d/credentials", "", "t", "admin") /* 更新 w 的值。 */
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte("PENDING")) {      /* 判断条件并选择处理分支。 */
		t.Fatal(w.Code, w.Body.String()) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	w = call("POST", "/api/v1/device-mqtt/token", "", "t", "admin") /* 更新 w 的值。 */
	if w.Code != 401 {                                              /* 判断条件并选择处理分支。 */
		t.Fatal("disabled token accepted", w.Code) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	// Details follow the current binding even while a profile still records the
	// original release, and the alarm preview must remain device/tenant scoped.
	d, _ := repo.GetManagedDevice(ctx, "t", "d")                                                                                                                      /* 更新 _ 的值。 */
	d.Connector, d.ConnectorProfileID = "TCP", "profile"                                                                                                              /* 更新 d.Tags 的值。 */
	repo.SaveManagedDevice(ctx, d)                                                                                                                                    /* 执行当前语句并推进处理流程。 */
	repo.SaveDeviceAccessProfile(ctx, model.DeviceAccessProfile{TenantID: "t", ID: "profile", ProductID: "p", ProtocolID: "old", ProtocolVersion: "1"})               /* 执行当前语句并推进处理流程。 */
	repo.CreateProtocolRelease(ctx, model.ProtocolRelease{TenantID: "t", ProtocolID: "current", Version: "2", Status: "PUBLISHED", Capabilities: []string{"encode"}}) /* 执行当前语句并推进处理流程。 */
	repo.SaveProductProtocolBinding(ctx, model.ProductProtocolBinding{TenantID: "t", ProductID: "p", ProtocolID: "current", Version: "2"})                            /* 执行当前语句并推进处理流程。 */
	repo.UpsertAlarm(ctx, model.Alarm{TenantID: "t", DeviceID: "d", ID: "own", RuleID: "r", AlarmType: "FIRE", LastTriggeredAt: 1})                                   /* 执行当前语句并推进处理流程。 */
	repo.UpsertAlarm(ctx, model.Alarm{TenantID: "other", DeviceID: "d", ID: "foreign", RuleID: "r", AlarmType: "FIRE", LastTriggeredAt: 2})                           /* 执行当前语句并推进处理流程。 */
	w = call("GET", "/api/v1/device-registry/d/connection", "", "t", "viewer")                                                                                        /* 更新 w 的值。 */
	var detail struct {                                                                                                                                               /* 声明 detail。 */
		ProtocolID string        `json:"protocolId"`      /* 执行当前语句并推进处理流程。 */
		Version    string        `json:"protocolVersion"` /* 执行当前语句并推进处理流程。 */
		CanCommand bool          `json:"canCommand"`      /* 执行当前语句并推进处理流程。 */
		Alarms     []model.Alarm `json:"recentAlarms"`    /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &detail) != nil || detail.ProtocolID != "current" || detail.Version != "2" || !detail.CanCommand || len(detail.Alarms) != 1 || detail.Alarms[0].ID != "own" { /* 判断条件并选择处理分支。 */
		t.Fatal(w.Code, w.Body.String()) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
