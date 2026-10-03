package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	kafkaadapter "iot-platform/internal/adapters/kafka"
	"iot-platform/internal/messagetopics"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func (s *Server) createBrokerMessageTopic(w http.ResponseWriter, r *http.Request, topic string) bool {
	admin, ok := s.messageTopicKafka.(messageTopicKafkaTopicAdmin)
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

func sharedMessageTopic(w http.ResponseWriter, r *http.Request, cfg model.MessageTopicConfig) (model.MessageTopicRoute, bool) {
	if !messageTopicWriteAllowed(w, r) {
		return model.MessageTopicRoute{}, false
	}
	for _, topic := range cfg.Topics {
		if topic.ID == r.PathValue("id") && topic.Protocol != "" {
			return topic, true
		}
	}
	problem(w, 422, "此操作仅适用于新建的共享消息主题")
	return model.MessageTopicRoute{}, false
}

type messageTopicRuleInput struct {
	Revision *int64 `json:"revision"`
	model.MessageTopicRule
}

func (s *Server) validateMessageTopicRule(w http.ResponseWriter, r *http.Request, cfg *model.MessageTopicConfig, rule model.MessageTopicRule) bool {
	permissions, managed := r.Context().Value(permissionsKey{}).(map[string]bool)
	if managed && !messagetopics.SourceAllowed(rule.SourceID, messagetopics.MessageTopicIdentity{Permissions: permissions, DeviceScope: "all"}) {
		problem(w, 403, "没有所选自动发送事件的数据访问权限")
		return false
	}
	if err := messagetopics.AccumulateExposure(cfg, rule); err != nil {
		problem(w, 422, err.Error())
		return false
	}
	if managed {
		for _, topic := range cfg.Topics {
			if topic.ID != rule.TopicID {
				continue
			}
			topic.Enabled = true
			topic.Exposure = []model.MessageTopicExposure{{SourceID: rule.SourceID, DeviceScope: rule.DeviceScope, DeviceIDs: rule.DeviceIDs}}
			policy := model.MessageTopicConfig{Topics: []model.MessageTopicRoute{topic}}
			identity := messagetopics.MessageTopicIdentity{Permissions: permissions, DeviceScope: "all"}
			if !messagetopics.RouteAllowed(policy, topic.ID, identity, model.MessageTopicAccount{Enabled: true, DeviceScope: "all"}) {
				problem(w, 403, "没有所选事件所需的完整业务数据权限")
				return false
			}
		}
	}
	// Existing credentials are revoked by saveMessageTopics. Validate the new
	// policy without requiring their superseded snapshots to fit the new rules.
	validation := *cfg
	validation.Credentials = nil
	if err := messagetopics.Validate(claims(r).TenantID, validation); err != nil {
		problem(w, 422, err.Error())
		return false
	}
	if len(rule.DeviceIDs) > 0 {
		_, total, err := s.unscopedRepo().ListManagedDevicesFiltered(r.Context(), ports.DeviceFilter{TenantID: claims(r).TenantID, RestrictDevices: true, DeviceIDs: rule.DeviceIDs}, 1, 0)
		if err != nil {
			problem(w, 503, "校验规则设备范围失败")
			return false
		}
		if total != len(rule.DeviceIDs) {
			problem(w, 422, "规则包含不存在或不属于当前租户的设备")
			return false
		}
	}
	return true
}

func (s *Server) createMessageTopicRule(w http.ResponseWriter, r *http.Request) {
	var in messageTopicRuleInput
	if decode(w, r, &in) != nil {
		return
	}
	cfg, ok := s.loadMessageTopics(w, r)
	if !ok || !topicRevision(w, in.Revision, cfg) {
		return
	}
	topic, ok := sharedMessageTopic(w, r, cfg)
	if !ok {
		return
	}
	rule := in.MessageTopicRule
	rule.ID, rule.TopicID, rule.Name = "rule_"+randomHex(8), topic.ID, strings.TrimSpace(rule.Name)
	cfg.Rules = append(cfg.Rules, rule)
	if s.validateMessageTopicRule(w, r, &cfg, rule) {
		s.saveMessageTopics(w, r, cfg, "message-topic-rule.create", nil)
	}
}

func (s *Server) updateMessageTopicRule(w http.ResponseWriter, r *http.Request) {
	var in messageTopicRuleInput
	if decode(w, r, &in) != nil {
		return
	}
	cfg, ok := s.loadMessageTopics(w, r)
	if !ok || !topicRevision(w, in.Revision, cfg) {
		return
	}
	topic, ok := sharedMessageTopic(w, r, cfg)
	if !ok {
		return
	}
	for i, old := range cfg.Rules {
		if old.ID == r.PathValue("ruleId") && old.TopicID == topic.ID {
			rule := in.MessageTopicRule
			rule.ID, rule.TopicID, rule.Name = old.ID, topic.ID, strings.TrimSpace(rule.Name)
			cfg.Rules[i] = rule
			if s.validateMessageTopicRule(w, r, &cfg, rule) {
				s.saveMessageTopics(w, r, cfg, "message-topic-rule.update", nil)
			}
			return
		}
	}
	problem(w, 404, "自动发送规则不存在")
}

func (s *Server) deleteMessageTopicRule(w http.ResponseWriter, r *http.Request) {
	cfg, ok := s.loadMessageTopics(w, r)
	if !ok || !topicQueryRevision(w, r, cfg) {
		return
	}
	topic, ok := sharedMessageTopic(w, r, cfg)
	if !ok {
		return
	}
	before := len(cfg.Rules)
	cfg.Rules = slices.DeleteFunc(cfg.Rules, func(rule model.MessageTopicRule) bool {
		return rule.ID == r.PathValue("ruleId") && rule.TopicID == topic.ID
	})
	if before == len(cfg.Rules) {
		problem(w, 404, "自动发送规则不存在")
		return
	}
	s.saveMessageTopics(w, r, cfg, "message-topic-rule.delete", nil)
}

func (s *Server) previewMessageTopicRule(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Rule    model.MessageTopicRule `json:"rule"`
		Payload string                 `json:"payload"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	cfg, ok := s.loadMessageTopics(w, r)
	if !ok {
		return
	}
	topic, ok := sharedMessageTopic(w, r, cfg)
	if !ok {
		return
	}
	in.Rule.ID, in.Rule.TopicID = "preview_"+randomHex(8), topic.ID
	in.Rule.Name = strings.TrimSpace(in.Rule.Name)
	if in.Rule.Name == "" {
		in.Rule.Name = "消息预览"
	}
	cfg.Rules = []model.MessageTopicRule{in.Rule}
	if !s.validateMessageTopicRule(w, r, &cfg, in.Rule) {
		return
	}
	result, err := messagetopics.PreviewRule(in.Rule, []byte(in.Payload))
	if err != nil {
		problem(w, 422, err.Error())
		return
	}
	write(w, 200, map[string]any{"payload": string(result)})
}

func (s *Server) publishMessageTopic(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Revision *int64 `json:"revision"`
		Payload  string `json:"payload"`
		Format   string `json:"format"`
		Key      string `json:"key"`
		QoS      int    `json:"qos"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	cfg, ok := s.loadMessageTopics(w, r)
	if !ok || !topicRevision(w, in.Revision, cfg) {
		return
	}
	topic, ok := sharedMessageTopic(w, r, cfg)
	if !ok {
		return
	}
	if !topic.Enabled {
		problem(w, 422, "主题已停用")
		return
	}
	if len(in.Payload) > 256<<10 || len(in.Key) > 1024 || in.QoS < 0 || in.QoS > 2 || (in.Format != "text" && in.Format != "json") || (in.Format == "json" && !json.Valid([]byte(in.Payload))) {
		problem(w, 422, "请检查消息格式、256 KiB 大小限制、消息键或 MQTT QoS")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	var err error
	if topic.Protocol == "kafka" {
		if len(s.cfg.KafkaBrokers) == 0 || s.engine.Bus == nil {
			problem(w, 503, "Kafka 通道未启用")
			return
		}
		err = s.engine.Bus.Publish(ctx, topic.Topic, in.Key, []byte(in.Payload))
	} else {
		if s.cfg.MQTTBroker == "" || s.engine.Realtime == nil {
			problem(w, 503, "MQTT 通道未启用")
			return
		}
		err = s.engine.Realtime.Publish(ctx, topic.Topic, []byte(in.Payload), byte(in.QoS), false)
	}
	if err != nil {
		problem(w, 502, "发送未获得成功确认，请检查 Broker；消息可能已经送达，请勿自动重试")
		return
	}
	s.audit(r, "message-topic.publish", "message-topic", topic.ID, map[string]any{"protocol": topic.Protocol, "bytes": len(in.Payload)})
	write(w, 200, map[string]any{"published": true, "topic": topic.Topic, "bytes": len(in.Payload)})
}
