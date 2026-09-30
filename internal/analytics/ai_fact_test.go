package analytics

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"iot-platform/internal/model"
)

func TestExactSnapshotFactUsesRegisteredFixedCollectionsAndViewerAuthorization(t *testing.T) {
	_, service, actor, run := aiFixtureKind(t, KindRecurring)
	current := actor
	current.Permissions = []string{"menu:devices", "menu:alarms", "menu:alarmGovernance"}
	service.Facts.Resolve = func(context.Context, Actor) (Actor, error) { return current, nil }
	ctx := context.Background()
	for _, id := range []string{"fact00", "fact31", "proof", "snapshot/summary"} {
		fact, err := service.Fact(ctx, actor, KindRecurring, run.ID, id)
		if err != nil || fact.ID != id || fact.SnapshotID != run.SnapshotID || fact.FactsHash == "" {
			t.Fatal(fact, err)
		}
		if id == "snapshot/summary" {
			var summary model.AnalysisAIFacts
			if json.Unmarshal(fact.Summary, &summary) != nil || string(summary.Statistics) != `{"missing":4,"unknown":2}` || summary.SummaryFactID != id {
				t.Fatal("summary changed fixed statistics", fact)
			}
		}
	}
	for _, id := range []string{"raw1", "missing", "snapshot/summary/extra"} {
		if _, err := service.Fact(ctx, actor, KindRecurring, run.ID, id); !errors.Is(err, model.ErrNotFound) {
			t.Fatal("source alias or guessed ID resolved", id, err)
		}
	}
	current.AllDevices, current.DeviceIDs = false, []string{"d1"}
	if _, err := service.Fact(ctx, actor, KindRecurring, run.ID, "fact00"); !errors.Is(err, ErrForbidden) {
		t.Fatal("shared source was trimmed to one member", err)
	}
	current = actor
	calls := 0
	service.Facts.AuthorizeSources = func(context.Context, Actor, model.AnalysisRun) error {
		calls++
		if calls > 1 {
			return ErrForbidden
		}
		return nil
	}
	if _, err := service.Fact(ctx, actor, KindRecurring, run.ID, "fact00"); !errors.Is(err, ErrForbidden) || calls != 2 {
		t.Fatal("provenance revocation during read bypassed", calls, err)
	}
}

func TestExactSnapshotFactDoesNotExposeInternalManifest(t *testing.T) {
	_, service, actor, run := aiFixtureKind(t, KindMonitoring)
	if _, err := service.Fact(context.Background(), actor, KindMonitoring, run.ID, "private"); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("private fixed inputs exposed as model fact", err)
	}
}
