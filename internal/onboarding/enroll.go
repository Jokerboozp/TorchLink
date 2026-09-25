package onboarding

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/protocolruntime"
)

// EnrollError carries a user-facing message and the HTTP status for it.
type EnrollError struct {
	Status  int
	Message string
}

func (e *EnrollError) Error() string { return e.Message }

func invalid(message string) error  { return &EnrollError{Status: 422, Message: message} }
func conflict(message string) error { return &EnrollError{Status: 409, Message: message} }

// reservedTags are maintained by the platform and cannot be supplied as labels.
var reservedTags = map[string]bool{"connector": true, "connectorProfileId": true, "childAddress": true, "childType": true, "onboardingRequestHash": true}

// NewProduct is a template created together with its first device.
type NewProduct struct {
	ID                string         `json:"id"`
	Name              string         `json:"name"`
	Category          string         `json:"category,omitempty"`
	ProtocolPackageID string         `json:"protocolPackageId"`
	Transport         string         `json:"transport,omitempty"`
	Description       string         `json:"description,omitempty"`
	Metadata          map[string]any `json:"metadata,omitempty"`
}

func (p NewProduct) product(tenant string, now int64) model.Product {
	category := strings.TrimSpace(p.Category)
	if category == "" {
		category = "other"
	}
	return model.Product{ID: strings.TrimSpace(p.ID), TenantID: tenant, Name: strings.TrimSpace(p.Name), Category: category, ProtocolPackageID: strings.TrimSpace(p.ProtocolPackageID), Transport: strings.ToUpper(strings.TrimSpace(p.Transport)), Description: strings.TrimSpace(p.Description), Metadata: p.Metadata, Status: "ENABLED", CreatedAt: now, UpdatedAt: now}
}

