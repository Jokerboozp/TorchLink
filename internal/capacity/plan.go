// Package capacity implements the one-click capacity measurement loop described
// in docs/CLUSTER_AND_CAPACITY_PLAN.md (phase P1): plan validation, distributed
// open-loop load, per-instance observation, drain, ID reconciliation, boundary
// search and deterministic reports. The CLI and any later management page share
// this single schema and statistics implementation.
package capacity

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// SchemaVersion is the only plan and summary schema this package reads.
const SchemaVersion = 1

// Duration is a time.Duration written as "90s" / "2m" in plans and results.
type Duration time.Duration

func (d Duration) D() time.Duration { return time.Duration(d) }
func (d Duration) String() string   { return time.Duration(d).String() }
func (d Duration) MarshalYAML() (any, error) {
	return time.Duration(d).String(), nil
}
func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	v, err := time.ParseDuration(strings.TrimSpace(n.Value))
	if err != nil {
		return fmt.Errorf("line %d: invalid duration %q", n.Line, n.Value)
	}
	*d = Duration(v)
	return nil
}
func (d Duration) MarshalJSON() ([]byte, error) { return []byte(`"` + d.String() + `"`), nil }
func (d *Duration) UnmarshalJSON(b []byte) error {
	v, err := time.ParseDuration(strings.Trim(string(b), `"`))
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

type Plan struct {
	SchemaVersion int         `yaml:"schemaVersion" json:"schemaVersion"`
	Name          string      `yaml:"name" json:"name"`
	Suite         string      `yaml:"suite" json:"suite"`
	Preset        string      `yaml:"preset" json:"preset"`
	Seed          int64       `yaml:"seed" json:"seed"`
	Target        Target      `yaml:"target" json:"target"`
	Credentials   Credentials `yaml:"credentials" json:"credentials"`
	Fixtures      Fixtures    `yaml:"fixtures" json:"fixtures"`
	Load          Load        `yaml:"load" json:"load"`
	Search        SearchSpec  `yaml:"search" json:"search"`
	SLO           SLO         `yaml:"slo" json:"slo"`
	Budget        Budget      `yaml:"budget" json:"budget"`
	Modules       Modules     `yaml:"modules" json:"modules"`
	Faults        Faults      `yaml:"faults" json:"faults"`
	Outputs       Outputs     `yaml:"outputs" json:"outputs"`
}

type Target struct {
	InventoryRef     string `yaml:"inventoryRef" json:"inventoryRef"`
	EnvironmentClass string `yaml:"environmentClass" json:"environmentClass"`
	Deployment       string `yaml:"deployment" json:"deployment"`
}

// Credentials only name secrets; values are resolved at run time and never
// written to evidence.
type Credentials struct {
	OperatorSecretRef string `yaml:"operatorSecretRef" json:"operatorSecretRef"`
	AgentSecretRef    string `yaml:"agentSecretRef,omitempty" json:"agentSecretRef,omitempty"`
}

type Fixtures struct {
	Tenant       string `yaml:"tenant" json:"tenant"`
	Product      string `yaml:"product" json:"product"`
	DevicePrefix string `yaml:"devicePrefix" json:"devicePrefix"`
	DeviceCount  int    `yaml:"deviceCount" json:"deviceCount"`
	ReuseDevices bool   `yaml:"reuseDevices" json:"reuseDevices"`
	MessageBytes int    `yaml:"messageBytes" json:"messageBytes"`
	Fields       int    `yaml:"fields" json:"fields"`
	// AlarmFraction sets stressAlarm=1 on that share of reports; alarm
	// outcomes are observed, not reconciled, in P1.
	AlarmFraction float64 `yaml:"alarmFraction" json:"alarmFraction"`
	// AlarmRuleID names an existing rule that fires on stressAlarm=1; when
	// set, every device's alarms are reconciled against its message
	// sequence. AlarmRecovers states the rule recovers on stressAlarm=0.
	AlarmRuleID   string `yaml:"alarmRuleId" json:"alarmRuleId"`
	AlarmRecovers bool   `yaml:"alarmRecovers" json:"alarmRecovers"`
	// AutoProvision creates or reuses the test product (standard protocol)
	// and, with alarmFraction > 0, the stressAlarm test rule through the
	// platform API before the run. Defaults: product cap-standard, rule
	// cap-stress-alarm (recovers on stressAlarm=0).
	AutoProvision bool `yaml:"autoProvision" json:"autoProvision"`
}

// Names used by fixtures.autoProvision when the plan leaves them empty.
const (
	AutoProductID = "cap-standard"
	AutoRuleID    = "cap-stress-alarm"
)

type Load struct {
	IngressShare             map[string]float64 `yaml:"ingressShare" json:"ingressShare"`
	InitialMessagesPerSecond float64            `yaml:"initialMessagesPerSecond" json:"initialMessagesPerSecond"`
	QueryRequestsPerSecond   float64            `yaml:"queryRequestsPerSecond" json:"queryRequestsPerSecond"`
	QueryMix                 map[string]float64 `yaml:"queryMix" json:"queryMix"`
	ScaleQueries             bool               `yaml:"scaleQueries" json:"scaleQueries"`
	MQTTConnections          int                `yaml:"mqttConnections" json:"mqttConnections"`
	TCPConnections           int                `yaml:"tcpConnections" json:"tcpConnections"`
	PerDeviceMaxPerSecond    float64            `yaml:"perDeviceMaxPerSecond" json:"perDeviceMaxPerSecond"`
	RequestTimeout           Duration           `yaml:"requestTimeout" json:"requestTimeout"`
	MaxInflight              int                `yaml:"maxInflight" json:"maxInflight"`
	ReceiptWait              Duration           `yaml:"receiptWait" json:"receiptWait"`
	ReceiptRetries           int                `yaml:"receiptRetries" json:"receiptRetries"`
}

type SearchSpec struct {
	Rates                 []float64 `yaml:"rates,omitempty" json:"rates,omitempty"`
	RampFactor            float64   `yaml:"rampFactor" json:"rampFactor"`
	Warmup                Duration  `yaml:"warmup" json:"warmup"`
	Measure               Duration  `yaml:"measure" json:"measure"`
	BoundaryRelativeWidth float64   `yaml:"boundaryRelativeWidth" json:"boundaryRelativeWidth"`
	CandidateHold         Duration  `yaml:"candidateHold" json:"candidateHold"`
	Repeats               int       `yaml:"repeats" json:"repeats"`
	DrainTimeout          Duration  `yaml:"drainTimeout" json:"drainTimeout"`
	Cooldown              Duration  `yaml:"cooldown" json:"cooldown"`
	MaxSteps              int       `yaml:"maxSteps" json:"maxSteps"`
	ObserveInterval       Duration  `yaml:"observeInterval" json:"observeInterval"`
}

type SLO struct {
	SuccessRatio                       float64  `yaml:"successRatio" json:"successRatio"`
	SendTolerance                      float64  `yaml:"sendTolerance" json:"sendTolerance"`
	QueryP95                           Duration `yaml:"queryP95" json:"queryP95"`
	QueryP99                           Duration `yaml:"queryP99" json:"queryP99"`
	BusinessP95                        Duration `yaml:"businessP95" json:"businessP95"`
	BusinessP99                        Duration `yaml:"businessP99" json:"businessP99"`
	MaximumUnaccountedConfirmedMessage int      `yaml:"maximumUnaccountedConfirmedMessages" json:"maximumUnaccountedConfirmedMessages"`
	MaxBacklogGrowthRatio              float64  `yaml:"maxBacklogGrowthRatio" json:"maxBacklogGrowthRatio"`
	MaxClockUncertainty                Duration `yaml:"maxClockUncertainty" json:"maxClockUncertainty"`
}

type Budget struct {
	MaximumWallTime          Duration `yaml:"maximumWallTime" json:"maximumWallTime"`
	MaximumMessagesPerSecond float64  `yaml:"maximumMessagesPerSecond" json:"maximumMessagesPerSecond"`
	MaximumDevices           int      `yaml:"maximumDevices" json:"maximumDevices"`
	MaximumEvidenceGiB       float64  `yaml:"maximumEvidenceGiB" json:"maximumEvidenceGiB"`
}

// Modules are business scenarios offered alongside device load. Rates are
// per minute across all agents; P95 is the module's own latency SLO.
type Modules struct {
	AI        AIModule        `yaml:"ai" json:"ai"`
	Video     VideoModule     `yaml:"video" json:"video"`
	Backup    BackupModule    `yaml:"backup" json:"backup"`
	Knowledge KnowledgeModule `yaml:"knowledge" json:"knowledge"`
	Realtime  RealtimeModule  `yaml:"realtime" json:"realtime"`
	Exports   ExportsModule   `yaml:"exports" json:"exports"`
	OpenAPI   OpenAPIModule   `yaml:"openapi" json:"openapi"`
}

// AIModule runs manual alarm analyses on active alarms of the test tenant.
// Mode "mock" states that the platform uses cmd/harness-mock; "real" needs
// explicit request and time budgets because each run costs model quota.
type AIModule struct {
	Enabled       bool     `yaml:"enabled" json:"enabled"`
	Mode          string   `yaml:"mode" json:"mode"`
	RunsPerMinute float64  `yaml:"runsPerMinute" json:"runsPerMinute"`
	MaxRuns       int      `yaml:"maxRuns" json:"maxRuns"`
	Timeout       Duration `yaml:"timeout" json:"timeout"`
	P95           Duration `yaml:"p95" json:"p95"`
}

type KnowledgeModule struct {
	Enabled          bool     `yaml:"enabled" json:"enabled"`
	UploadsPerMinute float64  `yaml:"uploadsPerMinute" json:"uploadsPerMinute"`
	WorkflowID       string   `yaml:"workflowId" json:"workflowId"`
	DocumentBytes    int      `yaml:"documentBytes" json:"documentBytes"`
	P95              Duration `yaml:"p95" json:"p95"`
}

// VideoModule opens and closes HLS play sessions on listed test cameras;
// with inventory.web set it also fetches the playlist and first segment.
type VideoModule struct {
	Enabled           bool     `yaml:"enabled" json:"enabled"`
	Cameras           []string `yaml:"cameras" json:"cameras"`
	SessionsPerMinute float64  `yaml:"sessionsPerMinute" json:"sessionsPerMinute"`
	Timeout           Duration `yaml:"timeout" json:"timeout"`
	P95               Duration `yaml:"p95" json:"p95"`
}

// BackupModule runs one backup per step under load, downloads and checks
// every file, and optionally restores it into the independent target.
type BackupModule struct {
	Enabled bool     `yaml:"enabled" json:"enabled"`
	Restore bool     `yaml:"restore" json:"restore"`
	Timeout Duration `yaml:"timeout" json:"timeout"`
}

// RealtimeModule holds MQTT subscribers like browser pages and measures
// alarm and state push delivery.
type RealtimeModule struct {
	Enabled     bool     `yaml:"enabled" json:"enabled"`
	Subscribers int      `yaml:"subscribers" json:"subscribers"`
	P95         Duration `yaml:"p95" json:"p95"`
}

type ExportsModule struct {
	Enabled               bool     `yaml:"enabled" json:"enabled"`
	RawDownloadsPerMinute float64  `yaml:"rawDownloadsPerMinute" json:"rawDownloadsPerMinute"`
	ReplaysPerMinute      float64  `yaml:"replaysPerMinute" json:"replaysPerMinute"`
	InspectionsPerMinute  float64  `yaml:"inspectionsPerMinute" json:"inspectionsPerMinute"`
	Timeout               Duration `yaml:"timeout" json:"timeout"`
	P95                   Duration `yaml:"p95" json:"p95"`
}

type OpenAPIModule struct {
	Enabled           bool     `yaml:"enabled" json:"enabled"`
	KeySecretRef      string   `yaml:"keySecretRef" json:"keySecretRef"`
	RequestsPerSecond float64  `yaml:"requestsPerSecond" json:"requestsPerSecond"`
	P95               Duration `yaml:"p95" json:"p95"`
}

// ModuleStreams maps enabled modules to agent streams with rates per
// second (across all agents).
func (p *Plan) ModuleStreams() map[string]float64 {
	m := p.Modules
	out := map[string]float64{}
	if m.AI.Enabled {
		out["ai"] = m.AI.RunsPerMinute / 60
	}
	if m.Knowledge.Enabled {
		out["knowledge"] = m.Knowledge.UploadsPerMinute / 60
	}
	if m.Video.Enabled {
		out["video"] = m.Video.SessionsPerMinute / 60
	}
	if m.Exports.Enabled {
		out["export_raw"] = m.Exports.RawDownloadsPerMinute / 60
		out["export_replay"] = m.Exports.ReplaysPerMinute / 60
		out["export_inspection"] = m.Exports.InspectionsPerMinute / 60
	}
	if m.OpenAPI.Enabled {
		out["openapi"] = m.OpenAPI.RequestsPerSecond
	}
	for k, v := range out {
		if v <= 0 {
			delete(out, k)
		}
	}
	return out
}

// ModuleP95 is the latency SLO of a module stream (0 = none).
func (p *Plan) ModuleP95(stream string) time.Duration {
	m := p.Modules
	switch stream {
	case "ai":
		return m.AI.P95.D()
	case "knowledge":
		return m.Knowledge.P95.D()
	case "video":
		return m.Video.P95.D()
	case "export_raw", "export_replay", "export_inspection":
		return m.Exports.P95.D()
	case "openapi":
		return m.OpenAPI.P95.D()
	case "realtime":
		return m.Realtime.P95.D()
	}
	return 0
}

// Faults name actions registered on agents (capacity-test agent -fault-allow);
// the plan can only reference them, never supply commands.
type Faults struct {
	Enabled bool          `yaml:"enabled" json:"enabled"`
	Actions []FaultAction `yaml:"actions" json:"actions"`
	// MaxRecovery bounds the time from recovery until the pipeline is back
	// to normal (backlog drained, success restored).
	MaxRecovery Duration `yaml:"maxRecovery" json:"maxRecovery"`
}

type FaultAction struct {
	Agent    string   `yaml:"agent" json:"agent"`
	Action   string   `yaml:"action" json:"action"`
	At       Duration `yaml:"at" json:"at"`
	Duration Duration `yaml:"duration" json:"duration"`
}

type Outputs struct {
	Formats []string `yaml:"formats" json:"formats"`
}

const (
	PresetQuick      = "quick"
	PresetCapacity   = "capacity"
	PresetSoak       = "soak"
	PresetResilience = "resilience"
)

// Streams accepted in load.ingressShare. TCP is GB26875 over TCP and only
// yields protocol ACK evidence (see verifier).
var ingressStreams = []string{"http", "mqtt", "tcp"}

// QueryEndpoints is the fixed management-query allowlist; plans choose a mix
// but cannot supply arbitrary paths.
var QueryEndpoints = map[string]string{
	"devices":     "/api/v1/devices?page=1&pageSize=20",
	"alarms":      "/api/v1/alarms?page=1&pageSize=20",
	"dashboard":   "/api/v1/dashboard",
	"rawMessages": "/api/v1/raw-messages?page=1&pageSize=20",
}

var identifier = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

// LoadPlan reads a plan file strictly: unknown fields are errors so a typo
// never silently becomes a default.
func LoadPlan(path string) (*Plan, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParsePlan(b)
}

func ParsePlan(b []byte) (*Plan, error) {
	// Defaults are filled before decoding so an explicit zero (warmup: 0s)
	// stays zero instead of being mistaken for "unset".
	p := defaultPlan()
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("plan: %w", err)
	}
	if p.Search.CandidateHold == 0 {
		p.Search.CandidateHold = p.Search.Measure
	}
	if f := &p.Fixtures; f.AutoProvision {
		if f.Product == "" {
			f.Product = AutoProductID
		}
		if f.AlarmFraction > 0 && f.AlarmRuleID == "" {
			f.AlarmRuleID, f.AlarmRecovers = AutoRuleID, true
		}
	}
	return &p, nil
}

