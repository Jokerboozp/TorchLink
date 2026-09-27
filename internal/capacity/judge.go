package capacity

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Verdicts and structured stop reasons (plan §13.4). Only a service_limit with
// complete evidence bounds system capacity.
const (
	VerdictPassed       = "passed"
	VerdictFailed       = "failed"
	VerdictInconclusive = "inconclusive"
	VerdictNotCovered   = "not_covered"

	ReasonService        = "service_limit"
	ReasonPolicy         = "policy_limit"
	ReasonGenerator      = "generator_limit"
	ReasonObservability  = "observability_gap"
	ReasonIntegrity      = "data_integrity_failure"
	ReasonBudget         = "budget_limit"
	ReasonCancel         = "manual_cancel"
	ReasonInfrastructure = "infrastructure_failure"
)

type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
	Detail string `json:"detail"`
}

type AgentPhaseSummary struct {
	Name               string     `json:"name"`
	Interrupted        bool       `json:"interrupted"`
	LeaseExpired       bool       `json:"leaseExpired"`
	Reason             string     `json:"reason,omitempty"`
	ClockOffsetMS      float64    `json:"clockOffsetMs"`
	ClockUncertaintyMS float64    `json:"clockUncertaintyMs"`
	HeapBytes          uint64     `json:"heapBytes"`
	Goroutines         int        `json:"goroutines"`
	Ledger             LedgerInfo `json:"ledger"`
}

// Pipeline is what platform metrics said during the measurement window.
// Nil values are missing observations, never zero.
type Pipeline struct {
	MetricsValid       bool     `json:"metricsValid"`
	ArchivedPerSec     *float64 `json:"archivedPerSec"`
	ParsedPerSec       *float64 `json:"parsedPerSec"`
	AlarmsPerSec       *float64 `json:"alarmsPerSec"`
	BacklogStart       *float64 `json:"backlogStart"`
	BacklogEnd         *float64 `json:"backlogEnd"`
	BacklogSlopePerSec *float64 `json:"backlogSlopePerSec"`
	BacklogPoints      int      `json:"backlogPoints"`
	FailedScrapes      int      `json:"failedScrapes"`
	Rounds             int      `json:"rounds"`
}

type Drain struct {
	Seconds   float64 `json:"seconds"`
	Completed bool    `json:"completed"`
}

type PhaseRecord struct {
	PhaseID              string                  `json:"phaseId"`
	Index                int                     `json:"index"`
	Kind                 string                  `json:"kind"`
	TargetMessagesPerSec float64                 `json:"targetMessagesPerSecond"`
	TargetQueriesPerSec  float64                 `json:"targetQueriesPerSecond"`
	StartedAt            int64                   `json:"startedAt"`
	MeasureFrom          int64                   `json:"measureFrom"`
	MeasureTo            int64                   `json:"measureTo"`
	EndedAt              int64                   `json:"endedAt"`
	WarmupSeconds        float64                 `json:"warmupSeconds"`
	MeasureSeconds       float64                 `json:"measureSeconds"`
	Streams              map[string]*StreamStats `json:"streams"`
	Agents               []AgentPhaseSummary     `json:"agents"`
	Pipeline             Pipeline                `json:"pipeline"`
	Drain                Drain                   `json:"drain"`
	Integrity            Integrity               `json:"integrity"`
	Verdict              string                  `json:"verdict"`
	StopReason           string                  `json:"stopReason,omitempty"`
	Checks               []Check                 `json:"checks"`
	Cancelled            bool                    `json:"cancelled,omitempty"`
	// Operations are single business operations run during the step
	// (backup, download, restore); Faults are injected failures.
	Operations              []Operation   `json:"operations,omitempty"`
	Faults                  []FaultEvent  `json:"faults,omitempty"`
	Recovery                *RecoveryInfo `json:"recovery,omitempty"`
	BusinessCompletedPerSec *float64      `json:"businessCompletedPerSecond"`
}

var messageStreams = []string{"http", "mqtt", "tcp"}

var moduleLabel = map[string]string{"ai": "AI 研判", "knowledge": "知识库上传", "video": "视频播放", "export_raw": "原文下载", "export_replay": "报文回放", "export_inspection": "巡检与 PDF", "openapi": "开放 API"}

// Operation is one business operation measured during a step.
type Operation struct {
	Name    string            `json:"name"`
	OK      bool              `json:"ok"`
	Seconds float64           `json:"seconds"`
	Detail  string            `json:"detail"`
	Steps   map[string]string `json:"steps,omitempty"`
}

// StatusRecorded marks a check kept for information only (resilience steps).
const StatusRecorded = "recorded"

