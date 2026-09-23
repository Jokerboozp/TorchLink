package core /* 声明 core 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bytes"           /* 执行当前语句并推进处理流程。 */
	"encoding/binary" /* 执行当前语句并推进处理流程。 */
	"fmt"             /* 执行当前语句并推进处理流程。 */
	"strconv"         /* 执行当前语句并推进处理流程。 */
	"strings"         /* 执行当前语句并推进处理流程。 */
	"time"            /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

const ( /* 执行当前语句并推进处理流程。 */
	inspectionPDFPageWidth  = 595.0 /* 更新 inspectionPDFPageWidth 的值。 */
	inspectionPDFPageHeight = 842.0 /* 更新 inspectionPDFPageHeight 的值。 */
	inspectionPDFLeft       = 42.0  /* 更新 inspectionPDFLeft 的值。 */
	inspectionPDFRight      = 553.0 /* 更新 inspectionPDFRight 的值。 */
	inspectionPDFFooterY    = 22.0  /* 更新 inspectionPDFFooterY 的值。 */
) /* 结束当前表达式或代码块。 */

type inspectionPDFColor struct { /* 定义 inspectionPDFColor 类型。 */
	r float64 /* 执行当前语句并推进处理流程。 */
	g float64 /* 执行当前语句并推进处理流程。 */
	b float64 /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

var ( /* 执行当前语句并推进处理流程。 */
	inspectionPDFNavy      = inspectionPDFColor{r: 0.08, g: 0.20, b: 0.33} /* 更新 inspectionPDFNavy 的值。 */
	inspectionPDFTeal      = inspectionPDFColor{r: 0.07, g: 0.48, b: 0.52} /* 更新 inspectionPDFTeal 的值。 */
	inspectionPDFBlue      = inspectionPDFColor{r: 0.10, g: 0.36, b: 0.58} /* 更新 inspectionPDFBlue 的值。 */
	inspectionPDFGreen     = inspectionPDFColor{r: 0.13, g: 0.49, b: 0.34} /* 更新 inspectionPDFGreen 的值。 */
	inspectionPDFAmber     = inspectionPDFColor{r: 0.78, g: 0.47, b: 0.08} /* 更新 inspectionPDFAmber 的值。 */
	inspectionPDFRed       = inspectionPDFColor{r: 0.71, g: 0.20, b: 0.18} /* 更新 inspectionPDFRed 的值。 */
	inspectionPDFPurple    = inspectionPDFColor{r: 0.38, g: 0.28, b: 0.63} /* 更新 inspectionPDFPurple 的值。 */
	inspectionPDFText      = inspectionPDFColor{r: 0.12, g: 0.16, b: 0.21} /* 更新 inspectionPDFText 的值。 */
	inspectionPDFMuted     = inspectionPDFColor{r: 0.38, g: 0.43, b: 0.49} /* 更新 inspectionPDFMuted 的值。 */
	inspectionPDFLine      = inspectionPDFColor{r: 0.85, g: 0.88, b: 0.91} /* 更新 inspectionPDFLine 的值。 */
	inspectionPDFSurface   = inspectionPDFColor{r: 0.97, g: 0.98, b: 0.99} /* 更新 inspectionPDFSurface 的值。 */
	inspectionPDFBlueTint  = inspectionPDFColor{r: 0.93, g: 0.97, b: 0.99} /* 更新 inspectionPDFBlueTint 的值。 */
	inspectionPDFTealTint  = inspectionPDFColor{r: 0.92, g: 0.98, b: 0.97} /* 更新 inspectionPDFTealTint 的值。 */
	inspectionPDFAmberTint = inspectionPDFColor{r: 1.00, g: 0.97, b: 0.90} /* 更新 inspectionPDFAmberTint 的值。 */
	inspectionPDFRedTint   = inspectionPDFColor{r: 1.00, g: 0.94, b: 0.94} /* 更新 inspectionPDFRedTint 的值。 */
	inspectionPDFWhite     = inspectionPDFColor{r: 1.00, g: 1.00, b: 1.00} /* 更新 inspectionPDFWhite 的值。 */
) /* 结束当前表达式或代码块。 */

// RenderHealthInspectionPDF renders the verified inspection snapshot as a
// standard A4 report. The report uses the built-in STSong-Light CJK font for
// Chinese text and Helvetica for Latin text, keeping the PDF selectable and
// avoiding the spacing defects of drawing every glyph with one CJK font.
func RenderHealthInspectionPDF(report model.DeviceHealthReport) ([]byte, error) { /* 定义 RenderHealthInspectionPDF 函数。 */
	pages := inspectionPDFPages(report) /* 更新 pages 的值。 */
	if len(pages) == 0 {                /* 判断条件并选择处理分支。 */
		pages = append(pages, &inspectionPDFCanvas{}) /* 更新 pages 的值。 */
	} /* 结束当前表达式或代码块。 */
	for index := range pages { /* 循环处理当前数据。 */
		pages[index].footer(index+1, len(pages)) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */

	doc := &pdfDocument{}                                                                                                                                                /* 更新 doc 的值。 */
	fontCID := doc.add(`<< /Type /Font /Subtype /CIDFontType0 /BaseFont /STSong-Light /CIDSystemInfo << /Registry (Adobe) /Ordering (GB1) /Supplement 4 >> /DW 1000 >>`) /* 更新 fontCID 的值。 */
	fontCJK := doc.add(fmt.Sprintf(`<< /Type /Font /Subtype /Type0 /BaseFont /STSong-Light /Encoding /UniGB-UCS2-H /DescendantFonts [%d 0 R] >>`, fontCID))              /* 更新 fontCJK 的值。 */
	fontLatin := doc.add(`<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>`)                                                            /* 更新 fontLatin 的值。 */
	fontLatinBold := doc.add(`<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold /Encoding /WinAnsiEncoding >>`)                                                   /* 更新 fontLatinBold 的值。 */
	pageTree := doc.add("")                                                                                                                                              /* 更新 pageTree 的值。 */
	catalog := doc.add("")                                                                                                                                               /* 更新 catalog 的值。 */
	info := doc.add(`<< /Title (Health Inspection Report) /Author (iot-platform) /Producer (iot-platform) >>`)                                                           /* 更新 info 的值。 */

	pageIDs := make([]int, 0, len(pages)) /* 更新 pageIDs 的值。 */
	for _, page := range pages {          /* 循环处理当前数据。 */
		content := page.body.String()                                                                                                                                                                                                                                                        /* 更新 content 的值。 */
		contentID := doc.add(fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content))                                                                                                                                                                                  /* 更新 contentID 的值。 */
		pageID := doc.add(fmt.Sprintf("<< /Type /Page /Parent %d 0 R /MediaBox [0 0 %.0f %.0f] /Resources << /Font << /F1 %d 0 R /F2 %d 0 R /F3 %d 0 R >> >> /Contents %d 0 R >>", pageTree, inspectionPDFPageWidth, inspectionPDFPageHeight, fontCJK, fontLatin, fontLatinBold, contentID)) /* 更新 pageID 的值。 */
		pageIDs = append(pageIDs, pageID)                                                                                                                                                                                                                                                    /* 更新 pageIDs 的值。 */
	} /* 结束当前表达式或代码块。 */

	doc.set(pageTree, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", pdfReferences(pageIDs), len(pageIDs))) /* 执行当前语句并推进处理流程。 */
	doc.set(catalog, fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pageTree))                                   /* 执行当前语句并推进处理流程。 */
	return doc.write(catalog, info)                                                                                 /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

type inspectionPDFCanvas struct { /* 定义 inspectionPDFCanvas 类型。 */
	body strings.Builder /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (c *inspectionPDFCanvas) fillRect(x, y, width, height float64, color inspectionPDFColor) { /* 定义 fillRect 函数。 */
	c.fill(color)                                                           /* 执行当前语句并推进处理流程。 */
	fmt.Fprintf(&c.body, "%.2f %.2f %.2f %.2f re f\n", x, y, width, height) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (c *inspectionPDFCanvas) strokeRect(x, y, width, height float64, color inspectionPDFColor, lineWidth float64) { /* 定义 strokeRect 函数。 */
	c.stroke(color, lineWidth)                                              /* 执行当前语句并推进处理流程。 */
	fmt.Fprintf(&c.body, "%.2f %.2f %.2f %.2f re S\n", x, y, width, height) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (c *inspectionPDFCanvas) line(x1, y1, x2, y2 float64, color inspectionPDFColor, lineWidth float64) { /* 定义 line 函数。 */
	c.stroke(color, lineWidth)                                          /* 执行当前语句并推进处理流程。 */
	fmt.Fprintf(&c.body, "%.2f %.2f m %.2f %.2f l S\n", x1, y1, x2, y2) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (c *inspectionPDFCanvas) fill(color inspectionPDFColor) { /* 定义 fill 函数。 */
	fmt.Fprintf(&c.body, "%.3f %.3f %.3f rg\n", color.r, color.g, color.b) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (c *inspectionPDFCanvas) stroke(color inspectionPDFColor, lineWidth float64) { /* 定义 stroke 函数。 */
	fmt.Fprintf(&c.body, "%.3f %.3f %.3f RG %.2f w\n", color.r, color.g, color.b, lineWidth) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (c *inspectionPDFCanvas) mixedText(x, y float64, value string, size float64, color inspectionPDFColor, latinFont string) { /* 定义 mixedText 函数。 */
	value = strings.ReplaceAll(strings.ReplaceAll(value, "\r", " "), "\n", " ") /* 更新 value 的值。 */
	if strings.TrimSpace(value) == "" {                                         /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	c.fill(color)                                            /* 执行当前语句并推进处理流程。 */
	fmt.Fprintf(&c.body, "BT\n1 0 0 1 %.2f %.2f Tm\n", x, y) /* 执行当前语句并推进处理流程。 */
	runes := []rune(value)                                   /* 更新 runes 的值。 */
	start := 0                                               /* 更新 start 的值。 */
	for start < len(runes) {                                 /* 循环处理当前数据。 */
		ascii := inspectionPDFIsLatin(runes[start])                         /* 更新 ascii 的值。 */
		end := start + 1                                                    /* 更新 end 的值。 */
		for end < len(runes) && inspectionPDFIsLatin(runes[end]) == ascii { /* 循环处理当前数据。 */
			end++ /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if ascii { /* 判断条件并选择处理分支。 */
			fmt.Fprintf(&c.body, "/%s %g Tf (%s) Tj\n", latinFont, size, inspectionPDFTextLiteral(string(runes[start:end]))) /* 执行当前语句并推进处理流程。 */
		} else { /* 结束当前表达式或代码块。 */
			fmt.Fprintf(&c.body, "/F1 %g Tf <%s> Tj\n", size, inspectionPDFTextHex(string(runes[start:end]))) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		start = end /* 更新 start 的值。 */
	} /* 结束当前表达式或代码块。 */
	c.body.WriteString("ET\n") /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (c *inspectionPDFCanvas) rightText(right, y float64, value string, size float64, color inspectionPDFColor, latinFont string) { /* 定义 rightText 函数。 */
	c.mixedText(right-inspectionPDFTextWidth(value, size), y, value, size, color, latinFont) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (c *inspectionPDFCanvas) footer(page, total int) { /* 定义 footer 函数。 */
	c.line(inspectionPDFLeft, 37, inspectionPDFRight, 37, inspectionPDFLine, 0.7)                                                 /* 执行当前语句并推进处理流程。 */
	c.mixedText(inspectionPDFLeft, inspectionPDFFooterY, "iot-platform | 智能巡检报告", 7.6, inspectionPDFMuted, "F2")                  /* 执行当前语句并推进处理流程。 */
	c.rightText(inspectionPDFRight, inspectionPDFFooterY, fmt.Sprintf("第 %d / %d 页", page, total), 7.6, inspectionPDFMuted, "F2") /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func inspectionPDFPages(report model.DeviceHealthReport) []*inspectionPDFCanvas { /* 定义 inspectionPDFPages 函数。 */
	pages := make([]*inspectionPDFCanvas, 0, 2) /* 更新 pages 的值。 */
	overview := &inspectionPDFCanvas{}          /* 更新 overview 的值。 */
	drawInspectionOverview(overview, report)    /* 执行当前语句并推进处理流程。 */
	pages = append(pages, overview)             /* 更新 pages 的值。 */
	if len(report.Items) == 0 {                 /* 判断条件并选择处理分支。 */
		return pages /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */

	current := inspectionPDFNewDetailPage(report) /* 更新 current 的值。 */
	y := inspectionPDFDetailTableTop - 28         /* 更新 y 的值。 */
	for _, item := range report.Items {           /* 循环处理当前数据。 */
		rowHeight := inspectionPDFDeviceRowHeight(item) /* 更新 rowHeight 的值。 */
		if y-rowHeight < 52 {                           /* 判断条件并选择处理分支。 */
			pages = append(pages, current)               /* 更新 pages 的值。 */
			current = inspectionPDFNewDetailPage(report) /* 更新 current 的值。 */
			y = inspectionPDFDetailTableTop - 28         /* 更新 y 的值。 */
		} /* 结束当前表达式或代码块。 */
		drawInspectionDeviceRow(current, item, y, rowHeight) /* 执行当前语句并推进处理流程。 */
		y -= rowHeight + 6                                   /* 更新 y 的值。 */
	} /* 结束当前表达式或代码块。 */
	pages = append(pages, current) /* 更新 pages 的值。 */
	return pages                   /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func drawInspectionOverview(c *inspectionPDFCanvas, report model.DeviceHealthReport) { /* 定义 drawInspectionOverview 函数。 */
	drawInspectionHeader(c, report, false)          /* 执行当前语句并推进处理流程。 */
	y := drawInspectionSectionTitle(c, "巡检概览", 731) /* 更新 y 的值。 */
	y -= 5                                          /* 更新 y 的值。 */
	y = drawInspectionSummary(c, report, y)         /* 更新 y 的值。 */
	y -= 16                                         /* 更新 y 的值。 */
	y = drawInspectionMetrics(c, report, y)         /* 更新 y 的值。 */
	y -= 19                                         /* 更新 y 的值。 */
	y = drawInspectionStatusOverview(c, report, y)  /* 更新 y 的值。 */
	y -= 17                                         /* 更新 y 的值。 */
	y = drawInspectionAdvice(c, report, y)          /* 更新 y 的值。 */
	if len(report.Warnings) > 0 {                   /* 判断条件并选择处理分支。 */
		y -= 17                                       /* 更新 y 的值。 */
		drawInspectionWarnings(c, report.Warnings, y) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func drawInspectionHeader(c *inspectionPDFCanvas, report model.DeviceHealthReport, compact bool) { /* 定义 drawInspectionHeader 函数。 */
	if compact { /* 判断条件并选择处理分支。 */
		c.fillRect(0, 792, inspectionPDFPageWidth, 50, inspectionPDFNavy)                                           /* 执行当前语句并推进处理流程。 */
		c.fillRect(0, 792, 6, 50, inspectionPDFTeal)                                                                /* 执行当前语句并推进处理流程。 */
		c.mixedText(inspectionPDFLeft, 812, "设备智能巡检报告", 14, inspectionPDFWhite, "F3")                               /* 执行当前语句并推进处理流程。 */
		c.rightText(inspectionPDFRight, 813, "生成 "+inspectionTime(report.GeneratedAt), 8, inspectionPDFWhite, "F2") /* 执行当前语句并推进处理流程。 */
		return                                                                                                      /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */

	c.fillRect(0, 760, inspectionPDFPageWidth, 82, inspectionPDFNavy)                        /* 执行当前语句并推进处理流程。 */
	c.fillRect(0, 760, 7, 82, inspectionPDFTeal)                                             /* 执行当前语句并推进处理流程。 */
	c.mixedText(inspectionPDFLeft, 807, "设备智能巡检报告", 22, inspectionPDFWhite, "F3")            /* 执行当前语句并推进处理流程。 */
	c.mixedText(inspectionPDFLeft, 784, "消防物联网设备健康与运行状态综合评估", 9.5, inspectionPDFWhite, "F2") /* 执行当前语句并推进处理流程。 */
	tenant := strings.TrimSpace(report.TenantID)                                             /* 更新 tenant 的值。 */
	if tenant == "" {                                                                        /* 判断条件并选择处理分支。 */
		tenant = "默认租户" /* 更新 tenant 的值。 */
	} /* 结束当前表达式或代码块。 */
	c.rightText(inspectionPDFRight, 807, "租户 "+tenant, 8.5, inspectionPDFWhite, "F2")                             /* 执行当前语句并推进处理流程。 */
	c.rightText(inspectionPDFRight, 784, "生成 "+inspectionTime(report.GeneratedAt), 8.5, inspectionPDFWhite, "F2") /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func drawInspectionSectionTitle(c *inspectionPDFCanvas, title string, y float64) float64 { /* 定义 drawInspectionSectionTitle 函数。 */
	c.mixedText(inspectionPDFLeft, y, title, 12.5, inspectionPDFNavy, "F3")   /* 执行当前语句并推进处理流程。 */
	lineStart := inspectionPDFLeft + inspectionPDFTextWidth(title, 12.5) + 12 /* 更新 lineStart 的值。 */
	if lineStart < inspectionPDFRight {                                       /* 判断条件并选择处理分支。 */
		c.line(lineStart, y+3, inspectionPDFRight, y+3, inspectionPDFLine, 0.8) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	return y - 20 /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func drawInspectionSummary(c *inspectionPDFCanvas, report model.DeviceHealthReport, top float64) float64 { /* 定义 drawInspectionSummary 函数。 */
	text := strings.TrimSpace(report.Summary) /* 更新 text 的值。 */
	if text == "" {                           /* 判断条件并选择处理分支。 */
		text = "暂无总体结论。" /* 更新 text 的值。 */
	} /* 结束当前表达式或代码块。 */
	lines := inspectionPDFWrapText(text, 9.6, inspectionPDFRight-inspectionPDFLeft-32) /* 更新 lines 的值。 */
	height := 29 + float64(len(lines))*12.5                                            /* 更新 height 的值。 */
	if height < 50 {                                                                   /* 判断条件并选择处理分支。 */
		height = 50 /* 更新 height 的值。 */
	} /* 结束当前表达式或代码块。 */
	bottom := top - height                                                                                     /* 更新 bottom 的值。 */
	c.fillRect(inspectionPDFLeft, bottom, inspectionPDFRight-inspectionPDFLeft, height, inspectionPDFBlueTint) /* 执行当前语句并推进处理流程。 */
	c.fillRect(inspectionPDFLeft, bottom, 4, height, inspectionPDFBlue)                                        /* 执行当前语句并推进处理流程。 */
	c.mixedText(inspectionPDFLeft+15, top-17, "总体结论", 9, inspectionPDFBlue, "F3")                              /* 执行当前语句并推进处理流程。 */
	drawInspectionWrapped(c, inspectionPDFLeft+15, top-33, lines, 9.6, 12.5, inspectionPDFText, "F2")          /* 执行当前语句并推进处理流程。 */
	return bottom                                                                                              /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

type inspectionPDFMetric struct { /* 定义 inspectionPDFMetric 类型。 */
	label string             /* 执行当前语句并推进处理流程。 */
	value int                /* 执行当前语句并推进处理流程。 */
	color inspectionPDFColor /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func drawInspectionMetrics(c *inspectionPDFCanvas, report model.DeviceHealthReport, top float64) float64 { /* 定义 drawInspectionMetrics 函数。 */
	metrics := []inspectionPDFMetric{ /* 更新 metrics 的值。 */
		{label: "设备总数", value: report.Counts["total"], color: inspectionPDFBlue},          /* 执行当前语句并推进处理流程。 */
		{label: "状态正常", value: report.Counts["healthy"], color: inspectionPDFGreen},       /* 执行当前语句并推进处理流程。 */
		{label: "需关注", value: report.Counts["attention"], color: inspectionPDFAmber},      /* 执行当前语句并推进处理流程。 */
		{label: "高风险", value: report.Counts["critical"], color: inspectionPDFRed},         /* 执行当前语句并推进处理流程。 */
		{label: "离线 / 疑似离线", value: report.Counts["offline"], color: inspectionPDFPurple}, /* 执行当前语句并推进处理流程。 */
		{label: "活动告警", value: report.Counts["activeAlarms"], color: inspectionPDFRed},    /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	gap := 10.0                                                       /* 更新 gap 的值。 */
	cardWidth := (inspectionPDFRight - inspectionPDFLeft - gap*2) / 3 /* 更新 cardWidth 的值。 */
	cardHeight := 52.0                                                /* 更新 cardHeight 的值。 */
	rowGap := 9.0                                                     /* 更新 rowGap 的值。 */
	for index, metric := range metrics {                              /* 循环处理当前数据。 */
		row := index / 3                                                            /* 更新 row 的值。 */
		column := index % 3                                                         /* 更新 column 的值。 */
		x := inspectionPDFLeft + float64(column)*(cardWidth+gap)                    /* 更新 x 的值。 */
		y := top - float64(row)*(cardHeight+rowGap) - cardHeight                    /* 更新 y 的值。 */
		c.fillRect(x, y, cardWidth, cardHeight, inspectionPDFSurface)               /* 执行当前语句并推进处理流程。 */
		c.fillRect(x, y, 3, cardHeight, metric.color)                               /* 执行当前语句并推进处理流程。 */
		c.strokeRect(x, y, cardWidth, cardHeight, inspectionPDFLine, 0.7)           /* 执行当前语句并推进处理流程。 */
		c.mixedText(x+13, y+35, metric.label, 8.8, inspectionPDFMuted, "F2")        /* 执行当前语句并推进处理流程。 */
		c.mixedText(x+13, y+14, strconv.Itoa(metric.value), 21, metric.color, "F3") /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	return top - cardHeight*2 - rowGap /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func drawInspectionStatusOverview(c *inspectionPDFCanvas, report model.DeviceHealthReport, top float64) float64 { /* 定义 drawInspectionStatusOverview 函数。 */
	y := drawInspectionSectionTitle(c, "状态概览", top) /* 更新 y 的值。 */
	barTop := y - 2                                 /* 更新 barTop 的值。 */
	barHeight := 13.0                               /* 更新 barHeight 的值。 */
	total := report.Counts["total"]                 /* 更新 total 的值。 */
	healthy := report.Counts["healthy"]             /* 更新 healthy 的值。 */
	if total < 0 {                                  /* 判断条件并选择处理分支。 */
		total = 0 /* 更新 total 的值。 */
	} /* 结束当前表达式或代码块。 */
	if healthy < 0 { /* 判断条件并选择处理分支。 */
		healthy = 0 /* 更新 healthy 的值。 */
	} /* 结束当前表达式或代码块。 */
	if healthy > total { /* 判断条件并选择处理分支。 */
		healthy = total /* 更新 healthy 的值。 */
	} /* 结束当前表达式或代码块。 */
	attention := total - healthy                                                         /* 更新 attention 的值。 */
	width := inspectionPDFRight - inspectionPDFLeft                                      /* 更新 width 的值。 */
	c.fillRect(inspectionPDFLeft, barTop-barHeight, width, barHeight, inspectionPDFLine) /* 执行当前语句并推进处理流程。 */
	if total > 0 {                                                                       /* 判断条件并选择处理分支。 */
		c.fillRect(inspectionPDFLeft, barTop-barHeight, width*float64(healthy)/float64(total), barHeight, inspectionPDFGreen)                                         /* 执行当前语句并推进处理流程。 */
		c.fillRect(inspectionPDFLeft+width*float64(healthy)/float64(total), barTop-barHeight, width*float64(attention)/float64(total), barHeight, inspectionPDFAmber) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	legendY := barTop - 29                                                                                                                                                                                     /* 更新 legendY 的值。 */
	c.fillRect(inspectionPDFLeft, legendY-2, 8, 8, inspectionPDFGreen)                                                                                                                                         /* 执行当前语句并推进处理流程。 */
	c.mixedText(inspectionPDFLeft+14, legendY, fmt.Sprintf("状态正常 %d", healthy), 8.6, inspectionPDFText, "F2")                                                                                                  /* 执行当前语句并推进处理流程。 */
	secondX := inspectionPDFLeft + 165                                                                                                                                                                         /* 更新 secondX 的值。 */
	c.fillRect(secondX, legendY-2, 8, 8, inspectionPDFAmber)                                                                                                                                                   /* 执行当前语句并推进处理流程。 */
	c.mixedText(secondX+14, legendY, fmt.Sprintf("需关注 %d", attention), 8.6, inspectionPDFText, "F2")                                                                                                           /* 执行当前语句并推进处理流程。 */
	c.mixedText(inspectionPDFLeft, legendY-18, fmt.Sprintf("其中高风险 %d，离线 / 疑似离线 %d，活动告警 %d。", report.Counts["critical"], report.Counts["offline"], report.Counts["activeAlarms"]), 8, inspectionPDFMuted, "F2") /* 执行当前语句并推进处理流程。 */
	return legendY - 29                                                                                                                                                                                        /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func drawInspectionAdvice(c *inspectionPDFCanvas, report model.DeviceHealthReport, top float64) float64 { /* 定义 drawInspectionAdvice 函数。 */
	y := drawInspectionSectionTitle(c, "智能分析建议", top) /* 更新 y 的值。 */
	text := strings.TrimSpace(report.AIAdvice)        /* 更新 text 的值。 */
	if text == "" {                                   /* 判断条件并选择处理分支。 */
		text = "本次未生成 AI 建议，请依据设备状态和告警信息完成现场复核。" /* 更新 text 的值。 */
	} /* 结束当前表达式或代码块。 */
	lines := inspectionPDFWrapText(text, 8.9, inspectionPDFRight-inspectionPDFLeft-32) /* 更新 lines 的值。 */
	height := 29 + float64(len(lines))*12                                              /* 更新 height 的值。 */
	if height < 52 {                                                                   /* 判断条件并选择处理分支。 */
		height = 52 /* 更新 height 的值。 */
	} /* 结束当前表达式或代码块。 */
	bottom := y - height                                                                                             /* 更新 bottom 的值。 */
	c.fillRect(inspectionPDFLeft, bottom, inspectionPDFRight-inspectionPDFLeft, height, inspectionPDFTealTint)       /* 执行当前语句并推进处理流程。 */
	c.fillRect(inspectionPDFLeft, bottom, 4, height, inspectionPDFTeal)                                              /* 执行当前语句并推进处理流程。 */
	drawInspectionWrapped(c, inspectionPDFLeft+15, y-17, []string{"辅助判断，不替代现场处置"}, 8.6, 12, inspectionPDFTeal, "F3") /* 执行当前语句并推进处理流程。 */
	drawInspectionWrapped(c, inspectionPDFLeft+15, y-32, lines, 8.9, 12, inspectionPDFText, "F2")                    /* 执行当前语句并推进处理流程。 */
	return bottom                                                                                                    /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func drawInspectionWarnings(c *inspectionPDFCanvas, warnings []string, top float64) float64 { /* 定义 drawInspectionWarnings 函数。 */
	y := drawInspectionSectionTitle(c, "注意事项", top) /* 更新 y 的值。 */
	lines := make([]string, 0, len(warnings))       /* 更新 lines 的值。 */
	for _, warning := range warnings {              /* 循环处理当前数据。 */
		warning = strings.TrimSpace(warning) /* 更新 warning 的值。 */
		if warning == "" {                   /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		lines = append(lines, "- "+warning) /* 更新 lines 的值。 */
	} /* 结束当前表达式或代码块。 */
	if len(lines) == 0 { /* 判断条件并选择处理分支。 */
		return y /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	wrapped := make([]string, 0, len(lines)) /* 更新 wrapped 的值。 */
	for _, line := range lines {             /* 循环处理当前数据。 */
		wrapped = append(wrapped, inspectionPDFWrapText(line, 8.7, inspectionPDFRight-inspectionPDFLeft-32)...) /* 更新 wrapped 的值。 */
	} /* 结束当前表达式或代码块。 */
	height := 20 + float64(len(wrapped))*11.5                                                                   /* 更新 height 的值。 */
	bottom := y - height                                                                                        /* 更新 bottom 的值。 */
	c.fillRect(inspectionPDFLeft, bottom, inspectionPDFRight-inspectionPDFLeft, height, inspectionPDFAmberTint) /* 执行当前语句并推进处理流程。 */
	c.fillRect(inspectionPDFLeft, bottom, 4, height, inspectionPDFAmber)                                        /* 执行当前语句并推进处理流程。 */
	drawInspectionWrapped(c, inspectionPDFLeft+15, y-16, wrapped, 8.7, 11.5, inspectionPDFText, "F2")           /* 执行当前语句并推进处理流程。 */
	return bottom                                                                                               /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

const inspectionPDFDetailTableTop = 746.0 /* 声明 inspectionPDFDetailTableTop。 */

func inspectionPDFNewDetailPage(report model.DeviceHealthReport) *inspectionPDFCanvas { /* 定义 inspectionPDFNewDetailPage 函数。 */
	page := &inspectionPDFCanvas{}                               /* 更新 page 的值。 */
	drawInspectionHeader(page, report, true)                     /* 执行当前语句并推进处理流程。 */
	drawInspectionSectionTitle(page, "设备巡检明细", 775)              /* 执行当前语句并推进处理流程。 */
	drawInspectionTableHeader(page, inspectionPDFDetailTableTop) /* 执行当前语句并推进处理流程。 */
	return page                                                  /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func drawInspectionTableHeader(c *inspectionPDFCanvas, top float64) { /* 定义 drawInspectionTableHeader 函数。 */
	columns := []struct { /* 更新 columns 的值。 */
		x     float64 /* 执行当前语句并推进处理流程。 */
		width float64 /* 执行当前语句并推进处理流程。 */
		title string  /* 执行当前语句并推进处理流程。 */
	}{ /* 结束当前表达式或代码块。 */
		{inspectionPDFLeft, 155, "设备 / 产品 / ID"}, /* 执行当前语句并推进处理流程。 */
		{197, 88, "业务状态"},                        /* 执行当前语句并推进处理流程。 */
		{285, 77, "数据质量"},                        /* 执行当前语句并推进处理流程。 */
		{362, 100, "最近上报"},                       /* 执行当前语句并推进处理流程。 */
		{462, 91, "风险 / 告警"},                     /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	height := 28.0                                                                                             /* 更新 height 的值。 */
	c.fillRect(inspectionPDFLeft, top-height, inspectionPDFRight-inspectionPDFLeft, height, inspectionPDFNavy) /* 执行当前语句并推进处理流程。 */
	for _, column := range columns {                                                                           /* 循环处理当前数据。 */
		c.mixedText(column.x+8, top-18, column.title, 8.2, inspectionPDFWhite, "F2") /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func inspectionPDFDeviceRowHeight(item model.DeviceHealthItem) float64 { /* 定义 inspectionPDFDeviceRowHeight 函数。 */
	findings := inspectionPDFFindings(item) /* 更新 findings 的值。 */
	lineCount := 0                          /* 更新 lineCount 的值。 */
	for _, finding := range findings {      /* 循环处理当前数据。 */
		lineCount += len(inspectionPDFWrapText("- "+finding, 8.2, inspectionPDFRight-inspectionPDFLeft-16)) /* 更新 lineCount 的值。 */
	} /* 结束当前表达式或代码块。 */
	if lineCount == 0 { /* 判断条件并选择处理分支。 */
		lineCount = 1 /* 更新 lineCount 的值。 */
	} /* 结束当前表达式或代码块。 */
	return 62 + float64(lineCount)*10.5 /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func drawInspectionDeviceRow(c *inspectionPDFCanvas, item model.DeviceHealthItem, top, height float64) { /* 定义 drawInspectionDeviceRow 函数。 */
	bottom := top - height /* 更新 bottom 的值。 */
	if int(top/2)%2 == 0 { /* 判断条件并选择处理分支。 */
		c.fillRect(inspectionPDFLeft, bottom, inspectionPDFRight-inspectionPDFLeft, height, inspectionPDFSurface) /* 执行当前语句并推进处理流程。 */
	} else { /* 结束当前表达式或代码块。 */
		c.fillRect(inspectionPDFLeft, bottom, inspectionPDFRight-inspectionPDFLeft, height, inspectionPDFWhite) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	c.strokeRect(inspectionPDFLeft, bottom, inspectionPDFRight-inspectionPDFLeft, height, inspectionPDFLine, 0.6) /* 执行当前语句并推进处理流程。 */
	for _, x := range []float64{197, 285, 362, 462} {                                                             /* 循环处理当前数据。 */
		c.line(x, bottom, x, top, inspectionPDFLine, 0.5) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */

	name := strings.TrimSpace(item.DeviceName) /* 更新 name 的值。 */
	if name == "" {                            /* 判断条件并选择处理分支。 */
		name = item.DeviceID /* 更新 name 的值。 */
	} /* 结束当前表达式或代码块。 */
	c.mixedText(50, top-15, inspectionPDFShorten(name, 22), 9.2, inspectionPDFText, "F2")                  /* 执行当前语句并推进处理流程。 */
	c.mixedText(50, top-28, "产品 "+inspectionPDFShorten(item.ProductID, 20), 7.8, inspectionPDFMuted, "F2") /* 执行当前语句并推进处理流程。 */
	c.mixedText(50, top-40, "ID "+inspectionPDFShorten(item.DeviceID, 24), 7.8, inspectionPDFMuted, "F2")  /* 执行当前语句并推进处理流程。 */

	drawInspectionBadge(c, 205, top-11, inspectionBusinessStatus(item.BusinessStatus), inspectionBusinessColor(item.BusinessStatus), 80) /* 执行当前语句并推进处理流程。 */
	drawInspectionBadge(c, 293, top-11, inspectionDataStatus(item.DataStatus), inspectionDataColor(item.DataStatus), 69)                 /* 执行当前语句并推进处理流程。 */
	c.mixedText(370, top-18, inspectionTime(item.LastSeenAt), 8, inspectionPDFText, "F2")                                                /* 执行当前语句并推进处理流程。 */

	severity, severityFill := inspectionSeverityStyle(item.Severity)                             /* 更新 severityFill 的值。 */
	drawInspectionBadge(c, 470, top-11, inspectionSeverity(item.Severity), severityFill, 75)     /* 执行当前语句并推进处理流程。 */
	c.mixedText(470, top-32, fmt.Sprintf("活动告警 %d", item.ActiveAlarmCount), 7.8, severity, "F2") /* 执行当前语句并推进处理流程。 */

	y := top - 51                                         /* 更新 y 的值。 */
	for _, finding := range inspectionPDFFindings(item) { /* 循环处理当前数据。 */
		lines := inspectionPDFWrapText("- "+finding, 8.2, inspectionPDFRight-inspectionPDFLeft-16) /* 更新 lines 的值。 */
		for _, line := range lines {                                                               /* 循环处理当前数据。 */
			c.mixedText(50, y, line, 8.2, inspectionPDFMuted, "F2") /* 执行当前语句并推进处理流程。 */
			y -= 10.5                                               /* 更新 y 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func drawInspectionBadge(c *inspectionPDFCanvas, x, top float64, label string, color inspectionPDFColor, maxWidth float64) { /* 定义 drawInspectionBadge 函数。 */
	size := 7.7                                       /* 更新 size 的值。 */
	width := inspectionPDFTextWidth(label, size) + 14 /* 更新 width 的值。 */
	if width > maxWidth {                             /* 判断条件并选择处理分支。 */
		size = 7.0                                       /* 更新 size 的值。 */
		width = inspectionPDFTextWidth(label, size) + 12 /* 更新 width 的值。 */
	} /* 结束当前表达式或代码块。 */
	if width > maxWidth { /* 判断条件并选择处理分支。 */
		width = maxWidth /* 更新 width 的值。 */
	} /* 结束当前表达式或代码块。 */
	height := 15.0                                                     /* 更新 height 的值。 */
	c.fillRect(x, top-height, width, height, inspectionPDFTint(color)) /* 执行当前语句并推进处理流程。 */
	c.mixedText(x+7, top-11, label, size, color, "F2")                 /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func inspectionPDFFindings(item model.DeviceHealthItem) []string { /* 定义 inspectionPDFFindings 函数。 */
	findings := make([]string, 0, len(item.Findings)) /* 更新 findings 的值。 */
	for _, finding := range item.Findings {           /* 循环处理当前数据。 */
		if finding = strings.TrimSpace(finding); finding != "" { /* 判断条件并选择处理分支。 */
			findings = append(findings, finding) /* 更新 findings 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if len(findings) == 0 { /* 判断条件并选择处理分支。 */
		findings = append(findings, "最近状态正常") /* 更新 findings 的值。 */
	} /* 结束当前表达式或代码块。 */
	return findings /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func inspectionBusinessStatus(value string) string { /* 定义 inspectionBusinessStatus 函数。 */
	switch strings.ToUpper(strings.TrimSpace(value)) { /* 根据条件选择处理路径。 */
	case "ONLINE": /* 处理当前分支。 */
		return "在线" /* 返回当前处理结果。 */
	case "ALARM": /* 处理当前分支。 */
		return "告警中" /* 返回当前处理结果。 */
	case "OFFLINE": /* 处理当前分支。 */
		return "离线" /* 返回当前处理结果。 */
	case "SUSPECTED_OFFLINE": /* 处理当前分支。 */
		return "疑似离线" /* 返回当前处理结果。 */
	case "NEVER_SEEN": /* 处理当前分支。 */
		return "未上报" /* 返回当前处理结果。 */
	default: /* 处理当前分支。 */
		return inspectionPDFFallback(value, "未记录") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func inspectionDataStatus(value string) string { /* 定义 inspectionDataStatus 函数。 */
	switch strings.ToUpper(strings.TrimSpace(value)) { /* 根据条件选择处理路径。 */
	case "FRESH": /* 处理当前分支。 */
		return "新鲜" /* 返回当前处理结果。 */
	case "STALE": /* 处理当前分支。 */
		return "陈旧" /* 返回当前处理结果。 */
	case "SILENT": /* 处理当前分支。 */
		return "静默" /* 返回当前处理结果。 */
	default: /* 处理当前分支。 */
		return inspectionPDFFallback(value, "未记录") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func inspectionSeverity(value string) string { /* 定义 inspectionSeverity 函数。 */
	switch strings.ToUpper(strings.TrimSpace(value)) { /* 根据条件选择处理路径。 */
	case "INFO": /* 处理当前分支。 */
		return "正常" /* 返回当前处理结果。 */
	case "MEDIUM": /* 处理当前分支。 */
		return "关注" /* 返回当前处理结果。 */
	case "HIGH": /* 处理当前分支。 */
		return "高风险" /* 返回当前处理结果。 */
	case "CRITICAL": /* 处理当前分支。 */
		return "严重" /* 返回当前处理结果。 */
	default: /* 处理当前分支。 */
		return inspectionPDFFallback(value, "未分级") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func inspectionBusinessColor(value string) inspectionPDFColor { /* 定义 inspectionBusinessColor 函数。 */
	switch strings.ToUpper(strings.TrimSpace(value)) { /* 根据条件选择处理路径。 */
	case "ONLINE": /* 处理当前分支。 */
		return inspectionPDFGreen /* 返回当前处理结果。 */
	case "ALARM": /* 处理当前分支。 */
		return inspectionPDFRed /* 返回当前处理结果。 */
	case "OFFLINE", "SUSPECTED_OFFLINE": /* 处理当前分支。 */
		return inspectionPDFRed /* 返回当前处理结果。 */
	default: /* 处理当前分支。 */
		return inspectionPDFMuted /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func inspectionDataColor(value string) inspectionPDFColor { /* 定义 inspectionDataColor 函数。 */
	switch strings.ToUpper(strings.TrimSpace(value)) { /* 根据条件选择处理路径。 */
	case "FRESH": /* 处理当前分支。 */
		return inspectionPDFGreen /* 返回当前处理结果。 */
	case "STALE", "SILENT": /* 处理当前分支。 */
		return inspectionPDFAmber /* 返回当前处理结果。 */
	default: /* 处理当前分支。 */
		return inspectionPDFMuted /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func inspectionSeverityStyle(value string) (inspectionPDFColor, inspectionPDFColor) { /* 定义 inspectionSeverityStyle 函数。 */
	switch strings.ToUpper(strings.TrimSpace(value)) { /* 根据条件选择处理路径。 */
	case "CRITICAL", "HIGH": /* 处理当前分支。 */
		return inspectionPDFRed, inspectionPDFRedTint /* 返回当前处理结果。 */
	case "MEDIUM": /* 处理当前分支。 */
		return inspectionPDFAmber, inspectionPDFAmberTint /* 返回当前处理结果。 */
	case "INFO": /* 处理当前分支。 */
		return inspectionPDFGreen, inspectionPDFTealTint /* 返回当前处理结果。 */
	default: /* 处理当前分支。 */
		return inspectionPDFMuted, inspectionPDFSurface /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func inspectionPDFTint(color inspectionPDFColor) inspectionPDFColor { /* 定义 inspectionPDFTint 函数。 */
	return inspectionPDFColor{r: 0.90 + color.r*0.08, g: 0.90 + color.g*0.08, b: 0.90 + color.b*0.08} /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func inspectionPDFFallback(value, fallback string) string { /* 定义 inspectionPDFFallback 函数。 */
	value = strings.TrimSpace(value) /* 更新 value 的值。 */
	if value == "" {                 /* 判断条件并选择处理分支。 */
		return fallback /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return value /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func inspectionPDFShorten(value string, limit int) string { /* 定义 inspectionPDFShorten 函数。 */
	value = strings.TrimSpace(value)               /* 更新 value 的值。 */
	if limit <= 0 || len([]rune(value)) <= limit { /* 判断条件并选择处理分支。 */
		return value /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	runes := []rune(value)                 /* 更新 runes 的值。 */
	return string(runes[:limit-3]) + "..." /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func inspectionPDFWrapText(value string, size, maxWidth float64) []string { /* 定义 inspectionPDFWrapText 函数。 */
	value = strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n") /* 更新 value 的值。 */
	paragraphs := strings.Split(value, "\n")                                        /* 更新 paragraphs 的值。 */
	wrapped := make([]string, 0, len(paragraphs))                                   /* 更新 wrapped 的值。 */
	for _, paragraph := range paragraphs {                                          /* 循环处理当前数据。 */
		if paragraph == "" { /* 判断条件并选择处理分支。 */
			wrapped = append(wrapped, "") /* 更新 wrapped 的值。 */
			continue                      /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		current := make([]rune, 0, len([]rune(paragraph))) /* 更新 current 的值。 */
		width := 0.0                                       /* 更新 width 的值。 */
		for _, r := range []rune(paragraph) {              /* 循环处理当前数据。 */
			runeWidth := inspectionPDFRuneWidth(r, size)        /* 更新 runeWidth 的值。 */
			if len(current) > 0 && width+runeWidth > maxWidth { /* 判断条件并选择处理分支。 */
				wrapped = append(wrapped, string(current)) /* 更新 wrapped 的值。 */
				current = current[:0]                      /* 更新 current 的值。 */
				width = 0                                  /* 更新 width 的值。 */
			} /* 结束当前表达式或代码块。 */
			current = append(current, r) /* 更新 current 的值。 */
			width += runeWidth           /* 更新 width 的值。 */
		} /* 结束当前表达式或代码块。 */
		if len(current) > 0 { /* 判断条件并选择处理分支。 */
			wrapped = append(wrapped, string(current)) /* 更新 wrapped 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if len(wrapped) == 0 { /* 判断条件并选择处理分支。 */
		return []string{""} /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return wrapped /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func drawInspectionWrapped(c *inspectionPDFCanvas, x, top float64, lines []string, size, lineHeight float64, color inspectionPDFColor, latinFont string) float64 { /* 定义 drawInspectionWrapped 函数。 */
	y := top                     /* 更新 y 的值。 */
	for _, line := range lines { /* 循环处理当前数据。 */
		if strings.TrimSpace(line) != "" { /* 判断条件并选择处理分支。 */
			c.mixedText(x, y, line, size, color, latinFont) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		y -= lineHeight /* 更新 y 的值。 */
	} /* 结束当前表达式或代码块。 */
	return y /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func inspectionPDFTextWidth(value string, size float64) float64 { /* 定义 inspectionPDFTextWidth 函数。 */
	width := 0.0                      /* 更新 width 的值。 */
	for _, r := range []rune(value) { /* 循环处理当前数据。 */
		width += inspectionPDFRuneWidth(r, size) /* 更新 width 的值。 */
	} /* 结束当前表达式或代码块。 */
	return width /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func inspectionPDFRuneWidth(r rune, size float64) float64 { /* 定义 inspectionPDFRuneWidth 函数。 */
	if !inspectionPDFIsLatin(r) { /* 判断条件并选择处理分支。 */
		return size /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	switch r { /* 根据条件选择处理路径。 */
	case ' ', '\t': /* 处理当前分支。 */
		return size * 0.28 /* 返回当前处理结果。 */
	case 'i', 'l', 'I', '.', ',', ':', ';', '!', '|': /* 处理当前分支。 */
		return size * 0.28 /* 返回当前处理结果。 */
	case 'm', 'w', 'M', 'W': /* 处理当前分支。 */
		return size * 0.82 /* 返回当前处理结果。 */
	case '-', '_': /* 处理当前分支。 */
		return size * 0.38 /* 返回当前处理结果。 */
	default: /* 处理当前分支。 */
		return size * 0.54 /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func inspectionPDFIsLatin(r rune) bool { /* 定义 inspectionPDFIsLatin 函数。 */
	return r >= 0x20 && r <= 0x7e /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func inspectionPDFTextLiteral(value string) string { /* 定义 inspectionPDFTextLiteral 函数。 */
	value = strings.ReplaceAll(value, "\\", "\\\\") /* 更新 value 的值。 */
	value = strings.ReplaceAll(value, "(", "\\(")   /* 更新 value 的值。 */
	value = strings.ReplaceAll(value, ")", "\\)")   /* 更新 value 的值。 */
	return value                                    /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func inspectionPDFTextHex(value string) string { /* 定义 inspectionPDFTextHex 函数。 */
	var output strings.Builder /* 声明 output。 */
	var buffer [2]byte         /* 声明 buffer。 */
	for _, r := range value {  /* 循环处理当前数据。 */
		if r > 0xffff { /* 判断条件并选择处理分支。 */
			r = '?' /* 更新 r 的值。 */
		} /* 结束当前表达式或代码块。 */
		binary.BigEndian.PutUint16(buffer[:], uint16(r))       /* 执行当前语句并推进处理流程。 */
		fmt.Fprintf(&output, "%02X%02X", buffer[0], buffer[1]) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	return output.String() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func inspectionTime(value int64) string { /* 定义 inspectionTime 函数。 */
	if value <= 0 { /* 判断条件并选择处理分支。 */
		return "未记录" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return time.UnixMilli(value).In(time.Local).Format("2006-01-02 15:04:05") /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

type pdfObject struct { /* 定义 pdfObject 类型。 */
	body string /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type pdfDocument struct { /* 定义 pdfDocument 类型。 */
	objects []pdfObject /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (d *pdfDocument) add(body string) int { /* 定义 add 函数。 */
	d.objects = append(d.objects, pdfObject{body: body}) /* 更新 d.objects 的值。 */
	return len(d.objects)                                /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (d *pdfDocument) set(id int, body string) { /* 定义 set 函数。 */
	if id > 0 && id <= len(d.objects) { /* 判断条件并选择处理分支。 */
		d.objects[id-1].body = body /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (d *pdfDocument) write(root, info int) ([]byte, error) { /* 定义 write 函数。 */
	if len(d.objects) == 0 || root <= 0 || info <= 0 { /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("PDF has no root objects") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var output bytes.Buffer                      /* 声明 output。 */
	output.WriteString("%PDF-1.4\n")             /* 执行当前语句并推进处理流程。 */
	output.Write([]byte{0xe2, 0xe3, 0xcf, 0xd3}) /* 执行当前语句并推进处理流程。 */
	output.WriteByte('\n')                       /* 执行当前语句并推进处理流程。 */
	offsets := make([]int, len(d.objects)+1)     /* 更新 offsets 的值。 */
	for index, object := range d.objects {       /* 循环处理当前数据。 */
		offsets[index+1] = output.Len()                                      /* 执行当前语句并推进处理流程。 */
		fmt.Fprintf(&output, "%d 0 obj\n%s\nendobj\n", index+1, object.body) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	xref := output.Len()                                                        /* 更新 xref 的值。 */
	fmt.Fprintf(&output, "xref\n0 %d\n0000000000 65535 f \n", len(d.objects)+1) /* 执行当前语句并推进处理流程。 */
	for _, offset := range offsets[1:] {                                        /* 循环处理当前数据。 */
		fmt.Fprintf(&output, "%010d 00000 n \n", offset) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	fmt.Fprintf(&output, "trailer\n<< /Size %d /Root %d 0 R /Info %d 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(d.objects)+1, root, info, xref) /* 执行当前语句并推进处理流程。 */
	return output.Bytes(), nil                                                                                                              /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func pdfReferences(ids []int) string { /* 定义 pdfReferences 函数。 */
	refs := make([]string, 0, len(ids)) /* 更新 refs 的值。 */
	for _, id := range ids {            /* 循环处理当前数据。 */
		refs = append(refs, fmt.Sprintf("%d 0 R", id)) /* 更新 refs 的值。 */
	} /* 结束当前表达式或代码块。 */
	return strings.Join(refs, " ") /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
