package mqttadapter

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/auth"
	"iot-platform/internal/messagetopics"
	"iot-platform/internal/model"
)

type brokerTestBearerTransport struct{ token string }

func (r brokerTestBearerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header = request.Header.Clone()
	clone.Header.Set("Authorization", "Bearer "+r.token)
	return http.DefaultTransport.RoundTrip(clone)
}

// Only non-retained messages under a random topic are written. This probe
// verifies the operator's fixed credentials independently of device/JWT tokens.
func TestExistingMQTTToolCredentials(t *testing.T) {
	broker := os.Getenv("IOT_TEST_MESSAGE_TOPICS_MQTT_BROKER")
	username, password := os.Getenv("IOT_TEST_MQTT_TOOL_USERNAME"), os.Getenv("IOT_TEST_MQTT_TOOL_PASSWORD")
	if broker == "" || username == "" || password == "" {
		t.Skip("explicit MQTT broker and tool credentials required")
	}
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	id := hex.EncodeToString(random[:])
	connect := func(user, pass, suffix string) (mqtt.Client, error) {
		options := mqtt.NewClientOptions().AddBroker(broker).SetClientID("tool-probe-" + id + suffix).SetUsername(user).SetPassword(pass).SetCleanSession(true).SetAutoReconnect(false).SetConnectRetry(false).SetConnectTimeout(3 * time.Second)
		client := mqtt.NewClient(options)
		t.Cleanup(func() { client.Disconnect(50) })
		token := client.Connect()
		if !token.WaitTimeout(5 * time.Second) {
			return client, context.DeadlineExceeded
		}
		return client, token.Error()
	}
	client, err := connect(username, password, "-valid")
	if err != nil {
		t.Fatal("tool credentials rejected")
	}
	for _, rejected := range [][2]string{{username, password + "-incorrect"}, {"", ""}} {
		if _, err := connect(rejected[0], rejected[1], "-invalid"); err == nil {
			t.Fatal("wrong password or anonymous connection accepted")
		}
	}
	topic := "/tool-probe/" + id
	received := make(chan struct{}, 1)
	token := client.Subscribe(topic, 1, func(_ mqtt.Client, message mqtt.Message) {
		if string(message.Payload()) == "tool-probe" {
			select {
			case received <- struct{}{}:
			default:
			}
		}
	})
	if !token.WaitTimeout(5*time.Second) || token.Error() != nil {
		t.Fatal("tool subscription failed")
	}
	if result, ok := token.(*mqtt.SubscribeToken); !ok || result.Result()[topic] != 1 {
		t.Fatal("tool subscription denied")
	}
	token = client.Publish(topic, 1, false, "tool-probe")
	if !token.WaitTimeout(5*time.Second) || token.Error() != nil {
		t.Fatal("tool publication failed")
	}
	select {
	case <-received:
	case <-time.After(5 * time.Second):
		t.Fatal("tool message not received")
	}
	t.Log("fixed tool credentials publish/subscribe; wrong password and anonymous clients refused")
}

