package response

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/google/uuid"
	"iot-platform/internal/analytics"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type PlanBody struct {
	Name                string                 `json:"name"`
	ProcedureRevisionID string                 `json:"procedureRevisionId"`
	People              []model.ResponsePerson `json:"people"`
	PlannedStart        int64                  `json:"plannedStart"`
	PlannedEnd          int64                  `json:"plannedEnd"`
	AlarmID             string                 `json:"alarmId,omitempty"`
	AlarmReportEventID  string                 `json:"alarmReportEventId,omitempty"`
	EvidenceStart       int64                  `json:"evidenceStart,omitempty"`
	EvidenceEnd         int64                  `json:"evidenceEnd,omitempty"`
}

func (s *Service) CreateExecution(ctx context.Context, a analytics.Actor, q RevisionRequest, source string) (model.AnalysisConfigRevision, error) {
	path := "POST /api/v1/drills"
	if source == "REAL_CASE" {
		path = "POST /api/v1/response-cases"
	} else if source != "DRILL" {
		return model.AnalysisConfigRevision{}, invalid("执行来源无效")
	}
	if _, err := s.authorize(ctx, a, path, q.DeviceIDs); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	if err := s.validateDevices(ctx, a, q.DeviceIDs); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	if q.ExpectedVersion != 0 {
		return model.AnalysisConfigRevision{}, model.ErrAnalysisConflict
	}
	var b PlanBody
	if err := decode(q.Body, &b); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	if strings.TrimSpace(b.Name) == "" || len(b.People) > 100 || (source == "DRILL" && (b.PlannedStart <= 0 || b.PlannedEnd <= b.PlannedStart)) {
		return model.AnalysisConfigRevision{}, invalid("计划名称、人员或起止时间无效")
	}
	old, err := s.Latest(ctx, a, ExecutionKind, q.ResourceID)
	if err == nil {
		var existing model.ResponseExecution
		_ = json.Unmarshal(old.Body, &existing)
		return s.commit(ctx, a, ExecutionKind, q.ResourceID, old.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &existing)
	}
	if !errors.Is(err, model.ErrNotFound) {
		return old, err
	}
	procedure, err := s.Revision(ctx, a, ProcedureKind, b.ProcedureRevisionID)
	if err != nil {
		return procedure, err
	}
	for _, id := range q.DeviceIDs {
		if !slices.Contains(procedure.DeviceIDs, id) {
			return model.AnalysisConfigRevision{}, analytics.ErrForbidden
		}
	}
	var p model.ResponseProcedure
	_ = json.Unmarshal(procedure.Body, &p)
	if !p.Published {
		return model.AnalysisConfigRevision{}, invalid("计划必须引用已发布流程版本")
	}
	p.Requests = nil
	if s.ValidateStaff == nil && len(b.People) > 0 {
		return model.AnalysisConfigRevision{}, analytics.ErrUnsupported
	}
	seen := map[string]bool{}
	for _, person := range b.People {
		if person.Username == "" || person.Role == "" || seen[person.Username] {
			return model.AnalysisConfigRevision{}, invalid("人员与岗位无效或重复")
		}
		seen[person.Username] = true
		if err = s.ValidateStaff(ctx, a.TenantID, person.Username, q.DeviceIDs); err != nil {
			return model.AnalysisConfigRevision{}, err
		}
	}
	exec := model.ResponseExecution{Name: b.Name, Source: source, Status: "DRAFT", ProcedureRevisionID: procedure.ID, Procedure: p, People: b.People, PlannedStart: b.PlannedStart, PlannedEnd: b.PlannedEnd, Milestones: []model.ResponseMilestone{}, SimulationEvents: []model.ResponseSimulationEvent{}, Confirmations: []model.ResponseConfirmation{}, Requests: []model.ResponseRequestReceipt{}}
	if source == "DRILL" {
		if b.AlarmID != "" || b.AlarmReportEventID != "" {
			return model.AnalysisConfigRevision{}, invalid("演练不自动关联整台设备或聚合告警为测试")
		}
	} else {
		alarm, e := s.Catalog.GetAlarm(ctx, a.TenantID, b.AlarmID)
		if e != nil || !slices.Contains(q.DeviceIDs, alarm.DeviceID) {
			return model.AnalysisConfigRevision{}, analytics.ErrForbidden
		}
		if s.ResolveEvidence == nil {
			return model.AnalysisConfigRevision{}, analytics.ErrUnsupported
		}
		if _, e = s.ResolveEvidence(ctx, a, model.ResponseEvidenceReference{Kind: "ALARM", SourceID: b.AlarmID, DeviceID: alarm.DeviceID}); e != nil {
			return model.AnalysisConfigRevision{}, e
		}
		exec.Status, exec.StartedAt, exec.AlarmID = "RECORDING", s.now(), alarm.ID
		if b.AlarmReportEventID != "" {
			if s.Facts == nil || b.EvidenceStart <= 0 || b.EvidenceEnd <= b.EvidenceStart || b.EvidenceEnd-b.EvidenceStart > s.Analysis.Limits.MaxRange.Milliseconds() {
				return model.AnalysisConfigRevision{}, invalid("单次上报证据区间无效")
			}
			found := false
			e = s.Facts.AnalyticsFactsRead(ctx, a.TenantID, func(reader ports.AnalyticsFactReader) error {
				query := model.FactQuery{DeviceIDs: []string{alarm.DeviceID}, Start: b.EvidenceStart, End: b.EvidenceEnd, Limit: 1000}
				for count := 0; count < 50000; {
					page, e := reader.ListAlarmReportEvents(query)
					if e != nil {
						return e
					}
					for _, event := range page.Items {
						if event.SourceEventID == b.AlarmReportEventID && event.ResourceID == alarm.ID {
							found = true
							return nil
						}
					}
					count += len(page.Items)
					if page.Cursor == "" {
						return nil
					}
					query.Cursor = page.Cursor
				}
				return invalid("单次报警证据超过读取保护上限")
			})
			if e != nil {
				return model.AnalysisConfigRevision{}, e
			}
			if !found {
				return model.AnalysisConfigRevision{}, invalid("单次上报证据与所选告警不匹配")
			}
			canonical, e := s.ResolveEvidence(ctx, a, model.ResponseEvidenceReference{Kind: "ALARM_REPORT", SourceID: b.AlarmReportEventID, DeviceID: alarm.DeviceID})
			if e != nil {
				return model.AnalysisConfigRevision{}, e
			}
			exec.AlarmReportEventID, exec.PlatformReceivedAt = b.AlarmReportEventID, canonical.ReceivedAt
		}
	}
	return s.commit(ctx, a, ExecutionKind, q.ResourceID, q.DeviceIDs, 0, q.IdempotencyKey, q, &exec)
}

