package httpapi

import (
	"iot-platform/internal/devicescope"
	"net/http"
	"slices"
	"strings"

	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
)

// The older inventory and discovery APIs may still be used by integrations.
// New inventory records must use the same atomic registration and template
// snapshot as the onboarding API; ordinary edits retain their existing path.
func (s *Server) enrollCompatibleDevice(w http.ResponseWriter, r *http.Request, device model.ManagedDevice, product model.Product, trial bool, auditAction string) {
	if devicescope.Limited(r.Context()) {
		problem(w, 403, "登记新设备需要全部设备范围")
		return
	}
	if trial && !requestAllows(r, "PUT", "/api/v1/products/:id") {
		problem(w, 403, "首台验证需要设备模板配置权限")
		return
	}
	if device.IsChild() || device.ChildAddress != "" || device.ChildType != "" {
		problem(w, 422, "子设备请从主设备详情添加，并填写稳定的子设备地址")
		return
	}
	if device.Status != "" && device.Status != "ENABLED" {
		problem(w, 422, "新设备须完成接入登记后再调整启停状态，请使用设备接入流程")
		return
	}
	if !trial {
		_, ready, err := s.onboarding.TemplateReadiness(r.Context(), product.TenantID, product.ID)
		if err != nil {
			s.enrollProblem(w, r, err)
			return
		}
		if !ready {
			problem(w, 409, "该模板尚未通过首台实机验证，请由模板工作人员完成验证后再登记同类设备")
			return
		}
	}
	plan, err := s.onboarding.Plan(r.Context(), product.TenantID, product)
	if err != nil {
		s.enrollProblem(w, r, err)
		return
	}
	connection := onboarding.EnrollConnection{Mode: plan.Mode}
	switch plan.Mode {
	case onboarding.ModeStandard, onboarding.ModeManaged:
		if device.ConnectorProfileID != "" {
			problem(w, 422, "该模板不使用共享监听，请通过设备接入流程确认连接方式")
			return
		}
		if plan.Mode == onboarding.ModeStandard {
			connection.Transport = plan.Connector
			if value := strings.ToUpper(device.Connector); value != "" {
				connection.Transport = value
			}
		}
	case onboarding.ModeListener:
		connection.ProfileID = device.ConnectorProfileID
		if connection.ProfileID == "" {
			profiles, e := s.onboarding.SharedListeners(r.Context(), product.TenantID, product.ID)
			if e != nil {
				s.enrollProblem(w, r, e)
				return
			}
			for _, profile := range profiles {
				if !profile.Enabled || !slices.Contains(plan.Networks, profile.Network) {
					continue
				}
				if connection.ProfileID != "" {
					problem(w, 422, "模板有多个共享监听，请通过设备接入流程选择接入点")
					return
				}
				connection.ProfileID = profile.ID
			}
		}
		if connection.ProfileID == "" {
			problem(w, 422, "模板没有可复用的共享监听，请通过设备接入流程配置连接")
			return
		}
	case onboarding.ModePoll:
		problem(w, 422, "该模板需要设备地址、端口和站号，请通过设备接入流程登记")
		return
	default:
		problem(w, 422, plan.Reason+"，请通过设备接入流程完成模板配置")
		return
	}
	q := onboarding.EnrollRequest{Trial: trial, RequestID: "compat-" + onboarding.Hash(product.TenantID + "/" + device.ID)[:32], ProductID: product.ID, Device: onboarding.EnrollDevice{ID: device.ID, Name: device.Name, DeviceRole: device.DeviceRole, Description: device.Description, Tags: device.Tags}, Connection: connection}
	result, err := s.onboarding.Enroll(r.Context(), product.TenantID, q)
	if err != nil {
		s.enrollProblem(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	response := map[string]any{"device": result.Device, "accessInfo": s.deviceAccessInfo(result.Device, result.Product)}
	if result.Credential.Secret != "" {
		response["credential"] = result.Credential
	}
	if !result.Reused {
		s.audit(r, auditAction, "device", result.Device.ID, map[string]any{"productId": product.ID, "mode": result.Mode, "trial": trial})
	}
	write(w, 201, response)
}
