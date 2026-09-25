package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                     /* 执行当前语句并推进处理流程。 */
	"errors"                      /* 执行当前语句并推进处理流程。 */
	"github.com/gin-gonic/gin"    /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports" /* 执行当前语句并推进处理流程。 */
	"net/http"                    /* 执行当前语句并推进处理流程。 */
	"sort"
	"strings" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type deviceScopeKey struct{} /* 定义 deviceScopeKey 类型。 */
type deviceScope struct {    /* 定义 deviceScope 类型。 */
	Tenant string          /* 执行当前语句并推进处理流程。 */
	All    bool            /* 执行当前语句并推进处理流程。 */
	IDs    map[string]bool /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func scopeFor(u model.PlatformUser, p map[string]bool, tenant string) deviceScope { /* 定义 scopeFor 函数。 */
	v := deviceScope{Tenant: tenant, IDs: map[string]bool{}} /* 更新 v 的值。 */
	if !p["menu:devices"] {                                  /* 判断条件并选择处理分支。 */
		return v /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	v.All = u.DeviceScope == "all"   /* 更新 v.All 的值。 */
	if u.DeviceScope == "selected" { /* 判断条件并选择处理分支。 */
		for _, id := range u.DeviceIDs { /* 循环处理当前数据。 */
			v.IDs[id] = true /* 更新 v.IDs[id] 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return v /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func requestScope(ctx context.Context) (deviceScope, bool) { /* 定义 requestScope 函数。 */
	v, ok := ctx.Value(deviceScopeKey{}).(deviceScope) /* 更新 ok 的值。 */
	return v, ok                                       /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func deviceAllowed(ctx context.Context, tenant, id string) bool { /* 定义 deviceAllowed 函数。 */
	v, ok := requestScope(ctx)                                 /* 更新 ok 的值。 */
	return !ok || (tenant == v.Tenant && (v.All || v.IDs[id])) /* 返回当前处理结果。 */
}                                      /* 结束当前表达式或代码块。 */
func limited(ctx context.Context) bool { v, ok := requestScope(ctx); return ok && !v.All } /* 定义 limited 函数。 */

var errDeviceScope = errors.New("设备不存在或无访问权限") /* 声明 errDeviceScope。 */

type deviceScopeRepository struct{ ports.Repository } /* 定义 deviceScopeRepository 类型。 */

func (r *deviceScopeRepository) scopedIDs(ctx context.Context, tenant string, ids []string) []string { /* 定义 scopedIDs 函数。 */
	out := make([]string, 0, len(ids)) /* 更新 out 的值。 */
	for _, id := range ids {           /* 循环处理当前数据。 */
		if deviceAllowed(ctx, tenant, id) { /* 判断条件并选择处理分支。 */
			out = append(out, id) /* 更新 out 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return out /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *deviceScopeRepository) GetDeviceStatesByIDs(ctx context.Context, tenant string, ids []string) (map[string]model.DeviceState, error) { /* 定义 GetDeviceStatesByIDs 函数。 */
	return r.Repository.GetDeviceStatesByIDs(ctx, tenant, r.scopedIDs(ctx, tenant, ids)) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *deviceScopeRepository) GetStandardMessagesByRawIDs(ctx context.Context, tenant string, ids []string) (map[string]model.StandardMessage, error) { /* 定义 GetStandardMessagesByRawIDs 函数。 */
	items, err := r.Repository.GetStandardMessagesByRawIDs(ctx, tenant, ids) /* 更新 err 的值。 */
	if err != nil {                                                          /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for id, item := range items { /* 循环处理当前数据。 */
		if !deviceAllowed(ctx, tenant, item.DeviceID) { /* 判断条件并选择处理分支。 */
			delete(items, id) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return items, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *deviceScopeRepository) ListVideoCameraMappingsByDeviceIDs(ctx context.Context, tenant string, ids []string) (map[string][]model.VideoCameraMapping, error) { /* 定义 ListVideoCameraMappingsByDeviceIDs 函数。 */
	return r.Repository.ListVideoCameraMappingsByDeviceIDs(ctx, tenant, r.scopedIDs(ctx, tenant, ids)) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) unscopedRepo() ports.Repository { /* 定义 unscopedRepo 函数。 */
	if r, ok := s.engine.Repo.(*deviceScopeRepository); ok { /* 判断条件并选择处理分支。 */
		return r.Repository /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return s.engine.Repo /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func pageSlice[T any](items []T, limit, offset int) []T { /* 定义 pageSlice 函数。 */
	if offset < 0 { /* 判断条件并选择处理分支。 */
		offset = 0 /* 更新 offset 的值。 */
	} /* 结束当前表达式或代码块。 */
	if offset > len(items) { /* 判断条件并选择处理分支。 */
		offset = len(items) /* 更新 offset 的值。 */
	} /* 结束当前表达式或代码块。 */
	end := len(items)                    /* 更新 end 的值。 */
	if limit > 0 && offset+limit < end { /* 判断条件并选择处理分支。 */
		end = offset + limit /* 更新 end 的值。 */
	} /* 结束当前表达式或代码块。 */
	return items[offset:end] /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// Filtering occurs before pagination and totals. The wrapper is shared, while
// the scope lives only in the authenticated request context; background ingest
// and internal maintenance retain their existing repository behavior.
func (r *deviceScopeRepository) ListManagedDevices(ctx context.Context, t string) ([]model.ManagedDevice, error) { /* 定义 ListManagedDevices 函数。 */
	rows, e := r.Repository.ListManagedDevices(ctx, t) /* 更新 e 的值。 */
	if e != nil {                                      /* 判断条件并选择处理分支。 */
		return nil, e /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	out := []model.ManagedDevice{} /* 更新 out 的值。 */
	for _, v := range rows {       /* 循环处理当前数据。 */
		if deviceAllowed(ctx, t, v.ID) { /* 判断条件并选择处理分支。 */
			if !deviceAllowed(ctx, t, v.GatewayID) { /* 判断条件并选择处理分支。 */
				v.GatewayID = "" /* 更新 v.GatewayID 的值。 */
			} /* 结束当前表达式或代码块。 */
			out = append(out, v) /* 更新 out 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return out, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *deviceScopeRepository) ListManagedDevicesPage(ctx context.Context, t string, l, o int) ([]model.ManagedDevice, int, error) { /* 定义 ListManagedDevicesPage 函数。 */
	if !limited(ctx) { /* 判断条件并选择处理分支。 */
		return r.Repository.ListManagedDevicesPage(ctx, t, l, o) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	rows, e := r.ListManagedDevices(ctx, t)    /* 更新 e 的值。 */
	return pageSlice(rows, l, o), len(rows), e /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
// Limited users filter their authorized devices in memory so totals never count
// devices outside the request scope.
func (r *deviceScopeRepository) ListManagedDevicesFiltered(ctx context.Context, f ports.DeviceFilter, l, o int) ([]model.ManagedDevice, int, error) {
	if !limited(ctx) {
		return r.Repository.ListManagedDevicesFiltered(ctx, f, l, o)
	}
	rows, e := r.ListManagedDevices(ctx, f.TenantID)
	if e != nil {
		return nil, 0, e
	}
	ids := make([]string, 0, len(rows))
	for _, v := range rows {
		ids = append(ids, v.ID)
	}
	states, e := r.GetDeviceStatesByIDs(ctx, f.TenantID, ids)
	if e != nil {
		return nil, 0, e
	}
	out := []model.ManagedDevice{}
	for _, v := range rows {
		var state *model.DeviceState
		if current, ok := states[v.ID]; ok {
			state = &current
		}
		if f.Matches(v, state) {
			out = append(out, v)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].UpdatedAt != out[j].UpdatedAt {
			return out[i].UpdatedAt > out[j].UpdatedAt
		}
		return out[i].ID > out[j].ID
	})
	return pageSlice(out, l, o), len(out), nil
}
func (r *deviceScopeRepository) ListManagedDeviceChildren(ctx context.Context, t, id string, l, o int) ([]model.ManagedDevice, int, error) { /* 定义 ListManagedDeviceChildren 函数。 */
	if !deviceAllowed(ctx, t, id) { /* 判断条件并选择处理分支。 */
		return nil, 0, errDeviceScope /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !limited(ctx) { /* 判断条件并选择处理分支。 */
		return r.Repository.ListManagedDeviceChildren(ctx, t, id, l, o) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	rows, e := r.ListManagedDevices(ctx, t) /* 更新 e 的值。 */
	out := []model.ManagedDevice{}          /* 更新 out 的值。 */
	for _, v := range rows {                /* 循环处理当前数据。 */
		if v.GatewayID == id { /* 判断条件并选择处理分支。 */
			out = append(out, v) /* 更新 out 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return pageSlice(out, l, o), len(out), e /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *deviceScopeRepository) CountManagedDeviceChildren(ctx context.Context, t string, ids []string) (map[string]int, error) { /* 定义 CountManagedDeviceChildren 函数。 */
	if !limited(ctx) { /* 判断条件并选择处理分支。 */
		return r.Repository.CountManagedDeviceChildren(ctx, t, ids) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	rows, e := r.ListManagedDevices(ctx, t) /* 更新 e 的值。 */
	out := map[string]int{}                 /* 更新 out 的值。 */
	for _, v := range rows {                /* 循环处理当前数据。 */
		if v.GatewayID != "" { /* 判断条件并选择处理分支。 */
			out[v.GatewayID]++ /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return out, e /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *deviceScopeRepository) ListDeviceStates(ctx context.Context, t string) ([]model.DeviceState, error) { /* 定义 ListDeviceStates 函数。 */
	rows, e := r.Repository.ListDeviceStates(ctx, t) /* 更新 e 的值。 */
	out := []model.DeviceState{}                     /* 更新 out 的值。 */
	for _, v := range rows {                         /* 循环处理当前数据。 */
		if deviceAllowed(ctx, t, v.DeviceID) { /* 判断条件并选择处理分支。 */
			out = append(out, v) /* 更新 out 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return out, e /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *deviceScopeRepository) ListDeviceStatesPage(ctx context.Context, t string, l, o int) ([]model.DeviceState, int, error) { /* 定义 ListDeviceStatesPage 函数。 */
	if !limited(ctx) { /* 判断条件并选择处理分支。 */
		return r.Repository.ListDeviceStatesPage(ctx, t, l, o) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	rows, e := r.ListDeviceStates(ctx, t)      /* 更新 e 的值。 */
	return pageSlice(rows, l, o), len(rows), e /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *deviceScopeRepository) ListUnregisteredDeviceStatesPage(ctx context.Context, t string, l, o int) ([]model.DeviceState, int, error) { /* 定义 ListUnregisteredDeviceStatesPage 函数。 */
	if !limited(ctx) { /* 判断条件并选择处理分支。 */
		return r.Repository.ListUnregisteredDeviceStatesPage(ctx, t, l, o) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	// Selected grants refer to registered devices only.
	return []model.DeviceState{}, 0, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *deviceScopeRepository) CountDeviceStates(ctx context.Context, t string, unregistered bool) (int, int, error) { /* 定义 CountDeviceStates 函数。 */
	if !limited(ctx) { /* 判断条件并选择处理分支。 */
		return r.Repository.CountDeviceStates(ctx, t, unregistered) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if unregistered { /* 判断条件并选择处理分支。 */
		return 0, 0, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	rows, e := r.ListDeviceStates(ctx, t) /* 更新 e 的值。 */
	online := 0                           /* 更新 online 的值。 */
	for _, v := range rows {              /* 循环处理当前数据。 */
		if v.BusinessStatus == "ONLINE" || v.BusinessStatus == "ALARM" { /* 判断条件并选择处理分支。 */
			online++ /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return len(rows), online, e /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *deviceScopeRepository) scopedAlarms(ctx context.Context, f ports.AlarmFilter) ([]model.Alarm, error) { /* 定义 scopedAlarms 函数。 */
	out := []model.Alarm{}                                               /* 更新 out 的值。 */
	if f.DeviceID != "" && !deviceAllowed(ctx, f.TenantID, f.DeviceID) { /* 判断条件并选择处理分支。 */
		return out, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	f.Offset = 0  /* 更新 f.Offset 的值。 */
	f.Limit = 500 /* 更新 f.Limit 的值。 */
	for {         /* 循环处理当前数据。 */
		rows, e := r.Repository.ListAlarms(ctx, f) /* 更新 e 的值。 */
		if e != nil {                              /* 判断条件并选择处理分支。 */
			return nil, e /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		for _, v := range rows { /* 循环处理当前数据。 */
			if deviceAllowed(ctx, f.TenantID, v.DeviceID) { /* 判断条件并选择处理分支。 */
				out = append(out, v) /* 更新 out 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		if len(rows) < f.Limit { /* 判断条件并选择处理分支。 */
			return out, nil /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		f.Offset += len(rows) /* 更新 f.Offset 的值。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
func (r *deviceScopeRepository) ListAlarms(ctx context.Context, f ports.AlarmFilter) ([]model.Alarm, error) { /* 定义 ListAlarms 函数。 */
	if !limited(ctx) { /* 判断条件并选择处理分支。 */
		return r.Repository.ListAlarms(ctx, f) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	rows, e := r.scopedAlarms(ctx, f)            /* 更新 e 的值。 */
	return pageSlice(rows, f.Limit, f.Offset), e /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *deviceScopeRepository) CountAlarms(ctx context.Context, f ports.AlarmFilter) (int, error) { /* 定义 CountAlarms 函数。 */
	if !limited(ctx) { /* 判断条件并选择处理分支。 */
		return r.Repository.CountAlarms(ctx, f) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	rows, e := r.scopedAlarms(ctx, f) /* 更新 e 的值。 */
	return len(rows), e               /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *deviceScopeRepository) scopedRaw(ctx context.Context, f ports.RawFilter) ([]model.RawArchiveIndex, error) { /* 定义 scopedRaw 函数。 */
	out := []model.RawArchiveIndex{}                                     /* 更新 out 的值。 */
	if f.DeviceID != "" && !deviceAllowed(ctx, f.TenantID, f.DeviceID) { /* 判断条件并选择处理分支。 */
		return out, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	f.Offset = 0  /* 更新 f.Offset 的值。 */
	f.Limit = 500 /* 更新 f.Limit 的值。 */
	for {         /* 循环处理当前数据。 */
		rows, e := r.Repository.ListRawIndexes(ctx, f) /* 更新 e 的值。 */
		if e != nil {                                  /* 判断条件并选择处理分支。 */
			return nil, e /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		for _, v := range rows { /* 循环处理当前数据。 */
			if deviceAllowed(ctx, f.TenantID, v.DeviceID) { /* 判断条件并选择处理分支。 */
				out = append(out, v) /* 更新 out 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		if len(rows) < f.Limit { /* 判断条件并选择处理分支。 */
			return out, nil /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		f.Offset += len(rows) /* 更新 f.Offset 的值。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
func (r *deviceScopeRepository) ListRawIndexes(ctx context.Context, f ports.RawFilter) ([]model.RawArchiveIndex, error) { /* 定义 ListRawIndexes 函数。 */
	if !limited(ctx) { /* 判断条件并选择处理分支。 */
		return r.Repository.ListRawIndexes(ctx, f) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	rows, e := r.scopedRaw(ctx, f)               /* 更新 e 的值。 */
	return pageSlice(rows, f.Limit, f.Offset), e /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *deviceScopeRepository) CountRawIndexes(ctx context.Context, f ports.RawFilter) (int, error) { /* 定义 CountRawIndexes 函数。 */
	if !limited(ctx) { /* 判断条件并选择处理分支。 */
		return r.Repository.CountRawIndexes(ctx, f) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	rows, e := r.scopedRaw(ctx, f) /* 更新 e 的值。 */
	return len(rows), e            /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *deviceScopeRepository) DashboardCounts(ctx context.Context, t string, start, end int64) ([]model.DashboardCount, error) { /* 定义 DashboardCounts 函数。 */
	if !limited(ctx) { /* 判断条件并选择处理分支。 */
		return r.Repository.DashboardCounts(ctx, t, start, end) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	scope, _ := requestScope(ctx)            /* 更新 _ 的值。 */
	ids := make([]string, 0, len(scope.IDs)) /* 更新 ids 的值。 */
	for id := range scope.IDs {              /* 循环处理当前数据。 */
		ids = append(ids, id) /* 更新 ids 的值。 */
	} /* 结束当前表达式或代码块。 */
	return r.Repository.DashboardCountsForDevices(ctx, t, start, end, ids) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) accessDeviceOptions(w http.ResponseWriter, r *http.Request) { /* 定义 accessDeviceOptions 函数。 */
	rows, e := s.unscopedRepo().ListManagedDevices(r.Context(), claims(r).TenantID) /* 更新 e 的值。 */
	if e != nil {                                                                   /* 判断条件并选择处理分支。 */
		problem(w, 500, "读取设备选项失败") /* 执行当前语句并推进处理流程。 */
		return                      /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	out := []map[string]string{} /* 更新 out 的值。 */
	for _, v := range rows {     /* 循环处理当前数据。 */
		out = append(out, map[string]string{"id": v.ID, "name": v.Name, "deviceRole": v.DeviceRole}) /* 更新 out 的值。 */
	} /* 结束当前表达式或代码块。 */
	write(w, 200, map[string]any{"items": out, "tenantId": claims(r).TenantID}) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) allowScopedRequest(c *gin.Context, v deviceScope) bool { /* 定义 allowScopedRequest 函数。 */
	path := c.FullPath()                                                                                                     /* 更新 path 的值。 */
	ctx := c.Request.Context()                                                                                               /* 更新 ctx 的值。 */
	t := v.Tenant                                                                                                            /* 更新 t 的值。 */
	if strings.HasPrefix(path, "/api/v1/device-registry/:id") || strings.HasPrefix(path, "/api/v1/discovered-devices/:id") { /* 判断条件并选择处理分支。 */
		if !deviceAllowed(ctx, t, c.Param("id")) { /* 判断条件并选择处理分支。 */
			return false /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if id := c.Param("deviceId"); id != "" && !deviceAllowed(ctx, t, id) { /* 判断条件并选择处理分支。 */
		return false /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	alarmID := c.Param("alarmId")                      /* 更新 alarmID 的值。 */
	if strings.HasPrefix(path, "/api/v1/alarms/:id") { /* 判断条件并选择处理分支。 */
		alarmID = c.Param("id") /* 更新 alarmID 的值。 */
	} /* 结束当前表达式或代码块。 */
	if alarmID != "" { /* 判断条件并选择处理分支。 */
		if _, e := s.engine.Repo.GetAlarm(ctx, t, alarmID); e != nil { /* 判断条件并选择处理分支。 */
			return false /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if !v.All { /* 判断条件并选择处理分支。 */
		// Tenant-wide jobs and configuration can expose other devices. Their menus
		// are also removed from the effective permission list.
		// Adding devices is limited to users who can see every device.
		if strings.HasPrefix(path, "/api/v1/onboarding") {
			return false
		}
		if strings.HasPrefix(path, "/api/v1/replays") || strings.HasSuffix(path, "/replay") { /* 判断条件并选择处理分支。 */
			return false /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if c.Request.Method != "GET" && (path == "/api/v1/device-registry" || path == "/api/v1/device-states" || path == "/api/v1/raw-messages") { /* 判断条件并选择处理分支。 */
			return false /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return true /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *deviceScopeRepository) GetManagedDevice(ctx context.Context, t, id string) (model.ManagedDevice, error) { /* 定义 GetManagedDevice 函数。 */
	v, e := r.Repository.GetManagedDevice(ctx, t, id) /* 更新 e 的值。 */
	if e != nil {                                     /* 判断条件并选择处理分支。 */
		return v, e /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !deviceAllowed(ctx, t, v.ID) { /* 判断条件并选择处理分支。 */
		return model.ManagedDevice{}, errDeviceScope /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return v, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *deviceScopeRepository) GetDeviceState(ctx context.Context, t, id string) (model.DeviceState, error) { /* 定义 GetDeviceState 函数。 */
	v, e := r.Repository.GetDeviceState(ctx, t, id) /* 更新 e 的值。 */
	if e != nil {                                   /* 判断条件并选择处理分支。 */
		return v, e /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !deviceAllowed(ctx, t, v.DeviceID) { /* 判断条件并选择处理分支。 */
		return model.DeviceState{}, errDeviceScope /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return v, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *deviceScopeRepository) GetLatestMessage(ctx context.Context, t, id string) (model.StandardMessage, error) { /* 定义 GetLatestMessage 函数。 */
	v, e := r.Repository.GetLatestMessage(ctx, t, id) /* 更新 e 的值。 */
	if e != nil {                                     /* 判断条件并选择处理分支。 */
		return v, e /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !deviceAllowed(ctx, t, v.DeviceID) { /* 判断条件并选择处理分支。 */
		return model.StandardMessage{}, errDeviceScope /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return v, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *deviceScopeRepository) GetRawIndex(ctx context.Context, t, id string) (model.RawArchiveIndex, error) { /* 定义 GetRawIndex 函数。 */
	v, e := r.Repository.GetRawIndex(ctx, t, id) /* 更新 e 的值。 */
	if e != nil {                                /* 判断条件并选择处理分支。 */
		return v, e /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !deviceAllowed(ctx, t, v.DeviceID) { /* 判断条件并选择处理分支。 */
		return model.RawArchiveIndex{}, errDeviceScope /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return v, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *deviceScopeRepository) GetStandardMessageByRaw(ctx context.Context, t, id string) (model.StandardMessage, error) { /* 定义 GetStandardMessageByRaw 函数。 */
	v, e := r.Repository.GetStandardMessageByRaw(ctx, t, id) /* 更新 e 的值。 */
	if e != nil {                                            /* 判断条件并选择处理分支。 */
		return v, e /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !deviceAllowed(ctx, t, v.DeviceID) { /* 判断条件并选择处理分支。 */
		return model.StandardMessage{}, errDeviceScope /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return v, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *deviceScopeRepository) GetAlarm(ctx context.Context, t, id string) (model.Alarm, error) { /* 定义 GetAlarm 函数。 */
	v, e := r.Repository.GetAlarm(ctx, t, id) /* 更新 e 的值。 */
	if e != nil {                             /* 判断条件并选择处理分支。 */
		return v, e /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !deviceAllowed(ctx, t, v.DeviceID) { /* 判断条件并选择处理分支。 */
		return model.Alarm{}, errDeviceScope /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return v, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *deviceScopeRepository) SaveManagedDevice(ctx context.Context, v model.ManagedDevice) error { /* 定义 SaveManagedDevice 函数。 */
	if !deviceAllowed(ctx, v.TenantID, v.ID) { /* 判断条件并选择处理分支。 */
		return errDeviceScope /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return r.Repository.SaveManagedDevice(ctx, v) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *deviceScopeRepository) UpsertDeviceState(ctx context.Context, v model.DeviceState) error { /* 定义 UpsertDeviceState 函数。 */
	if !deviceAllowed(ctx, v.TenantID, v.DeviceID) { /* 判断条件并选择处理分支。 */
		return errDeviceScope /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return r.Repository.UpsertDeviceState(ctx, v) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *deviceScopeRepository) UpdateAlarm(ctx context.Context, v model.Alarm) error { /* 定义 UpdateAlarm 函数。 */
	if !deviceAllowed(ctx, v.TenantID, v.DeviceID) { /* 判断条件并选择处理分支。 */
		return errDeviceScope /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return r.Repository.UpdateAlarm(ctx, v) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *deviceScopeRepository) PropertyHistoryPage(ctx context.Context, t, d, p string, start, end int64, l, o int) ([]map[string]any, int, error) { /* 定义 PropertyHistoryPage 函数。 */
	if !deviceAllowed(ctx, t, d) { /* 判断条件并选择处理分支。 */
		return nil, 0, errDeviceScope /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return r.Repository.PropertyHistoryPage(ctx, t, d, p, start, end, l, o) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

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
