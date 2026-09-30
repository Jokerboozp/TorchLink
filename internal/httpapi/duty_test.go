package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
)

type dutyHTTPFixture struct {
	t                                       *testing.T
	repo                                    *memory.Repository
	api                                     *Server
	server                                  *httptest.Server
	tenant, password, admin, day, night     string
	station, dayRoster, nightRoster, dayRun map[string]any
}

func newDutyHTTPFixture(t *testing.T, tenant, password string) *dutyHTTPFixture {
	t.Helper()
	root := t.TempDir()
	repo := memory.NewRepository()
	archive, e := local.NewArchive(root)
	if e != nil {
		t.Fatal(e)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(root), log)
	cfg := config.Load()
	cfg.AdminUser = "root"
	cfg.AdminPassword = password
	cfg.AdminTenants = []string{tenant}
	cfg.JWTSecret = "duty-tests-only-signing-key-longer-than-32-bytes"
	cfg.DataDir = root
	cfg.DevMode = true
	cfg.HTTPAddr = "127.0.0.1:18091"
	cfg.CORSAllowedOrigins = []string{"http://localhost:5173", "http://localhost:5174", "http://localhost:5181", "http://127.0.0.1:5181", "http://127.0.0.1:5173"}
	api := New(cfg, engine, metrics.New(), log)
	server := httptest.NewServer(api.Handler())
	t.Cleanup(server.Close)
	f := &dutyHTTPFixture{t: t, repo: repo, api: api, server: server, tenant: tenant, password: password}
	f.admin = f.login("root")
	if e = repo.SaveProduct(context.Background(), model.Product{ID: "duty-product", TenantID: tenant, Name: "值班测试设备产品", Status: "ENABLED"}); e != nil {
		t.Fatal(e)
	}
	for _, id := range []string{"duty-device", "private-device"} {
		if e = repo.SaveManagedDevice(context.Background(), model.ManagedDevice{ID: id, TenantID: tenant, ProductID: "duty-product", Name: map[string]string{"duty-device": "消防室控制器", "private-device": "未授权控制器"}[id], Status: "ENABLED", AccessKey: id + "-test-key"}); e != nil {
			t.Fatal(e)
		}
	}
	for _, username := range []string{"day", "night"} {
		f.req("POST", "/api/v1/access/users", f.admin, map[string]any{"username": username, "displayName": map[string]string{"day": "白班负责人", "night": "夜班负责人"}[username], "password": password, "enabled": true, "roleIds": []string{}, "permissions": []string{"menu:duty", "menu:devices", "menu:alarms", "action:duty:participate", "action:duty:record", "action:duty:handover", "action:duty:accept", "action:duty:item", "action:duty:ai", "action:duty:history", "action:duty:export"}, "deviceScope": "selected", "deviceIds": []string{"duty-device"}}, 200)
	}
	f.day = f.login("day")
	f.night = f.login("night")
	f.station = f.command("POST", "/api/v1/duty/stations", f.admin, 0, map[string]any{"name": "消防控制室", "supervisorId": "root", "deviceIds": []string{"duty-device"}, "requiredPeople": 1, "enabled": true, "timezone": "Asia/Shanghai", "reminderMinutes": 15}, 200)
	start := time.Now().Add(-time.Hour).UnixMilli()
	f.dayRoster = f.command("POST", "/api/v1/duty/rosters", f.admin, 0, map[string]any{"stationId": f.station["id"], "startAt": start, "endAt": start + int64(2*time.Hour/time.Millisecond), "memberIds": []string{"day"}, "leaderId": "day"}, 200)
	f.dayRoster = f.command("POST", "/api/v1/duty/rosters/"+f.id(f.dayRoster)+"/publish", f.admin, f.version(f.dayRoster), nil, 200)
	f.nightRoster = f.command("POST", "/api/v1/duty/rosters", f.admin, 0, map[string]any{"stationId": f.station["id"], "startAt": start + int64(2*time.Hour/time.Millisecond), "endAt": start + int64(4*time.Hour/time.Millisecond), "memberIds": []string{"night"}, "leaderId": "night"}, 200)
	f.nightRoster = f.command("POST", "/api/v1/duty/rosters/"+f.id(f.nightRoster)+"/publish", f.admin, f.version(f.nightRoster), nil, 200)
	f.command("POST", "/api/v1/duty/runs/arrive", f.day, 0, map[string]any{"rosterId": f.dayRoster["id"]}, 200)
	f.dayRun = f.command("POST", "/api/v1/duty/runs/open", f.admin, 0, map[string]any{"rosterId": f.dayRoster["id"], "reason": "首次开班"}, 200)
	return f
}
func (f *dutyHTTPFixture) login(user string) string {
	f.t.Helper()
	out := f.req("POST", "/api/v1/auth/login", "", map[string]any{"username": user, "password": f.password, "tenantId": f.tenant}, 200)
	return out["accessToken"].(string)
}
func (f *dutyHTTPFixture) req(method, path, token string, body any, status int) map[string]any {
	f.t.Helper()
	return requestJSON(f.t, f.server.Client(), method, f.server.URL+path, token, body, status)
}
func (f *dutyHTTPFixture) command(method, path, token string, version int64, body any, status int) map[string]any {
	f.t.Helper()
	return f.req(method, path, token, map[string]any{"expectedVersion": version, "idempotencyKey": fmt.Sprintf("http-%d", time.Now().UnixNano()), "body": body}, status)
}
func (f *dutyHTTPFixture) id(v map[string]any) string     { return v["id"].(string) }
func (f *dutyHTTPFixture) version(v map[string]any) int64 { return int64(v["version"].(float64)) }
func dutyHTTPBody(v map[string]any) map[string]any        { return v["body"].(map[string]any) }

