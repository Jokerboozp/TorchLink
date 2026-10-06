package mqttadapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/auth"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/durablequeue"
	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
)

type receivedMessage struct {
	retained bool
	topic    string
	payload  []byte
	acked    atomic.Bool
}

func (m *receivedMessage) Duplicate() bool   { return false }
func (m *receivedMessage) Qos() byte         { return 1 }
func (m *receivedMessage) Retained() bool    { return m.retained }
func (m *receivedMessage) Topic() string     { return m.topic }
func (m *receivedMessage) MessageID() uint16 { return 1 }
func (m *receivedMessage) Payload() []byte   { return m.payload }
func (m *receivedMessage) Ack()              { m.acked.Store(true) }
func inboxClient(t *testing.T, d *durableInbox) *Client {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{inbox: d, ctx: ctx, cancel: cancel, routes: map[string]ingressHandler{}, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	t.Cleanup(func() { cancel(); d.close() })
	return c
}
func eventually(t *testing.T, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not reached")
}
func inboxDepth(d *durableInbox) int {
	n := 0
	for _, q := range d.queues {
		n += q.Depth()
	}
	return n
}

func TestReceiveRetainedStateSnapshotAndRejectionReasons(t *testing.T) {
	stateTopic := "/iot/device/state/tenant_001/cap-20260927-standard/cap-20260927-005409"
	for _, tc := range []struct {
		name     string
		topic    string
		retained bool
		payload  []byte
		reason   string
		depth    int
	}{
		{name: "historical state is quietly ignored", topic: stateTopic, retained: true, payload: []byte(`{"businessStatus":"normal"}`)},
		{name: "live state still enters inbox", topic: stateTopic, payload: []byte(`{"businessStatus":"normal"}`), depth: 1},
		{name: "state clear is ignored, not quarantined", topic: stateTopic, payload: []byte{}},
		{name: "retained uplink remains rejected", topic: "/iot/up/t/p/d/event", retained: true, reason: "retained"},
		{name: "malformed retained state is not quiet", topic: "/iot/device/state/t/p", retained: true, reason: "retained"},
		{name: "unknown topic", topic: "/unknown/topic", reason: "unknown_topic"},
		{name: "oversized retained state", topic: stateTopic, retained: true, payload: make([]byte, (128<<10)+1), reason: "payload_too_large"},
		{name: "oversized uplink", topic: "/iot/up/t/p/d/event", payload: make([]byte, (128<<10)+1), reason: "payload_too_large"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := openInbox(t.TempDir(), 8<<20, 80)
			if err != nil {
				t.Fatal(err)
			}
			c := inboxClient(t, d)
			var logs bytes.Buffer
			c.log = slog.New(slog.NewJSONHandler(&logs, nil))
			m := &receivedMessage{topic: tc.topic, retained: tc.retained, payload: tc.payload}
			c.receive(m)
			if !m.acked.Load() || inboxDepth(d) != tc.depth {
				t.Fatalf("unexpected receipt: ack=%v depth=%d", m.acked.Load(), inboxDepth(d))
			}
			if tc.reason == "" {
				if logs.Len() != 0 {
					t.Fatalf("normal state delivery generated a warning: %s", logs.String())
				}
				return
			}
			var entry map[string]any
			if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
				t.Fatal("missing rejection diagnostic", err)
			}
			if entry["level"] != "WARN" || entry["reason"] != tc.reason || entry["topic"] != tc.topic {
				t.Fatalf("unexpected rejection diagnostic: %v", entry)
			}
		})
	}
}

