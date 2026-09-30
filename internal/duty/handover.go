package duty

import (
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type HandoverEdit struct {
	RunID        string `json:"runId"`
	NextRosterID string `json:"nextRosterId"`
	HumanNotes   string `json:"humanNotes"`
	RevisionID   string `json:"revisionId"`
	SnapshotHash string `json:"snapshotHash"`
	Reason       string `json:"reason"`
	Content      string `json:"content"`
	Note         string `json:"note"`
	UseKnowledge bool   `json:"useKnowledge"`
	InputLimit   int    `json:"inputLimit"`
}

func (s *Service) revision(tx ports.DutyTx, a Actor, hid string, h Handover, r Run, notes string, number int) (model.DutyDocument, error) {
	snapshot, e := tx.Snapshot(h.DeviceIDs)
	if e != nil {
		return model.DutyDocument{}, e
	}
	events, e := eventsAll(tx, model.DutyFilter{DeviceIDs: h.DeviceIDs, Start: r.StartedAt, End: snapshot.CutoffAt + 1})
	if e != nil {
		return model.DutyDocument{}, e
	}
	local, e := eventsAll(tx, model.DutyFilter{StationID: h.StationID, Start: r.StartedAt, End: snapshot.CutoffAt + 1})
	if e != nil {
		return model.DutyDocument{}, e
	}
	seen := map[string]bool{}
	all := []model.DutyBusinessEvent{}
	for _, ev := range append(events, local...) {
		if !seen[ev.ID] {
			all = append(all, ev)
			seen[ev.ID] = true
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Seq < all[j].Seq })
	records, e := listAll(tx, model.DutyFilter{Kind: model.DutyRecordKind, RunID: h.RunID})
	if e != nil {
		return model.DutyDocument{}, e
	}
	items, e := listAll(tx, model.DutyFilter{Kind: model.DutyItemKind, StationID: h.StationID})
	if e != nil {
		return model.DutyDocument{}, e
	}
	unfinished := []model.DutyDocument{}
	for _, doc := range items {
		v, _ := model.DutyBody[Item](doc)
		if v.Status != "DONE" && v.Status != "CANCELLED" {
			unfinished = append(unfinished, doc)
		}
	}
	v := Revision{StationID: h.StationID, RunID: h.RunID, HandoverID: hid, Number: number, AuthorID: a.Username, StartAt: r.StartedAt, Snapshot: snapshot, Records: records, Items: unfinished, HumanNotes: notes, Events: all, EventTotal: len(all), Frozen: true, Evidence: []model.DutyEvidence{}}
	_, station, e := read[Station](tx, model.DutyStationKind, h.StationID)
	if e != nil {
		return model.DutyDocument{}, e
	}
	_, next, e := read[Roster](tx, model.DutyRosterKind, h.NextRosterID)
	if e != nil {
		return model.DutyDocument{}, e
	}
	v.StationName = station.Name
	v.LeaderID = r.LeaderID
	v.NextLeaderID = next.LeaderID
	v.NextRosterID = h.NextRosterID
	if strings.TrimSpace(notes) != "" {
		v.Evidence = append(v.Evidence, model.DutyEvidence{ID: fmt.Sprintf("notes:%s:%d", hid, number), Type: "notes", Label: "交班人补充说明", Content: notes, At: s.now()})
	}
	v.SnapshotHash, e = factsHash(tx, h, snapshot, records, unfinished)
	if e != nil {
		return model.DutyDocument{}, e
	}
	for _, ev := range all {
		switch ev.Type {
		case "ALARM_CREATED":
			v.Statistics.NewAlarms++
			v.Statistics.Reports++
		case "ALARM_REPORTED":
			v.Statistics.Reports++
		case "ALARM_ACKNOWLEDGED":
			v.Statistics.Acknowledged++
		case "ALARM_RECOVERED":
			v.Statistics.Recovered++
		case "ALARM_CLOSED":
			v.Statistics.Closed++
		case "DEVICE_OFFLINE":
			v.Statistics.Offline++
		case "DEVICE_ONLINE":
			v.Statistics.Online++
		}
		v.Evidence = append(v.Evidence, model.DutyEvidence{ID: ev.ID, Type: "event", DeviceID: ev.DeviceID, Label: ev.Type, Content: string(ev.Body), At: ev.OccurredAt})
	}
	for _, alarm := range snapshot.Alarms {
		v.Evidence = append(v.Evidence, model.DutyEvidence{ID: "alarm:" + alarm.ID, Type: "alarm", DeviceID: alarm.DeviceID, Label: alarm.DeviceName + " " + alarm.AlarmType, Content: fmt.Sprintf("状态%s 等级%s", alarm.Status, alarm.AlarmLevel), At: alarm.LastTriggeredAt})
	}
	for _, state := range snapshot.States {
		if state.BusinessStatus == "OFFLINE" || state.BusinessStatus == "SUSPECTED_OFFLINE" {
			label := "设备离线"
			if state.BusinessStatus == "SUSPECTED_OFFLINE" {
				label = "设备疑似离线"
			}
			v.Evidence = append(v.Evidence, model.DutyEvidence{ID: "state:" + state.DeviceID, Type: "state", DeviceID: state.DeviceID, Label: label, Content: state.Reason, At: state.OfflineDetectedAt})
		}
	}
	for _, doc := range records {
		record, _ := model.DutyBody[Record](doc)
		v.Evidence = append(v.Evidence, model.DutyEvidence{ID: doc.ID, Type: "record", DeviceID: record.DeviceID, Label: "当班人工记录", Content: record.Content, At: record.OccurredAt})
	}
	for _, doc := range unfinished {
		item, _ := model.DutyBody[Item](doc)
		v.Evidence = append(v.Evidence, model.DutyEvidence{ID: doc.ID, Type: "item", DeviceID: item.DeviceID, Label: item.Title, Content: item.NextAction, At: doc.UpdatedAt})
	}
	known, e := eventsAll(tx, model.DutyFilter{DeviceIDs: h.DeviceIDs})
	if e != nil {
		return model.DutyDocument{}, e
	}
	for _, ev := range known {
		if ev.Source != "duty" {
			v.KnownEventIDs = append(v.KnownEventIDs, ev.ID)
		}
	}

	return create(tx, model.DutyRevisionKind, v)
}
func factHash(snapshot model.DutySnapshot, records, items []model.DutyDocument) string {
	snapshot.CutoffAt = 0
	snapshot.EventSeq = 0
	sort.Slice(snapshot.Alarms, func(i, j int) bool { return snapshot.Alarms[i].ID < snapshot.Alarms[j].ID })
	sort.Slice(snapshot.States, func(i, j int) bool { return snapshot.States[i].DeviceID < snapshot.States[j].DeviceID })
	sort.Slice(snapshot.Devices, func(i, j int) bool { return snapshot.Devices[i].ID < snapshot.Devices[j].ID })
	sort.Slice(records, func(i, j int) bool { return records[i].ID < records[j].ID })
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return digest(struct {
		Snapshot       model.DutySnapshot
		Records, Items []model.DutyDocument
	}{snapshot, records, items})
}
func factsHash(tx ports.DutyTx, h Handover, snapshot model.DutySnapshot, records, items []model.DutyDocument) (string, error) {
	events, err := eventsAll(tx, model.DutyFilter{DeviceIDs: h.DeviceIDs})
	if err != nil {
		return "", err
	}
	var markers []string
	for _, ev := range events {
		if ev.Source != "duty" {
			markers = append(markers, fmt.Sprintf("%d:%s:%s:%d", ev.Seq, ev.ID, ev.Type, ev.ResourceVersion))
		}
	}
	sort.Strings(markers)
	return digest([]any{factHash(snapshot, records, items), markers}), nil
}
func (s *Service) latestFacts(tx ports.DutyTx, h Handover) (model.DutySnapshot, []model.DutyDocument, []model.DutyDocument, error) {
	sn, e := tx.Snapshot(h.DeviceIDs)
	if e != nil {
		return sn, nil, nil, e
	}
	records, e := listAll(tx, model.DutyFilter{Kind: model.DutyRecordKind, RunID: h.RunID})
	if e != nil {
		return sn, nil, nil, e
	}
	items, e := listAll(tx, model.DutyFilter{Kind: model.DutyItemKind, StationID: h.StationID})
	if e != nil {
		return sn, nil, nil, e
	}
	open := []model.DutyDocument{}
	for _, d := range items {
		i, _ := model.DutyBody[Item](d)
		if i.Status != "DONE" && i.Status != "CANCELLED" {
			open = append(open, d)
		}
	}
	return sn, records, open, nil
}

func (s *Service) handover(tx ports.DutyTx, a Actor, c Command, actors map[string]Actor) (any, error) {
	b, e := decode[HandoverEdit](c.Body)
	if e != nil {
		return nil, e
	}
	if c.Operation == "create" {
		_, r, e := read[Run](tx, model.DutyRunKind, b.RunID)
		if e != nil {
			return nil, e
		}
		if r.Status != "ACTIVE" || a.Username != r.LeaderID || !a.covers(r.DeviceIDs) {
			return nil, ErrForbidden
		}
		_, next, e := read[Roster](tx, model.DutyRosterKind, b.NextRosterID)
		if e != nil {
			return nil, e
		}
		if next.Status != "PUBLISHED" || next.StationID != r.StationID || next.EndAt <= s.now() {
			return nil, invalid("接班排班须为同岗位未结束的已发布班次")
		}
		existing, e := listAll(tx, model.DutyFilter{Kind: model.DutyHandoverKind, RunID: b.RunID})
		if e != nil {
			return nil, e
		}
		for _, d := range existing {
			v, _ := model.DutyBody[Handover](d)
			if v.Status != "VOID" {
				return nil, invalid("本班已有交接单，请继续编辑")
			}
		}
		h := Handover{StationID: r.StationID, RunID: b.RunID, NextRosterID: b.NextRosterID, DeviceIDs: r.DeviceIDs, Status: "DRAFT", RevisionNumber: 1}
		id := uuid.NewString()
		rev, e := s.revision(tx, a, id, h, r, b.HumanNotes, 1)
		if e != nil {
			return nil, e
		}
		h.CurrentRevisionID = rev.ID
		doc, e := tx.Put(model.NewDutyDocument(model.DutyHandoverKind, id, h), 0)
		if e != nil {
			return nil, e
		}
		return doc, s.appendEvent(tx, a, "duty.handover.created", doc, r.StationID, b.RunID, "", "", s.now())
	}
	doc, h, e := read[Handover](tx, model.DutyHandoverKind, c.ID)
	if e != nil {
		return nil, e
	}
	if !a.covers(h.DeviceIDs) {
		return nil, ErrForbidden
	}
	if e = checkVersion(doc, c.ExpectedVersion); e != nil {
		return nil, e
	}
	runDoc, r, e := read[Run](tx, model.DutyRunKind, h.RunID)
	if e != nil {
		return nil, e
	}
	_, next, e := read[Roster](tx, model.DutyRosterKind, h.NextRosterID)
	if e != nil {
		return nil, e
	}
	_, st, e := read[Station](tx, model.DutyStationKind, h.StationID)
	if e != nil {
		return nil, e
	}
	if c.Operation == "return" || c.Operation == "accept" {
		if a.Username != next.LeaderID {
			return nil, ErrForbidden
		}
	} else if c.Operation == "amend" {
		if !runMember(a, r) && !contains(next.MemberIDs, a.Username) && a.Username != st.SupervisorID && !a.Admin {
			return nil, ErrForbidden
		}
	} else if a.Username != r.LeaderID {
		return nil, ErrForbidden
	}
	if c.Operation == "start-ai" {
		return s.startJob(tx, a, h, c.ID, b)
	}
	if c.Operation == "update" {
		if h.Status != "DRAFT" && h.Status != "RETURNED" {
			return nil, invalid("已提交或接收的交接不能覆盖，请退回或追加补充")
		}
		if b.NextRosterID != "" && b.NextRosterID != h.NextRosterID {
			_, updated, e := read[Roster](tx, model.DutyRosterKind, b.NextRosterID)
			if e != nil {
				return nil, e
			}
			if updated.StationID != h.StationID || updated.Status != "PUBLISHED" || updated.EndAt <= s.now() {
				return nil, invalid("接班排班须为同岗位未结束的已发布班次")
			}
			h.NextRosterID = b.NextRosterID
		}
		rev, e := s.revision(tx, a, c.ID, h, r, b.HumanNotes, h.RevisionNumber+1)
		if e != nil {
			return nil, e
		}
		h.CurrentRevisionID = rev.ID
		h.RevisionNumber++
		h.Status = "DRAFT"
	} else if c.Operation == "submit" {
		if h.Status != "DRAFT" && h.Status != "RETURNED" {
			return nil, invalid("交接单不在可提交状态")
		}
		if r.Status != "ACTIVE" {
			return nil, invalid("原值班已结束")
		}
		if e = s.rosterChecksForStart(tx, next, actors); e != nil {
			return nil, e
		}
		_, rev, e := read[Revision](tx, model.DutyRevisionKind, h.CurrentRevisionID)
		if e != nil {
			return nil, e
		}
		snapshot, records, items, e := s.latestFacts(tx, h)
		if e != nil {
			return nil, e
		}
		hash, e := factsHash(tx, h, snapshot, records, items)
		if e != nil {
			return nil, e
		}
		if b.RevisionID != h.CurrentRevisionID || b.SnapshotHash != rev.SnapshotHash || hash != rev.SnapshotHash {
			return nil, ErrConflict
		}
		h.Submission = &model.DutyConfirmation{UserID: a.Username, At: s.now(), RevisionID: h.CurrentRevisionID, SnapshotHash: hash, IdempotencyKey: c.IdempotencyKey, Note: b.Note}
		h.Submission.Type = "SUBMISSION"
		h.Confirmations = append(h.Confirmations, *h.Submission)
		h.Status = "SUBMITTED"
		if e = s.notify(tx, next.LeaderID, h.StationID, h.RunID, "HANDOVER_PENDING", doc.ID, "有值班交接待确认接收"); e != nil {
			return nil, e
		}
	} else if c.Operation == "return" {
		if h.Status != "SUBMITTED" {
			return nil, invalid("只有已提交交接可退回")
		}
		if strings.TrimSpace(b.Reason) == "" {
			return nil, invalid("退回须填写原因")
		}
		h.Status = "RETURNED"
		h.ReturnReason = b.Reason
		if e = s.notify(tx, r.LeaderID, h.StationID, h.RunID, "HANDOVER_RETURNED", doc.ID, "交接单已退回，请完善内容"); e != nil {
			return nil, e
		}
	} else if c.Operation == "void" {
		if h.Status == "ACCEPTED" {
			return nil, invalid("已接收交接不可作废")
		}
		if strings.TrimSpace(b.Reason) == "" {
			return nil, invalid("作废须填写原因")
		}
		h.Status = "VOID"
		h.VoidReason = b.Reason
	} else if c.Operation == "amend" {
		if strings.TrimSpace(b.Content) == "" {
			return nil, invalid("补充内容不能为空")
		}
		h.Amendments = append(h.Amendments, model.DutyHandoverAmendment{ID: uuid.NewString(), ActorID: a.Username, At: s.now(), Content: b.Content})
	} else if c.Operation == "accept" {
		if h.Status != "SUBMITTED" || h.Submission == nil || r.Status != "ACTIVE" {
			return nil, invalid("交接已接收、未提交或原班已结束")
		}
		if b.RevisionID != h.CurrentRevisionID {
			return nil, ErrConflict
		}
		if e = s.rosterChecksForStart(tx, next, actors); e != nil {
			return nil, e
		}
		if e = eligible(actors, a.Username, h.DeviceIDs); e != nil {
			return nil, e
		}
		if e = s.actualConflicts(tx, next.MemberIDs, h.StationID); e != nil {
			return nil, e
		}
		snapshot, records, items, e := s.latestFacts(tx, h)
		if e != nil {
			return nil, e
		}
		hash, e := factsHash(tx, h, snapshot, records, items)
		if e != nil {
			return nil, e
		}
		if b.SnapshotHash == "" || b.SnapshotHash != hash {
			return nil, ErrConflict
		}
		_, submittedRevision, e := read[Revision](tx, model.DutyRevisionKind, h.CurrentRevisionID)
		if e != nil {
			return nil, e
		}
		acceptedFacts, e := s.revision(tx, a, c.ID, h, r, submittedRevision.HumanNotes, h.RevisionNumber+1)
		if e != nil {
			return nil, e
		}
		h.AcceptanceRevisionID = acceptedFacts.ID
		h.RevisionNumber++

		at := s.now()
		runs, e := listAll(tx, model.DutyFilter{Kind: model.DutyRunKind, StationID: h.StationID})
		if e != nil {
			return nil, e
		}
		var toDoc model.DutyDocument
		to := Run{}
		for _, d := range runs {
			v, _ := model.DutyBody[Run](d)
			if v.Status == "ACTIVE" && d.ID != runDoc.ID {
				return nil, ErrConflict
			}
			if v.RosterID == h.NextRosterID {
				toDoc = d
				to = v
			}
		}
		if to.Status == "ENDED" || to.Status == "ACTIVE" {
			return nil, invalid("接班班次已执行")
		}
		if toDoc.ID == "" {
			to = Run{RosterID: h.NextRosterID, StationID: h.StationID, MemberIDs: next.MemberIDs, LeaderID: next.LeaderID, DeviceIDs: next.DeviceIDs, ScopeVersion: next.ScopeVersion, Attendance: []Attendance{}}
		} else {
			to.MemberIDs = append([]string{}, next.MemberIDs...)
			to.LeaderID = next.LeaderID
			to.DeviceIDs = append([]string{}, next.DeviceIDs...)
			to.ScopeVersion = next.ScopeVersion
		}
		arrived := 0
		for _, n := range to.MemberIDs {
			for _, att := range to.Attendance {
				if att.UserID == n && att.LeftAt == 0 {
					arrived++
					break
				}
			}
		}
		if arrived < st.RequiredPeople {
			return nil, invalid("到岗人数不足，请当班人员先记录到岗")
		}
		leaderArrived := false
		for _, att := range to.Attendance {
			if att.UserID == a.Username && att.LeftAt == 0 {
				leaderArrived = true
			}
		}
		if !leaderArrived {
			return nil, invalid("接班负责人须先到岗")
		}
		to.Status = "ACTIVE"
		to.StartedAt = at
		to.StartReason = "正式交接"
		to.PreviousRunID = h.RunID
		to.IncomingHandoverID = c.ID
		to.Baseline = &snapshot
		if toDoc.ID == "" {
			toDoc, e = create(tx, model.DutyRunKind, to)
		} else {
			toDoc, e = put(tx, toDoc, to, toDoc.Version)
		}
		if e != nil {
			return nil, e
		}
		r.Status = "ENDED"
		r.EndedAt = at
		r.EndReason = "正式交接"
		for i := range r.Attendance {
			if r.Attendance[i].LeftAt == 0 {
				r.Attendance[i].LeftAt = at
			}
		}
		if _, e = put(tx, runDoc, r, runDoc.Version); e != nil {
			return nil, e
		}
		for _, itemDoc := range items {
			it, _ := model.DutyBody[Item](itemDoc)
			oldOwner := it.OwnerID
			it.RunID = toDoc.ID
			it.OwnerID = to.LeaderID
			saved, e := put(tx, itemDoc, it, itemDoc.Version)
			if e != nil {
				return nil, e
			}
			if e = s.itemHistory(tx, a, saved.ID, it, "handover", oldOwner, it.NextAction); e != nil {
				return nil, e
			}
		}
		h.Status = "ACCEPTED"
		h.NextRunID = toDoc.ID
		h.AcceptanceSnapshot = &snapshot
		h.Acceptance = &model.DutyConfirmation{UserID: a.Username, At: at, RevisionID: h.AcceptanceRevisionID, SnapshotHash: hash, IdempotencyKey: c.IdempotencyKey, Note: b.Note}
		h.Acceptance.Type = "ACCEPTANCE"
		h.Confirmations = append(h.Confirmations, *h.Acceptance)
		if e = s.notify(tx, r.LeaderID, h.StationID, h.RunID, "HANDOVER_ACCEPTED", doc.ID, "值班交接已确认接收"); e != nil {
			return nil, e
		}
	} else {
		return nil, invalid("未知交接操作")
	}
	saved, e := put(tx, doc, h, c.ExpectedVersion)
	if e != nil {
		return nil, e
	}
	return saved, s.appendEvent(tx, a, "duty.handover."+c.Operation, saved, h.StationID, h.RunID, "", "", s.now())
}

func (s *Service) startJob(tx ports.DutyTx, a Actor, h Handover, hid string, b HandoverEdit) (any, error) {
	if b.UseKnowledge && !a.Admin && !contains(a.Permissions, "*") && !contains(a.Permissions, "menu:knowledge") {
		return nil, invalid("启用知识检索需要知识库访问权限")
	}
	if h.Status != "DRAFT" && h.Status != "RETURNED" {
		return nil, invalid("仅编辑中的交接可以AI整理")
	}
	docs, e := listAll(tx, model.DutyFilter{Kind: model.DutyAIJobKind, HandoverID: hid})
	if e != nil {
		return nil, e
	}
	for _, d := range docs {
		j, _ := model.DutyBody[AIJob](d)
		if j.RevisionID == h.CurrentRevisionID && (j.Status == "QUEUED" || j.Status == "RUNNING") {
			return nil, invalid("该交接版本已有生成任务")
		}
	}
	limit := b.InputLimit
	if limit <= 0 || limit > 300 {
		limit = 100
	}
	return create(tx, model.DutyAIJobKind, AIJob{StationID: h.StationID, RunID: h.RunID, HandoverID: hid, RevisionID: h.CurrentRevisionID, RequesterID: a.Username, DeviceIDs: h.DeviceIDs, Permissions: a.Permissions, SessionVersion: a.SessionVersion, AccessVersion: a.AccessVersion, ManagedUser: a.ManagedUser, Status: "QUEUED", Stage: "等待执行", WorkflowID: "duty-handover", InputLimit: limit, UseKnowledge: b.UseKnowledge})
}
