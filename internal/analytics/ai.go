package analytics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

const WorkflowDataQuality = "data-quality-analyst"
const AnalysisAIPromptVersion = "analysis-fixed-facts-v1"

type AIRunner func(context.Context, model.AnalysisAIRevision, model.AnalysisAIFacts) (ports.AIWorkflowResult, error)
type AIWorkflowSpec struct{ Kind, WorkflowID, PromptVersion string }
type AIService struct {
	Facts                *Service
	Runner               AIRunner
	StopRunner           func(context.Context, string, string) error
	PrepareCandidate     func(context.Context, Actor, string, model.AlarmRule) (model.AnalysisConfigRevision, int64, error)
	Lease, Timeout, Poll time.Duration
	Workers              int
	mu                   sync.RWMutex
	workflows            map[string]AIWorkflowSpec
}

var _ ports.AnalysisAIReader = (*AIService)(nil)

func NewAIService(facts *Service, runner AIRunner) *AIService {
	s := &AIService{Facts: facts, Runner: runner, Lease: 30 * time.Second, Timeout: 4 * time.Minute, Poll: time.Second, Workers: 2, workflows: map[string]AIWorkflowSpec{}}
	_ = s.Register(AIWorkflowSpec{KindDataQuality, WorkflowDataQuality, AnalysisAIPromptVersion})
	_ = s.Register(AIWorkflowSpec{KindMonitoring, WorkflowMonitoring, MonitoringAIPromptVersion})
	_ = s.Register(AIWorkflowSpec{KindRuleLab, WorkflowRulePolicy, RulePolicyAIPromptVersion})
	_ = s.Register(AIWorkflowSpec{KindResponse, WorkflowResponse, ResponseAIPromptVersion})
	_ = s.Register(AIWorkflowSpec{KindMaintenance, WorkflowMaintenance, MaintenanceAIPromptVersion})
	_ = s.Register(AIWorkflowSpec{KindInvestment, WorkflowInvestment, InvestmentAIPromptVersion})
	return s
}
func (s *AIService) Register(spec AIWorkflowSpec) error {
	known, ok := AnalysisWorkflow(spec.Kind)
	if !ok || known != spec {
		return model.ErrAnalysisInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workflows[spec.Kind] = spec
	return nil
}
func (s *AIService) Workflow(kind string) (AIWorkflowSpec, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.workflows[kind]
	return v, ok
}
func (s *AIService) workflowIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []string{}
	for _, v := range s.workflows {
		out = append(out, v.WorkflowID)
	}
	slices.Sort(out)
	return out
}
func (s *AIService) store() (ports.AnalysisAIStore, error) {
	v, ok := s.Facts.Store.(ports.AnalysisAIStore)
	if !ok {
		return nil, ErrUnsupported
	}
	return v, nil
}

type CreateAIRequest struct {
	ExpectedVersion int64  `json:"expectedVersion"`
	IdempotencyKey  string `json:"idempotencyKey"`
	UseKnowledge    bool   `json:"useKnowledge"`
	Reinterpret     bool   `json:"reinterpret"`
}

