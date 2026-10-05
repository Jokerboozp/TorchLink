package messagetopics

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"iot-platform/internal/model"
)

type QueryField struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Dynamic bool   `json:"dynamic"`
}
type QueryDataset struct {
	ID     string       `json:"id"`
	Name   string       `json:"name"`
	Mode   string       `json:"mode"`
	Fields []QueryField `json:"fields"`
}

func queryFields(entries ...string) []QueryField {
	result := make([]QueryField, 0, len(entries))
	for _, entry := range entries {
		parts := strings.Split(entry, ":")
		result = append(result, QueryField{Path: parts[0], Name: parts[1], Type: parts[2], Dynamic: parts[2] == "object" || parts[2] == "array"})
	}
	return result
}

var queryDatasets = func() []QueryDataset {
	reports := queryFields("messageId:消息编号:string", "rawMessageId:原文编号:string", "tenantId:租户:string", "productId:产品编号:string", "deviceId:设备编号:string", "messageType:上报类型:string", "timestamp:上报时间:number", "properties:属性:object", "event:事件:object", "tags:标签:object", "parser:解析器:string", "parserVersion:解析版本:string")
	alarms := queryFields("alarmId:告警编号:string", "tenantId:租户:string", "ruleId:规则编号:string", "triggerId:触发编号:string", "deviceId:设备编号:string", "deviceName:设备名称:string", "alarmType:告警类型:string", "content:告警内容:string", "alarmLevel:告警等级:string", "status:状态:string", "source:来源:string", "cityCode:城市编号:string", "districtCode:区域编号:string", "buildingId:建筑编号:string", "deviceType:设备类型:string", "areaId:区域:string", "firstTriggeredAt:首次触发时间:number", "lastTriggeredAt:最近触发时间:number", "triggerCount:触发次数:number", "recoveredAt:恢复时间:number", "ackedAt:确认时间:number", "closedAt:关闭时间:number", "confidence:置信度:number", "multiSource:多源告警:boolean", "cameras:摄像头:array", "details:详情:object", "componentId:部件编号:string", "componentName:部件名称:string", "componentLocation:部件位置:string")
	devices := queryFields("id:设备编号:string", "deviceId:设备编号:string", "tenantId:租户:string", "productId:产品编号:string", "name:设备名称:string", "status:管理状态:string", "deviceRole:设备角色:string", "gatewayId:网关编号:string", "description:说明:string", "tags:标签:object", "createdAt:创建时间:number", "updatedAt:更新时间:number", "online:是否在线:boolean", "lastSeen:最近上报时间:number", "connectionStatus:连接状态:string", "dataStatus:数据状态:string", "businessStatus:业务状态:string", "lastSeenAt:最近上报时间:number")
	return []QueryDataset{{"device_reports", "设备上报", "realtime", reports}, {"alarms", "新告警", "realtime", alarms}, {"alarm_recoveries", "告警恢复", "realtime", alarms}, {"alarm_confirmations", "告警确认", "realtime", alarms}, {"devices", "当前设备信息", "interval", devices}, {"alarms_current", "当前告警记录", "interval", alarms}}
}()

func QueryDatasets() []QueryDataset {
	out := slices.Clone(queryDatasets)
	for i := range out {
		out[i].Fields = slices.Clone(out[i].Fields)
	}
	return out
}
func queryDataset(id string) (QueryDataset, bool) {
	for _, d := range queryDatasets {
		if d.ID == id {
			return d, true
		}
	}
	return QueryDataset{}, false
}
func QuerySourceID(protocol, dataset string) string {
	if protocol != "mqtt" && protocol != "kafka" {
		return ""
	}
	switch dataset {
	case "device_reports", "devices":
		return protocol + ".parsed"
	case "alarms", "alarms_current":
		return protocol + ".alarm-raised"
	case "alarm_recoveries":
		return protocol + ".alarm-recovered"
	case "alarm_confirmations":
		return protocol + ".alarm-confirmed"
	}
	return ""
}
func querySourceIDs(protocol, dataset string) []string {
	source := QuerySourceID(protocol, dataset)
	if source == "" {
		return nil
	}
	if protocol == "kafka" && dataset == "device_reports" {
		return []string{"kafka.property-report", "kafka.event-report", "kafka.parsed"}
	}
	return []string{source}
}

var queryIdentifier = regexp.MustCompile(`^[\pL_][\pL\pN_]*$`)

