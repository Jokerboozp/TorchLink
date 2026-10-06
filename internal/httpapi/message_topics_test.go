package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"iot-platform/internal/devicescope"
	"log/slog"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/messagetopics"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
)

func topicQuery(scope string, devices ...string) map[string]any {
	return map[string]any{"dataset": "device_reports", "deviceScope": scope, "deviceIds": devices}
}

func TestMessageTopicsCRUDPermissionsAndPublishing(t *testing.T) {
	ctx := context.Background()
	repo := &topicAuditRepository{Repository: memory.NewRepository()}
	realtime := local.NewRealtime()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(devicescope.Wrap(repo), nil, local.NewBus(), realtime, nil, log)
	cfg := config.Load()
	cfg.AdminUser, cfg.AdminPassword, cfg.AdminTenants = "root", "root-password-test", []string{"tenant_a", "tenant_b"}
	cfg.JWTSecret = "test-message-topics-secret-at-least-32"
	cfg.MQTTBroker = "tcp://broker.invalid:1883"
	server := httptest.NewServer(New(cfg, engine, metrics.New(), log).Handler())
	defer server.Close()
	req := func(method, path, token string, body any, status int) map[string]any {
		t.Helper()
		return requestJSON(t, server.Client(), method, server.URL+path, token, body, status)
	}
	login := func(user, password, tenant string) string {
		return req("POST", "/api/v1/auth/login", "", map[string]string{"username": user, "password": password, "tenantId": tenant}, 200)["accessToken"].(string)
	}
	root := login("root", cfg.AdminPassword, "tenant_a")
	other := login("root", cfg.AdminPassword, "tenant_b")
	req("GET", "/api/v1/message-topics", "", nil, 401)
	view := req("GET", "/api/v1/message-topics", root, nil, 200)
	if view["revision"] != float64(0) || len(view["items"].([]any)) != 0 || len(view["builtin"].([]any)) < 30 {
		t.Fatal("unexpected initial topics", view)
	}
	input := map[string]any{"revision": 0, "name": "设备上报", "protocol": "mqtt", "topic": "/device", "enabled": true, "query": topicQuery("all")}
	missing := map[string]any{"revision": 0, "name": "无查询", "protocol": "mqtt", "topic": "/plain", "enabled": true}
	req("POST", "/api/v1/message-topics", root, missing, 422)
	view = req("POST", "/api/v1/message-topics", root, input, 200)
	item := view["items"].([]any)[0].(map[string]any)
	id, address := item["id"].(string), item["topic"].(string)
	if address != messagetopics.MQTTPrefix("tenant_a")+"device" || item["querySql"] == "" || view["revision"] != float64(1) {
		t.Fatal("created topic not returned", item)
	}
	req("POST", "/api/v1/message-topics", root, input, 409)
	if otherView := req("GET", "/api/v1/message-topics", other, nil, 200); len(otherView["items"].([]any)) != 0 {
		t.Fatal("topic crossed tenants")
	}
	update := map[string]any{"revision": 1, "name": "设备上报", "protocol": "kafka", "topic": "/device", "enabled": true}
	req("PUT", "/api/v1/message-topics/"+id, root, update, 422)
	update["protocol"] = "mqtt"
	req("PUT", "/api/v1/message-topics/missing", root, update, 404)
	// Platform data reaches the query topic; the source publication is kept.
	payload := []byte(`{"tenantId":"tenant_a","deviceId":"d1","properties":{"temperature":26}}`)
	if err := engine.Realtime.Publish(ctx, "/iot/parsed/tenant_a/p/d1/PROPERTY_REPORT", payload, 1, false); err != nil {
		t.Fatal(err)
	}
	if len(realtime.Messages) != 2 || realtime.Messages[1].Topic != address {
		t.Fatal("query topic did not receive the report", realtime.Messages)
	}
	update["enabled"] = false
	req("PUT", "/api/v1/message-topics/"+id, root, update, 200)
	if err := engine.Realtime.Publish(ctx, "/iot/parsed/tenant_a/p/d1/PROPERTY_REPORT", payload, 1, false); err != nil {
		t.Fatal(err)
	}
	if len(realtime.Messages) != 3 {
		t.Fatal("disabled topic still published")
	}
	// Viewing needs the menu; changes also need explicit actions and full scope.
	permissions := []string{"menu:messageTopics"}
	req("POST", "/api/v1/access/roles", root, map[string]any{"id": "topic_reader", "name": "主题查看", "permissions": permissions}, 200)
	req("POST", "/api/v1/access/users", root, map[string]any{"username": "topic_user", "password": "topic-user-password", "enabled": true, "roleIds": []string{"topic_reader"}, "deviceScope": "all"}, 200)
	user := login("topic_user", "topic-user-password", "tenant_a")
	req("GET", "/api/v1/message-topics", user, nil, 200)
	update["revision"] = 2
	req("PUT", "/api/v1/message-topics/"+id, user, update, 403)
	permissions = append(permissions, "PUT /api/v1/message-topics/:id", "DELETE /api/v1/message-topics/:id", "menu:devices")
	req("PUT", "/api/v1/access/roles/topic_reader", root, map[string]any{"name": "主题管理", "permissions": permissions}, 200)
	req("PUT", "/api/v1/message-topics/"+id, user, update, 200)
	update["revision"], update["keyIds"] = 3, []string{"ak_missing"}
	req("PUT", "/api/v1/message-topics/"+id, user, update, 403)
	// Deleting keeps the address reserved so broker history is not inherited.
	req("DELETE", "/api/v1/message-topics/"+id+"?revision=2", root, nil, 409)
	view = req("DELETE", "/api/v1/message-topics/"+id+"?revision=3", root, nil, 200)
	if len(view["items"].([]any)) != 0 {
		t.Fatal("deleted topic remains")
	}
	input["revision"] = 4
	req("POST", "/api/v1/message-topics", root, input, 422)
	req("PUT", "/api/v1/access/roles/topic_reader", root, map[string]any{"name": "无权限", "permissions": []string{}}, 200)
	req("GET", "/api/v1/message-topics", user, nil, 403)
	if !slices.ContainsFunc(repo.audits, func(event model.AuditLog) bool { return strings.HasPrefix(event.Action, "message-topic.") }) {
		t.Fatal("topic mutation audit missing")
	}
}

