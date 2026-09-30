package response

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"iot-platform/internal/analytics"
	"iot-platform/internal/model"
)

type EvaluationParameters struct {
	ExecutionRevisionID string `json:"executionRevisionId"`
	Cutoff              int64  `json:"cutoff"`
}

func (s *Service) Register() error { return s.Analysis.Register(analytics.KindResponse, s.Process) }
func (s *Service) ValidateCreate(ctx context.Context, a analytics.Actor, q *analytics.CreateRequest) error {
	var p EvaluationParameters
	if err := decode(q.Parameters, &p); err != nil {
		return err
	}
	input, err := s.Revision(ctx, a, ExecutionKind, p.ExecutionRevisionID)
	if err != nil {
		return err
	}
	var exec model.ResponseExecution
	_ = json.Unmarshal(input.Body, &exec)
	if !slices.Contains([]string{"RUNNING", "RECORDING", "ENDED", "REVIEWED"}, exec.Status) || exec.StartedAt <= 0 {
		return invalid("执行尚未开始或已归档，不能生成复盘")
	}
	if !slices.Equal(q.DeviceIDs, input.DeviceIDs) {
		return invalid("复盘范围须与固定执行版本一致")
	}
	if q.Start != exec.StartedAt || q.End <= q.Start || q.End > s.now() || (exec.EndedAt > 0 && q.End != exec.EndedAt) {
		return invalid("复盘起止区间须与实际执行一致")
	}
	if p.Cutoff > s.now() {
		return invalid("资料截止时间不能在未来")
	}
	if p.Cutoff == 0 {
		// The default is derived from the immutable input and request interval.
		// Retrying the same request must not acquire a different server clock.
		p.Cutoff = q.End
		for _, milestone := range exec.Milestones {
			p.Cutoff = max(p.Cutoff, milestone.RecordedAt)
		}
		for _, event := range exec.Actions {
			p.Cutoff = max(p.Cutoff, event.RecordedAt)
		}
		if p.Cutoff > s.now() {
			return invalid("固定记录包含晚于当前平台时钟的登记时间")
		}
	}
	if _, err = Evaluate(exec, input.ID, p.Cutoff); err != nil {
		return invalid(err.Error())
	}
	q.ConfigurationVersion = input.Hash
	q.Parameters, _ = json.Marshal(p)
	return nil
}
func (s *Service) Process(ctx context.Context, e *analytics.Execution) error {
	var p EvaluationParameters
	if err := decode(e.Run.Parameters, &p); err != nil {
		return err
	}
	actor := analytics.Actor{TenantID: e.Run.TenantID, Username: e.Run.Creator, Managed: e.Run.CreatorManaged, SessionVersion: e.Run.CreatorSessionVersion, AccessVersion: e.Run.PermissionsVersion}
	input, err := s.Revision(ctx, actor, ExecutionKind, p.ExecutionRevisionID)
	if err != nil {
		return err
	}
	if input.Hash != e.Run.ConfigurationVersion || !slices.Equal(input.DeviceIDs, e.Run.DeviceIDs) {
		return model.ErrAnalysisConflict
	}
	var exec model.ResponseExecution
	_ = json.Unmarshal(input.Body, &exec)
	result, err := Evaluate(exec, input.ID, p.Cutoff)
	if err != nil {
		return invalid(err.Error())
	}
	source := model.AnalysisSourceCoverage{Source: "response_execution_revision", Start: e.Run.Start, End: e.Run.End, Complete: true, ReadAt: p.Cutoff, Version: input.ID, Watermark: input.Hash}
	if !e.Run.InputsFrozen {
		if err = e.Commit(ctx, model.AnalysisBatch{ID: "freeze-response", FreezeInputs: true, InputHashes: []string{input.Hash}, Sources: []model.AnalysisSourceCoverage{source}, DataCutoff: p.Cutoff, Stage: "fixed-evidence", Status: model.AnalysisRunning, Checkpoint: json.RawMessage(`{"next":0}`)}); err != nil {
			return err
		}
	}
	outputs := []model.AnalysisOutput{}
	for _, step := range result.Steps {
		body, _ := json.Marshal(step)
		id := e.Run.ID + "/step/" + step.StepID
		outputs = append(outputs, model.AnalysisOutput{ID: id, Kind: "metrics", Body: body})
		if step.Status != "CONFIRMED" || step.TargetResult == "EXCEEDS_UNIT_TARGET" {
			outputs = append(outputs, model.AnalysisOutput{ID: e.Run.ID + "/finding/" + step.StepID, Kind: "findings", Body: body})
		}
	}
	evidence := []model.AnalysisEvidence{}
	originalEvidence := 0
	for _, milestone := range exec.Milestones {
		if milestone.RecordedAt > p.Cutoff {
			continue
		}
		for _, v := range milestone.Evidence {
			originalEvidence++
			if len(evidence) >= 200 {
				continue
			}
			body, _ := json.Marshal(map[string]any{"stepId": milestone.StepID, "milestoneId": milestone.ID, "recordedAt": milestone.RecordedAt, "source": milestone.Source, "confirmation": milestone.Status, "description": v.Description})
			evidence = append(evidence, model.AnalysisEvidence{ID: e.Run.ID + "/evidence/" + milestone.ID + "/" + v.ID, SourceKind: v.Kind, SourceID: v.SourceID, DeviceID: v.DeviceID, OccurredAt: milestone.OccurredAt, ResourceVersion: input.ID, Summary: body, PermissionCategory: "response", OriginalAvailability: "RECORDED"})
		}
	}
	// The persisted execution keeps complete evidence. The report discloses
	// bounded representative evidence, never equating samples with totals.
	statistics, _ := json.Marshal(map[string]any{"evaluation": result, "originalEvidenceCount": originalEvidence, "representativeEvidenceCount": len(evidence), "algorithmVersion": AlgorithmVersion})
	limitations := slices.Clone(result.Limitations)
	if len(evidence) < originalEvidence {
		limitations = append(limitations, "代表证据最多200条，固定执行版本仍保留全部登记证据")
	}
	if exec.Confirmations != nil && len(exec.Confirmations) > 0 {
		limitations = append(limitations, "执行已有正式复盘；本次重算生成独立版本，不覆盖旧确认")
	}
	type checkpoint struct {
		Next int `json:"next"`
	}
	cp := checkpoint{}
	if e.Run.InputsFrozen && len(e.Run.Checkpoint) > 0 {
		_ = json.Unmarshal(e.Run.Checkpoint, &cp)
	}
	total := len(outputs) + len(evidence)
	for next := cp.Next; next < total; {
		if err = ctx.Err(); err != nil {
			return err
		}
		end := min(total, next+s.Analysis.Limits.BatchSize)
		batch := model.AnalysisBatch{ID: fmt.Sprintf("response-output-%d", next), Stage: "evidence-evaluation", Status: model.AnalysisRunning, Processed: int64(len(result.Steps)), Outputs: []model.AnalysisOutput{}, Evidence: []model.AnalysisEvidence{}}
		for i := next; i < end; i++ {
			if i < len(outputs) {
				batch.Outputs = append(batch.Outputs, outputs[i])
			} else {
				batch.Evidence = append(batch.Evidence, evidence[i-len(outputs)])
			}
		}
		batch.Checkpoint, _ = json.Marshal(checkpoint{end})
		if err = e.Commit(ctx, batch); err != nil {
			return err
		}
		next = end
	}
	snapshot := model.AnalysisSnapshot{ID: e.Run.ID + "/snapshot", DataCutoff: p.Cutoff, InputHashes: []string{input.Hash}, Sources: []model.AnalysisSourceCoverage{source}, InitialStateQuality: "RECORDED_EVIDENCE_ONLY", Statistics: statistics, Limitations: limitations}
	return e.Commit(ctx, model.AnalysisBatch{ID: "response-complete", Stage: "complete", Status: model.AnalysisSucceeded, Processed: int64(len(result.Steps)), Checkpoint: json.RawMessage(`{"completed":true}`), Snapshot: &snapshot})
}

