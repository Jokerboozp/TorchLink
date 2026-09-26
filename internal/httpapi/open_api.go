package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"iot-platform/internal/auth"
	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"

	"github.com/gin-gonic/gin"
)

// The open API lets external systems read alarms and device data, report
// device messages and alarms, and ask the AI assistant. Every call is made as
// the API key's bound platform user, so that user's permissions and device
// scope apply exactly as they do in the console; the key's capabilities can
// only narrow them further.

const (
	apiKeyScheme          = "tlk"
	maxAPIKeysPerTenant   = 100
	maxOpenBatchMessages  = 100
	openAPIBodyLimit      = 1 << 20
	openAPIMessageIDLimit = 128
	openAPIKeyRatePerSec  = 100
)

var apiCapabilityNames = map[string]string{
	model.APICapabilityAlarmsRead:     "查询告警",
	model.APICapabilityAlarmsReport:   "上报告警",
	model.APICapabilityAlarmsHandle:   "处置告警",
	model.APICapabilityMessagesRead:   "查询设备与数据",
	model.APICapabilityMessagesReport: "上报设备消息",
	model.APICapabilityAIChat:         "智能问答",
}

// Kinds an external system may report. Command replies stay with the device
// that received the command.
var openMessageKinds = map[string]bool{"property": true, "event": true, "alarm": true, "state": true}

type apiKeyContextKey struct{}

func (s *Server) openAPIRoutes() {
	s.router.GET("/api/v1/access/api-keys", s.authorize("admin"), s.endpoint(s.listAPIKeys))
	s.router.POST("/api/v1/access/api-keys", s.authorize("admin"), s.endpoint(s.createAPIKey))
	s.router.PUT("/api/v1/access/api-keys/:id", s.authorize("admin"), s.endpoint(s.updateAPIKey, "id"))
	s.router.DELETE("/api/v1/access/api-keys/:id", s.authorize("admin"), s.endpoint(s.deleteAPIKey, "id"))

	// Each open route names the console route whose permission the bound user
	// must hold; an empty route means the handler checks device access itself.
	open := func(method, path, capability, consoleMethod, consolePath string, handler endpointHandler, params ...string) {
		s.router.Handle(method, "/api/open/v1"+path, s.authorizeAPIKey(capability, consoleMethod, consolePath), s.endpoint(handler, params...))
	}
	open("GET", "/me", "", "", "", s.openIdentity)
	open("GET", "/alarms", model.APICapabilityAlarmsRead, "GET", "/api/v1/alarms", s.alarms)
	open("GET", "/alarms/:id", model.APICapabilityAlarmsRead, "GET", "/api/v1/alarms/:id", s.alarm, "id")
	open("POST", "/alarms", model.APICapabilityAlarmsReport, "", "", s.openReportAlarm)
	open("POST", "/alarms/:id/actions", model.APICapabilityAlarmsHandle, "POST", "/api/v1/alarms/:id/actions", s.openAlarmAction, "id")
	open("GET", "/devices", model.APICapabilityMessagesRead, "GET", "/api/v1/device-registry", s.deviceRegistry)
	open("GET", "/devices/:deviceId/latest", model.APICapabilityMessagesRead, "GET", "/api/v1/devices/:deviceId/latest", s.deviceLatest, "deviceId")
	open("GET", "/devices/:deviceId/properties/history", model.APICapabilityMessagesRead, "GET", "/api/v1/devices/:deviceId/properties/history", s.history, "deviceId")
	open("POST", "/device-messages", model.APICapabilityMessagesReport, "", "", s.openReportMessages)
	open("GET", "/ai/workflows", model.APICapabilityAIChat, "GET", "/api/v1/ai/workflows", s.aiWorkflows)
	open("POST", "/ai/chat", model.APICapabilityAIChat, "POST", "/api/v1/ai/chat", s.aiChat)
	open("POST", "/ai/chat/stream", model.APICapabilityAIChat, "POST", "/api/v1/ai/chat/stream", s.aiChatStream)
}

// formatAPIKey embeds the tenant so a key alone identifies where it is stored.
func formatAPIKey(tenantID, keyID, secret string) string {
	return apiKeyScheme + "." + base64.RawURLEncoding.EncodeToString([]byte(tenantID)) + "." + keyID + "." + secret
}

func parseAPIKey(value string) (tenantID, keyID, secret string, ok bool) {
	parts := strings.Split(strings.TrimSpace(value), ".")
	if len(parts) != 4 || parts[0] != apiKeyScheme || parts[2] == "" || parts[3] == "" || len(value) > 512 {
		return "", "", "", false
	}
	tenant, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(tenant) == 0 || !utf8.Valid(tenant) {
		return "", "", "", false
	}
	return string(tenant), parts[2], parts[3], true
}

