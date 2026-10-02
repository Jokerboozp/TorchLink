package kafkaadapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/segmentio/kafka-go"
)

var (
	managedConsumerName  = regexp.MustCompile(`^iot-topic-[A-Za-z0-9][A-Za-z0-9_-]{0,117}$`)
	managedConsumerTopic = regexp.MustCompile(`^iot\.external\.[0-9a-f]+\.[A-Za-z0-9_-][A-Za-z0-9._-]*$`)
)

const maxConsumerTopics = 115

// ConsumerAdmin owns only iot-topic-* principals and tenant-specific external
// topics. It never enables broker authentication or changes existing log data.
type ConsumerAdmin struct {
	brokers                            []string
	consumerMu                         sync.RWMutex
	consumerBrokers                    []string
	security                           SecurityConfig
	adminURL, adminUser, adminPassword string
	client                             *kafka.Client
	transport                          *kafka.Transport
	httpClient                         *http.Client
}

// Admin credentials authenticate both the Redpanda Admin API and Kafka ACL
// management; the platform's ordinary SASL identity can remain a service user.
func NewConsumerAdmin(brokers []string, security SecurityConfig, adminURL, adminUser, adminPassword string) (*ConsumerAdmin, error) {
	u, err := url.Parse(adminURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("Kafka administration URL must be an HTTP(S) root URL")
	}
	if len(brokers) == 0 || adminUser == "" || adminPassword == "" {
		return nil, errors.New("Kafka brokers and administration credentials are required")
	}
	if _, _, err := security.settings(); err != nil {
		return nil, err
	}
	adminSecurity := security
	adminSecurity.Username, adminSecurity.Password = adminUser, adminPassword
	transport, err := NewTransport(adminSecurity)
	if err != nil {
		return nil, err
	}
	httpTransport := http.DefaultTransport.(*http.Transport).Clone()
	if u.Scheme == "https" {
		httpSecurity := security
		httpSecurity.TLS = true
		httpTransport.TLSClientConfig, _, err = httpSecurity.settings()
		if err != nil {
			return nil, err
		}
	}
	return &ConsumerAdmin{
		brokers: append([]string(nil), brokers...), security: security,
		adminURL: strings.TrimRight(adminURL, "/"), adminUser: adminUser, adminPassword: adminPassword,
		transport: transport, client: &kafka.Client{Addr: kafka.TCP(brokers...), Timeout: 10 * time.Second, Transport: transport},
		httpClient: &http.Client{Transport: httpTransport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}, nil
}

func (a *ConsumerAdmin) Close() {
	a.transport.CloseIdleConnections()
	a.httpClient.CloseIdleConnections()
}

// SetConsumerBrokers declares the addresses returned in external connection
// instructions. Ready checks them and their advertised listeners against the
// same cluster used for ACL management before any credential can be issued.
func (a *ConsumerAdmin) SetConsumerBrokers(brokers []string) {
	a.consumerMu.Lock()
	a.consumerBrokers = append([]string(nil), brokers...)
	a.consumerMu.Unlock()
}

// adminRequest discards error bodies, which may contain credentials or broker
// configuration. Only the requested fields of successful responses are decoded.
func (a *ConsumerAdmin) adminRequest(ctx context.Context, method, path string, body, out any) (int, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return 0, errors.New("invalid Kafka administration request")
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.adminURL+path, reader)
	if err != nil {
		return 0, errors.New("invalid Kafka administration request")
	}
	req.SetBasicAuth(a.adminUser, a.adminPassword)
	req.Header.Set("Content-Type", "application/json")
	response, err := a.httpClient.Do(req)
	if err != nil {
		return 0, errors.New("Kafka administration request failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return response.StatusCode, fmt.Errorf("Kafka administration HTTP %d", response.StatusCode)
	}
	if out != nil {
		if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(out); err != nil {
			return response.StatusCode, errors.New("invalid Kafka administration response")
		}
	}
	return response.StatusCode, nil
}

type consumerSecurityState struct {
	EnableSASL          *bool    `json:"enable_sasl"`
	Authorization       *bool    `json:"kafka_enable_authorization"`
	AdminAuthentication bool     `json:"admin_api_require_auth"`
	Superusers          []string `json:"superusers"`
}

func (a *ConsumerAdmin) securityState(ctx context.Context) (consumerSecurityState, error) {
	var state consumerSecurityState
	_, err := a.adminRequest(ctx, http.MethodGet, "/v1/cluster_config?include_defaults=true", nil, &state)
	if err != nil {
		return state, err
	}
	authorized := state.EnableSASL != nil && *state.EnableSASL
	if state.Authorization != nil {
		authorized = *state.Authorization
	}
	if !authorized || !state.AdminAuthentication {
		return state, errors.New("Kafka consumer authorization requires broker ACL enforcement and authenticated Redpanda administration")
	}
	return state, nil
}

// Ready verifies the running broker, not an environment flag. A successful
// authenticated request followed by an anonymous rejection is required for
// each advertised address as well as every configured bootstrap address.
func (a *ConsumerAdmin) Ready(ctx context.Context) error {
	if a.security.Username == "" || a.security.Password == "" {
		return errors.New("Kafka service SASL credentials are not configured")
	}
	a.consumerMu.RLock()
	consumerBrokers := append([]string(nil), a.consumerBrokers...)
	a.consumerMu.RUnlock()
	if len(consumerBrokers) == 0 {
		return errors.New("Kafka external consumer broker addresses are not configured")
	}
	if _, err := a.securityState(ctx); err != nil {
		return err
	}
	const probeTopic = "iot.external.authentication-probe"
	addresses, err := a.authorizationAddresses(ctx, consumerBrokers)
	if err != nil {
		return err
	}
	for _, address := range addresses {
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := a.requireAnonymousRejection(probeCtx, address, probeTopic)
		cancel()
		if err != nil {
			return err
		}
	}
	// Wildcard users can bypass an otherwise exact grant. Do not claim that
	// account ACLs isolate consumers when the cluster grants access to everyone.
	response, err := a.client.DescribeACLs(ctx, &kafka.DescribeACLsRequest{Filter: kafka.ACLFilter{ResourceTypeFilter: kafka.ResourceTypeAny, ResourcePatternTypeFilter: kafka.PatternTypeAny, PrincipalFilter: "User:*", Operation: kafka.ACLOperationTypeAny, PermissionType: kafka.ACLPermissionTypeAllow}})
	if err != nil || response == nil || response.Error != nil {
		return errors.New("could not verify Kafka consumer ACL isolation")
	}
	if len(response.Resources) != 0 {
		return errors.New("Kafka has wildcard user grants; exact consumer authorization is unavailable")
	}
	return nil
}

func (a *ConsumerAdmin) authorizationAddresses(ctx context.Context, consumerBrokers []string) ([]string, error) {
	const probeTopic = "iot.external.authentication-probe"
	addresses := append([]string(nil), a.brokers...)
	for _, address := range consumerBrokers {
		if !slices.Contains(addresses, address) {
			addresses = append(addresses, address)
		}
	}
	clusterID := ""
	for i := 0; i < len(addresses); i++ {
		if len(addresses) > 64 {
			return nil, errors.New("Kafka authorization probe returned too many broker addresses")
		}
		address := addresses[i]
		host, port, err := net.SplitHostPort(address)
		portNumber, portErr := strconv.Atoi(port)
		if err != nil || portErr != nil || host == "" || strings.ContainsAny(host, "/@ \t\r\n") || portNumber < 1 || portNumber > 65535 {
			return nil, errors.New("Kafka broker address must use host:port")
		}
		metadata, err := a.client.Metadata(ctx, &kafka.MetadataRequest{Addr: kafka.TCP(address), Topics: []string{probeTopic}})
		if err != nil || metadata == nil {
			return nil, errors.New("authenticated Kafka broker metadata is unavailable")
		}
		if metadata.ClusterID == "" || len(metadata.Brokers) == 0 {
			return nil, errors.New("Kafka cluster identity is unavailable")
		}
		if clusterID == "" {
			clusterID = metadata.ClusterID
		} else if clusterID != metadata.ClusterID {
			return nil, errors.New("Kafka external broker address belongs to a different cluster")
		}
		for _, broker := range metadata.Brokers {
			next := net.JoinHostPort(broker.Host, fmt.Sprint(broker.Port))
			if !slices.Contains(addresses, next) {
				addresses = append(addresses, next)
			}
		}
	}
	return addresses, nil
}

func (a *ConsumerAdmin) requireAnonymousRejection(ctx context.Context, address, topic string) error {
	// First prove this exact address is reachable with authentication. A
	// timeout or unavailable broker must never count as an anonymous rejection.
	verified, err := a.client.Metadata(ctx, &kafka.MetadataRequest{Addr: kafka.TCP(address), Topics: []string{topic}})
	if err != nil || verified == nil {
		return errors.New("Kafka authentication probe could not reach a broker")
	}
	security := a.security
	security.Username, security.Password = "", ""
	dialer, err := NewDialer(security)
	if err != nil {
		return err
	}
	connection, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return errors.New("Kafka anonymous authentication probe could not connect")
	}
	defer connection.Close()
	deadline := time.Now().Add(4 * time.Second)
	if parent, ok := ctx.Deadline(); ok && parent.Before(deadline) {
		deadline = parent
	}
	_ = connection.SetDeadline(deadline)
	_, err = connection.Brokers()
	if ctx.Err() != nil {
		return errors.New("Kafka anonymous authorization probe timed out")
	}
	// Redpanda closes a non-SASL connection that sends Metadata before a
	// handshake. Other configurations may return a Kafka authorization error.
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, syscall.ECONNRESET) || consumerAccessDenied(err) {
		return nil
	}
	if err != nil {
		return errors.New("Kafka anonymous authorization rejection could not be verified")
	}
	return errors.New("Kafka accepts anonymous requests; consumer credentials cannot be issued")
}

