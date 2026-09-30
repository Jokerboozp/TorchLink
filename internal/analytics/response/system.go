package response

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/google/uuid"
	"iot-platform/internal/analytics"
	"iot-platform/internal/model"
)

type SystemMilestoneRequest struct {
	ExpectedVersion int64  `json:"expectedVersion"`
	IdempotencyKey  string `json:"idempotencyKey"`
	StepID          string `json:"stepId"`
	SourceEventID   string `json:"sourceEventId"`
	DeviceID        string `json:"deviceId"`
}

func (s *Service) AppendSystemMilestone(ctx context.Context, a analytics.Actor, id string, q SystemMilestoneRequest) (model.AnalysisConfigRevision, error) {
	old, err := s.Latest(ctx, a, ExecutionKind, id)
	if err != nil {
		return old, err
	}
	if _, err = s.authorize(ctx, a, "POST /api/v1/response-runs/:id/system-milestones", old.DeviceIDs); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	var exec model.ResponseExecution
	_ = json.Unmarshal(old.Body, &exec)
	for _, receipt := range exec.Requests {
		if receipt.Key == q.IdempotencyKey {
			return s.commit(ctx, a, ExecutionKind, id, old.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &exec)
		}
	}
	if old.Version != q.ExpectedVersion || !slices.Contains([]string{"RUNNING", "RECORDING", "ENDED", "REVIEWED"}, exec.Status) {
		return old, model.ErrAnalysisConflict
	}
	if q.SourceEventID == "" || !slices.Contains(old.DeviceIDs, q.DeviceID) || len(exec.Milestones) >= 5000 {
		return old, invalid("系统节点证据或范围无效")
	}
	var step model.ResponseStep
	for _, v := range exec.Procedure.Steps {
		if v.ID == q.StepID {
			step = v
		}
	}
	if step.SystemEventType == "" {
		return old, invalid("固定流程未定义该系统节点类型，不能从点击推断现场动作")
	}
	if s.ResolveEvidence == nil {
		return old, analytics.ErrUnsupported
	}
	evidence, err := s.ResolveEvidence(ctx, a, model.ResponseEvidenceReference{Kind: "ALARM_LIFECYCLE", SourceID: q.SourceEventID, DeviceID: q.DeviceID})
	if err != nil {
		return old, err
	}
	if evidence.Kind != "ALARM_LIFECYCLE" || evidence.EventType != step.SystemEventType || evidence.OccurredAt <= 0 || evidence.RecordedAt <= 0 || evidence.DeviceID != q.DeviceID {
		return old, invalid("系统节点类型或真实时间证据不符")
	}
	if exec.Source == "REAL_CASE" && evidence.ResourceID != exec.AlarmID {
		return old, invalid("系统证据不属于本案例的生产告警")
	}
	for _, milestone := range exec.Milestones {
		if milestone.StepID == q.StepID && milestone.Source == "SYSTEM" {
			for _, v := range milestone.Evidence {
				if v.SourceID == q.SourceEventID {
					return old, model.ErrAnalysisConflict
				}
			}
		}
	}
	exec.Milestones = append(exec.Milestones, model.ResponseMilestone{ID: uuid.NewString(), StepID: q.StepID, OccurredAt: evidence.OccurredAt, RecordedAt: s.now(), Executor: "platform", Recorder: a.Username, Source: "SYSTEM", Status: "CONFIRMED", Evidence: []model.ResponseEvidenceReference{evidence}, Explanation: "由已提交的生产告警事务事件建立，只证明该系统操作"})
	return s.commit(ctx, a, ExecutionKind, id, old.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &exec)
}