func TestDurableReceiveRestartRetryAndQuarantine(t *testing.T) {
	root := t.TempDir()
	d, err := openInbox(root, 8<<20, 80)
	if err != nil {
		t.Fatal(err)
	}
	c := inboxClient(t, d)
	m := &receivedMessage{topic: "/iot/up/t/p/d/event", payload: []byte(`{"id":"fire","data":{"fireAlarm":true}}`)}
	c.receive(m)
	if !m.acked.Load() || inboxDepth(d) != 1 {
		t.Fatal("ACK was not backed by durable receipt")
	}
	// Simulate loss of all process memory before handler registration.
	c.cancel()
	d.close()
	d, err = openInbox(root, 8<<20, 80)
	if err != nil {
		t.Fatal(err)
	}
	c = inboxClient(t, d)
	var attempts atomic.Int32
	var ready atomic.Bool
	c.routes["standard"] = func(ctx context.Context, topic string, payload []byte) error {
		attempts.Add(1)
		if topic != m.topic || string(payload) != string(m.payload) {
			t.Error("persisted envelope changed")
		}
		if !ready.Load() {
			return errors.New("database offline")
		}
		return nil
	}
	d.start(c)
	eventually(t, func() bool { return attempts.Load() > 0 })
	if inboxDepth(d) != 1 {
		t.Fatal("failure lost receipt")
	}
	if d.health() == nil {
		t.Fatal("pending failure hidden")
	}
	ready.Store(true)
	eventually(t, func() bool { return inboxDepth(d) == 0 })
	c.routeMu.Lock()
	c.routes["standard"] = func(context.Context, string, []byte) error { return Reject(errors.New("disabled device")) }
	c.routeMu.Unlock()
	if err = d.put(m.topic, []byte(`{"id":"denied"}`)); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		n := 0
		for _, q := range d.queues {
			n += q.Rejected()
		}
		return n == 1
	})
	if _, rejected, _ := c.InboxCounts(); rejected != 1 {
		t.Fatal("rejection quarantine hidden")
	}
	if d.health() != nil {
		t.Fatal("isolated rejection blocked healthy receive", d.health())
	}
	files, err := filepath.Glob(filepath.Join(root, "*", "*.rejected"))
	if err != nil || len(files) != 1 {
		t.Fatal(files, err)
	}
	b, err := os.ReadFile(files[0])
	if err != nil || len(b) == 0 {
		t.Fatal("rejected evidence lost")
	}
}

func TestDurableReceiveFullAndCorruptionDoNotClaimSuccess(t *testing.T) {
	root := t.TempDir()
	d, err := openInbox(root, 8<<20, 8)
	if err != nil {
		t.Fatal(err)
	}
	c := inboxClient(t, d)
	topic := "/iot/up/t/p/d/event"
	one := &receivedMessage{topic: topic, payload: []byte(`{"id":"one"}`)}
	c.receive(one)
	two := &receivedMessage{topic: topic, payload: []byte(`{"id":"two"}`)}
	c.receive(two)
	if !one.acked.Load() || two.acked.Load() {
		t.Fatal("capacity failure incorrectly acknowledged")
	}
	if !errors.Is(d.health(), durablequeue.ErrQueueFull) {
		t.Fatal(d.health())
	}
	c.cancel()
	d.close()
	files, _ := filepath.Glob(filepath.Join(root, "*", "*.json"))
	if len(files) != 1 {
		t.Fatal(files)
	}
	if err = os.WriteFile(files[0], []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	d, err = openInbox(root, 8<<20, 80)
	if err != nil {
		t.Fatal(err)
	}
	c = inboxClient(t, d)
	var handled atomic.Int32
	c.routes["standard"] = func(context.Context, string, []byte) error { handled.Add(1); return nil }
	d.start(c)
	if err = d.put(topic, []byte(`{"id":"valid"}`)); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return handled.Load() == 1 && inboxDepth(d) == 0 })
	if b, err := os.ReadFile(files[0] + ".corrupt"); err != nil || string(b) != "broken" {
		t.Fatal("corrupt bytes not retained", err)
	}
	if _, _, corrupt := c.InboxCounts(); corrupt != 1 {
		t.Fatal("corruption hidden")
	}
}

