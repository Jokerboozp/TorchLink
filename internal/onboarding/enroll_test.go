package onboarding

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"iot-platform/internal/adapters/clickhouse"
	"iot-platform/internal/adapters/memory"
	redisadapter "iot-platform/internal/adapters/redis"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
	"iot-platform/internal/protocolworker"
)

var standardPayload = json.RawMessage(`{"id":"1","timestamp":1788850000000,"data":{"temperature":26.5}}`)

func enrollFixture(t *testing.T) (*Service, *memory.Repository) {
	t.Helper()
	repo := memory.NewRepository()
	if err := repo.SaveProduct(context.Background(), model.Product{TenantID: "tenant", ID: "product", Name: "温度", Status: "ENABLED", ProtocolPackageID: StandardPackageID, Transport: "HTTP"}); err != nil {
		t.Fatal(err)
	}
	return New(repo, parser.NewPlatformRegistry(t.TempDir()), t.TempDir(), []string{"127.0.0.0/8"}), repo
}

func enrollRequest(id string) EnrollRequest {
	return EnrollRequest{RequestID: "req-" + id, ProductID: "product", Device: EnrollDevice{ID: id, Name: "温度 " + id}, Connection: EnrollConnection{Mode: ModeStandard}}
}

func statusOf(err error) int {
	var e *EnrollError
	if errors.As(err, &e) {
		return e.Status
	}
	return 0
}

// protocolProduct saves a published release and a template bound to it.
func protocolProduct(t *testing.T, repo *memory.Repository, product model.Product, release model.ProtocolRelease, bind bool) {
	t.Helper()
	ctx := context.Background()
	release.TenantID, release.Status = "tenant", "PUBLISHED"
	if err := repo.CreateProtocolRelease(ctx, release); err != nil {
		t.Fatal(err)
	}
	product.TenantID, product.Status = "tenant", "ENABLED"
	product.ProtocolPackageID = release.ProtocolID + "@" + release.Version
	if err := repo.SaveProduct(ctx, product); err != nil {
		t.Fatal(err)
	}
	if bind {
		if err := repo.SaveProductProtocolBinding(ctx, model.ProductProtocolBinding{TenantID: "tenant", ProductID: product.ID, ProtocolID: release.ProtocolID, Version: release.Version}); err != nil {
			t.Fatal(err)
		}
	}
}

func goRelease(id, transport string, capabilities ...string) model.ProtocolRelease {
	return model.ProtocolRelease{ProtocolID: id, Version: "1.0.0", Transport: transport, PayloadFormat: "hex", ParserType: parser.GoProtocolParserName, Artifact: map[string]any{"runtime": protocolworker.Runtime}, Capabilities: capabilities}
}

func TestEnrollStandardDeviceStoresOnlySecretHash(t *testing.T) {
	s, repo := enrollFixture(t)
	ctx := context.Background()
	q := enrollRequest("device")
	q.Device.Tags = map[string]string{"connector": "TCP", "onboardingRequestHash": "forged", "site": "A"}
	r, err := s.Enroll(ctx, "tenant", q)
	if err != nil {
		t.Fatal(err)
	}
	if r.Reused || r.Mode != ModeStandard || r.Credential.Secret == "" || r.Profile != nil {
		t.Fatalf("unexpected result %+v", r)
	}
	d, err := repo.GetManagedDevice(ctx, "tenant", "device")
	if err != nil {
		t.Fatal(err)
	}
	if d.SecretHash != Hash(r.Credential.Secret) || d.RegistrationSource != "ONBOARDING" || d.DeviceRole != "DIRECT" {
		t.Fatalf("stored device %+v", d)
	}
	if d.Tags["connector"] != "HTTP" || d.Tags["site"] != "A" || d.Tags["onboardingRequestHash"] == "forged" || d.Tags["onboardingRequestHash"] == "" {
		t.Fatalf("reserved tags must come from the platform: %+v", d.Tags)
	}
	data, _ := json.Marshal(d)
	if strings.Contains(string(data), r.Credential.Secret) || strings.Contains(string(data), d.SecretHash) {
		t.Fatal("credential leaked")
	}
	if _, err = s.Authenticate(ctx, r.Credential.AccessKey, r.Credential.Secret); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Authenticate(ctx, r.Credential.AccessKey, "wrong"); err == nil {
		t.Fatal("wrong secret accepted")
	}
	retry, err := s.Enroll(ctx, "tenant", q)
	if err != nil || !retry.Reused || retry.Credential.Secret != "" || retry.Mode != ModeStandard {
		t.Fatalf("retry must recover without a secret: %+v %v", retry, err)
	}
	if after, _ := repo.GetManagedDevice(ctx, "tenant", "device"); after.SecretHash != d.SecretHash {
		t.Fatal("retry rotated the credential")
	}
	changed := q
	changed.Device.Name = "changed"
	if _, err = s.Enroll(ctx, "tenant", changed); statusOf(err) != 409 {
		t.Fatalf("different request for a registered device: %v", err)
	}
	if _, err = s.Enroll(ctx, "other", enrollRequest("device-2")); statusOf(err) != 422 {
		t.Fatalf("cross-tenant template accepted: %v", err)
	}
	d.SecretHash = ""
	_ = repo.SaveManagedDevice(ctx, d)
	if _, err = s.PrepareStandard(ctx, "tenant", "product", "device", "property", "MQTT", standardPayload); err != ErrAuth {
		t.Fatalf("disabled credential accepted: %v", err)
	}
}

