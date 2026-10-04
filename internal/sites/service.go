// Package sites manages where devices are installed: units, buildings,
// floors with floor plans, and points that place devices or components.
package sites

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

var (
	ErrValidation = errors.New("单位建筑数据无效")
	ErrNotFound   = errors.New("单位建筑记录不存在")
	ErrConflict   = errors.New("单位建筑数据冲突")
)

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrValidation, fmt.Sprintf(format, args...))
}
func missing(label string) error { return fmt.Errorf("%w: %s不存在", ErrNotFound, label) }
func conflict(message string) error {
	return fmt.Errorf("%w: %s", ErrConflict, message)
}

// Service validates every change against the tenant's whole site state.
type Service struct {
	store ports.SiteStore
	now   func() time.Time
	mu    sync.Mutex
	cache map[string]model.SiteState
}

func New(store ports.SiteStore) *Service {
	return &Service{store: store, now: time.Now, cache: map[string]model.SiteState{}}
}

// Snapshot returns the tenant's sites for reading; callers must not modify
// it. It is reused while the stored revision is unchanged.
func (s *Service) Snapshot(ctx context.Context, tenant string) (model.SiteState, error) {
	revision, err := s.store.SiteRevision(ctx, tenant)
	if err != nil {
		return model.SiteState{}, err
	}
	s.mu.Lock()
	cached, ok := s.cache[tenant]
	s.mu.Unlock()
	if ok && cached.Revision == revision {
		return cached, nil
	}
	state, err := s.store.LoadSiteState(ctx, tenant)
	if err != nil {
		return state, err
	}
	s.mu.Lock()
	if previous, exists := s.cache[tenant]; !exists || previous.Revision <= state.Revision {
		s.cache[tenant] = state
	}
	s.mu.Unlock()
	return state, nil
}

// Revision returns the tenant's site revision.
func (s *Service) Revision(ctx context.Context, tenant string) (int64, error) {
	return s.store.SiteRevision(ctx, tenant)
}

// apply reloads and repeats the change after a competing commit.
func (s *Service) apply(ctx context.Context, tenant string, change func(*model.SiteState, int64) (any, error)) (any, error) {
	if strings.TrimSpace(tenant) == "" {
		return nil, invalid("租户不能为空")
	}
	for attempt := 0; attempt < 8; attempt++ {
		state, err := s.store.LoadSiteState(ctx, tenant)
		if err != nil {
			return nil, err
		}
		result, err := change(&state, s.now().UnixMilli())
		if err != nil {
			return nil, err
		}
		saved, err := s.store.SaveSiteState(ctx, tenant, state)
		if err != nil {
			return nil, err
		}
		if saved {
			return result, nil
		}
	}
	return nil, conflict("数据正在被其他操作更新，请刷新重试")
}

func text(value *string, label string, required bool, max int) error {
	*value = strings.TrimSpace(*value)
	if required && *value == "" {
		return invalid("%s不能为空", label)
	}
	if utf8.RuneCountInString(*value) > max {
		return invalid("%s不能超过 %d 个字符", label, max)
	}
	return nil
}

func texts(checks ...func() error) error {
	for _, check := range checks {
		if err := check(); err != nil {
			return err
		}
	}
	return nil
}

// stamp prepares the record for saving: a new identity, or the next version
// of old when the caller edited the version it read.
func stamp(in *model.SiteRecord, old *model.SiteRecord, actor string, now int64) error {
	if old == nil {
		*in = model.SiteRecord{ID: uuid.NewString(), Version: 1, CreatedAt: now, UpdatedAt: now, UpdatedBy: actor}
		return nil
	}
	if in.Version != old.Version {
		return conflict("记录已更新，请刷新后重试")
	}
	*in = *old
	in.Version++
	in.UpdatedAt, in.UpdatedBy = now, actor
	return nil
}

func find[T any](items []T, id string, key func(*T) string) int {
	for i := range items {
		if key(&items[i]) == id {
			return i
		}
	}
	return -1
}