func apiKeySecretHash(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func requestAPIKey(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get("X-API-Key")); value != "" {
		return value
	}
	return auth.Bearer(r.Header.Get("Authorization"))
}

func requestAPIKeyRecord(ctx context.Context) (model.APIKey, bool) {
	key, ok := ctx.Value(apiKeyContextKey{}).(model.APIKey)
	return key, ok
}

func (s *Server) authorizeAPIKey(capability, consoleMethod, consolePath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		fail := func(status int, detail string) {
			ginProblem(c, status, detail)
			c.Abort()
		}
		tenantID, keyID, secret, ok := parseAPIKey(requestAPIKey(c.Request))
		if !ok {
			fail(http.StatusUnauthorized, "missing or malformed API key")
			return
		}
		if !s.onboarding.AllowRate("openapi\x00"+tenantID+"\x00"+keyID, openAPIKeyRatePerSec) {
			c.Header("Retry-After", "1")
			fail(http.StatusTooManyRequests, "API key rate limit exceeded")
			return
		}
		store, err := s.accessStore()
		if err != nil {
			fail(http.StatusServiceUnavailable, "access storage unavailable")
			return
		}
		state, err := store.LoadAccessState(c.Request.Context(), tenantID)
		if err != nil {
			fail(http.StatusServiceUnavailable, "access storage unavailable")
			return
		}
		index := slices.IndexFunc(state.APIKeys, func(k model.APIKey) bool { return k.ID == keyID })
		if index < 0 || subtle.ConstantTimeCompare([]byte(state.APIKeys[index].SecretHash), []byte(apiKeySecretHash(secret))) != 1 {
			fail(http.StatusUnauthorized, "invalid API key")
			return
		}
		key := state.APIKeys[index]
		key.SecretHash = ""
		if !key.Enabled || key.ExpiresAt > 0 && time.Now().UnixMilli() >= key.ExpiresAt {
			fail(http.StatusUnauthorized, "API key is disabled or expired")
			return
		}
		if capability != "" && !slices.Contains(key.Capabilities, capability) {
			fail(http.StatusForbidden, "API key is not granted "+capability)
			return
		}
		userIndex := slices.IndexFunc(state.Users, func(u model.PlatformUser) bool { return u.Username == key.Username && u.Enabled })
		if userIndex < 0 {
			fail(http.StatusUnauthorized, "the API key's user is disabled or removed")
			return
		}
		user := state.Users[userIndex]
		permissions := effectivePermissions(state, user)
		s.stripOpsPermissions(tenantID, permissions)
		user = resolveUserDeviceScope(state, user)
		scope := scopeFor(user, permissions, tenantID)
		ctx := context.WithValue(c.Request.Context(), deviceScopeKey{}, scope)
		ctx = context.WithValue(ctx, permissionsKey{}, permissions)
		claimsValue := auth.Claims{Username: user.Username, TenantID: tenantID, Role: "operator", TokenUse: "user", SessionVersion: user.SessionVersion}
		ctx = auth.ContextWithClaims(context.WithValue(ctx, claimsKey, claimsValue), claimsValue)
		c.Request = c.Request.WithContext(context.WithValue(ctx, apiKeyContextKey{}, key))
		allowed := capability == "" || permissions["menu:devices"]
		if consolePath != "" {
			allowed = allowsRoute(permissions, consoleMethod, consolePath)
		}
		if allowed {
			allowed = s.allowScopedRequest(c, scope)
		}
		if allowed && strings.Contains(c.FullPath(), "/alarms/:id") {
			_, err = s.engine.Repo.GetAlarm(c.Request.Context(), tenantID, c.Param("id"))
			allowed = err == nil
		}
		if !allowed {
			fail(http.StatusForbidden, "the API key's user lacks this permission or device access")
			return
		}
		c.Next()
	}
}

func (s *Server) openIdentity(w http.ResponseWriter, r *http.Request) {
	key, _ := requestAPIKeyRecord(r.Context())
	scope, _ := requestScope(r.Context())
	deviceScope := "none"
	if scope.All {
		deviceScope = "all"
	} else if len(scope.IDs) > 0 {
		deviceScope = "selected"
	}
	write(w, 200, map[string]any{"tenantId": claims(r).TenantID, "keyId": key.ID, "name": key.Name, "username": key.Username, "capabilities": key.Capabilities, "deviceScope": deviceScope, "deviceCount": len(scope.IDs), "expiresAt": key.ExpiresAt})
}

