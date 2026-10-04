package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"iot-platform/internal/auth"
	"iot-platform/internal/externaldata"
	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
)

const externalBase = "/api/v1/external-data"

var errExternalDenied = errors.New("当前用户没有外部数据接入或关联对象权限")

func (s *Server) RunExternalData(ctx context.Context) {
	if s.externalData != nil {
		s.externalData.Run(ctx, s.log)
	}
}

func (s *Server) externalDataRoutes() {
	for _, resource := range []struct{ path, kind string }{{"sources", "source"}, {"endpoints", "endpoint"}, {"bindings", "binding"}, {"records", "record"}, {"jobs", "job"}} {
		s.router.GET(externalBase+"/"+resource.path, s.authorize("viewer"), s.endpoint(s.externalList(resource.kind)))
		if resource.kind == "record" || resource.kind == "job" {
			continue
		}
		s.router.POST(externalBase+"/"+resource.path, s.authorize("operator"), s.endpoint(s.externalSave(resource.kind)))
		s.router.PUT(externalBase+"/"+resource.path+"/:id", s.authorize("operator"), s.endpoint(s.externalSave(resource.kind), "id"))
		s.router.DELETE(externalBase+"/"+resource.path+"/:id", s.authorize("operator"), s.endpoint(s.externalDelete(resource.kind), "id"))
	}
	s.router.GET(externalBase+"/records/:id", s.authorize("viewer"), s.endpoint(s.externalRecord, "id"))
	for _, kind := range []string{"record", "job"} {
		s.router.POST(externalBase+"/"+kind+"s/:id/retry", s.authorize("operator"), s.endpoint(s.externalRetry(kind), "id"))
	}
	s.router.POST(externalBase+"/endpoints/:id/pull", s.authorize("operator"), s.endpoint(s.externalPull, "id"))
	s.router.POST(externalBase+"/endpoints/:id/test", s.authorize("operator"), s.endpoint(s.externalTest(false), "id"))
	s.router.POST(externalBase+"/endpoints/:id/test-fetch", s.authorize("operator"), s.endpoint(s.externalTest(true), "id"))
	s.router.POST(externalBase+"/endpoints/:id/rotate-key", s.authorize("operator"), s.endpoint(s.externalRotateKey, "id"))
	// Authenticated ingress is also the explicit permission granted to the
	// execution user. Public callbacks additionally verify their own credentials.
	s.router.POST(externalBase+"/endpoints/:id/receive", s.authorize("operator"), s.endpoint(s.externalManualReceive, "id"))
	s.router.POST("/api/external/v1/:tenantId/:id", s.endpoint(s.externalPush, "tenantId", "id"))
}

func externalError(w http.ResponseWriter, err error) {
	status := 500
	detail := "外部数据处理失败，请稍后重试"
	switch {
	case errors.Is(err, externaldata.ErrNotFound):
		status = 404
		detail = err.Error()
	case errors.Is(err, externaldata.ErrConflict):
		status = 409
		detail = err.Error()
	case errors.Is(err, externaldata.ErrInvalid):
		status = 422
		detail = err.Error()
	case errors.Is(err, errExternalDenied):
		status = 403
		detail = err.Error()
	}
	problem(w, status, detail)
}

func (s *Server) externalManagement(w http.ResponseWriter, r *http.Request) bool {
	if s.externalData == nil {
		problem(w, 503, "外部数据服务暂不可用")
		return false
	}
	// Configuration and unmatched raw payloads are tenant-wide resources.
	// A narrow execution user may receive data but cannot browse this console.
	if scope, ok := requestScope(r.Context()); ok && !scope.All {
		problem(w, 403, "管理外部数据接入需要全部设备范围；执行用户可单独限制设备范围")
		return false
	}
	if r.Method != "GET" && r.PathValue("id") != "" {
		kind := ""
		for _, k := range []string{"source", "endpoint", "binding", "record", "job"} {
			if strings.HasPrefix(r.URL.Path, externalBase+"/"+k+"s/") {
				kind = k
				break
			}
		}
		if kind != "" {
			entry, err := s.externalData.Store.Get(r.Context(), claims(r).TenantID, kind, r.PathValue("id"))
			if err != nil {
				externalError(w, err)
				return false
			}
			sourceID := entry.SourceID
			if kind == "source" {
				sourceID = entry.ID
			}
			if !s.externalControlSource(w, r, sourceID) {
				return false
			}
		}
	}
	return true
}

