package messagetopics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type testStore struct {
	Store
	mu        sync.Mutex
	configs   map[string]model.MessageTopicConfig
	err       error
	loads     int
	saves     int
	afterLoad func()
}

func newTestStore() *testStore {
	return &testStore{configs: map[string]model.MessageTopicConfig{}}
}

func (r *testStore) LoadMessageTopicConfig(_ context.Context, tenant string) (model.MessageTopicConfig, error) {
	r.mu.Lock()
	r.loads++
	cfg, err, after := cloneConfig(r.configs[tenant]), r.err, r.afterLoad
	r.mu.Unlock()
	if after != nil {
		after()
	}
	return cfg, err
}

func (r *testStore) SaveMessageTopicConfig(_ context.Context, tenant string, cfg model.MessageTopicConfig) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.saves++
	if r.err != nil {
		return false, r.err
	}
	if r.configs[tenant].Revision != cfg.Revision {
		return false, nil
	}
	cfg = cloneConfig(cfg)
	cfg.Revision++
	r.configs[tenant] = cfg
	return true, nil
}

func body(tenant string) []byte {
	b, _ := json.Marshal(map[string]string{"tenantId": tenant, "productId": "p", "deviceId": "d", "messageType": "PROPERTY_REPORT", "alarmId": "a", "cameraId": "c"})
	return b
}

func TestCatalogIncludesEveryKafkaTopicAndIsIndependent(t *testing.T) {
	seen, ids := map[string]bool{}, map[string]bool{}
	for _, topic := range Catalog() {
		if ids[topic.ID] {
			t.Fatalf("duplicate ID %s", topic.ID)
		}
		ids[topic.ID] = true
		if topic.Protocol == "kafka" {
			seen[topic.DefaultTopic] = true
		}
	}
	for _, topic := range model.AllTopics() {
		if !seen[topic] {
			t.Errorf("formal Kafka topic %q missing", topic)
		}
	}
	a := Catalog()
	for i := range a {
		a[i].Name = "changed"
		if len(a[i].Variables) > 0 {
			a[i].Variables[0] = "changed"
		}
	}
	for _, topic := range Catalog() {
		if topic.Name == "changed" || (len(topic.Variables) > 0 && topic.Variables[0] == "changed") {
			t.Fatal("caller mutated catalog")
		}
	}
}

func TestPublicationCacheSkipsQuietTenantsAndFailsOpenForSource(t *testing.T) {
	repo := newTestStore()
	svc := New(repo)
	now := time.Unix(1000, 0)
	svc.now = func() time.Time { return now }
	ctx := context.Background()
	source := "/iot/parsed/t/p/d/PROPERTY_REPORT"
	payload := []byte(`{"tenantId":"t","deviceId":"d","properties":{"temperature":27}}`)
	for i := 0; i < 2; i++ {
		if pubs := svc.Publications(ctx, "mqtt", source, payload); len(pubs) != 1 || pubs[0].Topic != source {
			t.Fatal("source publication changed", pubs)
		}
	}
	if repo.loads != 1 {
		t.Fatal("quiet tenant policy was not cached", repo.loads)
	}
	// A topic saved by another process becomes visible within the short TTL.
	cfg := queryFixture(t, "mqtt")
	if ok, err := repo.SaveMessageTopicConfig(ctx, "t", cfg); !ok || err != nil {
		t.Fatal(ok, err)
	}
	now = now.Add(cacheTTL)
	if pubs := svc.Publications(ctx, "mqtt", source, payload); len(pubs) != 2 {
		t.Fatal("new topic not published after cache expiry", pubs)
	}
	repo.err = errors.New("storage unavailable")
	now = now.Add(cacheTTL)
	if pubs := svc.Publications(ctx, "mqtt", source, payload); len(pubs) != 1 || pubs[0].Topic != source {
		t.Fatal("policy failure blocked or widened publication", pubs)
	}
	repo.err = nil
	for i := 0; i < maxCacheSize+20; i++ {
		svc.Publications(ctx, "mqtt", fmt.Sprintf("/iot/parsed/t%d/p/d/PROPERTY_REPORT", i), []byte(fmt.Sprintf(`{"tenantId":"t%d"}`, i)))
	}
	if len(svc.cache) > maxCacheSize {
		t.Fatal("cache is unbounded", len(svc.cache))
	}
}

