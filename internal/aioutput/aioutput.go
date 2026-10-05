// Package aioutput parses structured answers written by models. It is shared by
// the Harness business workflows and MCP tools so both accept the same shapes.
package aioutput

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"iot-platform/internal/model"
)

// ExtractJSON returns the first complete JSON object inside a model answer.
// Braces are matched outside strings, so text before or after the object
// (even text with braces of its own, such as a Markdown code fence) does not
// shift its bounds. When no object is valid, trailing commas before a closing
// bracket are removed once, a common model slip. Without a complete object the
// answer is returned unchanged and fails to decode.
func ExtractJSON(s string) string {
	if object, ok := firstJSONObject(s); ok {
		return object
	}
	if object, ok := firstJSONObject(withoutTrailingCommas(s)); ok {
		return object
	}
	return s
}

func firstJSONObject(s string) (string, bool) {
	for start := strings.IndexByte(s, '{'); start >= 0; {
		if end := matchingBrace(s, start); end > start && json.Valid([]byte(s[start:end+1])) {
			return s[start : end+1], true
		}
		next := strings.IndexByte(s[start+1:], '{')
		if next < 0 {
			break
		}
		start += next + 1
	}
	return "", false
}

// withoutTrailingCommas drops commas, outside strings, that are followed only
// by whitespace and a closing brace or bracket.
func withoutTrailingCommas(s string) string {
	var b strings.Builder
	inString, escaped := false, false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case inString && escaped:
			escaped = false
		case inString && ch == '\\':
			escaped = true
		case inString && ch == '"':
			inString = false
		case inString:
		case ch == '"':
			inString = true
		case ch == ',':
			rest := strings.TrimLeft(s[i+1:], " \t\r\n")
			if rest != "" && (rest[0] == '}' || rest[0] == ']') {
				continue
			}
		}
		b.WriteByte(ch)
	}
	return b.String()
}

// matchingBrace returns the index of the brace closing the object that opens
// at start, or -1.
func matchingBrace(s string, start int) int {
	depth, inString, escaped := 0, false, false
	for i := start; i < len(s); i++ {
		ch := s[i]
		switch {
		case inString && escaped:
			escaped = false
		case inString && ch == '\\':
			escaped = true
		case inString && ch == '"':
			inString = false
		case inString:
		case ch == '"':
			inString = true
		case ch == '{':
			depth++
		case ch == '}':
			if depth--; depth == 0 {
				return i
			}
		}
	}
	return -1
}

// Bounds of an alarm analysis; longer lists and items are cut, not rejected.
const (
	MaxAnalysisItems    = 10
	MaxAnalysisItemRune = 500
	MaxAnalysisSummary  = 1000
)

// AlarmRiskLevels are the risk levels an alarm analysis may report.
var AlarmRiskLevels = []string{"CRITICAL", "HIGH", "MEDIUM", "LOW", "INFO"}

// DecodeAlarmAnalysis parses a model answer into an alarm analysis. The risk
// level must be one of AlarmRiskLevels (any case); the caller sets the prompt
// version.
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
	if out.Summary = strings.TrimSpace(out.Summary); out.Summary == "" {
		return out, fmt.Errorf("decode model json: summary is empty")
	}
	out.RiskLevel = strings.ToUpper(strings.TrimSpace(out.RiskLevel))
	if !slices.Contains(AlarmRiskLevels, out.RiskLevel) {
		return out, fmt.Errorf("decode model json: riskLevel %q is not one of %s", out.RiskLevel, strings.Join(AlarmRiskLevels, "|"))
	}
	out.Summary = truncateRunes(out.Summary, MaxAnalysisSummary)
	out.PossibleReasons = boundItems(out.PossibleReasons)
	out.Suggestions = boundItems(out.Suggestions)
	out.Confidence = min(max(out.Confidence, 0), 1)
	out.AlarmID, out.Model, out.CreatedAt = alarmID, modelName, time.Now().UnixMilli()
	return out, nil
}

// boundItems drops empty entries and keeps at most MaxAnalysisItems items of
// at most MaxAnalysisItemRune runes.
func boundItems(items []string) []string {
	out := make([]string, 0, min(len(items), MaxAnalysisItems))
	for _, item := range items {
		if item = strings.TrimSpace(item); item == "" {
			continue
		}
		if out = append(out, truncateRunes(item, MaxAnalysisItemRune)); len(out) == MaxAnalysisItems {
			break
		}
	}
	return out
}

func truncateRunes(s string, limit int) string {
	if runes := []rune(s); len(runes) > limit {
		return string(runes[:limit])
	}
	return s
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
