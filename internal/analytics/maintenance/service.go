package maintenance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"iot-platform/internal/analytics"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

const (
	AssetKind           = "ASSET_INSTANCE"
	AssetSlotKind       = "ASSET_PHYSICAL_SLOT"
	InterventionKind    = "MAINTENANCE_RECORD"
	ContextKind         = "OPERATING_CONTEXT"
	FaultAssessmentKind = "MAINTENANCE_FAULT_ASSESSMENT"
	CostKind            = "MAINTENANCE_COST"
	ScenarioKind        = "INVESTMENT_SCENARIO"
	AttachmentKind      = "MAINTENANCE_ATTACHMENT"
	AdmissionKind       = "MAINTENANCE_ADMISSION"
)

type Catalog interface {
	GetManagedDevice(context.Context, string, string) (model.ManagedDevice, error)
	GetAlarm(context.Context, string, string) (model.Alarm, error)
}
type CorrectiveReader interface {
	Latest(context.Context, analytics.Actor, string, string) (model.AnalysisConfigRevision, error)
}
type AIReports interface {
	List(context.Context, analytics.Actor, string, string, model.AnalysisFilter) ([]model.AnalysisAIRevision, int, error)
}
type Service struct {
	Analysis        *analytics.Service
	Facts           ports.AnalyticsFactStore
	Catalog         Catalog
	Corrective      CorrectiveReader
	Archive         ports.Archive
	ResolveEvidence func(context.Context, analytics.Actor, model.ResponseEvidenceReference) (model.ResponseEvidenceReference, error)
	RecordLimit     int
	AI              AIReports
	Now             func() time.Time
}