func consumerAccessDenied(err error) bool {
	return errors.Is(err, kafka.TopicAuthorizationFailed) || errors.Is(err, kafka.ClusterAuthorizationFailed) || errors.Is(err, kafka.SASLAuthenticationFailed) || errors.Is(err, kafka.IllegalSASLState)
}

func consumerACLFilter(username string) kafka.DeleteACLsFilter {
	return kafka.DeleteACLsFilter{ResourceTypeFilter: kafka.ResourceTypeAny, ResourcePatternTypeFilter: kafka.PatternTypeAny, PrincipalFilter: "User:" + username, Operation: kafka.ACLOperationTypeAny, PermissionType: kafka.ACLPermissionTypeAny}
}

func (a *ConsumerAdmin) removeACLs(ctx context.Context, username string) error {
	response, err := a.client.DeleteACLs(ctx, &kafka.DeleteACLsRequest{Filters: []kafka.DeleteACLsFilter{consumerACLFilter(username)}})
	if err != nil || response == nil || len(response.Results) != 1 {
		return errors.New("could not revoke Kafka consumer ACLs")
	}
	for _, result := range response.Results {
		if result.Error != nil {
			return errors.New("could not revoke Kafka consumer ACLs")
		}
		for _, acl := range result.MatchingACLs {
			if acl.Error != nil {
				return errors.New("could not revoke Kafka consumer ACLs")
			}
		}
	}
	return nil
}

