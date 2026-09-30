package maintenance

import (
	"fmt"
	"math/big"
	"regexp"
	"slices"
	"strings"

	"iot-platform/internal/model"
)

const DefaultInvestmentPolicy = "required-tier-transparent-v1"
const FinancePermission = "GET /api/v1/maintenance-costs"

var decimalPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,23})(\.[0-9]{1,6})?$`)
var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

// All monetary arithmetic uses integer millionths. Unknown is represented by
// nil, never parsed as zero. The input bound caps arithmetic and export size.
func decimalUnits(value string) (*big.Int, error) {
	if !decimalPattern.MatchString(value) {
		return nil, fmt.Errorf("金额须为非负精确十进制，最多24位整数和6位小数")
	}
	parts := strings.SplitN(value, ".", 2)
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
	}
	units, ok := new(big.Int).SetString(parts[0]+fraction+strings.Repeat("0", 6-len(fraction)), 10)
	if !ok {
		return nil, fmt.Errorf("金额无效")
	}
	return units, nil
}
func decimalString(value *big.Int) string {
	raw := value.String()
	if len(raw) <= 6 {
		raw = strings.Repeat("0", 7-len(raw)) + raw
	}
	return strings.TrimRight(strings.TrimRight(raw[:len(raw)-6]+"."+raw[len(raw)-6:], "0"), ".")
}
func ValidateCost(cost model.MaintenanceCost) error {
	if !slices.Contains([]string{"ESTIMATE", "ACTUAL"}, cost.Type) || !currencyPattern.MatchString(cost.Currency) || cost.SourceKind == "" || cost.SourceID == "" || cost.OccurredAt <= 0 || cost.PlanningStart <= 0 || cost.PlanningEnd <= cost.PlanningStart || strings.TrimSpace(cost.Basis) == "" {
		return fmt.Errorf("费用类型、币种、来源、日期、规划范围及依据必须明确")
	}
	for _, amount := range []*string{cost.Material, cost.Labor, cost.ExternalService, cost.LaborHours} {
		if amount != nil {
			if _, err := decimalUnits(*amount); err != nil {
				return err
			}
		}
	}
	return nil
}
func costTotal(cost model.MaintenanceCost) (*big.Int, bool, error) {
	total := new(big.Int)
	for _, amount := range []*string{cost.Material, cost.Labor, cost.ExternalService} {
		if amount == nil {
			return nil, false, nil
		}
		units, err := decimalUnits(*amount)
		if err != nil {
			return nil, false, err
		}
		total.Add(total, units)
	}
	return total, true, nil
}

type InvestmentQuote struct {
	RevisionID string                `json:"revisionId"`
	Cost       model.MaintenanceCost `json:"cost"`
}
type InvestmentRankedItem struct {
	Candidate            model.InvestmentCandidate `json:"candidate"`
	Rank                 int                       `json:"rank"`
	Status               string                    `json:"status"`
	Reasons              []string                  `json:"reasons"`
	QuoteRevisionID      string                    `json:"quoteRevisionId,omitempty"`
	Quote                *string                   `json:"quote,omitempty"`
	BudgetStatus         string                    `json:"budgetStatus"`
	KnownCumulativeQuote *string                   `json:"knownCumulativeQuote,omitempty"`
	BudgetShortfall      *string                   `json:"budgetShortfall,omitempty"`
}
type InvestmentEvaluation struct {
	PolicyVersion string                 `json:"policyVersion"`
	Currency      string                 `json:"currency"`
	UsesFinance   bool                   `json:"usesFinance"`
	Budget        *string                `json:"budget,omitempty"`
	Items         []InvestmentRankedItem `json:"items"`
	Limitations   []string               `json:"limitations"`
}

// RankInvestment performs lexicographic ordering and an ordered budget trial.
// Inputs must already be bound to authorized immutable business revisions.
// Partial candidates are a named group within each tier; they receive no
// numerical position against comparable candidates until a human records an
// explicit complete order. This is not a procurement optimizer.
func RankInvestment(scenario model.InvestmentScenario, quotes map[string]InvestmentQuote, useFinance bool) (InvestmentEvaluation, error) {
	out := InvestmentEvaluation{PolicyVersion: DefaultInvestmentPolicy, Currency: scenario.Currency, UsesFinance: useFinance, Items: []InvestmentRankedItem{}, Limitations: []string{"DESCRIPTIVE_PRIORITY_AND_BUDGET_TRIAL_NOT_OPTIMAL_PROCUREMENT", "NO_ROI_OR_CAUSAL_BENEFIT_INFERRED"}}
	if scenario.PolicyVersion != DefaultInvestmentPolicy || !currencyPattern.MatchString(scenario.Currency) || scenario.PlanningStart <= 0 || scenario.PlanningEnd <= scenario.PlanningStart || len(scenario.Candidates) == 0 || len(scenario.Candidates) > 1000 {
		return out, fmt.Errorf("排序策略、币种、规划范围或候选数量无效")
	}
	candidates := slices.Clone(scenario.Candidates)
	seen := map[string]bool{}
	for _, v := range candidates {
		if v.ID == "" || seen[v.ID] || v.AssetID == "" || v.AssetRevisionID == "" || v.RequiredTier < 1 || v.RequiredTier > 9 || strings.TrimSpace(v.TierBasis) == "" || v.Importance < 0 || v.Importance > 9 {
			return out, fmt.Errorf("候选身份、人工分层依据或重要性无效")
		}
		seen[v.ID] = true
		if v.ConfirmedFaultRate != nil && (!finite(*v.ConfirmedFaultRate) || *v.ConfirmedFaultRate < 0) || v.KnownOfflineMs != nil && *v.KnownOfflineMs < 0 {
			return out, fmt.Errorf("候选指标无效")
		}
		if v.UnresolvedVerifiedDefect && len(v.DefectBasisIDs) == 0 {
			return out, fmt.Errorf("已核实缺陷缺少依据")
		}
	}
	comparable := func(v model.InvestmentCandidate) bool {
		return v.Comparable && v.ConfirmedFaultRate != nil && v.KnownOfflineMs != nil
	}
	slices.SortFunc(candidates, func(a, b model.InvestmentCandidate) int {
		if a.RequiredTier != b.RequiredTier {
			return a.RequiredTier - b.RequiredTier
		}
		if comparable(a) != comparable(b) {
			if comparable(a) {
				return -1
			}
			return 1
		}
		if comparable(a) {
			if a.UnresolvedVerifiedDefect != b.UnresolvedVerifiedDefect {
				if a.UnresolvedVerifiedDefect {
					return -1
				}
				return 1
			}
			if a.Importance != b.Importance {
				return b.Importance - a.Importance
			}
			if *a.ConfirmedFaultRate != *b.ConfirmedFaultRate {
				if *a.ConfirmedFaultRate > *b.ConfirmedFaultRate {
					return -1
				}
				return 1
			}
			if *a.KnownOfflineMs != *b.KnownOfflineMs {
				if *a.KnownOfflineMs > *b.KnownOfflineMs {
					return -1
				}
				return 1
			}
		}
		if c := strings.Compare(a.AssetID, b.AssetID); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	manuallyOrdered := false
	if len(scenario.Adjustments) > 0 {
		change := scenario.Adjustments[len(scenario.Adjustments)-1]
		if change.Actor == "" || change.RecordedAt <= 0 || strings.TrimSpace(change.Reason) == "" || len(change.CandidateOrder) != len(candidates) {
			return out, fmt.Errorf("人工调整须绑定完整顺序、人员、时间及理由")
		}
		byID := map[string]model.InvestmentCandidate{}
		for _, v := range candidates {
			byID[v.ID] = v
		}
		ordered := make([]model.InvestmentCandidate, 0, len(candidates))
		for _, id := range change.CandidateOrder {
			v, ok := byID[id]
			if !ok {
				return out, fmt.Errorf("人工顺序须是候选集合的唯一排列")
			}
			ordered = append(ordered, v)
			delete(byID, id)
		}
		candidates = ordered
		manuallyOrdered = true
	}
	var budget *big.Int
	if useFinance && scenario.Budget != nil {
		var err error
		budget, err = decimalUnits(*scenario.Budget)
		if err != nil {
			return out, err
		}
		v := decimalString(budget)
		out.Budget = &v
	}
	cumulative := new(big.Int)
	unknownPrevious := false
	for _, v := range candidates {
		item := InvestmentRankedItem{Candidate: v, Status: "COMPARABLE", Reasons: []string{fmt.Sprintf("REQUIRED_TIER_%d", v.RequiredTier), "UNIT_CONFIRMED_TIER_BASIS", fmt.Sprintf("MANUAL_IMPORTANCE_%d", v.Importance)}, BudgetStatus: "FINANCE_NOT_INCLUDED"}
		if comparable(v) {
			item.Reasons = append(item.Reasons, "CONFIRMED_FAULT_RATE_SAME_BASIS", "KNOWN_OFFLINE_DURATION")
			if v.UnresolvedVerifiedDefect {
				item.Reasons = append(item.Reasons, "UNRESOLVED_VERIFIED_FUNCTIONAL_DEFECT")
			}
		} else {
			item.Status = "AWAITING_INFORMATION"
			item.Reasons = append(item.Reasons, "MISSING_OR_INCOMPARABLE_METRICS_NOT_LOW_RISK")
			item.Candidate.ConfirmedFaultRate = nil
			item.Candidate.KnownOfflineMs = nil
		}
		if manuallyOrdered {
			item.Reasons = append(item.Reasons, "EXPLICIT_HUMAN_ORDER_WITH_RECORDED_REASON")
		}
		if comparable(v) || manuallyOrdered {
			item.Rank = len(out.Items) + 1
		}
		if useFinance {
			item.QuoteRevisionID = v.QuoteRevisionID
			quote, ok := quotes[v.QuoteRevisionID]
			valid := ok && quote.RevisionID == v.QuoteRevisionID && quote.Cost.Type == "ESTIMATE" && quote.Cost.Currency == scenario.Currency && quote.Cost.PlanningStart == scenario.PlanningStart && quote.Cost.PlanningEnd == scenario.PlanningEnd && quote.Cost.SourceID == v.AssetID
			if ok {
				if err := ValidateCost(quote.Cost); err != nil {
					return out, err
				}
			}
			var amount *big.Int
			complete := false
			var err error
			if valid {
				amount, complete, err = costTotal(quote.Cost)
				if err != nil {
					return out, err
				}
			}
			switch {
			case !valid || !complete:
				item.BudgetStatus = "QUOTE_MISSING_OR_INCOMPARABLE"
				unknownPrevious = true
			default:
				text := decimalString(amount)
				item.Quote = &text
				cumulative.Add(cumulative, amount)
				sum := decimalString(cumulative)
				item.KnownCumulativeQuote = &sum
				switch {
				case !comparable(v) && !manuallyOrdered:
					item.BudgetStatus = "PRIORITY_UNCONFIRMED"
					unknownPrevious = true
				case budget == nil:
					item.BudgetStatus = "BUDGET_UNKNOWN"
				case unknownPrevious:
					item.BudgetStatus = "EARLIER_QUOTE_OR_PRIORITY_UNKNOWN"
				case cumulative.Cmp(budget) <= 0:
					item.BudgetStatus = "INCLUDED_IN_ORDERED_TRIAL"
				default:
					item.BudgetStatus = "ADDITIONAL_BUDGET_REQUIRED"
					gap := decimalString(new(big.Int).Sub(cumulative, budget))
					item.BudgetShortfall = &gap
				}
			}
		} else {
			item.Candidate.QuoteRevisionID = ""
		}
		out.Items = append(out.Items, item)
	}
	if unknownPrevious {
		out.Limitations = append(out.Limitations, "KNOWN_QUOTES_ARE_NOT_COMPLETE_BUDGET_COST")
	}
	return out, nil
}
