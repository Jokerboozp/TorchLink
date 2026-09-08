package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	mqttadapter "iot-platform/internal/adapters/mqtt"
	"iot-platform/internal/auth"
	"iot-platform/internal/config"
	"iot-platform/internal/connector"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
	"iot-platform/internal/parser"
)

// Opt-in live broker check. All business storage is temporary, clients use clean
// sessions and messages are non-retained under a unique test tenant.
func TestStandardMQTTLiveBroker(t *testing.T) {
	broker, secret := os.Getenv("IOT_TEST_MQTT_BROKER"), os.Getenv("IOT_TEST_MQTT_JWT_SECRET")
	if broker == "" || secret == "" {
		t.Skip("configure IOT_TEST_MQTT_BROKER and IOT_TEST_MQTT_JWT_SECRET")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	tenant := "integration-" + randomHex(8)
	username := tenant + "-platform"
	token, e := auth.New(secret).IssueWithACL(username, tenant, "service", nil, []auth.ACLRule{{Permission: "allow", Action: "subscribe", Topic: "/iot/up/#"}, {Permission: "allow", Action: "publish", Topic: "/iot/down/" + tenant + "/#"}}, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	platform, e := mqttadapter.New(broker, username, token, username)
	if e != nil {
		t.Fatal("test platform MQTT connection failed", e)
	}
	defer platform.Close()
	repo := memory.NewRepository()
	root := t.TempDir()
	archive, e := local.NewArchive(root)
	if e != nil {
		t.Fatal(e)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(root), log)
	if e = engine.Start(ctx); e != nil {
		t.Fatal(e)
	}
	cfg := config.Load()
	cfg.JWTSecret = secret
	cfg.DataDir = root
	srv := New(cfg, engine, metrics.New(), log)
	srv.SetMQTTHealth(platform.Health)
	srv.SetDeviceOperations(platform.Publish, nil)
	request := onboarding.Request{ProductID: "product", ProductName: "temporary MQTT test", DeviceID: "device", Name: "test device", Type: connector.MQTT, MessageKind: "property", Payload: json.RawMessage(`{"id":"preview","timestamp":1788850000000,"data":{"temperature":20}}`)}
	preview, e := srv.onboarding.Test(ctx, tenant, request)
	if e != nil || !preview.Success {
		t.Fatal("onboarding preview failed", e)
	}
	request.TestToken = preview.TestToken
	created, e := srv.onboarding.Create(ctx, tenant, request)
	if e != nil {
		t.Fatal(e)
	}
	failures := make(chan error, 4)
	if e = platform.SubscribeStandard(func(c context.Context, tnt, p, d, kind string, payload []byte) error {
		if tnt != tenant {
			return nil
		}
		raw, e := srv.onboarding.PrepareStandard(c, tnt, p, d, kind, "MQTT", payload)
		if e == nil {
			_, _, e = engine.IngestRaw(c, raw)
		}
		if e != nil {
			select {
			case failures <- e:
			default:
			}
		}
		return e
	}); e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("POST", "/api/v1/device-mqtt/token", nil)
	r.Header.Set("X-Device-Key", created.Credential.AccessKey)
	r.Header.Set("X-Device-Secret", created.Credential.Secret)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("device token request failed", w.Code)
	}
	var credentials struct {
		Username string `json:"username"`
		Token    string `json:"token"`
	}
	if e = json.Unmarshal(w.Body.Bytes(), &credentials); e != nil {
		t.Fatal(e)
	}
	device := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(broker).SetClientID(tenant + "-device").SetUsername(credentials.Username).SetPassword(credentials.Token).SetCleanSession(true).SetAutoReconnect(false).SetConnectRetry(false).SetOrderMatters(false))
	wait := func(token mqtt.Token) {
		t.Helper()
		if !token.WaitTimeout(8 * time.Second) {
			t.Fatal("MQTT operation timed out")
		}
		if token.Error() != nil {
			t.Fatal("MQTT operation failed", token.Error())
		}
	}
	wait(device.Connect())
	defer device.Disconnect(100)
	prefix := fmt.Sprintf("/iot/up/%s/product/device/", tenant)
	wait(device.Subscribe(fmt.Sprintf("/iot/down/%s/product/device/command", tenant), 1, func(_ mqtt.Client, m mqtt.Message) {
		var command struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(m.Payload(), &command) != nil {
			return
		}
		body, _ := json.Marshal(map[string]any{"id": "reply-1", "timestamp": time.Now().UnixMilli(), "data": map[string]any{"commandId": command.ID, "success": true}})
		device.Publish(prefix+"command-reply", 1, false, body)
	}))
	property := []byte(fmt.Sprintf(`{"id":"property-1","timestamp":%d,"data":{"temperature":26.5}}`, time.Now().UnixMilli()))
	wait(device.Publish(prefix+"property", 1, false, property))
	until := func(check func() bool) {
		t.Helper()
		for !check() {
			select {
			case e := <-failures:
				t.Fatal(e)
			case <-ctx.Done():
				t.Fatal("MQTT integration deadline exceeded")
			case <-time.After(20 * time.Millisecond):
			}
		}
	}
	until(func() bool {
		m, e := repo.GetLatestMessage(ctx, tenant, "device")
		return e == nil && m.MessageType == model.PropertyReport && m.Properties["temperature"] == 26.5
	})
	adminToken, _ := srv.auth.Issue("test", tenant, "admin", nil, time.Minute)
	r = httptest.NewRequest("POST", "/api/v1/device-registry/device/commands", bytes.NewBufferString(`{"id":"command-1","type":"test","data":{}}`))
	r.Header.Set("Authorization", "Bearer "+adminToken)
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, r)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	until(func() bool {
		commands, _, e := repo.ListDeviceCommands(ctx, tenant, "device", 20, 0)
		return e == nil && len(commands) == 1 && commands[0].Status == "SUCCEEDED"
	})
	raw, e := onboarding.StandardRaw(tenant, "product", "device", "property", "MQTT", property)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = repo.GetRawIndex(ctx, tenant, raw.MessageID); e != nil {
		t.Fatal("live property was not archived", e)
	}
	t.Log("live MQTT: onboarding, device JWT, property archive, command dispatch and Raw command reply passed")
}