func (s *Server) externalControlSource(w http.ResponseWriter, r *http.Request, sourceID string) bool {
	src, err := s.externalData.SourceInfo(r.Context(), claims(r).TenantID, sourceID)
	if err != nil {
		externalError(w, err)
		return false
	}
	if !externalCanBindUser(r, src.Username) {
		problem(w, 403, "仅平台管理员可操作其他执行用户的外部系统")
		return false
	}
	return true
}
func externalCanBindUser(r *http.Request, username string) bool {
	c := claims(r)
	return username == c.Username || (c.TokenUse != "user" && c.Role == "admin")
}
func externalMutationRevision(w http.ResponseWriter, r *http.Request, revision int64) bool {
	if r.Method == "POST" && revision != 0 || r.Method == "PUT" && revision < 1 {
		problem(w, 422, "新建时版本须为 0；编辑时须提供当前版本")
		return false
	}
	return true
}

func (s *Server) externalList(kind string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.externalManagement(w, r) {
			return
		}
		p := parseListPagination(r)
		q := r.URL.Query()
		items, total, err := s.externalData.List(r.Context(), externaldata.Query{TenantID: claims(r).TenantID, Kind: kind, SourceID: q.Get("sourceId"), EndpointID: q.Get("endpointId"), Status: q.Get("status"), Limit: p.PageSize, Offset: p.Offset})
		if err != nil {
			externalError(w, err)
			return
		}
		writeList(w, 200, items, total, p, nil)
	}
}
func (s *Server) externalSave(kind string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.externalManagement(w, r) {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
		tenant := claims(r).TenantID
		id := r.PathValue("id")
		var result any
		var err error
		switch kind {
		case "source":
			var v externaldata.Source
			if decode(w, r, &v) != nil {
				return
			}
			if id != "" {
				v.ID = id
			}
			if !externalMutationRevision(w, r, v.Revision) {
				return
			}
			if !externalCanBindUser(r, v.Username) {
				problem(w, 403, "仅平台管理员可绑定其他执行用户")
				return
			}
			if !s.externalUserExists(r.Context(), tenant, v.Username) {
				problem(w, 422, "请选择本租户中启用的执行用户")
				return
			}
			result, err = s.externalData.SaveSource(r.Context(), tenant, v)
		case "endpoint":
			var v externaldata.Endpoint
			if decode(w, r, &v) != nil {
				return
			}
			if id != "" {
				v.ID = id
			}
			if !externalMutationRevision(w, r, v.Revision) || !s.externalControlSource(w, r, v.SourceID) {
				return
			}
			result, err = s.externalData.SaveEndpoint(r.Context(), tenant, v)
		case "binding":
			var v externaldata.Binding
			if decode(w, r, &v) != nil {
				return
			}
			if id != "" {
				v.ID = id
			}
			if !externalMutationRevision(w, r, v.Revision) || !s.externalControlSource(w, r, v.SourceID) {
				return
			}
			err = s.externalBindingAllowed(r.Context(), tenant, v)
			if err == nil {
				result, err = s.externalData.SaveBinding(r.Context(), tenant, v)
			}
		}
		if err != nil {
			externalError(w, err)
			return
		}
		s.audit(r, "external-data.save", kind, id, nil)
		status := 200
		if r.Method == "POST" {
			status = 201
		}
		write(w, status, result)
	}
}
func (s *Server) externalDelete(kind string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.externalManagement(w, r) {
			return
		}
		rev, _ := strconv.ParseInt(r.URL.Query().Get("revision"), 10, 64)
		if rev < 1 {
			problem(w, 422, "请提供当前记录版本")
			return
		}
		if err := s.externalData.Delete(r.Context(), claims(r).TenantID, kind, r.PathValue("id"), rev); err != nil {
			externalError(w, err)
			return
		}
		s.audit(r, "external-data.delete", kind, r.PathValue("id"), nil)
		write(w, 200, map[string]any{"deleted": true})
	}
}
func (s *Server) externalRecord(w http.ResponseWriter, r *http.Request) {
	if !s.externalManagement(w, r) {
		return
	}
	tenant := claims(r).TenantID
	e, err := s.externalData.Store.Get(r.Context(), tenant, "record", r.PathValue("id"))
	if err != nil {
		externalError(w, err)
		return
	}
	var record externaldata.Record
	if json.Unmarshal(e.Body, &record) != nil {
		problem(w, 500, "记录内容不可读")
		return
	}
	receipt, err := s.externalData.Store.Get(r.Context(), tenant, "receipt", record.ReceiptID)
	if err != nil {
		externalError(w, err)
		return
	}
	var original externaldata.Receipt
	if err = json.Unmarshal(receipt.Body, &original); err != nil {
		externalError(w, err)
		return
	}
	write(w, 200, map[string]any{"entry": e, "receipt": original.Payload})
}
func (s *Server) externalRetry(kind string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.externalManagement(w, r) {
			return
		}
		var in struct {
			Revision          int64 `json:"revision"`
			UseCurrentMapping bool  `json:"useCurrentMapping"`
		}
		if decode(w, r, &in) != nil {
			return
		}
		e, err := s.externalData.Retry(r.Context(), claims(r).TenantID, kind, r.PathValue("id"), in.Revision, in.UseCurrentMapping)
		if err != nil {
			externalError(w, err)
			return
		}
		s.audit(r, "external-data.retry", kind, e.ID, nil)
		write(w, 202, e)
	}
}
func (s *Server) externalPull(w http.ResponseWriter, r *http.Request) {
	if !s.externalManagement(w, r) {
		return
	}
	var in struct {
		From int64 `json:"from"`
		To   int64 `json:"to"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	ep, err := s.externalData.Endpoint(r.Context(), claims(r).TenantID, r.PathValue("id"))
	if err != nil {
		externalError(w, err)
		return
	}
	e, err := s.externalData.Pull(r.Context(), claims(r).TenantID, ep, in.From, in.To)
	if err != nil {
		externalError(w, err)
		return
	}
	s.audit(r, "external-data.pull", "job", e.ID, nil)
	write(w, 202, e)
}
func (s *Server) externalTest(fetch bool) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.externalManagement(w, r) {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		var in struct {
			Sample json.RawMessage `json:"sample"`
			From   int64           `json:"from"`
			To     int64           `json:"to"`
		}
		if decode(w, r, &in) != nil {
			return
		}
		ep, err := s.externalData.Endpoint(r.Context(), claims(r).TenantID, r.PathValue("id"))
		if err != nil {
			externalError(w, err)
			return
		}
		var result any
		if fetch {
			if ep.Mode != "pull" {
				problem(w, 422, "仅拉取接口可测试请求")
				return
			}
			result, err = s.externalData.FetchPreview(r.Context(), claims(r).TenantID, ep, in.From, in.To)
		} else {
			var items []any
			items, err = s.externalData.Preview(ep, in.Sample)
			result = map[string]any{"items": items, "raw": in.Sample}
		}
		if err != nil {
			var limited *externaldata.RateLimitError
			if errors.As(err, &limited) {
				w.Header().Set("Retry-After", strconv.FormatInt(limited.RetryAfterSeconds(), 10))
				problem(w, http.StatusTooManyRequests, limited.Error())
				return
			}
			problem(w, 422, err.Error())
			return
		}
		write(w, 200, result)
	}
}
func (s *Server) externalRotateKey(w http.ResponseWriter, r *http.Request) {
	if !s.externalManagement(w, r) {
		return
	}
	tenant := claims(r).TenantID
	id := r.PathValue("id")
	key, err := s.externalData.RotateKey(r.Context(), tenant, id)
	if err != nil {
		externalError(w, err)
		return
	}
	s.audit(r, "external-data.rotate-key", "endpoint", id, nil)
	w.Header().Set("Cache-Control", "no-store")
	write(w, 200, map[string]any{"key": key, "receiveUrl": "/api/external/v1/" + url.PathEscape(tenant) + "/" + url.PathEscape(id)})
}

func (s *Server) externalUserExists(ctx context.Context, tenant, username string) bool {
	if username == s.cfg.AdminUser && slices.Contains(s.cfg.AdminTenants, tenant) {
		return true
	}
	state, err := s.authorizationAccess(ctx, tenant)
	if err != nil {
		return false
	}
	for _, u := range state.Users {
		if u.Username == username && u.Enabled {
			return true
		}
	}
	return false
}

func (s *Server) authorizeExternalSource(ctx context.Context, tenant string, src externaldata.Source, ep externaldata.Endpoint) (context.Context, error) {
	if src.Username == s.cfg.AdminUser && slices.Contains(s.cfg.AdminTenants, tenant) {
		c := auth.Claims{Username: src.Username, TenantID: tenant, Role: "admin"}
		ctx = context.WithValue(ctx, claimsKey, c)
		ctx = auth.ContextWithClaims(ctx, c)
		return context.WithValue(ctx, deviceScopeKey{}, deviceScope{Tenant: tenant, All: true}), nil
	}
	state, err := s.authorizationAccess(ctx, tenant)
	if err != nil {
		return ctx, err
	}
	for _, u := range state.Users {
		if u.Username != src.Username || !u.Enabled {
			continue
		}
		permissions := effectivePermissions(state, u)
		s.stripOpsPermissions(tenant, permissions)
		op := "POST " + externalBase + "/endpoints/:id/receive"
		if ep.Mode == "pull" {
			op = "POST " + externalBase + "/endpoints/:id/pull"
		}
		if !permissions["menu:externalData"] || !permissions[op] {
			return ctx, errExternalDenied
		}
		if ep.Kind != "event" && !permissions["menu:devices"] {
			return ctx, errExternalDenied
		}
		u = resolveUserDeviceScope(state, u)
		scope := scopeFor(u, permissions, tenant)
		c := auth.Claims{Username: u.Username, TenantID: tenant, Role: "operator", TokenUse: "user", SessionVersion: u.SessionVersion}
		ctx = context.WithValue(ctx, claimsKey, c)
		ctx = auth.ContextWithClaims(ctx, c)
		ctx = context.WithValue(ctx, permissionsKey{}, permissions)
		ctx = context.WithValue(ctx, deviceScopeKey{}, scope)
		return ctx, nil
	}
	return ctx, errExternalDenied
}

func (s *Server) externalBindingAllowed(ctx context.Context, tenant string, b externaldata.Binding) error {
	if b.Kind == "camera" {
		cam, err := s.engine.Repo.GetVideoCameraMapping(ctx, tenant, b.TargetID)
		if err != nil || !cam.Enabled {
			return errExternalDenied
		}
		if cam.DeviceID == "" {
			return fmtExternalInvalid("摄像头须先关联平台设备，才能确定数据权限归属")
		}
		_, err = s.engine.Repo.GetManagedDevice(ctx, tenant, cam.DeviceID)
		if err != nil {
			return errExternalDenied
		}
		return nil
	}
	if b.Kind != "device" {
		return fmtExternalInvalid("对象类型须为设备或摄像头")
	}
	_, err := s.engine.Repo.GetManagedDevice(ctx, tenant, b.TargetID)
	if err != nil {
		return errExternalDenied
	}
	return nil
}
func fmtExternalInvalid(detail string) error {
	return errors.Join(externaldata.ErrInvalid, errors.New(detail))
}

func (s *Server) externalManualReceive(w http.ResponseWriter, r *http.Request) {
	if s.externalData == nil {
		problem(w, 503, "外部数据服务不可用")
		return
	}
	s.externalReceive(w, r, claims(r).TenantID, r.PathValue("id"), false)
}
func (s *Server) externalPush(w http.ResponseWriter, r *http.Request) {
	if s.externalData == nil {
		problem(w, 503, "外部数据服务不可用")
		return
	}
	s.externalReceive(w, r, r.PathValue("tenantId"), r.PathValue("id"), true)
}
func (s *Server) externalReceive(w http.ResponseWriter, r *http.Request, tenant, id string, public bool) {
	if !s.onboarding.AllowRate("external\x00"+tenant+"\x00"+id, 30) {
		w.Header().Set("Retry-After", "1")
		problem(w, 429, "接收过于频繁，请稍后重试")
		return
	}
	ep, err := s.externalData.Endpoint(r.Context(), tenant, id)
	if err != nil || ep.Mode != "push" {
		problem(w, 404, "接收接口不存在")
		return
	}
	src, err := s.externalData.Source(r.Context(), tenant, ep.SourceID)
	if err != nil || !ep.Enabled || !src.Enabled {
		problem(w, 503, "接收接口已停用")
		return
	}
	payload, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		problem(w, 413, "接收数据超过限制")
		return
	}
	if public && !verifyExternalPush(r, payload, src, ep) {
		problem(w, 401, "接收认证失败")
		return
	}
	workCtx, err := s.authorizeExternalSource(r.Context(), tenant, src, ep)
	if err != nil {
		externalError(w, err)
		return
	}
	if !public && !externalCanBindUser(r, src.Username) {
		problem(w, 403, "无权向其他执行用户的接口提交数据")
		return
	}
	_, err = s.externalData.Receive(workCtx, tenant, ep, payload, "")
	if err != nil {
		externalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	status := ep.ResponseStatus
	if status == 0 {
		status = 202
	}
	w.WriteHeader(status)
	_, _ = w.Write(ep.ResponseBody)
}
func verifyExternalPush(r *http.Request, payload []byte, src externaldata.Source, ep externaldata.Endpoint) bool {
	a := src.Auth
	if ep.Auth != nil {
		a = *ep.Auth
	}
	equal := func(a, b string) bool {
		return a != "" && b != "" && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
	}
	switch a.Type {
	case "hmac":
		tsHeader := a.TimestampHeader
		if tsHeader == "" {
			tsHeader = "X-Timestamp"
		}
		signatureHeader := a.Header
		if signatureHeader == "" {
			signatureHeader = "X-Signature"
		}
		ts := r.Header.Get(tsHeader)
		seconds, err := strconv.ParseInt(ts, 10, 64)
		if err != nil || abs(time.Now().Unix()-seconds) > 300 || a.Secret == "" {
			return false
		}
		mac := hmac.New(sha256.New, []byte(a.Secret))
		_, _ = mac.Write([]byte(ts))
		_, _ = mac.Write(payload)
		return equal(hex.EncodeToString(mac.Sum(nil)), strings.ToLower(r.Header.Get(signatureHeader)))
	case "basic":
		u, p, ok := r.BasicAuth()
		return ok && equal(u, a.Username) && equal(p, a.Secret)
	case "bearer":
		return equal(auth.Bearer(r.Header.Get("Authorization")), a.Secret)
	case "api_key":
		if a.Query != "" {
			return equal(r.URL.Query().Get(a.Query), a.Secret)
		}
		header := a.Header
		if header == "" {
			header = "X-API-Key"
		}
		return equal(r.Header.Get(header), a.Secret)
	case "token":
		return false
	default:
		provided := requestAPIKey(r)
		encoded, _ := json.Marshal([]string{provided})
		digest := sha256.Sum256(encoded)
		return provided != "" && equal(hex.EncodeToString(digest[:]), ep.PushKey)
	}
}

func (s *Server) deliverExternalEvent(ctx context.Context, tenant string, src externaldata.Source, ep externaldata.Endpoint, ev externaldata.Event, b externaldata.Binding, previous externaldata.Result) (externaldata.Result, error) {
	result := previous
	if ep.Kind == "event" && ev.ObjectID == "" {
		return result, nil
	}
	deviceID := b.TargetID
	var camera model.VideoCameraMapping
	if b.Kind == "camera" {
		var err error
		camera, err = s.engine.Repo.GetVideoCameraMapping(ctx, tenant, b.TargetID)
		if err != nil || !camera.Enabled || camera.DeviceID == "" {
			return result, errExternalDenied
		}
		deviceID = camera.DeviceID
		result.CameraID = camera.CameraID
	}
	if previous.DeviceID != "" && previous.DeviceID != deviceID || previous.CameraID != "" && previous.CameraID != result.CameraID {
		return result, fmtExternalInvalid("同一外部事件不能切换关联设备或摄像头，请使用新的事件编号")
	}
	device, err := s.engine.Repo.GetManagedDevice(ctx, tenant, deviceID)
	if err != nil || device.Status != "ENABLED" {
		return result, errExternalDenied
	}
	product, err := s.engine.Repo.GetProduct(ctx, tenant, device.ProductID)
	if err != nil || product.Status != "ENABLED" {
		return result, errExternalDenied
	}
	result.DeviceID = deviceID
	status := strings.ToUpper(ev.Status)
	isAlarm := ep.Kind == "alarm" || ep.Kind == "video_alarm"
	if isAlarm && (status == "RECOVERED" || status == "CLOSED") {
		if c, ok := ctx.Value(permissionsKey{}).(map[string]bool); ok && !allowsRoute(c, "POST", "/api/v1/alarms/:id/actions") {
			return result, errExternalDenied
		}
		if len(previous.AlarmIDs) > 0 {
			err = s.engine.RecoverExternalAlarms(ctx, tenant, previous.AlarmIDs, src.Username+" (外部数据)")
		}
		return result, err
	}
	if isAlarm && status != "" && status != "ACTIVE" {
		return result, fmtExternalInvalid("事件状态须映射为 ACTIVE、RECOVERED 或 CLOSED")
	}
	if err = s.onboarding.EnsureStandardRelease(ctx, tenant); err != nil {
		return result, err
	}
	data := map[string]any{}
	for k, v := range ev.Data {
		data[k] = v
	}
	kind := ep.Kind
	if kind == "video_alarm" {
		kind = "alarm"
	}
	if kind == "alarm" {
		if ev.AlarmType == "" {
			return result, fmtExternalInvalid("告警类型不能为空")
		}
		data["alarmType"] = ev.AlarmType
		data["alarmLevel"] = ev.AlarmLevel
		data["content"] = ev.Content
	}
	if kind == "event" {
		data["content"] = ev.Content
	}
	encoded, _ := json.Marshal(ev)
	digest := sha256.Sum256(append([]byte(src.ID+"\x00"+ep.Kind+"\x00"), encoded...))
	messageKey := hex.EncodeToString(digest[:])
	payload, _ := json.Marshal(map[string]any{"id": messageKey, "timestamp": ev.Timestamp, "data": data, "online": ev.Online, "event": ev.AlarmType})
	raw, err := onboarding.StandardRaw(tenant, device.ProductID, deviceID, kind, "HTTP", payload)
	if err != nil {
		return result, fmtExternalInvalid(err.Error())
	}
	raw.Source = "external-data"
	if raw.Metadata == nil {
		raw.Metadata = map[string]any{}
	}
	identity := sha256.Sum256([]byte(src.ID + "\x00" + ep.Kind + "\x00" + ev.ID))
	raw.Metadata["externalEventKey"] = hex.EncodeToString(identity[:])
	raw.Metadata["externalSourceId"] = src.ID
	raw.Metadata["externalEventId"] = ev.ID
	if b.Kind == "camera" {
		v := model.VideoAlarmEvent{EventID: messageKey, Source: src.ID, TenantID: tenant, CameraID: camera.CameraID, CameraName: camera.CameraName, AlarmType: ev.AlarmType, AlarmName: ev.Content, AlarmLevel: ev.AlarmLevel, Confidence: ev.Confidence, EventTime: ev.Timestamp, SnapshotURL: ev.SnapshotURL, VideoClipURL: ev.VideoClipURL, ReceivedAt: time.Now().UnixMilli(), Raw: map[string]any{"externalEventId": ev.ID, "externalSourceId": src.ID, "deviceId": deviceID}}
		vJSON, _ := json.Marshal(v)
		raw.Metadata["externalVideoEvent"] = string(vJSON)
	}
	idx, _, err := s.engine.IngestRaw(ctx, raw)
	if idx.MessageID != "" {
		result.MessageID = idx.MessageID
	}
	if err != nil {
		return result, err
	}
	delivery, err := s.externalData.Store.Get(ctx, tenant, "delivery", idx.MessageID)
	if err != nil {
		return result, errors.New("原始数据已保存，等待解析及业务处理完成")
	}
	// Do not let json.Unmarshal reuse the previous ledger's slice backing
	// array: its historical alarm identities must survive a type/rule change.
	result.AlarmIDs = nil
	if err = json.Unmarshal(delivery.Body, &result); err != nil {
		return result, err
	}
	for _, id := range previous.AlarmIDs {
		if !slices.Contains(result.AlarmIDs, id) {
			result.AlarmIDs = append(result.AlarmIDs, id)
		}
	}
	return result, nil
}