type topicAuditRepository struct {
	*memory.Repository
	audits []model.AuditLog
}

func (r *topicAuditRepository) SaveAudit(ctx context.Context, event model.AuditLog) error {
	r.audits = append(r.audits, event)
	return r.Repository.SaveAudit(ctx, event)
}

func TestMessageTopicKeysCredentialsAndBoundScope(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	realtime := local.NewRealtime()
	engine := core.New(devicescope.Wrap(repo), nil, local.NewBus(), realtime, nil, log)
	cfg := config.Load()
	cfg.AdminUser, cfg.AdminPassword, cfg.AdminTenants = "root", "test-password", []string{"tenant_a", "tenant_b"}
	cfg.JWTSecret = "topic-consumer-test-secret-long-enough"
	cfg.MQTTBroker = "tcp://mqtt.invalid:1883"
	cfg.MQTTPublicURL = cfg.MQTTBroker
	api := New(cfg, engine, metrics.New(), log)
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	req := func(method, path, token string, body any, status int) map[string]any {
		t.Helper()
		return requestJSON(t, server.Client(), method, server.URL+path, token, body, status)
	}
	root := req("POST", "/api/v1/auth/login", "", map[string]any{"username": "root", "password": cfg.AdminPassword, "tenantId": "tenant_a"}, 200)["accessToken"].(string)
	for _, id := range []string{"d1", "d2"} {
		if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "tenant_a", ID: id, ProductID: "p", AccessKey: "key_" + id}); err != nil {
			t.Fatal(err)
		}
	}
	state := model.AccessState{Users: []model.PlatformUser{{Username: "consumer", Enabled: true, DeviceScope: "selected", DeviceIDs: []string{"d1"}, Permissions: []string{"menu:devices", "menu:messageTopics"}}}}
	if ok, err := repo.SaveAccessState(ctx, "tenant_a", state); err != nil || !ok {
		t.Fatal(err)
	}
	created := req("POST", "/api/v1/access/api-keys", root, map[string]any{"name": "外部系统", "username": "consumer", "capabilities": []string{"topics:subscribe"}}, 201)
	keyID, apiKey := created["item"].(map[string]any)["id"].(string), created["apiKey"].(string)
	plain := req("POST", "/api/v1/access/api-keys", root, map[string]any{"name": "只读接口", "username": "consumer", "capabilities": []string{"alarms:read"}}, 201)
	plainKey := plain["apiKey"].(string)
	topic := map[string]any{"revision": 0, "name": "设备上报", "protocol": "mqtt", "topic": "/device", "enabled": true, "query": topicQuery("selected", "d2"), "keyIds": []string{keyID}}
	req("POST", "/api/v1/message-topics", root, topic, 422)
	topic["query"] = topicQuery("selected", "d1")
	topic["keyIds"] = []string{plain["item"].(map[string]any)["id"].(string)}
	req("POST", "/api/v1/message-topics", root, topic, 422)
	topic["keyIds"] = []string{keyID}
	view := req("POST", "/api/v1/message-topics", root, topic, 200)
	target := view["items"].([]any)[0].(map[string]any)["topic"].(string)
	if keys := view["keys"].([]any); len(keys) != 1 || keys[0].(map[string]any)["id"] != keyID {
		t.Fatal("subscription keys not listed", keys)
	}
	listed := req("GET", "/api/v1/access/api-keys", root, nil, 200)
	for _, value := range listed["items"].([]any) {
		if key := value.(map[string]any); key["id"] == keyID && len(key["topics"].([]any)) != 1 {
			t.Fatal("key does not list its granted topic", key)
		}
	}
	raw, _ := json.Marshal(view)
	if bytes.Contains(raw, []byte("secretHash")) {
		t.Fatal("key hash exposed in topic listing")
	}
	exchange := func(key string, status int) map[string]any {
		t.Helper()
		return requestJSON(t, server.Client(), "POST", server.URL+"/api/open/v1/message-topics/credentials", key, map[string]any{"protocol": "mqtt"}, status)
	}
	exchange("", 401)
	exchange(plainKey, 403)
	exchange(apiKey, 503)
	revoked := []string{}
	api.SetDeviceOperations(nil, func(_ context.Context, username string) error { revoked = append(revoked, username); return nil })
	api.SetMessageTopicMQTTReadiness(func(context.Context) error { return nil })
	credential := exchange(apiKey, 200)
	claims, err := api.auth.Parse(credential["password"].(string))
	if err != nil || claims.TokenUse != "topic-consumer" || claims.ACL[0].Action != "subscribe" {
		t.Fatal("invalid broker token", err)
	}
	if topics := credential["subscribeTopics"].([]any); len(topics) != 1 || topics[0] != target {
		t.Fatal("credential does not grant the exact topic", topics)
	}
	if again := exchange(apiKey, 200); again["username"] != credential["username"] {
		t.Fatal("exchange duplicated an active lease")
	}
	publish := func(device string) int {
		t.Helper()
		payload := []byte(`{"tenantId":"tenant_a","deviceId":"` + device + `"}`)
		if err := engine.Realtime.Publish(ctx, "/iot/parsed/tenant_a/p/"+device+"/PROPERTY_REPORT", payload, 1, false); err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, v := range realtime.Messages {
			if v.Topic == target {
				count++
			}
		}
		return count
	}
	if publish("d1") != 1 || publish("d2") != 1 {
		t.Fatal("query device scope not applied")
	}
	// A bound user scope change pauses the topic until the old broker identity
	// is revoked; the next exchange issues a new identity.
	state, _ = repo.LoadAccessState(ctx, "tenant_a")
	state.Users[0].DeviceIDs = []string{"d1", "d2"}
	if ok, err := repo.SaveAccessState(ctx, "tenant_a", state); err != nil || !ok {
		t.Fatal(err)
	}
	if publish("d1") != 1 {
		t.Fatal("stale permission snapshot still received data")
	}
	if err := api.RetryMessageTopicRevocationsOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(revoked, credential["username"].(string)) {
		t.Fatal("user scope change did not revoke broker identity")
	}
	if publish("d1") != 2 {
		t.Fatal("topic did not resume after revocation")
	}
	fresh := exchange(apiKey, 200)
	if fresh["username"] == credential["username"] {
		t.Fatal("revoked broker identity reused")
	}
	// Rotation keeps the key and its grants but revokes the old secret.
	rotated := req("POST", "/api/v1/access/api-keys/"+keyID+"/rotate", root, nil, 200)
	newKey := rotated["apiKey"].(string)
	exchange(apiKey, 401)
	if !slices.Contains(revoked, fresh["username"].(string)) {
		t.Fatal("rotation did not revoke issued credentials")
	}
	if next := exchange(newKey, 200); next["username"] == fresh["username"] {
		t.Fatal("rotated key reused old broker identity")
	}
	// Deleting the key drops its topic grants.
	req("DELETE", "/api/v1/access/api-keys/"+keyID, root, nil, 200)
	stored, _ := engine.MessageTopics.Load(ctx, "tenant_a")
	if len(stored.Topics[0].KeyIDs) != 0 {
		t.Fatal("deleted key kept topic grant")
	}
	exchange(newKey, 401)
	req("DELETE", "/api/v1/message-topics/"+stored.Topics[0].ID+"?revision="+strconv.FormatInt(stored.Revision, 10), root, nil, 200)
}

