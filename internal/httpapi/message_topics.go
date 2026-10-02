package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"iot-platform/internal/messagetopics"
	"iot-platform/internal/model"
)

func (s *Server) messageTopicRoutes() {
	s.router.GET("/api/v1/message-topics", s.authorize("viewer"), s.endpoint(s.listMessageTopics))
	s.router.PUT("/api/v1/message-topics/:id", s.authorize("admin"), s.endpoint(s.updateMessageTopic, "id"))
	s.router.DELETE("/api/v1/message-topics/:id", s.authorize("admin"), s.endpoint(s.resetMessageTopic, "id"))
}

type messageTopicDefinition = messagetopics.Topic

type messageTopicView struct {
	messageTopicDefinition
	Topic            string `json:"topic"`
	Enabled          bool   `json:"enabled"`
	Description      string `json:"description"`
	Overridden       bool   `json:"overridden"`
	EffectiveEnabled bool   `json:"effectiveEnabled"`
}

func (s *Server) messageTopicResponse(w http.ResponseWriter, tenant string, cfg model.MessageTopicConfig) {
	mqttEnabled := strings.TrimSpace(s.cfg.MQTTBroker) != ""
	kafkaEnabled := len(s.cfg.KafkaBrokers) > 0
	items := make([]messageTopicView, 0)
	for _, entry := range messagetopics.Catalog() {
		item := messageTopicView{messageTopicDefinition: entry, Topic: entry.DefaultTopic, Enabled: true}
		if override, ok := cfg.Overrides[entry.ID]; ok && entry.Editable {
			item.Overridden = true
			item.Enabled, item.Description = override.Enabled, override.Description
			if override.Topic != "" {
				item.Topic = override.Topic
			}
		}
		item.EffectiveEnabled = item.Enabled && ((entry.Protocol == "mqtt" && mqttEnabled) || (entry.Protocol == "kafka" && kafkaEnabled))
		if entry.ID == "kafka.property-report" || entry.ID == "kafka.event-report" || entry.ID == "kafka.parsed" {
			item.EffectiveEnabled = item.EffectiveEnabled && s.engine.PublishExternalTopics
		}
		items = append(items, item)
	}
	write(w, 200, map[string]any{
		"revision": cfg.Revision, "items": items,
		"runtime":  map[string]bool{"mqttEnabled": mqttEnabled, "kafkaEnabled": kafkaEnabled, "kafkaParsedEnabled": s.engine.PublishExternalTopics},
		"prefixes": map[string]string{"mqtt": messagetopics.MQTTPrefix(tenant), "kafka": messagetopics.KafkaPrefix(tenant)},
	})
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
		s.messageTopicResponse(w, claims(r).TenantID, cfg)
	}
}

func (s *Server) editableMessageTopic(w http.ResponseWriter, r *http.Request) bool {
	if limited(r.Context()) {
		problem(w, 403, "配置租户消息发布需要全部设备范围及设备管理菜单权限")
		return false
	}
	for _, item := range messagetopics.Catalog() {
		if item.ID == r.PathValue("id") {
			if !item.Editable {
				problem(w, 422, "此主题由平台内部或设备协议使用，不能修改")
			}
			return item.Editable
		}
	}
	problem(w, 404, "消息主题不存在")
	return false
}

func (s *Server) updateMessageTopic(w http.ResponseWriter, r *http.Request) {
	if !s.editableMessageTopic(w, r) {
		return
	}
	var input struct {
		Revision    *int64 `json:"revision"`
		Enabled     *bool  `json:"enabled"`
		Topic       string `json:"topic"`
		Description string `json:"description"`
	}
	if decode(w, r, &input) != nil {
		return
	}
	if input.Revision == nil || *input.Revision < 0 || input.Enabled == nil {
		problem(w, 422, "请提交当前配置版本及发布开关")
		return
	}
	cfg, ok := s.loadMessageTopics(w, r)
	if !ok {
		return
	}
	if cfg.Revision != *input.Revision {
		problem(w, 409, "消息主题配置已被修改，请刷新后重试")
		return
	}
	if cfg.Overrides == nil {
		cfg.Overrides = map[string]model.MessageTopicOverride{}
	}
	cfg.Overrides[r.PathValue("id")] = model.MessageTopicOverride{Enabled: *input.Enabled, Topic: strings.TrimSpace(input.Topic), Description: strings.TrimSpace(input.Description)}
	s.saveMessageTopics(w, r, cfg, "message-topic.update")
}

func (s *Server) resetMessageTopic(w http.ResponseWriter, r *http.Request) {
	if !s.editableMessageTopic(w, r) {
		return
	}
	revision, err := strconv.ParseInt(r.URL.Query().Get("revision"), 10, 64)
	if err != nil || revision < 0 {
		problem(w, 422, "请提交当前配置版本")
		return
	}
	cfg, ok := s.loadMessageTopics(w, r)
	if !ok {
		return
	}
	if cfg.Revision != revision {
		problem(w, 409, "消息主题配置已被修改，请刷新后重试")
		return
	}
	delete(cfg.Overrides, r.PathValue("id"))
	s.saveMessageTopics(w, r, cfg, "message-topic.reset")
}

func (s *Server) saveMessageTopics(w http.ResponseWriter, r *http.Request, cfg model.MessageTopicConfig, action string) {
	tenant := claims(r).TenantID
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
	s.audit(r, action, "message-topic", r.PathValue("id"), map[string]any{"revision": cfg.Revision, "config": cfg.Overrides[r.PathValue("id")]})
	s.messageTopicResponse(w, tenant, cfg)
}
