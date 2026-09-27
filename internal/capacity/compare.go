package capacity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Comparison lines up finished runs. Capacity values come from each run's
// own verified result; scaling efficiency E(n) is only computed when every
// run used the same workload and a confirmed capacity bound.
type Comparison struct {
	SchemaVersion int          `json:"schemaVersion"`
	Runs          []CompareRun `json:"runs"`
	Comparable    bool         `json:"comparable"`
	Reasons       []string     `json:"reasons"`
	Scaling       []ScalePoint `json:"scaling"`
}

type CompareRun struct {
	RunID          string   `json:"runId"`
	Plan           string   `json:"plan"`
	Preset         string   `json:"preset"`
	Deployment     string   `json:"deployment"`
	Inventory      string   `json:"inventory"`
	SourceCommit   string   `json:"sourceCommit"`
	WorkloadHash   string   `json:"workloadHash"`
	Instances      int      `json:"instances"`
	Verdict        string   `json:"verdict"`
	Classification string   `json:"classification"`
	LowerBound     *float64 `json:"lowerPassedBound"`
	UpperBound     *float64 `json:"upperFailedBound"`
	Recommended    *float64 `json:"recommendedOperatingValue"`
	EvidenceOK     bool     `json:"evidenceComplete"`
}

// ScalePoint is one run on the scaling curve; E = (C(n)/C(n0)) / (n/n0).
type ScalePoint struct {
	RunID      string  `json:"runId"`
	Instances  int     `json:"instances"`
	Capacity   float64 `json:"capacity"`
	Speedup    float64 `json:"speedup"`
	Efficiency float64 `json:"efficiency"`
}

// workloadHash covers what makes two capacity numbers comparable: the
// message mix and sizes, the query mix, business modules and the SLOs.
// Device count, rates, search tuning and budgets may differ.
func workloadHash(p *Plan) string {
	w := map[string]any{
		"suite": p.Suite, "messageBytes": p.Fixtures.MessageBytes, "fields": p.Fixtures.Fields, "alarmFraction": p.Fixtures.AlarmFraction,
		"alarmRule": p.Fixtures.AlarmRuleID != "", "ingressShare": p.Load.IngressShare, "queryMix": p.Load.QueryMix,
		"queryRate": p.Load.QueryRequestsPerSecond, "scaleQueries": p.Load.ScaleQueries, "modules": p.Modules, "slo": p.SLO,
	}
	b, _ := json.Marshal(w)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:6])
}

// Compare reads runs from resultsDir. instances overrides the instance count
// per run; the default is the number of platform processes in the run's
// metrics targets.
func Compare(resultsDir string, runIDs []string, instances map[string]int) (Comparison, error) {
	c := Comparison{SchemaVersion: SchemaVersion, Reasons: []string{}, Scaling: []ScalePoint{}}
	if len(runIDs) < 2 {
		return c, errors.New("compare needs at least two runs")
	}
	for _, id := range runIDs {
		if !runIDPattern.MatchString(id) {
			return c, fmt.Errorf("invalid run id %q", id)
		}
		dir := filepath.Join(resultsDir, id)
		var s Summary
		b, err := os.ReadFile(filepath.Join(dir, "summary.json"))
		if err != nil || json.Unmarshal(b, &s) != nil {
			return c, fmt.Errorf("run %s has no summary.json (run capacity-test report first)", id)
		}
		p, err := LoadPlan(filepath.Join(dir, "plan.sanitized.yaml"))
		if err != nil {
			return c, fmt.Errorf("run %s: %w", id, err)
		}
		var env struct {
			Deployment     string          `json:"deployment"`
			Inventory      string          `json:"inventory"`
			MetricsTargets []MetricsTarget `json:"metricsTargets"`
		}
		if b, err := os.ReadFile(filepath.Join(dir, "environment.json")); err == nil {
			_ = json.Unmarshal(b, &env)
		}
		r := s.Capacity["mixedBusinessMessagesPerSecond"]
		run := CompareRun{RunID: id, Plan: p.Name, Preset: p.Preset, Deployment: env.Deployment, Inventory: env.Inventory, SourceCommit: s.SourceCommit,
			WorkloadHash: workloadHash(p), Instances: len(env.MetricsTargets), Verdict: s.Verdict, Classification: r.Classification,
			LowerBound: r.LowerPassedBound, UpperBound: r.UpperFailedBound, Recommended: r.RecommendedOperatingValue, EvidenceOK: s.EvidenceComplete}
		if n, ok := instances[id]; ok {
			run.Instances = n
		}
		c.Runs = append(c.Runs, run)
	}
	c.Comparable = true
	reject := func(format string, a ...any) {
		c.Comparable = false
		c.Reasons = append(c.Reasons, fmt.Sprintf(format, a...))
	}
	base := c.Runs[0]
	for _, r := range c.Runs {
		if r.WorkloadHash != base.WorkloadHash {
			reject("%s 的负载组合或 SLO 与 %s 不同（workload %s ≠ %s）", r.RunID, base.RunID, r.WorkloadHash, base.WorkloadHash)
		}
		if r.Preset != PresetCapacity {
			reject("%s 是 %s 预设，没有经搜索确认的容量值", r.RunID, r.Preset)
		}
		if r.LowerBound == nil || (r.Classification != ClassBounded && r.Classification != ClassWide && r.Classification != ClassLowerOnly) {
			reject("%s 没有确认的通过档（%s）", r.RunID, firstNonEmpty(r.Classification, "unmeasured"))
		}
		if !r.EvidenceOK {
			reject("%s 的证据不完整", r.RunID)
		}
		if r.Instances < 1 {
			reject("%s 的实例数未知，请用 --instances 指定", r.RunID)
		}
	}
	if !c.Comparable {
		return c, nil
	}
	pts := append([]CompareRun(nil), c.Runs...)
	sort.SliceStable(pts, func(i, j int) bool { return pts[i].Instances < pts[j].Instances })
	n0, c0 := float64(pts[0].Instances), *pts[0].LowerBound
	for _, r := range pts {
		speed := *r.LowerBound / c0
		eff := speed / (float64(r.Instances) / n0)
		c.Scaling = append(c.Scaling, ScalePoint{RunID: r.RunID, Instances: r.Instances, Capacity: *r.LowerBound, Speedup: math.Round(speed*100) / 100, Efficiency: math.Round(eff*100) / 100})
	}
	for _, p := range c.Scaling {
		if p.Efficiency < 0.7 && p.Instances > pts[0].Instances {
			c.Reasons = append(c.Reasons, fmt.Sprintf("%d 实例时扩容效率 %.0f%%，低于 70%%：优先排查共享瓶颈（数据库、分区数、热点设备）", p.Instances, p.Efficiency*100))
		}
	}
	return c, nil
}

