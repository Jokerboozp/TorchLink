package devicescope

import (
	"context"
	"sort"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// Repository applies the request scope to device, state, alarm and raw-message
// queries.
type Repository struct{ ports.Repository }

func (r *Repository) scopedIDs(ctx context.Context, tenant string, ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if Allowed(ctx, tenant, id) {
			out = append(out, id)
		}
	}
	return out
}

func (r *Repository) GetDeviceStatesByIDs(ctx context.Context, tenant string, ids []string) (map[string]model.DeviceState, error) {
	return r.Repository.GetDeviceStatesByIDs(ctx, tenant, r.scopedIDs(ctx, tenant, ids))
}

func (r *Repository) ListMessageTopicDevices(ctx context.Context, tenant string, ids []string, limit int) ([]model.MessageTopicDeviceRecord, error) {
	if scope, ok := FromContext(ctx); ok {
		if scope.Tenant != tenant {
			ids = []string{}
		} else if !scope.All {
			if ids == nil {
				ids = GrantedIDs(ctx, tenant)
			} else {
				ids = r.scopedIDs(ctx, tenant, ids)
			}
		}
	}
	rows, err := r.Repository.ListMessageTopicDevices(ctx, tenant, ids, limit)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		if !Allowed(ctx, tenant, rows[i].Device.GatewayID) {
			rows[i].Device.GatewayID = ""
		}
	}
	return rows, nil
}

func (r *Repository) GetStandardMessagesByRawIDs(ctx context.Context, tenant string, ids []string) (map[string]model.StandardMessage, error) {
	items, err := r.Repository.GetStandardMessagesByRawIDs(ctx, tenant, ids)
	if err != nil {
		return nil, err
	}
	for id, item := range items {
		if !Allowed(ctx, tenant, item.DeviceID) {
			delete(items, id)
		}
	}
	return items, nil
}

func (r *Repository) ListVideoCameraMappingsByDeviceIDs(ctx context.Context, tenant string, ids []string) (map[string][]model.VideoCameraMapping, error) {
	return r.Repository.ListVideoCameraMappingsByDeviceIDs(ctx, tenant, r.scopedIDs(ctx, tenant, ids))
}

// Wrap wraps repo with per-request device scope checks; wrapping twice is a
// no-op. The process wraps the repository once, before the engine and the API
// start. Without a scope in the request context the wrapper passes calls
// through unchanged.
func Wrap(repo ports.Repository) ports.Repository {
	if _, ok := repo.(*Repository); ok {
		return repo
	}
	return &Repository{Repository: repo}
}

// Filtering occurs before pagination and totals. The wrapper is shared, while
// the scope lives only in the authenticated request context; background ingest
// and internal maintenance retain their existing repository behavior.
func (r *Repository) ListManagedDevices(ctx context.Context, t string) ([]model.ManagedDevice, error) {
	if !Limited(ctx) {
		return r.Repository.ListManagedDevices(ctx, t)
	}
	// Limited users read only their grant, page by page, from the store.
	out := []model.ManagedDevice{}
	for offset := 0; ; offset += listPage {
		rows, _, err := r.ListManagedDevicesFiltered(ctx, ports.DeviceFilter{TenantID: t}, listPage, offset)
		if err != nil {
			return nil, err
		}
		out = append(out, rows...)
		if len(rows) < listPage {
			return out, nil
		}
	}
}

// listPage is the page size used to read a limited user's devices.
const listPage = 500

func (r *Repository) ListManagedDevicesPage(ctx context.Context, t string, l, o int) ([]model.ManagedDevice, int, error) {
	if !Limited(ctx) {
		return r.Repository.ListManagedDevicesPage(ctx, t, l, o)
	}
	return r.ListManagedDevicesFiltered(ctx, ports.DeviceFilter{TenantID: t}, l, o)
}

// grantedIDs lists the request's granted devices of tenant t in a stable order.
func GrantedIDs(ctx context.Context, t string) []string {
	v, _ := FromContext(ctx)
	if v.Tenant != t {
		return []string{}
	}
	return v.DeviceIDs()
}

