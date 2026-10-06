package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"time"

	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
)

func (s *Server) deviceRegistry(w http.ResponseWriter, r *http.Request) {
	tenantID := claims(r).TenantID
	pagination := parseListPagination(r)
	filter, err := s.deviceFilter(r.Context(), tenantID, r.URL.Query())
	if err != nil {
		problem(w, 422, err.Error())
		return
	}
	// The scope-aware repository filters limited users before totals and pagination.
	items, total, err := s.engine.Repo.ListManagedDevicesFiltered(r.Context(), filter, pagination.PageSize, pagination.Offset)
	if err != nil {
		s.fail(w, r, err, "")
		return
	}
	deviceIDs := make([]string, 0, len(items))
	for _, item := range items {
		deviceIDs = append(deviceIDs, item.ID)
	}
	childCounts, err := s.engine.Repo.CountManagedDeviceChildren(r.Context(), tenantID, deviceIDs)
	if err != nil {
		s.fail(w, r, err, "")
		return
	}
	productIDs := make([]string, 0, len(items))
	for _, item := range items {
		productIDs = append(productIDs, item.ProductID)
	}
	products, err := s.engine.Repo.GetProductsByIDs(r.Context(), tenantID, productIDs)
	if err != nil {
		products = map[string]model.Product{}
		for _, id := range productIDs {
			if product, getErr := s.engine.Repo.GetProduct(r.Context(), tenantID, id); getErr == nil {
				products[id] = product
			}
		}
	}
	states, err := s.engine.Repo.GetDeviceStatesByIDs(r.Context(), tenantID, deviceIDs)
	if err != nil {
		states = map[string]model.DeviceState{}
		for _, id := range deviceIDs {
			if state, getErr := s.engine.Repo.GetDeviceState(r.Context(), tenantID, id); getErr == nil {
				states[id] = state
			}
		}
	}
	// Parents may be on another page; resolve their names within the caller's scope.
	parents := map[string]map[string]string{}
	for _, v := range items {
		if v.GatewayID == "" {
			continue
		}
		if _, seen := parents[v.GatewayID]; seen {
			continue
		}
		parents[v.GatewayID] = nil
		if parent, getErr := s.engine.Repo.GetManagedDevice(r.Context(), tenantID, v.GatewayID); getErr == nil {
			parents[v.GatewayID] = map[string]string{"id": parent.ID, "name": parent.Name}
		}
	}
	out := make([]map[string]any, 0, len(items))
	for _, v := range items {
		product := products[v.ProductID]
		row := map[string]any{"device": v.Public(product), "childCount": childCounts[v.ID], "credentialSupported": v.UsesPlatformCredentials(product)}
		if parent := parents[v.GatewayID]; parent != nil {
			row["parent"] = parent
		}
		if state, ok := states[v.ID]; ok {
			row["runtimeState"] = state
		}
		out = append(out, row)
	}
	writeList(w, 200, out, total, pagination, nil)
}

