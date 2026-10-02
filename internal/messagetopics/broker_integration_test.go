package messagetopics_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/segmentio/kafka-go"

	kafkaadapter "iot-platform/internal/adapters/kafka"
	"iot-platform/internal/adapters/memory"
	mqttadapter "iot-platform/internal/adapters/mqtt"
	"iot-platform/internal/auth"
	"iot-platform/internal/messagetopics"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// These tests use only explicitly supplied existing brokers. MQTT messages are
// never retained, Kafka topics are created/deleted by exact random name, and
// neither test creates accounts, changes ACLs or starts infrastructure.
func TestMessageTopicsExistingMQTTBroker(t *testing.T) {
	broker := os.Getenv("IOT_TEST_MESSAGE_TOPICS_MQTT_BROKER")
	if broker == "" {
		t.Skip("IOT_TEST_MESSAGE_TOPICS_MQTT_BROKER is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	id := brokerTestID(t)
	tenant := "topic-test-" + id
	topic := messagetopics.MQTTPrefix(tenant) + "parsed/device-one"
	username, password := os.Getenv("IOT_TEST_MESSAGE_TOPICS_MQTT_USERNAME"), os.Getenv("IOT_TEST_MESSAGE_TOPICS_MQTT_PASSWORD")
	if password == "" {
		secret := os.Getenv("IOT_TEST_MESSAGE_TOPICS_JWT_SECRET")
		if secret == "" {
			t.Fatal("configure dedicated MQTT credentials or the existing broker JWT signing secret")
		}
		username = "topic-test-" + id
		var err error
		password, err = auth.New(secret).IssueWithACL(username, "system", "service", nil, []auth.ACLRule{{Permission: "allow", Action: "all", Topic: topic}}, 2*time.Minute)
		if err != nil {
			t.Fatal("could not issue the temporary exact-topic MQTT credential")
		}
	}
	credentials := func() (string, string) { return username, password }
	messages := make(chan []byte, 8)
	subscriber := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(broker).
		SetClientID("topic-sub-" + id).SetCredentialsProvider(credentials).
		SetCleanSession(true).SetAutoReconnect(false).SetConnectRetry(false).
		SetConnectTimeout(5 * time.Second))
	t.Cleanup(func() { subscriber.Disconnect(250) })
	if token := subscriber.Connect(); !token.WaitTimeout(10*time.Second) || token.Error() != nil {
		t.Fatal("temporary MQTT subscriber could not connect")
	}
	if token := subscriber.Subscribe(topic, 1, func(_ mqtt.Client, message mqtt.Message) {
		select {
		case messages <- bytes.Clone(message.Payload()):
		default:
		}
	}); !token.WaitTimeout(5*time.Second) || token.Error() != nil {
		t.Fatal("temporary MQTT subscriber could not subscribe to its exact topic")
	}
	publisher, err := mqttadapter.NewWithCredentials(broker, "topic-pub-"+id, credentials)
	if err != nil {
		t.Fatal("temporary MQTT publisher could not connect")
	}
	t.Cleanup(func() { _ = publisher.Close() })
	guard := &exactTopicRealtime{RealtimePublisher: publisher, topic: topic}
	service := messagetopics.New(memory.NewRepository())
	routed := service.WrapRealtime(guard)
	saveBrokerRoute(t, ctx, service, tenant, "mqtt.parsed", messagetopics.MQTTPrefix(tenant)+"parsed/{deviceId}", true)
	source := "/iot/parsed/" + tenant + "/product-one/device-one/PROPERTY_REPORT"
	first := brokerTestPayload(t, tenant, "enabled")
	if err := routed.Publish(ctx, source, first, 1, false); err != nil {
		t.Fatal(err)
	}
	receive := func(want []byte) {
		t.Helper()
		select {
		case got := <-messages:
			if !bytes.Equal(got, want) {
				t.Fatalf("unexpected MQTT payload: %s", got)
			}
		case <-ctx.Done():
			t.Fatal("custom MQTT topic did not receive the routed publication")
		}
	}
	receive(first)
	saveBrokerRoute(t, ctx, service, tenant, "mqtt.parsed", topic, false)
	if err := routed.Publish(ctx, source, brokerTestPayload(t, tenant, "disabled"), 1, false); err != nil {
		t.Fatal(err)
	}
	if guard.calls.Load() != 1 {
		t.Fatal("disabled MQTT route reached the real publisher")
	}
	// A second successful delivery proves the connection stayed usable while
	// disabled. QoS 1 publications to this one topic preserve their ordering.
	saveBrokerRoute(t, ctx, service, tenant, "mqtt.parsed", topic, true)
	last := brokerTestPayload(t, tenant, "enabled-again")
	if err := routed.Publish(ctx, source, last, 1, false); err != nil {
		t.Fatal(err)
	}
	receive(last)
	if guard.calls.Load() != 2 {
		t.Fatal("unexpected number of real MQTT publications")
	}
}

func TestMessageTopicsExistingKafkaBroker(t *testing.T) {
	configured := os.Getenv("IOT_TEST_MESSAGE_TOPICS_KAFKA_BROKERS")
	if configured == "" {
		t.Skip("IOT_TEST_MESSAGE_TOPICS_KAFKA_BROKERS is not configured")
	}
	brokers := strings.Split(configured, ",")
	for i := range brokers {
		brokers[i] = strings.TrimSpace(brokers[i])
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	tenant := "topic-test-" + brokerTestID(t)
	topic := messagetopics.KafkaPrefix(tenant) + "property"
	connection, err := kafka.DialContext(ctx, "tcp", brokers[0])
	if err != nil {
		t.Fatal("could not connect to the existing Kafka broker")
	}
	_ = connection.SetDeadline(time.Now().Add(10 * time.Second))
	controller, err := connection.Controller()
	_ = connection.Close()
	if err != nil {
		t.Fatal("could not locate the Kafka controller")
	}
	transport := &kafka.Transport{ClientID: "message-topics-integration", DialTimeout: 5 * time.Second}
	t.Cleanup(transport.CloseIdleConnections)
	admin := &kafka.Client{Addr: kafka.TCP(net.JoinHostPort(controller.Host, strconv.Itoa(controller.Port))), Timeout: 10 * time.Second, Transport: transport}
	owned := true
	t.Cleanup(func() {
		if !owned {
			return
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		response, err := admin.DeleteTopics(cleanup, &kafka.DeleteTopicsRequest{Topics: []string{topic}})
		if err != nil {
			t.Errorf("could not delete temporary Kafka topic %s: %v", topic, err)
			return
		}
		if err := response.Errors[topic]; err != nil && !errors.Is(err, kafka.UnknownTopicOrPartition) {
			t.Errorf("could not delete temporary Kafka topic %s: %v", topic, err)
		}
	})
	created, err := admin.CreateTopics(ctx, &kafka.CreateTopicsRequest{Topics: []kafka.TopicConfig{{Topic: topic, NumPartitions: 1, ReplicationFactor: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := created.Errors[topic]; err != nil {
		owned = !errors.Is(err, kafka.TopicAlreadyExists)
		t.Fatalf("could not create the isolated Kafka topic: %v", err)
	}
	bus := kafkaadapter.New(brokers)
	bus.SetAutoCreateTopics(false)
	t.Cleanup(func() { _ = bus.Close() })
	service := messagetopics.New(memory.NewRepository())
	routed := service.WrapBus(&exactTopicBus{EventBus: bus, topic: topic})
	saveBrokerRoute(t, ctx, service, tenant, "kafka.property-report", topic, true)
	first := brokerTestPayload(t, tenant, "enabled")
	if err := routed.Publish(ctx, model.TopicPropertyReport, "device-one", first); err != nil {
		t.Fatal(err)
	}
	leader, err := kafka.DialLeader(ctx, "tcp", brokers[0], topic, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = leader.Close() })
	_ = leader.SetDeadline(time.Now().Add(15 * time.Second))
	saveBrokerRoute(t, ctx, service, tenant, "kafka.property-report", topic, false)
	if err := routed.Publish(ctx, model.TopicPropertyReport, "device-one", brokerTestPayload(t, tenant, "disabled")); err != nil {
		t.Fatal(err)
	}
	if offset, err := leader.ReadLastOffset(); err != nil || offset != 1 {
		t.Fatalf("disabled route changed the Kafka log: offset=%d error=%v", offset, err)
	}
	saveBrokerRoute(t, ctx, service, tenant, "kafka.property-report", topic, true)
	last := brokerTestPayload(t, tenant, "enabled-again")
	if err := routed.Publish(ctx, model.TopicPropertyReport, "device-one", last); err != nil {
		t.Fatal(err)
	}
	reader := kafka.NewReader(kafka.ReaderConfig{Brokers: brokers, Topic: topic, Partition: 0, MinBytes: 1, MaxBytes: 1 << 20, MaxWait: time.Second})
	t.Cleanup(func() { _ = reader.Close() })
	for i, want := range [][]byte{first, last} {
		message, err := reader.ReadMessage(ctx)
		if err != nil || message.Offset != int64(i) || message.Topic != topic || string(message.Key) != "device-one" || !bytes.Equal(message.Value, want) {
			t.Fatalf("unexpected custom Kafka message at offset %d: %+v, error=%v", i, message, err)
		}
	}
	if offset, err := leader.ReadLastOffset(); err != nil || offset != 2 {
		t.Fatalf("unexpected Kafka log size: offset=%d error=%v", offset, err)
	}
}

func brokerTestID(t *testing.T) string {
	t.Helper()
	var id [12]byte
	if _, err := rand.Read(id[:]); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(id[:])
}

func brokerTestPayload(t *testing.T, tenant, marker string) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]string{"tenantId": tenant, "productId": "product-one", "deviceId": "device-one", "messageType": "PROPERTY_REPORT", "marker": marker})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func saveBrokerRoute(t *testing.T, ctx context.Context, service *messagetopics.Service, tenant, id, topic string, enabled bool) {
	t.Helper()
	config, err := service.Load(ctx, tenant)
	if err != nil {
		t.Fatal(err)
	}
	config.Overrides[id] = model.MessageTopicOverride{Enabled: enabled, Topic: topic}
	if ok, err := service.Save(ctx, tenant, config); err != nil || !ok {
		t.Fatalf("could not update route: accepted=%t, error=%v", ok, err)
	}
}

// Guard the integration test itself: a routing regression must fail before it
// can publish a test event into one of the broker's existing shared topics.
type exactTopicBus struct {
	ports.EventBus
	topic string
}

func (b *exactTopicBus) Publish(ctx context.Context, topic, key string, payload []byte) error {
	if topic != b.topic {
		return fmt.Errorf("refusing test publication outside the isolated Kafka topic: %s", topic)
	}
	return b.EventBus.Publish(ctx, topic, key, payload)
}

type exactTopicRealtime struct {
	ports.RealtimePublisher
	topic string
	calls atomic.Int32
}

func (p *exactTopicRealtime) Publish(ctx context.Context, topic string, payload []byte, qos byte, retained bool) error {
	if topic != p.topic || retained {
		return errors.New("refusing retained or non-isolated MQTT test publication")
	}
	p.calls.Add(1)
	return p.RealtimePublisher.Publish(ctx, topic, payload, qos, retained)
}
