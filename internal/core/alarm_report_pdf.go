package core

import (
	"context"
	"fmt"
	"sort"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// MaxReportHighlights bounds the alarms listed one by one in the monthly
// report; the counts above the list cover every alarm.
const MaxReportHighlights = 200

// alarmReportPage draws the monthly report on A4 pages, opening a new page
// when the next block does not fit.
type alarmReportPage struct {
	report model.AlarmMonthlyReport
	pages  []*inspectionPDFCanvas
	c      *inspectionPDFCanvas
	y      float64
}

const alarmReportBottom = 52.0

func (p *alarmReportPage) newPage() {
	p.c = &inspectionPDFCanvas{}
	p.pages = append(p.pages, p.c)
	p.c.fillRect(0, 792, inspectionPDFPageWidth, 50, inspectionPDFNavy)
	p.c.fillRect(0, 792, 6, 50, inspectionPDFRed)
	p.c.mixedText(inspectionPDFLeft, 812, "消防告警月报 "+p.report.Month, 14, inspectionPDFWhite, "F3")
	p.c.rightText(inspectionPDFRight, 813, "生成 "+inspectionTime(p.report.GeneratedAt), 8, inspectionPDFWhite, "F2")
	p.y = 768
}

// need starts a new page unless height fits above the footer.
func (p *alarmReportPage) need(height float64) {
	if p.y-height < alarmReportBottom {
		p.newPage()
	}
}

// section starts a titled block, keeping the title on the page of the
// first content height points of the block.
func (p *alarmReportPage) section(title string, content float64) {
	p.need(content + 26)
	p.y = drawInspectionSectionTitle(p.c, title, p.y) - 4
}

// countsHeight is the height drawAlarmReportCounts needs.
func countsHeight(columns ...[]alarmReportCount) float64 {
	rows := 1
	for _, c := range columns {
		rows = max(rows, len(c))
	}
	return float64(rows)*17 + 30
}

// RenderAlarmMonthlyPDF renders the monthly alarm report.
func RenderAlarmMonthlyPDF(report model.AlarmMonthlyReport) ([]byte, error) {
	p := &alarmReportPage{report: report}
	p.c = &inspectionPDFCanvas{}
	p.pages = []*inspectionPDFCanvas{p.c}
	drawAlarmReportCover(p.c, report)
	p.y = 731

	p.y = drawInspectionSectionTitle(p.c, "月度概览", p.y) - 5
	drawAlarmReportMetrics(p)
	p.section("每日告警", 140)
	drawAlarmReportDays(p)
	levels := orderedCounts(report.Breakdown.ByLevel, []string{"CRITICAL", "HIGH", "MEDIUM", "LOW", "INFO"}, model.AlarmLevelName)
	statuses := orderedCounts(report.Breakdown.ByStatus, []string{"ACTIVE", "ACKED", "RECOVERED", "CLOSED", "SUPPRESSED"}, model.AlarmStatusName)
	p.section("告警等级与处理状态", countsHeight(levels, statuses))
	drawAlarmReportCounts(p, []alarmReportColumn{{"等级", levels}, {"状态", statuses}})
	types := orderedCounts(report.Breakdown.ByType, nil, model.AlarmTypeName)
	if len(types) > 10 {
		types = types[:10]
	}
	results := orderedCounts(report.Stats.ByResult, []string{model.DispositionRealFire, model.DispositionFalseAlarm, model.DispositionTest, model.DispositionMaintenance, model.DispositionFault}, model.DispositionName)
	p.section("告警类型与核实结论", countsHeight(types, results))
	drawAlarmReportCounts(p, []alarmReportColumn{{"类型（前 10）", types}, {"核实结论", results}})
	p.section("重点设备", countsHeight(make([]alarmReportCount, max(len(report.Breakdown.TopDevices), len(report.Stats.TopFalseAlarmDevices)))))
	drawAlarmReportDevices(p)
	p.section("真实火警与待核实火警", 60)
	drawAlarmReportHighlights(p)

	for index, page := range p.pages {
		page.line(inspectionPDFLeft, 37, inspectionPDFRight, 37, inspectionPDFLine, 0.7)
		page.mixedText(inspectionPDFLeft, inspectionPDFFooterY, "iot-platform | 消防告警月报", 7.6, inspectionPDFMuted, "F2")
		page.rightText(inspectionPDFRight, inspectionPDFFooterY, fmt.Sprintf("第 %d / %d 页", index+1, len(p.pages)), 7.6, inspectionPDFMuted, "F2")
	}
	return renderCanvasPDF(p.pages, "Alarm Monthly Report")
}

func drawAlarmReportCover(c *inspectionPDFCanvas, r model.AlarmMonthlyReport) {
	c.fillRect(0, 760, inspectionPDFPageWidth, 82, inspectionPDFNavy)
	c.fillRect(0, 760, 7, 82, inspectionPDFRed)
	c.mixedText(inspectionPDFLeft, 807, "消防告警月报", 22, inspectionPDFWhite, "F3")
	period := time.UnixMilli(r.Start).In(model.ReportZone).Format("2006-01-02") + " 至 " + time.UnixMilli(r.End-1).In(model.ReportZone).Format("2006-01-02")
	c.mixedText(inspectionPDFLeft, 784, "统计期间 "+period+"（按告警最后发生时间，北京时间）", 9.5, inspectionPDFWhite, "F2")
	c.rightText(inspectionPDFRight, 807, "租户 "+inspectionPDFFallback(r.TenantID, "默认租户"), 8.5, inspectionPDFWhite, "F2")
	c.rightText(inspectionPDFRight, 784, "生成 "+inspectionTime(r.GeneratedAt), 8.5, inspectionPDFWhite, "F2")
}

func reportDuration(ms int64, count int64) string {
	if count == 0 {
		return "—"
	}
	d := time.Duration(ms) * time.Millisecond
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d 秒", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%.1f 分钟", d.Minutes())
	default:
		return fmt.Sprintf("%.1f 小时", d.Hours())
	}
}