// FaultEvent records an injected failure and its recovery.
type FaultEvent struct {
	Agent      string `json:"agent"`
	Action     string `json:"action"`
	InjectedAt int64  `json:"injectedAt"`
	RecoverAt  int64  `json:"recoveredAt"`
	InjectOK   bool   `json:"injectOk"`
	RecoverOK  bool   `json:"recoverOk"`
	Output     string `json:"output,omitempty"`
}

// RecoveryInfo summarises a resilience step.
type RecoveryInfo struct {
	RecoverySeconds *float64 `json:"recoverySeconds"`
	BaselinePerSec  *float64 `json:"baselinePerSecond"`
	DuringPerSec    *float64 `json:"duringFaultPerSecond"`
	Detail          string   `json:"detail"`
}

// MessageTotals merges the device-message streams of a phase.
func (r *PhaseRecord) MessageTotals() StreamStats {
	var t StreamStats
	for _, s := range messageStreams {
		t.Merge(r.Streams[s])
	}
	return t
}

// Judge evaluates one phase against the plan's SLOs and fills Verdict,
// StopReason and Checks deterministically from recorded evidence.
func Judge(p *Plan, r *PhaseRecord) {
	r.Checks = nil
	add := func(name, status, reason, format string, a ...any) {
		r.Checks = append(r.Checks, Check{Name: name, Status: status, Reason: reason, Detail: fmt.Sprintf(format, a...)})
	}
	slo := p.SLO
	// A resilience step measures behaviour under an injected failure: the
	// normal SLOs are recorded, recovery and integrity decide.
	resilience := r.Kind == PresetResilience
	sloAdd := add
	if resilience {
		sloAdd = func(name, status, reason, format string, a ...any) {
			if status == VerdictPassed {
				add(name, status, reason, format, a...)
				return
			}
			add(name, StatusRecorded, "", "故障期间仅记录："+format, a...)
		}
	}
	if r.Cancelled {
		add("停止", VerdictInconclusive, ReasonCancel, "本档被人工停止，结果不完整")
	}
	for _, a := range r.Agents {
		if a.LeaseExpired || a.Interrupted {
			add("发压 Agent", VerdictInconclusive, ReasonInfrastructure, "Agent %s 未完整执行本档：%s", a.Name, a.Reason)
		}
	}
	// 1. Generator: the planned load must actually have been offered.
	for _, name := range append(append(append([]string{}, messageStreams...), "query"), ModuleStreamNames...) {
		s := r.Streams[name]
		if s == nil || s.Scheduled == 0 {
			continue
		}
		short := float64(s.NotSent) / float64(s.Scheduled)
		late, _ := s.Lateness.Quantile(0.95)
		switch {
		case short > slo.SendTolerance:
			sloAdd("实发达成 "+name, VerdictInconclusive, ReasonGenerator, "计划 %d 条，未发出 %d 条（%.2f%%，容差 %.2f%%）", s.Scheduled, s.NotSent, short*100, slo.SendTolerance*100)
		case late > 1000:
			sloAdd("配速准确 "+name, VerdictInconclusive, ReasonGenerator, "调度迟到 P95 %.0fms，超过 1000ms", late)
		default:
			sloAdd("实发达成 "+name, VerdictPassed, "", "计划 %d 条，实发 %d 条，调度迟到 P95 %.1fms", s.Scheduled, s.Sent, late)
		}
	}
	// 2. Ingress success of device messages.
	m := r.MessageTotals()
	if m.Sent > 0 {
		ratio := float64(m.OK) / float64(m.Sent)
		if ratio >= slo.SuccessRatio {
			sloAdd("入口成功率", VerdictPassed, "", "%.4f ≥ %.4f（%d/%d）", ratio, slo.SuccessRatio, m.OK, m.Sent)
		} else {
			reason := ReasonService
			if m.Codes["429"]*2 > m.Fail {
				reason = ReasonPolicy
			}
			sloAdd("入口成功率", VerdictFailed, reason, "%.4f < %.4f；结果码 %s", ratio, slo.SuccessRatio, codeSummary(m.Codes))
		}
	}
	// 3. Management queries.
	if q := r.Streams["query"]; q != nil && q.Sent > 0 {
		ratio := float64(q.OK) / float64(q.Sent)
		if ratio < slo.SuccessRatio {
			sloAdd("查询成功率", VerdictFailed, ReasonService, "%.4f < %.4f；结果码 %s", ratio, slo.SuccessRatio, codeSummary(q.Codes))
		} else {
			sloAdd("查询成功率", VerdictPassed, "", "%.4f（%d/%d）", ratio, q.OK, q.Sent)
		}
		latencyCheck(sloAdd, "查询时延", q.Latency, slo.QueryP95.D().Seconds()*1000, slo.QueryP99.D().Seconds()*1000)
	}
	// Business modules: each keeps its own success and latency targets.
	for _, name := range ModuleStreamNames {
		st := r.Streams[name]
		if st == nil || st.Sent == 0 {
			continue
		}
		label := moduleLabel[name]
		capped := st.Codes["budget_exhausted"]
		if n := st.Codes["no_active_alarm"]; n > 0 {
			add(label, VerdictInconclusive, ReasonObservability, "%d 次没有可研判的活动告警（设备负载需产生告警）", n)
			continue
		}
		attempted := st.Sent - capped
		if attempted == 0 {
			add(label, VerdictInconclusive, ReasonBudget, "请求预算已用完，本档未执行")
			continue
		}
		ratio := float64(st.OK) / float64(attempted)
		if ratio < slo.SuccessRatio {
			reason := ReasonService
			if st.Codes["429"]*2 > st.Fail {
				reason = ReasonPolicy
			}
			sloAdd(label, VerdictFailed, reason, "成功率 %.4f < %.4f；结果码 %s", ratio, slo.SuccessRatio, codeSummary(st.Codes))
		} else {
			add(label, VerdictPassed, "", "成功 %d/%d（预算截止 %d）", st.OK, attempted, capped)
		}
		if limit := p.ModuleP95(name); limit > 0 {
			latencyCheck(sloAdd, label+"时延", st.Latency, limit.Seconds()*1000, limit.Seconds()*1000*2.5)
		}
	}
	if rt := r.Streams["realtime"]; rt != nil {
		if rt.OK == 0 {
			add("实时推送", VerdictInconclusive, ReasonObservability, "测量窗口内订阅者未收到告警或状态推送")
		} else if limit := p.ModuleP95("realtime"); limit > 0 {
			latencyCheck(add, "实时推送时延", rt.Latency, limit.Seconds()*1000, limit.Seconds()*1000*2.5)
		}
	}
	for _, op := range r.Operations {
		if op.OK {
			add("业务操作 "+op.Name, VerdictPassed, "", "%s（%.0f 秒）", op.Detail, op.Seconds)
		} else {
			add("业务操作 "+op.Name, VerdictFailed, ReasonService, "%s（%.0f 秒）", op.Detail, op.Seconds)
		}
	}
	// 4. Integrity and drain.
	in := r.Integrity
	if in.UniqueSent > 0 {
		switch {
		case in.Unaccounted > uint64(slo.MaximumUnaccountedConfirmedMessage):
			add("数据完整性", VerdictFailed, ReasonIntegrity, "已确认消息缺失/冲突/解析失败 %d 条（允许 %d）；状态 %s", in.Unaccounted, slo.MaximumUnaccountedConfirmedMessage, codeSummary(in.States))
		case in.Pending > 0:
			add("排空", VerdictFailed, ReasonService, "排空时限内仍有 %d 条未完成业务处理", in.Pending)
		case in.AlarmMismatches > 0:
			add("告警序列", VerdictFailed, ReasonIntegrity, "%d/%d 台设备的告警与上报序列不符：%s", in.AlarmMismatches, in.AlarmDevicesChecked, strings.Join(in.AlarmSamples, "；"))
		default:
			add("数据完整性", VerdictPassed, "", "唯一消息 %d，业务完成 %d，结果未知 %d，入口拒绝 %d", in.UniqueSent, in.UniqueBusinessDone, in.Unknown, in.States[StateRejected])
		}
		// 5. Business completion latency.
		switch {
		case !in.BusinessLatencyValid:
			sloAdd("业务完成时延", VerdictInconclusive, ReasonObservability, "无法计算：%s", firstNonEmpty(in.Note, "测量窗口内没有完成的消息"))
		case in.ClockUncertaintyMS > slo.MaxClockUncertainty.D().Seconds()*1000:
			sloAdd("业务完成时延", VerdictInconclusive, ReasonObservability, "时钟误差 %.1fms 超过 %.0fms，端到端时延无效", in.ClockUncertaintyMS, slo.MaxClockUncertainty.D().Seconds()*1000)
		default:
			latencyCheck(sloAdd, "业务完成时延", in.BusinessLatency, slo.BusinessP95.D().Seconds()*1000, slo.BusinessP99.D().Seconds()*1000)
		}
	} else if in.TCPAckOnly > 0 {
		sloAdd("数据完整性", VerdictPassed, "", "仅 TCP 协议 ACK 证据 %d 条；未做原文 ID 核对", in.TCPAckOnly)
	}
	// 6. Pipeline backlog must not keep growing during the window.
	if m.Sent > 0 {
		pl := r.Pipeline
		if pl.BacklogSlopePerSec == nil {
			sloAdd("管道积压趋势", VerdictInconclusive, ReasonObservability, "积压指标不足（有效点 %d），无法判断是否持续增长", pl.BacklogPoints)
		} else {
			growth := *pl.BacklogSlopePerSec * r.MeasureSeconds
			limit := math.Max(50, slo.MaxBacklogGrowthRatio*float64(m.Sent))
			if growth > limit {
				sloAdd("管道积压趋势", VerdictFailed, ReasonService, "测量窗口内积压约增长 %.0f（斜率 %.2f/s），超过 %.0f", growth, *pl.BacklogSlopePerSec, limit)
			} else {
				sloAdd("管道积压趋势", VerdictPassed, "", "窗口内趋势 %.2f/s，估算增长 %.0f（上限 %.0f）", *pl.BacklogSlopePerSec, growth, limit)
			}
		}
	}
	if resilience {
		judgeFaults(p, r, add)
	}
	r.Verdict, r.StopReason = VerdictPassed, ""
	order := []string{ReasonIntegrity, ReasonService, ReasonPolicy}
	for _, reason := range order {
		for _, c := range r.Checks {
			if c.Status == VerdictFailed && c.Reason == reason && r.Verdict == VerdictPassed {
				r.Verdict, r.StopReason = VerdictFailed, reason
			}
		}
	}
	if r.Verdict == VerdictPassed {
		for _, c := range r.Checks {
			if c.Status == VerdictInconclusive {
				r.Verdict, r.StopReason = VerdictInconclusive, c.Reason
				break
			}
		}
	}
	if r.Verdict == VerdictPassed && m.Sent == 0 && (r.Streams["query"] == nil || r.Streams["query"].Sent == 0) {
		r.Verdict, r.StopReason = VerdictInconclusive, ReasonGenerator
	}
}

