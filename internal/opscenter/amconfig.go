package opscenter

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// The Alertmanager configuration file is edited as a YAML node tree so that
// sections the platform does not manage (global, inhibit_rules, templates,
// time intervals, other receiver types, unknown per-integration options) are
// preserved byte-for-byte in meaning. Routes containing fields the platform
// cannot edit are exposed read-only instead of being rewritten lossy.

const testLabel = "torchlink_test_receiver"
const testIDLabel = "torchlink_test_id"

var supportedRouteKeys = map[string]bool{"receiver": true, "group_by": true, "continue": true, "match": true, "match_re": true, "matchers": true, "group_wait": true, "group_interval": true, "repeat_interval": true, "mute_time_intervals": true, "routes": true}

var matcherPattern = regexp.MustCompile(`^\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*(=~|!~|!=|=)\s*(.*?)\s*$`)

type amRoute struct {
	Receiver          string            `yaml:"receiver"`
	GroupBy           []string          `yaml:"group_by"`
	Continue          bool              `yaml:"continue"`
	Match             map[string]string `yaml:"match"`
	MatchRE           map[string]string `yaml:"match_re"`
	Matchers          []string          `yaml:"matchers"`
	GroupWait         string            `yaml:"group_wait"`
	GroupInterval     string            `yaml:"group_interval"`
	RepeatInterval    string            `yaml:"repeat_interval"`
	MuteTimeIntervals []string          `yaml:"mute_time_intervals"`
	Routes            []amRoute         `yaml:"routes"`
}

func mapGet(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func mapSet(node *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			node.Content[i+1] = value
			return
		}
	}
	node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
}

func mapDelete(node *yaml.Node, key string) {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			node.Content = append(node.Content[:i], node.Content[i+2:]...)
			return
		}
	}
}

func str(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func boolNode(value bool) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(value)}
}

func intNode(value int) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(value)}
}

func strSeq(values []string) *yaml.Node {
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, v := range values {
		seq.Content = append(seq.Content, str(v))
	}
	return seq
}

func newMap() *yaml.Node { return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"} }

func cloneNode(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	c := *n
	c.Content = make([]*yaml.Node, len(n.Content))
	for i, child := range n.Content {
		c.Content[i] = cloneNode(child)
	}
	return &c
}

func parseAMRoot(content []byte) (*yaml.Node, error) {
	var doc yaml.Node
	if len(bytes.TrimSpace(stripHeader(content))) == 0 {
		return newMap(), nil
	}
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return nil, err
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("alertmanager configuration root must be a mapping")
	}
	return doc.Content[0], nil
}

func parseMatcher(text string) (model.OpsMatcher, bool) {
	m := matcherPattern.FindStringSubmatch(text)
	if m == nil {
		return model.OpsMatcher{}, false
	}
	value := m[3]
	if strings.HasPrefix(value, `"`) {
		unquoted, err := strconv.Unquote(value)
		if err != nil {
			return model.OpsMatcher{}, false
		}
		value = unquoted
	}
	return model.OpsMatcher{Name: m[1], Value: value, IsRegex: strings.HasSuffix(m[2], "~"), IsEqual: !strings.HasPrefix(m[2], "!")}, true
}

func matcherString(m model.OpsMatcher) string {
	op := "="
	switch {
	case m.IsRegex && m.IsEqual:
		op = "=~"
	case m.IsRegex:
		op = "!~"
	case !m.IsEqual:
		op = "!="
	}
	return m.Name + op + strconv.Quote(m.Value)
}

func isTestRoute(r amRoute) bool {
	for _, m := range r.Matchers {
		if parsed, ok := parseMatcher(m); ok && parsed.Name == testLabel {
			return true
		}
	}
	return false
}

