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
	"regexp"
	"strings"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
	"iot-platform/internal/ratelimit"
)

var segment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
var ErrAuth = errors.New("invalid or disabled device credential")
var ErrRate = errors.New("device rate limit exceeded")

// ErrUnavailable reports that credentials could not be checked because the
// repository failed; callers answer 503 so devices retry instead of treating
// a working credential as revoked.
var ErrUnavailable = errors.New("device credential check temporarily unavailable")

type Service struct {
	RevokeUsername func(context.Context, string) error
	PublishCommand func(context.Context, string, []byte, byte, bool) error
	Repo           ports.Repository
	Parsers        *parser.Registry
	Root           string
	AllowedCIDRs   []string
	// Limiter holds per-device and per-key budgets; a cluster shares it so
	// replicas do not multiply the allowance.
	Limiter         ratelimit.Limiter
	ListenerStatus  func(string, string) (string, string, int64)
	MQTTHealth      func(context.Context) error
	LoadRaw         func(context.Context, model.RawArchiveIndex) (model.RawMessage, error)
	RequirePrepared bool
	PublicHTTP      string
	PublicMQTT      string
}

func New(repo ports.Repository, p *parser.Registry, root string, cidrs []string) *Service {
	return &Service{Repo: repo, Parsers: p, Root: root, AllowedCIDRs: cidrs, Limiter: ratelimit.NewLocal()}
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
	if key == "" || secret == "" {
		return model.ManagedDevice{}, ErrAuth
	}
	d, err := s.Repo.GetManagedDeviceByAccessKey(ctx, key)
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		return model.ManagedDevice{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if err != nil || d.Status != "ENABLED" || !hmac.Equal([]byte(d.SecretHash), []byte(Hash(secret))) {
		return model.ManagedDevice{}, ErrAuth
	}
	p, err := s.Repo.GetProduct(ctx, d.TenantID, d.ProductID)
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		return model.ManagedDevice{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if err != nil || !d.UsesPlatformCredentials(p) {
		return model.ManagedDevice{}, ErrAuth
	}
	return d, nil
}
func (s *Service) Allow(key string) bool { return s.AllowRate(key, 20) }

// AllowRate applies a fixed one-second window of perSecond requests to key,
// shared by all replicas when the limiter is cluster-wide.
func (s *Service) AllowRate(key string, perSecond int) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	ok, err := s.Limiter.Allow(ctx, "rate\x00"+key, perSecond, time.Second)
	return err == nil && ok
}

// StandardRaw never accepts tenant/device identity or parser metadata from a payload.
func StandardRaw(tenant, product, device, kind, transport string, payload []byte) (model.RawMessage, error) {
	r := model.RawMessage{TenantID: tenant, ProductID: product, DeviceID: device, Protocol: parser.StandardProtocolID, ProtocolID: parser.StandardProtocolID, ProtocolVersion: "1.0.0", Transport: transport, Source: "standard-" + strings.ToLower(transport), PayloadFormat: "json", Payload: append(json.RawMessage(nil), payload...), Headers: map[string]string{"messageKind": kind}}
	if !segment.MatchString(tenant) || !segment.MatchString(product) || !segment.MatchString(device) || len(payload) > 64<<10 {
		return r, fmt.Errorf("%w: invalid identity or payload exceeds 64 KiB", model.ErrInvalidIngress)
	}
	r.Normalize(time.Now())
	if _, err := (parser.StandardParser{}).Parse(r); err != nil {
		return r, fmt.Errorf("%w: %v", model.ErrInvalidIngress, err)
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
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		return model.RawMessage{}, err
	}
	if err != nil || d.Status != "ENABLED" || d.SecretHash == "" || d.ProductID != product {
		return model.RawMessage{}, ErrAuth
	}
	if d.Connector != "MQTT" && d.Connector != "HTTP" {
		return model.RawMessage{}, ErrAuth
	}
	p, err := s.Repo.GetProduct(ctx, tenant, product)
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		return model.RawMessage{}, err
	}
	if err != nil || p.Status != "ENABLED" || !d.UsesPlatformCredentials(p) {
		return model.RawMessage{}, ErrAuth
	}
	if !s.Allow(tenant + "\x00" + device) {
		return model.RawMessage{}, ErrRate
	}
	return StandardRaw(tenant, product, device, kind, transport, payload)
}
