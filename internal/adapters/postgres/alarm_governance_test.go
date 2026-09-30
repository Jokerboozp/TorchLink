package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"iot-platform/internal/alarmgovernance"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func TestGovernancePostgresCaseTransitionHistoryAndRollback(t *testing.T) {
	r := testRepository(t)
	ctx := context.Background()
	a := alarmgovernance.Actor{TenantID: "governance-t", Username: "owner", Permissions: []string{"*"}, AllDevices: true}
	s := alarmgovernance.New(r, func(context.Context, alarmgovernance.Actor) (alarmgovernance.Actor, error) { return a, nil }, nil, r)
	p := model.GovernancePoint{DeviceID: "d", AlarmType: "FIRE", OriginKind: "DEVICE_DIRECT", SignalKey: "device:FIRE"}
	raw, _ := json.Marshal(model.GovernanceCase{GovernancePoint: p, Title: "隔离PG核查事项", OwnerUserID: a.Username})
	created, err := s.Execute(ctx, a, alarmgovernance.Command{Kind: model.GovernanceCaseKind, Operation: "create", IdempotencyKey: "create", Body: raw})
	if err != nil {
		t.Fatal(err)
	}
	started, err := s.Execute(ctx, a, alarmgovernance.Command{Kind: created.Kind, ID: created.ID, Operation: "start", ExpectedVersion: created.Version, IdempotencyKey: "start", Body: []byte(`{}`)})
	if err != nil {
		t.Fatal("case transition failed", err)
	}
	if started.Status != "INVESTIGATING" || started.Version != 2 || started.CreatedBy != created.CreatedBy || started.CreatedAt != created.CreatedAt {
		t.Fatal("case transition corrupted envelope", started)
	}
	var history, events int
	if err = r.pool.QueryRow(ctx, `SELECT count(*) FROM alarm_governance_case_history WHERE tenant_id=$1 AND resource_id=$2`, a.TenantID, created.ID).Scan(&history); err != nil || history != 2 {
		t.Fatal("case history missing", history, err)
	}
	if err = r.pool.QueryRow(ctx, `SELECT count(*) FROM alarm_governance_event WHERE tenant_id=$1 AND case_id=$2`, a.TenantID, created.ID).Scan(&events); err != nil || events != 2 {
		t.Fatal("case audit missing", events, err)
	}
	err = r.GovernanceTransaction(ctx, a.TenantID, func(tx ports.AlarmGovernanceTx) error {
		d, e := tx.Get(created.Kind, created.ID)
		if e != nil {
			return e
		}
		d.Status = "IMPROVING"
		if _, e = tx.Put(d, d.Version); e != nil {
			return e
		}
		return errors.New("rollback synthetic transaction")
	})
	if err == nil {
		t.Fatal("failed transaction committed")
	}
	got, err := s.Get(ctx, a, created.Kind, created.ID)
	if err != nil || got.Status != started.Status || got.Version != started.Version {
		t.Fatal("rollback changed current resource", got, err)
	}
	if err = r.pool.QueryRow(ctx, `SELECT count(*) FROM alarm_governance_case_history WHERE tenant_id=$1 AND resource_id=$2`, a.TenantID, created.ID).Scan(&history); err != nil || history != 2 {
		t.Fatal("rollback leaked history", history, err)
	}
	sample := model.StandardMessage{TenantID: a.TenantID, DeviceID: p.DeviceID, MessageID: "source-sample", RawMessageID: "synthetic-raw", MessageType: model.PropertyReport, Timestamp: time.Now().UnixMilli() - 1000, Properties: map[string]any{"temperature": 27.0}}
	claim, err := r.ClaimStandardMessage(ctx, sample, "test", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.MarkStandardMessageProcessed(ctx, sample.TenantID, sample.MessageID, claim.Token); err != nil {
		t.Fatal(err)
	}
	cfg, _ := model.GovernanceBody[model.GovernanceConfiguration](alarmgovernance.Builtins()[0])
	cfg.Builtin = false
	cfg.ResourceID = "source-binding"
	cfg.DeviceIDs = []string{p.DeviceID}
	cfg.Fields = append(cfg.Fields, model.GovernanceField{ID: "temperatureHint", Label: "温度样本线索", Control: "number", SourceFieldPath: "properties.temperature", SourceDeviceID: sample.DeviceID, SourceMessageID: sample.MessageID, SourceType: "NUMBER"})
	raw, _ = json.Marshal(cfg)
	draft, err := s.Execute(ctx, a, alarmgovernance.Command{Kind: model.GovernanceTemplateKind, Operation: "create", IdempotencyKey: "config-draft", Body: raw})
	if err != nil {
		t.Fatal("bound configuration draft", err)
	}
	pub, err := s.Execute(ctx, a, alarmgovernance.Command{Kind: draft.Kind, ID: draft.ID, Operation: "publish", IdempotencyKey: "config-publish", ExpectedVersion: draft.Version, Body: []byte(`{}`)})
	if err != nil {
		t.Fatal("bound configuration publication", err)
	}
	pubBody, _ := model.GovernanceBody[model.GovernanceConfiguration](pub)
	bound := pubBody.Fields[len(pubBody.Fields)-1]
	if bound.SourceConfirmedBy != a.Username || bound.SourceConfirmedAt <= 0 || bound.SourceValueHash != model.GovernanceHash(27.0) {
		t.Fatal("PG source binding publication lost actual sample confirmation", bound)
	}
	if _, err = s.Execute(ctx, a, alarmgovernance.Command{Kind: pub.Kind, ID: pub.ID, Operation: "update", IdempotencyKey: "overwrite-published", ExpectedVersion: pub.Version, Body: raw}); !errors.Is(err, model.ErrGovernanceInvalid) {
		t.Fatal("PG published source semantics overwritten", err)
	}
}
