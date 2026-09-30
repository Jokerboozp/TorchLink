package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/analytics"
	"iot-platform/internal/analytics/response"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
)

func TestResponseAPIExerciseProductionCoexistenceFixedReviewsAndRevocation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	repo := memory.NewRepository()
	for _, id := range []string{"d1", "hidden"} {
		if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "t", ID: id, ProductID: "p", Name: id, AccessKey: id}); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.SaveProduct(ctx, model.Product{TenantID: "t", ID: "p", Name: "隔离产品"}); err != nil {
		t.Fatal(err)
	}
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bus := local.NewBus()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(repo, archive, bus, local.NewRealtime(), parser.NewPlatformRegistry(t.TempDir()), log)

	cfg := config.Config{AdminUser: "admin", AdminPassword: "test-only", AdminTenants: []string{"t", "other"}, JWTSecret: "response-api-test-secret-32-characters", DevMode: true, Analytics: config.AnalyticsConfig{Poll: time.Millisecond}}
	api := New(cfg, engine, metrics.New(), log)
	api.SetAnalysisStorage(analytics.NewMemoryStore(), responseAPILedgerFacts{repo: repo, ctx: ctx})
	if err = engine.StartWith(ctx, core.Components{Processor: true}); err != nil {
		t.Fatal(err)
	}
	go api.RunAnalysisWorkers(ctx)
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	token, err := api.auth.Issue("admin", "t", "admin", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	req := func(method, path string, body any, status int) map[string]any {
		return requestJSON(t, server.Client(), method, server.URL+path, token, body, status)
	}
	procedure := model.ResponseProcedure{Name: "隔离处置流程", Scenario: "场景", Steps: []model.ResponseStep{{ID: "ack", Name: "平台确认", Role: "检查员", Required: true, RequiredEvidence: []string{"ALARM_LIFECYCLE"}, ClockStart: "PLATFORM_RECEIVED", SystemEventType: "ALARM_ACKNOWLEDGED"}, {ID: "arrival", Name: "现场到达", Role: "检查员", Required: true, RequiredEvidence: []string{"MANUAL_RECORD"}, ClockStart: "RUN_START"}}}
	body := func(id, key string, version any, b any) map[string]any {
		return map[string]any{"resourceId": id, "idempotencyKey": key, "expectedVersion": version, "deviceIds": []string{"d1"}, "body": b}
	}
	p := req("POST", "/api/v1/response-procedures", body("procedure", "save", 0, procedure), 201)
	p = req("POST", "/api/v1/response-procedures/procedure/publish", map[string]any{"expectedVersion": p["version"], "idempotencyKey": "publish"}, 201)
	now := time.Now().UnixMilli()
	plan := response.PlanBody{Name: "隔离演练", ProcedureRevisionID: p["id"].(string), People: []model.ResponsePerson{{Username: "admin", Role: "检查员"}}, PlannedStart: now, PlannedEnd: now + 60000}
	execution := req("POST", "/api/v1/drills", body("drill", "drill-create", 0, plan), 201)
	execution = req("POST", "/api/v1/drills/drill/actions", map[string]any{"expectedVersion": execution["version"], "idempotencyKey": "drill-publish", "action": "PUBLISH"}, 201)
	execution = req("POST", "/api/v1/drills/drill/actions", map[string]any{"expectedVersion": execution["version"], "idempotencyKey": "drill-start", "action": "START"}, 201)
	simulation := map[string]any{"expectedVersion": execution["version"], "idempotencyKey": "simulation", "deviceId": "d1", "occurredAt": time.Now().UnixMilli(), "content": "演练模拟事件"}
	execution = req("POST", "/api/v1/drills/drill/simulation-events", simulation, 201)
	if alarms, err := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "t"}); err != nil || len(alarms) != 0 {
		t.Fatal("simulation generated production alarm", alarms, err)
	}
	// Actual production processor runs concurrently with the isolated exercise.
	rawID := "raw-real-fire"
	if _, err = repo.SaveRawIndex(ctx, model.RawArchiveIndex{TenantID: "t", MessageID: rawID, DeviceID: "d1", ProductID: "p", ReceivedAt: time.Now().UnixMilli() - 10, ArchivedAt: time.Now().UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	message := model.StandardMessage{TenantID: "t", DeviceID: "d1", ProductID: "p", MessageID: "real-fire", RawMessageID: rawID, MessageType: model.AlarmReport, Timestamp: time.Now().UnixMilli(), Properties: map[string]any{"alarmType": "FIRE", "alarmLevel": "HIGH", "content": "演练同时发生的真实火警"}}
	payload, _ := json.Marshal(message)
	if err = bus.Publish(ctx, model.TopicDeviceBusiness, message.MessageID, payload); err != nil {
		t.Fatal(err)
	}
	alarms, err := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "t"})
	if err != nil || len(alarms) != 1 || alarms[0].Source != "device" || alarms[0].Status != "ACTIVE" {
		t.Fatalf("real alarm suppressed %+v %v", alarms, err)
	}
	realID := alarms[0].ID
	casePlan := response.PlanBody{Name: "真实案例", ProcedureRevisionID: p["id"].(string), AlarmID: realID, People: plan.People}
	real := req("POST", "/api/v1/response-cases", body("case", "case-create", 0, casePlan), 201)
	if alarm, err := repo.GetAlarm(ctx, "t", realID); err != nil || alarm.Status != "ACTIVE" {
		t.Fatal("case creation changed alarm")
	}
	_, err = engine.SetAlarmStatus(ctx, "t", realID, "ACKED", "admin")
	if err != nil {
		t.Fatal(err)
	}
	// The candidate is a committed alarm transaction, rather than a caller's
	// claimed timestamp. Only its defined system step can consume this evidence.
	candidates := req("GET", "/api/v1/response-runs/case/evidence-candidates?kind=ALARM_LIFECYCLE", nil, 200)
	var ackEvidence map[string]any
	for _, value := range candidates["items"].([]any) {
		v := value.(map[string]any)
		if v["eventType"] == "ALARM_ACKNOWLEDGED" {
			ackEvidence = v
		}
	}
	if ackEvidence == nil {
		t.Fatal("committed ACK missing from candidates", candidates)
	}
	req("POST", "/api/v1/response-runs/case/system-milestones", map[string]any{"expectedVersion": real["version"], "idempotencyKey": "cannot-call-arrival-ack", "stepId": "arrival", "sourceEventId": ackEvidence["sourceId"], "deviceId": "d1"}, 422)
	real = req("POST", "/api/v1/response-runs/case/system-milestones", map[string]any{"expectedVersion": real["version"], "idempotencyKey": "actual-ack", "stepId": "ack", "sourceEventId": ackEvidence["sourceId"], "deviceId": "d1"}, 201)
	milestones := real["body"].(map[string]any)["milestones"].([]any)
	if len(milestones) != 1 || milestones[0].(map[string]any)["stepId"] != "ack" || milestones[0].(map[string]any)["occurredAt"] != ackEvidence["occurredAt"] {
		t.Fatal("ACK was changed or invented arrival", milestones)
	}
	time.Sleep(5 * time.Millisecond)
	execution = req("POST", "/api/v1/drills/drill/actions", map[string]any{"expectedVersion": execution["version"], "idempotencyKey": "drill-end", "action": "END"}, 201)
	execBody := execution["body"].(map[string]any)
	q := map[string]any{"deviceIds": []string{"d1"}, "start": execBody["startedAt"], "end": execBody["endedAt"], "idempotencyKey": "evaluation", "parameters": map[string]any{"executionRevisionId": execution["id"]}}
	run := req("POST", "/api/v1/response-runs/drill/evaluations", q, 202)
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		run = req("GET", "/api/v1/response-evaluations/"+run["id"].(string), nil, 200)
		if run["status"] == "SUCCEEDED" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if run["status"] != "SUCCEEDED" {
		t.Fatal(run)
	}
	history := req("GET", "/api/v1/response-runs/drill/evaluations", nil, 200)
	if history["total"] != float64(1) || history["items"].([]any)[0].(map[string]any)["id"] != run["id"] {
		t.Fatal("fixed evaluation missing from execution history", history)
	}
	snapshot := req("GET", "/api/v1/response-evaluations/"+run["id"].(string)+"/snapshot", nil, 200)
	req("POST", "/api/v1/response-runs/drill/reviews/"+run["id"].(string)+"/confirm", map[string]any{"expectedVersion": execution["version"], "expectedRunVersion": run["version"], "idempotencyKey": "confirm", "factsHash": "forged", "conclusion": "人工结论"}, 409)
	req("POST", "/api/v1/response-runs/drill/reviews/"+run["id"].(string)+"/confirm", map[string]any{"expectedVersion": execution["version"], "expectedRunVersion": run["version"], "idempotencyKey": "confirm", "factsHash": snapshot["factsHash"], "conclusion": "缺少记录，不代表未采取现场动作"}, 201)
	state, err := repo.LoadAccessState(ctx, "t")
	if err != nil {
		t.Fatal(err)
	}
	state.Users = []model.PlatformUser{{Username: "reader", Enabled: true, SessionVersion: 1, DeviceScope: "selected", DeviceIDs: []string{"d1"}, Permissions: []string{"menu:devices", "menu:response"}}}
	if ok, err := repo.SaveAccessState(ctx, "t", state); err != nil || !ok {
		t.Fatal(ok, err)
	}
	reader, err := api.auth.IssueUser("reader", "t", 1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	// Owning an execution does not grant access to alarm or raw sources.
	requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/response-runs/case/evidence-candidates?kind=ALARM_LIFECYCLE", reader, nil, 403)
	requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/response-runs/case/evidence-candidates?kind=RAW_MESSAGE", reader, nil, 403)
	staff := requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/response-staff?deviceIds=d1", reader, nil, 200)
	if staff["total"] != float64(2) {
		t.Fatal("enabled authorized off-duty staff not selectable", staff)
	}
	for _, path := range []string{"/api/v1/drills/drill", "/api/v1/response-evaluations/" + run["id"].(string) + "/snapshot"} {
		requestJSON(t, server.Client(), "GET", server.URL+path, reader, nil, 200)
	}
	state, err = repo.LoadAccessState(ctx, "t")
	if err != nil {
		t.Fatal(err)
	}
	state.Users[0].DeviceIDs = []string{"hidden"}
	if ok, err := repo.SaveAccessState(ctx, "t", state); err != nil || !ok {
		t.Fatal(ok, err)
	}
	for _, path := range []string{"/api/v1/drills/drill", "/api/v1/response-evaluations/" + run["id"].(string) + "/snapshot"} {
		requestJSON(t, server.Client(), "GET", server.URL+path, reader, nil, 403)
	}
	page := requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/drills", reader, nil, 200)
	if page["total"] != float64(0) {
		t.Fatal("hidden drill count", page)
	}
	other, _ := api.auth.Issue("admin", "other", "admin", nil, time.Hour)
	requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/drills/drill", other, nil, 404)
}

