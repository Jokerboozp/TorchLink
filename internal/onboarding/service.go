package onboarding

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"iot-platform/internal/connector"
	"iot-platform/internal/core"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
	"iot-platform/internal/protocolruntime"
	"iot-platform/internal/protocolworker"
)

var segment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
var ErrAuth = errors.New("invalid or disabled device credential")
var ErrRate = errors.New("device rate limit exceeded")
var testSlots = make(chan struct{}, 8)

type Request struct {
	ReadPoints        []model.PollPoint         `json:"readPoints,omitempty"`
	ExistingProfileID string                    `json:"existingProfileId,omitempty"`
	ProductName       string                    `json:"productName,omitempty"`
	TestToken         string                    `json:"testToken,omitempty"`
	ProductID         string                    `json:"productId"`
	DeviceID          string                    `json:"deviceId"`
	Name              string                    `json:"name"`
	Type              connector.Type            `json:"type"`
	Profile           model.DeviceAccessProfile `json:"profile"`
	ProtocolID        string                    `json:"protocolId,omitempty"`
	ProtocolVersion   string                    `json:"protocolVersion,omitempty"`
	PollIntervalSec   int                       `json:"pollIntervalSec,omitempty"`
	PointTableCSV     string                    `json:"pointTableCsv,omitempty"`
	Payload           json.RawMessage           `json:"payload"`
	MessageKind       string                    `json:"messageKind"`
}
type Result struct {
	AccessInfo map[string]any         `json:"accessInfo,omitempty"`
	Reused     bool                   `json:"reused"`
	Device     model.ManagedDevice    `json:"device"`
	Credential model.DeviceCredential `json:"credential"`
	ClientID   string                 `json:"clientId"`
	Username   string                 `json:"username"`
	Connector  connector.Instance     `json:"connector"`
}
type bucket struct {
	At    time.Time
	Count int
}
type Service struct {
	RemoteRead     func(context.Context, model.DeviceAccessProfile, model.ProtocolRelease) ([]model.RawMessage, error)
	RevokeUsername func(context.Context, string) error
	PublishCommand func(context.Context, string, []byte, byte, bool) error
	Repo           ports.Repository
	Parsers        *parser.Registry
	Root           string
	AllowedCIDRs   []string
	mu             sync.Mutex
	rates          map[string]bucket
	proofKey       []byte
	ListenerStatus func(string, string) (string, string, int64)
	MQTTHealth     func(context.Context) error
}

func New(repo ports.Repository, p *parser.Registry, root string, cidrs []string, signingKey ...string) *Service {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic(err)
	}
	if len(signingKey) > 0 && signingKey[0] != "" {
		key = []byte(signingKey[0])
	}
	return &Service{Repo: repo, Parsers: p, Root: root, AllowedCIDRs: cidrs, rates: map[string]bucket{}, proofKey: key}
}