type openDeviceMessage struct {
	DeviceID  string         `json:"deviceId"`
	Kind      string         `json:"kind"`
	ID        string         `json:"id"`
	Timestamp int64          `json:"timestamp"`
	Event     string         `json:"event,omitempty"`
	Online    *bool          `json:"online,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
}

type openIngestResult struct {
	Index     int    `json:"index"`
	DeviceID  string `json:"deviceId"`
	MessageID string `json:"messageId,omitempty"`
	Created   bool   `json:"created,omitempty"`
	Status    string `json:"status"`
	ErrorCode string `json:"errorCode,omitempty"`
	Error     string `json:"error,omitempty"`
}

var openIngestStatus = map[string]int{"DEVICE_NOT_FOUND": 404, "DEVICE_DISABLED": 422, "INVALID_MESSAGE": 422, "RATE_LIMITED": 429, "MESSAGE_CONFLICT": 409, "INGEST_FAILED": 503}

// ingestOpenMessage sends one external message through the standard protocol,
// so it is archived, parsed and evaluated by rules like any device report.
func (s *Server) ingestOpenMessage(r *http.Request, index int, in openDeviceMessage) openIngestResult {
	result := openIngestResult{Index: index, DeviceID: in.DeviceID, Status: "REJECTED"}
	reject := func(code, message string) openIngestResult {
		result.ErrorCode, result.Error = code, message
		return result
	}
	ctx, tenantID := r.Context(), claims(r).TenantID
	if !openMessageKinds[in.Kind] {
		return reject("INVALID_MESSAGE", "kind must be property, event, alarm or state")
	}
	if in.ID == "" || len(in.ID) > openAPIMessageIDLimit || in.Timestamp <= 0 {
		return reject("INVALID_MESSAGE", "id (at most 128 bytes) and positive millisecond timestamp are required")
	}
	// The scoped repository hides devices outside the bound user's scope.
	device, err := s.engine.Repo.GetManagedDevice(ctx, tenantID, strings.TrimSpace(in.DeviceID))
	if err != nil {
		return reject("DEVICE_NOT_FOUND", "设备不存在或无访问权限")
	}
	if device.Status != "ENABLED" {
		return reject("DEVICE_DISABLED", "设备已停用")
	}
	if product, err := s.engine.Repo.GetProduct(ctx, tenantID, device.ProductID); err != nil || product.Status != "ENABLED" {
		return reject("DEVICE_DISABLED", "设备模板未启用")
	}
	if !s.onboarding.Allow(tenantID + "\x00" + device.ID) {
		return reject("RATE_LIMITED", "device report rate limit exceeded")
	}
	payload, err := json.Marshal(struct {
		ID        string         `json:"id"`
		Timestamp int64          `json:"timestamp"`
		Event     string         `json:"event,omitempty"`
		Online    *bool          `json:"online,omitempty"`
		Data      map[string]any `json:"data,omitempty"`
	}{in.ID, in.Timestamp, in.Event, in.Online, in.Data})
	if err != nil {
		return reject("INVALID_MESSAGE", err.Error())
	}
	raw, err := onboarding.StandardRaw(tenantID, device.ProductID, device.ID, in.Kind, "HTTP", payload)
	if err != nil {
		return reject("INVALID_MESSAGE", err.Error())
	}
	raw.Source, raw.RemoteAddress = "open-api", r.RemoteAddr
	if key, ok := requestAPIKeyRecord(ctx); ok {
		raw.Metadata["apiKeyId"] = key.ID
	}
	idx, created, err := s.engine.IngestRaw(ctx, raw)
	if errors.Is(err, model.ErrRawConflict) {
		return reject("MESSAGE_CONFLICT", err.Error())
	}
	if err != nil {
		return reject("INGEST_FAILED", err.Error())
	}
	result.MessageID, result.Created, result.Status = idx.MessageID, created, "ACCEPTED"
	return result
}

func (s *Server) openReportMessages(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, openAPIBodyLimit)
	var in struct {
		Messages []openDeviceMessage `json:"messages"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	if len(in.Messages) == 0 || len(in.Messages) > maxOpenBatchMessages {
		problem(w, 422, "messages must contain 1 to 100 items")
		return
	}
	if err := s.onboarding.EnsureStandardRelease(r.Context(), claims(r).TenantID); err != nil {
		problem(w, 503, "standard protocol is unavailable")
		return
	}
	results := make([]openIngestResult, 0, len(in.Messages))
	accepted, rateLimited := 0, 0
	for index, message := range in.Messages {
		result := s.ingestOpenMessage(r, index, message)
		if result.Status == "ACCEPTED" {
			accepted++
		} else if result.ErrorCode == "RATE_LIMITED" {
			rateLimited++
		}
		results = append(results, result)
	}
	status := http.StatusAccepted
	switch {
	case accepted == len(results):
	case accepted > 0:
		status = http.StatusMultiStatus
	case rateLimited == len(results):
		w.Header().Set("Retry-After", "1")
		status = http.StatusTooManyRequests
	default:
		status = http.StatusUnprocessableEntity
	}
	write(w, status, map[string]any{"accepted": accepted, "rejected": len(results) - accepted, "results": results})
}

