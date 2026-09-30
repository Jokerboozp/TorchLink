package analytics

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"iot-platform/internal/model"
)

// SnapshotFact is an exact registered fact in one immutable snapshot. IDs are
// those issued by the fixed-fact reader, never aliases for a mutable source.
type SnapshotFact struct {
	ID                   string          `json:"id"`
	Collection           string          `json:"collection"`
	SnapshotID           string          `json:"snapshotId"`
	SnapshotVersion      int64           `json:"snapshotVersion"`
	FactsHash            string          `json:"factsHash"`
	DeviceIDs            []string        `json:"deviceIds"`
	DeviceID             string          `json:"deviceId,omitempty"`
	SourceID             string          `json:"sourceId,omitempty"`
	SourceType           string          `json:"sourceType"`
	Summary              json.RawMessage `json:"summary,omitempty"`
	Body                 json.RawMessage `json:"body,omitempty"`
	RawMessageID         string          `json:"rawMessageId,omitempty"`
	OccurredAt           int64           `json:"occurredAt,omitempty"`
	ResourceVersion      string          `json:"resourceVersion,omitempty"`
	OriginalAvailability string          `json:"originalAvailability,omitempty"`
}

// Fact shares the same collection whitelist, fixed snapshot reader and source
// permissions as model input. Browser reads do not require an active model
// lease, do not record sent facts and do not start a workflow.
func (s *AIService) Fact(ctx context.Context, a Actor, kind, runID, factID string) (SnapshotFact, error) {
	if factID == "" || len(factID) > 512 || strings.ContainsAny(factID, "\x00\r\n") {
		return SnapshotFact{}, model.ErrAnalysisInvalid
	}
	run, err := s.Facts.Get(ctx, a, kind, runID)
	if err != nil {
		return SnapshotFact{}, err
	}
	if run.SnapshotID == "" {
		return SnapshotFact{}, model.ErrNotFound
	}
	spec, ok := s.Workflow(kind)
	if !ok {
		return SnapshotFact{}, ErrUnsupported
	}
	snap, err := s.Facts.Store.GetAnalysisSnapshot(ctx, a.TenantID, run.SnapshotID)
	if err != nil {
		return SnapshotFact{}, err
	}
	if snap.RunID != run.ID || !slices.Equal(snap.DeviceIDs, run.DeviceIDs) {
		return SnapshotFact{}, model.ErrAnalysisConflict
	}
	job := model.AnalysisAIRevision{TenantID: run.TenantID, RunID: run.ID, Kind: kind, WorkflowID: spec.WorkflowID, SnapshotID: snap.ID, SnapshotVersion: snap.Version, DeviceIDs: run.DeviceIDs}
	result := SnapshotFact{ID: factID, SnapshotID: snap.ID, SnapshotVersion: snap.Version, FactsHash: snap.FactsHash, DeviceIDs: slices.Clone(run.DeviceIDs)}
	found := false
	if factID == snap.ID+"/summary" {
		facts, err := s.readFacts(ctx, job, "summary", 1, 0)
		if err != nil {
			return SnapshotFact{}, err
		}
		result.Collection, result.SourceID, result.SourceType = "summary", snap.ID, "FIXED_SNAPSHOT_SUMMARY"
		result.Summary, _ = json.Marshal(facts)
		found = true
	} else {
		for _, collection := range analysisCollections(spec.WorkflowID) {
			for offset := 0; ; {
				facts, err := s.readFacts(ctx, job, collection, 100, offset)
				if err != nil {
					return SnapshotFact{}, err
				}
				for _, output := range facts.Outputs {
					if output.ID != factID {
						continue
					}
					result.Collection, result.SourceID, result.SourceType, result.DeviceID, result.Body = collection, output.ID, output.Kind, output.DeviceID, slices.Clone(output.Body)
					found = true
					break
				}
				for _, evidence := range facts.Evidence {
					if evidence.ID != factID {
						continue
					}
					result.Collection, result.SourceID, result.SourceType, result.DeviceID = "evidence", evidence.SourceID, evidence.SourceKind, evidence.DeviceID
					result.Summary, result.RawMessageID, result.OccurredAt = slices.Clone(evidence.Summary), evidence.RawMessageID, evidence.OccurredAt
					result.ResourceVersion, result.OriginalAvailability = evidence.ResourceVersion, evidence.OriginalAvailability
					found = true
					break
				}
				if found || !facts.HasMore {
					break
				}
				if facts.NextOffset <= offset {
					return SnapshotFact{}, model.ErrAnalysisConflict
				}
				offset = facts.NextOffset
			}
			if found {
				break
			}
		}
	}
	// Current permissions and inherited provenance are checked after the read
	// too. The returned facts remain from the original immutable snapshot.
	current, err := s.Facts.Get(ctx, a, kind, runID)
	if err != nil {
		return SnapshotFact{}, err
	}
	if current.SnapshotID != snap.ID {
		return SnapshotFact{}, model.ErrAnalysisConflict
	}
	if !found {
		return SnapshotFact{}, model.ErrNotFound
	}
	return result, nil
}
