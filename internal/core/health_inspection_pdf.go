package core

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"time"

	"iot-platform/internal/model"
)

const (
	inspectionPDFPageWidth  = 595.0
	inspectionPDFPageHeight = 842.0
	inspectionPDFLeft       = 42.0
	inspectionPDFRight      = 553.0
	inspectionPDFFooterY    = 22.0
)

type inspectionPDFColor struct {
	r float64
	g float64
	b float64
}

var (
	inspectionPDFNavy      = inspectionPDFColor{r: 0.08, g: 0.20, b: 0.33}
	inspectionPDFTeal      = inspectionPDFColor{r: 0.07, g: 0.48, b: 0.52}
	inspectionPDFBlue      = inspectionPDFColor{r: 0.10, g: 0.36, b: 0.58}
	inspectionPDFGreen     = inspectionPDFColor{r: 0.13, g: 0.49, b: 0.34}
	inspectionPDFAmber     = inspectionPDFColor{r: 0.78, g: 0.47, b: 0.08}
	inspectionPDFRed       = inspectionPDFColor{r: 0.71, g: 0.20, b: 0.18}
	inspectionPDFPurple    = inspectionPDFColor{r: 0.38, g: 0.28, b: 0.63}
	inspectionPDFText      = inspectionPDFColor{r: 0.12, g: 0.16, b: 0.21}
	inspectionPDFMuted     = inspectionPDFColor{r: 0.38, g: 0.43, b: 0.49}
	inspectionPDFLine      = inspectionPDFColor{r: 0.85, g: 0.88, b: 0.91}
	inspectionPDFSurface   = inspectionPDFColor{r: 0.97, g: 0.98, b: 0.99}
	inspectionPDFBlueTint  = inspectionPDFColor{r: 0.93, g: 0.97, b: 0.99}
	inspectionPDFTealTint  = inspectionPDFColor{r: 0.92, g: 0.98, b: 0.97}
	inspectionPDFAmberTint = inspectionPDFColor{r: 1.00, g: 0.97, b: 0.90}
	inspectionPDFRedTint   = inspectionPDFColor{r: 1.00, g: 0.94, b: 0.94}
	inspectionPDFWhite     = inspectionPDFColor{r: 1.00, g: 1.00, b: 1.00}
)

// RenderHealthInspectionPDF renders the verified inspection snapshot as a
// standard A4 report. The report uses the built-in STSong-Light CJK font for
// Chinese text and Helvetica for Latin text, keeping the PDF selectable and
// avoiding the spacing defects of drawing every glyph with one CJK font.
func RenderHealthInspectionPDF(report model.DeviceHealthReport) ([]byte, error) {
	pages := inspectionPDFPages(report)
	if len(pages) == 0 {
		pages = append(pages, &inspectionPDFCanvas{})
	}
	for index := range pages {
		pages[index].footer(index+1, len(pages))
	}

	doc := &pdfDocument{}
	fontCID := doc.add(`<< /Type /Font /Subtype /CIDFontType0 /BaseFont /STSong-Light /CIDSystemInfo << /Registry (Adobe) /Ordering (GB1) /Supplement 4 >> /DW 1000 >>`)
	fontCJK := doc.add(fmt.Sprintf(`<< /Type /Font /Subtype /Type0 /BaseFont /STSong-Light /Encoding /UniGB-UCS2-H /DescendantFonts [%d 0 R] >>`, fontCID))
	fontLatin := doc.add(`<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>`)
	fontLatinBold := doc.add(`<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold /Encoding /WinAnsiEncoding >>`)
	pageTree := doc.add("")
	catalog := doc.add("")
	info := doc.add(`<< /Title (Health Inspection Report) /Author (iot-platform) /Producer (iot-platform) >>`)

	pageIDs := make([]int, 0, len(pages))
	for _, page := range pages {
		content := page.body.String()
		contentID := doc.add(fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content))
		pageID := doc.add(fmt.Sprintf("<< /Type /Page /Parent %d 0 R /MediaBox [0 0 %.0f %.0f] /Resources << /Font << /F1 %d 0 R /F2 %d 0 R /F3 %d 0 R >> >> /Contents %d 0 R >>", pageTree, inspectionPDFPageWidth, inspectionPDFPageHeight, fontCJK, fontLatin, fontLatinBold, contentID))
		pageIDs = append(pageIDs, pageID)
	}

	doc.set(pageTree, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", pdfReferences(pageIDs), len(pageIDs)))
	doc.set(catalog, fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pageTree))
	return doc.write(catalog, info)
}

