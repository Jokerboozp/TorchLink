// Package clusterplan validates a multi-node deployment inventory and renders
// one Compose project per node (host networking) plus shared configuration.
// Compose runs each node; this package, not Compose, owns cross-node layout.
package clusterplan

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"iot-platform/internal/deploycheck"
)

// Inventory is the operator-maintained cluster manifest.
type Inventory struct {
	SchemaVersion int            `yaml:"schemaVersion"`
	Name          string         `yaml:"name"`
	DataRoot      string         `yaml:"dataRoot,omitempty"`
	Images        Images         `yaml:"images"`
	Nodes         []Node         `yaml:"nodes"`
	Redpanda      RedpandaSpec   `yaml:"redpanda"`
	EMQX          GroupSpec      `yaml:"emqx"`
	Etcd          GroupSpec      `yaml:"etcd"`
	Postgres      PostgresSpec   `yaml:"postgres"`
	Redis         RedisSpec      `yaml:"redis"`
	ClickHouse    ClickHouseSpec `yaml:"clickhouse"`
	MinIO         MinIOSpec      `yaml:"minio"`
	Harness       GroupSpec      `yaml:"harness"`
	// Video places media servers: the first is the primary, the others
	// standbys the live module fails over to.
	Video  PoolSpec   `yaml:"video,omitempty"`
	Backup SingleSpec `yaml:"backup,omitempty"`
	// Monitoring places Prometheus with Alertmanager; several nodes run
	// independent Prometheus replicas and one Alertmanager cluster.
	Monitoring PoolSpec `yaml:"monitoring,omitempty"`
	// Capacity places the capacity-test module (capacity-test serve); empty = off.
	Capacity SingleSpec        `yaml:"capacity,omitempty"`
	Platform PlatformSpec      `yaml:"platform"`
	Env      map[string]string `yaml:"env,omitempty"`
}

type Images struct {
	Platform     string `yaml:"platform"`
	Web          string `yaml:"web"`
	Harness      string `yaml:"harness"`
	Backup       string `yaml:"backup"`
	Video        string `yaml:"video"`
	Redpanda     string `yaml:"redpanda"`
	EMQX         string `yaml:"emqx"`
	Etcd         string `yaml:"etcd"`
	Postgres     string `yaml:"postgres"` // Spilo (Patroni + PostgreSQL)
	Redis        string `yaml:"redis"`
	ClickHouse   string `yaml:"clickhouse"`
	Keeper       string `yaml:"keeper"`
	MinIO        string `yaml:"minio"`
	Prometheus   string `yaml:"prometheus"`
	Alertmanager string `yaml:"alertmanager"`
	NodeExporter string `yaml:"nodeExporter"`
	// LB is the HAProxy image of the per-node internal load balancers used
	// when platform.internalURL/gatewayURL are left empty.
	LB string `yaml:"lb"`
}

type Node struct {
	Name        string `yaml:"name"`
	Address     string `yaml:"address"`
	FaultDomain string `yaml:"faultDomain"`
}

type GroupSpec struct {
	Nodes []string `yaml:"nodes"`
}

type SingleSpec struct {
	Node string `yaml:"node"`
}

// PoolSpec places a service on one node (node) or several (nodes).
type PoolSpec struct {
	Node  string   `yaml:"node,omitempty"`
	Nodes []string `yaml:"nodes,omitempty"`
}

// Members lists the nodes in placement order.
func (p PoolSpec) Members() []string {
	if len(p.Nodes) > 0 {
		return p.Nodes
	}
	if p.Node != "" {
		return []string{p.Node}
	}
	return nil
}

// MinIOSpec runs one MinIO server (node), or a distributed erasure-coded
// deployment over nodes with drivesPerNode volumes each.
type MinIOSpec struct {
	PoolSpec      `yaml:",inline"`
	DrivesPerNode int `yaml:"drivesPerNode,omitempty"`
}

// Distributed reports a multi-node MinIO deployment.
func (m MinIOSpec) Distributed() bool { return len(m.Members()) > 1 }