func unitID(v *model.SiteUnit) string         { return v.ID }
func buildingID(v *model.SiteBuilding) string { return v.ID }
func floorID(v *model.SiteFloor) string       { return v.ID }
func pointID(v *model.SitePoint) string       { return v.ID }

// SaveUnit creates a unit when in.ID is empty, otherwise edits it.
func (s *Service) SaveUnit(ctx context.Context, tenant, actor string, in model.SiteUnit) (model.SiteUnit, error) {
	if err := texts(func() error { return text(&in.Name, "单位名称", true, 100) }, func() error { return text(&in.Code, "单位编码", false, 64) },
		func() error { return text(&in.Address, "地址", false, 200) }, func() error { return text(&in.Contact, "联系人", false, 50) },
		func() error { return text(&in.Phone, "联系电话", false, 32) }, func() error { return text(&in.Notes, "备注", false, 500) }); err != nil {
		return in, err
	}
	result, err := s.apply(ctx, tenant, func(state *model.SiteState, now int64) (any, error) {
		v := in
		index := -1
		if v.ID != "" {
			if index = find(state.Units, v.ID, unitID); index < 0 {
				return nil, missing("单位")
			}
		}
		for _, other := range state.Units {
			if other.ID != v.ID && (other.Name == v.Name || (v.Code != "" && other.Code == v.Code)) {
				return nil, conflict("单位名称或编码已存在")
			}
		}
		var old *model.SiteRecord
		if index >= 0 {
			old = &state.Units[index].SiteRecord
		}
		if err := stamp(&v.SiteRecord, old, actor, now); err != nil {
			return nil, err
		}
		if index >= 0 {
			state.Units[index] = v
		} else {
			state.Units = append(state.Units, v)
		}
		return v, nil
	})
	if err != nil {
		return in, err
	}
	return result.(model.SiteUnit), nil
}

func requireVersion(record model.SiteRecord, version int64) error {
	if record.Version != version {
		return conflict("记录已更新，请刷新后重试")
	}
	return nil
}

// DeleteUnit removes a unit that has no buildings or points.
func (s *Service) DeleteUnit(ctx context.Context, tenant, id string, version int64) error {
	_, err := s.apply(ctx, tenant, func(state *model.SiteState, _ int64) (any, error) {
		index := find(state.Units, id, unitID)
		if index < 0 {
			return nil, missing("单位")
		}
		if err := requireVersion(state.Units[index].SiteRecord, version); err != nil {
			return nil, err
		}
		for _, b := range state.Buildings {
			if b.UnitID == id {
				return nil, conflict("请先删除该单位下的建筑")
			}
		}
		for _, p := range state.Points {
			if p.UnitID == id {
				return nil, conflict("请先删除该单位下的设备点位")
			}
		}
		state.Units = append(state.Units[:index], state.Units[index+1:]...)
		return nil, nil
	})
	return err
}

func (s *Service) SaveBuilding(ctx context.Context, tenant, actor string, in model.SiteBuilding) (model.SiteBuilding, error) {
	if err := texts(func() error { return text(&in.Name, "建筑名称", true, 100) }, func() error { return text(&in.Address, "地址", false, 200) },
		func() error { return text(&in.Notes, "备注", false, 500) }); err != nil {
		return in, err
	}
	if in.AboveFloors < 0 || in.AboveFloors > 300 || in.BelowFloors < 0 || in.BelowFloors > 20 {
		return in, invalid("地上层数为 0 至 300，地下层数为 0 至 20")
	}
	result, err := s.apply(ctx, tenant, func(state *model.SiteState, now int64) (any, error) {
		v := in
		if find(state.Units, v.UnitID, unitID) < 0 {
			return nil, missing("所属单位")
		}
		index := -1
		if v.ID != "" {
			if index = find(state.Buildings, v.ID, buildingID); index < 0 {
				return nil, missing("建筑")
			}
			if state.Buildings[index].UnitID != v.UnitID && hasBuildingPoints(state, v.ID) {
				return nil, conflict("建筑下已有设备点位，不能改到其他单位")
			}
		}
		for _, other := range state.Buildings {
			if other.ID != v.ID && other.UnitID == v.UnitID && other.Name == v.Name {
				return nil, conflict("同一单位下建筑名称重复")
			}
		}
		var old *model.SiteRecord
		if index >= 0 {
			old = &state.Buildings[index].SiteRecord
		}
		if err := stamp(&v.SiteRecord, old, actor, now); err != nil {
			return nil, err
		}
		if index >= 0 {
			state.Buildings[index] = v
		} else {
			state.Buildings = append(state.Buildings, v)
		}
		return v, nil
	})
	if err != nil {
		return in, err
	}
	return result.(model.SiteBuilding), nil
}

