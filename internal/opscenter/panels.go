package opscenter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// Panel queries run on the server: viewers send only the panel id, time range
// and variable selections. Selections are checked against the variable's
// options, then interpolated with Grafana's escaping rules for Prometheus and
// Loki before the query is sent through Grafana's data source API.

type PanelDataInput struct {
	From          int64               `json:"from"`
	To            int64               `json:"to"`
	Vars          map[string][]string `json:"vars"`
	MaxDataPoints int                 `json:"maxDataPoints"`
	Dashboard     map[string]any      `json:"dashboard,omitempty"`
	Panel         map[string]any      `json:"panel,omitempty"`
}

type ExecutedQuery struct {
	RefID      string `json:"refId"`
	DataSource string `json:"dataSource"`
	Type       string `json:"type"`
	Expr       string `json:"expr"`
}

type PanelData struct {
	Frames     map[string][]model.DataFrame `json:"frames"`
	Errors     map[string]string            `json:"errors,omitempty"`
	Queries    []ExecutedQuery              `json:"queries"`
	IntervalMs int64                        `json:"intervalMs"`
	Warnings   []string                     `json:"warnings,omitempty"`
}

type VariableOption struct {
	Text  string `json:"text"`
	Value string `json:"value"`
}

type varState struct {
	def     map[string]any
	name    string
	kind    string
	multi   bool
	all     bool
	values  []string
	options []VariableOption
	allRaw  string
}

const allValue = "$__all"

func (s *Service) PanelData(ctx context.Context, uid string, panelID int, in PanelDataInput) (PanelData, error) {
	if err := requireBackend(s.Dashboards); err != nil {
		return PanelData{}, err
	}
	if !dashboardUIDPattern.MatchString(uid) {
		return PanelData{}, ports.ErrOpsNotFound
	}
	dash, err := s.cachedDashboard(ctx, uid)
	if err != nil {
		return PanelData{}, err
	}
	var panel map[string]any
	for _, p := range allPanels(dash) {
		if id, ok := p["id"].(float64); ok && int(id) == panelID {
			panel = p
		}
	}
	if panel == nil {
		return PanelData{}, fmt.Errorf("%w: 面板不存在", ports.ErrOpsNotFound)
	}
	return s.runPanel(ctx, uid, dash, panel, in, true)
}

// PreviewPanel runs an unsaved panel definition from the editor. It requires
// the edit permission because the queries come from the request.
func (s *Service) PreviewPanel(ctx context.Context, in PanelDataInput) (PanelData, error) {
	if err := requireBackend(s.Dashboards); err != nil {
		return PanelData{}, err
	}
	if in.Panel == nil {
		return PanelData{}, invalid("panel", "缺少面板定义")
	}
	dash := in.Dashboard
	if dash == nil {
		dash = map[string]any{}
	}
	return s.runPanel(ctx, "", dash, in.Panel, in, false)
}

