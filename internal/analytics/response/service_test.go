package response

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/analytics"
	"iot-platform/internal/config"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func TestSelectedProductionReportReceiptUsesItsOwnArchiveIdentity(t *testing.T) {
	s, a := responseServiceFixture(t)
	repo := memory.NewRepository()
	s.Catalog = repo
	ctx := context.Background()
	for index := 1; index <= 2; index++ {
		m := model.StandardMessage{TenantID: "t1", DeviceID: "d1", ProductID: "p1", MessageID: fmt.Sprintf("standard-%d", index), RawMessageID: fmt.Sprintf("raw-%d", index), Timestamp: int64(3000 * index)}
		_, err := repo.SaveRawIndex(ctx, model.RawArchiveIndex{TenantID: "t1", DeviceID: "d1", ProductID: "p1", MessageID: m.RawMessageID, ReceivedAt: int64(2000 * index), ArchivedAt: int64(2000*index + 100)})
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = repo.UpsertAlarm(ctx, model.Alarm{ID: "real", TenantID: "t1", DeviceID: "d1", TriggerID: m.MessageID, AlarmType: "FAULT", Source: "device", Status: "ACTIVE", TriggerCount: 1, FirstTriggeredAt: m.Timestamp, LastTriggeredAt: m.Timestamp, Details: map[string]any{"message": m}})
		if err != nil {
			t.Fatal(err)
		}
	}
	var events []model.DutyBusinessEvent
	if err := repo.DutyRead(ctx, "t1", func(tx ports.DutyTx) error {
		var err error
		events, _, err = tx.Events(model.DutyFilter{DeviceIDs: []string{"d1"}, Limit: 100})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("report snapshots: %+v", events)
	}
	for _, event := range events {
		var alarm model.Alarm
		_ = json.Unmarshal(event.Body, &alarm)
		received, err := s.reportReceipt(ctx, a, model.ResponseEvidenceReference{Kind: "ALARM_REPORT", SourceID: event.ID, DeviceID: "d1", ResourceID: "real"})
		if err != nil {
			t.Fatal(err)
		}
		expected := int64(2000)
		if alarm.TriggerID == "standard-2" {
			expected = 4000
		}
		if received != expected {
			t.Fatalf("newer aggregate/event time replaced selected receive clock: %+v %d", event, received)
		}
	}
}

func TestResponseAttachmentsIdempotencyIntegrityAndCurrentPermission(t *testing.T) {
	s, a := responseServiceFixture(t)
	ctx := context.Background()
	responseExecution(t, s, a)
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.Archive = archive
	first, err := s.UploadAttachment(ctx, a, "drill", "file-request", "../arrival.txt", "text/plain", []byte("现场登记附件"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.UploadAttachment(ctx, a, "drill", "file-request", "../arrival.txt", "text/plain", []byte("现场登记附件"))
	if err != nil || first.ID != second.ID {
		t.Fatalf("durable retry: %+v %v", second, err)
	}
	if _, err = s.UploadAttachment(ctx, a, "drill", "file-request", "../arrival.txt", "text/plain", []byte("changed")); !errors.Is(err, model.ErrAnalysisConflict) {
		t.Fatalf("changed bytes accepted: %v", err)
	}
	ref, err := s.AttachmentEvidence(ctx, a, model.ResponseEvidenceReference{SourceID: first.ID, DeviceID: "d1"})
	if err != nil || ref.OccurredAt != 0 || ref.Classification != "UNCLASSIFIED" {
		t.Fatalf("attachment invented field time/source: %+v %v", ref, err)
	}
	metadata, reader, err := s.DownloadAttachment(ctx, a, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || string(data) != "现场登记附件" || metadata.Name != "arrival.txt" || metadata.ObjectKey != "" {
		t.Fatalf("download: %+v %q %v", metadata, data, err)
	}
	var internal model.ResponseAttachment
	_ = json.Unmarshal(first.Body, &internal)
	_, err = archive.PutObject(ctx, AttachmentBucket, internal.ObjectKey, strings.NewReader("tampered"), 8, "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AttachmentEvidence(ctx, a, model.ResponseEvidenceReference{SourceID: first.ID, DeviceID: "d1"}); !errors.Is(err, model.ErrAnalysisInvalid) {
		t.Fatalf("tampered evidence accepted: %v", err)
	}
	s.Analysis.Resolve = func(_ context.Context, original analytics.Actor) (analytics.Actor, error) {
		original.Permissions = []string{"menu:devices", "menu:response"}
		original.AllDevices = false
		original.DeviceIDs = []string{"other"}
		return original, nil
	}
	if _, _, err = s.DownloadAttachment(ctx, a, first.ID); !errors.Is(err, analytics.ErrForbidden) {
		t.Fatalf("revoked scope downloaded old attachment: %v", err)
	}
}

type responseCatalog struct{ alarms map[string]model.Alarm }

func (c responseCatalog) GetManagedDevice(_ context.Context, tenant, id string) (model.ManagedDevice, error) {
	if tenant != "t1" || id != "d1" {
		return model.ManagedDevice{}, model.ErrNotFound
	}
	return model.ManagedDevice{ID: id, TenantID: tenant}, nil
}
func (c responseCatalog) GetAlarm(_ context.Context, tenant, id string) (model.Alarm, error) {
	a, ok := c.alarms[id]
	if !ok || a.TenantID != tenant {
		return a, model.ErrNotFound
	}
	return a, nil
}
func responseServiceFixture(t *testing.T) (*Service, analytics.Actor) {
	t.Helper()
	actor := analytics.Actor{TenantID: "t1", Username: "admin", AllDevices: true, Permissions: []string{"*"}, AccessVersion: "v1"}
	store := analytics.NewMemoryStore()
	base := analytics.NewService(store, config.AnalyticsConfig{Poll: time.Millisecond, Lease: time.Second, BatchSize: 1}, func(_ context.Context, a analytics.Actor) (analytics.Actor, error) {
		if a.TenantID != actor.TenantID {
			return a, analytics.ErrForbidden
		}
		return actor, nil
	}, func(_ context.Context, tenant, id string) error {
		if tenant != "t1" || id != "d1" {
			return model.ErrNotFound
		}
		return nil
	})
	svc := NewService(base, nil)
	svc.Catalog = responseCatalog{alarms: map[string]model.Alarm{"real": {ID: "real", TenantID: "t1", DeviceID: "d1", Status: "ACTIVE", TriggerCount: 3}}}
	svc.Now = func() time.Time { return time.UnixMilli(100000) }
	svc.ValidateStaff = func(_ context.Context, tenant, user string, ids []string) error {
		if tenant == "t1" && (user == "admin" || user == "off-duty") && len(ids) == 1 && ids[0] == "d1" {
			return nil
		}
		return analytics.ErrForbidden
	}
	if err := svc.Register(); err != nil {
		t.Fatal(err)
	}
	return svc, actor
}
func responseRequest(id, key string, expected int64, body any) RevisionRequest {
	data, _ := json.Marshal(body)
	return RevisionRequest{ResourceID: id, ExpectedVersion: expected, DeviceIDs: []string{"d1"}, IdempotencyKey: key, Body: data}
}
func responseExecution(t *testing.T, s *Service, a analytics.Actor) model.AnalysisConfigRevision {
	t.Helper()
	return responseExecutionProcedure(t, s, a, procedureFixture())
}
func responseExecutionProcedure(t *testing.T, s *Service, a analytics.Actor, procedure model.ResponseProcedure) model.AnalysisConfigRevision {
	t.Helper()
	ctx := context.Background()
	p, err := s.SaveProcedure(ctx, a, responseRequest("procedure", "save", 0, procedure))
	if err != nil {
		t.Fatal(err)
	}
	p, err = s.PublishProcedure(ctx, a, "procedure", ActionRequest{ExpectedVersion: p.Version, IdempotencyKey: "publish"})
	if err != nil {
		t.Fatal(err)
	}
	x, err := s.CreateExecution(ctx, a, responseRequest("drill", "create", 0, PlanBody{Name: "隔离演练", ProcedureRevisionID: p.ID, People: []model.ResponsePerson{{Username: "off-duty", Role: "检查员"}}, PlannedStart: 90000, PlannedEnd: 120000}), "DRILL")
	if err != nil {
		t.Fatal(err)
	}
	x, err = s.ExecutionAction(ctx, a, "drill", ActionRequest{ExpectedVersion: x.Version, IdempotencyKey: "publish-plan", Action: "PUBLISH"})
	if err != nil {
		t.Fatal(err)
	}
	x, err = s.ExecutionAction(ctx, a, "drill", ActionRequest{ExpectedVersion: x.Version, IdempotencyKey: "start", Action: "START"})
	if err != nil {
		t.Fatal(err)
	}
	return x
}
func TestExerciseIsolationFixedProcedureAndDurableRequestReceipts(t *testing.T) {
	s, a := responseServiceFixture(t)
	ctx := context.Background()
	x := responseExecution(t, s, a)
	q := SimulationRequest{ExpectedVersion: x.Version, IdempotencyKey: "sim-1", DeviceID: "d1", OccurredAt: 100000, Content: "仅隔离记录的模拟烟感事件"}
	saved, err := s.AppendSimulation(ctx, a, "drill", q)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := s.AppendSimulation(ctx, a, "drill", q)
	if err != nil || retry.ID != saved.ID {
		t.Fatalf("request retry: %v %+v", err, retry)
	}
	q.Content = "same key changed body"
	if _, err = s.AppendSimulation(ctx, a, "drill", q); !errors.Is(err, model.ErrAnalysisConflict) {
		t.Fatalf("changed body accepted: %v", err)
	}
	real, err := s.Catalog.GetAlarm(ctx, a.TenantID, "real")
	if err != nil || real.Status != "ACTIVE" || real.TriggerCount != 3 {
		t.Fatalf("exercise mutated production: %+v %v", real, err)
	}
	var exec model.ResponseExecution
	_ = json.Unmarshal(saved.Body, &exec)
	if exec.ProcedureRevisionID == "" || len(exec.SimulationEvents) != 1 || exec.SimulationEvents[0].Source != "EXERCISE" {
		t.Fatalf("incorrect frozen procedure/source: %+v", exec)
	}
	other := a
	other.TenantID = "t2"
	if _, err = s.Latest(ctx, other, ExecutionKind, "drill"); err == nil {
		t.Fatal("cross-tenant execution read")
	}
}
func TestEvaluationConfirmationAndIndependentCorrectiveVerification(t *testing.T) {
	s, a := responseServiceFixture(t)
	ctx := context.Background()
	x := responseExecution(t, s, a)
	s.Now = func() time.Time { return time.UnixMilli(110000) }
	x, err := s.ExecutionAction(ctx, a, "drill", ActionRequest{ExpectedVersion: x.Version, IdempotencyKey: "end", Action: "END"})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := json.Marshal(EvaluationParameters{ExecutionRevisionID: x.ID})
	q := analytics.CreateRequest{DeviceIDs: []string{"d1"}, Start: 100000, End: 110000, Parameters: p, IdempotencyKey: "evaluation"}
	if err = s.ValidateCreate(ctx, a, &q); err != nil {
		t.Fatal(err)
	}
	run, err := s.Analysis.Create(ctx, a, analytics.KindResponse, AlgorithmVersion, q)
	if err != nil {
		t.Fatal(err)
	}
	workerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Analysis.RunWorkers(workerCtx, "response-test", slog.New(slog.NewTextHandler(io.Discard, nil)))
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		run, err = s.Analysis.Get(ctx, a, analytics.KindResponse, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if analytics.TerminalAnalysisStatus(run.Status) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if run.Status != model.AnalysisSucceeded {
		t.Fatalf("evaluation failed: %+v", run)
	}
	snapshot, err := s.Analysis.Snapshot(ctx, a, analytics.KindResponse, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	findings, total, err := s.Analysis.Outputs(ctx, a, analytics.KindResponse, run.ID, model.AnalysisFilter{Kind: "findings", Limit: 100})
	if err != nil || total != 2 {
		t.Fatalf("missing node findings: %+v %d %v", findings, total, err)
	}
	corrective := responseRequest("action", "create-action", 0, CorrectiveBody{Title: "补齐到场证据", ReviewRevisionID: run.ID, ReviewFactsHash: snapshot.FactsHash, FindingID: findings[0].ID, Owner: "off-duty", DueAt: 150000, RequiredVerification: []string{"现场验证"}})
	if _, err = s.CreateCorrective(ctx, a, corrective); err == nil {
		t.Fatal("unconfirmed AI/report created formal corrective")
	}
	x, err = s.ConfirmReview(ctx, a, "drill", run.ID, ConfirmRequest{ExpectedVersion: x.Version, ExpectedRunVersion: run.Version, IdempotencyKey: "confirm", FactsHash: snapshot.FactsHash, Conclusion: "仅记录缺项，不能据此判断现场未执行"})
	if err != nil {
		t.Fatal(err)
	}
	action, err := s.CreateCorrective(ctx, a, corrective)
	if err != nil {
		t.Fatal(err)
	}
	action, err = s.CorrectiveAction(ctx, a, "action", CorrectiveRequest{ExpectedVersion: action.Version, IdempotencyKey: "start-action", Action: "START", Explanation: "非当班人员处理"})
	if err != nil {
		t.Fatal(err)
	}
	evidence := []model.ResponseEvidenceReference{{Kind: "MANUAL_RECORD", DeviceID: "d1", Description: "实际检查记录"}}
	action, err = s.CorrectiveAction(ctx, a, "action", CorrectiveRequest{ExpectedVersion: action.Version, IdempotencyKey: "submit", Action: "SUBMIT_VERIFICATION", Explanation: "提交验证", Evidence: evidence})
	if err != nil {
		t.Fatal(err)
	}
	action, err = s.VerifyCorrective(ctx, a, "action", VerificationRequest{ExpectedVersion: action.Version, IdempotencyKey: "failed-check", Result: "FAILED", Explanation: "证据尚不符合要求"})
	if err != nil {
		t.Fatal(err)
	}
	var body model.CorrectiveAction
	_ = json.Unmarshal(action.Body, &body)
	if body.Status != "IN_PROGRESS" {
		t.Fatalf("failed verification closed action: %+v", body)
	}
	action, err = s.CorrectiveAction(ctx, a, "action", CorrectiveRequest{ExpectedVersion: action.Version, IdempotencyKey: "resubmit", Action: "SUBMIT_VERIFICATION", Explanation: "再次提交", Evidence: evidence})
	if err != nil {
		t.Fatal(err)
	}
	action, err = s.VerifyCorrective(ctx, a, "action", VerificationRequest{ExpectedVersion: action.Version, IdempotencyKey: "passed-check", Result: "PASSED", Items: []string{"现场验证"}, Explanation: "验收人员确认", Evidence: evidence})
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(action.Body, &body)
	if body.Status != "DONE" || len(body.Verifications) != 2 {
		t.Fatalf("independent verification missing: %+v", body)
	}
	if _, err = s.Latest(ctx, a, ExecutionKind, "drill"); err != nil {
		t.Fatal(err)
	}
}

func TestDraftPlanUpdatesFreezeAtPublicationAndReviewedSampleAssociationDoesNotRelabelProduction(t *testing.T) {
	s, a := responseServiceFixture(t)
	ctx := context.Background()
	body := procedureFixture()
	p, err := s.SaveProcedure(ctx, a, responseRequest("procedure-edit", "save-edit", 0, body))
	if err != nil {
		t.Fatal(err)
	}
	p, err = s.PublishProcedure(ctx, a, "procedure-edit", ActionRequest{ExpectedVersion: p.Version, IdempotencyKey: "publish-edit"})
	if err != nil {
		t.Fatal(err)
	}
	draft := PlanBody{Name: "原计划", ProcedureRevisionID: p.ID, People: []model.ResponsePerson{{Username: "off-duty", Role: "检查员"}}, PlannedStart: 90000, PlannedEnd: 120000}
	x, err := s.CreateExecution(ctx, a, responseRequest("drill-edit", "create-edit", 0, draft), "DRILL")
	if err != nil {
		t.Fatal(err)
	}
	draft.Name = "修订计划"
	draft.PlannedEnd = 130000
	q := responseRequest("drill-edit", "update-edit", x.Version, draft)
	x, err = s.UpdatePlan(ctx, a, "drill-edit", q)
	if err != nil {
		t.Fatal(err)
	}
	repeat, err := s.UpdatePlan(ctx, a, "drill-edit", q)
	if err != nil || repeat.ID != x.ID {
		t.Fatalf("draft retry %+v %v", repeat, err)
	}
	var fixed model.ResponseExecution
	_ = json.Unmarshal(x.Body, &fixed)
	if fixed.Name != draft.Name || fixed.PlannedEnd != draft.PlannedEnd {
		t.Fatal("plan not updated")
	}
	x, err = s.ExecutionAction(ctx, a, "drill-edit", ActionRequest{ExpectedVersion: x.Version, IdempotencyKey: "publish-plan", Action: "PUBLISH"})
	if err != nil {
		t.Fatal(err)
	}
	q.ExpectedVersion = x.Version
	q.IdempotencyKey = "too-late"
	if _, err = s.UpdatePlan(ctx, a, "drill-edit", q); !errors.Is(err, model.ErrAnalysisConflict) {
		t.Fatalf("published plan edited %v", err)
	}
	s.ResolveEvidence = func(_ context.Context, _ analytics.Actor, ref model.ResponseEvidenceReference) (model.ResponseEvidenceReference, error) {
		if ref.Kind != "ALARM_REPORT" || ref.SourceID != "single-report" || ref.DeviceID != "d1" {
			return ref, analytics.ErrForbidden
		}
		return model.ResponseEvidenceReference{ID: "canonical-report", Kind: ref.Kind, SourceID: ref.SourceID, DeviceID: ref.DeviceID, ResourceID: "real", Classification: "PRODUCTION", OccurredAt: 95000, RecordedAt: 96000, Description: "真实单次上报"}, nil
	}
	assoc := SampleAssociationRequest{ExpectedVersion: x.Version, IdempotencyKey: "test-association", SourceKind: "ALARM_REPORT", SourceID: "single-report", DeviceID: "d1", Classification: "TEST", Status: "CONFIRMED", Basis: "指定单次测试样本", Evidence: []model.ResponseEvidenceReference{{Kind: "MANUAL_RECORD", DeviceID: "d1", Description: "记录核对"}}}
	x, err = s.AssociateSample(ctx, a, "drill-edit", assoc)
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(x.Body, &fixed)
	got := fixed.SampleAssociations[0]
	if got.Classification != "TEST" || got.Source.Classification != "PRODUCTION" || got.Source.SourceID != "single-report" {
		t.Fatalf("production relabeled %+v", got)
	}
	alarm, err := s.Catalog.GetAlarm(ctx, "t1", "real")
	if err != nil || alarm.Status != "ACTIVE" || alarm.TriggerCount != 3 {
		t.Fatal("association mutated alarm")
	}
	assoc.ExpectedVersion = x.Version
	assoc.IdempotencyKey = "mixed-production"
	assoc.ContainsProduction = true
	x, err = s.AssociateSample(ctx, a, "drill-edit", assoc)
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(x.Body, &fixed)
	if fixed.SampleAssociations[1].Classification != "PRODUCTION" {
		t.Fatal("mixed real event excluded")
	}
	assoc.ExpectedVersion = x.Version
	assoc.IdempotencyKey = "whole-alarm"
	assoc.SourceKind = "ALARM"
	if _, err = s.AssociateSample(ctx, a, "drill-edit", assoc); !errors.Is(err, model.ErrAnalysisInvalid) {
		t.Fatalf("whole alarm test marker %v", err)
	}
}

func TestMilestoneCorrectionUsesItsOwnExactPermission(t *testing.T) {
	ctx := context.Background()
	s, a := responseServiceFixture(t)
	x := responseExecution(t, s, a)
	q := MilestoneRequest{ExpectedVersion: x.Version, IdempotencyKey: "manual", StepID: "ack", OccurredAt: 100000, Executor: "admin", Status: "UNVERIFIED", Explanation: "现场时钟待核对", Evidence: []model.ResponseEvidenceReference{{Kind: "MANUAL_RECORD", DeviceID: "d1", Description: "现场记录"}}}
	x, e := s.AppendMilestone(ctx, a, "drill", q)
	if e != nil {
		t.Fatal(e)
	}
	var body model.ResponseExecution
	_ = json.Unmarshal(x.Body, &body)
	a.Permissions = []string{"menu:devices", "menu:response", "POST /api/v1/response-runs/:id/corrections"}
	s.Analysis.Resolve = func(context.Context, analytics.Actor) (analytics.Actor, error) { return a, nil }
	q.ExpectedVersion, q.IdempotencyKey, q.CorrectsID = x.Version, "correction", body.Milestones[0].ID
	q.Status, q.Explanation = "CONFIRMED", "确认原登记的更正依据"
	corrected, e := s.AppendMilestone(ctx, a, "drill", q)
	if e != nil || corrected.Version != x.Version+1 {
		t.Fatal(corrected, e)
	}
	q.ExpectedVersion, q.IdempotencyKey, q.CorrectsID = corrected.Version, "new-manual", ""
	if _, e = s.AppendMilestone(ctx, a, "drill", q); !errors.Is(e, analytics.ErrForbidden) {
		t.Fatal("correction permission granted new milestone write", e)
	}
	a.Permissions = []string{"menu:devices", "menu:response", "POST /api/v1/response-runs/:id/milestones"}
	q.IdempotencyKey, q.CorrectsID = "another-correction", body.Milestones[0].ID
	if _, e = s.AppendMilestone(ctx, a, "drill", q); !errors.Is(e, analytics.ErrForbidden) {
		t.Fatal("milestone permission granted correction", e)
	}
}

func TestSystemMilestoneCannotBeConfirmedWithBrowserProcessingClock(t *testing.T) {
	s, a := responseServiceFixture(t)
	ctx := context.Background()
	p := procedureFixture()
	p.Steps[0].SystemEventType = "ALARM_ACKNOWLEDGED"
	x := responseExecutionProcedure(t, s, a, p)
	for _, status := range []string{"CONFIRMED", "UNVERIFIED", "DISPUTED"} {
		q := MilestoneRequest{ExpectedVersion: x.Version, IdempotencyKey: "browser-" + status, StepID: "ack", OccurredAt: 90000, Executor: "admin", Status: status, Explanation: "客户端不能代替真实确认事务时钟", Evidence: []model.ResponseEvidenceReference{{Kind: "MANUAL_RECORD", DeviceID: "d1", Description: "现场声明"}}}
		if _, e := s.AppendMilestone(ctx, a, "drill", q); !errors.Is(e, model.ErrAnalysisInvalid) {
			t.Fatal("browser inserted system milestone", status, e)
		}
	}
	unchanged, e := s.Latest(ctx, a, ExecutionKind, "drill")
	if e != nil || unchanged.ID != x.ID || unchanged.Version != x.Version {
		t.Fatal(unchanged, e)
	}
}
