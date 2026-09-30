package core

import (
	"bytes"
	"fmt"
	"os"
	"testing"

	"iot-platform/internal/model"
)

func TestDutyPDFImmutableRevisionAndLongContent(t *testing.T) {
	r := model.DutyHandoverRevision{StationID: "消防控制室", RunID: "run-a", HandoverID: "handover-a", Number: 3, StartAt: 1790787600000, Snapshot: model.DutySnapshot{CutoffAt: 1790798400000}, SnapshotHash: "65ab1100c54df6b591b9845458d81f39f61e2d1034cd2203262bf9483d6fa6a71", HumanNotes: "已与下一班说明设备离线情况，请持续跟进现场巡检。", Statistics: model.DutyStatistics{NewAlarms: 3, Reports: 5, Acknowledged: 2, Offline: 1}}
	for i := 0; i < 90; i++ {
		r.Records = append(r.Records, model.NewDutyDocument(model.DutyRecordKind, fmt.Sprintf("record-%d", i), model.DutyRecord{AuthorID: "值班员甲", Content: fmt.Sprintf("第%d条巡查记录：一楼烟感触发告警，现场复核及处置结果已登记。内容保持在固定版本，不随后续编辑变化。", i+1), OccurredAt: r.StartAt + int64(i)*60000}))
	}
	r.Events = []model.DutyBusinessEvent{{ID: "event-001", Type: "ALARM_CREATED", DeviceID: "smoke-01", ActorID: "system", OccurredAt: r.StartAt + 1000, RecordedAt: r.StartAt + 2000}}
	r.EventTotal = 1
	r.Items = []model.DutyDocument{model.NewDutyDocument(model.DutyItemKind, "item-1", model.DutyItem{Title: "现场复核离线设备", Status: "IN_PROGRESS", OwnerID: "值班员甲", NextAction: "携带检测工具复查供电及线路", DueAt: r.Snapshot.CutoffAt + 3600000})}
	doc := model.DutyDocument{ID: "revision-3"}
	h := model.DutyHandover{Submission: &model.DutyConfirmation{UserID: "值班员甲", At: r.Snapshot.CutoffAt, RevisionID: doc.ID, SnapshotHash: r.SnapshotHash}}
	data, e := RenderDutyHandoverPDF(doc, r, h)
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) || bytes.Count(data, []byte("/Type /Page ")) < 3 || !bytes.Contains(data, []byte("65ab1100")) {
		t.Fatal("invalid multipage fixed-revision PDF")
	}
	if path := os.Getenv("DUTY_PDF_SAMPLE"); path != "" {
		if e = os.WriteFile(path, data, 0600); e != nil {
			t.Fatal(e)
		}
	}
}
