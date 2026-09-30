package backup

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"iot-platform/internal/analytics"
	"iot-platform/internal/model"
)

type applicationDocument struct {
	Tenant          string          `json:"tenant_id"`
	Kind            string          `json:"kind"`
	ID              string          `json:"id"`
	RunID           string          `json:"run_id"`
	DeviceID        string          `json:"device_id"`
	ApplicationKind string          `json:"application_kind"`
	ResourceID      string          `json:"resource_id"`
	Status          string          `json:"status"`
	DeviceIDs       []string        `json:"device_ids"`
	Version         int64           `json:"version"`
	CreatedAt       int64           `json:"created_at"`
	Body            json.RawMessage `json:"body"`
}

func applicationIdentity(tenant, kind, id string) string { return tenant + "\x00" + kind + "\x00" + id }
func decodeApplication[T any](d applicationDocument) (v T, err error) {
	err = json.Unmarshal(d.Body, &v)
	return
}
func sameApplicationHash(a, b any) bool {
	x, e := analytics.AnalysisHash(a)
	y, f := analytics.AnalysisHash(b)
	return e == nil && f == nil && x == y
}
func applicationInvalid(d applicationDocument, reason string) error {
	return fmt.Errorf("invalid application %s %s: %s", d.Kind, d.ID, reason)
}

