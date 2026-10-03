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
	config := model.MessageTopicConfig{
		Topics:        []model.MessageTopicRoute{{ID: "persisted", Name: "持久主题", Protocol: "mqtt", Topic: "parsed", Enabled: true, Exposure: []model.MessageTopicExposure{{SourceID: "mqtt.parsed", DeviceScope: "selected", DeviceIDs: []string{"device"}}}, KeyIDs: []string{"key"}}},
		RetiredTopics: []string{"mqtt:/previous/topic"},
		Credentials:   []model.MessageTopicCredential{{ID: "credential", KeyID: "key", Protocol: "mqtt", Username: "broker-user", Topics: []string{"/exact/topic"}, Provisioning: true, Status: "revoking", ExpiresAt: 9999}},
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
	got.Topics[0].Name = "更新后的主题"
	if ok, err := reopened.SaveMessageTopicConfig(ctx, "persistent", got); err != nil || !ok {
		t.Fatalf("update after reconnect: %t, %v", ok, err)
	}
	if ok, err := reopened.SaveMessageTopicConfig(ctx, "persistent", config); err != nil || ok {
		t.Fatalf("old revision accepted after reconnect: %t, %v", ok, err)
	}
	latest, err := reopened.LoadMessageTopicConfig(ctx, "persistent")
	if err != nil || latest.Revision != 2 || !reflect.DeepEqual(latest.Topics, got.Topics) {
		t.Fatalf("reconnected update not retained: %+v, %v", latest, err)
	}
}
