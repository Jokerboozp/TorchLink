package httpapi

import (
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"net/http"
	"strings"
)

type deviceScopeKey struct{}
type deviceScope struct {
	Tenant string
	All    bool
	IDs    map[string]bool
}

func scopeFor(u model.PlatformUser, p map[string]bool, tenant string) deviceScope {
	v := deviceScope{Tenant: tenant, IDs: map[string]bool{}}
	if !p["menu:devices"] {
		return v
	}
	v.All = u.DeviceScope == "all"
	if u.DeviceScope == "selected" {
		for _, id := range u.DeviceIDs {
			v.IDs[id] = true
		}
	}
	return v
}
func requestScope(ctx context.Context) (deviceScope, bool) {
	v, ok := ctx.Value(deviceScopeKey{}).(deviceScope)
	return v, ok
}
func deviceAllowed(ctx context.Context, tenant, id string) bool {
	v, ok := requestScope(ctx)
	return !ok || (tenant == v.Tenant && (v.All || v.IDs[id]))
}
func limited(ctx context.Context) bool { v, ok := requestScope(ctx); return ok && !v.All }

var errDeviceScope = errors.New("设备不存在或无访问权限")

type deviceScopeRepository struct{ ports.Repository }

func (r *deviceScopeRepository) scopedIDs(ctx context.Context, tenant string, ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if deviceAllowed(ctx, tenant, id) {
			out = append(out, id)
		}
	}
	return out
}

func (r *deviceScopeRepository) GetDeviceStatesByIDs(ctx context.Context, tenant string, ids []string) (map[string]model.DeviceState, error) {
	return r.Repository.GetDeviceStatesByIDs(ctx, tenant, r.scopedIDs(ctx, tenant, ids))
}

func (r *deviceScopeRepository) GetStandardMessagesByRawIDs(ctx context.Context, tenant string, ids []string) (map[string]model.StandardMessage, error) {
	items, err := r.Repository.GetStandardMessagesByRawIDs(ctx, tenant, ids)
	if err != nil {
		return nil, err
	}
	for id, item := range items {
		if !deviceAllowed(ctx, tenant, item.DeviceID) {
			delete(items, id)
		}
	}
	return items, nil
}

func (r *deviceScopeRepository) ListVideoCameraMappingsByDeviceIDs(ctx context.Context, tenant string, ids []string) (map[string][]model.VideoCameraMapping, error) {
	return r.Repository.ListVideoCameraMappingsByDeviceIDs(ctx, tenant, r.scopedIDs(ctx, tenant, ids))
}

func (s *Server) unscopedRepo() ports.Repository {
	if r, ok := s.engine.Repo.(*deviceScopeRepository); ok {
		return r.Repository
	}
	return s.engine.Repo
}
func pageSlice[T any](items []T, limit, offset int) []T {
	if offset < 0 {
		offset = 0
	}
	if offset > len(items) {
		offset = len(items)
	}
	end := len(items)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return items[offset:end]
}

