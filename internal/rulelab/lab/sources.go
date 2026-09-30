package lab

import (
	"context"
	"slices"

	"iot-platform/internal/analytics"
	"iot-platform/internal/model"
)

type ruleCatalog interface {
	ListRules(context.Context, string) ([]model.AlarmRule, error)
}

// RuleSources reveals only revisions applicable to explicitly authorized task
// devices. It grants no global production-rule read or management permission.
func (s *Service) RuleSources(ctx context.Context, a analytics.Actor, devices []string, f model.AnalysisFilter) ([]model.AlarmRuleRevision, int, error) {
	devices = slices.Clone(devices)
	slices.Sort(devices)
	devices = slices.Compact(devices)
	if len(devices) == 0 || len(devices) > min(1000, s.Analysis.Limits.MaxDevices) {
		return nil, 0, invalid("请指定规则实验的设备集合")
	}
	if _, err := s.authorize(ctx, a, "", devices); err != nil {
		return nil, 0, err
	}
	catalog, ok := s.Catalog.(ruleCatalog)
	if !ok || s.History == nil || s.ValidateCandidate == nil {
		return nil, 0, analytics.ErrUnsupported
	}
	products := map[string]bool{}
	for _, id := range devices {
		d, err := s.Catalog.GetManagedDevice(ctx, a.TenantID, id)
		if err != nil || d.TenantID != a.TenantID {
			return nil, 0, analytics.ErrForbidden
		}
		products[d.ProductID] = true
	}
	rules, err := catalog.ListRules(ctx, a.TenantID)
	if err != nil {
		return nil, 0, err
	}
	if len(rules) > s.recordLimit() {
		return nil, 0, invalid("规则来源集合超过读取上限")
	}
	result := []model.AlarmRuleRevision{}
	for _, rule := range rules {
		if rule.TenantID != a.TenantID || rule.ProductID != "" && !products[rule.ProductID] {
			continue
		}
		for offset := 0; ; {
			page, total, err := s.History.ListRuleRevisions(ctx, a.TenantID, rule.ID, 100, offset)
			if err != nil {
				return nil, 0, err
			}
			for _, v := range page {
				if v.TenantID != a.TenantID || v.RuleID != rule.ID || v.Hash != model.RuleBodyHash(v.Rule) || v.Rule.ProductID != "" && !products[v.Rule.ProductID] {
					continue
				}
				candidate := v.Rule
				candidate.Enabled = false
				// A hidden camera reference makes the whole immutable revision unreadable;
				// changing its body would break the published version/hash contract.
				if s.ValidateCandidate(ctx, a, candidate) != nil {
					continue
				}
				v.Actor = ""
				v.Reason = ""
				v.ExperimentID = ""
				result = append(result, v)
				if len(result) > s.recordLimit() {
					return nil, 0, invalid("规则版本来源集合超过读取上限")
				}
			}
			offset += len(page)
			if offset >= total {
				break
			}
			if len(page) == 0 {
				return nil, 0, invalid("规则版本分页未推进")
			}
		}
	}
	total := len(result)
	limit, offset := analytics.NormalizeAnalysisPage(f.Limit, f.Offset)
	if offset >= total {
		return []model.AlarmRuleRevision{}, total, nil
	}
	return result[offset:min(total, offset+limit)], total, nil
}
