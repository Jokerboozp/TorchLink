package onboarding

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"iot-platform/internal/model"
)

// PublicAddresses tells which device-facing platform addresses are configured.
type PublicAddresses struct {
	HTTP bool
	MQTT bool
}

// Preflight lists what must be in place before a device of the template can
// report data. It never writes resources and does not contact field devices.
type Preflight struct {
	Product  model.Product               `json:"product"`
	Plan     AccessPlan                  `json:"plan"`
	Profiles []model.DeviceAccessProfile `json:"profiles"`
	Checks   []DiagnosisCheck            `json:"checks"`
	Ready    bool                        `json:"ready"`
}

// SharedListeners returns the template's listener profiles that several devices
// can use, with the runtime status of the local listener process.
func (s *Service) SharedListeners(ctx context.Context, tenant, productID string) ([]model.DeviceAccessProfile, error) {
	profiles, err := s.Repo.ListDeviceAccessProfiles(ctx, tenant)
	if err != nil {
		return nil, err
	}
	out := []model.DeviceAccessProfile{}
	for _, p := range profiles {
		if p.TenantID != tenant || p.ProductID != productID || p.Mode != "listener" || p.ConnectionMode == "dial" || p.DeviceID != "" || p.EdgeNodeID != "" {
			continue
		}
		p.RuntimeStatus = s.listenerStatus(p)
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *Service) listenerStatus(p model.DeviceAccessProfile) string {
	if !p.Enabled {
		return "DISABLED"
	}
	if s.ListenerStatus == nil {
		return p.RuntimeStatus
	}
	status, _, _ := s.ListenerStatus(p.TenantID, p.ID)
	if status == "" {
		return p.RuntimeStatus
	}
	return status
}

// Preflight evaluates a saved template, or a template still being defined in
// the wizard when draft is non-nil.
func (s *Service) Preflight(ctx context.Context, tenant, productID string, draft *NewProduct, addresses PublicAddresses) (Preflight, error) {
	var product model.Product
	var plan AccessPlan
	var err error
	if draft != nil {
		product = draft.product(tenant, 0)
		plan, err = s.DraftPlan(ctx, tenant, product)
	} else {
		product, err = s.Repo.GetProduct(ctx, tenant, productID)
		if errors.Is(err, model.ErrNotFound) {
			return Preflight{}, &EnrollError{Status: 404, Message: "设备模板不存在或当前账号无权查看"}
		}
		if err != nil {
			return Preflight{}, err
		}
		plan, err = s.Plan(ctx, tenant, product)
	}
	if err != nil {
		return Preflight{}, err
	}
	result := Preflight{Product: product, Plan: plan, Profiles: []model.DeviceAccessProfile{}}
	add := func(key, label, state, detail string) {
		result.Checks = append(result.Checks, DiagnosisCheck{Key: key, Label: label, State: state, Detail: detail})
	}
	if product.Status == "ENABLED" {
		add("product", "设备模板", "passed", "已启用")
	} else {
		add("product", "设备模板", "failed", "模板未启用，不能添加新设备")
	}
	switch {
	case plan.Mode == ModeUnsupported:
		add("protocol", "通信协议", "failed", plan.Reason)
	default:
		add("protocol", "通信协议", "passed", strings.TrimSuffix(plan.Protocol.ID+" · "+plan.Protocol.Version, " · "))
	}
	switch plan.Mode {
	case ModeStandard:
		ready := addresses.MQTT
		if plan.Connector == "HTTP" {
			ready = addresses.HTTP
		}
		if ready {
			add("address", "设备端地址", "passed", "平台对外 "+plan.Connector+" 地址已配置")
		} else {
			add("address", "设备端地址", "warning", "尚未配置平台对外 "+plan.Connector+" 地址，设备保存后仍需管理员补齐")
		}
		if plan.Connector == "MQTT" && s.MQTTHealth != nil {
			healthCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			err := s.MQTTHealth(healthCtx)
			cancel()
			if err != nil {
				add("broker", "MQTT 服务", "warning", "平台当前未连上 MQTT Broker，设备连接前需先恢复")
			} else {
				add("broker", "MQTT 服务", "passed", "平台已连上 MQTT Broker")
			}
		}
	case ModeManaged:
		if addresses.HTTP {
			add("address", "设备端地址", "passed", "平台对外 HTTP 地址已配置")
		} else {
			add("address", "设备端地址", "warning", "尚未配置平台对外 HTTP 地址，设备保存后仍需管理员补齐")
		}
	case ModeListener:
		if product.ID != "" && draft == nil {
			if result.Profiles, err = s.SharedListeners(ctx, tenant, product.ID); err != nil {
				return Preflight{}, err
			}
		}
		usable := 0
		for _, p := range result.Profiles {
			if p.Enabled && p.PublicHost != "" && hasNetwork(plan.Networks, p.Network) {
				usable++
			}
		}
		switch {
		case usable > 0:
			add("listener", "平台接入点", "passed", "已有可用的平台监听")
		case plan.Dial:
			add("listener", "平台接入点", "warning", "尚无可用监听；可以新建监听，或由平台主动连接设备")
		default:
			add("listener", "平台接入点", "warning", "尚无可用监听，需要在下一步新建")
		}
	case ModePoll:
		add("target", "设备地址", "passed", "在下一步填写设备地址、端口和站号，平台按点表定时读取")
	}
	result.Ready = product.Status == "ENABLED" && plan.Mode != ModeUnsupported
	return result, nil
}
