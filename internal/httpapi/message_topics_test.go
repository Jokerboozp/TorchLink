package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
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

func TestMessageTopicsPermissionsPersistenceAndPublishing(t *testing.T) {
	ctx := context.Background()
	repo := &topicAuditRepository{Repository: memory.NewRepository()}
	bus, realtime := local.NewBus(), local.NewRealtime()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(ScopedRepository(repo), nil, bus, realtime, nil, log)
	cfg := config.Load()
	cfg.AdminUser, cfg.AdminPassword, cfg.AdminTenants = "root", "root-password-test", []string{"tenant_a", "tenant_b"}
	cfg.JWTSecret = "test-message-topics-secret-at-least-32"
	cfg.MQTTBroker, cfg.KafkaBrokers = "tcp://broker.invalid:1883", []string{"kafka.invalid:9092"}
	cfg.KafkaPublicBrokers = cfg.KafkaBrokers
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
	if view["revision"] != float64(0) || len(view["items"].([]any)) < 30 {
		t.Fatal("missing initial topic catalog", view)
	}
	find := func(view map[string]any, id string) map[string]any {
		t.Helper()
		for _, value := range view["items"].([]any) {
			row := value.(map[string]any)
			if row["id"] == id {
				return row
			}
		}
		t.Fatal("topic missing", id)
		return nil
	}
	if find(view, "kafka.raw")["editable"] != false || find(view, "mqtt.parsed")["effectiveEnabled"] != true {
		t.Fatal("incorrect editability/runtime")
	}
	prefix := messagetopics.KafkaPrefix("tenant_a")
	input := map[string]any{"revision": 0, "enabled": true, "topic": prefix + "properties", "description": "测试发布"}
	req("PUT", "/api/v1/message-topics/kafka.raw", root, input, 422)
	req("DELETE", "/api/v1/message-topics/kafka.raw?revision=0", root, nil, 422)
	req("PUT", "/api/v1/message-topics/missing", root, input, 404)
	input["topic"] = messagetopics.KafkaPrefix("tenant_b") + "properties"
	req("PUT", "/api/v1/message-topics/kafka.property-report", root, input, 422)
	input["topic"] = prefix + "properties"
	view = req("PUT", "/api/v1/message-topics/kafka.property-report", root, input, 200)
	if view["revision"] != float64(1) || find(view, "kafka.property-report")["topic"] != input["topic"] {
		t.Fatal("saved route not returned", view)
	}
	req("PUT", "/api/v1/message-topics/kafka.property-report", root, input, 409)
	req("DELETE", "/api/v1/message-topics/kafka.property-report?revision=0", root, nil, 409)
	if otherView := req("GET", "/api/v1/message-topics", other, nil, 200); otherView["revision"] != float64(0) || find(otherView, "kafka.property-report")["overridden"] != false {
		t.Fatal("configuration crossed tenants")
	}
	var custom, original int
	_ = bus.Subscribe(ctx, prefix+"properties", "test-custom", func(context.Context, []byte) error { custom++; return nil })
	_ = bus.Subscribe(ctx, model.TopicPropertyReport, "test-original", func(context.Context, []byte) error { original++; return nil })
	if err := engine.Bus.Publish(ctx, model.TopicPropertyReport, "k", []byte(`{"tenantId":"tenant_a"}`)); err != nil {
		t.Fatal(err)
	}
	if err := engine.Bus.Publish(ctx, model.TopicPropertyReport, "k", []byte(`{"tenantId":"tenant_b"}`)); err != nil {
		t.Fatal(err)
	}
	if custom != 1 || original != 1 {
		t.Fatal("HTTP configuration did not reach publisher", custom, original)
	}
	input["revision"], input["enabled"], input["topic"] = 1, false, ""
	req("PUT", "/api/v1/message-topics/mqtt.parsed", root, input, 200)
	if err := engine.Realtime.Publish(ctx, "/iot/parsed/tenant_a/p/d/PROPERTY_REPORT", []byte(`{"tenantId":"tenant_a"}`), 1, false); err != nil {
		t.Fatal(err)
	}
	if len(realtime.Messages) != 0 {
		t.Fatal("disabled publication still sent")
	}
	// A new service sees the stored policy without sharing the API process cache.
	_, enabled, err := messagetopics.New(ScopedRepository(repo)).Resolve(ctx, "mqtt", "/iot/parsed/tenant_a/p/d/PROPERTY_REPORT", []byte(`{"tenantId":"tenant_a"}`))
	if err != nil || enabled {
		t.Fatal("policy did not survive service replacement", err)
	}
	view = req("POST", "/api/v1/message-topics/kafka.property-report/reset?revision=2", root, nil, 200)
	if find(view, "kafka.property-report")["overridden"] != false || view["revision"] != float64(3) {
		t.Fatal("reset failed")
	}
	if err := engine.Bus.Publish(ctx, model.TopicPropertyReport, "k", []byte(`{"tenantId":"tenant_a"}`)); err != nil {
		t.Fatal(err)
	}
	if original != 2 {
		t.Fatal("reset did not invalidate publisher cache")
	}
	// A menu allows inspecting configuration, but tenant-wide routing changes
	// require both an explicit action and full device scope.
	permissions := []string{"menu:messageTopics"}
	req("POST", "/api/v1/access/roles", root, map[string]any{"id": "topic_reader", "name": "主题查看", "permissions": permissions}, 200)
	req("POST", "/api/v1/access/users", root, map[string]any{"username": "topic_user", "password": "topic-user-password", "enabled": true, "roleIds": []string{"topic_reader"}, "deviceScope": "all"}, 200)
	user := login("topic_user", "topic-user-password", "tenant_a")
	req("GET", "/api/v1/message-topics", user, nil, 200)
	input["revision"] = 3
	req("PUT", "/api/v1/message-topics/mqtt.parsed", user, input, 403)
	permissions = append(permissions, "PUT /api/v1/message-topics/:id", "DELETE /api/v1/message-topics/:id")
	req("PUT", "/api/v1/access/roles/topic_reader", root, map[string]any{"name": "主题管理", "permissions": permissions}, 200)
	req("PUT", "/api/v1/message-topics/mqtt.parsed", user, input, 403)
	permissions = append(permissions, "menu:devices")
	req("PUT", "/api/v1/access/roles/topic_reader", root, map[string]any{"name": "主题管理", "permissions": permissions}, 200)
	req("PUT", "/api/v1/message-topics/mqtt.parsed", user, input, 200)
	req("PUT", "/api/v1/access/roles/topic_reader", root, map[string]any{"name": "无权限", "permissions": []string{}}, 200)
	req("GET", "/api/v1/message-topics", user, nil, 403)
	for _, event := range repo.audits {
		if strings.HasPrefix(event.Action, "message-topic.") {
			return
		}
	}
	t.Fatal("topic mutation audit missing")
}

