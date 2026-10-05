package capacity

import (
	"errors"
	"fmt"
	"strings"
)

// SelfEnvironmentName is the only environment of the capacity module: the
// platform it is deployed with.
const SelfEnvironmentName = "self"

// Secret references of the module's in-memory observer credentials.
const (
	selfPostgresRef   = "self-postgres"
	selfClickHouseRef = "self-clickhouse"
)

// SelfEnvironment describes the platform the capacity module runs beside
// (capacity-test serve --self). It comes from the deployment's environment,
// so an operator never writes an inventory or a secrets file.
type SelfEnvironment struct {
	API, MQTT, Web string
	Metrics        []MetricsTarget
	Nodes          []NodeTarget
	PostgresDSN    string
	ClickHouseURL  string
}

// SelfEnvironmentFromEnv reads IOT_CAPACITY_* variables:
//
//	IOT_CAPACITY_API_URL         platform API origin (required)
//	IOT_CAPACITY_MQTT_URL        tcp://host:1883 (MQTT load and realtime push)
//	IOT_CAPACITY_WEB_URL         management web origin (video HLS)
//	IOT_CAPACITY_METRICS         role@instance=url,... for every platform process (required)
//	IOT_METRICS_TOKEN            bearer token for those endpoints, when the platform sets one
//	IOT_CAPACITY_NODES           name=url,... node-exporter endpoints
//	IOT_CAPACITY_POSTGRES_DSN    reconciliation database (required)
//	IOT_CAPACITY_CLICKHOUSE_URL  optional raw/telemetry row checks
func SelfEnvironmentFromEnv(getenv func(string) string) (SelfEnvironment, error) {
	e := SelfEnvironment{
		API: strings.TrimRight(strings.TrimSpace(getenv("IOT_CAPACITY_API_URL")), "/"), MQTT: strings.TrimSpace(getenv("IOT_CAPACITY_MQTT_URL")),
		Web: strings.TrimRight(strings.TrimSpace(getenv("IOT_CAPACITY_WEB_URL")), "/"), PostgresDSN: strings.TrimSpace(getenv("IOT_CAPACITY_POSTGRES_DSN")),
		ClickHouseURL: strings.TrimSpace(getenv("IOT_CAPACITY_CLICKHOUSE_URL")),
	}
	for _, item := range splitList(getenv("IOT_CAPACITY_METRICS")) {
		target, u, ok := strings.Cut(item, "=")
		role, instance, ok2 := strings.Cut(target, "@")
		if !ok || !ok2 || role == "" || instance == "" || u == "" {
			return e, fmt.Errorf("IOT_CAPACITY_METRICS entry %q must be role@instance=url", item)
		}
		e.Metrics = append(e.Metrics, MetricsTarget{Role: role, Instance: instance, URL: u, Token: strings.TrimSpace(getenv("IOT_METRICS_TOKEN"))})
	}
	for _, item := range splitList(getenv("IOT_CAPACITY_NODES")) {
		name, u, ok := strings.Cut(item, "=")
		if !ok || name == "" || u == "" {
			return e, fmt.Errorf("IOT_CAPACITY_NODES entry %q must be name=url", item)
		}
		e.Nodes = append(e.Nodes, NodeTarget{Name: name, URL: u})
	}
	if e.PostgresDSN == "" {
		return e, errors.New("IOT_CAPACITY_POSTGRES_DSN is required for reconciliation")
	}
	_, err := e.Inventory()
	return e, err
}

func splitList(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Inventory is the trusted inventory of the self environment; its agent runs
// inside the service process.
func (e SelfEnvironment) Inventory() (*Inventory, error) {
	inv := &Inventory{Name: SelfEnvironmentName, API: e.API, MQTT: e.MQTT, Web: e.Web, Metrics: e.Metrics, Nodes: e.Nodes,
		Agents: []AgentTarget{{Name: "local"}}, Observers: Observers{PostgresSecretRef: selfPostgresRef}}
	if e.ClickHouseURL != "" {
		inv.Observers.ClickHouseSecretRef = selfClickHouseRef
	}
	if err := inv.Validate(); err != nil {
		return nil, fmt.Errorf("capacity module environment: %w", err)
	}
	return inv, nil
}

func (e SelfEnvironment) secrets() map[string]string {
	out := map[string]string{selfPostgresRef: e.PostgresDSN}
	if e.ClickHouseURL != "" {
		out[selfClickHouseRef] = e.ClickHouseURL
	}
	return out
}