func (s *Service) ExecutionAction(ctx context.Context, a analytics.Actor, id string, q ActionRequest) (model.AnalysisConfigRevision, error) {
	old, err := s.Latest(ctx, a, ExecutionKind, id)
	if err != nil {
		return old, err
	}
	var exec model.ResponseExecution
	_ = json.Unmarshal(old.Body, &exec)
	operation := "POST /api/v1/drills/:id/actions"
	if exec.Source == "REAL_CASE" {
		operation = "POST /api/v1/response-runs/:id/actions"
	}
	if _, err = s.authorize(ctx, a, operation, old.DeviceIDs); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	for _, r := range exec.Requests {
		if r.Key == q.IdempotencyKey {
			return s.commit(ctx, a, ExecutionKind, id, old.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &exec)
		}
	}
	if q.ExpectedVersion != old.Version {
		return old, model.ErrAnalysisConflict
	}
	action := strings.ToUpper(q.Action)
	now := s.now()
	switch action {
	case "PUBLISH":
		if exec.Source != "DRILL" || exec.Status != "DRAFT" {
			return old, model.ErrAnalysisConflict
		}
		exec.Status = "PUBLISHED"
	case "START":
		if exec.Source != "DRILL" || exec.Status != "PUBLISHED" {
			return old, model.ErrAnalysisConflict
		}
		if len(exec.People) == 0 {
			return old, invalid("开始前必须明确实际人员")
		}
		for _, person := range exec.People {
			if s.ValidateStaff == nil {
				return old, analytics.ErrUnsupported
			}
			if err = s.ValidateStaff(ctx, a.TenantID, person.Username, old.DeviceIDs); err != nil {
				return old, err
			}
		}
		exec.Status, exec.StartedAt = "RUNNING", now
	case "END":
		if !slices.Contains([]string{"RUNNING", "RECORDING"}, exec.Status) {
			return old, model.ErrAnalysisConflict
		}
		exec.Status, exec.EndedAt = "ENDED", now
	case "CANCEL":
		if exec.Source != "DRILL" || !slices.Contains([]string{"DRAFT", "PUBLISHED", "RUNNING"}, exec.Status) || strings.TrimSpace(q.Reason) == "" {
			return old, invalid("取消须处于可取消状态并填写原因")
		}
		exec.Status, exec.EndedAt = "CANCELLED", now
	case "ARCHIVE":
		if !slices.Contains([]string{"ENDED", "REVIEWED", "CANCELLED"}, exec.Status) {
			return old, model.ErrAnalysisConflict
		}
		exec.Status = "ARCHIVED"
	default:
		return old, invalid("不支持的执行状态操作")
	}
	exec.Actions = append(exec.Actions, model.ResponseActionEvent{Action: action, Reason: q.Reason, Actor: a.Username, RecordedAt: now})
	return s.commit(ctx, a, ExecutionKind, id, old.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &exec)
}

