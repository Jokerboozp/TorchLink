package core

import (
	"bytes"
	"strconv"
	"testing"

	"iot-platform/internal/model"

	"github.com/ledongthuc/pdf"
)

func TestRenderHealthInspectionPDF(t *testing.T) {
	data, err := RenderHealthInspectionPDF(model.DeviceHealthReport{
		GeneratedAt: 1700000000000,
		Summary:     "共检查 1 个设备。",
		Counts:      map[string]int{"total": 1, "healthy": 0, "attention": 1, "critical": 0, "offline": 1, "activeAlarms": 1},
		Items: []model.DeviceHealthItem{{
			DeviceID: "device-001", DeviceName: "一号烟感", ProductID: "smoke", BusinessStatus: "OFFLINE", DataStatus: "STALE", ActiveAlarmCount: 1, Severity: "HIGH", Findings: []string{"设备已离线或疑似离线"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range [][]byte{[]byte("%PDF-1.4"), []byte("/STSong-Light"), []byte("/Type /Page"), []byte("xref"), []byte("%%EOF")} {
		if !bytes.Contains(data, marker) {
			t.Fatalf("PDF is missing marker %q", marker)
		}
	}
	if len(data) < 500 {
		t.Fatalf("PDF is unexpectedly small: %d bytes", len(data))
	}
	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("generated PDF cannot be parsed: %v", err)
	}
	if reader.NumPage() < 1 {
		t.Fatalf("generated PDF has no pages")
	}
}

func TestRenderHealthInspectionPDFStandardLayout(t *testing.T) {
	report := model.DeviceHealthReport{
		TenantID:    "tenant-001",
		GeneratedAt: 1700000000000,
		Summary:     "本次巡检完成设备健康、数据新鲜度和活动告警核查。",
		AIAdvice:    "总体判断：设备整体可用。\n建议动作：优先处理离线设备并核查活动告警。\n数据局限：本报告基于平台最近一次上报快照。",
		Counts:      map[string]int{"total": 12, "healthy": 8, "attention": 4, "critical": 1, "offline": 2, "activeAlarms": 3},
		Warnings:    []string{"部分设备需要现场复核。"},
	}
	for index := 0; index < 12; index++ {
		report.Items = append(report.Items, model.DeviceHealthItem{
			DeviceID:         "device-" + strconv.Itoa(index),
			DeviceName:       "测试设备",
			ProductID:        "smoke-detector",
			BusinessStatus:   "ONLINE",
			DataStatus:       "FRESH",
			LastSeenAt:       1700000000000,
			ActiveAlarmCount: 0,
			Severity:         "INFO",
			Findings:         []string{"最近状态正常"},
		})
	}

	data, err := RenderHealthInspectionPDF(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range [][]byte{[]byte("/BaseFont /Helvetica"), []byte("/BaseFont /Helvetica-Bold"), []byte(" rg"), []byte(" re f")} {
		if !bytes.Contains(data, marker) {
			t.Fatalf("standard PDF is missing layout marker %q", marker)
		}
	}
	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("standard PDF cannot be parsed: %v", err)
	}
	if reader.NumPage() < 2 {
		t.Fatalf("standard PDF should contain overview and detail pages, got %d", reader.NumPage())
	}
}
