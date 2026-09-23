package postgres /* 声明 postgres 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"       /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func (r *Repository) GetProductsByIDs(ctx context.Context, tenant string, ids []string) (map[string]model.Product, error) { /* 定义 GetProductsByIDs 函数。 */
	out := make(map[string]model.Product, len(ids)) /* 更新 out 的值。 */
	if len(ids) == 0 {                              /* 判断条件并选择处理分支。 */
		return out, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	rows, err := r.pool.Query(ctx, `SELECT body FROM iot_product WHERE tenant_id=$1 AND id=ANY($2::text[])`, tenant, ids) /* 更新 err 的值。 */
	if err != nil {                                                                                                       /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer rows.Close() /* 安排函数结束时执行清理。 */
	for rows.Next() {  /* 循环处理当前数据。 */
		var body []byte                         /* 声明 body。 */
		var value model.Product                 /* 声明 value。 */
		if err = rows.Scan(&body); err != nil { /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if err = json.Unmarshal(body, &value); err != nil { /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		out[value.ID] = value /* 更新 out[value.ID] 的值。 */
	} /* 结束当前表达式或代码块。 */
	return out, rows.Err() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *Repository) GetDeviceStatesByIDs(ctx context.Context, tenant string, ids []string) (map[string]model.DeviceState, error) { /* 定义 GetDeviceStatesByIDs 函数。 */
	out := make(map[string]model.DeviceState, len(ids)) /* 更新 out 的值。 */
	if len(ids) == 0 {                                  /* 判断条件并选择处理分支。 */
		return out, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	rows, err := r.pool.Query(ctx, `SELECT body FROM device_state WHERE tenant_id=$1 AND device_id=ANY($2::text[])`, tenant, ids) /* 更新 err 的值。 */
	if err != nil {                                                                                                               /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer rows.Close() /* 安排函数结束时执行清理。 */
	for rows.Next() {  /* 循环处理当前数据。 */
		var body []byte                         /* 声明 body。 */
		var value model.DeviceState             /* 声明 value。 */
		if err = rows.Scan(&body); err != nil { /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if err = json.Unmarshal(body, &value); err != nil { /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		out[value.DeviceID] = value /* 更新 out[value.DeviceID] 的值。 */
	} /* 结束当前表达式或代码块。 */
	return out, rows.Err() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *Repository) GetStandardMessagesByRawIDs(ctx context.Context, tenant string, ids []string) (map[string]model.StandardMessage, error) { /* 定义 GetStandardMessagesByRawIDs 函数。 */
	out := make(map[string]model.StandardMessage, len(ids)) /* 更新 out 的值。 */
	if len(ids) == 0 {                                      /* 判断条件并选择处理分支。 */
		return out, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	rows, err := r.pool.Query(ctx, `SELECT DISTINCT ON (raw_message_id) raw_message_id,body FROM standard_message WHERE tenant_id=$1 AND raw_message_id=ANY($2::text[]) ORDER BY raw_message_id,ts DESC,message_id DESC`, tenant, ids) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                                                    /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer rows.Close() /* 安排函数结束时执行清理。 */
	for rows.Next() {  /* 循环处理当前数据。 */
		var id string                                /* 声明 id。 */
		var body []byte                              /* 声明 body。 */
		var value model.StandardMessage              /* 声明 value。 */
		if err = rows.Scan(&id, &body); err != nil { /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if err = json.Unmarshal(body, &value); err != nil { /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		out[id] = value /* 更新 out[id] 的值。 */
	} /* 结束当前表达式或代码块。 */
	return out, rows.Err() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *Repository) ListVideoCameraMappingsByDeviceIDs(ctx context.Context, tenant string, ids []string) (map[string][]model.VideoCameraMapping, error) { /* 定义 ListVideoCameraMappingsByDeviceIDs 函数。 */
	out := make(map[string][]model.VideoCameraMapping, len(ids)) /* 更新 out 的值。 */
	if len(ids) == 0 {                                           /* 判断条件并选择处理分支。 */
		return out, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	relations, err := r.pool.Query(ctx, `SELECT target_id,camera_id FROM video_camera_relation WHERE tenant_id=$1 AND relation_type='device' AND target_id=ANY($2::text[]) ORDER BY target_id,camera_id`, tenant, ids) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                                    /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	cameraToDevice := map[string]string{} /* 更新 cameraToDevice 的值。 */
	cameraIDs := []string{}               /* 更新 cameraIDs 的值。 */
	for relations.Next() {                /* 循环处理当前数据。 */
		var deviceID, cameraID string                               /* 声明 deviceID。 */
		if err = relations.Scan(&deviceID, &cameraID); err != nil { /* 判断条件并选择处理分支。 */
			break /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		cameraToDevice[cameraID] = deviceID     /* 更新 cameraToDevice[cameraID] 的值。 */
		cameraIDs = append(cameraIDs, cameraID) /* 更新 cameraIDs 的值。 */
	} /* 结束当前表达式或代码块。 */
	if err == nil { /* 判断条件并选择处理分支。 */
		err = relations.Err() /* 更新 err 的值。 */
	} /* 结束当前表达式或代码块。 */
	relations.Close() /* 执行当前语句并推进处理流程。 */
	if err != nil {   /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(cameraIDs) == 0 { /* 判断条件并选择处理分支。 */
		return out, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	rows, err := r.pool.Query(ctx, `SELECT `+videoCameraMappingColumns+` FROM video_camera_mapping WHERE tenant_id=$1 AND camera_id=ANY($2::text[]) ORDER BY camera_id`, tenant, cameraIDs) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                         /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer rows.Close() /* 安排函数结束时执行清理。 */
	for rows.Next() {  /* 循环处理当前数据。 */
		value, scanErr := r.scanVideoMapping(rows) /* 更新 scanErr 的值。 */
		if scanErr != nil {                        /* 判断条件并选择处理分支。 */
			return nil, scanErr /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		out[cameraToDevice[value.CameraID]] = append(out[cameraToDevice[value.CameraID]], value) /* 更新 out[cameraToDevice[value.CameraID]] 的值。 */
	} /* 结束当前表达式或代码块。 */
	return out, rows.Err() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