type MilestoneRequest struct {
	ExpectedVersion int64                             `json:"expectedVersion"`
	IdempotencyKey  string                            `json:"idempotencyKey"`
	StepID          string                            `json:"stepId"`
	OccurredAt      int64                             `json:"occurredAt"`
	Executor        string                            `json:"executor"`
	Status          string                            `json:"status"`
	Evidence        []model.ResponseEvidenceReference `json:"evidence"`
	Explanation     string                            `json:"explanation"`
	CorrectsID      string                            `json:"correctsId,omitempty"`
}

func (s *Service) AppendMilestone(ctx context.Context, a analytics.Actor, id string, q MilestoneRequest) (model.AnalysisConfigRevision, error) {
	old, err := s.Latest(ctx, a, ExecutionKind, id)
	if err != nil {
		return old, err
	}
	operation := "POST /api/v1/response-runs/:id/milestones"
	if q.CorrectsID != "" {
		operation = "POST /api/v1/response-runs/:id/corrections"
	}
	if _, err = s.authorize(ctx, a, operation, old.DeviceIDs); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	var exec model.ResponseExecution
	_ = json.Unmarshal(old.Body, &exec)
	for _, r := range exec.Requests {
		if r.Key == q.IdempotencyKey {
			return s.commit(ctx, a, ExecutionKind, id, old.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &exec)
		}
	}
	if old.Version != q.ExpectedVersion {
		return old, model.ErrAnalysisConflict
	}
	if slices.Contains([]string{"DRAFT", "PUBLISHED", "CANCELLED", "ARCHIVED"}, exec.Status) {
		return old, model.ErrAnalysisConflict
	}
	if len(exec.Milestones) >= 5000 || q.OccurredAt <= 0 || q.OccurredAt > s.now() || q.Executor == "" || q.Explanation == "" || !slices.Contains([]string{"UNVERIFIED", "CONFIRMED", "DISPUTED"}, q.Status) || len(q.Evidence) > 20 {
		return old, invalid("节点、时间、人员、依据或确认状态无效")
	}
	stepFound := false
	for _, step := range exec.Procedure.Steps {
		if step.ID == q.StepID {
			if step.SystemEventType != "" {
				return old, invalid("系统节点只允许关联已提交的系统事件，不能用人工登记替代实际操作时钟")
			}
			stepFound = true
		}
	}
	if !stepFound {
		return old, invalid("节点不属于开始时固定流程")
	}
	if s.ValidateStaff == nil {
		return old, analytics.ErrUnsupported
	}
	if err = s.ValidateStaff(ctx, a.TenantID, q.Executor, old.DeviceIDs); err != nil {
		return old, err
	}
	if q.CorrectsID != "" {
		found := false
		for _, m := range exec.Milestones {
			if m.ID == q.CorrectsID && m.StepID == q.StepID {
				found = true
			}
			if m.CorrectsID == q.CorrectsID {
				return old, model.ErrAnalysisConflict
			}
		}
		if !found {
			return old, invalid("更正来源节点不存在或不属于本步骤")
		}
	}
	milestone := model.ResponseMilestone{ID: uuid.NewString(), StepID: q.StepID, OccurredAt: q.OccurredAt, RecordedAt: s.now(), Executor: q.Executor, Recorder: a.Username, Source: "MANUAL", Status: q.Status, Evidence: []model.ResponseEvidenceReference{}, Explanation: q.Explanation, CorrectsID: q.CorrectsID}
	for _, input := range q.Evidence {
		if !slices.Contains(old.DeviceIDs, input.DeviceID) {
			return old, analytics.ErrForbidden
		}
		if input.Kind == "MANUAL_RECORD" {
			if strings.TrimSpace(input.Description) == "" {
				return old, invalid("现场记录必须填写依据")
			}
			input.ID, input.SourceID = uuid.NewString(), milestone.ID
			input.OccurredAt, input.RecordedAt, input.ReceivedAt = milestone.OccurredAt, milestone.RecordedAt, 0
			input.Classification, input.EventType, input.ResourceID = "UNCLASSIFIED", "", ""
			input.ResourceID, input.EventType = "", ""
		} else {
			if s.ResolveEvidence == nil {
				return old, analytics.ErrUnsupported
			}
			input, err = s.ResolveEvidence(ctx, a, input)
			if err != nil {
				return old, err
			}
		}
		milestone.Evidence = append(milestone.Evidence, input)
	}
	exec.Milestones = append(exec.Milestones, milestone)
	return s.commit(ctx, a, ExecutionKind, id, old.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &exec)
}

