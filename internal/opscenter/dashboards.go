package opscenter

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// Dashboards, folders and data sources are stored only in Grafana. The
// platform keeps nothing but per-user favorites, so there is one source of
// truth. The UI edits the Grafana JSON model in place and leaves every field
// it does not understand untouched; the analysis below states exactly which
// parts the native renderer supports.

// PanelTypes the native renderer draws. "graph" is Grafana's legacy time
// series panel and is drawn with the time series renderer.
var PanelTypes = map[string]string{
	"timeseries": "时序图", "graph": "时序图（旧版 graph）", "stat": "统计卡片", "gauge": "仪表", "bargauge": "条形仪表",
	"table": "表格", "logs": "日志", "text": "文本", "row": "分组行",
}

var supportedVariableTypes = map[string]bool{"query": true, "custom": true, "constant": true, "textbox": true, "interval": true, "datasource": true}

var reduceCalcs = map[string]bool{"lastNotNull": true, "last": true, "firstNotNull": true, "first": true, "mean": true, "max": true, "min": true, "sum": true, "count": true, "delta": true, "range": true, "diff": true}

var dashboardUIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,40}$`)

type SupportItem struct {
	ID      any      `json:"id,omitempty"`
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Status  string   `json:"status"`
	Reasons []string `json:"reasons,omitempty"`
}

type SupportReport struct {
	Level     string        `json:"level"`
	Panels    []SupportItem `json:"panels"`
	Variables []SupportItem `json:"variables"`
	Notes     []string      `json:"notes"`
	Requires  []string      `json:"requires,omitempty"`
}

func obj(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func arr(v any) []any {
	a, _ := v.([]any)
	return a
}

func strv(v any) string {
	s, _ := v.(string)
	return s
}

// allPanels flattens rows, including panels nested in collapsed rows.
func allPanels(dash map[string]any) []map[string]any {
	out := []map[string]any{}
	for _, p := range arr(dash["panels"]) {
		panel := obj(p)
		if panel == nil {
			continue
		}
		out = append(out, panel)
		for _, child := range arr(panel["panels"]) {
			if c := obj(child); c != nil {
				out = append(out, c)
			}
		}
	}
	return out
}

func (s *Service) dataSourceIndex(ctx context.Context) (map[string]model.OpsDataSource, model.OpsDataSource, error) {
	items, err := s.cachedDataSources(ctx)
	if err != nil {
		return nil, model.OpsDataSource{}, err
	}
	index := map[string]model.OpsDataSource{}
	var def model.OpsDataSource
	for _, ds := range items {
		index[ds.UID] = ds
		index["name:"+ds.Name] = ds
		if ds.IsDefault {
			def = ds
		}
	}
	return index, def, nil
}

// AnalyzeDashboard reports what the native renderer supports. Nothing is
// removed from the dashboard; unsupported parts stay in Grafana and are
// listed so users know the platform view is incomplete.
func AnalyzeDashboard(dash map[string]any, dataSources map[string]model.OpsDataSource, def model.OpsDataSource) SupportReport {
	report := SupportReport{Panels: []SupportItem{}, Variables: []SupportItem{}, Notes: []string{}}
	variables := map[string]map[string]any{}
	for _, v := range arr(obj(dash["templating"])["list"]) {
		variable := obj(v)
		if variable == nil {
			continue
		}
		name, kind := strv(variable["name"]), strv(variable["type"])
		variables[name] = variable
		item := SupportItem{Name: name, Type: kind, Status: "supported"}
		if !supportedVariableTypes[kind] {
			item.Status, item.Reasons = "unsupported", []string{"变量类型 " + kind + " 暂不支持，面板中引用它的查询按空值处理"}
		} else if kind == "query" {
			if _, err := resolveDataSource(variable["datasource"], nil, dataSources, def, variables); err != nil {
				item.Status, item.Reasons = "unsupported", []string{err.Error()}
			}
		}
		report.Variables = append(report.Variables, item)
	}
	for _, panel := range allPanels(dash) {
		kind := strv(panel["type"])
		item := SupportItem{ID: panel["id"], Name: strv(panel["title"]), Type: kind, Status: "supported"}
		reasons := []string{}
		if panel["libraryPanel"] != nil {
			item.Status = "unsupported"
			reasons = append(reasons, "库面板（library panel）内容不在仪表盘 JSON 中，平台无法渲染")
		} else if _, ok := PanelTypes[kind]; !ok {
			item.Status = "unsupported"
			reasons = append(reasons, "面板类型 "+kind+" 未在平台中实现，内容保留在 Grafana")
		}
		if item.Status == "supported" && kind != "row" && kind != "text" {
			if ts := arr(panel["transformations"]); len(ts) > 0 {
				names := []string{}
				for _, t := range ts {
					names = append(names, strv(obj(t)["id"]))
				}
				reasons = append(reasons, "数据转换未执行："+strings.Join(names, "、"))
			}
			if overrides := arr(obj(panel["fieldConfig"])["overrides"]); len(overrides) > 0 {
				reasons = append(reasons, fmt.Sprintf("%d 条字段覆盖规则未应用", len(overrides)))
			}
			if panel["repeat"] != nil && strv(panel["repeat"]) != "" {
				reasons = append(reasons, "按变量重复面板未展开，只显示一次")
			}
			if calcs := arr(obj(obj(panel["options"])["reduceOptions"])["calcs"]); len(calcs) > 0 && !reduceCalcs[strv(calcs[0])] {
				reasons = append(reasons, "统计方式 "+strv(calcs[0])+" 未实现，按最新非空值显示")
			}
			for _, t := range arr(panel["targets"]) {
				target := obj(t)
				if target == nil || target["hide"] == true {
					continue
				}
				ds, err := resolveDataSource(target["datasource"], panel["datasource"], dataSources, def, variables)
				if err != nil {
					reasons = append(reasons, "查询 "+strv(target["refId"])+"："+err.Error())
					item.Status = "unsupported"
					continue
				}
				if ds.Type == "prometheus" && strv(target["format"]) == "heatmap" {
					reasons = append(reasons, "查询 "+strv(target["refId"])+" 的热力图格式按时序数据显示")
				}
			}
		}
		if kind == "logs" {
			if d := strv(obj(panel["options"])["dedupStrategy"]); d != "" && d != "none" {
				reasons = append(reasons, "日志去重方式 "+d+" 未实现")
			}
		}
		if kind == "text" && strv(obj(panel["options"])["mode"]) == "html" {
			reasons = append(reasons, "HTML 模式按纯文本显示（平台不执行面板中的 HTML）")
		}
		if item.Status == "supported" && len(reasons) > 0 {
			item.Status = "partial"
		}
		item.Reasons = reasons
		report.Panels = append(report.Panels, item)
	}
	for _, a := range arr(obj(dash["annotations"])["list"]) {
		annotation := obj(a)
		if annotation != nil && annotation["builtIn"] != float64(1) && annotation["enable"] != false {
			report.Notes = append(report.Notes, "注释查询“"+strv(annotation["name"])+"”未在平台中显示")
		}
	}
	if len(arr(dash["links"])) > 0 {
		report.Notes = append(report.Notes, "仪表盘链接未显示")
	}
	for _, r := range arr(dash["__requires"]) {
		req := obj(r)
		if req != nil {
			report.Requires = append(report.Requires, strv(req["type"])+":"+strv(req["id"]))
		}
	}
	report.Level = "full"
	for _, list := range [][]SupportItem{report.Panels, report.Variables} {
		for _, item := range list {
			switch item.Status {
			case "unsupported":
				report.Level = "limited"
			case "partial":
				if report.Level == "full" {
					report.Level = "partial"
				}
			}
		}
	}
	if report.Level == "full" && len(report.Notes) > 0 {
		report.Level = "partial"
	}
	return report
}

var dsVarPattern = regexp.MustCompile(`^\$\{?([A-Za-z0-9_]+)(?::[a-z]+)?\}?$`)

// resolveDataSource follows Grafana's rules: target datasource, then panel
// datasource, then the default; references may be names, {type,uid} objects
// or datasource variables.
func resolveDataSource(ref, fallback any, index map[string]model.OpsDataSource, def model.OpsDataSource, variables map[string]map[string]any) (model.OpsDataSource, error) {
	if ref == nil || (obj(ref) != nil && strv(obj(ref)["uid"]) == "" && strv(obj(ref)["type"]) == "") {
		if fallback != nil {
			return resolveDataSource(fallback, nil, index, def, variables)
		}
		if def.UID == "" {
			return model.OpsDataSource{}, errors.New("未设置数据源且 Grafana 没有默认数据源")
		}
		return checkSupported(def)
	}
	uid, name := "", ""
	switch v := ref.(type) {
	case string:
		name = v
	case map[string]any:
		uid = strv(v["uid"])
	}
	key := firstNonEmpty(uid, name)
	switch key {
	case "-- Mixed --":
		return model.OpsDataSource{}, errors.New("混合数据源需要在每个查询上指定数据源")
	case "-- Grafana --", "grafana", "-- Dashboard --":
		return model.OpsDataSource{}, errors.New("Grafana 内置数据源暂不支持")
	}
	if m := dsVarPattern.FindStringSubmatch(key); m != nil {
		variable := variables[m[1]]
		if variable == nil {
			return model.OpsDataSource{}, fmt.Errorf("数据源变量 %s 不存在（导入时请映射数据源）", key)
		}
		current := obj(variable["current"])
		value := strv(current["value"])
		if value == "" {
			if vs := arr(current["value"]); len(vs) > 0 {
				value = strv(vs[0])
			}
		}
		if value == "" || value == "default" {
			for _, ds := range index {
				if ds.Type == strv(variable["query"]) {
					return checkSupported(ds)
				}
			}
			return model.OpsDataSource{}, fmt.Errorf("数据源变量 %s 没有可用的数据源", key)
		}
		key = value
	}
	if ds, ok := index[key]; ok {
		return checkSupported(ds)
	}
	if ds, ok := index["name:"+key]; ok {
		return checkSupported(ds)
	}
	return model.OpsDataSource{}, fmt.Errorf("数据源 %s 不存在", key)
}

func checkSupported(ds model.OpsDataSource) (model.OpsDataSource, error) {
	if !ds.Supported {
		return ds, fmt.Errorf("数据源类型 %s 暂不支持（仅支持 Prometheus 与 Loki）", ds.Type)
	}
	return ds, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

type DashboardListInput struct {
	Query     string
	FolderUID string
	Tag       string
	Favorites bool
}

func (s *Service) ListDashboards(ctx context.Context, in DashboardListInput, tenant, user string) ([]model.OpsDashboardSummary, error) {
	if err := requireBackend(s.Dashboards); err != nil {
		return nil, err
	}
	favorites, err := s.favoriteSet(ctx, tenant, user)
	if err != nil {
		return nil, err
	}
	q := ports.DashboardSearch{Query: strings.TrimSpace(in.Query), Limit: 1000}
	if in.FolderUID != "" {
		q.FolderUIDs = []string{in.FolderUID}
	}
	if in.Tag != "" {
		q.Tags = []string{in.Tag}
	}
	if in.Favorites {
		if len(favorites) == 0 {
			return []model.OpsDashboardSummary{}, nil
		}
		for uid := range favorites {
			q.UIDs = append(q.UIDs, uid)
		}
		sort.Strings(q.UIDs)
	}
	items, err := s.Dashboards.SearchDashboards(ctx, q)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].Favorite = favorites[items[i].UID]
	}
	return items, nil
}

type DashboardView struct {
	Dashboard map[string]any `json:"dashboard"`
	Meta      map[string]any `json:"meta"`
	Support   SupportReport  `json:"support"`
	Favorite  bool           `json:"favorite"`
}

func (s *Service) GetDashboard(ctx context.Context, uid, tenant, user string) (DashboardView, error) {
	if err := requireBackend(s.Dashboards); err != nil {
		return DashboardView{}, err
	}
	if !dashboardUIDPattern.MatchString(uid) {
		return DashboardView{}, ports.ErrOpsNotFound
	}
	dash, meta, err := s.Dashboards.GetDashboard(ctx, uid)
	if err != nil {
		return DashboardView{}, err
	}
	index, def, err := s.dataSourceIndex(ctx)
	if err != nil {
		return DashboardView{}, err
	}
	favorites, _ := s.favoriteSet(ctx, tenant, user)
	return DashboardView{Dashboard: dash, Meta: meta, Support: AnalyzeDashboard(dash, index, def), Favorite: favorites[uid]}, nil
}

type DashboardSaveInput struct {
	Dashboard map[string]any `json:"dashboard"`
	FolderUID string         `json:"folderUid"`
	Message   string         `json:"message"`
	Overwrite bool           `json:"overwrite"`
}

func validateDashboard(dash map[string]any) error {
	if dash == nil {
		return invalid("dashboard", "缺少仪表盘内容")
	}
	title := strings.TrimSpace(strv(dash["title"]))
	if title == "" || len([]rune(title)) > 200 {
		return invalid("dashboard.title", "仪表盘标题为 1～200 个字符")
	}
	if uid := strv(dash["uid"]); uid != "" && !dashboardUIDPattern.MatchString(uid) {
		return invalid("dashboard.uid", "仪表盘 UID 只能包含字母、数字、下划线和短横线，最长 40 个字符")
	}
	if p := dash["panels"]; p != nil && arr(p) == nil {
		return invalid("dashboard.panels", "panels 必须是数组")
	}
	if len(allPanels(dash)) > 200 {
		return invalid("dashboard.panels", "面板数量不能超过 200")
	}
	return nil
}

// SaveDashboard creates or updates a dashboard in Grafana. Grafana's version
// check turns concurrent edits into a conflict instead of a silent overwrite.
func (s *Service) SaveDashboard(ctx context.Context, uid string, in DashboardSaveInput) (ports.DashboardSaveResult, error) {
	if err := requireBackend(s.Dashboards); err != nil {
		return ports.DashboardSaveResult{}, err
	}
	if err := validateDashboard(in.Dashboard); err != nil {
		return ports.DashboardSaveResult{}, err
	}
	if uid != "" && strv(in.Dashboard["uid"]) != uid {
		return ports.DashboardSaveResult{}, invalid("dashboard.uid", "仪表盘 UID 与路径不一致")
	}
	if uid == "" {
		in.Dashboard["id"] = nil
		in.Overwrite = false
	}
	message := strings.TrimSpace(in.Message)
	if len([]rune(message)) > 500 {
		return ports.DashboardSaveResult{}, invalid("message", "保存说明不能超过 500 字")
	}
	result, err := s.Dashboards.SaveDashboard(ctx, in.Dashboard, in.FolderUID, message, in.Overwrite)
	s.invalidateDashboard(uid)
	return result, err
}

func (s *Service) CopyDashboard(ctx context.Context, uid, title, folderUID string) (ports.DashboardSaveResult, error) {
	if err := requireBackend(s.Dashboards); err != nil {
		return ports.DashboardSaveResult{}, err
	}
	dash, meta, err := s.Dashboards.GetDashboard(ctx, uid)
	if err != nil {
		return ports.DashboardSaveResult{}, err
	}
	dash["id"], dash["uid"], dash["version"] = nil, nil, 0
	if title = strings.TrimSpace(title); title == "" {
		title = strv(dash["title"]) + " 副本"
	}
	dash["title"] = title
	if folderUID == "" {
		folderUID = strv(meta["folderUid"])
	}
	if err := validateDashboard(dash); err != nil {
		return ports.DashboardSaveResult{}, err
	}
	return s.Dashboards.SaveDashboard(ctx, dash, folderUID, "复制自 "+uid, false)
}

func (s *Service) DeleteDashboard(ctx context.Context, uid string) error {
	if err := requireBackend(s.Dashboards); err != nil {
		return err
	}
	if !dashboardUIDPattern.MatchString(uid) {
		return ports.ErrOpsNotFound
	}
	s.invalidateDashboard(uid)
	return s.Dashboards.DeleteDashboard(ctx, uid)
}

type ImportInput struct {
	Dashboard map[string]any    `json:"dashboard"`
	FolderUID string            `json:"folderUid"`
	Inputs    map[string]string `json:"inputs"`
	Overwrite bool              `json:"overwrite"`
	NewUID    bool              `json:"newUid"`
	DryRun    bool              `json:"dryRun"`
	Template  string            `json:"template"`
}

type ImportInputSpec struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Type        string `json:"type"`
	PluginID    string `json:"pluginId,omitempty"`
	Value       string `json:"value,omitempty"`
	Description string `json:"description,omitempty"`
}

type ImportResult struct {
	Support  SupportReport              `json:"support"`
	Inputs   []ImportInputSpec          `json:"inputs"`
	Existing *model.OpsDashboardSummary `json:"existing,omitempty"`
	Title    string                     `json:"title"`
	UID      string                     `json:"uid,omitempty"`
	Saved    *ports.DashboardSaveResult `json:"saved,omitempty"`
}

var inputRefPattern = regexp.MustCompile(`\$\{([A-Za-z0-9_]+)\}`)

// ImportDashboard analyses (dryRun) or imports an exported Grafana dashboard.
// Data source inputs must be mapped to existing supported data sources; the
// full JSON, including parts the platform cannot render, is stored in Grafana.
func (s *Service) ImportDashboard(ctx context.Context, in ImportInput) (ImportResult, error) {
	if err := requireBackend(s.Dashboards); err != nil {
		return ImportResult{}, err
	}
	if in.Template != "" {
		dash, err := dashboardTemplate(in.Template)
		if err != nil {
			return ImportResult{}, err
		}
		in.Dashboard = dash
	}
	if in.Dashboard == nil {
		return ImportResult{}, invalid("dashboard", "请提供仪表盘 JSON")
	}
	if inner := obj(in.Dashboard["dashboard"]); inner != nil && in.Dashboard["title"] == nil {
		in.Dashboard = inner
	}
	index, def, err := s.dataSourceIndex(ctx)
	if err != nil {
		return ImportResult{}, err
	}
	specs := []ImportInputSpec{}
	values := map[string]string{}
	for _, raw := range arr(in.Dashboard["__inputs"]) {
		input := obj(raw)
		spec := ImportInputSpec{Name: strv(input["name"]), Label: strv(input["label"]), Type: strv(input["type"]), PluginID: strv(input["pluginId"]), Description: strv(input["description"])}
		switch spec.Type {
		case "datasource":
			value := in.Inputs[spec.Name]
			if value == "" {
				for _, ds := range index {
					if ds.Type == spec.PluginID && ds.IsDefault {
						value = ds.UID
					}
				}
				if value == "" {
					for _, ds := range index {
						if ds.Type == spec.PluginID {
							value = ds.UID
							break
						}
					}
				}
			}
			if ds, ok := index[value]; ok && ds.Type == spec.PluginID {
				spec.Value = value
			}
			values[spec.Name] = spec.Value
		case "constant":
			spec.Value = firstNonEmpty(in.Inputs[spec.Name], strv(input["value"]))
			values[spec.Name] = spec.Value
		default:
			return ImportResult{}, invalid("inputs", "不支持的导入参数类型 %s", spec.Type)
		}
		specs = append(specs, spec)
	}
	dash := replaceInputs(in.Dashboard, values).(map[string]any)
	delete(dash, "__inputs")
	delete(dash, "__elements")
	dash["id"] = nil
	if in.NewUID {
		dash["uid"] = nil
	}
	if err := validateDashboard(dash); err != nil {
		return ImportResult{}, err
	}
	result := ImportResult{Support: AnalyzeDashboard(dash, index, def), Inputs: specs, Title: strv(dash["title"]), UID: strv(dash["uid"])}
	if result.UID != "" {
		if hits, err := s.Dashboards.SearchDashboards(ctx, ports.DashboardSearch{UIDs: []string{result.UID}, Limit: 1}); err == nil && len(hits) > 0 {
			result.Existing = &hits[0]
		}
	}
	if in.DryRun {
		return result, nil
	}
	for _, spec := range specs {
		if spec.Type == "datasource" && spec.Value == "" {
			return result, invalid("inputs", "请为 %s 选择 %s 类型的数据源", firstNonEmpty(spec.Label, spec.Name), spec.PluginID)
		}
	}
	if result.Existing != nil && !in.Overwrite {
		return result, fmt.Errorf("%w: 已存在 UID 相同的仪表盘“%s”，请选择覆盖或作为新仪表盘导入", ports.ErrOpsConflict, result.Existing.Title)
	}
	delete(dash, "__requires")
	delete(dash, "version")
	saved, err := s.Dashboards.SaveDashboard(ctx, dash, in.FolderUID, "通过炬联运维中心导入", in.Overwrite)
	if err != nil {
		return result, err
	}
	s.invalidateDashboard(saved.UID)
	result.Saved = &saved
	return result, nil
}

func replaceInputs(v any, values map[string]string) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, child := range t {
			out[k] = replaceInputs(child, values)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, child := range t {
			out[i] = replaceInputs(child, values)
		}
		return out
	case string:
		return inputRefPattern.ReplaceAllStringFunc(t, func(ref string) string {
			name := inputRefPattern.FindStringSubmatch(ref)[1]
			if value, ok := values[name]; ok && value != "" {
				return value
			}
			return ref
		})
	}
	return v
}

// ExportDashboard returns the JSON model. With external=true data source
// references are replaced by ${DS_...} inputs so another Grafana (or another
// platform installation) can map them on import.
func (s *Service) ExportDashboard(ctx context.Context, uid string, external bool) (map[string]any, error) {
	if err := requireBackend(s.Dashboards); err != nil {
		return nil, err
	}
	dash, _, err := s.Dashboards.GetDashboard(ctx, uid)
	if err != nil {
		return nil, err
	}
	dash["id"] = nil
	if !external {
		return dash, nil
	}
	index, _, err := s.dataSourceIndex(ctx)
	if err != nil {
		return nil, err
	}
	inputs := map[string]map[string]any{}
	var walk func(any) any
	walk = func(v any) any {
		switch t := v.(type) {
		case map[string]any:
			if uid := strv(t["uid"]); uid != "" && strv(t["type"]) != "" && len(t) <= 3 {
				if ds, ok := index[uid]; ok {
					name := "DS_" + strings.ToUpper(regexp.MustCompile(`[^A-Za-z0-9]+`).ReplaceAllString(ds.Name, "_"))
					inputs[name] = map[string]any{"name": name, "label": ds.Name, "description": "", "type": "datasource", "pluginId": ds.Type, "pluginName": ds.Type}
					return map[string]any{"type": ds.Type, "uid": "${" + name + "}"}
				}
			}
			out := make(map[string]any, len(t))
			for k, child := range t {
				out[k] = walk(child)
			}
			return out
		case []any:
			out := make([]any, len(t))
			for i, child := range t {
				out[i] = walk(child)
			}
			return out
		}
		return v
	}
	exported := walk(dash).(map[string]any)
	names := make([]string, 0, len(inputs))
	for name := range inputs {
		names = append(names, name)
	}
	sort.Strings(names)
	list := []any{}
	requires := []any{}
	seenPlugin := map[string]bool{}
	for _, name := range names {
		list = append(list, inputs[name])
		plugin := strv(inputs[name]["pluginId"])
		if !seenPlugin[plugin] {
			seenPlugin[plugin] = true
			requires = append(requires, map[string]any{"type": "datasource", "id": plugin, "name": plugin})
		}
	}
	for _, panel := range allPanels(dash) {
		kind := strv(panel["type"])
		if kind != "" && kind != "row" && !seenPlugin["panel:"+kind] {
			seenPlugin["panel:"+kind] = true
			requires = append(requires, map[string]any{"type": "panel", "id": kind, "name": kind})
		}
	}
	exported["__inputs"], exported["__requires"] = list, requires
	return exported, nil
}

func (s *Service) Folders(ctx context.Context) ([]model.OpsFolder, error) {
	if err := requireBackend(s.Dashboards); err != nil {
		return nil, err
	}
	return s.Dashboards.Folders(ctx)
}

func (s *Service) SaveFolder(ctx context.Context, uid, title string) (model.OpsFolder, error) {
	if err := requireBackend(s.Dashboards); err != nil {
		return model.OpsFolder{}, err
	}
	title = strings.TrimSpace(title)
	if title == "" || len([]rune(title)) > 100 {
		return model.OpsFolder{}, invalid("title", "文件夹名称为 1～100 个字符")
	}
	if uid != "" && !dashboardUIDPattern.MatchString(uid) {
		return model.OpsFolder{}, ports.ErrOpsNotFound
	}
	return s.Dashboards.SaveFolder(ctx, uid, title, 0)
}

// DeleteFolder refuses non-empty folders: deleting a Grafana folder also
// deletes its dashboards, which must never happen implicitly.
func (s *Service) DeleteFolder(ctx context.Context, uid string) error {
	if err := requireBackend(s.Dashboards); err != nil {
		return err
	}
	if !dashboardUIDPattern.MatchString(uid) {
		return ports.ErrOpsNotFound
	}
	items, err := s.Dashboards.SearchDashboards(ctx, ports.DashboardSearch{FolderUIDs: []string{uid}, Limit: 1})
	if err != nil {
		return err
	}
	folders, err := s.Dashboards.Folders(ctx)
	if err != nil {
		return err
	}
	for _, f := range folders {
		if f.ParentUID == uid {
			return invalid("folder", "文件夹下还有子文件夹，请先移走或删除")
		}
	}
	if len(items) > 0 {
		return invalid("folder", "文件夹不为空，请先移走或删除其中的仪表盘")
	}
	return s.Dashboards.DeleteFolder(ctx, uid)
}

// Small read-through caches keep panel refreshes from re-reading the same
// dashboard and data source list from Grafana for every panel.
type cacheEntry struct {
	value   any
	expires time.Time
}

var (
	cacheMu sync.Mutex
	cache   = map[string]cacheEntry{}
)

func cacheGet(key string) (any, bool) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	e, ok := cache[key]
	if !ok || time.Now().After(e.expires) {
		delete(cache, key)
		return nil, false
	}
	return e.value, true
}

func cachePut(key string, value any, ttl time.Duration) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if len(cache) > 500 {
		for k, e := range cache {
			if time.Now().After(e.expires) {
				delete(cache, k)
			}
		}
	}
	cache[key] = cacheEntry{value: value, expires: time.Now().Add(ttl)}
}

func (s *Service) invalidateDashboard(uid string) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	for k := range cache {
		if strings.HasPrefix(k, "dash:"+uid) || strings.HasPrefix(k, "ds:") || strings.HasPrefix(k, "var:"+uid) {
			delete(cache, k)
		}
	}
}

func (s *Service) cachedDataSources(ctx context.Context) ([]model.OpsDataSource, error) {
	if v, ok := cacheGet("ds:list"); ok {
		return v.([]model.OpsDataSource), nil
	}
	items, err := s.Dashboards.DataSources(ctx)
	if err != nil {
		return nil, err
	}
	cachePut("ds:list", items, 10*time.Second)
	return items, nil
}

func (s *Service) cachedDashboard(ctx context.Context, uid string) (map[string]any, error) {
	if v, ok := cacheGet("dash:" + uid); ok {
		return deepCopy(v.(map[string]any)), nil
	}
	dash, _, err := s.Dashboards.GetDashboard(ctx, uid)
	if err != nil {
		return nil, err
	}
	cachePut("dash:"+uid, dash, 5*time.Second)
	return deepCopy(dash), nil
}

func deepCopy(v map[string]any) map[string]any {
	b, _ := json.Marshal(v)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}

//go:embed templates/*.json
var templateFS embed.FS

type DashboardTemplate struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

var templateCatalog = []DashboardTemplate{
	{ID: "platform-overview", Title: "炬联平台运行", Description: "上报、解析、归档、队列积压、AI 调用与备份指标"},
	{ID: "host-resources", Title: "主机资源", Description: "node-exporter 采集的 CPU、内存、磁盘与网络"},
	{ID: "logs-overview", Title: "服务日志概览", Description: "各服务日志量、错误日志趋势与最新错误日志"},
}

func DashboardTemplates() []DashboardTemplate { return templateCatalog }

func dashboardTemplate(id string) (map[string]any, error) {
	for _, t := range templateCatalog {
		if t.ID == id {
			data, err := templateFS.ReadFile("templates/" + id + ".json")
			if err != nil {
				return nil, err
			}
			var dash map[string]any
			if err := json.Unmarshal(data, &dash); err != nil {
				return nil, err
			}
			return dash, nil
		}
	}
	return nil, invalid("template", "内置模板不存在")
}
