package kafkaadapter

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/protocol/createtopics"
	"github.com/segmentio/kafka-go/protocol/deleteacls"
	"github.com/segmentio/kafka-go/protocol/describeacls"
	"github.com/segmentio/kafka-go/protocol/metadata"
)

func TestConsumerAdminSecurityStateFailsClosed(t *testing.T) {
	for _, body := range []string{
		`{}`, `{ "enable_sasl":false, "admin_api_require_auth":true }`,
		`{ "enable_sasl":true, "kafka_enable_authorization":false, "admin_api_require_auth":true }`,
		`{ "enable_sasl":true, "admin_api_require_auth":false }`,
	} {
		t.Run(body, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
			defer srv.Close()
			admin, err := NewConsumerAdmin([]string{"unused:9092"}, SecurityConfig{Username: "service", Password: "test-secret"}, srv.URL, "admin", "admin-secret")
			if err != nil {
				t.Fatal(err)
			}
			defer admin.Close()
			admin.SetConsumerBrokers([]string{"unused:19092"})
			if err := admin.Ready(context.Background()); err == nil {
				t.Fatal("unsafe broker reported ready")
			}
		})
	}
	for _, body := range []string{
		`{"enable_sasl":true,"kafka_enable_authorization":null,"admin_api_require_auth":true}`,
		`{"enable_sasl":false,"kafka_enable_authorization":true,"admin_api_require_auth":true}`,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
		admin, err := NewConsumerAdmin([]string{"unused:9092"}, SecurityConfig{}, srv.URL, "admin", "secret")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := admin.securityState(context.Background()); err != nil {
			t.Fatal(err)
		}
		admin.Close()
		srv.Close()
	}
}

func TestConsumerAdminValidatesExternalClusterAndAdvertisedAddresses(t *testing.T) {
	for _, externalCluster := range []string{"cluster-one", "cluster-two", ""} {
		admin, err := NewConsumerAdmin([]string{"internal:9092"}, SecurityConfig{}, "http://unused", "admin", "secret")
		if err != nil {
			t.Fatal(err)
		}
		queried := []string{}
		admin.client.Transport = consumerRoundTripFunc(func(_ context.Context, addr net.Addr, request kafka.Request) (kafka.Response, error) {
			if _, ok := request.(*metadata.Request); !ok {
				t.Fatal("unexpected Kafka request")
			}
			queried = append(queried, addr.String())
			cluster, advertised, port := "cluster-one", "internal", int32(9092)
			if addr.String() != "internal:9092" {
				cluster, advertised, port = externalCluster, "advertised", 19092
			}
			return &metadata.Response{ClusterID: cluster, Brokers: []metadata.ResponseBroker{{Host: advertised, Port: port}}}, nil
		})
		addresses, err := admin.authorizationAddresses(context.Background(), []string{"public:19092"})
		if externalCluster == "cluster-one" {
			if err != nil || len(addresses) != 3 || !slices.Contains(queried, "public:19092") || !slices.Contains(queried, "advertised:19092") {
				t.Fatal("external or advertised listener was not checked", addresses, queried, err)
			}
		} else if err == nil {
			t.Fatal("different or unknown external cluster accepted")
		}
		admin.Close()
	}
	admin, err := NewConsumerAdmin([]string{"internal:9092"}, SecurityConfig{Username: "service", Password: "password"}, "http://unused", "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if err := admin.Ready(context.Background()); err == nil || !strings.Contains(err.Error(), "external consumer") {
		t.Fatal("missing external listener accepted", err)
	}
}

func TestConsumerAdminSupportsFullTopicGrantLimit(t *testing.T) {
	admin, err := NewConsumerAdmin([]string{"unused:9092"}, SecurityConfig{}, "http://unused", "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	topics := make([]string, 115)
	for i := range topics {
		topics[i] = fmt.Sprintf("iot.external.74657374.topic-%d", i)
	}
	err = admin.Provision(context.Background(), "iot-topic-test", strings.Repeat("x", 32), "iot-topic-test", topics)
	if err == nil || !strings.Contains(err.Error(), "service SASL credentials") {
		t.Fatal("full supported grant list did not reach readiness validation", err)
	}
	topics = append(topics, "iot.external.74657374.over-limit")
	if err := admin.Provision(context.Background(), "iot-topic-test", strings.Repeat("x", 32), "iot-topic-test", topics); err == nil || !strings.Contains(err.Error(), "credential or grants") {
		t.Fatal("excessive grant list accepted", err)
	}
}

func TestConsumerAdminDoesNotFollowRedirectsOrExposeBodies(t *testing.T) {
	forwarded := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded = true }))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "admin" || password != "admin-secret" {
			t.Error("missing dedicated admin identity")
		}
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
		_, _ = w.Write([]byte("admin-secret private-broker-configuration"))
	}))
	defer srv.Close()
	admin, err := NewConsumerAdmin([]string{"unused:9092"}, SecurityConfig{}, srv.URL, "admin", "admin-secret")
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	_, err = admin.adminRequest(context.Background(), http.MethodGet, "/v1/cluster_config", nil, nil)
	if err == nil || forwarded || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "configuration") {
		t.Fatal("unsafe admin error/redirect handling")
	}
}

