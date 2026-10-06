package httpapi

import (
	"context"
	"iot-platform/internal/logkey"
	"net/http"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/parser"
)

func (s *Server) products(w http.ResponseWriter, r *http.Request) {
	pagination := parseListPagination(r)
	items, total, err := s.engine.Repo.ListProductsPage(r.Context(), claims(r).TenantID, r.URL.Query().Get("q"), pagination.PageSize, pagination.Offset)
	if err != nil {
		s.fail(w, r, err, "")
		return
	}
	for i := range items {
		status, ready, e := s.onboarding.TemplateReadiness(r.Context(), claims(r).TenantID, items[i].ID)
		if e != nil {
			s.fail(w, r, e, "读取模板准备状态失败")
			return
		}
		items[i].PreparationStatus, items[i].Reusable = status, ready
	}
	writeList(w, 200, items, total, pagination, nil)
}

// productBindingCheck lists templates without a usable protocol; their raw
// messages fail to parse unless they use the platform's standard format.
func (s *Server) productBindingCheck(w http.ResponseWriter, r *http.Request) {
	items, err := s.engine.UnboundProducts(r.Context(), claims(r).TenantID)
	if err != nil {
		s.fail(w, r, err, "检查设备模板协议绑定失败")
		return
	}
	write(w, 200, map[string]any{"items": items})
}

func (s *Server) saveProduct(w http.ResponseWriter, r *http.Request) {
	var v model.Product
	if decode(w, r, &v) != nil {
		return
	}
	c := claims(r)
	v.TenantID = c.TenantID
	if id := r.PathValue("id"); id != "" {
		v.ID = id
	}
	saved, err := s.onboarding.SaveProduct(r.Context(), v, s.productProtocol)
	if err != nil {
		s.fail(w, r, err, "保存设备模板失败")
		return
	}
	v, pkg, newProduct := saved.Product, saved.Protocol, saved.Created
	s.engine.ProtocolsChanged(c.TenantID)
	// Versioned protocols are parsed by the bound release; the binding is their single source.
	_, releaseErr := s.engine.Repo.GetProtocolRelease(r.Context(), c.TenantID, pkg.Protocol, pkg.Version)
	if newProduct && v.ProtocolPackageID != parser.StandardProtocolID+"@1.0.0" && releaseErr == nil {
		if _, err := s.bindProtocolRelease(r, pkg.Protocol, pkg.Version, v.ID); err != nil {
			s.fail(w, r, err, "绑定协议版本失败")
			return
		}
		v, err = s.engine.Repo.GetProduct(r.Context(), c.TenantID, v.ID)
		if err != nil {
			s.fail(w, r, err, "")
			return
		}
	}
	if saved.TimingChanged {
		s.applyTemplateTiming(c.TenantID, v.ID)
	}
	s.audit(r, "product.save", "product", v.ID, map[string]any{"status": v.Status, "reportIntervalSec": v.ReportIntervalSec, "offlineToleranceSec": v.OfflineToleranceSec})
	write(w, 201, v)
}

// applyTemplateTiming moves existing device states of a template to its new
// reporting timing in the background; a large template must not hold the
// request open.
func (s *Server) applyTemplateTiming(tenant, productID string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if changed, err := s.engine.ApplyDeviceTiming(ctx, tenant, productID, ""); err != nil {
			s.log.Error("apply template reporting timing failed", logkey.Tenant, tenant, "product", productID, "updated", changed, "error", err)
		}
	}()
}
