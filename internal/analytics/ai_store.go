package analytics

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

var _ ports.AnalysisAIStore = (*Store)(nil)

func saveAI(tx StorageTx, v model.AnalysisAIRevision, expected int64) error {
	d, err := bodyDocument("ai", v.ID, v.TenantID, v)
	if err != nil {
		return err
	}
	d.RunID, d.DeviceIDs, d.ApplicationKind, d.Status, d.CreatedAt = v.RunID, v.DeviceIDs, v.WorkflowID, v.Status, v.CreatedAt
	return save(tx, d, expected)
}
func aiSnapshot(tx StorageTx, v model.AnalysisAIRevision) (model.AnalysisRun, error) {
	r, err := load[model.AnalysisRun](tx, "run", v.RunID)
	if err != nil {
		return r, err
	}
	if r.Kind == KindRuleLab {
		var p model.RuleLabRunParameters
		if json.Unmarshal(r.Parameters, &p) != nil || p.Phase != "EXPERIMENT" || p.ExperimentRevisionID == "" {
			return r, model.ErrAnalysisInvalid
		}
	}
	if r.Kind == KindResponse || r.Kind == KindMaintenance || r.Kind == KindInvestment {
		var p struct {
			ExecutionRevisionID string `json:"executionRevisionId"`
			ScenarioRevisionID  string `json:"scenarioRevisionId"`
			UseFinance          bool   `json:"useFinance"`
			Observation         struct {
				InterventionRevisionID string `json:"interventionRevisionId"`
			} `json:"observation"`
		}
		if json.Unmarshal(r.Parameters, &p) != nil {
			return r, model.ErrAnalysisInvalid
		}
		sourceID, sourceKind := p.ExecutionRevisionID, "RESPONSE_EXECUTION"
		if r.Kind == KindMaintenance {
			sourceID, sourceKind = p.Observation.InterventionRevisionID, "MAINTENANCE_RECORD"
		}
		if r.Kind == KindInvestment {
			sourceID, sourceKind = p.ScenarioRevisionID, "INVESTMENT_SCENARIO"
		}
		if sourceID == "" {
			return r, model.ErrAnalysisInvalid
		}
		source, err := load[model.AnalysisConfigRevision](tx, "config", sourceID)
		if err != nil {
			return r, err
		}
		if source.Kind != sourceKind || !slices.Equal(source.DeviceIDs, r.DeviceIDs) {
			return r, model.ErrAnalysisConflict
		}
		if r.Kind == KindResponse && source.Hash != r.ConfigurationVersion {
			return r, model.ErrAnalysisConflict
		}
		if r.Kind != KindResponse && p.UseFinance != slices.Contains(r.RequiredPermissions, FinanceReadOperation) {
			return r, model.ErrAnalysisConflict
		}
		if r.Kind == KindInvestment {
			var scenario struct {
				UseFinance bool `json:"useFinance"`
			}
			if json.Unmarshal(source.Body, &scenario) != nil || scenario.UseFinance != p.UseFinance {
				return r, model.ErrAnalysisConflict
			}
		}
	}
	if (r.Status != model.AnalysisSucceeded && r.Status != model.AnalysisPartial) || r.SnapshotID != v.SnapshotID || r.Kind != v.Kind {
		return r, model.ErrAnalysisConflict
	}
	snap, err := load[model.AnalysisSnapshot](tx, "snapshot", v.SnapshotID)
	if err != nil {
		return r, err
	}
	if snap.RunID != r.ID || snap.Version != v.SnapshotVersion || !slices.Equal(snap.DeviceIDs, v.DeviceIDs) || !slices.Equal(r.DeviceIDs, v.DeviceIDs) {
		return r, model.ErrAnalysisConflict
	}
	return r, nil
}
func aiLease(v model.AnalysisAIRevision, token, now int64) error {
	if token <= 0 || v.LeaseToken != token || v.Status != model.AnalysisRunning || v.LeaseExpiresAt <= now || v.Deadline <= now {
		return model.ErrAnalysisLeaseLost
	}
	return nil
}
func fenceAI(tx StorageTx, v model.AnalysisAIRevision) {
	if fence, ok := tx.(interface{ SetLeaseDeadline(int64) }); ok {
		fence.SetLeaseDeadline(min(v.LeaseExpiresAt, v.Deadline))
	}
}

