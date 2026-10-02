package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	mqttadapter "iot-platform/internal/adapters/mqtt"
	"iot-platform/internal/auth"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
	"iot-platform/internal/protocolruntime"
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
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(root), log)
	if e = engine.Start(ctx); e != nil {
		t.Fatal(e)
	}
	cfg := config.Load()
	cfg.JWTSecret = secret
	cfg.DataDir = root
	srv := New(cfg, engine, metrics.New(), log)
	srv.SetMQTTHealth(platform.Probe)
	srv.SetDeviceOperations(platform.Publish, nil)
	draft := &onboarding.NewProduct{ID: "product", Name: "temporary MQTT test", ProtocolPackageID: onboarding.StandardPackageID, Transport: "MQTT"}
	check, e := srv.onboarding.Preflight(ctx, tenant, "", draft, onboarding.PublicAddresses{MQTT: true})
	if e != nil || !check.Ready || check.Checks[len(check.Checks)-1].State != "passed" {
		t.Fatal("onboarding preflight failed", check.Checks, e)
	}
	created, e := srv.onboarding.Enroll(ctx, tenant, onboarding.EnrollRequest{RequestID: "mqtt-live", NewProduct: draft, Device: onboarding.EnrollDevice{ID: "device", Name: "test device"}, Connection: onboarding.EnrollConnection{Mode: onboarding.ModeStandard}})
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
	wait := func(t *testing.T, token mqtt.Token) {
		t.Helper()
		if !token.WaitTimeout(8 * time.Second) {
			t.Fatal("MQTT operation timed out")
		}
		if token.Error() != nil {
			t.Fatal("MQTT operation failed", token.Error())
		}
	}
	wait(t, device.Connect())
	defer device.Disconnect(100)
	prefix := fmt.Sprintf("/iot/up/%s/product/device/", tenant)
	wait(t, device.Subscribe(fmt.Sprintf("/iot/down/%s/product/device/command", tenant), 1, func(_ mqtt.Client, m mqtt.Message) {
		var command struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(m.Payload(), &command) != nil {
			return
		}
		body, _ := json.Marshal(map[string]any{"id": "reply-1", "timestamp": time.Now().UnixMilli(), "data": map[string]any{"commandId": command.ID, "success": true}})
		device.Publish(prefix+"command-reply", 1, false, body)
	}))
	property := fmt.Appendf(nil, `{"id":"property-1","timestamp":%d,"data":{"temperature":26.5}}`, time.Now().UnixMilli())
	wait(t, device.Publish(prefix+"property", 1, false, property))
	until := func(t *testing.T, check func() bool) {
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
	until(t, func() bool {
		m, e := repo.GetLatestMessage(ctx, tenant, "device")
		return e == nil && m.MessageType == model.PropertyReport && m.Properties["temperature"] == 26.5
	})
	adminToken, _ := srv.auth.Issue("test", tenant, "admin", nil, time.Minute)
	r = httptest.NewRequest("POST", "/api/v1/device-registry/device/commands", bytes.NewBufferString(`{"confirmed":true,"id":"command-1","type":"test","data":{}}`))
	r.Header.Set("Authorization", "Bearer "+adminToken)
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, r)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	until(t, func() bool {
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
	// A clean-session reconnect must still accept the device JWT and resume
	// ingress. Exercise rule activation/recovery after the transport reconnect.
	device.Disconnect(100)
	wait(t, device.Connect())
	if e = repo.SaveRule(ctx, model.AlarmRule{TenantID: tenant, ID: "temperature-rule", ProductID: "product", Name: "temporary threshold", Enabled: true, AlarmType: "HIGH_TEMPERATURE", Level: "HIGH", Conditions: []model.RuleCondition{{Field: "temperature", Operator: ">", Value: 80}}, Recovery: []model.RuleCondition{{Field: "temperature", Operator: "<", Value: 70}}}); e != nil {
		t.Fatal(e)
	}
	raise := fmt.Sprintf(`{"id":"alarm-property","timestamp":%d,"data":{"temperature":85}}`, time.Now().UnixMilli())
	wait(t, device.Publish(prefix+"property", 1, false, raise))
	until(t, func() bool {
		items, e := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: tenant, DeviceID: "device", Status: "ACTIVE", Limit: 10})
		return e == nil && len(items) == 1 && items[0].RuleID == "temperature-rule"
	})
	// Retransmit the same application message id before recovery.
	wait(t, device.Publish(prefix+"property", 1, false, raise))
	recoverBody := fmt.Sprintf(`{"id":"recovery-property","timestamp":%d,"data":{"temperature":25}}`, time.Now().UnixMilli())
	wait(t, device.Publish(prefix+"property", 1, false, recoverBody))
	until(t, func() bool {
		items, e := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: tenant, DeviceID: "device", Status: "RECOVERED", Limit: 10})
		return e == nil && len(items) == 1 && items[0].TriggerCount == 1
	})
	for _, body := range []string{raise, recoverBody} {
		record, e := onboarding.StandardRaw(tenant, "product", "device", "property", "MQTT", []byte(body))
		if e != nil {
			t.Fatal(e)
		}
		if _, e = repo.GetRawIndex(ctx, tenant, record.MessageID); e != nil {
			t.Fatal("rule input bypassed archive", e)
		}
	}
	t.Log("live MQTT: clean-session reconnect, alarm activation, duplicate message deduplication and recovery passed")
	t.Log("live MQTT: onboarding, device JWT, property archive, command dispatch and Raw command reply passed")
	t.Run("AuthenticationAndACL", func(t *testing.T) {
		reject := func(user, password string) {
			client := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(broker).SetClientID(tenant + "-deny-" + randomHex(4)).SetUsername(user).SetPassword(password).SetAutoReconnect(false).SetConnectRetry(false))
			defer client.Disconnect(100)
			attempt := client.Connect()
			if !attempt.WaitTimeout(5 * time.Second) {
				t.Fatal("authentication rejection timed out")
			}
			code := attempt.(*mqtt.ConnectToken).ReturnCode()
			if attempt.Error() == nil || (code != 4 && code != 5) {
				t.Fatalf("authentication rejection was not confirmed: CONNACK %d", code)
			}
		}
		reject(credentials.Username, "invalid-jwt")
		if os.Getenv("IOT_TEST_MQTT_STRICT_IDENTITY") == "true" {
			reject(username, credentials.Token)
		} else {
			t.Log("username binding check not executed: set IOT_TEST_MQTT_STRICT_IDENTITY=true against updated isolated Broker")
		}
		for _, topic := range []string{"/iot/down/" + tenant + "/product/device/shadow", "/iot/down/" + tenant + "/product/other/command", "/iot/down/other-" + tenant + "/product/device/command", "/iot/up/#", "/iot/down/" + tenant + "/product/other/shadow", "/iot/down/other-" + tenant + "/product/device/shadow"} {
			op := device.Subscribe(topic, 1, func(mqtt.Client, mqtt.Message) {})
			if !op.WaitTimeout(5 * time.Second) {
				t.Fatal("ACL subscription result timed out")
			}
			if op.(*mqtt.SubscribeToken).Result()[topic] != 128 {
				t.Fatal("unauthorized subscription not rejected", topic)
			}
		}
		forbidden := []string{"/iot/up/" + tenant + "/product/device/shadow-get", "/iot/up/" + tenant + "/product/other/property", "/iot/up/other-" + tenant + "/product/device/property", "/external/raw/" + tenant + "/product/device", "/iot/up/" + tenant + "/product/other/shadow-get", "/iot/up/other-" + tenant + "/product/device/shadow-get"}
		acl := []auth.ACLRule{}
		for _, topic := range append(forbidden, prefix+"property") {
			acl = append(acl, auth.ACLRule{Permission: "allow", Action: "subscribe", Topic: topic})
		}
		observerName := tenant + "-observer"
		jwt, _ := srv.auth.IssueWithACL(observerName, tenant, "service", nil, acl, time.Minute)
		observer := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(broker).SetClientID(observerName).SetUsername(observerName).SetPassword(jwt).SetAutoReconnect(false))
		wait(t, observer.Connect())
		defer observer.Disconnect(100)
		received := make(chan string, 8)
		for _, topic := range append(forbidden, prefix+"property") {
			wait(t, observer.Subscribe(topic, 1, func(_ mqtt.Client, m mqtt.Message) { received <- m.Topic() }))
		}
		for _, topic := range forbidden {
			wait(t, device.Publish(topic, 1, false, `{"id":"acl-denied","timestamp":1000,"data":{"x":1}}`))
		}
		// A valid canary proves the observer and publication path are functioning.
		wait(t, device.Publish(prefix+"property", 1, false, `{"id":"acl-canary","timestamp":1000,"data":{"x":1}}`))
		select {
		case topic := <-received:
			if topic != prefix+"property" {
				t.Fatal("unauthorized publication delivered", topic)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("allowed canary was not received")
		}
		select {
		case topic := <-received:
			t.Fatal("unexpected forbidden publication", topic)
		case <-time.After(300 * time.Millisecond):
		}
		t.Log("bad credentials and forbidden subscriptions rejected; cross-device/cross-tenant/raw-topic publications blocked; valid canary received")
	})
	t.Run("CredentialRevocation", func(t *testing.T) {
		base, key, apiSecret := os.Getenv("IOT_TEST_EMQX_API_URL"), os.Getenv("IOT_TEST_EMQX_API_KEY"), os.Getenv("IOT_TEST_EMQX_API_SECRET")
		if base == "" || key == "" || apiSecret == "" {
			t.Skip("configure IOT_TEST_EMQX_API_URL, IOT_TEST_EMQX_API_KEY and IOT_TEST_EMQX_API_SECRET")
		}
		admin := &mqttadapter.Admin{URL: base, Key: key, Secret: apiSecret}
		srv.SetDeviceOperations(platform.Publish, admin.RevokeUsername)
		cleanupBan := func(user string) {
			t.Cleanup(func() {
				req, e := http.NewRequest("DELETE", strings.TrimRight(base, "/")+"/api/v5/banned/username/"+url.PathEscape(user), nil)
				if e != nil {
					t.Error(e)
					return
				}
				req.SetBasicAuth(key, apiSecret)
				client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
				res, e := client.Do(req)
				if e != nil {
					t.Error("test ban cleanup request failed")
					return
				}
				res.Body.Close()
				if res.StatusCode != 204 && res.StatusCode != 404 {
					t.Errorf("test ban cleanup HTTP %d", res.StatusCode)
				}
			})
		}
		cleanupBan(created.Credential.AccessKey)
		next, revocation, e := srv.onboarding.ChangeCredential(ctx, tenant, "device", true)
		if e != nil || revocation.Status != "REVOKED" {
			t.Fatal("live credential rotation/revocation failed", revocation.Status, revocation.LastError, e)
		}
		until(t, func() bool { return !device.IsConnected() })
		if e = admin.RevokeUsername(ctx, created.Credential.AccessKey); e != nil {
			t.Fatal("repeated broker revocation failed", e)
		}
		rejectConnection := func(user, jwt string) {
			t.Helper()
			stale := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(broker).SetClientID(tenant + "-rejected-" + randomHex(4)).SetUsername(user).SetPassword(jwt).SetCleanSession(true).SetAutoReconnect(false).SetConnectRetry(false))
			defer stale.Disconnect(100)
			attempt := stale.Connect()
			if !attempt.WaitTimeout(5 * time.Second) {
				t.Fatal("stale JWT rejection was not confirmed")
			}
			if attempt.Error() == nil || attempt.(*mqtt.ConnectToken).ReturnCode() != 5 {
				t.Fatalf("expected broker CONNACK 5 for revoked JWT; got code %d", attempt.(*mqtt.ConnectToken).ReturnCode())
			}
		}
		rejectConnection(credentials.Username, credentials.Token)
		issue := func(c model.DeviceCredential) *httptest.ResponseRecorder {
			r := httptest.NewRequest("POST", "/api/v1/device-mqtt/token", nil)
			r.Header.Set("X-Device-Key", c.AccessKey)
			r.Header.Set("X-Device-Secret", c.Secret)
			w := httptest.NewRecorder()
			srv.Handler().ServeHTTP(w, r)
			return w
		}
		if issue(created.Credential).Code != 401 {
			t.Fatal("old device secret accepted after rotation")
		}
		tokenResponse := issue(next)
		if tokenResponse.Code != 200 {
			t.Fatal("new credentials rejected", tokenResponse.Code)
		}
		var freshCredentials struct {
			Username string `json:"username"`
			Token    string `json:"token"`
		}
		if e = json.Unmarshal(tokenResponse.Body.Bytes(), &freshCredentials); e != nil {
			t.Fatal(e)
		}
		fresh := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(broker).SetClientID(tenant + "-fresh").SetUsername(freshCredentials.Username).SetPassword(freshCredentials.Token).SetCleanSession(true).SetAutoReconnect(false).SetConnectRetry(false))
		wait(t, fresh.Connect())
		defer fresh.Disconnect(100)
		body := fmt.Sprintf(`{"id":"property-2","timestamp":%d,"data":{"temperature":30}}`, time.Now().UnixMilli())
		wait(t, fresh.Publish(prefix+"property", 1, false, body))
		until(t, func() bool {
			m, e := repo.GetLatestMessage(ctx, tenant, "device")
			return e == nil && m.MessageType == model.PropertyReport && m.Properties["temperature"] == float64(30)
		})
		cleanupBan(next.AccessKey)
		_, disabled, e := srv.onboarding.ChangeCredential(ctx, tenant, "device", false)
		if e != nil || disabled.Status != "REVOKED" {
			t.Fatal("live disable failed", disabled.Status, e)
		}
		until(t, func() bool { return !fresh.IsConnected() })
		rejectConnection(freshCredentials.Username, freshCredentials.Token)
		if issue(next).Code != 401 {
			t.Fatal("disabled device secret accepted")
		}
		if e = platform.Health(ctx); e != nil {
			t.Fatal("unrelated platform MQTT connection was affected", e)
		}
		t.Log("live EMQX: rotation and disable disconnect sessions; stale JWT reconnect rejected; new credential ingress succeeds; repeated revocation succeeds; unrelated connection remains healthy")
	})
}