type inspectionPDFCanvas struct {
	body strings.Builder
}

func (c *inspectionPDFCanvas) fillRect(x, y, width, height float64, color inspectionPDFColor) {
	c.fill(color)
	fmt.Fprintf(&c.body, "%.2f %.2f %.2f %.2f re f\n", x, y, width, height)
}

func (c *inspectionPDFCanvas) strokeRect(x, y, width, height float64, color inspectionPDFColor, lineWidth float64) {
	c.stroke(color, lineWidth)
	fmt.Fprintf(&c.body, "%.2f %.2f %.2f %.2f re S\n", x, y, width, height)
}

func (c *inspectionPDFCanvas) line(x1, y1, x2, y2 float64, color inspectionPDFColor, lineWidth float64) {
	c.stroke(color, lineWidth)
	fmt.Fprintf(&c.body, "%.2f %.2f m %.2f %.2f l S\n", x1, y1, x2, y2)
}

func (c *inspectionPDFCanvas) fill(color inspectionPDFColor) {
	fmt.Fprintf(&c.body, "%.3f %.3f %.3f rg\n", color.r, color.g, color.b)
}

func (c *inspectionPDFCanvas) stroke(color inspectionPDFColor, lineWidth float64) {
	fmt.Fprintf(&c.body, "%.3f %.3f %.3f RG %.2f w\n", color.r, color.g, color.b, lineWidth)
}

func (c *inspectionPDFCanvas) mixedText(x, y float64, value string, size float64, color inspectionPDFColor, latinFont string) {
	value = strings.ReplaceAll(strings.ReplaceAll(value, "\r", " "), "\n", " ")
	if strings.TrimSpace(value) == "" {
		return
	}
	c.fill(color)
	fmt.Fprintf(&c.body, "BT\n1 0 0 1 %.2f %.2f Tm\n", x, y)
	runes := []rune(value)
	start := 0
	for start < len(runes) {
		ascii := inspectionPDFIsLatin(runes[start])
		end := start + 1
		for end < len(runes) && inspectionPDFIsLatin(runes[end]) == ascii {
			end++
		}
		if ascii {
			fmt.Fprintf(&c.body, "/%s %g Tf (%s) Tj\n", latinFont, size, inspectionPDFTextLiteral(string(runes[start:end])))
		} else {
			fmt.Fprintf(&c.body, "/F1 %g Tf <%s> Tj\n", size, inspectionPDFTextHex(string(runes[start:end])))
		}
		start = end
	}
	c.body.WriteString("ET\n")
}

func (c *inspectionPDFCanvas) rightText(right, y float64, value string, size float64, color inspectionPDFColor, latinFont string) {
	c.mixedText(right-inspectionPDFTextWidth(value, size), y, value, size, color, latinFont)
}

func (c *inspectionPDFCanvas) footer(page, total int) {
	c.line(inspectionPDFLeft, 37, inspectionPDFRight, 37, inspectionPDFLine, 0.7)
	c.mixedText(inspectionPDFLeft, inspectionPDFFooterY, "iot-platform | 智能巡检报告", 7.6, inspectionPDFMuted, "F2")
	c.rightText(inspectionPDFRight, inspectionPDFFooterY, fmt.Sprintf("第 %d / %d 页", page, total), 7.6, inspectionPDFMuted, "F2")
}

func inspectionPDFPages(report model.DeviceHealthReport) []*inspectionPDFCanvas {
	pages := make([]*inspectionPDFCanvas, 0, 2)
	overview := &inspectionPDFCanvas{}
	drawInspectionOverview(overview, report)
	pages = append(pages, overview)
	if len(report.Items) == 0 {
		return pages
	}

	current := inspectionPDFNewDetailPage(report)
	y := inspectionPDFDetailTableTop - 28
	for _, item := range report.Items {
		rowHeight := inspectionPDFDeviceRowHeight(item)
		if y-rowHeight < 52 {
			pages = append(pages, current)
			current = inspectionPDFNewDetailPage(report)
			y = inspectionPDFDetailTableTop - 28
		}
		drawInspectionDeviceRow(current, item, y, rowHeight)
		y -= rowHeight + 6
	}
	pages = append(pages, current)
	return pages
}

