package alarmgovernance

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"sync"
	"testing"
	"time"
)

type fixture struct {
	ctx  context.Context
	repo *memory.Repository
	s    *Service
	a    Actor
	p    model.GovernancePoint
	obs  model.AlarmObservation
	now  int64
}

func setup(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{ctx: context.Background(), repo: memory.NewRepository(), a: Actor{TenantID: "tenant-a", Username: "owner", Permissions: []string{"*"}, AllDevices: true}, p: model.GovernancePoint{DeviceID: "device-a", AlarmType: "FIRE", OriginKind: "DEVICE_DIRECT", SignalKey: "device:FIRE"}, now: time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC).UnixMilli()}
	for _, id := range []string{"device-a", "device-b"} {
		if e := f.repo.SaveManagedDevice(f.ctx, model.ManagedDevice{TenantID: f.a.TenantID, ID: id, AccessKey: id}); e != nil {
			t.Fatal(e)
		}
	}
	f.s = New(f.repo, nil, func(ctx context.Context, tenant, id string) error {
		_, e := f.repo.GetManagedDevice(ctx, tenant, id)
		return e
	}, f.repo)
	f.s.Now = func() time.Time { return time.UnixMilli(f.now) }
	f.s.ValidateReview = func(context.Context, string, model.ObservationReview) error { return nil }
	o := model.AlarmObservation{TenantID: f.a.TenantID, DeviceID: f.p.DeviceID, AlarmType: f.p.AlarmType, OriginKind: f.p.OriginKind, SignalKey: f.p.SignalKey, SourceSystem: "DEVICE", SourceEventID: "first", EventIndex: "alarm", FactKind: "REPORT", EventAt: f.now - 1000, TimeQuality: "TRUSTED", ReceivedAt: f.now - 900, Payload: map[string]any{"alarm": true}}
	var e error
	f.obs, _, e = f.repo.SaveAlarmObservation(f.ctx, o)
	if e != nil {
		t.Fatal(e)
	}
	return f
}
func command(kind, id, round, op string, body any, version int64) Command {
	raw, _ := json.Marshal(body)
	return Command{Kind: kind, ID: id, RoundID: round, Operation: op, ExpectedVersion: version, IdempotencyKey: uuid.NewString(), Body: raw}
}
func (f *fixture) must(t *testing.T, c Command) model.GovernanceDocument {
	t.Helper()
	d, e := f.s.Execute(f.ctx, f.a, c)
	if e != nil {
		t.Fatal(e)
	}
	return d
}
func (f *fixture) newCase(t *testing.T) model.GovernanceDocument {
	return f.must(t, command(model.GovernanceCaseKind, "", "", "create", model.GovernanceCase{GovernancePoint: f.p, Title: "点位核查", OwnerUserID: f.a.Username, ObservationIDs: []string{f.obs.ID}}, 0))
}
func (f *fixture) newVerification(t *testing.T) model.GovernanceDocument {
	at := f.now - 100
	return f.must(t, command(model.GovernanceVerificationKind, "", "", "create", model.FieldVerification{GovernancePoint: f.p, ObservationIDs: []string{f.obs.ID}, VerificationMethod: "ON_SITE", VerifiedAt: &at, FieldResult: "NO_FIRE_OBSERVED", ActivityRelation: "CONFIRMED_UNRELATED", CheckScope: "明确查看点位及相邻区域", FieldValues: map[string]any{"facilityStatus": "NORMAL"}}, 0))
}

