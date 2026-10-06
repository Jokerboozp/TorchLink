package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
	"iot-platform/internal/protocolworker"
)

func setCompatibilityTemplateStatus(t *testing.T, api *Server, productID, status string) {
	t.Helper()
	ctx := context.Background()
	fp, err := api.onboarding.TemplateFingerprint(ctx, "user-tenant", productID)
	if err != nil {
		t.Fatal(err)
	}
	rec, prep, err := api.onboarding.TemplateRecord(ctx, "user-tenant", productID)
	if err != nil {
		t.Fatal(err)
	}
	prep.Fingerprint, prep.Status = fp, status
	prep.Verification = &model.DeviceVerification{Status: "VERIFIED", Fingerprint: fp}
	if _, err = api.onboarding.SaveTemplateRecord(ctx, rec, prep); err != nil {
		t.Fatal(err)
	}
}

func TestCompatibleRegistrationRequiresReadyOrExplicitAuthorizedTrial(t *testing.T) {
	api, repo, call := taskHTTPFixture(t)
	ctx := context.Background()
	state, err := repo.LoadAccessState(ctx, "user-tenant")
	if err != nil {
		t.Fatal(err)
	}
	for i := range state.Users {
		state.Users[i].Permissions = append(state.Users[i].Permissions, "menu:devices", "POST /api/v1/device-registry", "PUT /api/v1/device-registry/:id", "POST /api/v1/discovered-devices/:id/register")
	}
	if saved, err := repo.SaveAccessState(ctx, "user-tenant", state); err != nil || !saved {
		t.Fatal("grant compatibility permissions", saved, err)
	}
	setCompatibilityTemplateStatus(t, api, "product", "DRAFT")
	for _, route := range []string{"registry", "discovery"} {
		t.Run(route, func(t *testing.T) {
			id := "compat-" + route
			path := "/api/v1/device-registry"
			body := map[string]any{"id": id, "name": "接入设备", "productId": "product"}
			if route == "discovery" {
				path = "/api/v1/discovered-devices/" + id + "/register"
				body = map[string]any{}
				if err := repo.UpsertDeviceState(ctx, model.DeviceState{TenantID: "user-tenant", DeviceID: id, ProductID: "product"}); err != nil {
					t.Fatal(err)
				}
			}
			for _, user := range []string{"alice", "template-editor"} {
				if w := call(user, "POST", path, body); w.Code != 409 {
					t.Fatal("implicit trial", user, w.Code, w.Body.String())
				}
			}
			body["trial"] = true
			if w := call("alice", "POST", path, body); w.Code != 403 {
				t.Fatal("unauthorized trial", w.Code, w.Body.String())
			}
			if _, err := repo.GetManagedDevice(ctx, "user-tenant", id); !errors.Is(err, model.ErrNotFound) {
				t.Fatal("rejected creation persisted", err)
			}
			w := call("template-editor", "POST", path, body)
			if w.Code != 201 || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("explicit trial", w.Code, w.Body.String())
			}
			d, err := repo.GetManagedDevice(ctx, "user-tenant", id)
			if err != nil || d.OnboardingRequestHash == "" || d.SecretHash == "" {
				t.Fatal("creation skipped Enroll", d.ID, err)
			}
			// Existing real devices can still be edited while a template is under preparation.
			if w = call("alice", "PUT", "/api/v1/device-registry/"+id, map[string]any{"name": "更新名称", "productId": "product"}); w.Code != 201 {
				t.Fatal("existing edit gated", w.Code, w.Body.String())
			}
			updated, err := repo.GetManagedDevice(ctx, "user-tenant", id)
			if err != nil || updated.SecretHash != d.SecretHash || updated.AccessKey != d.AccessKey || updated.OnboardingRequestHash != d.OnboardingRequestHash || updated.Name != "更新名称" {
				t.Fatal("editing changed connection identity", err)
			}
		})
	}
	setCompatibilityTemplateStatus(t, api, "product", "READY")
	w := call("alice", "POST", "/api/v1/device-registry", map[string]any{"id": "daily", "name": "日常登记", "productId": "product"})
	if w.Code != 201 {
		t.Fatal("ready template blocked", w.Code, w.Body.String())
	}
	state, err = repo.LoadAccessState(ctx, "user-tenant")
	if err != nil {
		t.Fatal(err)
	}
	for i := range state.Users {
		if state.Users[i].Username == "alice" {
			state.Users[i].DeviceScope, state.Users[i].DeviceIDs = "selected", []string{"scoped-discovery"}
		}
	}
	if saved, err := repo.SaveAccessState(ctx, "user-tenant", state); err != nil || !saved {
		t.Fatal("restrict device scope", saved, err)
	}
	if err := repo.UpsertDeviceState(ctx, model.DeviceState{TenantID: "user-tenant", DeviceID: "scoped-discovery", ProductID: "product"}); err != nil {
		t.Fatal(err)
	}
	if w = call("alice", "POST", "/api/v1/discovered-devices/scoped-discovery/register", nil); w.Code != 403 {
		t.Fatal("limited scope created device", w.Code, w.Body.String())
	}
}