type topicKafkaAdminFake struct {
	readyErr, provisionErr, revokeErr error
	provisioned, revoked              []string
	duringProvision                   func()
	duringReady                       func()
}

func (f *topicKafkaAdminFake) Ready(context.Context) error {
	if f.duringReady != nil {
		f.duringReady()
	}
	return f.readyErr
}
func (f *topicKafkaAdminFake) Provision(_ context.Context, username, password, group string, topics []string) error {
	if password == "" || username != group || len(topics) == 0 {
		return errors.New("invalid broker input")
	}
	f.provisioned = append(f.provisioned, username)
	if f.duringProvision != nil {
		f.duringProvision()
	}
	return f.provisionErr
}
func (f *topicKafkaAdminFake) Revoke(_ context.Context, username string) error {
	f.revoked = append(f.revoked, username)
	return f.revokeErr
}

// kafkaTopicFixture stores one subscription key and one Kafka query topic.
func kafkaTopicFixture(t *testing.T, repo *memory.Repository, engine *core.Engine, expiresAt int64) {
	t.Helper()
	ctx := context.Background()
	state := model.AccessState{
		Users:   []model.PlatformUser{{Username: "u", Enabled: true, DeviceScope: "all", Permissions: []string{"menu:devices", "menu:messageTopics"}}},
		APIKeys: []model.APIKey{{ID: "key", Name: "consumer", Username: "u", Capabilities: []string{model.APICapabilityTopicsSubscribe}, SecretHash: apiKeySecretHash("secret"), Enabled: true, ExpiresAt: expiresAt}},
	}
	if ok, err := repo.SaveAccessState(ctx, "t", state); err != nil || !ok {
		t.Fatal(ok, err)
	}
	query := model.MessageTopicQuery{Dataset: "device_reports", Mode: "realtime", DeviceScope: "all"}
	topics := model.MessageTopicConfig{Topics: []model.MessageTopicRoute{{ID: "topic", Name: "设备上报", Protocol: "kafka", Topic: "device", Enabled: true, Query: &query, KeyIDs: []string{"key"}}}}
	if err := messagetopics.AccumulateQueryExposure(&topics, "topic", query); err != nil {
		t.Fatal(err)
	}
	if ok, err := engine.MessageTopics.Save(ctx, "t", topics); err != nil || !ok {
		t.Fatal(ok, err)
	}
}