// routeModel converts a decoded route; unsupported collects fields and
// matchers the editor cannot round-trip.
func routeModel(node *yaml.Node, unsupported map[string]bool) model.OpsRoute {
	if node == nil {
		return model.OpsRoute{}
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if !supportedRouteKeys[node.Content[i].Value] {
			unsupported[node.Content[i].Value] = true
		}
	}
	var r amRoute
	_ = node.Decode(&r)
	out := model.OpsRoute{Receiver: r.Receiver, GroupBy: r.GroupBy, GroupWait: r.GroupWait, GroupInterval: r.GroupInterval, RepeatInterval: r.RepeatInterval, Continue: r.Continue, MuteTimeIntervals: r.MuteTimeIntervals}
	for _, text := range r.Matchers {
		if m, ok := parseMatcher(text); ok {
			out.Matchers = append(out.Matchers, m)
		} else {
			unsupported["matchers: "+text] = true
		}
	}
	keys := make([]string, 0, len(r.Match)+len(r.MatchRE))
	for k := range r.Match {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out.Matchers = append(out.Matchers, model.OpsMatcher{Name: k, Value: r.Match[k], IsEqual: true})
	}
	keys = keys[:0]
	for k := range r.MatchRE {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out.Matchers = append(out.Matchers, model.OpsMatcher{Name: k, Value: r.MatchRE[k], IsEqual: true, IsRegex: true})
	}
	if routes := mapGet(node, "routes"); routes != nil && routes.Kind == yaml.SequenceNode {
		for _, child := range routes.Content {
			var decoded amRoute
			if child.Decode(&decoded) == nil && isManagedNotificationRoute(decoded) {
				continue
			}
			out.Routes = append(out.Routes, routeModel(child, unsupported))
		}
	}
	return out
}

func secretHint(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "已设置"
	}
	return u.Scheme + "://" + u.Host + "/…"
}

func receiverModel(node *yaml.Node) model.OpsReceiver {
	out := model.OpsReceiver{Webhooks: []model.OpsWebhookReceiver{}, Emails: []model.OpsEmailReceiver{}}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, value := node.Content[i].Value, node.Content[i+1]
		switch key {
		case "name":
			out.Name = value.Value
		case "webhook_configs":
			for idx, cfg := range value.Content {
				index := idx
				w := model.OpsWebhookReceiver{SourceIndex: &index, SendResolved: true}
				if u := mapGet(cfg, "url"); u != nil && u.Value != "" {
					w.URL = model.OpsSecret{Set: true, Hint: secretHint(u.Value)}
				} else if mapGet(cfg, "url_file") != nil {
					w.URL = model.OpsSecret{Set: true, Hint: "来自文件"}
				}
				if auth := mapGet(mapGet(cfg, "http_config"), "authorization"); auth != nil && (mapGet(auth, "credentials") != nil || mapGet(auth, "credentials_file") != nil) {
					w.BearerToken = model.OpsSecret{Set: true}
				}
				if v := mapGet(cfg, "send_resolved"); v != nil {
					w.SendResolved = v.Value == "true"
				}
				if v := mapGet(cfg, "max_alerts"); v != nil {
					w.MaxAlerts, _ = strconv.Atoi(v.Value)
				}
				if v := mapGet(cfg, "timeout"); v != nil {
					w.Timeout = v.Value
				}
				out.Webhooks = append(out.Webhooks, w)
			}
		case "email_configs":
			for idx, cfg := range value.Content {
				index := idx
				e := model.OpsEmailReceiver{SourceIndex: &index}
				for field, target := range map[string]*string{"to": &e.To, "from": &e.From, "smarthost": &e.Smarthost, "auth_username": &e.AuthUsername} {
					if v := mapGet(cfg, field); v != nil {
						*target = v.Value
					}
				}
				if mapGet(cfg, "auth_password") != nil || mapGet(cfg, "auth_password_file") != nil {
					e.AuthPassword = model.OpsSecret{Set: true}
				}
				if v := mapGet(cfg, "require_tls"); v != nil {
					b := v.Value == "true"
					e.RequireTLS = &b
				}
				if v := mapGet(cfg, "send_resolved"); v != nil {
					e.SendResolved = v.Value == "true"
				}
				out.Emails = append(out.Emails, e)
			}
		default:
			if strings.HasSuffix(key, "_configs") {
				out.ReadOnly = append(out.ReadOnly, key)
			}
		}
	}
	return out
}

