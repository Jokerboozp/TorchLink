package postgres

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"iot-platform/internal/model"
	"iot-platform/internal/repositorytest"
)

func TestMessageTopics(t *testing.T) {
	repositorytest.MessageTopics(t, testRepository(t))
}

func TestMessageTopicDevices(t *testing.T) {
	repositorytest.MessageTopicDevices(t, testRepository(t))
}

func TestMessageTopicsReopen(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	config := model.MessageTopicConfig{Overrides: map[string]model.MessageTopicOverride{
		"mqtt.parsed":  {Enabled: true, Topic: "/iot/custom/{tenant}", Description: "自定义解析主题"},
		"kafka.parsed": {Enabled: false, Topic: "iot.custom"},
	},
		Topics:         []model.MessageTopicRoute{{ID: "persisted", Name: "持久主题", Protocol: "mqtt", Topic: "parsed", Enabled: true, Exposure: []model.MessageTopicExposure{{SourceID: "mqtt.parsed", DeviceScope: "selected", DeviceIDs: []string{"device"}}}}},
		Rules:          []model.MessageTopicRule{{ID: "rule", TopicID: "persisted", Name: "字段转换", SourceID: "mqtt.parsed", Enabled: true, DeviceScope: "selected", DeviceIDs: []string{"device"}, Format: "json", Fields: map[string]string{"temperature": "properties.temperature"}}},
		RetiredTopics:  []string{"mqtt:/previous/topic"},
		RoutingHistory: []model.MessageTopicReservedRoute{{SourceID: "mqtt.parsed", Topic: "/previous/{deviceId}"}},
		Accounts:       []model.MessageTopicAccount{{ID: "account", Username: "reader", TopicIDs: []string{"persisted"}, PublishTopicIDs: []string{"persisted"}, DeviceScope: "selected", DeviceIDs: []string{"device"}, SecretHash: "hash"}},
		Credentials:    []model.MessageTopicCredential{{ID: "credential", AccountID: "account", Protocol: "mqtt", Username: "broker-user", Topics: []string{"/exact/topic"}, PublishTopics: []string{"/write/topic"}, Provisioning: true, Status: "revoking", ExpiresAt: 9999}},
		Deleted:        []string{"kafka.property-report"},
	}
	config.Topics = append(config.Topics, model.MessageTopicRoute{ID: "query", Name: "查询主题", Protocol: "mqtt", Topic: "query", Enabled: true, Query: &model.MessageTopicQuery{Dataset: "device_reports", Mode: "realtime", DeviceScope: "all", Fields: map[string]string{"value": "properties.value"}, Filter: &model.MessageTopicFilter{Field: "properties.value", Operator: "gte", Value: json.Number("9007199254740993")}}})
	if ok, err := repo.SaveMessageTopicConfig(ctx, "persistent", config); err != nil || !ok {
		t.Fatalf("save configuration: %t, %v", ok, err)
	}
	poolConfig := repo.pool.Config()
	repo.pool.Close()
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	reopened := &Repository{pool: pool}
	if err := reopened.Migrate(ctx); err != nil {
		t.Fatal("migration not idempotent", err)
	}
	got, err := reopened.LoadMessageTopicConfig(ctx, "persistent")
	expected := config
	expected.Revision = 1
	if err != nil || !reflect.DeepEqual(got, expected) {
		t.Fatalf("configuration lost after reconnect: %+v, %v", got, err)
	}
	got.Overrides["kafka.parsed"] = model.MessageTopicOverride{Enabled: true, Topic: "iot.custom.next"}
	if ok, err := reopened.SaveMessageTopicConfig(ctx, "persistent", got); err != nil || !ok {
		t.Fatalf("update after reconnect: %t, %v", ok, err)
	}
	if ok, err := reopened.SaveMessageTopicConfig(ctx, "persistent", config); err != nil || ok {
		t.Fatalf("old revision accepted after reconnect: %t, %v", ok, err)
	}
	latest, err := reopened.LoadMessageTopicConfig(ctx, "persistent")
	if err != nil || latest.Revision != 2 || !reflect.DeepEqual(latest.Overrides, got.Overrides) {
		t.Fatalf("reconnected update not retained: %+v, %v", latest, err)
	}
}
