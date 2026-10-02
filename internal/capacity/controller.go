package capacity

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// Execution states. Capacity verdicts are recorded separately.
const (
	StatusQueued     = "QUEUED"
	StatusPreflight  = "PREFLIGHT"
	StatusPreparing  = "PREPARING"
	StatusWarmup     = "WARMUP"
	StatusRunning    = "RUNNING"
	StatusDraining   = "DRAINING"
	StatusVerifying  = "VERIFYING"
	StatusReporting  = "REPORTING"
	StatusFinished   = "FINISHED"
	StatusCancelling = "CANCELLING"
	StatusCancelled  = "CANCELLED"
	StatusFailed     = "FAILED"
)

// StopFile is created by `capacity-test stop`; "force" skips the drain.
const StopFile = "STOP"

type RunOptions struct {
	PlanPath      string
	InventoryPath string
	SecretsPath   string
	ResultsDir    string
	WorkDir       string
	SourceCommit  string
	Log           io.Writer
	// FaultAllow is the fault allowlist of in-process agents (remote agents
	// load their own with capacity-test agent -fault-allow).
	FaultAllow FaultAllowlist
	// OnStart receives the run ID once the evidence directory exists.
	OnStart func(runID string)
	// Inventory replaces InventoryPath (the capacity module builds it from
	// its environment); ExtraSecrets are in-memory secrets it references.
	Inventory    *Inventory
	ExtraSecrets map[string]string
	// OperatorToken, when set, is used instead of credentials.operatorSecretRef:
	// the platform issues it for the user who started the run.
	OperatorToken string
	// Tenant overrides fixtures.tenant (the caller's tenant).
	Tenant string
	// ResumeRunID continues an interrupted run in ResultsDir: completed
	// steps are replayed from phases/, agents are prepared again with the
	// next generation and the search continues where it stopped.
	ResumeRunID string
	// Test seams.
	NewStore func(ctx context.Context, pgDSN, chURL string) (Store, error)
	NewAgent func(t AgentTarget, token, workDir string) Agent
}

type PhaseBrief struct {
	PhaseID    string  `json:"phaseId"`
	Kind       string  `json:"kind"`
	Rate       float64 `json:"rate"`
	Verdict    string  `json:"verdict"`
	StopReason string  `json:"stopReason,omitempty"`
}

type AgentClock struct {
	Name          string  `json:"name"`
	Lost          bool    `json:"lost"`
	OffsetMS      float64 `json:"offsetMs"`
	UncertaintyMS float64 `json:"uncertaintyMs"`
}

type RunState struct {
	SchemaVersion int          `json:"schemaVersion"`
	RunID         string       `json:"runId"`
	Status        string       `json:"status"`
	PID           int          `json:"pid"`
	StartedAt     int64        `json:"startedAt"`
	UpdatedAt     int64        `json:"updatedAt"`
	Message       string       `json:"message,omitempty"`
	PhaseID       string       `json:"phaseId,omitempty"`
	TargetRate    float64      `json:"targetRate,omitempty"`
	MeasureFrom   int64        `json:"measureFrom,omitempty"`
	MeasureTo     int64        `json:"measureTo,omitempty"`
	Completed     []PhaseBrief `json:"completed"`
	Agents        []AgentClock `json:"agents"`
	StopReason    string       `json:"stopReason,omitempty"`
	// Generation increases on every resume; agents refuse older generations.
	Generation int64 `json:"generation,omitempty"`
	// Result is the execution outcome decided before the report is written.
	Result string `json:"result,omitempty"`
}