// Revoke removes authorization before credentials so already authenticated
// connections lose access too. No topic, message or offset is deleted.
func (a *ConsumerAdmin) Revoke(ctx context.Context, username string) error {
	if !managedConsumerName.MatchString(username) || username == a.adminUser || username == a.security.Username {
		return errors.New("invalid managed Kafka consumer username")
	}
	if err := a.removeACLs(ctx, username); err != nil {
		return err
	}
	code, err := a.adminRequest(ctx, http.MethodDelete, "/v1/security/users/"+url.PathEscape(username), nil, nil)
	if err != nil && code != http.StatusNotFound {
		return err
	}
	return nil
}

// Provision is idempotent for an account generation. Callers must persist a
// new random username for rotations. All changed account grants are replaced;
// failures revoke them and the credential, including after request cancellation.
func (a *ConsumerAdmin) Provision(ctx context.Context, username, password, group string, topics []string) (result error) {
	if !managedConsumerName.MatchString(username) || username == a.adminUser || username == a.security.Username || !managedConsumerName.MatchString(group) || len(password) < 24 || len(password) > 256 || len(topics) == 0 || len(topics) > maxConsumerTopics {
		return errors.New("invalid managed Kafka consumer credential or grants")
	}
	unique := make([]string, 0, len(topics))
	for _, topic := range topics {
		if len(topic) > 249 || !managedConsumerTopic.MatchString(topic) {
			return errors.New("Kafka consumers can only access tenant-specific external topics")
		}
		if !slices.Contains(unique, topic) {
			unique = append(unique, topic)
		}
	}
	if err := a.Ready(ctx); err != nil {
		return err
	}
	state, err := a.securityState(ctx)
	if err != nil {
		return err
	}
	if slices.Contains(state.Superusers, username) {
		return errors.New("a Kafka superuser cannot be a managed consumer")
	}
	defer func() {
		if result != nil {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
			defer cancel()
			if err := a.Revoke(cleanup, username); err != nil {
				result = errors.Join(result, errors.New("Kafka consumer cleanup is incomplete; revocation must be retried"))
			}
		}
	}()
	if err := a.removeACLs(ctx, username); err != nil {
		return err
	}
	// Existing topics retain their partition, replication and retention
	// settings. New topics use the cluster defaults rather than forcing RF=1.
	configs := make([]kafka.TopicConfig, 0, len(unique))
	for _, topic := range unique {
		configs = append(configs, kafka.TopicConfig{Topic: topic, NumPartitions: -1, ReplicationFactor: -1})
	}
	created, err := a.client.CreateTopics(ctx, &kafka.CreateTopicsRequest{Topics: configs})
	if err != nil || created == nil {
		return errors.New("could not prepare Kafka consumer topics")
	}
	for _, topic := range unique {
		cause, ok := created.Errors[topic]
		if !ok || (cause != nil && !errors.Is(cause, kafka.TopicAlreadyExists)) {
			return errors.New("could not prepare Kafka consumer topics")
		}
	}
	// Consumer connection instructions use this fixed mechanism regardless
	// of the mechanism used by the platform/admin service identities.
	const mechanism = "SCRAM-SHA-256"
	body := map[string]string{"username": username, "password": password, "algorithm": mechanism}
	code, err := a.adminRequest(ctx, http.MethodPost, "/v1/security/users", body, nil)
	if err != nil && (code == http.StatusConflict || code == http.StatusBadRequest) {
		_, err = a.adminRequest(ctx, http.MethodPut, "/v1/security/users/"+url.PathEscape(username), map[string]string{"password": password, "algorithm": mechanism}, nil)
	}
	if err != nil {
		return err
	}
	acls := make([]kafka.ACLEntry, 0, len(unique)*2+1)
	for _, topic := range unique {
		for _, operation := range []kafka.ACLOperationType{kafka.ACLOperationTypeRead, kafka.ACLOperationTypeDescribe} {
			acls = append(acls, kafka.ACLEntry{ResourceType: kafka.ResourceTypeTopic, ResourceName: topic, ResourcePatternType: kafka.PatternTypeLiteral, Principal: "User:" + username, Host: "*", Operation: operation, PermissionType: kafka.ACLPermissionTypeAllow})
		}
	}
	acls = append(acls, kafka.ACLEntry{ResourceType: kafka.ResourceTypeGroup, ResourceName: group, ResourcePatternType: kafka.PatternTypeLiteral, Principal: "User:" + username, Host: "*", Operation: kafka.ACLOperationTypeRead, PermissionType: kafka.ACLPermissionTypeAllow})
	granted, err := a.client.CreateACLs(ctx, &kafka.CreateACLsRequest{ACLs: acls})
	if err != nil || granted == nil || len(granted.Errors) != len(acls) {
		return errors.New("could not grant Kafka consumer access")
	}
	for _, err := range granted.Errors {
		if err != nil {
			return errors.New("could not grant Kafka consumer access")
		}
	}
	return a.verifyACLs(ctx, username, acls)
}