type SimulationRequest struct {
	ExpectedVersion int64  `json:"expectedVersion"`
	IdempotencyKey  string `json:"idempotencyKey"`
	DeviceID        string `json:"deviceId"`
	OccurredAt      int64  `json:"occurredAt"`
	Content         string `json:"content"`
}

func (s *Service) AppendSimulation(ctx context.Context, a analytics.Actor, id string, q SimulationRequest) (model.AnalysisConfigRevision, error) {
	old, err := s.Latest(ctx, a, ExecutionKind, id)
	if err != nil {
		return old, err
	}
	if _, err = s.authorize(ctx, a, "POST /api/v1/drills/:id/simulation-events", old.DeviceIDs); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	var exec model.ResponseExecution
	_ = json.Unmarshal(old.Body, &exec)
	for _, r := range exec.Requests {
		if r.Key == q.IdempotencyKey {
			return s.commit(ctx, a, ExecutionKind, id, old.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &exec)
		}
	}
	if old.Version != q.ExpectedVersion || exec.Source != "DRILL" || exec.Status != "RUNNING" {
		return old, model.ErrAnalysisConflict
	}
	if !slices.Contains(old.DeviceIDs, q.DeviceID) || q.OccurredAt <= 0 || q.OccurredAt > s.now() || q.Content == "" || len(q.Content) > 5000 || len(exec.SimulationEvents) >= 5000 {
		return old, invalid("模拟记录范围、时间或内容无效")
	}
	exec.SimulationEvents = append(exec.SimulationEvents, model.ResponseSimulationEvent{ID: uuid.NewString(), DeviceID: q.DeviceID, OccurredAt: q.OccurredAt, RecordedAt: s.now(), Content: q.Content, Source: "EXERCISE", Recorder: a.Username})
	return s.commit(ctx, a, ExecutionKind, id, old.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &exec)
}