func TestConfigurationControlledSourceBinding(t *testing.T) {
	f := setup(t)
	sample := model.StandardMessage{TenantID: f.a.TenantID, DeviceID: f.p.DeviceID, MessageID: "successful-sample", RawMessageID: "raw-private", Timestamp: f.now - 1000, Properties: map[string]any{"temperature": 27.0, "state": "NORMAL", "unsafe.path": "never-offered", "nested": map[string]any{"x": 1}}, Event: map[string]any{"name": "COOKING"}, Raw: map[string]any{"secret": "not-a-directory-field"}}
	claim, e := f.repo.ClaimStandardMessage(f.ctx, sample, "test", time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.repo.MarkStandardMessageProcessed(f.ctx, sample.TenantID, sample.MessageID, claim.Token); e != nil {
		t.Fatal(e)
	}
	pending := sample
	pending.MessageID = "unprocessed-sample"
	pending.Properties = map[string]any{"pending": true}
	if e = f.repo.SaveStandardMessage(f.ctx, pending); e != nil {
		t.Fatal(e)
	}
	directory, e := f.s.SourceFields(f.ctx, f.a, ports.AlarmObservationFilter{DeviceID: f.p.DeviceID, Start: f.now - 2000, End: f.now})
	if e != nil || len(directory) != 3 {
		t.Fatal("directory admitted unprocessed, raw or non-scalar fields", directory, e)
	}
	for _, v := range directory {
		if v.MessageID != sample.MessageID || v.DeviceID != sample.DeviceID {
			t.Fatal("directory lost successful source identity", v)
		}
	}
	cfg, _ := model.GovernanceBody[model.GovernanceConfiguration](Builtins()[0])
	cfg.Builtin = false
	cfg.ResourceID = "controlled-source-template"
	cfg.DeviceIDs = []string{f.p.DeviceID}
	cfg.Fields = append(cfg.Fields, model.GovernanceField{ID: "temperatureHint", Label: "成功样本温度线索", Control: "number", SourceFieldPath: "properties.temperature", SourceDeviceID: sample.DeviceID, SourceMessageID: sample.MessageID, SourceType: "NUMBER", SourceConfirmedBy: "forged-client", SourceConfirmedAt: 1})
	draft := f.must(t, command(model.GovernanceTemplateKind, "", "", "create", cfg, 0))
	draftBody, _ := model.GovernanceBody[model.GovernanceConfiguration](draft)
	bound := draftBody.Fields[len(draftBody.Fields)-1]
	if bound.SourceConfirmedBy != "" || bound.SourceConfirmedAt != 0 || bound.SourceValueHash != "" {
		t.Fatal("draft inherited client confirmation", bound)
	}
	published := f.must(t, command(draft.Kind, draft.ID, "", "publish", map[string]any{}, draft.Version))
	publishedBody, _ := model.GovernanceBody[model.GovernanceConfiguration](published)
	bound = publishedBody.Fields[len(publishedBody.Fields)-1]
	if bound.SourceConfirmedBy != f.a.Username || bound.SourceConfirmedAt != f.now || bound.SourceValueHash != model.GovernanceHash(27.0) {
		t.Fatal("publication did not freeze actual scalar provenance", bound)
	}
	frozenHash := publishedBody.Hash
	publishedBody.Hash = ""
	if model.GovernanceHash(publishedBody) != frozenHash {
		t.Fatal("publication hash omitted signed binding")
	}
	publishedBody.ResourceID = "controlled-source-copy"
	copyDoc := f.must(t, command(draft.Kind, "", "", "create", publishedBody, 0))
	copyBody, _ := model.GovernanceBody[model.GovernanceConfiguration](copyDoc)
	if copyBody.Fields[len(copyBody.Fields)-1].SourceConfirmedBy != "" {
		t.Fatal("copied draft inherited publication confirmation")
	}
	for _, tc := range []struct {
		name   string
		change func(*model.GovernanceConfiguration)
		want   error
	}{
		{"unprocessed", func(c *model.GovernanceConfiguration) { c.Fields[len(c.Fields)-1].SourceMessageID = pending.MessageID }, model.ErrNotFound},
		{"wrong-device", func(c *model.GovernanceConfiguration) {
			c.Fields[len(c.Fields)-1].SourceDeviceID = "device-b"
			c.DeviceIDs = []string{"device-a", "device-b"}
		}, ErrForbidden},
		{"wrong-type", func(c *model.GovernanceConfiguration) { c.Fields[len(c.Fields)-1].SourceType = "STRING" }, model.ErrGovernanceInvalid},
		{"expression", func(c *model.GovernanceConfiguration) {
			c.Fields[len(c.Fields)-1].SourceFieldPath = "properties.temperature > 20"
		}, model.ErrGovernanceInvalid},
		{"protected-result", func(c *model.GovernanceConfiguration) {
			c.Fields[0].SourceFieldPath = "properties.state"
			c.Fields[0].SourceDeviceID = sample.DeviceID
			c.Fields[0].SourceMessageID = sample.MessageID
			c.Fields[0].SourceType = "STRING"
		}, model.ErrGovernanceInvalid},
		{"remove-dispute", func(c *model.GovernanceConfiguration) {
			c.Fields[0].Options = []model.GovernanceOption{{Code: "UNABLE_TO_DETERMINE", Label: "未知"}}
		}, model.ErrGovernanceInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(cfg)
			var changed model.GovernanceConfiguration
			_ = json.Unmarshal(raw, &changed)
			changed.ResourceID = "bad-" + tc.name
			tc.change(&changed)
			if _, e := f.s.Execute(f.ctx, f.a, command(draft.Kind, "", "", "create", changed, 0)); !errors.Is(e, tc.want) {
				t.Fatal("unsafe binding accepted", e)
			}
		})
	}
	if _, e := decodeConfiguration([]byte(`{"name":"unsafe","script":"fetch(url)"}`)); !errors.Is(e, model.ErrGovernanceInvalid) {
		t.Fatal("executable unknown configuration structure accepted", e)
	}
}

