package memory /* 声明 memory 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context" /* 执行当前语句并推进处理流程。 */
	"sort"    /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func idSet(ids []string) map[string]bool { /* 定义 idSet 函数。 */
	set := make(map[string]bool, len(ids)) /* 更新 set 的值。 */
	for _, id := range ids {               /* 循环处理当前数据。 */
		set[id] = true /* 更新 set[id] 的值。 */
	} /* 结束当前表达式或代码块。 */
	return set /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *Repository) GetProductsByIDs(_ context.Context, tenant string, ids []string) (map[string]model.Product, error) { /* 定义 GetProductsByIDs 函数。 */
	out := make(map[string]model.Product, len(ids)) /* 更新 out 的值。 */
	r.mu.RLock()                                    /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()                            /* 安排函数结束时执行清理。 */
	for _, id := range ids {                        /* 循环处理当前数据。 */
		if value, ok := r.products[key(tenant, id)]; ok { /* 判断条件并选择处理分支。 */
			out[id] = clone(value) /* 更新 out[id] 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return out, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *Repository) GetDeviceStatesByIDs(_ context.Context, tenant string, ids []string) (map[string]model.DeviceState, error) { /* 定义 GetDeviceStatesByIDs 函数。 */
	out := make(map[string]model.DeviceState, len(ids)) /* 更新 out 的值。 */
	r.mu.RLock()                                        /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()                                /* 安排函数结束时执行清理。 */
	for _, id := range ids {                            /* 循环处理当前数据。 */
		if value, ok := r.states[key(tenant, id)]; ok { /* 判断条件并选择处理分支。 */
			out[id] = clone(value) /* 更新 out[id] 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return out, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *Repository) GetStandardMessagesByRawIDs(_ context.Context, tenant string, ids []string) (map[string]model.StandardMessage, error) { /* 定义 GetStandardMessagesByRawIDs 函数。 */
	wanted := idSet(ids)                                    /* 更新 wanted 的值。 */
	out := make(map[string]model.StandardMessage, len(ids)) /* 更新 out 的值。 */
	r.mu.RLock()                                            /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()                                    /* 安排函数结束时执行清理。 */
	for _, value := range r.standard {                      /* 循环处理当前数据。 */
		if value.TenantID != tenant || !wanted[value.RawMessageID] { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if previous, ok := out[value.RawMessageID]; !ok || value.Timestamp > previous.Timestamp || value.Timestamp == previous.Timestamp && value.MessageID > previous.MessageID { /* 判断条件并选择处理分支。 */
			out[value.RawMessageID] = clone(value) /* 更新 out[value.RawMessageID] 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return out, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *Repository) ListVideoCameraMappingsByDeviceIDs(_ context.Context, tenant string, ids []string) (map[string][]model.VideoCameraMapping, error) { /* 定义 ListVideoCameraMappingsByDeviceIDs 函数。 */
	wanted := idSet(ids)                                         /* 更新 wanted 的值。 */
	out := make(map[string][]model.VideoCameraMapping, len(ids)) /* 更新 out 的值。 */
	r.mu.RLock()                                                 /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()                                         /* 安排函数结束时执行清理。 */
	for _, relations := range r.videoRelations {                 /* 循环处理当前数据。 */
		for _, relation := range relations { /* 循环处理当前数据。 */
			if relation.TenantID != tenant || relation.RelationType != "device" || !wanted[relation.TargetID] { /* 判断条件并选择处理分支。 */
				continue /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			if mapping, ok := r.videoMappings[key(tenant, relation.CameraID)]; ok { /* 判断条件并选择处理分支。 */
				out[relation.TargetID] = append(out[relation.TargetID], clone(mapping)) /* 更新 out[relation.TargetID] 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	for id := range out { /* 循环处理当前数据。 */
		sort.Slice(out[id], func(i, j int) bool { return out[id][i].CameraID < out[id][j].CameraID }) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	return out, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