type EnrollDevice struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	DeviceRole  string            `json:"deviceRole,omitempty"`
	Description string            `json:"description,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
}

// EnrollListener describes a shared listener created in the same request.
type EnrollListener struct {
	Network    string `json:"network"`
	Host       string `json:"host,omitempty"`
	PublicHost string `json:"publicHost"`
	Port       int    `json:"port"`
}

type EnrollConnection struct {
	Mode string `json:"mode"`
	// Transport selects MQTT or HTTP for the standard protocol.
	Transport string          `json:"transport,omitempty"`
	ProfileID string          `json:"profileId,omitempty"`
	Listener  *EnrollListener `json:"listener,omitempty"`
	Host      string          `json:"host,omitempty"`
	Port      int             `json:"port,omitempty"`
	UnitID    *int            `json:"unitId,omitempty"`
	TimeoutMs int             `json:"timeoutMs,omitempty"`
}

// EnrollRequest registers one device, and optionally its template and its
// platform connection, in a single transaction.
type EnrollRequest struct {
	RequestID  string           `json:"requestId"`
	ProductID  string           `json:"productId,omitempty"`
	NewProduct *NewProduct      `json:"newProduct,omitempty"`
	Device     EnrollDevice     `json:"device"`
	Connection EnrollConnection `json:"connection"`
}

type EnrollResult struct {
	Reused     bool                       `json:"reused"`
	Mode       string                     `json:"mode"`
	Device     model.ManagedDevice        `json:"device"`
	Product    model.Product              `json:"product"`
	Profile    *model.DeviceAccessProfile `json:"profile,omitempty"`
	Credential model.DeviceCredential     `json:"credential,omitzero"`
}

func normalizeEnroll(q EnrollRequest) EnrollRequest {
	q.RequestID = strings.TrimSpace(q.RequestID)
	q.ProductID = strings.TrimSpace(q.ProductID)
	q.Device.ID = strings.TrimSpace(q.Device.ID)
	q.Device.Name = strings.TrimSpace(q.Device.Name)
	q.Device.DeviceRole = strings.ToUpper(strings.TrimSpace(q.Device.DeviceRole))
	q.Device.Description = strings.TrimSpace(q.Device.Description)
	q.Connection.Mode = strings.ToLower(strings.TrimSpace(q.Connection.Mode))
	q.Connection.ProfileID = strings.TrimSpace(q.Connection.ProfileID)
	q.Connection.Host = strings.TrimSpace(q.Connection.Host)
	labels := map[string]string{}
	for key, value := range q.Device.Tags {
		if key = strings.TrimSpace(key); key != "" && !reservedTags[key] {
			labels[key] = value
		}
	}
	q.Device.Tags = labels
	return q
}

// Enroll is idempotent per device ID: repeating the same request returns the
// saved device without a new secret; a different request for that ID conflicts.
func (s *Service) Enroll(ctx context.Context, tenant string, q EnrollRequest) (EnrollResult, error) {
	q = normalizeEnroll(q)
	if !segment.MatchString(q.RequestID) {
		return EnrollResult{}, invalid("请求标识无效，请刷新页面后重试")
	}
	if !segment.MatchString(q.Device.ID) {
		return EnrollResult{}, invalid("设备编号须为 1 至 128 位字母、数字、点、横线或下划线，且以字母或数字开头")
	}
	if q.Device.Name == "" || len(q.Device.Name) > 256 {
		return EnrollResult{}, invalid("请填写 256 字节以内的设备名称")
	}
	if len(q.Device.Tags) > 32 || len(q.Device.Description) > 1024 {
		return EnrollResult{}, invalid("标签最多 32 个，备注不能超过 1024 字节")
	}
	encoded, err := json.Marshal(q)
	if err != nil {
		return EnrollResult{}, err
	}
	digest := Hash(string(encoded))
	if existing, err := s.Repo.GetManagedDevice(ctx, tenant, q.Device.ID); err == nil {
		return s.recoverEnroll(ctx, tenant, existing, digest)
	} else if !errors.Is(err, model.ErrNotFound) {
		return EnrollResult{}, err
	}
	b, result, err := s.planEnroll(ctx, tenant, q)
	var e *EnrollError
	if errors.As(err, &e) && e.Status == 409 {
		// A concurrent identical request may have saved the template or
		// connection between the device lookup above and planning.
		if existing, getErr := s.Repo.GetManagedDevice(ctx, tenant, q.Device.ID); getErr == nil {
			return s.recoverEnroll(ctx, tenant, existing, digest)
		}
	}
	if err != nil {
		return EnrollResult{}, err
	}
	product := result.Product
	if b.Device.UsesPlatformCredentials(product) {
		credential, err := Credential()
		if err != nil {
			return EnrollResult{}, err
		}
		b.Device.AccessKey, b.Device.SecretHash, b.Device.SecretHint = credential.AccessKey, Hash(credential.Secret), credential.Secret[len(credential.Secret)-6:]
		result.Credential = credential
	} else {
		b.Device.AccessKey = model.ProtocolDeviceAccessKey(tenant, b.Device.ID)
	}
	b.Device.Tags["onboardingRequestHash"] = digest
	if err = s.Repo.SaveOnboarding(ctx, b); err != nil {
		// A competing identical request may have committed first.
		if existing, getErr := s.Repo.GetManagedDevice(ctx, tenant, q.Device.ID); getErr == nil {
			return s.recoverEnroll(ctx, tenant, existing, digest)
		}
		return EnrollResult{}, persistError(err)
	}
	result.Device = b.Device.Public(product)
	return result, nil
}

func persistError(err error) error {
	message := err.Error()
	switch {
	case strings.Contains(message, "listener port is already reserved"):
		return conflict("该端口已被其他平台监听占用，请换一个端口")
	case strings.Contains(message, "product binding changed"), strings.Contains(message, "listener changed"), strings.Contains(message, "protocol release changed"):
		return conflict("模板协议或接入点刚刚发生变化，请刷新后重试")
	case strings.Contains(message, "product is disabled"):
		return conflict("设备模板已停用，请刷新后重试")
	case strings.Contains(message, "already exists"), strings.Contains(message, "duplicate key"):
		return conflict("设备、模板或接入点标识已被占用，请刷新后重试")
	}
	return err
}

func (s *Service) recoverEnroll(ctx context.Context, tenant string, d model.ManagedDevice, digest string) (EnrollResult, error) {
	if d.Tags["onboardingRequestHash"] != digest {
		return EnrollResult{}, conflict("设备编号已登记，请在设备列表中查看或编辑该设备")
	}
	product, err := s.Repo.GetProduct(ctx, tenant, d.ProductID)
	if err != nil {
		return EnrollResult{}, err
	}
	result := EnrollResult{Reused: true, Device: d.Public(product), Product: product}
	if plan, err := s.Plan(ctx, tenant, product); err == nil {
		result.Mode = plan.Mode
	}
	if id := d.Tags["connectorProfileId"]; id != "" {
		if p, err := s.Repo.GetDeviceAccessProfile(ctx, tenant, id); err == nil {
			result.Profile = &p
			if p.ConnectionMode == "dial" {
				result.Mode = "dial"
			}
		}
	}
	return result, nil
}

func (s *Service) planEnroll(ctx context.Context, tenant string, q EnrollRequest) (model.OnboardingBundle, EnrollResult, error) {
	var b model.OnboardingBundle
	var result EnrollResult
	now := time.Now().UnixMilli()
	var product model.Product
	var plan AccessPlan
	var err error
	if q.NewProduct != nil {
		product = q.NewProduct.product(tenant, now)
		if !segment.MatchString(product.ID) || product.Name == "" || len(product.Name) > 256 || len(product.Description) > 1024 {
			return b, result, invalid("请填写有效的模板标识、256 字节以内的模板名称和 1024 字节以内的说明")
		}
		if _, err = s.Repo.GetProduct(ctx, tenant, product.ID); err == nil {
			return b, result, conflict("模板标识已存在，请换一个标识或选择已有模板")
		} else if !errors.Is(err, model.ErrNotFound) {
			return b, result, err
		}
		plan, err = s.DraftPlan(ctx, tenant, product)
	} else {
		product, err = s.Repo.GetProduct(ctx, tenant, q.ProductID)
		if errors.Is(err, model.ErrNotFound) {
			return b, result, invalid("设备模板不存在或当前账号无权查看")
		}
		if err != nil {
			return b, result, err
		}
		if product.Status != "ENABLED" {
			return b, result, invalid("设备模板未启用，不能添加新设备")
		}
		plan, err = s.Plan(ctx, tenant, product)
	}
	if err != nil {
		return b, result, err
	}
	if plan.Mode == ModeUnsupported {
		return b, result, invalid(plan.Reason)
	}
	release := *plan.Release
	c := q.Connection
	connector := plan.Connector
	if plan.Mode == ModeStandard {
		if t := strings.ToUpper(strings.TrimSpace(c.Transport)); t != "" {
			if t != "MQTT" && t != "HTTP" {
				return b, result, invalid("标准上报方式只能是 MQTT 或 HTTP")
			}
			connector = t
		}
	}
	if plan.Mode == ModeStandard {
		// Standard reports are parsed by the tenant's standard release; create it
		// with the device when the tenant has none yet.
		b.Release = &release
	}
	if (plan.Mode == ModeListener || plan.Mode == ModePoll) && q.NewProduct == nil {
		// Connections are checked against a stored protocol version and binding.
		if _, err = s.Repo.GetProtocolRelease(ctx, tenant, release.ProtocolID, release.Version); err != nil {
			if errors.Is(err, model.ErrNotFound) {
				return b, result, invalid("模板的协议没有可用版本，请先在设备模板中绑定已发布的协议版本")
			}
			return b, result, err
		}
		if _, err = s.Repo.GetProductProtocolBinding(ctx, tenant, product.ID); errors.Is(err, model.ErrNotFound) {
			b.Binding = &model.ProductProtocolBinding{TenantID: tenant, ProductID: product.ID, ProtocolID: release.ProtocolID, Version: release.Version, UpdatedAt: now}
		} else if err != nil {
			return b, result, err
		}
		b.Release = &release
	}
	if q.NewProduct != nil {
		transport := strings.ToUpper(product.Transport)
		switch {
		case plan.Mode == ModeStandard:
			product.Transport = connector
		case strings.EqualFold(release.Transport, "TCP_UDP") && (transport == "TCP" || transport == "UDP"):
			// A dual-network protocol may be narrowed to one network per template.
		default:
			product.Transport = strings.ToUpper(release.Transport)
		}
		product.PayloadFormat = release.PayloadFormat
		product.ProtocolPackageID = StandardPackageID
		if plan.Mode != ModeStandard {
			product.ProtocolPackageID = release.ProtocolID + "@" + release.Version
			b.Binding = &model.ProductProtocolBinding{TenantID: tenant, ProductID: product.ID, ProtocolID: release.ProtocolID, Version: release.Version, UpdatedAt: now}
		}
		b.Product, b.Release = &product, &release
	}
	role := q.Device.DeviceRole
	switch role {
	case "":
		role = "DIRECT"
		if product.Category == "gateway" {
			role = "GATEWAY"
		}
	case "DIRECT", "GATEWAY":
	default:
		return b, result, invalid("设备角色只能是独立设备或主设备；子设备请从主设备详情中添加")
	}
	b.Device = model.ManagedDevice{ID: q.Device.ID, TenantID: tenant, ProductID: product.ID, Name: q.Device.Name, Status: "ENABLED", DeviceRole: role, RegistrationSource: "ONBOARDING", Description: q.Device.Description, Tags: q.Device.Tags, CreatedAt: now, UpdatedAt: now}
	result.Product, result.Mode = product, plan.Mode
	switch plan.Mode {
	case ModeStandard, ModeManaged:
		if c.Mode != plan.Mode {
			return b, result, invalid("该模板的接入方式与所选连接方式不一致，请返回上一步重新选择")
		}
		if plan.Mode == ModeStandard {
			b.Device.Tags["connector"] = connector
		}
	case ModeListener:
		profile, reuse, err := s.listenerProfile(ctx, tenant, product, plan, release, b.Device.ID, c, now)
		if err != nil {
			return b, result, err
		}
		b.Profile, b.ReuseProfile = &profile, reuse
		b.Device.Tags["connectorProfileId"] = profile.ID
		b.Device.Tags["connector"] = strings.ToUpper(profile.Network)
		result.Profile = &profile
		if profile.ConnectionMode == "dial" {
			result.Mode = "dial"
		}
	case ModePoll:
		if c.Mode != ModePoll {
			return b, result, invalid("该模板由平台定时读取，请填写设备地址、端口和站号")
		}
		profile, err := s.targetProfile(ctx, tenant, product, release, b.Device.ID, c, now, "poll")
		if err != nil {
			return b, result, err
		}
		if plan.Connector == "MODBUS_RTU_TCP" {
			profile.WireFormat = "rtu_over_tcp"
		}
		if err = model.ValidateProtocolAccess(profile); err != nil {
			return b, result, invalid(err.Error())
		}
		b.Profile = &profile
		b.Device.Tags["connectorProfileId"] = profile.ID
		b.Device.Tags["connector"] = plan.Connector
		result.Profile = &profile
	}
	return b, result, nil
}

func (s *Service) listenerProfile(ctx context.Context, tenant string, product model.Product, plan AccessPlan, release model.ProtocolRelease, deviceID string, c EnrollConnection, now int64) (model.DeviceAccessProfile, bool, error) {
	switch {
	case c.Mode == "dial":
		if !plan.Dial {
			return model.DeviceAccessProfile{}, false, invalid("该协议不支持由平台主动连接设备")
		}
		profile, err := s.targetProfile(ctx, tenant, product, release, deviceID, c, now, "dial")
		if err != nil {
			return profile, false, err
		}
		if err = model.ValidateProtocolAccess(profile); err != nil {
			return profile, false, invalid(err.Error())
		}
		return profile, false, nil
	case c.Mode != ModeListener:
		return model.DeviceAccessProfile{}, false, invalid("请选择设备连接平台的监听，或由平台主动连接设备")
	case c.ProfileID != "":
		profile, err := s.Repo.GetDeviceAccessProfile(ctx, tenant, c.ProfileID)
		if errors.Is(err, model.ErrNotFound) {
			return profile, false, invalid("所选接入点已不存在，请刷新后重新选择")
		}
		if err != nil {
			return profile, false, err
		}
		if profile.ProductID != product.ID || profile.Mode != "listener" || profile.ConnectionMode == "dial" || profile.DeviceID != "" || profile.EdgeNodeID != "" || !hasNetwork(plan.Networks, profile.Network) {
			return profile, false, invalid("所选接入点不属于该模板或不是共享监听")
		}
		if !profile.Enabled {
			return profile, false, invalid("所选接入点未启用，请先在设备模板的接入点中启用")
		}
		// Listeners parse with the template's bound version, not the version stored
		// when the listener was created.
		profile.ProtocolID, profile.ProtocolVersion = release.ProtocolID, release.Version
		return profile, true, nil
	case c.Listener != nil:
		l := c.Listener
		network := strings.ToLower(strings.TrimSpace(l.Network))
		if network == "" {
			network = plan.Networks[0]
		}
		if !hasNetwork(plan.Networks, network) {
			return model.DeviceAccessProfile{}, false, invalid("该协议不支持所选的网络类型")
		}
		host := strings.TrimSpace(l.Host)
		if host == "" {
			host = "0.0.0.0"
		}
		if net.ParseIP(host) == nil {
			return model.DeviceAccessProfile{}, false, invalid("本机监听地址须为 IP，例如 0.0.0.0")
		}
		if l.Port < 1 || l.Port > 65535 {
			return model.DeviceAccessProfile{}, false, invalid("请填写 1 至 65535 之间的监听端口")
		}
		profile := model.DeviceAccessProfile{ID: fmt.Sprintf("%s-%s-%d", product.ID, network, l.Port), TenantID: tenant, ProductID: product.ID, ProtocolID: release.ProtocolID, ProtocolVersion: release.Version, Mode: "listener", Network: network, ConnectionMode: "listen", Host: host, PublicHost: strings.TrimSpace(l.PublicHost), Port: l.Port, TimeoutMs: 5000, Enabled: true, RuntimeStatus: "PENDING", CreatedAt: now, UpdatedAt: now}
		if len(profile.ID) > 128 || !segment.MatchString(profile.ID) {
			profile.ID = fmt.Sprintf("listener-%s-%d", Hash(tenant + "/" + product.ID)[:16], l.Port)
		}
		if profile.PublicHost == "" {
			return profile, false, invalid("请填写现场设备可访问的平台对外地址")
		}
		if err := model.ValidateProtocolAccess(profile); err != nil {
			return profile, false, invalid(err.Error())
		}
		if _, err := s.Repo.GetDeviceAccessProfile(ctx, tenant, profile.ID); err == nil {
			return profile, false, conflict("接入点标识已存在，请直接选择该接入点")
		}
		return profile, false, nil
	default:
		return model.DeviceAccessProfile{}, false, invalid("请选择一个共享监听，或新建监听")
	}
}

// targetProfile creates the per-device connection that the platform opens to
// the device (TCP dial or Modbus polling).
func (s *Service) targetProfile(ctx context.Context, tenant string, product model.Product, release model.ProtocolRelease, deviceID string, c EnrollConnection, now int64, mode string) (model.DeviceAccessProfile, error) {
	host := strings.TrimSpace(c.Host)
	if host == "" || len(host) > 253 || strings.ContainsAny(host, "/@?# ") {
		return model.DeviceAccessProfile{}, invalid("请填写平台可以访问的设备 IP 或域名")
	}
	port := c.Port
	if port == 0 && mode == "poll" {
		port = 502
	}
	if port < 1 || port > 65535 {
		return model.DeviceAccessProfile{}, invalid("请填写 1 至 65535 之间的设备端口")
	}
	timeout := c.TimeoutMs
	if timeout == 0 {
		timeout = 3000
	}
	if timeout < 100 || timeout > 10000 {
		return model.DeviceAccessProfile{}, invalid("连接超时须在 100 至 10000 毫秒之间")
	}
	if ip := net.ParseIP(host); ip != nil {
		// Hostnames are resolved again by the runtime; literal addresses can be checked now.
		checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		_, err := protocolruntime.ResolveAllowedTarget(checkCtx, host, s.AllowedCIDRs)
		cancel()
		if err != nil {
			return model.DeviceAccessProfile{}, invalid("设备地址不在平台允许访问的网段内，请联系管理员调整 IOT_MODBUS_ALLOWED_CIDRS")
		}
	}
	profile := model.DeviceAccessProfile{ID: "onboard-" + Hash(tenant + "/" + deviceID)[:24], TenantID: tenant, ProductID: product.ID, DeviceID: deviceID, ProtocolID: release.ProtocolID, ProtocolVersion: release.Version, PointTableVersion: release.PointTableVersion, Network: "tcp", Host: host, Port: port, TimeoutMs: timeout, Enabled: true, RuntimeStatus: "PENDING", CreatedAt: now, UpdatedAt: now}
	if mode == "poll" {
		profile.Mode = "poll"
		unit := 1
		if c.UnitID != nil {
			unit = *c.UnitID
		}
		if unit < 0 || unit > 255 {
			return profile, invalid("站号须在 0 至 255 之间")
		}
		profile.UnitID = unit
	} else {
		profile.Mode, profile.ConnectionMode = "listener", "dial"
	}
	if _, err := s.Repo.GetDeviceAccessProfile(ctx, tenant, profile.ID); err == nil {
		return profile, conflict("该设备已有平台连接，请在设备详情中查看")
	}
	return profile, nil
}