func TestDutyHTTPCompleteHandoverAndScopedExport(t *testing.T) {
	f := newDutyHTTPFixture(t, "duty_http", "duty-http-test-password")
	f.req("GET", "/api/v1/duty/runs/current", "", nil, 401)
	current := f.req("GET", "/api/v1/duty/runs/current", f.day, nil, 200)
	if len(current["items"].([]any)) != 1 {
		t.Fatal("static current route shadowed by :id")
	}
	record := f.command("POST", "/api/v1/duty/runs/"+f.id(f.dayRun)+"/records", f.day, 0, map[string]any{"content": "已联系维修，请下一班核实到场", "deviceId": "duty-device"}, 201)
	if dutyHTTPBody(record)["runId"] != f.id(f.dayRun) {
		t.Fatal(record)
	}
	item := f.command("POST", "/api/v1/duty/items", f.day, 0, map[string]any{"runId": f.id(f.dayRun), "deviceId": "duty-device", "title": "核实维修到场", "nextAction": "21:00联系维修人员", "ownerId": "day"}, 200)
	h := f.command("POST", "/api/v1/duty/handovers", f.day, 0, map[string]any{"runId": f.id(f.dayRun), "nextRosterId": f.id(f.nightRoster), "humanNotes": "本班人工说明"}, 200)
	revisionID := dutyHTTPBody(h)["currentRevisionId"].(string)
	rev := f.req("GET", "/api/v1/duty/revisions/"+revisionID, f.day, nil, 200)
	h = f.command("POST", "/api/v1/duty/handovers/"+f.id(h)+"/submit", f.day, f.version(h), map[string]any{"revisionId": revisionID, "snapshotHash": dutyHTTPBody(rev)["snapshotHash"]}, 200)
	f.command("POST", "/api/v1/duty/runs/arrive", f.night, 0, map[string]any{"rosterId": f.nightRoster["id"]}, 200)
	delta := f.req("GET", "/api/v1/duty/handovers/"+f.id(h)+"/delta", f.night, nil, 200)
	sign := map[string]any{"expectedVersion": f.version(h), "idempotencyKey": "accept-http-once", "body": map[string]any{"revisionId": revisionID, "snapshotHash": delta["snapshotHash"]}}
	accepted := f.req("POST", "/api/v1/duty/handovers/"+f.id(h)+"/accept", f.night, sign, 200)
	retry := f.req("POST", "/api/v1/duty/handovers/"+f.id(h)+"/accept", f.night, sign, 200)
	if f.version(accepted) != f.version(retry) || dutyHTTPBody(accepted)["status"] != "ACCEPTED" {
		t.Fatal("signature duplicated", accepted, retry)
	}
	updated := f.req("GET", "/api/v1/duty/items/"+f.id(item), f.night, nil, 200)
	if dutyHTTPBody(updated)["ownerId"] != "night" || dutyHTTPBody(updated)["status"] != "OPEN" {
		t.Fatal("item was not handed over", updated)
	}
	history := f.req("GET", "/api/v1/duty/items/"+f.id(item)+"/events", f.night, nil, 200)
	if history["total"].(float64) < 2 {
		t.Fatal(history)
	}
	f.req("GET", "/api/v1/duty/revisions/"+revisionID+"/events", f.night, nil, 200)
	for _, suffix := range []string{"pdf", "events.csv"} {
		req, _ := http.NewRequest("GET", f.server.URL+"/api/v1/duty/revisions/"+revisionID+"/"+suffix, nil)
		req.Header.Set("Authorization", "Bearer "+f.night)
		resp, e := f.server.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("export %s status=%d body=%s", suffix, resp.StatusCode, data)
		}
		if suffix == "pdf" && !bytes.HasPrefix(data, []byte("%PDF-")) {
			t.Fatal("invalid PDF header")
		}
	}
	state, e := f.repo.LoadAccessState(context.Background(), f.tenant)
	if e != nil {
		t.Fatal(e)
	}
	for i := range state.Users {
		if state.Users[i].Username == "night" {
			state.Users[i].DeviceIDs = []string{}
			state.Users[i].DeviceScope = "selected"
		}
	}
	if ok, e := f.repo.SaveAccessState(context.Background(), f.tenant, state); e != nil || !ok {
		t.Fatal(ok, e)
	}
	f.req("GET", "/api/v1/duty/revisions/"+revisionID, f.night, nil, 403)
	f.req("GET", "/api/v1/duty/revisions/"+revisionID+"/pdf", f.night, nil, 403)
	f.req("POST", "/api/v1/duty/handovers/"+f.id(h)+"/accept", f.night, sign, 403)
}

