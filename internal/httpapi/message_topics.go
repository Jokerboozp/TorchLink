package httpapi

import (
	"context"
	"errors"
	"fmt"
	"iot-platform/internal/devicescope"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	kafkaadapter "iot-platform/internal/adapters/kafka"
	"iot-platform/internal/messagetopics"
	"iot-platform/internal/model"
)

func (s *Server) messageTopicRoutes() {
	s.router.GET("/api/v1/message-topics", s.authorize("viewer"), s.endpoint(s.listMessageTopics))
	s.router.POST("/api/v1/message-topics", s.authorize("admin"), s.endpoint(s.createMessageTopic))
	s.router.POST("/api/v1/message-topics/query/preview", s.authorize("admin"), s.endpoint(s.previewMessageTopicQuery))
	s.router.PUT("/api/v1/message-topics/:id", s.authorize("admin"), s.endpoint(s.updateMessageTopic, "id"))
	s.router.DELETE("/api/v1/message-topics/:id", s.authorize("admin"), s.endpoint(s.deleteMessageTopic, "id"))
}

type messageTopicView struct {
	ID               string                       `json:"id"`
	Name             string                       `json:"name"`
	Protocol         string                       `json:"protocol"`
	Topic            string                       `json:"topic"`
	Enabled          bool                         `json:"enabled"`
	EffectiveEnabled bool                         `json:"effectiveEnabled"`
	Description      string                       `json:"description"`
	Exposure         []model.MessageTopicExposure `json:"exposure"`
	Query            *model.MessageTopicQuery     `json:"query"`
	QuerySQL         string                       `json:"querySql"`
	KeyIDs           []string                     `json:"keyIds"`
}

// topicKeys lists the open API keys that can be granted topic subscriptions.
func (s *Server) topicKeys(ctx context.Context, tenant string) (map[string]model.APIKey, error) {
	store, err := s.accessStore()
	if err != nil {
		return nil, err
	}
	state, err := store.LoadAccessState(ctx, tenant)
	if err != nil {
		return nil, err
	}
	keys := map[string]model.APIKey{}
	for _, key := range state.APIKeys {
		if slices.Contains(key.Capabilities, model.APICapabilityTopicsSubscribe) {
			key.SecretHash = ""
			keys[key.ID] = key
		}
	}
	return keys, nil
}

func (s *Server) messageTopicResponse(w http.ResponseWriter, r *http.Request, cfg model.MessageTopicConfig, extra map[string]any) {
	tenant := claims(r).TenantID
	mqttEnabled := strings.TrimSpace(s.cfg.MQTTBroker) != ""
	kafkaEnabled := len(s.cfg.KafkaBrokers) > 0
	effective := func(protocol, source string, enabled bool) bool {
		enabled = enabled && ((protocol == "mqtt" && mqttEnabled) || (protocol == "kafka" && kafkaEnabled))
		if source == "kafka.property-report" || source == "kafka.event-report" || source == "kafka.parsed" {
			enabled = enabled && s.engine.PublishExternalTopics
		}
		return enabled
	}
	items := make([]messageTopicView, 0, len(cfg.Topics))
	for _, route := range cfg.Topics {
		source := ""
		if route.Query.Mode == "realtime" {
			source = messagetopics.QuerySourceID(route.Protocol, route.Query.Dataset)
		}
		keyIDs := route.KeyIDs
		if keyIDs == nil {
			keyIDs = []string{}
		}
		items = append(items, messageTopicView{ID: route.ID, Name: route.Name, Protocol: route.Protocol, Topic: messagetopics.Destination(tenant, route), Enabled: route.Enabled, EffectiveEnabled: effective(route.Protocol, source, route.Enabled), Description: route.Description, Exposure: route.Exposure, Query: route.Query, QuerySQL: messagetopics.QuerySQL(*route.Query), KeyIDs: keyIDs})
	}
	builtin := []map[string]any{}
	for _, entry := range messagetopics.Catalog() {
		builtin = append(builtin, map[string]any{"id": entry.ID, "name": entry.Name, "protocol": entry.Protocol, "direction": entry.Direction, "topic": entry.DefaultTopic, "reason": entry.Reason, "effectiveEnabled": effective(entry.Protocol, entry.ID, true)})
	}
	keys := []map[string]any{}
	if requestAllows(r, "PUT", "/api/v1/access/api-keys/:id") {
		if available, err := s.topicKeys(r.Context(), tenant); err == nil {
			for _, key := range available {
				keys = append(keys, map[string]any{"id": key.ID, "name": key.Name, "username": key.Username, "enabled": key.Enabled, "expiresAt": key.ExpiresAt})
			}
			slices.SortFunc(keys, func(a, b map[string]any) int { return strings.Compare(a["name"].(string), b["name"].(string)) })
		}
	}
	readiness := map[string]any{}
	for _, protocol := range []string{"mqtt", "kafka"} {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		err := s.messageTopicAuthorizationReady(ctx, protocol)
		cancel()
		reason := ""
		if err != nil {
			reason = err.Error()
		}
		readiness[protocol] = map[string]any{"ready": err == nil, "reason": reason}
	}
	result := map[string]any{"revision": cfg.Revision, "items": items, "builtin": builtin, "datasets": messagetopics.QueryDatasets(), "keys": keys, "authorization": readiness,
		"runtime":  map[string]bool{"mqttEnabled": mqttEnabled, "kafkaEnabled": kafkaEnabled, "kafkaParsedEnabled": s.engine.PublishExternalTopics},
		"prefixes": map[string]string{"mqtt": messagetopics.MQTTPrefix(tenant), "kafka": messagetopics.KafkaPrefix(tenant)}}
	for key, value := range extra {
		result[key] = value
	}
	write(w, 200, result)
}