func (s *Service) NotificationConfig(ctx context.Context) (model.OpsNotificationConfig, error) {
	if err := requireBackend(s.Alerts); err != nil {
		return model.OpsNotificationConfig{}, err
	}
	if !configured(s.AMConfig) {
		return model.OpsNotificationConfig{Receivers: []model.OpsReceiver{}, Preserved: []string{"未配置 Alertmanager 配置文件路径，通知渠道只能在部署配置中维护"}}, nil
	}
	file, err := s.AMConfig.Read()
	if err != nil {
		return model.OpsNotificationConfig{}, err
	}
	root, err := parseAMRoot(file.Content)
	if err != nil {
		return model.OpsNotificationConfig{}, &ApplyError{Message: "Alertmanager 配置文件无法解析", Detail: err.Error()}
	}
	unsupported := map[string]bool{}
	out := model.OpsNotificationConfig{Revision: file.Revision, Writable: true, Route: routeModel(mapGet(root, "route"), unsupported), Receivers: []model.OpsReceiver{}, UpdatedBy: headerValue(file.Content, "updated-by")}
	out.DeviceAlarmReceiver, out.DeviceAlarmSince = deviceNotificationRoute(mapGet(root, "route"))
	if t, err := time.Parse(time.RFC3339Nano, headerValue(file.Content, "updated-at")); err == nil {
		out.UpdatedAt = t.UnixMilli()
	}
	if receivers := mapGet(root, "receivers"); receivers != nil {
		for _, r := range receivers.Content {
			if name := mapGet(r, "name"); name != nil && name.Value == deviceDiscardReceiver {
				continue
			}
			out.Receivers = append(out.Receivers, receiverModel(r))
		}
	}
	if inhibit := mapGet(root, "inhibit_rules"); inhibit != nil {
		out.InhibitRules = len(inhibit.Content)
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		switch key := root.Content[i].Value; key {
		case "route", "receivers":
		default:
			out.Preserved = append(out.Preserved, key)
		}
	}
	for key := range unsupported {
		out.Preserved = append(out.Preserved, "route."+key)
	}
	sort.Strings(out.Preserved)
	if len(unsupported) > 0 {
		out.Writable = false
	}
	return out, nil
}

