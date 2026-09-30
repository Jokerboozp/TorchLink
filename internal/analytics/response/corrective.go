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
)

type CorrectiveBody struct {
	Title                string   `json:"title"`
	ReviewRevisionID     string   `json:"reviewRevisionId"`
	ReviewFactsHash      string   `json:"reviewFactsHash"`
	FindingID            string   `json:"findingId"`
	Owner                string   `json:"owner"`
	DueAt                int64    `json:"dueAt"`
	RequiredVerification []string `json:"requiredVerification"`
}

func (s *Service) CreateCorrective(ctx context.Context, a analytics.Actor, q RevisionRequest) (model.AnalysisConfigRevision, error) {
	if _, err := s.authorize(ctx, a, "POST /api/v1/corrective-actions", q.DeviceIDs); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	if q.ExpectedVersion != 0 {
		return model.AnalysisConfigRevision{}, model.ErrAnalysisConflict
	}
	old, err := s.Latest(ctx, a, CorrectiveKind, q.ResourceID)
	if err == nil {
		var action model.CorrectiveAction
		_ = json.Unmarshal(old.Body, &action)
		return s.commit(ctx, a, CorrectiveKind, q.ResourceID, old.DeviceIDs, 0, q.IdempotencyKey, q, &action)
	}
	if !errors.Is(err, model.ErrNotFound) {
		return old, err
	}
	var body CorrectiveBody
	if err := decode(q.Body, &body); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	if body.Title == "" || body.Owner == "" || body.DueAt <= 0 || len(body.RequiredVerification) == 0 || len(body.RequiredVerification) > 20 || len(unique(body.RequiredVerification)) != len(body.RequiredVerification) {
		return model.AnalysisConfigRevision{}, invalid("整改名称、负责人、期限或验收项无效")
	}
	run, err := s.Analysis.Get(ctx, a, analytics.KindResponse, body.ReviewRevisionID)
	if err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	snapshot, err := s.Analysis.Snapshot(ctx, a, analytics.KindResponse, run.ID)
	if err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	if snapshot.FactsHash != body.ReviewFactsHash {
		return model.AnalysisConfigRevision{}, model.ErrAnalysisConflict
	}
	for _, id := range q.DeviceIDs {
		if !slices.Contains(run.DeviceIDs, id) {
			return model.AnalysisConfigRevision{}, analytics.ErrForbidden
		}
	}
	var params EvaluationParameters
	if err := decode(run.Parameters, &params); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	input, err := s.Revision(ctx, a, ExecutionKind, params.ExecutionRevisionID)
	if err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	execution, err := s.Latest(ctx, a, ExecutionKind, input.ResourceID)
	if err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	var exec model.ResponseExecution
	_ = json.Unmarshal(execution.Body, &exec)
	confirmed := false
	for _, v := range exec.Confirmations {
		if v.RevisionID == run.ID && v.FactsHash == snapshot.FactsHash {
			confirmed = true
		}
	}
	if !confirmed {
		return model.AnalysisConfigRevision{}, invalid("正式整改必须来自已人工确认的固定复盘")
	}
	found := false
	for offset := 0; ; {
		page, total, e := s.Analysis.Outputs(ctx, a, analytics.KindResponse, run.ID, model.AnalysisFilter{Kind: "findings", Limit: 100, Offset: offset})
		if e != nil {
			return model.AnalysisConfigRevision{}, e
		}
		for _, v := range page {
			if v.ID == body.FindingID && (v.DeviceID == "" || slices.Contains(q.DeviceIDs, v.DeviceID)) {
				found = true
			}
		}
		offset += len(page)
		if offset >= total {
			break
		}
		if offset > 5000 || len(page) == 0 {
			return model.AnalysisConfigRevision{}, invalid("复盘发现超过读取保护范围")
		}
	}
	if !found {
		return model.AnalysisConfigRevision{}, invalid("整改必须关联实际复盘发现")
	}
	if s.ValidateStaff == nil {
		return model.AnalysisConfigRevision{}, analytics.ErrUnsupported
	}
	if err = s.ValidateStaff(ctx, a.TenantID, body.Owner, q.DeviceIDs); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	action := model.CorrectiveAction{Title: body.Title, ReviewRevisionID: run.ID, ReviewFactsHash: snapshot.FactsHash, FindingID: body.FindingID, Owner: body.Owner, DueAt: body.DueAt, Status: "OPEN", RequiredVerification: body.RequiredVerification, Handling: []model.CorrectiveHandling{}, Verifications: []model.CorrectiveVerification{}, FollowUpExecutionIDs: []string{}, Requests: []model.ResponseRequestReceipt{}}
	return s.commit(ctx, a, CorrectiveKind, q.ResourceID, q.DeviceIDs, 0, q.IdempotencyKey, q, &action)
}

