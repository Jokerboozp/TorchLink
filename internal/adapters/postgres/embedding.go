package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func (r *Repository) LoadEmbeddingConfig(ctx context.Context, active bool) (ports.EmbeddingConfig, bool, error) {
	id := "__embedding__"
	if active {
		id = "__embedding_active__"
	}
	var raw []byte
	err := r.pool.QueryRow(ctx, "SELECT config FROM ai_model_config WHERE id=$1 AND enabled=true", id).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.EmbeddingConfig{}, false, nil
	}
	if err != nil {
		return ports.EmbeddingConfig{}, false, err
	}
	var cfg ports.EmbeddingConfig
	err = json.Unmarshal(raw, &cfg)
	return cfg, err == nil, err
}

func (r *Repository) SaveEmbeddingConfig(ctx context.Context, cfg ports.EmbeddingConfig, active bool) error {
	id := "__embedding__"
	if active {
		id = "__embedding_active__"
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(728194604)); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO ai_model_config(id,tenant_id,provider,model,config,enabled,updated_at)
 VALUES($1,'__global__','embedding-api',$2,$3,true,now())
 ON CONFLICT(id) DO UPDATE SET model=excluded.model,config=excluded.config,enabled=true,updated_at=now()`, id, cfg.Model, raw)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Claims have a bounded lease; a crashed process's job is picked up again.
func (r *Repository) ClaimKnowledgeDocument(ctx context.Context) (model.KnowledgeDoc, bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return model.KnowledgeDoc{}, false, err
	}
	defer tx.Rollback(ctx)
	var id, tenant string
	err = tx.QueryRow(ctx, `SELECT id,tenant_id FROM ai_knowledge_doc
 WHERE status IN ('UPLOADED','DELETING') OR (status='INDEXING' AND COALESCE((metadata->>'indexLeaseUntil')::bigint,0)<(extract(epoch from now())*1000)::bigint)
 ORDER BY created_at,id FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&id, &tenant)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.KnowledgeDoc{}, false, nil
	}
	if err != nil {
		return model.KnowledgeDoc{}, false, err
	}
	_, err = tx.Exec(ctx, `UPDATE ai_knowledge_doc SET status=CASE WHEN status='DELETING' THEN 'DELETING' ELSE 'INDEXING' END,metadata=metadata || jsonb_build_object('indexLeaseUntil',(extract(epoch from now()+interval '5 minutes')*1000)::bigint,'indexStage','embedding') WHERE id=$1 AND tenant_id=$2`, id, tenant)
	if err != nil {
		return model.KnowledgeDoc{}, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return model.KnowledgeDoc{}, false, err
	}
	docs, err := r.queryKnowledgeDocs(ctx, `SELECT id,tenant_id,coalesce(workflow_id,''),coalesce(product_id,''),coalesce(category,''),coalesce(tags,'{}'),object_bucket,object_key,filename,status,metadata,(extract(epoch from created_at)*1000)::bigint FROM ai_knowledge_doc WHERE id=$1 AND tenant_id=$2`, id, tenant)
	if err != nil || len(docs) == 0 {
		return model.KnowledgeDoc{}, false, err
	}
	return docs[0], true, nil
}

func (r *Repository) UpdateKnowledgeDocument(ctx context.Context, doc model.KnowledgeDoc) (bool, error) {
	raw, err := json.Marshal(doc.Metadata)
	if err != nil {
		return false, err
	}
	result, err := r.pool.Exec(ctx, `UPDATE ai_knowledge_doc SET status=$3,metadata=$4 WHERE id=$1 AND tenant_id=$2 AND status<>'DELETING'`, doc.ID, doc.TenantID, doc.Status, raw)
	return result.RowsAffected() > 0, err
}