func defaultPlan() Plan {
	return Plan{
		Suite:    "core",
		Preset:   PresetCapacity,
		Fixtures: Fixtures{DevicePrefix: "cap", Fields: 6},
		Load: Load{
			// Current per-device standard ingest allowance (onboarding.Service.Allow).
			PerDeviceMaxPerSecond: 20,
			RequestTimeout:        Duration(10 * time.Second),
			MaxInflight:           2048,
			ReceiptWait:           Duration(5 * time.Second),
			ReceiptRetries:        2,
		},
		Search: SearchSpec{
			RampFactor:            1.5,
			Warmup:                Duration(2 * time.Minute),
			Measure:               Duration(10 * time.Minute),
			BoundaryRelativeWidth: 0.05,
			Repeats:               3,
			DrainTimeout:          Duration(10 * time.Minute),
			Cooldown:              Duration(10 * time.Second),
			MaxSteps:              20,
			ObserveInterval:       Duration(5 * time.Second),
		},
		SLO: SLO{
			SuccessRatio:          0.999,
			SendTolerance:         0.02,
			QueryP95:              Duration(500 * time.Millisecond),
			QueryP99:              Duration(1500 * time.Millisecond),
			BusinessP95:           Duration(2 * time.Second),
			BusinessP99:           Duration(5 * time.Second),
			MaxBacklogGrowthRatio: 0.05,
			MaxClockUncertainty:   Duration(100 * time.Millisecond),
		},
		Outputs: Outputs{Formats: []string{"html", "markdown", "json", "csv", "svg"}},
		Modules: Modules{
			AI:        AIModule{Mode: "mock", Timeout: Duration(3 * time.Minute), P95: Duration(2 * time.Minute)},
			Knowledge: KnowledgeModule{DocumentBytes: 16 << 10, P95: Duration(30 * time.Second)},
			Video:     VideoModule{Timeout: Duration(20 * time.Second), P95: Duration(5 * time.Second)},
			Backup:    BackupModule{Timeout: Duration(30 * time.Minute)},
			Realtime:  RealtimeModule{P95: Duration(3 * time.Second)},
			Exports:   ExportsModule{Timeout: Duration(5 * time.Minute), P95: Duration(60 * time.Second)},
			OpenAPI:   OpenAPIModule{P95: Duration(500 * time.Millisecond)},
		},
		Faults: Faults{MaxRecovery: Duration(5 * time.Minute)},
	}
}