// Filtering occurs before pagination and totals. The wrapper is shared, while
// the scope lives only in the authenticated request context; background ingest
// and internal maintenance retain their existing repository behavior.
func (r *deviceScopeRepository) ListManagedDevices(ctx context.Context, t string) ([]model.ManagedDevice, error) {
	rows, e := r.Repository.ListManagedDevices(ctx, t)
	if e != nil {
		return nil, e
	}
	out := []model.ManagedDevice{}
	for _, v := range rows {
		if deviceAllowed(ctx, t, v.ID) {
			if !deviceAllowed(ctx, t, v.GatewayID) {
				v.GatewayID = ""
			}
			out = append(out, v)
		}
	}
	return out, nil
}
func (r *deviceScopeRepository) ListManagedDevicesPage(ctx context.Context, t string, l, o int) ([]model.ManagedDevice, int, error) {
	if !limited(ctx) {
		return r.Repository.ListManagedDevicesPage(ctx, t, l, o)
	}
	rows, e := r.ListManagedDevices(ctx, t)
	return pageSlice(rows, l, o), len(rows), e
}
func (r *deviceScopeRepository) ListManagedDeviceChildren(ctx context.Context, t, id string, l, o int) ([]model.ManagedDevice, int, error) {
	if !deviceAllowed(ctx, t, id) {
		return nil, 0, errDeviceScope
	}
	if !limited(ctx) {
		return r.Repository.ListManagedDeviceChildren(ctx, t, id, l, o)
	}
	rows, e := r.ListManagedDevices(ctx, t)
	out := []model.ManagedDevice{}
	for _, v := range rows {
		if v.GatewayID == id {
			out = append(out, v)
		}
	}
	return pageSlice(out, l, o), len(out), e
}
func (r *deviceScopeRepository) CountManagedDeviceChildren(ctx context.Context, t string, ids []string) (map[string]int, error) {
	if !limited(ctx) {
		return r.Repository.CountManagedDeviceChildren(ctx, t, ids)
	}
	rows, e := r.ListManagedDevices(ctx, t)
	out := map[string]int{}
	for _, v := range rows {
		if v.GatewayID != "" {
			out[v.GatewayID]++
		}
	}
	return out, e
}
func (r *deviceScopeRepository) ListDeviceStates(ctx context.Context, t string) ([]model.DeviceState, error) {
	rows, e := r.Repository.ListDeviceStates(ctx, t)
	out := []model.DeviceState{}
	for _, v := range rows {
		if deviceAllowed(ctx, t, v.DeviceID) {
			out = append(out, v)
		}
	}
	return out, e
}
func (r *deviceScopeRepository) ListDeviceStatesPage(ctx context.Context, t string, l, o int) ([]model.DeviceState, int, error) {
	if !limited(ctx) {
		return r.Repository.ListDeviceStatesPage(ctx, t, l, o)
	}
	rows, e := r.ListDeviceStates(ctx, t)
	return pageSlice(rows, l, o), len(rows), e
}
func (r *deviceScopeRepository) ListUnregisteredDeviceStatesPage(ctx context.Context, t string, l, o int) ([]model.DeviceState, int, error) {
	if !limited(ctx) {
		return r.Repository.ListUnregisteredDeviceStatesPage(ctx, t, l, o)
	}
	// Selected grants refer to registered devices only.
	return []model.DeviceState{}, 0, nil
}
func (r *deviceScopeRepository) CountDeviceStates(ctx context.Context, t string, unregistered bool) (int, int, error) {
	if !limited(ctx) {
		return r.Repository.CountDeviceStates(ctx, t, unregistered)
	}
	if unregistered {
		return 0, 0, nil
	}
	rows, e := r.ListDeviceStates(ctx, t)
	online := 0
	for _, v := range rows {
		if v.BusinessStatus == "ONLINE" || v.BusinessStatus == "ALARM" {
			online++
		}
	}
	return len(rows), online, e
}
func (r *deviceScopeRepository) scopedAlarms(ctx context.Context, f ports.AlarmFilter) ([]model.Alarm, error) {
	out := []model.Alarm{}
	if f.DeviceID != "" && !deviceAllowed(ctx, f.TenantID, f.DeviceID) {
		return out, nil
	}
	f.Offset = 0
	f.Limit = 500
	for {
		rows, e := r.Repository.ListAlarms(ctx, f)
		if e != nil {
			return nil, e
		}
		for _, v := range rows {
			if deviceAllowed(ctx, f.TenantID, v.DeviceID) {
				out = append(out, v)
			}
		}
		if len(rows) < f.Limit {
			return out, nil
		}
		f.Offset += len(rows)
	}
}
func (r *deviceScopeRepository) ListAlarms(ctx context.Context, f ports.AlarmFilter) ([]model.Alarm, error) {
	if !limited(ctx) {
		return r.Repository.ListAlarms(ctx, f)
	}
	rows, e := r.scopedAlarms(ctx, f)
	return pageSlice(rows, f.Limit, f.Offset), e
}
func (r *deviceScopeRepository) CountAlarms(ctx context.Context, f ports.AlarmFilter) (int, error) {
	if !limited(ctx) {
		return r.Repository.CountAlarms(ctx, f)
	}
	rows, e := r.scopedAlarms(ctx, f)
	return len(rows), e
}
func (r *deviceScopeRepository) scopedRaw(ctx context.Context, f ports.RawFilter) ([]model.RawArchiveIndex, error) {
	out := []model.RawArchiveIndex{}
	if f.DeviceID != "" && !deviceAllowed(ctx, f.TenantID, f.DeviceID) {
		return out, nil
	}
	f.Offset = 0
	f.Limit = 500
	for {
		rows, e := r.Repository.ListRawIndexes(ctx, f)
		if e != nil {
			return nil, e
		}
		for _, v := range rows {
			if deviceAllowed(ctx, f.TenantID, v.DeviceID) {
				out = append(out, v)
			}
		}
		if len(rows) < f.Limit {
			return out, nil
		}
		f.Offset += len(rows)
	}
}
func (r *deviceScopeRepository) ListRawIndexes(ctx context.Context, f ports.RawFilter) ([]model.RawArchiveIndex, error) {
	if !limited(ctx) {
		return r.Repository.ListRawIndexes(ctx, f)
	}
	rows, e := r.scopedRaw(ctx, f)
	return pageSlice(rows, f.Limit, f.Offset), e
}
func (r *deviceScopeRepository) CountRawIndexes(ctx context.Context, f ports.RawFilter) (int, error) {
	if !limited(ctx) {
		return r.Repository.CountRawIndexes(ctx, f)
	}
	rows, e := r.scopedRaw(ctx, f)
	return len(rows), e
}
func (r *deviceScopeRepository) DashboardCounts(ctx context.Context, t string, start, end int64) ([]model.DashboardCount, error) {
	if !limited(ctx) {
		return r.Repository.DashboardCounts(ctx, t, start, end)
	}
	scope, _ := requestScope(ctx)
	ids := make([]string, 0, len(scope.IDs))
	for id := range scope.IDs {
		ids = append(ids, id)
	}
	return r.Repository.DashboardCountsForDevices(ctx, t, start, end, ids)
}
func (s *Server) accessDeviceOptions(w http.ResponseWriter, r *http.Request) {
	rows, e := s.unscopedRepo().ListManagedDevices(r.Context(), claims(r).TenantID)
	if e != nil {
		problem(w, 500, "读取设备选项失败")
		return
	}
	out := []map[string]string{}
	for _, v := range rows {
		out = append(out, map[string]string{"id": v.ID, "name": v.Name, "deviceRole": v.DeviceRole})
	}
	write(w, 200, map[string]any{"items": out, "tenantId": claims(r).TenantID})
}
func (s *Server) allowScopedRequest(c *gin.Context, v deviceScope) bool {
	path := c.FullPath()
	ctx := c.Request.Context()
	t := v.Tenant
	if strings.HasPrefix(path, "/api/v1/device-registry/:id") || strings.HasPrefix(path, "/api/v1/discovered-devices/:id") {
		if !deviceAllowed(ctx, t, c.Param("id")) {
			return false
		}
	}
	if id := c.Param("deviceId"); id != "" && !deviceAllowed(ctx, t, id) {
		return false
	}
	alarmID := c.Param("alarmId")
	if strings.HasPrefix(path, "/api/v1/alarms/:id") {
		alarmID = c.Param("id")
	}
	if alarmID != "" {
		if _, e := s.engine.Repo.GetAlarm(ctx, t, alarmID); e != nil {
			return false
		}
	}
	if !v.All {
		// Tenant-wide jobs and configuration can expose other devices. Their menus
		// are also removed from the effective permission list.
		if strings.HasPrefix(path, "/api/v1/replays") || strings.HasSuffix(path, "/replay") {
			return false
		}
		if c.Request.Method != "GET" && (path == "/api/v1/device-registry" || path == "/api/v1/device-states" || path == "/api/v1/raw-messages") {
			return false
		}
	}
	return true
}

