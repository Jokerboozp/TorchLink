package capacity

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type Coverage struct {
	Module string `json:"module"`
	Status string `json:"status"` // measured, not_covered
	Detail string `json:"detail"`
}

type SummaryIntegrity struct {
	VerificationMode         string  `json:"verificationMode"`
	UniqueSent               *uint64 `json:"uniqueSent"`
	UniqueArchiveConfirmed   *uint64 `json:"uniqueArchiveConfirmed"`
	UniqueBusinessCompleted  *uint64 `json:"uniqueBusinessCompleted"`
	Missing                  *uint64 `json:"missing"`
	Unknown                  *uint64 `json:"unknown"`
	Pending                  *uint64 `json:"pending"`
	PhysicalDuplicateRows    *uint64 `json:"physicalDuplicateRows"`
	DuplicateBusinessEffects *uint64 `json:"duplicateBusinessEffects"`
	TCPAckOnly               uint64  `json:"tcpAckOnly"`
}

type Bottleneck struct {
	PhaseID   string `json:"phaseId"`
	Candidate string `json:"candidate"`
	Evidence  string `json:"evidence"`
	NextStep  string `json:"nextStep"`
}

// Summary is the machine-readable report in summary.json.
type Summary struct {
	SchemaVersion    int                     `json:"schemaVersion"`
	RunID            string                  `json:"runId"`
	ExecutionStatus  string                  `json:"executionStatus"`
	Verdict          string                  `json:"verdict"`
	VerdictReason    string                  `json:"verdictReason,omitempty"`
	EvidenceComplete bool                    `json:"evidenceComplete"`
	EvidenceGaps     []string                `json:"evidenceGaps"`
	PlanHash         string                  `json:"planHash"`
	SourceCommit     string                  `json:"sourceCommit"`
	Preset           string                  `json:"preset"`
	Suite            string                  `json:"suite"`
	Capacity         map[string]SearchResult `json:"capacity"`
	Coverage         []Coverage              `json:"coverage"`
	Integrity        SummaryIntegrity        `json:"integrity"`
	Phases           []PhaseBrief            `json:"phases"`
	Bottlenecks      []Bottleneck            `json:"bottlenecks"`
	StopReason       *string                 `json:"stopReason"`
	EvidenceRefs     []string                `json:"evidenceRefs"`
}

type reportData struct {
	dir       string
	plan      *Plan
	planYAML  string
	env       map[string]any
	state     RunState
	search    SearchResult
	hasSearch bool
	preflight []PreflightCheck
	manifest  *Manifest
	phases    []PhaseRecord
	rounds    []Round
	nodes     []Round
	summary   Summary
	specs     map[string]chartSpec
}

// chart renders an SVG and keeps the spec for PNG output.
func (d *reportData) chart(name string, s chartSpec) string {
	if d.specs == nil {
		d.specs = map[string]chartSpec{}
	}
	d.specs[name] = s
	return renderChart(s)
}

// GenerateReport rebuilds every report file from the evidence in dir. It
// reads nothing from the target system, so `capacity-test report` can rerun
// it offline and get identical output. secrets are checked for leaks.
func GenerateReport(dir string, secrets []string) error {
	d := &reportData{dir: dir}
	if err := d.load(); err != nil {
		return err
	}
	d.summarize()
	formats := map[string]bool{"json": true}
	for _, f := range d.plan.Outputs.Formats {
		formats[f] = true
	}
	files := map[string][]byte{}
	sb, _ := json.MarshalIndent(d.summary, "", "  ")
	files["summary.json"] = append(sb, '\n')
	charts := d.charts()
	if formats["svg"] {
		for name, svg := range charts {
			files[filepath.Join("charts", name+".svg")] = []byte(svg)
		}
	}
	if formats["png"] {
		for name, spec := range d.specs {
			files[filepath.Join("charts", name+".png")] = renderPNG(spec)
		}
	}
	if formats["csv"] {
		files["phases.csv"] = d.phasesCSV()
	}
	if formats["markdown"] {
		files["report.md"] = []byte(d.markdown())
	}
	if formats["html"] {
		files["report.html"] = []byte(d.html(charts))
	}
	for name, b := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			return err
		}
		if err := os.WriteFile(p, b, 0o640); err != nil {
			return err
		}
	}
	if err := checkNoSecrets(dir, secrets); err != nil {
		return err
	}
	return writeChecksums(dir)
}

func (d *reportData) load() error {
	b, err := os.ReadFile(filepath.Join(d.dir, "plan.sanitized.yaml"))
	if err != nil {
		return fmt.Errorf("not a capacity run directory: %w", err)
	}
	d.planYAML = string(b)
	if d.plan, err = ParsePlan(b); err != nil {
		return err
	}
	readJSON := func(name string, v any) bool {
		b, err := os.ReadFile(filepath.Join(d.dir, name))
		return err == nil && json.Unmarshal(b, v) == nil
	}
	readJSON("environment.json", &d.env)
	if !readJSON("state.json", &d.state) {
		d.state.Status = "UNKNOWN"
	}
	d.hasSearch = readJSON("search.json", &d.search)
	readJSON("preflight.json", &d.preflight)
	var m Manifest
	if readJSON("manifest.json", &m) {
		d.manifest = &m
	}
	entries, _ := os.ReadDir(filepath.Join(d.dir, "phases"))
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		var r PhaseRecord
		if readJSON(filepath.Join("phases", e.Name()), &r) {
			d.phases = append(d.phases, r)
		}
	}
	sort.Slice(d.phases, func(i, j int) bool { return d.phases[i].Index < d.phases[j].Index })
	d.rounds, _ = LoadRounds(filepath.Join(d.dir, "observations", "metrics.jsonl"))
	d.nodes, _ = LoadRounds(filepath.Join(d.dir, "observations", "nodes.jsonl"))
	return nil
}

func u64(v uint64) *uint64 { return &v }

