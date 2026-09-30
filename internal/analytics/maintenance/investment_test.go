package maintenance

import (
	"iot-platform/internal/model"
	"testing"
)

func str(v string) *string   { return &v }
func num(v float64) *float64 { return &v }
func ms(v int64) *int64      { return &v }
func investmentFixture() (model.InvestmentScenario, map[string]InvestmentQuote) {
	s := model.InvestmentScenario{Currency: "CNY", Budget: str("0.3"), PlanningStart: 1000, PlanningEnd: 2000, PolicyVersion: DefaultInvestmentPolicy, Candidates: []model.InvestmentCandidate{{ID: "b", AssetID: "b", AssetRevisionID: "br", Action: "CHECK", RequiredTier: 1, TierBasis: "单位已确认", Comparable: true, ConfirmedFaultRate: num(2), KnownOfflineMs: ms(3), QuoteRevisionID: "qb"}, {ID: "a", AssetID: "a", AssetRevisionID: "ar", Action: "REPAIR", RequiredTier: 1, TierBasis: "单位已确认", Comparable: true, ConfirmedFaultRate: num(2), KnownOfflineMs: ms(3), QuoteRevisionID: "qa"}}}
	quotes := map[string]InvestmentQuote{}
	for _, id := range []string{"a", "b"} {
		amount := "0.1"
		if id == "b" {
			amount = "0.2"
		}
		revision := "q" + id
		quotes[revision] = InvestmentQuote{RevisionID: revision, Cost: model.MaintenanceCost{Type: "ESTIMATE", Currency: "CNY", SourceKind: "ASSET", SourceID: id, Material: str(amount), Labor: str("0"), ExternalService: str("0"), OccurredAt: 1000, PlanningStart: 1000, PlanningEnd: 2000, Basis: "确认报价"}}
	}
	return s, quotes
}
func TestDecimalBudgetExactAndStablePriority(t *testing.T) {
	s, q := investmentFixture()
	got, err := RankInvestment(s, q, true)
	if err != nil || got.Items[0].Candidate.ID != "a" || got.Items[1].BudgetStatus != "INCLUDED_IN_ORDERED_TRIAL" || got.Items[1].KnownCumulativeQuote == nil || *got.Items[1].KnownCumulativeQuote != "0.3" {
		t.Fatalf("decimal or ties wrong %+v %v", got, err)
	}
	s.Budget = str("0.299999")
	got, err = RankInvestment(s, q, true)
	if err != nil || got.Items[1].BudgetStatus != "ADDITIONAL_BUDGET_REQUIRED" || *got.Items[1].BudgetShortfall != "0.000001" || got.Items[1].Candidate.RequiredTier != 1 {
		t.Fatalf("required item downgraded %+v %v", got, err)
	}
}
func TestUnknownQuoteDoesNotBecomeZeroOrMixCurrencies(t *testing.T) {
	s, q := investmentFixture()
	v := q["qa"]
	v.Cost.Labor = nil
	q["qa"] = v
	got, err := RankInvestment(s, q, true)
	if err != nil || got.Items[0].BudgetStatus != "QUOTE_MISSING_OR_INCOMPARABLE" || got.Items[0].Quote != nil || got.Items[1].BudgetStatus != "EARLIER_QUOTE_OR_PRIORITY_UNKNOWN" {
		t.Fatalf("unknown diluted cost %+v %v", got, err)
	}
	v.Cost.Labor = str("0")
	v.Cost.Currency = "USD"
	q["qa"] = v
	got, err = RankInvestment(s, q, true)
	if err != nil || got.Items[0].Quote != nil {
		t.Fatalf("mixed currency added %+v %v", got, err)
	}
	v.Cost.Currency = "CNY"
	v.Cost.Type = "ACTUAL"
	q["qa"] = v
	got, err = RankInvestment(s, q, true)
	if err != nil || got.Items[0].Quote != nil {
		t.Fatalf("past actual used as quote %+v %v", got, err)
	}
}
func TestMissingMetricsSeparateAndExplicitManualException(t *testing.T) {
	s, q := investmentFixture()
	s.Candidates[1].Comparable = false
	s.Candidates[1].Importance = 9
	got, err := RankInvestment(s, q, true)
	if err != nil || got.Items[1].Candidate.ID != "a" || got.Items[1].Status != "AWAITING_INFORMATION" || got.Items[1].Rank != 0 || got.Items[1].Candidate.ConfirmedFaultRate != nil || got.Items[1].BudgetStatus != "PRIORITY_UNCONFIRMED" {
		t.Fatalf("unknown ranked low numeric %+v %v", got, err)
	}
	s.Adjustments = []model.InvestmentAdjustment{{CandidateOrder: []string{"a", "b"}, Actor: "unit-reviewer", RecordedAt: 1500, Reason: "必需事项先人工检查"}}
	got, err = RankInvestment(s, q, true)
	if err != nil || got.Items[0].Candidate.ID != "a" || got.Items[0].Rank != 1 || got.Items[1].BudgetStatus != "INCLUDED_IN_ORDERED_TRIAL" {
		t.Fatalf("explicit order not applied %+v %v", got, err)
	}
	s.Adjustments[0].CandidateOrder = []string{"a", "a"}
	if _, err = RankInvestment(s, q, true); err == nil {
		t.Fatal("duplicate manual order")
	}
}
func TestNonFinancialEvaluationContainsNoBudgetQuoteOrDerivedAllocation(t *testing.T) {
	s, q := investmentFixture()
	got, err := RankInvestment(s, q, false)
	if err != nil || got.UsesFinance || got.Budget != nil {
		t.Fatalf("finance exposed %+v %v", got, err)
	}
	for _, item := range got.Items {
		if item.Quote != nil || item.QuoteRevisionID != "" || item.Candidate.QuoteRevisionID != "" || item.BudgetShortfall != nil || item.KnownCumulativeQuote != nil || item.BudgetStatus != "FINANCE_NOT_INCLUDED" {
			t.Fatalf("derived finance leak %+v", item)
		}
	}
}
func TestCostPrecisionAndUnknownVersusZero(t *testing.T) {
	s, q := investmentFixture()
	_ = s
	cost := q["qa"].Cost
	for _, invalid := range []string{"-1", "1e3", "NaN", " 1", "01", "1.0000001"} {
		cost.Material = str(invalid)
		if ValidateCost(cost) == nil {
			t.Fatalf("invalid amount %q", invalid)
		}
	}
	cost.Material = nil
	if err := ValidateCost(cost); err != nil {
		t.Fatal(err)
	}
	if total, known, err := costTotal(cost); err != nil || known || total != nil {
		t.Fatal("nil cost converted to zero")
	}
	cost.Material = str("0")
	total, known, err := costTotal(cost)
	if err != nil || !known || decimalString(total) != "0" {
		t.Fatalf("explicit zero %+v %v", total, err)
	}
}
