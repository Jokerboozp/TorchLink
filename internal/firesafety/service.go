// Package firesafety manages tenant-scoped fire resources and duty workflows.
package firesafety

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

var (
	ErrValidation = errors.New("消防管理数据无效")
	ErrNotFound   = errors.New("消防管理记录不存在")
	ErrConflict   = errors.New("消防管理数据冲突")
	ErrForbidden  = errors.New("没有执行此操作的权限")
)

type Service struct {
	store ports.FireSafetyStore
	now   func() time.Time
}

func New(store ports.FireSafetyStore) *Service { return &Service{store: store, now: time.Now} }

func (s *Service) Snapshot(ctx context.Context, tenant string) (model.FireSafetyState, error) {
	if strings.TrimSpace(tenant) == "" {
		return model.FireSafetyState{}, invalid("租户不能为空")
	}
	state, err := s.store.LoadFireSafetyState(ctx, tenant)
	if err != nil {
		return state, err
	}
	normalize(&state)
	return state, nil
}

// Apply reloads and revalidates after a competing commit. Record versions
// prevent a stale edit from overwriting that commit; new workflow operations
// also repeat relationship and occupancy checks against the winning state.
func (s *Service) Apply(ctx context.Context, tenant, actor, action, id string, body json.RawMessage) (any, error) {
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(actor) == "" {
		return nil, invalid("租户和操作人不能为空")
	}
	if len(body) > 128*1024 {
		return nil, invalid("提交内容过大")
	}
	for attempt := 0; attempt < 8; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		state, err := s.Snapshot(ctx, tenant)
		if err != nil {
			return nil, err
		}
		result, err := mutate(&state, actor, action, id, body, s.now().UnixMilli())
		if err != nil {
			return nil, err
		}
		saved, err := s.store.SaveFireSafetyState(ctx, tenant, state)
		if err != nil {
			return nil, err
		}
		if saved {
			return result, nil
		}
	}
	return nil, conflict("数据正在被其他操作更新，请刷新重试")
}

func normalize(v *model.FireSafetyState) {
	if v.Stations == nil {
		v.Stations = []model.FireStation{}
	}
	if v.Personnel == nil {
		v.Personnel = []model.FirePersonnel{}
	}
	if v.Equipment == nil {
		v.Equipment = []model.FireEquipment{}
	}
	if v.Dispatches == nil {
		v.Dispatches = []model.FireDispatch{}
	}
	if v.Shifts == nil {
		v.Shifts = []model.DutyShift{}
	}
	if v.Assignments == nil {
		v.Assignments = []model.DutyAssignment{}
	}
	if v.Swaps == nil {
		v.Swaps = []model.DutySwap{}
	}
	if v.Extinguishers == nil {
		v.Extinguishers = []model.Extinguisher{}
	}
	if v.Inspections == nil {
		v.Inspections = []model.FireInspection{}
	}
}

func mutate(v *model.FireSafetyState, actor, action, id string, body json.RawMessage, now int64) (any, error) {
	switch action {
	case "saveStation":
		return saveStation(v, id, body, now)
	case "deleteStation":
		return deleteStation(v, id, body)
	case "savePersonnel":
		return savePersonnel(v, id, body, now)
	case "deletePersonnel":
		return deletePersonnel(v, id, body)
	case "saveEquipment":
		return saveEquipment(v, id, body, now)
	case "deleteEquipment":
		return deleteEquipment(v, id, body)
	case "createDispatch":
		return createDispatch(v, actor, body, now)
	case "returnDispatch":
		return returnDispatch(v, actor, id, body, now)
	case "saveShift":
		return saveShift(v, id, body, now)
	case "deleteShift":
		return deleteShift(v, id, body)
	case "saveAssignment":
		return saveAssignment(v, id, body, now)
	case "deleteAssignment":
		return deleteAssignment(v, id, body)
	case "createAssignments":
		return createAssignments(v, body, now)
	case "createSwap":
		return createSwap(v, actor, body, now)
	case "reviewSwap":
		return reviewSwap(v, actor, id, body, now)
	case "saveExtinguisher":
		return saveExtinguisher(v, id, body, now)
	case "deleteExtinguisher":
		return deleteExtinguisher(v, id, body)
	case "createInspection":
		return createInspection(v, actor, body, now)
	case "createInspections":
		return createInspections(v, actor, body, now)
	case "inspect":
		return inspect(v, actor, id, body, now)
	case "rectify":
		return rectify(v, actor, id, body, now)
	case "reviewInspection":
		return reviewInspection(v, actor, id, body, now)
	case "cancelInspection":
		return cancelInspection(v, id, body, now)
	default:
		return nil, invalid("不支持的消防管理操作")
	}
}