func (d *reportData) summarize() {
	s := &d.summary
	s.SchemaVersion, s.RunID, s.ExecutionStatus = SchemaVersion, d.state.RunID, firstNonEmpty(d.state.Result, d.state.Status)
	if s.RunID == "" {
		s.RunID = filepath.Base(d.dir)
	}
	s.PlanHash, _ = d.env["planHash"].(string)
	s.SourceCommit, _ = d.env["sourceCommit"].(string)
	s.Preset, s.Suite = d.plan.Preset, d.plan.Suite
	res := d.search
	if !d.hasSearch {
		res = SearchResult{Preset: d.plan.Preset, Classification: ClassUnmeasured}
	}
	s.Capacity = map[string]SearchResult{"mixedBusinessMessagesPerSecond": res}
	s.Coverage = d.coverage()
	s.EvidenceGaps = []string{}
	if !d.hasSearch {
		s.EvidenceGaps = append(s.EvidenceGaps, "search.json 缺失：控制器未正常结束")
	}
	for _, c := range d.preflight {
		if !c.OK {
			s.EvidenceGaps = append(s.EvidenceGaps, "前置检查未通过："+c.Name+" "+c.Detail)
		}
	}
	integrityFail := false
	var in SummaryIntegrity
	mode := "not_run"
	var sent, arch, done, miss, unk, pend, dup uint64
	for _, p := range d.phases {
		s.Phases = append(s.Phases, PhaseBrief{PhaseID: p.PhaseID, Kind: p.Kind, Rate: p.TargetMessagesPerSec, Verdict: p.Verdict, StopReason: p.StopReason})
		i := p.Integrity
		if i.VerificationMode == "full_id" || i.VerificationMode == "ack_only" {
			if mode == "not_run" {
				mode = i.VerificationMode
			}
		} else if i.VerificationMode != "" {
			mode = "incomplete"
			s.EvidenceGaps = append(s.EvidenceGaps, p.PhaseID+" 核对未完成："+i.Note)
		}
		sent += i.UniqueSent
		arch += i.UniqueArchiveConfirmed
		done += i.UniqueBusinessDone
		miss += i.Missing
		unk += i.Unknown
		pend += i.Pending
		dup += i.PhysicalDuplicateRows
		in.TCPAckOnly += i.TCPAckOnly
		if p.StopReason == ReasonIntegrity {
			integrityFail = true
		}
		for _, a := range p.Agents {
			if a.Interrupted || a.LeaseExpired {
				s.EvidenceGaps = append(s.EvidenceGaps, fmt.Sprintf("%s Agent %s 未完整执行：%s", p.PhaseID, a.Name, a.Reason))
			}
		}
		if !p.Pipeline.MetricsValid {
			s.EvidenceGaps = append(s.EvidenceGaps, p.PhaseID+" 平台管道计数器缺测或重置，归档/解析速率未知")
		}
	}
	in.VerificationMode = mode
	if mode != "not_run" {
		in.UniqueSent, in.UniqueArchiveConfirmed, in.UniqueBusinessCompleted, in.Missing, in.Unknown, in.Pending, in.PhysicalDuplicateRows = u64(sent), u64(arch), u64(done), u64(miss), u64(unk), u64(pend), u64(dup)
	}
	s.Integrity = in
	s.EvidenceComplete = len(s.EvidenceGaps) == 0 && len(d.phases) > 0
	s.Bottlenecks = d.bottlenecks()
	if r := firstNonEmpty(res.StopReason, d.state.StopReason); r != "" {
		s.StopReason = &r
	}
	switch {
	case integrityFail:
		s.Verdict, s.VerdictReason = VerdictFailed, ReasonIntegrity
	case res.Classification == ClassNoPass:
		s.Verdict, s.VerdictReason = VerdictFailed, res.FailureMode
	case res.Classification == ClassRegression || res.Classification == ClassSoak || res.Classification == ClassResilience:
		s.Verdict = VerdictPassed
		for _, p := range d.phases {
			if p.Verdict == VerdictFailed {
				s.Verdict, s.VerdictReason = VerdictFailed, p.StopReason
				break
			}
			if p.Verdict != VerdictPassed {
				s.Verdict, s.VerdictReason = VerdictInconclusive, p.StopReason
			}
		}
		if len(d.phases) == 0 {
			s.Verdict, s.VerdictReason = VerdictInconclusive, firstNonEmpty(res.StopReason, "no_steps")
		}
	case res.LowerPassedBound != nil && res.RecommendedOperatingValue != nil:
		s.Verdict = VerdictPassed
	default:
		s.Verdict, s.VerdictReason = VerdictInconclusive, firstNonEmpty(res.StopReason, "candidate_not_confirmed")
		if res.Unstable {
			s.VerdictReason = "boundary_unstable"
		}
	}
	if s.Verdict == VerdictPassed && !s.EvidenceComplete {
		s.Verdict, s.VerdictReason = VerdictInconclusive, "evidence_incomplete"
	}
	if s.Verdict == VerdictPassed && d.plan.Suite == "full" {
		s.Verdict, s.VerdictReason = VerdictInconclusive, "coverage_incomplete"
	}
	s.EvidenceRefs = []string{"plan.sanitized.yaml", "environment.json", "preflight.json", "manifest.json", "search.json", "phases/", "verification/", "ledgers/", "observations/metrics.jsonl", "events.jsonl"}
}

func (d *reportData) coverage() []Coverage {
	share := d.plan.Load.IngressShare
	measured := func(on bool, detail string) (string, string) {
		if on {
			return "measured", detail
		}
		return VerdictNotCovered, "计划未分配负载"
	}
	var out []Coverage
	add := func(module string, on bool, detail string) {
		st, dt := measured(on, detail)
		out = append(out, Coverage{Module: module, Status: st, Detail: dt})
	}
	add("http_standard_ingest", share["http"] > 0, "真实设备凭据 HTTP 标准上报，原文 ID 全量核对")
	add("mqtt_standard_ingest", share["mqtt"] > 0, "MQTT QoS1 + 应用归档回执，原文 ID 全量核对")
	add("tcp_gb26875", share["tcp"] > 0, "GB26875 TCP，仅协议 ACK 层证据，未做原文核对")
	add("management_queries", d.plan.Load.QueryRequestsPerSecond > 0, "固定查询组合的开环请求，非真实用户会话")
	m := d.plan.Modules
	off := "计划未启用"
	if d.plan.Suite == "full" {
		off = "suite=full 但计划未启用，全系统结论不成立"
	}
	module := func(name string, on bool, detail string) {
		if on {
			out = append(out, Coverage{Module: name, Status: "measured", Detail: detail})
		} else {
			out = append(out, Coverage{Module: name, Status: VerdictNotCovered, Detail: off})
		}
	}
	if d.plan.Fixtures.AlarmRuleID != "" {
		out = append(out, Coverage{Module: "rules_and_alarms", Status: "measured", Detail: "按设备上报序列核对规则 " + d.plan.Fixtures.AlarmRuleID + " 的触发与最终状态"})
	} else {
		out = append(out, Coverage{Module: "rules_and_alarms", Status: VerdictNotCovered, Detail: "未指定 fixtures.alarmRuleId，只观测 alarm_trigger_total"})
	}
	aiDetail := "手动告警研判（真实 Harness 与模型，受 maxRuns 预算限制）"
	if m.AI.Mode == "mock" {
		aiDetail = "手动告警研判，平台连接 harness-mock：只测平台调度，不代表模型与供应商容量"
	}
	module("ai_workflows", m.AI.Enabled, aiDetail)
	module("knowledge_base", m.Knowledge.Enabled, "文档上传、解析、分块与索引；异步上传等待 INDEXED 后计为成功，包含索引等候时间；检索只经 AI 工作流间接覆盖")
	videoDetail := "播放会话建立与释放（控制面）"
	if d.env != nil && m.Video.Enabled {
		videoDetail += "；配置 inventory.web 时拉取 HLS 播放列表与首个分片，WebRTC 未覆盖"
	}
	module("video_live", m.Video.Enabled, videoDetail)
	backupDetail := "负载下备份并逐文件校验"
	if m.Backup.Restore {
		backupDetail += "，恢复到独立库并核对条数"
	}
	module("backup_restore", m.Backup.Enabled, backupDetail)
	module("realtime_push", m.Realtime.Enabled, "MQTT 订阅告警与设备状态推送，测送达时延")
	module("exports_and_replay", m.Exports.Enabled, "原文批量下载、回放 DRY_RUN、巡检与 PDF")
	module("open_api", m.OpenAPI.Enabled, "开放 API 密钥查询")
	module("fault_injection", d.plan.Faults.Enabled, "Agent 白名单故障动作，记录注入/恢复与恢复时间")
	out = append(out, Coverage{Module: "browser_ui", Status: VerdictNotCovered, Detail: "负载不经浏览器；页面交互另用少量真实浏览器会话检查"})
	return out
}

