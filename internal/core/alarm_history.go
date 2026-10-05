package core

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"iot-platform/internal/model"
)

// defaultAlarmHistoryProperties are checked for products without a thing
// model declaring numeric properties.
var defaultAlarmHistoryProperties = []string{"temperature", "smoke", "water_pressure", "voltage", "current", "gas"}

const (
	alarmHistoryLimit       = 1000
	alarmHistoryRecentLimit = 30
	alarmHistoryRecentMs    = 10 * 60 * 1000
	alarmHistoryHourMs      = 60 * 60 * 1000
	alarmHistoryDayMs       = 24 * 60 * 60 * 1000
	// alarmHistoryMaxProperties bounds the properties read for one alarm.
	alarmHistoryMaxProperties = 12
)

// alarmHistoryProperties chooses the properties an alarm analysis reads: the
// product's numeric thing-model properties, those named by the triggering
// rule first, then those with alarm thresholds. Without a thing model the
// default measurements are used.
func alarmHistoryProperties(product *model.Product, ruleFields []string) []model.ThingField {
	if product == nil || product.ThingModel == nil {
		out := make([]model.ThingField, 0, len(defaultAlarmHistoryProperties))
		for _, name := range defaultAlarmHistoryProperties {
			out = append(out, model.ThingField{Identifier: name})
		}
		return out
	}
	named := map[string]bool{}
	for _, field := range ruleFields {
		named[strings.TrimPrefix(field, "properties.")] = true
	}
	out := []model.ThingField{}
	for _, field := range product.ThingModel.Properties {
		if field.Numeric() || field.DataType == "boolean" && named[field.Identifier] {
			out = append(out, field)
		}
	}
	rank := func(f model.ThingField) int {
		switch {
		case named[f.Identifier]:
			return 0
		case f.AlarmHigh != nil || f.AlarmLow != nil:
			return 1
		}
		return 2
	}
	sort.SliceStable(out, func(i, j int) bool { return rank(out[i]) < rank(out[j]) })
	if len(out) > alarmHistoryMaxProperties {
		out = out[:alarmHistoryMaxProperties]
	}
	if len(out) == 0 {
		return alarmHistoryProperties(nil, nil)
	}
	return out
}

// alarmPropertyHistory reads each property once for the last 24 hours and
// condenses it: the latest value, recent points and per-window statistics,
// with the thing model's unit, valid range and alarm thresholds. This keeps
// the query count at one per property and the prompt within the model's
// context instead of repeating overlapping raw windows.
func (e *Engine) alarmPropertyHistory(ctx context.Context, alarm model.Alarm, product *model.Product, ruleFields []string) []map[string]any {
	end := alarm.LastTriggeredAt
	out := []map[string]any{}
	for _, field := range alarmHistoryProperties(product, ruleFields) {
		items, err := e.Repo.PropertyHistory(ctx, alarm.TenantID, alarm.DeviceID, field.Identifier, end-alarmHistoryDayMs, end, alarmHistoryLimit)
		if err != nil || len(items) == 0 {
			continue
		}
		if summary := summarizePropertyHistory(field.Identifier, items, end); summary != nil {
			for key, value := range thingFieldSemantics(field) {
				summary[key] = value
			}
			out = append(out, summary)
		}
	}
	return out
}

// thingFieldSemantics is the thing-model context of one property.
func thingFieldSemantics(field model.ThingField) map[string]any {
	out := map[string]any{}
	if field.Name != "" {
		out["name"] = field.Name
	}
	if field.Unit != "" {
		out["unit"] = field.Unit
	}
	for key, value := range map[string]*float64{"min": field.Min, "max": field.Max, "alarmLow": field.AlarmLow, "alarmHigh": field.AlarmHigh} {
		if value != nil {
			out[key] = *value
		}
	}
	return out
}

type historyPoint struct {
	ts    int64
	value any
}

func summarizePropertyHistory(property string, items []map[string]any, end int64) map[string]any {
	points := make([]historyPoint, 0, len(items))
	for _, item := range items {
		ts, ok := historyMillis(item["timestamp"])
		if !ok {
			continue
		}
		points = append(points, historyPoint{ts: ts, value: item["value"]})
	}
	if len(points) == 0 {
		return nil
	}
	sort.Slice(points, func(i, j int) bool { return points[i].ts < points[j].ts })
	recent := []map[string]any{}
	for _, point := range points {
		if point.ts >= end-alarmHistoryRecentMs {
			recent = append(recent, map[string]any{"timestamp": point.ts, "value": point.value})
		}
	}
	if len(recent) > alarmHistoryRecentLimit {
		recent = recent[len(recent)-alarmHistoryRecentLimit:]
	}
	latest := points[len(points)-1]
	summary := map[string]any{
		"contextType": "propertyHistory",
		"property":    property,
		"latest":      map[string]any{"timestamp": latest.ts, "value": latest.value},
		"recent10m":   recent,
		"stats1h":     historyStats(points, end-alarmHistoryHourMs),
		"stats24h":    historyStats(points, end-alarmHistoryDayMs),
	}
	if len(points) >= alarmHistoryLimit {
		summary["truncated"] = fmt.Sprintf("24 小时内数据超过 %d 条，仅统计最近 %d 条", alarmHistoryLimit, alarmHistoryLimit)
	}
	return summary
}

// historyStats summarises points newer than since: min/max/mean for numbers,
// value counts otherwise (for example smoke true/false).
func historyStats(points []historyPoint, since int64) map[string]any {
	count := 0
	numeric := 0
	minimum, maximum, sum := math.Inf(1), math.Inf(-1), 0.0
	counts := map[string]int{}
	for _, point := range points {
		if point.ts < since {
			continue
		}
		count++
		if number, ok := historyNumber(point.value); ok {
			numeric++
			minimum, maximum, sum = math.Min(minimum, number), math.Max(maximum, number), sum+number
			continue
		}
		if len(counts) < 16 {
			counts[fmt.Sprint(point.value)]++
		}
	}
	stats := map[string]any{"count": count}
	if numeric > 0 {
		stats["min"], stats["max"] = minimum, maximum
		stats["mean"] = math.Round(sum/float64(numeric)*1000) / 1000
	}
	if len(counts) > 0 {
		stats["valueCounts"] = counts
	}
	return stats
}

func historyMillis(value any) (int64, bool) {
	switch v := value.(type) {
	case int64:
		return v, true
	case int:
		return int64(v), true
	case float64:
		return int64(v), true
	case json.Number:
		n, err := v.Int64()
		return n, err == nil
	case string:
		n, err := strconv.ParseInt(v, 10, 64)
		return n, err == nil
	}
	return 0, false
}

func historyNumber(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, !math.IsNaN(v) && !math.IsInf(v, 0)
	case int64:
		return float64(v), true
	case int:
		return float64(v), true
	case json.Number:
		n, err := v.Float64()
		return n, err == nil
	}
	return 0, false
}
