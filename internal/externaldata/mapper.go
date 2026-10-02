package externaldata

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"reflect"
	"strconv"
	"strings"
	"time"
)

const MaxBodyBytes = 1 << 20
const MaxItems = 1000

func decodeJSON(body []byte) (any, error) {
	if len(body) > MaxBodyBytes {
		return nil, errors.New("数据超过 1 MiB 限制")
	}
	d := json.NewDecoder(bytes.NewReader(body))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return nil, errors.New("数据必须是有效 JSON")
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return nil, errors.New("JSON 后存在多余内容")
	}
	return v, nil
}

// pathParts accepts $.items[0].id, items.*.id, and bracket-quoted keys.
func pathParts(path string) ([]string, error) {
	path = strings.TrimSpace(path)
	if path == "" || path == "$" {
		return nil, nil
	}
	if strings.HasPrefix(path, "$") {
		path = path[1:]
		if len(path) > 0 && path[0] != '.' && path[0] != '[' {
			return nil, errors.New("字段路径无效")
		}
	}
	var parts []string
	for len(path) > 0 {
		if path[0] == '.' {
			path = path[1:]
			if path == "" {
				return nil, errors.New("字段路径无效")
			}
		}
		if path[0] == '[' {
			end := strings.IndexByte(path, ']')
			if end < 0 {
				return nil, errors.New("字段路径缺少右括号")
			}
			part := strings.TrimSpace(path[1:end])
			if part == "" {
				return nil, errors.New("字段路径无效")
			}
			if part[0] == '\'' && part[len(part)-1] == '\'' && len(part) >= 2 {
				part = part[1 : len(part)-1]
			} else if part[0] == '"' {
				var key string
				if json.Unmarshal([]byte(part), &key) != nil {
					return nil, errors.New("字段路径无效")
				}
				part = key
			} else if part != "*" {
				if n, err := strconv.Atoi(part); err != nil || n < 0 {
					return nil, errors.New("数组下标无效")
				}
			}
			parts = append(parts, part)
			path = path[end+1:]
			if len(path) > 0 && path[0] != '.' && path[0] != '[' {
				return nil, errors.New("字段路径无效")
			}
		} else {
			end := strings.IndexAny(path, ".[")
			if end < 0 {
				end = len(path)
			}
			if end == 0 {
				return nil, errors.New("字段路径无效")
			}
			parts = append(parts, path[:end])
			path = path[end:]
		}
	}
	return parts, nil
}

func lookupParts(v any, parts []string) (any, bool) {
	if len(parts) == 0 {
		return v, true
	}
	if parts[0] == "*" {
		list, ok := v.([]any)
		if !ok {
			return nil, false
		}
		out := make([]any, 0, len(list))
		for _, item := range list {
			if val, found := lookupParts(item, parts[1:]); found {
				out = append(out, val)
			}
		}
		return out, true
	}
	switch obj := v.(type) {
	case map[string]any:
		val, ok := obj[parts[0]]
		if !ok {
			return nil, false
		}
		return lookupParts(val, parts[1:])
	case []any:
		n, err := strconv.Atoi(parts[0])
		if err != nil || n < 0 || n >= len(obj) {
			return nil, false
		}
		return lookupParts(obj[n], parts[1:])
	default:
		return nil, false
	}
}

func lookup(v any, path string) (any, bool) {
	p, err := pathParts(path)
	if err != nil {
		return nil, false
	}
	return lookupParts(v, p)
}

func Extract(mapping Mapping, body []byte) ([]json.RawMessage, error) {
	root, err := decodeJSON(body)
	if err != nil {
		return nil, err
	}
	if mapping.SuccessPath != "" {
		v, ok := lookup(root, mapping.SuccessPath)
		if !ok || !equalValue(v, mapping.SuccessValue) {
			return nil, errors.New("外部接口返回未满足成功条件")
		}
	}
	v, found := lookup(root, mapping.ItemsPath)
	if !found || v == nil {
		return nil, errors.New("未找到配置的数据列表")
	}
	var values []any
	switch data := v.(type) {
	case []any:
		values = data
	case map[string]any:
		values = []any{data}
	default:
		return nil, errors.New("数据必须是对象或对象数组")
	}
	if len(values) > MaxItems {
		return nil, errors.New("单次数据超过 1000 条限制")
	}
	items := make([]json.RawMessage, 0, len(values))
	for _, item := range values {
		if _, ok := item.(map[string]any); !ok {
			return nil, errors.New("数据列表中存在非对象记录")
		}
		raw, err := json.Marshal(item)
		if err != nil {
			return nil, errors.New("数据编码失败")
		}
		items = append(items, raw)
	}
	return items, nil
}

