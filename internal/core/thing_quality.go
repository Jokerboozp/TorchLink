package core

import (
	"encoding/json"
	"math"
	"sort"
	"strings"

	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
)

// QualityTag is the standard message tag listing thing-model mismatches, such
// as "range:temperature,unknown:foo". Rules may read it like any other tag.
const QualityTag = "quality"

// Thing-model mismatch reasons recorded in QualityTag and parse_quality_total.
const (
	QualityUnknown = "unknown"
	QualityType    = "type"
	QualityRange   = "range"
)

const maxQualityTag = 256

// thingModelIssues compares reported properties with the product's declared
// properties. Products without declared properties are not checked.
func thingModelIssues(tm *model.ThingModel, msg *model.StandardMessage) []string {
	if tm == nil || len(tm.Properties) == 0 || len(msg.Properties) == 0 {
		return nil
	}
	declared := make(map[string]model.ThingField, len(tm.Properties))
	for _, field := range tm.Properties {
		declared[field.Identifier] = field
	}
	issues := []string{}
	for name, value := range msg.Properties {
		field, ok := declared[name]
		switch {
		case !ok:
			issues = append(issues, QualityUnknown+":"+name)
		case !thingValueMatches(field.DataType, value):
			issues = append(issues, QualityType+":"+name)
		case field.Numeric():
			if number, _ := thingNumber(value); field.Min != nil && number < *field.Min || field.Max != nil && number > *field.Max {
				issues = append(issues, QualityRange+":"+name)
			}
		}
	}
	sort.Strings(issues)
	return issues
}

// markThingQuality records thing-model mismatches on the message and in the
// metrics; the message is still forwarded.
func (e *Engine) markThingQuality(product model.Product, msg *model.StandardMessage) {
	issues := thingModelIssues(product.ThingModel, msg)
	if len(issues) == 0 {
		return
	}
	if msg.Tags == nil {
		msg.Tags = map[string]string{}
	}
	tag := strings.Join(issues, ",")
	if len(tag) > maxQualityTag {
		if cut := strings.LastIndexByte(tag[:maxQualityTag], ','); cut > 0 {
			tag = tag[:cut]
		} else {
			tag = tag[:maxQualityTag]
		}
	}
	msg.Tags[QualityTag] = tag
	if e.Metrics == nil {
		return
	}
	for _, issue := range issues {
		reason, _, _ := strings.Cut(issue, ":")
		e.Metrics.Inc(metrics.Series("parse_quality_total", "reason", reason))
	}
}

func thingNumber(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case int32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case uint32:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	}
	return 0, false
}

func thingValueMatches(dataType string, value any) bool {
	switch dataType {
	case "number":
		_, ok := thingNumber(value)
		return ok
	case "integer":
		n, ok := thingNumber(value)
		return ok && n == math.Trunc(n)
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	}
	return true
}