func newKafkaTopicAPI(t *testing.T, secret string) (*Server, *memory.Repository, *core.Engine) {
	t.Helper()
	repo := memory.NewRepository()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Load()
	cfg.JWTSecret = secret
	cfg.KafkaBrokers = []string{"broker:9092"}
	cfg.KafkaPublicBrokers = cfg.KafkaBrokers
	engine := core.New(devicescope.Wrap(repo), nil, local.NewBus(), local.NewRealtime(), nil, log)
	return New(cfg, engine, metrics.New(), log), repo, engine
}

func TestMessageTopicKafkaCredentialRecoveryAndRenewal(t *testing.T) {
	ctx := context.Background()
	api, repo, engine := newKafkaTopicAPI(t, "kafka-topic-test-signing-key")
	kafkaTopicFixture(t, repo, engine, 0)
	broker := &topicKafkaAdminFake{readyErr: errors.New("anonymous broker")}
	api.SetMessageTopicKafkaAdmin(broker)
	if _, status, err := api.issueTopicCredential(ctx, "t", "key", "kafka"); err == nil || status != 503 {
		t.Fatal("insecure broker issued credentials", status, err)
	}
	current, _ := engine.MessageTopics.Load(ctx, "t")
	if len(current.Credentials) != 0 {
		t.Fatal("readiness failure persisted a credential")
	}
	broker.readyErr = nil
	broker.provisionErr = errors.New("partial ACL failure")
	if _, status, err := api.issueTopicCredential(ctx, "t", "key", "kafka"); err == nil || status != 503 {
		t.Fatal("provision failure not reported", status, err)
	}
	current, _ = engine.MessageTopics.Load(ctx, "t")
	if len(current.Credentials) != 1 || current.Credentials[0].Status != "revoking" {
		t.Fatal("provision failure lost durable revocation")
	}
	broker.revokeErr = errors.New("management unavailable")
	if err := api.RetryMessageTopicRevocationsOnce(ctx); err == nil {
		t.Fatal("revoke failure not reported")
	}
	current, _ = engine.MessageTopics.Load(ctx, "t")
	if current.Credentials[0].Status != "revoking" {
		t.Fatal("failed revoke recorded as complete")
	}
	broker.revokeErr, broker.provisionErr = nil, nil
	if err := api.RetryMessageTopicRevocationsOnce(ctx); err != nil {
		t.Fatal(err)
	}
	current, _ = engine.MessageTopics.Load(ctx, "t")
	if current.Credentials[0].Status != "revoked" {
		t.Fatal("confirmed revoke not recorded")
	}
	result, _, err := api.issueTopicCredential(ctx, "t", "key", "kafka")
	if err != nil {
		t.Fatal(err)
	}
	username, password := result["username"].(string), result["password"].(string)
	current, _ = engine.MessageTopics.Load(ctx, "t")
	encoded, _ := json.Marshal(current)
	if bytes.Contains(encoded, []byte(password)) {
		t.Fatal("stored broker password plaintext")
	}
	for i := range current.Credentials {
		if current.Credentials[i].Username == username {
			current.Credentials[i].ExpiresAt = time.Now().Add(3 * time.Minute).Unix()
		}
	}
	if ok, err := engine.MessageTopics.Save(ctx, "t", current); err != nil || !ok {
		t.Fatal(err)
	}
	renewed, _, err := api.issueTopicCredential(ctx, "t", "key", "kafka")
	if err != nil || renewed["username"] != username || renewed["password"] != password || renewed["expiresAt"].(int64) < time.Now().Add(55*time.Minute).Unix() {
		t.Fatal("lease renewal changed broker identity or did not extend", err)
	}
	// Rotating the server signing secret also invalidates deterministic Kafka
	// credentials, instead of returning a new password for an old SCRAM user.
	api.cfg.JWTSecret = "rotated-kafka-topic-test-signing-key"
	rotated, _, err := api.issueTopicCredential(ctx, "t", "key", "kafka")
	if err != nil || rotated["username"] == username {
		t.Fatal("server secret change reused old SCRAM identity", err)
	}
	// A policy change while the broker is provisioning cannot activate the
	// credential, and leaves a durable cleanup record.
	api.cfg.JWTSecret = "third-kafka-topic-test-signing-key"
	broker.duringProvision = func() {
		state, e := repo.LoadAccessState(ctx, "t")
		if e != nil {
			t.Fatal(e)
		}
		state.APIKeys[0].Enabled = false
		if ok, e := repo.SaveAccessState(ctx, "t", state); e != nil || !ok {
			t.Fatal(e)
		}
	}
	if _, status, err := api.issueTopicCredential(ctx, "t", "key", "kafka"); err == nil || status != 409 {
		t.Fatal("policy change during provisioning activated grant", status, err)
	}
	current, _ = engine.MessageTopics.Load(ctx, "t")
	for i, c := range current.Credentials {
		if c.Status == "active" && c.Username != rotated["username"] {
			t.Fatal("disabled key has a new active credential")
		}
		current.Credentials[i].ExpiresAt = time.Now().Add(-time.Minute).Unix()
	}
	if ok, err := engine.MessageTopics.Save(ctx, "t", current); err != nil || !ok {
		t.Fatal(err)
	}
	if err := api.RetryMessageTopicRevocationsOnce(ctx); err != nil {
		t.Fatal(err)
	}
	current, _ = engine.MessageTopics.Load(ctx, "t")
	if len(current.Credentials) != 0 {
		t.Fatal("expired revoked credentials not cleaned")
	}
}