func (s *Store) CreateAnalysisAIRevision(ctx context.Context, v model.AnalysisAIRevision, expectedRun int64, queueLimit int) (out model.AnalysisAIRevision, err error) {
	if v.ID == "" || v.TenantID == "" || v.RunID == "" || v.SnapshotID == "" || v.SnapshotVersion <= 0 || v.WorkflowID == "" || v.PromptVersion == "" || v.Creator == "" || v.PermissionVersion == "" || v.IdempotencyKey == "" || expectedRun <= 0 {
		return out, model.ErrAnalysisInvalid
	}
	if v.DeviceIDs, err = explicitDevices(v.DeviceIDs); err != nil {
		return out, err
	}
	if queueLimit <= 0 {
		queueLimit = 100
	}
	key, _ := AnalysisHash(struct{ Workflow, Creator, Key string }{v.WorkflowID, v.Creator, v.IdempotencyKey})
	hash, _ := AnalysisHash(struct {
		Run, Snapshot, Workflow, Prompt, Actor, Permission string
		SnapshotVersion, Expected, Session                 int64
		Knowledge, Reinterpret, Managed                    bool
		Devices                                            []string
	}{v.RunID, v.SnapshotID, v.WorkflowID, v.PromptVersion, v.Creator, v.PermissionVersion, v.SnapshotVersion, expectedRun, v.CreatorSessionVersion, v.UseKnowledge, v.Reinterpret, v.CreatorManaged, v.DeviceIDs})
	err = s.backend.Transaction(ctx, v.TenantID, func(tx StorageTx) error {
		if d, e := tx.Get("ai-key", key); e == nil {
			var ref struct{ ID, Hash string }
			if e = json.Unmarshal(d.Body, &ref); e != nil {
				return e
			}
			if ref.Hash != hash {
				return model.ErrAnalysisConflict
			}
			out, e = load[model.AnalysisAIRevision](tx, "ai", ref.ID)
			return e
		} else if e != model.ErrNotFound {
			return e
		}
		remember := func(id string) error {
			d, _ := bodyDocument("ai-key", key, v.TenantID, struct{ ID, Hash string }{id, hash})
			d.RunID, d.DeviceIDs = v.RunID, v.DeviceIDs
			return save(tx, d, 0)
		}
		r, e := aiSnapshot(tx, v)
		if e != nil {
			return e
		}
		if r.Version != expectedRun {
			return model.ErrAnalysisConflict
		}
		active, _, e := tx.List("ai", model.AnalysisFilter{RunID: v.RunID, Statuses: []string{model.AnalysisQueued, model.AnalysisRunning}, Limit: 100})
		if e != nil {
			return e
		}
		for _, d := range active {
			var old model.AnalysisAIRevision
			if e = json.Unmarshal(d.Body, &old); e != nil {
				return e
			}
			if old.SnapshotID == v.SnapshotID && old.WorkflowID == v.WorkflowID && old.Creator == v.Creator {
				if old.PermissionVersion != v.PermissionVersion || old.UseKnowledge != v.UseKnowledge {
					return model.ErrAnalysisConflict
				}
				out = old
				return remember(old.ID)
			}
		}
		if !v.Reinterpret {
			var latest *model.AnalysisAIRevision
			for offset := 0; ; {
				docs, total, e := tx.List("ai", model.AnalysisFilter{RunID: v.RunID, Statuses: []string{model.AnalysisSucceeded}, Limit: 100, Offset: offset})
				if e != nil {
					return e
				}
				for _, d := range docs {
					var old model.AnalysisAIRevision
					if e = json.Unmarshal(d.Body, &old); e != nil {
						return e
					}
					if old.SnapshotID == v.SnapshotID && old.WorkflowID == v.WorkflowID && old.Creator == v.Creator && old.PermissionVersion == v.PermissionVersion && old.UseKnowledge == v.UseKnowledge {
						copyOld := old
						latest = &copyOld
					}
				}
				offset += len(docs)
				if offset >= total {
					break
				}
			}
			if latest != nil {
				out = *latest
				return remember(latest.ID)
			}
		}
		_, n, e := tx.List("ai", model.AnalysisFilter{Kind: v.WorkflowID, Statuses: []string{model.AnalysisQueued, model.AnalysisRunning}, Limit: 1})
		if e != nil {
			return e
		}
		if n >= queueLimit {
			return model.ErrAnalysisQueueFull
		}
		v.Version, v.Status, v.CreatedAt = 1, model.AnalysisQueued, s.timestamp(tx)
		v.HarnessRunID = "analysis_ai_" + uuid.NewString()
		v.Interpretation, v.Coverage = nil, nil
		v.FactIDs, v.SentFactIDs = []string{}, []string{}
		v.Error, v.LeaseOwner = "", ""
		v.LeaseToken, v.LeaseExpiresAt, v.StartedAt, v.CompletedAt, v.Deadline = 0, 0, 0, 0, 0
		if e = saveAI(tx, v, 0); e != nil {
			return e
		}
		d, _ := bodyDocument("ai-key", key, v.TenantID, struct{ ID, Hash string }{v.ID, hash})
		d.RunID, d.DeviceIDs = v.RunID, v.DeviceIDs
		if e = save(tx, d, 0); e != nil {
			return e
		}
		out = v
		return nil
	})
	return
}