// Validate reports every problem at once. Modules and actions that P1 does not
// implement are rejected instead of being reported as covered.
func (p *Plan) Validate() error {
	var errs []string
	bad := func(format string, a ...any) { errs = append(errs, fmt.Sprintf(format, a...)) }
	if p.SchemaVersion != SchemaVersion {
		bad("schemaVersion must be %d", SchemaVersion)
	}
	if !identifier.MatchString(p.Name) {
		bad("name must match %s", identifier)
	}
	switch p.Suite {
	case "full", "core", "custom":
	default:
		bad("suite must be full, core or custom")
	}
	switch p.Preset {
	case PresetQuick, PresetCapacity, PresetSoak, PresetResilience:
	default:
		bad("preset must be quick, capacity, soak or resilience")
	}
	f := p.Fixtures
	if !identifier.MatchString(f.Tenant) || !identifier.MatchString(f.Product) || !identifier.MatchString(f.DevicePrefix) {
		bad("fixtures.tenant, fixtures.product and fixtures.devicePrefix must be plain identifiers")
	}
	if f.DeviceCount < 1 {
		bad("fixtures.deviceCount must be at least 1")
	}
	if p.Budget.MaximumDevices > 0 && f.DeviceCount > p.Budget.MaximumDevices {
		bad("fixtures.deviceCount %d exceeds budget.maximumDevices %d", f.DeviceCount, p.Budget.MaximumDevices)
	}
	if f.MessageBytes < 0 || f.MessageBytes > 60<<10 {
		bad("fixtures.messageBytes must be between 0 and 61440 (standard ingest accepts 64 KiB)")
	}
	if f.Fields < 1 || f.Fields > 100 {
		bad("fixtures.fields must be between 1 and 100")
	}
	if f.AlarmFraction < 0 || f.AlarmFraction > 1 {
		bad("fixtures.alarmFraction must be between 0 and 1")
	}
	l := p.Load
	sum := 0.0
	for k, v := range l.IngressShare {
		if !contains(ingressStreams, k) {
			bad("load.ingressShare.%s is not a supported stream (http, mqtt, tcp)", k)
		}
		if v < 0 {
			bad("load.ingressShare.%s must not be negative", k)
		}
		sum += v
	}
	if math.Abs(sum-1) > 1e-6 {
		bad("load.ingressShare must sum to 1 (got %.4f)", sum)
	}
	if l.InitialMessagesPerSecond <= 0 && len(p.Search.Rates) == 0 {
		bad("load.initialMessagesPerSecond must be positive")
	}
	if l.QueryRequestsPerSecond < 0 {
		bad("load.queryRequestsPerSecond must not be negative")
	}
	if l.QueryRequestsPerSecond > 0 {
		qs := 0.0
		for k, v := range l.QueryMix {
			if _, ok := QueryEndpoints[k]; !ok {
				bad("load.queryMix.%s is not an allowed query (%s)", k, strings.Join(sortedKeys(QueryEndpoints), ", "))
			}
			if v < 0 {
				bad("load.queryMix.%s must not be negative", k)
			}
			qs += v
		}
		if math.Abs(qs-1) > 1e-6 {
			bad("load.queryMix must sum to 1 (got %.4f)", qs)
		}
	}
	if l.IngressShare["mqtt"] > 0 && (l.MQTTConnections < 1 || l.MQTTConnections > f.DeviceCount) {
		bad("load.mqttConnections must be between 1 and fixtures.deviceCount when MQTT has a share")
	}
	if l.IngressShare["tcp"] > 0 && (l.TCPConnections < 1 || l.TCPConnections > 65535) {
		bad("load.tcpConnections must be between 1 and 65535 when TCP has a share")
	}
	if l.IngressShare["http"] > 0 && f.DeviceCount-l.MQTTConnections < 1 && l.IngressShare["mqtt"] > 0 {
		bad("fixtures.deviceCount must leave at least one device for HTTP after MQTT publishers")
	}
	if l.PerDeviceMaxPerSecond <= 0 || l.MaxInflight < 1 || l.RequestTimeout <= 0 || l.ReceiptWait <= 0 || l.ReceiptRetries < 0 || l.ReceiptRetries > 10 {
		bad("load limits (perDeviceMaxPerSecond, maxInflight, requestTimeout, receiptWait, receiptRetries 0-10) are invalid")
	}
	s := p.Search
	if s.RampFactor <= 1 || s.RampFactor > 4 {
		bad("search.rampFactor must be in (1, 4]")
	}
	if s.Warmup < 0 || s.Measure < Duration(10*time.Second) || s.CandidateHold < s.Measure {
		bad("search.measure must be at least 10s, warmup non-negative, candidateHold >= measure")
	}
	if s.BoundaryRelativeWidth <= 0 || s.BoundaryRelativeWidth > 1 {
		bad("search.boundaryRelativeWidth must be in (0, 1]")
	}
	if s.Repeats < 1 || s.Repeats > 10 || s.MaxSteps < 1 || s.MaxSteps > 200 {
		bad("search.repeats must be 1-10 and search.maxSteps 1-200")
	}
	if s.DrainTimeout < Duration(10*time.Second) || s.ObserveInterval < Duration(time.Second) {
		bad("search.drainTimeout must be at least 10s and observeInterval at least 1s")
	}
	for _, r := range s.Rates {
		if r <= 0 {
			bad("search.rates must be positive")
		}
	}
	if (p.Preset == PresetSoak || p.Preset == PresetResilience) && len(s.Rates) != 1 {
		bad("preset soak needs exactly one rate in search.rates")
	}
	o := p.SLO
	if o.SuccessRatio <= 0 || o.SuccessRatio > 1 || o.SendTolerance <= 0 || o.SendTolerance >= 1 || o.MaximumUnaccountedConfirmedMessage < 0 || o.MaxBacklogGrowthRatio <= 0 {
		bad("slo ratios are out of range")
	}
	if o.QueryP95 <= 0 || o.QueryP99 < o.QueryP95 || o.BusinessP95 <= 0 || o.BusinessP99 < o.BusinessP95 {
		bad("slo latency limits must be positive and P99 >= P95")
	}
	b := p.Budget
	if b.MaximumWallTime <= 0 || b.MaximumMessagesPerSecond <= 0 || b.MaximumEvidenceGiB <= 0 {
		bad("budget.maximumWallTime, maximumMessagesPerSecond and maximumEvidenceGiB are required")
	}
	for _, r := range p.planRates() {
		if r > b.MaximumMessagesPerSecond {
			bad("rate %.0f exceeds budget.maximumMessagesPerSecond %.0f", r, b.MaximumMessagesPerSecond)
		}
		if msg := p.perDeviceViolation(r); msg != "" {
			bad("%s", msg)
		}
	}
	m := p.Modules
	if m.AI.Enabled {
		if m.AI.Mode != "real" && m.AI.Mode != "mock" {
			bad("modules.ai.mode must be real or mock (mock = the platform points IOT_AI_HARNESS_URL at cmd/harness-mock)")
		}
		if m.AI.RunsPerMinute <= 0 || m.AI.MaxRuns < 1 {
			bad("modules.ai needs runsPerMinute and a hard maxRuns budget")
		}
	}
	if m.Knowledge.Enabled && (m.Knowledge.UploadsPerMinute <= 0 || m.Knowledge.WorkflowID == "" || m.Knowledge.DocumentBytes < 0 || m.Knowledge.DocumentBytes > 8<<20) {
		bad("modules.knowledge needs uploadsPerMinute, workflowId and documentBytes up to 8 MiB")
	}
	if m.Video.Enabled && (len(m.Video.Cameras) == 0 || m.Video.SessionsPerMinute <= 0) {
		bad("modules.video needs test cameras and sessionsPerMinute")
	}
	if m.Realtime.Enabled && (m.Realtime.Subscribers < 1 || m.Realtime.Subscribers > 1000) {
		bad("modules.realtime.subscribers must be 1-1000")
	}
	if m.Exports.Enabled && m.Exports.RawDownloadsPerMinute+m.Exports.ReplaysPerMinute+m.Exports.InspectionsPerMinute <= 0 {
		bad("modules.exports needs at least one positive rate")
	}
	if m.OpenAPI.Enabled && (m.OpenAPI.KeySecretRef == "" || m.OpenAPI.RequestsPerSecond <= 0) {
		bad("modules.openapi needs keySecretRef and requestsPerSecond")
	}
	if p.Faults.Enabled != (p.Preset == PresetResilience) {
		bad("faults are used by (and only by) preset resilience")
	}
	if p.Faults.Enabled {
		if len(p.Faults.Actions) == 0 {
			bad("faults.actions must list at least one registered agent action")
		}
		for i, a := range p.Faults.Actions {
			if !identifier.MatchString(a.Agent) || !identifier.MatchString(a.Action) || a.At < 0 || a.Duration <= 0 || a.At+a.Duration > p.Search.Measure {
				bad("faults.actions[%d] needs agent, action, at >= 0 and duration > 0 within search.measure", i)
			}
		}
		if len(p.Search.Rates) != 1 {
			bad("preset resilience needs exactly one background rate in search.rates")
		}
	}
	for _, format := range p.Outputs.Formats {
		switch format {
		case "html", "markdown", "json", "csv", "svg", "png":
		default:
			bad("outputs.formats %q is unknown", format)
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "\n"))
	}
	return nil
}