func NewService(a *analytics.Service, f ports.AnalyticsFactStore) *Service {
	return &Service{Analysis: a, Facts: f, RecordLimit: 50000, Now: time.Now}
}
func (s *Service) Register() error {
	if err := s.Analysis.Register(analytics.KindMaintenance, s.ProcessObservation); err != nil {
		return err
	}
	return s.Analysis.Register(analytics.KindInvestment, s.ProcessInvestment)
}
func invalid(reason string) error { return fmt.Errorf("%w: %s", model.ErrAnalysisInvalid, reason) }
func decode(body json.RawMessage, result any) error {
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if err := d.Decode(result); err != nil {
		return invalid(err.Error())
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return invalid("只允许一个JSON对象")
	}
	return nil
}
func (s *Service) now() int64 {
	if s.Now != nil {
		return s.Now().UTC().UnixMilli()
	}
	return time.Now().UTC().UnixMilli()
}
func (s *Service) limit() int {
	if s.RecordLimit <= 0 {
		return 50000
	}
	return min(50000, s.RecordLimit)
}
func (s *Service) authorize(ctx context.Context, a analytics.Actor, operation string, devices []string) (analytics.Actor, error) {
	current, err := s.Analysis.Current(ctx, a)
	if err != nil || !current.Allows(analytics.KindMaintenance, operation, devices) {
		return current, analytics.ErrForbidden
	}
	return current, nil
}
func (s *Service) finance(ctx context.Context, a analytics.Actor, devices []string) error {
	_, err := s.authorize(ctx, a, FinancePermission, devices)
	return err
}
func path(kind string) string {
	switch kind {
	case AssetKind:
		return "/api/v1/assets"
	case InterventionKind:
		return "/api/v1/maintenance-records"
	case ContextKind:
		return "/api/v1/maintenance-contexts"
	case FaultAssessmentKind:
		return "/api/v1/maintenance-fault-cycles"
	case CostKind:
		return "/api/v1/maintenance-costs"
	case ScenarioKind:
		return "/api/v1/investment-scenarios"
	case AttachmentKind:
		return "/api/v1/maintenance-attachments"
	case AdmissionKind:
		return "/api/v1/maintenance-admissions"
	}
	return ""
}
func (s *Service) validateDevices(ctx context.Context, a analytics.Actor, ids []string) error {
	sorted := slices.Clone(ids)
	slices.Sort(sorted)
	if len(ids) == 0 || len(ids) > min(1000, s.Analysis.Limits.MaxDevices) || len(slices.Compact(sorted)) != len(ids) {
		return invalid("设备集合须明确、无重复且符合数量限制")
	}
	if s.Catalog == nil {
		return analytics.ErrUnsupported
	}
	for _, id := range ids {
		d, err := s.Catalog.GetManagedDevice(ctx, a.TenantID, id)
		if err != nil || d.TenantID != a.TenantID {
			return analytics.ErrForbidden
		}
	}
	return nil
}
func (s *Service) Revision(ctx context.Context, a analytics.Actor, kind, id string) (model.AnalysisConfigRevision, error) {
	v, err := s.Analysis.Store.GetAnalysisConfig(ctx, a.TenantID, id)
	if err != nil {
		return v, err
	}
	if v.Kind != kind || v.Scope != "SHARED" || path(kind) == "" {
		return model.AnalysisConfigRevision{}, model.ErrNotFound
	}
	if _, err = s.authorize(ctx, a, "", v.DeviceIDs); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	current, currentErr := s.Analysis.Current(ctx, a)
	if currentErr != nil || !current.AllowsRequired(configRequired(v)) {
		return model.AnalysisConfigRevision{}, analytics.ErrForbidden
	}
	return v, nil
}
func configRequired(v model.AnalysisConfigRevision) []string {
	if v.Kind == CostKind {
		return []string{FinancePermission}
	}
	if v.Kind != ScenarioKind {
		return nil
	}
	var b model.InvestmentScenario
	if json.Unmarshal(v.Body, &b) != nil {
		return []string{"invalid-config"}
	}
	required := slices.Clone(b.RequiredPermissions)
	if b.UseFinance {
		required = append(required, FinancePermission)
	}
	return required
}
func (s *Service) Latest(ctx context.Context, a analytics.Actor, kind, resource string) (model.AnalysisConfigRevision, error) {
	if path(kind) == "" || resource == "" {
		return model.AnalysisConfigRevision{}, model.ErrNotFound
	}
	var latest model.AnalysisConfigRevision
	for offset := 0; ; {
		page, total, err := s.Analysis.Store.ListAnalysisConfigs(ctx, a.TenantID, model.AnalysisFilter{Kind: kind, ResourceID: resource, Limit: 100, Offset: offset})
		if err != nil {
			return latest, err
		}
		for _, v := range page {
			if v.Version > latest.Version {
				latest = v
			}
		}
		offset += len(page)
		if offset >= total {
			break
		}
		if offset >= 10000 || len(page) == 0 {
			return latest, invalid("资源版本读取超限")
		}
	}
	if latest.ID == "" {
		return latest, model.ErrNotFound
	}
	return s.Revision(ctx, a, kind, latest.ID)
}
func (s *Service) List(ctx context.Context, a analytics.Actor, kind string, f model.AnalysisFilter) ([]model.AnalysisConfigRevision, int, error) {
	current, err := s.Analysis.Current(ctx, a)
	if err != nil {
		return nil, 0, err
	}
	probe := current.DeviceIDs
	if current.AllDevices {
		probe = []string{"catalog"}
	}
	if path(kind) == "" || !current.Allows(analytics.KindMaintenance, "", probe) {
		return nil, 0, analytics.ErrForbidden
	}
	if kind == CostKind && !current.Allows(analytics.KindMaintenance, FinancePermission, probe) {
		return nil, 0, analytics.ErrForbidden
	}
	latest := map[string]model.AnalysisConfigRevision{}
	for offset := 0; ; {
		page, total, err := s.Analysis.Store.ListAnalysisConfigs(ctx, a.TenantID, model.AnalysisFilter{Kind: kind, ResourceID: f.ResourceID, Limit: 100, Offset: offset})
		if err != nil {
			return nil, 0, err
		}
		for _, v := range page {
			if v.Version > latest[v.ResourceID].Version {
				latest[v.ResourceID] = v
			}
		}
		offset += len(page)
		if offset >= total {
			break
		}
		if offset >= 10000 || len(page) == 0 {
			return nil, 0, invalid("请缩小资源筛选范围")
		}
	}
	visible := []model.AnalysisConfigRevision{}
	for _, v := range latest {
		if current.Allows(analytics.KindMaintenance, "", v.DeviceIDs) && current.AllowsRequired(configRequired(v)) {
			visible = append(visible, v)
		}
	}
	slices.SortFunc(visible, func(a, b model.AnalysisConfigRevision) int {
		if a.CreatedAt > b.CreatedAt {
			return -1
		}
		if a.CreatedAt < b.CreatedAt {
			return 1
		}
		return strings.Compare(a.ResourceID, b.ResourceID)
	})
	total := len(visible)
	limit, offset := analytics.NormalizeAnalysisPage(f.Limit, f.Offset)
	if offset >= total {
		return []model.AnalysisConfigRevision{}, total, nil
	}
	return visible[offset:min(total, offset+limit)], total, nil
}
func receipts(body any) *[]model.ResponseRequestReceipt {
	switch b := body.(type) {
	case *model.AssetInstance:
		return &b.Requests
	case *model.MaintenanceIntervention:
		return &b.Requests
	case *model.OperatingContext:
		return &b.Requests
	case *model.MaintenanceFaultAssessment:
		return &b.Requests
	case *model.MaintenanceCost:
		return &b.Requests
	case *model.InvestmentScenario:
		return &b.Requests
	case *model.MaintenanceAdmissionRecord:
		return &b.Requests
	case *model.MaintenanceAttachment:
		return &b.Requests
	}
	return nil
}