// Limited users pass their device grant to the store, which filters, counts
// and pages in one query instead of loading the tenant's whole registry.
func (r *Repository) ListManagedDevicesFiltered(ctx context.Context, f ports.DeviceFilter, l, o int) ([]model.ManagedDevice, int, error) {
	if !Limited(ctx) {
		return r.Repository.ListManagedDevicesFiltered(ctx, f, l, o)
	}
	f.RestrictDevices, f.DeviceIDs = true, GrantedIDs(ctx, f.TenantID)
	rows, total, e := r.Repository.ListManagedDevicesFiltered(ctx, f, l, o)
	for i := range rows {
		if !Allowed(ctx, f.TenantID, rows[i].GatewayID) {
			rows[i].GatewayID = ""
		}
	}
	return rows, total, e
}

func (r *Repository) ListManagedDeviceChildren(ctx context.Context, t, id string, l, o int) ([]model.ManagedDevice, int, error) {
	if !Allowed(ctx, t, id) {
		return nil, 0, ErrDenied
	}
	if !Limited(ctx) {
		return r.Repository.ListManagedDeviceChildren(ctx, t, id, l, o)
	}
	return r.Repository.ListManagedDeviceChildrenForDevices(ctx, t, id, GrantedIDs(ctx, t), l, o)
}

func (r *Repository) ListManagedDeviceChildrenForDevices(ctx context.Context, t, id string, ids []string, l, o int) ([]model.ManagedDevice, int, error) {
	if !Allowed(ctx, t, id) {
		return nil, 0, ErrDenied
	}
	if Limited(ctx) {
		ids = r.scopedIDs(ctx, t, ids)
	}
	return r.Repository.ListManagedDeviceChildrenForDevices(ctx, t, id, ids, l, o)
}

func (r *Repository) CountManagedDeviceChildren(ctx context.Context, t string, ids []string) (map[string]int, error) {
	if !Limited(ctx) {
		return r.Repository.CountManagedDeviceChildren(ctx, t, ids)
	}
	return r.Repository.CountManagedDeviceChildrenForDevices(ctx, t, r.scopedIDs(ctx, t, ids), GrantedIDs(ctx, t))
}