// bottlenecks applies fixed rules to the lowest failing step, or
// the last step when nothing failed. They are candidates, not root causes.
func (d *reportData) bottlenecks() []Bottleneck {
	var target *PhaseRecord
	for i := range d.phases {
		p := &d.phases[i]
		if p.Verdict == VerdictFailed && (target == nil || p.TargetMessagesPerSec < target.TargetMessagesPerSec) {
			target = p
		}
	}
	if target == nil && len(d.phases) > 0 {
		target = &d.phases[len(d.phases)-1]
		if target.Verdict == VerdictPassed {
			return []Bottleneck{}
		}
	}
	if target == nil {
		return []Bottleneck{}
	}
	var out []Bottleneck
	add := func(c, e, n string) {
		out = append(out, Bottleneck{PhaseID: target.PhaseID, Candidate: c, Evidence: e, NextStep: n})
	}
	failedCheck := func(prefix string) *Check {
		for i := range target.Checks {
			if strings.HasPrefix(target.Checks[i].Name, prefix) && target.Checks[i].Status != VerdictPassed {
				return &target.Checks[i]
			}
		}
		return nil
	}
	if c := failedCheck("实发达成"); c != nil {
		add("发压能力", c.Detail, "增加 Agent、源地址或提高 maxInflight 后重测；该档不代表服务端上限")
	}
	if c := failedCheck("配速准确"); c != nil {
		add("发压能力", c.Detail, "检查负载机 CPU、文件句柄与临时端口")
	}
	if c := failedCheck("入口成功率"); c != nil {
		if c.Reason == ReasonPolicy {
			add("配额/保护策略", c.Detail, "记录策略上限，按正式约束评估，不自动移除限流")
		} else {
			add("入口接收", c.Detail, "查看入口错误码分布、网关资源与归档仓储时延")
		}
	}
	if c := failedCheck("数据完整性"); c != nil {
		add("数据完整性", c.Detail, "按 verification/ 样例定位缺失消息所在阶段（归档、发布、解析、处理）")
	}
	if c := failedCheck("查询"); c != nil {
		add("查询或缓存", c.Detail, "优化过滤、索引与聚合，按一致性要求分担读负载")
	}
	rounds := make([]Round, 0)
	for _, r := range d.rounds {
		if r.At >= target.MeasureFrom && r.At <= target.MeasureTo {
			rounds = append(rounds, r)
		}
	}
	type growth struct {
		name  string
		value float64
	}
	var gs []growth
	for _, name := range []string{"kafka_lag_parser", "kafka_lag_storage", "kafka_lag_state", "kafka_lag_device_alarm_notifications", "mqtt_inbox_pending"} {
		if slope, _, ok := Trend(GaugeSeries(rounds, name)); ok && slope > 0 {
			gs = append(gs, growth{name, slope * target.MeasureSeconds})
		}
	}
	sort.Slice(gs, func(i, j int) bool { return gs[i].value > gs[j].value })
	if len(gs) > 0 && gs[0].value > 10 {
		g := gs[0]
		ev := fmt.Sprintf("测量窗口内 %s 约增长 %.0f", g.name, g.value)
		switch {
		case g.name == "mqtt_inbox_pending":
			add("归档/接收处理", ev, "检查 inbox fsync、归档仓储、共享订阅分配和网关资源")
		case g.name == "kafka_lag_parser":
			add("解析", ev, "调整解析资源与协议 Worker，检查分区与设备热点")
		default:
			add("业务存储/事务", ev, "定位 PostgreSQL 锁、WAL、连接池等待与单条消息往返，先解除共享瓶颈再扩 Worker")
		}
	} else if c := failedCheck("业务完成时延"); c != nil && c.Status == VerdictFailed {
		add("业务处理时延", c.Detail+"；各消费组积压未见明显增长", "对照解析与存储耗时，剖析规则与状态写入")
	}
	// Host saturation from node-exporter during the failing window.
	for host, pts := range HostSeries(d.nodes) {
		var cpu, disk, n, nd float64
		for _, pt := range pts {
			if pt.At < target.MeasureFrom || pt.At > target.MeasureTo {
				continue
			}
			if pt.CPUValid {
				cpu, n = cpu+pt.CPU, n+1
			}
			if pt.DiskValid {
				disk, nd = disk+pt.Disk, nd+1
			}
		}
		if n > 0 && cpu/n >= 85 {
			add("主机 CPU 饱和", fmt.Sprintf("%s 测量窗口平均 CPU %.0f%%", host, cpu/n), "确认该主机上的进程分布，拆分角色或扩容后重测")
		}
		if nd > 0 && disk/nd >= 80 {
			add("主机磁盘繁忙", fmt.Sprintf("%s 测量窗口最忙磁盘平均繁忙 %.0f%%", host, disk/nd), "检查 fsync、WAL、ClickHouse 合并与日志写入，考虑更快磁盘或分离数据盘")
		}
	}
	if c := failedCheck("管道积压"); c != nil && len(gs) == 0 {
		add("管道积压（来源未知）", c.Detail, "补齐 kafka_lag_<group> 与 inbox 指标采集后重测")
	}
	if len(out) == 0 {
		add("证据不足", "本档未通过，但没有可归类的失败检查："+target.StopReason, "查看 phases/ 中的检查项与 events.jsonl")
	}
	return out
}

// --- tables -----------------------------------------------------------------

func fmtPtr(v *float64, unit string) string {
	if v == nil {
		return "—"
	}
	return fmtNum(*v) + unit
}

func fmtNum(v float64) string {
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return strconv.FormatFloat(v, 'f', 0, 64)
	}
	if math.Abs(v) >= 100 {
		return strconv.FormatFloat(v, 'f', 0, 64)
	}
	return strconv.FormatFloat(v, 'f', 1, 64)
}

func perSec(n uint64, seconds float64) *float64 {
	if seconds <= 0 {
		return nil
	}
	v := math.Round(float64(n)/seconds*10) / 10
	return &v
}

var phaseColumns = []string{"phaseId", "kind", "targetMsgPerSec", "targetQueryPerSec", "measureSeconds", "scheduled", "sent", "notSent", "ingressOk", "ingressFail", "ingressOkPerSec", "ackP50Ms", "ackP95Ms", "ackP99Ms", "latenessP95Ms", "querySent", "queryOk", "queryP95Ms", "queryP99Ms", "businessSamples", "businessP95Ms", "businessP99Ms", "uniqueSent", "businessCompleted", "missing", "unknown", "pending", "archivedPerSec", "parsedPerSec", "alarmsPerSec", "backlogStart", "backlogEnd", "backlogSlopePerSec", "drainSeconds", "verdict", "stopReason"}

func (d *reportData) phaseRows() [][]string {
	var rows [][]string
	p2s := func(v *float64) string {
		if v == nil {
			return ""
		}
		return fmtNum(*v)
	}
	for _, p := range d.phases {
		m := p.MessageTotals()
		ack := m.Latency.Percentiles()
		late, _ := m.Lateness.Quantile(0.95)
		q := StreamStats{}
		q.Merge(p.Streams["query"])
		qp := q.Latency.Percentiles()
		bp := p.Integrity.BusinessLatency.Percentiles()
		rows = append(rows, []string{p.PhaseID, p.Kind, fmtNum(p.TargetMessagesPerSec), fmtNum(p.TargetQueriesPerSec), fmtNum(p.MeasureSeconds),
			fmt.Sprint(m.Scheduled), fmt.Sprint(m.Sent), fmt.Sprint(m.NotSent), fmt.Sprint(m.OK), fmt.Sprint(m.Fail), p2s(perSec(m.OK, p.MeasureSeconds)),
			p2s(ack.P50MS), p2s(ack.P95MS), p2s(ack.P99MS), fmtNum(math.Round(late*10) / 10),
			fmt.Sprint(q.Sent), fmt.Sprint(q.OK), p2s(qp.P95MS), p2s(qp.P99MS),
			fmt.Sprint(bp.N), p2s(bp.P95MS), p2s(bp.P99MS),
			fmt.Sprint(p.Integrity.UniqueSent), fmt.Sprint(p.Integrity.UniqueBusinessDone), fmt.Sprint(p.Integrity.Missing), fmt.Sprint(p.Integrity.Unknown), fmt.Sprint(p.Integrity.Pending),
			p2s(p.Pipeline.ArchivedPerSec), p2s(p.Pipeline.ParsedPerSec), p2s(p.Pipeline.AlarmsPerSec),
			p2s(p.Pipeline.BacklogStart), p2s(p.Pipeline.BacklogEnd), p2s(p.Pipeline.BacklogSlopePerSec), fmtNum(math.Round(p.Drain.Seconds*10) / 10),
			p.Verdict, p.StopReason})
	}
	return rows
}