func TestInboxIdentityPersistsAndDirectoryIsExclusive(t *testing.T) {
	root := t.TempDir()
	d, err := openInbox(root, 8<<20, 80)
	if err != nil {
		t.Fatal(err)
	}
	defer d.close()
	first, err := inboxIdentity(root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := inboxIdentity(root)
	if err != nil || first != second {
		t.Fatal(first, second, err)
	}
	if other, err := openInbox(root, 8<<20, 80); err == nil {
		other.close()
		t.Fatal("two receivers own the same inbox")
	} else if !errors.Is(err, ErrInboxInUse) || (runtime.GOOS != "windows" && !strings.Contains(err.Error(), "holder pid "+strconv.Itoa(os.Getpid()))) {
		t.Fatalf("second open should name the holder: %v", err)
	}
	for i := 0; i < 200; i++ {
		if err = d.put(fmt.Sprintf("/iot/up/t/p/d%d/event", i), []byte(`{}`)); err != nil {
			break
		}
	}
}

func TestDurableReceiptPreservesBytesAndOriginalTime(t *testing.T) {
	root := t.TempDir()
	d, err := openInbox(root, 8<<20, 80)
	if err != nil {
		t.Fatal(err)
	}
	defer d.close()
	topic := "/iot/up/t/p/d/event"
	body := []byte{0xff, 0x00, 'a'}
	if err = d.put(topic, body); err != nil {
		t.Fatal(err)
	}
	var first model.RawMessage
	for _, q := range d.queues {
		raw, found, err := q.Next()
		if err != nil {
			t.Fatal(err)
		}
		if found {
			first = raw
		}
	}
	if first.ReceivedAt <= 0 {
		t.Fatal("receipt time missing")
	}
	time.Sleep(2 * time.Millisecond)
	if err = d.put(topic, body); err != nil {
		t.Fatal(err)
	}
	for _, q := range d.queues {
		raw, found, err := q.Next()
		if err != nil {
			t.Fatal(err)
		}
		if found {
			var got []byte
			if err = json.Unmarshal(raw.Payload, &got); err != nil || !bytes.Equal(got, body) || raw.ReceivedAt != first.ReceivedAt {
				t.Fatal("receipt bytes/time changed", err)
			}
		}
	}
}

func TestInboxReadFailureIsVisibleAndRecovers(t *testing.T) {
	root := t.TempDir()
	d, err := openInbox(root, 8<<20, 80)
	if err != nil {
		t.Fatal(err)
	}
	c := inboxClient(t, d)
	// A queued entry replaced by a non-regular file makes Next fail on every
	// platform. Renaming the queue root fails before the test starts on
	// Windows because its lock file remains open for the lifetime of the queue.
	topic := "/iot/up/t/p/d/event"
	if err = d.put(topic, []byte(`{"v":1}`)); err != nil {
		t.Fatal(err)
	}
	shard := filepath.Join(root, fmt.Sprint(d.shard(topic)))
	entries, err := os.ReadDir(shard)
	if err != nil {
		t.Fatal(err)
	}
	path := ""
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			path = filepath.Join(shard, e.Name())
		}
	}
	if path == "" || os.Remove(path) != nil || os.Mkdir(path, 0700) != nil {
		t.Fatal("cannot replace queued entry")
	}
	d.start(c)
	eventually(t, func() bool { return d.health() != nil })
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return d.health() == nil })
}

