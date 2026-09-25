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
	"regexp"        /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */
	"sync"          /* 执行当前语句并推进处理流程。 */
	"time"          /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports"  /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

var segment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`) /* 声明 segment。 */
var ErrAuth = errors.New("invalid or disabled device credential")      /* 声明 ErrAuth。 */
var ErrRate = errors.New("device rate limit exceeded")                 /* 声明 ErrRate。 */
type bucket struct {                                                   /* 定义 bucket 类型。 */
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
	ListenerStatus func(string, string) (string, string, int64)            /* 执行当前语句并推进处理流程。 */
	MQTTHealth     func(context.Context) error                             /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func New(repo ports.Repository, p *parser.Registry, root string, cidrs []string) *Service { /* 定义 New 函数。 */
	return &Service{Repo: repo, Parsers: p, Root: root, AllowedCIDRs: cidrs, rates: map[string]bucket{}} /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

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