// planRates are the explicit rates that can be checked before running.
func (p *Plan) planRates() []float64 {
	if len(p.Search.Rates) > 0 {
		return p.Search.Rates
	}
	return []float64{p.Load.InitialMessagesPerSecond}
}

// perDeviceViolation flags a rate that would exceed the platform's per-device
// allowance; the result would measure the policy, not capacity.
func (p *Plan) perDeviceViolation(rate float64) string {
	for _, stream := range []string{"http", "mqtt"} {
		share := p.Load.IngressShare[stream]
		if share == 0 {
			continue
		}
		devices := p.streamDevices(stream)
		if devices > 0 && rate*share/float64(devices) > p.Load.PerDeviceMaxPerSecond {
			return fmt.Sprintf("rate %.0f puts %.1f msg/s on each of %d %s devices, above load.perDeviceMaxPerSecond %.0f", rate, rate*share/float64(devices), devices, stream, p.Load.PerDeviceMaxPerSecond)
		}
	}
	return ""
}

// MaxRateForDevices is the highest total rate the per-device allowance permits.
func (p *Plan) MaxRateForDevices() float64 {
	limit := math.Inf(1)
	for _, stream := range []string{"http", "mqtt"} {
		if share := p.Load.IngressShare[stream]; share > 0 {
			limit = math.Min(limit, p.Load.PerDeviceMaxPerSecond*float64(p.streamDevices(stream))/share)
		}
	}
	return limit
}