func (s *Service) runPanel(ctx context.Context, uid string, dash, panel map[string]any, in PanelDataInput, strict bool) (PanelData, error) {
	from, to, err := TimeRange(msTime(in.From), msTime(in.To), s.Limits.MaxMetricRange, s.now())
	if err != nil {
		return PanelData{}, err
	}
	index, def, err := s.dataSourceIndex(ctx)
	if err != nil {
		return PanelData{}, err
	}
	maxPoints := in.MaxDataPoints
	if maxPoints <= 0 || maxPoints > 5000 {
		maxPoints = 800
	}
	vars, warnings, err := s.resolveVariables(ctx, uid, dash, in.Vars, from, to, strict, index, def)
	if err != nil {
		return PanelData{}, err
	}
	out := PanelData{Frames: map[string][]model.DataFrame{}, Errors: map[string]string{}, Queries: []ExecutedQuery{}, Warnings: warnings}
	kind := strv(panel["type"])
	if kind == "row" || kind == "text" {
		return out, nil
	}
	if _, ok := PanelTypes[kind]; !ok {
		return out, invalid("panel", "面板类型 %s 未在平台中实现", kind)
	}
	variables := variableDefs(dash)
	queries := []map[string]any{}
	var maxInterval time.Duration
	for _, raw := range arr(panel["targets"]) {
		target := obj(raw)
		if target == nil || target["hide"] == true {
			continue
		}
		refID := firstNonEmpty(strv(target["refId"]), "A")
		ds, err := resolveDataSource(target["datasource"], panel["datasource"], index, def, variables)
		if err != nil {
			out.Errors[refID] = err.Error()
			continue
		}
		scrape := durationValue(strv(ds.JSONData["timeInterval"]), 15*time.Second)
		minInterval := durationValue(interpolatePlain(firstNonEmpty(strv(target["interval"]), strv(panel["interval"])), vars), scrape)
		interval := calculateInterval(from, to, maxPoints, minInterval)
		if interval > maxInterval {
			maxInterval = interval
		}
		builtins := builtinValues(from, to, interval, scrape)
		expr := interpolate(strv(target["expr"]), vars, builtins, ds.Type)
		if strings.TrimSpace(expr) == "" {
			continue
		}
		if err := checkQueryText("expr", expr); err != nil {
			out.Errors[refID] = err.Error()
			continue
		}
		q := map[string]any{"refId": refID, "datasource": map[string]any{"type": ds.Type, "uid": ds.UID}, "expr": expr, "intervalMs": interval.Milliseconds(), "maxDataPoints": maxPoints}
		legend := interpolatePlain(strv(target["legendFormat"]), vars)
		if legend != "" {
			q["legendFormat"] = legend
		}
		switch ds.Type {
		case "prometheus":
			instant := target["instant"] == true
			q["instant"], q["range"] = instant, target["range"] == true || !instant
			if format := strv(target["format"]); format == "table" || format == "time_series" {
				q["format"] = format
			}
			q["exemplar"] = false
		case "loki":
			queryType := firstNonEmpty(strv(target["queryType"]), "range")
			if queryType != "instant" {
				queryType = "range"
			}
			q["queryType"] = queryType
			maxLines := s.Limits.MaxLogLines
			if v, ok := target["maxLines"].(float64); ok && int(v) > 0 && int(v) < maxLines {
				maxLines = int(v)
			}
			if kind == "logs" {
				if v, ok := obj(panel["options"])["maxLines"].(float64); ok && int(v) > 0 && int(v) < maxLines {
					maxLines = int(v)
				}
			}
			q["maxLines"] = maxLines
			if strv(obj(panel["options"])["sortOrder"]) == "Ascending" {
				q["direction"] = "forward"
			} else {
				q["direction"] = "backward"
			}
		}
		queries = append(queries, q)
		out.Queries = append(out.Queries, ExecutedQuery{RefID: refID, DataSource: ds.Name, Type: ds.Type, Expr: expr})
	}
	out.IntervalMs = maxInterval.Milliseconds()
	if len(queries) == 0 {
		return out, nil
	}
	if len(queries) > 20 {
		return out, invalid("targets", "单个面板最多执行 20 个查询")
	}
	frames, errs, err := s.Dashboards.QueryData(ctx, ports.PanelQueryRequest{From: from, To: to, Queries: queries, MaxDataPoints: maxPoints})
	if err != nil {
		return out, err
	}
	for refID, list := range frames {
		out.Frames[refID] = limitFrames(list, s.Limits.MaxSeries)
	}
	for refID, msg := range errs {
		out.Errors[refID] = msg
	}
	return out, nil
}

// limitFrames caps the number of series returned for one query.
func limitFrames(frames []model.DataFrame, max int) []model.DataFrame {
	if max > 0 && len(frames) > max {
		return frames[:max]
	}
	return frames
}

func variableDefs(dash map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, v := range arr(obj(dash["templating"])["list"]) {
		if variable := obj(v); variable != nil {
			out[strv(variable["name"])] = variable
		}
	}
	return out
}

