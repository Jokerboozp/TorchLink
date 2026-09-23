package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"       /* 执行当前语句并推进处理流程。 */
	"crypto/sha256" /* 执行当前语句并推进处理流程。 */
	"encoding/hex"  /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */
	"net/http"      /* 执行当前语句并推进处理流程。 */
	"time"          /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

// provisionTestDevice creates a tenant-scoped, repeatable fixture for the web
// test console. It deliberately uses the normal protocol, product and device
// repositories so data sent from the console follows the same pipeline as a
// managed device report. Alarm rules remain tenant-owned configuration and are
// never created implicitly by this endpoint.
func (s *Server) provisionTestDevice(w http.ResponseWriter, r *http.Request) { /* 定义 provisionTestDevice 函数。 */
	var in struct { /* 声明 in。 */
		Reset bool `json:"reset"` /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if decode(w, r, &in) != nil { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */

	tenantID := claims(r).TenantID                /* 更新 tenantID 的值。 */
	scope := testDeviceScope(tenantID)            /* 更新 scope 的值。 */
	now := time.Now().UnixMilli()                 /* 更新 now 的值。 */
	protocolID := "protocol_test_device_" + scope /* 更新 protocolID 的值。 */
	productID := "product_test_device_" + scope   /* 更新 productID 的值。 */
	deviceID := "device_test_device_" + scope     /* 更新 deviceID 的值。 */

	pkg, packageCreated, err := s.ensureTestProtocolPackage(r.Context(), tenantID, protocolID, in.Reset, now) /* 更新 err 的值。 */
	if err != nil {                                                                                           /* 判断条件并选择处理分支。 */
		problem(w, http.StatusInternalServerError, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                                                  /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	product, productCreated, err := s.ensureTestProduct(r.Context(), tenantID, productID, pkg.ID, in.Reset, now) /* 更新 err 的值。 */
	if err != nil {                                                                                              /* 判断条件并选择处理分支。 */
		problem(w, http.StatusInternalServerError, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                                                  /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if _, err := s.removeLegacyTestAlarmRule(r.Context(), tenantID, "rule_test_device_"+scope, product.ID); err != nil { /* 判断条件并选择处理分支。 */
		problem(w, http.StatusInternalServerError, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                                                  /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	device, deviceCreated, credential, err := s.ensureTestManagedDevice(r.Context(), tenantID, deviceID, product.ID, in.Reset, now) /* 更新 err 的值。 */
	if err != nil {                                                                                                                 /* 判断条件并选择处理分支。 */
		problem(w, http.StatusInternalServerError, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                                                  /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	s.audit(r, "test-device.provision", "device", device.ID, map[string]any{ /* 执行当前语句并推进处理流程。 */
		"protocolPackageId": pkg.ID,     /* 执行当前语句并推进处理流程。 */
		"productId":         product.ID, /* 执行当前语句并推进处理流程。 */
		"reset":             in.Reset,   /* 执行当前语句并推进处理流程。 */
	}) /* 结束当前表达式或代码块。 */
	result := map[string]any{ /* 更新 result 的值。 */
		"device":          device,                /* 执行当前语句并推进处理流程。 */
		"product":         product,               /* 执行当前语句并推进处理流程。 */
		"protocolPackage": pkg,                   /* 执行当前语句并推进处理流程。 */
		"rule":            nil,                   /* 执行当前语句并推进处理流程。 */
		"templates":       testDeviceTemplates(), /* 执行当前语句并推进处理流程。 */
		"created": map[string]bool{ /* 执行当前语句并推进处理流程。 */
			"protocolPackage": packageCreated, /* 执行当前语句并推进处理流程。 */
			"product":         productCreated, /* 执行当前语句并推进处理流程。 */
			"device":          deviceCreated,  /* 执行当前语句并推进处理流程。 */
		}, /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if credential.Secret != "" { /* 判断条件并选择处理分支。 */
		// Match the normal device-registration contract: the secret is returned
		// only at initial creation or when a fixture had no credential.
		result["credential"] = credential /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	status := http.StatusOK                                /* 更新 status 的值。 */
	if packageCreated || productCreated || deviceCreated { /* 判断条件并选择处理分支。 */
		status = http.StatusCreated /* 更新 status 的值。 */
	} /* 结束当前表达式或代码块。 */
	write(w, status, result) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func testDeviceScope(tenantID string) string { /* 定义 testDeviceScope 函数。 */
	sum := sha256.Sum256([]byte(tenantID)) /* 更新 sum 的值。 */
	return hex.EncodeToString(sum[:4])     /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func testProtocolPackage(tenantID, id string, now int64) model.ProtocolPackage { /* 定义 testProtocolPackage 函数。 */
	return model.ProtocolPackage{ /* 返回当前处理结果。 */
		ID:            id,                                /* 执行当前语句并推进处理流程。 */
		TenantID:      tenantID,                          /* 执行当前语句并推进处理流程。 */
		Name:          "测试设备 JSON 协议",                    /* 执行当前语句并推进处理流程。 */
		Version:       "1.0.0",                           /* 执行当前语句并推进处理流程。 */
		Protocol:      "json",                            /* 执行当前语句并推进处理流程。 */
		Transport:     "HTTP",                            /* 执行当前语句并推进处理流程。 */
		PayloadFormat: "json",                            /* 执行当前语句并推进处理流程。 */
		ParserType:    "custom_json_parser",              /* 执行当前语句并推进处理流程。 */
		Status:        "PUBLISHED",                       /* 执行当前语句并推进处理流程。 */
		Description:   "系统生成的测试设备协议包，用于验证报文、解析、规则和告警链路。", /* 执行当前语句并推进处理流程。 */
		CreatedAt:     now,                               /* 执行当前语句并推进处理流程。 */
		UpdatedAt:     now,                               /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) ensureTestProtocolPackage(ctx context.Context, tenantID, id string, reset bool, now int64) (model.ProtocolPackage, bool, error) { /* 定义 ensureTestProtocolPackage 函数。 */
	pkg, err := s.engine.Repo.GetProtocolPackage(ctx, tenantID, id) /* 更新 err 的值。 */
	if err != nil {                                                 /* 判断条件并选择处理分支。 */
		pkg = testProtocolPackage(tenantID, id, now)                  /* 更新 pkg 的值。 */
		return pkg, true, s.engine.Repo.SaveProtocolPackage(ctx, pkg) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if reset || pkg.Status != "PUBLISHED" || pkg.ParserType != "custom_json_parser" || pkg.PayloadFormat != "json" { /* 判断条件并选择处理分支。 */
		createdAt := pkg.CreatedAt                                          /* 更新 createdAt 的值。 */
		pkg = testProtocolPackage(tenantID, id, now)                        /* 更新 pkg 的值。 */
		pkg.CreatedAt = createdAt                                           /* 更新 pkg.CreatedAt 的值。 */
		if err := s.engine.Repo.SaveProtocolPackage(ctx, pkg); err != nil { /* 判断条件并选择处理分支。 */
			return model.ProtocolPackage{}, false, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return pkg, false, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func testProduct(tenantID, id, protocolID string, now int64) model.Product { /* 定义 testProduct 函数。 */
	return model.Product{ /* 返回当前处理结果。 */
		ID:                id,                              /* 执行当前语句并推进处理流程。 */
		TenantID:          tenantID,                        /* 执行当前语句并推进处理流程。 */
		Name:              "测试烟感设备",                        /* 执行当前语句并推进处理流程。 */
		Category:          "smoke",                         /* 执行当前语句并推进处理流程。 */
		ProtocolPackageID: protocolID,                      /* 执行当前语句并推进处理流程。 */
		Transport:         "HTTP",                          /* 执行当前语句并推进处理流程。 */
		PayloadFormat:     "json",                          /* 执行当前语句并推进处理流程。 */
		Status:            "ENABLED",                       /* 执行当前语句并推进处理流程。 */
		Description:       "系统生成的测试设备产品，默认支持温度、烟雾和电池电量字段。", /* 执行当前语句并推进处理流程。 */
		Metadata: map[string]any{ /* 执行当前语句并推进处理流程。 */
			"systemFixture": true,                                        /* 执行当前语句并推进处理流程。 */
			"properties":    []string{"temperature", "smoke", "battery"}, /* 执行当前语句并推进处理流程。 */
		}, /* 结束当前表达式或代码块。 */
		CreatedAt: now, /* 执行当前语句并推进处理流程。 */
		UpdatedAt: now, /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) ensureTestProduct(ctx context.Context, tenantID, id, protocolID string, reset bool, now int64) (model.Product, bool, error) { /* 定义 ensureTestProduct 函数。 */
	product, err := s.engine.Repo.GetProduct(ctx, tenantID, id) /* 更新 err 的值。 */
	if err != nil {                                             /* 判断条件并选择处理分支。 */
		product = testProduct(tenantID, id, protocolID, now)          /* 更新 product 的值。 */
		return product, true, s.engine.Repo.SaveProduct(ctx, product) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if reset || product.ProtocolPackageID != protocolID || product.Status != "ENABLED" { /* 判断条件并选择处理分支。 */
		createdAt := product.CreatedAt                                  /* 更新 createdAt 的值。 */
		product = testProduct(tenantID, id, protocolID, now)            /* 更新 product 的值。 */
		product.CreatedAt = createdAt                                   /* 更新 product.CreatedAt 的值。 */
		if err := s.engine.Repo.SaveProduct(ctx, product); err != nil { /* 判断条件并选择处理分支。 */
			return model.Product{}, false, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return product, false, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func testManagedDevice(tenantID, id, productID string, now int64) model.ManagedDevice { /* 定义 testManagedDevice 函数。 */
	return model.ManagedDevice{ /* 返回当前处理结果。 */
		ID:                 id,                                  /* 执行当前语句并推进处理流程。 */
		TenantID:           tenantID,                            /* 执行当前语句并推进处理流程。 */
		ProductID:          productID,                           /* 执行当前语句并推进处理流程。 */
		Name:               "测试设备 · 一号烟感",                       /* 执行当前语句并推进处理流程。 */
		Status:             "ENABLED",                           /* 执行当前语句并推进处理流程。 */
		DeviceRole:         "DIRECT",                            /* 执行当前语句并推进处理流程。 */
		RegistrationSource: "SYSTEM_TEST_FIXTURE",               /* 执行当前语句并推进处理流程。 */
		Description:        "页面自动生成的受管测试设备，可直接发送数据、事件、报警和恢复报文。", /* 执行当前语句并推进处理流程。 */
		Tags: map[string]string{ /* 执行当前语句并推进处理流程。 */
			"fixture":      "test-device", /* 执行当前语句并推进处理流程。 */
			"cityCode":     "city_001",    /* 执行当前语句并推进处理流程。 */
			"districtCode": "district_01", /* 执行当前语句并推进处理流程。 */
			"buildingId":   "A-01",        /* 执行当前语句并推进处理流程。 */
			"deviceType":   "smoke",       /* 执行当前语句并推进处理流程。 */
		}, /* 结束当前表达式或代码块。 */
		CreatedAt: now, /* 执行当前语句并推进处理流程。 */
		UpdatedAt: now, /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) ensureTestManagedDevice(ctx context.Context, tenantID, id, productID string, reset bool, now int64) (model.ManagedDevice, bool, model.DeviceCredential, error) { /* 定义 ensureTestManagedDevice 函数。 */
	device, err := s.engine.Repo.GetManagedDevice(ctx, tenantID, id) /* 更新 err 的值。 */
	created := false                                                 /* 更新 created 的值。 */
	credential := model.DeviceCredential{}                           /* 更新 credential 的值。 */
	if err != nil {                                                  /* 判断条件并选择处理分支。 */
		credential = newDeviceCredential()                               /* 更新 credential 的值。 */
		device = testManagedDevice(tenantID, id, productID, now)         /* 更新 device 的值。 */
		device.AccessKey = credential.AccessKey                          /* 更新 device.AccessKey 的值。 */
		device.SecretHash = secretHash(credential.Secret)                /* 更新 device.SecretHash 的值。 */
		device.SecretHint = credential.Secret[len(credential.Secret)-6:] /* 更新 device.SecretHint 的值。 */
		created = true                                                   /* 更新 created 的值。 */
	} else if device.AccessKey == "" { /* 结束当前表达式或代码块。 */
		credential = newDeviceCredential()                               /* 更新 credential 的值。 */
		device.AccessKey = credential.AccessKey                          /* 更新 device.AccessKey 的值。 */
		device.SecretHash = secretHash(credential.Secret)                /* 更新 device.SecretHash 的值。 */
		device.SecretHint = credential.Secret[len(credential.Secret)-6:] /* 更新 device.SecretHint 的值。 */
	} /* 结束当前表达式或代码块。 */
	if created || reset || device.ProductID != productID || device.Status != "ENABLED" || device.DeviceRole != "DIRECT" { /* 判断条件并选择处理分支。 */
		createdAt := device.CreatedAt                               /* 更新 createdAt 的值。 */
		baseline := testManagedDevice(tenantID, id, productID, now) /* 更新 baseline 的值。 */
		baseline.CreatedAt = createdAt                              /* 更新 baseline.CreatedAt 的值。 */
		baseline.AccessKey = device.AccessKey                       /* 更新 baseline.AccessKey 的值。 */
		baseline.SecretHash = device.SecretHash                     /* 更新 baseline.SecretHash 的值。 */
		baseline.SecretHint = device.SecretHint                     /* 更新 baseline.SecretHint 的值。 */
		if !reset && !created {                                     /* 判断条件并选择处理分支。 */
			baseline.Name = device.Name               /* 更新 baseline.Name 的值。 */
			baseline.Description = device.Description /* 更新 baseline.Description 的值。 */
			baseline.Tags = device.Tags               /* 更新 baseline.Tags 的值。 */
		} /* 结束当前表达式或代码块。 */
		device = baseline /* 更新 device 的值。 */
	} /* 结束当前表达式或代码块。 */
	if err := s.engine.Repo.SaveManagedDevice(ctx, device); err != nil { /* 判断条件并选择处理分支。 */
		return model.ManagedDevice{}, false, model.DeviceCredential{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return device, created, credential, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) removeLegacyTestAlarmRule(ctx context.Context, tenantID, id, productID string) (bool, error) { /* 定义 removeLegacyTestAlarmRule 函数。 */
	rules, err := s.engine.Repo.ListRules(ctx, tenantID) /* 更新 err 的值。 */
	if err != nil {                                      /* 判断条件并选择处理分支。 */
		return false, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for _, rule := range rules { /* 循环处理当前数据。 */
		if rule.ID != id || rule.ProductID != productID || rule.Name != "测试设备高温烟雾报警" || rule.AlarmType != "FIRE_RISK" || rule.Level != "HIGH" || rule.Match != "all" || !rule.Enabled || len(rule.Actions) != 0 || len(rule.Conditions) != 2 { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if !legacyTestCondition(rule.Conditions[0], "temperature", ">", "80") || !legacyTestCondition(rule.Conditions[1], "smoke", "eq", "true") || len(rule.Recovery) != 2 || !legacyTestCondition(rule.Recovery[0], "temperature", "<=", "80") || !legacyTestCondition(rule.Recovery[1], "smoke", "eq", "false") { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if err := s.engine.Repo.DeleteRule(ctx, tenantID, id); err != nil { /* 判断条件并选择处理分支。 */
			return false, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return true, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return false, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func legacyTestCondition(condition model.RuleCondition, field, operator, value string) bool { /* 定义 legacyTestCondition 函数。 */
	return condition.Field == field && condition.Operator == operator && fmt.Sprint(condition.Value) == value /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func testDeviceTemplates() map[string]any { /* 定义 testDeviceTemplates 函数。 */
	tags := map[string]any{ /* 更新 tags 的值。 */
		"cityCode":     "city_001",    /* 执行当前语句并推进处理流程。 */
		"districtCode": "district_01", /* 执行当前语句并推进处理流程。 */
		"buildingId":   "A-01",        /* 执行当前语句并推进处理流程。 */
		"areaId":       "floor-01",    /* 执行当前语句并推进处理流程。 */
		"deviceType":   "smoke",       /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	properties := map[string]any{"temperature": 26.5, "smoke": false, "battery": 96} /* 更新 properties 的值。 */
	return map[string]any{                                                           /* 返回当前处理结果。 */
		"data": map[string]any{ /* 执行当前语句并推进处理流程。 */
			"messageId": "raw_test_data_<unique>",                               /* 执行当前语句并推进处理流程。 */
			"payload":   map[string]any{"properties": properties, "tags": tags}, /* 执行当前语句并推进处理流程。 */
		}, /* 结束当前表达式或代码块。 */
		"alarm": map[string]any{ /* 执行当前语句并推进处理流程。 */
			"messageId": "raw_test_alarm_<unique>", /* 执行当前语句并推进处理流程。 */
			"payload": map[string]any{ /* 执行当前语句并推进处理流程。 */
				"alarm":      true,                                                              /* 执行当前语句并推进处理流程。 */
				"properties": map[string]any{"temperature": 88.5, "smoke": true, "battery": 92}, /* 执行当前语句并推进处理流程。 */
				"tags":       tags,                                                              /* 执行当前语句并推进处理流程。 */
			}, /* 结束当前表达式或代码块。 */
		}, /* 结束当前表达式或代码块。 */
		"recovery": map[string]any{ /* 执行当前语句并推进处理流程。 */
			"messageId": "raw_test_recovery_<unique>",                           /* 执行当前语句并推进处理流程。 */
			"payload":   map[string]any{"properties": properties, "tags": tags}, /* 执行当前语句并推进处理流程。 */
		}, /* 结束当前表达式或代码块。 */
		"event": map[string]any{ /* 执行当前语句并推进处理流程。 */
			"messageId": "raw_test_event_<unique>", /* 执行当前语句并推进处理流程。 */
			"payload": map[string]any{ /* 执行当前语句并推进处理流程。 */
				"event":      map[string]any{"type": "HEARTBEAT", "message": "测试事件"}, /* 执行当前语句并推进处理流程。 */
				"properties": map[string]any{"battery": 96},                          /* 执行当前语句并推进处理流程。 */
				"tags":       tags,                                                   /* 执行当前语句并推进处理流程。 */
			}, /* 结束当前表达式或代码块。 */
		}, /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
