package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func (r *Repository) AppendAIConversationTurn(ctx context.Context, c model.AIConversation, messages []model.AIConversationMessage) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var accessVersion string
	var next int
	// The row lock orders concurrent turns of one conversation.
	err = tx.QueryRow(ctx, `INSERT INTO ai_conversation(tenant_id,actor,id,workflow_id,title,access_version,message_count,created_at,updated_at)
 VALUES($1,$2,$3,$4,$5,$6,0,$7,$7)
 ON CONFLICT(tenant_id,actor,id) DO UPDATE SET updated_at=ai_conversation.updated_at
 RETURNING access_version, message_count`, c.TenantID, c.Actor, c.ID, c.WorkflowID, c.Title, c.AccessVersion, c.UpdatedAt).Scan(&accessVersion, &next)
	if err != nil {
		return err
	}
	if accessVersion != c.AccessVersion {
		return ports.ErrAIConversationAccessChanged
	}
	for _, m := range messages {
		next++
		if _, err = tx.Exec(ctx, `INSERT INTO ai_conversation_message(tenant_id,actor,conversation_id,seq,role,text,run_id,status,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			c.TenantID, c.Actor, c.ID, next, m.Role, m.Text, m.RunID, m.Status, m.CreatedAt); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE ai_conversation SET message_count=$4, updated_at=$5 WHERE tenant_id=$1 AND actor=$2 AND id=$3`, c.TenantID, c.Actor, c.ID, next, c.UpdatedAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

const aiConversationColumns = `id,tenant_id,actor,workflow_id,title,access_version,message_count,created_at,updated_at`

func scanAIConversation(row pgx.Row) (model.AIConversation, error) {
	var v model.AIConversation
	err := row.Scan(&v.ID, &v.TenantID, &v.Actor, &v.WorkflowID, &v.Title, &v.AccessVersion, &v.MessageCount, &v.CreatedAt, &v.UpdatedAt)
	return v, err
}

func (r *Repository) ListAIConversations(ctx context.Context, tenantID, actor, workflowID, accessVersion string, limit int) ([]model.AIConversation, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := r.reader().Query(ctx, `SELECT `+aiConversationColumns+` FROM ai_conversation
 WHERE tenant_id=$1 AND actor=$2 AND workflow_id=$3 AND access_version=$4 ORDER BY updated_at DESC, id DESC LIMIT $5`, tenantID, actor, workflowID, accessVersion, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.AIConversation{}
	for rows.Next() {
		v, err := scanAIConversation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *Repository) GetAIConversation(ctx context.Context, tenantID, actor, id, accessVersion string, limit int) (model.AIConversation, []model.AIConversationMessage, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	v, err := scanAIConversation(r.reader().QueryRow(ctx, `SELECT `+aiConversationColumns+` FROM ai_conversation WHERE tenant_id=$1 AND actor=$2 AND id=$3 AND access_version=$4`, tenantID, actor, id, accessVersion))
	if errors.Is(err, pgx.ErrNoRows) {
		return v, nil, ErrNotFound
	}
	if err != nil {
		return v, nil, err
	}
	rows, err := r.reader().Query(ctx, `SELECT seq,role,text,run_id,status,created_at FROM (
 SELECT seq,role,text,run_id,status,created_at FROM ai_conversation_message WHERE tenant_id=$1 AND actor=$2 AND conversation_id=$3 ORDER BY seq DESC LIMIT $4
) recent ORDER BY seq`, tenantID, actor, id, limit)
	if err != nil {
		return v, nil, err
	}
	defer rows.Close()
	messages := []model.AIConversationMessage{}
	for rows.Next() {
		var m model.AIConversationMessage
		if err = rows.Scan(&m.Seq, &m.Role, &m.Text, &m.RunID, &m.Status, &m.CreatedAt); err != nil {
			return v, nil, err
		}
		messages = append(messages, m)
	}
	return v, messages, rows.Err()
}

func (r *Repository) DeleteAIConversation(ctx context.Context, tenantID, actor, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM ai_conversation WHERE tenant_id=$1 AND actor=$2 AND id=$3`, tenantID, actor, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

var _ ports.AIConversationStore = (*Repository)(nil)
