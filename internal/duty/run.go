package duty

import (
	"encoding/json"
	"strings"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func runMember(a Actor, r Run) bool { return a.Admin || contains(r.MemberIDs, a.Username) }
func (s *Service) run(tx ports.DutyTx, a Actor, c Command, actors map[string]Actor) (any, error) {
	if c.Operation == "arrive" || c.Operation == "open" {
		req, e := decode[struct {
			RosterID string `json:"rosterId"`
			Reason   string `json:"reason"`
		}](c.Body)
		if e != nil {
			return nil, e
		}
		_, roster, e := read[Roster](tx, model.DutyRosterKind, req.RosterID)
		if e != nil {
			return nil, e
		}
		_, st, e := read[Station](tx, model.DutyStationKind, roster.StationID)
		if e != nil {
			return nil, e
		}
		if roster.Status != "PUBLISHED" {
			return nil, invalid("只有已发布排班可以到岗或开班")
		}
		if !a.covers(roster.DeviceIDs) {
			return nil, ErrForbidden
		}
		if e = eligible(actors, a.Username, roster.DeviceIDs); e != nil {
			return nil, e
		}
		if !contains(roster.MemberIDs, a.Username) && a.Username != st.SupervisorID && !a.Admin {
			return nil, ErrForbidden
		}
		docs, e := listAll(tx, model.DutyFilter{Kind: model.DutyRunKind, StationID: roster.StationID})
		if e != nil {
			return nil, e
		}
		var doc model.DutyDocument
		var r Run
		for _, d := range docs {
			v, _ := model.DutyBody[Run](d)
			if v.RosterID == req.RosterID {
				doc = d
				r = v
			}
		}
		if doc.ID == "" {
			r = Run{RosterID: req.RosterID, StationID: roster.StationID, MemberIDs: append([]string{}, roster.MemberIDs...), LeaderID: roster.LeaderID, DeviceIDs: append([]string{}, roster.DeviceIDs...), ScopeVersion: roster.ScopeVersion, Status: "WAITING", Attendance: []Attendance{}}
		} else {
			if r.Status == "ENDED" {
				return nil, invalid("该班实际值班已经结束")
			}
			if c.ExpectedVersion > 0 && c.ExpectedVersion != doc.Version {
				return nil, ErrConflict
			}
			if r.Status == "WAITING" {
				r.MemberIDs = append([]string{}, roster.MemberIDs...)
				r.LeaderID = roster.LeaderID
				r.DeviceIDs = append([]string{}, roster.DeviceIDs...)
				r.ScopeVersion = roster.ScopeVersion
			}
		}
		if c.Operation == "arrive" {
			if !contains(r.MemberIDs, a.Username) {
				return nil, invalid("非当班人员不能记录本人到岗")
			}
			for _, at := range r.Attendance {
				if at.UserID == a.Username && at.LeftAt == 0 {
					return doc, nil
				}
			}
			r.Attendance = append(r.Attendance, Attendance{UserID: a.Username, ArrivedAt: s.now()})
		} else {
			if a.Username != st.SupervisorID && !a.Admin {
				return nil, ErrForbidden
			}
			if strings.TrimSpace(req.Reason) == "" {
				return nil, invalid("首次开班或空岗续班须记录原因")
			}
			if r.Status == "ACTIVE" {
				return nil, invalid("班次已经开班")
			}
			for _, d := range docs {
				v, _ := model.DutyBody[Run](d)
				if v.Status == "ACTIVE" {
					return nil, invalid("岗位仍有实际值班，应通过交接接班")
				}
			}
			if s.now() < roster.StartAt {
				return nil, invalid("计划开始时间未到，不能提前首次开班")
			}
			if e = s.rosterChecksForStart(tx, roster, actors); e != nil {
				return nil, e
			}
			if e = s.actualConflicts(tx, r.MemberIDs, r.StationID); e != nil {
				return nil, e
			}
			arrived, leaderArrived := 0, false
			for _, n := range r.MemberIDs {
				for _, att := range r.Attendance {
					if att.UserID == n && att.LeftAt == 0 {
						arrived++
						if n == r.LeaderID {
							leaderArrived = true
						}
						break
					}
				}
			}
			if arrived < st.RequiredPeople || !leaderArrived {
				return nil, invalid("开班前须满足到岗人数且负责人已到岗")
			}
			r.Status = "ACTIVE"
			r.StartedAt = s.now()
			r.StartReason = req.Reason
			snapshot, e := tx.Snapshot(r.DeviceIDs)
			if e != nil {
				return nil, e
			}
			r.Baseline = &snapshot
		}
		var saved model.DutyDocument
		if doc.ID == "" {
			saved, e = create(tx, model.DutyRunKind, r)
		} else {
			saved, e = put(tx, doc, r, doc.Version)
		}
		if e != nil {
			return nil, e
		}
		return saved, s.appendEvent(tx, a, "duty.run."+c.Operation, saved, r.StationID, saved.ID, "", "", s.now())
	}
	doc, r, e := read[Run](tx, model.DutyRunKind, c.ID)
	if e != nil {
		return nil, e
	}
	if e = checkVersion(doc, c.ExpectedVersion); e != nil {
		return nil, e
	}
	_, st, e := read[Station](tx, model.DutyStationKind, r.StationID)
	if e != nil {
		return nil, e
	}
	if !a.covers(r.DeviceIDs) || (a.Username != st.SupervisorID && !a.Admin && a.Username != r.LeaderID) {
		return nil, ErrForbidden
	}
	if r.Status != "ACTIVE" {
		return nil, invalid("仅实际在岗班次可以代班或结束")
	}
	if c.Operation == "substitute" {
		req, e := decode[struct {
			UserID        string `json:"userId"`
			ReplacementID string `json:"replacementId"`
			Reason        string `json:"reason"`
			LeaderID      string `json:"leaderId"`
		}](c.Body)
		if e != nil {
			return nil, e
		}
		if !contains(r.MemberIDs, req.UserID) || req.ReplacementID == "" || req.UserID == req.ReplacementID || contains(r.MemberIDs, req.ReplacementID) || strings.TrimSpace(req.Reason) == "" {
			return nil, invalid("代班人员或原因无效")
		}
		if e = eligible(actors, req.ReplacementID, r.DeviceIDs); e != nil {
			return nil, e
		}
		if e = s.actualConflicts(tx, []string{req.ReplacementID}, r.StationID); e != nil {
			return nil, e
		}
		for i, n := range r.MemberIDs {
			if n == req.UserID {
				r.MemberIDs[i] = req.ReplacementID
			}
		}
		for i, at := range r.Attendance {
			if at.UserID == req.UserID && at.LeftAt == 0 {
				r.Attendance[i].LeftAt = s.now()
				r.Attendance[i].Reason = req.Reason
			}
		}
		r.Attendance = append(r.Attendance, Attendance{UserID: req.ReplacementID, ArrivedAt: s.now(), Reason: req.Reason})
		if req.LeaderID != "" {
			if !contains(r.MemberIDs, req.LeaderID) {
				return nil, invalid("负责人必须为实际当班人员")
			}
			r.LeaderID = req.LeaderID
		} else if r.LeaderID == req.UserID {
			r.LeaderID = req.ReplacementID
		}
		if e = s.notify(tx, req.ReplacementID, r.StationID, c.ID, "SUBSTITUTE", c.ID, "已安排代班，请核对当班事项"); e != nil {
			return nil, e
		}
	} else if c.Operation == "end" {
		if a.Username != st.SupervisorID && !a.Admin {
			return nil, ErrForbidden
		}
		req, e := decode[struct {
			Reason string `json:"reason"`
		}](c.Body)
		if e != nil {
			return nil, e
		}
		if strings.TrimSpace(req.Reason) == "" {
			return nil, invalid("异常结束须填写空岗或结束原因")
		}
		r.Status = "ENDED"
		r.EndedAt = s.now()
		r.EndReason = req.Reason
		for i := range r.Attendance {
			if r.Attendance[i].LeftAt == 0 {
				r.Attendance[i].LeftAt = r.EndedAt
			}
		}
		if e = s.notify(tx, st.SupervisorID, r.StationID, c.ID, "STATION_VACANT", c.ID, "岗位实际值班已结束，需安排接续"); e != nil {
			return nil, e
		}
	} else {
		return nil, invalid("未知实际值班操作")
	}
	saved, e := put(tx, doc, r, c.ExpectedVersion)
	if e != nil {
		return nil, e
	}
	return saved, s.appendEvent(tx, a, "duty.run."+c.Operation, saved, r.StationID, c.ID, "", "", s.now())
}
func (s *Service) actualConflicts(tx ports.DutyTx, members []string, station string) error {
	docs, e := listAll(tx, model.DutyFilter{Kind: model.DutyRunKind, Status: "ACTIVE"})
	if e != nil {
		return e
	}
	for _, d := range docs {
		r, _ := model.DutyBody[Run](d)
		if r.StationID == station {
			continue
		}
		for _, n := range members {
			if contains(r.MemberIDs, n) {
				return invalid("人员 %s 正在其他岗位实际值守", n)
			}
		}
	}
	return nil
}
func (s *Service) rosterChecksForStart(tx ports.DutyTx, r Roster, actors map[string]Actor) error {
	if r.Status != "PUBLISHED" {
		return invalid("接班排班尚未发布或已取消")
	}
	_, st, e := read[Station](tx, model.DutyStationKind, r.StationID)
	if e != nil {
		return e
	}
	if !st.Enabled || digest(r.DeviceIDs) != digest(st.DeviceIDs) || r.ScopeVersion != st.ScopeVersion {
		return invalid("岗位已停用或设备范围已变化，请重新发布排班")
	}
	if len(r.MemberIDs) < st.RequiredPeople {
		return invalid("人员数量不足")
	}
	for _, n := range r.MemberIDs {
		if e = eligible(actors, n, r.DeviceIDs); e != nil {
			return e
		}
	}
	return nil
}
func validateLink(tx ports.DutyTx, device, alarm string, ids []string) error {
	if device != "" && !contains(ids, device) {
		return ErrForbidden
	}
	if alarm == "" {
		return nil
	}
	fact, e := tx.Alarm(alarm)
	if e != nil {
		return e
	}
	if !contains(ids, fact.DeviceID) {
		return ErrForbidden
	}
	if device != "" && fact.DeviceID != device {
		return invalid("告警与设备不匹配")
	}
	return nil
}

func (s *Service) record(tx ports.DutyTx, a Actor, c Command) (any, error) {
	v, e := decode[Record](c.Body)
	if e != nil {
		return nil, e
	}
	if c.Operation == "correct" {
		old, prev, e := read[Record](tx, model.DutyRecordKind, c.ID)
		if e != nil {
			return nil, e
		}
		if e = checkVersion(old, c.ExpectedVersion); e != nil {
			return nil, e
		}
		if prev.AuthorID != a.Username && !a.Admin {
			return nil, ErrForbidden
		}
		v.RunID = prev.RunID
		v.CorrectsID = old.ID
		if strings.TrimSpace(v.CorrectionReason) == "" {
			return nil, invalid("更正须填写原因")
		}
	} else if c.Operation != "create" {
		return nil, invalid("记录只能新增或追加更正")
	} else {
		v.CorrectsID = ""
		v.CorrectionReason = ""
	}
	_, r, e := read[Run](tx, model.DutyRunKind, v.RunID)
	if e != nil {
		return nil, e
	}
	if !runMember(a, r) || !a.covers(r.DeviceIDs) {
		return nil, ErrForbidden
	}
	if r.Status != "ACTIVE" {
		return nil, invalid("当班已结束，请在交接记录中追加补充")
	}
	if strings.TrimSpace(v.Content) == "" || len(v.Content) > 20000 {
		return nil, invalid("记录内容不能为空且不能超过20000字节")
	}
	v.StationID = r.StationID
	v.AuthorID = a.Username
	if v.OccurredAt == 0 {
		v.OccurredAt = s.now()
	}
	if v.OccurredAt < r.StartedAt || v.OccurredAt > s.now() {
		return nil, invalid("记录发生时间须在本班实际值班区间内")
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
	if v.ItemID != "" {
		_, item, e := read[Item](tx, model.DutyItemKind, v.ItemID)
		if e != nil {
			return nil, e
		}
		if item.StationID != r.StationID {
			return nil, ErrForbidden
		}
	}
	for i, attachment := range v.Attachments {
		doc, e := tx.Get(model.DutyAttachmentKind, attachment.ID)
		if e != nil {
			return nil, invalid("附件未上传或不存在")
		}
		var saved model.DutyAttachmentRecord
		if e = json.Unmarshal(doc.Body, &saved); e != nil {
			return nil, e
		}
		if saved.RunID != v.RunID || saved.StationID != r.StationID || saved.AuthorID != a.Username {
			return nil, ErrForbidden
		}
		v.Attachments[i] = saved.Attachment
	}
	doc, e := create(tx, model.DutyRecordKind, v)
	if e != nil {
		return nil, e
	}
	return doc, s.appendEvent(tx, a, "duty.record."+c.Operation, doc, r.StationID, v.RunID, v.DeviceID, v.AlarmID, v.OccurredAt)
}
