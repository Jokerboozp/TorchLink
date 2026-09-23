package core /* 声明 core 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bytes"   /* 执行当前语句并推进处理流程。 */
	"strconv" /* 执行当前语句并推进处理流程。 */
	"testing" /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */

	"github.com/ledongthuc/pdf" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestRenderHealthInspectionPDF(t *testing.T) { /* 定义 TestRenderHealthInspectionPDF 函数。 */
	data, err := RenderHealthInspectionPDF(model.DeviceHealthReport{ /* 更新 err 的值。 */
		GeneratedAt: 1700000000000,                                                                                            /* 执行当前语句并推进处理流程。 */
		Summary:     "共检查 1 个设备。",                                                                                             /* 执行当前语句并推进处理流程。 */
		Counts:      map[string]int{"total": 1, "healthy": 0, "attention": 1, "critical": 0, "offline": 1, "activeAlarms": 1}, /* 执行当前语句并推进处理流程。 */
		Items: []model.DeviceHealthItem{{ /* 执行当前语句并推进处理流程。 */
			DeviceID: "device-001", DeviceName: "一号烟感", ProductID: "smoke", BusinessStatus: "OFFLINE", DataStatus: "STALE", ActiveAlarmCount: 1, Severity: "HIGH", Findings: []string{"设备已离线或疑似离线"}, /* 执行当前语句并推进处理流程。 */
		}}, /* 结束当前表达式或代码块。 */
	}) /* 结束当前表达式或代码块。 */
	if err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	for _, marker := range [][]byte{[]byte("%PDF-1.4"), []byte("/STSong-Light"), []byte("/Type /Page"), []byte("xref"), []byte("%%EOF")} { /* 循环处理当前数据。 */
		if !bytes.Contains(data, marker) { /* 判断条件并选择处理分支。 */
			t.Fatalf("PDF is missing marker %q", marker) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if len(data) < 500 { /* 判断条件并选择处理分支。 */
		t.Fatalf("PDF is unexpectedly small: %d bytes", len(data)) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data))) /* 更新 err 的值。 */
	if err != nil {                                                       /* 判断条件并选择处理分支。 */
		t.Fatalf("generated PDF cannot be parsed: %v", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if reader.NumPage() < 1 { /* 判断条件并选择处理分支。 */
		t.Fatalf("generated PDF has no pages") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestRenderHealthInspectionPDFStandardLayout(t *testing.T) { /* 定义 TestRenderHealthInspectionPDFStandardLayout 函数。 */
	report := model.DeviceHealthReport{ /* 更新 report 的值。 */
		TenantID:    "tenant-001",                                                                                              /* 执行当前语句并推进处理流程。 */
		GeneratedAt: 1700000000000,                                                                                             /* 执行当前语句并推进处理流程。 */
		Summary:     "本次巡检完成设备健康、数据新鲜度和活动告警核查。",                                                                                /* 执行当前语句并推进处理流程。 */
		AIAdvice:    "总体判断：设备整体可用。\n建议动作：优先处理离线设备并核查活动告警。\n数据局限：本报告基于平台最近一次上报快照。",                                              /* 执行当前语句并推进处理流程。 */
		Counts:      map[string]int{"total": 12, "healthy": 8, "attention": 4, "critical": 1, "offline": 2, "activeAlarms": 3}, /* 执行当前语句并推进处理流程。 */
		Warnings:    []string{"部分设备需要现场复核。"},                                                                                   /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	for index := 0; index < 12; index++ { /* 循环处理当前数据。 */
		report.Items = append(report.Items, model.DeviceHealthItem{ /* 更新 report.Items 的值。 */
			DeviceID:         "device-" + strconv.Itoa(index), /* 执行当前语句并推进处理流程。 */
			DeviceName:       "测试设备",                          /* 执行当前语句并推进处理流程。 */
			ProductID:        "smoke-detector",                /* 执行当前语句并推进处理流程。 */
			BusinessStatus:   "ONLINE",                        /* 执行当前语句并推进处理流程。 */
			DataStatus:       "FRESH",                         /* 执行当前语句并推进处理流程。 */
			LastSeenAt:       1700000000000,                   /* 执行当前语句并推进处理流程。 */
			ActiveAlarmCount: 0,                               /* 执行当前语句并推进处理流程。 */
			Severity:         "INFO",                          /* 执行当前语句并推进处理流程。 */
			Findings:         []string{"最近状态正常"},              /* 执行当前语句并推进处理流程。 */
		}) /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */

	data, err := RenderHealthInspectionPDF(report) /* 更新 err 的值。 */
	if err != nil {                                /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	for _, marker := range [][]byte{[]byte("/BaseFont /Helvetica"), []byte("/BaseFont /Helvetica-Bold"), []byte(" rg"), []byte(" re f")} { /* 循环处理当前数据。 */
		if !bytes.Contains(data, marker) { /* 判断条件并选择处理分支。 */
			t.Fatalf("standard PDF is missing layout marker %q", marker) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data))) /* 更新 err 的值。 */
	if err != nil {                                                       /* 判断条件并选择处理分支。 */
		t.Fatalf("standard PDF cannot be parsed: %v", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if reader.NumPage() < 2 { /* 判断条件并选择处理分支。 */
		t.Fatalf("standard PDF should contain overview and detail pages, got %d", reader.NumPage()) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