func (r *Repository) ListDeviceStates(ctx context.Context, t string) ([]model.DeviceState, error) {
	if !Limited(ctx) {
		return r.Repository.ListDeviceStates(ctx, t)
	}
	// Read only the granted devices' states, newest first like the store does.
	states, e := r.Repository.GetDeviceStatesByIDs(ctx, t, GrantedIDs(ctx, t))
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

func (r *Repository) ListDeviceStatesPage(ctx context.Context, t string, l, o int) ([]model.DeviceState, int, error) {
	if !Limited(ctx) {
		return r.Repository.ListDeviceStatesPage(ctx, t, l, o)
	}
	return r.Repository.ListDeviceStatesForDevicesPage(ctx, t, GrantedIDs(ctx, t), l, o)
}

func (r *Repository) ListUnregisteredDeviceStatesPage(ctx context.Context, t string, l, o int) ([]model.DeviceState, int, error) {
	if !Limited(ctx) {
		return r.Repository.ListUnregisteredDeviceStatesPage(ctx, t, l, o)
	}
	// Selected grants refer to registered devices only.
	return []model.DeviceState{}, 0, nil
}

func (r *Repository) CountDeviceStates(ctx context.Context, t string, unregistered bool) (int, int, error) {
	if !Limited(ctx) {
		return r.Repository.CountDeviceStates(ctx, t, unregistered)
	}
	if unregistered {
		return 0, 0, nil
	}
	// Counted in the store over the granted devices only.
	overview, err := r.Repository.DeviceOverviewCounts(ctx, t, true, GrantedIDs(ctx, t))
	online := overview.BusinessStatus["ONLINE"] + overview.BusinessStatus["ALARM"]
	return overview.Reported + overview.DiscoveredUnregistered, online, err
}

// scopedDevices narrows a list query to the request's granted devices so the
// store filters, counts and paginates in one query. ok=false matches nothing.
func (r *Repository) scopedDevices(ctx context.Context, tenant, device string, ids []string) ([]string, bool) {
	if device != "" && !Allowed(ctx, tenant, device) {
		return nil, false
	}
	if ids == nil {
		if device != "" {
			return nil, true
		}
		v, _ := FromContext(ctx)
		ids = v.DeviceIDs()
	}
	ids = r.scopedIDs(ctx, tenant, ids)
	return ids, len(ids) > 0
}

func (r *Repository) ListAlarms(ctx context.Context, f ports.AlarmFilter) ([]model.Alarm, error) {
	if Limited(ctx) {
		var ok bool
		if f.DeviceIDs, ok = r.scopedDevices(ctx, f.TenantID, f.DeviceID, f.DeviceIDs); !ok {
			return []model.Alarm{}, nil
		}
	}
	return r.Repository.ListAlarms(ctx, f)
}

func (r *Repository) AlarmDispositionStats(ctx context.Context, f ports.AlarmFilter) (model.AlarmDispositionStats, error) {
	if Limited(ctx) {
		var ok bool
		if f.DeviceIDs, ok = r.scopedDevices(ctx, f.TenantID, f.DeviceID, f.DeviceIDs); !ok {
			return model.SummarizeAlarms(nil), nil
		}
	}
	return r.Repository.AlarmDispositionStats(ctx, f)
}

func (r *Repository) AlarmBreakdown(ctx context.Context, f ports.AlarmFilter) (model.AlarmBreakdown, error) {
	if Limited(ctx) {
		var ok bool
		if f.DeviceIDs, ok = r.scopedDevices(ctx, f.TenantID, f.DeviceID, f.DeviceIDs); !ok {
			return model.BreakdownAlarms(nil), nil
		}
	}
	return r.Repository.AlarmBreakdown(ctx, f)
}

func (r *Repository) EachAlarm(ctx context.Context, f ports.AlarmFilter, fn func(model.Alarm) error) error {
	if Limited(ctx) {
		var ok bool
		if f.DeviceIDs, ok = r.scopedDevices(ctx, f.TenantID, f.DeviceID, f.DeviceIDs); !ok {
			return nil
		}
	}
	return r.Repository.EachAlarm(ctx, f, fn)
}

func (r *Repository) AlarmOverviewCounts(ctx context.Context, f ports.AlarmFilter, since int64) (model.AlarmOverview, error) {
	if Limited(ctx) {
		var ok bool
		if f.DeviceIDs, ok = r.scopedDevices(ctx, f.TenantID, f.DeviceID, f.DeviceIDs); !ok {
			return model.NewAlarmOverview(), nil
		}
	}
	return r.Repository.AlarmOverviewCounts(ctx, f, since)
}

func (r *Repository) AIAnalysisOutcomes(ctx context.Context, f ports.AlarmFilter, promptVersion string) ([]model.AIAnalysisOutcome, error) {
	if Limited(ctx) {
		var ok bool
		if f.DeviceIDs, ok = r.scopedDevices(ctx, f.TenantID, f.DeviceID, f.DeviceIDs); !ok {
			return []model.AIAnalysisOutcome{}, nil
		}
	}
	return r.Repository.AIAnalysisOutcomes(ctx, f, promptVersion)
}

func (r *Repository) DeviceOverviewCounts(ctx context.Context, t string, restrict bool, ids []string) (model.DeviceOverview, error) {
	if Limited(ctx) {
		if restrict {
			ids = r.scopedIDs(ctx, t, ids)
		} else {
			restrict, ids = true, GrantedIDs(ctx, t)
		}
	}
	return r.Repository.DeviceOverviewCounts(ctx, t, restrict, ids)
}

func (r *Repository) CountAlarms(ctx context.Context, f ports.AlarmFilter) (int, error) {
	if Limited(ctx) {
		var ok bool
		if f.DeviceIDs, ok = r.scopedDevices(ctx, f.TenantID, f.DeviceID, f.DeviceIDs); !ok {
			return 0, nil
		}
	}
	return r.Repository.CountAlarms(ctx, f)
}

func (r *Repository) ListRawIndexes(ctx context.Context, f ports.RawFilter) ([]model.RawArchiveIndex, error) {
	if Limited(ctx) {
		var ok bool
		if f.DeviceIDs, ok = r.scopedDevices(ctx, f.TenantID, f.DeviceID, f.DeviceIDs); !ok {
			return []model.RawArchiveIndex{}, nil
		}
	}
	return r.Repository.ListRawIndexes(ctx, f)
}

func (r *Repository) CountRawIndexes(ctx context.Context, f ports.RawFilter) (int, error) {
	if Limited(ctx) {
		var ok bool
		if f.DeviceIDs, ok = r.scopedDevices(ctx, f.TenantID, f.DeviceID, f.DeviceIDs); !ok {
			return 0, nil
		}
	}
	return r.Repository.CountRawIndexes(ctx, f)
}

func (r *Repository) DashboardCounts(ctx context.Context, t string, start, end int64) ([]model.DashboardCount, error) {
	if !Limited(ctx) {
		return r.Repository.DashboardCounts(ctx, t, start, end)
	}
	scope, _ := FromContext(ctx)
	return r.Repository.DashboardCountsForDevices(ctx, t, start, end, scope.DeviceIDs())
}

func (r *Repository) GetManagedDevice(ctx context.Context, t, id string) (model.ManagedDevice, error) {
	v, e := r.Repository.GetManagedDevice(ctx, t, id)
	if e != nil {
		return v, e
	}
	if !Allowed(ctx, t, v.ID) {
		return model.ManagedDevice{}, ErrDenied
	}
	return v, nil
}

func (r *Repository) GetDeviceState(ctx context.Context, t, id string) (model.DeviceState, error) {
	v, e := r.Repository.GetDeviceState(ctx, t, id)
	if e != nil {
		return v, e
	}
	if !Allowed(ctx, t, v.DeviceID) {
		return model.DeviceState{}, ErrDenied
	}
	return v, nil
}

func (r *Repository) GetLatestMessage(ctx context.Context, t, id string) (model.StandardMessage, error) {
	v, e := r.Repository.GetLatestMessage(ctx, t, id)
	if e != nil {
		return v, e
	}
	if !Allowed(ctx, t, v.DeviceID) {
		return model.StandardMessage{}, ErrDenied
	}
	return v, nil
}

func (r *Repository) GetRawIndex(ctx context.Context, t, id string) (model.RawArchiveIndex, error) {
	v, e := r.Repository.GetRawIndex(ctx, t, id)
	if e != nil {
		return v, e
	}
	if !Allowed(ctx, t, v.DeviceID) {
		return model.RawArchiveIndex{}, ErrDenied
	}
	return v, nil
}

func (r *Repository) GetStandardMessageByRaw(ctx context.Context, t, id string) (model.StandardMessage, error) {
	v, e := r.Repository.GetStandardMessageByRaw(ctx, t, id)
	if e != nil {
		return v, e
	}
	if !Allowed(ctx, t, v.DeviceID) {
		return model.StandardMessage{}, ErrDenied
	}
	return v, nil
}

func (r *Repository) GetAlarm(ctx context.Context, t, id string) (model.Alarm, error) {
	v, e := r.Repository.GetAlarm(ctx, t, id)
	if e != nil {
		return v, e
	}
	if !Allowed(ctx, t, v.DeviceID) {
		return model.Alarm{}, ErrDenied
	}
	return v, nil
}

func (r *Repository) SaveManagedDevice(ctx context.Context, v model.ManagedDevice) error {
	if !Allowed(ctx, v.TenantID, v.ID) {
		return ErrDenied
	}
	return r.Repository.SaveManagedDevice(ctx, v)
}

func (r *Repository) UpsertDeviceState(ctx context.Context, v model.DeviceState) error {
	if !Allowed(ctx, v.TenantID, v.DeviceID) {
		return ErrDenied
	}
	return r.Repository.UpsertDeviceState(ctx, v)
}

func (r *Repository) GetDeviceStateFresh(ctx context.Context, t, id string) (model.DeviceState, error) {
	v, e := r.Repository.GetDeviceStateFresh(ctx, t, id)
	if e != nil {
		return v, e
	}
	if !Allowed(ctx, t, v.DeviceID) {
		return model.DeviceState{}, ErrDenied
	}
	return v, nil
}

func (r *Repository) UpsertDeviceStateIf(ctx context.Context, v model.DeviceState) (bool, error) {
	if !Allowed(ctx, v.TenantID, v.DeviceID) {
		return false, ErrDenied
	}
	return r.Repository.UpsertDeviceStateIf(ctx, v)
}

func (r *Repository) UpdateAlarmIf(ctx context.Context, v model.Alarm) (bool, error) {
	if !Allowed(ctx, v.TenantID, v.DeviceID) {
		return false, ErrDenied
	}
	return r.Repository.UpdateAlarmIf(ctx, v)
}

func (r *Repository) UpsertExternalAlarm(ctx context.Context, v model.Alarm) (model.Alarm, bool, bool, error) {
	if !Allowed(ctx, v.TenantID, v.DeviceID) {
		return v, false, false, ErrDenied
	}
	return r.Repository.UpsertExternalAlarm(ctx, v)
}

func (r *Repository) UpdateAlarm(ctx context.Context, v model.Alarm) error {
	if !Allowed(ctx, v.TenantID, v.DeviceID) {
		return ErrDenied
	}
	return r.Repository.UpdateAlarm(ctx, v)
}

func (r *Repository) PropertyHistoryPage(ctx context.Context, t, d, p string, start, end int64, l, o int) ([]map[string]any, int, error) {
	if !Allowed(ctx, t, d) {
		return nil, 0, ErrDenied
	}
	return r.Repository.PropertyHistoryPage(ctx, t, d, p, start, end, l, o)
}

// MCP history queries use the non-paginated repository method too.
func (r *Repository) PropertyHistory(ctx context.Context, t, d, p string, start, end int64, limit int) ([]map[string]any, error) {
	if !Allowed(ctx, t, d) {
		return nil, ErrDenied
	}
	return r.Repository.PropertyHistory(ctx, t, d, p, start, end, limit)
}

func (r *Repository) ListVideoCameraMappings(ctx context.Context, tenant string) ([]model.VideoCameraMapping, error) {
	rows, err := r.Repository.ListVideoCameraMappings(ctx, tenant)
	if err != nil {
		return nil, err
	}
	out := []model.VideoCameraMapping{}
	for _, item := range rows {
		if Allowed(ctx, tenant, item.DeviceID) {
			out = append(out, item)
		}
	}
	return out, nil
}

func (r *Repository) CountManagedDeviceChildrenForDevices(ctx context.Context, tenant string, parents, children []string) (map[string]int, error) {
	if Limited(ctx) {
		if children == nil {
			children = GrantedIDs(ctx, tenant)
		} else {
			children = r.scopedIDs(ctx, tenant, children)
		}
	}
	return r.Repository.CountManagedDeviceChildrenForDevices(ctx, tenant, r.scopedIDs(ctx, tenant, parents), children)
}

func (r *Repository) ListDeviceStatesForDevicesPage(ctx context.Context, t string, ids []string, l, o int) ([]model.DeviceState, int, error) {
	if Limited(ctx) {
		if ids == nil {
			ids = GrantedIDs(ctx, t)
		} else {
			ids = r.scopedIDs(ctx, t, ids)
		}
	}
	return r.Repository.ListDeviceStatesForDevicesPage(ctx, t, ids, l, o)
}

func (r *Repository) HasOpenAlarm(ctx context.Context, tenant, device string) (bool, error) {
	if !Allowed(ctx, tenant, device) {
		return false, ErrDenied
	}
	return r.Repository.HasOpenAlarm(ctx, tenant, device)
}

// Unwrap returns the repository the wrapper filters.
func (r *Repository) Unwrap() ports.Repository { return r.Repository }

// Unscoped returns repo without the scope wrapper, for reads that must see the
// whole tenant (for example the device options offered when granting access).
func Unscoped(repo ports.Repository) ports.Repository {
	if r, ok := repo.(*Repository); ok {
		return r.Repository
	}
	return repo
}
