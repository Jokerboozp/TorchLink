package mqttadapter /* 声明 mqttadapter 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                                  /* 执行当前语句并推进处理流程。 */
	"encoding/json"                            /* 执行当前语句并推进处理流程。 */
	"fmt"                                      /* 执行当前语句并推进处理流程。 */
	mqtt "github.com/eclipse/paho.mqtt.golang" /* 执行当前语句并推进处理流程。 */
	"io"                                       /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/local"     /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/memory"    /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/core"               /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"              /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/onboarding"         /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser"             /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports"              /* 执行当前语句并推进处理流程。 */
	"log/slog"                                 /* 执行当前语句并推进处理流程。 */
	"net"                                      /* 执行当前语句并推进处理流程。 */
	"os"                                       /* 执行当前语句并推进处理流程。 */
	"os/exec"                                  /* 执行当前语句并推进处理流程。 */
	"path/filepath"                            /* 执行当前语句并推进处理流程。 */
	"strings"                                  /* 执行当前语句并推进处理流程。 */
	"sync/atomic"                              /* 执行当前语句并推进处理流程。 */
	"testing"                                  /* 执行当前语句并推进处理流程。 */
	"time"                                     /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

// Opt-in: starts an isolated real broker with password authentication and ACLs.
// Never connects to the user's configured broker or starts the business stack.
func TestDurableMQTTRealBrokerAuthenticationRestartAndOfflineDelivery(t *testing.T) { /* 定义 TestDurableMQTTRealBrokerAuthenticationRestartAndOfflineDelivery 函数。 */
	if os.Getenv("IOT_TEST_MQTT_DOCKER") != "1" { /* 判断条件并选择处理分支。 */
		t.Skip("set IOT_TEST_MQTT_DOCKER=1 for isolated real Mosquitto") /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second) /* 更新 cancel 的值。 */
	defer cancel()                                                           /* 安排函数结束时执行清理。 */
	run := func(args ...string) string {                                     /* 更新 run 的值。 */
		t.Helper()                                                               /* 执行当前语句并推进处理流程。 */
		out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput() /* 更新 err 的值。 */
		if err != nil {                                                          /* 判断条件并选择处理分支。 */
			t.Fatalf("test broker operation failed: %v %s", err, out) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		return strings.TrimSpace(string(out)) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	root := t.TempDir()                          /* 更新 root 的值。 */
	if err := os.Chmod(root, 0755); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	write := func(name, body string) { /* 更新 write 的值。 */
		t.Helper()                                                                          /* 执行当前语句并推进处理流程。 */
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0644); err != nil { /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	write("mosquitto.conf", "listener 1883\nallow_anonymous false\npassword_file /mosquitto/config/password\nacl_file /mosquitto/config/acl\npersistence true\npersistence_location /mosquitto/data/\nautosave_interval 1\n") /* 执行当前语句并推进处理流程。 */
	write("acl", "user receiver\ntopic read /iot/up/test/#\nuser sender\ntopic write /iot/up/test/p/d/#\n")                                                                                                                   /* 执行当前语句并推进处理流程。 */
	run("run", "--rm", "--user", "0", "-v", root+":/mosquitto/config", "--entrypoint", "mosquitto_passwd", "eclipse-mosquitto:2", "-b", "-c", "/mosquitto/config/password", "receiver", "test-receiver-password")             /* 执行当前语句并推进处理流程。 */
	run("run", "--rm", "--user", "0", "-v", root+":/mosquitto/config", "--entrypoint", "mosquitto_passwd", "eclipse-mosquitto:2", "-b", "/mosquitto/config/password", "sender", "test-sender-password")                       /* 执行当前语句并推进处理流程。 */
	if err := os.Chmod(filepath.Join(root, "password"), 0644); err != nil {                                                                                                                                                   /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	name := fmt.Sprintf("iot-mqtt-receive-test-%d", time.Now().UnixNano()) /* 更新 name 的值。 */
	listener, err := net.Listen("tcp", "127.0.0.1:0")                      /* 更新 err 的值。 */
	if err != nil {                                                        /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	hostPort := listener.Addr().String()                                                                             /* 更新 hostPort 的值。 */
	listener.Close()                                                                                                 /* 执行当前语句并推进处理流程。 */
	run("run", "-d", "--name", name, "-p", hostPort+":1883", "-v", root+":/mosquitto/config", "eclipse-mosquitto:2") /* 执行当前语句并推进处理流程。 */
	t.Cleanup(func() {                                                                                               /* 执行当前语句并推进处理流程。 */
		if t.Failed() { /* 判断条件并选择处理分支。 */
			out, _ := exec.Command("docker", "logs", name).CombinedOutput() /* 更新 _ 的值。 */
			t.Log(string(out))                                              /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		_ = exec.Command("docker", "rm", "-f", "-v", name).Run() /* 更新 _ 的值。 */
	}) /* 结束当前表达式或代码块。 */
	address := run("port", name, "1883/tcp")                                               /* 更新 address 的值。 */
	broker := "tcp://" + address                                                           /* 更新 broker 的值。 */
	credentials := func() (string, string) { return "receiver", "test-receiver-password" } /* 更新 credentials 的值。 */
	inboxRoot := t.TempDir()                                                               /* 更新 inboxRoot 的值。 */
	var c *Client                                                                          /* 声明 c。 */
	for i := 0; i < 30; i++ {                                                              /* 循环处理当前数据。 */
		c, err = NewDurableWithCredentials(broker, inboxRoot, credentials) /* 更新 err 的值。 */
		if err == nil {                                                    /* 判断条件并选择处理分支。 */
			break /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		time.Sleep(100 * time.Millisecond) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	defer func() { /* 安排函数结束时执行清理。 */
		if c != nil { /* 判断条件并选择处理分支。 */
			_ = c.Close() /* 更新 _ 的值。 */
		} /* 结束当前表达式或代码块。 */
	}() /* 结束当前表达式或代码块。 */
	repo := memory.NewRepository()                /* 更新 repo 的值。 */
	archive, err := local.NewArchive(t.TempDir()) /* 更新 err 的值。 */
	if err != nil {                               /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(t.TempDir()), slog.New(slog.NewTextHandler(io.Discard, nil)))                                                      /* 更新 engine 的值。 */
	if err = repo.CreateProtocolRelease(ctx, model.ProtocolRelease{TenantID: "test", ProtocolID: parser.StandardProtocolID, Version: "1.0.0", ParserType: parser.StandardParserName, Status: "PUBLISHED"}); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err = engine.Start(ctx); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err = repo.SaveProduct(ctx, model.Product{TenantID: "test", ID: "p", Status: "ENABLED"}); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err = repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "test", ProductID: "p", ID: "d", Name: "消防控制器", Status: "ENABLED", SecretHash: "test-inventory-credential", Tags: map[string]string{"connector": "MQTT"}}); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	ingress := onboarding.New(repo, engine.Parsers, t.TempDir(), nil)                                  /* 更新 ingress 的值。 */
	var blocked atomic.Bool                                                                            /* 声明 blocked。 */
	blocked.Store(true)                                                                                /* 执行当前语句并推进处理流程。 */
	var handled atomic.Int32                                                                           /* 声明 handled。 */
	handler := func(ctx context.Context, tenant, product, device, kind string, payload []byte) error { /* 更新 handler 的值。 */
		if tenant != "test" || product != "p" || device != "d" { /* 判断条件并选择处理分支。 */
			t.Error("broker ACL leaked another identity") /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		for blocked.Load() { /* 循环处理当前数据。 */
			select { /* 根据条件选择处理路径。 */
			case <-ctx.Done(): /* 处理当前分支。 */
				return ctx.Err() /* 返回当前处理结果。 */
			case <-time.After(20 * time.Millisecond): /* 处理当前分支。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		raw, err := ingress.PrepareStandard(ctx, tenant, product, device, kind, "MQTT", payload) /* 更新 err 的值。 */
		if err != nil {                                                                          /* 判断条件并选择处理分支。 */
			return err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		raw.ReceivedAt = ReceivedAt(ctx)                        /* 更新 raw.ReceivedAt 的值。 */
		if _, _, err = engine.IngestRaw(ctx, raw); err != nil { /* 判断条件并选择处理分支。 */
			return err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		handled.Add(1) /* 执行当前语句并推进处理流程。 */
		return nil     /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err = c.SubscribeStandard(handler); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	wrong := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(broker).SetClientID("bad-auth").SetUsername("sender").SetPassword("wrong").SetConnectRetry(false)) /* 更新 wrong 的值。 */
	tok := wrong.Connect()                                                                                                                                       /* 更新 tok 的值。 */
	if !tok.WaitTimeout(5*time.Second) || tok.Error() == nil {                                                                                                   /* 判断条件并选择处理分支。 */
		wrong.Disconnect(0)                            /* 执行当前语句并推进处理流程。 */
		t.Fatal("broker accepted invalid credentials") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	sender := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(broker).SetClientID("valid-sender").SetUsername("sender").SetPassword("test-sender-password").SetAutoReconnect(true).SetMaxReconnectInterval(time.Second).SetConnectTimeout(time.Second).SetKeepAlive(2 * time.Second)) /* 更新 sender 的值。 */
	tok = sender.Connect()                                                                                                                                                                                                                                                             /* 更新 tok 的值。 */
	if !tok.WaitTimeout(5*time.Second) || tok.Error() != nil {                                                                                                                                                                                                                         /* 判断条件并选择处理分支。 */
		t.Fatal("publisher authentication failed", tok.Error()) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	defer sender.Disconnect(0)               /* 安排函数结束时执行清理。 */
	publish := func(topic, payload string) { /* 更新 publish 的值。 */
		t.Helper()                                                     /* 执行当前语句并推进处理流程。 */
		token := sender.Publish(topic, 1, false, payload)              /* 更新 token 的值。 */
		if !token.WaitTimeout(5*time.Second) || token.Error() != nil { /* 判断条件并选择处理分支。 */
			t.Fatal("publish", token.Error()) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	payload := func(id string, at int64, active bool) string { /* 更新 payload 的值。 */
		b, _ := json.Marshal(map[string]any{"id": id, "timestamp": at, "data": map[string]any{"components": []model.ComponentStatus{{ID: "loop-1/device-7", Name: "烟感", Location: "二楼", Alarms: map[string]bool{"FIRE": active}}}}}) /* 更新 _ 的值。 */
		return string(b)                                                                                                                                                                                                             /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	publish("/iot/up/test/p/d/event", payload("before-crash", 1000, true)) /* 执行当前语句并推进处理流程。 */
	eventually(t, func() bool { return inboxDepth(c.inbox) == 1 })         /* 执行当前语句并推进处理流程。 */
	c.Close()                                                              /* 执行当前语句并推进处理流程。 */
	blocked.Store(false)                                                   /* 执行当前语句并推进处理流程。 */
	// Drop all in-process work and restart broker too. The receiver's fsynced
	// envelope must survive independently of broker memory/session redelivery.
	run("restart", name)      /* 执行当前语句并推进处理流程。 */
	for i := 0; i < 30; i++ { /* 循环处理当前数据。 */
		c, err = NewDurableWithCredentials(broker, inboxRoot, credentials) /* 更新 err 的值。 */
		if err == nil {                                                    /* 判断条件并选择处理分支。 */
			break /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		time.Sleep(100 * time.Millisecond) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err = c.SubscribeStandard(handler); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	eventually(t, func() bool { return handled.Load() >= 1 && inboxDepth(c.inbox) == 0 })                                 /* 执行当前语句并推进处理流程。 */
	alarms, err := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "test", DeviceID: "d", Status: "ACTIVE", Limit: 100}) /* 更新 err 的值。 */
	if err != nil || len(alarms) != 1 || alarms[0].ComponentLocation != "二楼" {                                            /* 判断条件并选择处理分支。 */
		t.Fatal("durable MQTT did not reach component alarm", alarms, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	c.Close()                                                                /* 执行当前语句并推进处理流程。 */
	before := handled.Load()                                                 /* 更新 before 的值。 */
	eventually(t, func() bool { return sender.IsConnectionOpen() })          /* 执行当前语句并推进处理流程。 */
	publish("/iot/up/test/p/d/event", payload("while-offline", 2000, false)) /* 执行当前语句并推进处理流程。 */
	c, err = NewDurableWithCredentials(broker, inboxRoot, credentials)       /* 更新 err 的值。 */
	if err != nil {                                                          /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err = c.SubscribeStandard(handler); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	eventually(t, func() bool { return handled.Load() > before && inboxDepth(c.inbox) == 0 }) /* 执行当前语句并推进处理流程。 */
	saved, err := repo.GetAlarm(ctx, "test", alarms[0].ID)                                    /* 更新 err 的值。 */
	if err != nil || saved.Status != "RECOVERED" {                                            /* 判断条件并选择处理分支。 */
		t.Fatal("offline report did not recover component", saved, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	before = handled.Load()                                     /* 更新 before 的值。 */
	publish("/iot/up/test/p/other/event", `{"id":"forbidden"}`) /* 执行当前语句并推进处理流程。 */
	time.Sleep(250 * time.Millisecond)                          /* 执行当前语句并推进处理流程。 */
	if handled.Load() != before {                               /* 判断条件并选择处理分支。 */
		t.Fatal("unauthorized topic reached receiver") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	// A full receiver inbox must withhold ACK and obtain a real broker redelivery.
	c.Close()                                        /* 执行当前语句并推进处理流程。 */
	limitedRoot := t.TempDir()                       /* 更新 limitedRoot 的值。 */
	limited, err := openInbox(limitedRoot, 8<<20, 8) /* 更新 err 的值。 */
	if err != nil {                                  /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	limitedID, err := inboxIdentity(limitedRoot) /* 更新 err 的值。 */
	if err != nil {                              /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	blocked.Store(true)                                         /* 执行当前语句并推进处理流程。 */
	c, err = newClient(broker, limitedID, credentials, limited) /* 更新 err 的值。 */
	if err != nil {                                             /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err = c.SubscribeStandard(handler); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	before = handled.Load()                                                                                      /* 更新 before 的值。 */
	publish("/iot/up/test/p/d/event", payload("capacity-first", 3000, true))                                     /* 执行当前语句并推进处理流程。 */
	eventually(t, func() bool { return inboxDepth(c.inbox) == 1 })                                               /* 执行当前语句并推进处理流程。 */
	publish("/iot/up/test/p/d/event", payload("capacity-second", 4000, false))                                   /* 执行当前语句并推进处理流程。 */
	eventually(t, func() bool { c.inbox.mu.Lock(); defer c.inbox.mu.Unlock(); return c.inbox.lastError != nil }) /* 执行当前语句并推进处理流程。 */
	blocked.Store(false)                                                                                         /* 执行当前语句并推进处理流程。 */
	eventually(t, func() bool { return handled.Load() >= before+2 && inboxDepth(c.inbox) == 0 })                 /* 执行当前语句并推进处理流程。 */
	for _, id := range []string{"capacity-first", "capacity-second"} {                                           /* 循环处理当前数据。 */
		raw, err := onboarding.StandardRaw("test", "p", "d", "event", "MQTT", []byte(payload(id, 3000, true))) /* 更新 err 的值。 */
		if err != nil {                                                                                        /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		idx, err := repo.GetRawIndex(ctx, "test", raw.MessageID) /* 更新 err 的值。 */
		if err != nil || idx.PublishedAt == 0 {                  /* 判断条件并选择处理分支。 */
			t.Fatal("capacity report not archived/published", id, err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */

} /* 结束当前表达式或代码块。 */