func (s *Server) openReportAlarm(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, openAPIBodyLimit)
	var in struct {
		DeviceID   string         `json:"deviceId"`
		ID         string         `json:"id"`
		Timestamp  int64          `json:"timestamp"`
		AlarmType  string         `json:"alarmType,omitempty"`
		AlarmLevel string         `json:"alarmLevel,omitempty"`
		Content    string         `json:"content,omitempty"`
		Data       map[string]any `json:"data,omitempty"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	data := map[string]any{}
	for key, value := range in.Data {
		data[key] = value
	}
	data["alarm"] = true
	for key, value := range map[string]string{"alarmType": in.AlarmType, "alarmLevel": in.AlarmLevel, "content": in.Content} {
		if value = strings.TrimSpace(value); value != "" {
			data[key] = value
		}
	}
	if err := s.onboarding.EnsureStandardRelease(r.Context(), claims(r).TenantID); err != nil {
		problem(w, 503, "standard protocol is unavailable")
		return
	}
	result := s.ingestOpenMessage(r, 0, openDeviceMessage{DeviceID: in.DeviceID, Kind: "alarm", ID: in.ID, Timestamp: in.Timestamp, Data: data})
	if result.Status != "ACCEPTED" {
		if result.ErrorCode == "RATE_LIMITED" {
			w.Header().Set("Retry-After", "1")
		}
		write(w, openIngestStatus[result.ErrorCode], map[string]any{"type": "about:blank", "status": openIngestStatus[result.ErrorCode], "errorCode": result.ErrorCode, "detail": result.Error})
		return
	}
	// The alarm is raised asynchronously; its triggerId is "msg_" + messageId.
	write(w, http.StatusAccepted, map[string]any{"deviceId": result.DeviceID, "messageId": result.MessageID, "created": result.Created, "status": result.Status, "triggerId": "msg_" + result.MessageID})
}

func (s *Server) openAlarmAction(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Action string `json:"action"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	key, _ := requestAPIKeyRecord(r.Context())
	c := claims(r)
	v, err := s.engine.SetAlarmStatus(r.Context(), c.TenantID, r.PathValue("id"), strings.ToUpper(strings.TrimSpace(in.Action)), c.Username+" (API "+key.Name+")")
	if err != nil {
		problem(w, 422, err.Error())
		return
	}
	s.audit(r, "openapi.alarm.action", "alarm", v.ID, map[string]any{"action": v.Status, "apiKeyId": key.ID})
	write(w, 200, v)
}

type apiKeyInput struct {
	Name         string   `json:"name"`
	Username     string   `json:"username"`
	Capabilities []string `json:"capabilities"`
	Enabled      *bool    `json:"enabled,omitempty"`
	ExpiresAt    int64    `json:"expiresAt,omitempty"`
}

func (in *apiKeyInput) validate() error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || utf8.RuneCountInString(in.Name) > 64 {
		return errors.New("名称须为 1 至 64 个字符")
	}
	if len(in.Capabilities) == 0 {
		return errors.New("至少选择一项开放能力")
	}
	for _, capability := range in.Capabilities {
		if !slices.Contains(model.APICapabilities, capability) {
			return errors.New("存在无效的开放能力")
		}
	}
	slices.Sort(in.Capabilities)
	in.Capabilities = slices.Compact(in.Capabilities)
	if in.ExpiresAt < 0 || in.ExpiresAt > 0 && in.ExpiresAt <= time.Now().UnixMilli() {
		return errors.New("过期时间须晚于当前时间")
	}
	return nil
}

func publicAPIKeys(keys []model.APIKey) []model.APIKey {
	out := make([]model.APIKey, 0, len(keys))
	for _, key := range keys {
		key.SecretHash = ""
		out = append(out, key)
	}
	return out
}

