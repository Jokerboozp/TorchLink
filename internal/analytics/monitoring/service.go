package monitoring

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"iot-platform/internal/analytics"
	"iot-platform/internal/analytics/continuity"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type Catalog interface {
	GetManagedDevice(context.Context, string, string) (model.ManagedDevice, error)
	GetProduct(context.Context, string, string) (model.Product, error)
	ListManagedDevices(context.Context, string) ([]model.ManagedDevice, error)
	GetDeviceAccessProfile(context.Context, string, string) (model.DeviceAccessProfile, error)
}
type Service struct {
	Analysis           *analytics.Service
	Facts              ports.AnalyticsFactStore
	Catalog            Catalog
	RecordLimit        int
	AvailabilitySource string
}

func NewService(analysis *analytics.Service, facts ports.AnalyticsFactStore) *Service {
	return &Service{Analysis: analysis, Facts: facts, RecordLimit: MaxRecords}
}
func (s *Service) Register() error { return s.Analysis.Register(analytics.KindMonitoring, s.Process) }
func (s *Service) recordLimit() int {
	if s.RecordLimit <= 0 {
		return MaxRecords
	}
	return min(MaxRecords, s.RecordLimit)
}
func (s *Service) authorize(ctx context.Context, a analytics.Actor, operation string, devices []string) (analytics.Actor, error) {
	current, err := s.Analysis.Current(ctx, a)
	if err != nil || !current.Allows(analytics.KindMonitoring, operation, devices) {
		return current, analytics.ErrForbidden
	}
	return current, nil
}
func kind(resource string) string {
	switch resource {
	case "profiles":
		return model.MonitoringProfileKind
	case "observations":
		return model.MonitoringObservationKind
	}
	return ""
}
func visible(v model.AnalysisConfigRevision, a analytics.Actor) (model.AnalysisConfigRevision, bool) {
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
	return v, a.Allows(analytics.KindMonitoring, "", v.DeviceIDs)
}
func (s *Service) GetConfig(ctx context.Context, a analytics.Actor, resource, id string) (model.AnalysisConfigRevision, error) {
	v, err := s.Analysis.Store.GetAnalysisConfig(ctx, a.TenantID, id)
	if err != nil {
		return v, err
	}
	if v.Kind != kind(resource) {
		return model.AnalysisConfigRevision{}, model.ErrNotFound
	}
	current, err := s.Analysis.Current(ctx, a)
	if err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	v, ok := visible(v, current)
	if !ok {
		return model.AnalysisConfigRevision{}, analytics.ErrForbidden
	}
	return v, nil
}
func (s *Service) ListConfigs(ctx context.Context, a analytics.Actor, resource string, f model.AnalysisFilter) ([]model.AnalysisConfigRevision, int, error) {
	current, err := s.Analysis.Current(ctx, a)
	if err != nil {
		return nil, 0, err
	}
	ids := current.DeviceIDs
	if current.AllDevices {
		ids = []string{"catalog-read"}
	}
	if !current.Allows(analytics.KindMonitoring, "", ids) {
		return nil, 0, analytics.ErrForbidden
	}
	filter := f
	filter.Kind = kind(resource)
	filter.Limit = 100
	filter.Offset = 0
	filter.DeviceScopeSet = false
	filter.DeviceIDs = nil
	if filter.Kind == "" {
		return nil, 0, invalid("未知配置种类")
	}
	items := []model.AnalysisConfigRevision{}
	for {
		page, total, err := s.Analysis.Store.ListAnalysisConfigs(ctx, a.TenantID, filter)
		if err != nil {
			return nil, 0, err
		}
		for _, v := range page {
			if projected, ok := visible(v, current); ok {
				items = append(items, projected)
			}
		}
		filter.Offset += len(page)
		if filter.Offset >= total {
			break
		}
		if filter.Offset >= 10000 {
			return nil, 0, invalid("配置读取范围过大，请筛选资源")
		}
	}
	total := len(items)
	limit, offset := analytics.NormalizeAnalysisPage(f.Limit, f.Offset)
	if offset >= total {
		return []model.AnalysisConfigRevision{}, total, nil
	}
	return items[offset:min(total, offset+limit)], total, nil
}
func (s *Service) writeConfig(ctx context.Context, a analytics.Actor, resource string, q model.MonitoringConfigRequest) (model.AnalysisConfigRevision, error) {
	q.Scope = strings.ToUpper(q.Scope)
	ids := slices.Clone(q.DeviceIDs)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	if q.ResourceID == "" || len(q.ResourceID) > 200 || q.ExpectedVersion < 0 || len(ids) == 0 || len(ids) > min(1000, s.Analysis.Limits.MaxDevices) || !slices.Contains([]string{"PERSONAL", "SHARED"}, q.Scope) || !json.Valid(q.Body) {
		return model.AnalysisConfigRevision{}, invalid("配置资源、版本或设备范围无效")
	}
	current, err := s.authorize(ctx, a, "POST /api/v1/monitoring-gaps/"+resource, ids)
	if err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	if q.Scope == "SHARED" {
		if _, err = s.authorize(ctx, a, "POST /api/v1/monitoring-gaps/profiles/publish", ids); err != nil {
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
			page, total, e := s.Analysis.Store.ListAnalysisConfigs(ctx, a.TenantID, model.AnalysisFilter{Kind: kind(resource), ResourceID: q.ResourceID, Limit: 100, Offset: offset})
			if e != nil {
				return model.AnalysisConfigRevision{}, e
			}
			for _, v := range page {
				if v.Scope == q.Scope && v.Version == q.ExpectedVersion && (v.Scope == "SHARED" || v.Creator == current.Username) {
					found = true
					oldAndNew := append(slices.Clone(ids), v.DeviceIDs...)
					if _, e = s.authorize(ctx, a, "POST /api/v1/monitoring-gaps/"+resource, oldAndNew); e != nil {
						return model.AnalysisConfigRevision{}, e
					}
					break
				}
			}
			offset += len(page)
			if offset >= total || offset >= 10000 {
				break
			}
		}
		if !found {
			return model.AnalysisConfigRevision{}, model.ErrAnalysisConflict
		}
	}
	return s.Analysis.Store.PutAnalysisConfig(ctx, model.AnalysisConfigRevision{ID: uuid.NewString(), TenantID: a.TenantID, Kind: kind(resource), ResourceID: q.ResourceID, Scope: q.Scope, DeviceIDs: ids, Creator: current.Username, Body: q.Body}, q.ExpectedVersion)
}
func (s *Service) SaveProfile(ctx context.Context, a analytics.Actor, q model.MonitoringConfigRequest) (model.AnalysisConfigRevision, error) {
	if _, err := s.authorize(ctx, a, "POST /api/v1/monitoring-gaps/profiles", q.DeviceIDs); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	_, b, err := profile(model.AnalysisConfigRevision{ID: "validation", Body: q.Body})
	if err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	if s.Catalog == nil {
		return model.AnalysisConfigRevision{}, analytics.ErrUnsupported
	}
	b.TargetType = strings.ToUpper(b.TargetType)
	if b.TargetType == "" {
		b.TargetType = "DEVICE"
		if b.ProductID != "" {
			b.TargetType = "PRODUCT"
		}
	}
	if !slices.Contains([]string{"DEVICE", "PRODUCT", "TENANT"}, b.TargetType) || b.TargetType == "PRODUCT" && b.ProductID == "" {
		return model.AnalysisConfigRevision{}, invalid("须指定DEVICE、完整PRODUCT或TENANT范围")
	}
	if strings.ToUpper(q.Scope) == "SHARED" && b.TargetType != "DEVICE" {
		devices, err := s.Catalog.ListManagedDevices(ctx, a.TenantID)
		if err != nil {
			return model.AnalysisConfigRevision{}, err
		}
		affected := []string{}
		for _, d := range devices {
			if b.TargetType == "TENANT" || d.ProductID == b.ProductID {
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
		if _, err = s.authorize(ctx, a, "POST /api/v1/monitoring-gaps/profiles/publish", affected); err != nil {
			return model.AnalysisConfigRevision{}, err
		}
	}
	for _, id := range q.DeviceIDs {
		d, err := s.Catalog.GetManagedDevice(ctx, a.TenantID, id)
		if err != nil {
			return model.AnalysisConfigRevision{}, analytics.ErrForbidden
		}
		if b.ProductID != "" && b.ProductID != d.ProductID {
			return model.AnalysisConfigRevision{}, invalid("设备不属于指定产品")
		}
		product, err := s.Catalog.GetProduct(ctx, a.TenantID, d.ProductID)
		if err != nil {
			return model.AnalysisConfigRevision{}, err
		}
		for _, attr := range b.Attributes {
			found := false
			if product.ThingModel != nil {
				for _, field := range product.ThingModel.Properties {
					if field.Identifier == attr.ID {
						found = true
						if !compatibleType(field.DataType, attr.ValueType) {
							return model.AnalysisConfigRevision{}, invalid("关键属性类型与物模型不一致")
						}
						break
					}
				}
			}
			if !found {
				return model.AnalysisConfigRevision{}, invalid("物模型中不存在关键属性")
			}
		}
	}
	q.Body, _ = json.Marshal(b)
	return s.writeConfig(ctx, a, "profiles", q)
}
func (s *Service) SaveObservation(ctx context.Context, a analytics.Actor, q model.MonitoringConfigRequest) (model.AnalysisConfigRevision, error) {
	if _, err := s.authorize(ctx, a, "POST /api/v1/monitoring-gaps/observations", q.DeviceIDs); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	var b model.MonitoringObservation
	if err := decode(q.Body, &b); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	if len(q.DeviceIDs) != 1 || q.DeviceIDs[0] != b.DeviceID || b.Start < 0 || b.End <= b.Start || b.End-b.Start > s.Analysis.Limits.MaxRange.Milliseconds() || !slices.Contains([]string{"RUNNING", "STOPPED", "MAINTENANCE"}, b.Type) || strings.TrimSpace(b.Basis) == "" || strings.TrimSpace(b.Reason) == "" || b.ConfirmedBy != "" || b.ConfirmedAt != 0 || b.Status != "" && b.Status != "DRAFT" {
		return model.AnalysisConfigRevision{}, invalid("观察区间须明确设备、起止、类型与依据；确认人由服务器绑定")
	}
	b.Status = "DRAFT"
	q.Body, _ = json.Marshal(b)
	return s.writeConfig(ctx, a, "observations", q)
}
func (s *Service) ConfirmObservation(ctx context.Context, a analytics.Actor, id string, expected int64) (model.AnalysisConfigRevision, error) {
	v, err := s.GetConfig(ctx, a, "observations", id)
	if err != nil {
		return v, err
	}
	if v.Version != expected {
		return v, model.ErrAnalysisConflict
	}
	if _, err = s.authorize(ctx, a, "POST /api/v1/monitoring-gaps/observations/:id/confirm", v.DeviceIDs); err != nil {
		return v, err
	}
	var b model.MonitoringObservation
	if err = decode(v.Body, &b); err != nil {
		return v, err
	}
	if b.Status != "DRAFT" {
		return v, model.ErrAnalysisConflict
	}
	b.Status = "CONFIRMED"
	b.ConfirmedBy = a.Username
	b.ConfirmedAt = time.Now().UnixMilli()
	body, _ := json.Marshal(b)
	return s.writeConfig(ctx, a, "observations", model.MonitoringConfigRequest{ResourceID: v.ResourceID, ExpectedVersion: v.Version, Scope: v.Scope, DeviceIDs: v.DeviceIDs, Body: body})
}
func (s *Service) ValidateCreate(ctx context.Context, a analytics.Actor, q *analytics.CreateRequest) error {
	if q.Start < 0 || q.End <= q.Start || q.End-q.Start > s.Analysis.Limits.MaxRange.Milliseconds() || len(q.DeviceIDs) > min(1000, s.Analysis.Limits.MaxDevices) {
		return invalid("分析范围超出保护上限")
	}
	if _, err := s.authorize(ctx, a, "POST /api/v1/monitoring-gaps/runs", q.DeviceIDs); err != nil {
		return err
	}
	var params model.MonitoringRunParameters
	if err := decode(q.Parameters, &params); err != nil {
		return err
	}
	if len(params.ProfileRevisionIDs) == 0 || len(params.ProfileRevisionIDs) > 500 || len(params.ObservationRevisionIDs) > 5000 || len(params.QualityRunIDs) > 100 {
		return invalid("固定版本选择超限")
	}
	for _, ids := range [][]string{params.ProfileRevisionIDs, params.ObservationRevisionIDs, params.QualityRunIDs} {
		seen := map[string]bool{}
		for _, id := range ids {
			if id == "" || seen[id] {
				return invalid("固定版本集合含空值或重复")
			}
			seen[id] = true
		}
	}
	if params.CommonGapPolicy != nil {
		if len(q.DeviceIDs)*(len(q.DeviceIDs)-1)/2 > MaxPairs {
			return invalid("共同缺报设备对超出保护上限")
		}
		if _, err := continuity.CommonGaps(nil, continuity.Range{Start: q.Start, End: q.End}, commonPolicy(params.CommonGapPolicy), MaxPairs); err != nil {
			return invalid(err.Error())
		}
	}
	revisions := []model.AnalysisConfigRevision{}
	for _, id := range params.ProfileRevisionIDs {
		v, err := s.GetConfig(ctx, a, "profiles", id)
		if err != nil {
			return err
		}
		if _, _, err = profile(v); err != nil {
			return err
		}
		revisions = append(revisions, v)
	}
	for _, device := range q.DeviceIDs {
		profiles := []continuity.Profile{}
		for _, v := range revisions {
			if slices.Contains(v.DeviceIDs, device) {
				p, _, _ := profile(v)
				profiles = append(profiles, p)
			}
		}
		if len(profiles) == 0 {
			return invalid("每个设备须选择监测配置")
		}
		slices.SortFunc(profiles, func(a, b continuity.Profile) int {
			if a.EffectiveFrom < b.EffectiveFrom {
				return -1
			}
			if a.EffectiveFrom > b.EffectiveFrom {
				return 1
			}
			return 0
		})
		for i := 1; i < len(profiles); i++ {
			if profiles[i-1].EffectiveTo == 0 || profiles[i-1].EffectiveTo > profiles[i].EffectiveFrom {
				return invalid("监测配置有效期重叠")
			}
		}
	}
	for _, id := range params.ObservationRevisionIDs {
		v, err := s.GetConfig(ctx, a, "observations", id)
		if err != nil {
			return err
		}
		var b model.MonitoringObservation
		if err = decode(v.Body, &b); err != nil {
			return err
		}
		if b.Status != "CONFIRMED" || b.ConfirmedBy == "" || !slices.Contains(q.DeviceIDs, b.DeviceID) {
			return invalid("观察区间未确认或不属于任务设备")
		}
		revisions = append(revisions, v)
	}
	quality := []model.AnalysisSnapshot{}
	for _, id := range params.QualityRunIDs {
		r, err := s.Analysis.Get(ctx, a, analytics.KindDataQuality, id)
		if err != nil {
			return err
		}
		if r.Status != model.AnalysisSucceeded && r.Status != model.AnalysisPartial {
			return invalid("仅可引用已冻结的数据质量结果")
		}
		for _, device := range r.DeviceIDs {
			if !slices.Contains(q.DeviceIDs, device) {
				return invalid("质量快照设备必须包含在监测任务中")
			}
		}
		if r.Start > q.Start || r.End < q.End {
			return invalid("质量快照未覆盖监测任务区间")
		}
		v, err := s.Analysis.Snapshot(ctx, a, analytics.KindDataQuality, id)
		if err != nil {
			return err
		}
		quality = append(quality, v)
	}
	hash, err := configurationHash(revisions, quality)
	if err != nil {
		return err
	}
	q.ConfigurationVersion = hash
	return nil
}