func judgeFaults(p *Plan, r *PhaseRecord, add func(name, status, reason, format string, a ...any)) {
	injected := true
	for _, f := range r.Faults {
		name := "故障 " + f.Agent + "/" + f.Action
		switch {
		case !f.InjectOK:
			injected = false
			add(name, VerdictInconclusive, ReasonInfrastructure, "注入未成功：%s", firstNonEmpty(f.Output, "无输出"))
		case !f.RecoverOK:
			add(name, VerdictInconclusive, ReasonInfrastructure, "恢复命令未成功，须人工确认环境：%s", firstNonEmpty(f.Output, "无输出"))
		default:
			add(name, VerdictPassed, "", "已注入并恢复，持续 %.0f 秒", float64(f.RecoverAt-f.InjectedAt)/1000)
		}
	}
	if len(r.Faults) == 0 || !injected {
		return
	}
	rc := r.Recovery
	limit := p.Faults.MaxRecovery.D().Seconds()
	switch {
	case rc == nil || rc.RecoverySeconds == nil:
		detail := "未观测到恢复"
		if rc != nil {
			detail = rc.Detail
		}
		if rc != nil && rc.BaselinePerSec == nil {
			add("恢复时间", VerdictInconclusive, ReasonObservability, "%s", detail)
		} else {
			add("恢复时间", VerdictFailed, ReasonService, "%s", detail)
		}
	case *rc.RecoverySeconds > limit:
		add("恢复时间", VerdictFailed, ReasonService, "%s；超过上限 %.0f 秒", rc.Detail, limit)
	default:
		add("恢复时间", VerdictPassed, "", "%s；上限 %.0f 秒", rc.Detail, limit)
	}
}

