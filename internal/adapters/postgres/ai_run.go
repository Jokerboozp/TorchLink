package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func (r *Repository) SaveAIRun(ctx context.Context, v model.AIRunRecord) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO ai_workflow_run(run_id,tenant_id,actor,workflow_id,prompt_version,model,input_bytes,output_bytes,input_tokens,output_tokens,cache_read_tokens,reasoning_tokens,usage_reported,tool_calls,duration_ms,status,error,started_at,finished_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19) ON CONFLICT(run_id) DO NOTHING`,
		v.RunID, v.TenantID, v.Actor, v.WorkflowID, v.PromptVersion, v.Model, v.InputBytes, v.OutputBytes, v.Usage.InputTokens, v.Usage.OutputTokens, v.Usage.CacheReadTokens, v.Usage.ReasoningTokens, v.UsageReported, v.ToolCalls, v.DurationMs, v.Status, v.Error, v.StartedAt, v.FinishedAt)
	return err
}

func aiRunFilterSQL(f ports.AIRunFilter) (string, []any) {
	where, args := []string{"tenant_id=$1"}, []any{f.TenantID}
	add := func(clause string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(clause, len(args)))
	}
	if f.WorkflowID != "" {
		add("workflow_id=$%d", f.WorkflowID)
	}
	if f.Status != "" {
		add("status=$%d", f.Status)
	}
	if f.Start > 0 {
		add("started_at>=$%d", f.Start)
	}
	if f.End > 0 {
		add("started_at<=$%d", f.End)
	}
	return " WHERE " + strings.Join(where, " AND "), args
}

func (r *Repository) ListAIRuns(ctx context.Context, f ports.AIRunFilter) ([]model.AIRunRecord, int, error) {
	where, args := aiRunFilterSQL(f)
	var total int
	if err := r.reader().QueryRow(ctx, `SELECT count(*) FROM ai_workflow_run`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	args = append(args, limit, max(f.Offset, 0))
	rows, err := r.reader().Query(ctx, fmt.Sprintf(`SELECT run_id,tenant_id,actor,workflow_id,prompt_version,model,input_bytes,output_bytes,input_tokens,output_tokens,cache_read_tokens,reasoning_tokens,usage_reported,tool_calls,duration_ms,status,error,started_at,finished_at
 FROM ai_workflow_run%s ORDER BY started_at DESC, run_id DESC LIMIT $%d OFFSET $%d`, where, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []model.AIRunRecord{}
	for rows.Next() {
		var v model.AIRunRecord
		if err := rows.Scan(&v.RunID, &v.TenantID, &v.Actor, &v.WorkflowID, &v.PromptVersion, &v.Model, &v.InputBytes, &v.OutputBytes, &v.Usage.InputTokens, &v.Usage.OutputTokens, &v.Usage.CacheReadTokens, &v.Usage.ReasoningTokens, &v.UsageReported, &v.ToolCalls, &v.DurationMs, &v.Status, &v.Error, &v.StartedAt, &v.FinishedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, v)
	}
	return out, total, rows.Err()
}

// AIRunUsage counts days in model.ReportZone, like the alarm reports.
func (r *Repository) AIRunUsage(ctx context.Context, f ports.AIRunFilter) ([]model.AIRunUsage, error) {
	where, args := aiRunFilterSQL(f)
	_, offset := time.Now().In(model.ReportZone).Zone()
	args = append(args, int64(offset)*1000)
	rows, err := r.reader().Query(ctx, fmt.Sprintf(`SELECT to_char(to_timestamp((started_at + $%d) / 1000.0) AT TIME ZONE 'UTC', 'YYYY-MM-DD') AS day, workflow_id,
  count(*), count(*) FILTER (WHERE status<>'SUCCEEDED'), coalesce(sum(input_tokens),0), coalesce(sum(output_tokens),0), coalesce(sum(cache_read_tokens),0), coalesce(sum(reasoning_tokens),0),
  coalesce(sum(tool_calls),0), coalesce(sum(duration_ms),0)
 FROM ai_workflow_run%s GROUP BY 1, 2 ORDER BY 1, 2`, len(args), where), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.AIRunUsage{}
	for rows.Next() {
		var v model.AIRunUsage
		if err := rows.Scan(&v.Day, &v.WorkflowID, &v.Runs, &v.Failed, &v.Usage.InputTokens, &v.Usage.OutputTokens, &v.Usage.CacheReadTokens, &v.Usage.ReasoningTokens, &v.ToolCalls, &v.DurationMs); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

var _ ports.AIRunStore = (*Repository)(nil)