func TestRequiredMeasureCorrectionUsesEffectiveVersion(t *testing.T) {
	f := setup(t)
	c := f.newCase(t)
	cb, _ := model.GovernanceBody[model.GovernanceCase](c)
	m := model.ImprovementMeasure{Content: "原检查安排", OwnerUserID: f.a.Username, DueAt: f.now + 1000, Required: true, Basis: "现场安排"}
	first := f.must(t, command(model.GovernanceMeasureKind, "", cb.CurrentRoundID, "create", m, 0))
	m.Content = "更正后的实际检查安排"
	m.CorrectionReason = "原排查路径已核实不适用"
	corrected := f.must(t, command(first.Kind, first.ID, "", "corrections", m, 0))
	if e := f.repo.GovernanceRead(f.ctx, f.a.TenantID, func(tx ports.AlarmGovernanceTx) error { return f.s.measureGate(tx, c.ID, cb.CurrentRoundID) }); !errors.Is(e, model.ErrGovernanceInvalid) {
		t.Fatal("unimplemented effective correction bypassed gate", e)
	}
	f.must(t, command(corrected.Kind, corrected.ID, "", "implement", map[string]any{"implementation": "现场已按更正安排检查并记录"}, corrected.Version))
	if e := f.repo.GovernanceRead(f.ctx, f.a.TenantID, func(tx ports.AlarmGovernanceTx) error { return f.s.measureGate(tx, c.ID, cb.CurrentRoundID) }); e != nil {
		t.Fatal("superseded original measure blocked implemented correction", e)
	}
}
func TestBuiltinsReadOnlyAndIndependentVerificationCorrections(t *testing.T) {
	f := setup(t)
	d := f.newVerification(t)
	b, _ := model.GovernanceBody[model.FieldVerification](d)
	if b.TemplateHash == "" || b.TypeProfileHash == "" || b.RecordedAt == 0 || b.Status != "DRAFT" {
		t.Fatalf("unfrozen verification: %+v", b)
	}
	confirmed := f.must(t, command(d.Kind, d.ID, "", "confirm", map[string]any{}, d.Version))
	b, _ = model.GovernanceBody[model.FieldVerification](confirmed)
	if !b.Complete || b.Status != "CONFIRMED" {
		t.Fatalf("verification not complete: %+v", b)
	}
	if _, e := f.s.Execute(f.ctx, f.a, command(d.Kind, d.ID, "", "update", b, confirmed.Version)); !errors.Is(e, model.ErrGovernanceInvalid) {
		t.Fatalf("formal overwrite accepted: %v", e)
	}
	b.Description = "补充检查范围"
	b.CorrectionReason = "补记到场细节"
	correction := f.must(t, command(d.Kind, d.ID, "", "corrections", b, 0))
	if correction.ID == d.ID || correction.CorrectsID != d.ID {
		t.Fatal("correction overwrote original")
	}
	original, e := f.s.Get(f.ctx, f.a, d.Kind, d.ID)
	if e != nil || original.Version != confirmed.Version {
		t.Fatal("original changed")
	}
	items, _, e := f.s.List(f.ctx, f.a, model.GovernanceFilter{Kind: model.GovernanceSceneKind, Limit: 100})
	if e != nil || len(items) != 7 {
		t.Fatalf("scene presets %d %v", len(items), e)
	}
	builtin, _ := f.s.Get(f.ctx, f.a, model.GovernanceTemplateKind, "template-general-v1")
	if _, e = f.s.Execute(f.ctx, f.a, command(builtin.Kind, builtin.ID, "", "retire", map[string]any{}, builtin.Version)); !errors.Is(e, model.ErrGovernanceInvalid) {
		t.Fatalf("builtin mutated: %v", e)
	}
}
func TestIdempotencyConflictActivePointAndOptimisticVersion(t *testing.T) {
	f := setup(t)
	c := command(model.GovernanceCaseKind, "", "", "create", model.GovernanceCase{GovernancePoint: f.p, Title: "核查", OwnerUserID: f.a.Username}, 0)
	first := f.must(t, c)
	again := f.must(t, c)
	if first.ID != again.ID {
		t.Fatal("retry created duplicate")
	}
	c.Body = []byte(`{"title":"different","deviceId":"device-a"}`)
	if _, e := f.s.Execute(f.ctx, f.a, c); !errors.Is(e, model.ErrGovernanceConflict) {
		t.Fatalf("key body conflict absent: %v", e)
	}
	if _, e := f.s.Execute(f.ctx, f.a, command(model.GovernanceCaseKind, "", "", "create", model.GovernanceCase{GovernancePoint: f.p, Title: "duplicate", OwnerUserID: f.a.Username}, 0)); !errors.Is(e, model.ErrGovernanceConflict) {
		t.Fatalf("duplicate active point accepted: %v", e)
	}
	started := f.must(t, command(first.Kind, first.ID, "", "start", map[string]any{}, first.Version))
	if started.Version <= first.Version {
		t.Fatal("version did not advance")
	}
	if _, e := f.s.Execute(f.ctx, f.a, command(first.Kind, first.ID, "", "assign", map[string]any{"ownerUserId": "other"}, first.Version)); !errors.Is(e, model.ErrGovernanceConflict) {
		t.Fatalf("stale action accepted: %v", e)
	}
}
func TestScopeRequiresAllSharedActivityMembersAndSourceMenus(t *testing.T) {
	f := setup(t)
	start, end := f.now-3000, f.now-1000
	activity := f.must(t, command(model.GovernanceActivityKind, "", "", "create", model.FieldActivityRevision{Location: "作业区", DeviceIDs: []string{"device-a", "device-b"}, ActivityType: "COOKING", Unit: "一次供餐", Actual: true, StartAt: &start, EndAt: &end, TimeQuality: "TRUSTED"}, 0))
	limited := Actor{TenantID: f.a.TenantID, Username: "limited", Permissions: []string{"menu:alarmGovernance", "menu:devices", "menu:alarms", "action:alarmGovernance:record"}, DeviceIDs: []string{"device-a"}}
	if _, e := f.s.Get(f.ctx, limited, activity.Kind, activity.ID); !errors.Is(e, ErrForbidden) {
		t.Fatalf("partial member read accepted: %v", e)
	}
	items, total, e := f.s.List(f.ctx, limited, model.GovernanceFilter{Kind: activity.Kind})
	if e != nil || total != 0 || len(items) != 0 {
		t.Fatalf("hidden activity count leaked: %d %+v %v", total, items, e)
	}
	limited.Permissions = []string{"menu:alarmGovernance", "action:alarmGovernance:record"}
	if _, _, e = f.s.List(f.ctx, limited, model.GovernanceFilter{Kind: model.GovernanceVerificationKind}); !errors.Is(e, ErrForbidden) {
		t.Fatalf("source menus bypassed: %v", e)
	}
	limited.Permissions = []string{"menu:alarmGovernance", "menu:devices", "menu:alarms"}
	limited.DeviceIDs = nil
	items, total, e = f.s.List(f.ctx, limited, model.GovernanceFilter{Kind: model.GovernanceVerificationKind})
	if e != nil || total != 0 {
		t.Fatalf("empty scope broadened %v %d", e, total)
	}
}
func TestVerificationLinkCannotExpandSignalOrEvents(t *testing.T) {
	f := setup(t)
	c := f.newCase(t)
	cb, _ := model.GovernanceBody[model.GovernanceCase](c)
	v := f.newVerification(t)
	good := model.VerificationRoundLink{VerificationID: v.ID, VerificationVersion: v.Version, ObservationIDs: []string{f.obs.ID}, Reason: "本次同点位核实"}
	f.must(t, command(model.GovernanceVerificationLinkKind, "", cb.CurrentRoundID, "create", good, 0))
	o := f.obs
	o.ID = ""
	o.DeviceID = "device-b"
	o.SourceEventID = "other"
	o.SourceContentHash = ""
	other, _, e := f.repo.SaveAlarmObservation(f.ctx, o)
	if e != nil {
		t.Fatal(e)
	}
	good.ObservationIDs = append(good.ObservationIDs, other.ID)
	if _, e = f.s.Execute(f.ctx, f.a, command(model.GovernanceVerificationLinkKind, "", cb.CurrentRoundID, "create", good, 0)); !errors.Is(e, model.ErrGovernanceInvalid) {
		t.Fatalf("verification scope expanded: %v", e)
	}
	if _, e = f.s.Execute(f.ctx, f.a, command(model.GovernanceAlarmLinkKind, "", cb.CurrentRoundID, "create", model.GovernanceAlarmLink{ObservationIDs: []string{other.ID}, Reason: "wrong"}, 0)); !errors.Is(e, model.ErrGovernanceInvalid) {
		t.Fatalf("foreign point linked: %v", e)
	}
}
func TestUnknownValuesSaveButCannotSupportConfirmedCause(t *testing.T) {
	f := setup(t)
	c := f.newCase(t)
	cb, _ := model.GovernanceBody[model.GovernanceCase](c)
	v := f.newVerification(t)
	b, _ := model.GovernanceBody[model.FieldVerification](v)
	b.FieldResult = "UNABLE_TO_DETERMINE"
	v = f.must(t, command(v.Kind, v.ID, "", "update", b, v.Version))
	v = f.must(t, command(v.Kind, v.ID, "", "confirm", map[string]any{}, v.Version))
	vb, _ := model.GovernanceBody[model.FieldVerification](v)
	if vb.Complete {
		t.Fatal("unknown treated as complete")
	}
	cause := f.must(t, command(model.GovernanceCauseKind, "", cb.CurrentRoundID, "create", model.CauseAssessment{Cause: "蒸汽候选", ObservationIDs: []string{f.obs.ID}, VerificationIDs: []string{v.ID}, SupportEvidenceIDs: []string{v.ID}, ConfirmationBasis: "现场核实"}, 0))
	if _, e := f.s.Execute(f.ctx, f.a, command(cause.Kind, cause.ID, "", "confirm", map[string]any{}, cause.Version)); !errors.Is(e, model.ErrGovernanceInvalid) {
		t.Fatalf("unknown confirmation accepted: %v", e)
	}
}

