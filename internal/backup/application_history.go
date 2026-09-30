package backup

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"
	"iot-platform/internal/model"
	"iot-platform/internal/rulelab/history"
)

func validateApplicationHistory(ctx context.Context, tx pgx.Tx, schema string) error {
	revisions := map[string]model.AlarmRuleRevision{}
	rows, err := tx.Query(ctx, "SELECT tenant_id,id,rule_id,version,hash,registered_at,body FROM "+pgx.Identifier{schema, "alarm_rule_revision"}.Sanitize())
	if err != nil {
		return err
	}
	for rows.Next() {
		var tenant, id, ruleID, hash string
		var version int
		var registered int64
		var raw []byte
		if err = rows.Scan(&tenant, &id, &ruleID, &version, &hash, &registered, &raw); err != nil {
			rows.Close()
			return err
		}
		var v model.AlarmRuleRevision
		if json.Unmarshal(raw, &v) != nil || v.TenantID != tenant || v.ID != id || v.RuleID != ruleID || v.Version != version || v.Hash != hash || v.RegisteredAt != registered || v.Rule.TenantID != tenant || v.Rule.ID != ruleID || v.Rule.Version != version || model.RuleBodyHash(v.Rule) != hash || model.RuleRevisionID(v.Rule) != id || v.SemanticsVersion == "" {
			rows.Close()
			return errors.New("invalid immutable rule revision")
		}
		revisions[applicationIdentity(tenant, "revision", id)] = v
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, revision := range revisions {
		if revision.RollbackFrom != "" {
			prior, ok := revisions[applicationIdentity(revision.TenantID, "revision", revision.RollbackFrom)]
			if !ok || prior.RuleID != revision.RuleID || prior.Version >= revision.Version {
				return errors.New("rule rollback source is missing or invalid")
			}
		}
	}
	activationVersions := map[string][]int{}
	rows, err = tx.Query(ctx, "SELECT tenant_id,rule_id,revision_id,version,since_at,deleted,body FROM "+pgx.Identifier{schema, "alarm_rule_activation"}.Sanitize())
	if err != nil {
		return err
	}
	for rows.Next() {
		var tenant, ruleID, id string
		var version int
		var since int64
		var deleted bool
		var raw []byte
		if err = rows.Scan(&tenant, &ruleID, &id, &version, &since, &deleted, &raw); err != nil {
			rows.Close()
			return err
		}
		var activation model.AlarmRuleActivation
		revision, ok := revisions[applicationIdentity(tenant, "revision", id)]
		if !ok || json.Unmarshal(raw, &activation) != nil || activation.ID != id+"/activation" || activation.TenantID != tenant || activation.RuleID != ruleID || activation.RevisionID != id || activation.Version != version || activation.Since != since || activation.Deleted != deleted || activation.Until != nil || revision.Version != version || revision.RuleID != ruleID || revision.RegisteredAt != since {
			rows.Close()
			return errors.New("rule activation body differs from its immutable revision")
		}
		key := applicationIdentity(tenant, "rule", ruleID)
		activationVersions[key] = append(activationVersions[key], version)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, versions := range activationVersions {
		slices.Sort(versions)
		for i := 1; i < len(versions); i++ {
			if versions[i] != versions[i-1]+1 {
				return errors.New("rule activation revision chain is incomplete")
			}
		}
	}
	var missingPointer int
	if err = tx.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{schema, "alarm_rule_activation"}.Sanitize()+" a WHERE NOT EXISTS (SELECT 1 FROM "+pgx.Identifier{schema, "alarm_rule_current_revision"}.Sanitize()+" c WHERE c.tenant_id=a.tenant_id AND c.rule_id=a.rule_id)").Scan(&missingPointer); err != nil {
		return err
	}
	if missingPointer != 0 {
		return errors.New("rule activation current pointer is missing")
	}
	for _, table := range []string{"alarm_rule_activation", "alarm_rule_current_revision", "alarm_rule_revision_pending"} {
		rows, err = tx.Query(ctx, "SELECT tenant_id,rule_id,revision_id FROM "+pgx.Identifier{schema, table}.Sanitize())
		if err != nil {
			return err
		}
		for rows.Next() {
			var tenant, ruleID, id string
			if err = rows.Scan(&tenant, &ruleID, &id); err != nil {
				rows.Close()
				return err
			}
			v, ok := revisions[applicationIdentity(tenant, "revision", id)]
			if !ok || v.RuleID != ruleID {
				rows.Close()
				return errors.New("rule activation or pending revision is missing")
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	var invalid int
	if err = tx.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{schema, "alarm_rule_current_revision"}.Sanitize()+" c LEFT JOIN "+pgx.Identifier{schema, "alarm_rule_revision"}.Sanitize()+" r ON r.tenant_id=c.tenant_id AND r.id=c.revision_id LEFT JOIN "+pgx.Identifier{schema, "alarm_rule_activation"}.Sanitize()+" a ON a.tenant_id=c.tenant_id AND a.rule_id=c.rule_id AND a.version=c.version WHERE r.version IS DISTINCT FROM c.version OR a.revision_id IS DISTINCT FROM c.revision_id OR a.deleted IS DISTINCT FROM c.deleted").Scan(&invalid); err != nil {
		return err
	}
	if invalid != 0 {
		return errors.New("rule current activation chain mismatch")
	}
	if err = tx.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{schema, "alarm_rule_current_revision"}.Sanitize()+" c WHERE c.version IS DISTINCT FROM (SELECT max(a.version) FROM "+pgx.Identifier{schema, "alarm_rule_activation"}.Sanitize()+" a WHERE a.tenant_id=c.tenant_id AND a.rule_id=c.rule_id)").Scan(&invalid); err != nil {
		return err
	}
	if invalid != 0 {
		return errors.New("rule current pointer is not the latest activation")
	}
	rows, err = tx.Query(ctx, "SELECT c.tenant_id,c.rule_id,c.revision_id,r.body FROM "+pgx.Identifier{schema, "alarm_rule_current_revision"}.Sanitize()+" c LEFT JOIN "+pgx.Identifier{schema, "alarm_rule"}.Sanitize()+" r ON r.tenant_id=c.tenant_id AND r.id=c.rule_id WHERE NOT c.deleted")
	if err != nil {
		return err
	}
	for rows.Next() {
		var tenant, ruleID, id string
		var raw []byte
		if err = rows.Scan(&tenant, &ruleID, &id, &raw); err != nil {
			rows.Close()
			return err
		}
		var rule model.AlarmRule
		revision := revisions[applicationIdentity(tenant, "revision", id)]
		if len(raw) == 0 || json.Unmarshal(raw, &rule) != nil || rule.TenantID != tenant || rule.ID != ruleID || model.RuleBodyHash(rule) != revision.Hash {
			rows.Close()
			return errors.New("active current rule does not match its immutable revision")
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}

	rows, err = tx.Query(ctx, "SELECT tenant_id,id,message_id,claim_token,device_id,message_timestamp,started_at,status,body FROM "+pgx.Identifier{schema, "rule_evaluation_trace"}.Sanitize())
	if err != nil {
		return err
	}
	for rows.Next() {
		var tenant, id, message, device, status string
		var token, timestamp, started int64
		var raw []byte
		if err = rows.Scan(&tenant, &id, &message, &token, &device, &timestamp, &started, &status, &raw); err != nil {
			rows.Close()
			return err
		}
		var v model.RuleEvaluationTrace
		if json.Unmarshal(raw, &v) != nil || v.ID != id || v.TenantID != tenant || v.MessageID != message || v.ClaimToken != token || v.DeviceID != device || v.MessageTimestamp != timestamp || v.StartedAt != started || v.Status != status || model.RuleTraceID(v.RuleTraceBinding) != id || model.RuleSetHash(v.Rules) != v.RuleSetHash {
			rows.Close()
			return errors.New("rule evaluation trace identity mismatch")
		}
		for _, rule := range v.Rules {
			fixed, ok := revisions[applicationIdentity(tenant, "revision", rule.ID)]
			if !ok || !sameApplicationHash(fixed, rule) {
				rows.Close()
				return errors.New("trace immutable rule set mismatch")
			}
		}
		for _, step := range v.Steps {
			for _, rid := range []string{step.RuleRevisionID, step.Alarm.CreatedRuleRevision, step.Alarm.TriggerRuleRevision} {
				if rid != "" {
					if _, ok := revisions[applicationIdentity(tenant, "revision", rid)]; !ok {
						rows.Close()
						return errors.New("trace referenced rule revision missing")
					}
				}
			}
			if step.Before.RecoveryRevision != nil {
				fixed, ok := revisions[applicationIdentity(tenant, "revision", step.Before.RecoveryRevision.ID)]
				if !ok || !sameApplicationHash(fixed, *step.Before.RecoveryRevision) {
					rows.Close()
					return errors.New("trace recovery revision mismatch")
				}
			}
		}
		if v.ReproductionQuality == "EXACT" && (v.Status != "COMPLETE" || history.Finish(v, v.FinishedAt).ReproductionQuality != "EXACT") {
			rows.Close()
			return errors.New("incomplete trace claims exact reproduction")
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	rows, err = tx.Query(ctx, "SELECT tenant_id,rule_id,body FROM "+pgx.Identifier{schema, "alarm_record"}.Sanitize())
	if err != nil {
		return err
	}
	for rows.Next() {
		var tenant, ruleID string
		var raw []byte
		if err = rows.Scan(&tenant, &ruleID, &raw); err != nil {
			rows.Close()
			return err
		}
		var alarm model.Alarm
		if json.Unmarshal(raw, &alarm) != nil {
			rows.Close()
			return errors.New("invalid alarm history")
		}
		for _, id := range []string{alarm.CreatedRuleRevision, alarm.TriggerRuleRevision} {
			if id == "" {
				continue
			}
			v, ok := revisions[applicationIdentity(tenant, "revision", id)]
			if !ok || v.RuleID != ruleID {
				rows.Close()
				return errors.New("alarm history revision missing")
			}
		}
	}
	err = rows.Err()
	rows.Close()
	return err
}