func queryFieldType(dataset QueryDataset, path string) (string, bool) {
	if !validFieldPath(path) {
		return "", false
	}
	for _, f := range dataset.Fields {
		if f.Path == path {
			return f.Type, true
		}
		if f.Dynamic && strings.HasPrefix(path, f.Path+".") {
			return "any", true
		}
	}
	return "", false
}
func ValidateQuery(q model.MessageTopicQuery) error {
	d, ok := queryDataset(q.Dataset)
	if !ok {
		return errors.New("请选择支持的业务数据")
	}
	if q.Mode != d.Mode {
		return errors.New("数据与发送方式不匹配")
	}
	if (q.Mode == "realtime" && q.IntervalSeconds != 0) || (q.Mode == "interval" && (q.IntervalSeconds < 10 || q.IntervalSeconds > 86400)) {
		return errors.New("定时查询间隔须在10至86400秒之间，实时查询不设置间隔")
	}
	if !validScope(q.DeviceScope, q.DeviceIDs) {
		return errors.New("查询设备范围无效")
	}
	if len(q.Fields) > 64 {
		return errors.New("最多返回64个字段")
	}
	for alias, path := range q.Fields {
		if !queryIdentifier.MatchString(alias) || len(alias) > 100 {
			return errors.New("输出名称须以字母或下划线开头，只能包含字母、数字和下划线")
		}
		if _, ok := queryFieldType(d, path); !ok {
			return fmt.Errorf("未知业务字段 %s", path)
		}
	}
	count := 0
	if err := validateQueryFilter(d, q.Filter, 0, &count); err != nil {
		return err
	}
	sql := querySQLUnchecked(q)
	if len(sql) > 16<<10 {
		return errors.New("查询配置生成的SQL不能超过16KB，请减少字段或条件")
	}
	if _, err := tokenizeQuerySQL(sql); err != nil {
		return err
	}
	return nil
}
func validateQueryFilter(d QueryDataset, f *model.MessageTopicFilter, depth int, count *int) error {
	if f == nil {
		return nil
	}
	*count = *count + 1
	if depth > 8 || *count > 100 {
		return errors.New("查询条件最多100项、8层分组")
	}
	if f.Logic != "" || len(f.Children) > 0 {
		if (f.Logic != "and" && f.Logic != "or") || len(f.Children) == 0 || f.Field != "" || f.Operator != "" || f.Value != nil {
			return errors.New("条件组须包含and/or和子条件，不能同时设置字段条件")
		}
		for i := range f.Children {
			if err := validateQueryFilter(d, &f.Children[i], depth+1, count); err != nil {
				return err
			}
		}
		return nil
	}
	typ, ok := queryFieldType(d, f.Field)
	if !ok {
		return fmt.Errorf("未知筛选字段 %s", f.Field)
	}
	scalar := func(v any) bool {
		t, ok := queryValueType(v)
		return ok && t != "null" && (typ == "any" || typ == t)
	}
	switch f.Operator {
	case "is_null", "not_null":
		if f.Value != nil {
			return errors.New("空值判断不需要比较值")
		}
	case "in", "not_in":
		list, ok := queryList(f.Value)
		if !ok || len(list) == 0 || len(list) > 100 {
			return errors.New("IN需要1至100个值")
		}
		for _, v := range list {
			if !scalar(v) {
				return errors.New("IN比较值类型与字段不匹配")
			}
		}
	case "eq", "ne":
		if !scalar(f.Value) {
			return errors.New("比较值须为类型匹配的文本、数字或布尔值；空值请使用IS NULL")
		}
	case "gt", "gte", "lt", "lte":
		t, ok := queryValueType(f.Value)
		if !ok || (t != "number" && t != "string") || !scalar(f.Value) {
			return errors.New("大小比较只支持类型匹配的数字或文本")
		}
	case "contains":
		if _, ok := f.Value.(string); !ok || (typ != "string" && typ != "any") {
			return errors.New("包含条件只支持文本")
		}
	default:
		return errors.New("不支持的查询条件")
	}
	return nil
}
func queryList(v any) ([]any, bool) {
	if v == nil {
		return nil, false
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, false
	}
	out := make([]any, rv.Len())
	for i := range out {
		out[i] = rv.Index(i).Interface()
	}
	return out, true
}
func queryNumber(v any) (*big.Rat, bool) {
	var text string
	switch n := v.(type) {
	case json.Number:
		text = string(n)
	case float64:
		text = strconv.FormatFloat(n, 'g', -1, 64)
	case float32:
		text = strconv.FormatFloat(float64(n), 'g', -1, 32)
	case int:
		text = strconv.Itoa(n)
	case int64:
		text = strconv.FormatInt(n, 10)
	case int32:
		text = strconv.FormatInt(int64(n), 10)
	case uint:
		text = strconv.FormatUint(uint64(n), 10)
	case uint64:
		text = strconv.FormatUint(n, 10)
	default:
		return nil, false
	}
	// The bound also prevents huge exponents from allocating unbounded integers.
	if len(text) > 128 || len(text) == 0 || !strings.ContainsRune("-0123456789", rune(text[0])) || !json.Valid([]byte(text)) {
		return nil, false
	}
	if i := strings.IndexAny(text, "eE"); i >= 0 {
		exponent, e := strconv.Atoi(text[i+1:])
		if e != nil || exponent < -1000 || exponent > 1000 {
			return nil, false
		}
	}
	value, ok := new(big.Rat).SetString(text)
	return value, ok
}
func queryValueType(v any) (string, bool) {
	switch value := v.(type) {
	case nil:
		return "null", true
	case string:
		return "string", len(value) <= 4096
	case bool:
		return "boolean", true
	}
	if _, ok := queryNumber(v); ok {
		return "number", true
	}
	return "", false
}
func queryCompare(a, b any) (int, bool) {
	if na, ok := queryNumber(a); ok {
		if nb, ok := queryNumber(b); ok {
			return na.Cmp(nb), true
		}
		return 0, false
	}
	switch av := a.(type) {
	case string:
		if bv, ok := b.(string); ok {
			return strings.Compare(av, bv), true
		}
	case bool:
		if bv, ok := b.(bool); ok {
			if av == bv {
				return 0, true
			}
			if av {
				return 1, true
			}
			return -1, true
		}
	}
	return 0, false
}
func matchesQueryFilter(f *model.MessageTopicFilter, input map[string]any) bool {
	if f == nil {
		return true
	}
	if f.Logic != "" {
		for i := range f.Children {
			matched := matchesQueryFilter(&f.Children[i], input)
			if f.Logic == "and" && !matched {
				return false
			}
			if f.Logic == "or" && matched {
				return true
			}
		}
		return f.Logic == "and"
	}
	value, exists := pathValue(input, f.Field)
	switch f.Operator {
	case "is_null":
		return !exists || value == nil
	case "not_null":
		return exists && value != nil
	}
	if !exists || value == nil {
		return false
	}
	if f.Operator == "contains" {
		text, ok := value.(string)
		needle, _ := f.Value.(string)
		return ok && strings.Contains(text, needle)
	}
	if f.Operator == "in" || f.Operator == "not_in" {
		list, _ := queryList(f.Value)
		matched := false
		compatible := false
		for _, candidate := range list {
			cmp, ok := queryCompare(value, candidate)
			compatible = compatible || ok
			if ok && cmp == 0 {
				matched = true
				break
			}
		}
		return compatible && ((f.Operator == "in" && matched) || (f.Operator == "not_in" && !matched))
	}
	cmp, ok := queryCompare(value, f.Value)
	if !ok {
		return false
	}
	switch f.Operator {
	case "eq":
		return cmp == 0
	case "ne":
		return cmp != 0
	case "gt":
		return cmp > 0
	case "gte":
		return cmp >= 0
	case "lt":
		return cmp < 0
	case "lte":
		return cmp <= 0
	}
	return false
}
func normalizedQueryRecord(q model.MessageTopicQuery, payload []byte) (map[string]any, error) {
	if len(payload) > MaxPayload {
		return nil, errors.New("查询输入不能超过256KB")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	var input map[string]any
	if err := decoder.Decode(&input); err != nil || input == nil {
		return nil, errors.New("查询输入必须是JSON对象")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, errors.New("查询输入只能包含一条JSON记录")
	}
	d, ok := queryDataset(q.Dataset)
	if !ok {
		return nil, errors.New("未知业务数据")
	}
	out := make(map[string]any, len(d.Fields))
	for _, f := range d.Fields {
		if value, ok := input[f.Path]; ok {
			out[f.Path] = value
		}
	}
	if q.Dataset == "devices" {
		if _, exists := out["deviceId"]; !exists {
			out["deviceId"] = out["id"]
		}
	}
	return out, nil
}
func NormalizeQueryRecord(q model.MessageTopicQuery, payload []byte) ([]byte, error) {
	input, err := normalizedQueryRecord(q, payload)
	if err != nil {
		return nil, err
	}
	return json.Marshal(input)
}
func PreviewQuery(q model.MessageTopicQuery, payload []byte) ([]byte, bool, error) {
	if err := ValidateQuery(q); err != nil {
		return nil, false, err
	}
	input, err := normalizedQueryRecord(q, payload)
	if err != nil {
		return nil, false, err
	}
	if q.DeviceScope == "selected" {
		device, _ := input["deviceId"].(string)
		if device == "" || input["source"] == "video" || !slices.Contains(q.DeviceIDs, device) {
			return nil, false, nil
		}
	}
	if !matchesQueryFilter(q.Filter, input) {
		return nil, false, nil
	}
	out := input
	if len(q.Fields) > 0 {
		out = make(map[string]any, len(q.Fields))
		for alias, path := range q.Fields {
			value, _ := pathValue(input, path)
			out[alias] = value
		}
	}
	encoded, err := json.Marshal(out)
	if len(encoded) > MaxPayload {
		return nil, false, errors.New("查询输出不能超过256KB")
	}
	return encoded, err == nil, err
}
func cloneQuery(q *model.MessageTopicQuery) *model.MessageTopicQuery {
	if q == nil {
		return nil
	}
	out := *q
	out.DeviceIDs = slices.Clone(q.DeviceIDs)
	if q.Fields != nil {
		out.Fields = make(map[string]string, len(q.Fields))
		for k, v := range q.Fields {
			out.Fields[k] = v
		}
	}
	out.Filter = cloneQueryFilter(q.Filter)
	return &out
}
func cloneQueryFilter(f *model.MessageTopicFilter) *model.MessageTopicFilter {
	if f == nil {
		return nil
	}
	out := *f
	out.Children = make([]model.MessageTopicFilter, len(f.Children))
	for i := range f.Children {
		out.Children[i] = *cloneQueryFilter(&f.Children[i])
	}
	if list, ok := queryList(f.Value); ok {
		out.Value = list
	}
	return &out
}
