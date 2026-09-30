package maintenance

import (
	"context"
	"errors"
	"slices"
	"strings"

	"iot-platform/internal/analytics"
	"iot-platform/internal/model"
)

func (s *Service) SaveContext(ctx context.Context, a analytics.Actor, q model.MaintenanceRevisionRequest) (model.AnalysisConfigRevision, error) {
	if v, found, err := s.replay(ctx, a, ContextKind, q.ResourceID, q.IdempotencyKey, q); found || err != nil {
		return v, err
	}
	if _, err := s.authorize(ctx, a, "POST /api/v1/maintenance-contexts", q.DeviceIDs); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	if err := s.validateDevices(ctx, a, q.DeviceIDs); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	var b model.OperatingContext
	if err := decode(q.Body, &b); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	if b.Start <= 0 || b.End <= b.Start || b.End-b.Start > s.Analysis.Limits.MaxRange.Milliseconds() || !slices.Contains([]string{"OPERATING_INTENSITY", "PLANNED_STOP", "CONFIRMED_TEST", "PLATFORM_OBSERVATION_UNAVAILABLE", "CONSTRUCTION", "RULE_CHANGE", "PROTOCOL_CHANGE", "GATEWAY_CHANGE", "POINT_TABLE_CHANGE", "REPORT_PERIOD_CHANGE"}, b.Kind) || strings.TrimSpace(b.Reason) == "" || b.Confirmer != "" || b.ConfirmedAt != 0 || len(b.Requests) > 0 || b.Kind == "OPERATING_INTENSITY" && strings.TrimSpace(b.Intensity) == "" {
		return model.AnalysisConfigRevision{}, invalid("工况须记录明确类型、区间、依据和强度，确认人由服务端绑定")
	}
	if b.Confirmed && len(b.Evidence) == 0 {
		return model.AnalysisConfigRevision{}, invalid("已确认工况须有证据；无报文不证明平台观测不可用")
	}
	evidence, err := s.canonicalEvidence(ctx, a, b.Evidence, q.DeviceIDs)
	if err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	b.Evidence = evidence
	if b.Confirmed {
		b.Confirmer = a.Username
		b.ConfirmedAt = s.now()
	}
	old, err := s.Latest(ctx, a, ContextKind, q.ResourceID)
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		return old, err
	}
	if err == nil {
		var prior model.OperatingContext
		if decode(old.Body, &prior) != nil {
			return old, model.ErrAnalysisConflict
		}
		if !sameDevices(old.DeviceIDs, q.DeviceIDs) {
			return old, invalid("工况范围不得隐式改变")
		}
		b.Requests = prior.Requests
	}
	return s.commit(ctx, a, ContextKind, q.ResourceID, q.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &b)
}
func (s *Service) SaveFaultAssessment(ctx context.Context, a analytics.Actor, q model.MaintenanceRevisionRequest) (model.AnalysisConfigRevision, error) {
	if v, found, err := s.replay(ctx, a, FaultAssessmentKind, q.ResourceID, q.IdempotencyKey, q); found || err != nil {
		return v, err
	}
	if _, err := s.authorize(ctx, a, "POST /api/v1/maintenance-fault-cycles", q.DeviceIDs); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	var b model.MaintenanceFaultAssessment
	if err := decode(q.Body, &b); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	if len(q.DeviceIDs) != 1 || q.DeviceIDs[0] != b.DeviceID || b.AlarmID == "" || !slices.Contains([]string{"FAULT", "FIRE", "OTHER"}, b.Type) || !slices.Contains([]string{"PRODUCTION", "TEST", "UNKNOWN"}, b.Classification) || strings.TrimSpace(b.Basis) == "" || b.Status != "" && b.Status != "DRAFT" || b.FaultCode != "" || b.ConfirmedBy != "" || b.ConfirmedAt != 0 || len(b.Requests) > 0 {
		return model.AnalysisConfigRevision{}, invalid("须选择稳定生产告警周期，并明确故障/真实火警/测试及核实依据")
	}
	if s.Catalog == nil {
		return model.AnalysisConfigRevision{}, analytics.ErrUnsupported
	}
	alarm, err := s.Catalog.GetAlarm(ctx, a.TenantID, b.AlarmID)
	if err != nil || alarm.TenantID != a.TenantID || alarm.DeviceID != b.DeviceID || alarm.ComponentID != b.ComponentID {
		return model.AnalysisConfigRevision{}, analytics.ErrForbidden
	}
	if b.Type == "FAULT" && slices.Contains([]string{"FIRE", "SMOKE", "HEAT_FIRE"}, alarm.AlarmType) {
		return model.AnalysisConfigRevision{}, invalid("真实火警不能作为维护故障次数")
	}
	b.Status = "DRAFT"
	b.FaultCode = alarm.AlarmType
	evidence, err := s.canonicalEvidence(ctx, a, b.Evidence, q.DeviceIDs)
	if err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	b.Evidence = evidence
	old, err := s.Latest(ctx, a, FaultAssessmentKind, q.ResourceID)
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		return old, err
	}
	if err == nil {
		var prior model.MaintenanceFaultAssessment
		if decode(old.Body, &prior) != nil {
			return old, model.ErrAnalysisConflict
		}
		if prior.AlarmID != b.AlarmID || !sameDevices(q.DeviceIDs, old.DeviceIDs) {
			return old, invalid("同一故障核实资源不能改绑周期")
		}
		b.Requests = prior.Requests
	}
	return s.commit(ctx, a, FaultAssessmentKind, q.ResourceID, q.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &b)
}
func (s *Service) ConfirmFault(ctx context.Context, a analytics.Actor, id string, q model.MaintenanceActionRequest) (model.AnalysisConfigRevision, error) {
	old, err := s.Latest(ctx, a, FaultAssessmentKind, id)
	if err != nil {
		return old, err
	}
	if _, err = s.authorize(ctx, a, "POST /api/v1/maintenance-fault-cycles/:id/confirm", old.DeviceIDs); err != nil {
		return old, err
	}
	var b model.MaintenanceFaultAssessment
	if err = decode(old.Body, &b); err != nil {
		return old, err
	}
	for _, r := range b.Requests {
		if r.Key == q.IdempotencyKey {
			return s.commit(ctx, a, FaultAssessmentKind, id, old.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &b)
		}
	}
	if old.Version != q.ExpectedVersion || b.Status != "DRAFT" {
		return old, model.ErrAnalysisConflict
	}
	b.Status = "CONFIRMED"
	b.ConfirmedBy = a.Username
	b.ConfirmedAt = s.now()
	return s.commit(ctx, a, FaultAssessmentKind, id, old.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &b)
}
