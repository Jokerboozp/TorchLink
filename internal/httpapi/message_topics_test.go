package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

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
	view = req("DELETE", "/api/v1/message-topics/kafka.property-report?revision=2", root, nil, 200)
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
