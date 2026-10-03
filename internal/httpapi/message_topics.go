package httpapi

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"iot-platform/internal/messagetopics"
	"iot-platform/internal/model"
)

func (s *Server) messageTopicRoutes() {
	s.router.GET("/api/v1/message-topics", s.authorize("viewer"), s.endpoint(s.listMessageTopics))
	s.router.POST("/api/v1/message-topics", s.authorize("admin"), s.endpoint(s.createMessageTopic))
	s.router.PUT("/api/v1/message-topics/:id", s.authorize("admin"), s.endpoint(s.updateMessageTopic, "id"))
	s.router.DELETE("/api/v1/message-topics/:id", s.authorize("admin"), s.endpoint(s.deleteMessageTopic, "id"))
	s.router.POST("/api/v1/message-topics/:id/reset", s.authorize("admin"), s.endpoint(s.resetMessageTopic, "id"))
	s.router.POST("/api/v1/message-topics/:id/publish", s.authorize("admin"), s.endpoint(s.publishMessageTopic, "id"))
	s.router.POST("/api/v1/message-topics/:id/preview", s.authorize("admin"), s.endpoint(s.previewMessageTopicRule, "id"))
	s.router.POST("/api/v1/message-topics/:id/rules", s.authorize("admin"), s.endpoint(s.createMessageTopicRule, "id"))
	s.router.PUT("/api/v1/message-topics/:id/rules/:ruleId", s.authorize("admin"), s.endpoint(s.updateMessageTopicRule, "id", "ruleId"))
	s.router.DELETE("/api/v1/message-topics/:id/rules/:ruleId", s.authorize("admin"), s.endpoint(s.deleteMessageTopicRule, "id", "ruleId"))
	s.router.POST("/api/v1/message-topic-accounts", s.authorize("admin"), s.endpoint(s.createMessageTopicAccount))
	s.router.PUT("/api/v1/message-topic-accounts/:id", s.authorize("admin"), s.endpoint(s.updateMessageTopicAccount, "id"))
	s.router.DELETE("/api/v1/message-topic-accounts/:id", s.authorize("admin"), s.endpoint(s.deleteMessageTopicAccount, "id"))
	s.router.POST("/api/v1/message-topic-accounts/:id/rotate", s.authorize("admin"), s.endpoint(s.rotateMessageTopicAccount, "id"))
	s.router.POST("/api/v1/message-topic-accounts/:id/credentials", s.authorize("admin"), s.endpoint(s.issueMessageTopicCredentials, "id"))
	s.router.POST("/api/open/v1/message-topics/credentials", s.endpoint(s.exchangeMessageTopicCredentials))
}

type messageTopicDefinition = messagetopics.Topic

type messageTopicView struct {
	messageTopicDefinition
	Topic            string                       `json:"topic"`
	Enabled          bool                         `json:"enabled"`
	Description      string                       `json:"description"`
	Overridden       bool                         `json:"overridden"`
	EffectiveEnabled bool                         `json:"effectiveEnabled"`
	Custom           bool                         `json:"custom"`
	SourceID         string                       `json:"sourceId"`
	Shared           bool                         `json:"shared"`
	Exposure         []model.MessageTopicExposure `json:"exposure,omitempty"`
}