func drawInspectionOverview(c *inspectionPDFCanvas, report model.DeviceHealthReport) {
	drawInspectionHeader(c, report, false)
	y := drawInspectionSectionTitle(c, "巡检概览", 731)
	y -= 5
	y = drawInspectionSummary(c, report, y)
	y -= 16
	y = drawInspectionMetrics(c, report, y)
	y -= 19
	y = drawInspectionStatusOverview(c, report, y)
	y -= 17
	y = drawInspectionAdvice(c, report, y)
	if len(report.Warnings) > 0 {
		y -= 17
		drawInspectionWarnings(c, report.Warnings, y)
	}
}

func drawInspectionHeader(c *inspectionPDFCanvas, report model.DeviceHealthReport, compact bool) {
	if compact {
		c.fillRect(0, 792, inspectionPDFPageWidth, 50, inspectionPDFNavy)
		c.fillRect(0, 792, 6, 50, inspectionPDFTeal)
		c.mixedText(inspectionPDFLeft, 812, "设备智能巡检报告", 14, inspectionPDFWhite, "F3")
		c.rightText(inspectionPDFRight, 813, "生成 "+inspectionTime(report.GeneratedAt), 8, inspectionPDFWhite, "F2")
		return
	}

	c.fillRect(0, 760, inspectionPDFPageWidth, 82, inspectionPDFNavy)
	c.fillRect(0, 760, 7, 82, inspectionPDFTeal)
	c.mixedText(inspectionPDFLeft, 807, "设备智能巡检报告", 22, inspectionPDFWhite, "F3")
	c.mixedText(inspectionPDFLeft, 784, "消防物联网设备健康与运行状态综合评估", 9.5, inspectionPDFWhite, "F2")
	tenant := strings.TrimSpace(report.TenantID)
	if tenant == "" {
		tenant = "默认租户"
	}
	c.rightText(inspectionPDFRight, 807, "租户 "+tenant, 8.5, inspectionPDFWhite, "F2")
	c.rightText(inspectionPDFRight, 784, "生成 "+inspectionTime(report.GeneratedAt), 8.5, inspectionPDFWhite, "F2")
}

func drawInspectionSectionTitle(c *inspectionPDFCanvas, title string, y float64) float64 {
	c.mixedText(inspectionPDFLeft, y, title, 12.5, inspectionPDFNavy, "F3")
	lineStart := inspectionPDFLeft + inspectionPDFTextWidth(title, 12.5) + 12
	if lineStart < inspectionPDFRight {
		c.line(lineStart, y+3, inspectionPDFRight, y+3, inspectionPDFLine, 0.8)
	}
	return y - 20
}

func drawInspectionSummary(c *inspectionPDFCanvas, report model.DeviceHealthReport, top float64) float64 {
	text := strings.TrimSpace(report.Summary)
	if text == "" {
		text = "暂无总体结论。"
	}
	lines := inspectionPDFWrapText(text, 9.6, inspectionPDFRight-inspectionPDFLeft-32)
	height := 29 + float64(len(lines))*12.5
	if height < 50 {
		height = 50
	}
	bottom := top - height
	c.fillRect(inspectionPDFLeft, bottom, inspectionPDFRight-inspectionPDFLeft, height, inspectionPDFBlueTint)
	c.fillRect(inspectionPDFLeft, bottom, 4, height, inspectionPDFBlue)
	c.mixedText(inspectionPDFLeft+15, top-17, "总体结论", 9, inspectionPDFBlue, "F3")
	drawInspectionWrapped(c, inspectionPDFLeft+15, top-33, lines, 9.6, 12.5, inspectionPDFText, "F2")
	return bottom
}

type inspectionPDFMetric struct {
	label string
	value int
	color inspectionPDFColor
}