func (d *reportData) phasesCSV() []byte {
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	_ = w.Write(phaseColumns)
	for _, r := range d.phaseRows() {
		_ = w.Write(r)
	}
	w.Flush()
	return b.Bytes()
}

// --- charts -----------------------------------------------------------------

func (d *reportData) charts() map[string]string {
	out := map[string]string{}
	var ticks []chartTick
	var target, sent, okRate, done []xy
	var ackP95, bizP95, bizP99, qP95 []xy
	for i, p := range d.phases {
		x := float64(i)
		ticks = append(ticks, chartTick{X: x, Label: fmt.Sprintf("%s %s", p.PhaseID[:3], fmtNum(p.TargetMessagesPerSec))})
		m := p.MessageTotals()
		target = append(target, xy{x, p.TargetMessagesPerSec, true})
		pt := func(v *float64) xy {
			if v == nil {
				return xy{X: x}
			}
			return xy{x, *v, true}
		}
		sent = append(sent, pt(perSec(m.Sent, p.MeasureSeconds)))
		okRate = append(okRate, pt(perSec(m.OK, p.MeasureSeconds)))
		done = append(done, pt(p.BusinessCompletedPerSec))
		ackP95 = append(ackP95, pt(m.Latency.Percentiles().P95MS))
		bp := p.Integrity.BusinessLatency.Percentiles()
		bizP95 = append(bizP95, pt(bp.P95MS))
		bizP99 = append(bizP99, pt(bp.P99MS))
		q := StreamStats{}
		q.Merge(p.Streams["query"])
		qP95 = append(qP95, pt(q.Latency.Percentiles().P95MS))
	}
	var bands []chartBand
	for i, p := range d.phases {
		fill := "#dcfce7"
		switch p.Verdict {
		case VerdictFailed:
			fill = "#fee2e2"
		case VerdictInconclusive:
			fill = "#f3f4f6"
		}
		bands = append(bands, chartBand{From: float64(i) - 0.45, To: float64(i) + 0.45, Label: p.PhaseID + " " + p.Verdict, Fill: fill})
	}
	out["throughput"] = d.chart("throughput", chartSpec{Title: "负载与吞吐（每档测量窗口）", ENTitle: "Load and throughput per step", ENYLabel: "msg/s", XLabel: "阶段与目标速率（绿=通过 红=失败 灰=证据不足）", YLabel: "消息/秒", Ticks: ticks, Bands: bands, Markers: true, Series: []chartSeries{
		{Name: "计划速率", En: "planned", Points: target, Dashed: true}, {Name: "实发", En: "sent", Points: sent}, {Name: "入口确认", En: "accepted", Points: okRate}, {Name: "业务完成", En: "completed", Points: done}}})
	slo := d.plan.SLO
	out["latency"] = d.chart("latency", chartSpec{Title: "时延拐点", ENTitle: "Latency by step", ENYLabel: "ms", XLabel: "阶段与目标速率", YLabel: "毫秒", Ticks: ticks, Bands: bands, Markers: true,
		Lines:  []chartLine{{Label: "业务 P95 SLO", En: "business P95 SLO", Y: slo.BusinessP95.D().Seconds() * 1000}, {Label: "查询 P95 SLO", En: "query P95 SLO", Y: slo.QueryP95.D().Seconds() * 1000}},
		Series: []chartSeries{{Name: "入口确认 P95", En: "ack P95", Points: ackP95}, {Name: "业务完成 P95", En: "business P95", Points: bizP95}, {Name: "业务完成 P99", En: "business P99", Points: bizP99, Dashed: true}, {Name: "查询 P95", En: "query P95", Points: qP95}}})
	var tbands []chartBand
	for _, p := range d.phases {
		tbands = append(tbands, chartBand{From: float64(p.MeasureFrom), To: float64(p.MeasureTo), Label: p.PhaseID + " 测量窗口", Fill: "#eff6ff"})
	}
	toXY := func(ps []Point) []xy {
		out := make([]xy, len(ps))
		for i, p := range ps {
			out[i] = xy{float64(p.At), p.Value, p.Valid}
		}
		return out
	}
	// Grey bands mark rounds where an instance scrape failed.
	for _, r := range d.rounds {
		for _, s := range r.Instances {
			if !s.OK {
				tbands = append(tbands, chartBand{From: float64(r.At) - 1000, To: float64(r.At) + 1000, Label: "缺测 " + s.Instance, Fill: "#d1d5db"})
				break
			}
		}
	}
	out["backlog"] = d.chart("backlog", chartSpec{Title: "管道积压（Kafka 消费组 lag + MQTT inbox）", ENTitle: "Pipeline backlog (Kafka lag + MQTT inbox)", ENYLabel: "items", XLabel: "时间（蓝色为各档测量窗口，灰色为缺测）", YLabel: "条目（未完成工作量）", TimeAxis: true, Bands: tbands, Series: []chartSeries{
		{Name: "合计积压", En: "total backlog", Points: toXY(BacklogSeries(d.rounds))}, {Name: "kafka_lag_parser", Points: toXY(GaugeSeries(d.rounds, "kafka_lag_parser")), Dashed: true},
		{Name: "kafka_lag_storage", Points: toXY(GaugeSeries(d.rounds, "kafka_lag_storage")), Dashed: true}, {Name: "mqtt_inbox_pending", Points: toXY(GaugeSeries(d.rounds, "mqtt_inbox_pending")), Dashed: true}},
		Note: "lag 由平台约每 15 秒采样"})
	var res []chartSeries
	for _, inst := range instances(d.rounds) {
		pts := InstanceSeries(d.rounds, inst, "go_memstats_heap_inuse_bytes")
		for i := range pts {
			pts[i].Value /= 1 << 20
		}
		res = append(res, chartSeries{Name: inst + " heap MiB", Points: toXY(pts)})
	}
	resNote := "主机 CPU/磁盘需由节点监控补充（清单 nodes）"
	if len(d.nodes) > 0 {
		resNote = "主机 CPU/内存/磁盘见 hosts.svg"
	}
	out["resources"] = d.chart("resources", chartSpec{Title: "平台实例资源（Go 堆内存）", ENTitle: "Platform instances (Go heap)", ENYLabel: "MiB", XLabel: "时间", YLabel: "MiB", TimeAxis: true, Bands: tbands, Series: res, Note: resNote})
	if len(d.nodes) > 0 {
		var hs []chartSeries
		hosts := HostSeries(d.nodes)
		for _, host := range sortedKeys(hosts) {
			var cpu, mem, disk []xy
			for _, pt := range hosts[host] {
				cpu = append(cpu, xy{float64(pt.At), pt.CPU, pt.CPUValid})
				mem = append(mem, xy{float64(pt.At), pt.Memory, pt.MemValid})
				disk = append(disk, xy{float64(pt.At), pt.Disk, pt.DiskValid})
			}
			hs = append(hs, chartSeries{Name: host + " CPU", Points: cpu}, chartSeries{Name: host + " 内存", En: host + " mem", Points: mem, Dashed: true}, chartSeries{Name: host + " 磁盘繁忙", En: host + " disk", Points: disk, Dashed: true})
		}
		out["hosts"] = d.chart("hosts", chartSpec{Title: "主机资源（node-exporter）", ENTitle: "Hosts (node-exporter): CPU / memory / disk busy", ENYLabel: "%", XLabel: "时间（蓝色为各档测量窗口）", YLabel: "%", TimeAxis: true, Bands: tbands, Series: hs, Lines: []chartLine{{Label: "85%", En: "85%", Y: 85}}, Note: "CPU 不含 idle/iowait；磁盘为最忙物理盘的繁忙时间占比"})
	}
	if svg := d.recoveryChart(toXY); svg != "" {
		out["recovery"] = svg
	}
	return out
}