// streamDevices is how many fixture devices publish on a stream: MQTT uses the
// first mqttConnections devices, HTTP the rest (or all when MQTT is unused).
func (p *Plan) streamDevices(stream string) int {
	switch stream {
	case "mqtt":
		return p.Load.MQTTConnections
	case "http":
		if p.Load.IngressShare["mqtt"] > 0 {
			return p.Fixtures.DeviceCount - p.Load.MQTTConnections
		}
		return p.Fixtures.DeviceCount
	}
	return 0
}

// QueryRate is the management query rate used together with message rate r.
func (p *Plan) QueryRate(r float64) float64 {
	q := p.Load.QueryRequestsPerSecond
	if p.Load.ScaleQueries && p.Load.InitialMessagesPerSecond > 0 {
		q *= r / p.Load.InitialMessagesPerSecond
	}
	return q
}

// Sanitized returns YAML with secret references kept as names only and a hash
// computed over that content.
func (p *Plan) Sanitized() ([]byte, string, error) {
	b, err := yaml.Marshal(p)
	if err != nil {
		return nil, "", err
	}
	h := sha256.Sum256(b)
	return b, hex.EncodeToString(h[:]), nil
}

// ResolveRef resolves a file reference relative to the plan's directory.
func ResolveRef(planPath, ref string) string {
	if ref == "" || filepath.IsAbs(ref) {
		return ref
	}
	return filepath.Join(filepath.Dir(planPath), ref)
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