func (s *Service) proof(tenant string, q Request, rel model.ProtocolRelease, profile *model.DeviceAccessProfile, expiry string) string {
	q.TestToken = ""
	rel.CreatedAt = 0
	rel.PublishedAt = 0
	var transport any
	if profile != nil {
		transport = map[string]any{"id": profile.ID, "host": profile.Host, "port": profile.Port, "network": profile.Network, "timeoutMs": profile.TimeoutMs, "unitId": profile.UnitID, "autoRegister": profile.AutoRegister, "collectorId": profile.CollectorID}
	}
	data, _ := json.Marshal([]any{tenant, q, rel, transport, expiry})
	mac := hmac.New(sha256.New, s.proofKey)
	_, _ = mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil))
}
func Hash(secret string) string { h := sha256.Sum256([]byte(secret)); return hex.EncodeToString(h[:]) }
func Credential() (model.DeviceCredential, error) {
	b := make([]byte, 40)
	if _, err := rand.Read(b); err != nil {
		return model.DeviceCredential{}, err
	}
	return model.DeviceCredential{AccessKey: "dk_" + hex.EncodeToString(b[:8]), Secret: "ds_" + hex.EncodeToString(b[8:])}, nil
}
func (s *Service) Authenticate(ctx context.Context, key, secret string) (model.ManagedDevice, error) {
	d, err := s.Repo.GetManagedDeviceByAccessKey(ctx, key)
	if err != nil || secret == "" || d.Status != "ENABLED" || !hmac.Equal([]byte(d.SecretHash), []byte(Hash(secret))) {
		return model.ManagedDevice{}, ErrAuth
	}
	return d, nil
}
func (s *Service) Allow(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	b := s.rates[key]
	if now.Sub(b.At) >= time.Second {
		b = bucket{At: now}
	}
	if b.Count >= 20 {
		return false
	}
	if len(s.rates) >= 10000 {
		for k, v := range s.rates {
			if now.Sub(v.At) > time.Minute {
				delete(s.rates, k)
			}
		}
		if _, ok := s.rates[key]; !ok && len(s.rates) >= 10000 {
			return false
		}
	}
	b.Count++
	s.rates[key] = b
	return true
}

// StandardRaw never accepts tenant/device identity or parser metadata from a payload.
func StandardRaw(tenant, product, device, kind, transport string, payload []byte) (model.RawMessage, error) {
	r := model.RawMessage{TenantID: tenant, ProductID: product, DeviceID: device, Protocol: parser.StandardProtocolID, ProtocolID: parser.StandardProtocolID, ProtocolVersion: "1.0.0", Transport: transport, Source: "standard-" + strings.ToLower(transport), PayloadFormat: "json", Payload: append(json.RawMessage(nil), payload...), Headers: map[string]string{"messageKind": kind}}
	if !segment.MatchString(tenant) || !segment.MatchString(product) || !segment.MatchString(device) || len(payload) > 64<<10 {
		return r, errors.New("invalid identity or payload exceeds 64 KiB")
	}
	r.Normalize(time.Now())
	if _, err := (parser.StandardParser{}).Parse(r); err != nil {
		return r, err
	}
	var body struct {
		ID        string `json:"id"`
		Timestamp int64  `json:"timestamp"`
	}
	_ = json.Unmarshal(payload, &body)
	r.Metadata = map[string]any{"clientMessageId": body.ID, "deviceTimestamp": body.Timestamp}
	r.MessageID = "raw_std_" + Hash(tenant + "\x00" + product + "\x00" + device + "\x00" + kind + "\x00" + body.ID)[:32]
	return r, nil
}
func (s *Service) PrepareStandard(ctx context.Context, tenant, product, device, kind, transport string, payload []byte) (model.RawMessage, error) {
	d, err := s.Repo.GetManagedDevice(ctx, tenant, device)
	if err != nil || d.Status != "ENABLED" || d.SecretHash == "" || d.ProductID != product {
		return model.RawMessage{}, ErrAuth
	}
	if d.Tags["connector"] != "MQTT" && d.Tags["connector"] != "HTTP" {
		return model.RawMessage{}, ErrAuth
	}
	p, err := s.Repo.GetProduct(ctx, tenant, product)
	if err != nil || p.Status != "ENABLED" {
		return model.RawMessage{}, ErrAuth
	}
	if !s.Allow(tenant + "\x00" + device) {
		return model.RawMessage{}, ErrRate
	}
	return StandardRaw(tenant, product, device, kind, transport, payload)
}