// SaveNotificationConfig applies receiver and routing changes, asks
// Alertmanager to reload and restores the previous file if it refuses.
func (s *Service) SaveNotificationConfig(ctx context.Context, in model.OpsNotificationConfig, actor string) (model.OpsNotificationConfig, error) {
	if err := requireBackend(s.Alerts); err != nil {
		return model.OpsNotificationConfig{}, err
	}
	if !configured(s.AMConfig) {
		return model.OpsNotificationConfig{}, ports.ErrOpsReadOnly
	}
	s.amMu.Lock()
	defer s.amMu.Unlock()
	file, err := s.AMConfig.Read()
	if err != nil {
		return model.OpsNotificationConfig{}, err
	}
	if in.Revision != file.Revision {
		return model.OpsNotificationConfig{}, ports.ErrOpsConflict
	}
	current, err := s.NotificationConfig(ctx)
	if err != nil {
		return model.OpsNotificationConfig{}, err
	}
	root, err := parseAMRoot(file.Content)
	if err != nil {
		return model.OpsNotificationConfig{}, err
	}
	oldReceivers := map[string]*yaml.Node{}
	if receivers := mapGet(root, "receivers"); receivers != nil {
		for _, r := range receivers.Content {
			if name := mapGet(r, "name"); name != nil {
				oldReceivers[name.Value] = r
			}
		}
	}
	names := map[string]bool{}
	receiversNode := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for i, r := range in.Receivers {
		field := fmt.Sprintf("receivers[%d]", i)
		r.Name = strings.TrimSpace(r.Name)
		if r.Name == "" || len([]rune(r.Name)) > 100 || strings.ContainsAny(r.Name, "\n\r") {
			return model.OpsNotificationConfig{}, invalid(field+".name", "接收人名称为 1～100 个字符")
		}
		if r.Name == deviceDiscardReceiver {
			return model.OpsNotificationConfig{}, invalid(field+".name", "此名称由设备通知保留")
		}
		if names[r.Name] {
			return model.OpsNotificationConfig{}, invalid(field+".name", "接收人名称重复：%s", r.Name)
		}
		names[r.Name] = true
		var source *yaml.Node
		if r.OriginalName != "" {
			if source = oldReceivers[r.OriginalName]; source == nil {
				return model.OpsNotificationConfig{}, invalid(field, "原接收人 %s 已不存在，请刷新后重试", r.OriginalName)
			}
		}
		node, err := buildReceiver(field, r, source, mapGet(root, "global"))
		if err != nil {
			return model.OpsNotificationConfig{}, err
		}
		receiversNode.Content = append(receiversNode.Content, node)
	}
	if len(receiversNode.Content) == 0 {
		return model.OpsNotificationConfig{}, invalid("receivers", "至少保留一个接收人（可以不配置任何渠道）")
	}
	var routeNode *yaml.Node
	if current.Writable {
		if err := validateRoute("route", in.Route, names, true); err != nil {
			return model.OpsNotificationConfig{}, err
		}
		routeNode = buildRoute(in.Route)
	} else {
		routeNode = cloneNode(mapGet(root, "route"))
		if routeNode == nil {
			return model.OpsNotificationConfig{}, invalid("route", "缺少路由配置")
		}
		if err := validateRoute("route", current.Route, names, true); err != nil {
			return model.OpsNotificationConfig{}, invalid("receivers", "路由由部署配置维护且仍引用了接收人：%s", err.Error())
		}
		stripManagedNotificationRoutes(routeNode)
	}
	if err := configureDeviceNotifications(routeNode, receiversNode, in, current, s.now()); err != nil {
		return model.OpsNotificationConfig{}, err
	}
	injectTestRoutes(routeNode, names)
	mapSet(root, "route", routeNode)
	mapSet(root, "receivers", receiversNode)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		return model.OpsNotificationConfig{}, err
	}
	if err := s.AMConfig.Write(withHeader(buf.Bytes(), actor, s.now())); err != nil {
		return model.OpsNotificationConfig{}, err
	}
	if reloadErr := s.Alerts.Reload(ctx); reloadErr != nil {
		_ = s.AMConfig.Write(withHeader(file.Content, actor+" (rollback)", s.now()))
		_ = s.Alerts.Reload(context.WithoutCancel(ctx))
		return model.OpsNotificationConfig{}, &ApplyError{Message: "Alertmanager 拒绝了新的通知配置，已恢复原配置", Detail: describeReload(reloadErr), RolledBack: true}
	}
	return s.NotificationConfig(ctx)
}

func describeReload(err error) string {
	if upstream, ok := err.(*ports.OpsUpstreamError); ok {
		return upstream.Message
	}
	return err.Error()
}

func applySecret(field string, node *yaml.Node, key string, secret model.OpsSecret, required bool, hadValue bool) error {
	switch secret.Mode {
	case "", "keep":
		if !hadValue && required {
			return invalid(field, "请填写%s", key)
		}
		return nil
	case "replace":
		if strings.TrimSpace(secret.Value) == "" {
			return invalid(field, "新的凭据不能为空")
		}
		mapSet(node, key, str(strings.TrimSpace(secret.Value)))
		mapDelete(node, key+"_file")
		return nil
	case "clear":
		if required {
			return invalid(field, "%s 不能清除", key)
		}
		mapDelete(node, key)
		mapDelete(node, key+"_file")
		return nil
	}
	return invalid(field, "未知的凭据操作")
}