func (r *deviceScopeRepository) GetManagedDevice(ctx context.Context, t, id string) (model.ManagedDevice, error) {
	v, e := r.Repository.GetManagedDevice(ctx, t, id)
	if e != nil {
		return v, e
	}
	if !deviceAllowed(ctx, t, v.ID) {
		return model.ManagedDevice{}, errDeviceScope
	}
	return v, nil
}

func (r *deviceScopeRepository) GetDeviceState(ctx context.Context, t, id string) (model.DeviceState, error) {
	v, e := r.Repository.GetDeviceState(ctx, t, id)
	if e != nil {
		return v, e
	}
	if !deviceAllowed(ctx, t, v.DeviceID) {
		return model.DeviceState{}, errDeviceScope
	}
	return v, nil
}

func (r *deviceScopeRepository) GetLatestMessage(ctx context.Context, t, id string) (model.StandardMessage, error) {
	v, e := r.Repository.GetLatestMessage(ctx, t, id)
	if e != nil {
		return v, e
	}
	if !deviceAllowed(ctx, t, v.DeviceID) {
		return model.StandardMessage{}, errDeviceScope
	}
	return v, nil
}

func (r *deviceScopeRepository) GetRawIndex(ctx context.Context, t, id string) (model.RawArchiveIndex, error) {
	v, e := r.Repository.GetRawIndex(ctx, t, id)
	if e != nil {
		return v, e
	}
	if !deviceAllowed(ctx, t, v.DeviceID) {
		return model.RawArchiveIndex{}, errDeviceScope
	}
	return v, nil
}