type topicAuditRepository struct {
	*memory.Repository
	audits []model.AuditLog
}

func (r *topicAuditRepository) SaveAudit(ctx context.Context, event model.AuditLog) error {
	r.audits = append(r.audits, event)
	return r.Repository.SaveAudit(ctx, event)
}

func TestMessageTopicAccountsCRUDCredentialsAndBoundScope(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	realtime := local.NewRealtime()
	engine := core.New(ScopedRepository(repo), nil, local.NewBus(), realtime, nil, log)
	cfg := config.Load()
	cfg.AdminUser = "root"
	cfg.AdminPassword = "test-password"
	cfg.AdminTenants = []string{"tenant_a", "tenant_b"}
	cfg.JWTSecret = "topic-consumer-test-secret-long-enough"
	cfg.MQTTBroker = "tcp://mqtt.invalid:1883"
	cfg.MQTTPublicURL = cfg.MQTTBroker
	cfg.KafkaBrokers = []string{"kafka.invalid:9092"}
	cfg.KafkaPublicBrokers = cfg.KafkaBrokers
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
	state := model.AccessState{Users: []model.PlatformUser{{Username: "consumer", Enabled: true, DeviceScope: "selected", DeviceIDs: []string{"d1"}, Permissions: []string{"menu:devices"}}}}
	if ok, err := repo.SaveAccessState(ctx, "tenant_a", state); err != nil || !ok {
		t.Fatal(err)
	}
	view := req("POST", "/api/v1/message-topics", root, map[string]any{"revision": 0, "name": "测试解析流", "sourceId": "mqtt.parsed", "topic": "parsed_events", "enabled": true}, 200)
	routeID := ""
	for _, item := range view["items"].([]any) {
		v := item.(map[string]any)
		if v["custom"] == true {
			routeID = v["id"].(string)
		}
	}
	if routeID == "" {
		t.Fatal("created topic missing")
	}
	req("POST", "/api/v1/message-topics", root, map[string]any{"revision": 0, "name": "stale", "sourceId": "mqtt.parsed", "topic": "stale", "enabled": true}, 409)
	accountBody := map[string]any{"revision": 1, "name": "外部系统", "username": "consumer", "enabled": true, "topicIds": []string{routeID}, "deviceScope": "selected", "deviceIds": []string{"d2"}}
	req("POST", "/api/v1/message-topic-accounts", root, accountBody, 422)
	accountBody["deviceScope"], accountBody["deviceIds"] = "all", []string{}
	accountBody["topicIds"] = []string{"mqtt.alarm-raised"}
	req("POST", "/api/v1/message-topic-accounts", root, accountBody, 422)
	accountBody["topicIds"] = []string{routeID}
	view = req("POST", "/api/v1/message-topic-accounts", root, accountBody, 200)
	created := view["accountSecret"].(map[string]any)
	accountID, secret := created["id"].(string), created["secret"].(string)
	if len(secret) < 32 {
		t.Fatal("account secret too short")
	}
	raw, _ := json.Marshal(req("GET", "/api/v1/message-topics", root, nil, 200))
	if bytes.Contains(raw, []byte(secret)) || bytes.Contains(raw, []byte("secretHash")) {
		t.Fatal("account credentials exposed in listing")
	}
	req("POST", "/api/v1/message-topic-accounts/"+accountID+"/credentials", root, map[string]any{"revision": 2, "protocol": "mqtt"}, 503)
	revoked := []string{}
	api.SetDeviceOperations(nil, func(_ context.Context, username string) error { revoked = append(revoked, username); return nil })
	api.SetMessageTopicMQTTReadiness(func(context.Context) error { return nil })
	credential := req("POST", "/api/v1/message-topic-accounts/"+accountID+"/credentials", root, map[string]any{"revision": 2, "protocol": "mqtt"}, 200)
	token := credential["password"].(string)
	claims, err := api.auth.Parse(token)
	if err != nil || claims.TokenUse != "topic-consumer" || len(claims.ACL) != 3 || claims.ACL[0].Action != "subscribe" {
		t.Fatal("invalid isolated broker token", err)
	}
	req("GET", "/api/v1/message-topics", token, nil, 403)
	target := credential["topics"].([]any)[0].(string)
	if target == "/iot/parsed/tenant_a/p/d1/PROPERTY_REPORT" || strings.ContainsAny(target, "+#") {
		t.Fatal("shared or wildcard grant", target)
	}
	for _, id := range []string{"d1", "d2"} {
		payload := []byte(`{"tenantId":"tenant_a","deviceId":"` + id + `"}`)
		if err := engine.Realtime.Publish(ctx, "/iot/parsed/tenant_a/p/"+id+"/PROPERTY_REPORT", payload, 1, false); err != nil {
			t.Fatal(err)
		}
	}
	count := 0
	for _, v := range realtime.Messages {
		if v.Topic == target {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("bound device scope publication count = %d", count)
	}
	exchange := map[string]any{"tenantId": "tenant_a", "accountId": accountID, "secret": secret, "protocol": "mqtt"}
	again := req("POST", "/api/open/v1/message-topics/credentials", "", exchange, 200)
	if again["username"] != credential["username"] {
		t.Fatal("exchange duplicated active lease")
	}
	exchange["tenantId"] = "tenant_b"
	req("POST", "/api/open/v1/message-topics/credentials", "", exchange, 401)
	exchange["tenantId"], exchange["secret"] = "tenant_a", "wrong"
	req("POST", "/api/open/v1/message-topics/credentials", "", exchange, 401)
	exchange["secret"] = secret
	// A direct platform scope change immediately stops future dispatch and the
	// persistent reconciler revokes the old broker identity.
	state, _ = repo.LoadAccessState(ctx, "tenant_a")
	state.Users[0].DeviceIDs = []string{"d2"}
	if ok, err := repo.SaveAccessState(ctx, "tenant_a", state); err != nil || !ok {
		t.Fatal(err)
	}
	if err := engine.Realtime.Publish(ctx, "/iot/parsed/tenant_a/p/d1/PROPERTY_REPORT", []byte(`{"tenantId":"tenant_a","deviceId":"d1"}`), 1, false); err != nil {
		t.Fatal(err)
	}
	count = 0
	for _, v := range realtime.Messages {
		if v.Topic == target {
			count++
		}
	}
	if count != 1 {
		t.Fatal("old permission snapshot still received data")
	}
	if err := api.RetryMessageTopicRevocationsOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(revoked, credential["username"].(string)) {
		t.Fatal("user scope change did not revoke broker identity")
	}
	fresh := req("POST", "/api/open/v1/message-topics/credentials", "", exchange, 200)
	if fresh["username"] == credential["username"] {
		t.Fatal("revoked broker identity reused")
	}
	// Delete is a real removal; no implicit restore of the default route.
	current, _ := engine.MessageTopics.Load(ctx, "tenant_a")
	view = req("DELETE", "/api/v1/message-topics/"+routeID+"?revision="+strconv.FormatInt(current.Revision, 10), root, nil, 200)
	for _, item := range view["items"].([]any) {
		if item.(map[string]any)["id"] == routeID {
			t.Fatal("deleted custom route remains")
		}
	}
	stored, _ := engine.MessageTopics.Load(ctx, "tenant_a")
	if len(stored.Accounts[0].TopicIDs) != 0 {
		t.Fatal("deleted topic kept account grant")
	}
	req("POST", "/api/open/v1/message-topics/credentials", "", exchange, 422)
	rev := strconv.FormatInt(stored.Revision, 10)
	view = req("DELETE", "/api/v1/message-topics/mqtt.parsed?revision="+rev, root, nil, 200)
	for _, item := range view["items"].([]any) {
		if item.(map[string]any)["id"] == "mqtt.parsed" {
			t.Fatal("deleted built-in route restored")
		}
	}
	stored, _ = engine.MessageTopics.Load(ctx, "tenant_a")
	req("DELETE", "/api/v1/message-topic-accounts/"+accountID+"?revision="+strconv.FormatInt(stored.Revision, 10), root, nil, 200)
	req("POST", "/api/open/v1/message-topics/credentials", "", exchange, 401)
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

func TestMessageTopicKafkaCredentialRecoveryAndRenewal(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Load()
	cfg.JWTSecret = "kafka-topic-test-signing-key"
	cfg.KafkaBrokers = []string{"broker:9092"}
	cfg.KafkaPublicBrokers = cfg.KafkaBrokers
	engine := core.New(ScopedRepository(repo), nil, local.NewBus(), local.NewRealtime(), nil, log)
	api := New(cfg, engine, metrics.New(), log)
	_, err := repo.SaveAccessState(ctx, "t", model.AccessState{Users: []model.PlatformUser{{Username: "u", Enabled: true, DeviceScope: "all", Permissions: []string{"menu:devices"}}}})
	if err != nil {
		t.Fatal(err)
	}
	account := model.MessageTopicAccount{ID: "account", Name: "consumer", Username: "u", Enabled: true, TopicIDs: []string{"kafka.property-report"}, DeviceScope: "all", SecretHash: apiKeySecretHash("secret")}
	if ok, err := engine.MessageTopics.Save(ctx, "t", model.MessageTopicConfig{Accounts: []model.MessageTopicAccount{account}}); err != nil || !ok {
		t.Fatal(err)
	}
	broker := &topicKafkaAdminFake{readyErr: errors.New("anonymous broker")}
	api.SetMessageTopicKafkaAdmin(broker)
	if _, status, err := api.issueTopicCredential(ctx, "t", "account", "kafka", "secret"); err == nil || status != 503 {
		t.Fatal("insecure broker issued credentials", status, err)
	}
	current, _ := engine.MessageTopics.Load(ctx, "t")
	if len(current.Credentials) != 0 {
		t.Fatal("readiness failure persisted a credential")
	}
	broker.readyErr = nil
	broker.provisionErr = errors.New("partial ACL failure")
	if _, status, err := api.issueTopicCredential(ctx, "t", "account", "kafka", "secret"); err == nil || status != 503 {
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
	broker.revokeErr = nil
	broker.provisionErr = nil
	if err := api.RetryMessageTopicRevocationsOnce(ctx); err != nil {
		t.Fatal(err)
	}
	current, _ = engine.MessageTopics.Load(ctx, "t")
	if current.Credentials[0].Status != "revoked" {
		t.Fatal("confirmed revoke not recorded")
	}
	result, _, err := api.issueTopicCredential(ctx, "t", "account", "kafka", "secret")
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
	renewed, _, err := api.issueTopicCredential(ctx, "t", "account", "kafka", "secret")
	if err != nil || renewed["username"] != username || renewed["password"] != password || renewed["expiresAt"].(int64) < time.Now().Add(55*time.Minute).Unix() {
		t.Fatal("lease renewal changed broker identity or did not extend", err)
	}
	// Rotating the server signing secret also invalidates deterministic Kafka
	// credentials, instead of returning a new password for an old SCRAM user.
	api.cfg.JWTSecret = "rotated-kafka-topic-test-signing-key"
	rotated, _, err := api.issueTopicCredential(ctx, "t", "account", "kafka", "secret")
	if err != nil || rotated["username"] == username {
		t.Fatal("server secret change reused old SCRAM identity", err)
	}
	// A policy change while the broker is provisioning cannot activate the
	// credential, and leaves a durable cleanup record even if revocation fails.
	api.cfg.JWTSecret = "third-kafka-topic-test-signing-key"
	broker.duringProvision = func() {
		latest, e := engine.MessageTopics.Load(ctx, "t")
		if e != nil {
			t.Fatal(e)
		}
		latest.Accounts[0].Enabled = false
		for i := range latest.Credentials {
			latest.Credentials[i].Status = "revoking"
		}
		if ok, e := engine.MessageTopics.Save(ctx, "t", latest); e != nil || !ok {
			t.Fatal(e)
		}
	}
	if _, status, err := api.issueTopicCredential(ctx, "t", "account", "kafka", "secret"); err == nil || status != 409 {
		t.Fatal("policy change during provisioning activated grant", status, err)
	}
	current, _ = engine.MessageTopics.Load(ctx, "t")
	for i, c := range current.Credentials {
		if c.Status == "active" {
			t.Fatal("disabled account has active credential")
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
	for _, scenario := range []string{"account disabled", "user permissions changed", "user disabled", "account expires"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			repo := memory.NewRepository()
			log := slog.New(slog.NewTextHandler(io.Discard, nil))
			cfg := config.Load()
			cfg.JWTSecret = "readiness-race-signing-key"
			cfg.KafkaBrokers = []string{"broker:9092"}
			cfg.KafkaPublicBrokers = cfg.KafkaBrokers
			engine := core.New(ScopedRepository(repo), nil, local.NewBus(), local.NewRealtime(), nil, log)
			api := New(cfg, engine, metrics.New(), log)
			if ok, err := repo.SaveAccessState(ctx, "t", model.AccessState{Users: []model.PlatformUser{{Username: "u", Enabled: true, DeviceScope: "all", Permissions: []string{"menu:devices"}}}}); !ok || err != nil {
				t.Fatal(ok, err)
			}
			account := model.MessageTopicAccount{ID: "account", Name: "consumer", Username: "u", Enabled: true, TopicIDs: []string{"kafka.property-report"}, DeviceScope: "all", SecretHash: apiKeySecretHash("secret")}
			if ok, err := engine.MessageTopics.Save(ctx, "t", model.MessageTopicConfig{Accounts: []model.MessageTopicAccount{account}}); !ok || err != nil {
				t.Fatal(ok, err)
			}
			broker := &topicKafkaAdminFake{}
			api.SetMessageTopicKafkaAdmin(broker)
			if _, _, err := api.issueTopicCredential(ctx, "t", "account", "kafka", "secret"); err != nil {
				t.Fatal(err)
			}
			wantStatus := 409
			switch scenario {
			case "account disabled":
				broker.duringReady = func() {
					current, err := engine.MessageTopics.Load(ctx, "t")
					if err != nil {
						t.Fatal(err)
					}
					current.Accounts[0].Enabled = false
					current.Credentials[0].Status = "revoking"
					if ok, err := engine.MessageTopics.Save(ctx, "t", current); !ok || err != nil {
						t.Fatal(ok, err)
					}
				}
			case "user permissions changed", "user disabled":
				if scenario == "user disabled" {
					wantStatus = 403
				}
				broker.duringReady = func() {
					state, err := repo.LoadAccessState(ctx, "t")
					if err != nil {
						t.Fatal(err)
					}
					if scenario == "user disabled" {
						state.Users[0].Enabled = false
					} else {
						state.Users[0].Permissions = nil
					}
					if ok, err := repo.SaveAccessState(ctx, "t", state); !ok || err != nil {
						t.Fatal(ok, err)
					}
				}
			case "account expires":
				wantStatus = 403
				expires := time.Now().Unix() + 1
				current, err := engine.MessageTopics.Load(ctx, "t")
				if err != nil {
					t.Fatal(err)
				}
				current.Accounts[0].ExpiresAt = expires
				if ok, err := engine.MessageTopics.Save(ctx, "t", current); !ok || err != nil {
					t.Fatal(ok, err)
				}
				broker.duringReady = func() { time.Sleep(time.Until(time.Unix(expires, 0).Add(10 * time.Millisecond))) }
			}
			result, status, err := api.issueTopicCredential(ctx, "t", "account", "kafka", "secret")
			if err == nil || result != nil || status != wantStatus {
				t.Fatalf("stale or expired credential returned after readiness: status=%d want=%d error=%v", status, wantStatus, err)
			}
			if len(broker.provisioned) != 1 {
				t.Fatal("readiness race created another broker identity")
			}
		})
	}
}

type topicRevisionRaceRepository struct {
	*memory.Repository
	afterLoad func()
}

func (r *topicRevisionRaceRepository) LoadMessageTopicConfig(ctx context.Context, tenant string) (model.MessageTopicConfig, error) {
	cfg, err := r.Repository.LoadMessageTopicConfig(ctx, tenant)
	if r.afterLoad != nil {
		after := r.afterLoad
		r.afterLoad = nil
		after()
	}
	return cfg, err
}

func TestMessageTopicAdminCredentialIssueHonorsRevisionAcrossReload(t *testing.T) {
	ctx := context.Background()
	repo := &topicRevisionRaceRepository{Repository: memory.NewRepository()}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Load()
	cfg.JWTSecret = "admin-revision-race-key"
	cfg.AdminUser = "root"
	cfg.AdminPassword = "test-root-password"
	cfg.AdminTenants = []string{"t"}
	cfg.KafkaBrokers = []string{"broker:9092"}
	cfg.KafkaPublicBrokers = cfg.KafkaBrokers
	engine := core.New(ScopedRepository(repo), nil, local.NewBus(), local.NewRealtime(), nil, log)
	api := New(cfg, engine, metrics.New(), log)
	if ok, err := repo.SaveAccessState(ctx, "t", model.AccessState{Users: []model.PlatformUser{{Username: "u", Enabled: true, DeviceScope: "all", Permissions: []string{"menu:devices"}}}}); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if ok, err := engine.MessageTopics.Save(ctx, "t", model.MessageTopicConfig{Accounts: []model.MessageTopicAccount{{ID: "account", Name: "consumer", Username: "u", Enabled: true, TopicIDs: []string{"kafka.property-report"}, DeviceScope: "all"}}}); !ok || err != nil {
		t.Fatal(ok, err)
	}
	broker := &topicKafkaAdminFake{}
	api.SetMessageTopicKafkaAdmin(broker)
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	login := requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/auth/login", "", map[string]string{"username": "root", "password": cfg.AdminPassword, "tenantId": "t"}, 200)
	root := login["accessToken"].(string)
	current, err := engine.MessageTopics.Load(ctx, "t")
	if err != nil {
		t.Fatal(err)
	}
	repo.afterLoad = func() {
		latest, err := repo.Repository.LoadMessageTopicConfig(ctx, "t")
		if err != nil {
			t.Fatal(err)
		}
		latest.Accounts[0].Name = "edited after request validation"
		if ok, err := repo.Repository.SaveMessageTopicConfig(ctx, "t", latest); !ok || err != nil {
			t.Fatal(ok, err)
		}
	}
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/message-topic-accounts/account/credentials", root, map[string]any{"revision": current.Revision, "protocol": "kafka"}, 409)
	if len(broker.provisioned) != 0 {
		t.Fatal("stale administrative request provisioned broker identity")
	}
	latest, err := engine.MessageTopics.Load(ctx, "t")
	if err != nil || len(latest.Credentials) != 0 {
		t.Fatal("stale request persisted credentials", err)
	}
}