func invalid(message string) error   { return fmt.Errorf("%w: %s", ErrValidation, message) }
func conflict(message string) error  { return fmt.Errorf("%w: %s", ErrConflict, message) }
func missing(label string) error     { return fmt.Errorf("%w: %s不存在", ErrNotFound, label) }
func forbidden(message string) error { return fmt.Errorf("%w: %s", ErrForbidden, message) }
func read(body json.RawMessage, out any) error {
	if len(body) == 0 || strings.TrimSpace(string(body)) == "null" {
		return invalid("提交内容不能为空")
	}
	if err := json.Unmarshal(body, out); err != nil {
		return invalid("提交内容格式错误")
	}
	return nil
}
func text(value *string, label string, required bool, max int) error {
	*value = strings.TrimSpace(*value)
	if required && *value == "" {
		return invalid(label + "不能为空")
	}
	if utf8.RuneCountInString(*value) > max {
		return invalid(fmt.Sprintf("%s不能超过%d个字符", label, max))
	}
	return nil
}

type field struct {
	value    *string
	label    string
	required bool
	max      int
}

func fields(values ...field) error {
	for _, v := range values {
		if err := text(v.value, v.label, v.required, v.max); err != nil {
			return err
		}
	}
	return nil
}
func oneOf(value, label string, choices ...string) error {
	for _, choice := range choices {
		if value == choice {
			return nil
		}
	}
	return invalid(label + "无效")
}
func stamp(value int64, label string) error {
	if value <= 0 || value > time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli() {
		return invalid(label + "无效")
	}
	return nil
}
func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }
func date(value, label string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	v, err := time.Parse("2006-01-02", value)
	if err != nil || v.Year() < 1900 || v.Year() > 2199 {
		return time.Time{}, invalid(label + "须为有效的 YYYY-MM-DD 日期")
	}
	return v, nil
}
func record(in model.FireRecord, old *model.FireRecord, now int64) (model.FireRecord, error) {
	if old == nil {
		return model.FireRecord{ID: uuid.NewString(), Version: 1, CreatedAt: now, UpdatedAt: now}, nil
	}
	if in.Version != old.Version {
		return model.FireRecord{}, conflict("记录已更新，请刷新后重试")
	}
	copy := *old
	copy.Version++
	copy.UpdatedAt = now
	return copy, nil
}
func requireVersion(body json.RawMessage, old model.FireRecord) error {
	var in struct {
		Version int64 `json:"version"`
	}
	if err := read(body, &in); err != nil {
		return err
	}
	if in.Version != old.Version {
		return conflict("记录已更新，请刷新后重试")
	}
	return nil
}
func advance(v *model.FireRecord, now int64) { v.Version++; v.UpdatedAt = now }
func deleted() any                           { return map[string]bool{"success": true} }
func contains(items []string, id string) bool {
	for _, v := range items {
		if v == id {
			return true
		}
	}
	return false
}
func uniqueIDs(ids []string, label string) error {
	if len(ids) == 0 {
		return invalid(label + "不能为空")
	}
	if len(ids) > 200 {
		return invalid(label + "最多200项")
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if strings.TrimSpace(id) == "" || seen[id] {
			return invalid(label + "存在空值或重复项")
		}
		seen[id] = true
	}
	return nil
}
func station(v *model.FireSafetyState, id string) *model.FireStation {
	for i := range v.Stations {
		if v.Stations[i].ID == id {
			return &v.Stations[i]
		}
	}
	return nil
}
func personnel(v *model.FireSafetyState, id string) *model.FirePersonnel {
	for i := range v.Personnel {
		if v.Personnel[i].ID == id {
			return &v.Personnel[i]
		}
	}
	return nil
}
func equipment(v *model.FireSafetyState, id string) *model.FireEquipment {
	for i := range v.Equipment {
		if v.Equipment[i].ID == id {
			return &v.Equipment[i]
		}
	}
	return nil
}
func shift(v *model.FireSafetyState, id string) *model.DutyShift {
	for i := range v.Shifts {
		if v.Shifts[i].ID == id {
			return &v.Shifts[i]
		}
	}
	return nil
}
func assignment(v *model.FireSafetyState, id string) *model.DutyAssignment {
	for i := range v.Assignments {
		if v.Assignments[i].ID == id {
			return &v.Assignments[i]
		}
	}
	return nil
}
func extinguisher(v *model.FireSafetyState, id string) *model.Extinguisher {
	for i := range v.Extinguishers {
		if v.Extinguishers[i].ID == id {
			return &v.Extinguishers[i]
		}
	}
	return nil
}
func inspectionOpen(v model.FireInspection) bool {
	return v.Status != "completed" && v.Status != "cancelled"
}
func enabledStation(v *model.FireSafetyState, id string) error {
	s := station(v, id)
	if s == nil {
		return invalid("消防站不存在或不属于当前租户")
	}
	if !s.Enabled {
		return conflict("消防站已停用")
	}
	return nil
}
