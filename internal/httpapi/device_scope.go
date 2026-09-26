package httpapi

import (
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"net/http"
	"sort"
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

// ScopedRepository wraps repo with per-request device scope checks. Install it
// before the engine starts: New replaces an unwrapped engine.Repo, which races
// with the engine's background readers once they are running. Without a scope
// in the request context the wrapper passes calls through unchanged.
func ScopedRepository(repo ports.Repository) ports.Repository {
	if _, ok := repo.(*deviceScopeRepository); ok {
		return repo
	}
	return &deviceScopeRepository{Repository: repo}
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
	return r.ListManagedDevicesFiltered(ctx, ports.DeviceFilter{TenantID: t}, l, o)
}

// grantedIDs lists the request's granted devices of tenant t in a stable order.
func grantedIDs(ctx context.Context, t string) []string {
	v, _ := requestScope(ctx)
	ids := []string{}
	if v.Tenant != t {
		return ids
	}
	for id := range v.IDs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Limited users pass their device grant to the store, which filters, counts
// and pages in one query instead of loading the tenant's whole registry.
func (r *deviceScopeRepository) ListManagedDevicesFiltered(ctx context.Context, f ports.DeviceFilter, l, o int) ([]model.ManagedDevice, int, error) {
	if !limited(ctx) {
		return r.Repository.ListManagedDevicesFiltered(ctx, f, l, o)
	}
	f.RestrictDevices, f.DeviceIDs = true, grantedIDs(ctx, f.TenantID)
	rows, total, e := r.Repository.ListManagedDevicesFiltered(ctx, f, l, o)
	for i := range rows {
		if !deviceAllowed(ctx, f.TenantID, rows[i].GatewayID) {
			rows[i].GatewayID = ""
		}
	}
	return rows, total, e
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
	if !limited(ctx) {
		rows, e := r.Repository.ListDeviceStates(ctx, t)
		out := []model.DeviceState{}
		for _, v := range rows {
			if deviceAllowed(ctx, t, v.DeviceID) {
				out = append(out, v)
			}
		}
		return out, e
	}
	// Read only the granted devices' states, newest first like the store does.
	states, e := r.Repository.GetDeviceStatesByIDs(ctx, t, grantedIDs(ctx, t))
	out := make([]model.DeviceState, 0, len(states))
	for _, v := range states {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].LastSeenAt != out[j].LastSeenAt {
			return out[i].LastSeenAt > out[j].LastSeenAt
		}
		return out[i].DeviceID < out[j].DeviceID
	})
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

// scopedDevices narrows a list query to the request's granted devices so the
// store filters, counts and paginates in one query. ok=false matches nothing.
func (r *deviceScopeRepository) scopedDevices(ctx context.Context, tenant, device string, ids []string) ([]string, bool) {
	if device != "" && !deviceAllowed(ctx, tenant, device) {
		return nil, false
	}
	if ids == nil {
		if device != "" {
			return nil, true
		}
		v, _ := requestScope(ctx)
		for id := range v.IDs {
			ids = append(ids, id)
		}
		sort.Strings(ids)
	}
	ids = r.scopedIDs(ctx, tenant, ids)
	return ids, len(ids) > 0
}
func (r *deviceScopeRepository) ListAlarms(ctx context.Context, f ports.AlarmFilter) ([]model.Alarm, error) {
	if limited(ctx) {
		var ok bool
		if f.DeviceIDs, ok = r.scopedDevices(ctx, f.TenantID, f.DeviceID, f.DeviceIDs); !ok {
			return []model.Alarm{}, nil
		}
	}
	return r.Repository.ListAlarms(ctx, f)
}
func (r *deviceScopeRepository) CountAlarms(ctx context.Context, f ports.AlarmFilter) (int, error) {
	if limited(ctx) {
		var ok bool
		if f.DeviceIDs, ok = r.scopedDevices(ctx, f.TenantID, f.DeviceID, f.DeviceIDs); !ok {
			return 0, nil
		}
	}
	return r.Repository.CountAlarms(ctx, f)
}
func (r *deviceScopeRepository) ListRawIndexes(ctx context.Context, f ports.RawFilter) ([]model.RawArchiveIndex, error) {
	if limited(ctx) {
		var ok bool
		if f.DeviceIDs, ok = r.scopedDevices(ctx, f.TenantID, f.DeviceID, f.DeviceIDs); !ok {
			return []model.RawArchiveIndex{}, nil
		}
	}
	return r.Repository.ListRawIndexes(ctx, f)
}
func (r *deviceScopeRepository) CountRawIndexes(ctx context.Context, f ports.RawFilter) (int, error) {
	if limited(ctx) {
		var ok bool
		if f.DeviceIDs, ok = r.scopedDevices(ctx, f.TenantID, f.DeviceID, f.DeviceIDs); !ok {
			return 0, nil
		}
	}
	return r.Repository.CountRawIndexes(ctx, f)
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
		// Adding devices is limited to users who can see every device.
		if strings.HasPrefix(path, "/api/v1/onboarding") {
			return false
		}
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

// MCP history queries use the non-paginated repository method too.
func (r *deviceScopeRepository) PropertyHistory(ctx context.Context, t, d, p string, start, end int64, limit int) ([]map[string]any, error) {
	if !deviceAllowed(ctx, t, d) {
		return nil, errDeviceScope
	}
	return r.Repository.PropertyHistory(ctx, t, d, p, start, end, limit)
}

func (r *deviceScopeRepository) ListVideoCameraMappings(ctx context.Context, tenant string) ([]model.VideoCameraMapping, error) {
	rows, err := r.Repository.ListVideoCameraMappings(ctx, tenant)
	if err != nil {
		return nil, err
	}
	out := []model.VideoCameraMapping{}
	for _, item := range rows {
		if deviceAllowed(ctx, tenant, item.DeviceID) {
			out = append(out, item)
		}
	}
	return out, nil
}
