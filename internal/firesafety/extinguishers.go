package firesafety

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"iot-platform/internal/model"
)

func saveExtinguisher(state *model.FireSafetyState, id string, body json.RawMessage, now int64) (any, error) {
	var v model.Extinguisher
	if err := read(body, &v); err != nil {
		return nil, err
	}
	if err := fields(field{&v.Code, "灭火器编号", true, 64}, field{&v.Location, "安装位置", true, 300}, field{&v.Specification, "规格", false, 100}, field{&v.Manufacturer, "生产厂家", false, 150}, field{&v.SerialNumber, "出厂序列号", false, 100}, field{&v.Notes, "备注", false, 2000}); err != nil {
		return nil, err
	}
	if err := oneOf(v.Type, "灭火器类型", "dry_powder", "co2", "foam", "water", "other"); err != nil {
		return nil, err
	}
	if err := oneOf(v.Status, "灭火器状态", "active", "maintenance", "retired"); err != nil {
		return nil, err
	}
	if v.InspectionCycleDays < 1 || v.InspectionCycleDays > 3650 {
		return nil, invalid("巡检周期须为1至3650天")
	}
	manufactured, err := date(v.ManufacturedOn, "生产日期")
	if err != nil {
		return nil, err
	}
	serviceDue, err := date(v.ServiceDueOn, "维保到期日期")
	if err != nil {
		return nil, err
	}
	retire, err := date(v.RetireOn, "报废日期")
	if err != nil {
		return nil, err
	}
	if v.ManufacturedOn != "" && v.ManufacturedOn > time.UnixMilli(now).In(time.Local).Format("2006-01-02") {
		return nil, invalid("生产日期不能晚于当前日期")
	}
	if !manufactured.IsZero() && ((!serviceDue.IsZero() && serviceDue.Before(manufactured)) || (!retire.IsZero() && retire.Before(manufactured))) {
		return nil, invalid("维保到期日期和报废日期不能早于生产日期")
	}
	if !serviceDue.IsZero() && !retire.IsZero() && retire.Before(serviceDue) {
		return nil, invalid("报废日期不能早于维保到期日期")
	}
	if station(state, v.StationID) == nil {
		return nil, invalid("所属消防站不存在或不属于当前租户")
	}
	if v.Status == "active" {
		if err := enabledStation(state, v.StationID); err != nil {
			return nil, err
		}
	}
	old := extinguisher(state, id)
	if id != "" && old == nil {
		return nil, missing("灭火器")
	}
	for _, e := range state.Extinguishers {
		if e.ID != id && strings.EqualFold(e.Code, v.Code) {
			return nil, conflict("灭火器编号已存在")
		}
	}
	if old != nil && (v.Status == "retired" || v.StationID != old.StationID) {
		for _, task := range state.Inspections {
			if task.ExtinguisherID == id && inspectionOpen(task) {
				return nil, conflict("灭火器存在未结巡检任务，不能报废或更换消防站")
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
		state.Extinguishers = append(state.Extinguishers, v)
	} else {
		*old = v
	}
	return v, nil
}

func deleteExtinguisher(state *model.FireSafetyState, id string, body json.RawMessage) (any, error) {
	old := extinguisher(state, id)
	if old == nil {
		return nil, missing("灭火器")
	}
	if err := requireVersion(body, old.FireRecord); err != nil {
		return nil, err
	}
	for _, task := range state.Inspections {
		if task.ExtinguisherID == id {
			return nil, conflict("灭火器已有巡检记录，不能删除")
		}
	}
	for i := range state.Extinguishers {
		if state.Extinguishers[i].ID == id {
			state.Extinguishers = append(state.Extinguishers[:i], state.Extinguishers[i+1:]...)
			break
		}
	}
	return deleted(), nil
}

// StandardChecks are the inspection items every extinguisher check must cover.
// Inspectors may add items but cannot rename or omit these.
var StandardChecks = []string{"外观及筒体", "压力指示", "喷管及附件", "铭牌及日期", "放置及标识"}

// maxBatchInspections bounds one batch of inspection tasks.
const maxBatchInspections = 500

// createInspections assigns one person to many due extinguishers. Assets that
// already have an open task or are retired are skipped and reported.
func createInspections(state *model.FireSafetyState, actor string, body json.RawMessage, now int64) (any, error) {
	var in struct {
		ExtinguisherIDs []string `json:"extinguisherIds"`
		AssigneeID      string   `json:"assigneeId"`
		DueAt           int64    `json:"dueAt"`
		Notes           string   `json:"notes"`
	}
	if err := read(body, &in); err != nil {
		return nil, err
	}
	if len(in.ExtinguisherIDs) == 0 || len(in.ExtinguisherIDs) > maxBatchInspections {
		return nil, invalid(fmt.Sprintf("批量巡检须选择1至%d个灭火器", maxBatchInspections))
	}
	if err := uniqueIDs(in.ExtinguisherIDs, "灭火器"); err != nil {
		return nil, err
	}
	type skipped struct {
		ID     string `json:"id"`
		Code   string `json:"code"`
		Reason string `json:"reason"`
	}
	created, skips := []model.FireInspection{}, []skipped{}
	for _, id := range in.ExtinguisherIDs {
		task, _ := json.Marshal(map[string]any{"extinguisherId": id, "assigneeId": in.AssigneeID, "dueAt": in.DueAt, "notes": in.Notes})
		result, err := createInspection(state, actor, task, now)
		if errors.Is(err, ErrConflict) {
			code := id
			if e := extinguisher(state, id); e != nil {
				code = e.Code
			}
			skips = append(skips, skipped{ID: id, Code: code, Reason: strings.TrimPrefix(err.Error(), ErrConflict.Error()+": ")})
			continue
		}
		if err != nil {
			return nil, err
		}
		created = append(created, result.(model.FireInspection))
	}
	if len(created) == 0 {
		return nil, conflict("所选灭火器均已有未结巡检任务或已报废，未创建任务")
	}
	return map[string]any{"items": created, "created": len(created), "skipped": skips}, nil
}

func createInspection(state *model.FireSafetyState, actor string, body json.RawMessage, now int64) (any, error) {
	var v model.FireInspection
	if err := read(body, &v); err != nil {
		return nil, err
	}
	if err := text(&v.Notes, "巡检备注", false, 2000); err != nil {
		return nil, err
	}
	e := extinguisher(state, v.ExtinguisherID)
	if e == nil {
		return nil, invalid("灭火器不存在或不属于当前租户")
	}
	if e.Status == "retired" {
		return nil, conflict("已报废灭火器不能创建巡检任务")
	}
	if e.StationID != "" {
		if err := enabledStation(state, e.StationID); err != nil {
			return nil, err
		}
	}
	p := personnel(state, v.AssigneeID)
	if p == nil || !p.Enabled {
		return nil, invalid("巡检人员不存在或未启用")
	}
	if err := enabledStation(state, p.StationID); err != nil {
		return nil, err
	}
	if err := stamp(v.DueAt, "巡检期限"); err != nil {
		return nil, err
	}
	for _, task := range state.Inspections {
		if task.ExtinguisherID == e.ID && inspectionOpen(task) {
			return nil, conflict("灭火器已有未结巡检任务")
		}
	}
	r, _ := record(model.FireRecord{}, nil, now)
	// Accept only task creation fields; execution and review facts are server owned.
	v = model.FireInspection{FireRecord: r, ExtinguisherID: v.ExtinguisherID, AssigneeID: v.AssigneeID, DueAt: v.DueAt, Status: "pending", Notes: v.Notes, CreatedBy: actor, Checks: []model.FireInspectionCheck{}, Rectifications: []model.FireRectification{}}
	state.Inspections = append(state.Inspections, v)
	return v, nil
}

func inspect(state *model.FireSafetyState, actor, id string, body json.RawMessage, now int64) (any, error) {
	var in struct {
		Checks   []model.FireInspectionCheck `json:"checks"`
		Findings string                      `json:"findings"`
	}
	if err := read(body, &in); err != nil {
		return nil, err
	}
	if len(in.Checks) == 0 || len(in.Checks) > 100 {
		return nil, invalid("检查项目须为1至100项")
	}
	seen := map[string]bool{}
	passed := true
	for i := range in.Checks {
		check := &in.Checks[i]
		if err := text(&check.Name, "检查项目", true, 100); err != nil {
			return nil, err
		}
		key := strings.ToLower(check.Name)
		if seen[key] {
			return nil, invalid("检查项目不能重复")
		}
		seen[key] = true
		passed = passed && check.Passed
	}
	for _, name := range StandardChecks {
		if !seen[strings.ToLower(name)] {
			return nil, invalid("检查项目须包含标准项目“" + name + "”")
		}
	}
	if err := text(&in.Findings, "问题描述", !passed, 4000); err != nil {
		return nil, err
	}
	for i := range state.Inspections {
		v := &state.Inspections[i]
		if v.ID != id {
			continue
		}
		if err := requireVersion(body, v.FireRecord); err != nil {
			return nil, err
		}
		if v.Status != "pending" {
			return nil, conflict("巡检任务已执行或结束，不能重复检查")
		}
		v.Checks, v.Findings, v.InspectedAt, v.InspectedBy = in.Checks, in.Findings, now, actor
		v.Result, v.Status = "pass", "completed"
		if !passed {
			v.Result, v.Status = "fail", "rectifying"
		}
		advance(&v.FireRecord, now)
		return *v, nil
	}
	return nil, missing("巡检任务")
}

func rectify(state *model.FireSafetyState, actor, id string, body json.RawMessage, now int64) (any, error) {
	var in struct {
		Action string `json:"action"`
	}
	if err := read(body, &in); err != nil {
		return nil, err
	}
	if err := text(&in.Action, "整改措施", true, 4000); err != nil {
		return nil, err
	}
	for i := range state.Inspections {
		v := &state.Inspections[i]
		if v.ID != id {
			continue
		}
		if err := requireVersion(body, v.FireRecord); err != nil {
			return nil, err
		}
		if v.Status != "rectifying" {
			return nil, conflict("巡检任务当前不处于整改阶段")
		}
		v.Rectifications = append(v.Rectifications, model.FireRectification{Action: in.Action, SubmittedBy: actor, SubmittedAt: now, Status: "pending"})
		v.Status = "reviewing"
		advance(&v.FireRecord, now)
		return *v, nil
	}
	return nil, missing("巡检任务")
}

func reviewInspection(state *model.FireSafetyState, actor, id string, body json.RawMessage, now int64) (any, error) {
	in, err := readReview(body)
	if err != nil {
		return nil, err
	}
	for i := range state.Inspections {
		v := &state.Inspections[i]
		if v.ID != id {
			continue
		}
		if err := requireVersion(body, v.FireRecord); err != nil {
			return nil, err
		}
		if v.Status != "reviewing" || len(v.Rectifications) == 0 {
			return nil, conflict("巡检任务当前不处于复核阶段")
		}
		r := &v.Rectifications[len(v.Rectifications)-1]
		if r.Status != "pending" {
			return nil, conflict("整改已复核，不能重复复核")
		}
		if r.SubmittedBy == actor {
			return nil, forbidden("不能复核自己提交的整改")
		}
		r.ReviewedBy, r.ReviewedAt, r.ReviewNote = actor, now, in.Note
		r.Status, v.Status = "approved", "completed"
		if !*in.Approved {
			r.Status, v.Status = "rejected", "rectifying"
		}
		advance(&v.FireRecord, now)
		return *v, nil
	}
	return nil, missing("巡检任务")
}

func cancelInspection(state *model.FireSafetyState, id string, body json.RawMessage, now int64) (any, error) {
	var in struct {
		Reason string `json:"reason"`
	}
	if err := read(body, &in); err != nil {
		return nil, err
	}
	if err := text(&in.Reason, "取消原因", true, 2000); err != nil {
		return nil, err
	}
	for i := range state.Inspections {
		v := &state.Inspections[i]
		if v.ID != id {
			continue
		}
		if err := requireVersion(body, v.FireRecord); err != nil {
			return nil, err
		}
		if !inspectionOpen(*v) {
			return nil, conflict("巡检任务已结束，不能取消")
		}
		v.Status, v.CancelReason = "cancelled", in.Reason
		advance(&v.FireRecord, now)
		return *v, nil
	}
	return nil, missing("巡检任务")
}
