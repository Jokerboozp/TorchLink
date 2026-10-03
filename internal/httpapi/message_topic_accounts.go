package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"iot-platform/internal/messagetopics"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type messageTopicKafkaGrantAdmin interface {
	ProvisionGrants(context.Context, string, string, string, []string, []string) error
}

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

func (s *Server) messageTopicIdentity(ctx context.Context, tenant, username string) (messagetopics.MessageTopicIdentity, error) {
	store, err := s.accessStore()
	if err != nil {
		return messagetopics.MessageTopicIdentity{}, err
	}
	state, err := store.LoadAccessState(ctx, tenant)
	if err != nil {
		return messagetopics.MessageTopicIdentity{}, err
	}
	for _, user := range state.Users {
		if user.Username != username {
			continue
		}
		if !user.Enabled {
			return messagetopics.MessageTopicIdentity{}, errors.New("绑定的平台用户已停用")
		}
		permissions := effectivePermissions(state, user)
		user = resolveUserDeviceScope(state, user)
		version := s.topicBrokerPassword(tenant, "access\x00"+accessVersion(user, permissions, tenant))
		return messagetopics.MessageTopicIdentity{Permissions: permissions, DeviceScope: user.DeviceScope, DeviceIDs: append([]string(nil), user.DeviceIDs...), Version: version}, nil
	}
	return messagetopics.MessageTopicIdentity{}, errors.New("绑定的平台用户不存在")
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

type messageTopicAccountInput struct {
	Revision        *int64   `json:"revision"`
	Name            string   `json:"name"`
	Username        string   `json:"username"`
	Enabled         bool     `json:"enabled"`
	TopicIDs        []string `json:"topicIds"`
	PublishTopicIDs []string `json:"publishTopicIds"`
	DeviceScope     string   `json:"deviceScope"`
	DeviceIDs       []string `json:"deviceIds"`
	ExpiresAt       int64    `json:"expiresAt"`
}

func (s *Server) validateMessageTopicAccount(ctx context.Context, tenant string, cfg model.MessageTopicConfig, a model.MessageTopicAccount) error {
	if !a.Enabled {
		return nil // Disabling an account must also work after its user was removed.
	}
	identity, err := s.messageTopicIdentity(ctx, tenant, a.Username)
	if err != nil {
		return err
	}
	if a.ExpiresAt > 0 && a.ExpiresAt <= time.Now().Unix() {
		return errors.New("账号到期时间必须晚于当前时间")
	}
	for _, id := range a.TopicIDs {
		if !messagetopics.RouteAllowed(cfg, id, identity, a) {
			return errors.New("绑定用户或账号范围没有所选订阅主题的数据访问权限")
		}
	}
	for _, id := range a.PublishTopicIDs {
		for _, topic := range cfg.Topics {
			if topic.ID == id && topic.Query != nil {
				return errors.New("查询主题仅支持订阅，不能授予外部发布权限")
			}
		}
		if !messagetopics.RoutePublishAllowed(cfg, id, identity, a) {
			return errors.New("发布授权仅支持绑定用户有消息主题权限的已启用共享主题")
		}
	}
	allowedDevices := make(map[string]bool, len(identity.DeviceIDs))
	for _, id := range identity.DeviceIDs {
		allowedDevices[id] = true
	}
	for _, id := range a.DeviceIDs {
		if identity.DeviceScope != "all" && !allowedDevices[id] {
			return errors.New("指定设备超出绑定用户的设备范围")
		}
	}
	if len(a.DeviceIDs) > 0 {
		if len(a.DeviceIDs) > 10000 {
			return errors.New("指定设备数量超过上限")
		}
		_, total, err := s.unscopedRepo().ListManagedDevicesFiltered(ctx, ports.DeviceFilter{TenantID: tenant, RestrictDevices: true, DeviceIDs: a.DeviceIDs}, 1, 0)
		if err != nil || total != len(a.DeviceIDs) {
			return errors.New("指定设备不存在或不属于当前租户")
		}
	}
	return nil
}
func accountFromInput(in messageTopicAccountInput) model.MessageTopicAccount {
	return model.MessageTopicAccount{Name: strings.TrimSpace(in.Name), Username: strings.TrimSpace(in.Username), Enabled: in.Enabled, TopicIDs: in.TopicIDs, PublishTopicIDs: in.PublishTopicIDs, DeviceScope: in.DeviceScope, DeviceIDs: in.DeviceIDs, ExpiresAt: in.ExpiresAt}
}
func (s *Server) createMessageTopicAccount(w http.ResponseWriter, r *http.Request) {
	if !messageTopicWriteAllowed(w, r) {
		return
	}
	var in messageTopicAccountInput
	if decode(w, r, &in) != nil {
		return
	}
	cfg, ok := s.loadMessageTopics(w, r)
	if !ok || !topicRevision(w, in.Revision, cfg) {
		return
	}
	a := accountFromInput(in)
	a.ID = "account_" + randomHex(8)
	a.CreatedAt = time.Now().Unix()
	secret := randomHex(32)
	a.SecretHash = apiKeySecretHash(secret)
	if err := s.validateMessageTopicAccount(r.Context(), claims(r).TenantID, cfg, a); err != nil {
		problem(w, 422, err.Error())
		return
	}
	cfg.Accounts = append(cfg.Accounts, a)
	w.Header().Set("Cache-Control", "no-store")
	s.saveMessageTopics(w, r, cfg, "message-topic-account.create", map[string]any{"accountSecret": map[string]string{"id": a.ID, "secret": secret}})
}
func (s *Server) updateMessageTopicAccount(w http.ResponseWriter, r *http.Request) {
	if !messageTopicWriteAllowed(w, r) {
		return
	}
	var in messageTopicAccountInput
	if decode(w, r, &in) != nil {
		return
	}
	cfg, ok := s.loadMessageTopics(w, r)
	if !ok || !topicRevision(w, in.Revision, cfg) {
		return
	}
	for i, old := range cfg.Accounts {
		if old.ID == r.PathValue("id") {
			a := accountFromInput(in)
			a.ID, a.SecretHash, a.CreatedAt = old.ID, old.SecretHash, old.CreatedAt
			if err := s.validateMessageTopicAccount(r.Context(), claims(r).TenantID, cfg, a); err != nil {
				problem(w, 422, err.Error())
				return
			}
			cfg.Accounts[i] = a
			s.saveMessageTopics(w, r, cfg, "message-topic-account.update", nil)
			return
		}
	}
	problem(w, 404, "对接账号不存在")
}
func (s *Server) deleteMessageTopicAccount(w http.ResponseWriter, r *http.Request) {
	if !messageTopicWriteAllowed(w, r) {
		return
	}
	cfg, ok := s.loadMessageTopics(w, r)
	if !ok || !topicQueryRevision(w, r, cfg) {
		return
	}
	before := len(cfg.Accounts)
	cfg.Accounts = slices.DeleteFunc(cfg.Accounts, func(a model.MessageTopicAccount) bool { return a.ID == r.PathValue("id") })
	if len(cfg.Accounts) == before {
		problem(w, 404, "对接账号不存在")
		return
	}
	s.saveMessageTopics(w, r, cfg, "message-topic-account.delete", nil)
}
func (s *Server) rotateMessageTopicAccount(w http.ResponseWriter, r *http.Request) {
	if !messageTopicWriteAllowed(w, r) {
		return
	}
	cfg, ok := s.loadMessageTopics(w, r)
	if !ok || !topicQueryRevision(w, r, cfg) {
		return
	}
	for i := range cfg.Accounts {
		if cfg.Accounts[i].ID == r.PathValue("id") {
			secret := randomHex(32)
			cfg.Accounts[i].SecretHash = apiKeySecretHash(secret)
			w.Header().Set("Cache-Control", "no-store")
			s.saveMessageTopics(w, r, cfg, "message-topic-account.rotate", map[string]any{"accountSecret": map[string]string{"id": cfg.Accounts[i].ID, "secret": secret}})
			return
		}
	}
	problem(w, 404, "对接账号不存在")
}
func (s *Server) issueMessageTopicCredentials(w http.ResponseWriter, r *http.Request) {
	if !messageTopicWriteAllowed(w, r) {
		return
	}
	var in struct {
		Revision *int64 `json:"revision"`
		Protocol string `json:"protocol"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	cfg, ok := s.loadMessageTopics(w, r)
	if !ok || !topicRevision(w, in.Revision, cfg) {
		return
	}
	result, status, err := s.issueTopicCredential(r.Context(), claims(r).TenantID, r.PathValue("id"), in.Protocol, "", *in.Revision)
	if err != nil {
		problem(w, status, err.Error())
		return
	}
	s.audit(r, "message-topic-account.credentials", "message-topic-account", r.PathValue("id"), map[string]any{"protocol": in.Protocol})
	w.Header().Set("Cache-Control", "no-store")
	write(w, 200, result)
}
func (s *Server) exchangeMessageTopicCredentials(w http.ResponseWriter, r *http.Request) {
	var in struct {
		TenantID  string `json:"tenantId"`
		AccountID string `json:"accountId"`
		Secret    string `json:"secret"`
		Protocol  string `json:"protocol"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	if len(in.TenantID) > 128 || len(in.AccountID) > 80 || len(in.Secret) > 128 || in.TenantID == "" || in.Secret == "" {
		problem(w, 401, "对接账号或密钥无效")
		return
	}
	key := "message-topic\x00" + in.TenantID + "\x00" + in.AccountID
	if wait := s.logins.retryAfter(key); wait > 0 {
		lockedOut(w, wait)
		return
	}
	result, status, err := s.issueTopicCredential(r.Context(), in.TenantID, in.AccountID, in.Protocol, in.Secret)
	if status == 401 || err == nil {
		s.logins.record(key, err == nil)
	}
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
func (s *Server) topicCredentialResponse(tenant string, credential model.MessageTopicCredential, revision int64) (map[string]any, error) {
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
		password, err = s.auth.IssueTopicClient(credential.Username, tenant, credential.Topics, credential.PublishTopics, time.Unix(credential.ExpiresAt, 0))
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
	return map[string]any{"revision": revision, "protocol": credential.Protocol, "username": credential.Username, "password": password, "topics": append([]string{}, credential.Topics...), "subscribeTopics": append([]string{}, credential.Topics...), "publishTopics": append([]string{}, credential.PublishTopics...), "groupId": credential.GroupID, "expiresAt": credential.ExpiresAt, "broker": broker, "mechanism": mechanism, "tls": tlsEnabled, "securityProtocol": securityProtocol}, nil
}

// Snapshot the exact current grants. Shared topics use their real address;
// legacy source routes retain their credential-isolated destinations.
func topicCredentialGrants(tenant string, cfg model.MessageTopicConfig, account model.MessageTopicAccount, identity messagetopics.MessageTopicIdentity, credential model.MessageTopicCredential) (subscribe, publish []string) {
	destination := func(id string) string {
		for _, route := range cfg.Topics {
			if route.ID == id && route.Protocol != "" && route.SourceID == "" {
				return messagetopics.SharedDestination(tenant, route)
			}
		}
		return messagetopics.Destination(tenant, id, credential)
	}
	for _, id := range account.TopicIDs {
		source, ok := messagetopics.RouteSource(cfg, id)
		if ok && source.Protocol == credential.Protocol && messagetopics.RouteAllowed(cfg, id, identity, account) {
			subscribe = append(subscribe, destination(id))
		}
	}
	for _, id := range account.PublishTopicIDs {
		source, ok := messagetopics.RouteSource(cfg, id)
		if ok && source.Protocol == credential.Protocol && messagetopics.RoutePublishAllowed(cfg, id, identity, account) {
			publish = append(publish, destination(id))
		}
	}
	return subscribe, publish
}

func (s *Server) issueTopicCredential(ctx context.Context, tenant, accountID, protocol, secret string, expectedRevision ...int64) (map[string]any, int, error) {
	if s.engine.MessageTopics == nil {
		return nil, 503, errors.New("消息主题管理未初始化")
	}
	if protocol != "mqtt" && protocol != "kafka" {
		return nil, 422, errors.New("请选择 MQTT 或 Kafka")
	}
	s.messageTopicCredentialsMu.Lock()
	defer s.messageTopicCredentialsMu.Unlock()
	cfg, err := s.engine.MessageTopics.Load(ctx, tenant)
	if err != nil {
		return nil, 503, errors.New("读取账号失败")
	}
	if len(expectedRevision) > 0 && expectedRevision[0] != cfg.Revision {
		return nil, 409, errors.New("授权配置已更新，请刷新后重试")
	}
	var account model.MessageTopicAccount
	found := false
	for _, a := range cfg.Accounts {
		if a.ID == accountID {
			account = a
			found = true
			break
		}
	}
	if !found || (secret != "" && subtle.ConstantTimeCompare([]byte(account.SecretHash), []byte(apiKeySecretHash(secret))) != 1) {
		return nil, 401, errors.New("对接账号或密钥无效")
	}
	now := time.Now().Unix()
	if !account.Enabled || (account.ExpiresAt > 0 && account.ExpiresAt <= now) {
		return nil, 403, errors.New("对接账号已停用或过期")
	}
	identity, err := s.messageTopicIdentity(ctx, tenant, account.Username)
	if err != nil {
		return nil, 403, err
	}
	readyCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	err = s.messageTopicAuthorizationReady(readyCtx, protocol)
	cancel()
	if err != nil {
		return nil, 503, err
	}
	// Broker checks may involve slow network I/O. Revalidate the snapshot before
	// returning an existing credential or provisioning one from stale grants.
	checked, err := s.engine.MessageTopics.Load(ctx, tenant)
	if err != nil {
		return nil, 503, errors.New("读取账号失败")
	}
	if checked.Revision != cfg.Revision {
		return nil, 409, errors.New("授权配置已更新，请重试")
	}
	checkedIdentity, err := s.messageTopicIdentity(ctx, tenant, account.Username)
	if err != nil {
		return nil, 403, err
	}
	if checkedIdentity.Version != identity.Version {
		return nil, 409, errors.New("绑定用户权限已更新，请重试")
	}
	now = time.Now().Unix()
	if account.ExpiresAt > 0 && account.ExpiresAt <= now {
		return nil, 403, errors.New("对接账号已过期")
	}
	// Reuse a current lease so polling the exchange endpoint does not duplicate
	// publications or create unbounded broker identities and topics.
	for i, c := range cfg.Credentials {
		if c.AccountID != accountID || c.Protocol != protocol || (c.Status == "revoking" || c.Status == "revoked") {
			continue
		}
		if c.Status == "provisioning" {
			return nil, 409, errors.New("凭据正在生成，请稍后重试")
		}
		subscribe, publish := topicCredentialGrants(tenant, cfg, account, identity, c)
		if !c.Provisioning && c.AccessVersion == identity.Version && c.ExpiresAt > now && len(subscribe)+len(publish) > 0 && slices.Equal(c.Topics, subscribe) && slices.Equal(c.PublishTopics, publish) {
			// Renew the lease without changing its destination. Consumers poll
			// before expiry and retain offsets/history until their grants change.
			if c.ExpiresAt <= now+600 {
				expires := now + 3600
				if account.ExpiresAt > 0 && expires > account.ExpiresAt {
					expires = account.ExpiresAt
				}
				if expires > c.ExpiresAt {
					c.ExpiresAt = expires
					cfg.Credentials[i] = c
					ok, e := s.engine.MessageTopics.Save(ctx, tenant, cfg)
					if e != nil {
						return nil, 503, errors.New("续期凭据失败")
					}
					if !ok {
						return nil, 409, errors.New("授权配置已更新，请重试")
					}
					cfg.Revision++
				}
			}
			result, e := s.topicCredentialResponse(tenant, c, cfg.Revision)
			return result, 500, e
		}
	}
	credential := model.MessageTopicCredential{ID: randomHex(16), AccountID: accountID, Protocol: protocol, AccessVersion: identity.Version, Status: "provisioning", Provisioning: true, CreatedAt: now, ExpiresAt: now + 3600}
	credential.Username = "iot-topic-" + credential.ID
	if account.ExpiresAt > 0 && credential.ExpiresAt > account.ExpiresAt {
		credential.ExpiresAt = account.ExpiresAt
	}
	credential.Topics, credential.PublishTopics = topicCredentialGrants(tenant, cfg, account, identity, credential)
	if len(credential.Topics)+len(credential.PublishTopics) == 0 {
		return nil, 422, errors.New("此账号没有可用的已授权主题，请检查绑定用户权限和主题开关")
	}
	if protocol == "kafka" {
		if len(credential.Topics) > 0 {
			credential.GroupID = credential.Username
		}
		if len(credential.PublishTopics) > 0 {
			if _, ok := s.messageTopicKafka.(messageTopicKafkaGrantAdmin); !ok {
				return nil, 503, errors.New("Kafka 管理服务不支持发布授权")
			}
		}
	}
	for i, c := range cfg.Credentials {
		if c.AccountID == accountID && c.Protocol == protocol {
			cfg.Credentials[i].Status = "revoking"
		}
	}
	cfg.Credentials = append(cfg.Credentials, credential)
	if err := messagetopics.Validate(tenant, cfg); err != nil {
		return nil, 422, err
	}
	saved, err := s.engine.MessageTopics.Save(ctx, tenant, cfg)
	if err != nil {
		return nil, 500, errors.New("保存凭据状态失败")
	}
	if !saved {
		return nil, 409, errors.New("配置已更新，请重试")
	}
	// Record before side effects; a crash leaves a durable revocation target.
	operationCtx, operationCancel := context.WithTimeout(ctx, 20*time.Second)
	defer operationCancel()
	if protocol == "kafka" {
		if admin, ok := s.messageTopicKafka.(messageTopicKafkaGrantAdmin); ok {
			err = admin.ProvisionGrants(operationCtx, credential.Username, s.topicBrokerPassword(tenant, credential.ID), credential.GroupID, credential.Topics, credential.PublishTopics)
		} else {
			err = s.messageTopicKafka.Provision(operationCtx, credential.Username, s.topicBrokerPassword(tenant, credential.ID), credential.GroupID, credential.Topics)
		}
	}
	if err != nil {
		s.markTopicCredentialRevoking(context.WithoutCancel(ctx), tenant, credential.ID, true)
		return nil, 503, errors.New("Broker 凭据创建失败，已安排撤销，请稍后重试")
	}
	latest, err := s.engine.MessageTopics.Load(ctx, tenant)
	if time.Now().Unix() >= credential.ExpiresAt {
		err = errors.New("凭据在生成期间已过期")
	}
	if err == nil {
		currentIdentity, e := s.messageTopicIdentity(ctx, tenant, account.Username)
		if e != nil || currentIdentity.Version != identity.Version {
			err = errors.New("绑定用户权限已改变")
		}
	}
	active := false
	if err == nil {
		for i, c := range latest.Credentials {
			if c.ID == credential.ID && c.Status == "provisioning" {
				latest.Credentials[i].Status = "active"
				latest.Credentials[i].Provisioning = false
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
	latest.Revision++
	credential.Status, credential.Provisioning = "active", false
	result, err := s.topicCredentialResponse(tenant, credential, latest.Revision)
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
		var account model.MessageTopicAccount
		for _, a := range cfg.Accounts {
			if a.ID == c.AccountID {
				account = a
				break
			}
		}
		if account.ID == "" || !account.Enabled || (account.ExpiresAt > 0 && account.ExpiresAt <= now) {
			shouldRevoke = true
		}
		identity, e := s.messageTopicIdentity(ctx, tenant, account.Username)
		subscribe, publish := topicCredentialGrants(tenant, cfg, account, identity, c)
		if e != nil || identity.Version != c.AccessVersion || len(subscribe)+len(publish) == 0 || !slices.Equal(c.Topics, subscribe) || !slices.Equal(c.PublishTopics, publish) {
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
