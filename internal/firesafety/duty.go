package firesafety

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"iot-platform/internal/model"
)

func saveShift(state *model.FireSafetyState, id string, body json.RawMessage, now int64) (any, error) {
	var v model.DutyShift
	if err := read(body, &v); err != nil {
		return nil, err
	}
	if err := fields(field{&v.Name, "班次名称", true, 100}, field{&v.StartTime, "班次开始时间", true, 5}, field{&v.EndTime, "班次结束时间", true, 5}); err != nil {
		return nil, err
	}
	for _, value := range []string{v.StartTime, v.EndTime} {
		if t, err := time.Parse("15:04", value); err != nil || t.Format("15:04") != value {
			return nil, invalid("班次时间须为有效的 HH:mm 时间")
		}
	}
	old := shift(state, id)
	if id != "" && old == nil {
		return nil, missing("班次")
	}
	for _, s := range state.Shifts {
		if s.ID != id && strings.EqualFold(s.Name, v.Name) {
			return nil, conflict("班次名称已存在")
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
		state.Shifts = append(state.Shifts, v)
	} else {
		*old = v
	}
	return v, nil
}

func deleteShift(state *model.FireSafetyState, id string, body json.RawMessage) (any, error) {
	old := shift(state, id)
	if old == nil {
		return nil, missing("班次")
	}
	if err := requireVersion(body, old.FireRecord); err != nil {
		return nil, err
	}
	for _, a := range state.Assignments {
		if a.ShiftID == id {
			return nil, conflict("班次已有排班记录，不能删除")
		}
	}
	for i := range state.Shifts {
		if state.Shifts[i].ID == id {
			state.Shifts = append(state.Shifts[:i], state.Shifts[i+1:]...)
			break
		}
	}
	return deleted(), nil
}

func validateAssignment(state *model.FireSafetyState, v model.DutyAssignment, excludeID string) error {
	if err := enabledStation(state, v.StationID); err != nil {
		return err
	}
	if shift(state, v.ShiftID) == nil {
		return invalid("班次不存在或不属于当前租户")
	}
	if err := uniqueIDs(v.PersonnelIDs, "排班人员"); err != nil {
		return err
	}
	if err := stamp(v.StartAt, "排班开始时间"); err != nil {
		return err
	}
	if err := stamp(v.EndAt, "排班结束时间"); err != nil {
		return err
	}
	if v.EndAt <= v.StartAt || v.EndAt-v.StartAt > int64(48*time.Hour/time.Millisecond) {
		return invalid("排班结束时间须晚于开始时间，时长不能超过48小时")
	}
	for _, id := range v.PersonnelIDs {
		p := personnel(state, id)
		if p == nil || p.StationID != v.StationID || !p.Enabled {
			return invalid("排班人员须属于本消防站且已启用")
		}
		for _, other := range state.Assignments {
			if other.ID != excludeID && contains(other.PersonnelIDs, id) && v.StartAt < other.EndAt && other.StartAt < v.EndAt {
				return conflict("人员“" + p.Name + "”存在重叠排班")
			}
		}
	}
	return nil
}

func saveAssignment(state *model.FireSafetyState, id string, body json.RawMessage, now int64) (any, error) {
	var v model.DutyAssignment
	if err := read(body, &v); err != nil {
		return nil, err
	}
	if err := text(&v.Notes, "排班备注", false, 2000); err != nil {
		return nil, err
	}
	old := assignment(state, id)
	if id != "" && old == nil {
		return nil, missing("排班")
	}
	if old != nil && (old.StationID != v.StationID || old.ShiftID != v.ShiftID || old.StartAt != v.StartAt || old.EndAt != v.EndAt || !slices.Equal(old.PersonnelIDs, v.PersonnelIDs)) {
		for _, swap := range state.Swaps {
			if swap.AssignmentID == id && swap.Status == "pending" {
				return nil, conflict("排班存在待审批换班，请先完成审批")
			}
		}
	}
	if err := validateAssignment(state, v, id); err != nil {
		return nil, err
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
		state.Assignments = append(state.Assignments, v)
	} else {
		*old = v
	}
	return v, nil
}

// maxBatchAssignments bounds one batch to about two months of daily shifts.
const maxBatchAssignments = 62

// createAssignments validates every assignment against existing duty and the
// earlier items of the same batch, then commits all of them or none.
func createAssignments(state *model.FireSafetyState, body json.RawMessage, now int64) (any, error) {
	var in struct {
		Assignments []model.DutyAssignment `json:"assignments"`
	}
	if err := read(body, &in); err != nil {
		return nil, err
	}
	if len(in.Assignments) == 0 || len(in.Assignments) > maxBatchAssignments {
		return nil, invalid(fmt.Sprintf("批量排班须为1至%d条", maxBatchAssignments))
	}
	var problems []string
	created := make([]model.DutyAssignment, 0, len(in.Assignments))
	for _, v := range in.Assignments {
		if err := text(&v.Notes, "排班备注", false, 2000); err != nil {
			return nil, err
		}
		day := time.UnixMilli(v.StartAt).Format("01-02 15:04")
		if err := validateAssignment(state, v, ""); err != nil {
			if errors.Is(err, ErrConflict) {
				problems = append(problems, day+" "+strings.TrimPrefix(err.Error(), ErrConflict.Error()+": "))
				continue
			}
			return nil, fmt.Errorf("%w（%s）", err, day)
		}
		r, _ := record(model.FireRecord{}, nil, now)
		v.FireRecord = r
		state.Assignments = append(state.Assignments, v)
		created = append(created, v)
	}
	if len(problems) > 0 {
		if len(problems) > 10 {
			problems = append(problems[:10], fmt.Sprintf("等 %d 处", len(problems)))
		}
		return nil, conflict("以下排班存在冲突，未保存任何排班：" + strings.Join(problems, "；"))
	}
	return map[string]any{"items": created, "created": len(created)}, nil
}

func deleteAssignment(state *model.FireSafetyState, id string, body json.RawMessage) (any, error) {
	old := assignment(state, id)
	if old == nil {
		return nil, missing("排班")
	}
	if err := requireVersion(body, old.FireRecord); err != nil {
		return nil, err
	}
	for _, swap := range state.Swaps {
		if swap.AssignmentID == id {
			return nil, conflict("排班已有换班记录，不能删除")
		}
	}
	for i := range state.Assignments {
		if state.Assignments[i].ID == id {
			state.Assignments = append(state.Assignments[:i], state.Assignments[i+1:]...)
			break
		}
	}
	return deleted(), nil
}

func swappedAssignment(state *model.FireSafetyState, v model.DutySwap, now int64) (model.DutyAssignment, error) {
	a := assignment(state, v.AssignmentID)
	if a == nil {
		return model.DutyAssignment{}, invalid("原排班不存在或不属于当前租户")
	}
	if a.EndAt <= now {
		return model.DutyAssignment{}, conflict("排班已结束，不能换班")
	}
	if v.FromPersonnelID == "" || v.ToPersonnelID == "" || v.FromPersonnelID == v.ToPersonnelID || !contains(a.PersonnelIDs, v.FromPersonnelID) || contains(a.PersonnelIDs, v.ToPersonnelID) {
		return model.DutyAssignment{}, invalid("换班人员无效，原人员须在排班中且替班人员不能已在排班中")
	}
	next := *a
	next.PersonnelIDs = append([]string{}, a.PersonnelIDs...)
	for i, id := range next.PersonnelIDs {
		if id == v.FromPersonnelID {
			next.PersonnelIDs[i] = v.ToPersonnelID
		}
	}
	if err := validateAssignment(state, next, a.ID); err != nil {
		return model.DutyAssignment{}, err
	}
	return next, nil
}

func createSwap(state *model.FireSafetyState, actor string, body json.RawMessage, now int64) (any, error) {
	var v model.DutySwap
	if err := read(body, &v); err != nil {
		return nil, err
	}
	if err := text(&v.Reason, "换班原因", true, 2000); err != nil {
		return nil, err
	}
	if _, err := swappedAssignment(state, v, now); err != nil {
		return nil, err
	}
	for _, pending := range state.Swaps {
		if pending.AssignmentID == v.AssignmentID && pending.FromPersonnelID == v.FromPersonnelID && pending.Status == "pending" {
			return nil, conflict("原人员已有待审批换班申请")
		}
	}
	r, _ := record(model.FireRecord{}, nil, now)
	v.FireRecord = r
	v.Status, v.RequestedBy, v.ReviewedBy, v.ReviewNote, v.ReviewedAt = "pending", actor, "", "", 0
	state.Swaps = append(state.Swaps, v)
	return v, nil
}

type reviewInput struct {
	Approved *bool  `json:"approved"`
	Note     string `json:"note"`
}

func readReview(body json.RawMessage) (reviewInput, error) {
	var in reviewInput
	if err := read(body, &in); err != nil {
		return in, err
	}
	if in.Approved == nil {
		return in, invalid("须明确选择通过或驳回")
	}
	if err := text(&in.Note, "审批意见", !*in.Approved, 2000); err != nil {
		return in, err
	}
	return in, nil
}

func reviewSwap(state *model.FireSafetyState, actor, id string, body json.RawMessage, now int64) (any, error) {
	in, err := readReview(body)
	if err != nil {
		return nil, err
	}
	for i := range state.Swaps {
		v := &state.Swaps[i]
		if v.ID != id {
			continue
		}
		if err := requireVersion(body, v.FireRecord); err != nil {
			return nil, err
		}
		if v.Status != "pending" {
			return nil, conflict("换班申请已审批，不能重复审批")
		}
		if v.RequestedBy == actor {
			return nil, forbidden("不能审批自己提交的换班申请")
		}
		if *in.Approved {
			next, err := swappedAssignment(state, *v, now)
			if err != nil {
				return nil, err
			}
			advance(&next.FireRecord, now)
			*assignment(state, v.AssignmentID) = next
			v.Status = "approved"
		} else {
			v.Status = "rejected"
		}
		v.ReviewedBy, v.ReviewNote, v.ReviewedAt = actor, in.Note, now
		advance(&v.FireRecord, now)
		return *v, nil
	}
	return nil, missing("换班申请")
}
