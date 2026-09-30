package duty

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func (s *Service) configure(tx ports.DutyTx, a Actor, c Command, actors map[string]Actor) (any, error) {
	if c.Operation == "delete" {
		doc, e := tx.Get(c.Kind, c.ID)
		if e != nil {
			return nil, e
		}
		if e = checkVersion(doc, c.ExpectedVersion); e != nil {
			return nil, e
		}
		ids, e := docDevices(tx, doc)
		if e != nil {
			return nil, e
		}
		if !a.covers(ids) {
			return nil, ErrForbidden
		}
		for _, k := range []string{model.DutyRosterKind, model.DutyRunKind, model.DutyItemKind, model.DutyHandoverKind} {
			docs, e := listAll(tx, model.DutyFilter{Kind: k})
			if e != nil {
				return nil, e
			}
			for _, d := range docs {
				var b map[string]any
				_ = json.Unmarshal(d.Body, &b)
				key := "stationId"
				if c.Kind == model.DutyTeamKind {
					key = "teamId"
				}
				if c.Kind == model.DutyShiftTemplateKind {
					key = "templateId"
				}
				if b[key] == c.ID {
					return nil, invalid("已有业务记录引用，不能删除；请停用岗位或保留配置")
				}
			}
		}
		return map[string]any{"deleted": c.ID}, tx.Delete(c.Kind, c.ID, c.ExpectedVersion)
	}
	if c.Operation != "create" && c.Operation != "update" {
		return nil, invalid("未知配置操作")
	}
	var old model.DutyDocument
	var err error
	if c.Operation == "update" {
		old, err = tx.Get(c.Kind, c.ID)
		if err != nil {
			return nil, err
		}
		if err = checkVersion(old, c.ExpectedVersion); err != nil {
			return nil, err
		}
		ids, e := docDevices(tx, old)
		if e != nil {
			return nil, e
		}
		if !a.covers(ids) {
			return nil, ErrForbidden
		}
	}
	var body any
	switch c.Kind {
	case model.DutyStationKind:
		v, e := decode[Station](c.Body)
		if e != nil {
			return nil, e
		}
		v.Name = strings.TrimSpace(v.Name)
		v.DeviceIDs = unique(v.DeviceIDs)
		if v.Name == "" || len(v.DeviceIDs) == 0 {
			return nil, invalid("岗位名称和负责设备不能为空")
		}
		if v.RequiredPeople < 1 || v.RequiredPeople > 100 {
			return nil, invalid("岗位人数应为1至100")
		}
		if v.ReminderMinutes <= 0 {
			v.ReminderMinutes = 15
		}
		if v.ReminderMinutes > 1440 {
			return nil, invalid("提醒提前时间不能超过一天")
		}
		if v.Timezone == "" {
			v.Timezone = "Asia/Shanghai"
		}
		if _, e = time.LoadLocation(v.Timezone); e != nil {
			return nil, invalid("无效时区")
		}
		if !a.covers(v.DeviceIDs) {
			return nil, ErrForbidden
		}
		snapshot, e := tx.Snapshot(v.DeviceIDs)
		if e != nil {
			return nil, e
		}
		if len(snapshot.Devices) != len(v.DeviceIDs) {
			return nil, invalid("岗位含不存在的设备")
		}
		if e = eligible(actors, v.SupervisorID, v.DeviceIDs); e != nil {
			return nil, e
		}
		stations, e := listAll(tx, model.DutyFilter{Kind: model.DutyStationKind})
		if e != nil {
			return nil, e
		}
		for _, d := range stations {
			if d.ID == c.ID {
				continue
			}
			st, _ := model.DutyBody[Station](d)
			for _, id := range v.DeviceIDs {
				if contains(st.DeviceIDs, id) {
					return nil, invalid("设备 %s 已由岗位 %s 负责", id, st.Name)
				}
			}
		}
		v.ScopeVersion = 1
		if old.ID != "" {
			prev, _ := model.DutyBody[Station](old)
			v.ScopeVersion = prev.ScopeVersion
			if digest(prev.DeviceIDs) != digest(v.DeviceIDs) {
				active, e := listAll(tx, model.DutyFilter{Kind: model.DutyRunKind, StationID: c.ID, Status: "ACTIVE"})
				if e != nil {
					return nil, e
				}
				if len(active) > 0 {
					return nil, invalid("岗位正在值守，不能修改负责范围")
				}
				v.ScopeVersion++
			}
		}
		body = v
	case model.DutyTeamKind:
		v, e := decode[Team](c.Body)
		if e != nil {
			return nil, e
		}
		v.MemberIDs = unique(v.MemberIDs)
		if strings.TrimSpace(v.Name) == "" || len(v.MemberIDs) == 0 || !contains(v.MemberIDs, v.LeaderID) {
			return nil, invalid("班组需要名称、成员及成员中的负责人")
		}
		for _, n := range v.MemberIDs {
			if e = eligible(actors, n, nil); e != nil {
				return nil, e
			}
		}
		body = v
	case model.DutyShiftTemplateKind:
		v, e := decode[ShiftTemplate](c.Body)
		if e != nil {
			return nil, e
		}
		if strings.TrimSpace(v.Name) == "" || v.EndDayOffset < 0 || v.EndDayOffset > 1 {
			return nil, invalid("班次模板名称或跨日参数错误")
		}
		st, e := time.Parse("15:04", v.StartTime)
		if e != nil {
			return nil, invalid("开始时间须为HH:mm")
		}
		end, e := time.Parse("15:04", v.EndTime)
		if e != nil || end.Add(time.Duration(v.EndDayOffset)*24*time.Hour).Sub(st) <= 0 || end.Add(time.Duration(v.EndDayOffset)*24*time.Hour).Sub(st) > 24*time.Hour {
			return nil, invalid("班次必须大于0且不超过24小时")
		}
		body = v
	}
	var saved model.DutyDocument
	if old.ID != "" {
		saved, err = put(tx, old, body, c.ExpectedVersion)
	} else {
		saved, err = create(tx, c.Kind, body)
	}
	if err != nil {
		return nil, err
	}
	return saved, s.appendEvent(tx, a, "duty.configuration."+c.Operation, saved, "", "", "", "", s.now())
}
func (s *Service) rosterChecks(tx ports.DutyTx, a Actor, r Roster, id string, actors map[string]Actor) error {
	_, station, e := read[Station](tx, model.DutyStationKind, r.StationID)
	if e != nil {
		return e
	}
	if !station.Enabled {
		return invalid("岗位已停用")
	}
	if !a.covers(station.DeviceIDs) {
		return ErrForbidden
	}
	if r.StartAt <= 0 || r.EndAt <= r.StartAt || r.EndAt-r.StartAt > 24*time.Hour.Milliseconds() {
		return invalid("排班必须指定完整日期，且时长不超过24小时")
	}
	if len(unique(r.MemberIDs)) < station.RequiredPeople || !contains(r.MemberIDs, r.LeaderID) {
		return invalid("排班人数不足或负责人不在成员中")
	}
	for _, n := range r.MemberIDs {
		if e = eligible(actors, n, station.DeviceIDs); e != nil {
			return e
		}
	}
	rosters, e := listAll(tx, model.DutyFilter{Kind: model.DutyRosterKind})
	if e != nil {
		return e
	}
	for _, d := range rosters {
		if d.ID == id {
			continue
		}
		other, _ := model.DutyBody[Roster](d)
		if other.Status == "CANCELLED" || other.StartAt >= r.EndAt || other.EndAt <= r.StartAt {
			continue
		}
		if other.StationID == r.StationID {
			return invalid("同岗位排班时间重叠")
		}
		for _, n := range r.MemberIDs {
			if contains(other.MemberIDs, n) {
				return invalid("人员 %s 在其他岗位已有冲突排班", n)
			}
		}
	}
	return nil
}
func (s *Service) buildRoster(tx ports.DutyTx, req GenerateRequest, date time.Time) (Roster, error) {
	_, st, e := read[Station](tx, model.DutyStationKind, req.StationID)
	if e != nil {
		return Roster{}, e
	}
	_, tmpl, e := read[ShiftTemplate](tx, model.DutyShiftTemplateKind, req.TemplateID)
	if e != nil {
		return Roster{}, e
	}
	loc, e := time.LoadLocation(st.Timezone)
	if e != nil {
		return Roster{}, e
	}
	start, e := time.ParseInLocation("2006-01-02 15:04", date.Format("2006-01-02")+" "+tmpl.StartTime, loc)
	if e != nil {
		return Roster{}, e
	}
	end, e := time.ParseInLocation("2006-01-02 15:04", date.AddDate(0, 0, tmpl.EndDayOffset).Format("2006-01-02")+" "+tmpl.EndTime, loc)
	if e != nil {
		return Roster{}, e
	}
	members, leader := unique(req.Members), req.Leader
	if req.TeamID != "" {
		_, team, e := read[Team](tx, model.DutyTeamKind, req.TeamID)
		if e != nil {
			return Roster{}, e
		}
		if len(members) == 0 {
			members = append([]string{}, team.MemberIDs...)
		}
		if leader == "" {
			leader = team.LeaderID
		}
	}
	return Roster{StationID: req.StationID, TemplateID: req.TemplateID, TeamID: req.TeamID, StartAt: start.UnixMilli(), EndAt: end.UnixMilli(), MemberIDs: members, LeaderID: leader, Status: "DRAFT", DeviceIDs: append([]string{}, st.DeviceIDs...), ScopeVersion: st.ScopeVersion}, nil
}
func (s *Service) roster(tx ports.DutyTx, a Actor, c Command, actors map[string]Actor) (any, error) {
	if c.Operation == "generate" {
		req, e := decode[GenerateRequest](c.Body)
		if e != nil {
			return nil, e
		}
		from, e := time.Parse("2006-01-02", req.From)
		if e != nil {
			return nil, invalid("起始日期须为YYYY-MM-DD")
		}
		to, e := time.Parse("2006-01-02", req.To)
		if e != nil || to.Before(from) || to.Sub(from) > 366*24*time.Hour {
			return nil, invalid("日期区间须在一年以内")
		}
		var result []model.DutyDocument
		for day := from; !day.After(to); day = day.AddDate(0, 0, 1) {
			r, e := s.buildRoster(tx, req, day)
			if e != nil {
				return nil, e
			}
			if e = s.rosterChecks(tx, a, r, "", actors); e != nil {
				return nil, e
			}
			d, e := create(tx, model.DutyRosterKind, r)
			if e != nil {
				return nil, e
			}
			result = append(result, d)
		}
		return Result{Items: result, Total: len(result)}, nil
	}
	if c.Operation == "import-preview" || c.Operation == "import" {
		req, e := decode[ImportRequest](c.Body)
		if e != nil {
			return nil, e
		}
		preview, e := s.previewImport(tx, a, req, actors)
		if e != nil {
			return nil, e
		}
		if c.Operation == "import-preview" {
			return preview, nil
		}
		if len(preview.Problems) > 0 {
			return nil, invalid("导入存在%d项问题，请先修正", len(preview.Problems))
		}
		if req.Digest == "" || req.Digest != preview.Digest {
			return nil, ErrConflict
		}
		batch := uuid.NewString()
		result := Result{Items: []model.DutyDocument{}}
		for _, r := range preview.Rows {
			r.ImportBatchID = batch
			r.Source = "legacy-import"
			d, e := create(tx, model.DutyRosterKind, r)
			if e != nil {
				return nil, e
			}
			result.Items = append(result.Items, d)
		}
		result.Total = len(result.Items)
		return result, nil
	}
	if c.Operation == "create" {
		r, e := decode[Roster](c.Body)
		if e != nil {
			return nil, e
		}
		r.MemberIDs = unique(r.MemberIDs)
		r.Status = "DRAFT"
		_, st, e := read[Station](tx, model.DutyStationKind, r.StationID)
		if e != nil {
			return nil, e
		}
		r.DeviceIDs = st.DeviceIDs
		r.ScopeVersion = st.ScopeVersion
		if e = s.rosterChecks(tx, a, r, "", actors); e != nil {
			return nil, e
		}
		return create(tx, model.DutyRosterKind, r)
	}
	doc, r, e := read[Roster](tx, model.DutyRosterKind, c.ID)
	if e != nil {
		return nil, e
	}
	if !a.covers(r.DeviceIDs) {
		return nil, ErrForbidden
	}
	if c.Operation == "validate" {
		if e = s.rosterChecks(tx, a, r, c.ID, actors); e != nil {
			return nil, e
		}
		return map[string]any{"valid": true, "gaps": s.rosterGaps(tx, r.StationID)}, nil
	}
	if e = checkVersion(doc, c.ExpectedVersion); e != nil {
		return nil, e
	}
	if c.Operation == "update" {
		runs, e := listAll(tx, model.DutyFilter{Kind: model.DutyRunKind})
		if e != nil {
			return nil, e
		}
		for _, d := range runs {
			v, _ := model.DutyBody[Run](d)
			if v.RosterID == c.ID && v.StartedAt > 0 {
				return nil, invalid("实际值班已开始，请通过代班记录调整人员")
			}
		}
		next, e := decode[Roster](c.Body)
		if e != nil {
			return nil, e
		}
		if r.Status == "PUBLISHED" && strings.TrimSpace(next.Reason) == "" {
			return nil, invalid("修改已发布排班须填写原因")
		}
		if r.Status == "CANCELLED" {
			return nil, invalid("已取消排班不能修改")
		}
		next.Status = r.Status
		next.History = append(r.History, model.DutyRosterChange{At: s.now(), ActorID: a.Username, Reason: next.Reason, Previous: doc.Body})
		next.MemberIDs = unique(next.MemberIDs)
		_, st, e := read[Station](tx, model.DutyStationKind, next.StationID)
		if e != nil {
			return nil, e
		}
		next.DeviceIDs = st.DeviceIDs
		next.ScopeVersion = st.ScopeVersion
		if e = s.rosterChecks(tx, a, next, c.ID, actors); e != nil {
			return nil, e
		}
		r = next
	} else if c.Operation == "publish" {
		if r.Status != "DRAFT" {
			return nil, invalid("仅草稿排班可发布")
		}
		if e = s.rosterChecks(tx, a, r, c.ID, actors); e != nil {
			return nil, e
		}
		r.Status = "PUBLISHED"
		for _, n := range r.MemberIDs {
			if e = s.notify(tx, n, r.StationID, "", "ROSTER_PUBLISHED", doc.ID, "值班排班已发布"); e != nil {
				return nil, e
			}
		}
	} else if c.Operation == "cancel" {
		b, e := decode[struct {
			Reason string `json:"reason"`
		}](c.Body)
		if e != nil {
			return nil, e
		}
		if strings.TrimSpace(b.Reason) == "" {
			return nil, invalid("取消排班须填写原因")
		}
		runs, e := listAll(tx, model.DutyFilter{Kind: model.DutyRunKind})
		if e != nil {
			return nil, e
		}
		for _, d := range runs {
			v, _ := model.DutyBody[Run](d)
			if v.RosterID == c.ID && v.StartedAt > 0 {
				return nil, invalid("已执行排班不能取消")
			}
		}
		r.Status = "CANCELLED"
		r.Reason = b.Reason
	} else {
		return nil, invalid("未知排班操作")
	}
	saved, e := put(tx, doc, r, c.ExpectedVersion)
	if e != nil {
		return nil, e
	}
	return saved, s.appendEvent(tx, a, "duty.roster."+c.Operation, saved, r.StationID, "", "", "", s.now())
}
func (s *Service) rosterGaps(tx ports.DutyTx, stationID string) []map[string]int64 {
	docs, _ := listAll(tx, model.DutyFilter{Kind: model.DutyRosterKind, StationID: stationID})
	var rows []Roster
	for _, d := range docs {
		r, _ := model.DutyBody[Roster](d)
		if r.Status == "PUBLISHED" {
			rows = append(rows, r)
		}
	}
	for i := 0; i < len(rows); i++ {
		for j := i + 1; j < len(rows); j++ {
			if rows[j].StartAt < rows[i].StartAt {
				rows[i], rows[j] = rows[j], rows[i]
			}
		}
	}
	gaps := []map[string]int64{}
	for i := 1; i < len(rows); i++ {
		if rows[i].StartAt > rows[i-1].EndAt {
			gaps = append(gaps, map[string]int64{"startAt": rows[i-1].EndAt, "endAt": rows[i].StartAt})
		}
	}
	return gaps
}
func parseImport(req ImportRequest) ([]ImportRow, error) {
	if req.CSV == "" {
		if len(req.Rows) > 1000 {
			return nil, invalid("每次最多导入1000条排班")
		}
		return req.Rows, nil
	}
	r := csv.NewReader(strings.NewReader(req.CSV))
	head, e := r.Read()
	if e != nil {
		return nil, invalid("CSV为空")
	}
	idx := map[string]int{}
	for i, k := range head {
		idx[strings.TrimSpace(strings.TrimPrefix(k, "\ufeff"))] = i
	}
	for _, key := range []string{"stationId", "memberIds", "leaderId"} {
		if _, ok := idx[key]; !ok {
			return nil, invalid("CSV缺少列 %s", key)
		}
	}
	if _, ok := idx["startAt"]; !ok {
		if _, ok = idx["date"]; !ok {
			return nil, invalid("CSV须提供startAt/endAt或date/templateId")
		}
		if _, ok = idx["templateId"]; !ok {
			return nil, invalid("CSV须提供templateId")
		}
	}

	var out []ImportRow
	for {
		cells, e := r.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, invalid("CSV格式错误")
		}
		get := func(k string) string {
			i, ok := idx[k]
			if !ok || i >= len(cells) {
				return ""
			}
			return strings.TrimSpace(cells[i])
		}
		start := importTimestamp(get("startAt"))
		end := importTimestamp(get("endAt"))
		out = append(out, ImportRow{StationID: get("stationId"), TemplateID: get("templateId"), TeamID: get("teamId"), Date: get("date"), Start: start, End: end, Members: strings.Split(get("memberIds"), ";"), Leader: get("leaderId"), SourceID: get("sourceId")})
		if len(out) > 1000 {
			return nil, invalid("每次最多导入1000条排班")
		}
	}
	return out, nil
}
func (s *Service) previewImport(tx ports.DutyTx, a Actor, req ImportRequest, actors map[string]Actor) (ImportPreview, error) {
	rows, e := parseImport(req)
	if e != nil {
		return ImportPreview{}, e
	}
	p := ImportPreview{Rows: []Roster{}, Problems: []ImportProblem{}}
	existing, e := listAll(tx, model.DutyFilter{Kind: model.DutyRosterKind})
	if e != nil {
		return p, e
	}
	for i, row := range rows {
		_, responsibility, e := read[Station](tx, model.DutyStationKind, row.StationID)
		if e == nil && !a.covers(responsibility.DeviceIDs) {
			return p, ErrForbidden
		}
		var r Roster
		if row.Start > 0 && row.End > row.Start {
			_, station, e := read[Station](tx, model.DutyStationKind, row.StationID)
			if e != nil {
				p.Problems = append(p.Problems, ImportProblem{i + 1, e.Error()})
				continue
			}
			r = Roster{StationID: row.StationID, TemplateID: row.TemplateID, TeamID: row.TeamID, StartAt: row.Start, EndAt: row.End, MemberIDs: unique(row.Members), LeaderID: row.Leader, DeviceIDs: station.DeviceIDs, ScopeVersion: station.ScopeVersion, Status: "DRAFT"}
			if row.TeamID != "" {
				_, team, e := read[Team](tx, model.DutyTeamKind, row.TeamID)
				if e != nil {
					p.Problems = append(p.Problems, ImportProblem{i + 1, e.Error()})
					continue
				}
				if len(r.MemberIDs) == 0 {
					r.MemberIDs = team.MemberIDs
				}
				if r.LeaderID == "" {
					r.LeaderID = team.LeaderID
				}
			}
		} else {
			day, e := time.Parse("2006-01-02", row.Date)
			if e != nil {
				p.Problems = append(p.Problems, ImportProblem{i + 1, "日期格式错误，或startAt/endAt无效"})
				continue
			}
			r, e = s.buildRoster(tx, GenerateRequest{StationID: row.StationID, TemplateID: row.TemplateID, TeamID: row.TeamID, Members: row.Members, Leader: row.Leader}, day)
			if e != nil {
				p.Problems = append(p.Problems, ImportProblem{i + 1, e.Error()})
				continue
			}
		}

		r.SourceID = row.SourceID
		if e = s.rosterChecks(tx, a, r, "", actors); e != nil {
			p.Problems = append(p.Problems, ImportProblem{i + 1, e.Error()})
		}
		for _, doc := range existing {
			other, _ := model.DutyBody[Roster](doc)
			if row.SourceID != "" && other.SourceID == row.SourceID {
				p.Problems = append(p.Problems, ImportProblem{i + 1, "来源排班已导入"})
			}
		}
		for _, other := range p.Rows {
			if row.SourceID != "" && other.SourceID == row.SourceID {
				p.Problems = append(p.Problems, ImportProblem{i + 1, "导入来源ID重复"})
			}
			if other.StartAt < r.EndAt && other.EndAt > r.StartAt {
				if other.StationID == r.StationID {
					p.Problems = append(p.Problems, ImportProblem{i + 1, "导入批次中岗位时间冲突"})
				} else {
					for _, n := range r.MemberIDs {
						if contains(other.MemberIDs, n) {
							p.Problems = append(p.Problems, ImportProblem{i + 1, fmt.Sprintf("导入批次中人员 %s 时间冲突", n)})
						}
					}
				}
			}
		}
		p.Rows = append(p.Rows, r)
	}
	p.Digest = digest(p.Rows)
	return p, nil
}

func importTimestamp(value string) int64 {
	if n, e := strconv.ParseInt(value, 10, 64); e == nil {
		return n
	}
	if tm, e := time.Parse(time.RFC3339, value); e == nil {
		return tm.UnixMilli()
	}
	return 0
}