func hasBuildingPoints(state *model.SiteState, id string) bool {
	for _, p := range state.Points {
		if p.BuildingID == id {
			return true
		}
	}
	return false
}

func (s *Service) DeleteBuilding(ctx context.Context, tenant, id string, version int64) error {
	_, err := s.apply(ctx, tenant, func(state *model.SiteState, _ int64) (any, error) {
		index := find(state.Buildings, id, buildingID)
		if index < 0 {
			return nil, missing("建筑")
		}
		if err := requireVersion(state.Buildings[index].SiteRecord, version); err != nil {
			return nil, err
		}
		for _, f := range state.Floors {
			if f.BuildingID == id {
				return nil, conflict("请先删除该建筑下的楼层")
			}
		}
		if hasBuildingPoints(state, id) {
			return nil, conflict("请先删除该建筑下的设备点位")
		}
		state.Buildings = append(state.Buildings[:index], state.Buildings[index+1:]...)
		return nil, nil
	})
	return err
}

// SaveFloor edits the floor's name and level; the plan is set separately.
func (s *Service) SaveFloor(ctx context.Context, tenant, actor string, in model.SiteFloor) (model.SiteFloor, error) {
	if err := text(&in.Name, "楼层名称", true, 50); err != nil {
		return in, err
	}
	if in.Level < -20 || in.Level > 300 {
		return in, invalid("楼层序号为 -20 至 300")
	}
	result, err := s.apply(ctx, tenant, func(state *model.SiteState, now int64) (any, error) {
		v := in
		if find(state.Buildings, v.BuildingID, buildingID) < 0 {
			return nil, missing("所属建筑")
		}
		index := -1
		if v.ID != "" {
			if index = find(state.Floors, v.ID, floorID); index < 0 {
				return nil, missing("楼层")
			}
			if state.Floors[index].BuildingID != v.BuildingID {
				return nil, invalid("楼层不能改到其他建筑")
			}
			v.Plan = state.Floors[index].Plan
		} else {
			v.Plan = nil
		}
		for _, other := range state.Floors {
			if other.ID != v.ID && other.BuildingID == v.BuildingID && other.Name == v.Name {
				return nil, conflict("同一建筑下楼层名称重复")
			}
		}
		var old *model.SiteRecord
		if index >= 0 {
			old = &state.Floors[index].SiteRecord
		}
		if err := stamp(&v.SiteRecord, old, actor, now); err != nil {
			return nil, err
		}
		if index >= 0 {
			state.Floors[index] = v
		} else {
			state.Floors = append(state.Floors, v)
		}
		return v, nil
	})
	if err != nil {
		return in, err
	}
	return result.(model.SiteFloor), nil
}

