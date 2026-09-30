package httpapi

import (
	"context"
	"io"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"log/slog"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestGovernanceAPIIndependentRecordPreciseActionsAndCurrentScope(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	for _, id := range []string{"a", "b"} {
		if e := repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "t", ID: id, AccessKey: id}); e != nil {
			t.Fatal(e)
		}
	}
	state := model.AccessState{Users: []model.PlatformUser{{Username: "reader", Enabled: true, SessionVersion: 1, DeviceScope: "selected", DeviceIDs: []string{"a"}, Permissions: []string{"menu:devices", "menu:alarms", "menu:alarmGovernance", "action:alarmGovernance:record"}}}}
	if ok, e := repo.SaveAccessState(ctx, "t", state); e != nil || !ok {
		t.Fatal(e)
	}
	api := New(config.Config{AdminUser: "admin", AdminTenants: []string{"t", "other"}, JWTSecret: "governance-test-secret-longer-than-32-characters", DevMode: true}, &core.Engine{Repo: repo}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	token, e := api.auth.IssueUser("reader", "t", 1, time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	admin, e := api.auth.Issue("admin", "t", "admin", nil, time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now().UnixMilli()
	obs, _, e := repo.SaveAlarmObservation(ctx, model.AlarmObservation{TenantID: "t", DeviceID: "a", AlarmType: "FIRE", OriginKind: "DEVICE_DIRECT", SignalKey: "device:FIRE", SourceSystem: "device", SourceEventID: "first", FactKind: "REPORT", EventAt: now - 1000, TimeQuality: "TRUSTED", Payload: map[string]any{"fullPayload": "restricted-source-body"}})
	if e != nil {
		t.Fatal(e)
	}
	req := func(method, path string, body any, status int) map[string]any {
		return requestJSON(t, server.Client(), method, server.URL+path, token, body, status)
	}
	caseBody := map[string]any{"title": "本点位核查", "deviceId": "a", "alarmType": "FIRE", "originKind": "DEVICE_DIRECT", "signalKey": "device:FIRE", "ownerUserId": "reader", "observationIds": []string{obs.ID}, "idempotencyKey": "case-create"}
	req("POST", "/api/v1/alarm-governance/cases", caseBody, 403)
	verificationBody := map[string]any{"deviceId": "a", "alarmType": "FIRE", "originKind": "DEVICE_DIRECT", "signalKey": "device:FIRE", "observationIds": []string{obs.ID}, "verificationMethod": "ON_SITE", "verifiedAt": now - 100, "fieldResult": "UNABLE_TO_DETERMINE", "activityRelation": "UNKNOWN", "checkScope": "已到场，部分资料未知", "fieldValues": map[string]any{"facilityStatus": "UNKNOWN"}, "idempotencyKey": "independent"}
	v := req("POST", "/api/v1/alarm-governance/verifications", verificationBody, 201)
	again := req("POST", "/api/v1/alarm-governance/verifications", verificationBody, 201)
	if v["id"] != again["id"] {
		t.Fatal("duplicate submission created new record")
	}
	verificationBody["tenantId"] = "other"
	verificationBody["idempotencyKey"] = "tenant-injection"
	req("POST", "/api/v1/alarm-governance/verifications", verificationBody, 422)
	delete(verificationBody, "tenantId")
	req("GET", "/api/v1/alarm-governance/verifications/"+v["id"].(string), nil, 200)
	direct := req("GET", "/api/v1/alarm-governance/observations/"+obs.ID, nil, 200)
	if _, ok := direct["payload"]; ok {
		t.Fatal("full source payload bypassed raw-message permissions")
	}
	page := req("GET", "/api/v1/alarm-governance/observations?deviceIds=a&start="+strconv.FormatInt(now-2000, 10)+"&end="+strconv.FormatInt(now, 10)+"&limit=1", nil, 200)
	cursor, ok := page["nextCursor"].(string)
	if !ok || cursor == obs.ID || cursor == "" {
		t.Fatal("unstable observation cursor", page)
	}
	shared := requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/alarm-governance/activities", admin, map[string]any{"deviceIds": []string{"a", "b"}, "location": "共享作业区", "activityType": "COOKING", "unit": "一次供餐", "actual": true, "startAt": now - 1000, "endAt": now - 500, "timeQuality": "TRUSTED", "idempotencyKey": "shared"}, 201)
	req("GET", "/api/v1/alarm-governance/activities/"+shared["id"].(string), nil, 403)
	list := req("GET", "/api/v1/alarm-governance/activities", nil, 200)
	if list["total"] != float64(0) {
		t.Fatal("hidden shared member count leaked")
	}
	sample := model.StandardMessage{TenantID: "t", DeviceID: "a", MessageID: "processed-source-field", RawMessageID: "private-raw", Timestamp: now - 1000, Properties: map[string]any{"temperature": 27.0, "nested": map[string]any{"private": "not-exposed"}}, Raw: map[string]any{"secret": "not-exposed"}}
	claim, e := repo.ClaimStandardMessage(ctx, sample, "test", time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	if e = repo.MarkStandardMessageProcessed(ctx, sample.TenantID, sample.MessageID, claim.Token); e != nil {
		t.Fatal(e)
	}
	sourcePath := "/api/v1/alarm-governance/source-fields?deviceId=a&start=" + strconv.FormatInt(now-2000, 10) + "&end=" + strconv.FormatInt(now, 10)
	req("GET", sourcePath, nil, 403)
	state, e = repo.LoadAccessState(ctx, "t")
	if e != nil {
		t.Fatal(e)
	}
	state.Users[0].Permissions = append(state.Users[0].Permissions, "action:alarmGovernance:templates")
	if ok, e := repo.SaveAccessState(ctx, "t", state); e != nil || !ok {
		t.Fatal(e)
	}
	fields := req("GET", sourcePath, nil, 200)
	items, ok := fields["items"].([]any)
	if !ok || len(items) != 1 || items[0].(map[string]any)["sourceFieldPath"] != "properties.temperature" || items[0].(map[string]any)["sourceType"] != "NUMBER" {
		t.Fatal("source directory exposed raw or nested fields", fields)
	}
	state, e = repo.LoadAccessState(ctx, "t")
	if e != nil {
		t.Fatal(e)
	}
	state.Users[0].DeviceIDs = []string{"b"}
	if ok, e := repo.SaveAccessState(ctx, "t", state); e != nil || !ok {
		t.Fatal(e)
	}
	req("GET", "/api/v1/alarm-governance/verifications/"+v["id"].(string), nil, 403)
	req("GET", "/api/v1/alarm-governance/observations/"+obs.ID, nil, 403)
	req("GET", sourcePath, nil, 403)
	list = req("GET", "/api/v1/alarm-governance/verifications", nil, 200)
	if list["total"] != float64(0) {
		t.Fatal("revoked record count leaked")
	}
}

func TestGovernancePermissionsCanBeAssignedThroughAccessAPI(t *testing.T) {
	repo := memory.NewRepository()
	if e := repo.SaveManagedDevice(context.Background(), model.ManagedDevice{TenantID: "t", ID: "a", AccessKey: "fixture-a"}); e != nil {
		t.Fatal(e)
	}
	api := New(config.Config{AdminUser: "admin", AdminTenants: []string{"t"}, JWTSecret: "governance-catalog-test-secret-at-least-32", DevMode: true}, &core.Engine{Repo: repo}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	token, e := api.auth.Issue("admin", "t", "admin", nil, time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	permissions := []string{"menu:devices", "menu:alarms", "menu:alarmGovernance", "menu:cameras", "action:cameras:download"}
	for action := range governanceActionNames {
		permissions = append(permissions, "action:alarmGovernance:"+action)
	}
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/access/roles", token, map[string]any{"id": "governance-operator", "name": "治理操作人员", "permissions": permissions, "deviceScope": "selected", "deviceIds": []string{"a"}}, 200)
	state, e := repo.LoadAccessState(context.Background(), "t")
	if e != nil || len(state.Roles) != 1 || len(state.Roles[0].Permissions) != len(permissions) {
		t.Fatalf("governance actions cannot be assigned: %v", e)
	}
}
