package onboarding

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"iot-platform/internal/adapters/clickhouse"
	"iot-platform/internal/adapters/memory"
	redisadapter "iot-platform/internal/adapters/redis"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
	"iot-platform/internal/protocolworker"
)

type credentialOutageRepository struct{ ports.Repository }

func (credentialOutageRepository) GetManagedDeviceByAccessKey(context.Context, string) (model.ManagedDevice, error) {
	return model.ManagedDevice{}, errors.New("failed to connect: too many clients already")
}

// Only a missing or mismatched credential is an authentication failure; a
// repository failure must let the caller answer "retry later".
func TestAuthenticateSeparatesOutageFromInvalidCredential(t *testing.T) {
	if _, err := New(credentialOutageRepository{memory.NewRepository()}, nil, "", nil).Authenticate(context.Background(), "dk_1", "secret"); !errors.Is(err, ErrUnavailable) || errors.Is(err, ErrAuth) {
		t.Fatalf("outage must be ErrUnavailable, got %v", err)
	}
	if _, err := New(memory.NewRepository(), nil, "", nil).Authenticate(context.Background(), "dk_missing", "secret"); !errors.Is(err, ErrAuth) {
		t.Fatalf("unknown key must be ErrAuth, got %v", err)
	}
}

// A full rate table admits new devices once earlier windows end, instead of
// rejecting every device beyond the limit for a minute.
func TestRateTableReleasesEndedWindows(t *testing.T) {
	s := New(memory.NewRepository(), nil, "", nil)
	for i := 0; i < rateTableLimit; i++ {
		if !s.Allow(fmt.Sprintf("tenant\x00device-%d", i)) {
			t.Fatalf("device %d rejected before the table was full", i)
		}
	}
	if s.Allow("tenant\x00late-device") {
		t.Fatal("a full table must reject a new key within the same second")
	}
	time.Sleep(1100 * time.Millisecond)
	if !s.Allow("tenant\x00late-device") {
		t.Fatal("a new device must be admitted once earlier windows ended")
	}
}

func TestDiagnoseOrdersFixesBeforeDataStages(t *testing.T) {
	listening := &model.DeviceAccessProfile{Mode: "listener", Enabled: true, RuntimeStatus: "LISTENING", PublicHost: "iot.example.com"}
	ready := DiagnosisInput{ProductEnabled: true, ProtocolValid: true, DeviceEnabled: true, Profile: listening}
	cases := []struct {
		name  string
		edit  func(*DiagnosisInput)
		stage string
		tone  string
	}{
		{"product disabled wins", func(in *DiagnosisInput) { in.ProductEnabled = false; in.Ingest.Parsed = true }, "PRODUCT_DISABLED", "warning"},
		{"protocol invalid", func(in *DiagnosisInput) { in.ProtocolValid = false }, "PROTOCOL_INVALID", "warning"},
		{"device disabled", func(in *DiagnosisInput) { in.DeviceEnabled = false }, "DEVICE_DISABLED", "warning"},
		{"child without visible parent", func(in *DiagnosisInput) { in.IsChild = true }, "PARENT_UNAVAILABLE", "warning"},
		{"protocol device without connection", func(in *DiagnosisInput) { in.Profile = nil }, "PROFILE_MISSING", "warning"},
		{"disabled listener", func(in *DiagnosisInput) { p := *listening; p.Enabled = false; in.Profile = &p }, "PROFILE_DISABLED", "warning"},
		{"listener runtime error", func(in *DiagnosisInput) { p := *listening; p.RuntimeStatus = "ERROR"; in.Profile = &p }, "PROFILE_ERROR", "error"},
		{"listener without public host", func(in *DiagnosisInput) { p := *listening; p.PublicHost = ""; in.Profile = &p }, "PUBLIC_HOST_MISSING", "warning"},
		{"dial profile needs no public host", func(in *DiagnosisInput) {
			p := *listening
			p.PublicHost = ""
			p.ConnectionMode = "dial"
			in.Profile = &p
		}, "WAITING", "info"},
		{"parse failure", func(in *DiagnosisInput) { in.Ingest = IngestSummary{RawReceived: true, ParseError: "bad frame"} }, "PARSE_FAILED", "error"},
		{"raw waiting for parser", func(in *DiagnosisInput) { in.Ingest = IngestSummary{RawReceived: true} }, "RAW_RECEIVED", "info"},
		{"stale after success", func(in *DiagnosisInput) { in.Ingest = IngestSummary{RawReceived: true, Parsed: true, Stale: true} }, "STALE", "warning"},
		{"single success", func(in *DiagnosisInput) { in.Ingest = IngestSummary{RawReceived: true, Parsed: true} }, "PARSED", "success"},
		{"continuous success", func(in *DiagnosisInput) {
			in.Ingest = IngestSummary{RawReceived: true, Parsed: true, ContinuouslyUpdating: true}
		}, "CONTINUOUS", "success"},
		{"credential device without public address", func(in *DiagnosisInput) { in.Profile = nil; in.UsesCredentials = true }, "ADDRESS_MISSING", "warning"},
		{"history is not current evidence", func(in *DiagnosisInput) { in.Ingest.PreviousParsedAt = 1700000000000 }, "PREVIOUSLY_PARSED", "info"},
		{"waiting", func(*DiagnosisInput) {}, "WAITING", "info"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := ready
			c.edit(&in)
			got := Diagnose(in)
			if got.Stage != c.stage || got.Tone != c.tone || got.Title == "" || got.NextAction == "" {
				t.Fatalf("Diagnose() = %+v, want stage %s tone %s", got, c.stage, c.tone)
			}
			if len(got.Checks) != 5 {
				t.Fatalf("checks = %d, want 5", len(got.Checks))
			}
		})
	}
}