func valueString(v any) (string, error) {
	switch val := v.(type) {
	case string:
		return val, nil
	case json.Number:
		return string(val), nil
	case bool:
		return strconv.FormatBool(val), nil
	case float64:
		if math.IsNaN(val) || math.IsInf(val, 0) {
			return "", errors.New("数值无效")
		}
		return strconv.FormatFloat(val, 'f', -1, 64), nil
	case nil:
		return "", errors.New("值为空")
	}
	// Direct Go callers may construct configuration constants with native
	// numeric types; JSON-decoded configurations normally use float64/Number.
	ref := reflect.ValueOf(v)
	switch ref.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(ref.Int(), 10), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(ref.Uint(), 10), nil
	case reflect.Float32:
		return strconv.FormatFloat(ref.Float(), 'f', -1, 32), nil
	}
	return "", errors.New("需要字符串、数字或布尔值")
}

func numberValue(v any) (float64, error) {
	s, err := valueString(v)
	if err != nil {
		return 0, err
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, errors.New("数值无效")
	}
	return n, nil
}

func equalValue(a, b any) bool {
	// Numeric representations from JSON and form values compare numerically.
	if na, ok := exactNumber(a); ok {
		if nb, ok := exactNumber(b); ok {
			return na.Cmp(nb) == 0
		}
	}
	return reflect.DeepEqual(a, b)
}

func exactNumber(v any) (*big.Rat, bool) {
	s, err := valueString(v)
	if err != nil || len(s) > 1024 {
		return nil, false
	}
	// Limit exponent length before big.Rat allocates an enormous integer.
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		n, err := strconv.Atoi(s[i+1:])
		if err != nil || n < -1024 || n > 1024 {
			return nil, false
		}
	}
	return new(big.Rat).SetString(strings.TrimSpace(s))
}

func filterMatches(v any, f Filter) (bool, error) {
	value, found := lookup(v, f.Path)
	switch f.Operator {
	case "exists":
		return found && value != nil, nil
	case "not_exists":
		return !found || value == nil, nil
	case "eq", "":
		return found && equalValue(value, f.Value), nil
	case "ne", "neq":
		return !found || !equalValue(value, f.Value), nil
	case "in", "not_in":
		list, ok := f.Value.([]any)
		if !ok {
			return false, errors.New("in 过滤值必须为数组")
		}
		match := false
		for _, item := range list {
			if found && equalValue(value, item) {
				match = true
				break
			}
		}
		if f.Operator == "not_in" {
			match = !match
		}
		return match, nil
	case "contains":
		if list, ok := value.([]any); ok {
			for _, item := range list {
				if equalValue(item, f.Value) {
					return true, nil
				}
			}
			return false, nil
		}
		a, aerr := valueString(value)
		b, berr := valueString(f.Value)
		return found && aerr == nil && berr == nil && strings.Contains(a, b), nil
	case "gt", "gte", "lt", "lte":
		a, ok := exactNumber(value)
		if !ok || !found {
			return false, nil
		}
		b, ok := exactNumber(f.Value)
		if !ok {
			return false, errors.New("比较过滤值必须为数字")
		}
		comparison := a.Cmp(b)
		switch f.Operator {
		case "gt":
			return comparison > 0, nil
		case "gte":
			return comparison >= 0, nil
		case "lt":
			return comparison < 0, nil
		default:
			return comparison <= 0, nil
		}
	default:
		return false, errors.New("过滤操作符不支持")
	}
}

func timestampValue(v any, f Field) (int64, error) {
	format := f.TimeFormat
	if format == "" || format == "milliseconds" || format == "seconds" {
		if n, err := numberValue(v); err == nil {
			if format == "seconds" {
				n *= 1000
			}
			if n <= 0 || n >= float64(math.MaxInt64) {
				return 0, errors.New("时间超出有效范围")
			}
			return int64(n), nil
		}
		if format != "" {
			return 0, errors.New("时间需要数值")
		}
		format = time.RFC3339
	}
	if format == "RFC3339" {
		format = time.RFC3339Nano
	}
	s, err := valueString(v)
	if err != nil {
		return 0, err
	}
	loc := time.UTC
	if f.Timezone != "" {
		loc, err = time.LoadLocation(f.Timezone)
		if err != nil {
			return 0, errors.New("时区无效")
		}
	}
	t, err := time.ParseInLocation(format, s, loc)
	if err != nil || t.UnixMilli() <= 0 {
		return 0, errors.New("时间与配置格式不匹配")
	}
	return t.UnixMilli(), nil
}

