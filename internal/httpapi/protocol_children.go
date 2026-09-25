package httpapi /* 声明 httpapi 包。 */

import (
	"iot-platform/internal/model"
	"net/http"
)

// Register a child only through a configured parent mapping. The repository
// derives the stable child ID from tenant, parent and address and handles retries.
func (s *Server) registerConfiguredChild(w http.ResponseWriter, r *http.Request) {
	if limited(r.Context()) {
		problem(w, 403, "登记子设备需要全部设备范围")
		return
	}
	tenant, parentID := claims(r).TenantID, r.PathValue("id")
	parent, err := s.engine.Repo.GetManagedDevice(r.Context(), tenant, parentID)
	if err != nil || parent.DeviceRole == "CHILD" {
		problem(w, 404, "所属主设备不存在")
		return
	}
	profileID := parent.Tags["connectorProfileId"]
	if profileID == "" {
		problem(w, 422, "主设备尚未关联接入点")
		return
	}
	profiles, err := s.engine.Repo.ListDeviceAccessProfiles(r.Context(), tenant)
	if err != nil {
		problem(w, 500, err.Error())
		return
	}
	var profile model.DeviceAccessProfile
	for _, item := range profiles {
		if item.ID == profileID && item.ProductID == parent.ProductID && (item.DeviceID == "" || item.DeviceID == parent.ID) {
			profile = item
			break
		}
	}
	if profile.ID == "" {
		problem(w, 422, "主设备的接入点已失效")
		return
	}
	var identity model.ChildIdentity
	if decode(w, r, &identity) != nil {
		return
	}
	child, created, err := s.engine.Repo.RegisterProtocolChild(r.Context(), profile, parent.ID, identity)
	if err != nil {
		problem(w, 422, "子设备地址、类型或已发布协议与主设备配置不匹配")
		return
	}
	s.audit(r, "device.child.register", "device", child.ID, map[string]any{"parentId": parent.ID, "created": created})
	write(w, map[bool]int{true: 201, false: 200}[created], map[string]any{"device": child, "reused": !created})
}

func (s *Server) getProductProtocolBinding(w http.ResponseWriter, r *http.Request) { /* 定义 getProductProtocolBinding 函数。 */
	binding, err := s.engine.Repo.GetProductProtocolBinding(r.Context(), claims(r).TenantID, r.PathValue("id")) /* 更新 err 的值。 */
	if err != nil {                                                                                             /* 判断条件并选择处理分支。 */
		problem(w, 404, "产品尚未绑定协议") /* 执行当前语句并推进处理流程。 */
		return                      /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	write(w, 200, binding) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) deviceChildren(w http.ResponseWriter, r *http.Request) { /* 定义 deviceChildren 函数。 */
	tenant, parent := claims(r).TenantID, r.PathValue("id")                                /* 更新 parent 的值。 */
	if _, err := s.engine.Repo.GetManagedDevice(r.Context(), tenant, parent); err != nil { /* 判断条件并选择处理分支。 */
		problem(w, 404, "主设备不存在") /* 执行当前语句并推进处理流程。 */
		return                    /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	pagination := parseListPagination(r)                                                                                              /* 更新 pagination 的值。 */
	items, total, err := s.engine.Repo.ListManagedDeviceChildren(r.Context(), tenant, parent, pagination.PageSize, pagination.Offset) /* 更新 err 的值。 */
	if err != nil {                                                                                                                   /* 判断条件并选择处理分支。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	out := []map[string]any{} /* 更新 out 的值。 */
	for _, d := range items { /* 循环处理当前数据。 */
		row := map[string]any{"device": d}                                                                     /* 更新 row 的值。 */
		if binding, e := s.engine.Repo.GetProductProtocolBinding(r.Context(), tenant, d.ProductID); e == nil { /* 判断条件并选择处理分支。 */
			row["binding"] = binding /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if state, e := s.engine.Repo.GetDeviceState(r.Context(), tenant, d.ID); e == nil { /* 判断条件并选择处理分支。 */
			row["runtimeState"] = state /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if product, e := s.engine.Repo.GetProduct(r.Context(), tenant, d.ProductID); e == nil { /* 判断条件并选择处理分支。 */
			row["productName"] = product.Name /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		out = append(out, row) /* 更新 out 的值。 */
	} /* 结束当前表达式或代码块。 */
	writeList(w, 200, out, total, pagination, nil) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