func TestMQTTSharedGatewaySubscription(t *testing.T) {
	broker, secret := os.Getenv("IOT_TEST_MQTT_BROKER"), os.Getenv("IOT_TEST_MQTT_JWT_SECRET")
	if broker == "" || secret == "" {
		t.Skip("MQTT integration environment is not configured")
	}
	suffix := randomHex(8)
	tenant := "shared-" + suffix
	topic := "/iot/up/" + tenant + "/product/device/property"
	manager := auth.New(secret)
	received := make(chan string, 8)
	clients := []*mqttadapter.Client{}
	for _, name := range []string{"first", "second"} {
		username := "shared-" + name + "-" + suffix
		token, err := manager.IssueWithACL(username, tenant, "service", nil, []auth.ACLRule{{Permission: "allow", Action: "subscribe", Topic: "/iot/up/#"}, {Permission: "allow", Action: "publish", Topic: topic}}, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		client, err := mqttadapter.New(broker, username, token, username)
		if err != nil {
			t.Fatal(err)
		}
		defer client.Close()
		if err = client.ConfigureSharedSubscriptions("test-" + suffix); err != nil {
			t.Fatal(err)
		}
		if err = client.SubscribeStandard(func(ctx context.Context, tnt, product, device, kind string, payload []byte) error {
			if tnt == tenant {
				received <- name
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		clients = append(clients, client)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := clients[0].Publish(ctx, topic, []byte(`{"id":"one","timestamp":1788850000000,"data":{"temperature":42}}`), 1, false); err != nil {
		t.Fatal(err)
	}
	select {
	case <-received:
	case <-ctx.Done():
		t.Fatal("shared subscription did not receive message")
	}
	select {
	case <-received:
		t.Fatal("one publish was delivered to both gateway workers")
	case <-time.After(300 * time.Millisecond):
	}
}

// This probe does not publish or subscribe to any business topic.
func TestMQTTBrokerRejectsInvalidJWT(t *testing.T) {
	broker := os.Getenv("IOT_TEST_MQTT_BROKER")
	if broker == "" {
		t.Skip("IOT_TEST_MQTT_BROKER not configured")
	}
	id := "auth-probe-" + randomHex(8)
	client := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(broker).SetClientID(id).SetUsername(id).SetPassword("invalid-jwt").SetCleanSession(true).SetAutoReconnect(false).SetConnectRetry(false).SetConnectTimeout(time.Second))
	defer client.Disconnect(100)
	token := client.Connect()
	if !token.WaitTimeout(2 * time.Second) {
		t.Fatal("broker did not return an authentication verdict")
	}
	code := token.(*mqtt.ConnectToken).ReturnCode()
	if token.Error() == nil || (code != 4 && code != 5) {
		t.Fatalf("invalid JWT was not rejected: CONNACK %d", code)
	}
}

func TestMQTTBrokerBindsJWTUsername(t *testing.T) {
	broker, secret := os.Getenv("IOT_TEST_MQTT_BROKER"), os.Getenv("IOT_TEST_MQTT_JWT_SECRET")
	if broker == "" || secret == "" {
		t.Skip("MQTT integration environment not configured")
	}
	id := "identity-probe-" + randomHex(8)
	jwt, err := auth.New(secret).IssueWithACL(id, id, "device", nil, nil, time.Minute)
	if err != nil {
		t.Fatal("could not issue probe JWT")
	}
	client := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(broker).SetClientID(id).SetUsername(id + "-wrong").SetPassword(jwt).SetCleanSession(true).SetAutoReconnect(false).SetConnectRetry(false).SetConnectTimeout(time.Second))
	defer client.Disconnect(100)
	token := client.Connect()
	if !token.WaitTimeout(2 * time.Second) {
		t.Fatal("broker did not return an authentication verdict")
	}
	code := token.(*mqtt.ConnectToken).ReturnCode()
	if token.Error() == nil || (code != 4 && code != 5) {
		t.Fatalf("JWT username mismatch was not rejected: CONNACK %d", code)
	}
}

// Without broker revocation a standard device token must stay short because
// expiry is the only way to cut off a revoked credential; with revocation it
// can last long enough that devices are not reconnected every five minutes.
func TestStandardDeviceTokenTTLFollowsRevocation(t *testing.T) {
	s := &Server{cfg: config.Config{MQTTDeviceTokenTTL: 12 * time.Hour}, onboarding: onboarding.New(memory.NewRepository(), nil, "", nil)}
	if got := s.standardDeviceTokenTTL(); got != 5*time.Minute {
		t.Fatalf("without revocation: %s", got)
	}
	s.onboarding.RevokeUsername = func(context.Context, string) error { return nil }
	if got := s.standardDeviceTokenTTL(); got != 12*time.Hour {
		t.Fatalf("with revocation: %s", got)
	}
}

func TestModbusOnboardingRuntimeChain(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.SetDeadline(time.Now().Add(time.Second))
			request := make([]byte, 12)
			if _, err = io.ReadFull(conn, request); err == nil {
				_, _ = conn.Write([]byte{request[0], request[1], 0, 0, 0, 5, request[6], 3, 2, 0, 42})
			}
			_ = conn.Close()
		}
	}()
	repo := memory.NewRepository()
	root := t.TempDir()
	archive, err := local.NewArchive(root)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(root), log)
	if err = engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.DataDir, cfg.JWTSecret, cfg.ModbusAllowedCIDRs = root, "isolated-modbus-chain-signing-key", []string{"127.0.0.0/8"}
	srv := New(cfg, engine, metrics.New(), log)
	token, err := srv.auth.Issue("tester", "tenant", "admin", nil, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path string, body any) *httptest.ResponseRecorder {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(payload))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		out := httptest.NewRecorder()
		srv.Handler().ServeHTTP(out, req)
		return out
	}
	// Modbus point tables are published protocol versions; the wizard only adds devices.
	table, _, err := core.ParseModbusPointTable("points.csv", []byte("name,functionCode,address,addressNotation,dataType,scale,bit\ntemperature,3,0,zero_based,uint16,1,\ninput6,3,0,zero_based,bits,1,5\n"), 1)
	if err != nil {
		t.Fatal(err)
	}
	blocks, err := core.CompileModbusReadBlocks(table.Points)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	release := model.ProtocolRelease{TenantID: "tenant", ProtocolID: "meter", Version: "1", Transport: "MODBUS_TCP", PayloadFormat: "hex", ParserType: parser.ModbusTCPParserName, Status: "PUBLISHED", PointTableVersion: "1", CreatedAt: now, PublishedAt: now, Config: map[string]any{"points": table.Points, "blocks": blocks}}
	table.TenantID, table.ProtocolID, table.Version, table.CreatedAt = "tenant", "meter", "1", now
	if err = repo.CreatePointTableRelease(ctx, table); err != nil {
		t.Fatal(err)
	}
	if err = repo.CreateProtocolRelease(ctx, release); err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveProduct(ctx, model.Product{TenantID: "tenant", ID: "modbus-product", Name: "模拟温度产品", Status: "ENABLED", ProtocolPackageID: "meter@1"}); err != nil {
		t.Fatal(err)
	}
	unit := 1
	q := onboarding.EnrollRequest{Trial: true, RequestID: "req-modbus", ProductID: "modbus-product", Device: onboarding.EnrollDevice{ID: "modbus-device", Name: "模拟温度设备"}, Connection: onboarding.EnrollConnection{Mode: onboarding.ModePoll, Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, UnitID: &unit, TimeoutMs: 500}}
	if preflight := call("GET", "/api/v1/onboarding/preflight?productId=modbus-product", nil); preflight.Code != 200 || !bytes.Contains(preflight.Body.Bytes(), []byte(`"mode":"poll"`)) {
		t.Fatal("preflight", preflight.Code, preflight.Body.String())
	}
	created := call("POST", "/api/v1/onboarding", q)
	if created.Code != 201 {
		t.Fatal("save", created.Code, created.Body.String())
	}
	recovered := call("POST", "/api/v1/onboarding", q)
	if recovered.Code != 200 || !bytes.Contains(recovered.Body.Bytes(), []byte(`"reused":true`)) {
		t.Fatal("Modbus onboarding recovery failed", recovered.Code)
	}
	for _, response := range []*httptest.ResponseRecorder{created, recovered} {
		for _, field := range []string{`"credential"`, `"accessKey"`, `"clientId"`, `"username"`} {
			if bytes.Contains(response.Body.Bytes(), []byte(field)) {
				t.Fatalf("Modbus onboarding returned %s", field)
			}
		}
	}
	stored, err := repo.GetManagedDevice(ctx, "tenant", q.Device.ID)
	if err != nil || stored.SecretHash != "" || stored.AccessKey == "" {
		t.Fatal("Modbus must persist an internal identity without a secret", err)
	}
	connection := func() map[string]any {
		result := call("GET", "/api/v1/device-registry/modbus-device/connection", nil)
		var data map[string]any
		if result.Code != 200 || json.Unmarshal(result.Body.Bytes(), &data) != nil {
			t.Fatal("connection", result.Code)
		}
		return data
	}
	if connection()["ingest"].(map[string]any)["rawReceived"] != false {
		t.Fatal("saving the configuration reported business data")
	}
	runtime := protocolruntime.New(repo, func(ctx context.Context, raw model.RawMessage) error {
		_, _, err := engine.IngestRaw(ctx, raw)
		return err
	}, log, "127.0.0.0/8")
	runtime.Start(ctx)
	wait := func(check func() bool) {
		t.Helper()
		for !check() {
			select {
			case <-ctx.Done():
				t.Fatal("runtime chain timed out")
			case <-time.After(20 * time.Millisecond):
			}
		}
	}
	wait(func() bool { return connection()["ingest"].(map[string]any)["parsed"] == true })
	records, count, err := repo.ListDeviceMessages(ctx, "tenant", "modbus-device", model.PropertyReport, 10, 0)
	if err != nil || count < 1 || records[0].Properties["temperature"] != float64(42) || records[0].Properties["input6"] != true {
		t.Fatal("missing parsed simulator value", count, err)
	}
	index, err := repo.GetRawIndex(ctx, "tenant", records[0].RawMessageID)
	if err != nil || index.ParseAttemptedAt == 0 || index.ParseError != "" {
		t.Fatal("missing raw parse evidence", err)
	}
	_ = listener.Close()
	wait(func() bool { return connection()["profile"].(map[string]any)["runtimeStatus"] == "ERROR" })
	if connection()["ingest"].(map[string]any)["parsed"] != true {
		t.Fatal("failed polling removed previous evidence")
	}
}