func (s *Service) plan(ctx context.Context, tenant string, q Request) (model.OnboardingBundle, model.ProtocolRelease, error) {
	var b model.OnboardingBundle
	var rel model.ProtocolRelease
	fail := func(msg string) (model.OnboardingBundle, model.ProtocolRelease, error) {
		return b, rel, errors.New(msg)
	}
	if !segment.MatchString(tenant) || !segment.MatchString(q.DeviceID) || strings.TrimSpace(q.Name) == "" || len(q.Name) > 256 {
		return fail("请填写有效设备标识和名称")
	}
	if q.Profile.EdgeNodeID != "" {
		edge, e := s.Repo.GetEdgeNode(ctx, tenant, q.Profile.EdgeNodeID)
		if e != nil || edge.Status != "ENABLED" {
			return fail("Edge 节点不存在或已停用")
		}
		if s.RemoteRead == nil || (q.Type != connector.ModbusTCP && q.Type != connector.ModbusRTU && q.Type != connector.OPCUA && q.Type != connector.SNMP && q.Type != connector.BACnet && q.Type != connector.TCP && q.Type != connector.UDP) {
			return fail("该 Edge 接入类型尚无可用的现场读取执行器")
		}
	}
	product, err := s.Repo.GetProduct(ctx, tenant, q.ProductID)
	if q.ProductName != "" {
		if !segment.MatchString(q.ProductID) || len(q.ProductName) > 256 {
			return fail("新产品标识或名称无效")
		}
		product = model.Product{ID: q.ProductID, TenantID: tenant, Name: q.ProductName, Status: "ENABLED", Category: "sensor", CreatedAt: time.Now().UnixMilli(), UpdatedAt: time.Now().UnixMilli()}
		b.Product = &product
	} else if err != nil || product.Status != "ENABLED" {
		return fail("请选择已启用的产品")
	}
	now := time.Now().UnixMilli()
	b.Device = model.ManagedDevice{ID: q.DeviceID, TenantID: tenant, ProductID: q.ProductID, Name: q.Name, Status: "ENABLED", DeviceRole: "DIRECT", RegistrationSource: "ONBOARDING", Tags: map[string]string{"connector": string(q.Type)}, CreatedAt: now, UpdatedAt: now}
	binding, bindErr := s.Repo.GetProductProtocolBinding(ctx, tenant, q.ProductID)
	switch q.Type {
	case connector.MQTT, connector.HTTP:
		rel = model.ProtocolRelease{TenantID: tenant, ProtocolID: parser.StandardProtocolID, Version: "1.0.0", Transport: "MQTT_HTTP", PayloadFormat: "json", ParserType: parser.StandardParserName, Status: "PUBLISHED", CreatedAt: now, PublishedAt: now}
		// The standard ingress pins its release explicitly without changing a
		// product's existing custom protocol binding.
	case connector.ModbusTCP, connector.ModbusRTU, connector.OPCUA, connector.SNMP, connector.BACnet, connector.TCP, connector.UDP:
		if bindErr == nil {
			if q.ProtocolID != "" && (q.ProtocolID != binding.ProtocolID || q.ProtocolVersion != binding.Version) {
				return fail("所选协议与产品现有绑定不一致，请在协议管理中变更绑定")
			}
			q.ProtocolID, q.ProtocolVersion = binding.ProtocolID, binding.Version
		}
		if q.ProtocolID != "" {
			rel, err = s.Repo.GetProtocolRelease(ctx, tenant, q.ProtocolID, q.ProtocolVersion)
			if err != nil || rel.Status != "PUBLISHED" {
				return fail("请选择已发布且兼容的协议")
			}
		} else if q.Type == connector.OPCUA || (q.Type == connector.SNMP || q.Type == connector.BACnet) {
			interval := q.PollIntervalSec
			if interval == 0 {
				interval = 10
			}
			if interval < 1 || interval > 3600 {
				return fail("采集周期须为 1 至 3600 秒")
			}
			config := map[string]any{"reads": q.ReadPoints, "pollIntervalSec": interval}
			if _, e := parser.PollPoints(config); e != nil {
				return b, rel, e
			}
			encoded, _ := json.Marshal(config)
			rel = model.ProtocolRelease{TenantID: tenant, ProtocolID: "onboard-" + Hash(tenant + "/" + q.ProductID)[:20], Version: Hash(string(q.Type) + string(encoded))[:16], Transport: string(q.Type), PayloadFormat: "json", ParserType: parser.PollResponseParserName, Status: "PUBLISHED", CreatedAt: now, PublishedAt: now, Config: config}
		} else if q.Type == connector.ModbusTCP || q.Type == connector.ModbusRTU {
			interval := q.PollIntervalSec
			if interval == 0 {
				interval = 10
			}
			if interval < 1 || interval > 3600 {
				return fail("采集周期须为 1 至 3600 秒")
			}
			table, _, e := core.ParseModbusPointTable("points.csv", []byte(q.PointTableCSV), interval)
			if e != nil {
				return b, rel, e
			}
			blocks, e := core.CompileModbusReadBlocks(table.Points)
			if e != nil {
				return b, rel, e
			}
			rel = model.ProtocolRelease{TenantID: tenant, ProtocolID: "onboard-" + Hash(tenant + "/" + q.ProductID)[:20], Version: Hash(fmt.Sprintf("%d\x00%s", interval, q.PointTableCSV))[:16], Transport: "MODBUS_TCP", PayloadFormat: "hex", ParserType: parser.ModbusTCPParserName, Status: "PUBLISHED", CreatedAt: now, PublishedAt: now, Config: map[string]any{"points": table.Points, "blocks": blocks}}
			rel.PointTableVersion = rel.Version
			if q.Type == connector.ModbusRTU {
				rel.Transport, rel.ParserType = "MODBUS_RTU", parser.ModbusRTUParserName
			}
			table.TenantID, table.ProtocolID, table.Version, table.CreatedAt = tenant, rel.ProtocolID, rel.Version, now
			b.PointTable = &table
		} else {
			return fail("该产品尚未绑定通信协议，请在高级设置选择已发布协议")
		}
		p := q.Profile
		p.TenantID, p.ProductID, p.DeviceID = tenant, q.ProductID, q.DeviceID
		p.ID = "onboard-" + Hash(tenant + "/" + q.DeviceID)[:24]
		p.ProtocolID, p.ProtocolVersion, p.PointTableVersion = rel.ProtocolID, rel.Version, rel.PointTableVersion
		p.Enabled = true
		p.CreatedAt, p.UpdatedAt = now, now
		p.RuntimeStatus = "PENDING"
		p.LastError = ""
		p.LastSuccessAt = 0
		p.LastErrorAt = 0
		if p.TimeoutMs == 0 {
			p.TimeoutMs = 3000
		}
		if p.TimeoutMs < 1 || p.TimeoutMs > 10000 || p.Retries < 0 || p.Retries > 3 || (q.Type != connector.ModbusRTU && (p.Port < 1 || p.Port > 65535)) {
			return fail("端口、超时或重试参数无效")
		}
		if q.Type == connector.OPCUA || (q.Type == connector.SNMP || q.Type == connector.BACnet) {
			p.Mode, p.Network = "poll", strings.ToLower(string(q.Type))
			if p.EdgeNodeID == "" || p.Host == "" || !segment.MatchString(p.CredentialRef) || rel.Transport != string(q.Type) || rel.ParserType != parser.PollResponseParserName {
				return fail("请选择现场节点、设备地址、本地凭据引用及兼容读取协议")
			}
		} else if q.Type == connector.ModbusRTU {
			p.Mode, p.Network = "poll", "serial"
			if p.EdgeNodeID == "" || rel.Transport != "MODBUS_RTU" {
				return fail("RTU 需要选择现场 Edge 节点和兼容协议")
			}
			if err := protocolruntime.ValidateSerialProfile(p); err != nil {
				return b, rel, err
			}
		} else if q.Type == connector.ModbusTCP {
			p.Mode = "poll"
			p.Network = "tcp"
			if p.Host == "" || p.UnitID < 0 || p.UnitID > 255 || rel.Transport != "MODBUS_TCP" {
				return fail("Modbus 地址、站号或协议无效")
			}
		} else {
			p.Mode = "listener"
			p.Network = strings.ToLower(string(q.Type))
			p.DeviceID = ""
			if net.ParseIP(p.Host) == nil || rel.ParserType != parser.GoProtocolParserName || rel.Artifact["runtime"] != protocolworker.Runtime || !protocolworker.HasCapability(rel, "ingress") || (rel.Transport != string(q.Type) && rel.Transport != "TCP_UDP") {
				return fail("监听地址或协议不兼容，需要支持 ingress 的 TCP/UDP 协议")
			}
		}
		b.Profile = &p
		if q.ExistingProfileID != "" {
			old, e := s.Repo.GetDeviceAccessProfile(ctx, tenant, q.ExistingProfileID)
			if e != nil || !old.Enabled || old.Mode != "listener" || old.Network != p.Network || old.ProductID != q.ProductID || q.Type == connector.ModbusTCP || q.Type == connector.ModbusRTU {
				return fail("已有监听实例不可用或不属于当前产品")
			}
			old.ProtocolID, old.ProtocolVersion = rel.ProtocolID, rel.Version
			b.Profile = &old
			p = old
			b.ReuseProfile = true
		}
		b.Device.Tags["connectorProfileId"] = p.ID
		if bindErr != nil {
			if b.Product == nil {
				devices, e := s.Repo.ListManagedDevices(ctx, tenant)
				if e != nil {
					return b, rel, e
				}
				for _, device := range devices {
					if device.ProductID == q.ProductID {
						return fail("该产品已有设备且使用旧协议配置，请选择已绑定产品或在向导中新建产品，避免改变已有设备的解析")
					}
				}
			}
			b.Binding = &model.ProductProtocolBinding{TenantID: tenant, ProductID: q.ProductID, ProtocolID: rel.ProtocolID, Version: rel.Version, UpdatedAt: now}
		}
	default:
		return fail("该接入方式尚未开放；视频设备请使用现有摄像头管理")
	}
	b.Release = &rel
	if b.Product != nil {
		b.Product.Transport = string(q.Type)
		b.Product.PayloadFormat = rel.PayloadFormat
		b.Product.ProtocolPackageID = rel.ProtocolID + "@" + rel.Version
	}
	return b, rel, nil
}
func (s *Service) Create(ctx context.Context, tenant string, q Request) (Result, error) {
	// Device ID is the tenant-scoped idempotency key. The digest excludes the
	// expiring preview proof, allowing recovery after a lost HTTP response.
	request := q
	request.TestToken = ""
	encoded, err := json.Marshal(request)
	if err != nil {
		return Result{}, err
	}
	digest := Hash(string(encoded))
	recover := func() (Result, error) {
		d, err := s.Repo.GetManagedDevice(ctx, tenant, q.DeviceID)
		if err != nil {
			return Result{}, err
		}
		if d.Tags["onboardingRequestHash"] != digest {
			return Result{}, errors.New("设备标识已存在且接入请求不同")
		}
		instance := connector.Instance{Type: connector.Type(d.Tags["connector"]), DeviceID: d.ID}
		if id := d.Tags["connectorProfileId"]; id != "" {
			p, err := s.Repo.GetDeviceAccessProfile(ctx, tenant, id)
			if err != nil {
				return Result{}, err
			}
			instance.Profile = &p
		}
		return Result{Reused: true, Device: d, ClientID: "device-" + d.AccessKey, Username: d.AccessKey, Connector: instance}, nil
	}
	if _, err := s.Repo.GetManagedDevice(ctx, tenant, q.DeviceID); err == nil {
		return recover()
	}
	b, rel, err := s.plan(ctx, tenant, q)
	if err != nil {
		return Result{}, err
	}
	parts := strings.Split(q.TestToken, ".")
	if len(parts) != 2 {
		return Result{}, errors.New("请先完成接入测试")
	}
	expiry, e := strconv.ParseInt(parts[0], 10, 64)
	if e != nil || expiry < time.Now().Unix() || !hmac.Equal([]byte(parts[1]), []byte(s.proof(tenant, q, rel, b.Profile, parts[0]))) {
		return Result{}, errors.New("接入配置已变化或测试过期，请重新测试")
	}
	credential, err := Credential()
	if err != nil {
		return Result{}, err
	}
	b.Device.AccessKey = credential.AccessKey
	b.Device.SecretHash = Hash(credential.Secret)
	b.Device.Tags["onboardingRequestHash"] = digest
	if err = s.Repo.SaveOnboarding(ctx, b); err != nil {
		// A competing request may have committed while this request was planning.
		if recovered, retryErr := recover(); retryErr == nil {
			return recovered, nil
		}
		return Result{}, err
	}
	return Result{Device: b.Device, Credential: credential, ClientID: "device-" + credential.AccessKey, Username: credential.AccessKey, Connector: connector.Instance{Type: q.Type, DeviceID: q.DeviceID, Profile: b.Profile}}, nil
}
func (s *Service) Test(ctx context.Context, tenant string, q Request) (result *connector.Result, resultErr error) {
	started := time.Now()
	defer func() {
		if result == nil {
			result = &connector.Result{Stage: "validate", Message: "接入配置校验失败", ErrorCode: "PROTOCOL_ERROR"}
			if resultErr != nil {
				result.Message = resultErr.Error()
				result.ErrorCode = connector.ErrorCode(resultErr, "PROTOCOL_ERROR")
			}
		}
		result.LatencyMs = time.Since(started).Milliseconds()
		if result.DeviceID == "" {
			result.DeviceID = q.DeviceID
		}
		if result.ProtocolID == "" {
			result.ProtocolID = q.ProtocolID
		}
		if result.ProtocolVersion == "" {
			result.ProtocolVersion = q.ProtocolVersion
		}
	}()
	if !connector.Describe(q.Type).Supported {
		return &connector.Result{Stage: "unsupported", ErrorCode: "UNSUPPORTED", Message: "接入方式尚未实现"}, connector.ErrUnsupported
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case testSlots <- struct{}{}:
		defer func() { <-testSlots }()
	default:
		return &connector.Result{Stage: "busy", ErrorCode: "BUSY", Message: "接入测试并发已达上限，请稍后重试"}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	b, rel, err := s.plan(ctx, tenant, q)
	if err != nil {
		return nil, err
	}
	if q.Type == connector.MQTT {
		if s.MQTTHealth == nil {
			return &connector.Result{Stage: "broker", ErrorCode: "NETWORK_ERROR", Message: "平台 MQTT 连接未启动，可先使用 HTTP 接入", ProtocolID: rel.ProtocolID, ProtocolVersion: rel.Version, Parser: rel.ParserType}, nil
		}
		healthCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := s.MQTTHealth(healthCtx)
		cancel()
		if err != nil {
			return &connector.Result{Stage: "broker", ErrorCode: connector.ErrorCode(err, "NETWORK_ERROR"), Message: err.Error(), ProtocolID: rel.ProtocolID, ProtocolVersion: rel.Version, Parser: rel.ParserType}, nil
		}
	}
	r := connector.Request{Type: q.Type, Release: rel, Reuse: b.ReuseProfile}
	if b.Profile != nil {
		r.Profile = *b.Profile
	}
	r.Raw = model.RawMessage{TenantID: tenant, ProductID: q.ProductID, DeviceID: q.DeviceID, Protocol: rel.ProtocolID, Transport: string(q.Type), ProtocolID: rel.ProtocolID, ProtocolVersion: rel.Version, PayloadFormat: rel.PayloadFormat, Payload: q.Payload, ReceivedAt: time.Now().UnixMilli()}
	r.Raw.Normalize(time.Now())
	if q.Type == connector.MQTT || q.Type == connector.HTTP {
		r.Raw, err = StandardRaw(tenant, q.ProductID, q.DeviceID, q.MessageKind, string(q.Type), q.Payload)
		if err != nil {
			return &connector.Result{Stage: "parse", ErrorCode: "PARSE_FAILED", Message: err.Error(), Raw: []model.RawMessage{r.Raw}, ProtocolID: rel.ProtocolID, ProtocolVersion: rel.Version, Parser: rel.ParserType}, nil
		}
	}
	a := connector.Adapter{Kind: q.Type, Probe: s.probe}
	result, err = a.Test(ctx, r)
	if err == nil && result.Success {
		expiry := strconv.FormatInt(time.Now().Add(10*time.Minute).Unix(), 10)
		result.TestToken = expiry + "." + s.proof(tenant, q, rel, b.Profile, expiry)
	}
	return result, err
}
func (s *Service) probe(ctx context.Context, q connector.Request) (*connector.Result, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	r := &connector.Result{Source: "sample", Stage: "sample", ProtocolID: q.Release.ProtocolID, ProtocolVersion: q.Release.Version, Parser: q.Release.ParserType, PointTable: q.Release.Config["points"]}
	finish := func(err error, code string) (*connector.Result, error) {
		r.LatencyMs = time.Since(start).Milliseconds()
		r.ErrorCode = code
		if err != nil {
			r.Message = err.Error()
			r.ErrorCode = connector.ErrorCode(err, code)
		}
		return r, nil
	}
	raws := []model.RawMessage{q.Raw}
	switch q.Type {
	case connector.ModbusTCP, connector.ModbusRTU, connector.OPCUA, connector.SNMP, connector.BACnet:
		r.Stage = "read"
		r.Source = "network-read"
		var blocks []model.ModbusReadBlock
		data, _ := json.Marshal(q.Release.Config["blocks"])
		if err := json.Unmarshal(data, &blocks); (err != nil || len(blocks) == 0) && (q.Type == connector.ModbusTCP || q.Type == connector.ModbusRTU) {
			return finish(errors.New("协议没有读取计划"), "PROTOCOL_ERROR")
		}
		var err error
		if q.Profile.EdgeNodeID != "" && s.RemoteRead != nil {
			r.Source = "edge-read"
			raws, err = s.RemoteRead(ctx, q.Profile, q.Release)
		} else {
			raws, err = protocolruntime.ReadModbusTCPWithPolicy(ctx, q.Profile, q.Release, blocks, s.AllowedCIDRs)
		}
		if err != nil {
			var wire *protocolruntime.ModbusReadError
			code := "NETWORK_ERROR"
			if errors.As(err, &wire) {
				r.RawRequest = strings.ToUpper(hex.EncodeToString(wire.Request))
				r.RawResponse = strings.ToUpper(hex.EncodeToString(wire.Response))
				var n net.Error
				if !errors.As(err, &n) {
					code = "PROTOCOL_ERROR"
				}
			}
			var exception *protocolruntime.ModbusException
			if errors.As(err, &exception) {
				r.ExceptionCode = int(exception.Code)
				code = "PROTOCOL_ERROR"
			}
			return finish(err, code)
		}
		if len(raws) == 0 {
			return finish(errors.New("设备未返回报文"), "PROTOCOL_ERROR")
		}
		r.RawRequest = fmt.Sprint(raws[0].Metadata["requestHex"])
		r.RawResponse = string(raws[0].Payload)
	case connector.TCP, connector.UDP:
		r.Stage = "listener-check"
		addr := net.JoinHostPort(q.Profile.Host, fmt.Sprint(q.Profile.Port))
		var closer interface{ Close() error }
		var err error
		if q.Profile.EdgeNodeID != "" {
			// This step validates the sample only. Node bind/worker checks are
			// reported through the runtime heartbeat after saving the profile.
			r.Stage = "edge-listener-sample"
		} else if q.Reuse {
			if s.ListenerStatus == nil {
				return finish(errors.New("监听运行时未启动"), "NETWORK_ERROR")
			}
			status, message, _ := s.ListenerStatus(q.Profile.TenantID, q.Profile.ID)
			if status != "LISTENING" {
				return finish(fmt.Errorf("监听实例 %s: %s", status, message), "NETWORK_ERROR")
			}
		} else if q.Type == connector.TCP {
			closer, err = net.Listen("tcp", addr)
		} else {
			closer, err = net.ListenPacket("udp", addr)
		}
		if err != nil {
			return finish(err, "NETWORK_ERROR")
		}
		if closer != nil {
			_ = closer.Close()
		}
		var frame string
		if json.Unmarshal(q.Raw.Payload, &frame) != nil {
			return finish(errors.New("请提供 HEX 字符串报文"), "PROTOCOL_ERROR")
		}
		response, err := protocolworker.Call(ctx, s.Root, q.Release, protocolworker.Request{Operation: "ingress", Data: frame, Now: time.Now().UnixMilli(), Raw: &q.Raw})
		if err != nil {
			return finish(err, "PROTOCOL_ERROR")
		}
		if response.NeedMore {
			return finish(errors.New("样例不是完整帧"), "PROTOCOL_ERROR")
		}
		if response.DeviceID != q.Raw.DeviceID {
			return finish(fmt.Errorf("报文设备标识 %s 与填写的设备标识不一致", response.DeviceID), "DEVICE_IDENTIFY_FAILED")
		}
		data, err := hex.DecodeString(frame)
		if err != nil {
			return finish(err, "PROTOCOL_ERROR")
		}
		if response.Consumed != len(data) {
			return finish(errors.New("请提供恰好一帧完整报文"), "PROTOCOL_ERROR")
		}
		r.RawResponse = response.Reply
	}
	r.Raw = raws
	r.Stage = "parse"
	mappings := []map[string]any{}
	parsed := []any{}
	for _, raw := range raws {
		m, err := s.Parsers.ParseWithConfig(q.Release.ParserType, q.Release.Config, raw)
		if err != nil {
			return finish(err, "PARSE_FAILED")
		}
		r.StandardMessages = append(r.StandardMessages, m)
		parsed = append(parsed, map[string]any{"properties": m.Properties, "event": m.Event, "tags": m.Tags})
		mappings = append(mappings, map[string]any{"messageType": m.MessageType, "properties": m.Properties, "event": m.Event, "alarm": m.MessageType == model.AlarmReport})
		r.DeviceID = m.DeviceID
	}
	r.Parsed = parsed
	r.Mapping = mappings
	r.Success = true
	r.Stage = "preview"
	r.Message = "样例解析通过；未向业务链路写入数据"
	if q.Type == connector.MQTT {
		r.Message = "平台 MQTT 连接健康，样例解析通过；尚未验证设备到 Broker 的网络和认证"
	}
	if q.Type == connector.ModbusTCP || q.Type == connector.ModbusRTU || q.Type == connector.OPCUA || (q.Type == connector.SNMP || q.Type == connector.BACnet) {
		r.Message = "目标设备读取与解析通过（设备或模拟器取决于配置）；测试数据未写入业务链路"
	}
	if q.Type == connector.TCP || q.Type == connector.UDP {
		r.Message = "本机端口可绑定，完整帧识别及解析通过；尚未验证设备到平台的网络"
		if q.Profile.EdgeNodeID != "" {
			r.Message = "完整帧样例识别及解析通过；现场监听启动、制品平台和设备连通情况须在保存后确认"
		}
	}
	return finish(nil, "SUCCESS")
}
