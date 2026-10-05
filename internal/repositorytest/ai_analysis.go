package repositorytest

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// AIAnalysisOutcomes checks the counts of verified alarms by AI risk level,
// verification result and prompt version, with the alarm filters applied.
func AIAnalysisOutcomes(t *testing.T, repo ports.Repository) {
	t.Helper()
	ctx := context.Background()
	fixtures := []struct {
		device, risk, result, version string
	}{
		{"d1", "HIGH", model.DispositionRealFire, "v2"},
		{"d1", "HIGH", model.DispositionRealFire, "v2"},
		{"d1", "LOW", model.DispositionFalseAlarm, "v1"},
		{"d2", "", model.DispositionTest, ""},
		{"d2", "HIGH", model.DispositionFalseAlarm, "v2"},
	}
	for i, f := range fixtures {
		a := model.Alarm{ID: fmt.Sprintf("ai-%d", i), TenantID: "ai-stats", RuleID: fmt.Sprintf("r%d", i), DeviceID: f.device, AlarmType: "FIRE", AlarmLevel: "HIGH", Status: "CLOSED", FirstTriggeredAt: 1000, LastTriggeredAt: int64(1000 + i),
			Disposition: &model.AlarmDisposition{Result: f.result, Handler: "h", VerifiedAt: 2000, AIRiskLevel: f.risk, AIPromptVersion: f.version}}
		if _, _, err := repo.UpsertAlarm(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	unverified := model.Alarm{ID: "ai-open", TenantID: "ai-stats", RuleID: "r-open", DeviceID: "d1", AlarmType: "FIRE", Status: "ACTIVE", LastTriggeredAt: 1000}
	if _, _, err := repo.UpsertAlarm(ctx, unverified); err != nil {
		t.Fatal(err)
	}
	got, err := repo.AIAnalysisOutcomes(ctx, ports.AlarmFilter{TenantID: "ai-stats"}, "")
	if err != nil {
		t.Fatal(err)
	}
	want := []model.AIAnalysisOutcome{
		{RiskLevel: "", Result: model.DispositionTest, PromptVersion: "", Count: 1},
		{RiskLevel: "HIGH", Result: model.DispositionFalseAlarm, PromptVersion: "v2", Count: 1},
		{RiskLevel: "HIGH", Result: model.DispositionRealFire, PromptVersion: "v2", Count: 2},
		{RiskLevel: "LOW", Result: model.DispositionFalseAlarm, PromptVersion: "v1", Count: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("outcomes\n got %+v\nwant %+v", got, want)
	}
	if got, err = repo.AIAnalysisOutcomes(ctx, ports.AlarmFilter{TenantID: "ai-stats", DeviceIDs: []string{"d1"}}, "v2"); err != nil || len(got) != 1 || got[0].Count != 2 {
		t.Fatalf("filtered outcomes %+v %v", got, err)
	}
	if got, err = repo.AIAnalysisOutcomes(ctx, ports.AlarmFilter{TenantID: "none"}, ""); err != nil || len(got) != 0 {
		t.Fatalf("empty outcomes %+v %v", got, err)
	}
}