func currentValues(variable map[string]any) []string {
	current := obj(variable["current"])
	switch v := current["value"].(type) {
	case string:
		return []string{v}
	case []any:
		out := []string{}
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// resolveVariables walks the templating list in order (later variables may
// reference earlier ones) and fixes each variable's selected values. With
// strict=true every selection must be one of the variable's options.
func (s *Service) resolveVariables(ctx context.Context, uid string, dash map[string]any, selected map[string][]string, from, to time.Time, strict bool, index map[string]model.OpsDataSource, def model.OpsDataSource) (map[string]*varState, []string, error) {
	vars := map[string]*varState{}
	warnings := []string{}
	for _, raw := range arr(obj(dash["templating"])["list"]) {
		variable := obj(raw)
		if variable == nil {
			continue
		}
		v := &varState{def: variable, name: strv(variable["name"]), kind: strv(variable["type"]), multi: variable["multi"] == true, allRaw: strv(variable["allValue"])}
		if !supportedVariableTypes[v.kind] {
			vars[v.name] = v
			continue
		}
		requested, provided := selected[v.name]
		if !provided {
			requested = currentValues(variable)
		}
		if len(requested) > 50 {
			return nil, nil, invalid("vars", "变量 %s 选择的值过多", v.name)
		}
		switch v.kind {
		case "constant":
			v.values = []string{strv(variable["query"])}
			vars[v.name] = v
			continue
		case "textbox":
			value := strv(variable["query"])
			if len(requested) > 0 {
				value = requested[0]
			}
			if len([]rune(value)) > 200 {
				return nil, nil, invalid("vars", "变量 %s 的值过长", v.name)
			}
			v.values = []string{value}
			vars[v.name] = v
			continue
		}
		options, err := s.variableOptions(ctx, uid, variable, vars, from, to, index, def)
		if err != nil {
			warnings = append(warnings, "变量 "+v.name+" 的可选值加载失败，已使用默认值")
			v.values = currentValues(variable)
			vars[v.name] = v
			continue
		}
		v.options = options
		allowed := map[string]bool{}
		for _, o := range options {
			allowed[o.Value] = true
		}
		values := []string{}
		for _, r := range requested {
			if r == allValue && variable["includeAll"] == true {
				v.all = true
				continue
			}
			if strict && !allowed[r] && !(v.kind == "interval" && strings.HasPrefix(r, "$__auto")) {
				if provided {
					return nil, nil, invalid("vars", "变量 %s 的取值 %q 不在可选范围内", v.name, r)
				}
				continue
			}
			values = append(values, r)
		}
		if !v.multi && len(values) > 1 {
			values = values[:1]
		}
		if len(values) == 0 && !v.all && len(options) > 0 {
			if options[0].Value == allValue {
				v.all = true
			} else {
				values = []string{options[0].Value}
			}
		}
		if v.all {
			values = values[:0]
			for _, o := range options {
				if o.Value != allValue {
					values = append(values, o.Value)
				}
			}
		}
		v.values = values
		if v.kind == "interval" && len(values) == 1 && strings.HasPrefix(values[0], "$__auto") {
			count := 30
			if c, ok := variable["auto_count"].(float64); ok && c > 0 {
				count = int(c)
			}
			min := durationValue(strv(variable["auto_min"]), 10*time.Second)
			v.values = []string{promDuration(calculateInterval(from, to, count, min))}
		}
		if v.kind == "datasource" && len(values) == 1 {
			// Selected data source is used when targets reference this variable.
			variable["current"] = map[string]any{"value": values[0]}
		}
		vars[v.name] = v
	}
	return vars, warnings, nil
}

type VariableOptionsInput struct {
	From      int64               `json:"from"`
	To        int64               `json:"to"`
	Vars      map[string][]string `json:"vars"`
	Dashboard map[string]any      `json:"dashboard,omitempty"`
}

// VariableOptions returns the options of one variable, resolving its query
// with the current selections of the variables before it.
func (s *Service) VariableOptions(ctx context.Context, uid, name string, in VariableOptionsInput) ([]VariableOption, error) {
	if err := requireBackend(s.Dashboards); err != nil {
		return nil, err
	}
	dash := in.Dashboard
	if uid != "" {
		if !dashboardUIDPattern.MatchString(uid) {
			return nil, ports.ErrOpsNotFound
		}
		var err error
		if dash, err = s.cachedDashboard(ctx, uid); err != nil {
			return nil, err
		}
	}
	from, to, err := TimeRange(msTime(in.From), msTime(in.To), s.Limits.MaxMetricRange, s.now())
	if err != nil {
		return nil, err
	}
	index, def, err := s.dataSourceIndex(ctx)
	if err != nil {
		return nil, err
	}
	prior := map[string][]string{}
	list := arr(obj(dash["templating"])["list"])
	for _, raw := range list {
		variable := obj(raw)
		if strv(variable["name"]) == name {
			break
		}
		if vals, ok := in.Vars[strv(variable["name"])]; ok {
			prior[strv(variable["name"])] = vals
		}
	}
	truncated := map[string]any{"templating": map[string]any{"list": []any{}}}
	var target map[string]any
	for _, raw := range list {
		variable := obj(raw)
		if strv(variable["name"]) == name {
			target = variable
			break
		}
		truncated["templating"].(map[string]any)["list"] = append(truncated["templating"].(map[string]any)["list"].([]any), raw)
	}
	if target == nil {
		return nil, fmt.Errorf("%w: 变量不存在", ports.ErrOpsNotFound)
	}
	vars, _, err := s.resolveVariables(ctx, uid, truncated, prior, from, to, uid != "", index, def)
	if err != nil {
		return nil, err
	}
	return s.variableOptions(ctx, uid, target, vars, from, to, index, def)
}

var customSplit = regexp.MustCompile(`(?:\\,|[^,])+`)

func (s *Service) variableOptions(ctx context.Context, uid string, variable map[string]any, vars map[string]*varState, from, to time.Time, index map[string]model.OpsDataSource, def model.OpsDataSource) ([]VariableOption, error) {
	kind := strv(variable["type"])
	options := []VariableOption{}
	switch kind {
	case "custom", "interval":
		for _, part := range customSplit.FindAllString(strv(variable["query"]), -1) {
			part = strings.TrimSpace(strings.ReplaceAll(part, `\,`, ","))
			if part == "" {
				continue
			}
			text, value := part, part
			if kind == "custom" {
				if i := strings.Index(part, " : "); i >= 0 {
					text, value = strings.TrimSpace(part[:i]), strings.TrimSpace(part[i+3:])
				}
			}
			options = append(options, VariableOption{Text: text, Value: value})
		}
		if kind == "interval" && variable["auto"] == true {
			options = append([]VariableOption{{Text: "auto", Value: "$__auto_interval_" + strv(variable["name"])}}, options...)
		}
	case "datasource":
		pattern := compileVariableRegex(strv(variable["regex"]))
		names := []string{}
		for key, ds := range index {
			if strings.HasPrefix(key, "name:") && ds.Type == strv(variable["query"]) && (pattern == nil || pattern.MatchString(ds.Name)) {
				names = append(names, ds.Name)
			}
		}
		sort.Strings(names)
		for _, name := range names {
			options = append(options, VariableOption{Text: name, Value: index["name:"+name].UID})
		}
	case "query":
		raw := variable["query"]
		query := strv(raw)
		if q := obj(raw); q != nil {
			query = strv(q["query"])
			if query == "" && q["type"] != nil {
				query = lokiVariableQuery(q)
			}
		}
		ds, err := resolveDataSource(variable["datasource"], nil, index, def, variableDefs(map[string]any{"templating": map[string]any{"list": varDefsList(vars)}}))
		if err != nil {
			return nil, err
		}
		key := "var:" + uid + ":" + strv(variable["name"]) + ":" + hashVars(query, vars, from, to)
		if cached, ok := cacheGet(key); ok && uid != "" {
			options = cached.([]VariableOption)
			break
		}
		builtins := builtinValues(from, to, calculateInterval(from, to, 100, 15*time.Second), 15*time.Second)
		values, err := s.queryVariable(ctx, ds, interpolate(query, vars, builtins, ds.Type), from, to)
		if err != nil {
			return nil, err
		}
		options = applyVariableRegex(values, strv(variable["regex"]))
		sortOptions(options, variable["sort"])
		if len(options) > 1000 {
			options = options[:1000]
		}
		if uid != "" {
			cachePut(key, options, 30*time.Second)
		}
	default:
		return nil, invalid("variable", "变量类型 %s 暂不支持", kind)
	}
	if variable["includeAll"] == true && kind != "interval" {
		options = append([]VariableOption{{Text: "All", Value: allValue}}, options...)
	}
	return options, nil
}

func varDefsList(vars map[string]*varState) []any {
	out := []any{}
	for _, v := range vars {
		out = append(out, v.def)
	}
	return out
}

func hashVars(query string, vars map[string]*varState, from, to time.Time) string {
	names := make([]string, 0, len(vars))
	for name := range vars {
		names = append(names, name)
	}
	sort.Strings(names)
	h := sha256.New()
	h.Write([]byte(query))
	for _, name := range names {
		h.Write([]byte(name + "=" + strings.Join(vars[name].values, "\x00")))
	}
	h.Write([]byte(from.Truncate(time.Minute).String() + to.Truncate(time.Minute).String()))
	return hex.EncodeToString(h.Sum(nil)[:8])
}

func lokiVariableQuery(q map[string]any) string {
	label, stream := strv(q["label"]), strv(q["stream"])
	switch fmt.Sprint(q["type"]) {
	case "0", "labelNames":
		return "label_names()"
	default:
		if stream != "" {
			return "label_values(" + stream + ", " + label + ")"
		}
		return "label_values(" + label + ")"
	}
}

var (
	labelNamesFn  = regexp.MustCompile(`^\s*label_names\(\s*(.*?)\s*\)\s*$`)
	labelValuesFn = regexp.MustCompile(`^\s*label_values\(\s*(?:(.+?)\s*,\s*)?([a-zA-Z_][a-zA-Z0-9_]*)\s*\)\s*$`)
	metricsFn     = regexp.MustCompile(`^\s*metrics\(\s*(.*?)\s*\)\s*$`)
	queryResultFn = regexp.MustCompile(`^\s*query_result\(\s*(.+)\s*\)\s*$`)
)

// queryVariable implements the Prometheus and Loki variable query functions
// through Grafana's data source resource API. Resource paths are fixed here.
func (s *Service) queryVariable(ctx context.Context, ds model.OpsDataSource, query string, from, to time.Time) ([]string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return []string{}, nil
	}
	params := url.Values{}
	resource := ""
	switch ds.Type {
	case "prometheus":
		params.Set("start", strconv.FormatInt(from.Unix(), 10))
		params.Set("end", strconv.FormatInt(to.Unix(), 10))
		switch {
		case labelNamesFn.MatchString(query):
			if m := labelNamesFn.FindStringSubmatch(query); m[1] != "" {
				params.Add("match[]", m[1])
			}
			resource = "api/v1/labels"
		case labelValuesFn.MatchString(query):
			m := labelValuesFn.FindStringSubmatch(query)
			if m[1] != "" {
				params.Add("match[]", m[1])
			}
			resource = "api/v1/label/" + m[2] + "/values"
		case metricsFn.MatchString(query):
			values, err := s.resourceStrings(ctx, ds, "api/v1/label/__name__/values", params)
			if err != nil {
				return nil, err
			}
			pattern := metricsFn.FindStringSubmatch(query)[1]
			re, err := regexp.Compile(pattern)
			if err != nil {
				return nil, invalid("variable", "metrics() 的正则表达式无效")
			}
			out := []string{}
			for _, v := range values {
				if re.MatchString(v) {
					out = append(out, v)
				}
			}
			return out, nil
		case queryResultFn.MatchString(query):
			expr := queryResultFn.FindStringSubmatch(query)[1]
			frames, errs, err := s.Dashboards.QueryData(ctx, ports.PanelQueryRequest{From: from, To: to, Queries: []map[string]any{{"refId": "V", "datasource": map[string]any{"type": ds.Type, "uid": ds.UID}, "expr": expr, "instant": true, "range": false}}})
			if err != nil {
				return nil, err
			}
			if msg := errs["V"]; msg != "" {
				return nil, invalid("variable", "%s", msg)
			}
			return frameResultStrings(frames["V"]), nil
		default:
			// A bare PromQL expression behaves like query_result in Grafana.
			return s.queryVariable(ctx, ds, "query_result("+query+")", from, to)
		}
	case "loki":
		params.Set("start", strconv.FormatInt(from.UnixNano(), 10))
		params.Set("end", strconv.FormatInt(to.UnixNano(), 10))
		switch {
		case labelNamesFn.MatchString(query):
			resource = "labels"
		case labelValuesFn.MatchString(query):
			m := labelValuesFn.FindStringSubmatch(query)
			if m[1] != "" {
				params.Set("query", m[1])
			}
			resource = "label/" + m[2] + "/values"
		default:
			return nil, invalid("variable", "Loki 变量查询只支持 label_names() 与 label_values()")
		}
	default:
		return nil, invalid("variable", "数据源类型 %s 暂不支持变量查询", ds.Type)
	}
	return s.resourceStrings(ctx, ds, resource, params)
}

func (s *Service) resourceStrings(ctx context.Context, ds model.OpsDataSource, resource string, params url.Values) ([]string, error) {
	raw, err := s.Dashboards.DataSourceResource(ctx, ds.UID, resource, params)
	if err != nil {
		return nil, err
	}
	var body struct {
		Data []string `json:"data"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("unexpected variable response")
	}
	return body.Data, nil
}

func frameResultStrings(frames []model.DataFrame) []string {
	out := []string{}
	for _, f := range frames {
		for _, field := range f.Fields {
			if field.Type != "number" {
				continue
			}
			names := make([]string, 0, len(field.Labels))
			for k := range field.Labels {
				if k != "__name__" {
					names = append(names, k)
				}
			}
			sort.Strings(names)
			parts := []string{}
			for _, k := range names {
				parts = append(parts, k+"="+strconv.Quote(field.Labels[k]))
			}
			value := ""
			if len(field.Values) > 0 {
				value = fmt.Sprint(field.Values[len(field.Values)-1])
			}
			out = append(out, field.Labels["__name__"]+"{"+strings.Join(parts, ", ")+"} "+value)
		}
	}
	return out
}

func compileVariableRegex(raw string) *regexp.Regexp {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	flags := ""
	if strings.HasPrefix(raw, "/") {
		if i := strings.LastIndex(raw, "/"); i > 0 {
			if strings.Contains(raw[i+1:], "i") {
				flags = "(?i)"
			}
			raw = raw[1:i]
		}
	}
	re, err := regexp.Compile(flags + raw)
	if err != nil {
		return nil
	}
	return re
}

func applyVariableRegex(values []string, raw string) []VariableOption {
	re := compileVariableRegex(raw)
	seen := map[string]bool{}
	out := []VariableOption{}
	for _, v := range values {
		text, value := v, v
		if re != nil {
			m := re.FindStringSubmatch(v)
			if m == nil {
				continue
			}
			if len(m) > 1 {
				text, value = m[1], m[1]
				for i, name := range re.SubexpNames() {
					switch name {
					case "text":
						text = m[i]
					case "value":
						value = m[i]
					}
				}
			}
		}
		if !seen[value] {
			seen[value] = true
			out = append(out, VariableOption{Text: text, Value: value})
		}
	}
	return out
}

func sortOptions(options []VariableOption, mode any) {
	m, _ := mode.(float64)
	switch int(m) {
	case 1, 5, 7:
		sort.SliceStable(options, func(i, j int) bool { return strings.ToLower(options[i].Text) < strings.ToLower(options[j].Text) })
	case 2, 6, 8:
		sort.SliceStable(options, func(i, j int) bool { return strings.ToLower(options[i].Text) > strings.ToLower(options[j].Text) })
	case 3, 4:
		num := func(s string) float64 {
			f, err := strconv.ParseFloat(regexp.MustCompile(`-?[0-9.]+`).FindString(s), 64)
			if err != nil {
				return math.Inf(1)
			}
			return f
		}
		sort.SliceStable(options, func(i, j int) bool {
			if int(m) == 3 {
				return num(options[i].Text) < num(options[j].Text)
			}
			return num(options[i].Text) > num(options[j].Text)
		})
	}
}

var varRefPattern = regexp.MustCompile(`\$(\w+)|\[\[(\w+?)(?::(\w+))?\]\]|\$\{(\w+)(?:\.([^:^}]+))?(?::([^}]+))?\}`)

func builtinValues(from, to time.Time, interval, scrape time.Duration) map[string]string {
	rangeDur := to.Sub(from)
	rate := interval + scrape
	if 4*scrape > rate {
		rate = 4 * scrape
	}
	return map[string]string{
		"__interval":      promDuration(interval),
		"__interval_ms":   strconv.FormatInt(interval.Milliseconds(), 10),
		"__rate_interval": promDuration(rate),
		"__range":         promDuration(rangeDur.Round(time.Second)),
		"__range_s":       strconv.FormatInt(int64(rangeDur.Seconds()), 10),
		"__range_ms":      strconv.FormatInt(rangeDur.Milliseconds(), 10),
		"__from":          strconv.FormatInt(from.UnixMilli(), 10),
		"__to":            strconv.FormatInt(to.UnixMilli(), 10),
		"__auto":          promDuration(interval),
	}
}

// interpolate replaces variable references following Grafana: the data
// source's default format applies unless ${var:format} names one.
func interpolate(text string, vars map[string]*varState, builtins map[string]string, dsType string) string {
	return varRefPattern.ReplaceAllStringFunc(text, func(ref string) string {
		m := varRefPattern.FindStringSubmatch(ref)
		name := firstNonEmpty(m[1], m[2], m[4])
		format := firstNonEmpty(m[3], m[6])
		if value, ok := builtins[name]; ok {
			return value
		}
		v, ok := vars[name]
		if !ok {
			return ref
		}
		return formatVariable(v, format, dsType)
	})
}

// interpolatePlain is used for legends and intervals: values are joined
// without query escaping.
func interpolatePlain(text string, vars map[string]*varState) string {
	return varRefPattern.ReplaceAllStringFunc(text, func(ref string) string {
		m := varRefPattern.FindStringSubmatch(ref)
		name := firstNonEmpty(m[1], m[2], m[4])
		if v, ok := vars[name]; ok {
			return strings.Join(v.values, ", ")
		}
		return ref
	})
}

func formatVariable(v *varState, format, dsType string) string {
	values := v.values
	if v.all && v.allRaw != "" && format == "" {
		return v.allRaw
	}
	switch format {
	case "raw":
		return strings.Join(values, ",")
	case "csv":
		return strings.Join(values, ",")
	case "pipe":
		return strings.Join(values, "|")
	case "regex":
		escaped := make([]string, len(values))
		for i, value := range values {
			escaped[i] = regexp.QuoteMeta(value)
		}
		if len(escaped) == 1 {
			return escaped[0]
		}
		return "(" + strings.Join(escaped, "|") + ")"
	case "json":
		b, _ := json.Marshal(values)
		return string(b)
	case "doublequote":
		quoted := make([]string, len(values))
		for i, value := range values {
			quoted[i] = strconv.Quote(value)
		}
		return strings.Join(quoted, ",")
	case "singlequote":
		quoted := make([]string, len(values))
		for i, value := range values {
			quoted[i] = "'" + strings.ReplaceAll(value, "'", `\'`) + "'"
		}
		return strings.Join(quoted, ",")
	case "glob":
		if len(values) == 1 {
			return values[0]
		}
		return "{" + strings.Join(values, ",") + "}"
	case "text":
		return strings.Join(values, " + ")
	case "percentencode":
		return url.QueryEscape(strings.Join(values, ","))
	}
	if dsType != "prometheus" && dsType != "loki" {
		return strings.Join(values, ",")
	}
	if !v.multi && !v.all && v.def["includeAll"] != true {
		if len(values) == 0 {
			return ""
		}
		return promRegularEscape(values[0])
	}
	escaped := make([]string, len(values))
	for i, value := range values {
		escaped[i] = promSpecialRegexEscape(value)
	}
	if len(escaped) == 1 {
		return escaped[0]
	}
	return "(" + strings.Join(escaped, "|") + ")"
}