func drawAlarmReportMetrics(p *alarmReportPage) {
	s := p.report.Stats
	rate := "—"
	if s.Verified > 0 {
		rate = fmt.Sprintf("%.1f%%", s.FalseAlarmRate*100)
	}
	metrics := []struct {
		label, value, note string
		color              inspectionPDFColor
	}{
		{"告警总数", fmt.Sprint(s.Total), fmt.Sprintf("须核实 %d 条", s.RequiringVerification), inspectionPDFBlue},
		{"真实火警", fmt.Sprint(s.ByResult[model.DispositionRealFire]), fmt.Sprintf("已核实 %d 条", s.Verified), inspectionPDFRed},
		{"待核实火警", fmt.Sprint(s.Unverified), "火灾类或紧急告警", inspectionPDFAmber},
		{"误报率", rate, "误报 / 已核实", inspectionPDFPurple},
		{"平均确认用时", reportDuration(s.Acknowledge.AvgMs, s.Acknowledge.Count), "90% 在 " + reportDuration(s.Acknowledge.P90Ms, s.Acknowledge.Count) + " 内", inspectionPDFTeal},
		{"平均核实用时", reportDuration(s.Verify.AvgMs, s.Verify.Count), "90% 在 " + reportDuration(s.Verify.P90Ms, s.Verify.Count) + " 内", inspectionPDFGreen},
	}
	width, height, gap := (inspectionPDFRight-inspectionPDFLeft-2*10)/3, 58.0, 10.0
	for i, m := range metrics {
		x := inspectionPDFLeft + float64(i%3)*(width+gap)
		top := p.y - float64(i/3)*(height+gap)
		p.c.fillRect(x, top-height, width, height, inspectionPDFSurface)
		p.c.fillRect(x, top-height, 3, height, m.color)
		p.c.mixedText(x+12, top-16, m.label, 8.5, inspectionPDFMuted, "F2")
		p.c.mixedText(x+12, top-36, m.value, 16, m.color, "F3")
		p.c.mixedText(x+12, top-50, m.note, 7.5, inspectionPDFMuted, "F2")
	}
	p.y -= 2*height + gap + 22
}

func drawAlarmReportDays(p *alarmReportPage) {
	start := time.UnixMilli(p.report.Start).In(model.ReportZone)
	end := time.UnixMilli(p.report.End).In(model.ReportZone)
	counts := map[string]int64{}
	for _, d := range p.report.Breakdown.ByDay {
		counts[d.Day] = d.Count
	}
	var days []string
	for d := start; d.Before(end) && len(days) < 62; d = d.AddDate(0, 0, 1) {
		days = append(days, d.Format(time.DateOnly))
	}
	var peak int64
	for _, d := range days {
		peak = max(peak, counts[d])
	}
	const height = 110.0
	p.need(height + 30)
	top := p.y
	base := top - height
	p.c.line(inspectionPDFLeft, base, inspectionPDFRight, base, inspectionPDFLine, 0.8)
	if peak == 0 || len(days) == 0 {
		p.c.mixedText(inspectionPDFLeft+8, base+height/2, "本月没有告警。", 9, inspectionPDFMuted, "F2")
		p.y = base - 24
		return
	}
	p.c.mixedText(inspectionPDFLeft, top+2, fmt.Sprintf("单日最多 %d 条", peak), 7.5, inspectionPDFMuted, "F2")
	slot := (inspectionPDFRight - inspectionPDFLeft) / float64(len(days))
	for i, d := range days {
		x := inspectionPDFLeft + float64(i)*slot
		if n := counts[d]; n > 0 {
			h := (height - 14) * float64(n) / float64(peak)
			p.c.fillRect(x+slot*0.15, base, slot*0.7, h, inspectionPDFRed)
		}
		if i%5 == 0 || i == len(days)-1 {
			p.c.mixedText(x, base-11, d[8:], 7, inspectionPDFMuted, "F2")
		}
	}
	p.y = base - 30
}