type RedpandaSpec struct {
	Nodes       []string `yaml:"nodes"`
	Partitions  int      `yaml:"partitions"`
	Replication int      `yaml:"replication"`
	SMP         int      `yaml:"smp"`
	Memory      string   `yaml:"memory"`
}

type PostgresSpec struct {
	Nodes          []string `yaml:"nodes"`
	MaxConnections int      `yaml:"maxConnections"`
	Reserved       int      `yaml:"reserved"`
	// Synchronous keeps one synchronous standby so a confirmed commit
	// survives the loss of the primary; commits wait while none is healthy.
	Synchronous bool `yaml:"synchronous"`
}

type RedisSpec struct {
	Master    string   `yaml:"master"`
	Replicas  []string `yaml:"replicas"`
	Sentinels []string `yaml:"sentinels"`
	Quorum    int      `yaml:"quorum"`
}

type ClickHouseSpec struct {
	Cluster      string     `yaml:"cluster"`
	Shards       [][]string `yaml:"shards"`
	Keeper       []string   `yaml:"keeper"`
	InsertQuorum string     `yaml:"insertQuorum"`
}

type RoleSpec struct {
	Nodes   []string `yaml:"nodes"`
	PoolMax int      `yaml:"poolMax"`
}

type PlatformSpec struct {
	// InternalURL is the load-balanced API origin other services use
	// (Harness MCP callbacks, web proxy); GatewayURL balances the gateways.
	// Leave both empty to render a local HAProxy on every node instead
	// (127.0.0.1:18181 → api instances, 127.0.0.1:18182 → gateways).
	InternalURL string              `yaml:"internalURL,omitempty"`
	GatewayURL  string              `yaml:"gatewayURL,omitempty"`
	PublicURL   string              `yaml:"publicURL,omitempty"`
	Roles       map[string]RoleSpec `yaml:"roles"`
	Web         GroupSpec           `yaml:"web"`
}

// Fixed host ports (host networking). Validation rejects two services that
// need the same port on one node.
var Ports = map[string][]int{
	"etcd": {2379, 2380}, "postgres": {5432, 8008}, "redpanda": {9092, 33145, 9644, 18081, 18082},
	"emqx": {1883, 8083, 8084, 8883, 18083, 4370, 5370}, "clickhouse": {8123, 9000, 9009}, "keeper": {9181, 9234},
	"redis": {6379}, "sentinel": {26379}, "minio": {9002, 9003}, "harness": {8091},
	"video":  {80, 8000},
	"backup": {8090}, "prometheus": {9090, 9093, 9094}, "node-exporter": {9100}, "web": {8080, 8443},
	"api": {8081, 5060}, "gateway": {8082, 26875}, "parser": {8101}, "processor": {8102}, "jobs": {8104},
	"lb": {LBAPIPort, LBGatewayPort, LBMinIOPort, LBVideoPort, LBPrometheusPort, LBAlertmanagerPort}, "capacity": {7080},
}

// Local load balancer ports (bound to 127.0.0.1 on every node).
const (
	LBAPIPort          = 18181
	LBGatewayPort      = 18182
	LBMinIOPort        = 18183
	LBVideoPort        = 18180
	LBPrometheusPort   = 18190
	LBAlertmanagerPort = 18193
)

// LocalLB reports whether nodes run the local HAProxy: for the API and
// gateway origins, or for a service with several instances.
func (inv *Inventory) LocalLB() bool {
	return inv.AutoLB() || inv.MinIO.Distributed() || len(inv.Monitoring.Members()) > 1 || len(inv.Video.Members()) > 1
}

// AutoLB reports whether the renderer provides the internal load balancers.
func (inv *Inventory) AutoLB() bool {
	return inv.Platform.InternalURL == "" && inv.Platform.GatewayURL == ""
}

// APIURL is the API origin used inside the cluster.
func (inv *Inventory) APIURL() string {
	if inv.AutoLB() {
		return fmt.Sprintf("http://127.0.0.1:%d", LBAPIPort)
	}
	return strings.TrimRight(inv.Platform.InternalURL, "/")
}