func latencyCheck(add func(name, status, reason, format string, a ...any), name string, h Histogram, p95Limit, p99Limit float64) {
	pc := h.Percentiles()
	if pc.P95MS == nil {
		add(name, VerdictInconclusive, ReasonObservability, "样本 %d 条，不足以计算 P95", pc.N)
		return
	}
	if *pc.P95MS > p95Limit {
		add(name, VerdictFailed, ReasonService, "P95 %.1fms > %.0fms（样本 %d）", *pc.P95MS, p95Limit, pc.N)
		return
	}
	if pc.P99MS != nil && *pc.P99MS > p99Limit {
		add(name, VerdictFailed, ReasonService, "P99 %.1fms > %.0fms（样本 %d）", *pc.P99MS, p99Limit, pc.N)
		return
	}
	p99 := "样本不足 100，未判定 P99"
	if pc.P99MS != nil {
		p99 = fmt.Sprintf("P99 %.1fms", *pc.P99MS)
	}
	add(name, VerdictPassed, "", "P95 %.1fms，%s（样本 %d）", *pc.P95MS, p99, pc.N)
}

func codeSummary(codes map[string]uint64) string {
	keys := sortedKeys(codes)
	sort.SliceStable(keys, func(i, j int) bool { return codes[keys[i]] > codes[keys[j]] })
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, codes[k]))
	}
	return strings.Join(parts, " ")
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}