func drawInspectionMetrics(c *inspectionPDFCanvas, report model.DeviceHealthReport, top float64) float64 {
	metrics := []inspectionPDFMetric{
		{label: "设备总数", value: report.Counts["total"], color: inspectionPDFBlue},
		{label: "状态正常", value: report.Counts["healthy"], color: inspectionPDFGreen},
		{label: "需关注", value: report.Counts["attention"], color: inspectionPDFAmber},
		{label: "高风险", value: report.Counts["critical"], color: inspectionPDFRed},
		{label: "离线 / 疑似离线", value: report.Counts["offline"], color: inspectionPDFPurple},
		{label: "活动告警", value: report.Counts["activeAlarms"], color: inspectionPDFRed},
	}
	gap := 10.0
	cardWidth := (inspectionPDFRight - inspectionPDFLeft - gap*2) / 3
	cardHeight := 52.0
	rowGap := 9.0
	for index, metric := range metrics {
		row := index / 3
		column := index % 3
		x := inspectionPDFLeft + float64(column)*(cardWidth+gap)
		y := top - float64(row)*(cardHeight+rowGap) - cardHeight
		c.fillRect(x, y, cardWidth, cardHeight, inspectionPDFSurface)
		c.fillRect(x, y, 3, cardHeight, metric.color)
		c.strokeRect(x, y, cardWidth, cardHeight, inspectionPDFLine, 0.7)
		c.mixedText(x+13, y+35, metric.label, 8.8, inspectionPDFMuted, "F2")
		c.mixedText(x+13, y+14, strconv.Itoa(metric.value), 21, metric.color, "F3")
	}
	return top - cardHeight*2 - rowGap
}

func drawInspectionStatusOverview(c *inspectionPDFCanvas, report model.DeviceHealthReport, top float64) float64 {
	y := drawInspectionSectionTitle(c, "状态概览", top)
	barTop := y - 2
	barHeight := 13.0
	total := report.Counts["total"]
	healthy := report.Counts["healthy"]
	if total < 0 {
		total = 0
	}
	if healthy < 0 {
		healthy = 0
	}
	if healthy > total {
		healthy = total
	}
	attention := total - healthy
	width := inspectionPDFRight - inspectionPDFLeft
	c.fillRect(inspectionPDFLeft, barTop-barHeight, width, barHeight, inspectionPDFLine)
	if total > 0 {
		c.fillRect(inspectionPDFLeft, barTop-barHeight, width*float64(healthy)/float64(total), barHeight, inspectionPDFGreen)
		c.fillRect(inspectionPDFLeft+width*float64(healthy)/float64(total), barTop-barHeight, width*float64(attention)/float64(total), barHeight, inspectionPDFAmber)
	}
	legendY := barTop - 29
	c.fillRect(inspectionPDFLeft, legendY-2, 8, 8, inspectionPDFGreen)
	c.mixedText(inspectionPDFLeft+14, legendY, fmt.Sprintf("状态正常 %d", healthy), 8.6, inspectionPDFText, "F2")
	secondX := inspectionPDFLeft + 165
	c.fillRect(secondX, legendY-2, 8, 8, inspectionPDFAmber)
	c.mixedText(secondX+14, legendY, fmt.Sprintf("需关注 %d", attention), 8.6, inspectionPDFText, "F2")
	c.mixedText(inspectionPDFLeft, legendY-18, fmt.Sprintf("其中高风险 %d，离线 / 疑似离线 %d，活动告警 %d。", report.Counts["critical"], report.Counts["offline"], report.Counts["activeAlarms"]), 8, inspectionPDFMuted, "F2")
	return legendY - 29
}

func drawInspectionAdvice(c *inspectionPDFCanvas, report model.DeviceHealthReport, top float64) float64 {
	y := drawInspectionSectionTitle(c, "智能分析建议", top)
	text := strings.TrimSpace(report.AIAdvice)
	if text == "" {
		text = "本次未生成 AI 建议，请依据设备状态和告警信息完成现场复核。"
	}
	lines := inspectionPDFWrapText(text, 8.9, inspectionPDFRight-inspectionPDFLeft-32)
	height := 29 + float64(len(lines))*12
	if height < 52 {
		height = 52
	}
	bottom := y - height
	c.fillRect(inspectionPDFLeft, bottom, inspectionPDFRight-inspectionPDFLeft, height, inspectionPDFTealTint)
	c.fillRect(inspectionPDFLeft, bottom, 4, height, inspectionPDFTeal)
	drawInspectionWrapped(c, inspectionPDFLeft+15, y-17, []string{"辅助判断，不替代现场处置"}, 8.6, 12, inspectionPDFTeal, "F3")
	drawInspectionWrapped(c, inspectionPDFLeft+15, y-32, lines, 8.9, 12, inspectionPDFText, "F2")
	return bottom
}

