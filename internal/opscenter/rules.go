package opscenter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"text/template"
	"time"
	"unicode"

	"gopkg.in/yaml.v3"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// Rule management: Prometheus evaluates metric rules and the Loki ruler
// evaluates log alert rules. Neither exposes a writable rule API in this
// deployment, so the platform writes one YAML file per group into a directory
// the component loads (Prometheus auto-reload / Loki ruler polling), then
// confirms the component loaded it and restores the previous file otherwise.

const (
	SourcePrometheus = "prometheus"
	SourceLoki       = "loki"
	managedHeader    = "# 由炬联运维中心管理；手工修改会在下次保存时被覆盖。"
)

var promDurationPattern = regexp.MustCompile(`^(([0-9]+)y)?(([0-9]+)w)?(([0-9]+)d)?(([0-9]+)h)?(([0-9]+)m)?(([0-9]+)s)?(([0-9]+)ms)?$`)

type RuleInput struct {
	Kind          string            `json:"kind"`
	Name          string            `json:"name"`
	Expr          string            `json:"expr"`
	For           string            `json:"for"`
	KeepFiringFor string            `json:"keepFiringFor"`
	Labels        map[string]string `json:"labels"`
	Annotations   map[string]string `json:"annotations"`
}

type RuleGroupInput struct {
	Name     string      `json:"name"`
	Interval string      `json:"interval"`
	Limit    int         `json:"limit"`
	Enabled  bool        `json:"enabled"`
	Revision string      `json:"revision"`
	Rules    []RuleInput `json:"rules"`
}

type ruleFileYAML struct {
	Groups []ruleGroupYAML `yaml:"groups"`
}

type ruleGroupYAML struct {
	Name     string     `yaml:"name"`
	Interval string     `yaml:"interval,omitempty"`
	Limit    int        `yaml:"limit,omitempty"`
	Rules    []ruleYAML `yaml:"rules"`
}

type ruleYAML struct {
	Record        string            `yaml:"record,omitempty"`
	Alert         string            `yaml:"alert,omitempty"`
	Expr          string            `yaml:"expr"`
	For           string            `yaml:"for,omitempty"`
	KeepFiringFor string            `yaml:"keep_firing_for,omitempty"`
	Labels        map[string]string `yaml:"labels,omitempty"`
	Annotations   map[string]string `yaml:"annotations,omitempty"`
}

type ruleTarget struct {
	source  string
	store   ports.ManagedFileStore
	ext     string
	backend interface {
		Configured() bool
		FormatQuery(context.Context, string) (string, error)
		RuleGroups(context.Context) ([]model.OpsRuleGroup, error)
	}
}

func (s *Service) ruleTarget(source string) (ruleTarget, error) {
	switch source {
	case SourcePrometheus:
		if !configured(s.Metrics) {
			return ruleTarget{}, ports.ErrOpsNotConfigured
		}
		return ruleTarget{source: source, store: s.PromRules, ext: ".yml", backend: s.Metrics}, nil
	case SourceLoki:
		if !configured(s.Logs) {
			return ruleTarget{}, ports.ErrOpsNotConfigured
		}
		return ruleTarget{source: source, store: s.LokiRules, ext: ".yaml", backend: s.Logs}, nil
	}
	return ruleTarget{}, invalid("source", "未知的规则来源")
}

func (s *Service) ruleLock(source string) func() {
	mu := &s.promMu
	if source == SourceLoki {
		mu = &s.lokiMu
	}
	mu.Lock()
	return mu.Unlock
}

// ruleFileName maps an arbitrary group name to a stable, path-safe file name.
func ruleFileName(groupName string) string {
	sum := sha256.Sum256([]byte(groupName))
	return "g-" + hex.EncodeToString(sum[:8])
}

func parseDuration(value string) (time.Duration, bool) {
	if value == "" || value == "0" || !promDurationPattern.MatchString(value) {
		return 0, value == "0"
	}
	var total time.Duration
	units := map[string]time.Duration{"y": 365 * 24 * time.Hour, "w": 7 * 24 * time.Hour, "d": 24 * time.Hour, "h": time.Hour, "m": time.Minute, "s": time.Second, "ms": time.Millisecond}
	for _, part := range regexp.MustCompile(`([0-9]+)(ms|y|w|d|h|m|s)`).FindAllStringSubmatch(value, -1) {
		var n int64
		fmt.Sscan(part[1], &n)
		total += time.Duration(n) * units[part[2]]
	}
	return total, true
}