func (s *Server) saveManagedDevice(w http.ResponseWriter, r *http.Request) {
	// An embedded ManagedDevice would promote UnmarshalJSON and silently ignore
	// Trial. Decode the method-free alias, then normalize its historical tags.
	type deviceFields model.ManagedDevice
	var input struct {
		deviceFields
		Trial bool `json:"trial,omitempty"`
	}
	if decode(w, r, &input) != nil {
		return
	}
	v := model.ManagedDevice(input.deviceFields)
	v.NormalizeConnectionTags()
	c := claims(r)
	v.TenantID = c.TenantID
	if id := r.PathValue("id"); id != "" {
		v.ID = id
	}
	if v.ID == "" {
		v.ID = "device_" + randomHex(6)
	}
	if v.Name == "" || v.ProductID == "" {
		problem(w, 422, "name and productId are required")
		return
	}
	if err := model.ValidateDeviceTiming(v.ReportIntervalSec, v.OfflineToleranceSec); err != nil {
		problem(w, 422, err.Error())
		return
	}
	product, err := s.engine.Repo.GetProduct(r.Context(), c.TenantID, v.ProductID)
	if err != nil {
		problem(w, 422, "product not found")
		return
	}
	now := time.Now().UnixMilli()
	created, timingChanged := false, false
	if old, err := s.engine.Repo.GetManagedDevice(r.Context(), c.TenantID, v.ID); err == nil {
		timingChanged = old.ReportIntervalSec != v.ReportIntervalSec || old.OfflineToleranceSec != v.OfflineToleranceSec
		if old.RegistrationSource == "PROTOCOL_CHILD_AUTO" {
			if v.ProductID != old.ProductID || v.GatewayID != old.GatewayID || v.DeviceRole != "CHILD" {
				problem(w, 422, "自动注册子设备的产品与主设备归属不可直接改写")
				return
			}
		}
		if v.ProductID != old.ProductID {
			problem(w, 409, "已登记设备不能直接更换设备模板，请从目标模板重新接入")
			return
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
		created = true
		v.CreatedAt = now
	} else {
		s.fail(w, r, err, "读取设备登记信息失败")
		return
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
		problem(w, 422, "deviceRole must be DIRECT, GATEWAY or CHILD")
		return
	}
	if v.RegistrationSource == "" {
		v.RegistrationSource = "MANUAL"
	}
	if v.DeviceRole == "CHILD" {
		if v.GatewayID == "" || v.GatewayID == v.ID {
			problem(w, 422, "a child device must reference a different gateway")
			return
		}
		gateway, gatewayErr := s.engine.Repo.GetManagedDevice(r.Context(), c.TenantID, v.GatewayID)
		if gatewayErr != nil {
			problem(w, 422, "gateway not found")
			return
		}
		// The parent must be registered as a gateway; the template category alone does not grant it.
		if gateway.DeviceRole != "GATEWAY" {
			problem(w, 422, "selected parent device is not a gateway")
			return
		}
	} else {
		v.GatewayID = ""
	}
	if v.Tags == nil {
		v.Tags = map[string]string{}
	}
	if v.DeviceRole == "CHILD" {
		parent, parentErr := s.engine.Repo.GetManagedDevice(r.Context(), c.TenantID, v.GatewayID)
		if parentErr != nil {
			problem(w, 422, "所属主设备已不存在")
			return
		}
		// A child uses its parent's physical connection; the caller cannot bind
		// it to an unrelated tenant-wide listener.
		if requested := v.ConnectorProfileID; requested != "" && requested != parent.ConnectorProfileID {
			problem(w, 422, "子设备只能继承所属主设备的连接")
			return
		}
		v.ConnectorProfileID = parent.ConnectorProfileID
	} else if requested := v.ConnectorProfileID; requested != "" {
		profiles, profileErr := s.engine.Repo.ListDeviceAccessProfiles(r.Context(), c.TenantID)
		if profileErr != nil {
			s.fail(w, r, profileErr, "")
			return
		}
		valid := false
		for _, candidate := range profiles {
			if candidate.ID == requested && candidate.ProductID == v.ProductID && (candidate.DeviceID == "" || candidate.DeviceID == v.ID) {
				valid = true
				break
			}
		}
		if !valid {
			problem(w, 422, "接入点不可用，请重新选择当前设备模板的接入点")
			return
		}
	}
	if created {
		s.enrollCompatibleDevice(w, r, v, product, input.Trial, "device.save")
		return
	}
	v.UpdatedAt = now
	if err := s.engine.Repo.SaveManagedDevice(r.Context(), v); err != nil {
		s.fail(w, r, err, "")
		return
	}
	if timingChanged {
		if _, err := s.engine.ApplyDeviceTiming(r.Context(), c.TenantID, v.ProductID, v.ID); err != nil {
			s.log.ErrorContext(r.Context(), "apply device reporting timing failed", "device", v.ID, "error", err)
		}
	}
	s.audit(r, "device.save", "device", v.ID, map[string]any{"productId": v.ProductID, "status": v.Status})
	write(w, 201, map[string]any{"device": v.Public(product)})
}

func (s *Server) registerDiscoveredDevice(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Trial              bool   `json:"trial,omitempty"`
		ConnectorProfileID string `json:"connectorProfileId,omitempty"`
	}
	// Discovery confirmation historically accepted an empty body.
	if r.Body != nil && r.ContentLength != 0 {
		if decode(w, r, &input) != nil {
			return
		}
	}
	c := claims(r)
	id := r.PathValue("id")
	if _, err := s.engine.Repo.GetManagedDevice(r.Context(), c.TenantID, id); err == nil {
		problem(w, 409, "device is already registered")
		return
	} else if !errors.Is(err, model.ErrNotFound) {
		s.fail(w, r, err, "读取设备登记信息失败")
		return
	}
	state, err := s.engine.Repo.GetDeviceState(r.Context(), c.TenantID, id)
	if err != nil {
		problem(w, 404, "discovered device not found")
		return
	}
	product, err := s.engine.Repo.GetProduct(r.Context(), c.TenantID, state.ProductID)
	if err != nil {
		problem(w, 422, "register its product before registering this device")
		return
	}
	device := model.ManagedDevice{ID: id, TenantID: c.TenantID, ProductID: state.ProductID, Name: "发现设备 " + id, Status: "ENABLED", ConnectorProfileID: input.ConnectorProfileID}
	s.enrollCompatibleDevice(w, r, device, product, input.Trial, "device.discovery.register")
}