func drawInspectionWarnings(c *inspectionPDFCanvas, warnings []string, top float64) float64 {
	y := drawInspectionSectionTitle(c, "注意事项", top)
	lines := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		warning = strings.TrimSpace(warning)
		if warning == "" {
			continue
		}
		lines = append(lines, "- "+warning)
	}
	if len(lines) == 0 {
		return y
	}
	wrapped := make([]string, 0, len(lines))
	for _, line := range lines {
		wrapped = append(wrapped, inspectionPDFWrapText(line, 8.7, inspectionPDFRight-inspectionPDFLeft-32)...)
	}
	height := 20 + float64(len(wrapped))*11.5
	bottom := y - height
	c.fillRect(inspectionPDFLeft, bottom, inspectionPDFRight-inspectionPDFLeft, height, inspectionPDFAmberTint)
	c.fillRect(inspectionPDFLeft, bottom, 4, height, inspectionPDFAmber)
	drawInspectionWrapped(c, inspectionPDFLeft+15, y-16, wrapped, 8.7, 11.5, inspectionPDFText, "F2")
	return bottom
}

const inspectionPDFDetailTableTop = 746.0

func inspectionPDFNewDetailPage(report model.DeviceHealthReport) *inspectionPDFCanvas {
	page := &inspectionPDFCanvas{}
	drawInspectionHeader(page, report, true)
	drawInspectionSectionTitle(page, "设备巡检明细", 775)
	drawInspectionTableHeader(page, inspectionPDFDetailTableTop)
	return page
}

func drawInspectionTableHeader(c *inspectionPDFCanvas, top float64) {
	columns := []struct {
		x     float64
		width float64
		title string
	}{
		{inspectionPDFLeft, 155, "设备 / 产品 / ID"},
		{197, 88, "业务状态"},
		{285, 77, "数据质量"},
		{362, 100, "最近上报"},
		{462, 91, "风险 / 告警"},
	}
	height := 28.0
	c.fillRect(inspectionPDFLeft, top-height, inspectionPDFRight-inspectionPDFLeft, height, inspectionPDFNavy)
	for _, column := range columns {
		c.mixedText(column.x+8, top-18, column.title, 8.2, inspectionPDFWhite, "F2")
	}
}

func inspectionPDFDeviceRowHeight(item model.DeviceHealthItem) float64 {
	findings := inspectionPDFFindings(item)
	lineCount := 0
	for _, finding := range findings {
		lineCount += len(inspectionPDFWrapText("- "+finding, 8.2, inspectionPDFRight-inspectionPDFLeft-16))
	}
	if lineCount == 0 {
		lineCount = 1
	}
	return 62 + float64(lineCount)*10.5
}

func drawInspectionDeviceRow(c *inspectionPDFCanvas, item model.DeviceHealthItem, top, height float64) {
	bottom := top - height
	if int(top/2)%2 == 0 {
		c.fillRect(inspectionPDFLeft, bottom, inspectionPDFRight-inspectionPDFLeft, height, inspectionPDFSurface)
	} else {
		c.fillRect(inspectionPDFLeft, bottom, inspectionPDFRight-inspectionPDFLeft, height, inspectionPDFWhite)
	}
	c.strokeRect(inspectionPDFLeft, bottom, inspectionPDFRight-inspectionPDFLeft, height, inspectionPDFLine, 0.6)
	for _, x := range []float64{197, 285, 362, 462} {
		c.line(x, bottom, x, top, inspectionPDFLine, 0.5)
	}

	name := strings.TrimSpace(item.DeviceName)
	if name == "" {
		name = item.DeviceID
	}
	c.mixedText(50, top-15, inspectionPDFShorten(name, 22), 9.2, inspectionPDFText, "F2")
	c.mixedText(50, top-28, "产品 "+inspectionPDFShorten(item.ProductID, 20), 7.8, inspectionPDFMuted, "F2")
	c.mixedText(50, top-40, "ID "+inspectionPDFShorten(item.DeviceID, 24), 7.8, inspectionPDFMuted, "F2")

	drawInspectionBadge(c, 205, top-11, inspectionBusinessStatus(item.BusinessStatus), inspectionBusinessColor(item.BusinessStatus), 80)
	drawInspectionBadge(c, 293, top-11, inspectionDataStatus(item.DataStatus), inspectionDataColor(item.DataStatus), 69)
	c.mixedText(370, top-18, inspectionTime(item.LastSeenAt), 8, inspectionPDFText, "F2")

	severity, severityFill := inspectionSeverityStyle(item.Severity)
	drawInspectionBadge(c, 470, top-11, inspectionSeverity(item.Severity), severityFill, 75)
	c.mixedText(470, top-32, fmt.Sprintf("活动告警 %d", item.ActiveAlarmCount), 7.8, severity, "F2")

	y := top - 51
	for _, finding := range inspectionPDFFindings(item) {
		lines := inspectionPDFWrapText("- "+finding, 8.2, inspectionPDFRight-inspectionPDFLeft-16)
		for _, line := range lines {
			c.mixedText(50, y, line, 8.2, inspectionPDFMuted, "F2")
			y -= 10.5
		}
	}
}

