package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bytes"             /* 执行当前语句并推进处理流程。 */
	"context"           /* 执行当前语句并推进处理流程。 */
	"encoding/json"     /* 执行当前语句并推进处理流程。 */
	"fmt"               /* 执行当前语句并推进处理流程。 */
	"io"                /* 执行当前语句并推进处理流程。 */
	"log/slog"          /* 执行当前语句并推进处理流程。 */
	"net/http"          /* 执行当前语句并推进处理流程。 */
	"net/http/httptest" /* 执行当前语句并推进处理流程。 */
	"net/url"           /* 执行当前语句并推进处理流程。 */
	"os"                /* 执行当前语句并推进处理流程。 */
	"strings"           /* 执行当前语句并推进处理流程。 */
	"testing"           /* 执行当前语句并推进处理流程。 */
	"time"              /* 执行当前语句并推进处理流程。 */

	mqtt "github.com/eclipse/paho.mqtt.golang"        /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/local"            /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/memory"           /* 执行当前语句并推进处理流程。 */
	mqttadapter "iot-platform/internal/adapters/mqtt" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/auth"                      /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/config"                    /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/core"                      /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/metrics"                   /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"                     /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/onboarding"                /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser"                    /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports"                     /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

// Opt-in live broker check. All business storage is temporary, clients use clean
// sessions and messages are non-retained under a unique test tenant.
func TestStandardMQTTLiveBroker(t *testing.T) { /* 定义 TestStandardMQTTLiveBroker 函数。 */
	broker, secret := os.Getenv("IOT_TEST_MQTT_BROKER"), os.Getenv("IOT_TEST_MQTT_JWT_SECRET") /* 更新 secret 的值。 */
	if broker == "" || secret == "" {                                                          /* 判断条件并选择处理分支。 */
		t.Skip("configure IOT_TEST_MQTT_BROKER and IOT_TEST_MQTT_JWT_SECRET") /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)                                                                                                                                                                                /* 更新 cancel 的值。 */
	defer cancel()                                                                                                                                                                                                                                          /* 安排函数结束时执行清理。 */
	tenant := "integration-" + randomHex(8)                                                                                                                                                                                                                 /* 更新 tenant 的值。 */
	username := tenant + "-platform"                                                                                                                                                                                                                        /* 更新 username 的值。 */
	token, e := auth.New(secret).IssueWithACL(username, tenant, "service", nil, []auth.ACLRule{{Permission: "allow", Action: "subscribe", Topic: "/iot/up/#"}, {Permission: "allow", Action: "publish", Topic: "/iot/down/" + tenant + "/#"}}, time.Minute) /* 更新 e 的值。 */
	if e != nil {                                                                                                                                                                                                                                           /* 判断条件并选择处理分支。 */
		t.Fatal(e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	platform, e := mqttadapter.New(broker, username, token, username) /* 更新 e 的值。 */
	if e != nil {                                                     /* 判断条件并选择处理分支。 */
		t.Fatal("test platform MQTT connection failed", e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	defer platform.Close()               /* 安排函数结束时执行清理。 */
	repo := memory.NewRepository()       /* 更新 repo 的值。 */
	root := t.TempDir()                  /* 更新 root 的值。 */
	archive, e := local.NewArchive(root) /* 更新 e 的值。 */
	if e != nil {                        /* 判断条件并选择处理分支。 */
		t.Fatal(e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	log := slog.New(slog.NewTextHandler(io.Discard, nil))                                                                           /* 更新 log 的值。 */
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(root), log) /* 更新 engine 的值。 */
	if e = engine.Start(ctx); e != nil {                                                                                            /* 判断条件并选择处理分支。 */
		t.Fatal(e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	cfg := config.Load()                           /* 更新 cfg 的值。 */
	cfg.JWTSecret = secret                         /* 更新 cfg.JWTSecret 的值。 */
	cfg.DataDir = root                             /* 更新 cfg.DataDir 的值。 */
	srv := New(cfg, engine, metrics.New(), log)    /* 更新 srv 的值。 */
	srv.SetMQTTHealth(platform.Probe)              /* 执行当前语句并推进处理流程。 */
	srv.SetDeviceOperations(platform.Publish, nil) /* 执行当前语句并推进处理流程。 */
	draft := &onboarding.NewProduct{ID: "product", Name: "temporary MQTT test", ProtocolPackageID: onboarding.StandardPackageID, Transport: "MQTT"}
	check, e := srv.onboarding.Preflight(ctx, tenant, "", draft, onboarding.PublicAddresses{MQTT: true})
	if e != nil || !check.Ready || check.Checks[len(check.Checks)-1].State != "passed" {
		t.Fatal("onboarding preflight failed", check.Checks, e)
	}
	created, e := srv.onboarding.Enroll(ctx, tenant, onboarding.EnrollRequest{RequestID: "mqtt-live", NewProduct: draft, Device: onboarding.EnrollDevice{ID: "device", Name: "test device"}, Connection: onboarding.EnrollConnection{Mode: onboarding.ModeStandard}})
	if e != nil {
		t.Fatal(e)
	}
	failures := make(chan error, 4)                                                                           /* 更新 failures 的值。 */
	if e = platform.SubscribeStandard(func(c context.Context, tnt, p, d, kind string, payload []byte) error { /* 判断条件并选择处理分支。 */
		if tnt != tenant { /* 判断条件并选择处理分支。 */
			return nil /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		raw, e := srv.onboarding.PrepareStandard(c, tnt, p, d, kind, "MQTT", payload) /* 更新 e 的值。 */
		if e == nil {                                                                 /* 判断条件并选择处理分支。 */
			_, _, e = engine.IngestRaw(c, raw) /* 更新 e 的值。 */
		} /* 结束当前表达式或代码块。 */
		if e != nil { /* 判断条件并选择处理分支。 */
			select { /* 根据条件选择处理路径。 */
			case failures <- e: /* 处理当前分支。 */
			default: /* 处理当前分支。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		return e /* 返回当前处理结果。 */
	}); e != nil { /* 结束当前表达式或代码块。 */
		t.Fatal(e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	r := httptest.NewRequest("POST", "/api/v1/device-mqtt/token", nil) /* 更新 r 的值。 */
	r.Header.Set("X-Device-Key", created.Credential.AccessKey)         /* 执行当前语句并推进处理流程。 */
	r.Header.Set("X-Device-Secret", created.Credential.Secret)         /* 执行当前语句并推进处理流程。 */
	w := httptest.NewRecorder()                                        /* 更新 w 的值。 */
	srv.Handler().ServeHTTP(w, r)                                      /* 执行当前语句并推进处理流程。 */
	if w.Code != 200 {                                                 /* 判断条件并选择处理分支。 */
		t.Fatal("device token request failed", w.Code) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	var credentials struct { /* 声明 credentials。 */
		Username string `json:"username"` /* 执行当前语句并推进处理流程。 */
		Token    string `json:"token"`    /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if e = json.Unmarshal(w.Body.Bytes(), &credentials); e != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	device := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(broker).SetClientID(tenant + "-device").SetUsername(credentials.Username).SetPassword(credentials.Token).SetCleanSession(true).SetAutoReconnect(false).SetConnectRetry(false).SetOrderMatters(false)) /* 更新 device 的值。 */
	wait := func(t *testing.T, token mqtt.Token) {                                                                                                                                                                                                                   /* 更新 wait 的值。 */
		t.Helper()                               /* 执行当前语句并推进处理流程。 */
		if !token.WaitTimeout(8 * time.Second) { /* 判断条件并选择处理分支。 */
			t.Fatal("MQTT operation timed out") /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		if token.Error() != nil { /* 判断条件并选择处理分支。 */
			t.Fatal("MQTT operation failed", token.Error()) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	wait(t, device.Connect())                                                                                                     /* 执行当前语句并推进处理流程。 */
	defer device.Disconnect(100)                                                                                                  /* 安排函数结束时执行清理。 */
	prefix := fmt.Sprintf("/iot/up/%s/product/device/", tenant)                                                                   /* 更新 prefix 的值。 */
	wait(t, device.Subscribe(fmt.Sprintf("/iot/down/%s/product/device/command", tenant), 1, func(_ mqtt.Client, m mqtt.Message) { /* 执行当前语句并推进处理流程。 */
		var command struct { /* 声明 command。 */
			ID string `json:"id"` /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if json.Unmarshal(m.Payload(), &command) != nil { /* 判断条件并选择处理分支。 */
			return /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		body, _ := json.Marshal(map[string]any{"id": "reply-1", "timestamp": time.Now().UnixMilli(), "data": map[string]any{"commandId": command.ID, "success": true}}) /* 更新 _ 的值。 */
		device.Publish(prefix+"command-reply", 1, false, body)                                                                                                          /* 执行当前语句并推进处理流程。 */
	})) /* 结束当前表达式或代码块。 */
	property := []byte(fmt.Sprintf(`{"id":"property-1","timestamp":%d,"data":{"temperature":26.5}}`, time.Now().UnixMilli())) /* 更新 property 的值。 */
	wait(t, device.Publish(prefix+"property", 1, false, property))                                                            /* 执行当前语句并推进处理流程。 */
	until := func(t *testing.T, check func() bool) {                                                                          /* 更新 until 的值。 */
		t.Helper()     /* 执行当前语句并推进处理流程。 */
		for !check() { /* 循环处理当前数据。 */
			select { /* 根据条件选择处理路径。 */
			case e := <-failures: /* 处理当前分支。 */
				t.Fatal(e) /* 验证实际结果符合预期。 */
			case <-ctx.Done(): /* 处理当前分支。 */
				t.Fatal("MQTT integration deadline exceeded") /* 验证实际结果符合预期。 */
			case <-time.After(20 * time.Millisecond): /* 处理当前分支。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	until(t, func() bool { /* 执行当前语句并推进处理流程。 */
		m, e := repo.GetLatestMessage(ctx, tenant, "device")                                            /* 更新 e 的值。 */
		return e == nil && m.MessageType == model.PropertyReport && m.Properties["temperature"] == 26.5 /* 返回当前处理结果。 */
	}) /* 结束当前表达式或代码块。 */
	adminToken, _ := srv.auth.Issue("test", tenant, "admin", nil, time.Minute)                                                                                       /* 更新 _ 的值。 */
	r = httptest.NewRequest("POST", "/api/v1/device-registry/device/commands", bytes.NewBufferString(`{"confirmed":true,"id":"command-1","type":"test","data":{}}`)) /* 更新 r 的值。 */
	r.Header.Set("Authorization", "Bearer "+adminToken)                                                                                                              /* 执行当前语句并推进处理流程。 */
	r.Header.Set("Content-Type", "application/json")                                                                                                                 /* 执行当前语句并推进处理流程。 */
	w = httptest.NewRecorder()                                                                                                                                       /* 更新 w 的值。 */
	srv.Handler().ServeHTTP(w, r)                                                                                                                                    /* 执行当前语句并推进处理流程。 */
	if w.Code != 202 {                                                                                                                                               /* 判断条件并选择处理分支。 */
		t.Fatal(w.Code, w.Body.String()) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	until(t, func() bool { /* 执行当前语句并推进处理流程。 */
		commands, _, e := repo.ListDeviceCommands(ctx, tenant, "device", 20, 0)    /* 更新 e 的值。 */
		return e == nil && len(commands) == 1 && commands[0].Status == "SUCCEEDED" /* 返回当前处理结果。 */
	}) /* 结束当前表达式或代码块。 */
	raw, e := onboarding.StandardRaw(tenant, "product", "device", "property", "MQTT", property) /* 更新 e 的值。 */
	if e != nil {                                                                               /* 判断条件并选择处理分支。 */
		t.Fatal(e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, e = repo.GetRawIndex(ctx, tenant, raw.MessageID); e != nil { /* 判断条件并选择处理分支。 */
		t.Fatal("live property was not archived", e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	// A clean-session reconnect must still accept the device JWT and resume
	// ingress. Exercise rule activation/recovery after the transport reconnect.
	device.Disconnect(100)                                                                                                                                                                                                                                                                                                                                                                   /* 执行当前语句并推进处理流程。 */
	wait(t, device.Connect())                                                                                                                                                                                                                                                                                                                                                                /* 执行当前语句并推进处理流程。 */
	if e = repo.SaveRule(ctx, model.AlarmRule{TenantID: tenant, ID: "temperature-rule", ProductID: "product", Name: "temporary threshold", Enabled: true, AlarmType: "HIGH_TEMPERATURE", Level: "HIGH", Conditions: []model.RuleCondition{{Field: "temperature", Operator: ">", Value: 80}}, Recovery: []model.RuleCondition{{Field: "temperature", Operator: "<", Value: 70}}}); e != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(e) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	raise := fmt.Sprintf(`{"id":"alarm-property","timestamp":%d,"data":{"temperature":85}}`, time.Now().UnixMilli()) /* 更新 raise 的值。 */
	wait(t, device.Publish(prefix+"property", 1, false, raise))                                                      /* 执行当前语句并推进处理流程。 */
	until(t, func() bool {                                                                                           /* 执行当前语句并推进处理流程。 */
		items, e := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: tenant, DeviceID: "device", Status: "ACTIVE", Limit: 10}) /* 更新 e 的值。 */
		return e == nil && len(items) == 1 && items[0].RuleID == "temperature-rule"                                            /* 返回当前处理结果。 */
	}) /* 结束当前表达式或代码块。 */
	// Retransmit the same application message id before recovery.
	wait(t, device.Publish(prefix+"property", 1, false, raise))                                                               /* 执行当前语句并推进处理流程。 */
	recoverBody := fmt.Sprintf(`{"id":"recovery-property","timestamp":%d,"data":{"temperature":25}}`, time.Now().UnixMilli()) /* 更新 recoverBody 的值。 */
	wait(t, device.Publish(prefix+"property", 1, false, recoverBody))                                                         /* 执行当前语句并推进处理流程。 */
	until(t, func() bool {                                                                                                    /* 执行当前语句并推进处理流程。 */
		items, e := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: tenant, DeviceID: "device", Status: "RECOVERED", Limit: 10}) /* 更新 e 的值。 */
		return e == nil && len(items) == 1 && items[0].TriggerCount == 1                                                          /* 返回当前处理结果。 */
	}) /* 结束当前表达式或代码块。 */
	for _, body := range []string{raise, recoverBody} { /* 循环处理当前数据。 */
		record, e := onboarding.StandardRaw(tenant, "product", "device", "property", "MQTT", []byte(body)) /* 更新 e 的值。 */
		if e != nil {                                                                                      /* 判断条件并选择处理分支。 */
			t.Fatal(e) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		if _, e = repo.GetRawIndex(ctx, tenant, record.MessageID); e != nil { /* 判断条件并选择处理分支。 */
			t.Fatal("rule input bypassed archive", e) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	t.Log("live MQTT: clean-session reconnect, alarm activation, duplicate message deduplication and recovery passed") /* 执行当前语句并推进处理流程。 */
	t.Log("live MQTT: onboarding, device JWT, property archive, command dispatch and Raw command reply passed")        /* 执行当前语句并推进处理流程。 */
	t.Run("AuthenticationAndACL", func(t *testing.T) {                                                                 /* 执行当前语句并推进处理流程。 */
		reject := func(user, password string) { /* 更新 reject 的值。 */
			client := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(broker).SetClientID(tenant + "-deny-" + randomHex(4)).SetUsername(user).SetPassword(password).SetAutoReconnect(false).SetConnectRetry(false)) /* 更新 client 的值。 */
			defer client.Disconnect(100)                                                                                                                                                                             /* 安排函数结束时执行清理。 */
			attempt := client.Connect()                                                                                                                                                                              /* 更新 attempt 的值。 */
			if !attempt.WaitTimeout(5 * time.Second) {                                                                                                                                                               /* 判断条件并选择处理分支。 */
				t.Fatal("authentication rejection timed out") /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			code := attempt.(*mqtt.ConnectToken).ReturnCode()       /* 更新 code 的值。 */
			if attempt.Error() == nil || (code != 4 && code != 5) { /* 判断条件并选择处理分支。 */
				t.Fatalf("authentication rejection was not confirmed: CONNACK %d", code) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		reject(credentials.Username, "invalid-jwt")               /* 执行当前语句并推进处理流程。 */
		if os.Getenv("IOT_TEST_MQTT_STRICT_IDENTITY") == "true" { /* 判断条件并选择处理分支。 */
			reject(username, credentials.Token) /* 执行当前语句并推进处理流程。 */
		} else { /* 结束当前表达式或代码块。 */
			t.Log("username binding check not executed: set IOT_TEST_MQTT_STRICT_IDENTITY=true against updated isolated Broker") /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		for _, topic := range []string{"/iot/down/" + tenant + "/product/device/shadow", "/iot/down/" + tenant + "/product/other/command", "/iot/down/other-" + tenant + "/product/device/command", "/iot/up/#", "/iot/down/" + tenant + "/product/other/shadow", "/iot/down/other-" + tenant + "/product/device/shadow"} { /* 循环处理当前数据。 */
			op := device.Subscribe(topic, 1, func(mqtt.Client, mqtt.Message) {}) /* 更新 op 的值。 */
			if !op.WaitTimeout(5 * time.Second) {                                /* 判断条件并选择处理分支。 */
				t.Fatal("ACL subscription result timed out") /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			if op.(*mqtt.SubscribeToken).Result()[topic] != 128 { /* 判断条件并选择处理分支。 */
				t.Fatal("unauthorized subscription not rejected", topic) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		forbidden := []string{"/iot/up/" + tenant + "/product/device/shadow-get", "/iot/up/" + tenant + "/product/other/property", "/iot/up/other-" + tenant + "/product/device/property", "/external/raw/" + tenant + "/product/device", "/iot/up/" + tenant + "/product/other/shadow-get", "/iot/up/other-" + tenant + "/product/device/shadow-get"} /* 更新 forbidden 的值。 */
		acl := []auth.ACLRule{}                                                                                                                                                                                                                                                                                                                        /* 更新 acl 的值。 */
		for _, topic := range append(forbidden, prefix+"property") {                                                                                                                                                                                                                                                                                   /* 循环处理当前数据。 */
			acl = append(acl, auth.ACLRule{Permission: "allow", Action: "subscribe", Topic: topic}) /* 更新 acl 的值。 */
		} /* 结束当前表达式或代码块。 */
		observerName := tenant + "-observer"                                                                                                                               /* 更新 observerName 的值。 */
		jwt, _ := srv.auth.IssueWithACL(observerName, tenant, "service", nil, acl, time.Minute)                                                                            /* 更新 _ 的值。 */
		observer := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(broker).SetClientID(observerName).SetUsername(observerName).SetPassword(jwt).SetAutoReconnect(false)) /* 更新 observer 的值。 */
		wait(t, observer.Connect())                                                                                                                                        /* 执行当前语句并推进处理流程。 */
		defer observer.Disconnect(100)                                                                                                                                     /* 安排函数结束时执行清理。 */
		received := make(chan string, 8)                                                                                                                                   /* 更新 received 的值。 */
		for _, topic := range append(forbidden, prefix+"property") {                                                                                                       /* 循环处理当前数据。 */
			wait(t, observer.Subscribe(topic, 1, func(_ mqtt.Client, m mqtt.Message) { received <- m.Topic() })) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		for _, topic := range forbidden { /* 循环处理当前数据。 */
			wait(t, device.Publish(topic, 1, false, `{"id":"acl-denied","timestamp":1000,"data":{"x":1}}`)) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		// A valid canary proves the observer and publication path are functioning.
		wait(t, device.Publish(prefix+"property", 1, false, `{"id":"acl-canary","timestamp":1000,"data":{"x":1}}`)) /* 执行当前语句并推进处理流程。 */
		select {                                                                                                    /* 根据条件选择处理路径。 */
		case topic := <-received: /* 处理当前分支。 */
			if topic != prefix+"property" { /* 判断条件并选择处理分支。 */
				t.Fatal("unauthorized publication delivered", topic) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		case <-time.After(5 * time.Second): /* 处理当前分支。 */
			t.Fatal("allowed canary was not received") /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		select { /* 根据条件选择处理路径。 */
		case topic := <-received: /* 处理当前分支。 */
			t.Fatal("unexpected forbidden publication", topic) /* 验证实际结果符合预期。 */
		case <-time.After(300 * time.Millisecond): /* 处理当前分支。 */
		} /* 结束当前表达式或代码块。 */
		t.Log("bad credentials and forbidden subscriptions rejected; cross-device/cross-tenant/raw-topic publications blocked; valid canary received") /* 执行当前语句并推进处理流程。 */
	}) /* 结束当前表达式或代码块。 */
	t.Run("CredentialRevocation", func(t *testing.T) { /* 执行当前语句并推进处理流程。 */
		base, key, apiSecret := os.Getenv("IOT_TEST_EMQX_API_URL"), os.Getenv("IOT_TEST_EMQX_API_KEY"), os.Getenv("IOT_TEST_EMQX_API_SECRET") /* 更新 apiSecret 的值。 */
		if base == "" || key == "" || apiSecret == "" {                                                                                       /* 判断条件并选择处理分支。 */
			t.Skip("configure IOT_TEST_EMQX_API_URL, IOT_TEST_EMQX_API_KEY and IOT_TEST_EMQX_API_SECRET") /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		admin := &mqttadapter.Admin{URL: base, Key: key, Secret: apiSecret} /* 更新 admin 的值。 */
		srv.SetDeviceOperations(platform.Publish, admin.RevokeUsername)     /* 执行当前语句并推进处理流程。 */
		cleanupBan := func(user string) {                                   /* 更新 cleanupBan 的值。 */
			t.Cleanup(func() { /* 执行当前语句并推进处理流程。 */
				req, e := http.NewRequest("DELETE", strings.TrimRight(base, "/")+"/api/v5/banned/username/"+url.PathEscape(user), nil) /* 更新 e 的值。 */
				if e != nil {                                                                                                          /* 判断条件并选择处理分支。 */
					t.Error(e) /* 验证实际结果符合预期。 */
					return     /* 返回当前处理结果。 */
				} /* 结束当前表达式或代码块。 */
				req.SetBasicAuth(key, apiSecret)                                                                                                               /* 执行当前语句并推进处理流程。 */
				client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }} /* 更新 client 的值。 */
				res, e := client.Do(req)                                                                                                                       /* 更新 e 的值。 */
				if e != nil {                                                                                                                                  /* 判断条件并选择处理分支。 */
					t.Error("test ban cleanup request failed") /* 验证实际结果符合预期。 */
					return                                     /* 返回当前处理结果。 */
				} /* 结束当前表达式或代码块。 */
				res.Body.Close()                                    /* 执行当前语句并推进处理流程。 */
				if res.StatusCode != 204 && res.StatusCode != 404 { /* 判断条件并选择处理分支。 */
					t.Errorf("test ban cleanup HTTP %d", res.StatusCode) /* 验证实际结果符合预期。 */
				} /* 结束当前表达式或代码块。 */
			}) /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		cleanupBan(created.Credential.AccessKey)                                            /* 执行当前语句并推进处理流程。 */
		next, revocation, e := srv.onboarding.ChangeCredential(ctx, tenant, "device", true) /* 更新 e 的值。 */
		if e != nil || revocation.Status != "REVOKED" {                                     /* 判断条件并选择处理分支。 */
			t.Fatal("live credential rotation/revocation failed", revocation.Status, revocation.LastError, e) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		until(t, func() bool { return !device.IsConnected() })                     /* 执行当前语句并推进处理流程。 */
		if e = admin.RevokeUsername(ctx, created.Credential.AccessKey); e != nil { /* 判断条件并选择处理分支。 */
			t.Fatal("repeated broker revocation failed", e) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		rejectConnection := func(user, jwt string) { /* 更新 rejectConnection 的值。 */
			t.Helper()                                                                                                                                                                                                                   /* 执行当前语句并推进处理流程。 */
			stale := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(broker).SetClientID(tenant + "-rejected-" + randomHex(4)).SetUsername(user).SetPassword(jwt).SetCleanSession(true).SetAutoReconnect(false).SetConnectRetry(false)) /* 更新 stale 的值。 */
			defer stale.Disconnect(100)                                                                                                                                                                                                  /* 安排函数结束时执行清理。 */
			attempt := stale.Connect()                                                                                                                                                                                                   /* 更新 attempt 的值。 */
			if !attempt.WaitTimeout(5 * time.Second) {                                                                                                                                                                                   /* 判断条件并选择处理分支。 */
				t.Fatal("stale JWT rejection was not confirmed") /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			if attempt.Error() == nil || attempt.(*mqtt.ConnectToken).ReturnCode() != 5 { /* 判断条件并选择处理分支。 */
				t.Fatalf("expected broker CONNACK 5 for revoked JWT; got code %d", attempt.(*mqtt.ConnectToken).ReturnCode()) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		rejectConnection(credentials.Username, credentials.Token)            /* 执行当前语句并推进处理流程。 */
		issue := func(c model.DeviceCredential) *httptest.ResponseRecorder { /* 更新 issue 的值。 */
			r := httptest.NewRequest("POST", "/api/v1/device-mqtt/token", nil) /* 更新 r 的值。 */
			r.Header.Set("X-Device-Key", c.AccessKey)                          /* 执行当前语句并推进处理流程。 */
			r.Header.Set("X-Device-Secret", c.Secret)                          /* 执行当前语句并推进处理流程。 */
			w := httptest.NewRecorder()                                        /* 更新 w 的值。 */
			srv.Handler().ServeHTTP(w, r)                                      /* 执行当前语句并推进处理流程。 */
			return w                                                           /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if issue(created.Credential).Code != 401 { /* 判断条件并选择处理分支。 */
			t.Fatal("old device secret accepted after rotation") /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		tokenResponse := issue(next)   /* 更新 tokenResponse 的值。 */
		if tokenResponse.Code != 200 { /* 判断条件并选择处理分支。 */
			t.Fatal("new credentials rejected", tokenResponse.Code) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		var freshCredentials struct { /* 声明 freshCredentials。 */
			Username string `json:"username"` /* 执行当前语句并推进处理流程。 */
			Token    string `json:"token"`    /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if e = json.Unmarshal(tokenResponse.Body.Bytes(), &freshCredentials); e != nil { /* 判断条件并选择处理分支。 */
			t.Fatal(e) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		fresh := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(broker).SetClientID(tenant + "-fresh").SetUsername(freshCredentials.Username).SetPassword(freshCredentials.Token).SetCleanSession(true).SetAutoReconnect(false).SetConnectRetry(false)) /* 更新 fresh 的值。 */
		wait(t, fresh.Connect())                                                                                                                                                                                                                          /* 执行当前语句并推进处理流程。 */
		defer fresh.Disconnect(100)                                                                                                                                                                                                                       /* 安排函数结束时执行清理。 */
		body := fmt.Sprintf(`{"id":"property-2","timestamp":%d,"data":{"temperature":30}}`, time.Now().UnixMilli())                                                                                                                                       /* 更新 body 的值。 */
		wait(t, fresh.Publish(prefix+"property", 1, false, body))                                                                                                                                                                                         /* 执行当前语句并推进处理流程。 */
		until(t, func() bool {                                                                                                                                                                                                                            /* 执行当前语句并推进处理流程。 */
			m, e := repo.GetLatestMessage(ctx, tenant, "device")                                                   /* 更新 e 的值。 */
			return e == nil && m.MessageType == model.PropertyReport && m.Properties["temperature"] == float64(30) /* 返回当前处理结果。 */
		}) /* 结束当前表达式或代码块。 */
		cleanupBan(next.AccessKey)                                                      /* 执行当前语句并推进处理流程。 */
		_, disabled, e := srv.onboarding.ChangeCredential(ctx, tenant, "device", false) /* 更新 e 的值。 */
		if e != nil || disabled.Status != "REVOKED" {                                   /* 判断条件并选择处理分支。 */
			t.Fatal("live disable failed", disabled.Status, e) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		until(t, func() bool { return !fresh.IsConnected() })               /* 执行当前语句并推进处理流程。 */
		rejectConnection(freshCredentials.Username, freshCredentials.Token) /* 执行当前语句并推进处理流程。 */
		if issue(next).Code != 401 {                                        /* 判断条件并选择处理分支。 */
			t.Fatal("disabled device secret accepted") /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		if e = platform.Health(ctx); e != nil { /* 判断条件并选择处理分支。 */
			t.Fatal("unrelated platform MQTT connection was affected", e) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		t.Log("live EMQX: rotation and disable disconnect sessions; stale JWT reconnect rejected; new credential ingress succeeds; repeated revocation succeeds; unrelated connection remains healthy") /* 执行当前语句并推进处理流程。 */
	}) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
