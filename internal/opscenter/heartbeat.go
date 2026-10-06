package opscenter

import (
	"bytes"
	"context"
	"time"

	"gopkg.in/yaml.v3"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// The Watchdog rule in ops/prometheus/alerts.yml always fires. Its arrival at
// Alertmanager proves that rules are evaluated and alerts are delivered; when
// it is missing the alerting chain itself is broken and no other alert would
// arrive either. A dedicated route sends it to a receiver without channels, so
// it never notifies anyone, and the ops center hides it from alert lists.
const (
	heartbeatAlert    = "Watchdog"
	heartbeatReceiver = "torchlink-heartbeat"
)

func isHeartbeatRoute(r amRoute) bool { return r.Receiver == heartbeatReceiver }

// injectHeartbeatRoute puts the heartbeat route first and adds its receiver.
// Existing heartbeat routes and receivers are replaced, so it is idempotent.
func injectHeartbeatRoute(route, receivers *yaml.Node) {
	children := mapGet(route, "routes")
	if children == nil {
		children = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		mapSet(route, "routes", children)
	}
	kept := []*yaml.Node{buildRoute(model.OpsRoute{Receiver: heartbeatReceiver,
		Matchers: []model.OpsMatcher{{Name: "alertname", Value: heartbeatAlert, IsEqual: true}},
		GroupBy:  []string{"alertname"}, RepeatInterval: "24h"})}
	for _, child := range children.Content {
		var decoded amRoute
		if child.Decode(&decoded) == nil && isHeartbeatRoute(decoded) {
			continue
		}
		kept = append(kept, child)
	}
	children.Content = kept
	list := receivers.Content[:0]
	for _, r := range receivers.Content {
		if name := mapGet(r, "name"); name != nil && name.Value == heartbeatReceiver {
			continue
		}
		list = append(list, r)
	}
	receiver := newMap()
	mapSet(receiver, "name", str(heartbeatReceiver))
	receivers.Content = append(list, receiver)
}

// hasHeartbeatRoute reports whether the routing tree sends the heartbeat to
// its silent receiver ahead of every other route.
func hasHeartbeatRoute(root *yaml.Node) bool {
	children := mapGet(mapGet(root, "route"), "routes")
	if children == nil || len(children.Content) == 0 {
		return false
	}
	var first amRoute
	return children.Content[0].Decode(&first) == nil && isHeartbeatRoute(first)
}

// EnsureHeartbeatRoute adds the heartbeat route to an Alertmanager
// configuration written before it existed (for example by an earlier
// release), following the same validate, write, reload and roll back steps
// as a notification change. It does nothing when the route is present.
func (s *Service) EnsureHeartbeatRoute(ctx context.Context) error {
	if !configured(s.Alerts) || !configured(s.AMConfig) {
		return nil
	}
	s.amMu.Lock()
	defer s.amMu.Unlock()
	file, err := s.AMConfig.Read()
	if err != nil {
		return err
	}
	root, err := parseAMRoot(file.Content)
	if err != nil {
		return err
	}
	if hasHeartbeatRoute(root) {
		return nil
	}
	route, receivers := mapGet(root, "route"), mapGet(root, "receivers")
	if route == nil || receivers == nil {
		return invalid("route", "Alertmanager 配置缺少路由或接收人")
	}
	injectHeartbeatRoute(route, receivers)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		return err
	}
	if err := s.AMConfig.Write(withHeader(buf.Bytes(), "platform (heartbeat route)", s.now())); err != nil {
		return err
	}
	if reloadErr := s.Alerts.Reload(ctx); reloadErr != nil {
		_ = s.AMConfig.Write(file.Content)
		_ = s.Alerts.Reload(context.WithoutCancel(ctx))
		return &ApplyError{Message: "Alertmanager 拒绝了告警心跳路由，已恢复原配置", Detail: describeReload(reloadErr), RolledBack: true}
	}
	return nil
}

// RunHeartbeatRoute retries EnsureHeartbeatRoute until it succeeds or the
// process stops; Alertmanager may start after the API.
func (s *Service) RunHeartbeatRoute(ctx context.Context) {
	for delay := 30 * time.Second; ctx.Err() == nil; delay = min(delay*2, 5*time.Minute) {
		err := s.EnsureHeartbeatRoute(ctx)
		if err == nil || ctx.Err() != nil {
			return
		}
		s.logger().Warn("alert heartbeat route setup failed; will retry", "error", err)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// heartbeatWarning reports a missing heartbeat at Alertmanager. It stays
// quiet while Prometheus is not configured, since nothing would send it.
func (s *Service) heartbeatWarning(ctx context.Context) string {
	if !configured(s.Metrics) {
		return ""
	}
	alerts, err := s.Alerts.Alerts(ctx, heartbeatFilter())
	if err != nil || len(alerts) > 0 {
		return ""
	}
	return "Alertmanager 未收到 Prometheus 的告警心跳（Watchdog）：告警规则可能未加载，或 Prometheus 无法连接 Alertmanager，其他平台告警同样不会送达"
}

// heartbeatFilter selects the heartbeat in whatever state Alertmanager holds it.
func heartbeatFilter() ports.AlertFilter {
	return ports.AlertFilter{Active: true, Silenced: true, Inhibited: true, Matchers: []string{Matcher{Name: "alertname", Op: "=", Value: heartbeatAlert}.String()}}
}
