package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"iot-platform/internal/messagetopics"
	"iot-platform/internal/model"
)

type messageTopicKafkaTopicAdmin interface {
	CreateTopic(context.Context, string) error
}

type messageTopicKafkaAdmin interface {
	Ready(context.Context) error
	Provision(context.Context, string, string, string, []string) error
	Revoke(context.Context, string) error
}

// Configure before serving requests. Publishing uses the separately locked
// identity resolver and never relies on mutable request-wide scope.
func (s *Server) SetMessageTopicKafkaAdmin(admin messageTopicKafkaAdmin) { s.messageTopicKafka = admin }
func (s *Server) SetMessageTopicMQTTReadiness(ready func(context.Context) error) {
	s.messageTopicMQTTReady = ready
}

// messageTopicIdentity resolves an open API key for topic subscription. The
// version covers the key itself, so rotating or editing it revokes credentials.
func (s *Server) messageTopicIdentity(ctx context.Context, tenant, keyID string) (messagetopics.MessageTopicIdentity, error) {
	store, err := s.accessStore()
	if err != nil {
		return messagetopics.MessageTopicIdentity{}, err
	}
	state, err := store.LoadAccessState(ctx, tenant)
	if err != nil {
		return messagetopics.MessageTopicIdentity{}, err
	}
	index := slices.IndexFunc(state.APIKeys, func(k model.APIKey) bool { return k.ID == keyID })
	if index < 0 {
		return messagetopics.MessageTopicIdentity{}, errors.New("开放接口密钥不存在")
	}
	key := state.APIKeys[index]
	if !key.Enabled || key.ExpiresAt > 0 && key.ExpiresAt <= time.Now().UnixMilli() {
		return messagetopics.MessageTopicIdentity{}, errors.New("开放接口密钥已停用或过期")
	}
	if !slices.Contains(key.Capabilities, model.APICapabilityTopicsSubscribe) {
		return messagetopics.MessageTopicIdentity{}, errors.New("开放接口密钥未开通订阅消息主题能力")
	}
	userIndex := slices.IndexFunc(state.Users, func(u model.PlatformUser) bool { return u.Username == key.Username && u.Enabled })
	if userIndex < 0 {
		return messagetopics.MessageTopicIdentity{}, errors.New("密钥绑定的平台用户不存在或已停用")
	}
	user := state.Users[userIndex]
	permissions := effectivePermissions(state, user)
	s.stripOpsPermissions(tenant, permissions)
	user = resolveUserDeviceScope(state, user)
	version := s.topicBrokerPassword(tenant, "access\x00"+accessVersion(user, permissions, tenant)+"\x00"+key.SecretHash+"\x00"+strconv.FormatInt(key.ExpiresAt, 10))
	return messagetopics.MessageTopicIdentity{Permissions: permissions, DeviceScope: user.DeviceScope, DeviceIDs: append([]string(nil), user.DeviceIDs...), Version: version, ExpiresAt: key.ExpiresAt / 1000}, nil
}

func (s *Server) messageTopicAuthorizationReady(ctx context.Context, protocol string) error {
	switch protocol {
	case "mqtt":
		if s.cfg.MQTTBroker == "" {
			return errors.New("未配置 MQTT Broker")
		}
		if strings.TrimSpace(s.cfg.MQTTPublicURL) == "" {
			return errors.New("请配置 MQTT 对外连接地址 IOT_DEVICE_MQTT_PUBLIC_URL")
		}
		if s.messageTopicMQTTReady == nil || s.onboarding.RevokeUsername == nil {
			return errors.New("未配置 MQTT 授权检查与凭据撤销服务")
		}
		if err := s.messageTopicMQTTReady(ctx); err != nil {
			return fmt.Errorf("MQTT 授权未就绪：%w", err)
		}
	case "kafka":
		if len(s.cfg.KafkaBrokers) == 0 {
			return errors.New("未配置 Kafka Broker")
		}
		if len(s.cfg.KafkaPublicBrokers) == 0 {
			return errors.New("请配置 Kafka 对外连接地址 IOT_KAFKA_PUBLIC_BROKERS")
		}
		if s.messageTopicKafka == nil {
			return errors.New("未配置 Kafka SASL 与授权管理服务")
		}
		if err := s.messageTopicKafka.Ready(ctx); err != nil {
			return fmt.Errorf("Kafka 授权未就绪：%w", err)
		}
	default:
		return errors.New("仅支持 MQTT 或 Kafka 凭据")
	}
	return nil
}