func (s *Server) loadMessageTopics(w http.ResponseWriter, r *http.Request) (model.MessageTopicConfig, bool) {
	if s.engine.MessageTopics == nil {
		problem(w, 503, "消息主题管理尚未初始化")
		return model.MessageTopicConfig{}, false
	}
	cfg, err := s.engine.MessageTopics.Load(r.Context(), claims(r).TenantID)
	if err != nil {
		s.fail(w, r, err, "读取消息主题配置失败")
		return cfg, false
	}
	return cfg, true
}

func (s *Server) listMessageTopics(w http.ResponseWriter, r *http.Request) {
	if cfg, ok := s.loadMessageTopics(w, r); ok {
		s.messageTopicResponse(w, r, cfg, nil)
	}
}

func messageTopicWriteAllowed(w http.ResponseWriter, r *http.Request) bool {
	if devicescope.Limited(r.Context()) {
		problem(w, 403, "配置消息主题需要全部设备范围及设备管理菜单权限")
		return false
	}
	return true
}

type messageTopicInput struct {
	Revision    *int64 `json:"revision"`
	Enabled     *bool  `json:"enabled"`
	Name        string `json:"name"`
	Protocol    string `json:"protocol"`
	Topic       string `json:"topic"`
	Description string `json:"description"`
	messageTopicQueryInput
	KeyIDs *[]string `json:"keyIds"`
}

func topicRevision(w http.ResponseWriter, revision *int64, cfg model.MessageTopicConfig) bool {
	if revision == nil || *revision < 0 {
		problem(w, 422, "请提交当前配置版本")
		return false
	}
	if cfg.Revision != *revision {
		problem(w, 409, "消息主题配置已被修改，请刷新后重试")
		return false
	}
	return true
}