func drawInspectionBadge(c *inspectionPDFCanvas, x, top float64, label string, color inspectionPDFColor, maxWidth float64) {
	size := 7.7
	width := inspectionPDFTextWidth(label, size) + 14
	if width > maxWidth {
		size = 7.0
		width = inspectionPDFTextWidth(label, size) + 12
	}
	if width > maxWidth {
		width = maxWidth
	}
	height := 15.0
	c.fillRect(x, top-height, width, height, inspectionPDFTint(color))
	c.mixedText(x+7, top-11, label, size, color, "F2")
}

func inspectionPDFFindings(item model.DeviceHealthItem) []string {
	findings := make([]string, 0, len(item.Findings))
	for _, finding := range item.Findings {
		if finding = strings.TrimSpace(finding); finding != "" {
			findings = append(findings, finding)
		}
	}
	if len(findings) == 0 {
		findings = append(findings, "最近状态正常")
	}
	return findings
}

func inspectionBusinessStatus(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "ONLINE":
		return "在线"
	case "ALARM":
		return "告警中"
	case "OFFLINE":
		return "离线"
	case "SUSPECTED_OFFLINE":
		return "疑似离线"
	case "NEVER_SEEN":
		return "未上报"
	default:
		return inspectionPDFFallback(value, "未记录")
	}
}

func inspectionDataStatus(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "FRESH":
		return "新鲜"
	case "STALE":
		return "陈旧"
	case "SILENT":
		return "静默"
	default:
		return inspectionPDFFallback(value, "未记录")
	}
}

func inspectionSeverity(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "INFO":
		return "正常"
	case "MEDIUM":
		return "关注"
	case "HIGH":
		return "高风险"
	case "CRITICAL":
		return "严重"
	default:
		return inspectionPDFFallback(value, "未分级")
	}
}

func inspectionBusinessColor(value string) inspectionPDFColor {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "ONLINE":
		return inspectionPDFGreen
	case "ALARM":
		return inspectionPDFRed
	case "OFFLINE", "SUSPECTED_OFFLINE":
		return inspectionPDFRed
	default:
		return inspectionPDFMuted
	}
}

func inspectionDataColor(value string) inspectionPDFColor {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "FRESH":
		return inspectionPDFGreen
	case "STALE", "SILENT":
		return inspectionPDFAmber
	default:
		return inspectionPDFMuted
	}
}

func inspectionSeverityStyle(value string) (inspectionPDFColor, inspectionPDFColor) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "CRITICAL", "HIGH":
		return inspectionPDFRed, inspectionPDFRedTint
	case "MEDIUM":
		return inspectionPDFAmber, inspectionPDFAmberTint
	case "INFO":
		return inspectionPDFGreen, inspectionPDFTealTint
	default:
		return inspectionPDFMuted, inspectionPDFSurface
	}
}

func inspectionPDFTint(color inspectionPDFColor) inspectionPDFColor {
	return inspectionPDFColor{r: 0.90 + color.r*0.08, g: 0.90 + color.g*0.08, b: 0.90 + color.b*0.08}
}

func inspectionPDFFallback(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func inspectionPDFShorten(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len([]rune(value)) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit-3]) + "..."
}

