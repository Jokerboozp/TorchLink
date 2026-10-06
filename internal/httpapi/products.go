package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
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
	v.PreparationStatus = ""
	v.Reusable = false
	if id := r.PathValue("id"); id != "" {
		v.ID = id
	}
	if v.ID == "" {
		v.ID = "product_" + randomHex(6)
	}
	if v.Name == "" || v.ProtocolPackageID == "" {
		problem(w, 422, "name and protocolPackageId are required")
		return
	}
	if err := onboarding.ValidateThingModel(v.ThingModel); err != nil {
		problem(w, 422, err.Error())
		return
	}
	if err := model.ValidateDeviceTiming(v.ReportIntervalSec, v.OfflineToleranceSec); err != nil {
		problem(w, 422, err.Error())
		return
	}
	if v.VerificationRules != nil {
		rules, e := onboarding.NormalizeVerificationRules(*v.VerificationRules)
		if e != nil {
			s.enrollProblem(w, r, e)
			return
		}
		v.VerificationRules = &rules
	}
	pkg, err := s.productProtocol(r.Context(), c.TenantID, v.ProtocolPackageID)
	if err != nil {
		problem(w, 422, "协议不可用，请选择内置标准上报或已发布的协议版本")
		return
	}
	if v.Transport == "" {
		v.Transport = pkg.Transport
	}
	if v.PayloadFormat == "" {
		v.PayloadFormat = pkg.PayloadFormat
	}
	if v.Status == "" {
		v.Status = "ENABLED"
	}
	now := time.Now().UnixMilli()
	newProduct, timingChanged := false, false
	if old, getErr := s.engine.Repo.GetProduct(r.Context(), c.TenantID, v.ID); getErr == nil {
		v.CreatedAt = old.CreatedAt
		timingChanged = old.ReportIntervalSec != v.ReportIntervalSec || old.OfflineToleranceSec != v.OfflineToleranceSec
		if v.ProtocolPackageID != old.ProtocolPackageID {
			problem(w, 409, "协议版本变更请在模板准备流程中保存候选配置并明确应用")
			return
		}
		if v.VerificationRules == nil {
			v.VerificationRules = old.VerificationRules
		}
		before := model.TemplateCandidate{Product: old, VerificationRules: onboarding.ProductVerificationRules(old)}
		after := model.TemplateCandidate{Product: v, VerificationRules: onboarding.ProductVerificationRules(v)}
		if onboarding.CandidateFingerprint(before) != onboarding.CandidateFingerprint(after) {
			_, count, e := s.engine.Repo.ListManagedDevicesFiltered(r.Context(), ports.DeviceFilter{TenantID: c.TenantID, RestrictProducts: true, ProductIDs: []string{v.ID}}, 1, 0)
			if e != nil {
				s.fail(w, r, e, "读取模板使用情况失败")
				return
			}
			if count > 0 {
				problem(w, 409, "运行中的模板配置请通过模板准备流程联调后应用")
				return
			}
		}
	} else if errors.Is(getErr, model.ErrNotFound) {
		newProduct = true
	} else {
		s.fail(w, r, getErr, "")
		return
	}
	if v.CreatedAt == 0 {
		v.CreatedAt = now
	}
	v.UpdatedAt = now
	if err = s.engine.Repo.SaveProduct(r.Context(), v); err != nil {
		s.fail(w, r, err, "")
		return
	}
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
	if timingChanged {
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
			s.log.Error("apply template reporting timing failed", "tenant", tenant, "product", productID, "updated", changed, "error", err)
		}
	}()
}
