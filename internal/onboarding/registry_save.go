package onboarding

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// newID returns prefix followed by 12 random hex digits.
func newID(prefix string) string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

// DeviceSave is the outcome of SaveDevice.
type DeviceSave struct {
	Device  model.ManagedDevice
	Product model.Product
	// Created is set for a device that is not registered yet. SaveDevice only
	// validates it; the caller registers it through enrollment.
	Created bool
	// TimingChanged is set when an existing device's reporting interval or
	// offline tolerance changed, so its state must be re-evaluated.
	TimingChanged bool
}

// SaveDevice validates a device edited directly in the registry and stores
// the changes of an existing device. Credentials, the platform connection and
// the registration source keep their stored values; a child device must name
// a registered gateway and inherits its connection; a direct device may only
// select an access profile of its template. Rule violations are model.Invalid
// or model.Conflict errors with the message for the user.
func (s *Service) SaveDevice(ctx context.Context, v model.ManagedDevice) (DeviceSave, error) {
	if v.ID == "" {
		v.ID = newID("device_")
	}
	if v.Name == "" || v.ProductID == "" {
		return DeviceSave{}, model.Invalid("name and productId are required")
	}
	if err := model.ValidateDeviceTiming(v.ReportIntervalSec, v.OfflineToleranceSec); err != nil {
		return DeviceSave{}, model.Invalid(err.Error())
	}
	product, err := s.Repo.GetProduct(ctx, v.TenantID, v.ProductID)
	if errors.Is(err, model.ErrNotFound) {
		return DeviceSave{}, model.Invalid("product not found")
	}
	if err != nil {
		return DeviceSave{}, err
	}
	now := time.Now().UnixMilli()
	result := DeviceSave{Product: product}
	if old, err := s.Repo.GetManagedDevice(ctx, v.TenantID, v.ID); err == nil {
		result.TimingChanged = old.ReportIntervalSec != v.ReportIntervalSec || old.OfflineToleranceSec != v.OfflineToleranceSec
		if old.RegistrationSource == "PROTOCOL_CHILD_AUTO" {
			if v.ProductID != old.ProductID || v.GatewayID != old.GatewayID || v.DeviceRole != "CHILD" {
				return DeviceSave{}, model.Invalid("自动注册子设备的产品与主设备归属不可直接改写")
			}
		}
		if v.ProductID != old.ProductID {
			return DeviceSave{}, model.Conflict("已登记设备不能直接更换设备模板，请从目标模板重新接入")
		}
		v.AccessKey, v.SecretHash, v.SecretHint, v.CreatedAt = old.AccessKey, old.SecretHash, old.SecretHint, old.CreatedAt
		if v.Tags == nil {
			v.Tags = map[string]string{}
		}
		// Platform connection fields keep their stored values; the connection and
		// child address may be re-selected but are not cleared by an edit.
		v.Connector, v.ChildType, v.OnboardingRequestHash = old.Connector, old.ChildType, old.OnboardingRequestHash
		if v.ConnectorProfileID == "" {
			v.ConnectorProfileID = old.ConnectorProfileID
		}
		if v.ChildAddress == "" {
			v.ChildAddress = old.ChildAddress
		}
		if v.RegistrationSource == "" {
			v.RegistrationSource = old.RegistrationSource
		}
		if old.AutoRegistered {
			v.AutoRegistered = true
		}
	} else if errors.Is(err, model.ErrNotFound) {
		result.Created = true
		v.CreatedAt = now
	} else {
		return DeviceSave{}, err
	}
	if v.Status == "" {
		v.Status = "ENABLED"
	}
	if v.DeviceRole == "" {
		if product.Category == "gateway" {
			v.DeviceRole = "GATEWAY"
		} else {
			v.DeviceRole = "DIRECT"
		}
	}
	if v.DeviceRole != "DIRECT" && v.DeviceRole != "GATEWAY" && v.DeviceRole != "CHILD" {
		return DeviceSave{}, model.Invalid("deviceRole must be DIRECT, GATEWAY or CHILD")
	}
	if v.RegistrationSource == "" {
		v.RegistrationSource = "MANUAL"
	}
	if v.Tags == nil {
		v.Tags = map[string]string{}
	}
	if v.DeviceRole == "CHILD" {
		if v.GatewayID == "" || v.GatewayID == v.ID {
			return DeviceSave{}, model.Invalid("a child device must reference a different gateway")
		}
		gateway, err := s.Repo.GetManagedDevice(ctx, v.TenantID, v.GatewayID)
		if errors.Is(err, model.ErrNotFound) {
			return DeviceSave{}, model.Invalid("gateway not found")
		}
		if err != nil {
			return DeviceSave{}, err
		}
		// The parent must be registered as a gateway; the template category alone does not grant it.
		if !gateway.IsGateway() {
			return DeviceSave{}, model.Invalid("selected parent device is not a gateway")
		}
		// A child uses its parent's physical connection; the caller cannot bind
		// it to an unrelated tenant-wide listener.
		if requested := v.ConnectorProfileID; requested != "" && requested != gateway.ConnectorProfileID {
			return DeviceSave{}, model.Invalid("子设备只能继承所属主设备的连接")
		}
		v.ConnectorProfileID = gateway.ConnectorProfileID
	} else {
		v.GatewayID = ""
		if requested := v.ConnectorProfileID; requested != "" {
			profiles, err := s.Repo.ListDeviceAccessProfiles(ctx, v.TenantID)
			if err != nil {
				return DeviceSave{}, err
			}
			valid := false
			for _, candidate := range profiles {
				if candidate.ID == requested && candidate.ProductID == v.ProductID && (candidate.DeviceID == "" || candidate.DeviceID == v.ID) {
					valid = true
					break
				}
			}
			if !valid {
				return DeviceSave{}, model.Invalid("接入点不可用，请重新选择当前设备模板的接入点")
			}
		}
	}
	result.Device = v
	if result.Created {
		return result, nil
	}
	v.UpdatedAt = now
	if err := s.Repo.SaveManagedDevice(ctx, v); err != nil {
		return DeviceSave{}, err
	}
	result.Device = v
	return result, nil
}