func TestDutyHTTPAttachmentMetadataAndPermissionBoundary(t *testing.T) {
	f := newDutyHTTPFixture(t, "duty_attachment_http", "duty-http-test-password")
	buf := new(bytes.Buffer)
	form := multipart.NewWriter(buf)
	file, e := form.CreateFormFile("file", "现场说明.txt")
	if e != nil {
		t.Fatal(e)
	}
	file.Write([]byte("现场情况已核实"))
	form.Close()
	req, _ := http.NewRequest("POST", f.server.URL+"/api/v1/duty/runs/"+f.id(f.dayRun)+"/attachments", buf)
	req.Header.Set("Authorization", "Bearer "+f.day)
	req.Header.Set("Content-Type", form.FormDataContentType())
	resp, e := f.server.Client().Do(req)
	if e != nil {
		t.Fatal(e)
	}
	var attachment model.DutyAttachment
	if e = json.NewDecoder(resp.Body).Decode(&attachment); e != nil {
		t.Fatal(e)
	}
	resp.Body.Close()
	if resp.StatusCode != 201 || attachment.ID == "" {
		t.Fatal(resp.StatusCode, attachment)
	}
	savedKey := attachment.ObjectKey
	attachment.ObjectKey = "attacker-controlled-key"
	attachment.Size = 999999
	record := f.command("POST", "/api/v1/duty/runs/"+f.id(f.dayRun)+"/records", f.day, 0, map[string]any{"content": "现场已核实", "attachments": []model.DutyAttachment{attachment}}, 201)
	metadata := dutyHTTPBody(record)["attachments"].([]any)[0].(map[string]any)
	if metadata["objectKey"] != savedKey || int64(metadata["size"].(float64)) == 999999 {
		t.Fatal("browser controlled storage metadata", metadata)
	}
	download, _ := http.NewRequest("GET", f.server.URL+"/api/v1/duty/attachments/"+attachment.ID, nil)
	download.Header.Set("Authorization", "Bearer "+f.day)
	res, e := f.server.Client().Do(download)
	if e != nil {
		t.Fatal(e)
	}
	data, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || string(data) != "现场情况已核实" {
		t.Fatal(res.StatusCode, string(data))
	}
	f.req("POST", "/api/v1/duty/stations", f.day, map[string]any{"body": map[string]any{"name": "越权配置"}}, 403)
	f.command("POST", "/api/v1/duty/runs/"+f.id(f.dayRun)+"/records", f.day, 0, map[string]any{"content": "未授权设备", "deviceId": "private-device"}, 403)
	f.req("GET", "/api/v1/duty/attachments/"+attachment.ID, "", nil, 401)
	state, _ := f.repo.LoadAccessState(context.Background(), f.tenant)
	for i := range state.Users {
		if state.Users[i].Username == "day" {
			state.Users[i].Permissions = []string{"menu:duty", "action:duty:participate"}
		}
	}
	if ok, e := f.repo.SaveAccessState(context.Background(), f.tenant, state); e != nil || !ok {
		t.Fatal(ok, e)
	}
	f.command("POST", "/api/v1/duty/runs/"+f.id(f.dayRun)+"/records", f.day, 0, map[string]any{"content": "撤销记录权限"}, 403)
}

