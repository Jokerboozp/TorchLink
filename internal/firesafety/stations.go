package firesafety

import (
	"encoding/json"
	"strings"

	"iot-platform/internal/model"
)

func saveStation(state *model.FireSafetyState, id string, body json.RawMessage, now int64) (any, error) {
	var v model.FireStation
	if err := read(body, &v); err != nil {
		return nil, err
	}
	if err := fields(field{&v.Code, "消防站编号", true, 64}, field{&v.Name, "消防站名称", true, 100}, field{&v.Address, "地址", false, 300}, field{&v.Contact, "联系人", false, 100}, field{&v.Phone, "联系电话", false, 40}, field{&v.Notes, "备注", false, 2000}); err != nil {
		return nil, err
	}
	if err := oneOf(v.Type, "消防站类型", "micro", "professional", "volunteer"); err != nil {
		return nil, err
	}
	if !finite(v.Longitude) || !finite(v.Latitude) || v.Longitude < -180 || v.Longitude > 180 || v.Latitude < -90 || v.Latitude > 90 {
		return nil, invalid("经纬度超出有效范围")
	}
	old := station(state, id)
	if id != "" && old == nil {
		return nil, missing("消防站")
	}
	for _, s := range state.Stations {
		if s.ID != id && strings.EqualFold(s.Code, v.Code) {
			return nil, conflict("消防站编号已存在")
		}
	}
	if old != nil && !v.Enabled {
		for _, a := range state.Assignments {
			if a.StationID == id && a.EndAt > now {
				return nil, conflict("消防站存在未结束排班，不能停用")
			}
		}
		for _, d := range state.Dispatches {
			if d.StationID == id && d.Status == "dispatched" {
				return nil, conflict("消防站存在未归队出勤，不能停用")
			}
		}
		for _, task := range state.Inspections {
			e := extinguisher(state, task.ExtinguisherID)
			p := personnel(state, task.AssigneeID)
			if inspectionOpen(task) && ((e != nil && e.StationID == id) || (p != nil && p.StationID == id)) {
				return nil, conflict("消防站存在未结巡检任务，不能停用")
			}
		}
	}
	var previous *model.FireRecord
	if old != nil {
		previous = &old.FireRecord
	}
	r, err := record(v.FireRecord, previous, now)
	if err != nil {
		return nil, err
	}
	v.FireRecord = r
	if old == nil {
		state.Stations = append(state.Stations, v)
	} else {
		*old = v
	}
	return v, nil
}

func deleteStation(state *model.FireSafetyState, id string, body json.RawMessage) (any, error) {
	old := station(state, id)
	if old == nil {
		return nil, missing("消防站")
	}
	if err := requireVersion(body, old.FireRecord); err != nil {
		return nil, err
	}
	for _, p := range state.Personnel {
		if p.StationID == id {
			return nil, conflict("消防站仍关联人员，不能删除")
		}
	}
	for _, e := range state.Equipment {
		if e.StationID == id {
			return nil, conflict("消防站仍关联器材，不能删除")
		}
	}
	for _, e := range state.Extinguishers {
		if e.StationID == id {
			return nil, conflict("消防站仍关联灭火器，不能删除")
		}
	}
	for _, a := range state.Assignments {
		if a.StationID == id {
			return nil, conflict("消防站已有排班记录，不能删除")
		}
	}
	for _, d := range state.Dispatches {
		if d.StationID == id {
			return nil, conflict("消防站已有出勤记录，不能删除")
		}
	}
	for i := range state.Stations {
		if state.Stations[i].ID == id {
			state.Stations = append(state.Stations[:i], state.Stations[i+1:]...)
			break
		}
	}
	return deleted(), nil
}

