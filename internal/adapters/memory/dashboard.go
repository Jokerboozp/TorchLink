package memory /* 声明 memory 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                     /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"strconv"                     /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func (r *Repository) DashboardCounts(_ context.Context, tenant string, start, end int64) ([]model.DashboardCount, error) { /* 定义 DashboardCounts 函数。 */
	return r.dashboardCounts(tenant, start, end, nil) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *Repository) DashboardCountsForDevices(_ context.Context, tenant string, start, end int64, ids []string) ([]model.DashboardCount, error) { /* 定义 DashboardCountsForDevices 函数。 */
	return r.dashboardCounts(tenant, start, end, idSet(ids)) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *Repository) dashboardCounts(tenant string, start, end int64, allowed map[string]bool) ([]model.DashboardCount, error) { /* 定义 dashboardCounts 函数。 */
	r.mu.RLock()                                /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()                        /* 安排函数结束时执行清理。 */
	counts := map[string]model.DashboardCount{} /* 更新 counts 的值。 */
	add := func(kind, id, name string) {        /* 更新 add 的值。 */
		k := kind + "\x00" + id /* 更新 k 的值。 */
		v := counts[k]          /* 更新 v 的值。 */
		v.Kind = kind           /* 更新 v.Kind 的值。 */
		v.Key = id              /* 更新 v.Key 的值。 */
		v.Name = name           /* 更新 v.Name 的值。 */
		v.Count++               /* 执行当前语句并推进处理流程。 */
		counts[k] = v           /* 更新 counts[k] 的值。 */
	} /* 结束当前表达式或代码块。 */
	for _, d := range r.devices { /* 循环处理当前数据。 */
		if d.TenantID != tenant || allowed != nil && !allowed[d.ID] { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		state := r.states[key(tenant, d.ID)] /* 更新 state 的值。 */
		status := state.BusinessStatus       /* 更新 status 的值。 */
		if status == "ALARM" {               /* 判断条件并选择处理分支。 */
			status = "ONLINE" /* 更新 status 的值。 */
		} /* 结束当前表达式或代码块。 */
		if status == "" { /* 判断条件并选择处理分支。 */
			status = "NEVER_SEEN" /* 更新 status 的值。 */
		} /* 结束当前表达式或代码块。 */
		add("state", status, "") /* 执行当前语句并推进处理流程。 */
		connection, dataStatus := state.ConnectionStatus, state.DataStatus
		if connection == "" {
			connection = "UNKNOWN"
		}
		if dataStatus == "" {
			dataStatus = "UNKNOWN"
		}
		add("connection", connection, "")
		add("dataStatus", dataStatus, "")
		p := r.products[key(tenant, d.ProductID)] /* 更新 p 的值。 */
		name := p.Name                            /* 更新 name 的值。 */
		if name == "" {                           /* 判断条件并选择处理分支。 */
			name = d.ProductID /* 更新 name 的值。 */
		} /* 结束当前表达式或代码块。 */
		add("product", d.ProductID, name) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	for _, a := range r.alarms { /* 循环处理当前数据。 */
		if a.TenantID != tenant || allowed != nil && !allowed[a.DeviceID] { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if a.Status == "ACTIVE" { /* 判断条件并选择处理分支。 */
			add("level", a.AlarmLevel, "") /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if a.FirstTriggeredAt >= start && a.FirstTriggeredAt <= end { /* 判断条件并选择处理分支。 */
			add("day", strconv.FormatInt((a.FirstTriggeredAt-start)/86400000, 10), "") /* 执行当前语句并推进处理流程。 */
			status, alarmType := a.Status, a.AlarmType
			if status == "" {
				status = "UNKNOWN"
			}
			if alarmType == "" {
				alarmType = "UNKNOWN"
			}
			add("alarmStatus", status, "")
			add("alarmType", alarmType, "")
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	out := make([]model.DashboardCount, 0, len(counts)) /* 更新 out 的值。 */
	for _, v := range counts {                          /* 循环处理当前数据。 */
		out = append(out, v) /* 更新 out 的值。 */
	} /* 结束当前表达式或代码块。 */
	return out, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
