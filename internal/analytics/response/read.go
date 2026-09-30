package response

import (
	"context"
	"encoding/json"
	"errors"

	"iot-platform/internal/analytics"
	"iot-platform/internal/model"
)

// Export reads persisted facts and their exact execution revision. It does not
// rerun evaluation or silently add late milestones to a confirmed report.
func (s *Service) Export(ctx context.Context, a analytics.Actor, revisionID string) (json.RawMessage, error) {
	run, err := s.Analysis.Get(ctx, a, analytics.KindResponse, revisionID)
	if err != nil {
		return nil, err
	}
	if _, err = s.authorize(ctx, a, "GET /api/v1/response-evaluations/:id/export", run.DeviceIDs); err != nil {
		return nil, err
	}
	snapshot, err := s.Analysis.Snapshot(ctx, a, analytics.KindResponse, run.ID)
	if err != nil {
		return nil, err
	}
	var p EvaluationParameters
	if err = decode(run.Parameters, &p); err != nil {
		return nil, err
	}
	input, err := s.Revision(ctx, a, ExecutionKind, p.ExecutionRevisionID)
	if err != nil {
		return nil, err
	}
	var fixed model.ResponseExecution
	_ = json.Unmarshal(input.Body, &fixed)
	fixed.Requests = nil
	fixed.Procedure.Requests = nil
	outputs := []model.AnalysisOutput{}
	for offset := 0; ; {
		page, total, e := s.Analysis.Outputs(ctx, a, analytics.KindResponse, run.ID, model.AnalysisFilter{Limit: 100, Offset: offset})
		if e != nil {
			return nil, e
		}
		outputs = append(outputs, page...)
		offset += len(page)
		if offset >= total {
			break
		}
		if offset > 5000 || len(page) == 0 {
			return nil, invalid("报告明细超过读取保护范围")
		}
	}
	evidence := []model.AnalysisEvidence{}
	for offset := 0; ; {
		page, total, e := s.Analysis.Evidence(ctx, a, analytics.KindResponse, run.ID, model.AnalysisFilter{Limit: 100, Offset: offset})
		if e != nil {
			return nil, e
		}
		evidence = append(evidence, page...)
		offset += len(page)
		if offset >= total {
			break
		}
		if offset > 5000 || len(page) == 0 {
			return nil, invalid("报告证据超过读取保护范围")
		}
	}
	confirmations := []model.ResponseConfirmation{}
	newEvidence := false
	current, err := s.Latest(ctx, a, ExecutionKind, input.ResourceID)
	if err != nil {
		return nil, err
	}
	var latest model.ResponseExecution
	_ = json.Unmarshal(current.Body, &latest)
	for _, v := range latest.Confirmations {
		if v.RevisionID == run.ID && v.FactsHash == snapshot.FactsHash {
			confirmations = append(confirmations, v)
		}
	}
	known := map[string]bool{}
	for _, v := range fixed.Milestones {
		known[v.ID] = true
	}
	for _, v := range latest.Milestones {
		if !known[v.ID] {
			newEvidence = true
		}
	}
	// Internal lease and browser/session details are not report content.
	run.LeaseOwner = ""
	run.LeaseToken = 0
	run.LeaseExpiresAt = 0
	run.CreatorSessionVersion = 0
	run.PermissionsVersion = ""
	run.Checkpoint = nil
	ai := []model.AnalysisAIRevision{}
	aiAccess := "not_configured"
	if s.AI != nil {
		aiAccess = "included"
		for offset := 0; ; {
			page, total, e := s.AI.List(ctx, a, analytics.KindResponse, run.ID, model.AnalysisFilter{Limit: 100, Offset: offset})
			if e != nil {
				if errors.Is(e, analytics.ErrForbidden) {
					ai = nil
					aiAccess = "restricted"
					break
				}
				return nil, e
			}
			for _, job := range page {
				job.LeaseOwner = ""
				job.LeaseToken = 0
				job.LeaseExpiresAt = 0
				job.CreatorSessionVersion = 0
				job.PermissionVersion = ""
				ai = append(ai, job)
			}
			offset += len(page)
			if offset >= total {
				break
			}
			if len(page) == 0 || offset > 5000 {
				return nil, invalid("AI版本超过读取保护范围")
			}
		}
	}
	return json.Marshal(map[string]any{"run": run, "snapshot": snapshot, "fixedExecution": fixed, "outputs": outputs, "evidence": evidence, "ai": ai, "aiAccess": aiAccess, "confirmations": confirmations, "newEvidenceAvailable": newEvidence, "newEvidencePolicy": "新证据需另行生成复盘版本，不覆盖当前固定事实"})
}

// EvaluationRuns retains historical versions of the same business execution.
// Visibility filtering happens before pagination, including complete device
// scope for both the fixed source revision and the analysis result.
func (s *Service) EvaluationRuns(ctx context.Context, a analytics.Actor, id string, filter model.AnalysisFilter) ([]model.AnalysisRun, int, error) {
	if _, err := s.Latest(ctx, a, ExecutionKind, id); err != nil {
		return nil, 0, err
	}
	current, err := s.Analysis.Current(ctx, a)
	if err != nil {
		return nil, 0, err
	}
	result := []model.AnalysisRun{}
	for offset := 0; ; {
		rows, total, err := s.Analysis.Store.ListAnalysisRuns(ctx, a.TenantID, model.AnalysisFilter{Kind: analytics.KindResponse, Limit: 100, Offset: offset})
		if err != nil {
			return nil, 0, err
		}
		for _, run := range rows {
			if !current.Allows(analytics.KindResponse, "", run.DeviceIDs) {
				continue
			}
			var p EvaluationParameters
			if json.Unmarshal(run.Parameters, &p) != nil {
				continue
			}
			input, err := s.Analysis.Store.GetAnalysisConfig(ctx, a.TenantID, p.ExecutionRevisionID)
			if err != nil {
				return nil, 0, err
			}
			if input.Kind != ExecutionKind || input.ResourceID != id || !current.Allows(analytics.KindResponse, "", input.DeviceIDs) {
				continue
			}
			run.LeaseOwner = ""
			run.LeaseToken = 0
			run.LeaseExpiresAt = 0
			run.Checkpoint = nil
			run.PermissionsVersion = ""
			run.CreatorSessionVersion = 0
			result = append(result, run)
		}
		offset += len(rows)
		if offset >= total {
			break
		}
		if len(rows) == 0 || offset > 10000 {
			return nil, 0, invalid("请缩小执行版本筛选范围")
		}
	}
	total := len(result)
	limit, offset := analytics.NormalizeAnalysisPage(filter.Limit, filter.Offset)
	if offset >= total {
		return []model.AnalysisRun{}, total, nil
	}
	return result[offset:min(total, offset+limit)], total, nil
}