func (s *Store) ClaimAnalysisAIRevision(ctx context.Context, owner string, lease, maxDuration time.Duration, workflows []string) (out model.AnalysisAIRevision, err error) {
	if owner == "" || lease < time.Millisecond || maxDuration < time.Millisecond {
		return out, model.ErrAnalysisInvalid
	}
	if len(workflows) == 0 {
		return out, model.ErrNotFound
	}
	tenants, err := s.backend.WorkTenants(ctx, workflows)
	if err != nil {
		return out, err
	}
	for _, tenant := range tenants {
		found := false
		err = s.backend.Transaction(ctx, tenant, func(tx StorageTx) error {
			docs := []StorageDocument{}
			for offset := 0; ; {
				page, total, e := tx.List("ai", model.AnalysisFilter{Statuses: []string{model.AnalysisQueued, model.AnalysisRunning}, Limit: 100, Offset: offset})
				if e != nil {
					return e
				}
				docs = append(docs, page...)
				offset += len(page)
				if offset >= total {
					break
				}
			}
			now := s.timestamp(tx)
			for _, d := range docs {
				var v model.AnalysisAIRevision
				if e := json.Unmarshal(d.Body, &v); e != nil {
					return e
				}
				if !slices.Contains(workflows, v.WorkflowID) {
					continue
				}
				if v.Status == model.AnalysisRunning {
					if v.LeaseExpiresAt <= now || v.Deadline <= now {
						old := v.Version
						v.Version++
						v.LeaseToken++
						v.Status = model.AnalysisFailed
						v.Error = "此前模型调用结果无法确认，未自动重试；请显式重新解读。"
						v.LeaseOwner = ""
						v.LeaseExpiresAt = 0
						v.CompletedAt = now
						if e := saveAI(tx, v, old); e != nil {
							return e
						}
					}
					continue
				}
				if _, e := aiSnapshot(tx, v); e != nil {
					old := v.Version
					v.Version++
					v.Status = model.AnalysisFailed
					v.Error = "绑定事实快照已失效"
					v.CompletedAt = now
					if e = saveAI(tx, v, old); e != nil {
						return e
					}
					continue
				}
				if found {
					continue
				}
				old := v.Version
				v.Version++
				v.LeaseToken++
				v.Status = model.AnalysisRunning
				v.LeaseOwner = owner
				v.StartedAt = now
				v.Deadline = now + maxDuration.Milliseconds()
				v.LeaseExpiresAt = min(now+lease.Milliseconds(), v.Deadline)
				if e := saveAI(tx, v, old); e != nil {
					return e
				}
				out = v
				found = true
			}
			return nil
		})
		if err != nil {
			return out, err
		}
		if found {
			return out, nil
		}
	}
	return out, model.ErrNotFound
}

func (s *Store) RenewAnalysisAILease(ctx context.Context, tenant, id string, token int64, lease time.Duration) (out model.AnalysisAIRevision, err error) {
	if lease < time.Millisecond {
		return out, model.ErrAnalysisInvalid
	}
	err = s.backend.Transaction(ctx, tenant, func(tx StorageTx) error {
		v, e := load[model.AnalysisAIRevision](tx, "ai", id)
		if e != nil {
			return e
		}
		now := s.timestamp(tx)
		if e = aiLease(v, token, now); e != nil {
			return e
		}
		fenceAI(tx, v)
		if _, e = aiSnapshot(tx, v); e != nil {
			return e
		}
		old := v.Version
		v.Version++
		v.LeaseExpiresAt = min(now+lease.Milliseconds(), v.Deadline)
		if e = saveAI(tx, v, old); e != nil {
			return e
		}
		out = v
		return nil
	})
	return
}

func (s *Store) RecordAnalysisAIFacts(ctx context.Context, tenant, id string, token int64, factIDs []string) (out model.AnalysisAIRevision, err error) {
	err = s.backend.Transaction(ctx, tenant, func(tx StorageTx) error {
		v, e := load[model.AnalysisAIRevision](tx, "ai", id)
		if e != nil {
			return e
		}
		if e = aiLease(v, token, s.timestamp(tx)); e != nil {
			return e
		}
		fenceAI(tx, v)
		if _, e = aiSnapshot(tx, v); e != nil {
			return e
		}
		for _, id := range factIDs {
			if id == v.SnapshotID+"/summary" {
				continue
			}
			valid := false
			for _, kind := range []string{"output", "evidence"} {
				d, e := tx.Get(kind, id)
				if e == nil && d.RunID == v.RunID && (d.DeviceID == "" || slices.Contains(v.DeviceIDs, d.DeviceID)) {
					valid = true
					break
				} else if e != nil && e != model.ErrNotFound {
					return e
				}
			}
			if !valid {
				return fmt.Errorf("%w: fact outside bound snapshot", model.ErrAnalysisInvalid)
			}
		}
		old := v.Version
		v.SentFactIDs = append(v.SentFactIDs, factIDs...)
		slices.Sort(v.SentFactIDs)
		v.SentFactIDs = slices.Compact(v.SentFactIDs)
		v.Version++
		if e = saveAI(tx, v, old); e != nil {
			return e
		}
		out = v
		return nil
	})
	return
}

