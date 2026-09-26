package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"iot-platform/internal/video"
	"iot-platform/internal/video/videotest"
)

type liveEnv struct {
	srv   *httptest.Server
	repo  *memory.Repository
	media *videotest.Fake
	req   func(method, path, token string, body any, status int) map[string]any
	root  string
	login func(user string) string
}

func newLiveEnv(t *testing.T, deployed bool) *liveEnv {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	repo := memory.NewRepository()
	cfg := config.Load()
	cfg.AdminUser, cfg.AdminPassword = "root", "live-root-test"
	cfg.AdminTenants = []string{"tenant_a", "tenant_b"}
	cfg.JWTSecret = "live-test-secret-at-least-32-bytes!!"
	cfg.DevMode = true
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(repo.SaveProduct(ctx, model.Product{TenantID: "tenant_a", ID: "product", Name: "演示产品"}))
	for _, id := range []string{"d1", "d2"} {
		must(repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "tenant_a", ID: id, AccessKey: id, Name: id, ProductID: "product", DeviceRole: "DIRECT"}))
	}
	engine := &core.Engine{Repo: repo, Clock: ports.RealClock{}}
	api := New(cfg, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	env := &liveEnv{repo: repo}
	if deployed {
		env.media = videotest.New("media-secret-0123456789abcdef")
		t.Cleanup(env.media.Close)
		_, lan, _ := net.ParseCIDR("10.0.0.0/8")
		vc := config.VideoConfig{MediaAPIURL: env.media.URL, MediaSecret: "media-secret-0123456789abcdef", MediaServerID: "torchlink-media-1", HookSecret: "hook-secret-0123456789abcdef0", CredentialKey: []byte(strings.Repeat("k", 32)), CredentialKeyID: "k1", AllowedCIDRs: []*net.IPNet{lan}, AllowedPorts: map[int]bool{554: true}, HLSPublicPath: "/media/hls", LeaseTTL: 45 * time.Second, IdleGrace: 20 * time.Second, StartTimeout: 2 * time.Second, MaxSessions: 50, MaxSourceStreams: 8, Transcode: true, MaxTranscodes: 2, HWAccel: "none"}
		svc := video.New(vc, repo, api.VideoCameraLookup, api.VideoAuthorize, nil)
		svc.Start(ctx)
		api.SetVideo(svc)
	}
	env.srv = httptest.NewServer(api.Handler())
	t.Cleanup(env.srv.Close)
	env.req = func(method, path, token string, body any, status int) map[string]any {
		t.Helper()
		return requestJSON(t, env.srv.Client(), method, env.srv.URL+path, token, body, status)
	}
	env.root = env.req("POST", "/api/v1/auth/login", "", map[string]any{"username": "root", "password": cfg.AdminPassword, "tenantId": "tenant_a"}, 200)["accessToken"].(string)
	env.login = func(user string) string {
		t.Helper()
		return env.req("POST", "/api/v1/auth/login", "", map[string]any{"username": user, "password": "live-password-test", "tenantId": "tenant_a"}, 200)["accessToken"].(string)
	}
	return env
}