func knowledgeAllowed(a Actor) bool {
	return slices.Contains(a.Permissions, "*") || slices.Contains(a.Permissions, "menu:knowledge")
}
func (s *AIService) Create(ctx context.Context, a Actor, kind, runID string, q CreateAIRequest) (model.AnalysisAIRevision, error) {
	spec, ok := s.Workflow(kind)
	if !ok {
		return model.AnalysisAIRevision{}, ErrUnsupported
	}
	if q.ExpectedVersion <= 0 || q.IdempotencyKey == "" || len(q.IdempotencyKey) > 200 {
		return model.AnalysisAIRevision{}, model.ErrAnalysisInvalid
	}
	run, err := s.Facts.Get(ctx, a, kind, runID)
	if err != nil {
		return model.AnalysisAIRevision{}, err
	}
	current, err := s.Facts.authorize(ctx, a, kind, AIStartOperation(kind), run.DeviceIDs)
	if err != nil {
		return model.AnalysisAIRevision{}, err
	}
	if q.UseKnowledge && !knowledgeAllowed(current) {
		return model.AnalysisAIRevision{}, ErrForbidden
	}
	if run.SnapshotID == "" || (run.Status != model.AnalysisSucceeded && run.Status != model.AnalysisPartial) {
		return model.AnalysisAIRevision{}, model.ErrAnalysisConflict
	}
	snapshot, err := s.Facts.Store.GetAnalysisSnapshot(ctx, a.TenantID, run.SnapshotID)
	if err != nil {
		return model.AnalysisAIRevision{}, err
	}
	store, err := s.store()
	if err != nil {
		return model.AnalysisAIRevision{}, err
	}
	job := model.AnalysisAIRevision{ID: uuid.NewString(), TenantID: a.TenantID, RunID: run.ID, Kind: kind, SnapshotID: snapshot.ID, SnapshotVersion: snapshot.Version, WorkflowID: spec.WorkflowID, PromptVersion: spec.PromptVersion, Creator: a.Username, CreatorManaged: current.Managed, CreatorSessionVersion: current.SessionVersion, PermissionVersion: current.AccessVersion, DeviceIDs: slices.Clone(run.DeviceIDs), IdempotencyKey: q.IdempotencyKey, UseKnowledge: q.UseKnowledge, Reinterpret: q.Reinterpret}
	return store.CreateAnalysisAIRevision(ctx, job, q.ExpectedVersion, s.Facts.Limits.QueueLimit)
}
func (s *AIService) Get(ctx context.Context, a Actor, kind, runID, jobID string) (model.AnalysisAIRevision, error) {
	run, err := s.Facts.Get(ctx, a, kind, runID)
	if err != nil {
		return model.AnalysisAIRevision{}, err
	}
	current, err := s.Facts.authorize(ctx, a, kind, "", run.DeviceIDs)
	if err != nil {
		return model.AnalysisAIRevision{}, err
	}
	job, err := s.Facts.Store.GetAnalysisAIRevision(ctx, a.TenantID, jobID)
	if err != nil {
		return job, err
	}
	if job.RunID != runID || job.Kind != kind || job.SnapshotID != run.SnapshotID {
		return model.AnalysisAIRevision{}, model.ErrNotFound
	}
	if job.UseKnowledge && !knowledgeAllowed(current) {
		return model.AnalysisAIRevision{}, ErrForbidden
	}
	return job, nil
}
func (s *AIService) List(ctx context.Context, a Actor, kind, runID string, f model.AnalysisFilter) ([]model.AnalysisAIRevision, int, error) {
	run, err := s.Facts.Get(ctx, a, kind, runID)
	if err != nil {
		return nil, 0, err
	}
	current, err := s.Facts.authorize(ctx, a, kind, "", run.DeviceIDs)
	if err != nil {
		return nil, 0, err
	}
	f.RunID = runID
	items, n, err := s.Facts.Store.ListAnalysisAIRevisions(ctx, a.TenantID, f)
	if err != nil {
		return nil, 0, err
	}
	for _, job := range items {
		if job.UseKnowledge && !knowledgeAllowed(current) {
			return nil, 0, ErrForbidden
		}
	}
	return items, n, nil
}
func (s *AIService) Stop(ctx context.Context, a Actor, kind, runID, jobID string, expected int64) (model.AnalysisAIRevision, error) {
	job, err := s.Get(ctx, a, kind, runID, jobID)
	if err != nil {
		return job, err
	}
	if _, err = s.Facts.authorize(ctx, a, kind, AIStopOperation(kind), job.DeviceIDs); err != nil {
		return model.AnalysisAIRevision{}, err
	}
	store, err := s.store()
	if err != nil {
		return job, err
	}
	stopped, err := store.StopAnalysisAIRevision(ctx, a.TenantID, jobID, expected)
	if err == nil && s.StopRunner != nil && job.Status == model.AnalysisRunning {
		stopCtx, done := context.WithTimeout(ctx, 5*time.Second)
		defer done()
		_ = s.StopRunner(stopCtx, job.TenantID, job.HarnessRunID)
	}
	return stopped, err
}
func AIIdentity(job model.AnalysisAIRevision) ports.AIRunIdentity {
	scopes := []string{ports.MCPToolScope("query_analysis_snapshot")}
	if job.UseKnowledge {
		scopes = append(scopes, ports.MCPToolScope("query_knowledge_base"))
	}
	return ports.AIRunIdentity{TenantID: job.TenantID, Username: job.Creator, ManagedUser: job.CreatorManaged, SessionVersion: job.CreatorSessionVersion, AccessVersion: job.PermissionVersion, AnalysisRunID: job.RunID, AnalysisSnapshotID: job.SnapshotID, AnalysisSnapshotVersion: job.SnapshotVersion, AnalysisJobID: job.ID, AnalysisLeaseToken: job.LeaseToken, AnalysisHarnessRunID: job.HarnessRunID, AnalysisWorkflowID: job.WorkflowID, Scopes: scopes}
}
func (s *AIService) ValidateBinding(ctx context.Context, identity ports.AIRunIdentity) (model.AnalysisAIRevision, error) {
	job, err := s.Facts.Store.GetAnalysisAIRevision(ctx, identity.TenantID, identity.AnalysisJobID)
	if err != nil {
		return job, ErrForbidden
	}
	spec, ok := s.Workflow(job.Kind)
	if !ok || spec.WorkflowID != job.WorkflowID || spec.PromptVersion != job.PromptVersion {
		return job, ErrForbidden
	}
	if job.WorkflowID != identity.AnalysisWorkflowID || job.Creator != identity.Username || job.CreatorManaged != identity.ManagedUser || job.CreatorSessionVersion != identity.SessionVersion || job.RunID != identity.AnalysisRunID || job.SnapshotID != identity.AnalysisSnapshotID || job.SnapshotVersion != identity.AnalysisSnapshotVersion || job.LeaseToken != identity.AnalysisLeaseToken || job.HarnessRunID != identity.AnalysisHarnessRunID || job.PermissionVersion != identity.AccessVersion || aiLease(job, identity.AnalysisLeaseToken, time.Now().UnixMilli()) != nil {
		return job, ErrForbidden
	}
	a := Actor{TenantID: job.TenantID, Username: job.Creator, Managed: job.CreatorManaged, SessionVersion: job.CreatorSessionVersion}
	current, err := s.Facts.authorize(ctx, a, job.Kind, AIStartOperation(job.Kind), job.DeviceIDs)
	if err != nil || current.AccessVersion != job.PermissionVersion || job.UseKnowledge && !knowledgeAllowed(current) {
		return job, ErrForbidden
	}
	run, err := s.Facts.Get(ctx, a, job.Kind, job.RunID)
	if err != nil || run.SnapshotID != job.SnapshotID || !slices.Equal(run.DeviceIDs, job.DeviceIDs) {
		return job, ErrForbidden
	}
	return job, nil
}