// templateFuncs mirrors the function names Prometheus and Loki make available
// to alert templates so parsing accepts valid templates.
var templateFuncs = template.FuncMap{}

func init() {
	for _, name := range []string{"humanize", "humanize1024", "humanizeDuration", "humanizePercentage", "humanizeTimestamp", "toTime", "query", "first", "label", "value", "strvalue", "sortByLabel", "args", "safeHtml", "match", "reReplaceAll", "title", "toUpper", "toLower", "graphLink", "tableLink", "stripPort", "stripDomain", "parseDuration", "pathPrefix", "externalURL", "now", "tmpl"} {
		templateFuncs[name] = func(...any) string { return "" }
	}
}

func checkTemplate(field, text string) error {
	if !strings.Contains(text, "{{") {
		return nil
	}
	// Prometheus and Loki predefine these variables before expanding templates.
	defs := "{{$labels := .Labels}}{{$externalLabels := .ExternalLabels}}{{$externalURL := .ExternalURL}}{{$value := .Value}}"
	if _, err := template.New("rule").Funcs(templateFuncs).Option("missingkey=zero").Parse(defs + text); err != nil {
		return invalid(field, "模板语法错误：%s", strings.TrimPrefix(err.Error(), "template: rule:"))
	}
	return nil
}

func validGroupName(name string) bool {
	if name == "" || len([]rune(name)) > 100 {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == '/' || r == '\\' {
			return false
		}
	}
	return true
}

// normalize validates a group definition and returns its YAML model.
func (s *Service) normalizeGroup(ctx context.Context, target ruleTarget, in RuleGroupInput) (ruleGroupYAML, error) {
	in.Name = strings.TrimSpace(in.Name)
	if !validGroupName(in.Name) {
		return ruleGroupYAML{}, invalid("name", "规则组名称为 1～100 个字符，且不能包含斜杠或控制字符")
	}
	group := ruleGroupYAML{Name: in.Name, Interval: strings.TrimSpace(in.Interval), Limit: in.Limit}
	if group.Interval != "" {
		if d, ok := parseDuration(group.Interval); !ok || d < time.Second {
			return group, invalid("interval", "评估间隔格式无效，例如 30s、1m")
		}
	}
	if in.Limit < 0 || in.Limit > 10000 {
		return group, invalid("limit", "告警数量上限必须在 0～10000 之间")
	}
	if len(in.Rules) == 0 || len(in.Rules) > 200 {
		return group, invalid("rules", "每个规则组需要 1～200 条规则")
	}
	seen := map[string]bool{}
	for i, r := range in.Rules {
		field := fmt.Sprintf("rules[%d]", i)
		r.Name, r.Expr = strings.TrimSpace(r.Name), strings.TrimSpace(r.Expr)
		if err := checkQueryText(field+".expr", r.Expr); err != nil {
			return group, err
		}
		item := ruleYAML{Expr: r.Expr}
		switch r.Kind {
		case "record":
			if target.source == SourceLoki {
				return group, invalid(field+".kind", "日志规则只支持告警规则（记录规则需要额外的 remote write 配置）")
			}
			if !validMetricName(r.Name) {
				return group, invalid(field+".name", "记录规则名称必须是合法的指标名，例如 job:up:sum")
			}
			if r.For != "" || r.KeepFiringFor != "" || len(r.Annotations) > 0 {
				return group, invalid(field, "记录规则不支持持续时间和注释")
			}
			item.Record = r.Name
		case "alert":
			if !validMetricName(r.Name) {
				return group, invalid(field+".name", "告警名称只能包含字母、数字、下划线和冒号，中文说明请写在注释中")
			}
			for _, d := range []struct{ field, value string }{{"for", r.For}, {"keepFiringFor", r.KeepFiringFor}} {
				if d.value != "" {
					if _, ok := parseDuration(d.value); !ok {
						return group, invalid(field+"."+d.field, "持续时间格式无效，例如 5m")
					}
				}
			}
			item.Alert, item.For, item.KeepFiringFor = r.Name, r.For, r.KeepFiringFor
		default:
			return group, invalid(field+".kind", "规则类型必须是告警规则或记录规则")
		}
		key := r.Kind + "\x00" + r.Name
		if seen[key] && r.Kind == "record" {
			return group, invalid(field+".name", "同一规则组内记录规则名称重复")
		}
		seen[key] = true
		labels, err := cleanRuleMap(field+".labels", r.Labels, 1024)
		if err != nil {
			return group, err
		}
		annotations, err := cleanRuleMap(field+".annotations", r.Annotations, 4096)
		if err != nil {
			return group, err
		}
		if item.Record != "" && len(annotations) > 0 {
			return group, invalid(field, "记录规则不支持注释")
		}
		item.Labels, item.Annotations = labels, annotations
		if _, err := target.backend.FormatQuery(ctx, r.Expr); err != nil {
			var upstream *ports.OpsUpstreamError
			if errors.As(err, &upstream) {
				return group, invalid(field+".expr", "表达式无效：%s", upstream.Message)
			}
			return group, err
		}
		if target.source == SourceLoki {
			if err := s.lokiSampleExpr(ctx, field+".expr", r.Expr); err != nil {
				return group, err
			}
		}
		group.Rules = append(group.Rules, item)
	}
	return group, nil
}