func (a *ConsumerAdmin) verifyACLs(ctx context.Context, username string, expected []kafka.ACLEntry) error {
	response, err := a.client.DescribeACLs(ctx, &kafka.DescribeACLsRequest{Filter: kafka.ACLFilter{ResourceTypeFilter: kafka.ResourceTypeAny, ResourcePatternTypeFilter: kafka.PatternTypeAny, PrincipalFilter: "User:" + username, Operation: kafka.ACLOperationTypeAny, PermissionType: kafka.ACLPermissionTypeAny}})
	if err != nil || response == nil || response.Error != nil {
		return errors.New("could not verify Kafka consumer ACLs")
	}
	actual := make([]kafka.ACLEntry, 0, len(expected))
	for _, resource := range response.Resources {
		for _, acl := range resource.ACLs {
			actual = append(actual, kafka.ACLEntry{ResourceType: resource.ResourceType, ResourceName: resource.ResourceName, ResourcePatternType: resource.PatternType, Principal: acl.Principal, Host: acl.Host, Operation: acl.Operation, PermissionType: acl.PermissionType})
		}
	}
	if len(actual) != len(expected) {
		return errors.New("Kafka consumer ACLs do not match the requested grants")
	}
	for _, acl := range expected {
		if !slices.Contains(actual, acl) {
			return errors.New("Kafka consumer ACLs do not match the requested grants")
		}
	}
	return nil
}
