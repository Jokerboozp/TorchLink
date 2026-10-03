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

func TestMessageTopicQueryAtomicConfigurationPreviewAndMigration(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	realtime := local.NewRealtime()
	engine := core.New(ScopedRepository(repo), nil, local.NewBus(), realtime, nil, log)
	cfg := config.Load()
	cfg.AdminUser, cfg.AdminPassword, cfg.AdminTenants = "root", "test-password", []string{"tenant_a", "tenant_b"}
	cfg.JWTSecret = "query-topic-rules-test-secret-long-enough"
	cfg.MQTTBroker, cfg.MQTTPublicURL = "tcp://mqtt.invalid:1883", "tcp://mqtt.invalid:1883"
	cfg.KafkaBrokers, cfg.KafkaPublicBrokers = []string{"kafka.invalid:9092"}, []string{"kafka.invalid:9092"}
	api := New(cfg, engine, metrics.New(), log)
	broker := &topicCreationAdmin{}
	api.SetMessageTopicKafkaAdmin(broker)
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	request := func(method, path, token string, body any, status int) map[string]any {
		t.Helper()
		return requestJSON(t, server.Client(), method, server.URL+path, token, body, status)
	}
	root := request("POST", "/api/v1/auth/login", "", map[string]any{"username": "root", "password": cfg.AdminPassword, "tenantId": "tenant_a"}, 200)["accessToken"].(string)
	for _, id := range []string{"d1", "d2"} {
		if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "tenant_a", ID: id, ProductID: "p", AccessKey: "key_" + id}); err != nil {
			t.Fatal(err)
		}
	}
	state := model.AccessState{Users: []model.PlatformUser{{Username: "consumer", Enabled: true, DeviceScope: "selected", DeviceIDs: []string{"d1"}, Permissions: []string{"menu:devices", "menu:messageTopics"}}}}
	if ok, err := repo.SaveAccessState(ctx, "tenant_a", state); err != nil || !ok {
		t.Fatal(err)
	}
	account := model.MessageTopicAccount{ID: "consumer", Name: "外部系统", Username: "consumer", Enabled: true, DeviceScope: "selected", DeviceIDs: []string{"d1"}}
	if saved, err := engine.MessageTopics.Save(ctx, "tenant_a", model.MessageTopicConfig{Accounts: []model.MessageTopicAccount{account}}); !saved || err != nil {
		t.Fatal("seed account", saved, err)
	}
	query := map[string]any{"dataset": "device_reports", "fields": map[string]string{"deviceId": "deviceId", "temperature": "properties.temperature"}, "deviceScope": "selected", "deviceIds": []string{"d1"}, "filter": map[string]any{"field": "properties.temperature", "operator": "gt", "value": 20}}
	body := map[string]any{"revision": 1, "name": "设备温度", "protocol": "mqtt", "topic": "/device", "enabled": true, "query": query, "accountIds": []string{"consumer"}}
	view := request("POST", "/api/v1/message-topics", root, body, 200)
	if view["revision"] != float64(2) || len(view["datasets"].([]any)) == 0 {
		t.Fatal("unified query save did not return catalogue/revision", view)
	}
	stored, _ := engine.MessageTopics.Load(ctx, "tenant_a")
	topic := stored.Topics[0]
	if topic.Query == nil || topic.Query.Mode != "realtime" || topic.Topic != messagetopics.MQTTPrefix("tenant_a")+"device" || len(topic.Exposure) != 1 || len(stored.Accounts[0].TopicIDs) != 1 || stored.Accounts[0].TopicIDs[0] != topic.ID {
		t.Fatal("topic/query/subscription not saved together", stored)
	}
	path := "/api/v1/message-topics/" + topic.ID
	request("POST", "/api/v1/message-topics", root, body, 409)
	request("POST", path+"/publish", root, map[string]any{"revision": 2, "format": "json", "payload": `{}`}, 422)
	request("POST", path+"/rules", root, map[string]any{"revision": 2, "name": "extra", "sourceId": "mqtt.parsed", "format": "original", "deviceScope": "all"}, 422)
	request("PUT", "/api/v1/message-topic-accounts/consumer", root, map[string]any{"revision": 2, "name": account.Name, "username": account.Username, "enabled": true, "deviceScope": "selected", "deviceIds": []string{"d1"}, "publishTopicIds": []string{topic.ID}}, 422)
	query["deviceIds"] = []string{"d2"}
	body["revision"] = 2
	request("PUT", path, root, body, 422)
	after, _ := engine.MessageTopics.Load(ctx, "tenant_a")
	if after.Revision != 2 || after.Topics[0].Query.DeviceIDs[0] != "d1" || len(after.Topics[0].Exposure[0].DeviceIDs) != 1 {
		t.Fatal("failed authorization partially changed query/history", after)
	}
	query["deviceIds"] = []string{"d1"}
	body["accountIds"] = []string{"missing"}
	request("PUT", path, root, body, 422)
	after, _ = engine.MessageTopics.Load(ctx, "tenant_a")
	if len(after.Accounts[0].TopicIDs) != 1 {
		t.Fatal("failed account selection removed existing subscription")
	}
	// Query previews use the publication evaluator without saving or publishing.
	previewBody := map[string]any{"querySql": "SELECT deviceId, properties.temperature AS temperature FROM device_reports WHERE properties.temperature > 20", "queryOptions": map[string]any{"deviceScope": "selected", "deviceIds": []string{"d1"}}, "payload": `{"deviceId":"d1","properties":{"temperature":25},"raw":{"secret":"hidden"}}`}
	preview := request("POST", "/api/v1/message-topics/query/preview", root, previewBody, 200)
	if preview["matched"] != true || preview["sampled"] != true || preview["payload"] != `{"deviceId":"d1","temperature":25}` || preview["querySql"] == "" {
		t.Fatal("SQL preview mismatch", preview)
	}
	previewBody["payload"] = `{"deviceId":"d2","properties":{"temperature":25}}`
	preview = request("POST", "/api/v1/message-topics/query/preview", root, previewBody, 200)
	if preview["matched"] != false {
		t.Fatal("sample device scope was ignored", preview)
	}
	delete(previewBody, "payload")
	preview = request("POST", "/api/v1/message-topics/query/preview", root, previewBody, 200)
	if preview["matched"] != false || preview["sampled"] != false {
		t.Fatal("preview fabricated a sample", preview)
	}
	if err := repo.SaveStandardMessage(ctx, model.StandardMessage{MessageID: "m1", TenantID: "tenant_a", DeviceID: "d1", ProductID: "p", Timestamp: 100, Properties: map[string]any{"temperature": 25}}); err != nil {
		t.Fatal(err)
	}
	preview = request("POST", "/api/v1/message-topics/query/preview", root, previewBody, 200)
	if preview["matched"] != true || preview["payload"] != `{"deviceId":"d1","temperature":25}` {
		t.Fatal("real authorized sample missing", preview)
	}
	previewBody["query"] = query
	request("POST", "/api/v1/message-topics/query/preview", root, previewBody, 422)
	request("POST", "/api/v1/message-topics/query/preview", root, map[string]any{"querySql": "DELETE FROM devices"}, 422)
	request("POST", "/api/v1/message-topics/query/preview", root, map[string]any{"querySql": "SELECT * FROM devices"}, 200)
	after, _ = engine.MessageTopics.Load(ctx, "tenant_a")
	if after.Revision != 2 || len(realtime.Messages) != 0 {
		t.Fatal("preview mutated configuration or published")
	}
	// Invalid atomic authorization is rejected before Kafka topic provisioning.
	body["protocol"], body["topic"] = "kafka", "device"
	request("POST", "/api/v1/message-topics", root, body, 422)
	if len(broker.created) != 0 {
		t.Fatal("invalid authorization created a Kafka topic")
	}
	permissions := []string{"menu:messageTopics", "menu:devices", "POST /api/v1/message-topics", "POST /api/v1/message-topics/query/preview"}
	request("POST", "/api/v1/access/roles", root, map[string]any{"id": "query_editor", "name": "查询配置", "permissions": permissions}, 200)
	request("POST", "/api/v1/access/users", root, map[string]any{"username": "query_editor", "password": "query-editor-password", "enabled": true, "roleIds": []string{"query_editor"}, "deviceScope": "all"}, 200)
	editor := request("POST", "/api/v1/auth/login", "", map[string]any{"username": "query_editor", "password": "query-editor-password", "tenantId": "tenant_a"}, 200)["accessToken"].(string)
	body["accountIds"] = []string{"consumer"}
	request("POST", "/api/v1/message-topics", editor, body, 403)
	request("POST", "/api/v1/message-topics/query/preview", editor, map[string]any{"querySql": "SELECT * FROM alarms"}, 403)
	request("POST", "/api/v1/message-topics/query/preview", editor, map[string]any{"querySql": "SELECT * FROM alarms", "protocol": "unknown"}, 422)
	request("POST", "/api/v1/message-topics/query/preview", editor, map[string]any{"querySql": "SELECT deviceId FROM device_reports"}, 200)
	request("PUT", "/api/v1/access/users/query_editor", root, map[string]any{"enabled": true, "roleIds": []string{"query_editor"}, "deviceScope": "selected", "deviceIds": []string{"d1"}}, 200)
	editor = request("POST", "/api/v1/auth/login", "", map[string]any{"username": "query_editor", "password": "query-editor-password", "tenantId": "tenant_a"}, 200)["accessToken"].(string)
	request("POST", "/api/v1/message-topics/query/preview", editor, map[string]any{"querySql": "SELECT deviceId FROM device_reports"}, 403)
	// Existing independent rules require explicit replacement; historical scope remains.
	legacy := model.MessageTopicRoute{ID: "legacy", Name: "旧主题", Protocol: "mqtt", Topic: messagetopics.MQTTPrefix("tenant_a") + "legacy", Enabled: true}
	after.Topics = append(after.Topics, legacy)
	rule := model.MessageTopicRule{ID: "legacy-rule", TopicID: legacy.ID, SourceID: "mqtt.parsed", Name: "旧规则", Enabled: true, DeviceScope: "selected", DeviceIDs: []string{"d2"}, Format: "original"}
	after.Rules = append(after.Rules, rule)
	if err := messagetopics.AccumulateExposure(&after, rule); err != nil {
		t.Fatal(err)
	}
	if saved, err := engine.MessageTopics.Save(ctx, "tenant_a", after); !saved || err != nil {
		t.Fatal("seed legacy rule", saved, err)
	}
	body["revision"], body["protocol"], body["topic"] = 3, "mqtt", legacy.Topic
	delete(body, "accountIds")
	request("PUT", "/api/v1/message-topics/legacy", root, body, 422)
	body["replaceLegacyRules"] = true
	request("PUT", "/api/v1/message-topics/legacy", root, body, 200)
	after, _ = engine.MessageTopics.Load(ctx, "tenant_a")
	if len(after.Rules) != 0 || after.Topics[1].Query == nil || len(after.Topics[1].Exposure[0].DeviceIDs) != 2 {
		t.Fatal("migration lost old exposure or retained a rule", after)
	}
	// A narrow account cannot read a wider historical topic after migration.
	body["revision"], body["accountIds"] = 4, []string{"consumer"}
	request("PUT", "/api/v1/message-topics/legacy", root, body, 422)
}