type CorrectiveRequest struct {
	ExpectedVersion     int64                             `json:"expectedVersion"`
	IdempotencyKey      string                            `json:"idempotencyKey"`
	Action              string                            `json:"action"`
	Owner               string                            `json:"owner,omitempty"`
	DueAt               int64                             `json:"dueAt,omitempty"`
	Explanation         string                            `json:"explanation"`
	Evidence            []model.ResponseEvidenceReference `json:"evidence"`
	FollowUpExecutionID string                            `json:"followUpExecutionId,omitempty"`
}

func (s *Service) resolvedEvidence(ctx context.Context, a analytics.Actor, devices []string, input []model.ResponseEvidenceReference, manualID string) ([]model.ResponseEvidenceReference, error) {
	if len(input) > 20 {
		return nil, invalid("证据数量超过上限")
	}
	result := []model.ResponseEvidenceReference{}
	for _, e := range input {
		if !slices.Contains(devices, e.DeviceID) {
			return nil, analytics.ErrForbidden
		}
		if e.Kind == "MANUAL_RECORD" {
			if e.Description == "" {
				return nil, invalid("人工证据须有内容")
			}
			e.ID, e.SourceID = uuid.NewString(), manualID
			e.OccurredAt, e.RecordedAt, e.ReceivedAt = 0, s.now(), 0
			e.Classification = "UNCLASSIFIED"
			e.EventType, e.ResourceID = "", ""
		} else {
			if s.ResolveEvidence == nil {
				return nil, analytics.ErrUnsupported
			}
			v, err := s.ResolveEvidence(ctx, a, e)
			if err != nil {
				return nil, err
			}
			e = v
		}
		result = append(result, e)
	}
	return result, nil
}
func (s *Service) CorrectiveAction(ctx context.Context, a analytics.Actor, id string, q CorrectiveRequest) (model.AnalysisConfigRevision, error) {
	old, err := s.Latest(ctx, a, CorrectiveKind, id)
	if err != nil {
		return old, err
	}
	if _, err = s.authorize(ctx, a, "POST /api/v1/corrective-actions/:id/actions", old.DeviceIDs); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	var action model.CorrectiveAction
	_ = json.Unmarshal(old.Body, &action)
	for _, r := range action.Requests {
		if r.Key == q.IdempotencyKey {
			return s.commit(ctx, a, CorrectiveKind, id, old.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &action)
		}
	}
	if old.Version != q.ExpectedVersion || action.Status == "DONE" || action.Status == "CANCELLED" {
		return old, model.ErrAnalysisConflict
	}
	if q.Explanation == "" {
		return old, invalid("状态或责任变更必须填写依据")
	}
	handlingID := uuid.NewString()
	evidence, err := s.resolvedEvidence(ctx, a, old.DeviceIDs, q.Evidence, handlingID)
	if err != nil {
		return old, err
	}
	switch strings.ToUpper(q.Action) {
	case "START":
		if action.Status != "OPEN" {
			return old, model.ErrAnalysisConflict
		}
		action.Status = "IN_PROGRESS"
	case "ASSIGN":
		if q.Owner == "" || s.ValidateStaff == nil {
			return old, invalid("负责人不能为空")
		}
		if err = s.ValidateStaff(ctx, a.TenantID, q.Owner, old.DeviceIDs); err != nil {
			return old, err
		}
		action.Owner = q.Owner
		if q.DueAt > 0 {
			action.DueAt = q.DueAt
		}
	case "RECORD":
		if action.Status != "IN_PROGRESS" {
			return old, model.ErrAnalysisConflict
		}
	case "SUBMIT_VERIFICATION":
		if action.Status != "IN_PROGRESS" || len(evidence) == 0 {
			return old, invalid("提交验收需处理证据")
		}
		action.Status = "PENDING_VERIFICATION"
	case "CANCEL":
		action.Status = "CANCELLED"
	case "LINK_FOLLOW_UP":
		v, e := s.Latest(ctx, a, ExecutionKind, q.FollowUpExecutionID)
		if e != nil {
			return old, e
		}
		for _, device := range old.DeviceIDs {
			if !slices.Contains(v.DeviceIDs, device) {
				return old, analytics.ErrForbidden
			}
		}
		if !slices.Contains(action.FollowUpExecutionIDs, v.ResourceID) {
			action.FollowUpExecutionIDs = append(action.FollowUpExecutionIDs, v.ResourceID)
		}
	default:
		return old, invalid("不支持的整改操作；完成须独立验收")
	}
	action.Handling = append(action.Handling, model.CorrectiveHandling{ID: handlingID, Actor: a.Username, RecordedAt: s.now(), Explanation: q.Explanation, Evidence: evidence})
	return s.commit(ctx, a, CorrectiveKind, id, old.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &action)
}