func TestSupersededVerificationCannotConfirmNewCause(t *testing.T) {
	f := setup(t)
	c := f.newCase(t)
	cb, _ := model.GovernanceBody[model.GovernanceCase](c)
	v := f.newVerification(t)
	v = f.must(t, command(v.Kind, v.ID, "", "confirm", map[string]any{}, v.Version))
	body, _ := model.GovernanceBody[model.FieldVerification](v)
	body.FieldResult = "UNABLE_TO_DETERMINE"
	body.CorrectionReason = "复核发现原资料不足，撤回完整核实含义"
	correction := f.must(t, command(v.Kind, v.ID, "", "corrections", body, 0))
	f.must(t, command(correction.Kind, correction.ID, "", "confirm", map[string]any{}, correction.Version))
	cause := f.must(t, command(model.GovernanceCauseKind, "", cb.CurrentRoundID, "create", model.CauseAssessment{Cause: "蒸汽候选", ObservationIDs: []string{f.obs.ID}, VerificationIDs: []string{v.ID}, SupportEvidenceIDs: []string{v.ID}, ConfirmationBasis: "引用已更正的旧核实"}, 0))
	if _, e := f.s.Execute(f.ctx, f.a, command(cause.Kind, cause.ID, "", "confirm", map[string]any{}, cause.Version)); !errors.Is(e, model.ErrGovernanceInvalid) {
		t.Fatal("historical complete verification overrode confirmed correction", e)
	}
}
func TestConcurrentAppendsPreserveBothAndMeasureGate(t *testing.T) {
	f := setup(t)
	c := f.newCase(t)
	cb, _ := model.GovernanceBody[model.GovernanceCase](c)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := f.s.Execute(f.ctx, f.a, command(model.GovernanceAlarmLinkKind, "", cb.CurrentRoundID, "create", model.GovernanceAlarmLink{ObservationIDs: []string{f.obs.ID}, Reason: "独立新增关联"}, 0))
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	latest, _ := f.s.Get(f.ctx, f.a, c.Kind, c.ID)
	body, _ := model.GovernanceBody[model.GovernanceCase](latest)
	if body.DataRevision != cb.DataRevision+2 {
		t.Fatalf("lost append revision: %d", body.DataRevision)
	}
	f.must(t, command(model.GovernanceMeasureKind, "", cb.CurrentRoundID, "create", model.ImprovementMeasure{Content: "检查排风设施", OwnerUserID: f.a.Username, DueAt: f.now + 1000, Required: true, RequiresAcceptance: true, Basis: "现场排风资料与负责人安排"}, 0))
	latest, _ = f.s.Get(f.ctx, f.a, c.Kind, c.ID)
	if _, e := f.s.Execute(f.ctx, f.a, command(c.Kind, c.ID, "", "start-observation", map[string]any{}, latest.Version)); !errors.Is(e, model.ErrGovernanceInvalid) {
		t.Fatalf("pending measure bypassed: %v", e)
	}
}
func TestSourceVectorDetectsRelevantLateFactButNotOutsideWindow(t *testing.T) {
	f := setup(t)
	keys := []model.GovernanceSourceVersion{{DependencyKey: f.p.Key("OBSERVATION"), BucketStart: f.now / dayMillis * dayMillis}, {DependencyKey: f.p.Key("OBSERVATION"), BucketStart: -1}}
	var vector []model.GovernanceSourceVersion
	if e := f.repo.GovernanceTransaction(f.ctx, f.a.TenantID, func(tx ports.AlarmGovernanceTx) error { var e error; vector, e = tx.SourceVersions(keys); return e }); e != nil {
		t.Fatal(e)
	}
	o := f.obs
	o.ID = ""
	o.SourceEventID = "outside"
	o.EventAt = f.now + 2*dayMillis
	o.ReceivedAt = o.EventAt
	o.SourceContentHash = ""
	if _, _, e := f.repo.SaveAlarmObservation(f.ctx, o); e != nil {
		t.Fatal(e)
	}
	if e := f.repo.GovernanceTransaction(f.ctx, f.a.TenantID, func(tx ports.AlarmGovernanceTx) error { return checkSourceVersions(tx, vector) }); e != nil {
		t.Fatalf("outside fact invalidated window: %v", e)
	}
	o.ID = ""
	o.SourceEventID = "late"
	o.EventAt = f.now - 500
	o.ReceivedAt = o.EventAt
	o.SourceContentHash = ""
	if _, _, e := f.repo.SaveAlarmObservation(f.ctx, o); e != nil {
		t.Fatal(e)
	}
	if e := f.repo.GovernanceTransaction(f.ctx, f.a.TenantID, func(tx ports.AlarmGovernanceTx) error { return checkSourceVersions(tx, vector) }); !errors.Is(e, model.ErrGovernanceConflict) {
		t.Fatalf("late fact did not invalidate: %v", e)
	}
}

