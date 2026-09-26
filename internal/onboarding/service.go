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
	"sync"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
)

var segment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
var ErrAuth = errors.New("invalid or disabled device credential")
var ErrRate = errors.New("device rate limit exceeded")

// ErrUnavailable reports that credentials could not be checked because the
// repository failed; callers answer 503 so devices retry instead of treating
// a working credential as revoked.
var ErrUnavailable = errors.New("device credential check temporarily unavailable")

type bucket struct {
	At    time.Time
	Count int
}
type Service struct {
	RevokeUsername func(context.Context, string) error
	PublishCommand func(context.Context, string, []byte, byte, bool) error
	Repo           ports.Repository
	Parsers        *parser.Registry
	Root           string
	AllowedCIDRs   []string
	mu             sync.Mutex
	rates          map[string]bucket
	lastSweep      time.Time
	ListenerStatus func(string, string) (string, string, int64)
	MQTTHealth     func(context.Context) error
}

func New(repo ports.Repository, p *parser.Registry, root string, cidrs []string) *Service {
	return &Service{Repo: repo, Parsers: p, Root: root, AllowedCIDRs: cidrs, rates: map[string]bucket{}}
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

// rateTableLimit bounds the keys tracked at once; keys expire with their
// one-second window, so it limits distinct callers per second, not per minute.
const rateTableLimit = 100000

// AllowRate applies a per-process fixed one-second window of perSecond requests.
func (s *Service) AllowRate(key string, perSecond int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	b := s.rates[key]
	if now.Sub(b.At) >= time.Second {
		b = bucket{At: now}
	}
	if b.Count >= perSecond {
		return false
	}
	if len(s.rates) >= rateTableLimit {
		// A key whose one-second window has ended equals an absent key, so
		// only keys active in the current second occupy the table.
		if now.Sub(s.lastSweep) >= time.Second {
			for k, v := range s.rates {
				if now.Sub(v.At) >= time.Second {
					delete(s.rates, k)
				}
			}
			s.lastSweep = now
		}
		if _, ok := s.rates[key]; !ok && len(s.rates) >= rateTableLimit {
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
