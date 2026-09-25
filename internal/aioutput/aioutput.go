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
const RuleDraftInstructions = `将自然语言告警要求转换成一个 JSON 规则草稿。只能返回 JSON，不要输出 Markdown。必须严格使用以下结构：{"name":"简短中文名称","description":"用中文说明这条规则的现场含义","alarmType":"SMOKE_DETECTED","level":"HIGH","match":"all","conditions":[{"field":"smoke","operator":"eq","value":true}],"durationSeconds":0,"recovery":[],"actions":[{"type":"OPEN_CAMERA","cameraId":"camera-001"}]}。description 必须说明触发条件和处置含义，帮助操作员复核；JSON 不要添加 _comment 或其他未定义字段。match 只能是字符串 all 或 any；conditions 和 recovery 必须是数组；每个条件只能包含 field、operator、value。actions 必须是数组，打开摄像头使用 {"type":"OPEN_CAMERA","cameraId":"用户指定的摄像头 ID"}，打开业务页面使用 {"type":"OPEN_PAGE","page":"alarms"}；没有动作要求时返回空数组，不得生成 URL、脚本或设备控制动作。alarmType 只能使用 FIRE_RISK、FIRE、SMOKE_DETECTED、FLAME_DETECTED、HIGH_TEMPERATURE、DEVICE_OFFLINE、WATER_PRESSURE_LOW、WATER_LEVEL_ABNORMAL、ELECTRICAL_FIRE、GAS_LEAK、MANUAL_ALARM；level 只能使用 CRITICAL、HIGH、MEDIUM、LOW、INFO。平台会根据 conditions 另外生成一份可选 Gengine 表达式，AI 草稿不要填写 expression，避免未经人工复核切换执行方式。` /* 声明 ruleDraftSystemPrompt。 */

