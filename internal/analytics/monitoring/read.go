package monitoring

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"iot-platform/internal/analytics"
	"iot-platform/internal/analytics/continuity"
	"iot-platform/internal/model"
)

func (s *Service) Review(ctx context.Context, a analytics.Actor, findingID string, q model.MonitoringReviewRequest) (model.AnalysisReview, error) {
	r, err := s.Analysis.Get(ctx, a, analytics.KindMonitoring, q.RunID)
	if err != nil {
		return model.AnalysisReview{}, err
	}
	if _, err = s.authorize(ctx, a, "POST /api/v1/monitoring-gaps/findings/:id/reviews", r.DeviceIDs); err != nil {
		return model.AnalysisReview{}, err
	}
	if !slices.Contains([]string{"CONFIRMED_PROBLEM", "NORMAL_EXPLANATION", "RESOLVED", "OBSERVE"}, q.Result) || strings.TrimSpace(q.Explanation) == "" {
		return model.AnalysisReview{}, invalid("核实状态及依据无效")
	}
	found := false
	for offset := 0; !found; {
		page, total, err := s.Analysis.Store.ListAnalysisOutputs(ctx, a.TenantID, model.AnalysisFilter{RunID: r.ID, Kind: "findings", Limit: 100, Offset: offset})
		if err != nil {
			return model.AnalysisReview{}, err
		}
		for _, v := range page {
			if v.ID == findingID {
				found = true
				break
			}
		}
		offset += len(page)
		if offset >= total {
			break
		}
	}
	if !found {
		return model.AnalysisReview{}, model.ErrNotFound
	}
	return s.Analysis.Store.AppendAnalysisReview(ctx, model.AnalysisReview{ID: uuid.NewString(), TenantID: a.TenantID, RunID: r.ID, ResourceID: findingID, ResourceVersion: 1, Reviewer: a.Username, Result: q.Result, Explanation: q.Explanation, CorrectsID: q.CorrectsID, IdempotencyKey: q.IdempotencyKey}, q.ExpectedRunVersion)
}
func (s *Service) Reviews(ctx context.Context, a analytics.Actor, runID, findingID string, f model.AnalysisFilter) ([]model.AnalysisReview, int, error) {
	if _, err := s.Analysis.Get(ctx, a, analytics.KindMonitoring, runID); err != nil {
		return nil, 0, err
	}
	f.RunID = runID
	f.ResourceID = findingID
	return s.Analysis.Store.ListAnalysisReviews(ctx, a.TenantID, f)
}
func (s *Service) Hypothesis(ctx context.Context, a analytics.Actor, runID string, q model.MonitoringHypothesisRequest) (model.MonitoringHypothesis, error) {
	r, err := s.Analysis.Get(ctx, a, analytics.KindMonitoring, runID)
	if err != nil {
		return model.MonitoringHypothesis{}, err
	}
	if _, err = s.authorize(ctx, a, "POST /api/v1/monitoring-gaps/runs/:id/hypotheses", r.DeviceIDs); err != nil {
		return model.MonitoringHypothesis{}, err
	}
	if r.Version != q.ExpectedRunVersion {
		return model.MonitoringHypothesis{}, model.ErrAnalysisConflict
	}
	if r.Status != model.AnalysisSucceeded && r.Status != model.AnalysisPartial {
		return model.MonitoringHypothesis{}, model.ErrAnalysisConflict
	}
	if q.IdempotencyKey == "" || len(q.IdempotencyKey) > 200 || q.GroupID == "" || q.At < 0 {
		return model.MonitoringHypothesis{}, invalid("假设需固定接入组、时间、任务版本和幂等键")
	}
	snapshot, err := s.Analysis.Snapshot(ctx, a, analytics.KindMonitoring, r.ID)
	if err != nil {
		return model.MonitoringHypothesis{}, err
	}
	var group continuity.DependencyGroup
	found := false
	for offset := 0; !found; {
		page, total, err := s.Analysis.Store.ListAnalysisOutputs(ctx, a.TenantID, model.AnalysisFilter{RunID: r.ID, Kind: "dependency-groups", Limit: 100, Offset: offset})
		if err != nil {
			return model.MonitoringHypothesis{}, err
		}
		for _, v := range page {
			if v.ID == q.GroupID {
				if err = json.Unmarshal(v.Body, &group); err != nil {
					return model.MonitoringHypothesis{}, err
				}
				found = true
				break
			}
		}
		offset += len(page)
		if offset >= total {
			break
		}
	}
	if !found {
		return model.MonitoringHypothesis{}, model.ErrNotFound
	}
	devices, err := continuity.Hypothesis(group, r.DeviceIDs, q.At)
	if err != nil {
		return model.MonitoringHypothesis{}, invalid(err.Error())
	}
	idHash, _ := analytics.AnalysisHash([]string{a.Username, r.ID, q.IdempotencyKey})
	id := r.ID + ":hypothesis:" + idHash
	readExisting := func() (model.MonitoringHypothesis, error) {
		v, err := s.Analysis.Store.GetAnalysisConfig(ctx, a.TenantID, id)
		if err != nil {
			return model.MonitoringHypothesis{}, err
		}
		var existing model.MonitoringHypothesis
		if err = json.Unmarshal(v.Body, &existing); err != nil {
			return existing, err
		}
		if v.Kind != model.MonitoringHypothesisKind || v.Creator != a.Username || existing.RunID != r.ID || existing.GroupID != q.GroupID || existing.At != q.At || existing.FactsHash != snapshot.FactsHash {
			return model.MonitoringHypothesis{}, model.ErrAnalysisConflict
		}
		return existing, nil
	}
	if existing, err := readExisting(); err == nil {
		return existing, nil
	} else if !errors.Is(err, model.ErrNotFound) {
		return model.MonitoringHypothesis{}, err
	}
	h := model.MonitoringHypothesis{ID: id, RunID: r.ID, SnapshotID: snapshot.ID, FactsHash: snapshot.FactsHash, GroupID: group.ID, At: q.At, DeviceIDs: devices, CreatedBy: a.Username, CreatedAt: time.Now().UnixMilli(), Limitations: []string{"假设影响只使用该固定快照的可见接入成员，不推断备用线路、因果关系或实体保障能力"}}
	if group.HistoryQuality == "CURRENT_ONLY" {
		h.Limitations = append(h.Limitations, "只代表固定当前关系快照的假设，过去关系未覆盖")
	}
	body, _ := json.Marshal(h)
	_, err = s.Analysis.Store.PutAnalysisConfig(ctx, model.AnalysisConfigRevision{ID: id, TenantID: a.TenantID, Kind: model.MonitoringHypothesisKind, ResourceID: id, Scope: "PERSONAL", DeviceIDs: r.DeviceIDs, Creator: a.Username, Body: body}, 0)
	if errors.Is(err, model.ErrAnalysisConflict) {
		return readExisting()
	}
	return h, err
}
func (s *Service) Hypotheses(ctx context.Context, a analytics.Actor, runID string, f model.AnalysisFilter) ([]model.MonitoringHypothesis, int, error) {
	if _, err := s.Analysis.Get(ctx, a, analytics.KindMonitoring, runID); err != nil {
		return nil, 0, err
	}
	items := []model.MonitoringHypothesis{}
	for offset := 0; ; {
		page, total, err := s.Analysis.Store.ListAnalysisConfigs(ctx, a.TenantID, model.AnalysisFilter{Kind: model.MonitoringHypothesisKind, Limit: 100, Offset: offset})
		if err != nil {
			return nil, 0, err
		}
		for _, v := range page {
			if v.Creator != a.Username {
				continue
			}
			var b model.MonitoringHypothesis
			if err = json.Unmarshal(v.Body, &b); err != nil {
				return nil, 0, err
			}
			if b.RunID == runID {
				items = append(items, b)
			}
		}
		offset += len(page)
		if offset >= total {
			break
		}
		if offset >= 10000 {
			return nil, 0, invalid("假设记录读取范围过大")
		}
	}
	total := len(items)
	limit, offset := analytics.NormalizeAnalysisPage(f.Limit, f.Offset)
	if offset >= total {
		return []model.MonitoringHypothesis{}, total, nil
	}
	return items[offset:min(total, offset+limit)], total, nil
}
func (s *Service) Export(ctx context.Context, a analytics.Actor, runID string) (json.RawMessage, error) {
	r, err := s.Analysis.Get(ctx, a, analytics.KindMonitoring, runID)
	if err != nil {
		return nil, err
	}
	if _, err = s.authorize(ctx, a, "GET /api/v1/monitoring-gaps/runs/:id/export", r.DeviceIDs); err != nil {
		return nil, err
	}
	snapshot, err := s.Analysis.Snapshot(ctx, a, analytics.KindMonitoring, r.ID)
	if err != nil {
		return nil, err
	}
	outputs := []model.AnalysisOutput{}
	evidence := []model.AnalysisEvidence{}
	reviews := []model.AnalysisReview{}
	hypotheses := []model.MonitoringHypothesis{}
	for _, kind := range []string{"metrics", "intervals", "dependency-groups", "findings"} {
		for offset := 0; ; {
			page, total, err := s.Analysis.Store.ListAnalysisOutputs(ctx, a.TenantID, model.AnalysisFilter{RunID: r.ID, Kind: kind, Limit: 100, Offset: offset})
			if err != nil {
				return nil, err
			}
			outputs = append(outputs, page...)
			offset += len(page)
			if offset >= total {
				break
			}
		}
	}
	for offset := 0; ; {
		page, total, err := s.Analysis.Store.ListAnalysisEvidence(ctx, a.TenantID, model.AnalysisFilter{RunID: r.ID, Limit: 100, Offset: offset})
		if err != nil {
			return nil, err
		}
		evidence = append(evidence, page...)
		offset += len(page)
		if offset >= total {
			break
		}
	}
	for offset := 0; ; {
		page, total, err := s.Analysis.Store.ListAnalysisReviews(ctx, a.TenantID, model.AnalysisFilter{RunID: r.ID, Limit: 100, Offset: offset})
		if err != nil {
			return nil, err
		}
		reviews = append(reviews, page...)
		offset += len(page)
		if offset >= total {
			break
		}
	}
	for offset := 0; ; {
		page, total, err := s.Hypotheses(ctx, a, r.ID, model.AnalysisFilter{Limit: 100, Offset: offset})
		if err != nil {
			return nil, err
		}
		hypotheses = append(hypotheses, page...)
		offset += len(page)
		if offset >= total {
			break
		}
	}
	r.LeaseOwner = ""
	r.LeaseToken = 0
	return json.Marshal(map[string]any{"format": "torchlink-monitoring-continuity-v1", "run": r, "snapshot": snapshot, "outputs": outputs, "evidence": evidence, "reviews": reviews, "hypotheses": hypotheses, "limitations": []string{"连接、接收、首次可用与未知时长分别计算；历史关系不足只支持固定当前假设", "结果不代表消防风险、实体设施保障或因果关系，需人工核实"}})
}
