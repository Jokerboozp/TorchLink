package kafkaadapter

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/protocol/createacls"
	"github.com/segmentio/kafka-go/protocol/createtopics"
)

// Opt in only with an explicitly disposable secured broker. This test creates
// accounts, ACLs and synthetic topics; it never changes broker security flags.
func TestConsumerAdminDisposableSecuredBroker(t *testing.T) {
	brokers := os.Getenv("IOT_TEST_SECURED_KAFKA_BROKERS")
	if brokers == "" {
		t.Skip("IOT_TEST_SECURED_KAFKA_BROKERS is not configured")
	}
	security := SecurityConfig{Username: os.Getenv("IOT_TEST_SECURED_KAFKA_USERNAME"), Password: os.Getenv("IOT_TEST_SECURED_KAFKA_PASSWORD"), Mechanism: os.Getenv("IOT_TEST_SECURED_KAFKA_MECHANISM")}
	admin, err := NewConsumerAdmin(strings.Split(brokers, ","), security, os.Getenv("IOT_TEST_SECURED_KAFKA_ADMIN_URL"), security.Username, security.Password)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	admin.SetConsumerBrokers(admin.brokers)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := admin.Ready(ctx); err != nil {
		t.Fatal("secured broker readiness", err)
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	id := hex.EncodeToString(random[:])
	username, password := "iot-topic-"+id, "consumer-private-"+id
	allowed, denied := "iot.external."+id+".allowed", "iot.external."+id+".other"
	// Even a misconfigured test address cannot make cleanup delete an
	// existing topic. Record only successful creates in this random namespace.
	owned := &consumerFixtureTopics{prefix: "iot.external." + id + ".", transport: admin.client.Transport}
	admin.client.Transport = owned
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		if err := admin.Revoke(cleanup, username); err != nil {
			t.Error("consumer cleanup", err)
		}
		if len(owned.created) == 0 {
			return
		}
		result, err := admin.client.DeleteTopics(cleanup, &kafka.DeleteTopicsRequest{Topics: owned.created})
		if err != nil {
			t.Error("synthetic topic cleanup", err)
			return
		}
		for _, err := range result.Errors {
			if err != nil && !errors.Is(err, kafka.UnknownTopicOrPartition) {
				t.Error("synthetic topic cleanup", err)
			}
		}
	})
	if err := admin.Provision(ctx, username, password, username, []string{allowed}); err != nil {
		t.Fatal("provision consumer", err)
	}
	if !slices.Contains(owned.created, allowed) {
		t.Fatal("authorized fixture was not newly created; existing topic will not receive test messages")
	}
	if err := admin.Provision(ctx, username, password, username, []string{allowed}); err != nil {
		t.Fatal("idempotent provision", err)
	}
	if err := admin.EnsureTopic(ctx, denied); err != nil {
		t.Fatal("ensure shared topic", err)
	}
	if !slices.Contains(owned.created, denied) {
		t.Fatal("write fixture was not newly created; existing topic will not receive test messages")
	}
	if err := admin.EnsureTopic(ctx, denied); err != nil {
		t.Fatal("idempotent shared topic creation", err)
	}
	if err := admin.CreateTopic(ctx, denied); !errors.Is(err, ErrTopicExists) {
		t.Fatal("new shared topic adopted existing broker history", err)
	}
	bus, err := NewWithSecurity(admin.brokers, security)
	if err != nil {
		t.Fatal(err)
	}
	defer bus.Close()
	bus.SetAutoCreateTopics(false)
	if err := bus.Health(ctx); err != nil {
		t.Fatal("authenticated bus health", err)
	}
	for _, topic := range []string{allowed, denied} {
		if err := bus.Publish(ctx, topic, "fixture", []byte("authorized-fixture")); err != nil {
			t.Fatal("authenticated bus publish", err)
		}
	}
	busCtx, stopBus := context.WithCancel(ctx)
	defer stopBus()
	received := make(chan struct{}, 1)
	if err := bus.Subscribe(busCtx, allowed, "auth-test-"+id, func(_ context.Context, body []byte) error {
		if string(body) != "authorized-fixture" {
			return errors.New("unexpected fixture")
		}
		select {
		case received <- struct{}{}:
		default:
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-received:
	case <-ctx.Done():
		t.Fatal("authenticated bus consumer timed out")
	}
	if _, err := bus.ConsumerLag(ctx); err != nil {
		t.Fatal("authenticated consumer lag", err)
	}
	if _, err := bus.GroupLag(ctx, "auth-test-"+id, allowed); err != nil {
		t.Fatal("authenticated group lag", err)
	}
	// Exercise the capacity adapter's authenticated connection without
	// creating, changing or deleting any internal business topic.
	capacityBackend := newCapacityQueueBackend(bus.brokers, bus.transport).(*kafkaCapacityQueueBackend)
	if _, err := capacityBackend.client.Metadata(ctx, &kafka.MetadataRequest{Topics: []string{allowed}}); err != nil {
		t.Fatal("authenticated capacity metadata", err)
	}
	stopBus()
	transport, err := NewTransport(SecurityConfig{Username: username, Password: password, Mechanism: "SCRAM-SHA-256"})
	if err != nil {
		t.Fatal(err)
	}
	defer transport.CloseIdleConnections()
	consumer := &kafka.Client{Addr: kafka.TCP(admin.brokers...), Transport: transport, Timeout: 5 * time.Second}
	fetch := func(topic string) (*kafka.FetchResponse, error) {
		return consumer.Fetch(ctx, &kafka.FetchRequest{Topic: topic, Partition: 0, Offset: 0, MinBytes: 1, MaxBytes: 1 << 20, MaxWait: time.Second})
	}
	result, err := fetch(allowed)
	if err != nil || result.Error != nil {
		t.Fatal("authorized fetch", err)
	}
	record, err := result.Records.ReadRecord()
	if err != nil {
		t.Fatal("authorized record", err)
	}
	value, err := io.ReadAll(record.Value)
	if err != nil || string(value) != "authorized-fixture" {
		t.Fatal("wrong authorized record")
	}
	if closer, ok := result.Records.(io.Closer); ok {
		_ = closer.Close()
	}
	result, err = fetch(denied)
	if err == nil && result != nil && result.Error == nil {
		t.Fatal("foreign topic fetch succeeded")
	}
	// kafka-go's transport hides unauthorized topics from its metadata map,
	// so Fetch may report ErrNoTopic. Check the broker's explicit response too.
	consumerDialer, err := NewDialer(SecurityConfig{Username: username, Password: password})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := consumerDialer.DialContext(ctx, "tcp", admin.brokers[0])
	if err != nil {
		t.Fatal(err)
	}
	_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
	_, err = connection.ReadPartitions(denied)
	_ = connection.Close()
	if !errors.Is(err, kafka.TopicAuthorizationFailed) {
		t.Fatalf("foreign topic metadata was not rejected: %v", err)
	}
	produced, err := consumer.Produce(ctx, &kafka.ProduceRequest{Topic: allowed, Partition: 0, RequiredAcks: kafka.RequireAll, Records: kafka.NewRecordReader(kafka.Record{Value: kafka.NewBytes([]byte("must-not-publish"))})})
	if !errors.Is(err, kafka.TopicAuthorizationFailed) && (produced == nil || !errors.Is(produced.Error, kafka.TopicAuthorizationFailed)) {
		t.Fatalf("consumer publication was not rejected: %v", err)
	}
	coordinator, err := consumer.FindCoordinator(ctx, &kafka.FindCoordinatorRequest{Key: "iot-platform-protected", KeyType: kafka.CoordinatorKeyTypeConsumer})
	if !errors.Is(err, kafka.GroupAuthorizationFailed) && (coordinator == nil || !errors.Is(coordinator.Error, kafka.GroupAuthorizationFailed)) {
		t.Fatalf("foreign consumer group was not rejected: %v", err)
	}
	coordinator, err = consumer.FindCoordinator(ctx, &kafka.FindCoordinatorRequest{Key: username, KeyType: kafka.CoordinatorKeyTypeConsumer})
	if err != nil || (coordinator.Error != nil && !errors.Is(coordinator.Error, kafka.GroupCoordinatorNotAvailable)) {
		t.Fatal("own consumer group denied", err)
	}
	// Independent read/write grants must not imply each other, including on
	// an already authenticated connection after a grant replacement.
	if err := admin.ProvisionGrants(ctx, username, password, username, []string{allowed}, []string{denied}); err != nil {
		t.Fatal("provision independent read/write grants", err)
	}
	// Previously denied topics can remain absent from kafka-go's metadata
	// cache. A newly issued account connects with a fresh client in production.
	grantedTransport, err := NewTransport(SecurityConfig{Username: username, Password: password, Mechanism: "SCRAM-SHA-256"})
	if err != nil {
		t.Fatal(err)
	}
	defer grantedTransport.CloseIdleConnections()
	consumer.Transport = grantedTransport
	produce := func(topic string) (*kafka.ProduceResponse, error) {
		return consumer.Produce(ctx, &kafka.ProduceRequest{Topic: topic, Partition: 0, RequiredAcks: kafka.RequireAll, Records: kafka.NewRecordReader(kafka.Record{Value: kafka.NewBytes([]byte("authorized-write"))})})
	}
	produced, err = produce(denied)
	if err != nil || produced == nil || produced.Error != nil {
		t.Fatal("authorized write failed", err)
	}
	result, err = fetch(denied)
	if !consumerAccessDenied(err) && (result == nil || !consumerAccessDenied(result.Error)) {
		t.Fatal("write grant also allowed reading", err)
	}
	produced, err = produce(allowed)
	if !consumerAccessDenied(err) && (produced == nil || !consumerAccessDenied(produced.Error)) {
		t.Fatal("read grant also allowed writing", err)
	}
	if err := admin.ProvisionGrants(ctx, username, password, "", nil, []string{denied}); err != nil {
		t.Fatal("provision publish-only client", err)
	}
	produced, err = produce(denied)
	if err != nil || produced == nil || produced.Error != nil {
		t.Fatal("publish-only client could not write", err)
	}
	coordinator, err = consumer.FindCoordinator(ctx, &kafka.FindCoordinatorRequest{Key: username, KeyType: kafka.CoordinatorKeyTypeConsumer})
	if !errors.Is(err, kafka.GroupAuthorizationFailed) && (coordinator == nil || !errors.Is(coordinator.Error, kafka.GroupAuthorizationFailed)) {
		t.Fatal("publish-only client retained consumer group access", err)
	}
	// Keep this transport alive across revocation: removing only the SCRAM
	// password must not be mistaken for revoking an authenticated connection.
	if err := admin.Revoke(ctx, username); err != nil {
		t.Fatal("revoke consumer", err)
	}
	result, err = fetch(allowed)
	if !consumerAccessDenied(err) && (result == nil || !consumerAccessDenied(result.Error)) {
		t.Fatalf("existing authenticated connection kept access after revoke: %v", err)
	}
	if err := admin.Revoke(ctx, username); err != nil {
		t.Fatal("idempotent revoke", err)
	}
	// A partial broker failure must clean up the SCRAM account and any grants.
	realTransport := admin.client.Transport
	admin.client.Transport = consumerRoundTripFunc(func(ctx context.Context, addr net.Addr, request kafka.Request) (kafka.Response, error) {
		if _, ok := request.(*createacls.Request); ok {
			return nil, errors.New("injected ACL outage")
		}
		return realTransport.RoundTrip(ctx, addr, request)
	})
	err = admin.ProvisionGrants(ctx, username, password, username, []string{allowed}, []string{denied})
	admin.client.Transport = realTransport
	if err == nil {
		t.Fatal("partial ACL failure reported success")
	}
	users := []string{}
	if _, err := admin.adminRequest(ctx, "GET", "/v1/security/users", nil, &users); err != nil {
		t.Fatal(err)
	}
	for _, user := range users {
		if user == username {
			t.Fatal("failed provision retained its SCRAM credential")
		}
	}
	if err := admin.verifyACLs(ctx, username, nil); err != nil {
		t.Fatal("failed provision retained ACLs", err)
	}
	t.Log("real secured Kafka: anonymous refused; independent exact read/write and publish-only grants enforced; foreign topic/operation/group denied; existing connection revoked; idempotent topic creation and failed-provision cleanup passed")
}

// This read-only probe covers distinct internal/public listeners; it does not
// create credentials, topics or ACLs and never changes broker configuration.
func TestConsumerAdminSecuredPublicListeners(t *testing.T) {
	public := os.Getenv("IOT_TEST_SECURED_KAFKA_PUBLIC_BROKERS")
	brokers := os.Getenv("IOT_TEST_SECURED_KAFKA_BROKERS")
	if public == "" || brokers == "" {
		t.Skip("dedicated secured internal and public Kafka listeners are not configured")
	}
	security := SecurityConfig{Username: os.Getenv("IOT_TEST_SECURED_KAFKA_USERNAME"), Password: os.Getenv("IOT_TEST_SECURED_KAFKA_PASSWORD"), Mechanism: os.Getenv("IOT_TEST_SECURED_KAFKA_MECHANISM")}
	admin, err := NewConsumerAdmin(strings.Split(brokers, ","), security, os.Getenv("IOT_TEST_SECURED_KAFKA_ADMIN_URL"), security.Username, security.Password)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	admin.SetConsumerBrokers(strings.Split(public, ","))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := admin.Ready(ctx); err != nil {
		t.Fatal("internal/public listener readiness", err)
	}
	t.Log("read-only secured Kafka: internal and public bootstrap/advertised listeners have the same cluster ID and reject anonymous requests")
}

type consumerFixtureTopics struct {
	prefix    string
	transport kafka.RoundTripper
	created   []string
}

func (f *consumerFixtureTopics) RoundTrip(ctx context.Context, addr net.Addr, request kafka.Request) (kafka.Response, error) {
	creation, creating := request.(*createtopics.Request)
	if creating {
		for _, topic := range creation.Topics {
			if !strings.HasPrefix(topic.Name, f.prefix) {
				return nil, errors.New("test attempted to create a topic outside its random namespace")
			}
		}
	}
	response, err := f.transport.RoundTrip(ctx, addr, request)
	if err == nil && creating && !creation.ValidateOnly {
		if result, ok := response.(*createtopics.Response); ok {
			for _, topic := range result.Topics {
				if topic.ErrorCode != 0 {
					continue
				}
				for _, requested := range creation.Topics {
					if topic.Name == requested.Name && !slices.Contains(f.created, topic.Name) {
						f.created = append(f.created, topic.Name)
					}
				}
			}
		}
	}
	return response, err
}

func TestConsumerFixtureCleanupOwnsOnlySuccessfulCreates(t *testing.T) {
	prefix := "iot.external.74657374."
	fixture := &consumerFixtureTopics{prefix: prefix, transport: consumerRoundTripFunc(func(context.Context, net.Addr, kafka.Request) (kafka.Response, error) {
		return &createtopics.Response{Topics: []createtopics.ResponseTopic{{Name: prefix + "new"}, {Name: prefix + "existing", ErrorCode: int16(kafka.TopicAlreadyExists)}, {Name: prefix + "failed", ErrorCode: int16(kafka.ClusterAuthorizationFailed)}, {Name: "iot.raw.message"}}}, nil
	})}
	request := &createtopics.Request{Topics: []createtopics.RequestTopic{{Name: prefix + "new"}, {Name: prefix + "existing"}, {Name: prefix + "failed"}}}
	if _, err := fixture.RoundTrip(context.Background(), kafka.TCP("unused"), request); err != nil {
		t.Fatal(err)
	}
	if len(fixture.created) != 1 || fixture.created[0] != prefix+"new" {
		t.Fatal("cleanup adopted an existing, failed or unrequested topic", fixture.created)
	}
	if _, err := fixture.RoundTrip(context.Background(), kafka.TCP("unused"), &createtopics.Request{Topics: []createtopics.RequestTopic{{Name: "iot.raw.message"}}}); err == nil {
		t.Fatal("fixture accepted internal topic creation")
	}
	fixture.created = nil
	fixture.transport = consumerRoundTripFunc(func(context.Context, net.Addr, kafka.Request) (kafka.Response, error) {
		return nil, errors.New("outcome unknown")
	})
	if _, err := fixture.RoundTrip(context.Background(), kafka.TCP("unused"), request); err == nil || len(fixture.created) != 0 {
		t.Fatal("unknown create outcome became deletion authority")
	}
}