// ExtractJSON returns the outermost JSON object inside a model answer.
func ExtractJSON(s string) string { /* 定义 extractJSON 函数。 */
	start := strings.Index(s, "{")   /* 更新 start 的值。 */
	end := strings.LastIndex(s, "}") /* 更新 end 的值。 */
	if start >= 0 && end > start {   /* 判断条件并选择处理分支。 */
		return s[start : end+1] /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return s /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// DecodeAlarmAnalysis parses a model answer into an alarm analysis.
func DecodeAlarmAnalysis(content, alarmID, modelName string) (model.AIAnalysis, error) { /* 定义 decodeAIAnalysis 函数。 */
	var raw map[string]json.RawMessage                                         /* 声明 raw。 */
	if err := json.Unmarshal([]byte(ExtractJSON(content)), &raw); err != nil { /* 判断条件并选择处理分支。 */
		return model.AIAnalysis{}, fmt.Errorf("decode model json: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if confidence, ok := raw["confidence"]; ok { /* 判断条件并选择处理分支。 */
		raw["confidence"] = normalizeAIConfidence(confidence) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	normalized, err := json.Marshal(raw) /* 更新 err 的值。 */
	if err != nil {                      /* 判断条件并选择处理分支。 */
		return model.AIAnalysis{}, fmt.Errorf("decode model json: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var out model.AIAnalysis                                 /* 声明 out。 */
	if err := json.Unmarshal(normalized, &out); err != nil { /* 判断条件并选择处理分支。 */
		return out, fmt.Errorf("decode model json: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if strings.TrimSpace(out.Summary) == "" { /* 判断条件并选择处理分支。 */
		return out, fmt.Errorf("decode model json: summary is empty") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	out.AlarmID, out.Model, out.PromptVersion, out.CreatedAt = alarmID, modelName, "alarm-diagnosis-v1", time.Now().UnixMilli() /* 更新 out.CreatedAt 的值。 */
	return out, nil                                                                                                             /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func normalizeAIConfidence(raw json.RawMessage) json.RawMessage { /* 定义 normalizeAIConfidence 函数。 */
	var number float64                                   /* 声明 number。 */
	if err := json.Unmarshal(raw, &number); err == nil { /* 判断条件并选择处理分支。 */
		return raw /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var text string                                    /* 声明 text。 */
	if err := json.Unmarshal(raw, &text); err == nil { /* 判断条件并选择处理分支。 */
		text = strings.TrimSpace(text) /* 更新 text 的值。 */
		if text != "" {                /* 判断条件并选择处理分支。 */
			if number, err = strconv.ParseFloat(text, 64); err == nil { /* 判断条件并选择处理分支。 */
				normalized, marshalErr := json.Marshal(number) /* 更新 marshalErr 的值。 */
				if marshalErr == nil {                         /* 判断条件并选择处理分支。 */
					return normalized /* 返回当前处理结果。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	// Confidence is presentation metadata. If the model emits a label such
	// as “较高” instead of a number, keep the useful analysis and expose zero
	// confidence rather than discarding the complete result.
	return json.RawMessage("0") /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// DecodeRuleDraft parses and normalises a model-written rule draft.
func DecodeRuleDraft(content string) (model.AlarmRule, error) { /* 定义 decodeRuleDraft 函数。 */
	var raw map[string]any                                                     /* 声明 raw。 */
	if err := json.Unmarshal([]byte(ExtractJSON(content)), &raw); err != nil { /* 判断条件并选择处理分支。 */
		return model.AlarmRule{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	raw["alarmType"] = normalizeAlarmType(stringValue(raw["alarmType"])) /* 执行当前语句并推进处理流程。 */
	raw["level"] = strings.ToUpper(stringValue(raw["level"]))            /* 执行当前语句并推进处理流程。 */
	match := strings.ToLower(stringValue(raw["match"]))                  /* 更新 match 的值。 */
	if match != "all" && match != "any" {                                /* 判断条件并选择处理分支。 */
		match = "all" /* 更新 match 的值。 */
	} /* 结束当前表达式或代码块。 */
	raw["match"] = match                                           /* 执行当前语句并推进处理流程。 */
	raw["conditions"] = normalizeRuleConditions(raw["conditions"]) /* 执行当前语句并推进处理流程。 */
	raw["recovery"] = normalizeRuleConditions(raw["recovery"])     /* 执行当前语句并推进处理流程。 */
	raw["actions"] = normalizeRuleActions(raw["actions"])          /* 执行当前语句并推进处理流程。 */
	normalized, err := json.Marshal(raw)                           /* 更新 err 的值。 */
	if err != nil {                                                /* 判断条件并选择处理分支。 */
		return model.AlarmRule{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var rule model.AlarmRule                                 /* 声明 rule。 */
	if err = json.Unmarshal(normalized, &rule); err != nil { /* 判断条件并选择处理分支。 */
		return rule, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return rule, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func normalizeRuleActions(value any) []map[string]any { /* 定义 normalizeRuleActions 函数。 */
	actions := []map[string]any{} /* 更新 actions 的值。 */
	items, ok := value.([]any)    /* 更新 ok 的值。 */
	if !ok {                      /* 判断条件并选择处理分支。 */
		if single, singleOK := value.(map[string]any); singleOK { /* 判断条件并选择处理分支。 */
			items = []any{single} /* 更新 items 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	for _, item := range items { /* 循环处理当前数据。 */
		object, ok := item.(map[string]any) /* 更新 ok 的值。 */
		if !ok {                            /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		actionType := strings.ToUpper(strings.TrimSpace(stringValue(object["type"]))) /* 更新 actionType 的值。 */
		switch actionType {                                                           /* 根据条件选择处理路径。 */
		case "OPEN_CAMERA": /* 处理当前分支。 */
			cameraID := strings.TrimSpace(stringValue(object["cameraId"])) /* 更新 cameraID 的值。 */
			if cameraID != "" {                                            /* 判断条件并选择处理分支。 */
				actions = append(actions, map[string]any{"type": actionType, "cameraId": cameraID}) /* 更新 actions 的值。 */
			} /* 结束当前表达式或代码块。 */
		case "OPEN_PAGE": /* 处理当前分支。 */
			page := strings.ToLower(strings.TrimSpace(stringValue(object["page"]))) /* 更新 page 的值。 */
			if page != "" {                                                         /* 判断条件并选择处理分支。 */
				actions = append(actions, map[string]any{"type": actionType, "page": page}) /* 更新 actions 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return actions /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func normalizeRuleConditions(value any) []map[string]any { /* 定义 normalizeRuleConditions 函数。 */
	conditions := []map[string]any{}         /* 更新 conditions 的值。 */
	appendMap := func(item map[string]any) { /* 更新 appendMap 的值。 */
		if field := strings.TrimSpace(stringValue(item["field"])); field != "" { /* 判断条件并选择处理分支。 */
			operator := strings.TrimSpace(stringValue(item["operator"])) /* 更新 operator 的值。 */
			if operator == "" {                                          /* 判断条件并选择处理分支。 */
				operator = "eq" /* 更新 operator 的值。 */
			} /* 结束当前表达式或代码块。 */
			conditions = append(conditions, map[string]any{"field": field, "operator": operator, "value": item["value"]}) /* 更新 conditions 的值。 */
			return                                                                                                        /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		keys := make([]string, 0, len(item)) /* 更新 keys 的值。 */
		for key := range item {              /* 循环处理当前数据。 */
			keys = append(keys, key) /* 更新 keys 的值。 */
		} /* 结束当前表达式或代码块。 */
		sort.Strings(keys)         /* 执行当前语句并推进处理流程。 */
		for _, key := range keys { /* 循环处理当前数据。 */
			conditions = append(conditions, map[string]any{"field": key, "operator": "eq", "value": item[key]}) /* 更新 conditions 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	switch typed := value.(type) { /* 根据条件选择处理路径。 */
	case []any: /* 处理当前分支。 */
		for _, item := range typed { /* 循环处理当前数据。 */
			if object, ok := item.(map[string]any); ok { /* 判断条件并选择处理分支。 */
				appendMap(object) /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	case map[string]any: /* 处理当前分支。 */
		appendMap(typed) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	return conditions /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func normalizeAlarmType(value string) string { /* 定义 normalizeAlarmType 函数。 */
	normalized := strings.ToUpper(strings.NewReplacer("-", "_", " ", "_").Replace(strings.TrimSpace(value))) /* 更新 normalized 的值。 */
	switch normalized {                                                                                      /* 根据条件选择处理路径。 */
	case "SMOKE", "SMOKE_ALARM", "SMOKE_DETECTOR": /* 处理当前分支。 */
		return "SMOKE_DETECTED" /* 返回当前处理结果。 */
	case "FLAME", "FLAME_ALARM": /* 处理当前分支。 */
		return "FLAME_DETECTED" /* 返回当前处理结果。 */
	case "HIGH_TEMP", "TEMPERATURE_HIGH": /* 处理当前分支。 */
		return "HIGH_TEMPERATURE" /* 返回当前处理结果。 */
	default: /* 处理当前分支。 */
		return normalized /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func stringValue(value any) string { /* 定义 stringValue 函数。 */
	if text, ok := value.(string); ok { /* 判断条件并选择处理分支。 */
		return text /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return "" /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