// recoveryChart plots processing rate and backlog around injected faults.
func (d *reportData) recoveryChart(toXY func([]Point) []xy) string {
	var bands []chartBand
	for _, p := range d.phases {
		for _, f := range p.Faults {
			if f.InjectedAt > 0 {
				to := f.RecoverAt
				if to == 0 {
					to = p.MeasureTo
				}
				bands = append(bands, chartBand{From: float64(f.InjectedAt), To: float64(to), Label: "故障 " + f.Action, Fill: "#fee2e2"})
			}
		}
	}
	if len(bands) == 0 {
		return ""
	}
	var rate []Point
	for i := 1; i < len(d.rounds); i++ {
		pt := Point{At: d.rounds[i].At}
		if v, ok := CounterIncrease(d.rounds[i-1:i+1], "parse_success_total"); ok {
			if dt := float64(d.rounds[i].At-d.rounds[i-1].At) / 1000; dt > 0 {
				pt.Value, pt.Valid = v/dt, true
			}
		}
		rate = append(rate, pt)
	}
	var lines []chartLine
	for _, p := range d.phases {
		if p.Recovery != nil && p.Recovery.BaselinePerSec != nil {
			lines = append(lines, chartLine{Label: p.PhaseID + " 基线", En: p.PhaseID + " baseline", Y: *p.Recovery.BaselinePerSec})
		}
	}
	return d.chart("recovery", chartSpec{Title: "故障与恢复（解析成功速率与积压）", ENTitle: "Fault and recovery (parsed/s and backlog)", ENYLabel: "per s / items", XLabel: "时间（红色为故障持续区间）", YLabel: "条/秒 · 条目", TimeAxis: true, Bands: bands, Lines: lines, Series: []chartSeries{
		{Name: "解析成功/秒", En: "parsed/s", Points: toXY(rate)}, {Name: "合计积压", En: "backlog", Points: toXY(BacklogSeries(d.rounds)), Dashed: true}}, Note: "恢复用时从最后一次恢复命令完成起算"})
}

func instances(rounds []Round) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range rounds {
		for _, s := range r.Instances {
			if !seen[s.Instance] {
				seen[s.Instance] = true
				out = append(out, s.Instance)
			}
		}
	}
	sort.Strings(out)
	return out
}

// --- text reports -----------------------------------------------------------

var verdictText = map[string]string{VerdictPassed: "通过", VerdictFailed: "失败", VerdictInconclusive: "证据不足", VerdictNotCovered: "未覆盖", "measured": "已测", StatusRecorded: "仅记录"}
var classText = map[string]string{ClassBounded: "已找到边界", ClassWide: "已找到边界（区间未收敛到目标精度）", ClassLowerOnly: "至少达到下界，尚未找到上限", ClassNoPass: "首档即失败", ClassUnstable: "结果不稳定", ClassInconclusive: "证据不足", ClassRegression: "回归（不认证最大容量）", ClassSoak: "长稳", ClassResilience: "故障恢复", ClassUnmeasured: "未测量"}
var reasonText = map[string]string{ReasonService: "服务能力上限", ReasonPolicy: "配额/保护策略", ReasonGenerator: "发压能力不足", ReasonObservability: "观测缺失", ReasonIntegrity: "数据完整性失败", ReasonBudget: "预算上限", ReasonCancel: "人工停止", ReasonInfrastructure: "测试基础设施故障", "coverage_incomplete": "覆盖不完整（full 套件含未适配模块）", "evidence_incomplete": "证据不完整", "boundary_unstable": "边界不稳定", "candidate_not_confirmed": "候选档未完成复测", ReasonNotRecovered: "上一档积压未排空"}

func tr(m map[string]string, k string) string {
	if v, ok := m[k]; ok {
		return v
	}
	if k == "" {
		return "—"
	}
	return k
}

// conclusion describes the measured bounds and remaining uncertainty.
func (d *reportData) conclusion() string {
	r := d.summary.Capacity["mixedBusinessMessagesPerSecond"]
	unit := " 条/秒（混合业务消息）"
	switch {
	case r.LowerPassedBound != nil && r.UpperFailedBound != nil && (r.Classification == ClassBounded || r.Classification == ClassWide):
		return fmt.Sprintf("本次条件下，稳定通过 %s，在 %s 失败，边界位于两者之间%s。", fmtNum(*r.LowerPassedBound), fmtNum(*r.UpperFailedBound), unit)
	case r.Classification == ClassLowerOnly && r.LowerPassedBound != nil:
		return fmt.Sprintf("至少达到 %s%s，尚未找到上限（停止原因：%s）。", fmtNum(*r.LowerPassedBound), unit, tr(reasonText, r.StopReason))
	case r.Classification == ClassNoPass && r.UpperFailedBound != nil:
		return fmt.Sprintf("首档 %s%s 即未通过（%s），本次没有通过档。", fmtNum(*r.UpperFailedBound), unit, tr(reasonText, r.FailureMode))
	case r.Classification == ClassUnstable && r.UpperFailedBound != nil:
		return fmt.Sprintf("曾通过的档位复测未通过：%s%s 失败（%s），结果不稳定，本次没有稳定通过档。", fmtNum(*r.UpperFailedBound), unit, tr(reasonText, r.FailureMode))
	case r.Classification == ClassResilience:
		return fmt.Sprintf("故障恢复预设：%s结论为「%s」，不认证最大容量。", d.recoveryText(), tr(verdictText, d.summary.Verdict))
	case r.Classification == ClassRegression || r.Classification == ClassSoak:
		return fmt.Sprintf("%s预设：结论为「%s」，不认证最大容量。", d.plan.Preset, tr(verdictText, d.summary.Verdict))
	}
	return fmt.Sprintf("结论为 inconclusive：%s，不填 0、不标记通过。", tr(reasonText, firstNonEmpty(r.StopReason, d.summary.VerdictReason)))
}

// recoveryText summarises fault steps in one sentence.
func (d *reportData) recoveryText() string {
	var parts []string
	for _, p := range d.phases {
		if p.Recovery == nil {
			continue
		}
		names := make([]string, 0, len(p.Faults))
		for _, f := range p.Faults {
			names = append(names, f.Agent+"/"+f.Action)
		}
		parts = append(parts, fmt.Sprintf("%s 在 %s msg/s 背景负载下注入 %s，%s。", p.PhaseID, fmtNum(p.TargetMessagesPerSec), strings.Join(names, "、"), p.Recovery.Detail))
	}
	return strings.Join(parts, "")
}

// faultRows lists every injected failure for the report tables.
func (d *reportData) faultRows() [][]string {
	var rows [][]string
	for _, p := range d.phases {
		for _, f := range p.Faults {
			held := "—"
			if f.RecoverAt > 0 && f.InjectedAt > 0 {
				held = fmt.Sprintf("%.0f", float64(f.RecoverAt-f.InjectedAt)/1000)
			}
			rec := "—"
			if p.Recovery != nil && p.Recovery.RecoverySeconds != nil {
				rec = fmtNum(*p.Recovery.RecoverySeconds)
			}
			rows = append(rows, []string{p.PhaseID, f.Agent + "/" + f.Action, fmt.Sprint(f.InjectOK), fmt.Sprint(f.RecoverOK), held, rec, clip(f.Output, 120)})
		}
	}
	return rows
}