func TestDutyHTTPLegacyClaimsCannotElevateToDutyAdministrator(t *testing.T) {
	f := newDutyHTTPFixture(t, "duty_legacy_identity", "duty-http-test-password")
	for _, role := range []string{"viewer", "operator", "admin"} {
		token, e := f.api.auth.Issue("not-a-platform-account", f.tenant, role, nil, time.Hour)
		if e != nil {
			t.Fatal(e)
		}
		f.req("GET", "/api/v1/duty/stations", token, nil, 403)
		f.command("POST", "/api/v1/duty/stations", token, 0, map[string]any{"name": "越权岗位", "supervisorId": "root", "deviceIds": []string{"private-device"}, "requiredPeople": 1, "enabled": true}, 403)
	}
	f.req("GET", "/api/v1/duty/stations", f.admin, nil, 200)
	f.req("GET", "/api/v1/duty/runs/current", f.day, nil, 200)
}

// Explicit opt-in fixture for a browser smoke run. It is an actual HTTP server
// with the real handlers and durable-in-process business state, not mocked JSON.
// No real credentials or host dependencies are used. The caller supplies an
// ephemeral password; logs never include it.
func TestDutyBrowserFixtureServer(t *testing.T) {
	if os.Getenv("DUTY_BROWSER_FIXTURE") != "1" {
		t.Skip("set DUTY_BROWSER_FIXTURE=1 for browser smoke")
	}
	password := os.Getenv("DUTY_BROWSER_PASSWORD")
	if len(password) < 12 {
		t.Fatal("set an ephemeral DUTY_BROWSER_PASSWORD with at least 12 characters")
	}
	f := newDutyHTTPFixture(t, "duty_browser", password)
	f.server.Close()
	listener, e := net.Listen("tcp", "127.0.0.1:18091")
	if e != nil {
		t.Fatal(e)
	}
	server := &http.Server{Handler: f.api.Handler(), ReadHeaderTimeout: 10 * time.Second}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	timer := time.NewTimer(30 * time.Minute)
	defer timer.Stop()
	t.Log("Duty browser fixture ready at http://127.0.0.1:18091; tenant duty_browser; users root, day, night")
	select {
	case <-ctx.Done():
	case <-timer.C:
	case e = <-done:
		if e != nil && e != http.ErrServerClosed {
			t.Fatal(e)
		}
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if e = server.Shutdown(shutdown); e != nil {
		t.Fatal(e)
	}
}