func (e *liveEnv) mediaAuth(t *testing.T, uri string) int {
	t.Helper()
	req, _ := http.NewRequest("GET", e.srv.URL+"/api/v1/video/media-auth", nil)
	req.Header.Set("X-Original-URI", uri)
	resp, err := e.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func TestVideoLiveNotDeployedKeepsCameraManagement(t *testing.T) {
	e := newLiveEnv(t, false)
	status := e.req("GET", "/api/v1/video/status", e.root, nil, 200)
	if status["status"].(map[string]any)["state"] != video.StateNotDeployed {
		t.Fatalf("status: %v", status)
	}
	e.req("POST", "/api/v1/integrations/video/cameras", e.root, map[string]any{"cameraId": "cam", "cameraName": "大厅", "deviceId": "d1", "enabled": true}, 201)
	e.req("PUT", "/api/v1/integrations/video/cameras/cam", e.root, map[string]any{"cameraName": "大厅东", "enabled": true}, 200)
	list := e.req("GET", "/api/v1/integrations/video/cameras", e.root, nil, 200)
	if items := list["items"].([]any); len(items) != 1 || items[0].(map[string]any)["cameraName"] != "大厅东" {
		t.Fatalf("camera CRUD must work without the live module: %v", list)
	}
	e.req("POST", "/api/v1/video/cameras/cam/play-sessions", e.root, map[string]any{}, 503)
	e.req("DELETE", "/api/v1/integrations/video/cameras/cam", e.root, nil, 200)
}

func TestVideoLiveAuthorizationBoundaries(t *testing.T) {
	e := newLiveEnv(t, true)
	root := e.root
	e.req("PUT", "/api/v1/video/module", root, map[string]any{"enabled": true}, 200)
	live := map[string]any{"enabled": true, "accessMode": "RTSP", "brandTemplate": "generic", "manualUrl": true, "username": "admin", "password": "p@ss:w/rd#1%"}
	for id, device := range map[string]string{"cam-d1": "d1", "cam-d2": "d2", "cam-free": ""} {
		e.req("POST", "/api/v1/integrations/video/cameras", root, map[string]any{"cameraId": id, "cameraName": id, "deviceId": device, "enabled": true}, 201)
		live["mainStreamUrl"] = "rtsp://10.0.0.5:554/" + id
		cfg := e.req("PUT", "/api/v1/integrations/video/cameras/"+id+"/live", root, live, 200)
		if _, leaked := cfg["password"]; leaked || cfg["hasPassword"] != true {
			t.Fatalf("live config must not return the password: %v", cfg)
		}
	}
	play := videoPlayPermission
	users := []map[string]any{
		{"username": "scoped", "permissions": []string{"menu:devices", "menu:alarms", play}, "deviceScope": "selected", "deviceIds": []string{"d1"}},
		{"username": "noplay", "permissions": []string{"menu:devices"}, "deviceScope": "selected", "deviceIds": []string{"d1"}},
		{"username": "wide", "permissions": []string{"menu:devices", "menu:cameras", play}, "deviceScope": "all"},
	}
	for _, u := range users {
		u["password"], u["enabled"] = "live-password-test", true
		e.req("POST", "/api/v1/access/users", root, u, 200)
	}
	scoped, noplay, wide := e.login("scoped"), e.login("noplay"), e.login("wide")

	grant := e.req("POST", "/api/v1/video/cameras/cam-d1/play-sessions", scoped, map[string]any{"protocol": "hls"}, 201)
	e.req("POST", "/api/v1/video/cameras/cam-d2/play-sessions", scoped, map[string]any{}, 403)
	e.req("POST", "/api/v1/video/cameras/cam-free/play-sessions", scoped, map[string]any{}, 403)
	e.req("GET", "/api/v1/video/cameras/cam-d2", scoped, nil, 404)
	e.req("GET", "/api/v1/video/devices/d2/cameras", scoped, nil, 403)
	e.req("GET", "/api/v1/integrations/video/cameras", scoped, nil, 403)
	e.req("PUT", "/api/v1/integrations/video/cameras/cam-d1/live", scoped, live, 403)
	e.req("POST", "/api/v1/video/cameras/cam-d1/play-sessions", noplay, map[string]any{}, 403)
	e.req("POST", "/api/v1/video/cameras/cam-free/play-sessions", wide, map[string]any{}, 201)
	cams := e.req("GET", "/api/v1/video/devices/d1/cameras", scoped, nil, 200)["items"].([]any)
	if len(cams) != 1 || cams[0].(map[string]any)["liveAvailable"] != true {
		t.Fatalf("device detail must offer the authorized camera: %v", cams)
	}
	// Another tenant's administrator cannot open this tenant's camera.
	other := e.req("POST", "/api/v1/auth/login", "", map[string]any{"username": "root", "password": "live-root-test", "tenantId": "tenant_b"}, 200)["accessToken"].(string)
	e.req("POST", "/api/v1/video/cameras/cam-d1/play-sessions", other, map[string]any{}, 403)
	// Knowing a session ID or stream URL grants nothing.
	sid := grant["sessionId"].(string)
	e.req("POST", "/api/v1/video/play-sessions/"+sid+"/heartbeat", wide, map[string]any{}, 410)
	hls := grant["hlsUrl"].(string)
	if e.mediaAuth(t, hls) != 204 {
		t.Fatal("the session's own HLS URL is authorized")
	}
	if e.mediaAuth(t, strings.Replace(hls, grant["token"].(string), strings.Repeat("0", 64), 1)) != 403 {
		t.Fatal("a forged token must be rejected")
	}
	// Narrowing the user's device scope ends the playback promptly.
	e.req("PUT", "/api/v1/access/users/scoped", root, map[string]any{"username": "scoped", "enabled": true, "permissions": users[0]["permissions"], "deviceScope": "selected", "deviceIds": []string{"d2"}}, 200)
	deadline := time.Now().Add(3 * time.Second)
	for e.mediaAuth(t, hls) != 403 {
		if time.Now().After(deadline) {
			t.Fatal("changing the user's permissions must revoke the live session")
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Saving basic camera data keeps the live configuration; re-linking the
	// device ends existing playback.
	g2 := e.req("POST", "/api/v1/video/cameras/cam-d1/play-sessions", root, map[string]any{"protocol": "hls"}, 201)
	e.req("PUT", "/api/v1/integrations/video/cameras/cam-d1", root, map[string]any{"cameraName": "改名", "deviceId": "d1", "enabled": true}, 200)
	if e.mediaAuth(t, g2["hlsUrl"].(string)) != 204 {
		t.Fatal("renaming a camera must not end playback")
	}
	cfg := e.req("GET", "/api/v1/integrations/video/cameras/cam-d1/live", root, nil, 200)
	if cfg["mainStreamUrl"] != "rtsp://10.0.0.5:554/cam-d1" || cfg["hasPassword"] != true {
		t.Fatalf("saving basic data must not overwrite live configuration: %v", cfg)
	}
	e.req("PUT", "/api/v1/integrations/video/cameras/cam-d1", root, map[string]any{"cameraName": "改名", "deviceId": "d2", "enabled": true}, 200)
	if e.mediaAuth(t, g2["hlsUrl"].(string)) != 403 {
		t.Fatal("re-linking the camera must end playback")
	}
	list := e.req("GET", "/api/v1/integrations/video/cameras", root, nil, 200)["items"].([]any)
	for _, item := range list {
		m := item.(map[string]any)
		if strings.Contains(strings.ToLower(jsonString(m)), "p@ss") || m["live"] == nil {
			t.Fatalf("camera list must carry safe live state only: %v", m)
		}
	}

	// Deleting a camera ends its playback and removes live config and credentials.
	g3 := e.req("POST", "/api/v1/video/cameras/cam-free/play-sessions", root, map[string]any{"protocol": "hls"}, 201)
	e.req("DELETE", "/api/v1/integrations/video/cameras/cam-free", root, nil, 200)
	if e.mediaAuth(t, g3["hlsUrl"].(string)) != 403 {
		t.Fatal("deleting a camera must end playback")
	}
	if _, err := e.repo.GetCameraLiveConfig(context.Background(), "tenant_a", "cam-free"); err == nil {
		t.Fatal("live config must be removed with the camera")
	}
	if _, err := e.repo.GetCameraCredential(context.Background(), "tenant_a", "cam-free"); err == nil {
		t.Fatal("credentials must be removed with the camera")
	}

	// Hooks need the shared secret.
	hook := func(secret string) int {
		resp, err := e.srv.Client().Post(e.srv.URL+"/api/v1/video/hooks/on_play?secret="+secret, "application/json", strings.NewReader(`{"app":"src","stream":"x","params":""}`))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if hook("wrong") != 403 || hook("hook-secret-0123456789abcdef0") != 200 {
		t.Fatal("hook secret must be enforced")
	}
	// Only the platform administrator switches the module.
	e.req("PUT", "/api/v1/video/module", wide, map[string]any{"enabled": false}, 403)
}

func TestVideoLivePermissionCatalog(t *testing.T) {
	e := newLiveEnv(t, true)
	items := e.req("GET", "/api/v1/access/permissions", e.root, nil, 200)["items"].([]any)
	found := map[string]string{}
	for _, item := range items {
		m := item.(map[string]any)
		found[m["id"].(string)] = m["name"].(string) + "@" + m["menu"].(string)
	}
	if found[videoPlayPermission] != "观看摄像头直播@devices" {
		t.Fatalf("watch permission: %q", found[videoPlayPermission])
	}
	if found["PUT /api/v1/integrations/video/cameras/:id/live"] != "配置摄像头直播@cameras" {
		t.Fatalf("config permission: %q", found["PUT /api/v1/integrations/video/cameras/:id/live"])
	}
	for id := range found {
		if strings.Contains(id, "/play-sessions/:id") || strings.Contains(id, "/video/module") || strings.Contains(id, "/video/hooks") {
			t.Fatalf("%s must not be a separately grantable permission", id)
		}
	}
}

func jsonString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