func (s *Server) listAPIKeys(w http.ResponseWriter, r *http.Request) {
	_, state, ok := s.accessState(w, r)
	if !ok {
		return
	}
	capabilities := make([]map[string]string, 0, len(model.APICapabilities))
	for _, id := range model.APICapabilities {
		capabilities = append(capabilities, map[string]string{"id": id, "name": apiCapabilityNames[id]})
	}
	write(w, 200, map[string]any{"items": publicAPIKeys(state.APIKeys), "capabilities": capabilities, "tenantId": claims(r).TenantID})
}

// saveAccessState commits like commitAccess but lets the caller write its own response.
func (s *Server) saveAccessState(w http.ResponseWriter, r *http.Request, state model.AccessState) bool {
	store, err := s.accessStore()
	if err != nil {
		problem(w, 503, err.Error())
		return false
	}
	saved, err := store.SaveAccessState(r.Context(), claims(r).TenantID, state)
	if err != nil {
		problem(w, 500, "保存用户权限失败")
		return false
	}
	if !saved {
		problem(w, 409, "配置已被其他操作更新，请刷新重试")
		return false
	}
	return true
}

func (s *Server) createAPIKey(w http.ResponseWriter, r *http.Request) {
	var in apiKeyInput
	if decode(w, r, &in) != nil {
		return
	}
	if err := in.validate(); err != nil {
		problem(w, 422, err.Error())
		return
	}
	_, state, ok := s.accessState(w, r)
	if !ok {
		return
	}
	if !slices.ContainsFunc(state.Users, func(u model.PlatformUser) bool { return u.Username == in.Username }) {
		problem(w, 422, "绑定用户不存在")
		return
	}
	if len(state.APIKeys) >= maxAPIKeysPerTenant {
		problem(w, 422, "开放接口密钥数量已达上限")
		return
	}
	c := claims(r)
	secret := randomHex(24)
	key := model.APIKey{ID: "ak" + randomHex(8), Name: in.Name, Username: in.Username, Capabilities: in.Capabilities, SecretHash: apiKeySecretHash(secret), Enabled: in.Enabled == nil || *in.Enabled, CreatedBy: c.Username, CreatedAt: time.Now().UnixMilli(), ExpiresAt: in.ExpiresAt}
	state.APIKeys = append(state.APIKeys, key)
	if !s.saveAccessState(w, r, state) {
		return
	}
	s.audit(r, "openapi.key.create", "api-key", key.ID, map[string]any{"username": key.Username, "capabilities": key.Capabilities})
	key.SecretHash = ""
	// The plaintext key is returned only once and is never stored.
	write(w, 201, map[string]any{"item": key, "apiKey": formatAPIKey(c.TenantID, key.ID, secret)})
}

func (s *Server) updateAPIKey(w http.ResponseWriter, r *http.Request) {
	var in apiKeyInput
	if decode(w, r, &in) != nil {
		return
	}
	if err := in.validate(); err != nil {
		problem(w, 422, err.Error())
		return
	}
	_, state, ok := s.accessState(w, r)
	if !ok {
		return
	}
	index := slices.IndexFunc(state.APIKeys, func(k model.APIKey) bool { return k.ID == r.PathValue("id") })
	if index < 0 {
		problem(w, 404, "开放接口密钥不存在")
		return
	}
	key := &state.APIKeys[index]
	if in.Username != "" && in.Username != key.Username {
		problem(w, 422, "绑定用户创建后不能修改，请新建密钥")
		return
	}
	key.Name, key.Capabilities, key.ExpiresAt = in.Name, in.Capabilities, in.ExpiresAt
	if in.Enabled != nil {
		key.Enabled = *in.Enabled
	}
	updated := *key
	if !s.saveAccessState(w, r, state) {
		return
	}
	s.audit(r, "openapi.key.update", "api-key", updated.ID, map[string]any{"enabled": updated.Enabled, "capabilities": updated.Capabilities})
	updated.SecretHash = ""
	write(w, 200, map[string]any{"item": updated})
}

func (s *Server) deleteAPIKey(w http.ResponseWriter, r *http.Request) {
	_, state, ok := s.accessState(w, r)
	if !ok {
		return
	}
	index := slices.IndexFunc(state.APIKeys, func(k model.APIKey) bool { return k.ID == r.PathValue("id") })
	if index < 0 {
		problem(w, 404, "开放接口密钥不存在")
		return
	}
	state.APIKeys = slices.Delete(state.APIKeys, index, index+1)
	if !s.saveAccessState(w, r, state) {
		return
	}
	s.audit(r, "openapi.key.delete", "api-key", r.PathValue("id"), nil)
	write(w, 200, map[string]bool{"success": true})
}