// The API integration reads the events committed by the actual production
// processor's memory repository; it does not fabricate an ACK event or time.
type responseAPILedgerFacts struct {
	ports.AnalyticsFactReader
	repo   *memory.Repository
	ctx    context.Context
	tenant string
}

func (f responseAPILedgerFacts) AnalyticsFactsRead(ctx context.Context, tenant string, fn func(ports.AnalyticsFactReader) error) error {
	f.ctx = ctx
	f.tenant = tenant
	return fn(f)
}
func (f responseAPILedgerFacts) ListAlarmLifecycleEvents(q model.FactQuery) (model.FactPage[model.BusinessEventFact], error) {
	return f.events(q, false)
}
func (f responseAPILedgerFacts) ListAlarmReportEvents(q model.FactQuery) (model.FactPage[model.BusinessEventFact], error) {
	return f.events(q, true)
}
func (f responseAPILedgerFacts) events(q model.FactQuery, reports bool) (page model.FactPage[model.BusinessEventFact], err error) {
	err = f.repo.DutyRead(f.ctx, f.tenant, func(tx ports.DutyTx) error {
		events, _, e := tx.Events(model.DutyFilter{DeviceIDs: q.DeviceIDs, Limit: 100})
		if e != nil {
			return e
		}
		for _, event := range events {
			if event.OccurredAt < q.Start || event.OccurredAt >= q.End || reports && event.Type != "ALARM_CREATED" && event.Type != "ALARM_REPORTED" {
				continue
			}
			page.Items = append(page.Items, model.BusinessEventFact{SourceEventID: event.ID, ResourceID: event.ResourceID, DeviceID: event.DeviceID, Type: event.Type, OccurredAt: event.OccurredAt, RecordedAt: event.RecordedAt, Body: event.Body})
		}
		return nil
	})
	return
}