func (s *Server) createMessageTopic(w http.ResponseWriter, r *http.Request) {
	if !messageTopicWriteAllowed(w, r) {
		return
	}
	var in messageTopicInput
	if decode(w, r, &in) != nil {
		return
	}
	cfg, ok := s.loadMessageTopics(w, r)
	if !ok || !topicRevision(w, in.Revision, cfg) {
		return
	}
	if in.Enabled == nil {
		problem(w, 422, "请提交主题开关")
		return
	}
	if in.Query == nil && in.QuerySQL == nil {
		problem(w, 422, "请配置主题的数据查询")
		return
	}
	route := model.MessageTopicRoute{ID: "topic_" + randomHex(8), Name: strings.TrimSpace(in.Name), Protocol: in.Protocol, Topic: strings.TrimSpace(in.Topic), Enabled: *in.Enabled, Description: strings.TrimSpace(in.Description)}
	route.Topic = messagetopics.Destination(claims(r).TenantID, route)
	cfg.Topics = append(cfg.Topics, route)
	if !s.configureMessageTopic(w, r, &cfg, route.ID, in) {
		return
	}
	if route.Protocol == "kafka" && !s.createBrokerMessageTopic(w, r, route.Topic) {
		return
	}
	s.saveMessageTopics(w, r, cfg, "message-topic.create", route.ID)
}

func (s *Server) updateMessageTopic(w http.ResponseWriter, r *http.Request) {
	if !messageTopicWriteAllowed(w, r) {
		return
	}
	var in messageTopicInput
	if decode(w, r, &in) != nil {
		return
	}
	cfg, ok := s.loadMessageTopics(w, r)
	if !ok || !topicRevision(w, in.Revision, cfg) {
		return
	}
	if in.Enabled == nil {
		problem(w, 422, "请提交主题开关")
		return
	}
	id := r.PathValue("id")
	index := slices.IndexFunc(cfg.Topics, func(route model.MessageTopicRoute) bool { return route.ID == id })
	if index < 0 {
		problem(w, 404, "消息主题不存在")
		return
	}
	route := &cfg.Topics[index]
	if in.Protocol != route.Protocol || messagetopics.Destination(claims(r).TenantID, model.MessageTopicRoute{Protocol: in.Protocol, Topic: strings.TrimSpace(in.Topic)}) != route.Topic {
		problem(w, 422, "主题协议和地址创建后不可修改，请新建主题")
		return
	}
	route.Name, route.Enabled, route.Description = strings.TrimSpace(in.Name), *in.Enabled, strings.TrimSpace(in.Description)
	if !s.configureMessageTopic(w, r, &cfg, id, in) {
		return
	}
	s.saveMessageTopics(w, r, cfg, "message-topic.update", id)
}

func (s *Server) deleteMessageTopic(w http.ResponseWriter, r *http.Request) {
	if !messageTopicWriteAllowed(w, r) {
		return
	}
	cfg, ok := s.loadMessageTopics(w, r)
	if !ok {
		return
	}
	n, err := strconv.ParseInt(r.URL.Query().Get("revision"), 10, 64)
	if err != nil || !topicRevision(w, &n, cfg) {
		if err != nil {
			problem(w, 422, "请提交当前配置版本")
		}
		return
	}
	if !messagetopics.RetireTopic(claims(r).TenantID, &cfg, r.PathValue("id")) {
		problem(w, 404, "消息主题不存在")
		return
	}
	s.saveMessageTopics(w, r, cfg, "message-topic.delete", r.PathValue("id"))
}

// configureMessageTopic applies the query and subscriber keys of one topic.
func (s *Server) configureMessageTopic(w http.ResponseWriter, r *http.Request, cfg *model.MessageTopicConfig, id string, in messageTopicInput) bool {
	query, err := in.messageTopicQueryInput.canonicalQuery()
	if err != nil {
		problem(w, 422, err.Error())
		return false
	}
	index := slices.IndexFunc(cfg.Topics, func(route model.MessageTopicRoute) bool { return route.ID == id })
	route := &cfg.Topics[index]
	if query != nil {
		if !s.validateMessageTopicQuery(w, r, route.Protocol, *query) {
			return false
		}
		route.Query = query
		if err := messagetopics.AccumulateQueryExposure(cfg, id, *query); err != nil {
			problem(w, 422, err.Error())
			return false
		}
		route = &cfg.Topics[index]
	}
	if in.KeyIDs != nil && !slices.Equal(*in.KeyIDs, route.KeyIDs) {
		keys, err := s.topicSubscribers(r, *route, *in.KeyIDs)
		if err != nil {
			status := 422
			if errors.Is(err, errTopicKeyPermission) {
				status = 403
			}
			problem(w, status, err.Error())
			return false
		}
		route.KeyIDs = keys
	}
	if err := messagetopics.Validate(claims(r).TenantID, *cfg); err != nil {
		problem(w, 422, err.Error())
		return false
	}
	return true
}

