package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"iot-platform/internal/analytics"
	"iot-platform/internal/model"
)

// Keep an audit copy of the original execution envelope. Neither an elapsed
// lease nor an unconfirmed external answer is resumed after a restore.
func retireApplicationExecutions(ctx context.Context, tx pgx.Tx, schema, restoreID string, documents []applicationDocument) (int, error) {
	now := time.Now().UTC().UnixMilli()
	n := 0
	ident := pgx.Identifier{schema, "analysis_document"}.Sanitize()
	for _, d := range documents {
		var body []byte
		var status string
		version := d.Version
		switch d.Kind {
		case "run":
			run, _ := decodeApplication[model.AnalysisRun](d)
			if analytics.TerminalAnalysisStatus(run.Status) {
				continue
			}
			run.Status = model.AnalysisCancelled
			run.Stage = "RESTORED_TASK_RETIRED"
			run.Error = "任务恢复后已退役；须显式创建新任务"
			run.LeaseOwner = ""
			run.LeaseExpiresAt = 0
			run.LeaseToken++
			run.Version++
			run.UpdatedAt = now
			run.CompletedAt = now
			body, _ = json.Marshal(run)
			status = run.Status
			version = run.Version
		case "ai":
			job, _ := decodeApplication[model.AnalysisAIRevision](d)
			if analytics.TerminalAnalysisStatus(job.Status) {
				continue
			}
			job.Status = model.AnalysisCancelled
			job.Error = "恢复前模型结果无法确认；任务退役且不会自动再次调用模型"
			job.LeaseOwner = ""
			job.LeaseExpiresAt = 0
			job.Deadline = 0
			job.LeaseToken++
			job.Version++
			job.CompletedAt = now
			body, _ = json.Marshal(job)
			status = job.Status
			version = job.Version
		default:
			continue
		}
		audit, _ := json.Marshal(struct {
			RestoreID string              `json:"restoreId"`
			RetiredAt int64               `json:"retiredAt"`
			Original  applicationDocument `json:"original"`
		}{restoreID, now, d})
		if _, err := tx.Exec(ctx, "INSERT INTO "+ident+"(tenant_id,kind,id,run_id,device_id,application_kind,resource_id,status,device_ids,version,created_at,body) VALUES($1,'restore-retirement',$2,$3,'','','','RETIRED',$4,1,$5,$6)", d.Tenant, d.Kind+"/"+d.ID, d.RunID, d.DeviceIDs, now, audit); err != nil {
			return n, err
		}
		if _, err := tx.Exec(ctx, "UPDATE "+ident+" SET body=$4,status=$5,version=$6 WHERE tenant_id=$1 AND kind=$2 AND id=$3", d.Tenant, d.Kind, d.ID, body, status, version); err != nil {
			return n, err
		}
		n++
	}
	pending := pgx.Identifier{schema, "alarm_rule_revision_pending"}.Sanitize()
	rows, err := tx.Query(ctx, "SELECT tenant_id,revision_id,device_id,to_jsonb(t) FROM "+pending+" t")
	if err != nil {
		return n, err
	}
	type row struct {
		tenant, id, device string
		body               []byte
	}
	items := []row{}
	for rows.Next() {
		var v row
		if err = rows.Scan(&v.tenant, &v.id, &v.device, &v.body); err != nil {
			rows.Close()
			return n, err
		}
		items = append(items, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return n, err
	}
	for _, v := range items {
		audit, _ := json.Marshal(map[string]any{"restoreId": restoreID, "retiredAt": now, "reason": "PROCESSING_CLOCK_BOUNDARY", "original": json.RawMessage(v.body)})
		if _, err = tx.Exec(ctx, "INSERT INTO "+ident+"(tenant_id,kind,id,run_id,device_id,application_kind,resource_id,status,device_ids,version,created_at,body) VALUES($1,'restore-retirement',$2,'',$3,'RULE_PENDING','','RETIRED',$4,1,$5,$6)", v.tenant, fmt.Sprintf("pending/%s/%s", v.id, v.device), v.device, []string{v.device}, now, audit); err != nil {
			return n, err
		}
	}
	if _, err = tx.Exec(ctx, "DELETE FROM "+pending); err != nil {
		return n, err
	}
	return n + len(items), nil
}