func buildReceiver(field string, r model.OpsReceiver, source, global *yaml.Node) (*yaml.Node, error) {
	node := newMap()
	if source != nil {
		node = cloneNode(source)
	}
	mapSet(node, "name", str(r.Name))
	var oldWebhooks, oldEmails []*yaml.Node
	if source != nil {
		if v := mapGet(source, "webhook_configs"); v != nil {
			oldWebhooks = v.Content
		}
		if v := mapGet(source, "email_configs"); v != nil {
			oldEmails = v.Content
		}
	}
	webhooks := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for i, w := range r.Webhooks {
		f := fmt.Sprintf("%s.webhooks[%d]", field, i)
		cfg := newMap()
		if w.SourceIndex != nil {
			if *w.SourceIndex < 0 || *w.SourceIndex >= len(oldWebhooks) {
				return nil, invalid(f, "原 Webhook 配置已变化，请刷新后重试")
			}
			cfg = cloneNode(oldWebhooks[*w.SourceIndex])
		}
		hadURL := mapGet(cfg, "url") != nil || mapGet(cfg, "url_file") != nil
		if w.URL.Mode == "replace" {
			u, err := url.Parse(strings.TrimSpace(w.URL.Value))
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return nil, invalid(f+".url", "Webhook 地址必须是 http(s) URL")
			}
		}
		if err := applySecret(f+".url", cfg, "url", w.URL, true, hadURL); err != nil {
			return nil, err
		}
		httpConfig := mapGet(cfg, "http_config")
		switch w.BearerToken.Mode {
		case "replace":
			if strings.TrimSpace(w.BearerToken.Value) == "" {
				return nil, invalid(f+".bearerToken", "新的令牌不能为空")
			}
			if httpConfig == nil {
				httpConfig = newMap()
				mapSet(cfg, "http_config", httpConfig)
			}
			authNode := newMap()
			mapSet(authNode, "type", str("Bearer"))
			mapSet(authNode, "credentials", str(strings.TrimSpace(w.BearerToken.Value)))
			mapSet(httpConfig, "authorization", authNode)
		case "clear":
			if httpConfig != nil {
				mapDelete(httpConfig, "authorization")
				if len(httpConfig.Content) == 0 {
					mapDelete(cfg, "http_config")
				}
			}
		case "", "keep":
		default:
			return nil, invalid(f+".bearerToken", "未知的凭据操作")
		}
		mapSet(cfg, "send_resolved", boolNode(w.SendResolved))
		if w.MaxAlerts < 0 || w.MaxAlerts > 1000 {
			return nil, invalid(f+".maxAlerts", "单条消息告警数上限为 0～1000")
		}
		if w.MaxAlerts > 0 {
			mapSet(cfg, "max_alerts", intNode(w.MaxAlerts))
		} else {
			mapDelete(cfg, "max_alerts")
		}
		if w.Timeout != "" {
			if _, ok := parseDuration(w.Timeout); !ok {
				return nil, invalid(f+".timeout", "超时格式无效，例如 10s")
			}
			mapSet(cfg, "timeout", str(w.Timeout))
		} else {
			mapDelete(cfg, "timeout")
		}
		webhooks.Content = append(webhooks.Content, cfg)
	}
	emails := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	globalSmarthost := mapGet(global, "smtp_smarthost") != nil
	for i, e := range r.Emails {
		f := fmt.Sprintf("%s.emails[%d]", field, i)
		cfg := newMap()
		if e.SourceIndex != nil {
			if *e.SourceIndex < 0 || *e.SourceIndex >= len(oldEmails) {
				return nil, invalid(f, "原邮件配置已变化，请刷新后重试")
			}
			cfg = cloneNode(oldEmails[*e.SourceIndex])
		}
		if strings.TrimSpace(e.To) == "" {
			return nil, invalid(f+".to", "请填写收件人")
		}
		if strings.TrimSpace(e.Smarthost) == "" && !globalSmarthost {
			return nil, invalid(f+".smarthost", "请填写 SMTP 服务器（host:port），或在全局配置中设置 smtp_smarthost")
		}
		for key, value := range map[string]string{"to": e.To, "from": e.From, "smarthost": e.Smarthost, "auth_username": e.AuthUsername} {
			if value = strings.TrimSpace(value); value != "" {
				mapSet(cfg, key, str(value))
			} else if key != "to" {
				mapDelete(cfg, key)
			}
		}
		hadPassword := mapGet(cfg, "auth_password") != nil || mapGet(cfg, "auth_password_file") != nil
		if err := applySecret(f+".authPassword", cfg, "auth_password", e.AuthPassword, false, hadPassword); err != nil {
			return nil, err
		}
		if e.RequireTLS != nil {
			mapSet(cfg, "require_tls", boolNode(*e.RequireTLS))
		}
		mapSet(cfg, "send_resolved", boolNode(e.SendResolved))
		emails.Content = append(emails.Content, cfg)
	}
	if len(webhooks.Content) > 0 {
		mapSet(node, "webhook_configs", webhooks)
	} else {
		mapDelete(node, "webhook_configs")
	}
	if len(emails.Content) > 0 {
		mapSet(node, "email_configs", emails)
	} else {
		mapDelete(node, "email_configs")
	}
	return node, nil
}