// Validate the immutable graph before changing execution leases or writing
// object overlays. Source evidence may have expired; that is preserved as a
// missing source, rather than silently inventing a replacement member.
func validateApplicationDocuments(documents []applicationDocument) error {
	byID := map[string]applicationDocument{}
	facts := map[string][]json.RawMessage{}
	for _, d := range documents {
		key := applicationIdentity(d.Tenant, d.Kind, d.ID)
		if d.Tenant == "" || d.Kind == "" || d.ID == "" || d.Version < 1 || !json.Valid(d.Body) {
			return applicationInvalid(d, "invalid envelope")
		}
		if _, exists := byID[key]; exists {
			return applicationInvalid(d, "duplicate")
		}
		byID[key] = d
		if d.Kind == "output" || d.Kind == "evidence" {
			facts[applicationIdentity(d.Tenant, "run", d.RunID)] = append(facts[applicationIdentity(d.Tenant, "run", d.RunID)], d.Body)
		}
	}
	lookup := func(d applicationDocument, kind, id string) (applicationDocument, bool) {
		v, ok := byID[applicationIdentity(d.Tenant, kind, id)]
		return v, ok
	}
	for _, d := range documents {
		if d.RunID != "" {
			if _, ok := lookup(d, "run", d.RunID); !ok {
				return applicationInvalid(d, "missing run")
			}
		}
		switch d.Kind {
		case "config":
			v, e := decodeApplication[model.AnalysisConfigRevision](d)
			if e != nil || v.TenantID != d.Tenant || v.ID != d.ID || v.Kind != d.ApplicationKind || v.ResourceID != d.ResourceID || v.Version < 1 || d.Version != 1 || !analytics.AnalysisHashMatchesJSONB(v.Hash, v.Body) || !slices.Equal(v.DeviceIDs, d.DeviceIDs) || len(v.DeviceIDs) == 0 || !slices.Contains([]string{"PERSONAL", "SHARED"}, v.Scope) {
				return applicationInvalid(d, "configuration hash or identity")
			}
			if err := validateRestoredScenarioPermissions(d, v, byID); err != nil {
				return err
			}
		case "config-pointer":
			p, e := decodeApplication[struct{ ID string }](d)
			target, ok := lookup(d, "config", p.ID)
			if e != nil || !ok {
				return applicationInvalid(d, "missing configuration revision")
			}
			v, e := decodeApplication[model.AnalysisConfigRevision](target)
			owner := ""
			if v.Scope == "PERSONAL" {
				owner = v.Creator
			}
			pointer, _ := analytics.AnalysisHash(struct{ Kind, Resource, Scope, Owner string }{v.Kind, v.ResourceID, v.Scope, owner})
			if e != nil || d.ID != pointer || d.Version != v.Version || !slices.Equal(d.DeviceIDs, v.DeviceIDs) {
				return applicationInvalid(d, "pointer identity or version")
			}
		case "run":
			v, e := decodeApplication[model.AnalysisRun](d)
			if e != nil || v.ID != d.ID || d.RunID != v.ID || v.TenantID != d.Tenant || v.Version != d.Version || v.Status != d.Status || v.Kind != d.ApplicationKind || !slices.Equal(v.DeviceIDs, d.DeviceIDs) || len(v.DeviceIDs) == 0 || v.Start >= v.End {
				return applicationInvalid(d, "run identity, scope or version")
			}
			required, requiredErr := analytics.NormalizeRequiredPermissions(v.Kind, v.RequiredPermissions)
			if requiredErr != nil || !slices.Equal(required, v.RequiredPermissions) {
				return applicationInvalid(d, "server required permissions")
			}
			if v.Kind == analytics.KindMaintenance || v.Kind == analytics.KindInvestment {
				var parameters struct {
					UseFinance bool `json:"useFinance"`
				}
				if json.Unmarshal(v.Parameters, &parameters) != nil || parameters.UseFinance != slices.Contains(v.RequiredPermissions, analytics.FinanceReadOperation) {
					return applicationInvalid(d, "fixed financial scope")
				}
				if err := validateMaintenanceFrozenInputs(d, v, byID); err != nil {
					return err
				}
			}
			if v.PreviousRunID != "" {
				prior, ok := lookup(d, "run", v.PreviousRunID)
				if !ok || prior.ApplicationKind != v.Kind {
					return applicationInvalid(d, "previous run")
				}
			}
			if v.SnapshotID != "" {
				snap, ok := lookup(d, "snapshot", v.SnapshotID)
				if !ok || snap.RunID != v.ID {
					return applicationInvalid(d, "missing fixed snapshot")
				}
			}
			if slices.Contains([]string{model.AnalysisSucceeded, model.AnalysisPartial}, v.Status) && v.SnapshotID == "" {
				return applicationInvalid(d, "completed run missing snapshot")
			}
		case "snapshot":
			v, e := decodeApplication[model.AnalysisSnapshot](d)
			rd, ok := lookup(d, "run", d.RunID)
			run, er := decodeApplication[model.AnalysisRun](rd)
			if e != nil || er != nil || !ok || v.ID != d.ID || v.TenantID != d.Tenant || v.RunID != d.RunID || v.Version != 1 || d.Version != 1 || run.SnapshotID != v.ID || !slices.Equal(v.DeviceIDs, run.DeviceIDs) || !slices.Equal(d.DeviceIDs, run.DeviceIDs) || v.Start != run.Start || v.End != run.End || !sameApplicationHash(v.InputHashes, run.InputHashes) || !sameApplicationHash(v.Sources, run.Sources) || v.DataCutoff != run.DataCutoff {
				return applicationInvalid(d, "snapshot binding")
			}
			entries := facts[applicationIdentity(d.Tenant, "run", d.RunID)]
			if entries == nil {
				entries = []json.RawMessage{}
			}
			slices.SortFunc(entries, func(a, b json.RawMessage) int {
				x, _ := analytics.AnalysisHash(a)
				y, _ := analytics.AnalysisHash(b)
				return strings.Compare(x, y)
			})
			fixed := v
			fixed.FactsHash = ""
			fixed.CreatedAt = 0
			hash, _ := analytics.AnalysisHash(struct {
				Snapshot model.AnalysisSnapshot
				Facts    []json.RawMessage
			}{fixed, entries})
			if v.FactsHash != hash {
				// Facts were queried from JSONB before the original digest,
				// while newly computed statistics were inserted afterward.
				// Bound the complete preimage, including unchanged facts;
				// the numeric helper only accounts for the statistics bytes.
				budget := 64 << 20
				if analytics.AnalysisLegacyJSONBDigestMatches(v.FactsHash, fixed.Statistics, func(candidate json.RawMessage) (string, error) {
					copy := fixed
					copy.Statistics = candidate
					preimage, err := json.Marshal(struct {
						Snapshot model.AnalysisSnapshot
						Facts    []json.RawMessage
					}{copy, entries})
					if err != nil || len(preimage) > budget {
						return "", errors.New("legacy snapshot digest verification work limit")
					}
					budget -= len(preimage)
					return analytics.AnalysisHash(json.RawMessage(preimage))
				}) {
					continue
				}
				return applicationInvalid(d, "fixed facts digest cannot be verified")
			}
		case "output":
			v, e := decodeApplication[model.AnalysisOutput](d)
			rd, _ := lookup(d, "run", d.RunID)
			if e != nil || v.ID != d.ID || v.TenantID != d.Tenant || v.RunID != d.RunID || v.DeviceID != d.DeviceID || d.ApplicationKind != v.Kind || d.Version != 1 || !slices.Equal(d.DeviceIDs, rd.DeviceIDs) || !json.Valid(v.Body) || (v.DeviceID != "" && !slices.Contains(rd.DeviceIDs, v.DeviceID)) {
				return applicationInvalid(d, "output binding")
			}
		case "evidence":
			v, e := decodeApplication[model.AnalysisEvidence](d)
			rd, _ := lookup(d, "run", d.RunID)
			if e != nil || v.ID != d.ID || v.TenantID != d.Tenant || v.RunID != d.RunID || v.DeviceID != d.DeviceID || d.Version != 1 || !slices.Equal(d.DeviceIDs, rd.DeviceIDs) || !slices.Contains(rd.DeviceIDs, v.DeviceID) {
				return applicationInvalid(d, "evidence binding")
			}
			if v.SnapshotID != "" {
				sd, ok := lookup(d, "snapshot", v.SnapshotID)
				if !ok || sd.RunID != d.RunID {
					return applicationInvalid(d, "evidence snapshot")
				}
			}
		case "ai":
			v, e := decodeApplication[model.AnalysisAIRevision](d)
			sd, ok := lookup(d, "snapshot", v.SnapshotID)
			snap, er := decodeApplication[model.AnalysisSnapshot](sd)
			rd, _ := lookup(d, "run", v.RunID)
			spec, registered := analytics.AnalysisWorkflow(rd.ApplicationKind)
			if e != nil || er != nil || !ok || v.ID != d.ID || v.TenantID != d.Tenant || v.RunID != d.RunID || v.Version != d.Version || v.Status != d.Status || v.SnapshotVersion != snap.Version || snap.RunID != v.RunID || !slices.Equal(v.DeviceIDs, snap.DeviceIDs) || !slices.Equal(d.DeviceIDs, snap.DeviceIDs) || !registered || v.WorkflowID != spec.WorkflowID || v.Kind != rd.ApplicationKind || d.ApplicationKind != v.WorkflowID {
				return applicationInvalid(d, "AI fixed snapshot or version")
			}
			allowed := map[string]bool{v.SnapshotID + "/summary": true}
			for _, kind := range []string{"output", "evidence"} {
				for _, fact := range documents {
					if fact.Tenant == d.Tenant && fact.Kind == kind && fact.RunID == v.RunID {
						allowed[fact.ID] = true
					}
				}
			}
			for _, id := range append(slices.Clone(v.FactIDs), v.SentFactIDs...) {
				if !allowed[id] {
					return applicationInvalid(d, "unknown AI fact reference")
				}
			}
			if err := validateRestoredAIResult(d, v, documents); err != nil {
				return err
			}
			if v.Status == model.AnalysisSucceeded {
				if !json.Valid(v.Interpretation) {
					return applicationInvalid(d, "missing AI result")
				}
				var result model.AnalysisAIResult
				if json.Unmarshal(v.Interpretation, &result) != nil {
					return applicationInvalid(d, "AI result")
				}
			}
		case "review":
			v, e := decodeApplication[model.AnalysisReview](d)
			if e != nil || v.ID != d.ID || v.TenantID != d.Tenant || v.RunID != d.RunID || v.ResourceID != d.ResourceID {
				return applicationInvalid(d, "review identity")
			}
			found := false
			for _, kind := range []string{"output", "snapshot"} {
				target, ok := lookup(d, kind, v.ResourceID)
				if ok && target.RunID == v.RunID && target.Version == v.ResourceVersion {
					found = true
				}
			}
			if !found {
				return applicationInvalid(d, "review immutable resource missing")
			}
			if v.CorrectsID != "" {
				previous, ok := lookup(d, "review", v.CorrectsID)
				old, _ := decodeApplication[model.AnalysisReview](previous)
				if !ok || old.RunID != v.RunID || old.ResourceID != v.ResourceID {
					return applicationInvalid(d, "review correction chain")
				}
			}
		case "restored-object":
			v, e := decodeApplication[model.AnalysisRestoredObjectLocation](d)
			if e != nil || v.TenantID != d.Tenant || d.ID != analytics.RestoredObjectID(v.SourceBucket, v.SourceKey) || v.RestoreID == "" || v.Key != v.SourceKey || !strings.HasPrefix(v.Bucket, "iot-application-restore-") || !strings.HasPrefix(v.Key, d.Tenant+"/") || len(v.SHA256) != 64 || v.Size < 0 {
				return applicationInvalid(d, "object mapping")
			}
		}
	}
	// Every logical resource has a current pointer, and versions form one
	// contiguous chain. This catches omitted old revisions as well as dangling
	// pointers before a restored configuration can be selected by an API.
	chains := map[string][]int64{}
	for _, d := range documents {
		if d.Kind != "config" {
			continue
		}
		v, _ := decodeApplication[model.AnalysisConfigRevision](d)
		owner := ""
		if v.Scope == "PERSONAL" {
			owner = v.Creator
		}
		id, _ := analytics.AnalysisHash(struct{ Kind, Resource, Scope, Owner string }{v.Kind, v.ResourceID, v.Scope, owner})
		key := applicationIdentity(v.TenantID, "config-pointer", id)
		chains[key] = append(chains[key], v.Version)
	}
	for key, versions := range chains {
		pointer, ok := byID[key]
		if !ok {
			return errors.New("application configuration current pointer missing")
		}
		slices.Sort(versions)
		for i, version := range versions {
			if version != int64(i+1) {
				return errors.New("application configuration revision chain incomplete")
			}
		}
		if pointer.Version != versions[len(versions)-1] {
			return errors.New("application configuration current pointer is stale")
		}
	}
	return validateApplicationReferences(documents)
}