// exchangeMessageTopicCredentials runs under open API key authentication.
func (s *Server) exchangeMessageTopicCredentials(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Protocol string `json:"protocol"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	key, _ := requestAPIKeyRecord(r.Context())
	result, status, err := s.issueTopicCredential(r.Context(), claims(r).TenantID, key.ID, in.Protocol)
	if err != nil {
		problem(w, status, err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	write(w, 200, result)
}

func (s *Server) topicBrokerPassword(tenant, id string) string {
	mac := hmac.New(sha256.New, []byte(s.cfg.JWTSecret))
	mac.Write([]byte("topic-consumer\x00" + tenant + "\x00" + id))
	return hex.EncodeToString(mac.Sum(nil))
}
func (s *Server) topicCredentialResponse(tenant string, credential model.MessageTopicCredential) (map[string]any, error) {
	password := s.topicBrokerPassword(tenant, credential.ID)
	broker := strings.Join(s.cfg.KafkaPublicBrokers, ",")
	mechanism := "SCRAM-SHA-256"
	securityProtocol := "SASL_PLAINTEXT"
	tlsEnabled := s.cfg.KafkaTLS
	if tlsEnabled {
		securityProtocol = "SASL_SSL"
	}
	if credential.Protocol == "mqtt" {
		var err error
		password, err = s.auth.IssueTopicConsumer(credential.Username, tenant, credential.Topics, time.Unix(credential.ExpiresAt, 0))
		if err != nil {
			return nil, err
		}
		broker = s.cfg.MQTTPublicURL
		if broker == "" {
			broker = s.cfg.MQTTBroker
		}
		mechanism = "JWT"
		securityProtocol = "MQTT"
		tlsEnabled = strings.HasPrefix(broker, "ssl://") || strings.HasPrefix(broker, "tls://") || strings.HasPrefix(broker, "mqtts://") || strings.HasPrefix(broker, "wss://")
	}
	return map[string]any{"protocol": credential.Protocol, "username": credential.Username, "password": password, "subscribeTopics": append([]string{}, credential.Topics...), "groupId": credential.GroupID, "expiresAt": credential.ExpiresAt, "broker": broker, "mechanism": mechanism, "tls": tlsEnabled, "securityProtocol": securityProtocol}, nil
}

func credentialExpiry(now int64, identity messagetopics.MessageTopicIdentity) int64 {
	expires := now + 3600
	if identity.ExpiresAt > 0 && expires > identity.ExpiresAt {
		expires = identity.ExpiresAt
	}
	return expires
}

func (s *Server) issueTopicCredential(ctx context.Context, tenant, keyID, protocol string) (map[string]any, int, error) {
	if s.engine.MessageTopics == nil {
		return nil, 503, errors.New("消息主题管理未初始化")
	}
	if protocol != "mqtt" && protocol != "kafka" {
		return nil, 422, errors.New("请选择 MQTT 或 Kafka")
	}
	s.messageTopicCredentialsMu.Lock()
	defer s.messageTopicCredentialsMu.Unlock()
	identity, err := s.messageTopicIdentity(ctx, tenant, keyID)
	if err != nil {
		return nil, 403, err
	}
	readyCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	err = s.messageTopicAuthorizationReady(readyCtx, protocol)
	cancel()
	if err != nil {
		return nil, 503, err
	}
	// Broker checks may involve slow network I/O. Load the policy afterwards
	// so neither a reused nor a new credential comes from stale grants.
	cfg, err := s.engine.MessageTopics.Load(ctx, tenant)
	if err != nil {
		return nil, 503, errors.New("读取主题授权失败")
	}
	if checked, err := s.messageTopicIdentity(ctx, tenant, keyID); err != nil || checked.Version != identity.Version {
		return nil, 409, errors.New("密钥或绑定用户权限已更新，请重试")
	}
	now := time.Now().Unix()
	grants := messagetopics.CredentialTopics(tenant, cfg, keyID, protocol, identity)
	// Reuse a current lease so polling the exchange endpoint does not create
	// unbounded broker identities; renew it without changing its destination.
	for i, c := range cfg.Credentials {
		if c.KeyID != keyID || c.Protocol != protocol || c.Status == "revoking" || c.Status == "revoked" {
			continue
		}
		if c.Status == "provisioning" {
			return nil, 409, errors.New("凭据正在生成，请稍后重试")
		}
		if c.Provisioning || c.AccessVersion != identity.Version || c.ExpiresAt <= now || len(grants) == 0 || !slices.Equal(c.Topics, grants) {
			break
		}
		if expires := credentialExpiry(now, identity); c.ExpiresAt <= now+600 && expires > c.ExpiresAt {
			cfg.Credentials[i].ExpiresAt = expires
			ok, err := s.engine.MessageTopics.Save(ctx, tenant, cfg)
			if err != nil {
				return nil, 503, errors.New("续期凭据失败")
			}
			if !ok {
				return nil, 409, errors.New("授权配置已更新，请重试")
			}
			c.ExpiresAt = expires
		}
		result, err := s.topicCredentialResponse(tenant, c)
		return result, 500, err
	}
	if len(grants) == 0 {
		return nil, 422, errors.New("此密钥没有可订阅的主题，请检查主题授权、主题开关和绑定用户权限")
	}
	credential := model.MessageTopicCredential{ID: randomHex(16), KeyID: keyID, Protocol: protocol, Topics: grants, AccessVersion: identity.Version, Status: "provisioning", Provisioning: true, CreatedAt: now, ExpiresAt: credentialExpiry(now, identity)}
	credential.Username = "iot-topic-" + credential.ID
	if protocol == "kafka" {
		credential.GroupID = credential.Username
	}
	for i, c := range cfg.Credentials {
		if c.KeyID == keyID && c.Protocol == protocol && c.Status != "revoked" {
			cfg.Credentials[i].Status = "revoking"
		}
	}
	cfg.Credentials = append(cfg.Credentials, credential)
	saved, err := s.engine.MessageTopics.Save(ctx, tenant, cfg)
	if err != nil {
		return nil, 500, errors.New("保存凭据状态失败")
	}
	if !saved {
		return nil, 409, errors.New("配置已更新，请重试")
	}
	// Record before side effects; a crash leaves a durable revocation target.
	if protocol == "kafka" {
		operationCtx, operationCancel := context.WithTimeout(ctx, 20*time.Second)
		err = s.messageTopicKafka.Provision(operationCtx, credential.Username, s.topicBrokerPassword(tenant, credential.ID), credential.GroupID, credential.Topics)
		operationCancel()
		if err != nil {
			s.markTopicCredentialRevoking(context.WithoutCancel(ctx), tenant, credential.ID, true)
			return nil, 503, errors.New("Broker 凭据创建失败，已安排撤销，请稍后重试")
		}
	}
	latest, err := s.engine.MessageTopics.Load(ctx, tenant)
	if err == nil && time.Now().Unix() >= credential.ExpiresAt {
		err = errors.New("凭据在生成期间已过期")
	}
	if err == nil {
		if current, e := s.messageTopicIdentity(ctx, tenant, keyID); e != nil || current.Version != identity.Version {
			err = errors.New("密钥或绑定用户权限已改变")
		}
	}
	active := false
	if err == nil {
		for i, c := range latest.Credentials {
			if c.ID == credential.ID && c.Status == "provisioning" {
				latest.Credentials[i].Status, latest.Credentials[i].Provisioning = "active", false
				active = true
				break
			}
		}
	}
	if active {
		saved, err = s.engine.MessageTopics.Save(ctx, tenant, latest)
		active = err == nil && saved
	}
	if !active {
		s.markTopicCredentialRevoking(context.WithoutCancel(ctx), tenant, credential.ID, true)
		revokeCtx, revokeCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		_ = s.revokeTopicBrokerCredential(revokeCtx, credential)
		revokeCancel()
		return nil, 409, errors.New("授权配置在生成期间改变，凭据已失效并安排撤销，请重试")
	}
	credential.Status, credential.Provisioning = "active", false
	result, err := s.topicCredentialResponse(tenant, credential)
	return result, 500, err
}

func (s *Server) markTopicCredentialRevoking(ctx context.Context, tenant, id string, provisioningFinished ...bool) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for attempt := 0; attempt < 3; attempt++ {
		cfg, err := s.engine.MessageTopics.Load(ctx, tenant)
		if err != nil {
			return
		}
		found := false
		for i := range cfg.Credentials {
			if cfg.Credentials[i].ID == id {
				cfg.Credentials[i].Status = "revoking"
				if len(provisioningFinished) > 0 && provisioningFinished[0] {
					cfg.Credentials[i].Provisioning = false
				}
				found = true
			}
		}
		if !found {
			return
		}
		ok, err := s.engine.MessageTopics.Save(ctx, tenant, cfg)
		if err != nil || ok {
			return
		}
	}
}
func (s *Server) revokeTopicBrokerCredential(ctx context.Context, c model.MessageTopicCredential) error {
	if c.Protocol == "mqtt" {
		if s.onboarding.RevokeUsername == nil {
			return errors.New("MQTT 撤销服务不可用")
		}
		return s.onboarding.RevokeUsername(ctx, c.Username)
	}
	if s.messageTopicKafka == nil {
		return errors.New("Kafka 撤销服务不可用")
	}
	return s.messageTopicKafka.Revoke(ctx, c.Username)
}

// RetryMessageTopicRevocationsOnce uses persisted tenant records; it survives
// process restarts and also revokes leases after user/role/scope changes.
func (s *Server) RetryMessageTopicRevocationsOnce(ctx context.Context) error {
	tenants, err := s.unscopedRepo().ListMessageTopicTenants(ctx)
	if err != nil {
		return err
	}
	var failures []error
	for _, tenant := range tenants {
		if err := ctx.Err(); err != nil {
			failures = append(failures, err)
			break
		}
		if err := s.reconcileMessageTopicTenant(ctx, tenant); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
func (s *Server) reconcileMessageTopicTenant(ctx context.Context, tenant string) error {
	cfg, err := s.engine.MessageTopics.Load(ctx, tenant)
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	changed := false
	for i, c := range cfg.Credentials {
		if err := ctx.Err(); err != nil {
			return err
		}
		if c.Status == "revoking" || c.Status == "revoked" {
			continue
		}
		shouldRevoke := c.ExpiresAt <= now
		// Allow a short provisioning window, then recover crashed issuance.
		if c.Status == "provisioning" {
			shouldRevoke = shouldRevoke || c.CreatedAt+60 <= now
		}
		identity, e := s.messageTopicIdentity(ctx, tenant, c.KeyID)
		if e != nil || identity.Version != c.AccessVersion || (identity.ExpiresAt > 0 && c.ExpiresAt > identity.ExpiresAt) || !slices.Equal(c.Topics, messagetopics.CredentialTopics(tenant, cfg, c.KeyID, c.Protocol, identity)) {
			shouldRevoke = true
		}
		if shouldRevoke {
			cfg.Credentials[i].Status = "revoking"
			changed = true
		}
	}
	if changed {
		ok, err := s.engine.MessageTopics.Save(ctx, tenant, cfg)
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("凭据状态已更新，稍后重试")
		}
		cfg.Revision++
	}
	removed := map[string]bool{}
	var failures []error
	changed = false
	for i, c := range cfg.Credentials {
		if err := ctx.Err(); err != nil {
			failures = append(failures, err)
			break
		}
		if c.Status != "revoking" && c.Status != "revoked" {
			continue
		}
		revokeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		e := s.revokeTopicBrokerCredential(revokeCtx, c)
		cancel()
		if e != nil {
			failures = append(failures, e)
			if c.Status != "revoking" {
				cfg.Credentials[i].Status = "revoking"
				changed = true
			}
			continue
		}
		if c.Status != "revoked" {
			cfg.Credentials[i].Status = "revoked"
			changed = true
		}
		// Keep a tombstone until the lease expires, so late broker provisioning on
		// another replica is repeatedly cleaned instead of becoming an orphan.
		if c.ExpiresAt <= now {
			removed[c.ID] = true
		}
	}
	if len(removed) > 0 || changed {
		cfg.Credentials = slices.DeleteFunc(cfg.Credentials, func(c model.MessageTopicCredential) bool { return removed[c.ID] })
		ok, e := s.engine.MessageTopics.Save(ctx, tenant, cfg)
		if e != nil {
			failures = append(failures, e)
		} else if !ok {
			failures = append(failures, errors.New("凭据状态已更新，稍后重试"))
		}
	}
	return errors.Join(failures...)
}
