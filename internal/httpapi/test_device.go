package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	"iot-platform/internal/model"
)

// provisionTestDevice creates a tenant-scoped, repeatable fixture for the web
// test console. It deliberately uses the normal protocol, product and device
// repositories so data sent from the console follows the same pipeline as a
// managed device report. Alarm rules remain tenant-owned configuration and are
// never created implicitly by this endpoint.
func (s *Server) provisionTestDevice(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Reset bool `json:"reset"`
	}
	if decode(w, r, &in) != nil {
		return
	}

	tenantID := claims(r).TenantID
	scope := testDeviceScope(tenantID)
	now := time.Now().UnixMilli()
	protocolID := "protocol_test_device_" + scope
	productID := "product_test_device_" + scope
	deviceID := "device_test_device_" + scope

	pkg, packageCreated, err := s.ensureTestProtocolPackage(r.Context(), tenantID, protocolID, in.Reset, now)
	if err != nil {
		problem(w, http.StatusInternalServerError, err.Error())
		return
	}
	product, productCreated, err := s.ensureTestProduct(r.Context(), tenantID, productID, pkg.ID, in.Reset, now)
	if err != nil {
		problem(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := s.removeLegacyTestAlarmRule(r.Context(), tenantID, "rule_test_device_"+scope, product.ID); err != nil {
		problem(w, http.StatusInternalServerError, err.Error())
		return
	}
	device, deviceCreated, credential, err := s.ensureTestManagedDevice(r.Context(), tenantID, deviceID, product.ID, in.Reset, now)
	if err != nil {
		problem(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.audit(r, "test-device.provision", "device", device.ID, map[string]any{
		"protocolPackageId": pkg.ID,
		"productId":         product.ID,
		"reset":             in.Reset,
	})
	result := map[string]any{
		"device":          device,
		"product":         product,
		"protocolPackage": pkg,
		"rule":            nil,
		"templates":       testDeviceTemplates(),
		"created": map[string]bool{
			"protocolPackage": packageCreated,
			"product":         productCreated,
			"device":          deviceCreated,
		},
	}
	if credential.Secret != "" {
		// Match the normal device-registration contract: the secret is returned
		// only at initial creation or when a fixture had no credential.
		result["credential"] = credential
	}
	status := http.StatusOK
	if packageCreated || productCreated || deviceCreated {
		status = http.StatusCreated
	}
	write(w, status, result)
}

func testDeviceScope(tenantID string) string {
	sum := sha256.Sum256([]byte(tenantID))
	return hex.EncodeToString(sum[:4])
}

func testProtocolPackage(tenantID, id string, now int64) model.ProtocolPackage {
	return model.ProtocolPackage{
		ID:            id,
		TenantID:      tenantID,
		Name:          "测试设备 JSON 协议",
		Version:       "1.0.0",
		Protocol:      "json",
		Transport:     "HTTP",
		PayloadFormat: "json",
		ParserType:    "custom_json_parser",
		Status:        "PUBLISHED",
		Description:   "系统生成的测试设备协议包，用于验证报文、解析、规则和告警链路。",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

func (s *Server) ensureTestProtocolPackage(ctx context.Context, tenantID, id string, reset bool, now int64) (model.ProtocolPackage, bool, error) {
	pkg, err := s.engine.Repo.GetProtocolPackage(ctx, tenantID, id)
	if err != nil {
		pkg = testProtocolPackage(tenantID, id, now)
		return pkg, true, s.engine.Repo.SaveProtocolPackage(ctx, pkg)
	}
	if reset || pkg.Status != "PUBLISHED" || pkg.ParserType != "custom_json_parser" || pkg.PayloadFormat != "json" {
		createdAt := pkg.CreatedAt
		pkg = testProtocolPackage(tenantID, id, now)
		pkg.CreatedAt = createdAt
		if err := s.engine.Repo.SaveProtocolPackage(ctx, pkg); err != nil {
			return model.ProtocolPackage{}, false, err
		}
	}
	return pkg, false, nil
}

func testProduct(tenantID, id, protocolID string, now int64) model.Product {
	return model.Product{
		ID:                id,
		TenantID:          tenantID,
		Name:              "测试烟感设备",
		Category:          "smoke",
		ProtocolPackageID: protocolID,
		Transport:         "HTTP",
		PayloadFormat:     "json",
		Status:            "ENABLED",
		Description:       "系统生成的测试设备产品，默认支持温度、烟雾和电池电量字段。",
		Metadata: map[string]any{
			"systemFixture": true,
			"properties":    []string{"temperature", "smoke", "battery"},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func (s *Server) ensureTestProduct(ctx context.Context, tenantID, id, protocolID string, reset bool, now int64) (model.Product, bool, error) {
	product, err := s.engine.Repo.GetProduct(ctx, tenantID, id)
	if err != nil {
		product = testProduct(tenantID, id, protocolID, now)
		return product, true, s.engine.Repo.SaveProduct(ctx, product)
	}
	if reset || product.ProtocolPackageID != protocolID || product.Status != "ENABLED" {
		createdAt := product.CreatedAt
		product = testProduct(tenantID, id, protocolID, now)
		product.CreatedAt = createdAt
		if err := s.engine.Repo.SaveProduct(ctx, product); err != nil {
			return model.Product{}, false, err
		}
	}
	return product, false, nil
}

func testManagedDevice(tenantID, id, productID string, now int64) model.ManagedDevice {
	return model.ManagedDevice{
		ID:                 id,
		TenantID:           tenantID,
		ProductID:          productID,
		Name:               "测试设备 · 一号烟感",
		Status:             "ENABLED",
		DeviceRole:         "DIRECT",
		RegistrationSource: "SYSTEM_TEST_FIXTURE",
		Description:        "页面自动生成的受管测试设备，可直接发送数据、事件、报警和恢复报文。",
		Tags: map[string]string{
			"fixture":      "test-device",
			"cityCode":     "city_001",
			"districtCode": "district_01",
			"buildingId":   "A-01",
			"deviceType":   "smoke",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func (s *Server) ensureTestManagedDevice(ctx context.Context, tenantID, id, productID string, reset bool, now int64) (model.ManagedDevice, bool, model.DeviceCredential, error) {
	device, err := s.engine.Repo.GetManagedDevice(ctx, tenantID, id)
	created := false
	credential := model.DeviceCredential{}
	if err != nil {
		credential = newDeviceCredential()
		device = testManagedDevice(tenantID, id, productID, now)
		device.AccessKey = credential.AccessKey
		device.SecretHash = secretHash(credential.Secret)
		device.SecretHint = credential.Secret[len(credential.Secret)-6:]
		created = true
	} else if device.AccessKey == "" {
		credential = newDeviceCredential()
		device.AccessKey = credential.AccessKey
		device.SecretHash = secretHash(credential.Secret)
		device.SecretHint = credential.Secret[len(credential.Secret)-6:]
	}
	if created || reset || device.ProductID != productID || device.Status != "ENABLED" || device.DeviceRole != "DIRECT" {
		createdAt := device.CreatedAt
		baseline := testManagedDevice(tenantID, id, productID, now)
		baseline.CreatedAt = createdAt
		baseline.AccessKey = device.AccessKey
		baseline.SecretHash = device.SecretHash
		baseline.SecretHint = device.SecretHint
		if !reset && !created {
			baseline.Name = device.Name
			baseline.Description = device.Description
			baseline.Tags = device.Tags
		}
		device = baseline
	}
	if err := s.engine.Repo.SaveManagedDevice(ctx, device); err != nil {
		return model.ManagedDevice{}, false, model.DeviceCredential{}, err
	}
	return device, created, credential, nil
}

func (s *Server) removeLegacyTestAlarmRule(ctx context.Context, tenantID, id, productID string) (bool, error) {
	rules, err := s.engine.Repo.ListRules(ctx, tenantID)
	if err != nil {
		return false, err
	}
	for _, rule := range rules {
		if rule.ID != id || rule.ProductID != productID || rule.Name != "测试设备高温烟雾报警" || rule.AlarmType != "FIRE_RISK" || rule.Level != "HIGH" || rule.Match != "all" || !rule.Enabled || len(rule.Actions) != 0 || len(rule.Conditions) != 2 {
			continue
		}
		if !legacyTestCondition(rule.Conditions[0], "temperature", ">", "80") || !legacyTestCondition(rule.Conditions[1], "smoke", "eq", "true") || len(rule.Recovery) != 2 || !legacyTestCondition(rule.Recovery[0], "temperature", "<=", "80") || !legacyTestCondition(rule.Recovery[1], "smoke", "eq", "false") {
			continue
		}
		if err := s.engine.Repo.DeleteRule(ctx, tenantID, id); err != nil {
			return false, err
		}
		s.engine.RulesChanged(tenantID)
		return true, nil
	}
	return false, nil
}

func legacyTestCondition(condition model.RuleCondition, field, operator, value string) bool {
	return condition.Field == field && condition.Operator == operator && fmt.Sprint(condition.Value) == value
}

func testDeviceTemplates() map[string]any {
	tags := map[string]any{
		"cityCode":     "city_001",
		"districtCode": "district_01",
		"buildingId":   "A-01",
		"areaId":       "floor-01",
		"deviceType":   "smoke",
	}
	properties := map[string]any{"temperature": 26.5, "smoke": false, "battery": 96}
	return map[string]any{
		"data": map[string]any{
			"messageId": "raw_test_data_<unique>",
			"payload":   map[string]any{"properties": properties, "tags": tags},
		},
		"alarm": map[string]any{
			"messageId": "raw_test_alarm_<unique>",
			"payload": map[string]any{
				"alarm":      true,
				"properties": map[string]any{"temperature": 88.5, "smoke": true, "battery": 92},
				"tags":       tags,
			},
		},
		"recovery": map[string]any{
			"messageId": "raw_test_recovery_<unique>",
			"payload":   map[string]any{"properties": properties, "tags": tags},
		},
		"event": map[string]any{
			"messageId": "raw_test_event_<unique>",
			"payload": map[string]any{
				"event":      map[string]any{"type": "HEARTBEAT", "message": "测试事件"},
				"properties": map[string]any{"battery": 96},
				"tags":       tags,
			},
		},
	}
}