func TestExistingMQTTTopicClientPublishSubscribe(t *testing.T) {
	broker, secret := os.Getenv("IOT_TEST_MESSAGE_TOPICS_MQTT_BROKER"), os.Getenv("IOT_TEST_MESSAGE_TOPICS_JWT_SECRET")
	if broker == "" || secret == "" {
		t.Skip("existing MQTT broker and signing secret required")
	}
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	id := hex.EncodeToString(random[:])
	tenant := "topic-client-" + id
	readTopic, writeTopic := messagetopics.MQTTPrefix(tenant)+"read", messagetopics.MQTTPrefix(tenant)+"write"
	manager := auth.New(secret)
	password, err := manager.IssueTopicClient("client-"+id, tenant, []string{readTopic}, []string{writeTopic}, time.Now().Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	observerPassword, err := manager.IssueWithACL("observer-"+id, tenant, "service", nil, []auth.ACLRule{{Permission: "allow", Action: "all", Topic: readTopic}, {Permission: "allow", Action: "all", Topic: writeTopic}}, 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	received := make(chan string, 16)
	connect := func(user, pass string) mqtt.Client {
		t.Helper()
		client := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(broker).SetClientID(user).SetUsername(user).SetPassword(pass).SetCleanSession(true).SetAutoReconnect(false).SetConnectRetry(false).SetConnectTimeout(3 * time.Second).SetDefaultPublishHandler(func(_ mqtt.Client, m mqtt.Message) { received <- string(m.Payload()) }))
		t.Cleanup(func() { client.Disconnect(100) })
		if token := client.Connect(); !token.WaitTimeout(5*time.Second) || token.Error() != nil {
			t.Fatal("temporary MQTT connection failed")
		}
		return client
	}
	client, observer := connect("client-"+id, password), connect("observer-"+id, observerPassword)
	subscribe := func(c mqtt.Client, topic string, want byte) {
		t.Helper()
		token := c.Subscribe(topic, 1, nil)
		if !token.WaitTimeout(5 * time.Second) {
			t.Fatal("subscription timeout")
		}
		result, ok := token.(*mqtt.SubscribeToken)
		if !ok || result.Result()[topic] != want {
			t.Fatal("unexpected subscription authorization")
		}
	}
	subscribe(observer, writeTopic, 1)
	subscribe(observer, readTopic, 1)
	subscribe(client, readTopic, 1)
	subscribe(client, writeTopic, 0x80)
	subscribe(client, messagetopics.MQTTPrefix(tenant)+"#", 0x80)
	if token := client.Publish(writeTopic, 1, false, "client-write-allowed"); !token.WaitTimeout(5*time.Second) || token.Error() != nil {
		t.Fatal("granted publish failed")
	}
	select {
	case payload := <-received:
		if payload != "client-write-allowed" {
			t.Fatal("unexpected authorized message")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("granted publication was not delivered")
	}
	if token := client.Publish(readTopic, 1, false, "client-write-forbidden"); !token.WaitTimeout(5 * time.Second) {
		t.Fatal("forbidden publication timed out")
	}
	if token := observer.Publish(readTopic, 1, false, "observer-read-allowed"); !token.WaitTimeout(5*time.Second) || token.Error() != nil {
		t.Fatal("observer publish failed")
	}
	for i := 0; i < 2; i++ {
		select {
		case payload := <-received:
			if payload != "observer-read-allowed" {
				t.Fatal("unauthorized publication was delivered")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("granted subscription message missing")
		}
	}
	select {
	case payload := <-received:
		t.Fatalf("unexpected additional publication: %q", payload)
	case <-time.After(250 * time.Millisecond):
	}
}

// This opt-in test creates only random short-lived identities and non-retained
// topics on an existing broker. Its exact temporary ban is removed at cleanup.
func TestExistingMQTTTopicConsumerAuthorization(t *testing.T) {
	broker, secret := os.Getenv("IOT_TEST_MESSAGE_TOPICS_MQTT_BROKER"), os.Getenv("IOT_TEST_MESSAGE_TOPICS_JWT_SECRET")
	admin := &Admin{URL: os.Getenv("IOT_TEST_MESSAGE_TOPICS_EMQX_URL"), Key: os.Getenv("IOT_TEST_MESSAGE_TOPICS_EMQX_KEY"), Secret: os.Getenv("IOT_TEST_MESSAGE_TOPICS_EMQX_SECRET")}
	if token := os.Getenv("IOT_TEST_MESSAGE_TOPICS_EMQX_TOKEN"); token != "" {
		admin.Key, admin.Secret = "test-token", "test-token"
		admin.Client = &http.Client{Transport: brokerTestBearerTransport{token: token}, Timeout: 10 * time.Second}
	}
	if broker == "" || secret == "" || admin.URL == "" || admin.Key == "" || admin.Secret == "" {
		t.Skip("existing MQTT broker, signing secret and EMQX administration credentials required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := admin.CheckTopicAuthorization(ctx); err != nil {
		t.Fatal("MQTT readiness:", err)
	}
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	id := hex.EncodeToString(random[:])
	tenant := "acl-test-" + id
	username := "topic-test-" + id
	credential := model.MessageTopicCredential{ID: "credential-" + id, KeyID: "key", Protocol: "mqtt", Username: username, AccessVersion: "access-v1", Status: "active", CreatedAt: time.Now().Unix(), ExpiresAt: time.Now().Add(2 * time.Minute).Unix()}
	topic := messagetopics.Destination(tenant, model.MessageTopicRoute{Protocol: "mqtt", Topic: "route-" + id})
	credential.Topics = []string{topic}
	otherTopic := messagetopics.Destination(tenant+"-other", model.MessageTopicRoute{Protocol: "mqtt", Topic: "route-" + id})
	manager := auth.New(secret)
	password, err := manager.IssueTopicConsumer(username, tenant, []string{topic}, time.Unix(credential.ExpiresAt, 0))
	if err != nil {
		t.Fatal(err)
	}
	publisherUser := "publisher-" + id
	publisherPassword, err := manager.IssueWithACL(publisherUser, tenant, "service", nil, []auth.ACLRule{{Permission: "allow", Action: "all", Topic: topic}, {Permission: "allow", Action: "publish", Topic: otherTopic}}, 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	connect := func(user, password, suffix string, lost chan struct{}) mqtt.Client {
		t.Helper()
		options := mqtt.NewClientOptions().AddBroker(broker).SetClientID("acl-" + suffix + "-" + id).SetUsername(user).SetPassword(password).SetCleanSession(true).SetAutoReconnect(false).SetConnectRetry(false).SetConnectTimeout(5 * time.Second)
		if lost != nil {
			options.SetConnectionLostHandler(func(mqtt.Client, error) {
				select {
				case lost <- struct{}{}:
				default:
				}
			})
		}
		client := mqtt.NewClient(options)
		token := client.Connect()
		if !token.WaitTimeout(8*time.Second) || token.Error() != nil {
			t.Fatal("temporary MQTT client connection failed")
		}
		t.Cleanup(func() { client.Disconnect(100) })
		return client
	}
	lost := make(chan struct{}, 1)
	consumer := connect(username, password, "consumer", lost)
	publisher := connect(publisherUser, publisherPassword, "publisher", nil)
	received := make(chan string, 16)
	subscribe := func(client mqtt.Client, target string, want byte) {
		t.Helper()
		token := client.Subscribe(target, 1, func(_ mqtt.Client, m mqtt.Message) {
			select {
			case received <- string(m.Payload()):
			default:
			}
		})
		if !token.WaitTimeout(5 * time.Second) {
			t.Fatal("MQTT subscribe timed out")
		}
		result, ok := token.(*mqtt.SubscribeToken)
		if !ok || result.Result()[target] != want {
			t.Fatalf("MQTT subscribe result=%v error=%v want=%d", result.Result(), token.Error(), want)
		}
	}
	subscribe(consumer, topic, 1)
	for _, denied := range []string{otherTopic, messagetopics.MQTTPrefix(tenant) + "#", "#", "$SYS/#"} {
		subscribe(consumer, denied, 0x80)
	}
	// Observe the consumer's forbidden publication on an allowed destination.
	// A QoS 1 acknowledgment alone is not proof that an EMQX publish was allowed.
	subscribe(publisher, topic, 1)
	if token := consumer.Publish(topic, 1, false, "forbidden-consumer-publish"); !token.WaitTimeout(5 * time.Second) {
		t.Fatal("consumer publish acknowledgment timed out")
	}
	repo := memory.NewRepository()
	service := messagetopics.New(repo)
	service.SetAccessResolver(func(context.Context, string, string) (messagetopics.MessageTopicIdentity, error) {
		return messagetopics.MessageTopicIdentity{Permissions: map[string]bool{"menu:devices": true, "menu:messageTopics": true}, DeviceScope: "selected", DeviceIDs: []string{"device-one"}, Version: "access-v1"}, nil
	})
	query := model.MessageTopicQuery{Dataset: "device_reports", Mode: "realtime", DeviceScope: "selected", DeviceIDs: []string{"device-one"}, Fields: map[string]string{"marker": "tags.marker"}}
	cfg := model.MessageTopicConfig{Topics: []model.MessageTopicRoute{{ID: "route", Name: "测试数据", Protocol: "mqtt", Topic: topic, Enabled: true, Query: &query, KeyIDs: []string{"key"}}}, Credentials: []model.MessageTopicCredential{credential}}
	if err := messagetopics.AccumulateQueryExposure(&cfg, "route", query); err != nil {
		t.Fatal(err)
	}
	if ok, err := service.Save(ctx, tenant, cfg); !ok || err != nil {
		t.Fatal("save managed route", ok, err)
	}
	adapter, err := NewWithCredentials(broker, "acl-adapter-"+id, func() (string, string) { return publisherUser, publisherPassword })
	if err != nil {
		t.Fatal("temporary routed MQTT publisher connection failed")
	}
	t.Cleanup(func() { _ = adapter.Close() })
	routed := service.WrapRealtime(adapter)
	publish := func(device string) {
		t.Helper()
		payload, _ := json.Marshal(map[string]any{"tenantId": tenant, "deviceId": device, "productId": "p", "messageType": "PROPERTY_REPORT", "tags": map[string]string{"marker": device}})
		if err := routed.Publish(ctx, "/iot/parsed/"+tenant+"/p/"+device+"/PROPERTY_REPORT", payload, 1, false); err != nil {
			t.Fatal(err)
		}
	}
	publish("not-granted")
	publish("device-one")
	for i := 0; i < 2; i++ {
		select {
		case got := <-received:
			if !strings.Contains(got, `"marker":"device-one"`) {
				t.Fatal("unauthorized publication reached subscriber")
			}
		case <-ctx.Done():
			t.Fatal("authorized managed delivery missing")
		}
	}
	if err := admin.RevokeUsername(ctx, username); err != nil {
		t.Fatal("temporary consumer revocation failed:", err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.request(cleanup, "DELETE", "/banned/username/"+url.PathEscape(username), nil, nil); err != nil {
			t.Error("temporary consumer ban cleanup failed:", err)
		}
	})
	select {
	case <-lost:
	case <-ctx.Done():
		t.Fatal("revoked consumer stayed connected")
	}
	consumer.Disconnect(100)
	reconnect := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(broker).SetClientID("acl-reconnect-" + id).SetUsername(username).SetPassword(password).SetCleanSession(true).SetAutoReconnect(false).SetConnectRetry(false).SetConnectTimeout(3 * time.Second))
	defer reconnect.Disconnect(100)
	if token := reconnect.Connect(); !token.WaitTimeout(5*time.Second) || token.Error() == nil {
		t.Fatal("revoked MQTT credential reconnected")
	}
}