func TestEnrollRejectsInvalidRequests(t *testing.T) {
	s, repo := enrollFixture(t)
	ctx := context.Background()
	_ = repo.SaveProduct(ctx, model.Product{TenantID: "tenant", ID: "disabled", Name: "停用", Status: "DISABLED", ProtocolPackageID: StandardPackageID})
	_ = repo.SaveProduct(ctx, model.Product{TenantID: "tenant", ID: "bare", Name: "无协议", Status: "ENABLED"})
	cases := map[string]func(*EnrollRequest){
		"missing request id": func(q *EnrollRequest) { q.RequestID = "" },
		"invalid device id":  func(q *EnrollRequest) { q.Device.ID = "../x" },
		"missing name":       func(q *EnrollRequest) { q.Device.Name = " " },
		"child role":         func(q *EnrollRequest) { q.Device.DeviceRole = "CHILD" },
		"disabled template":  func(q *EnrollRequest) { q.ProductID = "disabled" },
		"template without protocol": func(q *EnrollRequest) {
			q.ProductID = "bare"
		},
		"mode mismatch": func(q *EnrollRequest) { q.Connection.Mode = ModePoll },
		"bad transport": func(q *EnrollRequest) { q.Connection.Transport = "COAP" },
		"legacy package for a new template": func(q *EnrollRequest) {
			q.ProductID, q.NewProduct = "", &NewProduct{ID: "legacy", Name: "旧协议", ProtocolPackageID: "json-v1"}
		},
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			q := enrollRequest("device")
			edit(&q)
			if _, err := s.Enroll(ctx, "tenant", q); statusOf(err) != 422 {
				t.Fatalf("want 422, got %v", err)
			}
		})
	}
	if devices, _ := repo.ListManagedDevices(ctx, "tenant"); len(devices) != 0 {
		t.Fatal("rejected requests saved devices")
	}
}

func TestEnrollCreatesStandardTemplateAtomically(t *testing.T) {
	s, repo := enrollFixture(t)
	ctx := context.Background()
	q := enrollRequest("device")
	q.ProductID, q.NewProduct = "", &NewProduct{ID: "new-product", Name: "新产品", Category: "smoke", ProtocolPackageID: StandardPackageID}
	q.Connection.Transport = "mqtt"
	if _, err := s.Enroll(ctx, "tenant", q); err != nil {
		t.Fatal(err)
	}
	p, err := repo.GetProduct(ctx, "tenant", "new-product")
	if err != nil || p.Name != "新产品" || p.Transport != "MQTT" || p.ProtocolPackageID != StandardPackageID || p.Status != "ENABLED" {
		t.Fatalf("template not created: %+v %v", p, err)
	}
	pkg, err := repo.GetProtocolPackage(ctx, "tenant", p.ProtocolPackageID)
	if err != nil || pkg.ParserType != parser.StandardParserName {
		t.Fatal("compatibility package missing", err)
	}
	if d, _ := repo.GetManagedDevice(ctx, "tenant", "device"); d.Tags["connector"] != "MQTT" {
		t.Fatalf("connector %q", d.Tags["connector"])
	}
	again := enrollRequest("device-2")
	again.ProductID, again.NewProduct = "", &NewProduct{ID: "new-product", Name: "重复", ProtocolPackageID: StandardPackageID}
	if _, err = s.Enroll(ctx, "tenant", again); statusOf(err) != 409 {
		t.Fatalf("existing template identifier accepted: %v", err)
	}
}