type ConfirmRequest struct {
	ExpectedVersion    int64  `json:"expectedVersion"`
	ExpectedRunVersion int64  `json:"expectedRunVersion"`
	IdempotencyKey     string `json:"idempotencyKey"`
	FactsHash          string `json:"factsHash"`
	Conclusion         string `json:"conclusion"`
}

func (s *Service) ConfirmReview(ctx context.Context, a analytics.Actor, executionID, revisionID string, q ConfirmRequest) (model.AnalysisConfigRevision, error) {
	old, err := s.Latest(ctx, a, ExecutionKind, executionID)
	if err != nil {
		return old, err
	}
	if _, err = s.authorize(ctx, a, "POST /api/v1/response-runs/:id/reviews/:revisionId/confirm", old.DeviceIDs); err != nil {
		return model.AnalysisConfigRevision{}, err
	}
	var exec model.ResponseExecution
	_ = json.Unmarshal(old.Body, &exec)
	for _, receipt := range exec.Requests {
		if receipt.Key == q.IdempotencyKey {
			return s.commit(ctx, a, ExecutionKind, executionID, old.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &exec)
		}
	}
	if q.ExpectedVersion != old.Version || !slices.Contains([]string{"ENDED", "REVIEWED"}, exec.Status) || q.Conclusion == "" {
		return old, model.ErrAnalysisConflict
	}
	run, err := s.Analysis.Get(ctx, a, analytics.KindResponse, revisionID)
	if err != nil {
		return old, err
	}
	if run.Version != q.ExpectedRunVersion || run.Status != model.AnalysisSucceeded {
		return old, model.ErrAnalysisConflict
	}
	var p EvaluationParameters
	if err = decode(run.Parameters, &p); err != nil {
		return old, err
	}
	input, err := s.Revision(ctx, a, ExecutionKind, p.ExecutionRevisionID)
	if err != nil {
		return old, err
	}
	if input.ResourceID != executionID {
		return old, model.ErrNotFound
	}
	snapshot, err := s.Analysis.Snapshot(ctx, a, analytics.KindResponse, run.ID)
	if err != nil {
		return old, err
	}
	if snapshot.FactsHash != q.FactsHash {
		return old, model.ErrAnalysisConflict
	}
	for _, v := range exec.Confirmations {
		if v.RevisionID == revisionID {
			return old, model.ErrAnalysisConflict
		}
	}
	exec.Confirmations = append(exec.Confirmations, model.ResponseConfirmation{RevisionID: revisionID, FactsHash: snapshot.FactsHash, Conclusion: q.Conclusion, Confirmer: a.Username, ConfirmedAt: s.now()})
	exec.Status = "REVIEWED"
	return s.commit(ctx, a, ExecutionKind, executionID, old.DeviceIDs, q.ExpectedVersion, q.IdempotencyKey, q, &exec)
}
