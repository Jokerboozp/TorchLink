package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                          /* 执行当前语句并推进处理流程。 */
	"errors"                           /* 执行当前语句并推进处理流程。 */
	"io"                               /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/connector"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"      /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/onboarding" /* 执行当前语句并推进处理流程。 */
	"net/http"                         /* 执行当前语句并推进处理流程。 */
	"net/url"                          /* 执行当前语句并推进处理流程。 */
	"strings"                          /* 执行当前语句并推进处理流程。 */
	"time"                             /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func (s *Server) SetMQTTHealth(health func(context.Context) error) { s.onboarding.MQTTHealth = health } /* 定义 SetMQTTHealth 函数。 */

func (s *Server) publicAddresses() onboarding.PublicAddresses {
	return onboarding.PublicAddresses{HTTP: publicEndpoint(s.cfg.DeviceHTTPPublicURL) != "", MQTT: publicEndpoint(s.cfg.MQTTPublicURL) != ""}
}

func enrollProblem(w http.ResponseWriter, err error) {
	var e *onboarding.EnrollError
	if errors.As(err, &e) {
		problem(w, e.Status, e.Message)
		return
	}
	problem(w, 500, err.Error())
}

// onboardingPreflight evaluates a saved template, or a template draft described
// by protocolPackageId and transport, without writing anything.
func (s *Server) onboardingPreflight(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	productID := strings.TrimSpace(q.Get("productId"))
	var draft *onboarding.NewProduct
	if productID == "" {
		draft = &onboarding.NewProduct{Category: q.Get("category"), ProtocolPackageID: q.Get("protocolPackageId"), Transport: q.Get("transport")}
		if strings.TrimSpace(draft.ProtocolPackageID) == "" {
			problem(w, 422, "请选择设备模板，或为新模板选择通信协议")
			return
		}
	}
	result, err := s.onboarding.Preflight(r.Context(), claims(r).TenantID, productID, draft, s.publicAddresses())
	if err != nil {
		enrollProblem(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	write(w, 200, result)
}

// onboardingEnroll adds one device, and optionally its template and platform
// connection, in one transaction. Creating a template or a shared listener
// also requires the caller's permission for those resources.
func (s *Server) onboardingEnroll(w http.ResponseWriter, r *http.Request) {
	var q onboarding.EnrollRequest
	if decode(w, r, &q) != nil {
		return
	}
	if q.NewProduct != nil && !requestAllows(r, "POST", "/api/v1/products") {
		problem(w, 403, "当前账号不能新建设备模板，请选择已有模板")
		return
	}
	if q.Connection.Listener != nil && !requestAllows(r, "POST", "/api/v2/device-access-profiles") {
		problem(w, 403, "当前账号不能新建平台接入点，请选择已有接入点")
		return
	}
	result, err := s.onboarding.Enroll(r.Context(), claims(r).TenantID, q)
	if err != nil {
		enrollProblem(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	response := struct {
		onboarding.EnrollResult
		AccessInfo map[string]any `json:"accessInfo,omitempty"`
	}{result, s.deviceAccessInfo(result.Device, result.Product)}
	if result.Reused {
		write(w, 200, response)
		return
	}
	details := map[string]any{"productId": result.Product.ID, "mode": result.Mode, "newProduct": q.NewProduct != nil}
	if result.Profile != nil {
		details["profileId"] = result.Profile.ID
	}
	s.audit(r, "device.onboarding", "device", result.Device.ID, details)
	write(w, 201, response)
}
func (s *Server) standardDeviceIngest(w http.ResponseWriter, r *http.Request) { /* 定义 standardDeviceIngest 函数。 */
	fail := func(status int, code, message string) { /* 更新 fail 的值。 */
		write(w, status, map[string]string{"error": message, "errorCode": code}) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	d, err := s.onboarding.Authenticate(r.Context(), r.Header.Get("X-Device-Key"), r.Header.Get("X-Device-Secret"))                        /* 更新 err 的值。 */
	if err != nil || d.TenantID != r.PathValue("tenantId") || d.ProductID != r.PathValue("productId") || d.ID != r.PathValue("deviceId") { /* 判断条件并选择处理分支。 */
		fail(401, "AUTH_FAILED", "invalid or disabled device credential") /* 执行当前语句并推进处理流程。 */
		return                                                            /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10) /* 更新 r.Body 的值。 */
	data, err := io.ReadAll(r.Body)                 /* 更新 err 的值。 */
	if err != nil {                                 /* 判断条件并选择处理分支。 */
		fail(413, "BODY_TOO_LARGE", "body exceeds 64 KiB") /* 执行当前语句并推进处理流程。 */
		return                                             /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	raw, err := s.onboarding.PrepareStandard(r.Context(), d.TenantID, d.ProductID, d.ID, r.PathValue("kind"), "HTTP", data) /* 更新 err 的值。 */
	if err != nil {                                                                                                         /* 判断条件并选择处理分支。 */
		status, code := 422, "PROTOCOL_ERROR"   /* 更新 code 的值。 */
		if errors.Is(err, onboarding.ErrRate) { /* 判断条件并选择处理分支。 */
			status, code = 429, "RATE_LIMITED" /* 更新 code 的值。 */
			w.Header().Set("Retry-After", "1") /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if errors.Is(err, onboarding.ErrAuth) { /* 判断条件并选择处理分支。 */
			status, code = 401, "AUTH_FAILED" /* 更新 code 的值。 */
		} /* 结束当前表达式或代码块。 */
		fail(status, code, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                          /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	raw.RemoteAddress = r.RemoteAddr                          /* 更新 raw.RemoteAddress 的值。 */
	idx, created, err := s.engine.IngestRaw(r.Context(), raw) /* 更新 err 的值。 */
	if err != nil {                                           /* 判断条件并选择处理分支。 */
		if errors.Is(err, model.ErrRawConflict) { /* 判断条件并选择处理分支。 */
			fail(409, "MESSAGE_CONFLICT", err.Error()) /* 执行当前语句并推进处理流程。 */
			return                                     /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		fail(503, "INGEST_FAILED", err.Error()) /* 执行当前语句并推进处理流程。 */
		return                                  /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	write(w, 202, map[string]any{"messageId": idx.MessageID, "created": created, "status": "ACCEPTED"}) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) disableDeviceCredential(w http.ResponseWriter, r *http.Request) { /* 定义 disableDeviceCredential 函数。 */
	if !s.operationDevice(w, r) { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	_, v, e := s.onboarding.ChangeCredential(r.Context(), claims(r).TenantID, r.PathValue("id"), false) /* 更新 e 的值。 */
	if e != nil {                                                                                       /* 判断条件并选择处理分支。 */
		status := http.StatusInternalServerError               /* 更新 status 的值。 */
		if errors.Is(e, onboarding.ErrCredentialUnsupported) { /* 判断条件并选择处理分支。 */
			status = http.StatusUnprocessableEntity /* 更新 status 的值。 */
		} /* 结束当前表达式或代码块。 */
		problem(w, status, e.Error()) /* 执行当前语句并推进处理流程。 */
		return                        /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	s.audit(r, "device.credential.disable", "device", r.PathValue("id"), nil) /* 执行当前语句并推进处理流程。 */
	write(w, 200, map[string]any{"disabled": true, "revocation": v})          /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) connectorTypes(w http.ResponseWriter, r *http.Request) { /* 定义 connectorTypes 函数。 */
	write(w, 200, map[string]any{"items": connector.Types()}) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func publicEndpoint(value string) string { /* 定义 publicEndpoint 函数。 */
	u, err := url.Parse(strings.TrimSpace(value))                                            /* 更新 err 的值。 */
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" { /* 判断条件并选择处理分支。 */
		return "" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	switch u.Scheme { /* 根据条件选择处理路径。 */
	case "http", "https", "mqtt", "mqtts", "tcp", "ssl", "ws", "wss": /* 处理当前分支。 */
		return strings.TrimRight(u.String(), "/") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return "" /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
// deviceAccessInfo returns what a credential device needs to report data. It
// never includes the device secret.
func (s *Server) deviceAccessInfo(d model.ManagedDevice, product model.Product) map[string]any {
	if !d.UsesPlatformCredentials(product) {
		return nil
	}
	endpoint := publicEndpoint(s.cfg.DeviceHTTPPublicURL)
	if d.Tags["connector"] != "HTTP" && d.Tags["connector"] != "MQTT" {
		// Managed devices post raw payloads that the template protocol parses.
		httpURL := ""
		if endpoint != "" {
			httpURL = endpoint + "/api/v1/device-ingest/" + url.PathEscape(d.ID)
		}
		return map[string]any{"kind": "managed", "httpUrl": httpURL, "username": d.AccessKey, "sample": map[string]any{"payload": map[string]any{"temperature": 26.5}}}
	}
	identity := d.TenantID + "/" + d.ProductID + "/" + d.ID
	httpURL := ""
	if endpoint != "" {
		httpURL = endpoint + "/api/v1/device-ingest/standard/" + identity + "/property"
	}
	return map[string]any{
		"kind": "standard", "httpUrl": httpURL,
		"mqttBroker": publicEndpoint(s.cfg.MQTTPublicURL), "mqttWebSocket": publicEndpoint(s.cfg.MQTTWebSocketURL),
		"clientId": "device-" + d.AccessKey, "username": d.AccessKey, "tokenEndpoint": "/api/v1/device-mqtt/token",
		"upTopic": "/iot/up/" + identity + "/property", "downTopic": "/iot/down/" + identity + "/command",
		"sample": map[string]any{"version": "1.0", "id": "replace-with-unique-message-id", "timestamp": time.Now().UnixMilli(), "data": map[string]any{"temperature": 26.5}},
	}
}