// lokiSampleExpr rejects log-line selectors in Loki rules. The ruler loads
// them but can never evaluate them, so the platform asks Loki to run the
// expression as an instant query: log queries are refused with 400 and
// sample queries return a vector. Transient failures do not block a save;
// the ruler still reports evaluation errors on the rule itself.
func (s *Service) lokiSampleExpr(ctx context.Context, field, expr string) error {
	now := s.now()
	result, err := s.Logs.Query(ctx, ports.LogQuery{Query: expr, Start: now.Add(-time.Minute), End: now, Instant: true, Limit: 1})
	var upstream *ports.OpsUpstreamError
	switch {
	case errors.As(err, &upstream) && upstream.Status >= 400 && upstream.Status < 500:
		return invalid(field, "日志规则必须是统计查询（例如 count_over_time、rate 或 sum by (...)），Loki 返回：%s", upstream.Message)
	case err != nil:
		return nil
	case result.ResultType == "streams":
		return invalid(field, "日志规则必须是统计查询（例如 count_over_time、rate 或 sum by (...)），不能只筛选日志行")
	}
	return nil
}

func cleanRuleMap(field string, values map[string]string, maxValue int) (map[string]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	if len(values) > 50 {
		return nil, invalid(field, "最多 50 项")
	}
	out := map[string]string{}
	for k, v := range values {
		k = strings.TrimSpace(k)
		if !validLabelName(k) || strings.HasPrefix(k, "__") {
			return nil, invalid(field, "名称 %q 不合法", k)
		}
		if len(v) > maxValue {
			return nil, invalid(field, "%s 的值过长", k)
		}
		if err := checkTemplate(field+"."+k, v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, nil
}

func renderRuleFile(group ruleGroupYAML, actor string, now time.Time) ([]byte, error) {
	body, err := yaml.Marshal(ruleFileYAML{Groups: []ruleGroupYAML{group}})
	if err != nil {
		return nil, err
	}
	return withHeader(body, actor, now), nil
}

// withHeader stamps the managed-file header. The timestamp also guarantees a
// content change, which Prometheus auto-reload needs to reload after a rollback.
func withHeader(body []byte, actor string, now time.Time) []byte {
	var b bytes.Buffer
	b.WriteString(managedHeader + "\n")
	b.WriteString("# updated-by: " + strings.ReplaceAll(actor, "\n", " ") + "\n")
	b.WriteString("# updated-at: " + now.UTC().Format(time.RFC3339Nano) + "\n")
	b.Write(stripHeader(body))
	return b.Bytes()
}

func stripHeader(body []byte) []byte {
	lines := bytes.SplitAfter(body, []byte("\n"))
	i := 0
	for i < len(lines) && (bytes.HasPrefix(lines[i], []byte("# 由炬联")) || bytes.HasPrefix(lines[i], []byte("# updated-"))) {
		i++
	}
	return bytes.Join(lines[i:], nil)
}

func headerValue(body []byte, key string) string {
	for _, line := range strings.SplitN(string(body), "\n", 5) {
		if strings.HasPrefix(line, "# "+key+": ") {
			return strings.TrimPrefix(line, "# "+key+": ")
		}
	}
	return ""
}

func parseManagedGroup(file ports.ManagedFile) (model.OpsRuleGroup, error) {
	var parsed ruleFileYAML
	if err := yaml.Unmarshal(file.Content, &parsed); err != nil || len(parsed.Groups) != 1 {
		return model.OpsRuleGroup{}, fmt.Errorf("managed rule file %s is not a single rule group", file.Name)
	}
	g := parsed.Groups[0]
	out := model.OpsRuleGroup{Name: g.Name, Interval: g.Interval, Limit: g.Limit, Managed: true, Enabled: file.Enabled, Revision: file.Revision, UpdatedBy: headerValue(file.Content, "updated-by"), Rules: []model.OpsRule{}}
	if t, err := time.Parse(time.RFC3339Nano, headerValue(file.Content, "updated-at")); err == nil {
		out.UpdatedAt = t.UnixMilli()
	} else {
		out.UpdatedAt = file.ModTime.UnixMilli()
	}
	for _, r := range g.Rules {
		rule := model.OpsRule{Expr: r.Expr, For: r.For, KeepFiringFor: r.KeepFiringFor, Labels: r.Labels, Annotations: r.Annotations}
		if r.Record != "" {
			rule.Kind, rule.Name = "record", r.Record
		} else {
			rule.Kind, rule.Name = "alert", r.Alert
		}
		out.Rules = append(out.Rules, rule)
	}
	return out, nil
}

// RuleGroups merges platform-managed files with the groups the component has
// actually loaded, including built-in groups from the deployment config.
func (s *Service) RuleGroups(ctx context.Context, source string) ([]model.OpsRuleGroup, error) {
	target, err := s.ruleTarget(source)
	if err != nil {
		return nil, err
	}
	loaded, err := target.backend.RuleGroups(ctx)
	if err != nil {
		return nil, err
	}
	out := []model.OpsRuleGroup{}
	managedFiles := map[string]bool{}
	if configured(target.store) {
		files, err := target.store.List()
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			group, err := parseManagedGroup(file)
			if err != nil {
				s.logger().Warn("skip unreadable managed rule file", "source", source, "file", file.Name, "error", err)
				continue
			}
			group.Source, group.File = source, file.Name+target.ext
			managedFiles[group.File] = true
			for _, runtime := range loaded {
				if runtime.Name == group.Name && path.Base(runtime.File) == group.File {
					group.Loaded = true
					mergeRuntime(&group, runtime)
				}
			}
			out = append(out, group)
		}
	}
	for _, runtime := range loaded {
		if managedFiles[path.Base(runtime.File)] {
			continue
		}
		runtime.File = path.Base(runtime.File)
		out = append(out, runtime)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Managed != out[j].Managed {
			return !out[i].Managed
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func mergeRuntime(group *model.OpsRuleGroup, runtime model.OpsRuleGroup) {
	byName := map[string]model.OpsRule{}
	for _, r := range runtime.Rules {
		byName[r.Kind+"\x00"+r.Name] = r
	}
	for i, r := range group.Rules {
		if live, ok := byName[r.Kind+"\x00"+r.Name]; ok {
			group.Rules[i].Health, group.Rules[i].LastError, group.Rules[i].State = live.Health, live.LastError, live.State
			group.Rules[i].LastEvaluation, group.Rules[i].EvaluationTime, group.Rules[i].ActiveAlerts = live.LastEvaluation, live.EvaluationTime, live.ActiveAlerts
		}
	}
}

// SaveRuleGroup creates or replaces a managed group. originalName is empty
// for creation. The change is confirmed against the component or rolled back.
func (s *Service) SaveRuleGroup(ctx context.Context, source, originalName string, in RuleGroupInput, actor string) (model.OpsRuleGroup, error) {
	target, err := s.ruleTarget(source)
	if err != nil {
		return model.OpsRuleGroup{}, err
	}
	if !configured(target.store) {
		return model.OpsRuleGroup{}, fmt.Errorf("%w: 未配置规则目录，当前部署只能查看规则", ports.ErrOpsReadOnly)
	}
	group, err := s.normalizeGroup(ctx, target, in)
	if err != nil {
		return model.OpsRuleGroup{}, err
	}
	unlock := s.ruleLock(source)
	defer unlock()
	files, err := target.store.List()
	if err != nil {
		return model.OpsRuleGroup{}, err
	}
	var previous *ports.ManagedFile
	names := map[string]string{}
	for i, file := range files {
		parsed, err := parseManagedGroup(file)
		if err != nil {
			continue
		}
		names[parsed.Name] = file.Name
		if originalName != "" && parsed.Name == originalName {
			previous = &files[i]
		}
	}
	if originalName != "" {
		if previous == nil {
			return model.OpsRuleGroup{}, ports.ErrOpsNotFound
		}
		if in.Revision != "" && in.Revision != previous.Revision {
			return model.OpsRuleGroup{}, ports.ErrOpsConflict
		}
	}
	if existing, ok := names[group.Name]; ok && (previous == nil || existing != previous.Name) {
		return model.OpsRuleGroup{}, invalid("name", "已存在同名规则组")
	}
	if source == SourcePrometheus {
		for _, g := range s.builtinGroupNames(ctx) {
			if g == group.Name {
				return model.OpsRuleGroup{}, invalid("name", "与部署内置规则组重名")
			}
		}
	}
	content, err := renderRuleFile(group, actor, s.now())
	if err != nil {
		return model.OpsRuleGroup{}, err
	}
	fileName := ruleFileName(group.Name)
	changes := []fileChange{{name: fileName, content: content, enabled: in.Enabled}}
	if previous != nil && previous.Name != fileName {
		changes = append(changes, fileChange{name: previous.Name, remove: true})
	}
	before := []ports.ManagedFile{}
	if previous != nil {
		before = append(before, *previous)
	}
	if err := s.applyRuleChanges(ctx, target, changes, before, actor); err != nil {
		return model.OpsRuleGroup{}, err
	}
	saved, err := target.store.Read(fileName)
	if err != nil {
		return model.OpsRuleGroup{}, err
	}
	out, err := parseManagedGroup(saved)
	out.Source, out.File, out.Loaded = source, fileName+target.ext, in.Enabled
	return out, err
}

// SetRuleGroupEnabled moves a group between the loaded and disabled
// locations; the component stops or starts evaluating it.
func (s *Service) SetRuleGroupEnabled(ctx context.Context, source, name string, enabled bool, revision, actor string) error {
	target, err := s.ruleTarget(source)
	if err != nil {
		return err
	}
	if !configured(target.store) {
		return ports.ErrOpsReadOnly
	}
	unlock := s.ruleLock(source)
	defer unlock()
	file, err := s.findManaged(target, name)
	if err != nil {
		return err
	}
	if revision != "" && revision != file.Revision {
		return ports.ErrOpsConflict
	}
	if file.Enabled == enabled {
		return nil
	}
	return s.applyRuleChanges(ctx, target, []fileChange{{name: file.Name, content: withHeader(file.Content, actor, s.now()), enabled: enabled}}, []ports.ManagedFile{file}, actor)
}

func (s *Service) DeleteRuleGroup(ctx context.Context, source, name, revision, actor string) error {
	target, err := s.ruleTarget(source)
	if err != nil {
		return err
	}
	if !configured(target.store) {
		return ports.ErrOpsReadOnly
	}
	unlock := s.ruleLock(source)
	defer unlock()
	file, err := s.findManaged(target, name)
	if err != nil {
		return err
	}
	if revision != "" && revision != file.Revision {
		return ports.ErrOpsConflict
	}
	return s.applyRuleChanges(ctx, target, []fileChange{{name: file.Name, remove: true}}, []ports.ManagedFile{file}, actor)
}

func (s *Service) findManaged(target ruleTarget, name string) (ports.ManagedFile, error) {
	files, err := target.store.List()
	if err != nil {
		return ports.ManagedFile{}, err
	}
	for _, file := range files {
		if parsed, err := parseManagedGroup(file); err == nil && parsed.Name == name {
			return file, nil
		}
	}
	return ports.ManagedFile{}, ports.ErrOpsNotFound
}

func (s *Service) builtinGroupNames(ctx context.Context) []string {
	groups, err := s.Metrics.RuleGroups(ctx)
	if err != nil {
		return nil
	}
	names := []string{}
	for _, g := range groups {
		if !strings.HasPrefix(path.Base(g.File), "g-") {
			names = append(names, g.Name)
		}
	}
	return names
}

type fileChange struct {
	name    string
	content []byte
	enabled bool
	remove  bool
}

// applyRuleChanges writes the files, waits until the component reflects
// them, and restores `before` (and removes newly created files) on failure.
func (s *Service) applyRuleChanges(ctx context.Context, target ruleTarget, changes []fileChange, before []ports.ManagedFile, actor string) error {
	var baseline ports.PrometheusRuntime
	if target.source == SourcePrometheus {
		runtime, err := s.Metrics.Runtime(ctx)
		if err != nil {
			return err
		}
		if !runtime.ReloadSuccess {
			return &ApplyError{Message: "Prometheus 最近一次加载配置失败，请先排查现有配置后再修改规则"}
		}
		baseline = runtime
	}
	written := time.Now()
	for _, c := range changes {
		var err error
		if c.remove {
			err = target.store.Remove(c.name)
			if errors.Is(err, ports.ErrOpsNotFound) {
				err = nil
			}
		} else {
			err = target.store.Write(c.name, c.content, c.enabled)
		}
		if err != nil {
			s.rollbackRules(target, changes, before, actor)
			return err
		}
	}
	if !touchesActive(changes, before) {
		// Only the disabled copy changed: the component's rule set is the
		// same, so there is no reload to wait for.
		return nil
	}
	verifyErr := s.waitRulesApplied(ctx, target, changes, baseline, written)
	if verifyErr == nil {
		return nil
	}
	s.rollbackRules(target, changes, before, actor)
	s.logger().Warn("rule change rolled back", "source", target.source, "error", verifyErr)
	applyErr := &ApplyError{Message: "组件未确认加载新规则，已恢复原有规则", Detail: verifyErr.Error(), RolledBack: true}
	if isCanceled(verifyErr) {
		applyErr.Message = "请求已取消，已恢复原有规则"
	}
	return applyErr
}

// touchesActive reports whether a change adds, edits or removes a file the
// component loads, i.e. anything besides edits to disabled groups.
func touchesActive(changes []fileChange, before []ports.ManagedFile) bool {
	for _, c := range changes {
		if c.enabled && !c.remove {
			return true
		}
		for _, file := range before {
			if file.Name == c.name && file.Enabled {
				return true
			}
		}
	}
	return false
}

func (s *Service) rollbackRules(target ruleTarget, changes []fileChange, before []ports.ManagedFile, actor string) {
	for _, c := range changes {
		if !c.remove {
			_ = target.store.Remove(c.name)
		}
	}
	for _, file := range before {
		_ = target.store.Write(file.Name, withHeader(file.Content, actor+" (rollback)", s.now()), file.Enabled)
	}
}

// waitRulesApplied polls until every changed group is loaded (or absent) as
// expected. Prometheus also reports reload failures, which end the wait early.
func (s *Service) waitRulesApplied(ctx context.Context, target ruleTarget, changes []fileChange, baseline ports.PrometheusRuntime, written time.Time) error {
	expect := map[string]*ruleGroupYAML{}
	for _, c := range changes {
		file := c.name + target.ext
		if c.remove || !c.enabled {
			if _, ok := expect[file]; !ok {
				expect[file] = nil
			}
			continue
		}
		var parsed ruleFileYAML
		if err := yaml.Unmarshal(c.content, &parsed); err != nil || len(parsed.Groups) != 1 {
			return fmt.Errorf("invalid rendered rule file")
		}
		expect[file] = &parsed.Groups[0]
	}
	timeout := s.Limits.ReloadTimeout
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(s.pollInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			if target.source == SourceLoki {
				return fmt.Errorf("Loki ruler 在 %s 内未加载新规则（请确认 ruler.poll_interval 不超过 15s，且规则目录已挂载）", humanDuration(timeout))
			}
			return fmt.Errorf("Prometheus 在 %s 内未重新加载规则（请确认已启用 auto-reload-config 且规则目录已挂载）", humanDuration(timeout))
		case <-ticker.C:
		}
		if target.source == SourcePrometheus {
			runtime, err := s.Metrics.Runtime(ctx)
			if err != nil {
				continue
			}
			if !runtime.ReloadSuccess {
				return fmt.Errorf("Prometheus 拒绝加载新规则，请检查表达式、模板与持续时间")
			}
			if !runtime.LastConfig.After(baseline.LastConfig) && runtime.LastConfig.Before(written.Truncate(time.Second)) {
				continue
			}
		}
		loaded, err := target.backend.RuleGroups(ctx)
		if err != nil {
			continue
		}
		if rulesMatch(loaded, expect) {
			return nil
		}
	}
}

func rulesMatch(loaded []model.OpsRuleGroup, expect map[string]*ruleGroupYAML) bool {
	byFile := map[string]model.OpsRuleGroup{}
	for _, g := range loaded {
		byFile[path.Base(g.File)] = g
	}
	for file, want := range expect {
		got, ok := byFile[file]
		if want == nil {
			if ok {
				return false
			}
			continue
		}
		if !ok || got.Name != want.Name || len(got.Rules) != len(want.Rules) {
			return false
		}
		for i, r := range want.Rules {
			name := r.Record
			if name == "" {
				name = r.Alert
			}
			if got.Rules[i].Name != name {
				return false
			}
		}
	}
	return true
}
