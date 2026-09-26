// Package aioutput parses structured answers written by models. It is shared by
// the Harness business workflows, MCP tools and provider adapters so every path
// accepts the same shapes.
package aioutput

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"iot-platform/internal/model"
)

// RuleDraftInstructions describes the only rule JSON shape the platform
// accepts from a model; Harness workflows and tools share it.
const RuleDraftInstructions = `将自然语言告警要求转换成一个 JSON 规则草稿。只能返回 JSON，不要输出 Markdown。必须严格使用以下结构：{"name":"简短中文名称","description":"用中文说明这条规则的现场含义","alarmType":"SMOKE_DETECTED","level":"HIGH","match":"all","conditions":[{"field":"smoke","operator":"eq","value":true}],"durationSeconds":0,"recovery":[],"actions":[{"type":"OPEN_CAMERA","cameraId":"camera-001"}]}。description 必须说明触发条件和处置含义，帮助操作员复核；JSON 不要添加 _comment 或其他未定义字段。match 只能是字符串 all 或 any；conditions 和 recovery 必须是数组；每个条件只能包含 field、operator、value。actions 必须是数组，打开摄像头使用 {"type":"OPEN_CAMERA","cameraId":"用户指定的摄像头 ID"}，打开业务页面使用 {"type":"OPEN_PAGE","page":"alarms"}；没有动作要求时返回空数组，不得生成 URL、脚本或设备控制动作。alarmType 只能使用 FIRE_RISK、FIRE、SMOKE_DETECTED、FLAME_DETECTED、HIGH_TEMPERATURE、DEVICE_OFFLINE、WATER_PRESSURE_LOW、WATER_LEVEL_ABNORMAL、ELECTRICAL_FIRE、GAS_LEAK、MANUAL_ALARM；level 只能使用 CRITICAL、HIGH、MEDIUM、LOW、INFO。平台会根据 conditions 另外生成一份可选 Gengine 表达式，AI 草稿不要填写 expression，避免未经人工复核切换执行方式。`

// ExtractJSON returns the outermost JSON object inside a model answer.
func ExtractJSON(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return s
}

// DecodeAlarmAnalysis parses a model answer into an alarm analysis.
func DecodeAlarmAnalysis(content, alarmID, modelName string) (model.AIAnalysis, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(ExtractJSON(content)), &raw); err != nil {
		return model.AIAnalysis{}, fmt.Errorf("decode model json: %w", err)
	}
	if confidence, ok := raw["confidence"]; ok {
		raw["confidence"] = normalizeAIConfidence(confidence)
	}
	normalized, err := json.Marshal(raw)
	if err != nil {
		return model.AIAnalysis{}, fmt.Errorf("decode model json: %w", err)
	}
	var out model.AIAnalysis
	if err := json.Unmarshal(normalized, &out); err != nil {
		return out, fmt.Errorf("decode model json: %w", err)
	}
	if strings.TrimSpace(out.Summary) == "" {
		return out, fmt.Errorf("decode model json: summary is empty")
	}
	out.AlarmID, out.Model, out.PromptVersion, out.CreatedAt = alarmID, modelName, "alarm-diagnosis-v1", time.Now().UnixMilli()
	return out, nil
}

func normalizeAIConfidence(raw json.RawMessage) json.RawMessage {
	var number float64
	if err := json.Unmarshal(raw, &number); err == nil {
		return raw
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		text = strings.TrimSpace(text)
		if text != "" {
			if number, err = strconv.ParseFloat(text, 64); err == nil {
				normalized, marshalErr := json.Marshal(number)
				if marshalErr == nil {
					return normalized
				}
			}
		}
	}
	// Confidence is presentation metadata. If the model emits a label such
	// as “较高” instead of a number, keep the useful analysis and expose zero
	// confidence rather than discarding the complete result.
	return json.RawMessage("0")
}

// DecodeRuleDraft parses and normalises a model-written rule draft.
func DecodeRuleDraft(content string) (model.AlarmRule, error) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(ExtractJSON(content)), &raw); err != nil {
		return model.AlarmRule{}, err
	}
	raw["alarmType"] = normalizeAlarmType(stringValue(raw["alarmType"]))
	raw["level"] = strings.ToUpper(stringValue(raw["level"]))
	match := strings.ToLower(stringValue(raw["match"]))
	if match != "all" && match != "any" {
		match = "all"
	}
	raw["match"] = match
	raw["conditions"] = normalizeRuleConditions(raw["conditions"])
	raw["recovery"] = normalizeRuleConditions(raw["recovery"])
	raw["actions"] = normalizeRuleActions(raw["actions"])
	normalized, err := json.Marshal(raw)
	if err != nil {
		return model.AlarmRule{}, err
	}
	var rule model.AlarmRule
	if err = json.Unmarshal(normalized, &rule); err != nil {
		return rule, err
	}
	return rule, nil
}

func normalizeRuleActions(value any) []map[string]any {
	actions := []map[string]any{}
	items, ok := value.([]any)
	if !ok {
		if single, singleOK := value.(map[string]any); singleOK {
			items = []any{single}
		}
	}
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		actionType := strings.ToUpper(strings.TrimSpace(stringValue(object["type"])))
		switch actionType {
		case "OPEN_CAMERA":
			cameraID := strings.TrimSpace(stringValue(object["cameraId"]))
			if cameraID != "" {
				actions = append(actions, map[string]any{"type": actionType, "cameraId": cameraID})
			}
		case "OPEN_PAGE":
			page := strings.ToLower(strings.TrimSpace(stringValue(object["page"])))
			if page != "" {
				actions = append(actions, map[string]any{"type": actionType, "page": page})
			}
		}
	}
	return actions
}

func normalizeRuleConditions(value any) []map[string]any {
	conditions := []map[string]any{}
	appendMap := func(item map[string]any) {
		if field := strings.TrimSpace(stringValue(item["field"])); field != "" {
			operator := strings.TrimSpace(stringValue(item["operator"]))
			if operator == "" {
				operator = "eq"
			}
			conditions = append(conditions, map[string]any{"field": field, "operator": operator, "value": item["value"]})
			return
		}
		keys := make([]string, 0, len(item))
		for key := range item {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			conditions = append(conditions, map[string]any{"field": key, "operator": "eq", "value": item[key]})
		}
	}
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			if object, ok := item.(map[string]any); ok {
				appendMap(object)
			}
		}
	case map[string]any:
		appendMap(typed)
	}
	return conditions
}

func normalizeAlarmType(value string) string {
	normalized := strings.ToUpper(strings.NewReplacer("-", "_", " ", "_").Replace(strings.TrimSpace(value)))
	switch normalized {
	case "SMOKE", "SMOKE_ALARM", "SMOKE_DETECTOR":
		return "SMOKE_DETECTED"
	case "FLAME", "FLAME_ALARM":
		return "FLAME_DETECTED"
	case "HIGH_TEMP", "TEMPERATURE_HIGH":
		return "HIGH_TEMPERATURE"
	default:
		return normalized
	}
}

func stringValue(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}
