package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

// v4 also retires existing duty model jobs. The original unknown result and
// execution deadline are retained in the private body as restore evidence.
func retireRestoredDutyJobs(ctx context.Context, tx pgx.Tx, schema, restoreID string) (int, error) {
	ident := pgx.Identifier{schema, "duty_ai_job"}.Sanitize()
	rows, err := tx.Query(ctx, "SELECT tenant_id,id,body FROM "+ident+" WHERE body->>'status' IN ('QUEUED','RUNNING','STOP_REQUESTED')")
	if err != nil {
		return 0, err
	}
	type item struct {
		tenant, id string
		body       []byte
	}
	items := []item{}
	for rows.Next() {
		var v item
		if err = rows.Scan(&v.tenant, &v.id, &v.body); err != nil {
			rows.Close()
			return 0, err
		}
		items = append(items, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC().UnixMilli()
	for _, v := range items {
		var body map[string]any
		dec := json.NewDecoder(bytes.NewReader(v.body))
		dec.UseNumber()
		if err = dec.Decode(&body); err != nil {
			return 0, err
		}
		body["status"] = "CANCELLED"
		body["stage"] = "RESTORED_TASK_RETIRED"
		body["error"] = "恢复前模型结果无法确认；任务退役且不会自动再次调用模型"
		body["leaseOwner"] = ""
		body["leaseUntil"] = 0
		body["finishedAt"] = now
		body["restoreRetirement"] = map[string]any{"restoreId": restoreID, "retiredAt": now, "original": json.RawMessage(v.body)}
		raw, _ := json.Marshal(body)
		if _, err = tx.Exec(ctx, "UPDATE "+ident+" SET body=$3,version=version+1,updated_at=$4 WHERE tenant_id=$1 AND id=$2", v.tenant, v.id, raw, now); err != nil {
			return 0, err
		}
	}
	return len(items), nil
}