type alarmReportCount struct {
	label string
	count int64
}

type alarmReportColumn struct {
	title string
	rows  []alarmReportCount
}

// orderedCounts lists the known codes in order first, then the others by
// count; codes are shown by their Chinese names.
func orderedCounts(counts map[string]int64, order []string, name func(string) string) []alarmReportCount {
	out := []alarmReportCount{}
	seen := map[string]bool{}
	for _, code := range order {
		seen[code] = true
		if counts[code] > 0 {
			out = append(out, alarmReportCount{name(code), counts[code]})
		}
	}
	var rest []string
	for code, n := range counts {
		if !seen[code] && n > 0 {
			rest = append(rest, code)
		}
	}
	sort.Slice(rest, func(i, j int) bool {
		if counts[rest[i]] != counts[rest[j]] {
			return counts[rest[i]] > counts[rest[j]]
		}
		return rest[i] < rest[j]
	})
	for _, code := range rest {
		out = append(out, alarmReportCount{name(inspectionPDFFallback(code, "未设置")), counts[code]})
	}
	return out
}

// drawAlarmReportCounts draws side-by-side tables of counts with bars.
func drawAlarmReportCounts(p *alarmReportPage, columns []alarmReportColumn) {
	rows := 1
	for _, col := range columns {
		rows = max(rows, len(col.rows))
	}
	const line = 17.0
	p.need(float64(rows)*line + 30)
	width := (inspectionPDFRight - inspectionPDFLeft - 20) / float64(len(columns))
	for i, col := range columns {
		x := inspectionPDFLeft + float64(i)*(width+20)
		p.c.mixedText(x, p.y, col.title, 9, inspectionPDFNavy, "F3")
		var peak int64
		for _, r := range col.rows {
			peak = max(peak, r.count)
		}
		if len(col.rows) == 0 {
			p.c.mixedText(x, p.y-line, "无", 8.5, inspectionPDFMuted, "F2")
		}
		for j, r := range col.rows {
			y := p.y - float64(j+1)*line
			p.c.mixedText(x, y, inspectionPDFShorten(r.label, 12), 8.5, inspectionPDFText, "F2")
			barX, barWidth := x+96, width-140
			p.c.fillRect(barX, y-1, barWidth, 7, inspectionPDFSurface)
			p.c.fillRect(barX, y-1, barWidth*float64(r.count)/float64(max(peak, 1)), 7, inspectionPDFBlue)
			p.c.rightText(x+width, y, fmt.Sprint(r.count), 8.5, inspectionPDFText, "F2")
		}
	}
	p.y -= float64(rows)*line + 26
}

func drawAlarmReportDevices(p *alarmReportPage) {
	name := func(d model.AlarmDeviceCount) string { return inspectionPDFFallback(d.DeviceName, d.DeviceID) }
	most := []alarmReportCount{}
	for _, d := range p.report.Breakdown.TopDevices {
		most = append(most, alarmReportCount{name(d), d.Alarms})
	}
	falseAlarms := []alarmReportCount{}
	for _, d := range p.report.Stats.TopFalseAlarmDevices {
		falseAlarms = append(falseAlarms, alarmReportCount{name(d), d.FalseAlarms})
	}
	drawAlarmReportCounts(p, []alarmReportColumn{{"告警最多（前 10）", most}, {"误报最多（前 10）", falseAlarms}})
}

