package memory /* 声明 memory 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                     /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"sort"                        /* 执行当前语句并推进处理流程。 */
	"time"                        /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func (r *Repository) ListManagedDeviceChildren(ctx context.Context, tenant, parent string, limit, offset int) ([]model.ManagedDevice, int, error) { /* 定义 ListManagedDeviceChildren 函数。 */
	if err := ctx.Err(); err != nil { /* 判断条件并选择处理分支。 */
		return nil, 0, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	r.mu.RLock()                     /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()             /* 安排函数结束时执行清理。 */
	items := []model.ManagedDevice{} /* 更新 items 的值。 */
	for _, d := range r.devices {    /* 循环处理当前数据。 */
		if d.TenantID == tenant && d.GatewayID == parent { /* 判断条件并选择处理分支。 */
			items = append(items, cloneManaged(d)) /* 更新 items 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID }) /* 执行当前语句并推进处理流程。 */
	total := len(items)                                                         /* 更新 total 的值。 */
	offset = max(0, min(offset, total))                                         /* 更新 offset 的值。 */
	limit = max(1, min(limit, 100))                                             /* 更新 limit 的值。 */
	return items[offset:min(total, offset+limit)], total, nil                   /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *Repository) RegisterProtocolChild(ctx context.Context, expected model.DeviceAccessProfile, parentID string, identity model.ChildIdentity) (model.ManagedDevice, bool, error) { /* 定义 RegisterProtocolChild 函数。 */
	if err := ctx.Err(); err != nil { /* 判断条件并选择处理分支。 */
		return model.ManagedDevice{}, false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	r.mu.Lock()                                                                   /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()                                                           /* 安排函数结束时执行清理。 */
	current := r.accessProfiles[key(expected.TenantID, expected.ID)]              /* 更新 current 的值。 */
	parent := r.devices[key(expected.TenantID, parentID)]                         /* 更新 parent 的值。 */
	if r.products[key(expected.TenantID, parent.ProductID)].Status != "ENABLED" { /* 判断条件并选择处理分支。 */
		return model.ManagedDevice{}, false, model.ErrProtocolRegistration /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var productID string                      /* 声明 productID。 */
	for _, v := range current.ChildProducts { /* 循环处理当前数据。 */
		if v.Type == identity.Type { /* 判断条件并选择处理分支。 */
			productID = v.ProductID /* 更新 productID 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	d, err := model.ProtocolChildDevice(expected, current, parent, r.products[key(expected.TenantID, productID)], identity, time.Now().UnixMilli()) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                 /* 判断条件并选择处理分支。 */
		return d, false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	binding, ok := r.protocolBindings[key(d.TenantID, d.ProductID)]                     /* 更新 ok 的值。 */
	release := r.protocolReleases[key(d.TenantID, binding.ProtocolID, binding.Version)] /* 更新 release 的值。 */
	if !ok || release.Status != "PUBLISHED" {                                           /* 判断条件并选择处理分支。 */
		return d, false, model.ErrProtocolRegistration /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for _, old := range r.devices { /* 循环处理当前数据。 */
		if old.AccessKey == d.AccessKey && (old.TenantID != d.TenantID || old.ID != d.ID) { /* 判断条件并选择处理分支。 */
			return d, false, model.ErrProtocolRegistration /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	created := true                                      /* 更新 created 的值。 */
	if old, ok := r.devices[key(d.TenantID, d.ID)]; ok { /* 判断条件并选择处理分支。 */
		if err = model.ExistingProtocolChild(old, d); err != nil { /* 判断条件并选择处理分支。 */
			return d, false, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		old.UpdatedAt = d.UpdatedAt /* 更新 old.UpdatedAt 的值。 */
		if identity.Name != "" {    /* 判断条件并选择处理分支。 */
			old.Name = identity.Name /* 更新 old.Name 的值。 */
		} /* 结束当前表达式或代码块。 */
		d = old         /* 更新 d 的值。 */
		created = false /* 更新 created 的值。 */
	} /* 结束当前表达式或代码块。 */
	parent.DeviceRole = "GATEWAY"                                     /* 更新 parent.DeviceRole 的值。 */
	r.devices[key(parent.TenantID, parent.ID)] = cloneManaged(parent) /* 执行当前语句并推进处理流程。 */
	r.devices[key(d.TenantID, d.ID)] = cloneManaged(d)                /* 执行当前语句并推进处理流程。 */
	return cloneManaged(d), created, nil                              /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