// SetFloorPlan records an uploaded plan and returns the floor and the plan
// it replaced, if any.
func (s *Service) SetFloorPlan(ctx context.Context, tenant, actor, id string, version int64, plan model.FloorPlan) (model.SiteFloor, *model.FloorPlan, error) {
	var previous *model.FloorPlan
	result, err := s.apply(ctx, tenant, func(state *model.SiteState, now int64) (any, error) {
		index := find(state.Floors, id, floorID)
		if index < 0 {
			return nil, missing("楼层")
		}
		v := state.Floors[index]
		if err := requireVersion(v.SiteRecord, version); err != nil {
			return nil, err
		}
		previous = v.Plan
		plan.UploadedAt = now
		v.Plan = &plan
		v.Version++
		v.UpdatedAt, v.UpdatedBy = now, actor
		state.Floors[index] = v
		return v, nil
	})
	if err != nil {
		return model.SiteFloor{}, nil, err
	}
	return result.(model.SiteFloor), previous, nil
}

// DeleteFloor removes a floor without points and returns its plan so the
// caller can remove the image.
func (s *Service) DeleteFloor(ctx context.Context, tenant, id string, version int64) (*model.FloorPlan, error) {
	var plan *model.FloorPlan
	_, err := s.apply(ctx, tenant, func(state *model.SiteState, _ int64) (any, error) {
		index := find(state.Floors, id, floorID)
		if index < 0 {
			return nil, missing("楼层")
		}
		if err := requireVersion(state.Floors[index].SiteRecord, version); err != nil {
			return nil, err
		}
		for _, p := range state.Points {
			if p.FloorID == id {
				return nil, conflict("请先删除该楼层上的设备点位")
			}
		}
		plan = state.Floors[index].Plan
		state.Floors = append(state.Floors[:index], state.Floors[index+1:]...)
		return nil, nil
	})
	return plan, err
}

func validFraction(v *float64) bool {
	return v != nil && !math.IsNaN(*v) && *v >= 0 && *v <= 1
}

// placePoint fills the unit and building from the floor or building and
// checks the coordinates.
func placePoint(state *model.SiteState, v *model.SitePoint) error {
	if v.FloorID != "" {
		index := find(state.Floors, v.FloorID, floorID)
		if index < 0 {
			return missing("楼层")
		}
		floor := state.Floors[index]
		if v.BuildingID != "" && v.BuildingID != floor.BuildingID {
			return invalid("楼层不属于所选建筑")
		}
		v.BuildingID = floor.BuildingID
	}
	if v.BuildingID != "" {
		index := find(state.Buildings, v.BuildingID, buildingID)
		if index < 0 {
			return missing("建筑")
		}
		if v.UnitID != "" && v.UnitID != state.Buildings[index].UnitID {
			return invalid("建筑不属于所选单位")
		}
		v.UnitID = state.Buildings[index].UnitID
	}
	if find(state.Units, v.UnitID, unitID) < 0 {
		return missing("所属单位")
	}
	if (v.X == nil) != (v.Y == nil) {
		return invalid("平面图坐标需要同时提供横向和纵向位置")
	}
	if v.X != nil {
		if !validFraction(v.X) || !validFraction(v.Y) {
			return invalid("平面图坐标须在 0 至 1 之间")
		}
		floor := find(state.Floors, v.FloorID, floorID)
		if floor < 0 || state.Floors[floor].Plan == nil {
			return invalid("请先为楼层上传平面图再标注位置")
		}
	}
	return nil
}

func (s *Service) SavePoint(ctx context.Context, tenant, actor string, in model.SitePoint) (model.SitePoint, error) {
	if err := texts(func() error { return text(&in.DeviceID, "设备编号", true, 128) }, func() error { return text(&in.ComponentID, "部件编号", false, 128) },
		func() error { return text(&in.Name, "点位名称", false, 100) }); err != nil {
		return in, err
	}
	result, err := s.apply(ctx, tenant, func(state *model.SiteState, now int64) (any, error) {
		v := in
		if err := placePoint(state, &v); err != nil {
			return nil, err
		}
		index := -1
		if v.ID != "" {
			if index = find(state.Points, v.ID, pointID); index < 0 {
				return nil, missing("点位")
			}
		}
		for _, other := range state.Points {
			if other.ID != v.ID && other.DeviceID == v.DeviceID && other.ComponentID == v.ComponentID {
				return nil, conflict("该设备或部件已有点位")
			}
		}
		var old *model.SiteRecord
		if index >= 0 {
			old = &state.Points[index].SiteRecord
		}
		if err := stamp(&v.SiteRecord, old, actor, now); err != nil {
			return nil, err
		}
		if index >= 0 {
			state.Points[index] = v
		} else {
			state.Points = append(state.Points, v)
		}
		return v, nil
	})
	if err != nil {
		return in, err
	}
	return result.(model.SitePoint), nil
}

