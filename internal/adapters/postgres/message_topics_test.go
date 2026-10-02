package postgres

import (
	"context"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"iot-platform/internal/model"
	"iot-platform/internal/repositorytest"
)

func TestMessageTopics(t *testing.T) {
	repositorytest.MessageTopics(t, testRepository(t))
}

func TestMessageTopicsReopen(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	config := model.MessageTopicConfig{Overrides: map[string]model.MessageTopicOverride{
		"mqtt.parsed":  {Enabled: true, Topic: "/iot/custom/{tenant}", Description: "自定义解析主题"},
		"kafka.parsed": {Enabled: false, Topic: "iot.custom"},
	}}
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
	if err != nil || got.Revision != 1 || !reflect.DeepEqual(got.Overrides, config.Overrides) {
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