type VerificationRequest struct {
	ExpectedVersion int64                             `json:"expectedVersion"`
	IdempotencyKey  string                            `json:"idempotencyKey"`
	Result          string                            `json:"result"`
	Items           []string                          `json:"items"`
	Explanation     string                            `json:"explanation"`
	Evidence        []model.ResponseEvidenceReference `json:"evidence"`
}

func (s *Service) VerifyCorrective(ctx context.Context, a analytics.Actor, id string, q VerificationRequest) (model.AnalysisConfigRevision, error) {
	old, err := s.Latest(ctx, a, CorrectiveKind, id)
	if err != nil {
		return old, err
	}
	if _, err = s.authorize(ctx, a, "POST /api/v1/corrective-actions/:id/verifications", old.DeviceIDs); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	var action model.CorrectiveAction
	_ = json.Unmarshal(old.Body, &action)
	for _, r := range action.Requests {
		if r.Key == q.IdempotencyKey {
			return s.commit(ctx, a, CorrectiveKind, id, old.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &action)
		}
	}
	if old.Version != q.ExpectedVersion || action.Status != "PENDING_VERIFICATION" {
		return old, model.ErrAnalysisConflict
	}
	if !slices.Contains([]string{"PASSED", "FAILED"}, q.Result) || q.Explanation == "" {
		return old, invalid("验收结果与依据无效")
	}
	if q.Result == "PASSED" {
		for _, item := range action.RequiredVerification {
			if !slices.Contains(q.Items, item) {
				return old, invalid("规定验收项尚未全部核实")
			}
		}
		if len(q.Evidence) == 0 {
			return old, invalid("通过验收须有证据")
		}
	}
	verificationID := uuid.NewString()
	evidence, err := s.resolvedEvidence(ctx, a, old.DeviceIDs, q.Evidence, verificationID)
	if err != nil {
		return old, err
	}
	action.Status = "IN_PROGRESS"
	if q.Result == "PASSED" {
		action.Status = "DONE"
	}
	action.Verifications = append(action.Verifications, model.CorrectiveVerification{ID: verificationID, Actor: a.Username, RecordedAt: s.now(), Result: q.Result, Items: q.Items, Explanation: q.Explanation, Evidence: evidence})
	return s.commit(ctx, a, CorrectiveKind, id, old.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &action)
}