func (s *Service) DeletePoint(ctx context.Context, tenant, id string, version int64) (model.SitePoint, error) {
	var removed model.SitePoint
	_, err := s.apply(ctx, tenant, func(state *model.SiteState, _ int64) (any, error) {
		index := find(state.Points, id, pointID)
		if index < 0 {
			return nil, missing("点位")
		}
		if err := requireVersion(state.Points[index].SiteRecord, version); err != nil {
			return nil, err
		}
		removed = state.Points[index]
		state.Points = append(state.Points[:index], state.Points[index+1:]...)
		return nil, nil
	})
	return removed, err
}

// DeviceUnits maps each device with a device-level point to its unit.
func DeviceUnits(state model.SiteState) map[string]string {
	out := make(map[string]string, len(state.Points))
	for _, p := range state.Points {
		if p.ComponentID == "" {
			out[p.DeviceID] = p.UnitID
		}
	}
	return out
}

// Locate returns the position of a component, falling back to its device.
func Locate(state model.SiteState, deviceID, componentID string) *model.AlarmLocation {
	var point *model.SitePoint
	for i := range state.Points {
		p := &state.Points[i]
		if p.DeviceID != deviceID {
			continue
		}
		if p.ComponentID == componentID {
			point = p
			break
		}
		if p.ComponentID == "" && point == nil {
			point = p
		}
	}
	if point == nil {
		return nil
	}
	location := &model.AlarmLocation{UnitID: point.UnitID, BuildingID: point.BuildingID, FloorID: point.FloorID, PointID: point.ID, PointName: point.Name}
	if point.FloorID != "" && point.X != nil && point.Y != nil {
		x, y := *point.X, *point.Y
		location.X, location.Y = &x, &y
	}
	if i := find(state.Units, point.UnitID, unitID); i >= 0 {
		location.UnitName = state.Units[i].Name
	}
	if i := find(state.Buildings, point.BuildingID, buildingID); i >= 0 {
		location.BuildingName = state.Buildings[i].Name
	}
	if i := find(state.Floors, point.FloorID, floorID); i >= 0 {
		location.FloorName = state.Floors[i].Name
	}
	return location
}

// AlarmLocation implements ports.AlarmLocator. A storage failure leaves the
// alarm without a location rather than failing the alarm.
func (s *Service) AlarmLocation(ctx context.Context, tenant, deviceID, componentID string) *model.AlarmLocation {
	state, err := s.Snapshot(ctx, tenant)
	if err != nil {
		return nil
	}
	return Locate(state, deviceID, componentID)
}

// SortState orders records for display: units and buildings by name,
// floors by level descending, points by device.
func SortState(state *model.SiteState) {
	sort.SliceStable(state.Units, func(i, j int) bool { return state.Units[i].Name < state.Units[j].Name })
	sort.SliceStable(state.Buildings, func(i, j int) bool { return state.Buildings[i].Name < state.Buildings[j].Name })
	sort.SliceStable(state.Floors, func(i, j int) bool { return state.Floors[i].Level > state.Floors[j].Level })
	sort.SliceStable(state.Points, func(i, j int) bool {
		if state.Points[i].DeviceID != state.Points[j].DeviceID {
			return state.Points[i].DeviceID < state.Points[j].DeviceID
		}
		return state.Points[i].ComponentID < state.Points[j].ComponentID
	})
}
