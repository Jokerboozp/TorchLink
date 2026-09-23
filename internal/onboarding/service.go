package onboarding /* 声明 onboarding 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"       /* 执行当前语句并推进处理流程。 */
	"crypto/hmac"   /* 执行当前语句并推进处理流程。 */
	"crypto/rand"   /* 执行当前语句并推进处理流程。 */
	"crypto/sha256" /* 执行当前语句并推进处理流程。 */
	"encoding/hex"  /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"errors"        /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */
	"net"           /* 执行当前语句并推进处理流程。 */
	"regexp"        /* 执行当前语句并推进处理流程。 */
	"strconv"       /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */
	"sync"          /* 执行当前语句并推进处理流程。 */
	"time"          /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/connector"       /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/core"            /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"           /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports"           /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/protocolruntime" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/protocolworker"  /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

var segment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`) /* 声明 segment。 */
var ErrAuth = errors.New("invalid or disabled device credential")      /* 声明 ErrAuth。 */
var ErrRate = errors.New("device rate limit exceeded")                 /* 声明 ErrRate。 */
var testSlots = make(chan struct{}, 8)                                 /* 声明 testSlots。 */

type Request struct { /* 定义 Request 类型。 */
	ReadPoints        []model.PollPoint         `json:"readPoints,omitempty"`        /* 执行当前语句并推进处理流程。 */
	ExistingProfileID string                    `json:"existingProfileId,omitempty"` /* 执行当前语句并推进处理流程。 */
	ProductName       string                    `json:"productName,omitempty"`       /* 执行当前语句并推进处理流程。 */
	TestToken         string                    `json:"testToken,omitempty"`         /* 执行当前语句并推进处理流程。 */
	ProductID         string                    `json:"productId"`                   /* 执行当前语句并推进处理流程。 */
	DeviceID          string                    `json:"deviceId"`                    /* 执行当前语句并推进处理流程。 */
	Name              string                    `json:"name"`                        /* 执行当前语句并推进处理流程。 */
	Type              connector.Type            `json:"type"`                        /* 执行当前语句并推进处理流程。 */
	Profile           model.DeviceAccessProfile `json:"profile"`                     /* 执行当前语句并推进处理流程。 */
	ProtocolID        string                    `json:"protocolId,omitempty"`        /* 执行当前语句并推进处理流程。 */
	ProtocolVersion   string                    `json:"protocolVersion,omitempty"`   /* 执行当前语句并推进处理流程。 */
	PollIntervalSec   int                       `json:"pollIntervalSec,omitempty"`   /* 执行当前语句并推进处理流程。 */
	PointTableCSV     string                    `json:"pointTableCsv,omitempty"`     /* 执行当前语句并推进处理流程。 */
	Payload           json.RawMessage           `json:"payload"`                     /* 执行当前语句并推进处理流程。 */
	MessageKind       string                    `json:"messageKind"`                 /* 执行当前语句并推进处理流程。 */
}                    /* 结束当前表达式或代码块。 */
type Result struct { /* 定义 Result 类型。 */
	AccessInfo map[string]any         `json:"accessInfo,omitempty"` /* 执行当前语句并推进处理流程。 */
	Reused     bool                   `json:"reused"`               /* 执行当前语句并推进处理流程。 */
	Device     model.ManagedDevice    `json:"device"`               /* 执行当前语句并推进处理流程。 */
	Credential model.DeviceCredential `json:"credential,omitzero"`  /* 执行当前语句并推进处理流程。 */
	ClientID   string                 `json:"clientId,omitempty"`   /* 执行当前语句并推进处理流程。 */
	Username   string                 `json:"username,omitempty"`   /* 执行当前语句并推进处理流程。 */
	Connector  connector.Instance     `json:"connector"`            /* 执行当前语句并推进处理流程。 */
}                    /* 结束当前表达式或代码块。 */
type bucket struct { /* 定义 bucket 类型。 */
	At    time.Time /* 执行当前语句并推进处理流程。 */
	Count int       /* 执行当前语句并推进处理流程。 */
}                     /* 结束当前表达式或代码块。 */
type Service struct { /* 定义 Service 类型。 */
	RevokeUsername func(context.Context, string) error                     /* 执行当前语句并推进处理流程。 */
	PublishCommand func(context.Context, string, []byte, byte, bool) error /* 执行当前语句并推进处理流程。 */
	Repo           ports.Repository                                        /* 执行当前语句并推进处理流程。 */
	Parsers        *parser.Registry                                        /* 执行当前语句并推进处理流程。 */
	Root           string                                                  /* 执行当前语句并推进处理流程。 */
	AllowedCIDRs   []string                                                /* 执行当前语句并推进处理流程。 */
	mu             sync.Mutex                                              /* 执行当前语句并推进处理流程。 */
	rates          map[string]bucket                                       /* 执行当前语句并推进处理流程。 */
	proofKey       []byte                                                  /* 执行当前语句并推进处理流程。 */
	ListenerStatus func(string, string) (string, string, int64)            /* 执行当前语句并推进处理流程。 */
	MQTTHealth     func(context.Context) error                             /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func New(repo ports.Repository, p *parser.Registry, root string, cidrs []string, signingKey ...string) *Service { /* 定义 New 函数。 */
	key := make([]byte, 32)                   /* 更新 key 的值。 */
	if _, err := rand.Read(key); err != nil { /* 判断条件并选择处理分支。 */
		panic(err) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if len(signingKey) > 0 && signingKey[0] != "" { /* 判断条件并选择处理分支。 */
		key = []byte(signingKey[0]) /* 更新 key 的值。 */
	} /* 结束当前表达式或代码块。 */
	return &Service{Repo: repo, Parsers: p, Root: root, AllowedCIDRs: cidrs, rates: map[string]bucket{}, proofKey: key} /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (s *Service) proof(tenant string, q Request, rel model.ProtocolRelease, profile *model.DeviceAccessProfile, expiry string) string { /* 定义 proof 函数。 */
	q.TestToken = ""    /* 更新 q.TestToken 的值。 */
	rel.CreatedAt = 0   /* 更新 rel.CreatedAt 的值。 */
	rel.PublishedAt = 0 /* 更新 rel.PublishedAt 的值。 */
	var transport any   /* 声明 transport。 */
	if profile != nil { /* 判断条件并选择处理分支。 */
		transport = map[string]any{"id": profile.ID, "host": profile.Host, "port": profile.Port, "network": profile.Network, "timeoutMs": profile.TimeoutMs, "unitId": profile.UnitID, "autoRegister": profile.AutoRegister, "collectorId": profile.CollectorID} /* 更新 transport 的值。 */
	} /* 结束当前表达式或代码块。 */
	data, _ := json.Marshal([]any{tenant, q, rel, transport, expiry}) /* 更新 _ 的值。 */
	mac := hmac.New(sha256.New, s.proofKey)                           /* 更新 mac 的值。 */
	_, _ = mac.Write(data)                                            /* 更新 _ 的值。 */
	return hex.EncodeToString(mac.Sum(nil))                           /* 返回当前处理结果。 */
}                               /* 结束当前表达式或代码块。 */
func Hash(secret string) string { h := sha256.Sum256([]byte(secret)); return hex.EncodeToString(h[:]) } /* 定义 Hash 函数。 */
func Credential() (model.DeviceCredential, error) { /* 定义 Credential 函数。 */
	b := make([]byte, 40)                   /* 更新 b 的值。 */
	if _, err := rand.Read(b); err != nil { /* 判断条件并选择处理分支。 */
		return model.DeviceCredential{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return model.DeviceCredential{AccessKey: "dk_" + hex.EncodeToString(b[:8]), Secret: "ds_" + hex.EncodeToString(b[8:])}, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (s *Service) Authenticate(ctx context.Context, key, secret string) (model.ManagedDevice, error) { /* 定义 Authenticate 函数。 */
	d, err := s.Repo.GetManagedDeviceByAccessKey(ctx, key)                                                              /* 更新 err 的值。 */
	if err != nil || secret == "" || d.Status != "ENABLED" || !hmac.Equal([]byte(d.SecretHash), []byte(Hash(secret))) { /* 判断条件并选择处理分支。 */
		return model.ManagedDevice{}, ErrAuth /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	p, err := s.Repo.GetProduct(ctx, d.TenantID, d.ProductID) /* 更新 err 的值。 */
	if err != nil || !d.UsesPlatformCredentials(p) {          /* 判断条件并选择处理分支。 */
		return model.ManagedDevice{}, ErrAuth /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return d, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (s *Service) Allow(key string) bool { /* 定义 Allow 函数。 */
	s.mu.Lock()                       /* 执行当前语句并推进处理流程。 */
	defer s.mu.Unlock()               /* 安排函数结束时执行清理。 */
	now := time.Now()                 /* 更新 now 的值。 */
	b := s.rates[key]                 /* 更新 b 的值。 */
	if now.Sub(b.At) >= time.Second { /* 判断条件并选择处理分支。 */
		b = bucket{At: now} /* 更新 b 的值。 */
	} /* 结束当前表达式或代码块。 */
	if b.Count >= 20 { /* 判断条件并选择处理分支。 */
		return false /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(s.rates) >= 10000 { /* 判断条件并选择处理分支。 */
		for k, v := range s.rates { /* 循环处理当前数据。 */
			if now.Sub(v.At) > time.Minute { /* 判断条件并选择处理分支。 */
				delete(s.rates, k) /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		if _, ok := s.rates[key]; !ok && len(s.rates) >= 10000 { /* 判断条件并选择处理分支。 */
			return false /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	b.Count++        /* 执行当前语句并推进处理流程。 */
	s.rates[key] = b /* 更新 s.rates[key] 的值。 */
	return true      /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// StandardRaw never accepts tenant/device identity or parser metadata from a payload.
func StandardRaw(tenant, product, device, kind, transport string, payload []byte) (model.RawMessage, error) { /* 定义 StandardRaw 函数。 */
	r := model.RawMessage{TenantID: tenant, ProductID: product, DeviceID: device, Protocol: parser.StandardProtocolID, ProtocolID: parser.StandardProtocolID, ProtocolVersion: "1.0.0", Transport: transport, Source: "standard-" + strings.ToLower(transport), PayloadFormat: "json", Payload: append(json.RawMessage(nil), payload...), Headers: map[string]string{"messageKind": kind}} /* 更新 r 的值。 */
	if !segment.MatchString(tenant) || !segment.MatchString(product) || !segment.MatchString(device) || len(payload) > 64<<10 {                                                                                                                                                                                                                                                            /* 判断条件并选择处理分支。 */
		return r, fmt.Errorf("%w: invalid identity or payload exceeds 64 KiB", model.ErrInvalidIngress) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	r.Normalize(time.Now())                                       /* 执行当前语句并推进处理流程。 */
	if _, err := (parser.StandardParser{}).Parse(r); err != nil { /* 判断条件并选择处理分支。 */
		return r, fmt.Errorf("%w: %v", model.ErrInvalidIngress, err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var body struct { /* 声明 body。 */
		ID        string `json:"id"`        /* 执行当前语句并推进处理流程。 */
		Timestamp int64  `json:"timestamp"` /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	_ = json.Unmarshal(payload, &body)                                                                                   /* 更新 _ 的值。 */
	r.Metadata = map[string]any{"clientMessageId": body.ID, "deviceTimestamp": body.Timestamp}                           /* 更新 r.Metadata 的值。 */
	r.MessageID = "raw_std_" + Hash(tenant + "\x00" + product + "\x00" + device + "\x00" + kind + "\x00" + body.ID)[:32] /* 更新 r.MessageID 的值。 */
	return r, nil                                                                                                        /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (s *Service) PrepareStandard(ctx context.Context, tenant, product, device, kind, transport string, payload []byte) (model.RawMessage, error) { /* 定义 PrepareStandard 函数。 */
	d, err := s.Repo.GetManagedDevice(ctx, tenant, device) /* 更新 err 的值。 */
	if err != nil && !errors.Is(err, model.ErrNotFound) {  /* 判断条件并选择处理分支。 */
		return model.RawMessage{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err != nil || d.Status != "ENABLED" || d.SecretHash == "" || d.ProductID != product { /* 判断条件并选择处理分支。 */
		return model.RawMessage{}, ErrAuth /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if d.Tags["connector"] != "MQTT" && d.Tags["connector"] != "HTTP" { /* 判断条件并选择处理分支。 */
		return model.RawMessage{}, ErrAuth /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	p, err := s.Repo.GetProduct(ctx, tenant, product)     /* 更新 err 的值。 */
	if err != nil && !errors.Is(err, model.ErrNotFound) { /* 判断条件并选择处理分支。 */
		return model.RawMessage{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err != nil || p.Status != "ENABLED" || !d.UsesPlatformCredentials(p) { /* 判断条件并选择处理分支。 */
		return model.RawMessage{}, ErrAuth /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !s.Allow(tenant + "\x00" + device) { /* 判断条件并选择处理分支。 */
		return model.RawMessage{}, ErrRate /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return StandardRaw(tenant, product, device, kind, transport, payload) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (s *Service) plan(ctx context.Context, tenant string, q Request) (model.OnboardingBundle, model.ProtocolRelease, error) { /* 定义 plan 函数。 */
	var b model.OnboardingBundle                                                      /* 声明 b。 */
	var rel model.ProtocolRelease                                                     /* 声明 rel。 */
	fail := func(msg string) (model.OnboardingBundle, model.ProtocolRelease, error) { /* 更新 fail 的值。 */
		return b, rel, errors.New(msg) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !segment.MatchString(tenant) || !segment.MatchString(q.DeviceID) || strings.TrimSpace(q.Name) == "" || len(q.Name) > 256 { /* 判断条件并选择处理分支。 */
		return fail("请填写有效设备标识和名称") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if q.Profile.EdgeNodeID != "" { /* 判断条件并选择处理分支。 */
		return fail("边缘节点功能已移除，请使用中心直接接入") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	product, err := s.Repo.GetProduct(ctx, tenant, q.ProductID) /* 更新 err 的值。 */
	if q.ProductName != "" {                                    /* 判断条件并选择处理分支。 */
		if !segment.MatchString(q.ProductID) || len(q.ProductName) > 256 { /* 判断条件并选择处理分支。 */
			return fail("新产品标识或名称无效") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		product = model.Product{ID: q.ProductID, TenantID: tenant, Name: q.ProductName, Status: "ENABLED", Category: "sensor", CreatedAt: time.Now().UnixMilli(), UpdatedAt: time.Now().UnixMilli()} /* 更新 product 的值。 */
		b.Product = &product                                                                                                                                                                         /* 更新 b.Product 的值。 */
	} else if err != nil || product.Status != "ENABLED" { /* 结束当前表达式或代码块。 */
		return fail("请选择已启用的产品") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	now := time.Now().UnixMilli()                                                                                                                                                                                                                                           /* 更新 now 的值。 */
	b.Device = model.ManagedDevice{ID: q.DeviceID, TenantID: tenant, ProductID: q.ProductID, Name: q.Name, Status: "ENABLED", DeviceRole: "DIRECT", RegistrationSource: "ONBOARDING", Tags: map[string]string{"connector": string(q.Type)}, CreatedAt: now, UpdatedAt: now} /* 更新 b.Device 的值。 */
	binding, bindErr := s.Repo.GetProductProtocolBinding(ctx, tenant, q.ProductID)                                                                                                                                                                                          /* 更新 bindErr 的值。 */
	switch q.Type {                                                                                                                                                                                                                                                         /* 根据条件选择处理路径。 */
	case connector.MQTT, connector.HTTP: /* 处理当前分支。 */
		rel = model.ProtocolRelease{TenantID: tenant, ProtocolID: parser.StandardProtocolID, Version: "1.0.0", Transport: "MQTT_HTTP", PayloadFormat: "json", ParserType: parser.StandardParserName, Status: "PUBLISHED", CreatedAt: now, PublishedAt: now} /* 更新 rel 的值。 */
		// The standard ingress pins its release explicitly without changing a
		// product's existing custom protocol binding.
	case connector.ModbusTCP, connector.ModbusRTUTCP, connector.TCP, connector.UDP: /* 处理当前分支。 */
		if bindErr == nil { /* 判断条件并选择处理分支。 */
			if q.ProtocolID != "" && (q.ProtocolID != binding.ProtocolID || q.ProtocolVersion != binding.Version) { /* 判断条件并选择处理分支。 */
				return fail("所选协议与产品现有绑定不一致，请在协议管理中变更绑定") /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			q.ProtocolID, q.ProtocolVersion = binding.ProtocolID, binding.Version /* 更新 q.ProtocolVersion 的值。 */
		} /* 结束当前表达式或代码块。 */
		if q.ProtocolID != "" { /* 判断条件并选择处理分支。 */
			rel, err = s.Repo.GetProtocolRelease(ctx, tenant, q.ProtocolID, q.ProtocolVersion) /* 更新 err 的值。 */
			if err != nil || rel.Status != "PUBLISHED" {                                       /* 判断条件并选择处理分支。 */
				return fail("请选择已发布且兼容的协议") /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} else if q.Type == connector.ModbusTCP || q.Type == connector.ModbusRTUTCP { /* 结束当前表达式或代码块。 */
			interval := q.PollIntervalSec /* 更新 interval 的值。 */
			if interval == 0 {            /* 判断条件并选择处理分支。 */
				interval = 10 /* 更新 interval 的值。 */
			} /* 结束当前表达式或代码块。 */
			if interval < 1 || interval > 3600 { /* 判断条件并选择处理分支。 */
				return fail("采集周期须为 1 至 3600 秒") /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			table, _, e := core.ParseModbusPointTable("points.csv", []byte(q.PointTableCSV), interval) /* 更新 e 的值。 */
			if e != nil {                                                                              /* 判断条件并选择处理分支。 */
				return b, rel, e /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			blocks, e := core.CompileModbusReadBlocks(table.Points) /* 更新 e 的值。 */
			if e != nil {                                           /* 判断条件并选择处理分支。 */
				return b, rel, e /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			rel = model.ProtocolRelease{TenantID: tenant, ProtocolID: "onboard-" + Hash(tenant + "/" + q.ProductID)[:20], Version: Hash(fmt.Sprintf("%d\x00%s", interval, q.PointTableCSV))[:16], Transport: "MODBUS_TCP", PayloadFormat: "hex", ParserType: parser.ModbusTCPParserName, Status: "PUBLISHED", CreatedAt: now, PublishedAt: now, Config: map[string]any{"points": table.Points, "blocks": blocks}} /* 更新 rel 的值。 */
			if q.Type == connector.ModbusRTUTCP {                                                                                                                                                                                                                                                                                                                                                                 /* 判断条件并选择处理分支。 */
				rel.Transport = "MODBUS_RTU"                /* 更新 rel.Transport 的值。 */
				rel.ParserType = parser.ModbusRTUParserName /* 更新 rel.ParserType 的值。 */
				rel.Version = "rtu-" + rel.Version          /* 更新 rel.Version 的值。 */
			} /* 结束当前表达式或代码块。 */
			rel.PointTableVersion = rel.Version                                                                         /* 更新 rel.PointTableVersion 的值。 */
			table.TenantID, table.ProtocolID, table.Version, table.CreatedAt = tenant, rel.ProtocolID, rel.Version, now /* 更新 table.CreatedAt 的值。 */
			b.PointTable = &table                                                                                       /* 更新 b.PointTable 的值。 */
		} else { /* 结束当前表达式或代码块。 */
			return fail("该产品尚未绑定通信协议，请在高级设置选择已发布协议") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		p := q.Profile                                                                                            /* 更新 p 的值。 */
		p.TenantID, p.ProductID, p.DeviceID = tenant, q.ProductID, q.DeviceID                                     /* 更新 p.DeviceID 的值。 */
		p.ID = "onboard-" + Hash(tenant + "/" + q.DeviceID)[:24]                                                  /* 更新 p.ID 的值。 */
		p.ProtocolID, p.ProtocolVersion, p.PointTableVersion = rel.ProtocolID, rel.Version, rel.PointTableVersion /* 更新 p.PointTableVersion 的值。 */
		p.Enabled = true                                                                                          /* 更新 p.Enabled 的值。 */
		p.CreatedAt, p.UpdatedAt = now, now                                                                       /* 更新 p.UpdatedAt 的值。 */
		p.RuntimeStatus = "PENDING"                                                                               /* 更新 p.RuntimeStatus 的值。 */
		p.LastError = ""                                                                                          /* 更新 p.LastError 的值。 */
		p.LastSuccessAt = 0                                                                                       /* 更新 p.LastSuccessAt 的值。 */
		p.LastErrorAt = 0                                                                                         /* 更新 p.LastErrorAt 的值。 */
		if p.TimeoutMs == 0 {                                                                                     /* 判断条件并选择处理分支。 */
			p.TimeoutMs = 3000 /* 更新 p.TimeoutMs 的值。 */
		} /* 结束当前表达式或代码块。 */
		if p.TimeoutMs < 1 || p.TimeoutMs > 10000 || p.Retries < 0 || p.Retries > 3 || (p.Port < 1 || p.Port > 65535) { /* 判断条件并选择处理分支。 */
			return fail("端口、超时或重试参数无效") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if q.Type == connector.ModbusTCP || q.Type == connector.ModbusRTUTCP { /* 判断条件并选择处理分支。 */
			p.Mode = "poll"                       /* 更新 p.Mode 的值。 */
			p.Network = "tcp"                     /* 更新 p.Network 的值。 */
			expectedTransport := "MODBUS_TCP"     /* 更新 expectedTransport 的值。 */
			if q.Type == connector.ModbusRTUTCP { /* 判断条件并选择处理分支。 */
				p.WireFormat = "rtu_over_tcp"    /* 更新 p.WireFormat 的值。 */
				expectedTransport = "MODBUS_RTU" /* 更新 expectedTransport 的值。 */
			} /* 结束当前表达式或代码块。 */
			if p.Host == "" || p.UnitID < 0 || p.UnitID > 255 || rel.Transport != expectedTransport { /* 判断条件并选择处理分支。 */
				return fail("Modbus 地址、站号或协议无效") /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} else { /* 结束当前表达式或代码块。 */
			p.Mode = "listener"                         /* 更新 p.Mode 的值。 */
			p.Network = strings.ToLower(string(q.Type)) /* 更新 p.Network 的值。 */
			if p.ConnectionMode != "dial" {             /* 判断条件并选择处理分支。 */
				p.DeviceID = "" /* 更新 p.DeviceID 的值。 */
			} /* 结束当前表达式或代码块。 */
			if (p.ConnectionMode != "dial" && net.ParseIP(p.Host) == nil) || rel.ParserType != parser.GoProtocolParserName || rel.Artifact["runtime"] != protocolworker.Runtime || !protocolworker.HasCapability(rel, "ingress") || (rel.Transport != string(q.Type) && rel.Transport != "TCP_UDP") { /* 判断条件并选择处理分支。 */
				return fail("监听地址或协议不兼容，需要支持 ingress 的 TCP/UDP 协议") /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		if err := model.ValidateProtocolAccess(p); err != nil { /* 判断条件并选择处理分支。 */
			return fail(err.Error()) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if err := ValidateChildProducts(ctx, s.Repo, p, rel); err != nil { /* 判断条件并选择处理分支。 */
			return fail(err.Error()) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		b.Profile = &p                 /* 更新 b.Profile 的值。 */
		if q.ExistingProfileID != "" { /* 判断条件并选择处理分支。 */
			old, e := s.Repo.GetDeviceAccessProfile(ctx, tenant, q.ExistingProfileID)                                                                                                                                                                                                        /* 更新 e 的值。 */
			if e != nil || !old.Enabled || old.ConnectionMode == "dial" || old.EdgeNodeID != "" || old.Mode != "listener" || old.Network != p.Network || old.ProductID != q.ProductID || (q.Type == connector.ModbusTCP || q.Type == connector.ModbusRTUTCP) || p.ConnectionMode == "dial" { /* 判断条件并选择处理分支。 */
				return fail("已有监听实例不可用或不属于当前产品") /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			old.ProtocolID, old.ProtocolVersion = rel.ProtocolID, rel.Version /* 更新 old.ProtocolVersion 的值。 */
			b.Profile = &old                                                  /* 更新 b.Profile 的值。 */
			p = old                                                           /* 更新 p 的值。 */
			b.ReuseProfile = true                                             /* 更新 b.ReuseProfile 的值。 */
		} /* 结束当前表达式或代码块。 */
		b.Device.Tags["connectorProfileId"] = p.ID /* 执行当前语句并推进处理流程。 */
		if bindErr != nil {                        /* 判断条件并选择处理分支。 */
			if b.Product == nil { /* 判断条件并选择处理分支。 */
				devices, e := s.Repo.ListManagedDevices(ctx, tenant) /* 更新 e 的值。 */
				if e != nil {                                        /* 判断条件并选择处理分支。 */
					return b, rel, e /* 返回当前处理结果。 */
				} /* 结束当前表达式或代码块。 */
				for _, device := range devices { /* 循环处理当前数据。 */
					if device.ProductID == q.ProductID { /* 判断条件并选择处理分支。 */
						return fail("该产品已有设备且使用旧协议配置，请选择已绑定产品或在向导中新建产品，避免改变已有设备的解析") /* 返回当前处理结果。 */
					} /* 结束当前表达式或代码块。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
			b.Binding = &model.ProductProtocolBinding{TenantID: tenant, ProductID: q.ProductID, ProtocolID: rel.ProtocolID, Version: rel.Version, UpdatedAt: now} /* 更新 b.Binding 的值。 */
		} /* 结束当前表达式或代码块。 */
	default: /* 处理当前分支。 */
		return fail("该接入方式尚未开放；视频设备请使用现有摄像头管理") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	b.Release = &rel      /* 更新 b.Release 的值。 */
	if b.Product != nil { /* 判断条件并选择处理分支。 */
		b.Product.Transport = string(q.Type)                             /* 更新 b.Product.Transport 的值。 */
		b.Product.PayloadFormat = rel.PayloadFormat                      /* 更新 b.Product.PayloadFormat 的值。 */
		b.Product.ProtocolPackageID = rel.ProtocolID + "@" + rel.Version /* 更新 b.Product.ProtocolPackageID 的值。 */
	} /* 结束当前表达式或代码块。 */
	return b, rel, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (s *Service) Create(ctx context.Context, tenant string, q Request) (Result, error) { /* 定义 Create 函数。 */
	// Device ID is the tenant-scoped idempotency key. The digest excludes the
	// expiring preview proof, allowing recovery after a lost HTTP response.
	request := q                          /* 更新 request 的值。 */
	request.TestToken = ""                /* 更新 request.TestToken 的值。 */
	encoded, err := json.Marshal(request) /* 更新 err 的值。 */
	if err != nil {                       /* 判断条件并选择处理分支。 */
		return Result{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	digest := Hash(string(encoded))     /* 更新 digest 的值。 */
	recover := func() (Result, error) { /* 更新 recover 的值。 */
		d, err := s.Repo.GetManagedDevice(ctx, tenant, q.DeviceID) /* 更新 err 的值。 */
		if err != nil {                                            /* 判断条件并选择处理分支。 */
			return Result{}, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if d.Tags["onboardingRequestHash"] != digest { /* 判断条件并选择处理分支。 */
			return Result{}, errors.New("设备标识已存在且接入请求不同") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		instance := connector.Instance{Type: connector.Type(d.Tags["connector"]), DeviceID: d.ID} /* 更新 instance 的值。 */
		if id := d.Tags["connectorProfileId"]; id != "" {                                         /* 判断条件并选择处理分支。 */
			p, err := s.Repo.GetDeviceAccessProfile(ctx, tenant, id) /* 更新 err 的值。 */
			if err != nil {                                          /* 判断条件并选择处理分支。 */
				return Result{}, err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			instance.Profile = &p /* 更新 instance.Profile 的值。 */
		} /* 结束当前表达式或代码块。 */
		result := Result{Reused: true, Device: d.Public(model.Product{}), Connector: instance} /* 更新 result 的值。 */
		if d.UsesPlatformCredentials(model.Product{}) {                                        /* 判断条件并选择处理分支。 */
			result.ClientID, result.Username = "device-"+d.AccessKey, d.AccessKey /* 更新 result.Username 的值。 */
		} /* 结束当前表达式或代码块。 */
		return result, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if _, err := s.Repo.GetManagedDevice(ctx, tenant, q.DeviceID); err == nil { /* 判断条件并选择处理分支。 */
		return recover() /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	b, rel, err := s.plan(ctx, tenant, q) /* 更新 err 的值。 */
	if err != nil {                       /* 判断条件并选择处理分支。 */
		return Result{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	parts := strings.Split(q.TestToken, ".") /* 更新 parts 的值。 */
	if len(parts) != 2 {                     /* 判断条件并选择处理分支。 */
		return Result{}, errors.New("请先完成接入测试") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	expiry, e := strconv.ParseInt(parts[0], 10, 64)                                                                                    /* 更新 e 的值。 */
	if e != nil || expiry < time.Now().Unix() || !hmac.Equal([]byte(parts[1]), []byte(s.proof(tenant, q, rel, b.Profile, parts[0]))) { /* 判断条件并选择处理分支。 */
		return Result{}, errors.New("接入配置已变化或测试过期，请重新测试") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	credential := model.DeviceCredential{}                 /* 更新 credential 的值。 */
	if b.Device.UsesPlatformCredentials(model.Product{}) { /* 判断条件并选择处理分支。 */
		credential, err = Credential() /* 更新 err 的值。 */
		if err != nil {                /* 判断条件并选择处理分支。 */
			return Result{}, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		b.Device.AccessKey = credential.AccessKey     /* 更新 b.Device.AccessKey 的值。 */
		b.Device.SecretHash = Hash(credential.Secret) /* 更新 b.Device.SecretHash 的值。 */
	} else { /* 结束当前表达式或代码块。 */
		b.Device.AccessKey = model.ProtocolDeviceAccessKey(tenant, q.DeviceID) /* 更新 b.Device.AccessKey 的值。 */
	} /* 结束当前表达式或代码块。 */
	b.Device.Tags["onboardingRequestHash"] = digest      /* 执行当前语句并推进处理流程。 */
	if err = s.Repo.SaveOnboarding(ctx, b); err != nil { /* 判断条件并选择处理分支。 */
		// A competing request may have committed while this request was planning.
		if recovered, retryErr := recover(); retryErr == nil { /* 判断条件并选择处理分支。 */
			return recovered, nil /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return Result{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	result := Result{Device: b.Device.Public(model.Product{}), Credential: credential, Connector: connector.Instance{Type: q.Type, DeviceID: q.DeviceID, Profile: b.Profile}} /* 更新 result 的值。 */
	if credential.AccessKey != "" {                                                                                                                                           /* 判断条件并选择处理分支。 */
		result.ClientID, result.Username = "device-"+credential.AccessKey, credential.AccessKey /* 更新 result.Username 的值。 */
	} /* 结束当前表达式或代码块。 */
	return result, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (s *Service) Test(ctx context.Context, tenant string, q Request) (result *connector.Result, resultErr error) { /* 定义 Test 函数。 */
	started := time.Now() /* 更新 started 的值。 */
	defer func() {        /* 安排函数结束时执行清理。 */
		if result == nil { /* 判断条件并选择处理分支。 */
			result = &connector.Result{Stage: "validate", Message: "接入配置校验失败", ErrorCode: "PROTOCOL_ERROR"} /* 更新 result 的值。 */
			if resultErr != nil {                                                                           /* 判断条件并选择处理分支。 */
				result.Message = resultErr.Error()                                  /* 更新 result.Message 的值。 */
				result.ErrorCode = connector.ErrorCode(resultErr, "PROTOCOL_ERROR") /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		result.LatencyMs = time.Since(started).Milliseconds() /* 更新 result.LatencyMs 的值。 */
		if result.DeviceID == "" {                            /* 判断条件并选择处理分支。 */
			result.DeviceID = q.DeviceID /* 更新 result.DeviceID 的值。 */
		} /* 结束当前表达式或代码块。 */
		if result.ProtocolID == "" { /* 判断条件并选择处理分支。 */
			result.ProtocolID = q.ProtocolID /* 更新 result.ProtocolID 的值。 */
		} /* 结束当前表达式或代码块。 */
		if result.ProtocolVersion == "" { /* 判断条件并选择处理分支。 */
			result.ProtocolVersion = q.ProtocolVersion /* 更新 result.ProtocolVersion 的值。 */
		} /* 结束当前表达式或代码块。 */
	}() /* 结束当前表达式或代码块。 */
	if !connector.Describe(q.Type).Supported { /* 判断条件并选择处理分支。 */
		return &connector.Result{Stage: "unsupported", ErrorCode: "UNSUPPORTED", Message: "接入方式尚未实现"}, connector.ErrUnsupported /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err := ctx.Err(); err != nil { /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	select { /* 根据条件选择处理路径。 */
	case testSlots <- struct{}{}: /* 处理当前分支。 */
		defer func() { <-testSlots }() /* 安排函数结束时执行清理。 */
	default: /* 处理当前分支。 */
		return &connector.Result{Stage: "busy", ErrorCode: "BUSY", Message: "接入测试并发已达上限，请稍后重试"}, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second) /* 更新 cancel 的值。 */
	defer cancel()                                          /* 安排函数结束时执行清理。 */
	b, rel, err := s.plan(ctx, tenant, q)                   /* 更新 err 的值。 */
	if err != nil {                                         /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if q.Type == connector.MQTT { /* 判断条件并选择处理分支。 */
		if s.MQTTHealth == nil { /* 判断条件并选择处理分支。 */
			return &connector.Result{Stage: "broker", ErrorCode: "NETWORK_ERROR", Message: "平台 MQTT 连接未启动，可先使用 HTTP 接入", ProtocolID: rel.ProtocolID, ProtocolVersion: rel.Version, Parser: rel.ParserType}, nil /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		healthCtx, cancel := context.WithTimeout(ctx, 5*time.Second) /* 更新 cancel 的值。 */
		err := s.MQTTHealth(healthCtx)                               /* 更新 err 的值。 */
		cancel()                                                     /* 执行当前语句并推进处理流程。 */
		if err != nil {                                              /* 判断条件并选择处理分支。 */
			return &connector.Result{Stage: "broker", ErrorCode: connector.ErrorCode(err, "NETWORK_ERROR"), Message: err.Error(), ProtocolID: rel.ProtocolID, ProtocolVersion: rel.Version, Parser: rel.ParserType}, nil /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	r := connector.Request{Type: q.Type, Release: rel, Reuse: b.ReuseProfile} /* 更新 r 的值。 */
	if b.Profile != nil {                                                     /* 判断条件并选择处理分支。 */
		r.Profile = *b.Profile /* 更新 r.Profile 的值。 */
	} /* 结束当前表达式或代码块。 */
	r.Raw = model.RawMessage{TenantID: tenant, ProductID: q.ProductID, DeviceID: q.DeviceID, Protocol: rel.ProtocolID, Transport: string(q.Type), ProtocolID: rel.ProtocolID, ProtocolVersion: rel.Version, PayloadFormat: rel.PayloadFormat, Payload: q.Payload, ReceivedAt: time.Now().UnixMilli()} /* 更新 r.Raw 的值。 */
	r.Raw.Normalize(time.Now())                                                                                                                                                                                                                                                                       /* 执行当前语句并推进处理流程。 */
	if q.Type == connector.MQTT || q.Type == connector.HTTP {                                                                                                                                                                                                                                         /* 判断条件并选择处理分支。 */
		r.Raw, err = StandardRaw(tenant, q.ProductID, q.DeviceID, q.MessageKind, string(q.Type), q.Payload) /* 更新 err 的值。 */
		if err != nil {                                                                                     /* 判断条件并选择处理分支。 */
			return &connector.Result{Stage: "parse", ErrorCode: "PARSE_FAILED", Message: err.Error(), Raw: []model.RawMessage{r.Raw}, ProtocolID: rel.ProtocolID, ProtocolVersion: rel.Version, Parser: rel.ParserType}, nil /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	a := connector.Adapter{Kind: q.Type, Probe: s.probe} /* 更新 a 的值。 */
	result, err = a.Test(ctx, r)                         /* 更新 err 的值。 */
	if err == nil && result.Success {                    /* 判断条件并选择处理分支。 */
		expiry := strconv.FormatInt(time.Now().Add(10*time.Minute).Unix(), 10)       /* 更新 expiry 的值。 */
		result.TestToken = expiry + "." + s.proof(tenant, q, rel, b.Profile, expiry) /* 更新 result.TestToken 的值。 */
	} /* 结束当前表达式或代码块。 */
	return result, err /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (s *Service) probe(ctx context.Context, q connector.Request) (*connector.Result, error) { /* 定义 probe 函数。 */
	start := time.Now()                                                                                                                                                                                   /* 更新 start 的值。 */
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)                                                                                                                                               /* 更新 cancel 的值。 */
	defer cancel()                                                                                                                                                                                        /* 安排函数结束时执行清理。 */
	r := &connector.Result{Source: "sample", Stage: "sample", ProtocolID: q.Release.ProtocolID, ProtocolVersion: q.Release.Version, Parser: q.Release.ParserType, PointTable: q.Release.Config["points"]} /* 更新 r 的值。 */
	finish := func(err error, code string) (*connector.Result, error) {                                                                                                                                   /* 更新 finish 的值。 */
		r.LatencyMs = time.Since(start).Milliseconds() /* 更新 r.LatencyMs 的值。 */
		r.ErrorCode = code                             /* 更新 r.ErrorCode 的值。 */
		if err != nil {                                /* 判断条件并选择处理分支。 */
			r.Message = err.Error()                      /* 更新 r.Message 的值。 */
			r.ErrorCode = connector.ErrorCode(err, code) /* 更新 r.ErrorCode 的值。 */
		} /* 结束当前表达式或代码块。 */
		return r, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	raws := []model.RawMessage{q.Raw} /* 更新 raws 的值。 */
	switch q.Type {                   /* 根据条件选择处理路径。 */
	case connector.ModbusTCP, connector.ModbusRTUTCP: /* 处理当前分支。 */
		r.Stage = "read"                                                                                                                                   /* 更新 r.Stage 的值。 */
		r.Source = "network-read"                                                                                                                          /* 更新 r.Source 的值。 */
		var blocks []model.ModbusReadBlock                                                                                                                 /* 声明 blocks。 */
		data, _ := json.Marshal(q.Release.Config["blocks"])                                                                                                /* 更新 _ 的值。 */
		if err := json.Unmarshal(data, &blocks); (err != nil || len(blocks) == 0) && (q.Type == connector.ModbusTCP || q.Type == connector.ModbusRTUTCP) { /* 判断条件并选择处理分支。 */
			return finish(errors.New("协议没有读取计划"), "PROTOCOL_ERROR") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		var err error                                                                                          /* 声明 err。 */
		raws, err = protocolruntime.ReadModbusTCPWithPolicy(ctx, q.Profile, q.Release, blocks, s.AllowedCIDRs) /* 更新 err 的值。 */
		if err != nil {                                                                                        /* 判断条件并选择处理分支。 */
			var wire *protocolruntime.ModbusReadError /* 声明 wire。 */
			code := "NETWORK_ERROR"                   /* 更新 code 的值。 */
			if errors.As(err, &wire) {                /* 判断条件并选择处理分支。 */
				r.RawRequest = strings.ToUpper(hex.EncodeToString(wire.Request))   /* 更新 r.RawRequest 的值。 */
				r.RawResponse = strings.ToUpper(hex.EncodeToString(wire.Response)) /* 更新 r.RawResponse 的值。 */
				var n net.Error                                                    /* 声明 n。 */
				if !errors.As(err, &n) {                                           /* 判断条件并选择处理分支。 */
					code = "PROTOCOL_ERROR" /* 更新 code 的值。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
			var exception *protocolruntime.ModbusException /* 声明 exception。 */
			if errors.As(err, &exception) {                /* 判断条件并选择处理分支。 */
				r.ExceptionCode = int(exception.Code) /* 更新 r.ExceptionCode 的值。 */
				code = "PROTOCOL_ERROR"               /* 更新 code 的值。 */
			} /* 结束当前表达式或代码块。 */
			return finish(err, code) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if len(raws) == 0 { /* 判断条件并选择处理分支。 */
			return finish(errors.New("设备未返回报文"), "PROTOCOL_ERROR") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		r.RawRequest = fmt.Sprint(raws[0].Metadata["requestHex"]) /* 更新 r.RawRequest 的值。 */
		r.RawResponse = string(raws[0].Payload)                   /* 更新 r.RawResponse 的值。 */
	case connector.TCP, connector.UDP: /* 处理当前分支。 */
		r.Stage = "listener-check"                                           /* 更新 r.Stage 的值。 */
		addr := net.JoinHostPort(q.Profile.Host, fmt.Sprint(q.Profile.Port)) /* 更新 addr 的值。 */
		var closer interface{ Close() error }                                /* 声明 closer。 */
		var err error                                                        /* 声明 err。 */
		if q.Profile.ConnectionMode == "dial" {                              /* 判断条件并选择处理分支。 */
			r.Stage = "tcp-connect"                                                                /* 更新 r.Stage 的值。 */
			target, e := protocolruntime.ResolveAllowedTarget(ctx, q.Profile.Host, s.AllowedCIDRs) /* 更新 e 的值。 */
			if e != nil {                                                                          /* 判断条件并选择处理分支。 */
				return finish(e, "NETWORK_ERROR") /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			closer, err = (&net.Dialer{Timeout: time.Duration(q.Profile.TimeoutMs) * time.Millisecond}).DialContext(ctx, "tcp", net.JoinHostPort(target, fmt.Sprint(q.Profile.Port))) /* 更新 err 的值。 */
		} else if q.Reuse { /* 结束当前表达式或代码块。 */
			if s.ListenerStatus == nil { /* 判断条件并选择处理分支。 */
				return finish(errors.New("监听运行时未启动"), "NETWORK_ERROR") /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			status, message, _ := s.ListenerStatus(q.Profile.TenantID, q.Profile.ID) /* 更新 _ 的值。 */
			if status != "LISTENING" {                                               /* 判断条件并选择处理分支。 */
				return finish(fmt.Errorf("监听实例 %s: %s", status, message), "NETWORK_ERROR") /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} else if q.Type == connector.TCP { /* 结束当前表达式或代码块。 */
			closer, err = net.Listen("tcp", addr) /* 更新 err 的值。 */
		} else { /* 结束当前表达式或代码块。 */
			closer, err = net.ListenPacket("udp", addr) /* 更新 err 的值。 */
		} /* 结束当前表达式或代码块。 */
		if err != nil { /* 判断条件并选择处理分支。 */
			return finish(err, "NETWORK_ERROR") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if closer != nil { /* 判断条件并选择处理分支。 */
			_ = closer.Close() /* 更新 _ 的值。 */
		} /* 结束当前表达式或代码块。 */
		var frame string                                  /* 声明 frame。 */
		if json.Unmarshal(q.Raw.Payload, &frame) != nil { /* 判断条件并选择处理分支。 */
			return finish(errors.New("请提供 HEX 字符串报文"), "PROTOCOL_ERROR") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		response, err := protocolworker.Call(ctx, s.Root, q.Release, protocolworker.Request{Operation: "ingress", Data: frame, Now: time.Now().UnixMilli(), Raw: &q.Raw}) /* 更新 err 的值。 */
		if err != nil {                                                                                                                                                   /* 判断条件并选择处理分支。 */
			return finish(err, "PROTOCOL_ERROR") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if response.NeedMore { /* 判断条件并选择处理分支。 */
			return finish(errors.New("样例不是完整帧"), "PROTOCOL_ERROR") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if response.DeviceID != q.Raw.DeviceID { /* 判断条件并选择处理分支。 */
			return finish(fmt.Errorf("报文设备标识 %s 与填写的设备标识不一致", response.DeviceID), "DEVICE_IDENTIFY_FAILED") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		data, err := hex.DecodeString(frame) /* 更新 err 的值。 */
		if err != nil {                      /* 判断条件并选择处理分支。 */
			return finish(err, "PROTOCOL_ERROR") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if response.Consumed != len(data) { /* 判断条件并选择处理分支。 */
			return finish(errors.New("请提供恰好一帧完整报文"), "PROTOCOL_ERROR") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		r.RawResponse = response.Reply /* 更新 r.RawResponse 的值。 */
	} /* 结束当前表达式或代码块。 */
	r.Raw = raws                   /* 更新 r.Raw 的值。 */
	r.Stage = "parse"              /* 更新 r.Stage 的值。 */
	mappings := []map[string]any{} /* 更新 mappings 的值。 */
	parsed := []any{}              /* 更新 parsed 的值。 */
	for _, raw := range raws {     /* 循环处理当前数据。 */
		m, err := s.Parsers.ParseWithConfig(q.Release.ParserType, q.Release.Config, raw) /* 更新 err 的值。 */
		if err != nil {                                                                  /* 判断条件并选择处理分支。 */
			return finish(err, "PARSE_FAILED") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		r.StandardMessages = append(r.StandardMessages, m)                                                                                                                   /* 更新 r.StandardMessages 的值。 */
		parsed = append(parsed, map[string]any{"properties": m.Properties, "event": m.Event, "tags": m.Tags})                                                                /* 更新 parsed 的值。 */
		mappings = append(mappings, map[string]any{"messageType": m.MessageType, "properties": m.Properties, "event": m.Event, "alarm": m.MessageType == model.AlarmReport}) /* 更新 mappings 的值。 */
		r.DeviceID = m.DeviceID                                                                                                                                              /* 更新 r.DeviceID 的值。 */
	} /* 结束当前表达式或代码块。 */
	r.Parsed = parsed               /* 更新 r.Parsed 的值。 */
	r.Mapping = mappings            /* 更新 r.Mapping 的值。 */
	r.Success = true                /* 更新 r.Success 的值。 */
	r.Stage = "preview"             /* 更新 r.Stage 的值。 */
	r.Message = "样例解析通过；未向业务链路写入数据" /* 更新 r.Message 的值。 */
	if q.Type == connector.MQTT {   /* 判断条件并选择处理分支。 */
		r.Message = "平台 MQTT 连接健康，样例解析通过；尚未验证设备到 Broker 的网络和认证" /* 更新 r.Message 的值。 */
	} /* 结束当前表达式或代码块。 */
	if q.Type == connector.ModbusTCP || q.Type == connector.ModbusRTUTCP { /* 判断条件并选择处理分支。 */
		r.Message = "目标设备读取与解析通过（设备或模拟器取决于配置）；测试数据未写入业务链路" /* 更新 r.Message 的值。 */
	} /* 结束当前表达式或代码块。 */
	if q.Type == connector.TCP || q.Type == connector.UDP { /* 判断条件并选择处理分支。 */
		r.Message = "本机端口可绑定，完整帧识别及解析通过；尚未验证设备到平台的网络" /* 更新 r.Message 的值。 */
		if q.Profile.ConnectionMode == "dial" {       /* 判断条件并选择处理分支。 */
			r.Message = "目标 TCP 端口连接及样例解析通过；真实协议握手与定时查询需在启用后验证" /* 更新 r.Message 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return finish(nil, "SUCCESS") /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