func TestVerificationTimeCorrectionInvalidatesOriginalAndNewBuckets(t *testing.T) {
	f := setup(t)
	v := f.newVerification(t)
	b, _ := model.GovernanceBody[model.FieldVerification](v)
	oldBucket := *b.VerifiedAt / dayMillis * dayMillis
	newTime := f.now - 2*dayMillis
	keys := []model.GovernanceSourceVersion{{DependencyKey: f.p.Key("VERIFICATION"), BucketStart: oldBucket}, {DependencyKey: f.p.Key("VERIFICATION"), BucketStart: newTime / dayMillis * dayMillis}}
	var before, after []model.GovernanceSourceVersion
	if e := f.repo.GovernanceRead(f.ctx, f.a.TenantID, func(tx ports.AlarmGovernanceTx) error { var e error; before, e = tx.SourceVersions(keys); return e }); e != nil {
		t.Fatal(e)
	}
	b.VerifiedAt = &newTime
	f.must(t, command(v.Kind, v.ID, "", "update", b, v.Version))
	if e := f.repo.GovernanceRead(f.ctx, f.a.TenantID, func(tx ports.AlarmGovernanceTx) error { var e error; after, e = tx.SourceVersions(keys); return e }); e != nil {
		t.Fatal(e)
	}
	for i := range before {
		if after[i].Generation != before[i].Generation+1 {
			t.Fatal("verification time move failed to invalidate both source intervals", before, after)
		}
	}
}
func TestTransactionRefreshRejectsRevocationBeforeWrite(t *testing.T) {
	f := setup(t)
	f.s.ResolveTx = func(ports.AlarmGovernanceTx, Actor) (Actor, error) { return Actor{}, ErrForbidden }
	_, e := f.s.Execute(f.ctx, f.a, command(model.GovernanceCaseKind, "", "", "create", model.GovernanceCase{GovernancePoint: f.p, Title: "revoked", OwnerUserID: f.a.Username}, 0))
	if !errors.Is(e, ErrForbidden) {
		t.Fatalf("revocation ignored: %v", e)
	}
	var total int
	_ = f.repo.GovernanceRead(f.ctx, f.a.TenantID, func(tx ports.AlarmGovernanceTx) error {
		_, total, _ = tx.List(model.GovernanceFilter{Kind: model.GovernanceCaseKind, AllDevices: true})
		return nil
	})
	if total != 0 {
		t.Fatal("revoked write committed")
	}
}
func TestFormalCompletionFreezesRoundAndReopenRequiresNewEvidence(t *testing.T) {
	f := setup(t)
	c := f.newCase(t)
	c = f.must(t, command(c.Kind, c.ID, "", "start", map[string]any{}, c.Version))
	cb, _ := model.GovernanceBody[model.GovernanceCase](c)
	roundID := cb.CurrentRoundID
	plan := f.must(t, command(model.GovernancePlanKind, "", roundID, "create", model.ObservationPlan{BeforeStart: f.now - 2*dayMillis, BeforeEnd: f.now - dayMillis, AfterStart: f.now - dayMillis, AfterEnd: f.now, BeforeConditions: "同点位、同运行条件", AfterConditions: "同点位、同运行条件", ActivityUnit: "一次供餐", ActivityType: "COOKING", CoverageRequirement: "FULL_DECLARED", TimeBasis: "EVENT_AT", ReviewerUserID: f.a.Username}, 0))
	plan = f.must(t, command(plan.Kind, plan.ID, "", "confirm", map[string]any{}, plan.Version))
	c, _ = f.s.Get(f.ctx, f.a, c.Kind, c.ID)
	c = f.must(t, command(c.Kind, c.ID, "", "start-observation", map[string]any{}, c.Version))
	cb, _ = model.GovernanceBody[model.GovernanceCase](c)
	var vector []model.GovernanceSourceVersion
	_ = f.repo.GovernanceTransaction(f.ctx, f.a.TenantID, func(tx ports.AlarmGovernanceTx) error {
		vector, _ = tx.SourceVersions([]model.GovernanceSourceVersion{{DependencyKey: f.p.Key("OBSERVATION"), BucketStart: f.now / dayMillis * dayMillis}})
		return nil
	})
	reviewBody := model.ObservationReview{PlanID: plan.ID, PlanVersion: plan.Version, AnalysisSnapshotID: "fixed-snapshot", FactsHash: "fixed-hash", DataRevision: cb.DataRevision, SourceRevisionVector: vector, Conclusion: "INSUFFICIENT_DATA", Limitations: []string{"监测覆盖不足，不判断改善"}, Followup: "继续登记并补足观察证据", FollowupOwnerUserID: f.a.Username}
	f.s.ValidateReview = func(_ context.Context, _ string, r model.ObservationReview) error {
		if r.RoundID != roundID || r.PlanID != plan.ID {
			t.Fatalf("route round omitted before fixed snapshot validation: %+v", r)
		}
		return nil
	}
	review := f.must(t, command(model.GovernanceReviewKind, "", roundID, "create", reviewBody, 0))
	review = f.must(t, command(review.Kind, review.ID, "", "confirm", map[string]any{}, review.Version))
	c, _ = f.s.Get(f.ctx, f.a, c.Kind, c.ID)
	completed := f.must(t, command(c.Kind, c.ID, "", "complete", map[string]any{"reviewId": review.ID, "dataRevision": cb.DataRevision, "analysisSnapshotId": "fixed-snapshot", "factsHash": "fixed-hash", "reason": "本轮证据不足，已明确持续核查责任"}, c.Version))
	roundDoc, e := f.s.Get(f.ctx, f.a, model.GovernanceRoundKind, roundID)
	if e != nil {
		t.Fatal(e)
	}
	round, _ := model.GovernanceBody[model.GovernanceRound](roundDoc)
	if round.Status != "COMPLETED" || round.ReportID == "" {
		t.Fatalf("round not frozen: %+v", round)
	}
	reportDoc, e := f.s.Get(f.ctx, f.a, model.GovernanceReportKind, round.ReportID)
	if e != nil {
		t.Fatal(e)
	}
	report, _ := model.GovernanceBody[model.GovernanceReport](reportDoc)
	if report.Conclusion != "INSUFFICIENT_DATA" || report.FactsHash != "fixed-hash" {
		t.Fatal("formal conclusion changed")
	}
	independent := f.newVerification(t)
	if _, e = f.s.Execute(f.ctx, f.a, command(model.GovernanceVerificationLinkKind, "", roundID, "create", model.VerificationRoundLink{VerificationID: independent.ID, VerificationVersion: independent.Version, ObservationIDs: []string{f.obs.ID}, Reason: "结束后登记"}, 0)); !errors.Is(e, model.ErrGovernanceInvalid) {
		t.Fatalf("completed round accepted link: %v", e)
	}
	if _, e = f.s.Execute(f.ctx, f.a, command(completed.Kind, completed.ID, "", "reopen", map[string]any{"observationIds": []string{f.obs.ID}, "reason": "旧事件不能充当新增复发"}, completed.Version)); !errors.Is(e, model.ErrGovernanceInvalid) {
		t.Fatalf("old evidence reopened: %v", e)
	}
	o := f.obs
	o.ID = ""
	o.SourceEventID = "recurrence"
	o.SourceContentHash = ""
	o.EventAt = f.now + 1000
	o.ReceivedAt = o.EventAt
	newObservation, _, e := f.repo.SaveAlarmObservation(f.ctx, o)
	if e != nil {
		t.Fatal(e)
	}
	reopened := f.must(t, command(completed.Kind, completed.ID, "", "reopen", map[string]any{"observationIds": []string{newObservation.ID}, "reason": "人工核对新增上报"}, completed.Version))
	newCase, _ := model.GovernanceBody[model.GovernanceCase](reopened)
	if newCase.CurrentRoundID == roundID || newCase.Status != "PENDING_INVESTIGATION" {
		t.Fatal("reopen reused old round")
	}
	oldReport, _ := f.s.Get(f.ctx, f.a, reportDoc.Kind, reportDoc.ID)
	if string(oldReport.Body) != string(reportDoc.Body) {
		t.Fatal("reopen overwrote prior report")
	}
}