type Event struct {
	At      int64  `json:"at"`
	Type    string `json:"type"`
	Status  string `json:"status,omitempty"`
	PhaseID string `json:"phaseId,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

// PreflightCheck records one readiness check before any load is offered.
type PreflightCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

type Manifest struct {
	RunID          string   `json:"runId"`
	Tenant         string   `json:"tenant"`
	Product        string   `json:"product"`
	DevicePrefix   string   `json:"devicePrefix"`
	DevicesCreated []string `json:"devicesCreated"`
	DevicesReused  int      `json:"devicesReused"`
	Devices        []string `json:"devices,omitempty"`
	Retained       []string `json:"retained"`
	Cleanup        []string `json:"cleanup"`
}

type agentHandle struct {
	target  AgentTarget
	agent   Agent
	index   int
	http    []DeviceCredential
	mqtt    []DeviceCredential
	tcp     int
	mu      sync.Mutex
	clocks  []clockSample
	lost    bool
	lostWhy string
}

type clockSample struct {
	at          time.Time
	offset, unc time.Duration
}

func (h *agentHandle) clock() (time.Duration, time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	best := clockSample{unc: time.Duration(math.MaxInt64)}
	for _, c := range h.clocks {
		if c.unc < best.unc {
			best = c
		}
	}
	if best.unc == time.Duration(math.MaxInt64) {
		return 0, time.Second
	}
	return best.offset, best.unc
}

type controller struct {
	opt       RunOptions
	plan      *Plan
	inv       *Inventory
	secrets   *Secrets
	runID     string
	dir       string
	started   time.Time
	state     RunState
	stateMu   sync.Mutex
	events    *os.File
	agents    []*agentHandle
	collector *Collector
	verifier  *Verifier
	store     Store
	opToken   string
	agentTok  string
	httpc     *http.Client
	gen       int64
	phaseN    int
	stop      chan string // "soft" or "force"
	stopOnce  sync.Once
	forced    bool
	lastDrain time.Duration
	// unrecovered marks that the previous step's backlog had not drained
	// before this step started, so its result cannot be attributed to its rate.
	unrecovered bool
	// agentResults keeps each agent's phase result by "phase/agent".
	agentResults map[string]AgentPhaseResult
	openAPIKey   string
	nodes        *Collector
	// provisioned lists test objects created by fixtures.autoProvision.
	provisioned []string
	// replay holds completed steps of a resumed run, in search order.
	replay  []PhaseRecord
	resumed bool
}

func newRunID(now time.Time) string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return "cap-" + now.Format("20060102-150405") + "-" + hex.EncodeToString(b)
}

// Run executes a plan end to end and always tries to leave a report, also
// when preflight fails or the run is stopped. The returned error is a
// controller failure; capacity outcomes live in the report.
func Run(ctx context.Context, opt RunOptions) (string, error) {
	plan, err := LoadPlan(opt.PlanPath)
	if err != nil {
		return "", err
	}
	if opt.Tenant != "" {
		plan.Fixtures.Tenant = opt.Tenant
	}
	if err = plan.Validate(); err != nil {
		return "", fmt.Errorf("plan is invalid:\n%w", err)
	}
	inv := opt.Inventory
	if inv == nil {
		invPath := opt.InventoryPath
		if invPath == "" {
			invPath = ResolveRef(opt.PlanPath, plan.Target.InventoryRef)
		}
		if invPath == "" {
			return "", errors.New("no inventory: set target.inventoryRef or --inventory")
		}
		if inv, err = LoadInventory(invPath); err != nil {
			return "", err
		}
	}
	secrets, err := LoadSecrets(opt.SecretsPath)
	if err != nil {
		return "", err
	}
	for ref, v := range opt.ExtraSecrets {
		secrets.Set(ref, v)
	}
	if opt.ResultsDir == "" {
		opt.ResultsDir = "capacity-results"
	}
	if opt.WorkDir == "" {
		opt.WorkDir = filepath.Join(opt.ResultsDir, ".work")
	}
	if opt.Log == nil {
		opt.Log = io.Discard
	}
	if opt.NewStore == nil {
		opt.NewStore = func(ctx context.Context, pg, ch string) (Store, error) { return NewPGCHStore(ctx, pg, ch) }
	}
	if opt.NewAgent == nil {
		opt.NewAgent = func(t AgentTarget, token, workDir string) Agent {
			a := defaultAgent(t, token, workDir)
			if w, ok := a.(*Worker); ok && opt.FaultAllow != nil {
				w.SetFaults(opt.FaultAllow)
			}
			return a
		}
	}
	now := time.Now()
	c := &controller{opt: opt, plan: plan, inv: inv, secrets: secrets, runID: newRunID(now), started: now, gen: 1, stop: make(chan string, 1),
		httpc: &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{Proxy: nil}}}
	var prior *RunState
	if opt.ResumeRunID != "" {
		if prior, err = c.loadResume(opt.ResumeRunID, now); err != nil {
			return opt.ResumeRunID, err
		}
	}
	c.dir = filepath.Join(opt.ResultsDir, c.runID)
	for _, d := range []string{c.dir, filepath.Join(c.dir, "phases"), filepath.Join(c.dir, "ledgers"), filepath.Join(c.dir, "observations"), filepath.Join(c.dir, "verification"), opt.WorkDir} {
		if err = os.MkdirAll(d, 0o750); err != nil {
			return "", err
		}
	}
	if c.events, err = os.OpenFile(filepath.Join(c.dir, "events.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640); err != nil {
		return "", err
	}
	defer c.events.Close()
	c.logf("run %s evidence → %s", c.runID, c.dir)
	c.state = RunState{SchemaVersion: SchemaVersion, RunID: c.runID, PID: os.Getpid(), StartedAt: now.UnixMilli(), Completed: []PhaseBrief{}, Generation: c.gen}
	if prior != nil {
		c.state.StartedAt = prior.StartedAt
		_ = os.Remove(filepath.Join(c.dir, StopFile))
		c.event("resume", "", "", fmt.Sprintf("generation %d, replaying %d completed steps", c.gen, len(c.replay)))
	}
	c.setStatus(StatusQueued, "")
	if opt.OnStart != nil {
		opt.OnStart(c.runID)
	}
	return c.runID, c.execute(ctx)
}

func defaultAgent(t AgentTarget, token, workDir string) Agent {
	if t.URL == "" {
		return NewWorker(t.Name, filepath.Join(workDir, "agent-"+t.Name))
	}
	return NewRemoteAgent(t.Name, t.URL, token)
}

func (c *controller) logf(format string, a ...any) {
	fmt.Fprintf(c.opt.Log, "[%s] %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(format, a...))
}

func (c *controller) event(typ, status, phase, detail string) {
	b, _ := json.Marshal(Event{At: time.Now().UnixMilli(), Type: typ, Status: status, PhaseID: phase, Detail: detail})
	_, _ = c.events.Write(append(b, '\n'))
}

func (c *controller) setStatus(status, message string) {
	c.stateMu.Lock()
	c.state.Status, c.state.Message, c.state.UpdatedAt, c.state.Generation = status, message, time.Now().UnixMilli(), c.gen
	// A fresh slice keeps snapshots taken by touch() immutable.
	c.state.Agents = make([]AgentClock, 0, len(c.agents))
	for _, h := range c.agents {
		off, unc := h.clock()
		h.mu.Lock()
		c.state.Agents = append(c.state.Agents, AgentClock{Name: h.target.Name, Lost: h.lost, OffsetMS: ms(off), UncertaintyMS: ms(unc)})
		h.mu.Unlock()
	}
	st := c.state
	c.stateMu.Unlock()
	_ = writeJSONAtomic(filepath.Join(c.dir, "state.json"), st)
	c.event("status", status, st.PhaseID, message)
	c.logf("%s %s", status, message)
}

func ms(d time.Duration) float64 { return math.Round(float64(d.Microseconds())/10) / 100 }

func (c *controller) requestStop(kind string) {
	c.stopOnce.Do(func() { c.stop <- kind })
}

// watchStop turns the STOP file and ctx cancellation into a stop request. A
// second signal (ctx after STOP, or STOP containing "force") forces.
func (c *controller) watchStop(ctx context.Context, done <-chan struct{}) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	soft := false
	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			if soft {
				c.forced = true
			}
			c.requestStop("soft")
			soft = true
			ctx = context.Background()
		case <-t.C:
			if b, err := os.ReadFile(filepath.Join(c.dir, StopFile)); err == nil {
				if strings.Contains(string(b), "force") {
					c.forced = true
				}
				if !soft {
					soft = true
					c.requestStop("soft")
				}
			}
		}
	}
}

func (c *controller) stopped() bool {
	select {
	case k := <-c.stop:
		c.stop <- k
		return true
	default:
		return false
	}
}

func (c *controller) execute(parent context.Context) error {
	done := make(chan struct{})
	defer close(done)
	go c.watchStop(parent, done)
	// searchCtx ends on a stop request; evidence handling uses its own contexts.
	searchCtx, cancelSearch := context.WithCancel(context.Background())
	defer cancelSearch()
	go func() {
		select {
		case <-done:
		case k := <-c.stop:
			c.stop <- k
			cancelSearch()
		}
	}()

	sanitized, planHash, err := c.plan.Sanitized()
	if err == nil {
		err = os.WriteFile(filepath.Join(c.dir, "plan.sanitized.yaml"), sanitized, 0o640)
	}
	if err != nil {
		return err
	}
	_ = writeJSONAtomic(filepath.Join(c.dir, "environment.json"), c.environment(planHash))

	c.setStatus(StatusPreflight, "")
	checks, ok := c.preflight(searchCtx)
	_ = writeJSONAtomic(filepath.Join(c.dir, "preflight.json"), checks)
	defer c.release()
	result := SearchResult{Preset: c.plan.Preset, Classification: ClassUnmeasured}
	finalStatus := StatusFinished
	finalMessage := ""
	if !ok {
		result.StopReason = ReasonInfrastructure
		finalStatus = StatusFailed
		c.state.StopReason = ReasonInfrastructure
		var failed []string
		for _, check := range checks {
			if !check.OK {
				failed = append(failed, check.Name+"："+check.Detail)
			}
		}
		finalMessage = "前置检查失败：" + strings.Join(failed, "；")
		c.setStatus(StatusFailed, finalMessage)
	} else if err = c.prepare(searchCtx); err != nil {
		result.StopReason = ReasonInfrastructure
		finalStatus = StatusFailed
		c.state.StopReason = ReasonInfrastructure
		finalMessage = "测试准备失败：" + err.Error()
		c.setStatus(StatusFailed, finalMessage)
	} else {
		obs := filepath.Join(c.dir, "observations", "metrics.jsonl")
		if c.collector, err = NewCollector(c.inv.Metrics, c.plan.Search.ObserveInterval.D(), obs); err != nil {
			return err
		}
		colCtx, stopCol := context.WithCancel(context.Background())
		go c.collector.Run(colCtx)
		if len(c.inv.Nodes) > 0 {
			nodes, nerr := NewNodeCollector(c.inv.Nodes, c.plan.Search.ObserveInterval.D(), filepath.Join(c.dir, "observations", "nodes.jsonl"))
			if nerr != nil {
				stopCol()
				return nerr
			}
			c.nodes = nodes
			go nodes.Run(colCtx)
			defer nodes.Close()
		}
		hbCtx, stopHB := context.WithCancel(context.Background())
		go c.heartbeats(hbCtx)
		result, err = Search(searchCtx, c.plan, c)
		stopHB()
		stopCol()
		c.collector.Close()
		if err != nil {
			c.event("error", "", "", err.Error())
			finalStatus = StatusFailed
			finalMessage = err.Error()
		}
		if c.stopped() {
			finalStatus = StatusCancelled
			if result.StopReason == "" || result.StopReason == ReasonInfrastructure && err == nil {
				result.StopReason = ReasonCancel
			}
		}
	}
	_ = writeJSONAtomic(filepath.Join(c.dir, "search.json"), result)
	c.state.StopReason = firstNonEmpty(result.StopReason, c.state.StopReason)
	c.state.Result = finalStatus
	c.setStatus(StatusReporting, finalMessage)
	leakCheck := c.secrets.Values(c.secretRefs()...)
	if c.opt.OperatorToken != "" {
		leakCheck = append(leakCheck, c.opt.OperatorToken)
	}
	for _, v := range c.opt.ExtraSecrets {
		leakCheck = append(leakCheck, v)
	}
	if rerr := GenerateReport(c.dir, leakCheck); rerr != nil {
		c.event("error", "", "", "report: "+rerr.Error())
		finalStatus = StatusFailed
		finalMessage = strings.TrimSpace(finalMessage + " 报告生成失败：" + rerr.Error())
		err = errors.Join(err, rerr)
	}
	c.state.Result = finalStatus
	c.setStatus(finalStatus, finalMessage)
	return err
}

func (c *controller) secretRefs() []string {
	return []string{c.plan.Credentials.OperatorSecretRef, c.plan.Credentials.AgentSecretRef, c.inv.Observers.PostgresSecretRef, c.inv.Observers.ClickHouseSecretRef, c.plan.Modules.OpenAPI.KeySecretRef}
}

func (c *controller) environment(planHash string) map[string]any {
	host, _ := os.Hostname()
	agents := make([]map[string]string, 0, len(c.inv.Agents))
	for _, a := range c.inv.Agents {
		mode := "remote"
		if a.URL == "" {
			mode = "in-process"
		}
		agents = append(agents, map[string]string{"name": a.Name, "mode": mode})
	}
	return map[string]any{
		"schemaVersion":    SchemaVersion,
		"runId":            c.runID,
		"planHash":         planHash,
		"sourceCommit":     c.opt.SourceCommit,
		"controllerHost":   host,
		"controllerOS":     runtime.GOOS + "/" + runtime.GOARCH,
		"goVersion":        runtime.Version(),
		"inventory":        c.inv.Name,
		"deployment":       c.plan.Target.Deployment,
		"environmentClass": c.plan.Target.EnvironmentClass,
		"metricsTargets":   c.inv.Metrics,
		"agents":           agents,
		"observers":        map[string]bool{"postgres": c.inv.Observers.PostgresSecretRef != "", "clickhouse": c.inv.Observers.ClickHouseSecretRef != ""},
		"note":             "硬件、镜像摘要与中间件拓扑由清单登记方补充；此文件不含秘密值",
	}
}

func (c *controller) preflight(ctx context.Context) ([]PreflightCheck, bool) {
	var checks []PreflightCheck
	ok := true
	add := func(name string, good bool, detail string) {
		checks = append(checks, PreflightCheck{Name: name, OK: good, Detail: detail})
		ok = ok && good
	}
	var err error
	switch {
	case c.opt.OperatorToken != "":
		c.opToken = c.opt.OperatorToken
	case c.plan.Credentials.OperatorSecretRef == "":
		add("操作员凭据", false, "计划需要 credentials.operatorSecretRef（或由平台容量测试模块代为提供）")
	default:
		if c.opToken, err = c.secrets.Get(c.plan.Credentials.OperatorSecretRef); err != nil {
			add("操作员凭据", false, err.Error())
		}
	}
	needAgentToken := false
	for _, a := range c.inv.Agents {
		needAgentToken = needAgentToken || a.URL != ""
	}
	if needAgentToken {
		if c.plan.Credentials.AgentSecretRef == "" {
			add("Agent 凭据", false, "远程 Agent 需要 credentials.agentSecretRef")
		} else if c.agentTok, err = c.secrets.Get(c.plan.Credentials.AgentSecretRef); err != nil {
			add("Agent 凭据", false, err.Error())
		}
	}
	if ref := c.plan.Modules.OpenAPI.KeySecretRef; c.plan.Modules.OpenAPI.Enabled {
		if c.openAPIKey, err = c.secrets.Get(ref); err != nil {
			add("开放 API 密钥", false, err.Error())
		}
	}
	if c.plan.Modules.Realtime.Enabled && c.inv.MQTT == "" {
		add("实时推送", false, "实时推送场景需要清单的 mqtt 地址")
	}
	if c.plan.Load.IngressShare["mqtt"] > 0 && c.inv.MQTT == "" {
		add("MQTT 入口", false, "计划包含 MQTT 份额但清单没有 mqtt 地址")
	}
	if c.plan.Load.IngressShare["tcp"] > 0 && c.inv.TCP == "" {
		add("TCP 入口", false, "计划包含 TCP 份额但清单没有 tcp 地址")
	}
	pg, err := c.secrets.Get(c.inv.Observers.PostgresSecretRef)
	if err != nil {
		add("核对数据库凭据", false, err.Error())
	}
	ch := ""
	if c.inv.Observers.ClickHouseSecretRef != "" {
		if ch, err = c.secrets.Get(c.inv.Observers.ClickHouseSecretRef); err != nil {
			add("ClickHouse 核对凭据", false, err.Error())
		}
	}
	if !ok {
		return checks, false
	}
	status, _, err := c.get(ctx, "/health/ready", "")
	add("平台就绪", err == nil && status == 200, fmt.Sprintf("GET /health/ready → %d %s", status, errText(err)))
	status, _, err = c.get(ctx, "/api/v1/devices?page=1&pageSize=1", c.opToken)
	add("操作员权限", err == nil && status == 200, fmt.Sprintf("设备列表 → %d %s", status, errText(err)))
	if c.plan.Fixtures.AutoProvision {
		for _, chk := range c.provision(ctx) {
			add(chk.Name, chk.OK, chk.Detail)
		}
	}
	status, body, err := c.get(ctx, "/api/v1/onboarding/preflight?productId="+url.QueryEscape(c.plan.Fixtures.Product), c.opToken)
	ready, detail := testProductReady(body)
	add("测试产品", err == nil && status == 200 && ready, fmt.Sprintf("产品 %s 接入预检 → %d %s %s", c.plan.Fixtures.Product, status, errText(err), detail))
	col, _ := NewCollector(c.inv.Metrics, time.Second, "")
	round := col.Scrape(ctx)
	for _, s := range round.Instances {
		add("指标 "+s.Instance, s.OK, firstNonEmpty(s.Error, fmt.Sprintf("%d 个序列", len(s.Values))))
	}
	if len(c.inv.Nodes) > 0 {
		nodes, _ := NewNodeCollector(c.inv.Nodes, time.Second, "")
		for _, s := range nodes.Scrape(ctx).Instances {
			detail := firstNonEmpty(s.Error, "node-exporter 可读")
			if s.OK && s.Values["cpu_total"] == 0 {
				s.OK, detail = false, "没有 node_cpu_seconds_total，确认是 node-exporter 地址"
			}
			add("主机 "+s.Instance, s.OK, detail)
		}
	}
	if store, err := c.opt.NewStore(ctx, pg, ch); err != nil {
		add("核对数据库", false, err.Error())
	} else {
		c.store = store
		off, unc, cerr := store.ClockOffset(ctx)
		add("数据库时钟", cerr == nil, fmt.Sprintf("偏差 %.1fms ± %.1fms %s", ms(off), ms(unc), errText(cerr)))
		c.verifier = NewVerifier(store, c.plan.Fixtures.Tenant)
	}
	for i, t := range c.inv.Agents {
		h := &agentHandle{target: t, agent: c.opt.NewAgent(t, c.agentTok, c.opt.WorkDir), index: i}
		c.agents = append(c.agents, h)
		st, err := h.agent.Status(ctx)
		switch {
		case err != nil:
			add("Agent "+t.Name, false, err.Error())
		case st.RunID != "" && st.LeaseValid:
			add("Agent "+t.Name, false, "正被运行 "+st.RunID+" 占用，租约未到期")
		default:
			add("Agent "+t.Name, true, fmt.Sprintf("可用，goroutines=%d", st.Goroutines))
		}
		if err == nil {
			for _, f := range c.plan.Faults.Actions {
				if f.Agent == t.Name && !slices.Contains(st.Faults, f.Action) {
					add("故障动作 "+f.Agent+"/"+f.Action, false, "Agent 白名单（-fault-allow）中没有该动作")
				}
			}
		}
	}
	for _, f := range c.plan.Faults.Actions {
		if !slices.ContainsFunc(c.inv.Agents, func(a AgentTarget) bool { return a.Name == f.Agent }) {
			add("故障动作 "+f.Agent+"/"+f.Action, false, "清单中没有该 Agent")
		}
	}
	return checks, ok
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func (c *controller) get(ctx context.Context, path, token string) (int, []byte, error) {
	hdr := map[string]string{}
	if token != "" {
		hdr["Authorization"] = "Bearer " + token
	}
	return doHTTP(ctx, c.httpc, http.MethodGet, strings.TrimRight(c.inv.API, "/")+path, nil, hdr)
}

// prepare provisions or reuses fixture devices, splits them across agents and
// has every agent open its connections. A smaller device population than
// planned is refused rather than measured.
func (c *controller) prepare(ctx context.Context) error {
	c.setStatus(StatusPreparing, "")
	devices, manifest, err := c.fixtures(ctx)
	_ = writeJSONAtomic(filepath.Join(c.dir, "manifest.json"), manifest)
	if err != nil {
		return err
	}
	var mqttDevs, httpDevs []DeviceCredential
	if c.plan.Load.IngressShare["mqtt"] > 0 {
		mqttDevs, httpDevs = devices[:c.plan.Load.MQTTConnections], devices[c.plan.Load.MQTTConnections:]
	} else {
		httpDevs = devices
	}
	if c.plan.Load.IngressShare["http"] == 0 {
		httpDevs = nil
	}
	n := len(c.agents)
	for i, h := range c.agents {
		h.mqtt = chunk(mqttDevs, i, n)
		h.http = chunk(httpDevs, i, n)
		if c.plan.Load.IngressShare["tcp"] > 0 {
			h.tcp = c.plan.Load.TCPConnections/n + boolInt(i < c.plan.Load.TCPConnections%n)
		}
	}
	cfg := AgentConfig{API: strings.TrimRight(c.inv.API, "/"), MQTT: c.inv.MQTT, TCP: c.inv.TCP, OperatorToken: c.opToken, Tenant: c.plan.Fixtures.Tenant, Product: c.plan.Fixtures.Product,
		RequestTimeout: c.plan.Load.RequestTimeout, ReceiptWait: c.plan.Load.ReceiptWait, ReceiptRetries: c.plan.Load.ReceiptRetries, MaxInflight: max(1, c.plan.Load.MaxInflight/n),
		Fields: c.plan.Fixtures.Fields, MessageBytes: c.plan.Fixtures.MessageBytes, AlarmFraction: c.plan.Fixtures.AlarmFraction, Seed: c.plan.Seed, QueryMix: c.plan.Load.QueryMix}
	m := c.plan.Modules
	if m.AI.Enabled {
		cfg.Modules.AIMaxRuns, cfg.Modules.AITimeout = (m.AI.MaxRuns+n-1)/n, m.AI.Timeout
	}
	if m.Knowledge.Enabled {
		cfg.Modules.KnowledgeWorkflow, cfg.Modules.KnowledgeBytes = m.Knowledge.WorkflowID, m.Knowledge.DocumentBytes
	}
	if m.Video.Enabled {
		cfg.Modules.VideoCameras, cfg.Modules.VideoTimeout, cfg.Modules.WebURL = m.Video.Cameras, m.Video.Timeout, strings.TrimRight(c.inv.Web, "/")
	}
	if m.Exports.Enabled {
		cfg.Modules.ExportTimeout = m.Exports.Timeout
	}
	if m.OpenAPI.Enabled {
		cfg.Modules.OpenAPIKey = c.openAPIKey
	}
	var errs []string
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, h := range c.agents {
		wg.Add(1)
		go func(h *agentHandle) {
			defer wg.Done()
			cfg := cfg
			if m.Realtime.Enabled {
				cfg.Modules.RealtimeSubs = m.Realtime.Subscribers/n + boolInt(h.index < m.Realtime.Subscribers%n)
			}
			res, err := h.agent.Prepare(ctx, PrepareRequest{RunID: c.runID, Generation: c.gen, Agent: h.target.Name, AgentIndex: h.index, Lease: Duration(20 * time.Second), Config: cfg, HTTPDevices: h.http, MQTTDevices: h.mqtt, TCPConnections: h.tcp})
			c.sampleClock(ctx, h)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err != nil:
				errs = append(errs, h.target.Name+": "+err.Error())
			case res.MQTTFailed > 0:
				detail := ""
				if res.Failures["token_product_disabled_401"] > 0 {
					detail += "；MQTT 令牌 HTTP 401：测试产品未启用，请在设备模板中核对状态"
				}
				if res.Failures["token_credentials_401"] > 0 || res.Failures["token_http_401"] > 0 {
					detail += "；MQTT 令牌 HTTP 401：设备凭据被拒绝，请核对测试设备启用状态和目标 API"
					if manifest.DevicesReused > 0 {
						detail += "；本次复用了缓存凭据，可将 fixtures.reuseDevices: false 创建新测试设备后重试（保留原设备）"
					}
				}
				errs = append(errs, fmt.Sprintf("%s: MQTT 连接 %d/%d 成功，失败 %s；拒绝以不同设备规模测量%s", h.target.Name, res.MQTTConnected, len(h.mqtt), codeSummary(res.Failures), detail))
			default:
				c.event("agent_prepared", "", "", fmt.Sprintf("%s http=%d mqtt=%d tcp=%d", h.target.Name, res.HTTPDevices, res.MQTTConnected, res.TCPDevices))
			}
		}(h)
	}
	wg.Wait()
	if len(errs) > 0 {
		sort.Strings(errs)
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func chunk[T any](v []T, i, n int) []T {
	if len(v) == 0 {
		return nil
	}
	size := (len(v) + n - 1) / n
	lo, hi := min(i*size, len(v)), min((i+1)*size, len(v))
	return v[lo:hi]
}

// fixtures loads reusable credentials from the private work directory or
// enrolls devices through the normal onboarding API. Credentials never enter
// the run directory; the manifest lists device IDs only.
func (c *controller) fixtures(ctx context.Context) ([]DeviceCredential, Manifest, error) {
	f := c.plan.Fixtures
	m := Manifest{RunID: c.runID, Tenant: f.Tenant, Product: f.Product, DevicePrefix: f.DevicePrefix, DevicesCreated: []string{}}
	credPath := filepath.Join(c.opt.WorkDir, "fixtures", fmt.Sprintf("%s-%s-%s.json", f.Tenant, f.Product, f.DevicePrefix))
	var have []DeviceCredential
	// A resumed run reuses the devices it enrolled before the interruption.
	if f.ReuseDevices || c.resumed {
		if b, err := os.ReadFile(credPath); err == nil {
			_ = json.Unmarshal(b, &have)
		}
	}
	prefix := f.DevicePrefix
	if !f.ReuseDevices {
		prefix += "-" + c.runID[len(c.runID)-6:]
	}
	byID := map[string]DeviceCredential{}
	for _, d := range have {
		byID[d.ID] = d
	}
	out := make([]DeviceCredential, f.DeviceCount)
	var missing []int
	for i := range out {
		id := fmt.Sprintf("%s-%06d", prefix, i)
		m.Devices = append(m.Devices, id)
		if d, ok := byID[id]; ok && d.Secret != "" {
			out[i] = d
			m.DevicesReused++
		} else {
			out[i].ID = id
			missing = append(missing, i)
		}
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 16)
	failures := map[string]uint64{}
	for _, i := range missing {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			id := out[i].ID
			// Synthetic test devices use the normal authorized trial path. This
			// does not certify the template for ordinary device enrollment.
			body, _ := json.Marshal(map[string]any{"trial": true, "requestId": "req-" + id, "productId": f.Product, "device": map[string]any{"id": id, "name": "容量测试 " + id}, "connection": map[string]any{"mode": "standard"}})
			for attempt := 0; attempt < 4; attempt++ {
				status, resp, err := doHTTP(ctx, c.httpc, http.MethodPost, strings.TrimRight(c.inv.API, "/")+"/api/v1/onboarding", body, map[string]string{"Authorization": "Bearer " + c.opToken})
				var v struct {
					Credential struct {
						AccessKey string `json:"accessKey"`
						Secret    string `json:"secret"`
					} `json:"credential"`
				}
				if err == nil && status == http.StatusCreated && json.Unmarshal(resp, &v) == nil && v.Credential.Secret != "" {
					mu.Lock()
					out[i] = DeviceCredential{ID: id, Key: v.Credential.AccessKey, Secret: v.Credential.Secret}
					m.DevicesCreated = append(m.DevicesCreated, id)
					mu.Unlock()
					return
				}
				code := ShortError(err)
				if err == nil {
					code = fmt.Sprint(status)
					var problem struct {
						Detail string `json:"detail"`
					}
					if status >= 400 && json.Unmarshal(resp, &problem) == nil && strings.TrimSpace(problem.Detail) != "" {
						code += "（" + clip(problem.Detail, 240) + "）"
					}
				}
				mu.Lock()
				failures[code]++
				mu.Unlock()
				if err == nil && status/100 == 4 && status != 429 {
					return
				}
				time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
			}
		}(i)
	}
	wg.Wait()
	sort.Strings(m.DevicesCreated)
	all := append(have[:0:0], have...)
	for _, d := range out {
		if d.Secret != "" {
			if _, ok := byID[d.ID]; !ok {
				all = append(all, d)
			}
		}
	}
	if len(m.DevicesCreated) > 0 || f.ReuseDevices {
		if err := os.MkdirAll(filepath.Dir(credPath), 0o700); err == nil {
			b, _ := json.Marshal(all)
			_ = os.WriteFile(credPath, b, 0o600)
		}
	}
	m.Retained = []string{fmt.Sprintf("测试设备 %d 台保留在租户 %s 产品 %s（前缀 %s），凭据在工作目录，可供复测", f.DeviceCount, f.Tenant, f.Product, prefix)}
	m.Cleanup = []string{"压测结束已释放 Agent 连接与租约", "测试设备与数据默认保留；运维中心容量测试页可预览并清理本次运行"}
	for _, p := range c.provisioned {
		m.Retained = append(m.Retained, "自动创建的"+p+"保留，可供复测；最后一次关联运行清理时一并删除")
	}
	for _, d := range out {
		if d.Secret == "" {
			return nil, m, fmt.Errorf("device enrolment incomplete (%d/%d); results: %s", f.DeviceCount-len(missing)+len(m.DevicesCreated), f.DeviceCount, codeSummary(failures))
		}
	}
	return out, m, nil
}

// heartbeats renew agent leases and sample clock offsets every 5 seconds.
func (c *controller) heartbeats(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		for _, h := range c.agents {
			c.sampleClock(ctx, h)
		}
		c.touch()
	}
}

// touch refreshes state.json so a resume can tell a live controller from a
// crashed one.
func (c *controller) touch() {
	c.stateMu.Lock()
	c.state.UpdatedAt = time.Now().UnixMilli()
	st := c.state
	c.stateMu.Unlock()
	_ = writeJSONAtomic(filepath.Join(c.dir, "state.json"), st)
}

// ErrRunStillActive refuses to resume a run whose controller is alive.
var ErrRunStillActive = errors.New("run is still active (state updated in the last 30 seconds)")

func (c *controller) loadResume(runID string, now time.Time) (*RunState, error) {
	st, err := ReadState(c.opt.ResultsDir, runID)
	if err != nil {
		return nil, fmt.Errorf("run %s not found under %s", runID, c.opt.ResultsDir)
	}
	if st.Result != "" || st.Status == StatusFinished {
		return nil, fmt.Errorf("run %s already finished (%s); start a new run instead", runID, firstNonEmpty(st.Result, st.Status))
	}
	if now.UnixMilli()-st.UpdatedAt < 30_000 {
		return nil, ErrRunStillActive
	}
	dir := filepath.Join(c.opt.ResultsDir, runID)
	var env struct {
		PlanHash string `json:"planHash"`
	}
	if b, err := os.ReadFile(filepath.Join(dir, "environment.json")); err == nil {
		_ = json.Unmarshal(b, &env)
	}
	if _, hash, err := c.plan.Sanitized(); err != nil || env.PlanHash == "" || hash != env.PlanHash {
		return nil, fmt.Errorf("plan differs from the one run %s started with; resume needs the same plan", runID)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "phases"))
	for _, e := range entries {
		var rec PhaseRecord
		if b, err := os.ReadFile(filepath.Join(dir, "phases", e.Name())); err == nil && json.Unmarshal(b, &rec) == nil && !rec.Cancelled {
			c.replay = append(c.replay, rec)
		}
	}
	sort.Slice(c.replay, func(i, j int) bool { return c.replay[i].Index < c.replay[j].Index })
	c.runID, c.resumed, c.gen = runID, true, max(st.Generation, 1)+1
	// Wall-time budget counts the time the run was active before it stopped.
	c.started = now.Add(-time.Duration(max(st.UpdatedAt-st.StartedAt, 0)) * time.Millisecond)
	return &st, nil
}

// replayStep returns the recorded result when the resumed search asks for
// the same step it ran before; the first difference ends the replay.
func (c *controller) replayStep(rate float64, kind string) (PhaseRecord, bool) {
	if len(c.replay) == 0 {
		return PhaseRecord{}, false
	}
	rec := c.replay[0]
	if rec.TargetMessagesPerSec != rate || rec.Kind != kind {
		c.event("resume", "diverged", rec.PhaseID, fmt.Sprintf("search asked for %s %g; later recorded steps are ignored", kind, rate))
		for _, r := range c.replay {
			c.phaseN = max(c.phaseN, r.Index)
		}
		c.replay = nil
		return PhaseRecord{}, false
	}
	c.replay = c.replay[1:]
	c.phaseN = rec.Index
	c.stateMu.Lock()
	c.state.Completed = append(c.state.Completed, PhaseBrief{PhaseID: rec.PhaseID, Kind: rec.Kind, Rate: rec.TargetMessagesPerSec, Verdict: rec.Verdict, StopReason: rec.StopReason})
	c.stateMu.Unlock()
	c.event("phase", rec.Verdict, rec.PhaseID, "replayed from the interrupted run")
	return rec, true
}

func (c *controller) sampleClock(ctx context.Context, h *agentHandle) {
	hctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	t0 := time.Now()
	st, err := h.agent.Heartbeat(hctx, RunRef{RunID: c.runID, Generation: c.gen})
	t1 := time.Now()
	h.mu.Lock()
	defer h.mu.Unlock()
	if err != nil {
		if errors.Is(err, ErrStaleGeneration) && !h.lost {
			h.lost, h.lostWhy = true, "lease lost: "+err.Error()
			c.event("agent_lost", "", "", h.target.Name+" "+err.Error())
		}
		return
	}
	mid := t0.Add(t1.Sub(t0) / 2)
	h.clocks = append(h.clocks, clockSample{at: t1, offset: time.UnixMicro(st.AgentTime).Sub(mid), unc: t1.Sub(t0)/2 + 500*time.Microsecond})
	if len(h.clocks) > 12 {
		h.clocks = h.clocks[len(h.clocks)-12:]
	}
}

func (c *controller) release() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, h := range c.agents {
		_ = h.agent.Release(ctx, RunRef{RunID: c.runID, Generation: c.gen})
	}
	if c.store != nil {
		c.store.Close()
	}
}

// Affordable checks the wall-time and evidence budgets before a step.
func (c *controller) Affordable(hold time.Duration) bool {
	drain := c.lastDrain
	if drain == 0 {
		drain = min(c.plan.Search.DrainTimeout.D(), 30*time.Second)
	}
	need := time.Since(c.started) + c.plan.Search.Warmup.D() + hold + drain + c.plan.Search.Cooldown.D() + 30*time.Second
	if need > c.plan.Budget.MaximumWallTime.D() {
		c.event("budget", "", "", fmt.Sprintf("wall time budget: next step needs %s", need.Round(time.Second)))
		return false
	}
	if size := dirSize(c.dir); float64(size) > c.plan.Budget.MaximumEvidenceGiB*(1<<30)*0.9 {
		c.event("budget", "", "", fmt.Sprintf("evidence budget: %d bytes used", size))
		return false
	}
	return true
}

func dirSize(dir string) int64 {
	var n int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, e := d.Info(); e == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n
}

// RunStep offers one load step and settles it: all agents start together,
// results and ledgers are collected, the pipeline is drained and every
// message is reconciled before the step is judged.
func (c *controller) RunStep(ctx context.Context, rate float64, kind string, hold time.Duration) (PhaseRecord, error) {
	if rec, ok := c.replayStep(rate, kind); ok {
		return rec, nil
	}
	c.phaseN++
	p := c.plan
	unrecovered := c.unrecovered
	rec := PhaseRecord{PhaseID: fmt.Sprintf("p%02d-%s-%s", c.phaseN, kind, strings.ReplaceAll(fmt.Sprintf("%g", rate), ".", "_")), Index: c.phaseN, Kind: kind, TargetMessagesPerSec: rate, TargetQueriesPerSec: p.QueryRate(rate), Streams: map[string]*StreamStats{}}
	rates := map[string]float64{"query": rec.TargetQueriesPerSec}
	for _, s := range messageStreams {
		rates[s] = rate * p.Load.IngressShare[s]
	}
	// Business modules run at their own fixed rates alongside the load.
	for s, r := range p.ModuleStreams() {
		rates[s] = r
	}
	warmup := p.Search.Warmup.D()
	start := time.Now().Add(3 * time.Second)
	rec.StartedAt, rec.MeasureFrom, rec.MeasureTo = start.UnixMilli(), start.Add(warmup).UnixMilli(), start.Add(warmup+hold).UnixMilli()
	rec.WarmupSeconds, rec.MeasureSeconds = warmup.Seconds(), hold.Seconds()
	c.stateMu.Lock()
	c.state.PhaseID, c.state.TargetRate, c.state.MeasureFrom, c.state.MeasureTo = rec.PhaseID, rate, rec.MeasureFrom, rec.MeasureTo
	c.stateMu.Unlock()
	c.setStatus(StatusWarmup, fmt.Sprintf("%s rate=%g msg/s query=%g/s", rec.PhaseID, rate, rec.TargetQueriesPerSec))

	ref := RunRef{RunID: c.runID, Generation: c.gen}
	var active []*agentHandle
	for _, h := range c.agents {
		h.mu.Lock()
		lost := h.lost
		h.mu.Unlock()
		if !lost {
			active = append(active, h)
		}
	}
	if len(active) < len(c.agents) {
		// Redistributing a lost agent's share would silently change the
		// device population; the step is marked incomplete instead.
		rec.Agents = append(rec.Agents, AgentPhaseSummary{Name: "(lost)", Interrupted: true, Reason: "an agent lost its lease earlier in the run"})
	}
	for _, h := range active {
		share := map[string]float64{}
		for stream, r := range rates {
			if n := c.agentsWith(stream); n > 0 && c.hasStream(h, stream) {
				share[stream] = r / float64(n)
			}
		}
		off, _ := h.clock()
		a := PhaseAssignment{RunID: c.runID, Generation: c.gen, PhaseID: rec.PhaseID, PhaseIndex: c.phaseN, StartAt: start.Add(off).UnixMilli(), Warmup: Duration(warmup), Measure: Duration(hold), Rates: share}
		if err := h.agent.StartPhase(context.Background(), a); err != nil {
			c.event("agent_error", "", rec.PhaseID, h.target.Name+": "+err.Error())
			rec.Agents = append(rec.Agents, AgentPhaseSummary{Name: h.target.Name, Interrupted: true, Reason: "start: " + err.Error()})
		}
	}
	// One backup per step runs under the measured load.
	var backup chan Operation
	if p.Modules.Backup.Enabled {
		backup = make(chan Operation, 1)
		go func() {
			time.Sleep(time.Until(time.UnixMilli(rec.MeasureFrom)))
			backup <- c.backupOperation(p.Modules.Backup)
		}()
	}
	// Injected failures run at their offsets within the measure window and
	// are always recovered, also when the step is stopped early.
	var faultWG sync.WaitGroup
	faultEvents := make([]FaultEvent, len(p.Faults.Actions))
	if p.Faults.Enabled {
		for i, fa := range p.Faults.Actions {
			h := c.agentByName(fa.Agent)
			if h == nil {
				faultEvents[i] = FaultEvent{Agent: fa.Agent, Action: fa.Action, Output: "agent not in this step"}
				continue
			}
			faultWG.Add(1)
			go func(i int, fa FaultAction, h *agentHandle) {
				defer faultWG.Done()
				faultEvents[i] = c.runFault(ctx, h, fa, rec.PhaseID, time.UnixMilli(rec.MeasureFrom).Add(fa.At.D()))
			}(i, fa, h)
		}
	}
	// Wait for the load window, reacting to a stop request.
	end := start.Add(warmup + hold)
	measuring := false
	for time.Now().Before(end) {
		if !measuring && time.Now().UnixMilli() >= rec.MeasureFrom {
			measuring = true
			c.setStatus(StatusRunning, rec.PhaseID)
		}
		if ctx.Err() != nil {
			rec.Cancelled = true
			c.setStatus(StatusCancelling, "stopping new load")
			for _, h := range active {
				_ = h.agent.Stop(context.Background(), RunRef{RunID: c.runID, Generation: c.gen, Hard: c.forced})
			}
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if rec.Cancelled {
		rec.MeasureTo = min(rec.MeasureTo, time.Now().UnixMilli())
		rec.MeasureSeconds = math.Max(0, float64(rec.MeasureTo-rec.MeasureFrom)/1000)
	}
	faultWG.Wait()
	if p.Faults.Enabled {
		rec.Faults = faultEvents
	}
	// Collect agent results and ledgers.
	settle := p.Load.RequestTimeout.D() + p.Load.ReceiptWait.D()*time.Duration(p.Load.ReceiptRetries+1) + 30*time.Second
	for _, h := range active {
		sum := c.collectAgent(h, ref, rec.PhaseID, settle)
		rec.Agents = append(rec.Agents, sum)
		if res, ok := c.agentResults[rec.PhaseID+"/"+h.target.Name]; ok {
			for k, s := range res.Streams {
				if rec.Streams[k] == nil {
					rec.Streams[k] = &StreamStats{}
				}
				rec.Streams[k].Merge(s)
			}
		}
	}
	rec.EndedAt = time.Now().UnixMilli()
	// Drain and reconcile.
	c.setStatus(StatusDraining, rec.PhaseID)
	drainStart := time.Now()
	drainLimit := p.Search.DrainTimeout.D()
	if c.forced {
		drainLimit = 0
	} else if rec.Cancelled {
		drainLimit = min(drainLimit, time.Minute)
	}
	rec.Drain.Completed = c.drain(rec.PhaseID, drainLimit)
	rec.Drain.Seconds = time.Since(drainStart).Seconds()
	c.lastDrain = time.Since(drainStart)
	c.setStatus(StatusVerifying, rec.PhaseID)
	var worstUnc time.Duration
	for _, h := range active {
		if _, unc := h.clock(); unc > worstUnc {
			worstUnc = unc
		}
	}
	if c.verifier != nil {
		vctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		if err := c.verifier.Pass(vctx, true); err != nil {
			rec.Integrity.Note = "final reconciliation failed: " + err.Error()
			rec.Integrity.VerificationMode = "incomplete"
		} else {
			rec.Integrity = c.verifier.PhaseIntegrity(rec.PhaseID, worstUnc)
			if rule := p.Fixtures.AlarmRuleID; rule != "" {
				// The alarm window starts with the step (minus clock slack).
				checked, mismatches, samples, err := c.verifier.CheckAlarms(vctx, rec.PhaseID, rule, p.Fixtures.AlarmRecovers, rec.StartedAt-60_000)
				if err != nil {
					rec.Integrity.Note = firstNonEmpty(rec.Integrity.Note, "alarm reconciliation failed: "+err.Error())
				}
				rec.Integrity.AlarmDevicesChecked, rec.Integrity.AlarmMismatches, rec.Integrity.AlarmSamples = checked, mismatches, samples
			}
		}
		cancel()
	}
	if backup != nil {
		rec.Operations = append(rec.Operations, <-backup)
	}
	_ = writeJSONAtomic(filepath.Join(c.dir, "verification", rec.PhaseID+".json"), rec.Integrity)
	rec.Pipeline = PipelineFor(c.collector.Rounds(rec.MeasureFrom, rec.MeasureTo), rec.MeasureSeconds)
	if p.Faults.Enabled {
		// Recovery is observed through the drain, when backlog must return.
		rec.Recovery = computeRecovery(c.collector.Rounds(rec.MeasureFrom, time.Now().UnixMilli()), rec.Faults, rec.MeasureFrom, rec.MeasureTo)
	}
	if rec.MeasureSeconds > 0 && rec.Integrity.BusinessLatency.N > 0 {
		v := math.Round(float64(rec.Integrity.BusinessLatency.N)/rec.MeasureSeconds*10) / 10
		rec.BusinessCompletedPerSec = &v
	}
	if rec.Integrity.VerificationMode == "incomplete" {
		rec.Checks = append(rec.Checks, Check{Name: "核对", Status: VerdictInconclusive, Reason: ReasonObservability, Detail: rec.Integrity.Note})
	}
	Judge(p, &rec)
	if rec.Integrity.VerificationMode == "incomplete" && rec.Verdict == VerdictPassed {
		rec.Verdict, rec.StopReason = VerdictInconclusive, ReasonObservability
	}
	if unrecovered && !rec.Cancelled {
		rec.Checks = append(rec.Checks, Check{Name: "起始状态", Status: VerdictInconclusive, Reason: ReasonNotRecovered, Detail: "上一档积压未排空即开始本档，结果不能归因于本档负载"})
		rec.Verdict, rec.StopReason = VerdictInconclusive, ReasonNotRecovered
	}
	if err := writeJSONAtomic(filepath.Join(c.dir, "phases", rec.PhaseID+".json"), rec); err != nil {
		return rec, err
	}
	c.stateMu.Lock()
	c.state.Completed = append(c.state.Completed, PhaseBrief{PhaseID: rec.PhaseID, Kind: kind, Rate: rate, Verdict: rec.Verdict, StopReason: rec.StopReason})
	c.state.PhaseID = ""
	c.stateMu.Unlock()
	c.event("phase", rec.Verdict, rec.PhaseID, rec.StopReason)
	c.logf("%s verdict=%s %s", rec.PhaseID, rec.Verdict, rec.StopReason)
	if !rec.Cancelled && ctx.Err() == nil {
		c.unrecovered = !c.cooldown()
	}
	return rec, nil
}

func (c *controller) agentByName(name string) *agentHandle {
	for _, h := range c.agents {
		if h.target.Name == name {
			return h
		}
	}
	return nil
}

// runFault injects at the planned time and recovers after the duration (or
// at once when the step is stopped). Times are converted to controller clock.
func (c *controller) runFault(ctx context.Context, h *agentHandle, fa FaultAction, phaseID string, at time.Time) FaultEvent {
	ev := FaultEvent{Agent: fa.Agent, Action: fa.Action}
	select {
	case <-ctx.Done():
		ev.Output = "step stopped before injection"
		return ev
	case <-time.After(time.Until(at)):
	}
	req := FaultRequest{RunID: c.runID, Generation: c.gen, Action: fa.Action, Op: FaultInject}
	off, _ := h.clock()
	offMS := off.Milliseconds()
	c.event("fault", "inject", phaseID, fa.Agent+"/"+fa.Action)
	res, err := h.agent.Fault(context.Background(), req)
	ev.InjectedAt = time.Now().UnixMilli()
	if err == nil {
		ev.InjectOK, ev.Output = res.OK, res.Output
		ev.InjectedAt = res.StartedAt - offMS
	} else {
		ev.Output = err.Error()
	}
	select {
	case <-ctx.Done():
	case <-time.After(fa.Duration.D()):
	}
	req.Op = FaultRecover
	// Recovery is retried: a failure left in place would poison later steps.
	for attempt := 0; attempt < 3; attempt++ {
		res, err = h.agent.Fault(context.Background(), req)
		if err == nil && res.OK {
			ev.RecoverOK, ev.RecoverAt = true, res.FinishedAt-offMS
			break
		}
		if err != nil {
			ev.Output = clip(ev.Output+"; recover: "+err.Error(), 400)
		} else {
			ev.Output = clip(ev.Output+"; recover: "+res.Output, 400)
		}
		time.Sleep(2 * time.Second)
	}
	c.event("fault", map[bool]string{true: "recovered", false: "recover_failed"}[ev.RecoverOK], phaseID, fa.Agent+"/"+fa.Action)
	return ev
}

func (c *controller) agentsWith(stream string) int {
	n := 0
	for _, h := range c.agents {
		if c.hasStream(h, stream) {
			n++
		}
	}
	return n
}

func (c *controller) hasStream(h *agentHandle, stream string) bool {
	switch stream {
	case "http":
		return len(h.http) > 0
	case "mqtt":
		return len(h.mqtt) > 0
	case "tcp":
		return h.tcp > 0
	}
	return true
}

func (c *controller) collectAgent(h *agentHandle, ref RunRef, phaseID string, settle time.Duration) AgentPhaseSummary {
	off, unc := h.clock()
	sum := AgentPhaseSummary{Name: h.target.Name, ClockOffsetMS: ms(off), ClockUncertaintyMS: ms(unc)}
	deadline := time.Now().Add(settle)
	var res AgentPhaseResult
	var err error
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		res, err = h.agent.PhaseResult(ctx, ref, phaseID)
		cancel()
		if err == nil || !errors.Is(err, ErrPhaseRunning) || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		sum.Interrupted, sum.Reason = true, "result: "+err.Error()
		return sum
	}
	if c.agentResults == nil {
		c.agentResults = map[string]AgentPhaseResult{}
	}
	c.agentResults[phaseID+"/"+h.target.Name] = res
	sum.Interrupted, sum.LeaseExpired, sum.Reason, sum.HeapBytes, sum.Goroutines, sum.Ledger = res.Interrupted, res.LeaseExpired, res.Reason, res.HeapBytes, res.Goroutines, res.Ledger
	path := filepath.Join(c.dir, "ledgers", phaseID, h.target.Name+".jsonl.gz")
	if err = os.MkdirAll(filepath.Dir(path), 0o750); err == nil {
		var f *os.File
		if f, err = os.Create(path); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			err = h.agent.FetchLedger(ctx, ref, phaseID, f)
			cancel()
			err = errors.Join(err, f.Close())
		}
	}
	if err == nil {
		var digest string
		if digest, _, err = fileDigest(path); err == nil && digest != res.Ledger.SHA256 {
			err = errors.New("ledger digest mismatch")
		}
	}
	if err == nil && c.verifier != nil {
		err = c.verifier.Track(path, phaseID, off)
	}
	if err != nil {
		sum.Interrupted, sum.Reason = true, "ledger: "+err.Error()
	}
	return sum
}

// drain waits until every message of the phase reached a final state and the
// pipeline backlog is empty, or until limit.
func (c *controller) drain(phaseID string, limit time.Duration) bool {
	deadline := time.Now().Add(limit)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		err := c.verifier.Pass(ctx, false)
		cancel()
		open := c.verifier.Open(phaseID)
		round := c.collector.Scrape(context.Background())
		backlog := BacklogSeries([]Round{round})
		empty := len(backlog) == 1 && backlog[0].Valid && backlog[0].Value == 0
		if err == nil && open == 0 && empty {
			return true
		}
		if !time.Now().Before(deadline) {
			c.event("drain_timeout", "", phaseID, fmt.Sprintf("open=%d backlogEmpty=%v %s", open, empty, errText(err)))
			return err == nil && open == 0
		}
		time.Sleep(2 * time.Second)
	}
}

// cooldown waits until the platform is ready and the step's backlog has
// drained, so an overloaded step does not carry its backlog into the next one.
// It reports false when the platform did not recover in time.
func (c *controller) cooldown() bool {
	limit := max(2*time.Minute, c.plan.Search.DrainTimeout.D())
	deadline := time.Now().Add(limit)
	time.Sleep(c.plan.Search.Cooldown.D())
	for time.Now().Before(deadline) && !c.stopped() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		status, _, err := c.get(ctx, "/health/ready", "")
		cancel()
		// Missing backlog metrics are an observability gap judged per step,
		// not a reason to wait here.
		backlog := BacklogSeries([]Round{c.collector.Scrape(context.Background())})
		empty := len(backlog) != 1 || !backlog[0].Valid || backlog[0].Value == 0
		if err == nil && status == 200 && empty {
			return true
		}
		time.Sleep(2 * time.Second)
	}
	if c.stopped() {
		return true
	}
	c.event("cooldown", "", "", fmt.Sprintf("platform not ready or backlog not drained within %s after step", limit))
	return false
}

// PipelineFor summarises platform metrics over a measurement window.
func PipelineFor(rounds []Round, seconds float64) Pipeline {
	pl := Pipeline{Rounds: len(rounds)}
	for _, r := range rounds {
		for _, s := range r.Instances {
			if !s.OK {
				pl.FailedScrapes++
			}
		}
	}
	rate := func(name string) *float64 {
		v, ok := CounterIncrease(rounds, name)
		if !ok || seconds <= 0 || len(rounds) < 2 {
			return nil
		}
		span := float64(rounds[len(rounds)-1].At-rounds[0].At) / 1000
		if span <= 0 {
			return nil
		}
		x := math.Round(v/span*10) / 10
		return &x
	}
	pl.ArchivedPerSec, pl.ParsedPerSec, pl.AlarmsPerSec = rate("raw_archive_success_total"), rate("parse_success_total"), rate("alarm_trigger_total")
	pl.MetricsValid = pl.ArchivedPerSec != nil && pl.ParsedPerSec != nil
	backlog := BacklogSeries(rounds)
	for _, pt := range backlog {
		if pt.Valid {
			v := pt.Value
			if pl.BacklogStart == nil {
				pl.BacklogStart = &v
			}
			pl.BacklogEnd = &v
		}
	}
	if slope, n, ok := Trend(backlog); ok {
		s := math.Round(slope*100) / 100
		pl.BacklogSlopePerSec, pl.BacklogPoints = &s, n
	} else {
		pl.BacklogPoints = n
	}
	return pl
}

// Open counts phase messages that are not in a final state yet.
func (v *Verifier) Open(phase string) int {
	v.mu.Lock()
	defer v.mu.Unlock()
	n := 0
	for _, m := range v.msgs {
		if m.phase == phase && m.state == StatePending {
			n++
		}
	}
	return n
}

func writeJSONAtomic(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err = os.WriteFile(tmp, append(b, '\n'), 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ReadState reads a run's state for `capacity-test status`.
func ReadState(resultsDir, runID string) (RunState, error) {
	var s RunState
	b, err := os.ReadFile(filepath.Join(resultsDir, runID, "state.json"))
	if err != nil {
		return s, err
	}
	return s, json.Unmarshal(b, &s)
}

// RequestStop asks a running controller to stop; force skips the drain.
func RequestStop(resultsDir, runID string, force bool) error {
	dir := filepath.Join(resultsDir, runID)
	if _, err := os.Stat(filepath.Join(dir, "state.json")); err != nil {
		return fmt.Errorf("run %s not found under %s", runID, resultsDir)
	}
	content := []byte("soft\n")
	if force {
		content = []byte("force\n")
	}
	return os.WriteFile(filepath.Join(dir, StopFile), content, 0o640)
}
