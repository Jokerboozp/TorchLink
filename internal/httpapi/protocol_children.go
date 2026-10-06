package httpapi

import (
	"iot-platform/internal/devicescope"
	"iot-platform/internal/model"
	"net/http"
)

// Register a child only through a configured parent mapping. The repository
// derives the stable child ID from tenant, parent and address and handles retries.
func (s *Server) registerConfiguredChild(w http.ResponseWriter, r *http.Request) {
	if devicescope.Limited(r.Context()) {
		problem(w, 403, "登记子设备需要全部设备范围")
		return
	}
	tenant, parentID := claims(r).TenantID, r.PathValue("id")
	parent, err := s.engine.Repo.GetManagedDevice(r.Context(), tenant, parentID)
	if err != nil || parent.DeviceRole == "CHILD" {
		problem(w, 404, "所属主设备不存在")
		return
	}
	profileID := parent.ConnectorProfileID
	if profileID == "" {
		problem(w, 422, "主设备尚未关联接入点")
		return
	}
	profiles, err := s.engine.Repo.ListDeviceAccessProfiles(r.Context(), tenant)
	if err != nil {
		s.internalError(w, r, err)
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

func (s *Server) getProductProtocolBinding(w http.ResponseWriter, r *http.Request) {
	binding, err := s.engine.Repo.GetProductProtocolBinding(r.Context(), claims(r).TenantID, r.PathValue("id"))
	if err != nil {
		problem(w, 404, "产品尚未绑定协议")
		return
	}
	write(w, 200, binding)
}

func (s *Server) deviceChildren(w http.ResponseWriter, r *http.Request) {
	tenant, parent := claims(r).TenantID, r.PathValue("id")
	if _, err := s.engine.Repo.GetManagedDevice(r.Context(), tenant, parent); err != nil {
		problem(w, 404, "主设备不存在")
		return
	}
	pagination := parseListPagination(r)
	items, total, err := s.engine.Repo.ListManagedDeviceChildren(r.Context(), tenant, parent, pagination.PageSize, pagination.Offset)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	// One query per kind for the whole page instead of three per child.
	productIDs, deviceIDs := make([]string, 0, len(items)), make([]string, 0, len(items))
	seen := map[string]bool{}
	for _, d := range items {
		deviceIDs = append(deviceIDs, d.ID)
		if !seen[d.ProductID] {
			seen[d.ProductID] = true
			productIDs = append(productIDs, d.ProductID)
		}
	}
	bindings, err := s.engine.Repo.GetProductProtocolBindingsByIDs(r.Context(), tenant, productIDs)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	states, err := s.engine.Repo.GetDeviceStatesByIDs(r.Context(), tenant, deviceIDs)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	products, err := s.engine.Repo.GetProductsByIDs(r.Context(), tenant, productIDs)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := []map[string]any{}
	for _, d := range items {
		row := map[string]any{"device": d}
		if binding, ok := bindings[d.ProductID]; ok {
			row["binding"] = binding
		}
		if state, ok := states[d.ID]; ok {
			row["runtimeState"] = state
		}
		if product, ok := products[d.ProductID]; ok {
			row["productName"] = product.Name
		}
		out = append(out, row)
	}
	writeList(w, 200, out, total, pagination, nil)
}
