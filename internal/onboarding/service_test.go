package onboarding

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"strings"
	"testing"

	"iot-platform/internal/adapters/clickhouse"
	"iot-platform/internal/adapters/memory"
	redisadapter "iot-platform/internal/adapters/redis"
	"iot-platform/internal/connector"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
)

func fixture(t *testing.T) (*Service, *memory.Repository, Request) {
	t.Helper()
	repo := memory.NewRepository()
	if err := repo.SaveProduct(context.Background(), model.Product{TenantID: "tenant", ID: "product", Status: "ENABLED"}); err != nil {
		t.Fatal(err)
	}
	return New(repo, parser.NewPlatformRegistry(t.TempDir()), t.TempDir(), []string{"127.0.0.0/8"}), repo, Request{ProductID: "product", DeviceID: "device", Name: "温度", Type: connector.HTTP, MessageKind: "property", Payload: json.RawMessage(`{"id":"1","timestamp":1788850000000,"data":{"temperature":26.5}}`)}
}
func tested(t *testing.T, s *Service, q Request) Request {
	t.Helper()
	r, err := s.Test(context.Background(), "tenant", q)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Success || r.TestToken == "" {
		t.Fatalf("test failed: %+v", r)
	}
	q.TestToken = r.TestToken
	return q
}
func TestCredentialAndAtomicCreation(t *testing.T) {
	s, repo, q := fixture(t)
	ctx := context.Background()
	if _, err := s.Create(ctx, "tenant", q); err == nil {
		t.Fatal("untested request accepted")
	}
	q = tested(t, s, q)
	changed := q
	changed.Name = "changed"
	if _, err := s.Create(ctx, "tenant", changed); err == nil {
		t.Fatal("changed configuration accepted")
	}
	r, err := s.Create(ctx, "tenant", q)
	if err != nil {
		t.Fatal(err)
	}
	d, _ := repo.GetManagedDevice(ctx, "tenant", "device")
	if d.SecretHash == r.Credential.Secret || d.SecretHash != Hash(r.Credential.Secret) {
		t.Fatal("secret storage invalid")
	}
	data, _ := json.Marshal(d)
	if strings.Contains(string(data), r.Credential.Secret) || strings.Contains(string(data), d.SecretHash) {
		t.Fatal("credential leaked")
	}
	if _, err = s.Authenticate(ctx, r.Credential.AccessKey, r.Credential.Secret); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Authenticate(ctx, r.Credential.AccessKey, "wrong"); err == nil {
		t.Fatal("bad password accepted")
	}
	if _, err = s.Create(ctx, "tenant", q); err == nil {
		t.Fatal("duplicate overwritten")
	}
	after, _ := repo.GetManagedDevice(ctx, "tenant", "device")
	if after.SecretHash != d.SecretHash {
		t.Fatal("duplicate changed credential")
	}
	if _, err = s.Test(ctx, "other", q); err == nil {
		t.Fatal("cross-tenant product accepted")
	}
	d.SecretHash = ""
	_ = repo.SaveManagedDevice(ctx, d)
	if _, err = s.PrepareStandard(ctx, "tenant", "product", "device", "property", "MQTT", q.Payload); err != ErrAuth {
		t.Fatalf("disabled credential MQTT: %v", err)
	}
}

func TestMQTTHealthAndNewProduct(t *testing.T) {
	s, repo, q := fixture(t)
	q.Type = connector.MQTT
	q.ProductID = "new-product"
	q.ProductName = "新产品"
	r, err := s.Test(context.Background(), "tenant", q)
	if err != nil || r.Success || r.ErrorCode != "NETWORK_ERROR" {
		t.Fatalf("missing broker reported healthy: %+v %v", r, err)
	}
	s.MQTTHealth = func(context.Context) error { return nil }
	q = tested(t, s, q)
	if _, err = s.Create(context.Background(), "tenant", q); err != nil {
		t.Fatal(err)
	}
	p, err := repo.GetProduct(context.Background(), "tenant", "new-product")
	if err != nil || p.Name != "新产品" || p.ProtocolPackageID == "" {
		t.Fatal("product not atomically created", p, err)
	}
	pkg, err := repo.GetProtocolPackage(context.Background(), "tenant", p.ProtocolPackageID)
	if err != nil || pkg.ParserType != parser.StandardParserName {
		t.Fatal("legacy compatibility shim missing", err)
	}
}