func savePersonnel(state *model.FireSafetyState, id string, body json.RawMessage, now int64) (any, error) {
	var v model.FirePersonnel
	if err := read(body, &v); err != nil {
		return nil, err
	}
	if err := fields(field{&v.Name, "人员姓名", true, 100}, field{&v.Phone, "联系电话", false, 40}, field{&v.Position, "岗位", false, 100}, field{&v.Notes, "备注", false, 2000}); err != nil {
		return nil, err
	}
	s := station(state, v.StationID)
	if s == nil {
		return nil, invalid("所属消防站不存在或不属于当前租户")
	}
	if v.Enabled && !s.Enabled {
		return nil, conflict("所属消防站已停用，不能启用人员")
	}
	old := personnel(state, id)
	if id != "" && old == nil {
		return nil, missing("人员")
	}
	if old != nil && (!v.Enabled || old.StationID != v.StationID) {
		for _, a := range state.Assignments {
			if a.EndAt > now && contains(a.PersonnelIDs, id) {
				return nil, conflict("人员存在未结束排班，不能停用或更换消防站")
			}
		}
		for _, d := range state.Dispatches {
			if d.Status == "dispatched" && contains(d.PersonnelIDs, id) {
				return nil, conflict("人员正在出勤，不能停用或更换消防站")
			}
		}
		for _, task := range state.Inspections {
			if task.AssigneeID == id && inspectionOpen(task) {
				return nil, conflict("人员存在未结巡检任务，不能停用或更换消防站")
			}
		}
		for _, swap := range state.Swaps {
			if swap.Status == "pending" && (swap.FromPersonnelID == id || swap.ToPersonnelID == id) {
				return nil, conflict("人员存在待审批换班，不能停用或更换消防站")
			}
		}
	}
	var previous *model.FireRecord
	if old != nil {
		previous = &old.FireRecord
	}
	r, err := record(v.FireRecord, previous, now)
	if err != nil {
		return nil, err
	}
	v.FireRecord = r
	if old == nil {
		state.Personnel = append(state.Personnel, v)
	} else {
		*old = v
	}
	return v, nil
}

func deletePersonnel(state *model.FireSafetyState, id string, body json.RawMessage) (any, error) {
	old := personnel(state, id)
	if old == nil {
		return nil, missing("人员")
	}
	if err := requireVersion(body, old.FireRecord); err != nil {
		return nil, err
	}
	for _, a := range state.Assignments {
		if contains(a.PersonnelIDs, id) {
			return nil, conflict("人员已有排班记录，不能删除")
		}
	}
	for _, d := range state.Dispatches {
		if contains(d.PersonnelIDs, id) {
			return nil, conflict("人员已有出勤记录，不能删除")
		}
	}
	for _, task := range state.Inspections {
		if task.AssigneeID == id {
			return nil, conflict("人员已有巡检记录，不能删除")
		}
	}
	for _, swap := range state.Swaps {
		if swap.FromPersonnelID == id || swap.ToPersonnelID == id {
			return nil, conflict("人员已有换班记录，不能删除")
		}
	}
	for i := range state.Personnel {
		if state.Personnel[i].ID == id {
			state.Personnel = append(state.Personnel[:i], state.Personnel[i+1:]...)
			break
		}
	}
	return deleted(), nil
}

func occupiedEquipment(state *model.FireSafetyState, id string) int {
	quantity := 0
	for _, d := range state.Dispatches {
		if d.Status == "dispatched" {
			for _, e := range d.Equipment {
				if e.EquipmentID == id {
					quantity += e.Quantity
				}
			}
		}
	}
	return quantity
}
func saveEquipment(state *model.FireSafetyState, id string, body json.RawMessage, now int64) (any, error) {
	var v model.FireEquipment
	if err := read(body, &v); err != nil {
		return nil, err
	}
	if err := fields(field{&v.Name, "器材名称", true, 100}, field{&v.Category, "器材类别", true, 100}, field{&v.Unit, "数量单位", true, 20}, field{&v.Notes, "备注", false, 2000}); err != nil {
		return nil, err
	}
	if v.Quantity < 0 || v.Quantity > 1000000 {
		return nil, invalid("器材数量须为0至1000000")
	}
	if err := oneOf(v.Status, "器材状态", "ready", "maintenance", "retired"); err != nil {
		return nil, err
	}
	if station(state, v.StationID) == nil {
		return nil, invalid("所属消防站不存在或不属于当前租户")
	}
	if v.Status == "ready" {
		if err := enabledStation(state, v.StationID); err != nil {
			return nil, err
		}
	}
	old := equipment(state, id)
	if id != "" && old == nil {
		return nil, missing("器材")
	}
	used := occupiedEquipment(state, id)
	if old != nil && used > 0 && (v.Status != "ready" || v.StationID != old.StationID || v.Quantity < used) {
		return nil, conflict("器材正在出勤，不能改变归属、停用或减少至占用数量以下")
	}
	var previous *model.FireRecord
	if old != nil {
		previous = &old.FireRecord
	}
	r, err := record(v.FireRecord, previous, now)
	if err != nil {
		return nil, err
	}
	v.FireRecord = r
	if old == nil {
		state.Equipment = append(state.Equipment, v)
	} else {
		*old = v
	}
	return v, nil
}

