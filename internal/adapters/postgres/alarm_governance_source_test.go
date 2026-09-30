package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"iot-platform/internal/alarmgovernance"
	"iot-platform/internal/analytics/recurring"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func TestGovernancePostgresClickHouseRealSourceIdentityAndReadOnlyHistory(t *testing.T) {
	repo := testRepository(t)
	ch := monitoringClickHouseFixture(t, repo)
	ctx := context.Background()
	now := time.Now().UnixMilli()
	raw := model.RawMessage{TenantID: "governance-source", DeviceID: "d", ProductID: "p", MessageID: "raw-many-standard-events", ReceivedAt: now - 10, ProtocolVersion: "fixture-v1", Payload: []byte(`{"events":["a","b"]}`)}
	index := model.RawArchiveIndex{TenantID: raw.TenantID, DeviceID: raw.DeviceID, ProductID: raw.ProductID, MessageID: raw.MessageID, ReceivedAt: raw.ReceivedAt, ObjectBucket: "clickhouse", ObjectKey: raw.MessageID}
	if _, err := repo.SaveRawIndex(ctx, index); err != nil {
		t.Fatal(err)
	}
	if err := ch.SaveRawMessage(ctx, raw); err != nil {
		t.Fatal("existing VM ClickHouse raw write", err)
	}
	for _, id := range []string{"standard-a", "standard-b", "unprocessed", "other-device", "other-tenant"} {
		msg := model.StandardMessage{TenantID: raw.TenantID, DeviceID: raw.DeviceID, ProductID: raw.ProductID, MessageID: id, RawMessageID: raw.MessageID, MessageType: model.AlarmReport, Timestamp: now - 1000, Properties: map[string]any{"pressure": 106.0}, Event: map[string]any{"components": []model.ComponentStatus{{ID: "loop-1", Timestamp: now - 1000, Alarms: map[string]bool{"FIRE": true, "FAULT": false}}}}, Tags: map[string]string{"eventTimeQuality": "TRUSTED"}, ParserVersion: "fixture-v1"}
		if id == "other-device" {
			msg.DeviceID = "other"
		}
		if id == "other-tenant" {
			msg.TenantID = "other"
		}
		claim, err := ch.ClaimStandardMessage(ctx, msg, "source-fixture", time.Minute)
		if err != nil || !claim.ShouldProcess {
			t.Fatal(claim, err)
		}
		if id != "unprocessed" {
			if err = ch.MarkStandardMessageProcessed(ctx, msg.TenantID, msg.MessageID, claim.Token); err != nil {
				t.Fatal(err)
			}
		}
	}
	facts := []model.AlarmObservation{}
	if err := repo.GovernanceRead(ctx, raw.TenantID, func(tx ports.AlarmGovernanceTx) error {
		reader := tx.(ports.GovernanceHistoricalReader)
		filter := ports.AlarmObservationFilter{DeviceIDs: []string{raw.DeviceID}, Start: now - 2000, End: now, Limit: 1}
		for _, expected := range []string{"standard-a", "standard-b"} {
			page, err := reader.ListGovernanceHistoricalMessages(filter)
			if err != nil || len(page) != 1 || page[0].MessageID != expected {
				return fmt.Errorf("processed historical scope/cursor lost: %#v %v", page, err)
			}
			parsed, err := recurring.NormalizeHistoricalMessage(page[0], &index, now)
			if err != nil || len(parsed.Observations) != 2 || parsed.Quality != "PARTIAL" {
				return fmt.Errorf("read-only component normalization: %#v %v", parsed, err)
			}
			for _, o := range parsed.Observations {
				if o.StandardMessageID != expected || o.RawMessageID != raw.MessageID || o.ReceivedAt != raw.ReceivedAt || o.Acceptance != "HISTORICAL_UNRESOLVED" || o.SourceSystem != "HISTORICAL_STANDARD_MESSAGE" {
					return fmt.Errorf("source identity or historical quality fabricated: %#v", o)
				}
				facts = append(facts, o)
			}
			filter.Cursor = fmt.Sprintf("%d:%s", page[0].Timestamp, page[0].MessageID)
		}
		page, err := reader.ListGovernanceHistoricalMessages(filter)
		if err != nil || len(page) != 0 {
			return fmt.Errorf("unprocessed/foreign historical input exposed: %#v %v", page, err)
		}
		filter.DeviceIDs = []string{}
		if _, err = reader.ListGovernanceHistoricalMessages(filter); err == nil {
			return fmt.Errorf("empty source scope was accepted")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(facts) != 4 || facts[0].ID == facts[2].ID {
		t.Fatal("one raw frame collapsed distinct standard events", facts)
	}
	if err := repo.GovernanceRead(ctx, raw.TenantID, func(tx ports.AlarmGovernanceTx) error {
		reader := tx.(ports.GovernanceHistoricalReader)
		msg, err := reader.GetGovernanceHistoricalMessage("standard-a")
		if err != nil || msg.MessageID != "standard-a" || msg.RawMessageID != raw.MessageID {
			return fmt.Errorf("exact standard-source lookup confused raw identity: %#v %v", msg, err)
		}
		for _, id := range []string{"unprocessed", "other-tenant"} {
			if _, err := reader.GetGovernanceHistoricalMessage(id); err == nil {
				return fmt.Errorf("exact lookup admitted pending/foreign source %s", id)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	svc := alarmgovernance.New(repo, nil, nil, repo)
	a := alarmgovernance.Actor{TenantID: raw.TenantID, Username: "source-observer", DeviceIDs: []string{raw.DeviceID}, Permissions: []string{"menu:alarmGovernance", "menu:devices", "menu:alarms", "action:alarmGovernance:templates"}}
	directory, err := svc.SourceFields(ctx, a, ports.AlarmObservationFilter{DeviceID: raw.DeviceID, Start: now - 2000, End: now})
	if err != nil || len(directory) != 1 || directory[0].SourceFieldPath != "properties.pressure" || directory[0].SourceType != "NUMBER" || directory[0].MessageID != "standard-b" {
		t.Fatal("real source-field directory lost scalar provenance or included unprocessed source", directory, err)
	}
	if _, err = svc.SourceFields(ctx, a, ports.AlarmObservationFilter{DeviceID: "other", Start: now - 2000, End: now}); err == nil {
		t.Fatal("source-field directory crossed current device scope")
	}
	storedRaw, err := ch.GetDeviceRawMessage(ctx, raw.TenantID, raw.DeviceID, raw.MessageID)
	if err != nil || storedRaw.MessageID != raw.MessageID || storedRaw.ReceivedAt != raw.ReceivedAt || string(storedRaw.Payload) != string(raw.Payload) {
		t.Fatal("real original-source navigation failed", storedRaw, err)
	}
	if _, err = ch.GetDeviceRawMessage(ctx, raw.TenantID, "other", raw.MessageID); err == nil {
		t.Fatal("raw lookup crossed device scope")
	}
	if _, err = ch.GetDeviceRawMessage(ctx, "other", raw.DeviceID, raw.MessageID); err == nil {
		t.Fatal("raw lookup crossed tenant")
	}
	telemetry, err := ch.PropertyHistory(ctx, raw.TenantID, raw.DeviceID, "pressure", now-2000, now, 10)
	if err != nil || len(telemetry) != 3 {
		t.Fatal("ClickHouse source fixture missing actual pending telemetry", telemetry, err)
	}
	// Reading/normalizing history cannot reserve live ingestion slots or create
	// production alarms; the presence of CH telemetry is not processed evidence.
	observations, err := repo.ListAlarmObservations(ctx, raw.TenantID, ports.AlarmObservationFilter{DeviceIDs: []string{raw.DeviceID}})
	if err != nil || len(observations) != 0 {
		t.Fatal("history query mutated observation source", observations, err)
	}
	alarms, err := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: raw.TenantID})
	if err != nil || len(alarms) != 0 {
		t.Fatal("history query mutated production alarms", alarms, err)
	}
}