func (s *Store) FinishAnalysisAIRevision(ctx context.Context, tenant, id string, token int64, result model.AnalysisAIResult, modelName, failure string) (out model.AnalysisAIRevision, err error) {
	err = s.backend.Transaction(ctx, tenant, func(tx StorageTx) error {
		v, e := load[model.AnalysisAIRevision](tx, "ai", id)
		if e != nil {
			return e
		}
		if failure == "" {
			if e = aiLease(v, token, s.timestamp(tx)); e != nil {
				return e
			}
			fenceAI(tx, v)
		} else if token <= 0 || v.LeaseToken != token || v.Status != model.AnalysisRunning {
			return model.ErrAnalysisLeaseLost
		}
		if _, e = aiSnapshot(tx, v); e != nil {
			return e
		}
		old := v.Version
		v.Version++
		v.Status = model.AnalysisFailed
		v.Error = failure
		v.CompletedAt = s.timestamp(tx)
		v.LeaseOwner = ""
		v.LeaseExpiresAt = 0
		if failure == "" {
			ids, e := ValidateAIWorkflowResult(v.WorkflowID, result, v.SentFactIDs, v.DeviceIDs)
			if e != nil {
				return e
			}
			if e = validateAIObjects(tx, v, result); e != nil {
				return e
			}
			if e = s.commitAICandidate(tx, v, &result); e != nil {
				return e
			}
			v.FactIDs = ids
			v.Interpretation, e = marshalAIWorkflowResult(v.WorkflowID, result)
			if e != nil {
				return e
			}
			v.Coverage, e = json.Marshal(result.Coverage)
			if e != nil {
				return e
			}
			v.Status = model.AnalysisSucceeded
			v.Model = modelName
		}
		if e = saveAI(tx, v, old); e != nil {
			return e
		}
		out = v
		return nil
	})
	return
}

// A selected device must belong to the cited facts, rather than merely another
// authorized device in the same snapshot. Aggregate facts cover its full scope.
func validateAIObjects(tx StorageTx, job model.AnalysisAIRevision, result model.AnalysisAIResult) error {
	for _, list := range aiStatements(job.WorkflowID, result) {
		for _, statement := range list {
			covered := []string{}
			for _, ref := range statement.MetricRefs {
				d, err := tx.Get("output", ref)
				if err != nil || d.RunID != job.RunID || d.ApplicationKind != "metrics" {
					return model.ErrAnalysisInvalid
				}
			}
			for _, id := range append(slices.Clone(statement.FactIDs), statement.MetricRefs...) {
				if id == job.SnapshotID+"/summary" {
					covered = append(covered, job.DeviceIDs...)
					continue
				}
				found := false
				for _, kind := range []string{"output", "evidence"} {
					d, err := tx.Get(kind, id)
					if err == model.ErrNotFound {
						continue
					}
					if err != nil {
						return err
					}
					if d.RunID != job.RunID || d.DeviceID != "" && !slices.Contains(job.DeviceIDs, d.DeviceID) {
						return model.ErrAnalysisInvalid
					}
					if d.DeviceID == "" {
						covered = append(covered, job.DeviceIDs...)
					} else {
						covered = append(covered, d.DeviceID)
					}
					found = true
					break
				}
				if !found {
					return model.ErrAnalysisInvalid
				}
			}
			for _, device := range statement.DeviceIDs {
				if !slices.Contains(covered, device) {
					return fmt.Errorf("%w: AI object does not match referenced facts", model.ErrAnalysisInvalid)
				}
			}
		}
	}
	return nil
}

func (s *Store) StopAnalysisAIRevision(ctx context.Context, tenant, id string, expected int64) (out model.AnalysisAIRevision, err error) {
	err = s.backend.Transaction(ctx, tenant, func(tx StorageTx) error {
		v, e := load[model.AnalysisAIRevision](tx, "ai", id)
		if e != nil {
			return e
		}
		if v.Version != expected {
			return model.ErrAnalysisConflict
		}
		if TerminalAnalysisStatus(v.Status) {
			out = v
			return nil
		}
		old := v.Version
		v.Version++
		v.LeaseToken++
		v.Status = model.AnalysisCancelled
		v.LeaseOwner = ""
		v.LeaseExpiresAt = 0
		v.CompletedAt = s.timestamp(tx)
		v.Error = "用户停止或当前权限失效"
		if e = saveAI(tx, v, old); e != nil {
			return e
		}
		out = v
		return nil
	})
	return
}
