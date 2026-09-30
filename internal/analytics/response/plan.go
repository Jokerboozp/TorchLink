package response

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"github.com/google/uuid"

	"iot-platform/internal/analytics"
	"iot-platform/internal/model"
)

// UpdatePlan changes only a draft. The old scope and new scope are both
// authorized; an existing published/running procedure snapshot is immutable.
func (s *Service) UpdatePlan(ctx context.Context, a analytics.Actor, id string, q RevisionRequest) (model.AnalysisConfigRevision, error) {
	old, err := s.Latest(ctx, a, ExecutionKind, id)
	if err != nil {
		return old, err
	}
	operation := "PUT /api/v1/drills/:id"
	if _, err = s.authorize(ctx, a, operation, old.DeviceIDs); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	if _, err = s.authorize(ctx, a, operation, q.DeviceIDs); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	var exec model.ResponseExecution
	_ = json.Unmarshal(old.Body, &exec)
	q.ResourceID = id
	for _, receipt := range exec.Requests {
		if receipt.Key == q.IdempotencyKey {
			return s.commit(ctx, a, ExecutionKind, id, q.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &exec)
		}
	}
	if exec.Source != "DRILL" || exec.Status != "DRAFT" || old.Version != q.ExpectedVersion {
		return old, model.ErrAnalysisConflict
	}
	if err = s.validateDevices(ctx, a, q.DeviceIDs); err != nil {
		return old, err
	}
	var body PlanBody
	if err = decode(q.Body, &body); err != nil {
		return old, err
	}
	if strings.TrimSpace(body.Name) == "" || body.PlannedStart <= 0 || body.PlannedEnd <= body.PlannedStart || body.AlarmID != "" || body.AlarmReportEventID != "" || len(body.People) > 100 {
		return old, invalid("计划名称、起止时间、人员或来源无效")
	}
	procedure, err := s.Revision(ctx, a, ProcedureKind, body.ProcedureRevisionID)
	if err != nil {
		return old, err
	}
	for _, id := range q.DeviceIDs {
		if !slices.Contains(procedure.DeviceIDs, id) {
			return old, analytics.ErrForbidden
		}
	}
	var p model.ResponseProcedure
	_ = json.Unmarshal(procedure.Body, &p)
	if !p.Published {
		return old, invalid("计划必须引用已发布流程")
	}
	p.Requests = nil
	seen := map[string]bool{}
	for _, person := range body.People {
		if person.Username == "" || person.Role == "" || seen[person.Username] {
			return old, invalid("人员与岗位无效或重复")
		}
		seen[person.Username] = true
		if s.ValidateStaff == nil {
			return old, analytics.ErrUnsupported
		}
		if err = s.ValidateStaff(ctx, a.TenantID, person.Username, q.DeviceIDs); err != nil {
			return old, err
		}
	}
	exec.Name = body.Name
	exec.ProcedureRevisionID = procedure.ID
	exec.Procedure = p
	exec.People = body.People
	exec.PlannedStart = body.PlannedStart
	exec.PlannedEnd = body.PlannedEnd
	exec.Actions = append(exec.Actions, model.ResponseActionEvent{Action: "UPDATE_PLAN", Actor: a.Username, RecordedAt: s.now()})
	return s.commit(ctx, a, ExecutionKind, id, q.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &exec)
}

type SampleAssociationRequest struct {
	ExpectedVersion    int64                             `json:"expectedVersion"`
	IdempotencyKey     string                            `json:"idempotencyKey"`
	SourceKind         string                            `json:"sourceKind"`
	SourceID           string                            `json:"sourceId"`
	DeviceID           string                            `json:"deviceId"`
	Classification     string                            `json:"classification"`
	Status             string                            `json:"status"`
	ContainsProduction bool                              `json:"containsProduction"`
	Basis              string                            `json:"basis"`
	Evidence           []model.ResponseEvidenceReference `json:"evidence"`
}

func (s *Service) AssociateSample(ctx context.Context, a analytics.Actor, id string, q SampleAssociationRequest) (model.AnalysisConfigRevision, error) {
	old, err := s.Latest(ctx, a, ExecutionKind, id)
	if err != nil {
		return old, err
	}
	if _, err = s.authorize(ctx, a, "POST /api/v1/response-runs/:id/sample-associations", old.DeviceIDs); err != nil {
		return old, err
	}
	var exec model.ResponseExecution
	_ = json.Unmarshal(old.Body, &exec)
	for _, receipt := range exec.Requests {
		if receipt.Key == q.IdempotencyKey {
			return s.commit(ctx, a, ExecutionKind, id, old.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &exec)
		}
	}
	if exec.Source != "DRILL" || slices.Contains([]string{"CANCELLED", "ARCHIVED"}, exec.Status) || old.Version != q.ExpectedVersion {
		return old, model.ErrAnalysisConflict
	}
	if !slices.Contains([]string{"RAW_MESSAGE", "ALARM_REPORT"}, q.SourceKind) || q.SourceID == "" || !slices.Contains(old.DeviceIDs, q.DeviceID) || !slices.Contains([]string{"PRODUCTION", "EXERCISE", "TEST", "UNCLASSIFIED"}, q.Classification) || !slices.Contains([]string{"UNVERIFIED", "CONFIRMED", "DISPUTED"}, q.Status) || strings.TrimSpace(q.Basis) == "" || len(q.Basis) > 2000 {
		return old, invalid("关联必须指定单条真实报文或单次上报、核实状态和依据")
	}
	if s.ResolveEvidence == nil {
		return old, analytics.ErrUnsupported
	}
	source, err := s.ResolveEvidence(ctx, a, model.ResponseEvidenceReference{Kind: q.SourceKind, SourceID: q.SourceID, DeviceID: q.DeviceID})
	if err != nil {
		return old, err
	}
	evidence, err := s.resolvedEvidence(ctx, a, old.DeviceIDs, q.Evidence, id+":association:"+q.IdempotencyKey)
	if err != nil {
		return old, err
	}
	if q.Status == "CONFIRMED" && len(evidence) == 0 {
		return old, invalid("确认分类必须提供核实证据")
	}
	classification := "UNCLASSIFIED"
	if q.ContainsProduction {
		classification = "PRODUCTION"
	} else if q.Status == "CONFIRMED" {
		classification = q.Classification
	}
	association := model.ResponseSampleAssociation{ID: uuid.NewString(), Source: source, Classification: classification, Status: q.Status, ContainsProduction: q.ContainsProduction, Basis: q.Basis, Evidence: evidence, Reviewer: a.Username, RecordedAt: s.now()}
	// Every reassessment appends to a new aggregate revision. Prior evaluations
	// and production source records retain their original data.
	exec.SampleAssociations = append(exec.SampleAssociations, association)
	return s.commit(ctx, a, ExecutionKind, id, old.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &exec)
}