func validateRoute(field string, r model.OpsRoute, receivers map[string]bool, top bool) error {
	if top && r.Receiver == "" {
		return invalid(field+".receiver", "默认路由必须指定接收人")
	}
	if r.Receiver != "" && !receivers[r.Receiver] {
		return invalid(field+".receiver", "接收人 %s 不存在", r.Receiver)
	}
	if top && len(r.Matchers) > 0 {
		return invalid(field+".matchers", "默认路由必须匹配所有告警，不能设置匹配条件")
	}
	if !top && len(r.Matchers) == 0 {
		return invalid(field+".matchers", "子路由至少需要一个匹配条件")
	}
	for _, m := range r.Matchers {
		op := Matcher{Name: m.Name, Value: m.Value, Op: map[bool]map[bool]string{true: {true: "=~", false: "!~"}, false: {true: "=", false: "!="}}[m.IsRegex][m.IsEqual]}
		if err := op.validate(); err != nil {
			return err
		}
		if m.Name == testLabel || m.Name == testIDLabel || m.Name == deviceNotificationLabel {
			return invalid(field+".matchers", "标签 %s 由平台通知保留", m.Name)
		}
	}
	for _, label := range r.GroupBy {
		if label != "..." && !validLabelName(label) {
			return invalid(field+".groupBy", "分组标签 %q 不合法", label)
		}
		if label == "..." && len(r.GroupBy) > 1 {
			return invalid(field+".groupBy", "“...”必须单独使用")
		}
	}
	for name, value := range map[string]string{"groupWait": r.GroupWait, "groupInterval": r.GroupInterval, "repeatInterval": r.RepeatInterval} {
		if value != "" {
			if _, ok := parseDuration(value); !ok {
				return invalid(field+"."+name, "时间格式无效，例如 30s、5m、4h")
			}
		}
	}
	if len(r.Routes) > 50 {
		return invalid(field+".routes", "子路由最多 50 条")
	}
	for i, child := range r.Routes {
		if err := validateRoute(fmt.Sprintf("%s.routes[%d]", field, i), child, receivers, false); err != nil {
			return err
		}
	}
	return nil
}

func buildRoute(r model.OpsRoute) *yaml.Node {
	node := newMap()
	if r.Receiver != "" {
		mapSet(node, "receiver", str(r.Receiver))
	}
	if len(r.GroupBy) > 0 {
		mapSet(node, "group_by", strSeq(r.GroupBy))
	}
	if len(r.Matchers) > 0 {
		values := make([]string, len(r.Matchers))
		for i, m := range r.Matchers {
			values[i] = matcherString(m)
		}
		mapSet(node, "matchers", strSeq(values))
	}
	for key, value := range map[string]string{"group_wait": r.GroupWait, "group_interval": r.GroupInterval, "repeat_interval": r.RepeatInterval} {
		if value != "" {
			mapSet(node, key, str(value))
		}
	}
	if r.Continue {
		mapSet(node, "continue", boolNode(true))
	}
	if len(r.MuteTimeIntervals) > 0 {
		mapSet(node, "mute_time_intervals", strSeq(r.MuteTimeIntervals))
	}
	if len(r.Routes) > 0 {
		children := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, child := range r.Routes {
			children.Content = append(children.Content, buildRoute(child))
		}
		mapSet(node, "routes", children)
	}
	return node
}