// WriteComparison writes compare.json, compare.md and scaling.svg (when
// comparable) to out.
func WriteComparison(out string, c Comparison) error {
	if err := os.MkdirAll(out, 0o750); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	if err := os.WriteFile(filepath.Join(out, "compare.json"), append(b, '\n'), 0o640); err != nil {
		return err
	}
	var md strings.Builder
	md.WriteString("# 容量测试运行比较\n\n| 运行 | 计划 | 预设 | 部署 | 实例数 | 提交 | 负载组合 | 结论 | 最高通过档 | 最低失败档 | 证据完整 |\n| --- | --- | --- | --- | ---: | --- | --- | --- | ---: | ---: | --- |\n")
	for _, r := range c.Runs {
		fmt.Fprintf(&md, "| %s | %s | %s | %s | %d | %s | %s | %s %s | %s | %s | %v |\n", r.RunID, r.Plan, r.Preset, dash(r.Deployment), r.Instances, dash(r.SourceCommit), r.WorkloadHash, r.Verdict, tr(classText, r.Classification), fmtPtr(r.LowerBound, ""), fmtPtr(r.UpperBound, ""), r.EvidenceOK)
	}
	if c.Comparable {
		md.WriteString("\n## 扩容曲线\n\n| 实例数 | 容量（最高通过档，条/秒） | 相对首个 | 扩容效率 E(n) |\n| ---: | ---: | ---: | ---: |\n")
		for _, p := range c.Scaling {
			fmt.Fprintf(&md, "| %d | %s | %.2f× | %.0f%% |\n", p.Instances, fmtNum(p.Capacity), p.Speedup, p.Efficiency*100)
		}
		md.WriteString("\nE(n) = (C(n)/C(n₀)) ÷ (n/n₀)，C 取各运行经复测确认的最高通过档；图见 scaling.svg。\n")
	} else {
		md.WriteString("\n条件不一致，只并列展示，不计算扩容效率。\n")
	}
	if len(c.Reasons) > 0 {
		md.WriteString("\n## 说明\n\n")
		for _, r := range c.Reasons {
			fmt.Fprintf(&md, "- %s\n", r)
		}
	}
	if err := os.WriteFile(filepath.Join(out, "compare.md"), []byte(md.String()), 0o640); err != nil {
		return err
	}
	if !c.Comparable || len(c.Scaling) == 0 {
		_ = os.Remove(filepath.Join(out, "scaling.svg"))
		return nil
	}
	var actual, ideal []xy
	var ticks []chartTick
	for _, p := range c.Scaling {
		x := float64(p.Instances)
		actual = append(actual, xy{x, p.Capacity, true})
		ideal = append(ideal, xy{x, c.Scaling[0].Capacity * x / float64(c.Scaling[0].Instances), true})
		ticks = append(ticks, chartTick{X: x, Label: fmt.Sprintf("n=%d E=%.0f%%", p.Instances, p.Efficiency*100)})
	}
	svg := renderChart(chartSpec{Title: "扩容曲线（最高通过档）", XLabel: "实例数 n（标注扩容效率 E）", YLabel: "条/秒", Ticks: ticks, Markers: true,
		Series: []chartSeries{{Name: "实测容量", Points: actual}, {Name: "线性理想", Points: ideal, Dashed: true}}})
	return os.WriteFile(filepath.Join(out, "scaling.svg"), []byte(svg), 0o640)
}