func drawAlarmReportHighlights(p *alarmReportPage) {
	if len(p.report.Highlights) == 0 {
		p.need(24)
		p.c.mixedText(inspectionPDFLeft, p.y, "本月没有真实火警，也没有待核实的火灾类或紧急告警。", 9, inspectionPDFMuted, "F2")
		p.y -= 24
		return
	}
	columns := []struct {
		title string
		x     float64
	}{{"最后发生", inspectionPDFLeft}, {"设备", inspectionPDFLeft + 82}, {"类型 / 等级", inspectionPDFLeft + 210}, {"状态", inspectionPDFLeft + 300}, {"核实结论 / 内容", inspectionPDFLeft + 350}}
	header := func() {
		p.c.fillRect(inspectionPDFLeft, p.y-5, inspectionPDFRight-inspectionPDFLeft, 18, inspectionPDFSurface)
		for _, col := range columns {
			p.c.mixedText(col.x+4, p.y, col.title, 8, inspectionPDFMuted, "F3")
		}
		p.y -= 20
	}
	p.need(60)
	header()
	for _, a := range p.report.Highlights {
		note := "待核实"
		color := inspectionPDFAmber
		if a.Disposition != nil {
			note, color = model.DispositionName(a.Disposition.Result), inspectionPDFRed
		}
		if a.Content != "" {
			note += "：" + a.Content
		}
		lines := inspectionPDFWrapText(note, 8, inspectionPDFRight-columns[4].x-8)
		if len(lines) > 3 {
			last := []rune(lines[2])
			lines = append(lines[:2], string(last[:max(len(last)-1, 0)])+"…")
		}
		height := max(16, float64(len(lines))*11+5)
		if p.y-height < alarmReportBottom {
			p.newPage()
			header()
		}
		p.c.mixedText(columns[0].x+4, p.y, time.UnixMilli(a.LastTriggeredAt).In(model.ReportZone).Format("01-02 15:04"), 8, inspectionPDFText, "F2")
		p.c.mixedText(columns[1].x+4, p.y, inspectionPDFShorten(inspectionPDFFallback(a.DeviceName, a.DeviceID), 11), 8, inspectionPDFText, "F2")
		p.c.mixedText(columns[2].x+4, p.y, inspectionPDFShorten(model.AlarmTypeName(a.AlarmType), 6)+" / "+model.AlarmLevelName(a.AlarmLevel), 8, inspectionPDFText, "F2")
		p.c.mixedText(columns[3].x+4, p.y, model.AlarmStatusName(a.Status), 8, inspectionPDFText, "F2")
		drawInspectionWrapped(p.c, columns[4].x+4, p.y, lines, 8, 11, color, "F2")
		p.y -= height
		p.c.line(inspectionPDFLeft, p.y+8, inspectionPDFRight, p.y+8, inspectionPDFLine, 0.4)
	}
	if p.report.Omitted > 0 {
		p.need(20)
		p.c.mixedText(inspectionPDFLeft, p.y-4, fmt.Sprintf("另有 %d 条未逐条列出，可在告警中心按期间导出完整 CSV。", p.report.Omitted), 8.5, inspectionPDFMuted, "F2")
		p.y -= 24
	}
}

// keepHighlight reports alarms the monthly report lists one by one.
func keepHighlight(a model.Alarm) bool {
	if a.Disposition != nil {
		return a.Disposition.Result == model.DispositionRealFire
	}
	return a.RequiresVerification()
}

// AlarmMonthlyReport collects the report of the month starting at start
// (in model.ReportZone) through the repository, so the request's device
// scope applies.
func (e *Engine) AlarmMonthlyReport(ctx context.Context, tenant string, month time.Time) (model.AlarmMonthlyReport, error) {
	start := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, model.ReportZone)
	end := start.AddDate(0, 1, 0)
	report := model.AlarmMonthlyReport{TenantID: tenant, Month: start.Format("2006-01"), Start: start.UnixMilli(), End: end.UnixMilli(), GeneratedAt: e.Clock.Now().UnixMilli(), Highlights: []model.Alarm{}}
	// The filter's end is inclusive.
	filter := ports.AlarmFilter{TenantID: tenant, Start: report.Start, End: report.End - 1, Summary: true}
	var err error
	if report.Stats, err = e.Repo.AlarmDispositionStats(ctx, filter); err != nil {
		return report, err
	}
	if report.Breakdown, err = e.Repo.AlarmBreakdown(ctx, filter); err != nil {
		return report, err
	}
	err = e.Repo.EachAlarm(ctx, filter, func(a model.Alarm) error {
		if !keepHighlight(a) {
			return nil
		}
		if len(report.Highlights) >= MaxReportHighlights {
			report.Omitted++
			return nil
		}
		report.Highlights = append(report.Highlights, a)
		return nil
	})
	return report, err
}