// Check the persisted receipt before resolving replaceable source evidence.
// Identical retries keep their original result even after an original object
// expires; current resource scope and inherited permissions are still checked.
func (s *Service) replay(ctx context.Context, a analytics.Actor, kind, resource, key string, request any) (model.AnalysisConfigRevision, bool, error) {
	v, err := s.Latest(ctx, a, kind, resource)
	if errors.Is(err, model.ErrNotFound) {
		return v, false, nil
	}
	if err != nil {
		return v, false, err
	}
	if _, err = s.authorize(ctx, a, "POST "+path(kind), v.DeviceIDs); err != nil {
		return model.AnalysisConfigRevision{}, false, err
	}
	var body struct {
		Requests []model.ResponseRequestReceipt `json:"requests"`
	}
	if json.Unmarshal(v.Body, &body) != nil {
		return v, false, model.ErrAnalysisConflict
	}
	for _, r := range body.Requests {
		if r.Key == key {
			hash, err := analytics.AnalysisHash(request)
			if err != nil {
				return v, true, err
			}
			if r.Actor != a.Username || r.Hash != hash {
				return v, true, model.ErrAnalysisConflict
			}
			result, err := s.Revision(ctx, a, kind, r.ResultID)
			return result, true, err
		}
	}
	return v, false, nil
}
func (s *Service) prepare(ctx context.Context, a analytics.Actor, kind, resource string, devices []string, expected int64, key string, request, body any) (model.AnalysisConfigRevision, error) {
	if key == "" || len(key) > 200 || resource == "" || len(resource) > 200 || expected < 0 {
		return model.AnalysisConfigRevision{}, invalid("资源、幂等键或版本无效")
	}
	current, err := s.authorize(ctx, a, "", devices)
	if err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	hash, err := analytics.AnalysisHash(request)
	if err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	stored := receipts(body)
	if stored == nil {
		return model.AnalysisConfigRevision{}, invalid("资源不支持事务幂等")
	}
	for _, receipt := range *stored {
		if receipt.Key == key {
			if receipt.Hash != hash || receipt.Actor != current.Username {
				return model.AnalysisConfigRevision{}, model.ErrAnalysisConflict
			}
			return s.Revision(ctx, a, kind, receipt.ResultID)
		}
	}
	if len(*stored) >= 5000 {
		return model.AnalysisConfigRevision{}, invalid("业务操作保护上限已达到")
	}
	id := uuid.NewString()
	*stored = append(*stored, model.ResponseRequestReceipt{Key: key, Hash: hash, Actor: current.Username, ResultID: id})
	data, err := json.Marshal(body)
	if err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	return model.AnalysisConfigRevision{ID: id, TenantID: a.TenantID, Kind: kind, ResourceID: resource, Scope: "SHARED", Creator: current.Username, DeviceIDs: slices.Clone(devices), Body: data}, nil
}
func (s *Service) commit(ctx context.Context, a analytics.Actor, kind, resource string, devices []string, expected int64, key string, request, body any) (model.AnalysisConfigRevision, error) {
	v, err := s.prepare(ctx, a, kind, resource, devices, expected, key, request, body)
	if err != nil {
		return v, err
	}
	if v.Version > 0 {
		return v, nil
	}
	saved, err := s.Analysis.Store.PutAnalysisConfig(ctx, v, expected)
	if !errors.Is(err, model.ErrAnalysisConflict) {
		return saved, err
	}
	latest, e := s.Latest(ctx, a, kind, resource)
	if e != nil {
		return saved, err
	}
	var aggregate any
	switch kind {
	case AssetKind:
		aggregate = &model.AssetInstance{}
	case InterventionKind:
		aggregate = &model.MaintenanceIntervention{}
	case ContextKind:
		aggregate = &model.OperatingContext{}
	case FaultAssessmentKind:
		aggregate = &model.MaintenanceFaultAssessment{}
	case CostKind:
		aggregate = &model.MaintenanceCost{}
	case ScenarioKind:
		aggregate = &model.InvestmentScenario{}
	case AdmissionKind:
		aggregate = &model.MaintenanceAdmissionRecord{}
	case AttachmentKind:
		aggregate = &model.MaintenanceAttachment{}
	}
	if aggregate != nil && json.Unmarshal(latest.Body, aggregate) == nil {
		hash, _ := analytics.AnalysisHash(request)
		for _, r := range *receipts(aggregate) {
			if r.Key == key && r.Hash == hash && r.Actor == a.Username {
				return s.Revision(ctx, a, kind, r.ResultID)
			}
		}
	}
	return saved, err
}
func (s *Service) canonicalEvidence(ctx context.Context, a analytics.Actor, refs []model.ResponseEvidenceReference, devices []string) ([]model.ResponseEvidenceReference, error) {
	if len(refs) > 1000 {
		return nil, invalid("证据引用数量超限")
	}
	out := []model.ResponseEvidenceReference{}
	for _, ref := range refs {
		if !slices.Contains(devices, ref.DeviceID) {
			return nil, analytics.ErrForbidden
		}
		if ref.Kind == "ATTACHMENT" {
			canonical, err := s.AttachmentEvidence(ctx, a, ref)
			if err != nil {
				return nil, err
			}
			out = append(out, canonical)
			continue
		}
		if ref.Kind == "HUMAN_CONFIRMATION" {
			if strings.TrimSpace(ref.Description) == "" {
				return nil, invalid("人工确认须记录依据")
			}
			hash, _ := analytics.AnalysisHash([]any{a.Username, ref.Description, s.now()})
			out = append(out, model.ResponseEvidenceReference{ID: "human/" + hash, Kind: ref.Kind, DeviceID: ref.DeviceID, Description: ref.Description, Classification: "HUMAN_RECORDED", RecordedAt: s.now()})
			continue
		}
		if s.ResolveEvidence == nil {
			return nil, analytics.ErrUnsupported
		}
		canonical, err := s.ResolveEvidence(ctx, a, ref)
		if err != nil {
			return nil, err
		}
		out = append(out, canonical)
	}
	return out, nil
}