// Real uploaded Go functions, authenticated APIs, TCP sockets, archive, parser,
// registration and command routing. Bytes below are a teaching test protocol.
func TestTCPParentChildSourceChain(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	root := protocolDataDir(t)
	repo := memory.NewRepository()
	archive, err := local.NewArchive(root)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(root), log)
	if err = engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.DataDir = root
	api := New(cfg, engine, metrics.New(), log)
	ingested := make(chan model.RawMessage, 64)
	listeners := protocolruntime.NewListeners(repo, root, func(ctx context.Context, raw model.RawMessage) error {
		_, _, e := engine.IngestRaw(ctx, raw)
		if e == nil {
			ingested <- raw
		}
		return e
	}, log)
	listeners.SetConnectionReporter(engine.ReportConnection)
	api.SetProtocolListeners(listeners)
	token, _ := api.auth.Issue("tester", "tenant", "operator", nil, time.Hour)
	other, _ := api.auth.Issue("tester", "other", "operator", nil, time.Hour)
	request := func(method, path, auth string, body any, want int) *httptest.ResponseRecorder {
		t.Helper()
		data, _ := json.Marshal(body)
		req := httptest.NewRequest(method, path, bytes.NewReader(data))
		req.Header.Set("Authorization", "Bearer "+auth)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		api.Handler().ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("%s %s: %d want %d: %s", method, path, w.Code, want, w.Body.String())
		}
		return w
	}
	for _, id := range []string{"parent", "sensor"} {
		repo.SaveProduct(ctx, model.Product{TenantID: "tenant", ID: id, Name: id, Status: "ENABLED"})
	}
	upload := func(id, source string) {
		t.Helper()
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		f, _ := form.CreateFormFile("file", "protocol.go")
		f.Write([]byte(source))
		form.WriteField("productId", id)
		form.WriteField("publish", "true")
		form.WriteField("version", "1")
		form.Close()
		req := httptest.NewRequest("POST", "/api/v2/protocols/"+id+"/source-releases", &body)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", form.FormDataContentType())
		w := httptest.NewRecorder()
		api.Handler().ServeHTTP(w, req)
		if w.Code != 201 {
			t.Fatalf("upload %s: %d %s", id, w.Code, w.Body.String())
		}
	}
	upload("sensor", tcpChildSource)
	upload("parent", tcpParentSource)
	profiles := []model.DeviceAccessProfile{}
	peers := []net.Conn{}
	for i, mode := range []string{"listen", "dial"} {
		socket, e := net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		p := model.DeviceAccessProfile{ID: mode, TenantID: "tenant", ProductID: "parent", ProtocolID: "parent", ProtocolVersion: "1", Mode: "listener", Network: "tcp", ConnectionMode: mode, Host: "127.0.0.1", Port: socket.Addr().(*net.TCPAddr).Port, Enabled: true, AutoRegister: true, TimeoutMs: 3000, ChildProducts: []model.ChildProductBinding{{Type: "smoke", ProductID: "sensor"}}}
		if mode == "listen" {
			socket.Close()
		} else {
			defer socket.Close()
			p.DeviceID = fmt.Sprintf("main-%d", i+1)
			repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "tenant", ID: p.DeviceID, Name: p.DeviceID, ProductID: "parent", Status: "ENABLED", DeviceRole: "DIRECT", AccessKey: p.DeviceID})
		}
		request("POST", "/api/v2/device-access-profiles", token, p, 201)
		profiles = append(profiles, p)
		if mode == "listen" {
			listeners.Start(ctx)
		}
		var peer net.Conn
		if mode == "listen" {
			for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
				peer, e = net.DialTimeout("tcp", net.JoinHostPort(p.Host, fmt.Sprint(p.Port)), 100*time.Millisecond)
				if e == nil {
					break
				}
			}
		} else {
			socket.(*net.TCPListener).SetDeadline(time.Now().Add(4 * time.Second))
			peer, e = socket.Accept()
		}
		if e != nil || peer == nil {
			t.Fatal("connection", e)
		}
		defer peer.Close()
		peers = append(peers, peer)
		peer.SetDeadline(time.Now().Add(10 * time.Second))
		// Child information before the registration handshake must not be ACKed.
		if mode == "listen" {
			bad, e := net.Dial("tcp", peer.RemoteAddr().String())
			if e != nil {
				t.Fatal(e)
			}
			bad.SetDeadline(time.Now().Add(time.Second))
			bad.Write([]byte{1, 0, 9})
			buf := make([]byte, 1)
			if n, _ := bad.Read(buf); n != 0 {
				t.Fatal("bad credential ACKed")
			}
			bad.Close()
			if _, e = repo.GetManagedDevice(ctx, "tenant", "main-9"); e == nil {
				t.Fatal("bad credential registered")
			}
		}
		id := byte(i + 1)
		peer.Write([]byte{1, 0x5a, id})
		reply := make([]byte, 1)
		if _, e = io.ReadFull(peer, reply); e != nil || reply[0] != 0x81 {
			t.Fatal("register reply", reply, e)
		}
		// Same child address under two different parents must create different rows.
		peer.Write([]byte{2, id, 7, 42})
		if _, e = io.ReadFull(peer, reply); e != nil || reply[0] != 0x82 {
			t.Fatal("child ACK", reply, e)
		}
		parentID := fmt.Sprintf("main-%d", id)
		childID := model.ChildDeviceID("tenant", parentID, "7")
		child, e := repo.GetManagedDevice(ctx, "tenant", childID)
		if e != nil || child.GatewayID != parentID || child.ProductID != "sensor" {
			t.Fatal(child, e)
		}
		message, e := repo.GetLatestMessage(ctx, "tenant", childID)
		if e != nil || message.Properties["temperature"] != float64(42) {
			t.Fatal(message, e)
		}
		// Duplicate registration updates the same child; the state is not inherited from parent.
		peer.Write([]byte{2, id, 7, 43})
		if _, e = io.ReadFull(peer, reply); e != nil {
			t.Fatal(e)
		}
		children, total, e := repo.ListManagedDeviceChildren(ctx, "tenant", parentID, 20, 0)
		if e != nil || len(children) != 1 || total != 1 {
			t.Fatal(children, total, e)
		}
		result := request("GET", "/api/v1/device-registry/"+parentID+"/children", token, nil, 200)
		if !strings.Contains(result.Body.String(), childID) || !strings.Contains(result.Body.String(), `"protocolId":"sensor"`) {
			t.Fatal(result.Body.String())
		}
		request("GET", "/api/v1/device-registry/"+parentID+"/children", other, nil, 404)
		request("GET", "/api/v2/products/sensor/protocol-binding", other, nil, 404)
		request("GET", "/api/v1/device-registry/"+childID+"/connection", token, nil, 200)
		// A real child codec creates the inner command; the main codec wraps it.
		done := make(chan error, 1)
		go func() {
			q := make([]byte, 4)
			_, e := io.ReadFull(peer, q)
			if e == nil && !bytes.Equal(q, []byte{5, id, 7, 0x44}) {
				e = fmt.Errorf("wrong child command %x", q)
			}
			if e == nil && mode == "listen" {
				release, getErr := repo.GetProtocolRelease(ctx, "tenant", "sensor", "1")
				e = getErr
				if e == nil {
					release.Version = "2"
					e = repo.CreateProtocolRelease(ctx, release)
				}
				if e == nil {
					e = repo.SaveProductProtocolBinding(ctx, model.ProductProtocolBinding{TenantID: "tenant", ProductID: "sensor", ProtocolID: "sensor", Version: "2"})
				}
			}
			if e == nil {
				_, e = peer.Write([]byte{6, id, 7, 44})
			}
			done <- e
		}()
		cmd := request("POST", "/api/v2/device-access-profiles/"+mode+"/devices/"+childID+"/commands", token, map[string]any{"type": "read", "confirmed": true}, 200)
		if !strings.Contains(cmd.Body.String(), "acknowledged") {
			t.Fatal(cmd.Body.String())
		}
		if e := <-done; e != nil {
			t.Fatal(e)
		}
		if mode == "listen" {
			var raw model.RawMessage
		waitPinned:
			for {
				select {
				case raw = <-ingested:
					if raw.DeviceID == childID && string(raw.Payload) == "\"AA012C\"" {
						break waitPinned
					}
				case <-time.After(time.Second):
					t.Fatal("missing pinned child reply")
				}
			}
			if raw.ProtocolVersion != "1" {
				t.Fatal("pending child command changed parser version", raw.ProtocolVersion)
			}
			if e := repo.SaveProductProtocolBinding(ctx, model.ProductProtocolBinding{TenantID: "tenant", ProductID: "sensor", ProtocolID: "sensor", Version: "1"}); e != nil {
				t.Fatal(e)
			}
		}
		p.Queries = []model.ProtocolQuery{{Type: "read-main", IntervalSec: 60}}
		updateQueries := func() {
			t.Helper()
			if p.DeviceID != "" {
				request("PUT", "/api/v2/device-access-profiles/"+mode, token, p, 201)
				return
			}
			// A used shared listener must go through template preparation. This
			// runtime fixture seeds the result of that separately tested atomic
			// apply, while still proving the legacy HTTP route cannot bypass it.
			request("PUT", "/api/v2/device-access-profiles/"+mode, token, p, 409)
			applied, err := repo.GetDeviceAccessProfile(ctx, p.TenantID, p.ID)
			if err != nil {
				t.Fatal(err)
			}
			applied.Queries, applied.UpdatedAt = p.Queries, time.Now().UnixMilli()
			if err = repo.SaveDeviceAccessProfile(ctx, applied); err != nil {
				t.Fatal(err)
			}
		}
		updateQueries()
		query := make([]byte, 2)
		if _, e = io.ReadFull(peer, query); e != nil || !bytes.Equal(query, []byte{3, id}) {
			t.Fatal("real scheduled query", query, e)
		}
		peer.Write([]byte{4, id, 0})
	waitQuery:
		for {
			select {
			case raw := <-ingested:
				if string(raw.Payload) == fmt.Sprintf("\"04%02X00\"", id) {
					break waitQuery
				}
			case <-time.After(time.Second):
				t.Fatal("query response not ingested")
			}
		}
		p.Queries = nil
		updateQueries()
	}
	t.Run("browser", func(t *testing.T) {
		if os.Getenv("IOT_TEST_BROWSER") == "" {
			t.Skip("IOT_TEST_BROWSER is not configured")
		}
		assets := http.FileServer(http.Dir(filepath.Join("..", "..", "iot_front", "dist")))
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				api.Handler().ServeHTTP(w, r)
			} else {
				assets.ServeHTTP(w, r)
			}
		}))
		defer server.Close()
		cmd := exec.CommandContext(ctx, "node", filepath.Join("..", "..", "iot_front", "tests", "browser", "tcp-children-check.mjs"))
		cmd.Env = append(os.Environ(), "IOT_TEST_ORIGIN="+server.URL, "IOT_TEST_TOKEN="+token)
		if out, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("browser: %v %s", e, out)
		}
	})
	// Disable retains children but prevents any further device command.
	profiles[0].Enabled = false
	request("PUT", "/api/v2/device-access-profiles/listen", token, profiles[0], 201)
}