func TestEnrollThroughProductionRepositoryDecorators(t *testing.T) {
	s, repo := enrollFixture(t)
	telemetry := &clickhouse.Repository{Repository: repo}
	// Onboarding must be forwarded without touching the external cache or
	// telemetry service. This is the same nesting as the production wiring.
	s.Repo = redisadapter.New(telemetry, "127.0.0.1:1", "")
	if _, err := s.Enroll(context.Background(), "tenant", enrollRequest("device")); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetManagedDevice(context.Background(), "tenant", "device"); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentEnrollRecoversWithoutCredentialRotation(t *testing.T) {
	s, repo := enrollFixture(t)
	q := enrollRequest("device")
	q.ProductID, q.NewProduct = "", &NewProduct{ID: "new-product", Name: "new product", ProtocolPackageID: StandardPackageID}
	const count = 12
	var wg sync.WaitGroup
	results := make(chan EnrollResult, count)
	failures := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := s.Enroll(context.Background(), "tenant", q)
			results <- r
			failures <- e
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	secrets := 0
	for r := range results {
		if r.Credential.Secret != "" {
			secrets++
			if r.Reused {
				t.Fatal("replay disclosed a secret")
			}
		}
	}
	if secrets != 1 {
		t.Fatalf("generated %d secrets", secrets)
	}
	if devices, _ := repo.ListManagedDevices(context.Background(), "tenant"); len(devices) != 1 {
		t.Fatal("duplicate devices")
	}
}

type failingOnboardingRepository struct{ ports.Repository }

func (r failingOnboardingRepository) SaveOnboarding(context.Context, model.OnboardingBundle) error {
	return errors.New("injected storage failure")
}

func TestFailedEnrollLeavesNoResourcesAndCanRetry(t *testing.T) {
	s, repo := enrollFixture(t)
	q := enrollRequest("device")
	q.ProductID, q.NewProduct = "", &NewProduct{ID: "new-product", Name: "new product", ProtocolPackageID: StandardPackageID}
	s.Repo = failingOnboardingRepository{repo}
	if _, err := s.Enroll(context.Background(), "tenant", q); err == nil {
		t.Fatal("failed storage reported success")
	}
	if _, err := repo.GetProduct(context.Background(), "tenant", "new-product"); err == nil {
		t.Fatal("failed request leaked the template")
	}
	if _, err := repo.GetManagedDevice(context.Background(), "tenant", "device"); err == nil {
		t.Fatal("failed request leaked the device")
	}
	s.Repo = repo
	if r, err := s.Enroll(context.Background(), "tenant", q); err != nil || r.Reused || r.Credential.Secret == "" {
		t.Fatal("retry failed", err)
	}
}

func TestEnrollListenerCreatesReusesAndDialsConnections(t *testing.T) {
	s, repo := enrollFixture(t)
	ctx := context.Background()
	protocolProduct(t, repo, model.Product{ID: "gw", Name: "网关", Category: "gateway"}, goRelease("fire", "TCP", "ingress", "decode"), true)
	status := "LISTENING"
	s.ListenerStatus = func(tenant, id string) (string, string, int64) { return status, "", 0 }
	check, err := s.Preflight(ctx, "tenant", "gw", nil, PublicAddresses{})
	if err != nil || check.Plan.Mode != ModeListener || !check.Plan.Dial || len(check.Profiles) != 0 || !check.Ready {
		t.Fatalf("preflight %+v %v", check, err)
	}
	q := enrollRequest("gw-1")
	q.ProductID, q.Connection = "gw", EnrollConnection{Mode: ModeListener, Listener: &EnrollListener{PublicHost: "iot.example.com", Port: 9100}}
	r, err := s.Enroll(ctx, "tenant", q)
	if err != nil {
		t.Fatal(err)
	}
	if r.Profile == nil || r.Profile.ID != "gw-tcp-9100" || r.Profile.Host != "0.0.0.0" || r.Profile.ConnectionMode != "listen" || r.Profile.DeviceID != "" {
		t.Fatalf("listener %+v", r.Profile)
	}
	if r.Credential.Secret != "" || r.Device.AccessKey != "" || r.Device.DeviceRole != "GATEWAY" || r.Device.Tags["connector"] != "TCP" || r.Device.Tags["connectorProfileId"] != "gw-tcp-9100" {
		t.Fatalf("protocol device %+v", r.Device)
	}
	check, _ = s.Preflight(ctx, "tenant", "gw", nil, PublicAddresses{})
	if len(check.Profiles) != 1 || check.Profiles[0].RuntimeStatus != "LISTENING" {
		t.Fatalf("shared listener not offered: %+v", check.Profiles)
	}
	shared := enrollRequest("gw-2")
	shared.ProductID, shared.Connection = "gw", EnrollConnection{Mode: ModeListener, ProfileID: "gw-tcp-9100"}
	if r, err = s.Enroll(ctx, "tenant", shared); err != nil || r.Profile == nil || r.Profile.ID != "gw-tcp-9100" {
		t.Fatalf("reuse %+v %v", r.Profile, err)
	}
	if profiles, _ := repo.ListDeviceAccessProfiles(ctx, "tenant"); len(profiles) != 1 {
		t.Fatalf("reuse created %d profiles", len(profiles))
	}
	dial := enrollRequest("gw-3")
	dial.ProductID, dial.Connection = "gw", EnrollConnection{Mode: "dial", Host: "127.0.0.1", Port: 9200}
	if r, err = s.Enroll(ctx, "tenant", dial); err != nil || r.Mode != "dial" || r.Profile == nil || r.Profile.ConnectionMode != "dial" || r.Profile.DeviceID != "gw-3" {
		t.Fatalf("dial %+v %v", r, err)
	}
	outside := enrollRequest("gw-4")
	outside.ProductID, outside.Connection = "gw", EnrollConnection{Mode: "dial", Host: "10.0.0.5", Port: 9200}
	if _, err = s.Enroll(ctx, "tenant", outside); statusOf(err) != 422 {
		t.Fatalf("address outside the allowed networks accepted: %v", err)
	}
	noHost := enrollRequest("gw-5")
	noHost.ProductID, noHost.Connection = "gw", EnrollConnection{Mode: ModeListener, Listener: &EnrollListener{Port: 9101}}
	if _, err = s.Enroll(ctx, "tenant", noHost); statusOf(err) != 422 {
		t.Fatalf("listener without public address accepted: %v", err)
	}
	protocolProduct(t, repo, model.Product{ID: "other"}, goRelease("other", "TCP", "ingress", "decode"), true)
	taken := enrollRequest("other-1")
	taken.ProductID, taken.Connection = "other", EnrollConnection{Mode: ModeListener, Listener: &EnrollListener{PublicHost: "iot.example.com", Port: 9100}}
	if _, err = s.Enroll(ctx, "tenant", taken); statusOf(err) != 409 {
		t.Fatalf("reserved port accepted: %v", err)
	}
	protocolProduct(t, repo, model.Product{ID: "decode-only"}, goRelease("decode-only", "TCP", "decode"), true)
	if check, _ = s.Preflight(ctx, "tenant", "decode-only", nil, PublicAddresses{}); check.Plan.Mode != ModeUnsupported || check.Ready {
		t.Fatalf("protocol without ingress offered: %+v", check.Plan)
	}
}

func TestEnrollPollCreatesDeviceConnectionAndBinding(t *testing.T) {
	s, repo := enrollFixture(t)
	ctx := context.Background()
	protocolProduct(t, repo, model.Product{ID: "meter"}, model.ProtocolRelease{ProtocolID: "meter", Version: "1", Transport: "MODBUS_TCP", ParserType: parser.ModbusTCPParserName}, false)
	q := enrollRequest("meter-1")
	q.ProductID, q.Connection = "meter", EnrollConnection{Mode: ModePoll, Host: "127.0.0.1"}
	r, err := s.Enroll(ctx, "tenant", q)
	if err != nil {
		t.Fatal(err)
	}
	p := r.Profile
	if p == nil || p.Mode != "poll" || p.Port != 502 || p.UnitID != 1 || p.TimeoutMs != 3000 || p.DeviceID != "meter-1" || p.WireFormat != "" {
		t.Fatalf("poll profile %+v", p)
	}
	if r.Credential.Secret != "" || r.Device.Tags["connector"] != "MODBUS_TCP" {
		t.Fatalf("device %+v", r.Device)
	}
	if binding, err := repo.GetProductProtocolBinding(ctx, "tenant", "meter"); err != nil || binding.ProtocolID != "meter" || binding.Version != "1" {
		t.Fatalf("binding %+v %v", binding, err)
	}
	protocolProduct(t, repo, model.Product{ID: "rtu"}, model.ProtocolRelease{ProtocolID: "rtu", Version: "1", Transport: "MODBUS_RTU", ParserType: parser.ModbusRTUParserName}, true)
	rtu := enrollRequest("rtu-1")
	unit := 7
	rtu.ProductID, rtu.Connection = "rtu", EnrollConnection{Mode: ModePoll, Host: "127.0.0.1", Port: 4001, UnitID: &unit}
	if r, err = s.Enroll(ctx, "tenant", rtu); err != nil || r.Profile.WireFormat != "rtu_over_tcp" || r.Profile.UnitID != 7 || r.Device.Tags["connector"] != "MODBUS_RTU_TCP" {
		t.Fatalf("rtu %+v %v", r.Profile, err)
	}
	bad := enrollRequest("meter-2")
	unit = 300
	bad.ProductID, bad.Connection = "meter", EnrollConnection{Mode: ModePoll, Host: "127.0.0.1", UnitID: &unit}
	if _, err = s.Enroll(ctx, "tenant", bad); statusOf(err) != 422 {
		t.Fatalf("invalid unit accepted: %v", err)
	}
}

func TestEnrollManagedProtocolIssuesCredential(t *testing.T) {
	s, repo := enrollFixture(t)
	protocolProduct(t, repo, model.Product{ID: "json"}, goRelease("json", "MQTT_HTTP", "decode"), true)
	check, err := s.Preflight(context.Background(), "tenant", "json", nil, PublicAddresses{})
	if err != nil || check.Plan.Mode != ModeManaged || check.Checks[len(check.Checks)-1].State != "warning" {
		t.Fatalf("preflight %+v %v", check, err)
	}
	q := enrollRequest("json-1")
	q.ProductID, q.Connection.Mode = "json", ModeManaged
	r, err := s.Enroll(context.Background(), "tenant", q)
	if err != nil || r.Credential.Secret == "" || r.Device.Tags["connector"] != "" {
		t.Fatalf("managed %+v %v", r, err)
	}
}

func TestPreflightReportsMissingAddressesAndBroker(t *testing.T) {
	s, repo := enrollFixture(t)
	ctx := context.Background()
	check, err := s.Preflight(ctx, "tenant", "product", nil, PublicAddresses{HTTP: true})
	if err != nil || check.Plan.Connector != "HTTP" || !check.Ready || check.Checks[2].State != "passed" {
		t.Fatalf("http preflight %+v %v", check, err)
	}
	_ = repo.SaveProduct(ctx, model.Product{TenantID: "tenant", ID: "mqtt", Name: "MQTT", Status: "ENABLED", ProtocolPackageID: StandardPackageID, Transport: "MQTT"})
	s.MQTTHealth = func(context.Context) error { return errors.New("down") }
	check, _ = s.Preflight(ctx, "tenant", "mqtt", nil, PublicAddresses{HTTP: true})
	states := map[string]string{}
	for _, c := range check.Checks {
		states[c.Key] = c.State
	}
	if states["address"] != "warning" || states["broker"] != "warning" || !check.Ready {
		t.Fatalf("mqtt preflight %+v", check.Checks)
	}
	draft, err := s.Preflight(ctx, "tenant", "", &NewProduct{ProtocolPackageID: "json-v1"}, PublicAddresses{})
	if err != nil || draft.Ready || draft.Plan.Mode != ModeUnsupported {
		t.Fatalf("legacy package draft %+v %v", draft, err)
	}
	if _, err = s.Preflight(ctx, "other", "product", nil, PublicAddresses{}); statusOf(err) != 404 {
		t.Fatalf("cross-tenant template: %v", err)
	}
}

func TestStandardEnvelopeAndIdempotency(t *testing.T) {
	s, _ := enrollFixture(t)
	ctx := context.Background()
	if _, err := s.Enroll(ctx, "tenant", enrollRequest("device")); err != nil {
		t.Fatal(err)
	}
	a, err := s.PrepareStandard(ctx, "tenant", "product", "device", "property", "HTTP", standardPayload)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := StandardRaw("tenant", "product", "device", "property", "MQTT", standardPayload)
	other, _ := StandardRaw("tenant", "product", "other", "property", "HTTP", standardPayload)
	if a.MessageID != b.MessageID || a.MessageID == other.MessageID {
		t.Fatal("idempotency not scoped")
	}
	if string(a.Payload) != string(standardPayload) {
		t.Fatal("raw body changed")
	}
	for _, kind := range []string{"property", "event", "state"} {
		raw, err := StandardRaw("tenant", "product", "device", kind, "MQTT", standardPayload)
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
