package model

import "testing"

func TestSummarizeAIAnalysis(t *testing.T) {
	stats := SummarizeAIAnalysis([]AIAnalysisOutcome{
		{RiskLevel: "", Result: DispositionFalseAlarm, Count: 4},
		{RiskLevel: "HIGH", Result: DispositionRealFire, PromptVersion: "v2", Count: 3},
		{RiskLevel: "HIGH", Result: DispositionFalseAlarm, PromptVersion: "v2", Count: 1},
		{RiskLevel: "MEDIUM", Result: DispositionTest, PromptVersion: "v1", Count: 2},
		{RiskLevel: "LOW", Result: DispositionFalseAlarm, PromptVersion: "v1", Count: 5},
		{RiskLevel: "INFO", Result: DispositionRealFire, PromptVersion: "v1", Count: 1},
	})
	if stats.Verified != 16 || stats.Analyzed != 12 || stats.Comparable != 10 || stats.Agreement != 8 || stats.MissedFires != 1 || stats.AgreementRate != 0.8 {
		t.Fatalf("stats %+v", stats)
	}
	if stats.Matrix["HIGH"][DispositionFalseAlarm] != 1 || stats.ByPromptVersion["v1"] != 8 {
		t.Fatalf("matrix %+v versions %+v", stats.Matrix, stats.ByPromptVersion)
	}
	if empty := SummarizeAIAnalysis(nil); empty.AgreementRate != 0 || empty.Matrix == nil {
		t.Fatalf("empty %+v", empty)
	}
}