func convertField(v any, f Field) (any, error) {
	if len(f.Values) > 0 {
		key, err := valueString(v)
		if err != nil {
			return nil, err
		}
		if mapped, ok := f.Values[key]; ok {
			v = mapped
		}
	}
	kind := f.Type
	if kind == "" {
		switch f.Target {
		case "timestamp":
			kind = "timestamp"
		case "version", "confidence":
			kind = "number"
		case "online":
			kind = "boolean"
		case "data":
			kind = "json"
		default:
			if !strings.HasPrefix(f.Target, "data.") {
				kind = "string"
			} else {
				kind = "json"
			}
		}
	}
	switch kind {
	case "json":
		return v, nil
	case "string":
		return valueString(v)
	case "number":
		if f.Target == "version" {
			s, err := valueString(v)
			if err != nil {
				return nil, err
			}
			n, err := strconv.ParseInt(s, 10, 64)
			if err != nil {
				return nil, errors.New("版本需要 int64 整数")
			}
			return n, nil
		}
		return numberValue(v)
	case "timestamp":
		return timestampValue(v, f)
	case "boolean":
		if b, ok := v.(bool); ok {
			return b, nil
		}
		s, err := valueString(v)
		if err != nil {
			return nil, err
		}
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "true", "1":
			return true, nil
		case "false", "0":
			return false, nil
		}
		return nil, errors.New("布尔值需要 true、false、1 或 0")
	default:
		return nil, errors.New("字段类型不支持")
	}
}

func setTarget(result map[string]any, target string, value any) error {
	parts := strings.Split(target, ".")
	obj := result
	for _, part := range parts[:len(parts)-1] {
		if part == "" {
			return errors.New("目标字段无效")
		}
		next, ok := obj[part]
		if !ok {
			next = map[string]any{}
			obj[part] = next
		}
		child, ok := next.(map[string]any)
		if !ok {
			return errors.New("目标字段与已有值冲突")
		}
		obj = child
	}
	if parts[len(parts)-1] == "" {
		return errors.New("目标字段无效")
	}
	obj[parts[len(parts)-1]] = value
	return nil
}

func Transform(mapping Mapping, raw json.RawMessage) (Event, bool, error) {
	root, err := decodeJSON(raw)
	if err != nil {
		return Event{}, false, err
	}
	if _, ok := root.(map[string]any); !ok {
		return Event{}, false, errors.New("记录必须为对象")
	}
	for _, filter := range mapping.Filters {
		match, err := filterMatches(root, filter)
		if err != nil {
			return Event{}, false, err
		}
		if !match {
			return Event{}, true, nil
		}
	}
	result := make(map[string]any)
	for _, field := range mapping.Fields {
		value, found := lookup(root, field.Path)
		if field.Constant {
			value = field.Value
			found = true
		}
		if !found || value == nil {
			value = field.Default
		}
		if value == nil {
			if field.Required {
				return Event{}, false, fmt.Errorf("必填字段 %s 缺失", field.Target)
			}
			continue
		}
		value, err = convertField(value, field)
		if err != nil {
			return Event{}, false, fmt.Errorf("字段 %s 转换失败：%w", field.Target, err)
		}
		if field.Required && value == "" {
			return Event{}, false, fmt.Errorf("必填字段 %s 为空", field.Target)
		}
		if err = setTarget(result, field.Target, value); err != nil {
			return Event{}, false, err
		}
	}
	if len(mapping.IDFields) > 0 {
		parts := make([]any, 0, len(mapping.IDFields))
		for _, path := range mapping.IDFields {
			v, ok := lookup(root, path)
			if !ok || v == nil || v == "" {
				return Event{}, false, errors.New("组合事件编号字段缺失")
			}
			parts = append(parts, v)
		}
		encoded, err := json.Marshal(parts)
		if err != nil {
			return Event{}, false, errors.New("组合事件编号无效")
		}
		sum := sha256.Sum256(encoded)
		result["id"] = hex.EncodeToString(sum[:])
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return Event{}, false, errors.New("转换结果无效")
	}
	var event Event
	if err = json.Unmarshal(encoded, &event); err != nil {
		return Event{}, false, errors.New("转换后的字段类型与目标不匹配")
	}
	if strings.TrimSpace(event.ID) == "" {
		return Event{}, false, errors.New("事件编号不能为空，请配置 id 或组合编号字段")
	}
	if len(event.ID) > 512 || len(event.ObjectID) > 512 {
		return Event{}, false, errors.New("事件或对象编号超过 512 字符")
	}
	if event.Timestamp <= 0 {
		return Event{}, false, errors.New("事件时间必须为有效毫秒时间戳")
	}
	if event.Version < 0 {
		return Event{}, false, errors.New("事件版本不能为负数")
	}
	return event, false, nil
}
