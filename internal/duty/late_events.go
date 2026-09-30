package duty

import (
	"fmt"

	"github.com/google/uuid"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// A late event is an event committed after signing but whose occurrence belongs
// to the responsibility window of that signed handover. Append it visibly;
// neither its original timestamp nor the signed revision is rewritten.
func (s *Service) lateAmendments(tx ports.DutyTx, tenant string) error {
	docs, e := listAll(tx, model.DutyFilter{Kind: model.DutyHandoverKind, Status: "ACCEPTED"})
	if e != nil {
		return e
	}
	for _, doc := range docs {
		h, e := model.DutyBody[Handover](doc)
		if e != nil {
			return e
		}
		if h.Acceptance == nil || h.AcceptanceRevisionID == "" {
			continue
		}
		_, signed, e := read[Revision](tx, model.DutyRevisionKind, h.AcceptanceRevisionID)
		if e != nil {
			return e
		}
		events, e := eventsAll(tx, model.DutyFilter{DeviceIDs: h.DeviceIDs})
		if e != nil {
			return e
		}
		seen := map[string]bool{}
		for _, id := range signed.KnownEventIDs {
			seen[id] = true
		}
		for _, amend := range h.Amendments {
			for _, id := range amend.EventIDs {
				seen[id] = true
			}
		}
		changed := false
		for _, event := range events {
			if event.Source == "duty" || seen[event.ID] || event.OccurredAt >= h.Acceptance.At || event.OccurredAt < signed.StartAt {
				continue
			}
			text := fmt.Sprintf("平台在交接签收后收到此前事件：%s；设备 %s；发生时间 %d，入库时间 %d。请当前班次核实。", event.Type, event.DeviceID, event.OccurredAt, event.RecordedAt)
			h.Amendments = append(h.Amendments, model.DutyHandoverAmendment{ID: uuid.NewString(), ActorID: "system", At: s.now(), Content: text, EventIDs: []string{event.ID}})
			seen[event.ID] = true
			changed = true
			current, e := listAll(tx, model.DutyFilter{Kind: model.DutyRunKind, StationID: h.StationID, Status: "ACTIVE"})
			if e != nil {
				return e
			}
			for _, runDoc := range current {
				r, _ := model.DutyBody[Run](runDoc)
				if e = s.notifyKey(tx, r.LeaderID, h.StationID, runDoc.ID, "LATE_EVENT", doc.ID, "历史交接收到迟到事件，请核实补充", doc.ID+":"+event.ID); e != nil {
					return e
				}
			}
		}
		if changed {
			saved, e := put(tx, doc, h, doc.Version)
			if e != nil {
				return e
			}
			if e = s.appendEvent(tx, Actor{TenantID: tenant, Username: "system"}, "duty.handover.late-amendment", saved, h.StationID, h.RunID, "", "", s.now()); e != nil {
				return e
			}
		}
	}
	return nil
}