type consumerRoundTripFunc func(context.Context, net.Addr, kafka.Request) (kafka.Response, error)

func (f consumerRoundTripFunc) RoundTrip(ctx context.Context, addr net.Addr, request kafka.Request) (kafka.Response, error) {
	return f(ctx, addr, request)
}

func TestConsumerRevocationRequiresACLRemovalBeforeDeletingCredential(t *testing.T) {
	for _, fail := range []bool{true, false} {
		calls := []string{}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls = append(calls, "credential")
			if r.Method != http.MethodDelete || r.URL.Path != "/v1/security/users/iot-topic-test" {
				t.Errorf("unexpected admin route: %s %s", r.Method, r.URL.Path)
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		admin, err := NewConsumerAdmin([]string{"unused:9092"}, SecurityConfig{}, srv.URL, "admin", "secret")
		if err != nil {
			t.Fatal(err)
		}
		admin.client.Transport = consumerRoundTripFunc(func(_ context.Context, _ net.Addr, request kafka.Request) (kafka.Response, error) {
			calls = append(calls, "acl")
			deletion := request.(*deleteacls.Request)
			if len(deletion.Filters) != 1 || deletion.Filters[0].PrincipalFilter != "User:iot-topic-test" {
				t.Fatal("revocation was not restricted to exact principal")
			}
			if fail {
				return nil, errors.New("broker unavailable")
			}
			return &deleteacls.Response{FilterResults: []deleteacls.FilterResult{{}}}, nil
		})
		err = admin.Revoke(context.Background(), "iot-topic-test")
		if fail {
			if err == nil || len(calls) != 1 {
				t.Fatal("failed ACL revocation deleted credential or claimed success")
			}
		} else if err != nil || strings.Join(calls, ",") != "acl,credential" {
			t.Fatal("revocation order or idempotence failed", err, calls)
		}
		admin.Close()
		srv.Close()
	}
}

func TestConsumerAdminRejectsForeignPrincipalsAndSharedTopics(t *testing.T) {
	admin, err := NewConsumerAdmin([]string{"unused:9092"}, SecurityConfig{}, "http://unused", "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	for _, name := range []string{"", "admin", "iot-platform", "iot-topic-*", "iot-topic-test/other"} {
		if err := admin.Revoke(context.Background(), name); err == nil {
			t.Fatal("accepted unmanaged identity")
		}
	}
	for _, topic := range []string{"iot.alarm.raised", "iot.raw.message", "iot.external.other-tenant.events", "iot.external.74.*", "iot.external.74."} {
		if err := admin.Provision(context.Background(), "iot-topic-test", strings.Repeat("x", 32), "iot-topic-test", []string{topic}); err == nil || !strings.Contains(err.Error(), "tenant-specific") {
			t.Fatal("accepted a shared or invalid topic", topic, err)
		}
	}
}

func TestConsumerGrantACLsSeparateReadWriteAndGroup(t *testing.T) {
	const read, write, both = "iot.external.74.read", "iot.external.74.write", "iot.external.74.both"
	for _, tc := range []struct {
		name        string
		read, write []string
		count       int
	}{
		{"independent", []string{read, both}, []string{write, both}, 8},
		{"publish only", nil, []string{write, write}, 2},
		{"subscribe only", []string{read}, nil, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			acls := consumerGrantACLs("iot-topic-test", "iot-topic-test", tc.read, tc.write)
			if len(acls) != tc.count {
				t.Fatal("unexpected or duplicate grants", acls)
			}
			for _, acl := range acls {
				if acl.Principal != "User:iot-topic-test" || acl.ResourcePatternType != kafka.PatternTypeLiteral || acl.PermissionType != kafka.ACLPermissionTypeAllow {
					t.Fatal("non-exact grant", acl)
				}
				switch acl.ResourceType {
				case kafka.ResourceTypeGroup:
					if len(tc.read) == 0 || acl.ResourceName != "iot-topic-test" || acl.Operation != kafka.ACLOperationTypeRead {
						t.Fatal("unexpected group grant", acl)
					}
				case kafka.ResourceTypeTopic:
					switch acl.Operation {
					case kafka.ACLOperationTypeRead:
						if !slices.Contains(tc.read, acl.ResourceName) {
							t.Fatal("write grant implied read", acl)
						}
					case kafka.ACLOperationTypeWrite:
						if !slices.Contains(tc.write, acl.ResourceName) {
							t.Fatal("read grant implied write", acl)
						}
					case kafka.ACLOperationTypeDescribe:
					default:
						t.Fatal("unexpected operation", acl)
					}
				default:
					t.Fatal("unexpected resource", acl)
				}
			}
		})
	}
}