func TestMessageTopicQuerySaveRejectsConcurrentChangesAtomically(t *testing.T) {
	ctx := context.Background()
	repo := &topicRevisionRaceRepository{Repository: memory.NewRepository()}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(ScopedRepository(repo), nil, local.NewBus(), local.NewRealtime(), nil, log)
	cfg := config.Load()
	cfg.AdminUser, cfg.AdminPassword, cfg.AdminTenants = "root", "test-password", []string{"t"}
	cfg.JWTSecret = "query-revision-test-secret-long-enough"
	api := New(cfg, engine, metrics.New(), log)
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	request := func(method, path string, token string, body any, status int) map[string]any {
		t.Helper()
		return requestJSON(t, server.Client(), method, server.URL+path, token, body, status)
	}
	root := request("POST", "/api/v1/auth/login", "", map[string]any{"username": "root", "password": cfg.AdminPassword, "tenantId": "t"}, 200)["accessToken"].(string)
	if saved, err := repo.SaveAccessState(ctx, "t", model.AccessState{Users: []model.PlatformUser{{Username: "u", Enabled: true, DeviceScope: "all", Permissions: []string{"menu:devices", "menu:messageTopics"}}}}); !saved || err != nil {
		t.Fatal(err)
	}
	if saved, err := engine.MessageTopics.Save(ctx, "t", model.MessageTopicConfig{Accounts: []model.MessageTopicAccount{{ID: "a", Name: "外部账号", Username: "u", Enabled: true, DeviceScope: "all"}}}); !saved || err != nil {
		t.Fatal(err)
	}
	repo.afterLoad = func() {
		latest, err := repo.Repository.LoadMessageTopicConfig(ctx, "t")
		if err != nil {
			t.Fatal(err)
		}
		latest.Accounts[0].Name = "其他操作的新名称"
		if saved, err := repo.Repository.SaveMessageTopicConfig(ctx, "t", latest); !saved || err != nil {
			t.Fatal(err)
		}
	}
	request("POST", "/api/v1/message-topics", root, map[string]any{"revision": 1, "name": "设备", "protocol": "mqtt", "topic": "device", "enabled": true, "querySql": "SELECT deviceId FROM device_reports", "accountIds": []string{"a"}}, 409)
	latest, _ := repo.Repository.LoadMessageTopicConfig(ctx, "t")
	if latest.Revision != 2 || len(latest.Topics) != 0 || len(latest.Accounts[0].TopicIDs) != 0 || latest.Accounts[0].Name != "其他操作的新名称" {
		t.Fatal("concurrent save lost an update or partially added subscription", latest)
	}
}

