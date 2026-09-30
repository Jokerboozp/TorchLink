package recurring

import (
	"iot-platform/internal/model"
	"slices"
)

type ActivityCandidate struct {
	Point              model.GovernancePoint `json:"point"`
	ObservationID      string                `json:"observationId"`
	ActivityID         string                `json:"activityId"`
	ActivityRevisionID string                `json:"activityRevisionId"`
	ActivityType       string                `json:"activityType"`
	StartAt            int64                 `json:"startAt"`
	EndAt              int64                 `json:"endAt"`
	Basis              string                `json:"basis"`
	Relation           string                `json:"relation"`
}

// Candidates preserve all actual overlaps. Neither nearest activity nor an
// overlap itself establishes the cause or completes a field verification.
func ActivityCandidates(observations []model.AlarmObservation, resources []model.GovernanceDocument, allowed []string) []ActivityCandidate {
	latest := map[string]model.GovernanceDocument{}
	for _, d := range resources {
		if d.Kind != model.GovernanceActivityKind {
			continue
		}
		a, e := model.GovernanceBody[model.FieldActivityRevision](d)
		if e != nil {
			continue
		}
		old, ok := latest[a.ActivityID]
		if !ok || old.RevisionNumber < d.RevisionNumber {
			latest[a.ActivityID] = d
		}
	}
	ids := []string{}
	for id := range latest {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	out := []ActivityCandidate{}
	for _, o := range observations {
		if o.Acceptance != "ACCEPTED" || !comparableTime(o.TimeQuality) || (o.FactKind != "ASSERT" && o.FactKind != "REPORT") {
			continue
		}
		for _, id := range ids {
			d := latest[id]
			a, e := model.GovernanceBody[model.FieldActivityRevision](d)
			if e != nil || a.Status != "CONFIRMED" || !a.Actual || !comparableTime(a.TimeQuality) || a.StartAt == nil || a.EndAt == nil || !slices.Contains(a.DeviceIDs, o.DeviceID) || !slices.Contains(allowed, a.ActivityType) {
				continue
			}
			if *a.EndAt <= *a.StartAt || o.EventAt < *a.StartAt || o.EventAt >= *a.EndAt {
				continue
			}
			out = append(out, ActivityCandidate{Point: pointOf(o), ObservationID: o.ID, ActivityID: a.ActivityID, ActivityRevisionID: d.ID, ActivityType: a.ActivityType, StartAt: *a.StartAt, EndAt: *a.EndAt, Basis: "ACTUAL_INTERVAL_OVERLAP", Relation: "UNRESOLVED"})
		}
	}
	return out
}