// GatewayURL is the gateway origin used inside the cluster.
func (inv *Inventory) GatewayURL() string {
	if inv.AutoLB() {
		return fmt.Sprintf("http://127.0.0.1:%d", LBGatewayPort)
	}
	return strings.TrimRight(inv.Platform.GatewayURL, "/")
}

// RoleNames lists the platform roles a cluster may place.
var RoleNames = []string{"api", "gateway", "parser", "processor", "jobs"}

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}$`)

func Load(path string) (*Inventory, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	var inv Inventory
	if err = dec.Decode(&inv); err != nil {
		return nil, fmt.Errorf("inventory: %w", err)
	}
	inv.defaults()
	return &inv, nil
}

func (inv *Inventory) defaults() {
	if inv.DataRoot == "" {
		inv.DataRoot = "/opt/" + inv.Name + "/data"
	}
	if inv.Redpanda.Partitions == 0 {
		inv.Redpanda.Partitions = 24
	}
	if inv.Redpanda.Replication == 0 {
		inv.Redpanda.Replication = 3
	}
	if inv.Redpanda.SMP == 0 {
		inv.Redpanda.SMP = 2
	}
	if inv.Redpanda.Memory == "" {
		inv.Redpanda.Memory = "4G"
	}
	if inv.Postgres.MaxConnections == 0 {
		inv.Postgres.MaxConnections = 300
	}
	if inv.Postgres.Reserved == 0 {
		inv.Postgres.Reserved = 40
	}
	if inv.Redis.Quorum == 0 {
		inv.Redis.Quorum = len(inv.Redis.Sentinels)/2 + 1
	}
	if inv.ClickHouse.Cluster == "" {
		inv.ClickHouse.Cluster = "iot_cluster"
	}
	if inv.ClickHouse.InsertQuorum == "" {
		inv.ClickHouse.InsertQuorum = "2"
	}
	for name, r := range inv.Platform.Roles {
		if r.PoolMax == 0 {
			r.PoolMax = map[string]int{"api": 12, "gateway": 12, "parser": 16, "processor": 24, "jobs": 4}[name]
			inv.Platform.Roles[name] = r
		}
	}
}

func (inv *Inventory) node(name string) (Node, bool) {
	for _, n := range inv.Nodes {
		if n.Name == name {
			return n, true
		}
	}
	return Node{}, false
}

// Placement lists every service instance per node.
func (inv *Inventory) Placement() map[string][]string {
	out := map[string][]string{}
	add := func(service string, nodes ...string) {
		for _, n := range nodes {
			if n != "" {
				out[n] = append(out[n], service)
			}
		}
	}
	add("etcd", inv.Etcd.Nodes...)
	add("postgres", inv.Postgres.Nodes...)
	add("redpanda", inv.Redpanda.Nodes...)
	add("emqx", inv.EMQX.Nodes...)
	for _, shard := range inv.ClickHouse.Shards {
		add("clickhouse", shard...)
	}
	add("keeper", inv.ClickHouse.Keeper...)
	add("redis", append([]string{inv.Redis.Master}, inv.Redis.Replicas...)...)
	add("sentinel", inv.Redis.Sentinels...)
	add("minio", inv.MinIO.Members()...)
	add("harness", inv.Harness.Nodes...)
	add("video", inv.Video.Members()...)
	add("backup", inv.Backup.Node)
	add("prometheus", inv.Monitoring.Members()...)
	add("capacity", inv.Capacity.Node)
	for _, n := range inv.Nodes {
		add("node-exporter", n.Name)
		if inv.LocalLB() {
			add("lb", n.Name)
		}
	}
	add("web", inv.Platform.Web.Nodes...)
	for _, role := range RoleNames {
		add(role, inv.Platform.Roles[role].Nodes...)
	}
	return out
}

// Validate checks names, placement, failure domains, ports and the
// PostgreSQL connection budget.
func (inv *Inventory) Validate() (deploycheck.ConnectionBudget, error) {
	var errs []string
	bad := func(format string, a ...any) { errs = append(errs, fmt.Sprintf(format, a...)) }
	if inv.SchemaVersion != 1 {
		bad("schemaVersion must be 1")
	}
	if !namePattern.MatchString(inv.Name) {
		bad("name must be a lowercase identifier")
	}
	domains := map[string]bool{}
	seen := map[string]bool{}
	for _, n := range inv.Nodes {
		if !namePattern.MatchString(n.Name) || seen[n.Name] {
			bad("node %q needs a unique lowercase name", n.Name)
		}
		seen[n.Name] = true
		if net.ParseIP(n.Address) == nil {
			bad("node %s address must be an IP address", n.Name)
		}
		if n.FaultDomain == "" {
			bad("node %s needs a faultDomain (separate host/power/rack; VMs on one host share a domain)", n.Name)
		}
		domains[n.FaultDomain] = true
	}
	if len(inv.Nodes) < 3 {
		bad("a cluster needs at least 3 nodes")
	}
	if len(domains) < 2 {
		bad("nodes must span at least 2 fault domains")
	}
	for node := range inv.Placement() {
		if _, ok := inv.node(node); !ok {
			bad("service placed on unknown node %q", node)
		}
	}
	// Quorum groups: odd size, distinct nodes, no fault domain holding a
	// majority (losing that domain would lose the quorum).
	quorum := func(name string, members []string) {
		if len(members) < 3 || len(members)%2 == 0 {
			bad("%s needs an odd number (>=3) of members", name)
		}
		inv.spread(name, members, true, bad)
	}
	quorum("etcd", inv.Etcd.Nodes)
	quorum("postgres (Patroni)", inv.Postgres.Nodes)
	quorum("redpanda", inv.Redpanda.Nodes)
	quorum("clickhouse keeper", inv.ClickHouse.Keeper)
	quorum("redis sentinel", inv.Redis.Sentinels)
	if len(inv.EMQX.Nodes) < 2 {
		bad("emqx needs at least 2 nodes")
	}
	inv.spread("emqx", inv.EMQX.Nodes, false, bad)
	if inv.Redpanda.Replication > len(inv.Redpanda.Nodes) || inv.Redpanda.Replication < 3 {
		bad("redpanda replication must be at least 3 and not exceed the broker count")
	}
	if inv.Redis.Master == "" || len(inv.Redis.Replicas) < 1 {
		bad("redis needs a master and at least one replica")
	}
	inv.spread("redis", append([]string{inv.Redis.Master}, inv.Redis.Replicas...), false, bad)
	if inv.Redis.Quorum < 1 || inv.Redis.Quorum > len(inv.Redis.Sentinels) {
		bad("redis sentinel quorum must be between 1 and the sentinel count")
	}
	if len(inv.ClickHouse.Shards) == 0 {
		bad("clickhouse needs at least one shard")
	}
	chNodes := map[string]bool{}
	for i, shard := range inv.ClickHouse.Shards {
		if len(shard) < 2 {
			bad("clickhouse shard %d needs at least 2 replicas", i+1)
		}
		inv.spread(fmt.Sprintf("clickhouse shard %d", i+1), shard, false, bad)
		for _, n := range shard {
			if chNodes[n] {
				bad("node %s hosts two clickhouse replicas; one server per node", n)
			}
			chNodes[n] = true
		}
		if q := inv.ClickHouse.InsertQuorum; q != "auto" && q != "" && (len(q) != 1 || q[0] < '1' || int(q[0]-'0') > len(shard)) {
			bad("clickhouse insertQuorum %s exceeds replicas of shard %d", q, i+1)
		}
	}
	if !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(inv.ClickHouse.Cluster) {
		bad("clickhouse cluster name is invalid")
	}
	for name, pool := range map[string]PoolSpec{"minio": inv.MinIO.PoolSpec, "video": inv.Video, "monitoring": inv.Monitoring} {
		if pool.Node != "" && len(pool.Nodes) > 0 {
			bad("%s: set node or nodes, not both", name)
		}
		if len(pool.Nodes) == 1 {
			bad("%s.nodes needs at least 2 nodes; use node for one", name)
		}
		if len(pool.Nodes) > 1 {
			inv.spread(name, pool.Nodes, false, bad)
		}
	}
	if len(inv.MinIO.Members()) == 0 {
		bad("minio.node or minio.nodes is required (backups and knowledge files)")
	}
	if inv.MinIO.DrivesPerNode < 0 || inv.MinIO.DrivesPerNode > 16 || (!inv.MinIO.Distributed() && inv.MinIO.DrivesPerNode > 0) {
		bad("minio.drivesPerNode applies to minio.nodes and must be 1-16")
	}
	if inv.MinIO.Distributed() && (len(inv.MinIO.Nodes) < 3 || len(inv.MinIO.Nodes)*max(inv.MinIO.DrivesPerNode, 1) < 4) {
		bad("distributed minio needs at least 3 nodes and 4 drives in total, so one node can fail without losing reads or writes")
	}
	if len(inv.Harness.Nodes) == 0 {
		bad("harness needs at least one node (AI workflows are mandatory)")
	}
	for _, role := range RoleNames {
		if len(inv.Platform.Roles[role].Nodes) == 0 {
			bad("platform role %s needs at least one node", role)
		}
	}
	for role := range inv.Platform.Roles {
		if !contains(RoleNames, role) {
			bad("unknown platform role %q", role)
		}
	}
	if len(inv.Platform.Web.Nodes) == 0 {
		bad("platform.web needs at least one node")
	}
	if inv.LocalLB() && inv.Images.LB == "" {
		bad("images.lb (HAProxy) is required for the local load balancers (empty platform.internalURL/gatewayURL, or several minio, video or monitoring nodes)")
	}
	if !inv.AutoLB() {
		for _, u := range []string{inv.Platform.InternalURL, inv.Platform.GatewayURL} {
			if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
				bad("platform.internalURL and platform.gatewayURL must both be load-balanced http(s) origins, or both empty for the built-in local load balancers")
			}
		}
	}
	// Ports on each node must not collide under host networking.
	for node, services := range inv.Placement() {
		owner := map[int]string{}
		for _, svc := range services {
			for _, p := range Ports[svc] {
				if prev, clash := owner[p]; clash {
					bad("node %s: %s and %s both need port %d", node, prev, svc, p)
				}
				owner[p] = svc
			}
		}
	}
	for k := range inv.Env {
		if !strings.HasPrefix(k, "IOT_") || secretLike(k) {
			bad("env %s: only non-secret IOT_* settings may be set here; secrets come from the secrets file", k)
		}
	}
	var roles []deploycheck.RolePool
	for _, role := range RoleNames {
		r := inv.Platform.Roles[role]
		roles = append(roles, deploycheck.RolePool{Role: role, Instances: len(r.Nodes), PoolMax: r.PoolMax})
	}
	// Backup service and cluster-init hold a few connections each.
	roles = append(roles, deploycheck.RolePool{Role: "backup+init", Instances: 1, PoolMax: 4})
	budget, check := deploycheck.AssessConnectionBudget(roles, inv.Postgres.Reserved, inv.Postgres.MaxConnections)
	if check.Status == "blocked" {
		bad("postgres connection budget: %s", check.Detail)
	}
	sort.Strings(errs)
	if len(errs) > 0 {
		return budget, errors.New(strings.Join(errs, "\n"))
	}
	return budget, nil
}

// spread requires members on distinct nodes and more than one fault domain;
// for quorum groups no single domain may hold a majority.
func (inv *Inventory) spread(name string, members []string, majority bool, bad func(string, ...any)) {
	count := map[string]int{}
	nodes := map[string]bool{}
	for _, m := range members {
		if nodes[m] {
			bad("%s lists node %s twice", name, m)
		}
		nodes[m] = true
		if n, ok := inv.node(m); ok {
			count[n.FaultDomain]++
		}
	}
	if len(members) > 1 && len(count) < 2 {
		bad("%s members are all in one fault domain", name)
	}
	if majority {
		for d, c := range count {
			if c*2 > len(members) {
				bad("%s: fault domain %s holds %d of %d members (a majority)", name, d, c, len(members))
			}
		}
	}
}

func secretLike(k string) bool {
	k = strings.ToUpper(k)
	for _, s := range []string{"PASSWORD", "SECRET", "TOKEN", "KEY", "DSN"} {
		if strings.Contains(k, s) {
			return true
		}
	}
	return false
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