var errTopicKeyPermission = errors.New("修改订阅密钥需要开放接口密钥的编辑权限")

// topicSubscribers checks newly granted keys against the topic's complete
// historical exposure. Existing grants stay; a now-invalid key is inert.
func (s *Server) topicSubscribers(r *http.Request, route model.MessageTopicRoute, ids []string) ([]string, error) {
	if !requestAllows(r, "PUT", "/api/v1/access/api-keys/:id") {
		return nil, errTopicKeyPermission
	}
	keys, err := s.topicKeys(r.Context(), claims(r).TenantID)
	if err != nil {
		return nil, errors.New("读取开放接口密钥失败")
	}
	route.Enabled = true
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if slices.Contains(out, id) {
			return nil, errors.New("订阅密钥列表包含重复项")
		}
		key, exists := keys[id]
		if !exists {
			if slices.Contains(route.KeyIDs, id) {
				continue // The key was deleted; drop its stale grant.
			}
			return nil, errors.New("所选密钥不存在或未开通“订阅消息主题”能力")
		}
		identity, err := s.messageTopicIdentity(r.Context(), claims(r).TenantID, id)
		if err != nil {
			if slices.Contains(route.KeyIDs, id) {
				out = append(out, id)
				continue
			}
			return nil, fmt.Errorf("密钥“%s”不可用：%v", key.Name, err)
		}
		if !messagetopics.RouteAllowed(route, identity) {
			return nil, fmt.Errorf("密钥“%s”的绑定用户无权访问此主题的全部数据范围", key.Name)
		}
		out = append(out, id)
	}
	return out, nil
}

func (s *Server) createBrokerMessageTopic(w http.ResponseWriter, r *http.Request, topic string) bool {
	admin, ok := s.topicBrokers.kafka.(messageTopicKafkaTopicAdmin)
	if !ok || len(s.cfg.KafkaBrokers) == 0 {
		problem(w, 503, "Kafka 主题管理未就绪，请配置 Broker 和管理服务")
		return false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := admin.CreateTopic(ctx, topic); err != nil {
		if errors.Is(err, kafkaadapter.ErrTopicExists) {
			problem(w, 422, "Kafka 主题地址已被占用，请更换名称，不能接管已有历史消息的主题")
			return false
		}
		problem(w, 503, "Kafka 主题创建失败，请检查 Broker 连接与管理权限")
		return false
	}
	return true
}

func (s *Server) saveMessageTopics(w http.ResponseWriter, r *http.Request, cfg model.MessageTopicConfig, action, id string) {
	tenant := claims(r).TenantID
	// Any policy edit invalidates issued snapshots, including in-flight provisioning.
	for i := range cfg.Credentials {
		if cfg.Credentials[i].Status != "revoked" {
			cfg.Credentials[i].Status = "revoking"
		}
	}
	saved, err := s.engine.MessageTopics.Save(r.Context(), tenant, cfg)
	if errors.Is(err, messagetopics.ErrInvalidConfig) {
		problem(w, 422, err.Error())
		return
	}
	if err != nil {
		s.fail(w, r, err, "保存消息主题配置失败")
		return
	}
	if !saved {
		problem(w, 409, "消息主题配置已被修改，请刷新后重试")
		return
	}
	s.audit(r, action, "message-topic", id, map[string]any{"revision": cfg.Revision + 1})
	extra := map[string]any{}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	if err := s.reconcileMessageTopicTenant(ctx, tenant); err != nil {
		extra["warning"] = "配置已保存，旧凭据撤销仍在重试；新数据已停止向旧凭据发布。"
	}
	cancel()
	if latest, err := s.engine.MessageTopics.Load(r.Context(), tenant); err == nil {
		cfg = latest
	} else {
		cfg.Revision++
	}
	s.messageTopicResponse(w, r, cfg, extra)
}