func TestMessageTopicCredentialRechecksAuthorizationAfterBrokerReadiness(t *testing.T) {
	for _, scenario := range []string{"key disabled", "user permissions changed", "user disabled", "key expires"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			api, repo, engine := newKafkaTopicAPI(t, "readiness-race-signing-key")
			expires := int64(0)
			if scenario == "key expires" {
				expires = time.Now().Add(time.Second).UnixMilli()
			}
			kafkaTopicFixture(t, repo, engine, expires)
			broker := &topicKafkaAdminFake{}
			api.SetMessageTopicKafkaAdmin(broker)
			if _, _, err := api.issueTopicCredential(ctx, "t", "key", "kafka"); err != nil {
				t.Fatal(err)
			}
			change := func(edit func(*model.AccessState)) func() {
				return func() {
					state, err := repo.LoadAccessState(ctx, "t")
					if err != nil {
						t.Fatal(err)
					}
					edit(&state)
					if ok, err := repo.SaveAccessState(ctx, "t", state); !ok || err != nil {
						t.Fatal(ok, err)
					}
				}
			}
			switch scenario {
			case "key disabled":
				broker.duringReady = change(func(s *model.AccessState) { s.APIKeys[0].Enabled = false })
			case "user permissions changed":
				broker.duringReady = change(func(s *model.AccessState) {
					s.Users[0].Permissions = []string{"menu:devices", "menu:messageTopics", "menu:alarms"}
				})
			case "user disabled":
				broker.duringReady = change(func(s *model.AccessState) { s.Users[0].Enabled = false })
			case "key expires":
				broker.duringReady = func() { time.Sleep(time.Until(time.UnixMilli(expires).Add(10 * time.Millisecond))) }
			}
			// The issued lease is reused unless the key's policy changed, in
			// which case the stale lease must not be returned.
			result, status, err := api.issueTopicCredential(ctx, "t", "key", "kafka")
			if err == nil || result != nil || status != 409 {
				t.Fatalf("stale credential returned after readiness: status=%d error=%v", status, err)
			}
			if len(broker.provisioned) != 1 {
				t.Fatal("readiness race created another broker identity")
			}
		})
	}
}
