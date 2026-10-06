package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"iot-platform/internal/devicescope"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
	"iot-platform/internal/protocolworker"
)

func TestRegisterConfiguredChildUsesStableParentAddress(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(devicescope.Wrap(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(t.TempDir()), log)
	cfg := config.Load()
	cfg.JWTSecret = "child-register-test-signing-key-32-characters"
	cfg.AdminTenants = []string{"tenant"}
	srv := New(cfg, engine, metrics.New(), log)
	token, _ := srv.auth.Issue("tester", "tenant", "admin", nil, time.Hour)
	for _, p := range []model.Product{{TenantID: "tenant", ID: "parent-product", Status: "ENABLED", Transport: "TCP", ProtocolPackageID: "parent-protocol@1"}, {TenantID: "tenant", ID: "child-product", Status: "ENABLED"}} {
		if err := repo.SaveProduct(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.CreateProtocolRelease(ctx, model.ProtocolRelease{TenantID: "tenant", ProtocolID: "child-protocol", Version: "1", Status: "PUBLISHED"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateProtocolRelease(ctx, model.ProtocolRelease{TenantID: "tenant", ProtocolID: "parent-protocol", Version: "1", Status: "PUBLISHED", Transport: "TCP", ParserType: parser.GoProtocolParserName, Artifact: map[string]any{"runtime": protocolworker.Runtime}, Capabilities: []string{"ingress", "decode"}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveProductProtocolBinding(ctx, model.ProductProtocolBinding{TenantID: "tenant", ProductID: "child-product", ProtocolID: "child-protocol", Version: "1"}); err != nil {
		t.Fatal(err)
	}
	profile := model.DeviceAccessProfile{TenantID: "tenant", ID: "listener", ProductID: "parent-product", Mode: "listener", Network: "tcp", Enabled: true, ChildProducts: []model.ChildProductBinding{{Type: "sensor", ProductID: "child-product"}}}
	if err := repo.SaveDeviceAccessProfile(ctx, profile); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "tenant", ID: "parent", ProductID: "parent-product", Name: "主设备", Status: "ENABLED", DeviceRole: "GATEWAY", ConnectorProfileID: "listener"}); err != nil {
		t.Fatal(err)
	}
	call := func(body string) (int, map[string]any) {
		t.Helper()
		r := httptest.NewRequest("POST", "/api/v1/device-registry/parent/children", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, r)
		var result map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return w.Code, result
	}
	code, first := call(`{"address":"1-7","type":"sensor","name":"一层探测器"}`)
	if code != 201 {
		t.Fatalf("first registration: %d %+v", code, first)
	}
	id := first["device"].(map[string]any)["id"]
	if id != model.ChildDeviceID("tenant", "parent", "1-7") {
		t.Fatalf("unstable child ID: %v", id)
	}
	code, again := call(`{"address":"1-7","type":"sensor","name":"一层探测器"}`)
	if code != 200 || again["device"].(map[string]any)["id"] != id {
		t.Fatalf("duplicate child: %d %+v", code, again)
	}
	if code, _ := call(`{"address":"1-7","type":"unknown"}`); code != 422 {
		t.Fatalf("unmapped child accepted: %d", code)
	}
	child, err := repo.GetManagedDevice(ctx, "tenant", id.(string))
	if err != nil || child.GatewayID != "parent" || child.ConnectorProfileID != "listener" {
		t.Fatalf("child relation: %+v %v", child, err)
	}
	if err := repo.SaveDeviceAccessProfile(ctx, model.DeviceAccessProfile{TenantID: "tenant", ID: "foreign", ProductID: "child-product", Mode: "listener", Network: "tcp", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/api/v1/device-registry", strings.NewReader(`{"id":"wrong","productId":"parent-product","name":"错误关联","trial":true,"tags":{"connectorProfileId":"foreign"}}`))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, r)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "当前设备模板的接入点") {
		t.Fatalf("cross-product connection accepted: %d %s", w.Code, w.Body.String())
	}
	if _, err := repo.GetManagedDevice(ctx, "tenant", "wrong"); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("cross-product connection persisted", err)
	}
}

func TestProtocolDevicesHaveNoPlatformCredentials(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	root := t.TempDir()
	archive, err := local.NewArchive(root)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Load()
	cfg.DataDir, cfg.JWTSecret = root, "credential-scope-test-key-32-characters"
	api := New(cfg, core.New(devicescope.Wrap(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(root), log), metrics.New(), log)
	call := func(method, path, body, tenant, role, key, secret string) *httptest.ResponseRecorder {
		token, err := api.auth.Issue("tester", tenant, role, nil, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Device-Key", key)
		req.Header.Set("X-Device-Secret", secret)
		w := httptest.NewRecorder()
		api.Handler().ServeHTTP(w, req)
		return w
	}
	for _, transport := range []string{"TCP", "UDP", "TCP_UDP", "MODBUS_TCP", "MODBUS_RTU_TCP"} {
		t.Run(transport, func(t *testing.T) {
			id := strings.ToLower(transport)
			p := model.Product{TenantID: "tenant", ID: id, Status: "ENABLED", Transport: transport}
			if err := repo.SaveProduct(ctx, p); err != nil {
				t.Fatal(err)
			}
			w := call("POST", "/api/v1/device-registry", `{"id":"`+id+`","name":"设备","productId":"`+id+`"}`, "tenant", "admin", "", "")
			if w.Code != 409 || bytes.Contains(w.Body.Bytes(), []byte(`"credential"`)) || bytes.Contains(w.Body.Bytes(), []byte(`"accessKey"`)) {
				t.Fatalf("unprepared protocol template allowed registration: %d %s", w.Code, w.Body.String())
			}
			if _, err := repo.GetManagedDevice(ctx, "tenant", id); !errors.Is(err, model.ErrNotFound) {
				t.Fatal("rejected registration persisted", err)
			}
			discoveredID := "discovered-" + id
			if err := repo.UpsertDeviceState(ctx, model.DeviceState{TenantID: "tenant", DeviceID: discoveredID, ProductID: id}); err != nil {
				t.Fatal(err)
			}
			w = call("POST", "/api/v1/discovered-devices/"+discoveredID+"/register", "{}", "tenant", "admin", "", "")
			if w.Code != 409 || bytes.Contains(w.Body.Bytes(), []byte(`"credential"`)) || bytes.Contains(w.Body.Bytes(), []byte(`"accessKey"`)) {
				t.Fatalf("unprepared discovery generated credentials: %d %s", w.Code, w.Body.String())
			}
			if _, err := repo.GetManagedDevice(ctx, "tenant", discoveredID); !errors.Is(err, model.ErrNotFound) {
				t.Fatal("rejected discovery persisted", err)
			}
			// Existing records may still contain formerly generated secrets. They
			// must not expose or accept those as an alternate HTTP/MQTT identity.
			d := model.ManagedDevice{TenantID: "tenant", ID: id, Name: "历史协议设备", ProductID: id, Status: "ENABLED", AccessKey: model.ProtocolDeviceAccessKey("tenant", id), SecretHash: onboarding.Hash("old-secret"), SecretHint: "secret"}
			if err := repo.SaveManagedDevice(ctx, d); err != nil {
				t.Fatal(err)
			}
			w = call("GET", "/api/v1/device-registry/"+id+"/connection", "", "tenant", "admin", "", "")
			if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"credentialSupported":false`)) || bytes.Contains(w.Body.Bytes(), []byte(`"accessKey"`)) {
				t.Fatalf("unexpected detail: %d %s", w.Code, w.Body.String())
			}
			for _, method := range []string{"POST", "DELETE"} {
				path := "/api/v1/device-registry/" + id + "/credentials"
				for _, scope := range []struct {
					tenant, role string
					status       int
				}{{"tenant", "admin", 422}, {"tenant", "viewer", 403}, {"other", "admin", 404}} {
					w = call(method, path, "{}", scope.tenant, scope.role, "", "")
					if w.Code != scope.status {
						t.Fatalf("%s %s: %d want %d", method, path, w.Code, scope.status)
					}
				}
			}
			for _, path := range []string{"/api/v1/device-mqtt/token", "/api/v1/device-ingest/" + id, "/api/v1/device-ingest/standard/tenant/" + id + "/" + id + "/property"} {
				w = call("POST", path, "{}", "tenant", "admin", d.AccessKey, "old-secret")
				if w.Code != 401 {
					t.Fatalf("native device authenticated on %s: %d", path, w.Code)
				}
			}
			after, _ := repo.GetManagedDevice(ctx, "tenant", id)
			if after.AccessKey != d.AccessKey || after.SecretHash != d.SecretHash {
				t.Fatal("rejected operation mutated device")
			}
			revocations, err := repo.ListCredentialRevocations(ctx, "tenant", id, false)
			if err != nil || len(revocations) != 0 {
				t.Fatal("rejected operation generated revocations", err)
			}
		})
	}
	w := call("GET", "/api/v1/device-registry", "", "tenant", "admin", "", "")
	if w.Code != 200 || bytes.Contains(w.Body.Bytes(), []byte(`"accessKey"`)) {
		t.Fatal("registry exposes protocol keys", w.Code)
	}
}

func TestDeviceRegistryFiltersBeforePagination(t *testing.T) {
	forEachStore(t, checkDeviceRegistryFiltersBeforePagination)
}

func checkDeviceRegistryFiltersBeforePagination(t *testing.T, repo ports.Repository) {
	ctx := context.Background()
	api := newTestAPI(t, repo, func(cfg *config.Config) {
		cfg.JWTSecret = "test-only-secret-for-device-filters"
	})
	cfg := api.cfg
	req := func(method, path, token string, body any, status int) map[string]any {
		return api.request(t, method, path, token, body, status)
	}
	for _, p := range []model.Product{{ID: "gw-product", Category: "gateway"}, {ID: "smoke-product", Category: "smoke"}, {ID: "bare"}} {
		p.TenantID, p.Name, p.Status = "tenant_a", p.ID, "ENABLED"
		if err := repo.SaveProduct(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	// Saved oldest first: PostgreSQL orders by the row's write time, the
	// memory store by UpdatedAt, and in use both advance together.
	devices := []model.ManagedDevice{
		{ID: "other-1", Name: "其他", ProductID: "bare", Status: "ENABLED", UpdatedAt: 1},
		{ID: "direct-1", Name: "独立烟感", ProductID: "smoke-product", DeviceRole: "DIRECT", Status: "DISABLED", UpdatedAt: 2},
		{ID: "child-1", Name: "烟感", ProductID: "smoke-product", DeviceRole: "CHILD", GatewayID: "gw-1", Status: "ENABLED", UpdatedAt: 3},
		{ID: "gw-1", Name: "一号网关", ProductID: "gw-product", DeviceRole: "GATEWAY", Status: "ENABLED", UpdatedAt: 4},
		{ID: "foreign", Name: "其他租户", ProductID: "bare", Status: "ENABLED", TenantID: "tenant_b", UpdatedAt: 5},
	}
	for _, d := range devices {
		if d.TenantID == "" {
			d.TenantID = "tenant_a"
		}
		d.AccessKey = model.ProtocolDeviceAccessKey(d.TenantID, d.ID)
		if err := repo.SaveManagedDevice(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	for id, status := range map[string]string{"gw-1": "ONLINE", "direct-1": "ALARM"} {
		if err := repo.UpsertDeviceState(ctx, model.DeviceState{TenantID: "tenant_a", DeviceID: id, ProductID: "p", BusinessStatus: status}); err != nil {
			t.Fatal(err)
		}
	}
	list := func(token, query string) ([]string, float64) {
		t.Helper()
		body := req("GET", "/api/v1/device-registry?"+query, token, nil, 200)
		ids := []string{}
		for _, row := range body["items"].([]any) {
			ids = append(ids, row.(map[string]any)["device"].(map[string]any)["id"].(string))
		}
		return ids, body["total"].(float64)
	}
	root := api.login(t, "root", cfg.AdminPassword, "tenant_a")
	cases := map[string][]string{
		"":                              {"gw-1", "child-1", "direct-1", "other-1"},
		"role=GATEWAY":                  {"gw-1"},
		"role=child":                    {"child-1"},
		"role=DIRECT":                   {"direct-1", "other-1"},
		"category=other":                {"other-1"},
		"category=smoke&role=DIRECT":    {"direct-1"},
		"productId=smoke-product":       {"child-1", "direct-1"},
		"category=missing":              {},
		"q=%E7%BD%91%E5%85%B3":          {"gw-1"},
		"q=CHILD":                       {"child-1"},
		"status=DISABLED":               {"direct-1"},
		"runtime=NEVER_SEEN":            {"child-1", "other-1"},
		"runtime=ALARM&status=DISABLED": {"direct-1"},
	}
	for query, want := range cases {
		if got, total := list(root, query); !slices.Equal(got, want) || int(total) != len(want) {
			t.Fatalf("%q: got %v (%v), want %v", query, got, total, want)
		}
	}
	child := req("GET", "/api/v1/device-registry?role=CHILD", root, nil, 200)["items"].([]any)[0].(map[string]any)
	if parent, _ := child["parent"].(map[string]any); parent["name"] != "一号网关" {
		t.Fatalf("child row parent: %+v", child)
	}
	if got, total := list(root, "role=DIRECT&pageSize=1&page=2"); !slices.Equal(got, []string{"other-1"}) || total != 2 {
		t.Fatalf("filtered page: %v %v", got, total)
	}
	for _, query := range []string{"role=OWNER", "status=ON", "runtime=BUSY"} {
		req("GET", "/api/v1/device-registry?"+query, root, nil, 422)
	}

	// Users limited to selected devices get the same filters within their scope.
	req("POST", "/api/v1/access/roles", root, map[string]any{"id": "viewer", "name": "查看", "permissions": []string{"menu:devices"}}, 200)
	req("POST", "/api/v1/access/users", root, map[string]any{"username": "viewer", "displayName": "查看", "password": "viewer-password", "enabled": true, "roleIds": []string{"viewer"}, "permissions": []string{}, "deviceScope": "selected", "deviceIds": []string{"gw-1", "direct-1"}}, 200)
	limited := api.login(t, "viewer", "viewer-password", "tenant_a")
	if got, total := list(limited, ""); !slices.Equal(got, []string{"gw-1", "direct-1"}) || total != 2 {
		t.Fatalf("limited scope: %v %v", got, total)
	}
	if got, total := list(limited, "role=DIRECT"); !slices.Equal(got, []string{"direct-1"}) || total != 1 {
		t.Fatalf("limited filter: %v %v", got, total)
	}
	if got, _ := list(limited, "runtime=NEVER_SEEN"); len(got) != 0 {
		t.Fatalf("limited scope leaked devices: %v", got)
	}
}

type connectionSnapshot struct{}

func TestDeviceProfileRequiresIdentityEvidence(t *testing.T) {
	d := model.ManagedDevice{TenantID: "t", ID: "d", ProductID: "p", ConnectorProfileID: "listener"}
	p := model.DeviceAccessProfile{TenantID: "t", ID: "listener", ProductID: "p"}
	if !deviceUsesProfile(d, p, nil) {
		t.Fatal("explicit binding missing")
	}
	for _, other := range []model.DeviceAccessProfile{{TenantID: "other", ID: "listener", ProductID: "p", DeviceID: "d"}, {TenantID: "t", ID: "listener", ProductID: "other", DeviceID: "d"}} {
		if deviceUsesProfile(d, other, []map[string]any{{"deviceId": "d"}}) {
			t.Fatal("identity scope bypass")
		}
	}
	d.ConnectorProfileID = ""
	if deviceUsesProfile(d, p, []map[string]any{{"remoteAddress": "127.0.0.1"}}) {
		t.Fatal("unidentified session matched")
	}
}

func (connectionSnapshot) Status(string, string) (string, string, int64) { return "LISTENING", "", 123 }
func (connectionSnapshot) Sessions(tenant, id string) []map[string]any {
	if tenant == "t" && (id == "listener-a" || id == "listener-b") {
		return []map[string]any{{"deviceId": "legacy", "remoteAddress": "127.0.0.1:1234", "lastSeenAt": int64(123)}}
	}
	return nil
}
func (connectionSnapshot) Command(context.Context, string, string, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

func TestLegacyDeviceConnectionResolution(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	root := t.TempDir()
	archive, err := local.NewArchive(root)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(devicescope.Wrap(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(root), log)
	cfg := config.Load()
	cfg.JWTSecret = "legacy-test-key-32-characters"
	srv := New(cfg, engine, metrics.New(), log)
	srv.SetProtocolListeners(connectionSnapshot{})
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(repo.SaveProduct(ctx, model.Product{TenantID: "t", ID: "p", Status: "ENABLED"}))
	for _, id := range []string{"legacy", "poll-device", "unrelated"} {
		must(repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "t", ID: id, AccessKey: id, ProductID: "p", Status: "ENABLED"}))
	}
	for _, p := range []model.DeviceAccessProfile{
		{TenantID: "t", ID: "listener-a", ProductID: "p", Mode: "listener", Network: "tcp", Enabled: true},
		{TenantID: "t", ID: "listener-b", ProductID: "p", Mode: "listener", Network: "udp", Enabled: false},
		{TenantID: "t", ID: "poll", ProductID: "p", DeviceID: "poll-device", Mode: "poll", Enabled: true},
		{TenantID: "other", ID: "listener-other", ProductID: "p", DeviceID: "legacy", Mode: "listener", Enabled: true},
	} {
		must(repo.SaveDeviceAccessProfile(ctx, p))
	}
	call := func(method, path, body string) (int, map[string]any) {
		t.Helper()
		token, _ := srv.auth.Issue("test", "t", "admin", nil, time.Hour)
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, r)
		var v map[string]any
		must(json.Unmarshal(w.Body.Bytes(), &v))
		return w.Code, v
	}
	code, v := call("GET", "/api/v1/device-registry/legacy/connection", "")
	if code != 200 || v["profile"] != nil || len(v["profiles"].([]any)) != 2 || len(v["sessions"].([]any)) != 2 {
		t.Fatalf("ambiguous: %d %+v", code, v)
	}
	code, v = call("GET", "/api/v1/device-registry/legacy/connection?profileId=listener-b", "")
	if code != 200 || v["connector"] != "UDP" || v["profile"].(map[string]any)["runtimeStatus"] != "DISABLED" || len(v["sessions"].([]any)) != 1 {
		t.Fatalf("selected: %d %+v", code, v)
	}
	for _, id := range []string{"poll", "listener-other"} {
		code, _ = call("GET", "/api/v1/device-registry/legacy/connection?profileId="+id, "")
		if code != 422 {
			t.Fatal(code)
		}
	}
	code, v = call("GET", "/api/v1/device-registry/poll-device/connection", "")
	if code != 200 || v["connector"] != "MODBUS_TCP" || v["profile"].(map[string]any)["id"] != "poll" {
		t.Fatalf("poll: %+v", v)
	}
	_, v = call("GET", "/api/v1/device-registry/unrelated/connection", "")
	if v["profile"] != nil || len(v["profiles"].([]any)) != 0 {
		t.Fatalf("product-only match: %+v", v)
	}
	_, v = call("GET", "/api/v1/connectors", "")
	for _, item := range v["items"].([]any) {
		x := item.(map[string]any)
		if len(x["recentDevices"].([]any)) != 1 {
			t.Fatalf("legacy device missing: %+v", x)
		}
	}
	device, err := repo.GetManagedDevice(ctx, "t", "legacy")
	must(err)
	if len(device.Tags) != 0 {
		t.Fatal("read mutated legacy device")
	}
	code, v = call("POST", "/api/v1/onboarding", `{"requestId":"r1","productId":"missing","device":{"id":"test","name":"test"},"connection":{"mode":"standard"}}`)
	if code != 422 || v["detail"] != "设备模板不存在或当前账号无权查看" {
		t.Fatalf("validation result: %d %+v", code, v)
	}
	if _, err = repo.GetManagedDevice(ctx, "t", "test"); err == nil {
		t.Fatal("rejected onboarding saved a device")
	}
}

func TestDeviceOperationsHTTPAndRawReply(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	repo := memory.NewRepository()
	root := t.TempDir()
	archive, e := local.NewArchive(root)
	if e != nil {
		t.Fatal(e)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(devicescope.Wrap(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(root), log)
	if e = engine.Start(ctx); e != nil {
		t.Fatal(e)
	}
	cfg := config.Load()
	cfg.JWTSecret = "operations-test-key-32-characters"
	srv := New(cfg, engine, metrics.New(), log)
	sent := 0
	srv.SetDeviceOperations(func(context.Context, string, []byte, byte, bool) error { sent++; return nil }, nil)
	repo.SaveProduct(ctx, model.Product{TenantID: "t", ID: "p", Status: "ENABLED"})
	repo.CreateProtocolRelease(ctx, model.ProtocolRelease{TenantID: "t", ProtocolID: parser.StandardProtocolID, Version: "1.0.0", Status: "PUBLISHED", ParserType: parser.StandardParserName})
	repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "t", ID: "d", ProductID: "p", Status: "ENABLED", AccessKey: "key", SecretHash: onboarding.Hash("secret"), Connector: "MQTT"})
	call := func(method, path, body, tenant, role string) *httptest.ResponseRecorder {
		token, _ := srv.auth.Issue("test", tenant, role, nil, time.Hour)
		r := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("X-Device-Key", "key")
		r.Header.Set("X-Device-Secret", "secret")
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, r)
		return w
	}
	if w := call("POST", "/api/v1/device-registry/d/commands", `{"confirmed":true,"id":"c1","type":"set","data":{"value":1}}`, "t", "viewer"); w.Code != 403 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := call("POST", "/api/v1/device-registry/d/commands", `{"id":"unconfirmed","type":"reset","data":{}}`, "t", "operator"); w.Code != 422 || sent != 0 {
		t.Fatal("unconfirmed command dispatched", w.Code)
	}
	for i := 0; i < 2; i++ {
		if w := call("POST", "/api/v1/device-registry/d/commands", `{"confirmed":true,"id":"c1","type":"set","data":{"value":1}}`, "t", "operator"); w.Code != 202 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if sent != 1 {
		t.Fatal(sent)
	}
	if w := call("GET", "/api/v1/device-registry/d/history", "", "other", "admin"); w.Code != 404 {
		t.Fatal(w.Code, w.Body.String())
	}
	reply := `{"id":"reply1","timestamp":1788850000001,"data":{"commandId":"c1","success":true,"result":"ok"}}`
	w := call("POST", "/api/v1/device-ingest/standard/t/p/d/command-reply", reply, "t", "admin")
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	var accepted struct {
		MessageID string `json:"messageId"`
	}
	json.Unmarshal(w.Body.Bytes(), &accepted)
	deadline := time.Now().Add(5 * time.Second)
	for {
		v, _, err := repo.ListDeviceCommands(ctx, "t", "d", 20, 0)
		if err != nil {
			t.Fatal(err)
		}
		if v[0].Status == "SUCCEEDED" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("reply not applied", v)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, e = repo.GetRawIndex(ctx, "t", accepted.MessageID); e != nil {
		t.Fatal("reply bypassed archive", e)
	}
	m, e := repo.GetStandardMessageByRaw(ctx, "t", accepted.MessageID)
	if e != nil || m.MessageType != model.CommandReply {
		t.Fatal(m, e)
	}
	if e = engine.ReportConnection(ctx, "t", "p", "d", true, 3); e != nil {
		t.Fatal(e)
	}
	if e = engine.ReportConnection(ctx, "t", "p", "d", false, 4); e != nil {
		t.Fatal(e)
	}
	events, _, e := repo.ListDeviceStateEvents(ctx, "t", "d", 20, 0)
	if e != nil || len(events) < 2 || events[0].State.ConnectionStatus != "DISCONNECTED" || events[1].State.ConnectionStatus != "CONNECTED" {
		t.Fatal(events, e)
	}
	for _, path := range []string{"/api/v1/edge-nodes", "/api/v1/edge-nodes/old/credentials", "/api/v1/edge-nodes/old/runtime", "/api/v1/edge-nodes/old/program", "/api/v1/edge/t/old/config", "/api/v1/edge/t/old/raw", "/api/v1/edge/t/old/heartbeat", "/api/v1/integrations/video/onvif/test", "/api/v1/integrations/video/onvif/discover"} {
		for _, method := range []string{"GET", "POST"} {
			if w = call(method, path, `{}`, "t", "admin"); w.Code != 404 {
				t.Fatal("removed node route is still exposed", path, w.Code)
			}
		}
	}
	w = call("DELETE", "/api/v1/device-registry/d/credentials", "", "t", "admin")
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte("PENDING")) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = call("POST", "/api/v1/device-mqtt/token", "", "t", "admin")
	if w.Code != 401 {
		t.Fatal("disabled token accepted", w.Code)
	}
	// Details follow the current binding even while a profile still records the
	// original release, and the alarm preview must remain device/tenant scoped.
	d, _ := repo.GetManagedDevice(ctx, "t", "d")
	d.Connector, d.ConnectorProfileID = "TCP", "profile"
	repo.SaveManagedDevice(ctx, d)
	repo.SaveDeviceAccessProfile(ctx, model.DeviceAccessProfile{TenantID: "t", ID: "profile", ProductID: "p", ProtocolID: "old", ProtocolVersion: "1"})
	repo.CreateProtocolRelease(ctx, model.ProtocolRelease{TenantID: "t", ProtocolID: "current", Version: "2", Status: "PUBLISHED", Capabilities: []string{"encode"}})
	repo.SaveProductProtocolBinding(ctx, model.ProductProtocolBinding{TenantID: "t", ProductID: "p", ProtocolID: "current", Version: "2"})
	repo.UpsertAlarm(ctx, model.Alarm{TenantID: "t", DeviceID: "d", ID: "own", RuleID: "r", AlarmType: "FIRE", LastTriggeredAt: 1})
	repo.UpsertAlarm(ctx, model.Alarm{TenantID: "other", DeviceID: "d", ID: "foreign", RuleID: "r", AlarmType: "FIRE", LastTriggeredAt: 2})
	w = call("GET", "/api/v1/device-registry/d/connection", "", "t", "viewer")
	var detail struct {
		ProtocolID string        `json:"protocolId"`
		Version    string        `json:"protocolVersion"`
		CanCommand bool          `json:"canCommand"`
		Alarms     []model.Alarm `json:"recentAlarms"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &detail) != nil || detail.ProtocolID != "current" || detail.Version != "2" || !detail.CanCommand || len(detail.Alarms) != 1 || detail.Alarms[0].ID != "own" {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestDeviceConnectionCheckUsesCurrentFieldEvidence(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UnixMilli()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(devicescope.Wrap(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(t.TempDir()), log)
	cfg := config.Load()
	cfg.JWTSecret = "device-check-test-signing-key-32-characters"
	cfg.AdminTenants = []string{"tenant"}
	srv := New(cfg, engine, metrics.New(), log)
	token, _ := srv.auth.Issue("tester", "tenant", "admin", nil, time.Hour)
	if err := repo.SaveProduct(ctx, model.Product{TenantID: "tenant", ID: "product", Status: "ENABLED", ProtocolPackageID: "iot-standard@1.0.0", Transport: "HTTP"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "tenant", ID: "device", ProductID: "product", Name: "设备", Status: "ENABLED", UpdatedAt: now - 2000, Connector: "HTTP"}); err != nil {
		t.Fatal(err)
	}
	add := func(id, source string, at int64, parseError string, parsed bool) {
		t.Helper()
		raw := model.RawMessage{MessageID: id, TenantID: "tenant", ProductID: "product", DeviceID: "device", Source: source, Protocol: "iot-standard", ProtocolID: "iot-standard", ProtocolVersion: "1.0.0", Transport: "HTTP", PayloadFormat: "json", Payload: json.RawMessage(`{"data":{"temperature":22}}`), ReceivedAt: at}
		idx, err := archive.PutRaw(ctx, raw)
		if err != nil {
			t.Fatal(err)
		}
		idx.ParseError = parseError
		if _, err = repo.SaveRawIndex(ctx, idx); err != nil {
			t.Fatal(err)
		}
		if parsed {
			if err = repo.SaveStandardMessage(ctx, model.StandardMessage{MessageID: "parsed-" + id, RawMessageID: id, TenantID: "tenant", ProductID: "product", DeviceID: "device", MessageType: model.PropertyReport, Timestamp: at, Properties: map[string]any{"temperature": 22}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	check := func(since int64) map[string]any {
		t.Helper()
		path := "/api/v1/device-registry/device/connection?since=" + strconv.FormatInt(since, 10)
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("check %d: %s", w.Code, w.Body.String())
		}
		var result map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result["ingest"].(map[string]any)
	}
	add("historical", "standard-http", now-4000, "", true)
	if got := check(now - 1000)["stage"]; got != "WAITING_FOR_DATA" {
		t.Fatalf("historical counted: %v", got)
	}
	if check(now - 1000)["previousParsedAt"] == nil {
		t.Fatal("historical success not distinguished")
	}
	add("debug", "managed-device", now-500, "", true)
	if got := check(now - 1000)["stage"]; got != "WAITING_FOR_DATA" {
		t.Fatalf("simulated counted: %v", got)
	}
	if got := check(now - 1000)["simulationCount"]; got != float64(1) {
		t.Fatalf("simulation not identified: %v", got)
	}
	add("bad", "standard-http", now-300, "invalid payload", false)
	if got := check(now - 1000)["stage"]; got != "PARSE_FAILED" {
		t.Fatalf("parse failure hidden: %v", got)
	}
	add("good", "standard-http", now-100, "", true)
	if got := check(now - 1000)["stage"]; got != "PARSED" {
		t.Fatalf("parsed evidence missing: %v", got)
	}
	if check(now - 1000)["continuouslyUpdating"] != false {
		t.Fatal("one parsed message is not continuous")
	}
	add("good-again", "standard-http", now-50, "", true)
	if check(now - 1000)["continuouslyUpdating"] != true {
		t.Fatal("second parsed report missing")
	}
}

func TestIndependentProductAndDeviceRegistration(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(devicescope.Wrap(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(t.TempDir()), log)
	if err := engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.JWTSecret = "independent-product-test-key-32-chars"
	api := New(cfg, engine, metrics.New(), log)
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	token, _ := api.auth.Issue("tester", "tenant", "admin", nil, time.Hour)
	viewer, _ := api.auth.Issue("viewer", "tenant", "viewer", nil, time.Hour)
	product := map[string]any{"id": "standard-product", "name": "独立标准产品", "protocolPackageId": "iot-standard@1.0.0", "transport": "HTTP"}
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/products", viewer, product, 403)
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/products", token, product, 201)
	release, err := repo.GetProtocolRelease(ctx, "tenant", parser.StandardProtocolID, "1.0.0")
	if err != nil || release.ParserType != parser.StandardParserName {
		t.Fatalf("missing standard release: %+v %v", release, err)
	}
	if _, err := repo.GetProtocolRelease(ctx, "other", parser.StandardProtocolID, "1.0.0"); err == nil {
		t.Fatal("release crossed tenant boundary")
	}
	result := requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/device-registry", token, map[string]any{"id": "independent-device", "name": "独立设备", "productId": "standard-product", "trial": true}, 201)
	credential, ok := result["credential"].(map[string]any)
	if !ok || credential["secret"] == "" {
		t.Fatal("device registration did not issue credential")
	}
	// Product creation is repeatable and never rewrites the immutable release.
	requestJSON(t, server.Client(), "PUT", server.URL+"/api/v1/products/standard-product", token, product, 201)
	again, _ := repo.GetProtocolRelease(ctx, "tenant", parser.StandardProtocolID, "1.0.0")
	if again.CreatedAt != release.CreatedAt {
		t.Fatal("standard release was replaced")
	}
	raw, err := api.onboarding.PrepareStandard(ctx, "tenant", "standard-product", "independent-device", "property", "HTTP", []byte(`{"version":"1.0","id":"first","timestamp":1789315000000,"data":{"temperature":26}}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := engine.IngestRaw(ctx, raw); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		state, err := repo.GetDeviceState(ctx, "tenant", "independent-device")
		if err == nil && state.LastSeenAt > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("first report was not parsed: %+v %v", state, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	// A published Go release is selectable before a compatibility package exists.
	goRelease := model.ProtocolRelease{TenantID: "tenant", ProtocolID: "fire-go", Version: "1.0.0", Status: "PUBLISHED", ParserType: parser.GoProtocolParserName, Transport: "TCP", PayloadFormat: "hex"}
	if err := repo.CreateProtocolRelease(ctx, goRelease); err != nil {
		t.Fatal(err)
	}
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/products", token, map[string]any{"id": "go-product", "name": "Go 产品", "protocolPackageId": "fire-go@1.0.0"}, 201)
	binding, err := repo.GetProductProtocolBinding(ctx, "tenant", "go-product")
	if err != nil || binding.ProtocolID != "fire-go" {
		t.Fatalf("new product is not bound: %+v %v", binding, err)
	}
	for _, status := range []string{"VALIDATED", "REVOKED"} {
		blocked := goRelease
		blocked.Version = status
		blocked.Status = status
		if err := repo.CreateProtocolRelease(ctx, blocked); err != nil {
			t.Fatal(err)
		}
		requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/products", token, map[string]any{"id": status, "name": "不可绑定", "protocolPackageId": "fire-go@" + status}, 422)
		if _, err := repo.GetProduct(ctx, "tenant", status); err == nil {
			t.Fatal("invalid release created a product")
		}
	}
}

func TestNewTemplateOnVersionedReleaseIsBound(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Load()
	cfg.JWTSecret = "versioned-template-binding-test-key-32"
	api := New(cfg, &core.Engine{Repo: devicescope.Wrap(repo)}, metrics.New(), log)
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	token, _ := api.auth.Issue("tester", "tenant", "admin", nil, time.Hour)
	if err := repo.CreateProtocolRelease(ctx, model.ProtocolRelease{TenantID: "tenant", ProtocolID: "meter", Version: "1", Transport: "MODBUS_TCP", PayloadFormat: "hex", ParserType: parser.ModbusTCPParserName, Status: "PUBLISHED"}); err != nil {
		t.Fatal(err)
	}
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/products", token, map[string]any{"id": "meter-product", "name": "电表", "protocolPackageId": "meter@1"}, 201)
	binding, err := repo.GetProductProtocolBinding(ctx, "tenant", "meter-product")
	if err != nil || binding.ProtocolID != "meter" || binding.Version != "1" {
		t.Fatalf("versioned template not bound: %+v %v", binding, err)
	}
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/products", token, map[string]any{"id": "standard-product", "name": "标准", "protocolPackageId": "iot-standard@1.0.0"}, 201)
	if _, err = repo.GetProductProtocolBinding(ctx, "tenant", "standard-product"); err == nil {
		t.Fatal("the built-in standard protocol needs no binding")
	}
}

func TestTestDeviceUsesConfiguredAlarmRuleWithoutCreatingFixtureRule(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	scope := testDeviceScope("tenant_test_device")
	if err := repo.SaveRule(ctx, model.AlarmRule{
		ID:        "rule_test_device_" + scope,
		TenantID:  "tenant_test_device",
		ProductID: "product_test_device_" + scope,
		Name:      "测试设备高温烟雾报警",
		AlarmType: "FIRE_RISK",
		Level:     "HIGH",
		Match:     "all",
		Conditions: []model.RuleCondition{
			{Field: "temperature", Operator: ">", Value: 80},
			{Field: "smoke", Operator: "eq", Value: true},
		},
		Recovery: []model.RuleCondition{
			{Field: "temperature", Operator: "<=", Value: 80},
			{Field: "smoke", Operator: "eq", Value: false},
		},
		Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	realtime := local.NewRealtime()
	engine := core.New(devicescope.Wrap(repo), archive, local.NewBus(), realtime, parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.DevMode = true
	cfg.JWTSecret = "test-secret-at-least-32-characters"
	cfg.AdminTenants = []string{"tenant_test_device"}
	api := New(cfg, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	token, err := api.auth.Issue("operator", "tenant_test_device", "operator", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	fixture := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/test-devices/provision", token, map[string]any{}, http.StatusCreated)
	productID := fixture["product"].(map[string]any)["id"].(string)
	rules := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/rules", token, nil, http.StatusOK)
	if len(rules["items"].([]any)) != 0 {
		t.Fatalf("test device provisioning created an unexpected alarm rule: %#v", rules)
	}

	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/rules", token, map[string]any{
		"id":        "rule_test_device_navigation",
		"name":      "测试设备报警后打开设备管理",
		"productId": productID,
		"alarmType": "FIRE_RISK",
		"level":     "HIGH",
		"match":     "all",
		"enabled":   true,
		"conditions": []map[string]any{
			{"field": "temperature", "operator": ">", "value": 80},
			{"field": "smoke", "operator": "eq", "value": true},
		},
		"actions": []map[string]any{{"type": "OPEN_PAGE", "page": "devices"}},
	}, http.StatusCreated)

	deviceID := fixture["device"].(map[string]any)["id"].(string)
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/device-registry/"+deviceID+"/debug", token, map[string]any{
		"messageId": "raw_test_device_navigation",
		"payload": map[string]any{
			"properties": map[string]any{"temperature": 88.5, "smoke": true},
			"tags":       map[string]any{"cityCode": "city_001", "districtCode": "district_01", "buildingId": "A-01", "deviceType": "smoke"},
		},
	}, http.StatusCreated)

	found := false
	for _, published := range realtime.Messages {
		if published.Topic != "/iot/ui-action/tenant_test_device" {
			continue
		}
		var event model.UIActionEvent
		if err := json.Unmarshal(published.Payload, &event); err != nil {
			t.Fatal(err)
		}
		if event.RuleID == "rule_test_device_navigation" && event.Action.Type == "OPEN_PAGE" && event.Action.Page == "devices" {
			found = true
		}
	}
	if !found {
		t.Fatalf("configured alarm rule did not publish OPEN_PAGE devices action: %#v", realtime.Messages)
	}
}

func TestTestDeviceDirectAlarmCreatesAlarmAndUpdatesDeviceState(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine := core.New(devicescope.Wrap(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.DevMode = true
	cfg.JWTSecret = "test-secret-at-least-32-characters"
	cfg.AdminTenants = []string{"tenant_direct_alarm"}
	api := New(cfg, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	token, err := api.auth.Issue("operator", "tenant_direct_alarm", "operator", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	fixture := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/test-devices/provision", token, map[string]any{}, http.StatusCreated)
	deviceID := fixture["device"].(map[string]any)["id"].(string)
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/device-registry/"+deviceID+"/debug", token, map[string]any{
		"messageId": "raw_direct_alarm",
		"payload": map[string]any{
			"alarm":      true,
			"properties": map[string]any{"temperature": 88.5, "smoke": true, "battery": 92},
			"tags":       map[string]any{"deviceType": "smoke"},
		},
	}, http.StatusCreated)
	alarms := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/alarms?deviceId="+deviceID+"&status=ACTIVE", token, nil, http.StatusOK)
	if alarms["total"] != float64(1) {
		t.Fatalf("direct device alarm was not shown in alarm center: %#v", alarms)
	}
	items := alarms["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected one direct alarm item: %#v", alarms)
	}
	item := items[0].(map[string]any)
	if item["source"] != "device" || item["alarmType"] != "SMOKE_DETECTED" || item["alarmLevel"] != "HIGH" {
		t.Fatalf("direct alarm metadata was not inferred correctly: %#v", item)
	}
	state := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/devices/"+deviceID+"/latest", token, nil, http.StatusOK)
	if state["state"].(map[string]any)["businessStatus"] != "ALARM" {
		t.Fatalf("device state did not change to ALARM: %#v", state)
	}

	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/device-registry/"+deviceID+"/debug", token, map[string]any{
		"messageId": "raw_direct_recovery",
		"payload": map[string]any{
			"properties": map[string]any{"temperature": 26.5, "smoke": false, "battery": 96},
			"tags":       map[string]any{"deviceType": "smoke"},
		},
	}, http.StatusCreated)
	alarms = requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/alarms?deviceId="+deviceID+"&status=ACTIVE", token, nil, http.StatusOK)
	if alarms["total"] != float64(0) {
		t.Fatalf("direct device alarm was not recovered: %#v", alarms)
	}
	state = requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/devices/"+deviceID+"/latest", token, nil, http.StatusOK)
	if state["state"].(map[string]any)["businessStatus"] != "ONLINE" {
		t.Fatalf("device state did not return to ONLINE: %#v", state)
	}
}
