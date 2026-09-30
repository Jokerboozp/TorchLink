package dataquality

import (
	"context"
	"encoding/json"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"iot-platform/internal/analytics"
	"iot-platform/internal/analytics/quality"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type Catalog interface {
	GetManagedDevice(context.Context, string, string) (model.ManagedDevice, error)
	GetProduct(context.Context, string, string) (model.Product, error)
	ListManagedDevices(context.Context, string) ([]model.ManagedDevice, error)
}
type Service struct {
	Analysis *analytics.Service
	Facts    ports.AnalyticsFactStore
	Catalog  Catalog
	Archive  ports.Archive
	// RecordLimit bounds frozen measurement members and raw outcomes separately.
	// Zero uses the default; deployment wiring may lower the hard safety ceiling.
	RecordLimit int
}

func NewService(analysis *analytics.Service, facts ports.AnalyticsFactStore) *Service {
	return &Service{Analysis: analysis, Facts: facts, RecordLimit: MaxRecords}
}
func (s *Service) recordLimit() int {
	if s.RecordLimit <= 0 {
		return MaxRecords
	}
	return min(MaxRecords, s.RecordLimit)
}
func (s *Service) Register() error { return s.Analysis.Register(analytics.KindDataQuality, s.Process) }
func (s *Service) authorize(ctx context.Context, a analytics.Actor, operation string, devices []string) (analytics.Actor, error) {
	current, err := s.Analysis.Current(ctx, a)
	if err != nil || !current.Allows(analytics.KindDataQuality, operation, devices) {
		return current, analytics.ErrForbidden
	}
	return current, nil
}
func resourceKind(resource string) string {
	switch resource {
	case "profiles":
		return model.DataQualityProfileKind
	case "baselines":
		return model.DataQualityBaselineKind
	case "calibrations":
		return model.DataQualityCalibrationKind
	case "attachments":
		return model.DataQualityAttachmentKind
	}
	return ""
}

// Shared configuration can be inherited by an authorized subset. Its immutable
// revision/body hash does not change, while device memberships are projected so
// readers never learn hidden members or their count.
func visibleConfig(v model.AnalysisConfigRevision, a analytics.Actor) (model.AnalysisConfigRevision, bool) {
	if v.Scope == "PERSONAL" && v.Creator != a.Username {
		return model.AnalysisConfigRevision{}, false
	}
	if v.Scope == "SHARED" && !a.AllDevices {
		ids := []string{}
		for _, id := range v.DeviceIDs {
			if slices.Contains(a.DeviceIDs, id) {
				ids = append(ids, id)
			}
		}
		v.DeviceIDs = ids
	}
	return v, a.Allows(analytics.KindDataQuality, "", v.DeviceIDs)
}
func (s *Service) GetConfig(ctx context.Context, a analytics.Actor, kind, id string) (model.AnalysisConfigRevision, error) {
	v, err := s.Analysis.Store.GetAnalysisConfig(ctx, a.TenantID, id)
	if err != nil {
		return v, err
	}
	if v.Kind != kind {
		return v, model.ErrNotFound
	}
	current, err := s.Analysis.Current(ctx, a)
	if err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	visible, ok := visibleConfig(v, current)
	if !ok {
		return model.AnalysisConfigRevision{}, analytics.ErrForbidden
	}
	return visible, nil
}
func (s *Service) ListConfigs(ctx context.Context, a analytics.Actor, resource string, f model.AnalysisFilter) ([]model.AnalysisConfigRevision, int, error) {
	current, err := s.Analysis.Current(ctx, a)
	if err != nil {
		return nil, 0, err
	}
	if !current.AllDevices && len(current.DeviceIDs) == 0 {
		return nil, 0, analytics.ErrForbidden
	}
	ids := current.DeviceIDs
	if current.AllDevices {
		ids = []string{"catalog-read"}
	}
	if !current.Allows(analytics.KindDataQuality, "", ids) {
		return nil, 0, analytics.ErrForbidden
	}
	filter := f
	filter.Kind = resourceKind(resource)
	filter.Limit = 100
	filter.Offset = 0
	if filter.Kind == "" {
		return nil, 0, invalid("未知配置种类")
	}
	// Stored shared memberships may be broader than a user's authorized
	// subset; scope filtering is applied after projection, before totals.
	filter.DeviceScopeSet = false
	filter.DeviceIDs = nil
	visible := []model.AnalysisConfigRevision{}
	for {
		page, total, err := s.Analysis.Store.ListAnalysisConfigs(ctx, a.TenantID, filter)
		if err != nil {
			return nil, 0, err
		}
		for _, v := range page {
			if projected, ok := visibleConfig(v, current); ok {
				visible = append(visible, projected)
			}
		}
		filter.Offset += len(page)
		if filter.Offset >= total {
			break
		}
		if filter.Offset > 10000 {
			return nil, 0, invalid("配置读取范围过大，请筛选资源")
		}
	}
	total := len(visible)
	limit, offset := analytics.NormalizeAnalysisPage(f.Limit, f.Offset)
	if offset >= total {
		return []model.AnalysisConfigRevision{}, total, nil
	}
	return visible[offset:min(total, offset+limit)], total, nil
}
func (s *Service) configWrite(ctx context.Context, a analytics.Actor, resource string, q model.QualityConfigRequest) (model.AnalysisConfigRevision, error) {
	q.Scope = strings.ToUpper(q.Scope)
	ids := slices.Clone(q.DeviceIDs)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	if q.ResourceID == "" || len(q.ResourceID) > 200 || q.ExpectedVersion < 0 || (q.Scope != "PERSONAL" && q.Scope != "SHARED") || len(ids) == 0 || len(ids) > s.Analysis.Limits.MaxDevices || !json.Valid(q.Body) {
		return model.AnalysisConfigRevision{}, invalid("配置资源、范围或版本无效")
	}
	current, err := s.authorize(ctx, a, "POST /api/v1/data-quality/"+resource, ids)
	if err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	if q.Scope == "SHARED" {
		if _, err = s.authorize(ctx, a, "POST /api/v1/data-quality/profiles/publish", ids); err != nil {
			return model.AnalysisConfigRevision{}, err
		}
	}
	if s.Catalog == nil {
		return model.AnalysisConfigRevision{}, analytics.ErrUnsupported
	}
	for _, id := range ids {
		if _, err = s.Catalog.GetManagedDevice(ctx, a.TenantID, id); err != nil {
			return model.AnalysisConfigRevision{}, analytics.ErrForbidden
		}
	}
	if q.ExpectedVersion > 0 {
		found := false
		for offset := 0; !found; {
			old, total, e := s.Analysis.Store.ListAnalysisConfigs(ctx, a.TenantID, model.AnalysisFilter{Kind: resourceKind(resource), ResourceID: q.ResourceID, Limit: 100, Offset: offset})
			if e != nil {
				return model.AnalysisConfigRevision{}, e
			}
			for _, v := range old {
				if v.Scope == q.Scope && v.Version == q.ExpectedVersion && (v.Scope == "SHARED" || v.Creator == current.Username) {
					found = true
					if _, e = s.authorize(ctx, a, "POST /api/v1/data-quality/"+resource, append(slices.Clone(ids), v.DeviceIDs...)); e != nil {
						return model.AnalysisConfigRevision{}, e
					}
					break
				}
			}
			offset += len(old)
			if offset >= total || offset >= 10000 {
				break
			}
		}
		if !found {
			return model.AnalysisConfigRevision{}, model.ErrAnalysisConflict
		}
	}
	revision := model.AnalysisConfigRevision{ID: uuid.NewString(), TenantID: a.TenantID, Kind: resourceKind(resource), ResourceID: q.ResourceID, Creator: current.Username, Scope: q.Scope, DeviceIDs: ids, Body: q.Body}
	return s.Analysis.Store.PutAnalysisConfig(ctx, revision, q.ExpectedVersion)
}
func (s *Service) SaveProfile(ctx context.Context, a analytics.Actor, q model.QualityConfigRequest) (model.AnalysisConfigRevision, error) {
	if _, err := s.authorize(ctx, a, "POST /api/v1/data-quality/profiles", q.DeviceIDs); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	tentative := model.AnalysisConfigRevision{ID: "validation", Body: q.Body}
	_, body, err := profile(tentative)
	if err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	if s.Catalog == nil {
		return model.AnalysisConfigRevision{}, analytics.ErrUnsupported
	}
	target := strings.ToUpper(body.TargetType)
	if target == "" {
		target = "DEVICE"
		if body.ProductID != "" {
			target = "PRODUCT"
		}
	}
	if !slices.Contains([]string{"DEVICE", "PRODUCT", "TENANT"}, target) {
		return model.AnalysisConfigRevision{}, invalid("配置目标须为DEVICE、PRODUCT或TENANT")
	}
	if strings.ToUpper(q.Scope) == "SHARED" && target != "DEVICE" {
		devices, e := s.Catalog.ListManagedDevices(ctx, a.TenantID)
		if e != nil {
			return model.AnalysisConfigRevision{}, e
		}
		affected := []string{}
		for _, d := range devices {
			if target == "TENANT" || d.ProductID == body.ProductID {
				affected = append(affected, d.ID)
			}
		}
		slices.Sort(affected)
		submitted := slices.Clone(q.DeviceIDs)
		slices.Sort(submitted)
		submitted = slices.Compact(submitted)
		if !slices.Equal(affected, submitted) {
			return model.AnalysisConfigRevision{}, analytics.ErrForbidden
		}
		if _, e = s.authorize(ctx, a, "POST /api/v1/data-quality/profiles/publish", affected); e != nil {
			return model.AnalysisConfigRevision{}, e
		}
	}
	for _, id := range q.DeviceIDs {
		device, e := s.Catalog.GetManagedDevice(ctx, a.TenantID, id)
		if e != nil {
			return model.AnalysisConfigRevision{}, analytics.ErrForbidden
		}
		if body.ProductID != "" && device.ProductID != body.ProductID {
			return model.AnalysisConfigRevision{}, invalid("设备不属于指定产品")
		}
		product, e := s.Catalog.GetProduct(ctx, a.TenantID, device.ProductID)
		if e != nil {
			return model.AnalysisConfigRevision{}, e
		}
		found := false
		if product.ThingModel != nil {
			for _, field := range product.ThingModel.Properties {
				if field.Identifier == body.AttributeID {
					found = true
					if !compatibleType(field.DataType, body.ValueType) {
						return model.AnalysisConfigRevision{}, invalid("测点类型与物模型不一致")
					}
					if body.UnitConfirmed && field.Unit != body.Unit {
						return model.AnalysisConfigRevision{}, invalid("已确认单位与物模型不一致")
					}
					break
				}
			}
		}
		if !found {
			return model.AnalysisConfigRevision{}, invalid("物模型中不存在该测点，请先完善产品资料")
		}
	}
	body.TargetType = target
	q.Body, _ = json.Marshal(body)
	return s.configWrite(ctx, a, "profiles", q)
}
func (s *Service) BuildBaseline(ctx context.Context, a analytics.Actor, q model.QualityConfigRequest) (model.AnalysisConfigRevision, error) {
	var request model.QualityBaselineRequest
	if err := strictDecode(q.Body, &request); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	if len(q.DeviceIDs) != 1 || q.DeviceIDs[0] != request.DeviceID || request.AttributeID == "" || request.Start < 0 || request.End <= request.Start || request.End-request.Start > s.Analysis.Limits.MaxRange.Milliseconds() {
		return model.AnalysisConfigRevision{}, invalid("基线需明确单设备、属性及有效样本区间")
	}
	if _, err := s.authorize(ctx, a, "POST /api/v1/data-quality/baselines", q.DeviceIDs); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	revision, err := s.GetConfig(ctx, a, model.DataQualityProfileKind, request.ProfileRevisionID)
	if err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	p, pb, err := profile(revision)
	if err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	if pb.AttributeID != request.AttributeID || !slices.Contains(revision.DeviceIDs, request.DeviceID) {
		return model.AnalysisConfigRevision{}, invalid("基线配置与设备属性不匹配")
	}
	if strings.ToUpper(q.Scope) == "SHARED" && revision.Scope != "SHARED" {
		return model.AnalysisConfigRevision{}, invalid("共享基线须引用共享配置版本")
	}
	if s.Facts == nil {
		return model.AnalysisConfigRevision{}, analytics.ErrUnsupported
	}
	var facts []model.MeasurementFact
	var sources []model.AnalysisSourceCoverage
	err = s.Facts.AnalyticsFactsRead(ctx, a.TenantID, func(reader ports.AnalyticsFactReader) error {
		q := model.FactQuery{DeviceIDs: []string{request.DeviceID}, Properties: []string{request.AttributeID}, Start: request.Start, End: request.End, Limit: min(1000, s.Analysis.Limits.BatchSize)}
		for {
			page, e := reader.QueryMeasurementSeries(q)
			if e != nil {
				return e
			}
			sources = mergeSource(sources, page, q.TimeBasis)
			if !page.Complete {
				return invalid("基线数据覆盖不足，须完成来源采集或回填")
			}
			if len(facts)+len(page.Items) > s.recordLimit() {
				return invalid("基线样本超出读取保护上限")
			}
			facts = append(facts, page.Items...)
			if !page.HasMore {
				break
			}
			q.Cursor = page.Cursor
		}
		return nil
	})
	if err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	samples := make([]quality.Sample, len(facts))
	for i, v := range facts {
		samples[i] = sample(v)
		if samples[i].OperatingCondition == "" {
			samples[i].OperatingCondition = request.OperatingCondition
		}
	}
	b, err := quality.BuildBaseline(quality.BaselineRequest{Profile: p, Samples: samples, Baseline: quality.Baseline{ID: "built", ProfileVersion: p.Version, ProtocolVersion: request.ProtocolVersion, ConfigurationVersion: request.ConfigurationVersion, Unit: p.Unit, OperatingCondition: request.OperatingCondition, Window: quality.Window{Start: instant(request.Start), End: instant(request.End)}, ValidFrom: instant(request.ValidFrom), ValidUntil: instant(request.ValidUntil)}})
	if err != nil {
		return model.AnalysisConfigRevision{}, invalid(err.Error())
	}
	hash, _ := analytics.AnalysisHash(facts)
	body := model.QualityBaseline{QualityBaselineRequest: request, Unit: b.Unit, ProfileVersion: b.ProfileVersion, SampleCount: b.SampleCount, Median: b.Median, MAD: b.MAD, QuantileLower: b.QuantileLower, QuantileUpper: b.QuantileUpper, LowerValue: b.LowerValue, UpperValue: b.UpperValue, CUSUMVersion: b.CUSUMVersion, InputHash: hash, Sources: sources}
	q.Body, _ = json.Marshal(body)
	return s.configWrite(ctx, a, "baselines", q)
}
func (s *Service) ConfirmBaseline(ctx context.Context, a analytics.Actor, id string, expected int64) (model.AnalysisConfigRevision, error) {
	revision, err := s.GetConfig(ctx, a, model.DataQualityBaselineKind, id)
	if err != nil {
		return revision, err
	}
	if revision.Version != expected {
		return revision, model.ErrAnalysisConflict
	}
	if _, err = s.authorize(ctx, a, "POST /api/v1/data-quality/baselines/:id/confirm", revision.DeviceIDs); err != nil {
		return revision, err
	}
	_, body, err := baseline(revision)
	if err != nil {
		return revision, err
	}
	if body.ConfirmedAt > 0 {
		return revision, model.ErrAnalysisConflict
	}
	body.ConfirmedBy = a.Username
	body.ConfirmedAt = time.Now().UnixMilli()
	data, _ := json.Marshal(body)
	return s.configWrite(ctx, a, "baselines", model.QualityConfigRequest{ResourceID: revision.ResourceID, ExpectedVersion: revision.Version, Scope: revision.Scope, DeviceIDs: revision.DeviceIDs, Body: data})
}
func (s *Service) SaveCalibration(ctx context.Context, a analytics.Actor, q model.QualityConfigRequest) (model.AnalysisConfigRevision, error) {
	if _, err := s.authorize(ctx, a, "POST /api/v1/data-quality/calibrations", q.DeviceIDs); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	var body model.QualityCalibration
	if err := strictDecode(q.Body, &body); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	if len(q.DeviceIDs) != 1 || q.DeviceIDs[0] != body.DeviceID || body.AttributeID == "" || body.CalibratedAt <= 0 || body.CalibratedAt > time.Now().UnixMilli() || strings.TrimSpace(body.Basis) == "" || strings.TrimSpace(body.ImplementedBy) == "" || len(body.Attachments) > 20 {
		return model.AnalysisConfigRevision{}, invalid("校准记录需设备、测点、时间、依据及实施人")
	}
	for _, n := range []*float64{body.Minimum, body.Maximum, body.Epsilon} {
		if n != nil && (math.IsNaN(*n) || math.IsInf(*n, 0)) {
			return model.AnalysisConfigRevision{}, invalid("校准值须有限")
		}
	}
	if body.Minimum != nil && body.Maximum != nil && *body.Minimum > *body.Maximum || body.Epsilon != nil && *body.Epsilon <= 0 {
		return model.AnalysisConfigRevision{}, invalid("量程或精度无效")
	}
	if s.Catalog == nil {
		return model.AnalysisConfigRevision{}, analytics.ErrUnsupported
	}
	device, err := s.Catalog.GetManagedDevice(ctx, a.TenantID, body.DeviceID)
	if err != nil {
		return model.AnalysisConfigRevision{}, analytics.ErrForbidden
	}
	product, err := s.Catalog.GetProduct(ctx, a.TenantID, device.ProductID)
	if err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	found := false
	if product.ThingModel != nil {
		for _, field := range product.ThingModel.Properties {
			if field.Identifier == body.AttributeID {
				found = true
				break
			}
		}
	}
	if !found {
		return model.AnalysisConfigRevision{}, invalid("校准测点不在设备物模型中")
	}
	for _, id := range body.Attachments {
		file, err := s.GetConfig(ctx, a, model.DataQualityAttachmentKind, id)
		if err != nil {
			return model.AnalysisConfigRevision{}, err
		}
		if !slices.Contains(file.DeviceIDs, body.DeviceID) {
			return model.AnalysisConfigRevision{}, analytics.ErrForbidden
		}
		if strings.ToUpper(q.Scope) == "SHARED" && file.Scope != "SHARED" {
			return model.AnalysisConfigRevision{}, invalid("共享校准记录须使用共享附件，请按共享范围重新上传")
		}
	}
	return s.configWrite(ctx, a, "calibrations", q)
}
func (s *Service) ValidateCreate(ctx context.Context, a analytics.Actor, q *analytics.CreateRequest) error {
	if q.Start < 0 || q.End <= q.Start || q.End-q.Start > s.Analysis.Limits.MaxRange.Milliseconds() {
		return invalid("分析区间无效或超出范围保护上限")
	}
	if _, err := s.authorize(ctx, a, "POST /api/v1/data-quality/runs", q.DeviceIDs); err != nil {
		return err
	}
	var parameters model.QualityRunParameters
	if err := strictDecode(q.Parameters, &parameters); err != nil {
		return err
	}
	if len(parameters.AttributeIDs) == 0 || len(parameters.ProfileRevisionIDs) == 0 || len(parameters.AttributeIDs) > 50 || len(parameters.ProfileRevisionIDs) > 500 || len(parameters.BaselineRevisionIDs) > 500 {
		return invalid("须选择测点与固定配置版本")
	}
	for _, ids := range [][]string{parameters.AttributeIDs, parameters.ProfileRevisionIDs, parameters.BaselineRevisionIDs} {
		seen := map[string]bool{}
		for _, id := range ids {
			if id == "" || seen[id] {
				return invalid("选择集合不能含空值或重复成员")
			}
			seen[id] = true
		}
	}
	revisions := []model.AnalysisConfigRevision{}
	profileBodies := map[string]model.QualityProfile{}
	for _, id := range parameters.ProfileRevisionIDs {
		v, err := s.GetConfig(ctx, a, model.DataQualityProfileKind, id)
		if err != nil {
			return err
		}
		_, p, err := profile(v)
		if err != nil {
			return err
		}
		if !slices.Contains(parameters.AttributeIDs, p.AttributeID) {
			return invalid("配置属性未包含在任务范围")
		}
		profileBodies[v.ID] = p
		revisions = append(revisions, v)
	}
	// Reject contradictory selections and an excessive slot budget before the
	// asynchronous task is queued. Unconfigured portions remain explicitly
	// unknown in the algorithm rather than being silently filled.
	for _, device := range q.DeviceIDs {
		for _, attribute := range parameters.AttributeIDs {
			selected := []model.QualityProfile{}
			for _, v := range revisions {
				p := profileBodies[v.ID]
				if p.AttributeID == attribute && slices.Contains(v.DeviceIDs, device) {
					selected = append(selected, p)
				}
			}
			if len(selected) == 0 {
				return invalid("每个设备属性均须选择固定配置版本")
			}
			slices.SortFunc(selected, func(a, b model.QualityProfile) int {
				if a.EffectiveFrom < b.EffectiveFrom {
					return -1
				}
				if a.EffectiveFrom > b.EffectiveFrom {
					return 1
				}
				return 0
			})
			var slots int64
			for i, p := range selected {
				if i > 0 && (selected[i-1].EffectiveTo == 0 || selected[i-1].EffectiveTo > p.EffectiveFrom) {
					return invalid("所选配置有效期重叠，请选择连续且不重叠的版本")
				}
				end := q.End
				if p.EffectiveTo != 0 {
					end = min(end, p.EffectiveTo)
				}
				start := max(q.Start, p.EffectiveFrom)
				if end > start && p.Mode == "periodic" {
					span := end - start
					slots += 2 * (span/p.PeriodMs + 1)
					if slots > MaxSlots {
						return invalid("周期槽数量超出运行保护上限，请缩短区间")
					}
				}
			}
		}
	}
	for _, id := range parameters.BaselineRevisionIDs {
		v, err := s.GetConfig(ctx, a, model.DataQualityBaselineKind, id)
		if err != nil {
			return err
		}
		_, b, err := baseline(v)
		if err != nil {
			return err
		}
		if b.ConfirmedAt == 0 || !slices.Contains(parameters.ProfileRevisionIDs, b.ProfileRevisionID) || !slices.Contains(q.DeviceIDs, b.DeviceID) {
			return invalid("基线未确认或不属于任务配置")
		}
		revisions = append(revisions, v)
	}
	slices.SortFunc(revisions, func(a, b model.AnalysisConfigRevision) int { return strings.Compare(a.ID, b.ID) })
	hash, err := configurationHash(revisions)
	if err != nil {
		return err
	}
	q.ConfigurationVersion = hash
	return nil
}
func (s *Service) Review(ctx context.Context, a analytics.Actor, findingID string, q model.QualityReviewRequest) (model.AnalysisReview, error) {
	run, err := s.Analysis.Get(ctx, a, analytics.KindDataQuality, q.RunID)
	if err != nil {
		return model.AnalysisReview{}, err
	}
	if _, err = s.authorize(ctx, a, "POST /api/v1/data-quality/findings/:id/reviews", run.DeviceIDs); err != nil {
		return model.AnalysisReview{}, err
	}
	if !slices.Contains([]string{"CONFIRMED_PROBLEM", "NORMAL_EXPLANATION", "RESOLVED", "OBSERVE"}, q.Result) || strings.TrimSpace(q.Explanation) == "" {
		return model.AnalysisReview{}, invalid("核实状态及依据无效")
	}
	found := false
	for offset := 0; !found; {
		findings, total, err := s.Analysis.Store.ListAnalysisOutputs(ctx, a.TenantID, model.AnalysisFilter{Kind: "findings", RunID: run.ID, Limit: 100, Offset: offset})
		if err != nil {
			return model.AnalysisReview{}, err
		}
		for _, v := range findings {
			if v.ID == findingID {
				found = true
				break
			}
		}
		offset += len(findings)
		if offset >= total {
			break
		}
	}
	if !found {
		return model.AnalysisReview{}, model.ErrNotFound
	}
	return s.Analysis.Store.AppendAnalysisReview(ctx, model.AnalysisReview{ID: uuid.NewString(), TenantID: a.TenantID, RunID: run.ID, ResourceID: findingID, ResourceVersion: 1, Reviewer: a.Username, Result: q.Result, Explanation: q.Explanation, CorrectsID: q.CorrectsID, IdempotencyKey: q.IdempotencyKey}, q.ExpectedRunVersion)
}
func (s *Service) Reviews(ctx context.Context, a analytics.Actor, runID, findingID string, f model.AnalysisFilter) ([]model.AnalysisReview, int, error) {
	if _, err := s.Analysis.Get(ctx, a, analytics.KindDataQuality, runID); err != nil {
		return nil, 0, err
	}
	f.RunID = runID
	f.ResourceID = findingID
	return s.Analysis.Store.ListAnalysisReviews(ctx, a.TenantID, f)
}

func compatibleType(a, b string) bool {
	canonical := func(v string) string {
		switch strings.ToLower(v) {
		case "integer", "int", "int32", "int64", "long", "float", "float32", "float64", "double", "number", "decimal":
			return "number"
		case "bool", "boolean":
			return "boolean"
		case "string", "text":
			return "string"
		default:
			return strings.ToLower(v)
		}
	}
	return canonical(a) == canonical(b)
}