func TestCompatibleRegistrationSelectsExistingListenerAndGuidesIncompleteConnections(t *testing.T) {
	api, repo, call := taskHTTPFixture(t)
	ctx := context.Background()
	release := model.ProtocolRelease{TenantID: "user-tenant", ProtocolID: "wire", Version: "1", Status: "PUBLISHED", Transport: "TCP", ParserType: parser.GoProtocolParserName, Artifact: map[string]any{"runtime": protocolworker.Runtime}, Capabilities: []string{"ingress", "decode"}}
	if err := repo.CreateProtocolRelease(ctx, release); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveProduct(ctx, model.Product{TenantID: "user-tenant", ID: "wire", Name: "协议模板", Status: "ENABLED", ProtocolPackageID: "wire@1", Transport: "TCP"}); err != nil {
		t.Fatal(err)
	}
	profile := model.DeviceAccessProfile{TenantID: "user-tenant", ID: "shared", ProductID: "wire", ProtocolID: "wire", ProtocolVersion: "1", Mode: "listener", ConnectionMode: "listen", Network: "tcp", Host: "0.0.0.0", PublicHost: "devices.example.test", Port: 9701, Enabled: true}
	if err := repo.SaveDeviceAccessProfile(ctx, profile); err != nil {
		t.Fatal(err)
	}
	setCompatibilityTemplateStatus(t, api, "wire", "READY")
	w := call("alice", "POST", "/api/v1/device-registry", map[string]any{"id": "wire-device", "name": "协议设备", "productId": "wire"})
	if w.Code != 201 || strings.Contains(w.Body.String(), `"credential"`) {
		t.Fatal("listener compatibility", w.Code, w.Body.String())
	}
	d, err := repo.GetManagedDevice(ctx, "user-tenant", "wire-device")
	if err != nil || d.ConnectorProfileID != "shared" || d.SecretHash != "" {
		t.Fatal("listener not inherited", err)
	}
	profile.ID, profile.Port = "another", 9702
	if err = repo.SaveDeviceAccessProfile(ctx, profile); err != nil {
		t.Fatal(err)
	}
	setCompatibilityTemplateStatus(t, api, "wire", "READY")
	w = call("alice", "POST", "/api/v1/device-registry", map[string]any{"id": "ambiguous", "name": "选择连接", "productId": "wire"})
	if w.Code != 422 || !strings.Contains(w.Body.String(), "设备接入流程") {
		t.Fatal("ambiguous listener guessed", w.Code, w.Body.String())
	}
	w = call("alice", "POST", "/api/v1/device-registry", map[string]any{"id": "chosen", "name": "选择连接", "productId": "wire", "connectorProfileId": "shared"})
	if w.Code != 201 {
		t.Fatal("selected listener", w.Code, w.Body.String())
	}
	release.ProtocolID, release.ParserType, release.Transport = "poll", parser.ModbusTCPParserName, "MODBUS_TCP"
	if err = repo.CreateProtocolRelease(ctx, release); err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveProduct(ctx, model.Product{TenantID: "user-tenant", ID: "poll", Name: "定时采集", Status: "ENABLED", ProtocolPackageID: "poll@1", Transport: "MODBUS_TCP"}); err != nil {
		t.Fatal(err)
	}
	setCompatibilityTemplateStatus(t, api, "poll", "READY")
	w = call("alice", "POST", "/api/v1/device-registry", map[string]any{"id": "poll-device", "name": "缺少地址", "productId": "poll"})
	if w.Code != 422 || !strings.Contains(w.Body.String(), "设备接入流程") {
		t.Fatal("incomplete polling accepted", w.Code, w.Body.String())
	}
	if _, err = repo.GetManagedDevice(ctx, "user-tenant", "poll-device"); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("incomplete device persisted", err)
	}
}

