package httpapi

import (
	"context"
	"io"
	"iot-platform/internal/devicescope"
	"log/slog"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"iot-platform/internal/adapters/clickhouse"
	"iot-platform/internal/adapters/memory"
	redisadapter "iot-platform/internal/adapters/redis"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/repositorytest"
)

func TestFireSafetyHTTPWorkflowsAndPermissions(t *testing.T) {
	repo := memory.NewRepository()
	// Exercise the real forwarding chain: durable store -> telemetry -> cache -> scope.
	decorated := redisadapter.New(&clickhouse.Repository{Repository: repo}, nil)
	cfg := config.Config{AdminUser: "root", AdminPassword: "fire-root-test", AdminTenants: []string{"fire-a", "fire-b"}, JWTSecret: "fire-test-secret-at-least-32-characters", DevMode: true, DataDir: t.TempDir()}
	api := New(cfg, &core.Engine{Repo: devicescope.Wrap(decorated)}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	req := func(method, path, token string, body any, status int) map[string]any {
		return requestJSON(t, server.Client(), method, server.URL+path, token, body, status)
	}
	login := func(name, password, tenant string) string {
		return req("POST", "/api/v1/auth/login", "", map[string]any{"username": name, "password": password, "tenantId": tenant}, 200)["accessToken"].(string)
	}
	root := login("root", cfg.AdminPassword, "fire-a")
	other := login("root", cfg.AdminPassword, "fire-b")
	station := req("POST", "/api/v1/fire-stations", root, map[string]any{"code": "FS-1", "name": "一号消防站", "type": "micro", "enabled": true, "contact": "private contact", "phone": "private phone"}, 201)
	sid := station["id"].(string)
	person := func(name string) map[string]any {
		return req("POST", "/api/v1/fire-personnel", root, map[string]any{"name": name, "stationId": sid, "enabled": true}, 201)
	}
	p1, p2 := person("甲"), person("乙")
	pid1, pid2 := p1["id"].(string), p2["id"].(string)
	shift := req("POST", "/api/v1/duty/shifts", root, map[string]any{"name": "夜班", "startTime": "20:00", "endTime": "08:00"}, 201)
	start := time.Now().Add(24 * time.Hour).UnixMilli()
	assignment := req("POST", "/api/v1/duty/assignments", root, map[string]any{"stationId": sid, "shiftId": shift["id"], "personnelIds": []string{pid1}, "startAt": start, "endAt": start + 12*int64(time.Hour/time.Millisecond)}, 201)
	aid := assignment["id"].(string)
	req("POST", "/api/v1/duty/assignments", root, map[string]any{"stationId": sid, "shiftId": shift["id"], "personnelIds": []string{pid1}, "startAt": start + 1000, "endAt": start + 2000}, 409)
	// Window queries include an assignment crossing their left boundary.
	window := req("GET", "/api/v1/duty/assignments?fromAt="+strconv.FormatInt(start+1000, 10)+"&toAt="+strconv.FormatInt(start+2000, 10), root, nil, 200)
	if window["total"] != float64(1) {
		t.Fatalf("overlap query omitted assignment: %v", window)
	}
	byName := req("GET", "/api/v1/duty/assignments?q=%E5%A4%9C%E7%8F%AD", root, nil, 200)
	if byName["total"] != float64(1) {
		t.Fatalf("shift name search omitted assignment: %v", byName)
	}
	req("GET", "/api/v1/duty/assignments?page=2&pageSize=1", root, nil, 200)
	req("GET", "/api/v1/fire-stations/"+sid, other, nil, 404)
	req("POST", "/api/v1/fire-personnel", other, map[string]any{"name": "cross tenant", "stationId": sid, "enabled": true}, 400)
	req("DELETE", "/api/v1/fire-stations/"+sid+"?version=1", root, nil, 409)
	// Users may manage these independent resources without any IoT device grants.
	user := map[string]any{"username": "duty_reader", "displayName": "排班查看", "password": "fire-reader-password", "enabled": true, "roleIds": []string{}, "permissions": []string{"menu:duty"}, "deviceScope": "none"}
	req("POST", "/api/v1/access/users", root, user, 200)
	reader := login("duty_reader", "fire-reader-password", "fire-a")
	options := req("GET", "/api/v1/fire-safety/options", reader, nil, 200)
	if len(options["stations"].([]any)) != 1 || len(options["personnel"].([]any)) != 2 {
		t.Fatalf("missing shared options: %v", options)
	}
	for _, item := range options["stations"].([]any) {
		row := item.(map[string]any)
		if _, ok := row["phone"]; ok {
			t.Fatal("station contact leaked through options")
		}
		if _, ok := row["contact"]; ok {
			t.Fatal("station contact leaked through options")
		}
	}
	if len(options["equipment"].([]any)) != 0 || len(options["extinguishers"].([]any)) != 0 {
		t.Fatal("ungranted catalog exposed")
	}
	req("GET", "/api/v1/duty/assignments", reader, nil, 200)
	req("GET", "/api/v1/fire-personnel", reader, nil, 403)
	req("GET", "/api/v1/extinguishers", reader, nil, 403)
	req("POST", "/api/v1/duty/swaps", reader, map[string]any{}, 403)
	req("GET", "/api/v1/fire-safety/options", "", nil, 401)
	swap := req("POST", "/api/v1/duty/swaps", root, map[string]any{"assignmentId": aid, "fromPersonnelId": pid1, "toPersonnelId": pid2, "reason": "工作安排"}, 201)
	swaps := req("GET", "/api/v1/duty/swaps", root, nil, 200)
	swapContext := swaps["items"].([]any)[0].(map[string]any)["assignment"].(map[string]any)
	if swapContext["stationId"] != sid || swapContext["startAt"] != float64(start) {
		t.Fatalf("swap approval lacks schedule context: %v", swapContext)
	}
	req("POST", "/api/v1/duty/swaps/"+swap["id"].(string)+"/review", root, map[string]any{"version": swap["version"], "approved": true, "note": "同意"}, 403)
	reviewer, err := api.auth.Issue("reviewer", "fire-a", "operator", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	req("POST", "/api/v1/duty/swaps/"+swap["id"].(string)+"/review", reviewer, map[string]any{"version": swap["version"], "approved": true, "note": "同意"}, 200)
	changed := req("GET", "/api/v1/duty/assignments/"+aid, root, nil, 200)
	if changed["personnelIds"].([]any)[0] != pid2 {
		t.Fatalf("swap did not change assignment: %v", changed)
	}
	assignment["notes"] = "stale update"
	req("PUT", "/api/v1/duty/assignments/"+aid, root, assignment, 409)
	equipment := req("POST", "/api/v1/fire-equipment", root, map[string]any{"stationId": sid, "name": "水带", "category": "灭火器材", "quantity": 2, "unit": "条", "status": "ready"}, 201)
	dispatchBody := map[string]any{"stationId": sid, "title": "现场演练", "type": "drill", "location": "一号楼", "personnelIds": []string{pid1}, "equipment": []map[string]any{{"equipmentId": equipment["id"], "quantity": 2}}, "startedAt": time.Now().Add(-time.Minute).UnixMilli()}
	dispatch := req("POST", "/api/v1/fire-dispatches", root, dispatchBody, 201)
	dispatchBody["personnelIds"] = []string{pid2}
	req("POST", "/api/v1/fire-dispatches", root, dispatchBody, 409)
	req("POST", "/api/v1/fire-dispatches/"+dispatch["id"].(string)+"/return", root, map[string]any{"version": dispatch["version"], "returnedAt": time.Now().UnixMilli(), "summary": "演练完成，器材归还"}, 200)
	req("POST", "/api/v1/fire-dispatches", root, dispatchBody, 201)
	asset := req("POST", "/api/v1/extinguishers", root, map[string]any{"code": "EX-1", "stationId": sid, "location": "门厅", "type": "dry_powder", "inspectionCycleDays": 30, "status": "active", "manufacturedOn": "2025-01-01", "serviceDueOn": time.Now().AddDate(0, 0, -2).Format("2006-01-02"), "retireOn": "2030-01-01"}, 201)
	exid := asset["id"].(string)
	task := req("POST", "/api/v1/extinguisher-inspections", root, map[string]any{"extinguisherId": exid, "assigneeId": pid1, "dueAt": time.Now().Add(time.Hour).UnixMilli()}, 201)
	tid := task["id"].(string)
	task = req("POST", "/api/v1/extinguisher-inspections/"+tid+"/inspect", root, map[string]any{"version": task["version"], "checks": []map[string]any{{"name": "外观及筒体", "passed": true}, {"name": "压力指示", "passed": false}, {"name": "喷管及附件", "passed": true}, {"name": "铭牌及日期", "passed": true}, {"name": "放置及标识", "passed": true}}, "findings": "压力不足"}, 200)
	if task["status"] != "rectifying" || task["result"] != "fail" {
		t.Fatalf("failed inspection did not open rectification: %v", task)
	}
	task = req("POST", "/api/v1/extinguisher-inspections/"+tid+"/rectify", root, map[string]any{"version": task["version"], "action": "更换灭火器并重新检查"}, 200)
	req("POST", "/api/v1/extinguisher-inspections/"+tid+"/review", root, map[string]any{"version": task["version"], "approved": true, "note": "通过"}, 403)
	task = req("POST", "/api/v1/extinguisher-inspections/"+tid+"/review", reviewer, map[string]any{"version": task["version"], "approved": false, "note": "仍需处理"}, 200)
	task = req("POST", "/api/v1/extinguisher-inspections/"+tid+"/rectify", root, map[string]any{"version": task["version"], "action": "补充处理"}, 200)
	task = req("POST", "/api/v1/extinguisher-inspections/"+tid+"/review", reviewer, map[string]any{"version": task["version"], "approved": true, "note": "复核合格"}, 200)
	if task["status"] != "completed" || len(task["rectifications"].([]any)) != 2 {
		t.Fatalf("rectification history missing: %v", task)
	}
	stats := req("GET", "/api/v1/extinguishers/statistics", root, nil, 200)
	if stats["overdue"] != float64(1) || stats["rectifying"] != float64(0) {
		t.Fatalf("incorrect asset reminder statistics: %v", stats)
	}
	req("GET", "/api/v1/extinguishers?due=overdue", root, nil, 200)
	// Keyword search matches visible text only, never field names.
	if found := req("GET", "/api/v1/extinguishers?q=status", root, nil, 200); found["total"] != float64(0) {
		t.Fatalf("field name matched every row: %v", found)
	}
	if found := req("GET", "/api/v1/extinguishers?q=ex-1", root, nil, 200); found["total"] != float64(1) {
		t.Fatalf("asset code search failed: %v", found)
	}
	batch := req("POST", "/api/v1/extinguisher-inspections/batch", root, map[string]any{"extinguisherIds": []string{exid}, "assigneeId": pid1, "dueAt": time.Now().Add(time.Hour).UnixMilli()}, 200)
	if batch["created"] != float64(1) {
		t.Fatalf("batch inspection not created: %v", batch)
	}
	assets := req("GET", "/api/v1/extinguishers", root, nil, 200)
	if open := assets["items"].([]any)[0].(map[string]any)["openInspection"].(map[string]any); open["status"] != "pending" {
		t.Fatalf("asset does not show its open inspection: %v", open)
	}
	if checks := req("GET", "/api/v1/fire-safety/options", root, nil, 200)["inspectionChecks"].([]any); len(checks) != 5 {
		t.Fatalf("standard checks not offered: %v", checks)
	}
	future := time.Now().Add(30 * 24 * time.Hour).UnixMilli()
	days := []map[string]any{}
	for i := int64(0); i < 3; i++ {
		days = append(days, map[string]any{"stationId": sid, "shiftId": assignment["shiftId"], "personnelIds": []string{pid1}, "startAt": future + i*86400000, "endAt": future + i*86400000 + 3600000})
	}
	if created := req("POST", "/api/v1/duty/assignments/batch", root, map[string]any{"assignments": days}, 200); created["created"] != float64(3) {
		t.Fatalf("batch assignments not created: %v", created)
	}
	req("POST", "/api/v1/duty/assignments/batch", root, map[string]any{"assignments": days}, 409)
	req("DELETE", "/api/v1/extinguishers/"+exid+"?version=1", root, nil, 409)
	// Station statistics count enabled stations and personnel only.
	before := req("GET", "/api/v1/fire-stations/statistics", root, nil, 200)
	req("POST", "/api/v1/fire-personnel", root, map[string]any{"name": "停用人员", "stationId": sid, "enabled": false}, 201)
	req("POST", "/api/v1/fire-stations", root, map[string]any{"code": "FS-OFF", "name": "停用消防站", "type": "micro", "enabled": false}, 201)
	if after := req("GET", "/api/v1/fire-stations/statistics", root, nil, 200); after["stations"] != before["stations"] || after["personnel"] != before["personnel"] || len(after["byStation"].([]any)) != len(before["byStation"].([]any)) {
		t.Fatalf("disabled stations or personnel counted: before %v after %v", before, after)
	}
	// Revoking the menu immediately revokes shared lookups on the same token.
	user["permissions"] = []string{}
	req("PUT", "/api/v1/access/users/duty_reader", root, user, 200)
	req("GET", "/api/v1/fire-safety/options", reader, nil, 401)
	reader = login("duty_reader", "fire-reader-password", "fire-a")
	req("GET", "/api/v1/fire-safety/options", reader, nil, 403)
	stored, err := repo.LoadFireSafetyState(context.Background(), "fire-a")
	if err != nil || len(stored.Dispatches) != 2 || len(stored.Inspections) != 2 || len(stored.Assignments) != 4 {
		t.Fatalf("decorated API did not persist: %+v %v", stored, err)
	}
}

func TestFireSafetyRepositoryDecoratorContract(t *testing.T) {
	base := memory.NewRepository()
	repo := devicescope.Wrap(redisadapter.New(&clickhouse.Repository{Repository: base}, nil))
	repositorytest.FireSafety(t, repo)
}

func TestExtinguisherReminderDatesAndReview(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.Local)
	asset := model.Extinguisher{FireRecord: model.FireRecord{ID: "asset", CreatedAt: now.AddDate(0, 0, -31).UnixMilli()}, Status: "active", InspectionCycleDays: 30, ServiceDueOn: "2026-10-01", RetireOn: "2026-09-30"}
	reminders, _, _ := extinguisherReminders(asset, nil, now, 30)
	if reminders[0].Status != "soon" || reminders[1].Status != "overdue" || reminders[2].Status != "overdue" {
		t.Fatalf("date boundary: %v", reminders)
	}
	inspected := now.AddDate(0, 0, -10).UnixMilli()
	reviewed := now.AddDate(0, 0, -1).UnixMilli()
	tasks := []model.FireInspection{{ExtinguisherID: "asset", Status: "completed", Result: "fail", InspectedAt: inspected, Rectifications: []model.FireRectification{{ReviewedAt: reviewed, Status: "approved"}}}}
	_, last, next := extinguisherReminders(asset, tasks, now, 30)
	if last != inspected || next != "2026-10-30" {
		t.Fatalf("accepted rectification did not start cycle: %d %s", last, next)
	}
	asset.Status = "retired"
	reminders, _, _ = extinguisherReminders(asset, tasks, now, 30)
	if len(reminders) != 0 {
		t.Fatal("retired asset still reminded")
	}
}
