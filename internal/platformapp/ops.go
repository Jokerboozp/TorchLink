package platformapp

import (
	"log/slog"
	"path/filepath"

	"iot-platform/internal/adapters/observability"
	"iot-platform/internal/config"
	"iot-platform/internal/opscenter"
	"iot-platform/internal/ports"
)

// newOpsCenter wires the ops center to the configured components. Components
// left unconfigured are reported as such by the UI instead of failing startup.
func newOpsCenter(cfg config.Config, prefs ports.OpsPreferenceStore, log *slog.Logger) *opscenter.Service {
	ops := cfg.Ops.WithDefaults()
	state := filepath.Join(cfg.DataDir, "ops-state")
	svc := &opscenter.Service{
		PromRules:   observability.NewDirStore(ops.PrometheusRulesDir, filepath.Join(state, "prometheus-rules"), ".yml", 0o644),
		LokiRules:   observability.NewDirStore(ops.LokiRulesDir, filepath.Join(state, "loki-rules"), ".yaml", 0o644),
		LokiRuntime: observability.NewFileStore(ops.LokiRuntimeFile, state, 0o644),
		AMConfig:    observability.NewFileStore(ops.AlertmanagerConfig, state, ops.ConfigFileMode),
		Prefs:       prefs,
		Log:         log,
		Limits: opscenter.Limits{
			QueryTimeout: ops.QueryTimeout, ReloadTimeout: ops.ReloadTimeout, MaxSeries: ops.MaxSeries, MaxLogLines: ops.MaxLogLines,
			MaxExportLines: ops.MaxExportLines, MaxMetricRange: ops.MaxMetricRange, MaxLogRange: ops.MaxLogRange,
		},
	}
	if ops.PrometheusURL != "" {
		svc.Metrics = observability.NewPrometheus(ops.PrometheusURL, ops.QueryTimeout+5e9)
	}
	if ops.LokiURL != "" {
		svc.Logs = observability.NewLoki(ops.LokiURL, ops.LokiTenant, ops.QueryTimeout+5e9)
	}
	if ops.GrafanaURL != "" {
		svc.Dashboards = observability.NewGrafana(ops.GrafanaURL, ops.GrafanaToken, ops.GrafanaUser, ops.GrafanaPassword, ops.QueryTimeout+5e9)
	}
	if ops.AlertmanagerURL != "" {
		svc.Alerts = observability.NewAlertmanager(ops.AlertmanagerURL, ops.QueryTimeout)
	}
	configured := []string{}
	for name, ok := range map[string]bool{"prometheus": svc.Metrics != nil, "loki": svc.Logs != nil, "grafana": svc.Dashboards != nil, "alertmanager": svc.Alerts != nil} {
		if ok {
			configured = append(configured, name)
		}
	}
	log.Info("ops center configured", "components", configured, "metricRulesWritable", ops.PrometheusRulesDir != "", "logRulesWritable", ops.LokiRulesDir != "", "notificationWritable", ops.AlertmanagerConfig != "")
	return svc
}
