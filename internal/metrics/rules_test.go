package metrics

import (
	"regexp"
	"strings"
	"testing"

	"iot-platform/ops/prometheus"
)

// Every platform counter an alert rule applies increase() or delta() to must
// exist from start, or the first failure after a restart never alerts.
func TestAlertedCountersArePreregistered(t *testing.T) {
	exported := New().Prometheus()
	external := []string{"backup_", "node_", "http_request_", "redpanda_", "mqtt_broker_", "mqtt_inbox_", "protocol_listener_", "clickhouse_", "loki_"}
	for _, match := range regexp.MustCompile(`(?:increase|delta)\(([a-z_]+)\[`).FindAllStringSubmatch(prometheus.Alerts, -1) {
		name := match[1]
		skip := false
		for _, prefix := range external {
			skip = skip || strings.HasPrefix(name, prefix)
		}
		if skip {
			continue
		}
		if !strings.Contains(exported, "\n"+name+" ") && !strings.HasPrefix(exported, name+" ") && !strings.Contains(exported, "TYPE "+name+" ") {
			t.Errorf("alert rules watch %s, which a new process does not export", name)
		}
	}
}
