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

func (s *Server) onboardingTest(w http.ResponseWriter, r *http.Request) { /* 定义 onboardingTest 函数。 */
	var q onboarding.Request     /* 声明 q。 */
	if decode(w, r, &q) != nil { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	result, err := s.onboarding.Test(r.Context(), claims(r).TenantID, q) /* 更新 err 的值。 */
	if err != nil {                                                      /* 判断条件并选择处理分支。 */
		write(w, 422, result) /* 执行当前语句并推进处理流程。 */
		return                /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	write(w, 200, result) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) onboardingCreate(w http.ResponseWriter, r *http.Request) { /* 定义 onboardingCreate 函数。 */
	var q onboarding.Request     /* 声明 q。 */
	if decode(w, r, &q) != nil { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	result, err := s.onboarding.Create(r.Context(), claims(r).TenantID, q) /* 更新 err 的值。 */
	if err != nil {                                                        /* 判断条件并选择处理分支。 */
		problem(w, 409, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	result.AccessInfo = s.deviceAccessInfo(result.Device) /* 更新 result.AccessInfo 的值。 */
	w.Header().Set("Cache-Control", "no-store")           /* 执行当前语句并推进处理流程。 */
	if result.Reused {                                    /* 判断条件并选择处理分支。 */
		write(w, 200, result) /* 执行当前语句并推进处理流程。 */
		return                /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	s.audit(r, "device.onboarding", "device", result.Device.ID, map[string]any{"connector": q.Type}) /* 执行当前语句并推进处理流程。 */
	write(w, 201, result)                                                                            /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
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
func (s *Server) deviceAccessInfo(d model.ManagedDevice) map[string]any { /* 定义 deviceAccessInfo 函数。 */
	if !d.UsesPlatformCredentials(model.Product{}) || (d.Tags["connector"] != "HTTP" && d.Tags["connector"] != "MQTT") { /* 判断条件并选择处理分支。 */
		return nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	identity := d.TenantID + "/" + d.ProductID + "/" + d.ID /* 更新 identity 的值。 */
	return map[string]any{                                  /* 返回当前处理结果。 */
		"httpUrl":    publicEndpoint(s.cfg.DeviceHTTPPublicURL) + "/api/v1/device-ingest/standard/" + identity + "/property", /* 执行当前语句并推进处理流程。 */
		"mqttBroker": publicEndpoint(s.cfg.MQTTPublicURL), "mqttWebSocket": publicEndpoint(s.cfg.MQTTWebSocketURL),           /* 执行当前语句并推进处理流程。 */
		"clientId": "device-" + d.AccessKey, "username": d.AccessKey, "tokenEndpoint": "/api/v1/device-mqtt/token", /* 执行当前语句并推进处理流程。 */
		"upTopic": "/iot/up/" + identity + "/property", "downTopic": "/iot/down/" + identity + "/command", /* 执行当前语句并推进处理流程。 */
		"sample": map[string]any{"version": "1.0", "id": "replace-with-unique-message-id", "timestamp": time.Now().UnixMilli(), "data": map[string]any{"temperature": 26.5}}, /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
