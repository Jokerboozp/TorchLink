package maintenance

import (
	"context"
	"encoding/json"
	"fmt"
	"iot-platform/internal/analytics"
	"iot-platform/internal/model"
	"slices"
)

type businessInputs struct {
	SourceConfigurationHash string                         `json:"sourceConfigurationHash"`
	Parameters              model.MaintenanceRunParameters `json:"parameters"`
	Configs                 []model.AnalysisConfigRevision `json:"configs"`
	Snapshots               []model.AnalysisSnapshot       `json:"snapshots"`
	RequiredPermissions     []string                       `json:"requiredPermissions,omitempty"`
}

func businessHash(b businessInputs) (string, error) {
	refs := []string{}
	for _, v := range b.Configs {
		refs = append(refs, fmt.Sprintf("config:%s:%s", v.ID, v.Hash))
	}
	for _, v := range b.Snapshots {
		refs = append(refs, fmt.Sprintf("snapshot:%s:%d:%s", v.ID, v.Version, v.FactsHash))
	}
	slices.Sort(refs)
	refs = slices.Compact(refs)
	return analytics.AnalysisHash([]any{b.Parameters, refs, b.RequiredPermissions})
}
func configByID(b businessInputs, id string) model.AnalysisConfigRevision {
	for _, v := range b.Configs {
		if v.ID == id {
			return v
		}
	}
	return model.AnalysisConfigRevision{}
}
func (s *Service) observationInputs(ctx context.Context, a analytics.Actor, p model.MaintenanceRunParameters, devices []string) (businessInputs, error) {
	b := businessInputs{Parameters: p, Configs: []model.AnalysisConfigRevision{}, Snapshots: []model.AnalysisSnapshot{}}
	if p.Observation == nil || p.ScenarioRevisionID != "" {
		return b, invalid("观察参数缺失或混入投入方案")
	}
	q := p.Observation
	for _, v := range []struct{ kind, id string }{{InterventionKind, q.InterventionRevisionID}, {AssetKind, q.BeforeAssetRevisionID}, {AssetKind, q.AfterAssetRevisionID}} {
		config, err := s.Revision(ctx, a, v.kind, v.id)
		if err != nil {
			return b, err
		}
		if !slices.Equal(config.DeviceIDs, devices) {
			return b, analytics.ErrForbidden
		}
		b.Configs = append(b.Configs, config)
	}
	var work model.MaintenanceIntervention
	decode(b.Configs[0].Body, &work)
	if work.Status != "COMPLETED" || work.EndedAt <= 0 {
		return b, invalid("变化观察须绑定已结束的实际维修版本")
	}
	before, after := b.Configs[1], b.Configs[2]
	var beforeAsset, afterAsset model.AssetInstance
	decode(before.Body, &beforeAsset)
	decode(after.Body, &afterAsset)
	workAsset, err := s.Revision(ctx, a, AssetKind, work.AssetRevisionID)
	if err != nil {
		return b, err
	}
	if workAsset.ResourceID != before.ResourceID && workAsset.ResourceID != after.ResourceID {
		return b, invalid("维修未绑定比较实物")
	}
	if beforeAsset.DeviceID != afterAsset.DeviceID || beforeAsset.ComponentID != afterAsset.ComponentID {
		return b, invalid("前后须绑定同设备或同稳定部件槽")
	}
	if q.Before.Start <= 0 || q.Before.End <= q.Before.Start || q.After.End <= q.After.Start || q.Before.End > q.After.Start || q.Before.End > work.StartedAt || q.After.Start < work.EndedAt || q.After.End-q.Before.Start > s.Analysis.Limits.MaxRange.Milliseconds() || q.Before.End > s.now() {
		return b, invalid("前窗口须在维修开始前，后窗口在工作结束后；区间不得重叠或超限")
	}
	if q.ComparisonType == "SAME_INSTANCE_REPAIR" {
		if before.ResourceID != after.ResourceID {
			return b, invalid("同实例维修须绑定同实物资源")
		}
	} else if q.ComparisonType == "CROSS_INSTANCE_REPLACEMENT" {
		if before.ResourceID == after.ResourceID || q.SwitchAt <= 0 || beforeAsset.EffectiveEnd == nil || *beforeAsset.EffectiveEnd != q.SwitchAt || afterAsset.EffectiveStart != q.SwitchAt || beforeAsset.BoundaryStatus != "CONFIRMED" || afterAsset.BoundaryStatus != "CONFIRMED" || q.Before.End > q.SwitchAt || q.After.Start < q.SwitchAt {
			return b, invalid("跨实例须确认旧实例结束、新实例起点和实际切换时间")
		}
	} else {
		return b, invalid("比较类型无效")
	}
	if len(q.ContextRevisionIDs) > 1000 || len(q.QualityRunIDs) > 100 {
		return b, invalid("工况或质量快照数量超限")
	}
	for _, id := range q.ContextRevisionIDs {
		v, err := s.Revision(ctx, a, ContextKind, id)
		if err != nil {
			return b, err
		}
		if !subset(v.DeviceIDs, devices) {
			return b, analytics.ErrForbidden
		}
		b.Configs = append(b.Configs, v)
	}
	if q.AdmissionRevisionID != "" {
		v, err := s.Revision(ctx, a, AdmissionKind, q.AdmissionRevisionID)
		if err != nil {
			return b, err
		}
		if !subset(devices, v.DeviceIDs) {
			return b, analytics.ErrForbidden
		}
		var admission model.MaintenanceAdmissionRecord
		if decode(v.Body, &admission) != nil {
			return b, invalid("准入版本无效")
		}
		// Body criteria are derived from this immutable revision on every validation.
		q.Admission = &admission.Parameters
		b.Configs = append(b.Configs, v)
	} else if q.Admission != nil {
		return b, invalid("不得直接提交未版本化准入门槛")
	}
	for _, id := range q.QualityRunIDs {
		run, err := s.Analysis.Get(ctx, a, analytics.KindDataQuality, id)
		if err != nil {
			return b, err
		}
		if !subset(run.DeviceIDs, devices) {
			return b, analytics.ErrForbidden
		}
		snapshot, err := s.Analysis.Snapshot(ctx, a, analytics.KindDataQuality, id)
		if err != nil {
			return b, err
		}
		b.Snapshots = append(b.Snapshots, snapshot)
	}
	if p.UseFinance {
		if err = s.finance(ctx, a, devices); err != nil {
			return b, err
		}
	}
	b.Parameters = p
	if p.UseFinance {
		b.RequiredPermissions = append(b.RequiredPermissions, FinancePermission)
	}
	if len(q.QualityRunIDs) > 0 {
		b.RequiredPermissions = append(b.RequiredPermissions, analytics.QualityReadPermission)
	}
	slices.Sort(b.RequiredPermissions)
	b.SourceConfigurationHash, err = businessHash(b)
	return b, err
}
func (s *Service) investmentInputs(ctx context.Context, a analytics.Actor, p model.MaintenanceRunParameters, devices []string) (businessInputs, error) {
	b := businessInputs{Parameters: p, Configs: []model.AnalysisConfigRevision{}, Snapshots: []model.AnalysisSnapshot{}}
	if p.Observation != nil || p.ScenarioRevisionID == "" {
		return b, invalid("投入参数无效")
	}
	v, err := s.Revision(ctx, a, ScenarioKind, p.ScenarioRevisionID)
	if err != nil {
		return b, err
	}
	if !slices.Equal(v.DeviceIDs, devices) {
		return b, analytics.ErrForbidden
	}
	var scenario model.InvestmentScenario
	if decode(v.Body, &scenario) != nil {
		return b, invalid("方案版本无效")
	}
	if p.UseFinance != scenario.UseFinance {
		return b, invalid("任务必须使用方案固定资金口径；普通版本须另建方案")
	}
	if scenario.Status == "ARCHIVED" {
		return b, invalid("已归档方案不能继续评估")
	}
	refs, snapshots, err := s.bindCandidates(ctx, a, &scenario, devices, true)
	if err != nil {
		return b, err
	}
	// Derived values in the saved scenario must equal its fixed source snapshots.
	var saved model.InvestmentScenario
	decode(v.Body, &saved)
	h1, _ := analytics.AnalysisHash(saved.Candidates)
	h2, _ := analytics.AnalysisHash(scenario.Candidates)
	if h1 != h2 {
		return b, invalid("候选固定因素与其来源不一致；另建方案版本")
	}
	b.Configs = append(b.Configs, v)
	b.Configs = append(b.Configs, refs...)
	b.Snapshots = snapshots
	if p.UseFinance {
		b.RequiredPermissions = append(b.RequiredPermissions, FinancePermission)
	}
	for _, c := range scenario.Candidates {
		if c.ObservationRunID != "" {
			source, er := s.Analysis.Get(ctx, a, analytics.KindMaintenance, c.ObservationRunID)
			if er != nil {
				return b, er
			}
			b.RequiredPermissions = append(b.RequiredPermissions, source.RequiredPermissions...)
		}
	}
	slices.Sort(b.RequiredPermissions)
	b.RequiredPermissions = slices.Compact(b.RequiredPermissions)
	b.SourceConfigurationHash, err = businessHash(b)
	return b, err
}
func (s *Service) CreateObservation(ctx context.Context, a analytics.Actor, id string, q model.MaintenanceObservationRequest) (model.AnalysisRun, error) {
	v, err := s.Latest(ctx, a, InterventionKind, id)
	if err != nil {
		return model.AnalysisRun{}, err
	}
	if _, err = s.authorize(ctx, a, analytics.CreateOperation(analytics.KindMaintenance), v.DeviceIDs); err != nil {
		return model.AnalysisRun{}, err
	}
	if v.Version != q.ExpectedVersion {
		return model.AnalysisRun{}, model.ErrAnalysisConflict
	}
	if q.Parameters.InterventionRevisionID != "" && q.Parameters.InterventionRevisionID != v.ID {
		return model.AnalysisRun{}, invalid("任务与当前维修版本不匹配")
	}
	if q.Parameters.Admission != nil {
		return model.AnalysisRun{}, invalid("准入参数须引用固定版本")
	}
	q.Parameters.InterventionRevisionID = v.ID
	p := model.MaintenanceRunParameters{Observation: &q.Parameters, UseFinance: q.UseFinance}
	b, err := s.observationInputs(ctx, a, p, v.DeviceIDs)
	if err != nil {
		return model.AnalysisRun{}, err
	}
	data, _ := json.Marshal(b.Parameters)
	return s.Analysis.Create(ctx, a, analytics.KindMaintenance, AlgorithmVersion, analytics.CreateRequest{DeviceIDs: v.DeviceIDs, Start: q.Parameters.Before.Start, End: q.Parameters.After.End, ConfigurationVersion: b.SourceConfigurationHash, Parameters: data, IdempotencyKey: q.IdempotencyKey, RequiredPermissions: b.RequiredPermissions})
}
func (s *Service) EvaluateScenario(ctx context.Context, a analytics.Actor, id string, q model.InvestmentEvaluationRequest) (model.AnalysisRun, error) {
	v, err := s.Latest(ctx, a, ScenarioKind, id)
	if err != nil {
		return model.AnalysisRun{}, err
	}
	if v.Version != q.ExpectedVersion {
		return model.AnalysisRun{}, model.ErrAnalysisConflict
	}
	p := model.MaintenanceRunParameters{ScenarioRevisionID: v.ID, UseFinance: q.UseFinance}
	b, err := s.investmentInputs(ctx, a, p, v.DeviceIDs)
	if err != nil {
		return model.AnalysisRun{}, err
	}
	var scenario model.InvestmentScenario
	decode(v.Body, &scenario)
	data, _ := json.Marshal(p)
	return s.Analysis.Create(ctx, a, analytics.KindInvestment, DefaultInvestmentPolicy, analytics.CreateRequest{DeviceIDs: v.DeviceIDs, Start: scenario.PlanningStart, End: scenario.PlanningEnd, ConfigurationVersion: b.SourceConfigurationHash, Parameters: data, IdempotencyKey: q.IdempotencyKey, RequiredPermissions: b.RequiredPermissions})
}
func runActor(r model.AnalysisRun) analytics.Actor {
	return analytics.Actor{TenantID: r.TenantID, Username: r.Creator, Managed: r.CreatorManaged, SessionVersion: r.CreatorSessionVersion, AccessVersion: r.PermissionsVersion}
}
func (s *Service) GetScenarioCompletedRun(ctx context.Context, a analytics.Actor, id string) (model.AnalysisRun, error) {
	source, err := s.Revision(ctx, a, ScenarioKind, id)
	if err != nil {
		return model.AnalysisRun{}, err
	}
	// Related run filtering happens before selecting the latest completed task.
	for offset := 0; offset <= 10000; {
		rows, total, err := s.Analysis.List(ctx, a, analytics.KindInvestment, model.AnalysisFilter{Limit: 100, Offset: offset})
		if err != nil {
			return model.AnalysisRun{}, err
		}
		for _, r := range rows {
			var p model.MaintenanceRunParameters
			json.Unmarshal(r.Parameters, &p)
			if p.ScenarioRevisionID == source.ID && r.Status == model.AnalysisSucceeded {
				return r, nil
			}
		}
		offset += len(rows)
		if offset >= total || len(rows) == 0 {
			break
		}
	}
	return model.AnalysisRun{}, model.ErrNotFound
}