func (s *Server) rotateDeviceCredential(w http.ResponseWriter, r *http.Request) {
	if !s.operationDevice(w, r) {
		return
	}
	c, v, e := s.onboarding.ChangeCredential(r.Context(), claims(r).TenantID, r.PathValue("id"), true)
	if e != nil {
		status := http.StatusInternalServerError
		if errors.Is(e, onboarding.ErrCredentialUnsupported) {
			status = http.StatusUnprocessableEntity
		}
		problem(w, status, e.Error())
		return
	}
	s.audit(r, "device.credential.rotate", "device", r.PathValue("id"), nil)
	write(w, 200, map[string]any{"deviceId": r.PathValue("id"), "credential": c, "revocation": v})
}

func (s *Server) debugDeviceIngest(w http.ResponseWriter, r *http.Request) {
	// 接入测试报文仍走正常归档与解析链路；设备归属从当前租户的登记记录确定。
	c := claims(r)
	v, err := s.engine.Repo.GetManagedDevice(r.Context(), c.TenantID, r.PathValue("id"))
	if err != nil {
		problem(w, 404, "device not found")
		return
	}
	var raw model.RawMessage
	if decode(w, r, &raw) != nil {
		return
	}
	if err = s.prepareManagedRaw(r.Context(), &raw, v); err != nil {
		problem(w, 422, err.Error())
		return
	}
	idx, created, err := s.engine.IngestRaw(r.Context(), raw)
	if ingestBusy(w, err) {
		return
	}
	if err != nil {
		problem(w, 422, err.Error())
		return
	}
	s.audit(r, "device.debug.ingest", "device", v.ID, map[string]any{"messageId": idx.MessageID})
	write(w, map[bool]int{true: 201, false: 200}[created], map[string]any{"created": created, "archive": idx})
}