func TestMessageTopicDeviceSnapshotRepositoryNarrowsRequestScope(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	for _, device := range []model.ManagedDevice{{TenantID: "t", ID: "one", AccessKey: "one", GatewayID: "two"}, {TenantID: "t", ID: "two", AccessKey: "two"}, {TenantID: "other", ID: "one", AccessKey: "other"}} {
		if err := repo.SaveManagedDevice(ctx, device); err != nil {
			t.Fatal(err)
		}
	}
	_ = repo.UpsertDeviceState(ctx, model.DeviceState{TenantID: "t", DeviceID: "one", LastSeenAt: 42})
	scoped := ScopedRepository(repo)
	selectedCtx := context.WithValue(ctx, deviceScopeKey{}, deviceScope{Tenant: "t", IDs: map[string]bool{"one": true}})
	rows, err := scoped.ListMessageTopicDevices(selectedCtx, "t", nil, 10001)
	if err != nil || len(rows) != 1 || rows[0].Device.ID != "one" || rows[0].Device.GatewayID != "" || rows[0].State == nil || rows[0].State.LastSeenAt != 42 {
		t.Fatal("snapshot escaped device/gateway scope", rows, err)
	}
	rows, err = scoped.ListMessageTopicDevices(selectedCtx, "t", []string{"two"}, 10001)
	if err != nil || len(rows) != 0 {
		t.Fatal("query device filter was replaced instead of intersected", rows, err)
	}
	for _, scope := range []deviceScope{{Tenant: "t", All: true}, {Tenant: "t", IDs: map[string]bool{"one": true}}} {
		rows, err = scoped.ListMessageTopicDevices(context.WithValue(ctx, deviceScopeKey{}, scope), "other", nil, 10001)
		if err != nil || len(rows) != 0 {
			t.Fatal("snapshot crossed request tenant", rows, err)
		}
	}
	rows, err = scoped.ListMessageTopicDevices(ctx, "t", nil, 10001)
	if err != nil || len(rows) != 2 || rows[0].Device.GatewayID != "two" {
		t.Fatal("request scope leaked into background query", rows, err)
	}
}