// promRegularEscape and promSpecialRegexEscape match Grafana's Prometheus
// data source so imported dashboards produce the same queries.
func promRegularEscape(value string) string {
	return strings.NewReplacer(`\`, `\\`, `'`, `\\'`).Replace(value)
}

var promSpecial = regexp.MustCompile(`[$^*{}\[\]'+?.()|]`)

func promSpecialRegexEscape(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\\\`)
	return promSpecial.ReplaceAllString(value, `\\$0`)
}

func durationValue(value string, fallback time.Duration) time.Duration {
	value = strings.TrimPrefix(strings.TrimSpace(value), ">")
	if d, ok := parseDuration(value); ok && d > 0 {
		return d
	}
	return fallback
}

var roundSteps = []struct{ limit, step time.Duration }{
	{10 * time.Millisecond, time.Millisecond}, {15 * time.Millisecond, 10 * time.Millisecond}, {35 * time.Millisecond, 20 * time.Millisecond}, {75 * time.Millisecond, 50 * time.Millisecond},
	{150 * time.Millisecond, 100 * time.Millisecond}, {350 * time.Millisecond, 200 * time.Millisecond}, {750 * time.Millisecond, 500 * time.Millisecond}, {1500 * time.Millisecond, time.Second},
	{3500 * time.Millisecond, 2 * time.Second}, {7500 * time.Millisecond, 5 * time.Second}, {12500 * time.Millisecond, 10 * time.Second}, {17500 * time.Millisecond, 15 * time.Second},
	{25 * time.Second, 20 * time.Second}, {45 * time.Second, 30 * time.Second}, {90 * time.Second, time.Minute}, {210 * time.Second, 2 * time.Minute},
	{450 * time.Second, 5 * time.Minute}, {750 * time.Second, 10 * time.Minute}, {1050 * time.Second, 15 * time.Minute}, {1500 * time.Second, 20 * time.Minute},
	{2700 * time.Second, 30 * time.Minute}, {5400 * time.Second, time.Hour}, {9000 * time.Second, 2 * time.Hour}, {16200 * time.Second, 3 * time.Hour},
	{24300 * time.Second, 6 * time.Hour}, {64800 * time.Second, 12 * time.Hour}, {129600 * time.Second, 24 * time.Hour}, {1209600 * time.Second, 7 * 24 * time.Hour},
}

// calculateInterval mirrors Grafana's interval calculation: range divided by
// the maximum data points, rounded to a readable step, never below min.
func calculateInterval(from, to time.Time, maxPoints int, min time.Duration) time.Duration {
	if maxPoints <= 0 {
		maxPoints = 800
	}
	raw := to.Sub(from) / time.Duration(maxPoints)
	interval := 30 * 24 * time.Hour
	for _, s := range roundSteps {
		if raw <= s.limit {
			interval = s.step
			break
		}
	}
	if interval < min {
		interval = min
	}
	return interval
}

func promDuration(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	var b strings.Builder
	for _, unit := range []struct {
		suffix string
		size   time.Duration
	}{{"d", 24 * time.Hour}, {"h", time.Hour}, {"m", time.Minute}, {"s", time.Second}, {"ms", time.Millisecond}} {
		if n := d / unit.size; n > 0 {
			b.WriteString(strconv.FormatInt(int64(n), 10) + unit.suffix)
			d -= n * unit.size
		}
	}
	return b.String()
}