func TestConsumerPublishOnlyGrantsAndTopicCreationValidateBeforeIO(t *testing.T) {
	a, err := NewConsumerAdmin([]string{"unused:9092"}, SecurityConfig{}, "http://unused", "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	ctx := context.Background()
	if err := a.ProvisionGrants(ctx, "iot-topic-test", strings.Repeat("x", 32), "", nil, []string{"iot.external.74.write"}); err == nil || !strings.Contains(err.Error(), "service SASL credentials") {
		t.Fatal("publish-only grant rejected before readiness", err)
	}
	for _, topic := range []string{"iot.raw.message", "iot.external.74.*", ""} {
		if err := a.ProvisionGrants(ctx, "iot-topic-test", strings.Repeat("x", 32), "", nil, []string{topic}); err == nil || !strings.Contains(err.Error(), "tenant-specific") {
			t.Fatal("invalid write topic accepted", err)
		}
		if err := a.EnsureTopic(ctx, topic); err == nil || !strings.Contains(err.Error(), "tenant-specific") {
			t.Fatal("invalid topic creation accepted", err)
		}
		if err := a.CreateTopic(ctx, topic); err == nil || !strings.Contains(err.Error(), "tenant-specific") {
			t.Fatal("invalid strict topic creation accepted", err)
		}
	}
}

func TestConsumerCreateTopicRejectsExistingHistoryWithoutDeletion(t *testing.T) {
	// The authenticated Kafka transport is simulated; the separate anonymous
	// listener really refuses every connection, retaining the Ready precondition.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	host, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/cluster_config" {
			t.Errorf("unexpected broker mutation: %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"enable_sasl":true,"kafka_enable_authorization":null,"admin_api_require_auth":true}`))
	}))
	defer srv.Close()
	const topic = "iot.external.74.shared"
	for _, tc := range []struct {
		name         string
		code         kafka.Error
		transportErr bool
		missing      bool
	}{
		{name: "new topic"},
		{name: "existing history", code: kafka.TopicAlreadyExists},
		{name: "authorization failure", code: kafka.TopicAuthorizationFailed},
		{name: "unknown network outcome", transportErr: true},
		{name: "missing broker result", missing: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, err := NewConsumerAdmin([]string{listener.Addr().String()}, SecurityConfig{Username: "service", Password: "secret"}, srv.URL, "admin", "secret")
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			a.SetConsumerBrokers([]string{listener.Addr().String()})
			creates := 0
			a.client.Transport = consumerRoundTripFunc(func(_ context.Context, _ net.Addr, request kafka.Request) (kafka.Response, error) {
				switch req := request.(type) {
				case *metadata.Request:
					return &metadata.Response{ClusterID: "cluster", Brokers: []metadata.ResponseBroker{{Host: host, Port: int32(port)}}}, nil
				case *describeacls.Request:
					return &describeacls.Response{}, nil
				case *createtopics.Request:
					creates++
					if len(req.Topics) != 1 || req.Topics[0].Name != topic || req.Topics[0].NumPartitions != -1 || req.Topics[0].ReplicationFactor != -1 {
						t.Fatal("create changed target or cluster defaults")
					}
					if tc.transportErr {
						return nil, errors.New("connection lost after write")
					}
					if tc.missing {
						return &createtopics.Response{}, nil
					}
					return &createtopics.Response{Topics: []createtopics.ResponseTopic{{Name: topic, ErrorCode: int16(tc.code)}}}, nil
				default:
					t.Errorf("unexpected request, including deletion: %T", request)
					return nil, errors.New("unexpected mutation")
				}
			})
			err = a.CreateTopic(context.Background(), topic)
			if creates != 1 {
				t.Fatal("strict creation was not attempted once", creates, err)
			}
			if tc.name == "new topic" {
				if err != nil {
					t.Fatal(err)
				}
			} else if tc.code == kafka.TopicAlreadyExists {
				if !errors.Is(err, ErrTopicExists) {
					t.Fatal("existing history was adopted", err)
				}
			} else if err == nil || errors.Is(err, ErrTopicExists) {
				t.Fatal("unknown or failed creation was misreported", err)
			}
		})
	}
}
