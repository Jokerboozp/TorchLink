package backup

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"
	"iot-platform/internal/analytics"
	"iot-platform/internal/analytics/recurring"
	"iot-platform/internal/model"
)

type restoredAnalysisQuery interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func restoredAnalysisDocuments(ctx context.Context, q restoredAnalysisQuery, schema string) ([]applicationDocument, error) {
	rows, err := q.Query(ctx, "SELECT to_jsonb(t) FROM "+pgx.Identifier{schema, "analysis_document"}.Sanitize()+" t")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []applicationDocument{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var document applicationDocument
		if err := json.Unmarshal(raw, &document); err != nil {
			return nil, err
		}
		result = append(result, document)
	}
	return result, rows.Err()
}

// Governance profiles and cases are domain objects, unlike the five earlier
// applications' analysis configuration revisions. Verify the actual restored
// domain references without weakening their existing configuration validator.
func validateRestoredGovernanceAnalysis(ctx context.Context, target restoredAnalysisQuery, result *RestoreResult) error {
	governance, _ := result.Components["governance"].(map[string]any)
	if governance["status"] != "restored" {
		return nil
	}
	govSchema, _ := governance["schema"].(string)
	appSchema := govSchema
	if governance["analysisDocuments"] == "application" {
		application, _ := result.Components["application"].(map[string]any)
		if application["status"] != "restored" {
			return errors.New("governance analysis component missing")
		}
		appSchema, _ = application["schema"].(string)
	}
	documents, err := restoredAnalysisDocuments(ctx, target, appSchema)
	if err != nil {
		return err
	}
	byID := map[string]applicationDocument{}
	for _, d := range documents {
		byID[applicationIdentity(d.Tenant, d.Kind, d.ID)] = d
	}
	readDomain := func(tenant, kind, id string, ids []string) (model.GovernanceDocument, error) {
		table := map[string]string{model.GovernanceCaseKind: "alarm_governance_case", model.GovernanceRoundKind: "alarm_governance_round", model.GovernancePlanKind: "alarm_governance_observation_plan", model.GovernanceProfileKind: "alarm_governance_type_profile"}[kind]
		var d model.GovernanceDocument
		err := target.QueryRow(ctx, "SELECT device_ids,COALESCE(case_id,''),COALESCE(round_id,''),body FROM "+pgx.Identifier{govSchema, table}.Sanitize()+" WHERE tenant_id=$1 AND id=$2", tenant, id).Scan(&d.DeviceIDs, &d.CaseID, &d.RoundID, &d.Body)
		if err != nil {
			return d, errors.New("restored governance analysis domain reference missing")
		}
		for _, device := range d.DeviceIDs {
			if !slices.Contains(ids, device) {
				return d, errors.New("restored governance analysis domain scope incomplete")
			}
		}
		return d, nil
	}
	for _, d := range documents {
		if d.Kind != "run" || d.ApplicationKind != analytics.KindRecurring {
			continue
		}
		run, err := decodeApplication[model.AnalysisRun](d)
		if err != nil {
			return err
		}
		var p recurring.Parameters
		if json.Unmarshal(run.Parameters, &p) != nil {
			return errors.New("restored recurring parameters invalid")
		}
		if p.CaseID != "" {
			_, err := readDomain(d.Tenant, model.GovernanceCaseKind, p.CaseID, run.DeviceIDs)
			if err != nil {
				return err
			}
		}
		if p.RoundID != "" {
			round, err := readDomain(d.Tenant, model.GovernanceRoundKind, p.RoundID, run.DeviceIDs)
			if err != nil {
				return err
			}
			if p.CaseID == "" || round.CaseID != p.CaseID {
				return errors.New("restored recurring round belongs to another case")
			}
		}
		if p.PlanID != "" {
			plan, err := readDomain(d.Tenant, model.GovernancePlanKind, p.PlanID, run.DeviceIDs)
			if err != nil {
				return err
			}
			if p.RoundID == "" || plan.RoundID != p.RoundID {
				return errors.New("restored recurring plan belongs to another round")
			}
		}
		if p.ProfileRevisionID != "" {
			if _, err := readDomain(d.Tenant, model.GovernanceProfileKind, p.ProfileRevisionID, run.DeviceIDs); err != nil {
				return err
			}
		}
		for _, id := range p.HistoricalProjectionRunIDs {
			projection, ok := byID[applicationIdentity(d.Tenant, "run", id)]
			if !ok || projection.ApplicationKind != analytics.KindRecurring {
				return errors.New("restored historical projection missing")
			}
			v, err := decodeApplication[model.AnalysisRun](projection)
			var params recurring.Parameters
			if err != nil || json.Unmarshal(v.Parameters, &params) != nil || params.JobMode != recurring.HistoricalProjectionMode || v.SnapshotID == "" || (v.Status != model.AnalysisPartial && v.Status != model.AnalysisSucceeded) || run.Start < v.Start || run.End > v.End {
				return errors.New("restored historical projection binding invalid")
			}
			for _, device := range v.DeviceIDs {
				if !slices.Contains(run.DeviceIDs, device) {
					return errors.New("restored historical projection scope incomplete")
				}
			}
		}
	}
	// Every official review/report keeps its exact fixed snapshot digest.
	for _, table := range []string{"alarm_governance_observation_review", "alarm_governance_report"} {
		rows, err := target.Query(ctx, "SELECT tenant_id,body->>'analysisSnapshotId',body->>'factsHash' FROM "+pgx.Identifier{govSchema, table}.Sanitize()+" WHERE COALESCE(body->>'analysisSnapshotId','')<>''")
		if err != nil {
			return err
		}
		for rows.Next() {
			var tenant, id, hash string
			if err = rows.Scan(&tenant, &id, &hash); err != nil {
				break
			}
			sd, ok := byID[applicationIdentity(tenant, "snapshot", id)]
			var snapshot model.AnalysisSnapshot
			if !ok || json.Unmarshal(sd.Body, &snapshot) != nil || hash == "" || snapshot.FactsHash != hash {
				err = errors.New("restored governance report frozen snapshot missing or mismatched")
				break
			}
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
