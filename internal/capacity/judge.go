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
	PhaseID                 string                  `json:"phaseId"`
	Index                   int                     `json:"index"`
	Kind                    string                  `json:"kind"`
	TargetMessagesPerSec    float64                 `json:"targetMessagesPerSecond"`
	TargetQueriesPerSec     float64                 `json:"targetQueriesPerSecond"`
	StartedAt               int64                   `json:"startedAt"`
	MeasureFrom             int64                   `json:"measureFrom"`
	MeasureTo               int64                   `json:"measureTo"`
	EndedAt                 int64                   `json:"endedAt"`
	WarmupSeconds           float64                 `json:"warmupSeconds"`
	MeasureSeconds          float64                 `json:"measureSeconds"`
	Streams                 map[string]*StreamStats `json:"streams"`
	Agents                  []AgentPhaseSummary     `json:"agents"`
	Pipeline                Pipeline                `json:"pipeline"`
	Drain                   Drain                   `json:"drain"`
	Integrity               Integrity               `json:"integrity"`
	Verdict                 string                  `json:"verdict"`
	StopReason              string                  `json:"stopReason,omitempty"`
	Checks                  []Check                 `json:"checks"`
	Cancelled               bool                    `json:"cancelled,omitempty"`
	BusinessCompletedPerSec *float64                `json:"businessCompletedPerSecond"`
}

var messageStreams = []string{"http", "mqtt", "tcp"}

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
	if r.Cancelled {
		add("停止", VerdictInconclusive, ReasonCancel, "本档被人工停止，结果不完整")
	}
	for _, a := range r.Agents {
		if a.LeaseExpired || a.Interrupted {
			add("发压 Agent", VerdictInconclusive, ReasonInfrastructure, "Agent %s 未完整执行本档：%s", a.Name, a.Reason)
		}
	}
	// 1. Generator: the planned load must actually have been offered.
	for _, name := range append(append([]string{}, messageStreams...), "query") {
		s := r.Streams[name]
		if s == nil || s.Scheduled == 0 {
			continue
		}
		short := float64(s.NotSent) / float64(s.Scheduled)
		late, _ := s.Lateness.Quantile(0.95)
		switch {
		case short > slo.SendTolerance:
			add("实发达成 "+name, VerdictInconclusive, ReasonGenerator, "计划 %d 条，未发出 %d 条（%.2f%%，容差 %.2f%%）", s.Scheduled, s.NotSent, short*100, slo.SendTolerance*100)
		case late > 1000:
			add("配速准确 "+name, VerdictInconclusive, ReasonGenerator, "调度迟到 P95 %.0fms，超过 1000ms", late)
		default:
			add("实发达成 "+name, VerdictPassed, "", "计划 %d 条，实发 %d 条，调度迟到 P95 %.1fms", s.Scheduled, s.Sent, late)
		}
	}
	// 2. Ingress success of device messages.
	m := r.MessageTotals()
	if m.Sent > 0 {
		ratio := float64(m.OK) / float64(m.Sent)
		if ratio >= slo.SuccessRatio {
			add("入口成功率", VerdictPassed, "", "%.4f ≥ %.4f（%d/%d）", ratio, slo.SuccessRatio, m.OK, m.Sent)
		} else {
			reason := ReasonService
			if m.Codes["429"]*2 > m.Fail {
				reason = ReasonPolicy
			}
			add("入口成功率", VerdictFailed, reason, "%.4f < %.4f；结果码 %s", ratio, slo.SuccessRatio, codeSummary(m.Codes))
		}
	}
	// 3. Management queries.
	if q := r.Streams["query"]; q != nil && q.Sent > 0 {
		ratio := float64(q.OK) / float64(q.Sent)
		if ratio < slo.SuccessRatio {
			add("查询成功率", VerdictFailed, ReasonService, "%.4f < %.4f；结果码 %s", ratio, slo.SuccessRatio, codeSummary(q.Codes))
		} else {
			add("查询成功率", VerdictPassed, "", "%.4f（%d/%d）", ratio, q.OK, q.Sent)
		}
		latencyCheck(add, "查询时延", q.Latency, slo.QueryP95.D().Seconds()*1000, slo.QueryP99.D().Seconds()*1000)
	}
	// 4. Integrity and drain.
	in := r.Integrity
	if in.UniqueSent > 0 {
		switch {
		case in.Unaccounted > uint64(slo.MaximumUnaccountedConfirmedMessage):
			add("数据完整性", VerdictFailed, ReasonIntegrity, "已确认消息缺失/冲突/解析失败 %d 条（允许 %d）；状态 %s", in.Unaccounted, slo.MaximumUnaccountedConfirmedMessage, codeSummary(in.States))
		case in.Pending > 0:
			add("排空", VerdictFailed, ReasonService, "排空时限内仍有 %d 条未完成业务处理", in.Pending)
		default:
			add("数据完整性", VerdictPassed, "", "唯一消息 %d，业务完成 %d，结果未知 %d，入口拒绝 %d", in.UniqueSent, in.UniqueBusinessDone, in.Unknown, in.States[StateRejected])
		}
		// 5. Business completion latency.
		switch {
		case !in.BusinessLatencyValid:
			add("业务完成时延", VerdictInconclusive, ReasonObservability, "无法计算：%s", firstNonEmpty(in.Note, "测量窗口内没有完成的消息"))
		case in.ClockUncertaintyMS > slo.MaxClockUncertainty.D().Seconds()*1000:
			add("业务完成时延", VerdictInconclusive, ReasonObservability, "时钟误差 %.1fms 超过 %.0fms，端到端时延无效", in.ClockUncertaintyMS, slo.MaxClockUncertainty.D().Seconds()*1000)
		default:
			latencyCheck(add, "业务完成时延", in.BusinessLatency, slo.BusinessP95.D().Seconds()*1000, slo.BusinessP99.D().Seconds()*1000)
		}
	} else if in.TCPAckOnly > 0 {
		add("数据完整性", VerdictPassed, "", "仅 TCP 协议 ACK 证据 %d 条；未做原文 ID 核对", in.TCPAckOnly)
	}
	// 6. Pipeline backlog must not keep growing during the window.
	if m.Sent > 0 {
		pl := r.Pipeline
		if pl.BacklogSlopePerSec == nil {
			add("管道积压趋势", VerdictInconclusive, ReasonObservability, "积压指标不足（有效点 %d），无法判断是否持续增长", pl.BacklogPoints)
		} else {
			growth := *pl.BacklogSlopePerSec * r.MeasureSeconds
			limit := math.Max(50, slo.MaxBacklogGrowthRatio*float64(m.Sent))
			if growth > limit {
				add("管道积压趋势", VerdictFailed, ReasonService, "测量窗口内积压约增长 %.0f（斜率 %.2f/s），超过 %.0f", growth, *pl.BacklogSlopePerSec, limit)
			} else {
				add("管道积压趋势", VerdictPassed, "", "窗口内趋势 %.2f/s，估算增长 %.0f（上限 %.0f）", *pl.BacklogSlopePerSec, growth, limit)
			}
		}
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
