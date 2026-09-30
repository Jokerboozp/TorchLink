package httpapi

import (
	"encoding/json"
	"fmt"
	"iot-platform/internal/analytics"
	"iot-platform/internal/analytics/recurring"
	"iot-platform/internal/model"
	"net/http"
	"strings"
	"time"
)

func (s *Server) governanceReportFacts(r *http.Request, report model.GovernanceReport) (string, error) {
	snap, e := s.analysis.Store.GetAnalysisSnapshot(r.Context(), claims(r).TenantID, report.AnalysisSnapshotID)
	if e != nil {
		return "", e
	}
	if _, e = s.analysis.Get(r.Context(), analysisActor(r), analytics.KindRecurring, snap.RunID); e != nil {
		return "", e
	}
	if snap.FactsHash != report.FactsHash {
		return "", model.ErrGovernanceConflict
	}
	var stats recurring.Statistics
	if e = json.Unmarshal(snap.Statistics, &stats); e != nil {
		return "", e
	}
	return renderGovernanceStatistics(stats), nil
}
func renderGovernanceStatistics(stats recurring.Statistics) string {
	var out strings.Builder
	out.WriteString("\n固定版本的确定性观察事实\n")
	for _, m := range stats.Metrics {
		period := m.Period
		switch period {
		case "BEFORE":
			period = "改善前"
		case "AFTER":
			period = "改善后"
		case "WHOLE":
			period = "完整窗口"
		}
		fmt.Fprintf(&out, "\n%s：%s / %s / %s\n窗口（UTC，结束不含）：%s — %s\n合法源身份上报数：%d\n完整现场核实：%d；待核实：%d\n", period, m.Point.DeviceID, m.Point.AlarmType, m.Point.SignalKey, time.UnixMilli(m.Window.Start).UTC().Format(time.RFC3339), time.UnixMilli(m.Window.End).UTC().Format(time.RFC3339), m.ReportCount, m.VerificationCount, m.UnverifiedCount)
		if m.CycleMethod == "REPORT_ONLY" {
			out.WriteString("周期口径：仅上报；可靠新开始、未恢复周期、边界未知数不可计算。\n")
		} else {
			fmt.Fprintf(&out, "可靠新开始数：%d\n未恢复周期数：%d\n边界未知数：%d\n", m.KnownStarts, m.OpenCycles, m.BoundaryUnknown)
		}
		if m.Activity.Hours == nil {
			out.WriteString("有效监测小时：不可计算\n")
		} else {
			fmt.Fprintf(&out, "有效监测小时：%.4f\n", *m.Activity.Hours)
		}
		if m.NewStartsPer1000Hours == nil {
			out.WriteString("每千有效监测小时新开始：不可计算\n")
		} else {
			fmt.Fprintf(&out, "每千有效监测小时新开始：%.4f\n", *m.NewStartsPer1000Hours)
		}
		fmt.Fprintf(&out, "合格实际活动N：%d；已确认相关C：%d；归属待核实U：%d\n", m.Activity.N, m.Activity.C, m.Activity.U)
		if m.Activity.RelatedFraction == nil {
			out.WriteString("已确认活动相关占比：不可计算\n")
		} else {
			fmt.Fprintf(&out, "已确认活动相关占比：%d/%d = %.2f%%\n", m.Activity.C, m.Activity.N, *m.Activity.RelatedFraction*100)
		}
		if len(m.Activity.IdentificationInterval) == 2 {
			fmt.Fprintf(&out, "未核实事件归属识别区间：[%.2f%%, %.2f%%]，不是统计置信区间\n", m.Activity.IdentificationInterval[0]*100, m.Activity.IdentificationInterval[1]*100)
		}
		fmt.Fprintf(&out, "登记覆盖：%s\n限制：%s\n", m.Activity.Coverage, strings.Join(append(append([]string{}, m.Limitations...), m.Activity.Limitations...), "；"))
	}
	if stats.Comparison.Difference == nil {
		out.WriteString("\n前后差值：不可计算\n")
	} else {
		fmt.Fprintf(&out, "\n前后占比差值：%.2f个百分点\n", *stats.Comparison.Difference*100)
	}
	return out.String()
}

// Read the report's precise frozen snapshot; pagination of recent runs cannot
// change which fixed facts the formal report presents.
func (s *Server) governanceReportAnalysis(w http.ResponseWriter, r *http.Request) {
	if !s.governanceReady(w) {
		return
	}
	d, e := s.governance.Get(r.Context(), governanceActor(r), model.GovernanceReportKind, r.PathValue("id"))
	if e != nil {
		governanceProblem(w, e)
		return
	}
	report, e := model.GovernanceBody[model.GovernanceReport](d)
	if e != nil {
		governanceProblem(w, e)
		return
	}
	snap, e := s.analysis.Store.GetAnalysisSnapshot(r.Context(), claims(r).TenantID, report.AnalysisSnapshotID)
	if e != nil {
		governanceProblem(w, e)
		return
	}
	run, e := s.analysis.Get(r.Context(), analysisActor(r), analytics.KindRecurring, snap.RunID)
	if e != nil {
		governanceProblem(w, e)
		return
	}
	if snap.FactsHash != report.FactsHash {
		governanceProblem(w, model.ErrGovernanceConflict)
		return
	}
	write(w, 200, map[string]any{"run": run, "snapshot": snap})
}
