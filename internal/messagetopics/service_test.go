package messagetopics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type testStore struct {
	ports.Repository
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

func configuration(id string, enabled bool, topic string) model.MessageTopicConfig {
	return model.MessageTopicConfig{Overrides: map[string]model.MessageTopicOverride{id: {Enabled: enabled, Topic: topic}}}
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

func TestRoutingTenantIsolationDisableRestoreAndCAS(t *testing.T) {
	ctx := context.Background()
	repo := newTestStore()
	svc := New(repo)
	target := KafkaPrefix("t") + "properties"
	cfg := configuration("kafka.property-report", true, target)
	if ok, err := svc.Save(ctx, "t", cfg); !ok || err != nil {
		t.Fatal(ok, err)
	}
	for _, tt := range []struct{ tenant, want string }{{"t", target}, {"other", model.TopicPropertyReport}} {
		got, enabled, err := svc.Resolve(ctx, "kafka", model.TopicPropertyReport, body(tt.tenant))
		if err != nil || !enabled || got != tt.want {
			t.Fatalf("tenant %q: %q %v %v", tt.tenant, got, enabled, err)
		}
	}
	if ok, err := svc.Save(ctx, "t", cfg); ok || err != nil {
		t.Fatalf("stale revision accepted: %v %v", ok, err)
	}
	cfg, err := svc.Load(ctx, "t")
	if err != nil || cfg.Revision != 1 {
		t.Fatal(cfg, err)
	}
	cfg.Overrides["kafka.property-report"] = model.MessageTopicOverride{Enabled: false}
	if ok, err := svc.Save(ctx, "t", cfg); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if _, enabled, err := svc.Resolve(ctx, "kafka", model.TopicPropertyReport, body("t")); err != nil || enabled {
		t.Fatalf("disable was not immediate: enabled=%v err=%v", enabled, err)
	}
	cfg, _ = svc.Load(ctx, "t")
	cfg.Overrides["kafka.property-report"] = model.MessageTopicOverride{Enabled: true, Topic: model.TopicPropertyReport}
	if ok, err := svc.Save(ctx, "t", cfg); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if got, enabled, err := svc.Resolve(ctx, "kafka", model.TopicPropertyReport, body("t")); err != nil || !enabled || got != model.TopicPropertyReport {
		t.Fatalf("default restore failed: %q %v %v", got, enabled, err)
	}
}

func TestEveryEditableTopicRoutesAndCanBeDisabled(t *testing.T) {
	ctx := context.Background()
	for _, topic := range Catalog() {
		if !topic.Editable {
			continue
		}
		t.Run(topic.ID, func(t *testing.T) {
			svc := New(newTestStore())
			source := topic.DefaultTopic
			if topic.Protocol == "mqtt" {
				source = strings.NewReplacer("{tenantId}", "t", "{productId}", "p", "{deviceId}", "d", "{messageType}", "PROPERTY_REPORT", "{cityCode}", "city", "{districtCode}", "district", "{buildingId}", "building", "{deviceType}", "type").Replace(source)
			}
			target := KafkaPrefix("t") + "events"
			if topic.Protocol == "mqtt" {
				target = MQTTPrefix("t") + "events"
			}
			cfg := configuration(topic.ID, true, target)
			if ok, err := svc.Save(ctx, "t", cfg); !ok || err != nil {
				t.Fatal(ok, err)
			}
			if got, enabled, err := svc.Resolve(ctx, topic.Protocol, source, body("t")); err != nil || !enabled || got != target {
				t.Fatalf("not routed: %q %v %v", got, enabled, err)
			}
			cfg.Revision++
			cfg.Overrides[topic.ID] = model.MessageTopicOverride{Enabled: false}
			if ok, err := svc.Save(ctx, "t", cfg); !ok || err != nil {
				t.Fatal(ok, err)
			}
			if _, enabled, err := svc.Resolve(ctx, topic.Protocol, source, body("t")); err != nil || enabled {
				t.Fatalf("not disabled: %v %v", enabled, err)
			}
		})
	}
}

func TestValidationRejectsCrossTenantReservedAndInvalidDestinations(t *testing.T) {
	cases := []struct{ id, destination string }{
		{"kafka.property-report", KafkaPrefix("other") + "data"},
		{"kafka.property-report", KafkaPrefix("t") + "managed.credential.route"},
		{"kafka.property-report", model.TopicRaw},
		{"kafka.property-report", KafkaPrefix("t") + "data/{deviceId}"},
		{"kafka.property-report", KafkaPrefix("t") + "data space"},
		{"kafka.property-report", KafkaPrefix("t")},
		{"kafka.property-report", KafkaPrefix("t") + strings.Repeat("x", 250)},
		{"mqtt.parsed", MQTTPrefix("other") + "data"},
		{"mqtt.parsed", MQTTPrefix("t") + "managed/credential/route"},
		{"mqtt.parsed", "/iot/up/t/p/d/property"},
		{"mqtt.parsed", MQTTPrefix("t") + "data/+"},
		{"mqtt.parsed", MQTTPrefix("t") + "data/#"},
		{"mqtt.parsed", MQTTPrefix("t") + "data//nested"},
		{"mqtt.parsed", MQTTPrefix("t") + "data/"},
		{"mqtt.parsed", MQTTPrefix("t") + "data\x00"},
		{"mqtt.parsed", MQTTPrefix("t") + "data space"},
		{"mqtt.parsed", MQTTPrefix("t") + "{alarmId}"},
		{"mqtt.parsed", MQTTPrefix("t") + "{deviceId"},
		{"mqtt.parsed", MQTTPrefix("t") + "{unknown}"},
		{"mqtt.parsed", MQTTPrefix("t") + "{deviceId}}"},
		{"mqtt.parsed", MQTTPrefix("t") + strings.Repeat("x", 1025)},
		{"mqtt.alarm-ai-analysis", MQTTPrefix("t") + "{deviceId}"},
		{"mqtt.alarm-raised", MQTTPrefix("t") + "{productId}"},
		{"mqtt.ui-action", MQTTPrefix("t") + "{cameraId}"},
		{"kafka.raw", ""},
		{"mqtt.state", ""},
		{"not-in-catalog", ""},
	}
	repo := newTestStore()
	svc := New(repo)
	for _, tt := range cases {
		t.Run(tt.id+":"+tt.destination, func(t *testing.T) {
			if ok, err := svc.Save(context.Background(), "t", configuration(tt.id, true, tt.destination)); ok || !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("invalid configuration accepted: %v %v", ok, err)
			}
		})
	}
	cfg := configuration("mqtt.parsed", true, "")
	cfg.Overrides["mqtt.parsed"] = model.MessageTopicOverride{Enabled: true, Description: strings.Repeat("字", 501)}
	if err := Validate("t", cfg); !errors.Is(err, ErrInvalidConfig) {
		t.Fatal("description length accepted", err)
	}
	if err := Validate("", configuration("mqtt.parsed", true, "")); !errors.Is(err, ErrInvalidConfig) {
		t.Fatal("empty tenant accepted", err)
	}
	if repo.saves != 0 {
		t.Fatal("invalid input reached storage")
	}
	if KafkaPrefix("a/b") == KafkaPrefix("a_b") || MQTTPrefix("a/b") == MQTTPrefix("a_b") {
		t.Fatal("tenant identity collision")
	}
}

func TestMQTTRenderRequiresRealNonemptyFieldsAndPreservesDefaults(t *testing.T) {
	repo := newTestStore()
	svc := New(repo)
	ctx := context.Background()
	source := "/iot/parsed/t/p/d/PROPERTY_REPORT"
	target := MQTTPrefix("t") + "{tenantId}/{productId}/{deviceId}/{messageType}"
	if ok, err := svc.Save(ctx, "t", configuration("mqtt.parsed", true, target)); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if got, enabled, err := svc.Resolve(ctx, "mqtt", source, body("t")); err != nil || !enabled || got != MQTTPrefix("t")+"t/p/d/PROPERTY_REPORT" {
		t.Fatalf("render: %q %v %v", got, enabled, err)
	}
	for _, payload := range []string{
		`{}`, `null`, `[]`, `invalid`, `{"tenantId":""}`, `{"tenantId":7}`,
		`{"tenantId":"t","productId":"p","deviceId":"d"}`,
		`{"tenantId":"t","productId":"p","deviceId":"","messageType":"PROPERTY_REPORT"}`,
		`{"tenantId":"t","productId":"p","deviceId":"a/b","messageType":"PROPERTY_REPORT"}`,
		`{"tenantId":"t","productId":"p","deviceId":"+","messageType":"PROPERTY_REPORT"}`,
		`{"tenantId":"t","productId":"p","deviceId":123,"messageType":"PROPERTY_REPORT"}`,
	} {
		if _, enabled, err := svc.Resolve(ctx, "mqtt", source, []byte(payload)); err == nil || enabled {
			t.Errorf("invalid payload passed: %s enabled=%v err=%v", payload, enabled, err)
		}
	}
	if _, enabled, err := svc.Resolve(ctx, "mqtt", source, body("other")); err == nil || enabled {
		t.Fatal("source tenant mismatch passed", enabled, err)
	}
	for _, topic := range Catalog() {
		if topic.Protocol == "mqtt" && topic.Editable {
			if err := Validate("t", configuration(topic.ID, true, topic.DefaultTopic)); err != nil {
				t.Fatalf("default template rejected: %s %v", topic.ID, err)
			}
		}
	}
	cfg, _ := svc.Load(ctx, "t")
	template, _ := topicByID("mqtt.parsed")
	cfg.Overrides[template.ID] = model.MessageTopicOverride{Enabled: true, Topic: template.DefaultTopic}
	if ok, err := svc.Save(ctx, "t", cfg); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if got, enabled, err := svc.Resolve(ctx, "mqtt", source, body("t")); err != nil || !enabled || got != source {
		t.Fatalf("default template was rendered instead of preserving source: %q %v %v", got, enabled, err)
	}
}

func TestCacheRefreshBoundAndFailClosed(t *testing.T) {
	repo := newTestStore()
	svc := New(repo)
	now := time.Unix(100, 0)
	svc.now = func() time.Time { return now }
	ctx := context.Background()
	resolve := func(tenant string) (string, bool, error) {
		return svc.Resolve(ctx, "kafka", model.TopicPropertyReport, body(tenant))
	}
	if _, _, err := resolve("t"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := resolve("t"); err != nil || repo.loads != 1 {
		t.Fatal("cache was not reused", repo.loads, err)
	}
	// Simulate a policy saved by another process. Load must read it immediately;
	// runtime Resolve refreshes within the documented short TTL.
	if ok, err := repo.SaveMessageTopicConfig(ctx, "t", configuration("kafka.property-report", false, "")); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if cfg, err := svc.Load(ctx, "t"); err != nil || cfg.Revision != 1 || cfg.Overrides["kafka.property-report"].Enabled {
		t.Fatal("Load returned cached policy", cfg, err)
	}
	now = now.Add(cacheTTL)
	if _, enabled, err := resolve("t"); err != nil || enabled {
		t.Fatal("cross-process update did not refresh", enabled, err)
	}
	repo.err = errors.New("storage unavailable")
	now = now.Add(cacheTTL)
	if _, enabled, err := resolve("t"); err == nil || enabled {
		t.Fatal("expired deny policy fell back to allow", enabled, err)
	}
	if _, enabled, err := resolve("new-tenant"); err == nil || enabled {
		t.Fatal("unknown policy fell back to allow", enabled, err)
	}
	// Broken stored policy must also fail closed, including after a restart.
	repo.err = nil
	repo.configs["broken"] = configuration("kafka.property-report", true, model.TopicRaw)
	if _, enabled, err := resolve("broken"); err == nil || enabled {
		t.Fatal("invalid stored policy passed", enabled, err)
	}
	for i := 0; i < maxCacheSize+20; i++ {
		if _, _, err := resolve(fmt.Sprintf("t%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if len(svc.cache) > maxCacheSize {
		t.Fatal("cache is unbounded", len(svc.cache))
	}
}

func TestConcurrentSaveDoesNotResurrectInflightOldPolicy(t *testing.T) {
	repo := newTestStore()
	svc := New(repo)
	loaded, resume := make(chan struct{}), make(chan struct{})
	var once sync.Once
	repo.afterLoad = func() { once.Do(func() { close(loaded); <-resume }) }
	result := make(chan error, 1)
	go func() {
		_, enabled, err := svc.Resolve(context.Background(), "kafka", model.TopicPropertyReport, body("t"))
		if err == nil && enabled {
			err = errors.New("inflight old policy overwrote the completed save")
		}
		result <- err
	}()
	<-loaded
	if ok, err := svc.Save(context.Background(), "t", configuration("kafka.property-report", false, "")); !ok || err != nil {
		t.Fatal(ok, err)
	}
	close(resume)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestSlowPolicyReadDoesNotExtendStaleSnapshotLifetime(t *testing.T) {
	repo := newTestStore()
	svc := New(repo)
	now := time.Unix(100, 0)
	svc.now = func() time.Time { return now }
	// The read takes its old snapshot, another instance disables publication,
	// and only then does the delayed read complete after the cache TTL.
	repo.afterLoad = func() {
		now = now.Add(cacheTTL)
		if saved, err := repo.SaveMessageTopicConfig(context.Background(), "t", configuration("kafka.property-report", false, "")); err != nil || !saved {
			t.Fatal(saved, err)
		}
	}
	if _, enabled, err := svc.Resolve(context.Background(), "kafka", model.TopicPropertyReport, body("t")); err == nil || enabled {
		t.Fatalf("delayed old snapshot was used: enabled=%v err=%v", enabled, err)
	}
	if len(svc.cache) != 0 {
		t.Fatal("delayed snapshot was cached")
	}
	repo.afterLoad = nil
	if _, enabled, err := svc.Resolve(context.Background(), "kafka", model.TopicPropertyReport, body("t")); err != nil || enabled {
		t.Fatalf("retry missed latest policy: enabled=%v err=%v", enabled, err)
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

type capacityBusRecorder struct {
	busRecorder
	previewed, cleaned bool
}

func (b *capacityBusRecorder) PreviewCapacityQueue(_ context.Context, tenant string, batch model.CapacityCleanupBatch) (ports.CapacityQueuePlan, error) {
	b.previewed = tenant == "t"
	return ports.CapacityQueuePlan{}, b.err
}

func (b *capacityBusRecorder) CleanupCapacityQueue(_ context.Context, tenant string, batch model.CapacityCleanupBatch, plan ports.CapacityQueuePlan) (ports.RuntimeCleanupCounts, error) {
	b.cleaned = tenant == "t"
	return ports.RuntimeCleanupCounts{}, b.err
}

func TestBusWrapperPreservesOptionalCapacityCleanup(t *testing.T) {
	svc := New(newTestStore())
	plain := svc.WrapBus(&busRecorder{})
	if _, ok := plain.(ports.CapacityQueueCleaner); ok {
		t.Fatal("plain bus incorrectly gained capacity cleanup support")
	}
	broker := &capacityBusRecorder{}
	wrapped := svc.WrapBus(broker)
	cleaner, ok := wrapped.(ports.CapacityQueueCleaner)
	if !ok {
		t.Fatal("wrapping the broker hid capacity queue cleanup")
	}
	ctx := context.Background()
	plan, err := cleaner.PreviewCapacityQueue(ctx, "t", model.CapacityCleanupBatch{})
	if err != nil || !broker.previewed {
		t.Fatal("capacity preview not forwarded", err)
	}
	broker.err = errors.New("broker cleanup refused")
	if _, err = cleaner.CleanupCapacityQueue(ctx, "t", model.CapacityCleanupBatch{}, plan); !errors.Is(err, broker.err) || !broker.cleaned {
		t.Fatal("capacity cleanup or its error was not forwarded", err)
	}
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
	if err := bus.Publish(ctx, model.TopicPropertyReport, "k", body("t")); err == nil || len(b.topics) != before {
		t.Fatal("policy failure reached destination")
	}
	repo.err = nil
	if ok, err := svc.Save(ctx, "t", configuration("kafka.property-report", false, "")); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if err := bus.Publish(ctx, model.TopicPropertyReport, "k", body("t")); err != nil || len(b.topics) != before {
		t.Fatal("disabled topic published", err)
	}
	if err := bus.Publish(ctx, model.TopicEventReport, "k", []byte(`{}`)); err == nil || len(b.topics) != before {
		t.Fatal("missing tenant published")
	}
	p.err = errors.New("broker rejected")
	if err := mqtt.Publish(ctx, "/iot/parsed/t/p/d/PROPERTY_REPORT", body("t"), 2, false); !errors.Is(err, p.err) || p.qos != 2 || p.retained {
		t.Fatal("MQTT error or delivery flags were lost", err)
	}
	b.err = errors.New("Kafka unavailable")
	if err := bus.Publish(ctx, model.TopicEventReport, "ordered-key", body("t")); !errors.Is(err, b.err) || b.key != "ordered-key" || string(b.payload) != string(body("t")) {
		t.Fatal("Kafka error, key or payload were lost", err)
	}
}
