package core

import (
	"fmt"
	"strings"
	"time"

	"iot-platform/internal/model"
)

// RenderDutyHandoverPDF renders a selected immutable revision. Confirmations
// are included only when they refer to that exact revision; later additions
// have a separate appendix and never replace the frozen facts.
func RenderDutyHandoverPDF(doc model.DutyDocument, revision model.DutyHandoverRevision, handover model.DutyHandover) ([]byte, error) {
	pages := []*inspectionPDFCanvas{}
	var page *inspectionPDFCanvas
	y := 0.0
	newPage := func() {
		page = &inspectionPDFCanvas{}
		pages = append(pages, page)
		page.mixedText(42, 799, "值班交接报告", 19, inspectionPDFNavy, "F3")
		page.mixedText(42, 779, fmt.Sprintf("固定版本 %d · %s", revision.Number, doc.ID), 8, inspectionPDFMuted, "F2")
		page.line(42, 769, 553, 769, inspectionPDFLine, .7)
		y = 747
	}
	line := func(text string, title bool) {
		size := 9.0
		color := inspectionPDFText
		font := "F2"
		if title {
			size = 11
			color = inspectionPDFNavy
			font = "F3"
			y -= 8
		}
		rows := inspectionPDFWrapText(text, size, 511)
		needed := float64(len(rows))*15 + 4
		if title {
			needed += 30
		}
		if y-needed < 52 && needed < 690 {
			newPage()
		}
		for _, row := range rows {
			if y < 52 {
				newPage()
			}
			page.mixedText(42, y, row, size, color, font)
			y -= 15
		}
		y -= 4
	}
	newPage()
	stamp := func(at int64) string {
		if at <= 0 {
			return "未记录"
		}
		return time.UnixMilli(at).In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05")
	}
	line("岗位 "+inspectionPDFFallback(revision.StationName, revision.StationID)+" · 交班负责人 "+inspectionPDFFallback(revision.LeaderID, inspectionPDFFallback(revision.AuthorID, "未记录"))+" · 接班负责人 "+inspectionPDFFallback(revision.NextLeaderID, "未记录"), false)
	line("事实窗口 "+stamp(revision.StartAt)+" 至 "+stamp(revision.Snapshot.CutoffAt)+"；共 "+fmt.Sprint(revision.EventTotal)+" 条事件。", false)
	line("校验摘要 "+revision.SnapshotHash, false)
	line("签署记录", true)
	confirmations := []model.DutyConfirmation{}
	confirmations = append(confirmations, handover.Confirmations...)
	if len(confirmations) == 0 {
		if handover.Submission != nil {
			c := *handover.Submission
			c.Type = "SUBMISSION"
			confirmations = append(confirmations, c)
		}
		if handover.Acceptance != nil {
			c := *handover.Acceptance
			c.Type = "ACCEPTANCE"
			confirmations = append(confirmations, c)
		}
	}
	for _, c := range confirmations {
		if c.RevisionID == doc.ID {
			name := "交班"
			if c.Type == "ACCEPTANCE" {
				name = "接班"
			}
			line(name+"人 "+c.UserID+"；时间 "+stamp(c.At)+"；确认摘要 "+c.SnapshotHash, false)
		}
	}
	for _, v := range []struct {
		name string
		c    *model.DutyConfirmation
	}{{"交班", handover.Submission}, {"接班", handover.Acceptance}} {
		found := false
		for _, c := range confirmations {
			if c.RevisionID == doc.ID && ((v.name == "交班" && c.Type == "SUBMISSION") || (v.name == "接班" && c.Type == "ACCEPTANCE")) {
				found = true
			}
		}
		if !found {
			line(v.name+"：此版本无确认记录。", false)
		}
	}
	line("当班统计", true)
	s := revision.Statistics
	line(fmt.Sprintf("新增告警 %d · 再次上报 %d · 确认 %d · 恢复 %d · 关闭 %d · 离线 %d · 上线 %d", s.NewAlarms, s.Reports, s.Acknowledged, s.Recovered, s.Closed, s.Offline, s.Online), false)
	line("人工交班说明", true)
	line(inspectionPDFFallback(revision.HumanNotes, "未填写"), false)
	line("活动告警与设备状态", true)
	for _, a := range revision.Snapshot.Alarms {
		line(fmt.Sprintf("%s · %s · %s · %s；最后上报 %s", a.DeviceName, dutyPDFLabel(a.AlarmType), dutyPDFLabel(a.AlarmLevel), dutyPDFLabel(a.Status), stamp(a.LastTriggeredAt)), false)
	}
	for _, s := range revision.Snapshot.States {
		line(fmt.Sprintf("设备 %s · 连接 %s · 数据 %s · %s", s.DeviceID, dutyPDFLabel(s.ConnectionStatus), dutyPDFLabel(s.DataStatus), s.Reason), false)
	}
	line("人工值班记录", true)
	for _, d := range revision.Records {
		v, e := model.DutyBody[model.DutyRecord](d)
		if e != nil {
			return nil, e
		}
		line(stamp(v.OccurredAt)+" · "+v.AuthorID+" · "+v.Content, false)
		if v.CorrectsID != "" {
			line("更正记录 "+v.CorrectsID+"；原因 "+v.CorrectionReason, false)
		}
		for _, a := range v.Attachments {
			line("附件 "+a.Name+" · "+fmt.Sprint(a.Size)+" 字节", false)
		}
	}
	line("跨班跟进事项", true)
	for _, d := range revision.Items {
		v, e := model.DutyBody[model.DutyItem](d)
		if e != nil {
			return nil, e
		}
		line(fmt.Sprintf("%s · %s · 负责人 %s · 截止 %s", v.Title, dutyPDFLabel(v.Status), v.OwnerID, stamp(v.DueAt)), false)
		line("下一步："+v.NextAction, false)
	}
	if revision.AI != nil {
		line("AI 整理（待人工复核）", true)
		line(revision.AI.Summary, false)
		for _, group := range []struct {
			name string
			list []model.DutyAIStatement
		}{{"重点", revision.AI.Highlights}, {"建议", revision.AI.Suggestions}, {"待核实", revision.AI.Missing}} {
			for _, v := range group.list {
				line(group.name+"："+v.Text+"；证据 "+strings.Join(v.EvidenceIDs, ", "), false)
			}
		}
	}
	line("本版本完整事件明细", true)
	for _, ev := range revision.Events {
		line(stamp(ev.OccurredAt)+" · "+dutyPDFLabel(ev.Type)+" · "+ev.DeviceID+" · "+ev.ActorID, false)
		line("事件 "+ev.ID+"；记录时间 "+stamp(ev.RecordedAt), false)
	}
	if len(handover.Amendments) > 0 {
		line("签署后追加记录（生成报告时的独立附录）", true)
		for _, v := range handover.Amendments {
			line(stamp(v.At)+" · "+v.ActorID+" · "+v.Content, false)
		}
	}
	for i, p := range pages {
		p.line(42, 36, 553, 36, inspectionPDFLine, .5)
		p.mixedText(42, 22, "TorchLink | 值班交接报告", 7.6, inspectionPDFMuted, "F2")
		p.rightText(553, 22, fmt.Sprintf("第 %d / %d 页", i+1, len(pages)), 7.6, inspectionPDFMuted, "F2")
	}
	d := &pdfDocument{}
	cid := d.add(`<< /Type /Font /Subtype /CIDFontType0 /BaseFont /STSong-Light /CIDSystemInfo << /Registry (Adobe) /Ordering (GB1) /Supplement 4 >> /DW 1000 >>`)
	cjk := d.add(fmt.Sprintf(`<< /Type /Font /Subtype /Type0 /BaseFont /STSong-Light /Encoding /UniGB-UCS2-H /DescendantFonts [%d 0 R] >>`, cid))
	latin := d.add(`<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>`)
	bold := d.add(`<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold /Encoding /WinAnsiEncoding >>`)
	tree := d.add("")
	catalog := d.add("")
	info := d.add(`<< /Title (Duty Handover Report) /Author (TorchLink) >>`)
	ids := []int{}
	for _, p := range pages {
		body := p.body.String()
		content := d.add(fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body))
		ids = append(ids, d.add(fmt.Sprintf("<< /Type /Page /Parent %d 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 %d 0 R /F2 %d 0 R /F3 %d 0 R >> >> /Contents %d 0 R >>", tree, cjk, latin, bold, content)))
	}
	d.set(tree, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", pdfReferences(ids), len(ids)))
	d.set(catalog, fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", tree))
	return d.write(catalog, info)
}

func dutyPDFLabel(value string) string {
	labels := map[string]string{"OPEN": "待处理", "IN_PROGRESS": "处理中", "PENDING_VERIFICATION": "待复核", "DONE": "已完成", "CANCELLED": "已取消", "ACTIVE": "活动", "ACKED": "已确认", "RECOVERED": "已恢复", "CLOSED": "已关闭", "CONNECTED": "已连接", "DISCONNECTED": "未连接", "SILENT": "无上报", "OFFLINE": "离线", "SUSPECTED_OFFLINE": "疑似离线", "FIRE": "火警", "CRITICAL": "严重", "HIGH": "高", "MEDIUM": "中", "LOW": "低", "INFO": "信息", "ALARM_CREATED": "新增告警", "ALARM_REPORTED": "再次上报", "ALARM_ACKNOWLEDGED": "告警确认", "ALARM_RECOVERED": "告警恢复", "ALARM_CLOSED": "告警关闭", "DEVICE_OFFLINE": "设备离线", "DEVICE_ONLINE": "设备恢复在线", "DEVICE_SUSPECTED_OFFLINE": "设备疑似离线", "DEVICE_STATUS_CHANGED": "设备状态变化"}
	if v, ok := labels[value]; ok {
		return v
	}
	return value
}