func stripManagedNotificationRoutes(route *yaml.Node) {
	routes := mapGet(route, "routes")
	if routes == nil {
		return
	}
	kept := routes.Content[:0]
	for _, child := range routes.Content {
		var decoded amRoute
		if child.Decode(&decoded) == nil && isManagedNotificationRoute(decoded) {
			continue
		}
		kept = append(kept, child)
	}
	routes.Content = kept
}

// injectTestRoutes adds one first-position child route per receiver so a
// test notification reaches exactly that receiver and nothing else.
func injectTestRoutes(route *yaml.Node, receivers map[string]bool) {
	names := make([]string, 0, len(receivers))
	for name := range receivers {
		names = append(names, name)
	}
	sort.Strings(names)
	tests := []*yaml.Node{}
	for _, name := range names {
		tests = append(tests, buildRoute(model.OpsRoute{Receiver: name, Matchers: []model.OpsMatcher{{Name: testLabel, Value: name, IsEqual: true}}, GroupBy: []string{"alertname", testLabel, testIDLabel}, GroupWait: "1s", GroupInterval: "1m", RepeatInterval: "24h"}))
	}
	routes := mapGet(route, "routes")
	if routes == nil {
		routes = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		mapSet(route, "routes", routes)
	}
	routes.Content = append(tests, routes.Content...)
}

// TestReceiver posts a short-lived synthetic alert that only the receiver's
// dedicated test route matches. It sends a real notification to that
// receiver's configured channels.
func (s *Service) TestReceiver(ctx context.Context, name, actor string) error {
	if err := requireBackend(s.Alerts); err != nil {
		return err
	}
	cfg, err := s.NotificationConfig(ctx)
	if err != nil {
		return err
	}
	found := false
	for _, r := range cfg.Receivers {
		if r.Name == name {
			found = true
			if len(r.Webhooks) == 0 && len(r.Emails) == 0 && len(r.ReadOnly) == 0 {
				return invalid("receiver", "该接收人未配置任何通知渠道")
			}
		}
	}
	if !found {
		return ports.ErrOpsNotFound
	}
	file, err := s.AMConfig.Read()
	if err != nil {
		return err
	}
	root, err := parseAMRoot(file.Content)
	if err != nil {
		return err
	}
	hasRoute := false
	if routes := mapGet(mapGet(root, "route"), "routes"); routes != nil {
		for _, child := range routes.Content {
			var decoded amRoute
			if child.Decode(&decoded) == nil && decoded.Receiver == name && isTestRoute(decoded) {
				for _, label := range decoded.GroupBy {
					if label == testIDLabel || label == "..." {
						hasRoute = true
					}
				}
			}
		}
	}
	if !hasRoute {
		return invalid("receiver", "测试路由尚未生成或需要更新，请先在平台中保存一次通知配置")
	}
	now := s.now().UTC()
	return s.Alerts.PostAlerts(ctx, []map[string]any{{
		"labels":      map[string]string{"alertname": "TorchLinkNotificationTest", testLabel: name, testIDLabel: randomID(), "severity": "info"},
		"annotations": map[string]string{"summary": "炬联运维中心通知测试", "description": "由 " + actor + " 于 " + now.Format(time.RFC3339) + " 发起，仅用于验证通知渠道，可忽略。"},
		"startsAt":    now.Format(time.RFC3339),
		"endsAt":      now.Add(5 * time.Minute).Format(time.RFC3339),
	}})
}
