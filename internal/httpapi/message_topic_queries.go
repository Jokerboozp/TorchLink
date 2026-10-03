package httpapi

import (
	"context"
	"encoding/json"
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

type messageTopicQueryOptions struct {
	Mode            *string   `json:"mode"`
	IntervalSeconds *int      `json:"intervalSeconds"`
	DeviceScope     *string   `json:"deviceScope"`
	DeviceIDs       *[]string `json:"deviceIds"`
}

type messageTopicQueryInput struct {
	Query        *model.MessageTopicQuery  `json:"query"`
	QuerySQL     *string                   `json:"querySql"`
	QueryOptions *messageTopicQueryOptions `json:"queryOptions"`
}

func (in messageTopicQueryInput) canonicalQuery() (*model.MessageTopicQuery, error) {
	if in.Query != nil && in.QuerySQL != nil {
		return nil, errors.New("查询表单与 SQL 只能提交一种")
	}
	if in.QueryOptions != nil && in.QuerySQL == nil {
		return nil, errors.New("queryOptions 仅适用于 SQL 查询")
	}
	if in.Query == nil && in.QuerySQL == nil {
		return nil, nil
	}
	var query model.MessageTopicQuery
	if in.QuerySQL != nil {
		compiled, err := messagetopics.CompileQuerySQL(*in.QuerySQL)
		if err != nil {
			return nil, err
		}
		query = compiled
		if options := in.QueryOptions; options != nil {
			if options.Mode != nil {
				query.Mode = *options.Mode
			}
			if options.IntervalSeconds != nil {
				query.IntervalSeconds = *options.IntervalSeconds
			}
			if options.DeviceScope != nil {
				query.DeviceScope = *options.DeviceScope
			}
			if options.DeviceIDs != nil {
				query.DeviceIDs = *options.DeviceIDs
			}
		}
	} else {
		query = *in.Query
	}
	query.Dataset = strings.TrimSpace(query.Dataset)
	if query.DeviceScope == "" {
		query.DeviceScope = "all"
	}
	if query.Mode == "" {
		for _, dataset := range messagetopics.QueryDatasets() {
			if dataset.ID == query.Dataset {
				query.Mode = dataset.Mode
				break
			}
		}
	}
	if query.Mode == "interval" && query.IntervalSeconds == 0 {
		query.IntervalSeconds = 60
	}
	if err := messagetopics.ValidateQuery(query); err != nil {
		return nil, err
	}
	return &query, nil
}

func (s *Server) validateMessageTopicQuery(w http.ResponseWriter, r *http.Request, protocol string, query model.MessageTopicQuery) bool {
	if !messageTopicWriteAllowed(w, r) {
		return false
	}
	if protocol != "mqtt" && protocol != "kafka" {
		problem(w, 422, "数据查询仅支持 MQTT 或 Kafka 主题")
		return false
	}
	if err := messagetopics.ValidateQuery(query); err != nil {
		problem(w, 422, err.Error())
		return false
	}
	policy := model.MessageTopicConfig{Topics: []model.MessageTopicRoute{{ID: "preview", Protocol: protocol, Enabled: true, Query: &query}}}
	if err := messagetopics.AccumulateQueryExposure(&policy, "preview", query); err != nil {
		problem(w, 422, err.Error())
		return false
	}
	if permissions, managed := r.Context().Value(permissionsKey{}).(map[string]bool); managed {
		identity := messagetopics.MessageTopicIdentity{Permissions: permissions, DeviceScope: "all"}
		if !messagetopics.RouteAllowed(policy, "preview", identity, model.MessageTopicAccount{Enabled: true, DeviceScope: "all"}) {
			problem(w, 403, "没有所选查询数据的完整业务访问权限")
			return false
		}
	}
	if len(query.DeviceIDs) > 0 {
		_, total, err := s.unscopedRepo().ListManagedDevicesFiltered(r.Context(), ports.DeviceFilter{TenantID: claims(r).TenantID, RestrictDevices: true, DeviceIDs: query.DeviceIDs}, 1, 0)
		if err != nil {
			problem(w, 503, "校验查询设备范围失败")
			return false
		}
		if total != len(query.DeviceIDs) {
			problem(w, 422, "查询包含不存在或不属于当前租户的设备")
			return false
		}
	}
	return true
}

func (s *Server) configureMessageTopicQuery(w http.ResponseWriter, r *http.Request, cfg *model.MessageTopicConfig, topicID string, in messageTopicInput) bool {
	query, err := in.messageTopicQueryInput.canonicalQuery()
	if err != nil {
		problem(w, 422, err.Error())
		return false
	}
	for i := range cfg.Topics {
		topic := &cfg.Topics[i]
		if topic.ID != topicID {
			continue
		}
		if query != nil {
			if topic.Protocol == "" {
				problem(w, 422, "旧转发主题不能直接配置数据查询，请新建对外主题")
				return false
			}
			if !s.validateMessageTopicQuery(w, r, topic.Protocol, *query) {
				return false
			}
			for _, account := range cfg.Accounts {
				if slices.Contains(account.PublishTopicIDs, topicID) {
					problem(w, 422, "此主题仍有外部发布授权，请先解除发布授权再改为查询主题")
					return false
				}
			}
			for _, rule := range cfg.Rules {
				if rule.TopicID == topicID && !in.ReplaceLegacyRules {
					problem(w, 422, "此主题仍有独立发送规则，请明确确认用数据查询替换原规则")
					return false
				}
			}
			cfg.Rules = slices.DeleteFunc(cfg.Rules, func(rule model.MessageTopicRule) bool { return rule.TopicID == topicID })
			topic.Query = query
			if err := messagetopics.AccumulateQueryExposure(cfg, topicID, *query); err != nil {
				problem(w, 422, err.Error())
				return false
			}
		}
		return s.configureMessageTopicSubscribers(w, r, cfg, topicID, in.AccountIDs)
	}
	problem(w, 404, "消息主题不存在")
	return false
}

func (s *Server) configureMessageTopicSubscribers(w http.ResponseWriter, r *http.Request, cfg *model.MessageTopicConfig, topicID string, accountIDs *[]string) bool {
	if accountIDs == nil {
		return true
	}
	if !requestAllows(r, "PUT", "/api/v1/message-topic-accounts/:id") {
		problem(w, 403, "修改主题订阅账号需要对接账号授权管理权限")
		return false
	}
	selected := make(map[string]bool, len(*accountIDs))
	for _, id := range *accountIDs {
		if id == "" || selected[id] {
			problem(w, 422, "订阅账号列表包含空值或重复账号")
			return false
		}
		selected[id] = true
	}
	policy := *cfg
	policy.Topics = slices.Clone(cfg.Topics)
	for i := range policy.Topics {
		if policy.Topics[i].ID == topicID {
			policy.Topics[i].Enabled = true
		}
	}
	for i := range cfg.Accounts {
		account := &cfg.Accounts[i]
		if !selected[account.ID] {
			account.TopicIDs = slices.DeleteFunc(account.TopicIDs, func(id string) bool { return id == topicID })
			continue
		}
		delete(selected, account.ID)
		identity, err := s.messageTopicIdentity(r.Context(), claims(r).TenantID, account.Username)
		if err != nil || (account.ExpiresAt > 0 && account.ExpiresAt <= time.Now().Unix()) || !messagetopics.RouteAllowed(policy, topicID, identity, *account) {
			problem(w, 422, fmt.Sprintf("对接账号“%s”无法订阅此主题，请检查账号状态、绑定用户权限及主题历史设备范围", account.Name))
			return false
		}
		if !slices.Contains(account.TopicIDs, topicID) {
			account.TopicIDs = append(account.TopicIDs, topicID)
		}
	}
	if len(selected) != 0 {
		problem(w, 422, "所选对接账号不存在或不属于当前租户")
		return false
	}
	return true
}

func (s *Server) previewMessageTopicQuery(w http.ResponseWriter, r *http.Request) {
	var in struct {
		messageTopicQueryInput
		Protocol string  `json:"protocol"`
		Payload  *string `json:"payload"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	query, err := in.messageTopicQueryInput.canonicalQuery()
	if err != nil {
		problem(w, 422, err.Error())
		return
	}
	if query == nil {
		problem(w, 422, "请配置数据查询")
		return
	}
	if in.Protocol == "" {
		in.Protocol = "mqtt"
	}
	if !s.validateMessageTopicQuery(w, r, in.Protocol, *query) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	result := map[string]any{"query": query, "querySql": messagetopics.QuerySQL(*query), "matched": false, "sampled": false, "payload": ""}
	if in.Payload == nil || strings.TrimSpace(*in.Payload) == "" {
		if s.engine.MessageTopics == nil {
			problem(w, 503, "消息主题管理尚未初始化")
			return
		}
		if query.Mode == "interval" {
			payload, err := s.engine.MessageTopics.Snapshot(ctx, claims(r).TenantID, *query)
			if err != nil {
				problem(w, 422, "预览查询失败："+err.Error())
				return
			}
			var snapshot struct {
				Items []json.RawMessage `json:"items"`
			}
			if err := json.Unmarshal(payload, &snapshot); err != nil {
				problem(w, 500, "读取查询预览结果失败")
				return
			}
			result["payload"], result["sampled"], result["matched"] = string(payload), true, len(snapshot.Items) > 0
			write(w, 200, result)
			return
		}
		payloads, err := s.engine.MessageTopics.QuerySamples(ctx, claims(r).TenantID, *query, 50)
		if err != nil {
			problem(w, 503, "读取查询样本失败")
			return
		}
		result["sampled"] = len(payloads) > 0
		for _, payload := range payloads {
			value, matched, err := messagetopics.PreviewQuery(*query, payload)
			if err != nil {
				problem(w, 422, err.Error())
				return
			}
			if matched {
				result["matched"], result["payload"] = true, string(value)
				break
			}
		}
	} else {
		payload, matched, err := messagetopics.PreviewQuery(*query, []byte(*in.Payload))
		if err != nil {
			problem(w, 422, err.Error())
			return
		}
		result["matched"], result["sampled"], result["payload"] = matched, true, string(payload)
	}
	write(w, 200, result)
}
