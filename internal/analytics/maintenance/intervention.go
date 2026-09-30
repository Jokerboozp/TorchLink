package maintenance

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/google/uuid"
	"iot-platform/internal/analytics"
	"iot-platform/internal/model"
)

func (s *Service) SaveIntervention(ctx context.Context, a analytics.Actor, q model.MaintenanceRevisionRequest) (model.AnalysisConfigRevision, error) {
	if v, found, err := s.replay(ctx, a, InterventionKind, q.ResourceID, q.IdempotencyKey, q); found || err != nil {
		return v, err
	}
	if _, err := s.authorize(ctx, a, "POST /api/v1/maintenance-records", q.DeviceIDs); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	var b model.MaintenanceIntervention
	if err := decode(q.Body, &b); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	if strings.TrimSpace(b.Name) == "" || strings.TrimSpace(b.Reason) == "" || !slices.Contains([]string{"INSPECTION", "REPAIR", "REPLACEMENT", "CALIBRATION", "PREVENTIVE"}, b.Type) || b.Status != "" && b.Status != "DRAFT" || b.StartedAt != 0 || b.EndedAt != 0 || len(b.Verifications) > 0 || len(b.History) > 0 || len(b.Requests) > 0 || len(b.Actions) > 1000 || len(b.People) > 1000 || len(b.Parts) > 1000 || len(b.FaultCycleIDs) > 1000 {
		return model.AnalysisConfigRevision{}, invalid("维修记录需绑定实物、动作和依据；工作状态及验收由独立操作保存")
	}
	asset, err := s.Revision(ctx, a, AssetKind, b.AssetRevisionID)
	if err != nil {
		return asset, err
	}
	if !sameDevices(asset.DeviceIDs, q.DeviceIDs) {
		return asset, analytics.ErrForbidden
	}
	for _, id := range b.FaultCycleIDs {
		alarm, err := s.Catalog.GetAlarm(ctx, a.TenantID, id)
		if err != nil || alarm.TenantID != a.TenantID || !slices.Contains(q.DeviceIDs, alarm.DeviceID) {
			return model.AnalysisConfigRevision{}, analytics.ErrForbidden
		}
	}
	if b.CorrectiveActionID != "" {
		if s.Corrective == nil {
			return model.AnalysisConfigRevision{}, analytics.ErrUnsupported
		}
		action, err := s.Corrective.Latest(ctx, a, "CORRECTIVE_ACTION", b.CorrectiveActionID)
		if err != nil {
			return action, err
		}
		if !sameDevices(action.DeviceIDs, q.DeviceIDs) {
			return action, analytics.ErrForbidden
		}
	}
	b.Status = "DRAFT"
	old, err := s.Latest(ctx, a, InterventionKind, q.ResourceID)
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		return old, err
	}
	if err == nil {
		var prior model.MaintenanceIntervention
		if decode(old.Body, &prior) != nil {
			return old, model.ErrAnalysisConflict
		}
		for _, r := range prior.Requests {
			if r.Key == q.IdempotencyKey {
				return s.commit(ctx, a, InterventionKind, q.ResourceID, old.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &prior)
			}
		}
		if prior.Status != "DRAFT" || prior.AssetRevisionID != b.AssetRevisionID || !sameDevices(q.DeviceIDs, old.DeviceIDs) {
			return old, invalid("已开始的维修不能改绑实物或重写工作历史")
		}
		b.Requests = prior.Requests
		b.History = prior.History
	}
	return s.commit(ctx, a, InterventionKind, q.ResourceID, q.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &b)
}
func (s *Service) InterventionAction(ctx context.Context, a analytics.Actor, id string, q model.MaintenanceActionRequest) (model.AnalysisConfigRevision, error) {
	v, err := s.Latest(ctx, a, InterventionKind, id)
	if err != nil {
		return v, err
	}
	if _, err = s.authorize(ctx, a, "POST /api/v1/maintenance-records/:id/actions", v.DeviceIDs); err != nil {
		return v, err
	}
	var b model.MaintenanceIntervention
	if err = decode(v.Body, &b); err != nil {
		return v, err
	}
	for _, r := range b.Requests {
		if r.Key == q.IdempotencyKey {
			return s.commit(ctx, a, InterventionKind, id, v.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &b)
		}
	}
	if v.Version != q.ExpectedVersion {
		return v, model.ErrAnalysisConflict
	}
	at := q.At
	if at == 0 {
		at = s.now()
	}
	if at <= 0 || at > s.now() {
		return v, invalid("实际工作时间无效")
	}
	switch q.Action {
	case "START":
		if b.Status != "DRAFT" {
			return v, model.ErrAnalysisConflict
		}
		b.Status = "IN_PROGRESS"
		b.StartedAt = at
	case "COMPLETE":
		if b.Status != "IN_PROGRESS" || at < b.StartedAt || strings.TrimSpace(q.Reason) == "" {
			return v, invalid("完成须记录实际动作结束依据；不替代功能验收")
		}
		b.Status = "COMPLETED"
		b.EndedAt = at
	case "CANCEL":
		if b.Status == "COMPLETED" || b.Status == "CANCELLED" || strings.TrimSpace(q.Reason) == "" {
			return v, model.ErrAnalysisConflict
		}
		b.Status = "CANCELLED"
	default:
		return v, invalid("维修状态操作无效")
	}
	b.History = append(b.History, model.ResponseActionEvent{Action: q.Action, Actor: a.Username, OccurredAt: at, RecordedAt: s.now(), Reason: q.Reason})
	return s.commit(ctx, a, InterventionKind, id, v.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &b)
}
func (s *Service) VerifyIntervention(ctx context.Context, a analytics.Actor, id string, q model.MaintenanceVerificationRequest) (model.AnalysisConfigRevision, error) {
	v, err := s.Latest(ctx, a, InterventionKind, id)
	if err != nil {
		return v, err
	}
	if _, err = s.authorize(ctx, a, "POST /api/v1/maintenance-records/:id/verifications", v.DeviceIDs); err != nil {
		return v, err
	}
	var b model.MaintenanceIntervention
	if err = decode(v.Body, &b); err != nil {
		return v, err
	}
	for _, r := range b.Requests {
		if r.Key == q.IdempotencyKey {
			return s.commit(ctx, a, InterventionKind, id, v.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &b)
		}
	}
	if v.Version != q.ExpectedVersion || b.Status == "DRAFT" || b.Status == "CANCELLED" {
		return v, model.ErrAnalysisConflict
	}
	verification := q.Verification
	if verification.ID != "" || verification.Verifier != "" || verification.RecordedAt != 0 || len(verification.RequiredItems) == 0 || len(verification.RequiredItems) > 1000 || len(verification.CheckedItems) > 1000 || !slices.Contains([]string{"PENDING", "PASSED", "FAILED", "UNKNOWN"}, verification.Result) || strings.TrimSpace(verification.Explanation) == "" {
		return v, invalid("功能验收须有规定项、实际结果和依据，验收人由服务端绑定")
	}
	if verification.Result == "PASSED" {
		if len(verification.Evidence) == 0 {
			return v, invalid("功能验收通过须有实际证据")
		}
		for _, required := range verification.RequiredItems {
			if strings.TrimSpace(required) == "" || !slices.Contains(verification.CheckedItems, required) {
				return v, invalid("通过须完整检查规定项")
			}
		}
	}
	verification.Evidence, err = s.canonicalEvidence(ctx, a, verification.Evidence, v.DeviceIDs)
	if err != nil {
		return v, err
	}
	verification.ID = uuid.NewString()
	verification.Verifier = a.Username
	verification.RecordedAt = s.now()
	b.Verifications = append(b.Verifications, verification)
	if len(b.Verifications) > 5000 {
		return v, invalid("验收记录保护上限已达到")
	}
	return s.commit(ctx, a, InterventionKind, id, v.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &b)
}
func (s *Service) SaveAdmission(ctx context.Context, a analytics.Actor, q model.MaintenanceRevisionRequest) (model.AnalysisConfigRevision, error) {
	if v, found, err := s.replay(ctx, a, AdmissionKind, q.ResourceID, q.IdempotencyKey, q); found || err != nil {
		return v, err
	}
	if _, err := s.authorize(ctx, a, "POST /api/v1/maintenance-admissions", q.DeviceIDs); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	if err := s.validateDevices(ctx, a, q.DeviceIDs); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	var b model.MaintenanceAdmissionRecord
	if err := decode(q.Body, &b); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	if strings.TrimSpace(b.FaultType) == "" || len(b.Requests) > 0 {
		return model.AnalysisConfigRevision{}, invalid("准入要求须明确故障类型与参数版本")
	}
	if err := ValidateAdmission(&b.Parameters); err != nil {
		return model.AnalysisConfigRevision{}, invalid(err.Error())
	}
	if b.ProductID != "" {
		catalog, ok := s.Catalog.(interface {
			GetProduct(context.Context, string, string) (model.Product, error)
		})
		if !ok {
			return model.AnalysisConfigRevision{}, analytics.ErrUnsupported
		}
		product, err := catalog.GetProduct(ctx, a.TenantID, b.ProductID)
		if err != nil || product.TenantID != a.TenantID {
			return model.AnalysisConfigRevision{}, analytics.ErrForbidden
		}
		for _, id := range q.DeviceIDs {
			d, err := s.Catalog.GetManagedDevice(ctx, a.TenantID, id)
			if err != nil || d.ProductID != b.ProductID {
				return model.AnalysisConfigRevision{}, analytics.ErrForbidden
			}
		}
	}
	old, err := s.Latest(ctx, a, AdmissionKind, q.ResourceID)
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		return old, err
	}
	if err == nil {
		var prior model.MaintenanceAdmissionRecord
		if decode(old.Body, &prior) != nil {
			return old, model.ErrAnalysisConflict
		}
		if !sameDevices(q.DeviceIDs, old.DeviceIDs) || b.ProductID != prior.ProductID || b.FaultType != prior.FaultType {
			return old, invalid("准入版本不能隐式改变目标范围")
		}
		b.Requests = prior.Requests
	}
	return s.commit(ctx, a, AdmissionKind, q.ResourceID, q.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &b)
}