func (s *AIService) ReadBoundAnalysisFacts(ctx context.Context, identity ports.AIRunIdentity, collection string, limit, offset int) (model.AnalysisAIFacts, error) {
	job, err := s.ValidateBinding(ctx, identity)
	if err != nil {
		return model.AnalysisAIFacts{}, err
	}
	facts, err := s.readFacts(ctx, job, collection, limit, offset)
	if err != nil {
		return facts, err
	}
	if _, err = s.ValidateBinding(ctx, identity); err != nil {
		return model.AnalysisAIFacts{}, err
	}
	store, err := s.store()
	if err != nil {
		return facts, err
	}
	if _, err = store.RecordAnalysisAIFacts(ctx, job.TenantID, job.ID, job.LeaseToken, factsIDs(facts)); err != nil {
		return model.AnalysisAIFacts{}, err
	}
	return facts, nil
}
func factsIDs(f model.AnalysisAIFacts) []string {
	ids := []string{}
	if len(f.Statistics) > 0 {
		ids = append(ids, f.SummaryFactID)
	}
	for _, v := range f.Outputs {
		ids = append(ids, v.ID)
	}
	for _, v := range f.Evidence {
		ids = append(ids, v.ID)
	}
	return ids
}
func (s *AIService) readFacts(ctx context.Context, job model.AnalysisAIRevision, collection string, limit, offset int) (f model.AnalysisAIFacts, err error) {
	if collection != "summary" && !slices.Contains(analysisCollections(job.WorkflowID), collection) {
		return f, model.ErrAnalysisInvalid
	}
	limit, offset = NormalizeAnalysisPage(limit, offset)
	snap, err := s.Facts.Store.GetAnalysisSnapshot(ctx, job.TenantID, job.SnapshotID)
	if err != nil || snap.Version != job.SnapshotVersion || !slices.Equal(snap.DeviceIDs, job.DeviceIDs) {
		return f, ErrForbidden
	}
	f = model.AnalysisAIFacts{SnapshotID: snap.ID, SnapshotVersion: snap.Version, SummaryFactID: snap.ID + "/summary", Collection: collection, WindowStart: snap.Start, WindowEnd: snap.End, DataCutoff: snap.DataCutoff, Outputs: []model.AnalysisOutput{}, Evidence: []model.AnalysisEvidence{}, Offset: offset, Complete: true}
	switch collection {
	case "summary":
		f.Statistics = snap.Statistics
		f.Sources = snap.Sources
		f.Limitations = snap.Limitations
		f.MissingSources = snap.MissingSources
		f.AffectedIntervals = snap.AffectedIntervals
		f.UncomputableMetrics = snap.UncomputableMetrics
		f.InitialStateQuality = snap.InitialStateQuality
		f.Total = 1
	case "metrics", "findings", "intervals", "dependency-groups", "outcomes", "diffs", "labels", "observations", "change-metrics", "investment-priorities", "budget-lines":
		f.Outputs, f.Total, err = s.Facts.Store.ListAnalysisOutputs(ctx, job.TenantID, model.AnalysisFilter{RunID: job.RunID, Kind: collection, Limit: limit, Offset: offset})
	case "evidence":
		f.Evidence, f.Total, err = s.Facts.Store.ListAnalysisEvidence(ctx, job.TenantID, model.AnalysisFilter{RunID: job.RunID, Limit: limit, Offset: offset})
	default:
		return f, model.ErrAnalysisInvalid
	}
	if err != nil {
		return f, err
	}
	for _, e := range f.Evidence {
		allowed := []string{"", "devices"}
		if job.Kind == KindDataQuality {
			allowed = append(allowed, "dataQuality", "data-quality", "data-quality-evidence")
		} else if job.Kind == KindMonitoring {
			allowed = append(allowed, "monitoring", "monitoring-gaps", "monitoring-evidence")
		} else if job.Kind == KindRuleLab {
			allowed = append(allowed, "rule-lab-evidence")
		} else if job.Kind == KindResponse {
			allowed = append(allowed, "response")
		} else if job.Kind == KindMaintenance || job.Kind == KindInvestment {
			allowed = append(allowed, "maintenance-evidence")
		}
		if !slices.Contains(allowed, e.PermissionCategory) {
			return model.AnalysisAIFacts{}, ErrForbidden
		}
	}
	for {
		f.NextOffset = offset + len(f.Outputs) + len(f.Evidence)
		if collection == "summary" {
			f.NextOffset = 1
		}
		f.HasMore = f.NextOffset < f.Total
		raw, e := json.Marshal(f)
		if e != nil {
			return f, e
		}
		if len(raw) <= 12<<10 {
			break
		}
		f.Complete = false
		if len(f.Evidence) > 0 {
			f.Evidence = f.Evidence[:len(f.Evidence)-1]
		} else if len(f.Outputs) > 0 {
			f.Outputs = f.Outputs[:len(f.Outputs)-1]
		} else {
			return model.AnalysisAIFacts{}, fmt.Errorf("%w: accurate summary exceeds model budget", model.ErrAnalysisInvalid)
		}
	}
	if f.HasMore && f.NextOffset == offset {
		return model.AnalysisAIFacts{}, fmt.Errorf("%w: single fact exceeds model budget", model.ErrAnalysisInvalid)
	}
	f.Complete = f.Complete && !f.HasMore
	return f, nil
}
func (s *AIService) BuildInput(ctx context.Context, job model.AnalysisAIRevision) (f model.AnalysisAIFacts, err error) {
	identity := AIIdentity(job)
	if _, err = s.ValidateBinding(ctx, identity); err != nil {
		return f, err
	}
	f, err = s.readFacts(ctx, job, "summary", 1, 0)
	if err != nil {
		return f, err
	}
	for _, collection := range analysisCollections(job.WorkflowID) {
		limit := 10
		if collection == "findings" {
			limit = 20
		}
		part, e := s.readFacts(ctx, job, collection, limit, 0)
		if e != nil {
			return f, e
		}
		for _, v := range part.Outputs {
			candidate := f
			candidate.Outputs = append(slices.Clone(f.Outputs), v)
			raw, _ := json.Marshal(candidate)
			if len(raw) > 18<<10 {
				break
			}
			f = candidate
		}
		for _, v := range part.Evidence {
			candidate := f
			candidate.Evidence = append(slices.Clone(f.Evidence), v)
			raw, _ := json.Marshal(candidate)
			if len(raw) > 18<<10 {
				break
			}
			f = candidate
		}
	}
	if _, err = s.ValidateBinding(ctx, identity); err != nil {
		return model.AnalysisAIFacts{}, err
	}
	store, err := s.store()
	if err != nil {
		return f, err
	}
	updated, err := store.RecordAnalysisAIFacts(ctx, job.TenantID, job.ID, job.LeaseToken, factsIDs(f))
	if err != nil {
		return model.AnalysisAIFacts{}, err
	}
	f.Coverage, err = s.coverage(ctx, updated)
	f.Collection = "sample"
	f.Total = f.Coverage.TotalOutputs + f.Coverage.TotalEvidence
	f.NextOffset = 0 // The mixed sample has no shared cursor; page each collection.
	f.HasMore = false
	f.Complete = !f.Coverage.Truncated
	return f, err
}
func (s *AIService) coverage(ctx context.Context, job model.AnalysisAIRevision) (v model.AnalysisAICoverage, err error) {
	v.SummaryProvided = slices.Contains(job.SentFactIDs, job.SnapshotID+"/summary")
	for _, kind := range analysisCollections(job.WorkflowID) {
		offset := 0
		for {
			ids := []string{}
			var total int
			if kind != "evidence" {
				rows, n, e := s.Facts.Store.ListAnalysisOutputs(ctx, job.TenantID, model.AnalysisFilter{RunID: job.RunID, Kind: kind, Limit: 100, Offset: offset})
				if e != nil {
					return v, e
				}
				total = n
				for _, row := range rows {
					ids = append(ids, row.ID)
				}
			} else {
				rows, n, e := s.Facts.Store.ListAnalysisEvidence(ctx, job.TenantID, model.AnalysisFilter{RunID: job.RunID, Limit: 100, Offset: offset})
				if e != nil {
					return v, e
				}
				total = n
				for _, row := range rows {
					ids = append(ids, row.ID)
				}
			}
			provided := 0
			for _, id := range ids {
				if slices.Contains(job.SentFactIDs, id) {
					provided++
				}
			}
			if kind != "evidence" {
				v.OutputCount += provided
			} else {
				v.EvidenceCount += provided
				v.TotalEvidence = total
			}
			offset += len(ids)
			if offset >= total {
				if kind != "evidence" {
					v.TotalOutputs += total
				}
				break
			}
		}
	}
	v.Truncated = v.OutputCount < v.TotalOutputs || v.EvidenceCount < v.TotalEvidence
	return v, nil
}

func (s *AIService) cancelJob(ctx context.Context, job model.AnalysisAIRevision) {
	store, err := s.store()
	if err != nil {
		return
	}
	for i := 0; i < 3; i++ {
		current, e := s.Facts.Store.GetAnalysisAIRevision(ctx, job.TenantID, job.ID)
		if e != nil || TerminalAnalysisStatus(current.Status) {
			return
		}
		if _, e = store.StopAnalysisAIRevision(ctx, current.TenantID, current.ID, current.Version); !errors.Is(e, model.ErrAnalysisConflict) {
			return
		}
	}
}