func (s *Server) deviceIngest(w http.ResponseWriter, r *http.Request) {
	accessKey, secret := r.Header.Get("X-Device-Key"), r.Header.Get("X-Device-Secret")
	v, err := s.onboarding.Authenticate(r.Context(), accessKey, secret)
	if deviceAuthUnavailable(w, err) {
		return
	}
	if err != nil || v.ID != r.PathValue("deviceId") {
		problem(w, 401, "invalid or disabled device credential")
		return
	}
	if tenantID := r.Header.Get("X-Tenant-ID"); tenantID != "" && tenantID != v.TenantID {
		problem(w, 401, "invalid or disabled device credential")
		return
	}
	var raw model.RawMessage
	if decode(w, r, &raw) != nil {
		return
	}
	if err = s.prepareManagedRaw(r.Context(), &raw, v); err != nil {
		problem(w, 422, err.Error())
		return
	}
	// Credential-authenticated reports are field evidence; debug ingress keeps its own source.
	raw.Source = "device-http"
	idx, created, err := s.engine.IngestRaw(r.Context(), raw)
	if ingestBusy(w, err) {
		return
	}
	if err != nil {
		problem(w, 422, err.Error())
		return
	}
	write(w, map[bool]int{true: 201, false: 200}[created], map[string]any{"created": created, "messageId": idx.MessageID, "receivedAt": idx.ReceivedAt})
}

func (s *Server) prepareManagedRaw(ctx context.Context, raw *model.RawMessage, device model.ManagedDevice) error {
	if device.Connector == "HTTP" || device.Connector == "MQTT" {
		return fmt.Errorf("standard devices must use the authenticated standard ingress endpoint")
	}
	targetProductID := device.ProductID
	if raw.DeviceID != "" && raw.DeviceID != device.ID {
		if raw.ProductID == "" {
			return fmt.Errorf("child productId is required")
		}
		raw.GatewayID = device.ID
		targetProductID = raw.ProductID
		raw.Source = "gateway"
	} else {
		raw.DeviceID = device.ID
		raw.GatewayID = ""
		raw.Source = "managed-device"
	}
	product, err := s.engine.Repo.GetProduct(ctx, device.TenantID, targetProductID)
	if err != nil || product.Status != "ENABLED" {
		return fmt.Errorf("product is not enabled")
	}
	pkg, err := s.engine.Repo.GetProtocolPackage(ctx, device.TenantID, product.ProtocolPackageID)
	if err != nil || pkg.Status != "PUBLISHED" {
		return fmt.Errorf("protocol package is not published")
	}
	raw.TenantID, raw.ProductID = device.TenantID, product.ID
	raw.Protocol, raw.Transport, raw.PayloadFormat = pkg.Protocol, pkg.Transport, pkg.PayloadFormat
	return nil
}

func (s *Server) ingestRaw(w http.ResponseWriter, r *http.Request) {
	var v model.RawMessage
	if decode(w, r, &v) != nil {
		return
	}
	c := claims(r)
	v.TenantID = tenant(c, v.TenantID)
	start := time.Now()
	idx, created, err := s.engine.IngestRaw(r.Context(), v)
	s.metrics.ObserveIn("raw_archive_duration_seconds", metrics.RequestBuckets, time.Since(start).Seconds())
	if ingestBusy(w, err) {
		return
	}
	if err != nil {
		s.metrics.Inc("raw_archive_failed_total")
		problem(w, 422, err.Error())
		return
	}
	write(w, map[bool]int{true: 201, false: 200}[created], map[string]any{"created": created, "archive": idx})
}

// ingestBusy answers 429 with Retry-After while ingest is paused by backpressure.
func ingestBusy(w http.ResponseWriter, err error) bool {
	if !errors.Is(err, model.ErrBackpressure) {
		return false
	}
	w.Header().Set("Retry-After", "15")
	problem(w, http.StatusTooManyRequests, err.Error())
	return true
}

// deviceAuthUnavailable answers 503 when credentials could not be checked, so a
// repository failure is not reported to the device as a revoked credential.
func deviceAuthUnavailable(w http.ResponseWriter, err error) bool {
	if !errors.Is(err, onboarding.ErrUnavailable) {
		return false
	}
	w.Header().Set("Retry-After", "5")
	problem(w, http.StatusServiceUnavailable, "device credential check temporarily unavailable")
	return true
}

func newDeviceCredential() model.DeviceCredential {
	return model.DeviceCredential{AccessKey: "dk_" + randomHex(8), Secret: "ds_" + randomHex(18)}
}

func secretHash(secret string) string {
	h := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(h[:])
}