func TestSlowPolicyReadDoesNotExtendStaleSnapshotLifetime(t *testing.T) {
	repo := newTestStore()
	svc := New(repo)
	now := time.Unix(100, 0)
	svc.now = func() time.Time { return now }
	repo.afterLoad = func() { now = now.Add(cacheTTL) }
	if _, err := svc.cached(context.Background(), "t"); err == nil {
		t.Fatal("delayed snapshot was accepted")
	}
	if len(svc.cache) != 0 {
		t.Fatal("delayed snapshot was cached")
	}
}

type busRecorder struct {
	ports.EventBus
	topics                      []string
	key                         string
	payload                     []byte
	err                         error
	subscribed, healthy, closed bool
}

func (b *busRecorder) Publish(_ context.Context, topic, key string, payload []byte) error {
	b.topics = append(b.topics, topic)
	b.key, b.payload = key, append([]byte(nil), payload...)
	return b.err
}
func (b *busRecorder) Subscribe(context.Context, string, string, ports.Handler) error {
	b.subscribed = true
	return b.err
}
func (b *busRecorder) Health(context.Context) error { b.healthy = true; return b.err }
func (b *busRecorder) Close() error                 { b.closed = true; return b.err }

type realtimeRecorder struct {
	ports.RealtimePublisher
	topics   []string
	qos      byte
	retained bool
	err      error
}

func (p *realtimeRecorder) Publish(_ context.Context, topic string, _ []byte, qos byte, retained bool) error {
	p.topics = append(p.topics, topic)
	p.qos, p.retained = qos, retained
	return p.err
}

func TestWrappersKeepInternalTransportContractsAndPublishErrors(t *testing.T) {
	repo := newTestStore()
	svc := New(repo)
	ctx := context.Background()
	if svc.WrapBus(nil) != nil || svc.WrapRealtime(nil) != nil {
		t.Fatal("nil adapter semantics changed")
	}
	b, p := &busRecorder{}, &realtimeRecorder{}
	bus, mqtt := svc.WrapBus(b), svc.WrapRealtime(p)
	repo.err = errors.New("unavailable")
	for _, topic := range Catalog() {
		if topic.Editable {
			continue
		}
		var err error
		if topic.Protocol == "kafka" {
			err = bus.Publish(ctx, topic.DefaultTopic, "key", []byte("internal bytes"))
		} else {
			err = mqtt.Publish(ctx, topic.DefaultTopic, []byte("device bytes"), 1, true)
		}
		if err != nil {
			t.Fatalf("protected topic consulted policy store: %s %v", topic.ID, err)
		}
	}
	if repo.loads != 0 || !p.retained || p.qos != 1 {
		t.Fatal("internal delivery contract changed", repo.loads, p.retained, p.qos)
	}
	if err := bus.Subscribe(ctx, model.TopicRaw, "parser", nil); err != nil {
		t.Fatal(err)
	}
	if err := bus.Health(ctx); err != nil {
		t.Fatal(err)
	}
	if err := bus.Close(); err != nil {
		t.Fatal(err)
	}
	if !b.subscribed || !b.healthy || !b.closed {
		t.Fatal("event bus methods not forwarded")
	}
	before := len(b.topics)
	if err := bus.Publish(ctx, model.TopicPropertyReport, "k", body("t")); err != nil || len(b.topics) != before+1 {
		t.Fatal("policy failure blocked the source publication", err)
	}
	repo.err = nil
	p.err = errors.New("broker rejected")
	if err := mqtt.Publish(ctx, "/iot/parsed/t/p/d/PROPERTY_REPORT", body("t"), 2, false); !errors.Is(err, p.err) || p.qos != 2 || p.retained {
		t.Fatal("MQTT error or delivery flags were lost", err)
	}
	b.err = errors.New("Kafka unavailable")
	if err := bus.Publish(ctx, model.TopicEventReport, "ordered-key", body("t")); !errors.Is(err, b.err) || b.key != "ordered-key" || string(b.payload) != string(body("t")) {
		t.Fatal("Kafka error, key or payload were lost", err)
	}
}
