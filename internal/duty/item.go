package duty

import (
	"strings"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func (s *Service) itemHistory(tx ports.DutyTx, a Actor, id string, v Item, typ, previous, content string) error {
	doc, e := create(tx, model.DutyItemEventKind, ItemEvent{StationID: v.StationID, RunID: v.RunID, ItemID: id, Type: typ, ActorID: a.Username, PreviousOwnerID: previous, OwnerID: v.OwnerID, Content: content, OccurredAt: s.now()})
	if e != nil {
		return e
	}
	return s.appendEvent(tx, a, "duty.item."+typ, doc, v.StationID, v.RunID, v.DeviceID, v.AlarmID, s.now())
}
func (s *Service) item(tx ports.DutyTx, a Actor, c Command, actors map[string]Actor) (any, error) {
	if c.Operation == "create" {
		v, e := decode[Item](c.Body)
		if e != nil {
			return nil, e
		}
		_, r, e := read[Run](tx, model.DutyRunKind, v.RunID)
		if e != nil {
			return nil, e
		}
		if !runMember(a, r) || !a.covers(r.DeviceIDs) || r.Status != "ACTIVE" {
			return nil, ErrForbidden
		}
		v.StationID = r.StationID
		v.OwnerID = strings.TrimSpace(v.OwnerID)
		if v.OwnerID == "" {
			v.OwnerID = r.LeaderID
		}
		if !contains(r.MemberIDs, v.OwnerID) {
			return nil, invalid("事项负责人须为当班成员")
		}
		if e = eligible(actors, v.OwnerID, r.DeviceIDs); e != nil {
			return nil, e
		}
		if strings.TrimSpace(v.Title) == "" || strings.TrimSpace(v.NextAction) == "" {
			return nil, invalid("事项名称及下一步动作不能为空")
		}
		if v.DeviceID == "" && v.AlarmID != "" {
			alarm, e := tx.Alarm(v.AlarmID)
			if e != nil {
				return nil, e
			}
			v.DeviceID = alarm.DeviceID
		}

		if e = validateLink(tx, v.DeviceID, v.AlarmID, r.DeviceIDs); e != nil {
			return nil, e
		}
		v.Status = "OPEN"
		v.CreatedBy = a.Username
		doc, e := create(tx, model.DutyItemKind, v)
		if e != nil {
			return nil, e
		}
		if e = s.itemHistory(tx, a, doc.ID, v, "created", "", v.NextAction); e != nil {
			return nil, e
		}
		if e = s.notify(tx, v.OwnerID, v.StationID, v.RunID, "ITEM_ASSIGNED", doc.ID, "有新的值班跟进事项"); e != nil {
			return nil, e
		}
		return doc, nil
	}
	doc, v, e := read[Item](tx, model.DutyItemKind, c.ID)
	if e != nil {
		return nil, e
	}
	if e = checkVersion(doc, c.ExpectedVersion); e != nil {
		return nil, e
	}
	_, r, e := read[Run](tx, model.DutyRunKind, v.RunID)
	if e != nil {
		return nil, e
	}
	_, st, e := read[Station](tx, model.DutyStationKind, v.StationID)
	if e != nil {
		return nil, e
	}
	if !a.covers(r.DeviceIDs) || (!runMember(a, r) && a.Username != st.SupervisorID) {
		return nil, ErrForbidden
	}
	if r.Status != "ACTIVE" && !a.Admin && a.Username != st.SupervisorID {
		return nil, invalid("当班已结束，事项由当前班次或主管接续")
	}
	if v.Status == "DONE" || v.Status == "CANCELLED" {
		return nil, invalid("已结束事项只能查看，不能覆盖处理历史")
	}
	previous := v.OwnerID
	eventContent := ""
	if c.Operation == "update" {
		next, e := decode[Item](c.Body)
		if e != nil {
			return nil, e
		}
		if strings.TrimSpace(next.Title) == "" || strings.TrimSpace(next.NextAction) == "" {
			return nil, invalid("事项名称及下一步动作不能为空")
		}
		v.Title = next.Title
		v.NextAction = next.NextAction
		v.DueAt = next.DueAt
	} else if c.Operation == "transfer" || c.Operation == "assign" {
		req, e := decode[struct {
			OwnerID    string `json:"ownerId"`
			Content    string `json:"content"`
			NextAction string `json:"nextAction"`
		}](c.Body)
		if e != nil {
			return nil, e
		}
		if !contains(r.MemberIDs, req.OwnerID) {
			return nil, invalid("接续负责人须为当前当班成员")
		}
		if e = eligible(actors, req.OwnerID, r.DeviceIDs); e != nil {
			return nil, e
		}
		v.OwnerID = req.OwnerID
		if strings.TrimSpace(req.NextAction) != "" {
			v.NextAction = req.NextAction
		}
		eventContent = req.Content
	} else if c.Operation == "complete" || c.Operation == "cancel" || c.Operation == "status" {
		req, e := decode[struct {
			Status     string `json:"status"`
			Result     string `json:"result"`
			Reason     string `json:"reason"`
			NextAction string `json:"nextAction"`
		}](c.Body)
		if e != nil {
			return nil, e
		}
		if c.Operation == "complete" {
			req.Status = "DONE"
		}
		if c.Operation == "cancel" {
			req.Status = "CANCELLED"
		}
		if !contains([]string{"OPEN", "IN_PROGRESS", "PENDING_VERIFICATION", "DONE", "CANCELLED"}, req.Status) {
			return nil, invalid("无效事项状态")
		}
		if req.Status == "DONE" && strings.TrimSpace(req.Result) == "" {
			return nil, invalid("完成事项必须填写处理结果")
		}
		if req.Status == "CANCELLED" && strings.TrimSpace(req.Reason) == "" {
			return nil, invalid("取消事项必须填写原因")
		}
		v.Status = req.Status
		v.Result = req.Result
		v.Reason = req.Reason
		if req.NextAction != "" {
			v.NextAction = req.NextAction
		}
		if v.Status == "DONE" || v.Status == "CANCELLED" {
			v.CompletedAt = s.now()
		}
	} else {
		return nil, invalid("未知事项操作")
	}
	saved, e := put(tx, doc, v, c.ExpectedVersion)
	if e != nil {
		return nil, e
	}
	content := v.NextAction
	if v.Result != "" {
		content = v.Result
	}
	if v.Reason != "" {
		content = v.Reason
	}
	if eventContent != "" {
		content = eventContent
	}
	if e = s.itemHistory(tx, a, doc.ID, v, c.Operation, previous, content); e != nil {
		return nil, e
	}
	return saved, nil
}
