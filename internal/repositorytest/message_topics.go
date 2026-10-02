package repositorytest

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// MessageTopics exercises the same tenant, snapshot and revision guarantees on
// both repository implementations.
func MessageTopics(t *testing.T, store ports.MessageTopicStore) {
	t.Helper()
	ctx := context.Background()
	load := func(t *testing.T, tenant string) model.MessageTopicConfig {
		t.Helper()
		config, err := store.LoadMessageTopicConfig(ctx, tenant)
		if err != nil {
			t.Fatal(err)
		}
		return config
	}
	save := func(t *testing.T, tenant string, config model.MessageTopicConfig, expected bool) {
		t.Helper()
		ok, err := store.SaveMessageTopicConfig(ctx, tenant, config)
		if err != nil || ok != expected {
			t.Fatalf("save %s at revision %d: accepted=%t, want=%t, error=%v", tenant, config.Revision, ok, expected, err)
		}
	}
	t.Run("tenant isolation and stale revisions", func(t *testing.T) {
		config := load(t, "one")
		if config.Revision != 0 || len(config.Overrides) != 0 {
			t.Fatalf("unexpected initial configuration: %+v", config)
		}
		save(t, "one", model.MessageTopicConfig{Revision: 1}, false)
		config.Overrides = map[string]model.MessageTopicOverride{
			"mqtt.parsed": {Enabled: true, Topic: "/one/parsed", Description: "租户一解析数据"},
		}
		save(t, "one", config, true)
		save(t, "one", config, false)
		other := model.MessageTopicConfig{Overrides: map[string]model.MessageTopicOverride{
			"mqtt.parsed": {Enabled: false, Topic: "/two/parsed", Description: "租户二解析数据"},
		}}
		save(t, "two", other, true)
		if missing := load(t, "missing"); missing.Revision != 0 || len(missing.Overrides) != 0 {
			t.Fatalf("another tenant's configuration leaked: %+v", missing)
		}
		updated := load(t, "one")
		if updated.Revision != 1 || !reflect.DeepEqual(updated.Overrides, config.Overrides) {
			t.Fatalf("saved configuration not retained: %+v", updated)
		}
		updated.Overrides["mqtt.parsed"] = model.MessageTopicOverride{Enabled: false, Topic: "/one/next"}
		save(t, "one", updated, true)
		save(t, "one", updated, false)
		latest := load(t, "one")
		if latest.Revision != 2 || !reflect.DeepEqual(latest.Overrides, updated.Overrides) {
			t.Fatalf("latest configuration overwritten by stale revision: %+v", latest)
		}
		if got := load(t, "two"); got.Revision != 1 || !reflect.DeepEqual(got.Overrides, other.Overrides) {
			t.Fatalf("another tenant changed: %+v", got)
		}
		latest.Overrides = map[string]model.MessageTopicOverride{}
		save(t, "one", latest, true)
		if cleared := load(t, "one"); cleared.Revision != 3 || len(cleared.Overrides) != 0 {
			t.Fatalf("override removal not persisted: %+v", cleared)
		}
	})
	t.Run("input and output snapshot isolation", func(t *testing.T) {
		value := model.MessageTopicOverride{Enabled: true, Topic: "iot.parsed", Description: "Kafka 解析结果"}
		config := model.MessageTopicConfig{Overrides: map[string]model.MessageTopicOverride{"kafka.parsed": value}}
		save(t, "snapshots", config, true)
		config.Overrides["kafka.parsed"] = model.MessageTopicOverride{Topic: "input-mutated"}
		config.Overrides["new"] = value
		if config.Revision != 0 {
			t.Fatal("save changed the caller's revision")
		}
		got := load(t, "snapshots")
		if got.Revision != 1 || len(got.Overrides) != 1 || got.Overrides["kafka.parsed"] != value {
			t.Fatalf("input map mutation reached repository: %+v", got)
		}
		delete(got.Overrides, "kafka.parsed")
		got.Overrides["new"] = value
		if loaded := load(t, "snapshots"); loaded.Revision != 1 || len(loaded.Overrides) != 1 || loaded.Overrides["kafka.parsed"] != value {
			t.Fatalf("output map mutation reached repository: %+v", loaded)
		}
	})
	t.Run("concurrent writers have one winner", func(t *testing.T) {
		for revision := int64(0); revision < 2; revision++ {
			type result struct {
				topic string
				ok    bool
				err   error
			}
			results := make(chan result, 12)
			var wg sync.WaitGroup
			for i := 0; i < cap(results); i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					topic := fmt.Sprintf("iot.parsed.%d.%d", revision, i)
					ok, err := store.SaveMessageTopicConfig(ctx, "concurrent", model.MessageTopicConfig{
						Revision: revision,
						Overrides: map[string]model.MessageTopicOverride{
							"kafka.parsed": {Enabled: true, Topic: topic},
						},
					})
					results <- result{topic: topic, ok: ok, err: err}
				}(i)
			}
			wg.Wait()
			close(results)
			winner := ""
			for result := range results {
				if result.err != nil {
					t.Fatal(result.err)
				}
				if result.ok {
					if winner != "" {
						t.Fatal("multiple writes accepted with the same revision")
					}
					winner = result.topic
				}
			}
			got := load(t, "concurrent")
			if winner == "" || got.Revision != revision+1 || got.Overrides["kafka.parsed"].Topic != winner {
				t.Fatalf("winning configuration was not retained: %+v, winner=%s", got, winner)
			}
		}
	})
}
