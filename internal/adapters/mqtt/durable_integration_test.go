package mqttadapter

import (
	"context"
	"encoding/json"
	"fmt"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"io"
	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/core"
	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Opt-in: starts an isolated real broker with password authentication and ACLs.
// Never connects to the user's configured broker or starts the business stack.
func TestDurableMQTTRealBrokerAuthenticationRestartAndOfflineDelivery(t *testing.T) {
	if os.Getenv("IOT_TEST_MQTT_DOCKER") != "1" {
		t.Skip("set IOT_TEST_MQTT_DOCKER=1 for isolated real Mosquitto")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	run := func(args ...string) string {
		t.Helper()
		out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("test broker operation failed: %v %s", err, out)
		}
		return strings.TrimSpace(string(out))
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("mosquitto.conf", "listener 1883\nallow_anonymous false\npassword_file /mosquitto/config/password\nacl_file /mosquitto/config/acl\npersistence true\npersistence_location /mosquitto/data/\nautosave_interval 1\n")
	write("acl", "user receiver\ntopic read /iot/up/test/#\nuser sender\ntopic write /iot/up/test/p/d/#\n")
	run("run", "--rm", "--user", "0", "-v", root+":/mosquitto/config", "--entrypoint", "mosquitto_passwd", "eclipse-mosquitto:2", "-b", "-c", "/mosquitto/config/password", "receiver", "test-receiver-password")
	run("run", "--rm", "--user", "0", "-v", root+":/mosquitto/config", "--entrypoint", "mosquitto_passwd", "eclipse-mosquitto:2", "-b", "/mosquitto/config/password", "sender", "test-sender-password")
	if err := os.Chmod(filepath.Join(root, "password"), 0644); err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("iot-mqtt-receive-test-%d", time.Now().UnixNano())
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hostPort := listener.Addr().String()
	listener.Close()
	run("run", "-d", "--name", name, "-p", hostPort+":1883", "-v", root+":/mosquitto/config", "eclipse-mosquitto:2")
	t.Cleanup(func() {
		if t.Failed() {
			out, _ := exec.Command("docker", "logs", name).CombinedOutput()
			t.Log(string(out))
		}
		_ = exec.Command("docker", "rm", "-f", "-v", name).Run()
	})
	address := run("port", name, "1883/tcp")
	broker := "tcp://" + address
	credentials := func() (string, string) { return "receiver", "test-receiver-password" }
	inboxRoot := t.TempDir()
	var c *Client
	for i := 0; i < 30; i++ {
		c, err = NewDurableWithCredentials(broker, inboxRoot, credentials)
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if c != nil {
			_ = c.Close()
		}
	}()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(t.TempDir()), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err = repo.CreateProtocolRelease(ctx, model.ProtocolRelease{TenantID: "test", ProtocolID: parser.StandardProtocolID, Version: "1.0.0", ParserType: parser.StandardParserName, Status: "PUBLISHED"}); err != nil {
		t.Fatal(err)
	}
	if err = engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveProduct(ctx, model.Product{TenantID: "test", ID: "p", Status: "ENABLED"}); err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "test", ProductID: "p", ID: "d", Name: "消防控制器", Status: "ENABLED", SecretHash: "test-inventory-credential", Tags: map[string]string{"connector": "MQTT"}}); err != nil {
		t.Fatal(err)
	}
	ingress := onboarding.New(repo, engine.Parsers, t.TempDir(), nil)
	var blocked atomic.Bool
	blocked.Store(true)
	var handled atomic.Int32
	handler := func(ctx context.Context, tenant, product, device, kind string, payload []byte) error {
		if tenant != "test" || product != "p" || device != "d" {
			t.Error("broker ACL leaked another identity")
		}
		for blocked.Load() {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(20 * time.Millisecond):
			}
		}
		raw, err := ingress.PrepareStandard(ctx, tenant, product, device, kind, "MQTT", payload)
		if err != nil {
			return err
		}
		raw.ReceivedAt = ReceivedAt(ctx)
		if _, _, err = engine.IngestRaw(ctx, raw); err != nil {
			return err
		}
		handled.Add(1)
		return nil
	}
	if err = c.SubscribeStandard(handler); err != nil {
		t.Fatal(err)
	}
	wrong := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(broker).SetClientID("bad-auth").SetUsername("sender").SetPassword("wrong").SetConnectRetry(false))
	tok := wrong.Connect()
	if !tok.WaitTimeout(5*time.Second) || tok.Error() == nil {
		wrong.Disconnect(0)
		t.Fatal("broker accepted invalid credentials")
	}
	sender := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(broker).SetClientID("valid-sender").SetUsername("sender").SetPassword("test-sender-password").SetAutoReconnect(true).SetMaxReconnectInterval(time.Second).SetConnectTimeout(time.Second).SetKeepAlive(2 * time.Second))
	tok = sender.Connect()
	if !tok.WaitTimeout(5*time.Second) || tok.Error() != nil {
		t.Fatal("publisher authentication failed", tok.Error())
	}
	defer sender.Disconnect(0)
	publish := func(topic, payload string) {
		t.Helper()
		token := sender.Publish(topic, 1, false, payload)
		if !token.WaitTimeout(5*time.Second) || token.Error() != nil {
			t.Fatal("publish", token.Error())
		}
	}
	payload := func(id string, at int64, active bool) string {
		b, _ := json.Marshal(map[string]any{"id": id, "timestamp": at, "data": map[string]any{"components": []model.ComponentStatus{{ID: "loop-1/device-7", Name: "烟感", Location: "二楼", Alarms: map[string]bool{"FIRE": active}}}}})
		return string(b)
	}
	publish("/iot/up/test/p/d/event", payload("before-crash", 1000, true))
	eventually(t, func() bool { return inboxDepth(c.inbox) == 1 })
	c.Close()
	blocked.Store(false)
	// Drop all in-process work and restart broker too. The receiver's fsynced
	// envelope must survive independently of broker memory/session redelivery.
	run("restart", name)
	for i := 0; i < 30; i++ {
		c, err = NewDurableWithCredentials(broker, inboxRoot, credentials)
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	if err = c.SubscribeStandard(handler); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return handled.Load() >= 1 && inboxDepth(c.inbox) == 0 })
	alarms, err := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "test", DeviceID: "d", Status: "ACTIVE", Limit: 100})
	if err != nil || len(alarms) != 1 || alarms[0].ComponentLocation != "二楼" {
		t.Fatal("durable MQTT did not reach component alarm", alarms, err)
	}
	c.Close()
	before := handled.Load()
	eventually(t, func() bool { return sender.IsConnectionOpen() })
	publish("/iot/up/test/p/d/event", payload("while-offline", 2000, false))
	c, err = NewDurableWithCredentials(broker, inboxRoot, credentials)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.SubscribeStandard(handler); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return handled.Load() > before && inboxDepth(c.inbox) == 0 })
	saved, err := repo.GetAlarm(ctx, "test", alarms[0].ID)
	if err != nil || saved.Status != "RECOVERED" {
		t.Fatal("offline report did not recover component", saved, err)
	}
	before = handled.Load()
	publish("/iot/up/test/p/other/event", `{"id":"forbidden"}`)
	time.Sleep(250 * time.Millisecond)
	if handled.Load() != before {
		t.Fatal("unauthorized topic reached receiver")
	}
	// A full receiver inbox must withhold ACK and obtain a real broker redelivery.
	c.Close()
	limitedRoot := t.TempDir()
	limited, err := openInbox(limitedRoot, 8<<20, 8)
	if err != nil {
		t.Fatal(err)
	}
	limitedID, err := inboxIdentity(limitedRoot)
	if err != nil {
		t.Fatal(err)
	}
	blocked.Store(true)
	c, err = newClient(broker, limitedID, credentials, limited)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.SubscribeStandard(handler); err != nil {
		t.Fatal(err)
	}
	before = handled.Load()
	publish("/iot/up/test/p/d/event", payload("capacity-first", 3000, true))
	eventually(t, func() bool { return inboxDepth(c.inbox) == 1 })
	publish("/iot/up/test/p/d/event", payload("capacity-second", 4000, false))
	eventually(t, func() bool { c.inbox.mu.Lock(); defer c.inbox.mu.Unlock(); return c.inbox.lastError != nil })
	blocked.Store(false)
	eventually(t, func() bool { return handled.Load() >= before+2 && inboxDepth(c.inbox) == 0 })
	for _, id := range []string{"capacity-first", "capacity-second"} {
		raw, err := onboarding.StandardRaw("test", "p", "d", "event", "MQTT", []byte(payload(id, 3000, true)))
		if err != nil {
			t.Fatal(err)
		}
		idx, err := repo.GetRawIndex(ctx, "test", raw.MessageID)
		if err != nil || idx.PublishedAt == 0 {
			t.Fatal("capacity report not archived/published", id, err)
		}
	}

}