func (r *deviceScopeRepository) GetStandardMessageByRaw(ctx context.Context, t, id string) (model.StandardMessage, error) {
	v, e := r.Repository.GetStandardMessageByRaw(ctx, t, id)
	if e != nil {
		return v, e
	}
	if !deviceAllowed(ctx, t, v.DeviceID) {
		return model.StandardMessage{}, errDeviceScope
	}
	return v, nil
}

func (r *deviceScopeRepository) GetAlarm(ctx context.Context, t, id string) (model.Alarm, error) {
	v, e := r.Repository.GetAlarm(ctx, t, id)
	if e != nil {
		return v, e
	}
	if !deviceAllowed(ctx, t, v.DeviceID) {
		return model.Alarm{}, errDeviceScope
	}
	return v, nil
}

func (r *deviceScopeRepository) SaveManagedDevice(ctx context.Context, v model.ManagedDevice) error {
	if !deviceAllowed(ctx, v.TenantID, v.ID) {
		return errDeviceScope
	}
	return r.Repository.SaveManagedDevice(ctx, v)
}

func (r *deviceScopeRepository) UpsertDeviceState(ctx context.Context, v model.DeviceState) error {
	if !deviceAllowed(ctx, v.TenantID, v.DeviceID) {
		return errDeviceScope
	}
	return r.Repository.UpsertDeviceState(ctx, v)
}

func (r *deviceScopeRepository) UpdateAlarm(ctx context.Context, v model.Alarm) error {
	if !deviceAllowed(ctx, v.TenantID, v.DeviceID) {
		return errDeviceScope
	}
	return r.Repository.UpdateAlarm(ctx, v)
}

func (r *deviceScopeRepository) PropertyHistoryPage(ctx context.Context, t, d, p string, start, end int64, l, o int) ([]map[string]any, int, error) {
	if !deviceAllowed(ctx, t, d) {
		return nil, 0, errDeviceScope
	}
	return r.Repository.PropertyHistoryPage(ctx, t, d, p, start, end, l, o)
}
