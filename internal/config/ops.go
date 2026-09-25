package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// OpsConfig holds the server-side connection settings for the observability
// components behind the ops center. Browsers never receive these values.
type OpsConfig struct {
	PrometheusURL      string
	LokiURL            string
	LokiTenant         string
	GrafanaURL         string
	GrafanaToken       string
	GrafanaUser        string
	GrafanaPassword    string
	AlertmanagerURL    string
	PrometheusRulesDir string
	LokiRulesDir       string
	LokiRuntimeFile    string
	AlertmanagerConfig string
	ConfigFileMode     os.FileMode
	QueryTimeout       time.Duration
	ReloadTimeout      time.Duration
	MaxSeries          int
	MaxLogLines        int
	MaxExportLines     int
	MaxMetricRange     time.Duration
	MaxLogRange        time.Duration
	Tenants            []string
	LogPushURL         string
	LogPushTenant      string
	LogServiceName     string
	loadErr            error
}

func loadOps() OpsConfig {
	cfg := OpsConfig{
		PrometheusURL:      trimURL(os.Getenv("IOT_OPS_PROMETHEUS_URL")),
		LokiURL:            trimURL(os.Getenv("IOT_OPS_LOKI_URL")),
		LokiTenant:         strings.TrimSpace(os.Getenv("IOT_OPS_LOKI_TENANT")),
		GrafanaURL:         trimURL(os.Getenv("IOT_OPS_GRAFANA_URL")),
		GrafanaToken:       strings.TrimSpace(os.Getenv("IOT_OPS_GRAFANA_TOKEN")),
		GrafanaUser:        strings.TrimSpace(os.Getenv("IOT_OPS_GRAFANA_USER")),
		GrafanaPassword:    os.Getenv("IOT_OPS_GRAFANA_PASSWORD"),
		AlertmanagerURL:    trimURL(os.Getenv("IOT_OPS_ALERTMANAGER_URL")),
		PrometheusRulesDir: strings.TrimSpace(os.Getenv("IOT_OPS_PROMETHEUS_RULES_DIR")),
		LokiRulesDir:       strings.TrimSpace(os.Getenv("IOT_OPS_LOKI_RULES_DIR")),
		LokiRuntimeFile:    strings.TrimSpace(os.Getenv("IOT_OPS_LOKI_RUNTIME_FILE")),
		AlertmanagerConfig: strings.TrimSpace(os.Getenv("IOT_OPS_ALERTMANAGER_CONFIG_FILE")),
		QueryTimeout:       duration("IOT_OPS_QUERY_TIMEOUT", 30*time.Second),
		ReloadTimeout:      duration("IOT_OPS_RELOAD_TIMEOUT", 45*time.Second),
		MaxSeries:          intValue("IOT_OPS_MAX_SERIES", 500),
		MaxLogLines:        intValue("IOT_OPS_MAX_LOG_LINES", 1000),
		MaxExportLines:     intValue("IOT_OPS_MAX_EXPORT_LINES", 5000),
		MaxMetricRange:     duration("IOT_OPS_MAX_METRIC_RANGE", 31*24*time.Hour),
		MaxLogRange:        duration("IOT_OPS_MAX_LOG_RANGE", 7*24*time.Hour),
		Tenants:            split(os.Getenv("IOT_OPS_TENANTS")),
		LogPushURL:         trimURL(os.Getenv("IOT_LOG_LOKI_URL")),
		LogPushTenant:      strings.TrimSpace(os.Getenv("IOT_LOG_LOKI_TENANT")),
		LogServiceName:     get("IOT_LOG_SERVICE_NAME", "platform-api"),
		ConfigFileMode:     0o640,
	}
	if raw := strings.TrimSpace(os.Getenv("IOT_OPS_CONFIG_FILE_MODE")); raw != "" {
		mode, err := strconv.ParseUint(raw, 8, 32)
		if err != nil || mode&^0o666 != 0 || mode&0o600 != 0o600 {
			cfg.loadErr = fmt.Errorf("IOT_OPS_CONFIG_FILE_MODE must be an octal file mode such as 0640 or 0644")
		} else {
			cfg.ConfigFileMode = os.FileMode(mode)
		}
	}
	return cfg
}

func (c OpsConfig) validate() error {
	if c.loadErr != nil {
		return c.loadErr
	}
	for name, raw := range map[string]string{
		"IOT_OPS_PROMETHEUS_URL":   c.PrometheusURL,
		"IOT_OPS_LOKI_URL":         c.LokiURL,
		"IOT_OPS_GRAFANA_URL":      c.GrafanaURL,
		"IOT_OPS_ALERTMANAGER_URL": c.AlertmanagerURL,
		"IOT_LOG_LOKI_URL":         c.LogPushURL,
	} {
		if raw == "" {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("%s must be an HTTP(S) URL without embedded credentials, query or fragment", name)
		}
	}
	if c.QueryTimeout < 0 || c.ReloadTimeout < 0 || c.MaxSeries < 0 || c.MaxLogLines < 0 || c.MaxExportLines < 0 || c.MaxMetricRange < 0 || c.MaxLogRange < 0 {
		return fmt.Errorf("ops center limits must not be negative")
	}
	return nil
}

// WithDefaults fills unset limits, so a Config built in code behaves like one
// loaded from an environment without the optional IOT_OPS_* limits.
func (c OpsConfig) WithDefaults() OpsConfig {
	def := func(v, fallback time.Duration) time.Duration {
		if v <= 0 {
			return fallback
		}
		return v
	}
	num := func(v, fallback int) int {
		if v <= 0 {
			return fallback
		}
		return v
	}
	c.QueryTimeout = def(c.QueryTimeout, 30*time.Second)
	c.ReloadTimeout = def(c.ReloadTimeout, 45*time.Second)
	c.MaxMetricRange = def(c.MaxMetricRange, 31*24*time.Hour)
	c.MaxLogRange = def(c.MaxLogRange, 7*24*time.Hour)
	c.MaxSeries = num(c.MaxSeries, 500)
	c.MaxLogLines = num(c.MaxLogLines, 1000)
	c.MaxExportLines = num(c.MaxExportLines, 5000)
	if c.ConfigFileMode == 0 {
		c.ConfigFileMode = 0o640
	}
	if c.LogServiceName == "" {
		c.LogServiceName = "platform-api"
	}
	return c
}

func trimURL(value string) string {
	return strings.TrimRight(strings.TrimSpace(value), "/")
}

func intValue(name string, fallback int) int {
	v, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}
