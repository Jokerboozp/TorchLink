package httpapi

import "net/http"

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
		problem(w, 500, err.Error())
		return
	}
	out := []map[string]any{}
	for _, d := range items {
		row := map[string]any{"device": d}
		if binding, e := s.engine.Repo.GetProductProtocolBinding(r.Context(), tenant, d.ProductID); e == nil {
			row["binding"] = binding
		}
		if state, e := s.engine.Repo.GetDeviceState(r.Context(), tenant, d.ID); e == nil {
			row["runtimeState"] = state
		}
		if product, e := s.engine.Repo.GetProduct(r.Context(), tenant, d.ProductID); e == nil {
			row["productName"] = product.Name
		}
		out = append(out, row)
	}
	writeList(w, 200, out, total, pagination, nil)
}