// The durable ACK must complete before the Paho delivery callback returns.
// Business processing remains on the asynchronous durable queue.
func TestDurableReceiveAcknowledgesWithinCallback(t *testing.T) {
	d, err := openInbox(t.TempDir(), 8<<20, 8000)
	if err != nil {
		t.Fatal(err)
	}
	c := inboxClient(t, d)
	messages := make([]*receivedMessage, 200)
	for i := range messages {
		messages[i] = &receivedMessage{topic: fmt.Sprintf("/iot/up/t/p/device-%d/property", i), payload: fmt.Appendf(nil, `{"id":"m-%d"}`, i)}
		c.receive(messages[i])
	}
	for _, m := range messages {
		if !m.acked.Load() {
			t.Fatal("delivery callback returned before durable ACK; Paho may close this connection's ACK channel")
		}
	}
	if got := inboxDepth(d); got != len(messages) {
		t.Fatalf("every acknowledged delivery must be on disk, depth=%d", got)
	}
}

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
	if err = repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "test", ProductID: "p", ID: "d", Name: "消防控制器", Status: "ENABLED", SecretHash: "test-inventory-credential", Connector: "MQTT"}); err != nil {
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

// Opt-in against an existing broker only. This never creates a container and
// uses an isolated exact topic and temporary durable inbox. It disconnects only
// the subscriber created by this test while its delivery callback is active.
func TestExistingBrokerDisconnectDuringDurableCallback(t *testing.T) {
	env := os.Getenv("IOT_TEST_EXISTING_MQTT_ENV")
	if env == "" {
		t.Skip("set IOT_TEST_EXISTING_MQTT_ENV to an existing broker environment file")
	}
	if err := config.LoadEnvFile(env); err != nil {
		t.Fatal("read test environment")
	}
	cfg := config.Load()
	manager := auth.New(cfg.JWTSecret)
	topic := fmt.Sprintf("/_torchlink_ack_regression/%d", time.Now().UnixNano())
	credentials := func() (string, string) {
		token, err := manager.IssueWithACL("torchlink-regression", "system", "service", nil, []auth.ACLRule{{Permission: "allow", Action: "all", Topic: topic}}, time.Minute)
		if err != nil {
			t.Error("issue test credential")
		}
		return "torchlink-regression", token
	}
	sub, err := NewDurableWithCredentials(cfg.MQTTBroker, t.TempDir(), credentials)
	if err != nil {
		t.Fatal("subscriber connection failed")
	}
	defer sub.Close()
	pub, err := NewWithCredentials(cfg.MQTTBroker, fmt.Sprintf("receipt-regression-%d", time.Now().UnixNano()), credentials)
	if err != nil {
		t.Fatal("publisher connection failed")
	}
	defer pub.Close()
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var first atomic.Bool
	callback := func(_ mqtt.Client, m mqtt.Message) {
		if first.CompareAndSwap(false, true) {
			entered <- struct{}{}
			<-release
		}
		sub.persist(m)
	}
	subscribe := func() {
		token := sub.client.Subscribe(topic, 1, callback)
		if !token.WaitTimeout(5*time.Second) || token.Error() != nil {
			t.Fatal("test subscription failed")
		}
	}
	subscribe()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err = pub.Publish(ctx, topic, []byte(`{"id":"before-disconnect"}`), 1, false); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-ctx.Done():
		close(release)
		t.Fatal("delivery callback never entered")
	}
	admin := Admin{URL: cfg.EMQXAPIURL, Key: cfg.EMQXAPIKey, Secret: cfg.EMQXAPISecret}
	_, err = admin.request(ctx, "DELETE", "/clients/"+sub.ClientID(), nil, nil)
	// The callback still owns the ACK closure during broker-side disconnection.
	time.Sleep(100 * time.Millisecond)
	close(release)
	if err != nil {
		t.Fatal("disconnect test client", err)
	}
	eventually(t, func() bool { return sub.client.IsConnectionOpen() })
	subscribe()
	if err = pub.Publish(ctx, topic, []byte(`{"id":"after-reconnect"}`), 1, false); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { pending, _, _ := sub.InboxCounts(); return pending >= 2 })
	t.Log("broker disconnected during callback; durable receipt survived and subsequent delivery succeeded")
}

func TestInboxDirectoryIsExclusiveToOneProcess(t *testing.T) {
	root := t.TempDir()
	first, err := openInbox(root, 1<<20, 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = openInbox(root, 1<<20, 100); !errors.Is(err, ErrInboxInUse) {
		t.Fatal("a second owner opened the same inbox", err)
	}
	first.close()
	again, err := openInbox(root, 1<<20, 100)
	if err != nil {
		t.Fatal("inbox not released on close", err)
	}
	again.close()
}

// A message that keeps failing with a retryable error is quarantined after
// maxPendingAge so the rest of its shard is not blocked forever; a permanent
// error is quarantined at once.
func TestInboxQuarantinesStuckHeadAndKeepsShardFlowing(t *testing.T) {
	d, err := openInbox(t.TempDir(), 8<<20, 8000)
	if err != nil {
		t.Fatal(err)
	}
	d.maxPendingAge = 50 * time.Millisecond
	c := inboxClient(t, d)
	topic := "/iot/up/t/p/d/property"
	var delivered sync.Map
	c.routes["standard"] = func(_ context.Context, _ string, payload []byte) error {
		switch string(payload) {
		case `{"id":"stuck"}`:
			return errors.New("transient failure that never clears")
		case `{"id":"gone"}`:
			return model.Permanent(errors.New("gateway g is not registered"))
		}
		delivered.Store(string(payload), true)
		return nil
	}
	for _, body := range []string{`{"id":"stuck"}`, `{"id":"gone"}`, `{"id":"next"}`} {
		if err = d.put(topic, []byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	d.start(c)
	eventually(t, func() bool { _, ok := delivered.Load(`{"id":"next"}`); return ok })
	if _, rejected, _ := c.InboxCounts(); rejected != 2 {
		t.Fatalf("stuck and permanent entries should both be quarantined, rejected=%d", rejected)
	}
}