func deleteEquipment(state *model.FireSafetyState, id string, body json.RawMessage) (any, error) {
	old := equipment(state, id)
	if old == nil {
		return nil, missing("器材")
	}
	if err := requireVersion(body, old.FireRecord); err != nil {
		return nil, err
	}
	for _, d := range state.Dispatches {
		for _, e := range d.Equipment {
			if e.EquipmentID == id {
				return nil, conflict("器材已有出勤记录，不能删除")
			}
		}
	}
	for i := range state.Equipment {
		if state.Equipment[i].ID == id {
			state.Equipment = append(state.Equipment[:i], state.Equipment[i+1:]...)
			break
		}
	}
	return deleted(), nil
}

func createDispatch(state *model.FireSafetyState, actor string, body json.RawMessage, now int64) (any, error) {
	var v model.FireDispatch
	if err := read(body, &v); err != nil {
		return nil, err
	}
	if err := fields(field{&v.Title, "出勤标题", true, 150}, field{&v.Location, "出勤地点", true, 300}); err != nil {
		return nil, err
	}
	if err := oneOf(v.Type, "出勤类型", "fire", "rescue", "drill", "other"); err != nil {
		return nil, err
	}
	if err := enabledStation(state, v.StationID); err != nil {
		return nil, err
	}
	if err := uniqueIDs(v.PersonnelIDs, "出勤人员"); err != nil {
		return nil, err
	}
	if v.StartedAt == 0 {
		v.StartedAt = now
	}
	if err := stamp(v.StartedAt, "出勤时间"); err != nil {
		return nil, err
	}
	if v.StartedAt > now+60000 {
		return nil, invalid("出勤时间不能晚于当前时间")
	}
	for _, id := range v.PersonnelIDs {
		p := personnel(state, id)
		if p == nil || p.StationID != v.StationID || !p.Enabled {
			return nil, invalid("出勤人员须属于本消防站且已启用")
		}
		for _, d := range state.Dispatches {
			if d.Status == "dispatched" && contains(d.PersonnelIDs, id) {
				return nil, conflict("出勤人员尚未归队")
			}
		}
	}
	if len(v.Equipment) > 200 {
		return nil, invalid("出勤器材最多200项")
	}
	seen := map[string]bool{}
	for _, item := range v.Equipment {
		if item.EquipmentID == "" || seen[item.EquipmentID] || item.Quantity <= 0 || item.Quantity > 1000000 {
			return nil, invalid("出勤器材存在空值、重复项或无效数量")
		}
		seen[item.EquipmentID] = true
		e := equipment(state, item.EquipmentID)
		if e == nil || e.StationID != v.StationID || e.Status != "ready" {
			return nil, invalid("出勤器材须属于本消防站且处于可用状态")
		}
		if item.Quantity > e.Quantity-occupiedEquipment(state, e.ID) {
			return nil, conflict("出勤器材可用数量不足")
		}
	}
	r, _ := record(model.FireRecord{}, nil, now)
	v.FireRecord = r
	v.Status, v.CreatedBy, v.ReturnedBy, v.Summary = "dispatched", actor, "", ""
	v.ReturnedAt = 0
	if v.Equipment == nil {
		v.Equipment = []model.FireEquipmentUsage{}
	}
	state.Dispatches = append(state.Dispatches, v)
	return v, nil
}

func returnDispatch(state *model.FireSafetyState, actor, id string, body json.RawMessage, now int64) (any, error) {
	var in struct {
		ReturnedAt int64  `json:"returnedAt"`
		Summary    string `json:"summary"`
	}
	if err := read(body, &in); err != nil {
		return nil, err
	}
	for i := range state.Dispatches {
		v := &state.Dispatches[i]
		if v.ID != id {
			continue
		}
		if err := requireVersion(body, v.FireRecord); err != nil {
			return nil, err
		}
		if v.Status != "dispatched" {
			return nil, conflict("出勤记录已归队，不能重复归队")
		}
		if err := text(&in.Summary, "归队总结", true, 4000); err != nil {
			return nil, err
		}
		if in.ReturnedAt == 0 {
			in.ReturnedAt = now
		}
		if err := stamp(in.ReturnedAt, "归队时间"); err != nil {
			return nil, err
		}
		if in.ReturnedAt < v.StartedAt || in.ReturnedAt > now+60000 {
			return nil, invalid("归队时间须不早于出勤时间且不晚于当前时间")
		}
		v.Status, v.ReturnedBy, v.Summary, v.ReturnedAt = "returned", actor, in.Summary, in.ReturnedAt
		advance(&v.FireRecord, now)
		return *v, nil
	}
	return nil, missing("出勤记录")
}