func TestOnboardingThroughProductionRepositoryDecorators(t *testing.T) {
	s, repo, q := fixture(t)
	telemetry := &clickhouse.Repository{Repository: repo}
	cache := redisadapter.New(telemetry, "127.0.0.1:1", "")
	// Onboarding must be forwarded without touching the external cache or
	// telemetry service. This is the same nesting as main's production wiring.
	s.Repo = cache
	q = tested(t, s, q)
	if _, err := s.Create(context.Background(), "tenant", q); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetManagedDevice(context.Background(), "tenant", "device"); err != nil {
		t.Fatal(err)
	}
}
func TestStandardEnvelopeAndIdempotency(t *testing.T) {
	s, _, q := fixture(t)
	ctx := context.Background()
	q = tested(t, s, q)
	if _, err := s.Create(ctx, "tenant", q); err != nil {
		t.Fatal(err)
	}
	a, err := s.PrepareStandard(ctx, "tenant", "product", "device", "property", "HTTP", q.Payload)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := StandardRaw("tenant", "product", "device", "property", "MQTT", q.Payload)
	other, _ := StandardRaw("tenant", "product", "other", "property", "HTTP", q.Payload)
	if a.MessageID != b.MessageID || a.MessageID == other.MessageID {
		t.Fatal("idempotency not scoped")
	}
	if string(a.Payload) != string(q.Payload) {
		t.Fatal("raw body changed")
	}
	for _, kind := range []string{"property", "event", "state"} {
		raw, err := StandardRaw("tenant", "product", "device", kind, "MQTT", q.Payload)
		if err != nil {
			t.Fatal(err)
		}
		m, err := s.Parsers.Parse(raw)
		if err != nil || m.RawMessageID != raw.MessageID {
			t.Fatalf("%s: %v", kind, err)
		}
	}
	for _, payload := range []string{`null`, `{}`, `{"id":"1","timestamp":1,"data":null}`, `{"id":"1","timestamp":1,"data":[]}`, `{"id":"1","timestamp":1,"data":{"x":1}} trailing`} {
		if _, err := StandardRaw("tenant", "product", "device", "property", "HTTP", []byte(payload)); err == nil {
			t.Fatalf("accepted %s", payload)
		}
	}
	for i := 0; i < 19; i++ {
		if !s.Allow("tenant\x00device") {
			t.Fatal("early rate limit")
		}
	}
	if s.Allow("tenant\x00device") {
		t.Fatal("rate limit missing")
	}
}
func TestModbusPreviewAndException(t *testing.T) {
	for _, exception := range []bool{false, true} {
		t.Run(map[bool]string{true: "exception", false: "success"}[exception], func(t *testing.T) {
			s, repo, q := fixture(t)
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()
			done := make(chan struct{})
			go func() {
				defer close(done)
				c, e := ln.Accept()
				if e != nil {
					return
				}
				defer c.Close()
				req := make([]byte, 12)
				if _, e = io.ReadFull(c, req); e != nil {
					return
				}
				response := []byte{req[0], req[1], 0, 0, 0, 5, req[6], 3, 2, 0, 42}
				if exception {
					response = []byte{req[0], req[1], 0, 0, 0, 3, req[6], 0x83, 2}
				}
				_, _ = c.Write(response)
			}()
			q.Type = connector.ModbusTCP
			q.Profile = model.DeviceAccessProfile{Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port, UnitID: 1}
			q.PointTableCSV = "name,functionCode,address,addressNotation,dataType,scale\ntemperature,3,0,zero_based,uint16,1\n"
			r, err := s.Test(context.Background(), "tenant", q)
			if err != nil {
				t.Fatal(err)
			}
			<-done
			if exception {
				if r.Success || r.ErrorCode != "PROTOCOL_ERROR" || r.ExceptionCode != 2 || r.RawRequest == "" || r.RawResponse == "" {
					t.Fatalf("lost exception: %+v", r)
				}
				return
			}
			if !r.Success || len(r.StandardMessages) != 1 || r.RawRequest == "" {
				t.Fatalf("preview: %+v", r)
			}
			if r.StandardMessages[0].Properties["temperature"] != uint64(42) {
				data, _ := json.Marshal(r.StandardMessages[0].Properties)
				if string(data) != `{"temperature":42}` {
					t.Fatalf("unexpected points %s", data)
				}
			}
			q.TestToken = r.TestToken
			if _, err = s.Create(context.Background(), "tenant", q); err != nil {
				t.Fatal(err)
			}
			profiles, _ := repo.ListDeviceAccessProfiles(context.Background(), "tenant")
			if len(profiles) != 1 || !profiles[0].Enabled || profiles[0].ProtocolID == "" {
				t.Fatal("runtime profile missing")
			}
		})
	}
}