func inspectionPDFWrapText(value string, size, maxWidth float64) []string {
	value = strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n")
	paragraphs := strings.Split(value, "\n")
	wrapped := make([]string, 0, len(paragraphs))
	for _, paragraph := range paragraphs {
		if paragraph == "" {
			wrapped = append(wrapped, "")
			continue
		}
		current := make([]rune, 0, len([]rune(paragraph)))
		width := 0.0
		for _, r := range []rune(paragraph) {
			runeWidth := inspectionPDFRuneWidth(r, size)
			if len(current) > 0 && width+runeWidth > maxWidth {
				wrapped = append(wrapped, string(current))
				current = current[:0]
				width = 0
			}
			current = append(current, r)
			width += runeWidth
		}
		if len(current) > 0 {
			wrapped = append(wrapped, string(current))
		}
	}
	if len(wrapped) == 0 {
		return []string{""}
	}
	return wrapped
}

func drawInspectionWrapped(c *inspectionPDFCanvas, x, top float64, lines []string, size, lineHeight float64, color inspectionPDFColor, latinFont string) float64 {
	y := top
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			c.mixedText(x, y, line, size, color, latinFont)
		}
		y -= lineHeight
	}
	return y
}

func inspectionPDFTextWidth(value string, size float64) float64 {
	width := 0.0
	for _, r := range []rune(value) {
		width += inspectionPDFRuneWidth(r, size)
	}
	return width
}

func inspectionPDFRuneWidth(r rune, size float64) float64 {
	if !inspectionPDFIsLatin(r) {
		return size
	}
	switch r {
	case ' ', '\t':
		return size * 0.28
	case 'i', 'l', 'I', '.', ',', ':', ';', '!', '|':
		return size * 0.28
	case 'm', 'w', 'M', 'W':
		return size * 0.82
	case '-', '_':
		return size * 0.38
	default:
		return size * 0.54
	}
}

func inspectionPDFIsLatin(r rune) bool {
	return r >= 0x20 && r <= 0x7e
}

func inspectionPDFTextLiteral(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "(", "\\(")
	value = strings.ReplaceAll(value, ")", "\\)")
	return value
}

func inspectionPDFTextHex(value string) string {
	var output strings.Builder
	var buffer [2]byte
	for _, r := range value {
		if r > 0xffff {
			r = '?'
		}
		binary.BigEndian.PutUint16(buffer[:], uint16(r))
		fmt.Fprintf(&output, "%02X%02X", buffer[0], buffer[1])
	}
	return output.String()
}

func inspectionTime(value int64) string {
	if value <= 0 {
		return "未记录"
	}
	return time.UnixMilli(value).In(time.Local).Format("2006-01-02 15:04:05")
}

type pdfObject struct {
	body string
}

type pdfDocument struct {
	objects []pdfObject
}

func (d *pdfDocument) add(body string) int {
	d.objects = append(d.objects, pdfObject{body: body})
	return len(d.objects)
}

func (d *pdfDocument) set(id int, body string) {
	if id > 0 && id <= len(d.objects) {
		d.objects[id-1].body = body
	}
}

func (d *pdfDocument) write(root, info int) ([]byte, error) {
	if len(d.objects) == 0 || root <= 0 || info <= 0 {
		return nil, fmt.Errorf("PDF has no root objects")
	}
	var output bytes.Buffer
	output.WriteString("%PDF-1.4\n")
	output.Write([]byte{0xe2, 0xe3, 0xcf, 0xd3})
	output.WriteByte('\n')
	offsets := make([]int, len(d.objects)+1)
	for index, object := range d.objects {
		offsets[index+1] = output.Len()
		fmt.Fprintf(&output, "%d 0 obj\n%s\nendobj\n", index+1, object.body)
	}
	xref := output.Len()
	fmt.Fprintf(&output, "xref\n0 %d\n0000000000 65535 f \n", len(d.objects)+1)
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&output, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&output, "trailer\n<< /Size %d /Root %d 0 R /Info %d 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(d.objects)+1, root, info, xref)
	return output.Bytes(), nil
}

func pdfReferences(ids []int) string {
	refs := make([]string, 0, len(ids))
	for _, id := range ids {
		refs = append(refs, fmt.Sprintf("%d 0 R", id))
	}
	return strings.Join(refs, " ")
}
