package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	kafkaadapter "iot-platform/internal/adapters/kafka"
	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/messagetopics"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
)

type topicCreationAdmin struct {
	topicKafkaAdminFake
	created []string
	failure error
}

func (a *topicCreationAdmin) CreateTopic(_ context.Context, topic string) error {
	a.created = append(a.created, topic)
	return a.failure
}

func TestSharedTopicsCreatePublishRulesAndHistoryBoundary(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	bus, realtime := local.NewBus(), local.NewRealtime()
	engine := core.New(ScopedRepository(repo), nil, bus, realtime, nil, log)
	cfg := config.Load()
	cfg.AdminUser, cfg.AdminPassword, cfg.AdminTenants = "root", "test-password", []string{"tenant_a", "tenant_b"}
	cfg.JWTSecret = "shared-topic-rules-test-secret-long-enough"
	cfg.MQTTBroker, cfg.MQTTPublicURL = "tcp://mqtt.invalid:1883", "tcp://mqtt.invalid:1883"
	cfg.KafkaBrokers, cfg.KafkaPublicBrokers = []string{"kafka.invalid:9092"}, []string{"kafka.invalid:9092"}
	api := New(cfg, engine, metrics.New(), log)
	admin := &topicCreationAdmin{}
	api.SetMessageTopicKafkaAdmin(admin)
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	request := func(method, path, token string, body any, status int) map[string]any {
		t.Helper()
		return requestJSON(t, server.Client(), method, server.URL+path, token, body, status)
	}
	login := func(tenant string) string {
		return request("POST", "/api/v1/auth/login", "", map[string]any{"username": "root", "password": cfg.AdminPassword, "tenantId": tenant}, 200)["accessToken"].(string)
	}
	root, other := login("tenant_a"), login("tenant_b")
	revision := float64(0)
	req := func(method, path string, body map[string]any, status int) map[string]any {
		t.Helper()
		if body != nil {
			body["revision"] = revision
		}
		result := request(method, path, root, body, status)
		if n, ok := result["revision"].(float64); ok {
			revision = n
		}
		return result
	}
	findShared := func(view map[string]any, protocol string) map[string]any {
		t.Helper()
		for _, row := range view["items"].([]any) {
			item := row.(map[string]any)
			if item["shared"] == true && item["protocol"] == protocol {
				return item
			}
		}
		t.Fatal("shared topic absent")
		return nil
	}
	view := req("POST", "/api/v1/message-topics", map[string]any{"name": "任意 MQTT 消息", "protocol": "mqtt", "topic": "garden/readings", "enabled": true}, 200)
	topic := findShared(view, "mqtt")
	id, address := topic["id"].(string), topic["topic"].(string)
	if !strings.HasSuffix(address, "/garden/readings") || topic["sourceId"] != "" || len(view["rules"].([]any)) != 0 {
		t.Fatal("topic creation still requires a source", topic)
	}
	path := "/api/v1/message-topics/" + id
	req("POST", path+"/publish", map[string]any{"payload": "任意文本\n第二行", "format": "text", "qos": 1}, 200)
	if len(realtime.Messages) != 1 || realtime.Messages[0].Topic != address || string(realtime.Messages[0].Payload) != "任意文本\n第二行" {
		t.Fatal("manual payload changed", realtime.Messages)
	}
	req("POST", path+"/publish", map[string]any{"payload": "{broken}", "format": "json"}, 422)
	req("POST", path+"/publish", map[string]any{"payload": strings.Repeat("a", (256<<10)+1), "format": "text"}, 422)
	request("POST", path+"/publish", other, map[string]any{"revision": 0, "payload": "cross tenant", "format": "text"}, 422)
	if len(realtime.Messages) != 1 {
		t.Fatal("invalid publication reached broker")
	}
	req("PUT", path, map[string]any{"name": "rename", "protocol": "mqtt", "topic": "different", "enabled": true}, 422)
	req("POST", "/api/v1/message-topics", map[string]any{"name": "duplicate", "protocol": "mqtt", "topic": "garden/readings", "enabled": true}, 422)
	for _, device := range []string{"d1", "d2"} {
		if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "tenant_a", ID: device, ProductID: "p", AccessKey: "key_" + device}); err != nil {
			t.Fatal(err)
		}
	}
	rule := map[string]any{"name": "温度", "sourceId": "mqtt.parsed", "enabled": true, "deviceScope": "selected", "deviceIds": []string{"d1"}, "format": "json", "fields": map[string]string{"temperature": "properties.temperature"}}
	preview := request("POST", path+"/preview", root, map[string]any{"rule": rule, "payload": `{"properties":{"temperature":24.5}}`}, 200)
	if preview["payload"] != `{"temperature":24.5}` {
		t.Fatal("preview mismatch", preview)
	}
	if stored, _ := engine.MessageTopics.Load(ctx, "tenant_a"); stored.Revision != int64(revision) || len(stored.Rules) != 0 || len(stored.Topics[0].Exposure) != 0 {
		t.Fatal("preview mutated configuration")
	}
	view = req("POST", path+"/rules", rule, 200)
	ruleID := view["rules"].([]any)[0].(map[string]any)["id"].(string)
	before := len(realtime.Messages)
	for _, device := range []string{"d1", "d2"} {
		payload := []byte(`{"tenantId":"tenant_a","deviceId":"` + device + `","properties":{"temperature":24.5}}`)
		if err := engine.Realtime.Publish(ctx, "/iot/parsed/tenant_a/p/"+device+"/PROPERTY_REPORT", payload, 1, false); err != nil {
			t.Fatal(err)
		}
	}
	forwarded := 0
	for _, msg := range realtime.Messages[before:] {
		if msg.Topic == address {
			forwarded++
			if string(msg.Payload) != `{"temperature":24.5}` {
				t.Fatal("automatic format not applied")
			}
		}
	}
	if forwarded != 1 {
		t.Fatal("rule device scope not applied", forwarded)
	}
	rule["deviceIds"] = []string{"missing"}
	req("PUT", path+"/rules/"+ruleID, rule, 422)
	req("DELETE", path+"/rules/"+ruleID+"?revision="+strconv.Itoa(int(revision)), nil, 200)
	stored, err := engine.MessageTopics.Load(ctx, "tenant_a")
	if err != nil || len(stored.Rules) != 0 || len(stored.Topics[0].Exposure) == 0 {
		t.Fatal("deleting rule lost its history boundary", err, stored)
	}
	view = req("POST", "/api/v1/message-topics", map[string]any{"name": "Kafka text", "protocol": "kafka", "topic": "garden.readings", "enabled": true}, 200)
	kafkaTopic := findShared(view, "kafka")
	if len(admin.created) != 1 || admin.created[0] != kafkaTopic["topic"] {
		t.Fatal("Kafka topic was not created", admin.created)
	}
	var received string
	_ = bus.Subscribe(ctx, kafkaTopic["topic"].(string), "test", func(_ context.Context, payload []byte) error { received = string(payload); return nil })
	req("POST", "/api/v1/message-topics/"+kafkaTopic["id"].(string)+"/publish", map[string]any{"format": "text", "payload": "hello Kafka", "key": "partition-key"}, 200)
	if received != "hello Kafka" {
		t.Fatal("Kafka manual message missing")
	}
	admin.failure = errors.New("offline")
	req("POST", "/api/v1/message-topics", map[string]any{"name": "failed", "protocol": "kafka", "topic": "unavailable", "enabled": true}, 503)
	admin.failure = kafkaadapter.ErrTopicExists
	req("POST", "/api/v1/message-topics", map[string]any{"name": "old history", "protocol": "kafka", "topic": "pre-existing", "enabled": true}, 422)
	admin.failure = nil
	// Removing an ordinary override cannot make its former target available to
	// a new shared channel, including messages already waiting to be published.
	for _, legacy := range []struct{ source, protocol, previous, candidate string }{
		{"kafka.property-report", "kafka", messagetopics.KafkaPrefix("tenant_a") + "previous", "previous"},
		{"mqtt.parsed", "mqtt", messagetopics.MQTTPrefix("tenant_a") + "previous/{deviceId}", "previous/d1"},
	} {
		req("PUT", "/api/v1/message-topics/"+legacy.source, map[string]any{"topic": legacy.previous, "enabled": true}, 200)
		req("POST", "/api/v1/message-topics/"+legacy.source+"/reset?revision="+strconv.Itoa(int(revision)), nil, 200)
		req("POST", "/api/v1/message-topics", map[string]any{"name": "reuse previous routing", "protocol": legacy.protocol, "topic": legacy.candidate, "enabled": true}, 422)
	}
	// A preview validates one proposed rule and still works at the stored limit.
	stored, err = engine.MessageTopics.Load(ctx, "tenant_a")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 200; i++ {
		stored.Rules = append(stored.Rules, model.MessageTopicRule{ID: "limit_" + strconv.Itoa(i), TopicID: id, Name: "温度", SourceID: "mqtt.parsed", Enabled: false, DeviceScope: "selected", DeviceIDs: []string{"d1"}, Format: "json", Fields: map[string]string{"temperature": "properties.temperature"}})
	}
	if saved, err := engine.MessageTopics.Save(ctx, "tenant_a", stored); err != nil || !saved {
		t.Fatal("save rules at limit", saved, err)
	}
	revision++
	request("POST", path+"/preview", root, map[string]any{"rule": stored.Rules[0], "payload": `{"properties":{"temperature":23}}`}, 200)
	req("DELETE", path+"?revision="+strconv.Itoa(int(revision)), nil, 200)
	req("POST", "/api/v1/message-topics", map[string]any{"name": "reuse history", "protocol": "mqtt", "topic": "garden/readings", "enabled": true}, 422)
}
