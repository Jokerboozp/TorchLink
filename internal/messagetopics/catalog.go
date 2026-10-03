// Package messagetopics manages the platform's external publication routes.
// Internal streams and device transport contracts are catalogued but immutable.
package messagetopics

import (
	"encoding/hex"
	"strings"

	"iot-platform/internal/model"
)

type Topic struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Protocol     string   `json:"protocol"`
	Direction    string   `json:"direction"`
	DefaultTopic string   `json:"defaultTopic"`
	Editable     bool     `json:"editable"`
	Reason       string   `json:"reason"`
	Variables    []string `json:"variables"`
}

// Prefixes encode the complete tenant identity; separators in tenant IDs cannot
// escape the namespace or make two different tenant IDs share one destination.
func KafkaPrefix(tenant string) string {
	return "iot.external." + hex.EncodeToString([]byte(tenant)) + "."
}
func MQTTPrefix(tenant string) string {
	return "/iot/external/" + hex.EncodeToString([]byte(tenant)) + "/"
}

var topics = buildCatalog()

func buildCatalog() []Topic {
	var result []Topic
	add := func(id, name, protocol, direction, topic string, editable bool, reason string, variables ...string) {
		result = append(result, Topic{ID: id, Name: name, Protocol: protocol, Direction: direction, DefaultTopic: topic, Editable: editable, Reason: reason, Variables: append([]string{}, variables...)})
	}
	for _, v := range []struct{ id, name, topic string }{
		{"property-report", "属性上报", model.TopicPropertyReport},
		{"event-report", "事件与设备告警上报", model.TopicEventReport},
		{"parsed", "其他解析结果", model.TopicParsed},
		{"alarm-raised", "告警触发", model.TopicAlarmRaised},
		{"alarm-recovered", "告警恢复", model.TopicAlarmRecovered},
		{"alarm-confirmed", "告警确认", model.TopicAlarmConfirmed},
		{"alarm-ai-analysis", "告警研判结果", model.TopicAlarmAIAnalysis},
		{"ui-action", "规则界面通知", model.TopicUIAction},
	} {
		add("kafka."+v.id, v.name, "kafka", "outbound", v.topic, true, "默认主题由多租户共享；自定义主题使用本租户专属前缀。")
	}
	for _, v := range []struct{ id, name, topic string }{
		{"raw", "原始报文", model.TopicRaw},
		{"business", "设备业务处理", model.TopicDeviceBusiness},
		{"state", "设备状态处理", model.TopicDeviceState},
		{"alarm-reported", "设备告警通知处理", model.TopicAlarmReported},
		{"replay", "原文回放请求", model.TopicReplayRequest},
		{"parse-failed", "解析失败保留主题", model.TopicParseFailed},
	} {
		add("kafka."+v.id, v.name, "kafka", "internal", v.topic, false, "平台内部处理契约，不允许修改或停用。")
	}
	for _, group := range model.ConsumerGroups {
		add("kafka.dlq-"+group, "失败消息保留（"+group+"）", "kafka", "internal", model.DLQTopic(group), false, "失败消息恢复使用的内部主题，不允许修改或停用。")
	}
	add("mqtt.parsed", "成功解析的设备数据", "mqtt", "outbound", "/iot/parsed/{tenantId}/{productId}/{deviceId}/{messageType}", true, "仅成功解析的数据会发布。", "tenantId", "productId", "deviceId", "messageType")
	for _, event := range []struct{ id, name string }{{"raised", "告警触发"}, {"recovered", "告警恢复"}, {"confirmed", "告警确认"}, {"ai-analysis", "告警研判结果"}} {
		vars := []string{"tenantId", "alarmId"}
		if event.id != "ai-analysis" {
			vars = append(vars, "deviceId")
		}
		add("mqtt.alarm-"+event.id, event.name, "mqtt", "outbound", "/iot/alarm/"+event.id+"/{cityCode}/{districtCode}/{buildingId}/{deviceType}/{deviceId}", true, "默认历史路径不含租户；自定义主题使用本租户专属前缀。", vars...)
	}
	add("mqtt.ui-action", "规则界面通知", "mqtt", "outbound", "/iot/ui-action/{tenantId}", true, "规则产生的界面通知。", "tenantId", "deviceId", "alarmId")
	for _, kind := range []struct{ id, name string }{{"property", "设备属性上报"}, {"event", "设备事件上报"}, {"alarm", "设备告警上报"}, {"state", "设备状态上报"}, {"command-reply", "设备命令回执"}} {
		add("mqtt.up-"+kind.id, kind.name, "mqtt", "inbound", "/iot/up/{tenantId}/{productId}/{deviceId}/"+kind.id, false, "设备凭据与标准上报协议绑定，不允许修改或停用。")
	}
	add("mqtt.raw", "原始报文接入", "mqtt", "inbound", "/external/raw/{tenantId}/{productId}/{deviceId}", false, "原始归档接入契约，不允许修改或停用。")
	add("mqtt.legacy-raw", "历史原始报文接入", "mqtt", "inbound", "/jetlinks/raw/{tenantId}/{productId}/{deviceId}", false, "现有历史接入路径，不允许修改或停用。")
	add("mqtt.state", "设备状态快照", "mqtt", "internal", "/iot/device/state/{tenantId}/{productId}/{deviceId}", false, "该 retained 状态主题同时属于平台状态链路，不允许修改或停用。")
	add("mqtt.command", "设备命令下发", "mqtt", "outbound", "/iot/down/{tenantId}/{productId}/{deviceId}/command", false, "设备命令与凭据授权绑定，不允许修改或停用。")
	add("mqtt.receipt", "原文归档确认", "mqtt", "outbound", "/iot/down/{tenantId}/{productId}/{deviceId}/receipt", false, "设备依赖此回执确认持久归档，不允许修改或停用。")
	add("mqtt.legacy-command", "历史设备命令订阅", "mqtt", "outbound", "/iot/device/command/{tenantId}/{deviceId}", false, "已有设备凭据授权保留的历史路径，不允许修改或停用。")
	return result
}

func Catalog() []Topic {
	out := append([]Topic(nil), topics...)
	for i := range out {
		out[i].Variables = append([]string{}, out[i].Variables...)
	}
	return out
}

func topicByID(id string) (Topic, bool) {
	for _, topic := range topics {
		if topic.ID == id {
			return topic, true
		}
	}
	return Topic{}, false
}

func externalTopic(protocol, source string) (Topic, bool) {
	if protocol == "kafka" {
		for _, topic := range topics {
			if topic.Protocol == protocol && topic.Editable && topic.DefaultTopic == source {
				return topic, true
			}
		}
		return Topic{}, false
	}
	if protocol != "mqtt" {
		return Topic{}, false
	}
	id := ""
	switch {
	case strings.HasPrefix(source, "/iot/parsed/"):
		id = "mqtt.parsed"
	case strings.HasPrefix(source, "/iot/ui-action/"):
		id = "mqtt.ui-action"
	default:
		for _, event := range []string{"raised", "recovered", "confirmed", "ai-analysis"} {
			if strings.HasPrefix(source, "/iot/alarm/"+event+"/") {
				id = "mqtt.alarm-" + event
				break
			}
		}
	}
	return topicByID(id)
}