func (d *reportData) markdown() string {
	s := d.summary
	var b strings.Builder
	r := s.Capacity["mixedBusinessMessagesPerSecond"]
	fmt.Fprintf(&b, "# 容量测试报告 %s\n\n", s.RunID)
	fmt.Fprintf(&b, "## 1. 本次结论\n\n%s\n\n", d.conclusion())
	if d.state.Message != "" {
		fmt.Fprintf(&b, "执行说明：%s\n\n", d.state.Message)
	}
	fmt.Fprintf(&b, "| 项目 | 值 |\n| --- | --- |\n| 执行状态 | %s |\n| 结论 | %s %s |\n| 搜索结论 | %s |\n| 最高通过档 | %s |\n| 最低失败档 | %s |\n| 建议运行值 | %s |\n| 健康验证时长 | %.0f 秒 |\n| 证据完整 | %v |\n\n",
		s.ExecutionStatus, tr(verdictText, s.Verdict), tr(reasonText, s.VerdictReason), tr(classText, r.Classification), fmtPtr(r.LowerPassedBound, ""), fmtPtr(r.UpperFailedBound, ""), recommendationText(r), r.HealthyVerifiedSeconds, s.EvidenceComplete)
	if len(s.EvidenceGaps) > 0 {
		b.WriteString("证据缺口：\n\n")
		for _, g := range s.EvidenceGaps {
			fmt.Fprintf(&b, "- %s\n", g)
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "## 2. 环境与比较条件\n\n- 提交：%s\n- 计划哈希：%s\n- 清单：%v；部署形态：%v\n- 控制端：%v %v\n\n", firstNonEmpty(s.SourceCommit, "未记录"), s.PlanHash, d.env["inventory"], d.env["deployment"], d.env["controllerOS"], d.env["goVersion"])
	fmt.Fprintf(&b, "## 3. 场景与负载\n\n- 预设 %s，套件 %s，随机种子 %d\n- 设备 %d 台（租户 %s，产品 %s），报文约 %d 字节，%d 个字段，告警比例 %.4f\n- 入口份额 %s；管理查询 %s 次/秒（组合 %s，随负载放大：%v）\n\n",
		d.plan.Preset, d.plan.Suite, d.plan.Seed, d.plan.Fixtures.DeviceCount, d.plan.Fixtures.Tenant, d.plan.Fixtures.Product, d.plan.Fixtures.MessageBytes, d.plan.Fixtures.Fields, d.plan.Fixtures.AlarmFraction, shareText(d.plan.Load.IngressShare), fmtNum(d.plan.Load.QueryRequestsPerSecond), shareText(d.plan.Load.QueryMix), d.plan.Load.ScaleQueries)
	b.WriteString("## 4. 阶段结果\n\n| 阶段 | 目标 msg/s | 实发 | 未发 | 入口成功 | 入口 P95 | 业务 P95 | 业务 P99 | 查询 P95 | 缺失 | 未知 | 待完成 | 结论 |\n| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |\n")
	for _, row := range d.phaseRows() {
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s %s |\n", row[0], row[2], row[6], row[7], row[8], dash(row[12]), dash(row[20]), dash(row[21]), dash(row[17]), row[24], row[25], row[26], tr(verdictText, row[34]), tr(reasonText, row[35]))
	}
	if rows := d.faultRows(); len(rows) > 0 {
		b.WriteString("\n故障注入：\n\n| 阶段 | 动作 | 注入成功 | 恢复成功 | 持续秒 | 恢复用时秒 | 输出 |\n| --- | --- | --- | --- | ---: | ---: | --- |\n")
		for _, r := range rows {
			fmt.Fprintf(&b, "| %s |\n", strings.Join(r, " | "))
		}
	}
	charts := "throughput.svg、latency.svg、backlog.svg、resources.svg"
	if len(d.nodes) > 0 {
		charts += "、hosts.svg"
	}
	if len(d.faultRows()) > 0 {
		charts += "、recovery.svg"
	}
	fmt.Fprintf(&b, "\n## 5. 图表\n\n图表位于 `charts/`：%s。\n\n", charts)
	b.WriteString("## 6. 数据完整性\n\n")
	in := s.Integrity
	fmt.Fprintf(&b, "核对方式：%s。唯一消息 %s，归档确认 %s，业务完成 %s，缺失 %s，结果未知 %s，待完成 %s，原文物理重复行 %s，TCP 仅 ACK %d。重复业务副作用：未测量。\n\n",
		in.VerificationMode, ptrU(in.UniqueSent), ptrU(in.UniqueArchiveConfirmed), ptrU(in.UniqueBusinessCompleted), ptrU(in.Missing), ptrU(in.Unknown), ptrU(in.Pending), ptrU(in.PhysicalDuplicateRows), in.TCPAckOnly)
	b.WriteString("## 7. 瓶颈候选与下一步\n\n")
	if len(s.Bottlenecks) == 0 {
		b.WriteString("没有未通过的档位可供归类。\n\n")
	}
	for _, x := range s.Bottlenecks {
		fmt.Fprintf(&b, "- **%s**（%s）：%s。下一步：%s\n", x.Candidate, x.PhaseID, x.Evidence, x.NextStep)
	}
	b.WriteString("\n## 8. 覆盖范围\n\n| 模块 | 状态 | 说明 |\n| --- | --- | --- |\n")
	for _, c := range s.Coverage {
		fmt.Fprintf(&b, "| %s | %s | %s |\n", c.Module, tr(verdictText, c.Status), c.Detail)
	}
	fmt.Fprintf(&b, "\n## 9. 复现方式\n\n```bash\ncapacity-test run --plan <计划> --secrets <秘密文件>\ncapacity-test report --run %s\n```\n\n所需秘密引用：%s。计划正文见 `plan.sanitized.yaml`。\n\n", s.RunID, strings.Join(d.secretNames(), "、"))
	b.WriteString("## 10. 测试后状态\n\n")
	if d.manifest != nil {
		for _, x := range append(append([]string{}, d.manifest.Retained...), d.manifest.Cleanup...) {
			fmt.Fprintf(&b, "- %s\n", x)
		}
		fmt.Fprintf(&b, "- 本次新建设备 %d 台，复用 %d 台\n", len(d.manifest.DevicesCreated), d.manifest.DevicesReused)
	} else {
		b.WriteString("- 未进入准备阶段，没有创建测试资源\n")
	}
	return b.String()
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func ptrU(v *uint64) string {
	if v == nil {
		return "—"
	}
	return strconv.FormatUint(*v, 10)
}

func recommendationText(r SearchResult) string {
	if r.RecommendedOperatingValue == nil {
		return "不输出（未取得经复测确认的通过档）"
	}
	return fmt.Sprintf("%s（= %.1f × 最高通过档，仅健康状态，无故障容量证据，不含 N−1 保证）", fmtNum(*r.RecommendedOperatingValue), RecommendationFactor)
}

func shareText(m map[string]float64) string {
	var parts []string
	for _, k := range sortedKeys(m) {
		parts = append(parts, fmt.Sprintf("%s %.0f%%", k, m[k]*100))
	}
	if len(parts) == 0 {
		return "—"
	}
	return strings.Join(parts, "，")
}

func (d *reportData) secretNames() []string {
	var out []string
	for _, n := range []string{d.plan.Credentials.OperatorSecretRef, d.plan.Credentials.AgentSecretRef} {
		if n != "" {
			out = append(out, n)
		}
	}
	return append(out, "清单 observers 中的核对库引用")
}

const reportCSS = `:root{--bg:#f8fafc;--card:#ffffff;--ink:#0f172a;--muted:#64748b;--line:#e2e8f0;--pass:#15803d;--fail:#b91c1c;--warn:#a16207;--accent:#2563eb}
@media (prefers-color-scheme:dark){:root:not([data-theme="light"]){--bg:#0b1120;--card:#111827;--ink:#e5e7eb;--muted:#94a3b8;--line:#1f2937;--pass:#4ade80;--fail:#f87171;--warn:#facc15;--accent:#60a5fa}}
:root[data-theme="dark"]{--bg:#0b1120;--card:#111827;--ink:#e5e7eb;--muted:#94a3b8;--line:#1f2937;--pass:#4ade80;--fail:#f87171;--warn:#facc15;--accent:#60a5fa}
*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--ink);font:14px/1.6 system-ui,-apple-system,"Segoe UI","PingFang SC","Microsoft YaHei",sans-serif}
main{max-width:1120px;margin:0 auto;padding:24px 16px 64px}h1{font-size:22px;margin:0 0 4px}h2{font-size:17px;margin:32px 0 12px}
.muted{color:var(--muted)}.card{background:var(--card);border:1px solid var(--line);border-radius:10px;padding:16px;margin:12px 0}
.hero{font-size:16px;font-weight:600}.kpis{display:grid;grid-template-columns:repeat(auto-fit,minmax(150px,1fr));gap:12px}
.kpi{background:var(--card);border:1px solid var(--line);border-radius:10px;padding:12px}.kpi b{display:block;font-size:20px}.kpi span{color:var(--muted);font-size:12px}
.scroll{overflow-x:auto}table{border-collapse:collapse;width:100%;background:var(--card)}th,td{border-bottom:1px solid var(--line);padding:6px 8px;text-align:left;white-space:nowrap;font-size:13px}th{color:var(--muted);font-weight:600}
td.n{text-align:right;font-variant-numeric:tabular-nums}.passed{color:var(--pass)}.failed{color:var(--fail)}.inconclusive,.not_covered{color:var(--warn)}
.chart{background:#fff;border-radius:10px;border:1px solid var(--line);margin:12px 0;overflow:hidden}pre{background:var(--card);border:1px solid var(--line);border-radius:8px;padding:12px;overflow-x:auto}
ul{padding-left:20px}`

func (d *reportData) html(charts map[string]string) string {
	s := d.summary
	r := s.Capacity["mixedBusinessMessagesPerSecond"]
	e := html.EscapeString
	var b strings.Builder
	fmt.Fprintf(&b, "<!doctype html><html lang=\"zh-CN\"><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\"><title>容量测试报告</title><style>%s</style></head><body><main>", reportCSS)
	fmt.Fprintf(&b, "<h1>容量测试报告</h1><div class=\"muted\">%s · 预设 %s · 套件 %s · 执行状态 %s</div>", e(s.RunID), e(d.plan.Preset), e(d.plan.Suite), e(s.ExecutionStatus))
	fmt.Fprintf(&b, "<h2>1. 本次结论</h2><div class=\"card hero\"><span class=\"%s\">%s</span> · %s</div>", e(s.Verdict), e(tr(verdictText, s.Verdict)), e(d.conclusion()))
	if d.state.Message != "" {
		fmt.Fprintf(&b, "<p>执行说明：%s</p>", e(d.state.Message))
	}
	b.WriteString("<div class=\"kpis\">")
	kpi := func(v, label string) {
		fmt.Fprintf(&b, "<div class=\"kpi\"><b>%s</b><span>%s</span></div>", e(v), e(label))
	}
	kpi(fmtPtr(r.LowerPassedBound, ""), "最高通过档 msg/s")
	kpi(fmtPtr(r.UpperFailedBound, ""), "最低失败档 msg/s")
	rec := "—"
	if r.RecommendedOperatingValue != nil {
		rec = fmtNum(*r.RecommendedOperatingValue)
	}
	kpi(rec, "建议运行值（仅健康状态）")
	kpi(fmt.Sprintf("%.0f s", r.HealthyVerifiedSeconds), "健康验证时长")
	kpi(tr(classText, r.Classification), "搜索结论")
	kpi(tr(reasonText, firstNonEmpty(r.FailureMode, r.StopReason)), "失败/停止原因")
	b.WriteString("</div>")
	fmt.Fprintf(&b, "<p class=\"muted\">建议运行值：%s</p>", e(recommendationText(r)))
	if len(s.EvidenceGaps) > 0 {
		b.WriteString("<div class=\"card\"><b>证据缺口</b><ul>")
		for _, g := range s.EvidenceGaps {
			fmt.Fprintf(&b, "<li>%s</li>", e(g))
		}
		b.WriteString("</ul></div>")
	}
	b.WriteString("<h2>2. 环境与比较条件</h2><div class=\"card\"><ul>")
	fmt.Fprintf(&b, "<li>提交：%s；计划哈希：<code>%s</code></li>", e(firstNonEmpty(s.SourceCommit, "未记录")), e(s.PlanHash))
	fmt.Fprintf(&b, "<li>清单 %v，部署形态 %v，环境类别 %v</li>", e(fmt.Sprint(d.env["inventory"])), e(fmt.Sprint(d.env["deployment"])), e(fmt.Sprint(d.env["environmentClass"])))
	fmt.Fprintf(&b, "<li>控制端 %v，%v；硬件、镜像摘要与中间件拓扑需由清单登记方补充</li>", e(fmt.Sprint(d.env["controllerOS"])), e(fmt.Sprint(d.env["goVersion"])))
	b.WriteString("</ul></div>")
	b.WriteString("<h2>3. 场景与负载</h2><div class=\"card\"><ul>")
	f := d.plan.Fixtures
	fmt.Fprintf(&b, "<li>设备 %d 台（租户 %s，产品 %s），报文约 %d 字节、%d 个字段，告警比例 %.4f</li>", f.DeviceCount, e(f.Tenant), e(f.Product), f.MessageBytes, f.Fields, f.AlarmFraction)
	fmt.Fprintf(&b, "<li>入口份额：%s；MQTT 发布连接 %d；TCP 连接 %d</li>", e(shareText(d.plan.Load.IngressShare)), d.plan.Load.MQTTConnections, d.plan.Load.TCPConnections)
	fmt.Fprintf(&b, "<li>管理查询 %s 次/秒，组合 %s，随负载放大：%v（开环请求，不等于真实用户数）</li>", e(fmtNum(d.plan.Load.QueryRequestsPerSecond)), e(shareText(d.plan.Load.QueryMix)), d.plan.Load.ScaleQueries)
	sp := d.plan.Search
	fmt.Fprintf(&b, "<li>预热 %s，测量 %s，候选持有 %s × %d 次，排空上限 %s，边界精度 %.0f%%</li>", sp.Warmup, sp.Measure, sp.CandidateHold, sp.Repeats, sp.DrainTimeout, sp.BoundaryRelativeWidth*100)
	b.WriteString("</ul></div>")
	b.WriteString("<h2>4. 阶段结果</h2><div class=\"scroll\"><table><thead><tr><th>阶段</th><th>目标 msg/s</th><th>实发</th><th>未发</th><th>入口成功</th><th>入口 P95</th><th>业务 P95</th><th>业务 P99</th><th>查询 P95</th><th>缺失</th><th>未知</th><th>待完成</th><th>归档/s</th><th>积压斜率</th><th>结论</th></tr></thead><tbody>")
	for _, row := range d.phaseRows() {
		fmt.Fprintf(&b, "<tr><td>%s</td>", e(row[0]))
		for _, i := range []int{2, 6, 7, 8, 12, 20, 21, 17, 24, 25, 26, 27, 32} {
			fmt.Fprintf(&b, "<td class=\"n\">%s</td>", e(dash(row[i])))
		}
		fmt.Fprintf(&b, "<td class=\"%s\">%s %s</td></tr>", e(row[34]), e(tr(verdictText, row[34])), e(tr(reasonText, row[35])))
	}
	b.WriteString("</tbody></table></div><p class=\"muted\">时延单位毫秒；“—”表示样本不足或缺测，不是零。入口 P95 为成功请求的确认时延；业务时延从预定发送时刻到业务处理完成。</p>")
	for _, p := range d.phases {
		fmt.Fprintf(&b, "<details class=\"card\"><summary>%s 检查项（%s）</summary><div class=\"scroll\"><table><tbody>", e(p.PhaseID), e(tr(verdictText, p.Verdict)))
		for _, c := range p.Checks {
			fmt.Fprintf(&b, "<tr><td>%s</td><td class=\"%s\">%s</td><td>%s</td></tr>", e(c.Name), e(c.Status), e(tr(verdictText, c.Status)), e(c.Detail))
		}
		b.WriteString("</tbody></table></div></details>")
	}
	if rows := d.faultRows(); len(rows) > 0 {
		b.WriteString("<div class=\"card\"><b>故障注入</b><div class=\"scroll\"><table><thead><tr><th>阶段</th><th>动作</th><th>注入成功</th><th>恢复成功</th><th>持续秒</th><th>恢复用时秒</th><th>输出</th></tr></thead><tbody>")
		for _, r := range rows {
			b.WriteString("<tr>")
			for _, c := range r {
				fmt.Fprintf(&b, "<td>%s</td>", e(c))
			}
			b.WriteString("</tr>")
		}
		b.WriteString("</tbody></table></div></div>")
	}
	b.WriteString("<h2>5. 图表</h2>")
	for _, name := range []string{"throughput", "latency", "backlog", "resources", "hosts", "recovery"} {
		if charts[name] != "" {
			fmt.Fprintf(&b, "<div class=\"chart\">%s</div>", charts[name])
		}
	}
	in := s.Integrity
	b.WriteString("<h2>6. 数据完整性</h2><div class=\"card\">")
	fmt.Fprintf(&b, "<p>核对方式 <b>%s</b>：按原文 ID 核对归档索引与摘要、正文可读、发布、解析、业务处理完成%s。</p>", e(in.VerificationMode), e(telemetryNote(d.env)))
	fmt.Fprintf(&b, "<div class=\"kpis\">")
	for _, kv := range [][2]string{{ptrU(in.UniqueSent), "唯一消息"}, {ptrU(in.UniqueArchiveConfirmed), "归档确认"}, {ptrU(in.UniqueBusinessCompleted), "业务完成"}, {ptrU(in.Missing), "已确认但缺失"}, {ptrU(in.Unknown), "结果未知"}, {ptrU(in.Pending), "排空后仍待完成"}, {ptrU(in.PhysicalDuplicateRows), "原文物理重复行"}, {"未测量", "重复业务副作用"}} {
		kpi(kv[0], kv[1])
	}
	b.WriteString("</div><p class=\"muted\">unknown 表示入口响应不确定且未找到归档，不视为成功，也不直接认定为永久丢失。</p></div>")
	b.WriteString("<h2>7. 瓶颈候选与下一步</h2><div class=\"card\">")
	if len(s.Bottlenecks) == 0 {
		b.WriteString("<p>没有未通过的档位可供归类。</p>")
	} else {
		b.WriteString("<ul>")
		for _, x := range s.Bottlenecks {
			fmt.Fprintf(&b, "<li><b>%s</b>（%s）：%s。下一步：%s</li>", e(x.Candidate), e(x.PhaseID), e(x.Evidence), e(x.NextStep))
		}
		b.WriteString("</ul>")
	}
	b.WriteString("<p class=\"muted\">候选原因由固定规则生成，是后续剖析方向，不是已确认的根因。</p></div>")
	b.WriteString("<h2>8. 覆盖范围</h2><div class=\"scroll\"><table><thead><tr><th>模块</th><th>状态</th><th>说明</th></tr></thead><tbody>")
	for _, c := range s.Coverage {
		fmt.Fprintf(&b, "<tr><td>%s</td><td class=\"%s\">%s</td><td>%s</td></tr>", e(c.Module), e(c.Status), e(tr(verdictText, c.Status)), e(c.Detail))
	}
	b.WriteString("</tbody></table></div>")
	fmt.Fprintf(&b, "<h2>9. 复现方式</h2><pre>capacity-test run --plan &lt;计划&gt; --secrets &lt;秘密文件&gt;\ncapacity-test report --run %s</pre><p class=\"muted\">所需秘密引用：%s。以下为脱敏计划：</p><pre>%s</pre>", e(s.RunID), e(strings.Join(d.secretNames(), "、")), e(d.planYAML))
	b.WriteString("<h2>10. 测试后状态</h2><div class=\"card\"><ul>")
	if d.manifest != nil {
		for _, x := range append(append([]string{}, d.manifest.Retained...), d.manifest.Cleanup...) {
			fmt.Fprintf(&b, "<li>%s</li>", e(x))
		}
		fmt.Fprintf(&b, "<li>本次新建设备 %d 台，复用 %d 台</li>", len(d.manifest.DevicesCreated), d.manifest.DevicesReused)
	} else {
		b.WriteString("<li>未进入准备阶段，没有创建测试资源</li>")
	}
	for _, c := range d.preflight {
		if !c.OK {
			fmt.Fprintf(&b, "<li class=\"failed\">前置检查未通过：%s — %s</li>", e(c.Name), e(c.Detail))
		}
	}
	b.WriteString("</ul></div></main></body></html>\n")
	return b.String()
}

func telemetryNote(env map[string]any) string {
	if obs, ok := env["observers"].(map[string]any); ok && obs["clickhouse"] == true {
		return "，并核对 ClickHouse 遥测行"
	}
	return "；未配置 ClickHouse 核对，遥测表未核对"
}

// checkNoSecrets fails the report when any resolved secret value appears in a
// plain-text evidence or report file.
func checkNoSecrets(dir string, secrets []string) error {
	if len(secrets) == 0 {
		return nil
	}
	var leaks []string
	_ = filepath.WalkDir(dir, func(path string, de fs.DirEntry, err error) error {
		if err != nil || de.IsDir() || strings.HasSuffix(path, ".gz") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		for _, s := range secrets {
			if bytes.Contains(b, []byte(s)) {
				rel, _ := filepath.Rel(dir, path)
				leaks = append(leaks, rel)
				break
			}
		}
		return nil
	})
	if len(leaks) > 0 {
		sort.Strings(leaks)
		return errors.New("secret value found in evidence files: " + strings.Join(leaks, ", "))
	}
	return nil
}

// writeChecksums lists every evidence file with its SHA-256. state.json and
// events.jsonl keep changing until the controller exits and are excluded.
func writeChecksums(dir string) error {
	var lines []string
	err := filepath.WalkDir(dir, func(path string, de fs.DirEntry, err error) error {
		if err != nil || de.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		rel = filepath.ToSlash(rel)
		if rel == "checksums.txt" || rel == "state.json" || rel == "events.jsonl" || rel == StopFile || strings.HasSuffix(rel, ".tmp") {
			return nil
		}
		sum, size, err := fileDigest(path)
		if err != nil {
			return err
		}
		lines = append(lines, fmt.Sprintf("%s  %d  %s", sum, size, rel))
		return nil
	})
	if err != nil {
		return err
	}
	sort.Slice(lines, func(i, j int) bool { return strings.Fields(lines[i])[2] < strings.Fields(lines[j])[2] })
	return os.WriteFile(filepath.Join(dir, "checksums.txt"), []byte(strings.Join(lines, "\n")+"\n"), 0o640)
}