func TestCompatibleDeviceEditsCannotChangeTemplate(t *testing.T) {
	api, repo, call := taskHTTPFixture(t)
	ctx := context.Background()
	state, err := repo.LoadAccessState(ctx, "user-tenant")
	if err != nil {
		t.Fatal(err)
	}
	for i := range state.Users {
		if state.Users[i].Username == "alice" {
			state.Users[i].Permissions = append(state.Users[i].Permissions, "PUT /api/v1/device-registry/:id")
		}
	}
	if saved, err := repo.SaveAccessState(ctx, "user-tenant", state); err != nil || !saved {
		t.Fatal("grant device edit permission", saved, err)
	}
	if w := call("alice", "POST", "/api/v1/device-registry", map[string]any{"id": "existing", "name": "已接入设备", "productId": "product"}); w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	original, err := repo.GetManagedDevice(ctx, "user-tenant", "existing")
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []model.Product{
		{TenantID: "user-tenant", ID: "unverified-http", Name: "未验收 HTTP 模板", Status: "ENABLED", Transport: "HTTP", ProtocolPackageID: "iot-standard@1.0.0"},
		{TenantID: "user-tenant", ID: "unverified-tcp", Name: "未验收 TCP 模板", Status: "ENABLED", Transport: "TCP", ProtocolPackageID: "wire@1"},
	} {
		if err := repo.SaveProduct(ctx, target); err != nil {
			t.Fatal(err)
		}
	}
	api.cfg.AdminTenants = append(api.cfg.AdminTenants, "user-tenant")
	adminToken, err := api.auth.IssueWithVersion(api.cfg.AdminUser, "user-tenant", "admin", api.adminSessionVersion(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	for _, actor := range []string{"alice", "admin"} {
		for _, method := range []string{"PUT", "POST"} {
			for _, target := range []string{"unverified-http", "unverified-tcp"} {
				t.Run(actor+"/"+method+"/"+target, func(t *testing.T) {
					path := "/api/v1/device-registry"
					if method == "PUT" {
						path += "/existing"
					}
					body := map[string]any{"id": "existing", "name": "不应保存的名称", "productId": target, "trial": true}
					var response *httptest.ResponseRecorder
					if actor == "admin" {
						data, _ := json.Marshal(body)
						request := httptest.NewRequest(method, path, strings.NewReader(string(data)))
						request.Header.Set("Authorization", "Bearer "+adminToken)
						request.Header.Set("Content-Type", "application/json")
						response = httptest.NewRecorder()
						api.Handler().ServeHTTP(response, request)
					} else {
						response = call(actor, method, path, body)
					}
					if response.Code != 409 || !strings.Contains(response.Body.String(), "已登记设备不能直接更换设备模板，请从目标模板重新接入") {
						t.Fatal("existing device bypassed target template preparation", response.Code, response.Body.String())
					}
					after, err := repo.GetManagedDevice(ctx, "user-tenant", "existing")
					if err != nil || !reflect.DeepEqual(original, after) {
						t.Fatal("rejected rebinding changed device, connection or credentials", err)
					}
				})
			}
		}
	}
	if w := call("alice", "PUT", "/api/v1/device-registry/existing", map[string]any{"name": "正常资料编辑", "productId": "product"}); w.Code != 201 {
		t.Fatal("same-template edit was blocked", w.Code, w.Body.String())
	}
	updated, err := repo.GetManagedDevice(ctx, "user-tenant", "existing")
	if err != nil || updated.Name != "正常资料编辑" || updated.ProductID != original.ProductID || updated.AccessKey != original.AccessKey || updated.SecretHash != original.SecretHash || updated.Connector != original.Connector || updated.OnboardingRequestHash != original.OnboardingRequestHash {
		t.Fatal("same-template edit changed connection identity", err)
	}
}

type changedCompatibilityRepository struct {
	ports.Repository
	t *testing.T
}

func (r *changedCompatibilityRepository) SaveOnboarding(ctx context.Context, b model.OnboardingBundle) error {
	if b.Prepared == nil {
		r.t.Fatal("legacy HTTP bypassed the prepared snapshot")
	}
	p, err := r.Repository.GetProduct(ctx, b.Device.TenantID, b.Device.ProductID)
	if err != nil {
		return err
	}
	p.Transport = "MQTT"
	if err = r.Repository.SaveProduct(ctx, p); err != nil {
		return err
	}
	return r.Repository.SaveOnboarding(ctx, b)
}

func TestCompatibleRegistrationUsesAtomicPreparedSnapshot(t *testing.T) {
	api, repo, call := taskHTTPFixture(t)
	api.onboarding.Repo = &changedCompatibilityRepository{Repository: api.onboarding.Repo.(ports.Repository), t: t}
	w := call("alice", "POST", "/api/v1/device-registry", map[string]any{"id": "changed", "name": "并发配置", "productId": "product"})
	if w.Code != 409 {
		t.Fatal("concurrent configuration not rejected", w.Code, w.Body.String())
	}
	if _, err := repo.GetManagedDevice(context.Background(), "user-tenant", "changed"); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("stale snapshot device persisted", err)
	}
}
