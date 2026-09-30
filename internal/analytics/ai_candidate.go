package analytics

import (
	"context"
	"encoding/json"
	"slices"

	"iot-platform/internal/model"
)

func (s *AIService) prepareAICandidate(ctx context.Context, job model.AnalysisAIRevision, result *model.AnalysisAIResult) error {
	if result.CandidateDraft == nil {
		return nil
	}
	if job.WorkflowID != WorkflowRulePolicy || s.PrepareCandidate == nil {
		return ErrUnsupported
	}
	run, err := s.Facts.Store.GetAnalysisRun(ctx, job.TenantID, job.RunID)
	if err != nil {
		return err
	}
	var p model.RuleLabRunParameters
	if json.Unmarshal(run.Parameters, &p) != nil || p.Phase != "EXPERIMENT" || p.ExperimentRevisionID == "" {
		return model.ErrAnalysisInvalid
	}
	a := Actor{TenantID: job.TenantID, Username: job.Creator, Managed: job.CreatorManaged, SessionVersion: job.CreatorSessionVersion, AccessVersion: job.PermissionVersion}
	v, expected, err := s.PrepareCandidate(ctx, a, p.ExperimentRevisionID, *result.CandidateDraft)
	if err != nil {
		return err
	}
	var body model.RuleLabExperiment
	if json.Unmarshal(v.Body, &body) != nil || body.Candidate.Enabled || body.CandidateEnabled {
		return model.ErrAnalysisInvalid
	}
	// Persist the platform-normalized candidate rather than the untrusted model body.
	result.CandidateDraft = &body.Candidate
	result.PreparedCandidate, result.CandidateExpectedVersion = &v, expected
	return nil
}

func (s *Store) commitAICandidate(tx StorageTx, job model.AnalysisAIRevision, result *model.AnalysisAIResult) error {
	if result.CandidateDraft == nil {
		if result.PreparedCandidate != nil || result.CandidateRevisionID != "" {
			return model.ErrAnalysisInvalid
		}
		return nil
	}
	if job.WorkflowID != WorkflowRulePolicy || result.PreparedCandidate == nil || result.CandidateRevisionID != "" {
		return model.ErrAnalysisInvalid
	}
	run, err := load[model.AnalysisRun](tx, "run", job.RunID)
	if err != nil {
		return err
	}
	var p model.RuleLabRunParameters
	if json.Unmarshal(run.Parameters, &p) != nil || p.Phase != "EXPERIMENT" {
		return model.ErrAnalysisInvalid
	}
	source, err := load[model.AnalysisConfigRevision](tx, "config", p.ExperimentRevisionID)
	if err != nil {
		return err
	}
	v := *result.PreparedCandidate
	if source.Kind != model.RuleLabExperimentKind || v.ID == source.ID || v.Kind != source.Kind || v.ResourceID != source.ResourceID || v.Scope != source.Scope || v.TenantID != job.TenantID || v.Creator != job.Creator || !slices.Equal(v.DeviceIDs, source.DeviceIDs) || !slices.Equal(v.DeviceIDs, job.DeviceIDs) || result.CandidateExpectedVersion != source.Version {
		return model.ErrAnalysisInvalid
	}
	var original, candidate model.RuleLabExperiment
	if json.Unmarshal(source.Body, &original) != nil || json.Unmarshal(v.Body, &candidate) != nil || candidate.Candidate.Enabled || candidate.CandidateEnabled || model.RuleBodyHash(candidate.Candidate) != model.RuleBodyHash(*result.CandidateDraft) {
		return model.ErrAnalysisInvalid
	}
	// Every noncandidate field stays bound to the original immutable experiment.
	original.Candidate, original.CandidateEnabled = candidate.Candidate, false
	a, _ := AnalysisHash(original)
	b, _ := AnalysisHash(candidate)
	if a != b {
		return model.ErrAnalysisInvalid
	}
	saved, err := s.putAnalysisConfig(tx, v, result.CandidateExpectedVersion)
	if err != nil {
		return err
	}
	result.CandidateRevisionID = saved.ID
	return nil
}
