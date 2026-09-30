package analytics

import (
	"context"
	"encoding/json"
	"errors"
	"iot-platform/internal/model"
	"strings"
	"testing"
)

func TestRecurringAIWorkflowReferencesAndSchema(t *testing.T) {
	cover := model.AnalysisAICoverage{SummaryProvided: true, OutputCount: 1, TotalOutputs: 3}
	valid := `{"summary":"candidate only","facts":[{"text":"reference","factIds":["s/summary"],"metricRefs":["metric"]}],"patterns":[],"hypotheses":[],"checks":[],"measures":[],"observation":[],"limitations":[]}`
	result, err := DecodeAIWorkflowResult(WorkflowRecurring, valid, []string{"s/summary", "metric"}, []string{"d"}, cover)
	if err != nil || len(result.Facts) != 1 || result.Summary == "candidate only" {
		t.Fatal(result, err)
	}
	for _, bad := range []string{strings.Replace(valid, `"metric"`, `"unread"`, 1), strings.Replace(valid, `"patterns":[]`, `"patterns":[],"alarmState":"CLOSED"`, 1), strings.Replace(valid, `"factIds":["s/summary"]`, `"factIds":["unread"]`, 1), strings.Replace(valid, `"metricRefs":["metric"]`, `"metricRefs":["metric"],"value":0`, 1), strings.Replace(valid, `"checks":[]`, `"checks":null`, 1), strings.Replace(valid, `"reference"`, `"已改善99%"`, 1)} {
		if _, err := DecodeAIWorkflowResult(WorkflowRecurring, bad, []string{"s/summary", "metric"}, []string{"d"}, cover); err == nil {
			t.Fatal("accepted invalid output", bad)
		}
	}
	if _, err := DecodeAIWorkflowResult(WorkflowDataQuality, valid, []string{"s/summary", "metric"}, []string{"d"}, cover); err == nil {
		t.Fatal("governance schema leaked into old workflow")
	}
	d, ok := WorkflowDefinitionFor(KindRecurring)
	if !ok || d.Menu != "alarmGovernance" || Menu(KindRecurring) != d.Menu || Prefix(KindRecurring) != d.Prefix || !IsAnalysisWorkflow(d.WorkflowID) {
		t.Fatal(d, ok)
	}
	a := Actor{TenantID: "t", Username: "u", DeviceIDs: []string{"d"}, Permissions: []string{"menu:devices", "menu:alarmGovernance"}}
	if a.Allows(KindRecurring, "", []string{"d"}) {
		t.Fatal("missing source alarm permission allowed")
	}
	a.Permissions = append(a.Permissions, "menu:alarms")
	if !a.Allows(KindRecurring, "", []string{"d"}) || a.Allows(KindRecurring, "", []string{"hidden"}) {
		t.Fatal("scope gate")
	}
}

func TestRecurringAIBuildInputAndAllRegisteredCollections(t *testing.T) {
	store, service, actor, run := aiFixtureKind(t, KindRecurring)
	createAI(t, service, actor, run, "recurring-build")
	job := claimAI(t, store, service)
	input, e := service.BuildInput(context.Background(), job)
	if e != nil || !input.Coverage.SummaryProvided || input.Coverage.TotalOutputs != 35 {
		t.Fatalf("recurring fixed facts cannot enter workflow: %v", e)
	}
	definition, _ := WorkflowDefinitionFor(KindRecurring)
	for _, collection := range definition.Collections {
		if _, e = service.ReadBoundAnalysisFacts(context.Background(), AIIdentity(job), collection, 10, 0); e != nil {
			t.Fatalf("registered collection %s unavailable: %v", collection, e)
		}
	}
}

func TestHistoricalDataProjectionCannotBeSentAsGovernanceAIWorkflow(t *testing.T) {
	_, service, actor, run := aiFixtureKind(t, KindRecurring, json.RawMessage(`{"jobMode":"HISTORICAL_PROJECTION"}`))
	_, e := service.Create(context.Background(), actor, KindRecurring, run.ID, CreateAIRequest{ExpectedVersion: run.Version, IdempotencyKey: "projection-ai"})
	if !errors.Is(e, model.ErrAnalysisInvalid) {
		t.Fatalf("historical data job sent for governance interpretation: %v", e)
	}
}