// ProductSave is the outcome of SaveProduct.
type ProductSave struct {
	Product  model.Product
	Protocol model.ProtocolPackage
	Created  bool
	// TimingChanged is set when an existing template's reporting interval or
	// offline tolerance changed, so its devices must be re-evaluated.
	TimingChanged bool
}

// SaveProduct validates and stores a template edited directly. resolve
// returns the protocol package the template names; it runs after the
// template itself is valid. Transport and payload format default to the
// protocol's. A template's protocol, and the configuration of a template that
// already has devices, change only through the template preparation flow.
func (s *Service) SaveProduct(ctx context.Context, v model.Product, resolve func(context.Context, string, string) (model.ProtocolPackage, error)) (ProductSave, error) {
	v.PreparationStatus = ""
	v.Reusable = false
	if v.ID == "" {
		v.ID = newID("product_")
	}
	if v.Name == "" || v.ProtocolPackageID == "" {
		return ProductSave{}, model.Invalid("name and protocolPackageId are required")
	}
	if err := ValidateThingModel(v.ThingModel); err != nil {
		return ProductSave{}, model.Invalid(err.Error())
	}
	if err := model.ValidateDeviceTiming(v.ReportIntervalSec, v.OfflineToleranceSec); err != nil {
		return ProductSave{}, model.Invalid(err.Error())
	}
	if v.VerificationRules != nil {
		rules, err := NormalizeVerificationRules(*v.VerificationRules)
		if err != nil {
			return ProductSave{}, err
		}
		v.VerificationRules = &rules
	}
	pkg, err := resolve(ctx, v.TenantID, v.ProtocolPackageID)
	if err != nil {
		return ProductSave{}, model.Invalid("协议不可用，请选择内置标准上报或已发布的协议版本")
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
	result := ProductSave{Protocol: pkg}
	if old, err := s.Repo.GetProduct(ctx, v.TenantID, v.ID); err == nil {
		v.CreatedAt = old.CreatedAt
		result.TimingChanged = old.ReportIntervalSec != v.ReportIntervalSec || old.OfflineToleranceSec != v.OfflineToleranceSec
		if v.ProtocolPackageID != old.ProtocolPackageID {
			return ProductSave{}, model.Conflict("协议版本变更请在模板准备流程中保存候选配置并明确应用")
		}
		if v.VerificationRules == nil {
			v.VerificationRules = old.VerificationRules
		}
		before := model.TemplateCandidate{Product: old, VerificationRules: ProductVerificationRules(old)}
		after := model.TemplateCandidate{Product: v, VerificationRules: ProductVerificationRules(v)}
		if CandidateFingerprint(before) != CandidateFingerprint(after) {
			_, count, err := s.Repo.ListManagedDevicesFiltered(ctx, ports.DeviceFilter{TenantID: v.TenantID, RestrictProducts: true, ProductIDs: []string{v.ID}}, 1, 0)
			if err != nil {
				return ProductSave{}, err
			}
			if count > 0 {
				return ProductSave{}, model.Conflict("运行中的模板配置请通过模板准备流程联调后应用")
			}
		}
	} else if errors.Is(err, model.ErrNotFound) {
		result.Created = true
	} else {
		return ProductSave{}, err
	}
	if v.CreatedAt == 0 {
		v.CreatedAt = now
	}
	v.UpdatedAt = now
	if err := s.Repo.SaveProduct(ctx, v); err != nil {
		return ProductSave{}, err
	}
	result.Product = v
	return result, nil
}
