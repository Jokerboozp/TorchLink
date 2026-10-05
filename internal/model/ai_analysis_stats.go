package model

import "slices"

// AIAnalysisOutcome counts verified alarms with one AI risk level, one
// verification result and one prompt version.
type AIAnalysisOutcome struct {
	RiskLevel     string `json:"riskLevel"`
	Result        string `json:"result"`
	PromptVersion string `json:"promptVersion"`
	Count         int    `json:"count"`
}

// AIAnalysisStats compares AI risk levels with on-site verification results.
//
// An analysis agrees with the verification when a CRITICAL or HIGH risk was a
// real fire, or a LOW or INFO risk was not. MEDIUM is a deliberate middle
// answer and is left out of the agreement rate. MissedFires are real fires
// the analysis rated LOW or INFO, the most costly disagreement.
type AIAnalysisStats struct {
	Verified        int                       `json:"verified"`
	Analyzed        int                       `json:"analyzed"`
	Matrix          map[string]map[string]int `json:"matrix"`
	ByPromptVersion map[string]int            `json:"byPromptVersion"`
	Agreement       int                       `json:"agreement"`
	Comparable      int                       `json:"comparable"`
	AgreementRate   float64                   `json:"agreementRate"`
	MissedFires     int                       `json:"missedFires"`
}

var (
	aiHighRisk = []string{"CRITICAL", "HIGH"}
	aiLowRisk  = []string{"LOW", "INFO"}
)

// SummarizeAIAnalysis builds the statistics from per-combination counts;
// verified alarms without an analysis have an empty RiskLevel.
func SummarizeAIAnalysis(outcomes []AIAnalysisOutcome) AIAnalysisStats {
	out := AIAnalysisStats{Matrix: map[string]map[string]int{}, ByPromptVersion: map[string]int{}}
	for _, o := range outcomes {
		out.Verified += o.Count
		if o.RiskLevel == "" {
			continue
		}
		out.Analyzed += o.Count
		if out.Matrix[o.RiskLevel] == nil {
			out.Matrix[o.RiskLevel] = map[string]int{}
		}
		out.Matrix[o.RiskLevel][o.Result] += o.Count
		out.ByPromptVersion[o.PromptVersion] += o.Count
		high, low := slices.Contains(aiHighRisk, o.RiskLevel), slices.Contains(aiLowRisk, o.RiskLevel)
		if !high && !low {
			continue
		}
		out.Comparable += o.Count
		fire := o.Result == DispositionRealFire
		if high == fire {
			out.Agreement += o.Count
		}
		if low && fire {
			out.MissedFires += o.Count
		}
	}
	if out.Comparable > 0 {
		out.AgreementRate = float64(out.Agreement) / float64(out.Comparable)
	}
	return out
}
