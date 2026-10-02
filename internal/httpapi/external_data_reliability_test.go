package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/core"
	"iot-platform/internal/externaldata"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// Models an acknowledged queue publish followed by delayed asynchronous raw
// consumption. Other topics still run the platform's real parser/processor.
type externalDelayedRawBus struct {
	ports.EventBus
	mu    sync.Mutex
	queue []externalDelayedRaw
}
type externalDelayedRaw struct {
	topic, key string
	payload    []byte
}

func (b *externalDelayedRawBus) Publish(ctx context.Context, topic, key string, payload []byte) error {
	if topic != model.TopicRaw {
		return b.EventBus.Publish(ctx, topic, key, payload)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.queue = append(b.queue, externalDelayedRaw{topic, key, append([]byte(nil), payload...)})
	return nil
}
func (b *externalDelayedRawBus) drain(ctx context.Context) error {
	b.mu.Lock()
	queued := b.queue
	b.queue = nil
	b.mu.Unlock()
	for _, item := range queued {
		if err := b.EventBus.Publish(ctx, item.topic, item.key, item.payload); err != nil {
			return err
		}
	}
	return nil
}

func externalReliabilityEntries(t *testing.T, s *Server, kind string) []externaldata.Entry {
	t.Helper()
	entries, _, err := s.externalData.Store.List(context.Background(), externaldata.Query{TenantID: externalTestTenant, Kind: kind, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func externalReliabilityRecord(t *testing.T, s *Server, eventID string, version int64) (externaldata.Entry, externaldata.Record) {
	t.Helper()
	for _, entry := range externalReliabilityEntries(t, s, "record") {
		var record externaldata.Record
		if err := json.Unmarshal(entry.Body, &record); err != nil {
			t.Fatal(err)
		}
		var raw struct {
			ID      string `json:"eventId"`
			Version int64  `json:"version"`
		}
		if err := json.Unmarshal(record.Raw, &raw); err != nil {
			t.Fatal(err)
		}
		if raw.ID == eventID && raw.Version == version {
			return entry, record
		}
	}
	t.Fatalf("record %s version %d is missing", eventID, version)
	return externaldata.Entry{}, externaldata.Record{}
}

func externalReliabilityStep(t *testing.T, api *Server, ctx context.Context) {
	t.Helper()
	worked, err := api.externalData.Step(ctx, "record", "async-test-worker")
	if !worked || err != nil {
		t.Fatalf("record worker: worked=%v error=%v", worked, err)
	}
}

func externalReliabilityRetryNow(t *testing.T, api *Server, ctx context.Context, eventID string, version int64) {
	t.Helper()
	entry, _ := externalReliabilityRecord(t, api, eventID, version)
	entry.DueAt = 0
	if _, err := api.externalData.Store.Put(ctx, entry, entry.Revision); err != nil {
		t.Fatal(err)
	}
	externalReliabilityStep(t, api, ctx)
}

func TestExternalDataRecoveryWaitsForAsynchronousRawDelivery(t *testing.T) {
	api, repo, ctx := externalAPIFixture(t)
	bus := &externalDelayedRawBus{EventBus: local.NewBus()}
	// The fixture's already-started engine remains on its own local bus. This
	// engine shares its repository and archive but has an independently queued
	// parser path and no additional singleton jobs.
	engine := core.New(api.engine.Repo, api.engine.Archive, bus, local.NewRealtime(), api.engine.Parsers, api.log)
	if err := engine.StartWith(ctx, core.Components{Parser: true, Processor: true}); err != nil {
		t.Fatal(err)
	}
	api.engine = engine
	source, err := api.externalData.SaveSource(ctx, externalTestTenant, externaldata.Source{Name: "异步视频平台", Username: "root", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	ep, err := api.externalData.SaveEndpoint(ctx, externalTestTenant, externaldata.Endpoint{Name: "异步告警", SourceID: source.ID, Enabled: true, Mode: "push", Kind: "video_alarm", Mapping: externalAlarmMapping()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = api.externalData.SaveBinding(ctx, externalTestTenant, externaldata.Binding{SourceID: source.ID, Kind: "camera", ExternalID: "vendor-camera", TargetID: "ext-camera"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Add(-time.Minute).UnixMilli()
	receive := func(id, status string, version int64) {
		t.Helper()
		payload, _ := json.Marshal(map[string]any{"eventId": id, "camera": "vendor-camera", "time": now + version, "version": version, "status": status, "type": "FIRE", "text": "视频识别火警"})
		if _, err := api.externalData.Receive(ctx, externalTestTenant, ep, payload, ""); err != nil {
			t.Fatal(err)
		}
		externalReliabilityStep(t, api, ctx)
	}
	receive("alarm-one", "ACTIVE", 1)
	initial, initialRecord := externalReliabilityRecord(t, api, "alarm-one", 1)
	if initial.Status != "RETRY" || initialRecord.MessageID == "" {
		t.Fatalf("initial event falsely completed before raw processing: %+v %+v", initial, initialRecord)
	}
	index, err := repo.GetRawIndex(ctx, externalTestTenant, initialRecord.MessageID)
	if err != nil || index.PublishedAt == 0 {
		t.Fatal("raw was not durably archived and queue-acknowledged", err)
	}
	if len(externalReliabilityEntries(t, api, "delivery")) != 0 {
		t.Fatal("test did not delay raw delivery")
	}
	receive("alarm-one", "RECOVERED", 2)
	recovery, _ := externalReliabilityRecord(t, api, "alarm-one", 2)
	if recovery.Status != "RETRY" {
		t.Fatalf("recovery completed without the pending alarm identity: %s", recovery.Status)
	}
	// A distinct event from the same platform/camera/type progresses separately.
	receive("alarm-two", "ACTIVE", 1)
	if err = bus.drain(ctx); err != nil {
		t.Fatal(err)
	}
	if len(externalReliabilityEntries(t, api, "delivery")) != 2 {
		t.Fatal("raw consumer did not complete both independent events")
	}
	externalReliabilityRetryNow(t, api, ctx, "alarm-one", 1)
	initial, initialRecord = externalReliabilityRecord(t, api, "alarm-one", 1)
	if initial.Status != "PROCESSED" || initialRecord.AlarmID == "" {
		t.Fatalf("late delivery result not consumed: %+v %+v", initial, initialRecord)
	}
	externalReliabilityRetryNow(t, api, ctx, "alarm-one", 2)
	recovery, recoveryRecord := externalReliabilityRecord(t, api, "alarm-one", 2)
	if recovery.Status != "PROCESSED" || recoveryRecord.AlarmID != initialRecord.AlarmID {
		t.Fatalf("deferred recovery lost alarm identity: %+v %+v", recovery, recoveryRecord)
	}
	externalReliabilityRetryNow(t, api, ctx, "alarm-two", 1)
	_, otherRecord := externalReliabilityRecord(t, api, "alarm-two", 1)
	first, err := repo.GetAlarm(ctx, externalTestTenant, initialRecord.AlarmID)
	if err != nil || first.Status != "RECOVERED" {
		t.Fatalf("first alarm not recovered: %+v %v", first, err)
	}
	second, err := repo.GetAlarm(ctx, externalTestTenant, otherRecord.AlarmID)
	if err != nil || second.Status != "ACTIVE" || second.ID == first.ID {
		t.Fatalf("event-specific recovery affected another event: %+v %v", second, err)
	}
}

func TestExternalDataHTTPCreateRevisionCannotReplaceExistingConfiguration(t *testing.T) {
	api, _, _ := externalAPIFixture(t)
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	req := func(method, path, token string, payload any, status int) map[string]any {
		t.Helper()
		return requestJSON(t, server.Client(), method, server.URL+path, token, payload, status)
	}
	admin := req("POST", "/api/v1/auth/login", "", map[string]any{"tenantId": externalTestTenant, "username": "root", "password": api.cfg.AdminPassword}, 200)["accessToken"].(string)
	permissions := []string{"menu:externalData", "menu:devices", "POST /api/v1/external-data/sources", "POST /api/v1/external-data/endpoints", "POST /api/v1/external-data/bindings"}
	req("POST", "/api/v1/access/roles", admin, map[string]any{"id": "create-only", "name": "只能新建", "permissions": permissions, "deviceScope": "all"}, 200)
	req("POST", "/api/v1/access/users", admin, map[string]any{"username": "creator", "password": "create-only-test-password", "enabled": true, "roleIds": []string{"create-only"}, "deviceScope": "inherit"}, 200)
	user := req("POST", "/api/v1/auth/login", "", map[string]any{"tenantId": externalTestTenant, "username": "creator", "password": "create-only-test-password"}, 200)["accessToken"].(string)
	source := req("POST", externalBase+"/sources", user, externaldata.Source{Name: "原系统", Username: "creator", Enabled: true}, 201)
	sourceID := source["id"].(string)
	ep := req("POST", externalBase+"/endpoints", user, externaldata.Endpoint{Name: "原接口", SourceID: sourceID, Enabled: true, Mode: "push", Kind: "video_alarm", Mapping: externalAlarmMapping()}, 201)
	binding := req("POST", externalBase+"/bindings", user, externaldata.Binding{SourceID: sourceID, Kind: "camera", ExternalID: "vendor-camera", TargetID: "ext-camera"}, 201)
	source["name"] = "非法覆盖系统"
	req("POST", externalBase+"/sources", user, source, 422)
	req("PUT", externalBase+"/sources/"+sourceID, user, source, 403)
	ep["name"] = "非法覆盖接口"
	req("POST", externalBase+"/endpoints", user, ep, 422)
	req("PUT", externalBase+"/endpoints/"+ep["id"].(string), user, ep, 403)
	binding["targetId"] = "other-device"
	req("POST", externalBase+"/bindings", user, binding, 422)
	req("PUT", externalBase+"/bindings/"+binding["id"].(string), user, binding, 403)
	loaded, err := api.externalData.Source(context.Background(), externalTestTenant, sourceID)
	if err != nil || loaded.Name != "原系统" || loaded.Revision != 1 {
		t.Fatalf("POST bypassed update permission: %+v %v", loaded, err)
	}
	loadedEP, err := api.externalData.Endpoint(context.Background(), externalTestTenant, ep["id"].(string))
	if err != nil || loadedEP.Name != "原接口" || loadedEP.Revision != 1 {
		t.Fatalf("POST overwrote endpoint: %+v %v", loadedEP, err)
	}
}

func TestExternalDataHTTPOrdinaryFullScopeCannotBindRootExecutor(t *testing.T) {
	api, _, _ := externalAPIFixture(t)
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	req := func(method, path, token string, payload any, status int) map[string]any {
		t.Helper()
		return requestJSON(t, server.Client(), method, server.URL+path, token, payload, status)
	}
	admin := req("POST", "/api/v1/auth/login", "", map[string]any{"tenantId": externalTestTenant, "username": "root", "password": api.cfg.AdminPassword}, 200)["accessToken"].(string)
	permissions := []string{"menu:externalData", "menu:devices", "POST /api/v1/external-data/sources", "PUT /api/v1/external-data/sources/:id", "DELETE /api/v1/external-data/sources/:id", "POST /api/v1/external-data/endpoints", "PUT /api/v1/external-data/endpoints/:id", "POST /api/v1/external-data/bindings", "POST /api/v1/external-data/endpoints/:id/rotate-key", "POST /api/v1/external-data/endpoints/:id/receive", "POST /api/v1/external-data/endpoints/:id/pull"}
	req("POST", "/api/v1/access/roles", admin, map[string]any{"id": "connector-manager", "name": "全部设备接入管理", "permissions": permissions, "deviceScope": "all"}, 200)
	req("POST", "/api/v1/access/users", admin, map[string]any{"username": "manager", "password": "connector-manager-password", "enabled": true, "roleIds": []string{"connector-manager"}, "deviceScope": "inherit"}, 200)
	user := req("POST", "/api/v1/auth/login", "", map[string]any{"tenantId": externalTestTenant, "username": "manager", "password": "connector-manager-password"}, 200)["accessToken"].(string)
	rootSource := req("POST", externalBase+"/sources", admin, externaldata.Source{Name: "管理员执行系统", Username: "root", Enabled: true}, 201)
	rootID := rootSource["id"].(string)
	rootEP := req("POST", externalBase+"/endpoints", admin, externaldata.Endpoint{Name: "管理员接口", SourceID: rootID, Enabled: true, Mode: "push", Kind: "video_alarm", Mapping: externalAlarmMapping()}, 201)
	ownSource := req("POST", externalBase+"/sources", user, externaldata.Source{Name: "个人系统", Username: "manager", Enabled: true}, 201)
	// Having all devices and all these operations is not a platform identity.
	req("POST", externalBase+"/sources", user, externaldata.Source{Name: "冒用管理员", Username: "root", Enabled: true}, 403)
	ownSource["username"] = "root"
	req("PUT", externalBase+"/sources/"+ownSource["id"].(string), user, ownSource, 403)
	rootSource["username"] = "manager"
	req("PUT", externalBase+"/sources/"+rootID, user, rootSource, 403)
	req("DELETE", externalBase+"/sources/"+rootID+"?revision=1", user, nil, 403)
	req("POST", externalBase+"/endpoints", user, externaldata.Endpoint{Name: "冒用接口", SourceID: rootID, Enabled: true, Mode: "push", Kind: "video_alarm", Mapping: externalAlarmMapping()}, 403)
	req("POST", externalBase+"/bindings", user, externaldata.Binding{SourceID: rootID, Kind: "camera", ExternalID: "vendor-camera", TargetID: "ext-camera"}, 403)
	rootEP["sourceId"] = ownSource["id"]
	req("PUT", externalBase+"/endpoints/"+rootEP["id"].(string), user, rootEP, 403)
	rootEndpointPath := externalBase + "/endpoints/" + rootEP["id"].(string)
	req("POST", rootEndpointPath+"/rotate-key", user, map[string]any{}, 403)
	req("POST", rootEndpointPath+"/receive", user, map[string]any{"eventId": "blocked", "camera": "vendor-camera", "time": 1}, 403)
	req("POST", rootEndpointPath+"/pull", user, map[string]any{}, 403)
	loaded, err := api.externalData.Source(context.Background(), externalTestTenant, rootID)
	if err != nil || loaded.Username != "root" || loaded.Revision != 1 {
		t.Fatalf("root execution source changed: %+v %v", loaded, err)
	}
	if len(externalReliabilityEntries(t, api, "receipt")) != 0 {
		t.Fatal("unauthorized root execution accepted a receipt")
	}
}

func externalReliabilityDirectEndpoint(t *testing.T, api *Server, ctx context.Context) externaldata.Endpoint {
	t.Helper()
	source, err := api.externalData.SaveSource(ctx, externalTestTenant, externaldata.Source{Name: "事件更新平台", Username: "root", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	ep, err := api.externalData.SaveEndpoint(ctx, externalTestTenant, externaldata.Endpoint{Name: "事件更新接口", SourceID: source.ID, Enabled: true, Mode: "push", Kind: "video_alarm", Mapping: externalAlarmMapping()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = api.externalData.SaveBinding(ctx, externalTestTenant, externaldata.Binding{SourceID: source.ID, Kind: "camera", ExternalID: "vendor-camera", TargetID: "ext-camera"}); err != nil {
		t.Fatal(err)
	}
	return ep
}

func externalReliabilityDeliver(t *testing.T, api *Server, ctx context.Context, ep externaldata.Endpoint, id, status, alarmType string, version int64) {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{"eventId": id, "camera": "vendor-camera", "time": time.Now().Add(-time.Minute).UnixMilli(), "version": version, "status": status, "type": alarmType, "text": "更新后的告警"})
	if _, err := api.externalData.Receive(ctx, externalTestTenant, ep, payload, ""); err != nil {
		t.Fatal(err)
	}
	externalReliabilityStep(t, api, ctx)
	entry, record := externalReliabilityRecord(t, api, id, version)
	if entry.Status != "PROCESSED" {
		t.Fatalf("event failed: %+v %+v", entry, record)
	}
}

func TestExternalDataEventTypeUpdateRetainsAllRecoverableAlarms(t *testing.T) {
	api, repo, ctx := externalAPIFixture(t)
	ep := externalReliabilityDirectEndpoint(t, api, ctx)
	externalReliabilityDeliver(t, api, ctx, ep, "independent-event", "ACTIVE", "FIRE", 1)
	externalReliabilityDeliver(t, api, ctx, ep, "updated-event", "ACTIVE", "FIRE", 1)
	externalReliabilityDeliver(t, api, ctx, ep, "updated-event", "ACTIVE", "SMOKE", 2)
	externalReliabilityDeliver(t, api, ctx, ep, "updated-event", "RECOVERED", "SMOKE", 3)
	alarms, err := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: externalTestTenant, DeviceID: "ext-device", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	updated, independent := 0, 0
	for _, alarm := range alarms {
		switch alarm.Details["externalEventId"] {
		case "updated-event":
			updated++
			if alarm.Status != "RECOVERED" {
				t.Fatalf("event type update lost earlier recoverable alarm: type=%s status=%s", alarm.AlarmType, alarm.Status)
			}
		case "independent-event":
			independent++
			if alarm.Status != "ACTIVE" {
				t.Fatal("recovery affected another event")
			}
		}
	}
	if updated == 0 || independent != 1 {
		t.Fatalf("event alarms missing: updated=%d independent=%d", updated, independent)
	}
}

func TestExternalDataUpdatedRuleAlarmKeepsRuleActions(t *testing.T) {
	api, repo, ctx := externalAPIFixture(t)
	ep := externalReliabilityDirectEndpoint(t, api, ctx)
	rule := model.AlarmRule{ID: "external-rule", TenantID: externalTestTenant, Name: "外部火警联动", AlarmType: "FIRE", Level: "HIGH", Enabled: true, Conditions: []model.RuleCondition{{Field: "alarmType", Operator: "eq", Value: "FIRE"}}, Actions: []model.RuleAction{{Type: "OPEN_PAGE", Page: "alarms"}}}
	if err := repo.SaveRule(ctx, rule); err != nil {
		t.Fatal(err)
	}
	api.engine.RulesChanged(externalTestTenant)
	var actions atomic.Int64
	var reports atomic.Int64
	if err := api.engine.Bus.Subscribe(ctx, model.TopicUIAction, "external-reliability-actions", func(context.Context, []byte) error { actions.Add(1); return nil }); err != nil {
		t.Fatal(err)
	}
	if err := api.engine.Bus.Subscribe(ctx, model.TopicAlarmReported, "external-reliability-reports", func(_ context.Context, payload []byte) error {
		var alarm model.Alarm
		if err := json.Unmarshal(payload, &alarm); err != nil {
			return err
		}
		if alarm.Details["externalEventId"] == "event-with-action" {
			reports.Add(1)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	externalReliabilityDeliver(t, api, ctx, ep, "event-with-action", "ACTIVE", "FIRE", 1)
	if actions.Load() != 1 {
		t.Fatalf("initial rule did not execute: %d", actions.Load())
	}
	externalReliabilityDeliver(t, api, ctx, ep, "event-with-action", "ACTIVE", "FIRE", 2)
	if actions.Load() != 2 {
		t.Fatalf("new matching event version silently skipped rule actions: %d", actions.Load())
	}
	if reports.Load() != 2 {
		t.Fatalf("new matching event version skipped alarm report notification: %d", reports.Load())
	}
	if err := api.engine.DisableRule(ctx, externalTestTenant, rule.ID); err != nil {
		t.Fatal(err)
	}
	_, record := externalReliabilityRecord(t, api, "event-with-action", 2)
	alarm, err := repo.GetAlarm(ctx, externalTestTenant, record.AlarmID)
	if err != nil || alarm.Status != "RECOVERED" || alarm.TriggerCount != 2 {
		t.Fatalf("disabled rule left its external event active: %+v %v", alarm, err)
	}
}

func TestExternalDataComponentEventsKeepIndependentLifecycle(t *testing.T) {
	api, repo, ctx := externalAPIFixture(t)
	ep := externalReliabilityDirectEndpoint(t, api, ctx)
	ep.Mapping.Fields = append(ep.Mapping.Fields, externaldata.Field{Target: "data.components", Path: "components", Type: "json"})
	var err error
	ep, err = api.externalData.SaveEndpoint(ctx, externalTestTenant, ep)
	if err != nil {
		t.Fatal(err)
	}
	deliver := func(id string, version int64, status string, active bool) externaldata.Record {
		t.Helper()
		payload, _ := json.Marshal(map[string]any{"eventId": id, "camera": "vendor-camera", "time": time.Now().Add(-time.Minute).UnixMilli(), "version": version, "status": status, "type": "FIRE", "components": []any{map[string]any{"id": "sensor-one", "alarms": map[string]bool{"FIRE": active}}}})
		if _, err = api.externalData.Receive(ctx, externalTestTenant, ep, payload, ""); err != nil {
			t.Fatal(err)
		}
		externalReliabilityStep(t, api, ctx)
		entry, record := externalReliabilityRecord(t, api, id, version)
		if entry.Status != "PROCESSED" || record.AlarmID == "" {
			t.Fatalf("component event delivery: %+v %+v", entry, record)
		}
		return record
	}
	one := deliver("component-event-one", 1, "ACTIVE", true)
	two := deliver("component-event-two", 1, "ACTIVE", true)
	if one.AlarmID == two.AlarmID {
		t.Fatal("different external component events shared a lifecycle")
	}
	deliver("component-event-one", 2, "RECOVERED", true)
	alarm, _ := repo.GetAlarm(ctx, externalTestTenant, one.AlarmID)
	if alarm.Status != "RECOVERED" {
		t.Fatal("external recovery lost component alarm association")
	}
	alarm, _ = repo.GetAlarm(ctx, externalTestTenant, two.AlarmID)
	if alarm.Status != "ACTIVE" {
		t.Fatal("recovery affected a different component event")
	}
	if _, err = api.engine.SetAlarmStatus(ctx, externalTestTenant, two.AlarmID, "CLOSED", "root"); err != nil {
		t.Fatal(err)
	}
	deliver("component-event-two", 2, "ACTIVE", true)
	alarm, _ = repo.GetAlarm(ctx, externalTestTenant, two.AlarmID)
	if alarm.Status != "CLOSED" {
		t.Fatal("new component observation reopened a manually closed event")
	}
}

func TestExternalDataCurrentMappingRetryCanCorrectBusinessValidation(t *testing.T) {
	api, _, ctx := externalAPIFixture(t)
	ep := externalReliabilityDirectEndpoint(t, api, ctx)
	ep.MaxAttempts = 1
	for i := range ep.Mapping.Fields {
		if ep.Mapping.Fields[i].Target == "alarmType" {
			ep.Mapping.Fields[i].Path = "missingType"
			ep.Mapping.Fields[i].Default = nil
		}
	}
	var err error
	ep, err = api.externalData.SaveEndpoint(ctx, externalTestTenant, ep)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"eventId": "correct-mapping", "camera": "vendor-camera", "time": time.Now().Add(-time.Minute).UnixMilli(), "version": 1, "status": "ACTIVE", "type": "FIRE", "text": "修正告警类型映射"})
	if _, err = api.externalData.Receive(ctx, externalTestTenant, ep, payload, ""); err != nil {
		t.Fatal(err)
	}
	externalReliabilityStep(t, api, ctx)
	entry, record := externalReliabilityRecord(t, api, "correct-mapping", 1)
	if entry.Status != "FAILED" || record.MessageID != "" {
		t.Fatalf("validation failure fixture unexpectedly ingested raw: %+v %+v", entry, record)
	}
	for i := range ep.Mapping.Fields {
		if ep.Mapping.Fields[i].Target == "alarmType" {
			ep.Mapping.Fields[i].Path = "type"
		}
	}
	if _, err = api.externalData.SaveEndpoint(ctx, externalTestTenant, ep); err != nil {
		t.Fatal(err)
	}
	if _, err = api.externalData.Retry(ctx, externalTestTenant, "record", entry.ID, entry.Revision, true); err != nil {
		t.Fatal(err)
	}
	externalReliabilityStep(t, api, ctx)
	entry, record = externalReliabilityRecord(t, api, "correct-mapping", 1)
	if entry.Status != "PROCESSED" || record.AlarmID == "" {
		t.Fatalf("corrected mapping blocked by stale pending event hash: %+v %+v", entry, record)
	}
}

func TestExternalDataPropertiesKeepExistingRuleRecovery(t *testing.T) {
	api, repo, ctx := externalAPIFixture(t)
	source, err := api.externalData.SaveSource(ctx, externalTestTenant, externaldata.Source{Name: "属性平台", Username: "root", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	ep, err := api.externalData.SaveEndpoint(ctx, externalTestTenant, externaldata.Endpoint{Name: "温度", SourceID: source.ID, Enabled: true, Mode: "push", Kind: "property", Mapping: externaldata.Mapping{Fields: []externaldata.Field{{Target: "id", Path: "eventId"}, {Target: "timestamp", Path: "time", Type: "timestamp", TimeFormat: "milliseconds"}, {Target: "objectId", Constant: true, Value: "external-sensor"}, {Target: "data.temperature", Path: "temperature", Type: "number"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = api.externalData.SaveBinding(ctx, externalTestTenant, externaldata.Binding{SourceID: source.ID, Kind: "device", ExternalID: "external-sensor", TargetID: "ext-device"}); err != nil {
		t.Fatal(err)
	}
	rule := model.AlarmRule{ID: "external-property-rule", TenantID: externalTestTenant, Name: "温度", Enabled: true, AlarmType: "HIGH_TEMPERATURE", Level: "HIGH", Conditions: []model.RuleCondition{{Field: "temperature", Operator: ">", Value: 80}}, Recovery: []model.RuleCondition{{Field: "temperature", Operator: "<", Value: 60}}}
	if err = repo.SaveRule(ctx, rule); err != nil {
		t.Fatal(err)
	}
	api.engine.RulesChanged(externalTestTenant)
	for index, temp := range []int{100, 110, 40} {
		payload, _ := json.Marshal(map[string]any{"eventId": fmt.Sprintf("temperature-%d", index), "time": time.Now().Add(-time.Minute).UnixMilli() + int64(index), "temperature": temp})
		if _, err = api.externalData.Receive(ctx, externalTestTenant, ep, payload, ""); err != nil {
			t.Fatal(err)
		}
		externalReliabilityStep(t, api, ctx)
	}
	alarms, err := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: externalTestTenant, DeviceID: "ext-device", Limit: 100})
	if err != nil || len(alarms) != 1 || alarms[0].RuleID != rule.ID || alarms[0].Status != "RECOVERED" {
		t.Fatalf("external properties changed normal rule lifecycle: %+v %v", alarms, err)
	}
}