const tcpChildSource = `package main
import "errors"
func Protocol() Definition{return Definition{
 Decode:func(b []byte,c Context)(Message,error){if len(b)!=3||b[0]!=0xaa{return Message{},errors.New("invalid sensor data")};return properties(map[string]any{"temperature":int(b[2])}),nil},
 Encode:func(q Command,c Context)(Frame,error){if q.Type!="read"{return Frame{},errors.New("unsupported command")};return Frame{Reply:[]byte{0x44},CorrelationID:"read"},nil},
 Samples:[]Sample{{Data:[]byte{0xaa,1,42},Want:properties(map[string]any{"temperature":42})}},
 Operations:[]OperationSample{{Operation:"encode",Command:Command{Type:"read"},Want:Frame{Reply:[]byte{0x44},CorrelationID:"read"}}},
}}
`

const tcpParentSource = `package main
import("fmt";"errors";"strconv")
func Protocol() Definition{return Definition{
 Transport:"TCP",
 Decode:func(b []byte,c Context)(Message,error){if len(b)<3{return Message{},errors.New("short frame")};return Message{MessageType:"EVENT_REPORT",Event:map[string]any{"type":"gatewayFrame"}},nil},
 Ingress:func(b []byte,c Context)(Frame,error){
  if len(b)<3{return Frame{NeedMore:true},nil}
  if b[0]==1{if b[1]!=0x5a{return Frame{},errors.New("invalid registration credential")};id:=fmt.Sprintf("main-%d",b[2]);return Frame{Consumed:3,DeviceID:id,Reply:[]byte{0x81},State:map[string]any{"id":int(b[2])}},nil}
  id,ok:=c.State["id"].(float64);if !ok||byte(id)!=b[1]{return Frame{},errors.New("registration required")}
  if b[0]==4{return Frame{Consumed:3,DeviceID:fmt.Sprintf("main-%d",int(id)),State:c.State,CorrelationID:"read-main"},nil}
  if len(b)<4{return Frame{NeedMore:true},nil}
  if b[0]!=2 && b[0]!=6{return Frame{},errors.New("unexpected frame")}
  f:=Frame{Consumed:4,DeviceID:fmt.Sprintf("main-%d",int(id)),State:c.State,Children:[]Child{{Address:strconv.Itoa(int(b[2])),Type:"smoke",Name:"烟感探测器",Data:[]byte{0xaa,1,b[3]}}}}
  if b[0]==2{f.Reply=[]byte{0x82}}else{f.CorrelationID="child-"+strconv.Itoa(int(b[2]))};return f,nil
 },
 Encode:func(q Command,c Context)(Frame,error){id,ok:=c.State["id"].(float64);if !ok{return Frame{},errors.New("registration required")};if q.Type=="read-main"{return Frame{Reply:[]byte{3,byte(id)},State:c.State,CorrelationID:"read-main"},nil};if q.Type!="child"{return Frame{},errors.New("unsupported command")};address,_:=q.Params["address"].(string);n,e:=strconv.Atoi(address);if e!=nil||q.Params["payload"]!="44"{return Frame{},errors.New("invalid child envelope")};return Frame{Reply:[]byte{5,byte(id),byte(n),0x44},State:c.State,CorrelationID:"child-"+address},nil},
 Samples:[]Sample{{Data:[]byte{1,0x5a,1},Want:Message{MessageType:"EVENT_REPORT",Event:map[string]any{"type":"gatewayFrame"}}}},
 Operations:[]OperationSample{
 {Operation:"ingress",Data:[]byte{1,0x5a,1},Want:Frame{Consumed:3,DeviceID:"main-1",Reply:[]byte{0x81},State:map[string]any{"id":1}}},
 {Operation:"ingress",Data:[]byte{2,1,7,42},Context:Context{State:map[string]any{"id":1}},Want:Frame{Consumed:4,DeviceID:"main-1",Reply:[]byte{0x82},State:map[string]any{"id":1},Children:[]Child{{Address:"7",Type:"smoke",Name:"烟感探测器",Data:[]byte{0xaa,1,42}}}}},
 {Operation:"encode",Command:Command{Type:"child",Params:map[string]any{"address":"7","payload":"44"}},Context:Context{State:map[string]any{"id":1}},Want:Frame{Reply:[]byte{5,1,7,0x44},State:map[string]any{"id":1},CorrelationID:"child-7"}},
 },
}}
`