func TestDiagnosisChecksReflectEachStage(t *testing.T) {
	got := Diagnose(DiagnosisInput{
		ProductEnabled: true, ProtocolValid: true, DeviceEnabled: true, UsesCredentials: true, AddressReady: true,
		Ingest: IngestSummary{ConfigurationSaved: true, RawReceived: true, ReceivedAt: 42, Parsed: true, ContinuouslyUpdating: true},
	})
	states := map[string]string{}
	for _, check := range got.Checks {
		states[check.Key] = check.State
	}
	for _, key := range []string{"configuration", "service", "raw", "parsed", "continuous"} {
		if states[key] != "passed" {
			t.Fatalf("%s = %q, want passed; checks %+v", key, states[key], got.Checks)
		}
	}
	if got.Checks[2].At != 42 {
		t.Fatalf("raw check should carry receivedAt: %+v", got.Checks[2])
	}
	failed := Diagnose(DiagnosisInput{ProductEnabled: true, ProtocolValid: true, DeviceEnabled: true, UsesCredentials: true, Ingest: IngestSummary{Stale: true, Parsed: true, RawReceived: true, ParseError: "x"}})
	for _, check := range failed.Checks {
		if (check.Key == "service" || check.Key == "parsed" || check.Key == "continuous") && check.State != "failed" {
			t.Fatalf("%s should fail: %+v", check.Key, failed.Checks)
		}
	}
}

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
	if d.Connector != "HTTP" || d.Tags["site"] != "A" || d.OnboardingRequestHash == "forged" || d.OnboardingRequestHash == "" {
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
	if d, _ := repo.GetManagedDevice(ctx, "tenant", "device"); d.Connector != "MQTT" {
		t.Fatalf("connector %q", d.Connector)
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
	s.Repo = redisadapter.New(telemetry, redisadapter.NewClient(redisadapter.Options{Addr: "127.0.0.1:1"}))
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
	if r.Credential.Secret != "" || r.Device.AccessKey != "" || r.Device.DeviceRole != "GATEWAY" || r.Device.Connector != "TCP" || r.Device.ConnectorProfileID != "gw-tcp-9100" {
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
	if r.Credential.Secret != "" || r.Device.Connector != "MODBUS_TCP" {
		t.Fatalf("device %+v", r.Device)
	}
	if binding, err := repo.GetProductProtocolBinding(ctx, "tenant", "meter"); err != nil || binding.ProtocolID != "meter" || binding.Version != "1" {
		t.Fatalf("binding %+v %v", binding, err)
	}
	protocolProduct(t, repo, model.Product{ID: "rtu"}, model.ProtocolRelease{ProtocolID: "rtu", Version: "1", Transport: "MODBUS_RTU", ParserType: parser.ModbusRTUParserName}, true)
	rtu := enrollRequest("rtu-1")
	unit := 7
	rtu.ProductID, rtu.Connection = "rtu", EnrollConnection{Mode: ModePoll, Host: "127.0.0.1", Port: 4001, UnitID: &unit}
	if r, err = s.Enroll(ctx, "tenant", rtu); err != nil || r.Profile.WireFormat != "rtu_over_tcp" || r.Profile.UnitID != 7 || r.Device.Connector != "MODBUS_RTU_TCP" {
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
	if err != nil || r.Credential.Secret == "" || r.Device.Connector != "" {
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

func operationService(t *testing.T) *Service {
	t.Helper()
	r := memory.NewRepository()
	ctx := context.Background()
	if e := r.SaveProduct(ctx, model.Product{TenantID: "t", ID: "p", Status: "ENABLED"}); e != nil {
		t.Fatal(e)
	}
	if e := r.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "t", ID: "d", ProductID: "p", Status: "ENABLED", AccessKey: "old", SecretHash: Hash("secret"), Connector: "MQTT"}); e != nil {
		t.Fatal(e)
	}
	return New(r, nil, "", nil)
}
func TestCommandConcurrencyAndEarlyReply(t *testing.T) {
	s := operationService(t)
	ctx := context.Background()
	var sent atomic.Int32
	s.PublishCommand = func(ctx context.Context, topic string, b []byte, qos byte, retained bool) error {
		sent.Add(1)
		if topic != "/iot/down/t/p/d/command" || retained {
			t.Error("wrong command routing")
		}
		var v map[string]any
		if json.Unmarshal(b, &v) != nil {
			t.Error("invalid envelope")
		}
		return s.Repo.CompleteDeviceCommand(ctx, "t", "d", "cmd1", map[string]any{"success": true}, 2)
	}
	q := model.DeviceCommand{Confirmed: true, ID: "cmd1", Type: "reboot", Data: map[string]any{}}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := s.SendCommand(ctx, "t", "d", q); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if sent.Load() != 1 {
		t.Fatal("duplicate physical dispatch", sent.Load())
	}
	v, _, e := s.Repo.ListDeviceCommands(ctx, "t", "d", 20, 0)
	if e != nil || len(v) != 1 || v[0].Status != "SUCCEEDED" {
		t.Fatal(v, e)
	}
	q.Data = map[string]any{"different": true}
	if _, e = s.SendCommand(ctx, "t", "d", q); e == nil {
		t.Fatal("conflicting id accepted")
	}
	if _, e = s.SendCommand(ctx, "other", "d", q); e == nil {
		t.Fatal("cross-tenant command accepted")
	}
}
func TestCommandUnknownIsNotRetried(t *testing.T) {
	s := operationService(t)
	n := 0
	s.PublishCommand = func(context.Context, string, []byte, byte, bool) error { n++; return errors.New("connection lost") }
	q := model.DeviceCommand{Confirmed: true, ID: "cmd1", Type: "open", Data: map[string]any{}}
	for i := 0; i < 2; i++ {
		v, e := s.SendCommand(context.Background(), "t", "d", q)
		if e != nil || v.Status != "UNKNOWN" {
			t.Fatal(v, e)
		}
	}
	if n != 1 {
		t.Fatal(n)
	}
}
func TestCredentialOutboxAndRecovery(t *testing.T) {
	s := operationService(t)
	ctx := context.Background()
	c, v, e := s.ChangeCredential(ctx, "t", "d", true)
	if e != nil || v.Status != "PENDING" || v.Username != "old" || c.Secret == "" {
		t.Fatal(v, e)
	}
	if _, e = s.Authenticate(ctx, "old", "secret"); e == nil {
		t.Fatal("old credential accepted")
	}
	if _, e = s.Authenticate(ctx, c.AccessKey, c.Secret); e != nil {
		t.Fatal(e)
	}
	s.RevokeUsername = func(_ context.Context, user string) error {
		if user != "old" {
			t.Fatal(user)
		}
		return nil
	}
	v = s.revoke(ctx, v)
	if v.Status != "REVOKED" {
		t.Fatal(v)
	}
	s.RevokeUsername = nil
	_, v, e = s.ChangeCredential(ctx, "t", "d", false)
	if e != nil || v.Username != c.AccessKey {
		t.Fatal(v, e)
	}
	if _, e = s.Authenticate(ctx, c.AccessKey, c.Secret); e == nil {
		t.Fatal("disabled credential accepted")
	}
	data, _ := json.Marshal(v)
	if strings.Contains(string(data), c.Secret) {
		t.Fatal("secret exposed")
	}
	items, _ := s.Repo.ListCredentialRevocations(ctx, "t", "d", false)
	if len(items) != 2 {
		t.Fatal(items)
	}
}
func TestThingModelValidation(t *testing.T) {
	s := operationService(t)
	ctx := context.Background()
	m := &model.ThingModel{Commands: []model.ThingOperation{{Identifier: "set", Fields: []model.ThingField{{Identifier: "value", DataType: "integer", Required: true}}}}}
	if e := ValidateThingModel(m); e != nil {
		t.Fatal(e)
	}
	m.Properties = []model.ThingField{{Identifier: "a", DataType: "bad"}}
	if ValidateThingModel(m) == nil {
		t.Fatal("invalid model accepted")
	}
	m.Properties = nil
	s.Repo.SaveProduct(ctx, model.Product{TenantID: "t", ID: "p", Status: "ENABLED", ThingModel: m})
	s.PublishCommand = func(context.Context, string, []byte, byte, bool) error { return nil }
	if _, e := s.SendCommand(ctx, "t", "d", model.DeviceCommand{Confirmed: true, ID: "c", Type: "set", Data: map[string]any{"value": 1.2}}); e == nil {
		t.Fatal("invalid integer accepted")
	}
}

func TestCommandRequiresManualConfirmation(t *testing.T) {
	s := operationService(t)
	s.PublishCommand = func(context.Context, string, []byte, byte, bool) error {
		t.Fatal("unconfirmed command was dispatched")
		return nil
	}
	if _, e := s.SendCommand(context.Background(), "t", "d", model.DeviceCommand{ID: "unconfirmed", Type: "reset", Data: map[string]any{}}); e == nil {
		t.Fatal("unconfirmed command accepted")
	}
}