func (s *Server) messageTopicResponse(w http.ResponseWriter, r *http.Request, cfg model.MessageTopicConfig, extra map[string]any) {
	tenant := claims(r).TenantID
	mqttEnabled := strings.TrimSpace(s.cfg.MQTTBroker) != ""
	kafkaEnabled := len(s.cfg.KafkaBrokers) > 0
	items := make([]messageTopicView, 0)
	sources := []messagetopics.Topic{}
	effective := func(source messagetopics.Topic, enabled bool) bool {
		enabled = enabled && ((source.Protocol == "mqtt" && mqttEnabled) || (source.Protocol == "kafka" && kafkaEnabled))
		if source.ID == "kafka.property-report" || source.ID == "kafka.event-report" || source.ID == "kafka.parsed" {
			enabled = enabled && s.engine.PublishExternalTopics
		}
		return enabled
	}
	for _, entry := range messagetopics.Catalog() {
		if entry.Editable {
			sources = append(sources, entry)
		}
		if slices.Contains(cfg.Deleted, entry.ID) {
			continue
		}
		item := messageTopicView{messageTopicDefinition: entry, Topic: entry.DefaultTopic, Enabled: true, SourceID: entry.ID}
		if override, ok := cfg.Overrides[entry.ID]; ok && entry.Editable {
			item.Overridden = true
			item.Enabled, item.Description = override.Enabled, override.Description
			if override.Topic != "" {
				item.Topic = override.Topic
			}
		}
		item.EffectiveEnabled = effective(entry, item.Enabled)
		items = append(items, item)
	}
	for _, route := range cfg.Topics {
		source, ok := messagetopics.RouteSource(cfg, route.ID)
		if !ok {
			continue
		}
		entry := source
		entry.ID, entry.Name, entry.DefaultTopic = route.ID, route.Name, route.Topic
		entry.Reason = "按对接账号独立分发，实际订阅地址在生成凭据后提供。"
		if route.Protocol != "" {
			entry.Reason = "共享主题，可直接发布文本或 JSON；平台数据通过自动发送规则配置。"
		}
		items = append(items, messageTopicView{messageTopicDefinition: entry, Topic: route.Topic, Enabled: route.Enabled, Description: route.Description, EffectiveEnabled: effective(source, route.Enabled), Custom: true, SourceID: route.SourceID, Shared: route.Protocol != "", Exposure: route.Exposure})
	}
	accounts := []map[string]any{}
	for _, a := range cfg.Accounts {
		creds := []map[string]any{}
		for _, c := range cfg.Credentials {
			if c.AccountID == a.ID && c.Status != "revoked" {
				creds = append(creds, map[string]any{"protocol": c.Protocol, "status": c.Status, "expiresAt": c.ExpiresAt})
			}
		}
		accounts = append(accounts, map[string]any{"id": a.ID, "name": a.Name, "username": a.Username, "enabled": a.Enabled, "topicIds": a.TopicIDs, "publishTopicIds": a.PublishTopicIDs, "deviceScope": a.DeviceScope, "deviceIds": a.DeviceIDs, "createdAt": a.CreatedAt, "expiresAt": a.ExpiresAt, "credentials": creds})
	}
	users := []map[string]any{}
	if !limited(r.Context()) && (requestAllows(r, "POST", "/api/v1/message-topic-accounts") || requestAllows(r, "PUT", "/api/v1/message-topic-accounts/:id")) {
		if store, err := s.accessStore(); err == nil {
			if state, err := store.LoadAccessState(r.Context(), tenant); err == nil {
				for _, u := range state.Users {
					users = append(users, map[string]any{"username": u.Username, "displayName": u.DisplayName, "enabled": u.Enabled})
				}
			}
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
	rules := cfg.Rules
	if rules == nil {
		rules = []model.MessageTopicRule{}
	}
	result := map[string]any{"revision": cfg.Revision, "items": items, "sources": sources, "rules": rules, "accounts": accounts, "users": users, "authorization": readiness,
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
		problem(w, 500, "读取消息主题配置失败")
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
	if limited(r.Context()) {
		problem(w, 403, "配置租户消息发布需要全部设备范围及设备管理菜单权限")
		return false
	}
	return true
}
func editableTopic(w http.ResponseWriter, r *http.Request, cfg model.MessageTopicConfig) bool {
	if !messageTopicWriteAllowed(w, r) {
		return false
	}
	for _, source := range messagetopics.Catalog() {
		if source.ID == r.PathValue("id") && !source.Editable {
			problem(w, 422, "此主题由平台内部或设备协议使用，不能修改")
			return false
		}
	}
	source, ok := messagetopics.RouteSource(cfg, r.PathValue("id"))
	if !ok {
		problem(w, 404, "消息主题不存在")
		return false
	}
	if !source.Editable {
		problem(w, 422, "此主题由平台内部或设备协议使用，不能修改")
		return false
	}
	return true
}

type messageTopicInput struct {
	Revision    *int64 `json:"revision"`
	Enabled     *bool  `json:"enabled"`
	Name        string `json:"name"`
	SourceID    string `json:"sourceId"`
	Protocol    string `json:"protocol"`
	Topic       string `json:"topic"`
	Description string `json:"description"`
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
func topicQueryRevision(w http.ResponseWriter, r *http.Request, cfg model.MessageTopicConfig) bool {
	n, err := strconv.ParseInt(r.URL.Query().Get("revision"), 10, 64)
	if err != nil {
		problem(w, 422, "请提交当前配置版本")
		return false
	}
	return topicRevision(w, &n, cfg)
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
		problem(w, 422, "请提交发布开关")
		return
	}
	route := model.MessageTopicRoute{ID: "topic_" + randomHex(8), Name: strings.TrimSpace(in.Name), SourceID: in.SourceID, Protocol: in.Protocol, Topic: strings.TrimSpace(in.Topic), Enabled: *in.Enabled, Description: strings.TrimSpace(in.Description)}
	if route.Protocol != "" {
		route.Topic = messagetopics.SharedDestination(claims(r).TenantID, route)
	}
	cfg.Topics = append(cfg.Topics, route)
	if err := messagetopics.Validate(claims(r).TenantID, cfg); err != nil {
		problem(w, 422, err.Error())
		return
	}
	if route.Protocol == "kafka" && !s.createBrokerMessageTopic(w, r, route.Topic) {
		return
	}
	s.saveMessageTopics(w, r, cfg, "message-topic.create", nil)
}
func (s *Server) updateMessageTopic(w http.ResponseWriter, r *http.Request) {
	var in messageTopicInput
	if decode(w, r, &in) != nil {
		return
	}
	cfg, ok := s.loadMessageTopics(w, r)
	if !ok || !editableTopic(w, r, cfg) || !topicRevision(w, in.Revision, cfg) {
		return
	}
	if in.Enabled == nil {
		problem(w, 422, "请提交发布开关")
		return
	}
	id := r.PathValue("id")
	for i := range cfg.Topics {
		if cfg.Topics[i].ID == id {
			old := cfg.Topics[i]
			if old.Protocol != "" {
				if in.Protocol != old.Protocol || messagetopics.SharedDestination(claims(r).TenantID, model.MessageTopicRoute{Protocol: in.Protocol, Topic: strings.TrimSpace(in.Topic)}) != old.Topic || in.SourceID != "" {
					problem(w, 422, "主题协议和地址创建后不可修改，请新建主题")
					return
				}
				old.Name, old.Enabled, old.Description = strings.TrimSpace(in.Name), *in.Enabled, strings.TrimSpace(in.Description)
				cfg.Topics[i] = old
			} else {
				if in.Protocol != "" {
					problem(w, 422, "旧转发主题不能直接变更为共享主题，请新建主题")
					return
				}
				cfg.Topics[i] = model.MessageTopicRoute{ID: id, Name: strings.TrimSpace(in.Name), SourceID: in.SourceID, Topic: strings.TrimSpace(in.Topic), Enabled: *in.Enabled, Description: strings.TrimSpace(in.Description)}
			}
			s.saveMessageTopics(w, r, cfg, "message-topic.update", nil)
			return
		}
	}
	if cfg.Overrides == nil {
		cfg.Overrides = map[string]model.MessageTopicOverride{}
	}
	cfg.Overrides[id] = model.MessageTopicOverride{Enabled: *in.Enabled, Topic: strings.TrimSpace(in.Topic), Description: strings.TrimSpace(in.Description)}
	s.saveMessageTopics(w, r, cfg, "message-topic.update", nil)
}
func (s *Server) deleteMessageTopic(w http.ResponseWriter, r *http.Request) {
	cfg, ok := s.loadMessageTopics(w, r)
	if !ok || !editableTopic(w, r, cfg) || !topicQueryRevision(w, r, cfg) {
		return
	}
	id := r.PathValue("id")
	custom := false
	cfg.Topics = slices.DeleteFunc(cfg.Topics, func(v model.MessageTopicRoute) bool {
		if v.ID == id {
			if v.Protocol != "" {
				cfg.RetiredTopics = append(cfg.RetiredTopics, v.Protocol+":"+messagetopics.SharedDestination(claims(r).TenantID, v))
			}
			custom = true
			return true
		}
		return false
	})
	if !custom {
		cfg.Deleted = append(cfg.Deleted, id)
		delete(cfg.Overrides, id)
	}
	for i := range cfg.Accounts {
		cfg.Accounts[i].TopicIDs = slices.DeleteFunc(cfg.Accounts[i].TopicIDs, func(v string) bool { return v == id })
		cfg.Accounts[i].PublishTopicIDs = slices.DeleteFunc(cfg.Accounts[i].PublishTopicIDs, func(v string) bool { return v == id })
	}
	cfg.Rules = slices.DeleteFunc(cfg.Rules, func(v model.MessageTopicRule) bool { return v.TopicID == id })
	s.saveMessageTopics(w, r, cfg, "message-topic.delete", nil)
}
func (s *Server) resetMessageTopic(w http.ResponseWriter, r *http.Request) {
	cfg, ok := s.loadMessageTopics(w, r)
	if !ok || !editableTopic(w, r, cfg) || !topicQueryRevision(w, r, cfg) {
		return
	}
	for _, v := range cfg.Topics {
		if v.ID == r.PathValue("id") {
			problem(w, 422, "新增主题没有默认配置")
			return
		}
	}
	delete(cfg.Overrides, r.PathValue("id"))
	s.saveMessageTopics(w, r, cfg, "message-topic.reset", nil)
}
func (s *Server) saveMessageTopics(w http.ResponseWriter, r *http.Request, cfg model.MessageTopicConfig, action string, extra map[string]any) {
	tenant := claims(r).TenantID
	previous, err := s.engine.MessageTopics.Load(r.Context(), tenant)
	if err != nil {
		problem(w, 500, "读取消息主题配置失败")
		return
	}
	if previous.Revision != cfg.Revision {
		problem(w, 409, "消息主题配置已被修改，请刷新后重试")
		return
	}
	messagetopics.ReserveOverrideTargets(&cfg, previous)
	// Any policy edit invalidates issued snapshots, including in-flight provisioning.
	for i := range cfg.Credentials {
		if cfg.Credentials[i].Status != "revoked" {
			cfg.Credentials[i].Status = "revoking"
		}
	}
	if err := messagetopics.Validate(tenant, cfg); err != nil {
		problem(w, 422, err.Error())
		return
	}
	saved, err := s.engine.MessageTopics.Save(r.Context(), tenant, cfg)
	if err != nil {
		problem(w, 500, "保存消息主题配置失败")
		return
	}
	if !saved {
		problem(w, 409, "消息主题配置已被修改，请刷新后重试")
		return
	}
	cfg.Revision++
	s.audit(r, action, "message-topic", r.PathValue("id"), map[string]any{"revision": cfg.Revision})
	if extra == nil {
		extra = map[string]any{}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	if err := s.reconcileMessageTopicTenant(ctx, tenant); err != nil {
		extra["warning"] = "配置已保存，旧凭据撤销仍在重试；新数据已停止向旧凭据发布。"
	}
	cancel()
	if latest, err := s.engine.MessageTopics.Load(r.Context(), tenant); err == nil {
		cfg = latest
	}
	s.messageTopicResponse(w, r, cfg, extra)
}
