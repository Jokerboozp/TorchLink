package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

var ErrKnowledgeIndexStale = errors.New("嵌入模型已变更，需要重建知识库索引")

const knowledgeVersionLock int64 = 728194604

// PostgresOptions describes the vector space, including preprocessing. Keys
// are intentionally excluded: rotating credentials must not invalidate vectors.
type PostgresOptions struct {
	Provider       string
	Dimensions     int
	Preprocessing  string
	ExpectedConfig *ports.EmbeddingConfig
}

// Postgres shares the platform repository pool. It owns neither the pool nor
// document originals. Rebuilds write a separate version and only activate it
// after all documents have been written successfully.
type Postgres struct {
	pool                  *pgxpool.Pool
	embedder              ports.Embedder
	options               PostgresOptions
	signature             string
	mu                    sync.Mutex
	pending               string
	readID                string
	activeConfigPersisted bool
}

var (
	_ ports.KnowledgeBase            = (*Postgres)(nil)
	_ ports.FilteredKnowledgeBase    = (*Postgres)(nil)
	_ ports.BatchKnowledgeBase       = (*Postgres)(nil)
	_ ports.InspectableKnowledgeBase = (*Postgres)(nil)
	_ ports.RebuildableKnowledgeBase = (*Postgres)(nil)
	_ ports.KnowledgeDocumentDeleter = (*Postgres)(nil)
)

func NewPostgres(pool *pgxpool.Pool, embedder ports.Embedder, options PostgresOptions) *Postgres {
	if options.ExpectedConfig != nil {
		cfg := *options.ExpectedConfig
		options.ExpectedConfig = &cfg
	}
	if options.Dimensions <= 0 {
		options.Dimensions = 1024
	}
	if options.Preprocessing == "" {
		options.Preprocessing = "unicode-text/chunk1200-overlap200/v1"
	}
	modelName := ""
	if embedder != nil {
		modelName = embedder.Model()
		if identified, ok := embedder.(interface{ Signature() string }); ok {
			options.Preprocessing += "\nembedding-signature=" + identified.Signature()
		}
	}
	descriptor := strings.Join([]string{options.Provider, modelName, strconv.Itoa(options.Dimensions), options.Preprocessing}, "\x00")
	sum := sha256.Sum256([]byte(descriptor))
	return &Postgres{pool: pool, embedder: embedder, options: options, signature: hex.EncodeToString(sum[:])}
}

func (p *Postgres) EmbeddingModel() string {
	if p.embedder == nil {
		return ""
	}
	return p.embedder.Model()
}

func (p *Postgres) configured() error {
	if p.pool == nil {
		return errors.New("knowledge PostgreSQL pool is not configured")
	}
	if p.embedder == nil {
		return errors.New("knowledge embedding API is not configured")
	}
	// pgvector HNSW supports up to 2000 dimensions for single precision vectors.
	if p.options.Dimensions < 1 || p.options.Dimensions > 2000 {
		return errors.New("knowledge embedding dimensions must be between 1 and 2000")
	}
	return nil
}

func (p *Postgres) Index(ctx context.Context, tenant, product, id string, data []byte) error {
	return p.IndexKnowledge(ctx, ports.KnowledgeIndexInput{TenantID: tenant, ProductID: product, DocumentID: id, ChunkID: id, Content: data})
}

func (p *Postgres) IndexKnowledge(ctx context.Context, input ports.KnowledgeIndexInput) error {
	return p.indexBatch(ctx, []ports.KnowledgeIndexInput{input}, false)
}

func (p *Postgres) IndexKnowledgeBatch(ctx context.Context, inputs []ports.KnowledgeIndexInput) error {
	return p.indexBatch(ctx, inputs, true)
}

func (p *Postgres) indexBatch(ctx context.Context, inputs []ports.KnowledgeIndexInput, replace bool) error {
	if len(inputs) == 0 {
		return nil
	}
	if err := p.configured(); err != nil {
		return err
	}
	first := inputs[0]
	texts := make([]string, len(inputs))
	seen := make(map[string]bool, len(inputs))
	for i, input := range inputs {
		if strings.TrimSpace(input.TenantID) == "" || strings.TrimSpace(input.DocumentID) == "" || strings.TrimSpace(input.ChunkID) == "" || strings.TrimSpace(string(input.Content)) == "" {
			return errors.New("knowledge tenant, document, chunk and content are required")
		}
		if input.TenantID != first.TenantID || input.WorkflowID != first.WorkflowID || input.DocumentID != first.DocumentID {
			return errors.New("knowledge batch must contain exactly one scoped document")
		}
		if seen[input.ChunkID] {
			return errors.New("knowledge batch contains duplicate chunk IDs")
		}
		seen[input.ChunkID] = true
		texts[i] = string(input.Content)
	}
	vectors, err := p.embedder.Embed(ctx, texts, ports.EmbedDocument)
	if err != nil {
		return fmt.Errorf("knowledge embedding: %w", err)
	}
	if err = validateVectors(vectors, len(inputs), p.options.Dimensions); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, knowledgeVersionLock); err != nil {
		return err
	}
	// Lock the original metadata row as well as the index version. Deleting a
	// document cascades all versions, and an already deleted document cannot be
	// resurrected by a background embedding request that finishes late.
	var documentPresent bool
	err = tx.QueryRow(ctx, `SELECT true FROM ai_knowledge_doc WHERE tenant_id=$1 AND workflow_id=$2 AND id=$3 AND status<>'DELETING' FOR KEY SHARE`, first.TenantID, first.WorkflowID, first.DocumentID).Scan(&documentPresent)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ErrNotFound
	}
	if err != nil {
		return err
	}
	versionID, err := p.writeVersion(ctx, tx)
	if err != nil {
		return err
	}
	if replace {
		if _, err = tx.Exec(ctx, `DELETE FROM ai_knowledge_chunk WHERE version_id=$1 AND tenant_id=$2 AND workflow_id=$3 AND document_id=$4`, versionID, first.TenantID, first.WorkflowID, first.DocumentID); err != nil {
			return err
		}
	}
	for i, input := range inputs {
		count := len([]rune(texts[i]))
		if input.CharacterCount > 0 && input.CharacterCount != count {
			return errors.New("knowledge chunk character count does not match its content")
		}
		start, end := input.StartChar, input.EndChar
		if end == 0 && start == 0 {
			end = count
		}
		if start < 0 || end-start != count || input.OverlapChars < 0 || input.OverlapChars > count {
			return errors.New("knowledge chunk has invalid character positions")
		}
		_, err = tx.Exec(ctx, `INSERT INTO ai_knowledge_chunk(version_id,tenant_id,workflow_id,document_id,chunk_id,product_id,category,tags,chunk_index,start_char,end_char,character_count,overlap_chars,content,keyword_tokens,dimensions,embedding)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17::public.vector)
			ON CONFLICT(version_id,tenant_id,workflow_id,document_id,chunk_id) DO UPDATE SET product_id=excluded.product_id,category=excluded.category,tags=excluded.tags,chunk_index=excluded.chunk_index,start_char=excluded.start_char,end_char=excluded.end_char,character_count=excluded.character_count,overlap_chars=excluded.overlap_chars,content=excluded.content,keyword_tokens=excluded.keyword_tokens,dimensions=excluded.dimensions,embedding=excluded.embedding`,
			versionID, input.TenantID, input.WorkflowID, input.DocumentID, input.ChunkID, strings.TrimSpace(input.ProductID), strings.TrimSpace(input.Category), normalizedValues(input.Tags), max(input.ChunkIndex, 1), start, end, count, input.OverlapChars, texts[i], keywordTokens(texts[i]), p.options.Dimensions, vectorLiteral(vectors[i]))
		if err != nil {
			return fmt.Errorf("write knowledge chunk: %w", err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	if p.pending == "" {
		p.readID = versionID
	}
	ports.ReportKnowledgeIndexProgress(ctx, len(inputs), len(inputs))
	return nil
}

func (p *Postgres) writeVersion(ctx context.Context, tx pgx.Tx) (string, error) {
	if p.pending != "" {
		var signature, status string
		err := tx.QueryRow(ctx, `SELECT signature,status FROM ai_knowledge_index_version WHERE id=$1 FOR UPDATE`, p.pending).Scan(&signature, &status)
		if err != nil {
			return "", err
		}
		if signature != p.signature || status != "building" {
			return "", ErrKnowledgeIndexStale
		}
		return p.pending, nil
	}
	var id, signature string
	err := tx.QueryRow(ctx, `SELECT id,signature FROM ai_knowledge_index_version WHERE status='active' FOR UPDATE`).Scan(&id, &signature)
	if errors.Is(err, pgx.ErrNoRows) {
		return p.createVersion(ctx, tx, "active")
	}
	if err != nil {
		return "", err
	}
	if signature != p.signature {
		return "", ErrKnowledgeIndexStale
	}
	return id, nil
}

func (p *Postgres) createVersion(ctx context.Context, tx pgx.Tx, status string) (string, error) {
	id := uuid.NewString()
	_, err := tx.Exec(ctx, `INSERT INTO ai_knowledge_index_version(id,signature,provider,model,dimensions,preprocessing,status,activated_at) VALUES($1,$2,$3,$4,$5,$6,$7,CASE WHEN $7='active' THEN now() END)`, id, p.signature, p.options.Provider, p.EmbeddingModel(), p.options.Dimensions, p.options.Preprocessing, status)
	if err != nil {
		return "", err
	}
	// A partial expression index keeps every model and dimension isolated.
	name := pgx.Identifier{"ai_knowledge_hnsw_" + strings.ReplaceAll(id, "-", "")}.Sanitize()
	query := fmt.Sprintf(`CREATE INDEX %s ON ai_knowledge_chunk USING hnsw ((embedding::public.vector(%d)) public.vector_cosine_ops) WHERE version_id='%s'`, name, p.options.Dimensions, id)
	if _, err = tx.Exec(ctx, query); err != nil {
		return "", fmt.Errorf("create knowledge cosine index: %w", err)
	}
	return id, nil
}

func (p *Postgres) NeedsRebuild(ctx context.Context) (bool, error) {
	if err := p.configured(); err != nil {
		return false, err
	}
	var id, signature string
	err := p.pool.QueryRow(ctx, `SELECT id,signature FROM ai_knowledge_index_version WHERE status='active'`).Scan(&id, &signature)
	if errors.Is(err, pgx.ErrNoRows) {
		var present bool
		// Pending/failed uploads belong to the durable document worker. Only
		// already indexed historical documents require an initial full rebuild.
		err = p.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ai_knowledge_doc WHERE status='INDEXED' OR metadata @> '{"rebuildRequired":true}'::jsonb)`).Scan(&present)
		return present, err
	}
	if err != nil {
		return false, err
	}
	if signature != p.signature {
		return true, nil
	}
	var missing bool
	err = p.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ai_knowledge_doc d WHERE d.status<>'DELETING' AND (d.status='INDEXED' OR d.metadata @> '{"rebuildRequired":true}'::jsonb) AND NOT EXISTS(SELECT 1 FROM ai_knowledge_chunk c WHERE c.version_id=$1 AND c.tenant_id=d.tenant_id AND c.workflow_id=d.workflow_id AND c.document_id=d.id))`, id).Scan(&missing)
	return missing, err
}

// RebuildDocumentIDs excludes failed or pending uploads which have never served
// readers. Existing indexed documents and failed replacement attempts remain
// required, including documents migrated from the previous index implementation.
func (p *Postgres) RebuildDocumentIDs(ctx context.Context) (map[string]bool, error) {
	rows, err := p.pool.Query(ctx, `SELECT d.id FROM ai_knowledge_doc d WHERE d.status<>'DELETING' AND (`+rebuildDocumentCondition+`)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := map[string]bool{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids[id] = true
	}
	return ids, rows.Err()
}

const rebuildDocumentCondition = `d.status='INDEXED' OR d.metadata @> '{"rebuildRequired":true}'::jsonb OR EXISTS(SELECT 1 FROM ai_knowledge_chunk old_chunk JOIN ai_knowledge_index_version old_version ON old_version.id=old_chunk.version_id AND old_version.status='active' WHERE old_chunk.document_id=d.id AND old_chunk.tenant_id=d.tenant_id AND old_chunk.workflow_id=d.workflow_id)`

// ResetIndex starts a replacement version. The live version remains intact.
// Callers serialize the entire rebuild using KnowledgeReindexStore's lock.
func (p *Postgres) ResetIndex(ctx context.Context) error {
	if err := p.configured(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, knowledgeVersionLock); err != nil {
		return err
	}
	// A restarted/retried rebuild supersedes unfinished attempts. Their model
	// descriptors remain available for audit, but their partial chunks and
	// indexes have never served readers and need not accumulate on each retry.
	if _, err = tx.Exec(ctx, `UPDATE ai_knowledge_index_version SET status='failed' WHERE status='building'`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM ai_knowledge_chunk c USING ai_knowledge_index_version v WHERE c.version_id=v.id AND v.status='failed'`); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT id FROM ai_knowledge_index_version WHERE status='failed'`)
	if err != nil {
		return err
	}
	failedIDs := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		if _, parseErr := uuid.Parse(id); parseErr != nil {
			rows.Close()
			return errors.New("knowledge index version has an invalid identifier")
		}
		failedIDs = append(failedIDs, id)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, id := range failedIDs {
		indexName := pgx.Identifier{"ai_knowledge_hnsw_" + strings.ReplaceAll(id, "-", "")}.Sanitize()
		if _, err = tx.Exec(ctx, `DROP INDEX IF EXISTS `+indexName); err != nil {
			return err
		}
	}
	id, err := p.createVersion(ctx, tx, "building")
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	p.pending = id
	return nil
}

// ActivateIndex commits the completed version atomically; retired chunks are retained for
// in-flight readers and audit, rather than dropping any live data.
func (p *Postgres) ActivateIndex(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pending == "" {
		return nil
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, knowledgeVersionLock); err != nil {
		return err
	}
	var missing int
	if err = p.checkExpectedConfig(ctx, tx); err != nil {
		return err
	}
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM ai_knowledge_doc d WHERE d.status<>'DELETING' AND (`+rebuildDocumentCondition+`) AND NOT EXISTS(SELECT 1 FROM ai_knowledge_chunk c WHERE c.version_id=$1 AND c.tenant_id=d.tenant_id AND c.workflow_id=d.workflow_id AND c.document_id=d.id)`, p.pending).Scan(&missing); err != nil {
		return err
	}
	if missing > 0 {
		return fmt.Errorf("knowledge replacement is incomplete: %d documents missing", missing)
	}
	if _, err = tx.Exec(ctx, `UPDATE ai_knowledge_index_version SET status='retired' WHERE status='active'`); err != nil {
		return err
	}
	result, err := tx.Exec(ctx, `UPDATE ai_knowledge_index_version SET status='active',activated_at=now() WHERE id=$1 AND signature=$2 AND status='building'`, p.pending, p.signature)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrKnowledgeIndexStale
	}
	if err = p.persistActiveConfig(ctx, tx); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	p.readID, p.pending = p.pending, ""
	p.activeConfigPersisted = p.options.ExpectedConfig != nil
	return nil
}

func (p *Postgres) checkExpectedConfig(ctx context.Context, tx pgx.Tx) error {
	if p.options.ExpectedConfig == nil {
		return nil
	}
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT config FROM ai_model_config WHERE id='__embedding__' AND enabled=true`).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		// An environment-only first deployment has no saved desired configuration.
		return nil
	}
	if err != nil {
		return err
	}
	var cfg ports.EmbeddingConfig
	if err = json.Unmarshal(raw, &cfg); err != nil {
		return errors.New("stored embedding configuration is invalid")
	}
	if cfg != *p.options.ExpectedConfig {
		return errors.New("embedding configuration changed during rebuild")
	}
	return nil
}

func (p *Postgres) persistActiveConfig(ctx context.Context, tx pgx.Tx) error {
	if p.options.ExpectedConfig == nil {
		return nil
	}
	raw, err := json.Marshal(*p.options.ExpectedConfig)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO ai_model_config(id,tenant_id,provider,model,config,enabled,updated_at) VALUES('__embedding_active__','__global__','embedding-api',$1,$2,true,now()) ON CONFLICT(id) DO UPDATE SET model=excluded.model,config=excluded.config,enabled=true,updated_at=now()`, p.options.ExpectedConfig.Model, raw)
	return err
}

// ActiveEmbeddingConfigPersisted reports successful atomic activation, rather
// than merely advertising support: credential-only changes also require a CAS.
func (p *Postgres) ActiveEmbeddingConfigPersisted() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.activeConfigPersisted
}

// PersistActiveEmbeddingConfig handles a matching space already activated by
// another replica and same-space credential or request-setting changes.
func (p *Postgres) PersistActiveEmbeddingConfig(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.options.ExpectedConfig == nil {
		return nil
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, knowledgeVersionLock); err != nil {
		return err
	}
	if err = p.checkExpectedConfig(ctx, tx); err != nil {
		return err
	}
	var signature string
	err = tx.QueryRow(ctx, `SELECT signature FROM ai_knowledge_index_version WHERE status='active'`).Scan(&signature)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	// Before the first indexed upload there is no vector space to mismatch.
	if err == nil && signature != p.signature {
		return ErrKnowledgeIndexStale
	}
	if err = p.persistActiveConfig(ctx, tx); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	p.activeConfigPersisted = true
	return nil
}

func (p *Postgres) readVersion(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var id, signature string
	err := p.pool.QueryRow(ctx, `SELECT id,signature FROM ai_knowledge_index_version WHERE status='active'`).Scan(&id, &signature)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if signature == p.signature {
		p.readID = id
		return id, nil
	}
	if p.readID != "" {
		var present bool
		err = p.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ai_knowledge_index_version WHERE id=$1 AND signature=$2 AND status IN ('active','retired'))`, p.readID, p.signature).Scan(&present)
		if err != nil {
			return "", err
		}
		if present {
			return p.readID, nil
		}
	}
	// A runtime can restart while the configured replacement is still rebuilding
	// or while its active configuration is being persisted. Recover the last
	// fully activated snapshot for this exact vector space; a partial build must
	// never be eligible. NeedsRebuild still reports the current mismatch.
	err = p.pool.QueryRow(ctx, `SELECT id FROM ai_knowledge_index_version WHERE signature=$1 AND status='retired' ORDER BY activated_at DESC NULLS LAST,created_at DESC LIMIT 1`, p.signature).Scan(&id)
	if err == nil {
		p.readID = id
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	return "", ErrKnowledgeIndexStale
}

func (p *Postgres) Search(ctx context.Context, tenant, question string, limit int) ([]string, error) {
	hits, err := p.SearchKnowledge(ctx, ports.KnowledgeSearchRequest{TenantID: tenant, Question: question, Limit: limit})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(hits))
	for _, hit := range hits {
		out = append(out, hit.Content)
	}
	return out, nil
}

func (p *Postgres) SearchKnowledge(ctx context.Context, input ports.KnowledgeSearchRequest) ([]ports.KnowledgeHit, error) {
	if err := p.configured(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.TenantID) == "" {
		return nil, errors.New("knowledge tenant scope is required")
	}
	if strings.TrimSpace(input.Question) == "" {
		return []ports.KnowledgeHit{}, nil
	}
	version, err := p.readVersion(ctx)
	if err != nil {
		return nil, err
	}
	if version == "" {
		return []ports.KnowledgeHit{}, nil
	}
	// Scope conditions are identical for the empty check, semantic and lexical
	// retrieval. An empty authorized index does not need an embedding API call.
	versionLiteral := "'" + strings.ReplaceAll(version, "'", "''") + "'"
	where := fmt.Sprintf(`version_id=%s AND tenant_id=$1 AND ($2='' OR workflow_id=$2) AND (cardinality($3::text[])=0 OR lower(product_id)=ANY($3::text[])) AND (cardinality($4::text[])=0 OR lower(category)=ANY($4::text[])) AND tags @> $5::text[] AND EXISTS(SELECT 1 FROM ai_knowledge_doc d WHERE d.id=ai_knowledge_chunk.document_id AND d.tenant_id=ai_knowledge_chunk.tenant_id AND d.workflow_id=ai_knowledge_chunk.workflow_id AND d.status<>'DELETING')`, versionLiteral)
	args := []any{input.TenantID, input.WorkflowID, normalizedValues(input.ProductIDs), normalizedValues(input.Categories), normalizedValues(input.Tags)}
	var searchable bool
	if err = p.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ai_knowledge_chunk WHERE `+where+`)`, args...).Scan(&searchable); err != nil {
		return nil, err
	}
	if !searchable {
		return []ports.KnowledgeHit{}, nil
	}
	vectors, err := p.embedder.Embed(ctx, []string{input.Question}, ports.EmbedQuery)
	if err != nil {
		return nil, fmt.Errorf("knowledge query embedding: %w", err)
	}
	if err = validateVectors(vectors, 1, p.options.Dimensions); err != nil {
		return nil, err
	}
	limit := min(max(input.Limit, 1), 100)
	if input.Limit <= 0 {
		limit = 5
	}
	candidateLimit := min(max(limit*5, 30), 500)
	terms := keywordTokens(input.Question)
	vector := fmt.Sprintf(`embedding::public.vector(%d) OPERATOR(public.<=>) $6::public.vector(%d)`, p.options.Dimensions, p.options.Dimensions)
	fields := `document_id,chunk_id,workflow_id,product_id,category,tags,content,chunk_index,start_char,end_char,character_count,overlap_chars,(SELECT d.filename FROM ai_knowledge_doc d WHERE d.id=ai_knowledge_chunk.document_id AND d.tenant_id=ai_knowledge_chunk.tenant_id)`
	args = append(args, vectorLiteral(vectors[0]), candidateLimit)
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SET LOCAL hnsw.iterative_scan='strict_order'`); err != nil {
		return nil, err
	}
	query := `SELECT ` + fields + `,greatest(0,least(1,1-(` + vector + `)/2)) AS score FROM ai_knowledge_chunk WHERE ` + where + ` ORDER BY ` + vector + ` LIMIT $7`
	semantic, err := queryKnowledgeHits(ctx, tx, query, args...)
	if err != nil {
		return nil, err
	}
	// ANN filters can exhaust candidates. An exact scoped scan restores the
	// expected number without removing tenant/Agent restrictions.
	if len(semantic) < candidateLimit {
		if _, err = tx.Exec(ctx, `SET LOCAL enable_indexscan=off`); err != nil {
			return nil, err
		}
		semantic, err = queryKnowledgeHits(ctx, tx, query, args...)
		if err != nil {
			return nil, err
		}
	}
	lexical := []ports.KnowledgeHit{}
	if len(terms) > 0 {
		args = append(args, terms)
		query = `SELECT ` + fields + `,greatest(0,least(1,1-(` + vector + `)/2)) AS score FROM ai_knowledge_chunk WHERE ` + where + ` AND keyword_tokens && $8::text[] ORDER BY cardinality(ARRAY(SELECT unnest(keyword_tokens) INTERSECT SELECT unnest($8::text[]))) DESC LIMIT $7`
		lexical, err = queryKnowledgeHits(ctx, tx, query, args...)
		if err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return mergeKnowledgeHits(semantic, lexical, terms, limit, input.MinScore), nil
}

func queryKnowledgeHits(ctx context.Context, tx pgx.Tx, query string, args ...any) ([]ports.KnowledgeHit, error) {
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ports.KnowledgeHit{}
	for rows.Next() {
		var hit ports.KnowledgeHit
		if err = rows.Scan(&hit.DocumentID, &hit.ChunkID, &hit.WorkflowID, &hit.ProductID, &hit.Category, &hit.Tags, &hit.Content, &hit.ChunkIndex, &hit.StartChar, &hit.EndChar, &hit.CharacterCount, &hit.OverlapChars, &hit.Filename, &hit.Score); err != nil {
			return nil, err
		}
		out = append(out, hit)
	}
	return out, rows.Err()
}

func mergeKnowledgeHits(semantic, lexical []ports.KnowledgeHit, terms []string, limit int, minScore float64) []ports.KnowledgeHit {
	byID := make(map[string]ports.KnowledgeHit, len(semantic)+len(lexical))
	for _, candidates := range [][]ports.KnowledgeHit{semantic, lexical} {
		for _, hit := range candidates {
			key := strings.Join([]string{hit.WorkflowID, hit.DocumentID, hit.ChunkID}, "\x00")
			keywords := keywordTokens(hit.Content)
			matched := 0
			for _, term := range terms {
				if containsAll(keywords, []string{term}) {
					matched++
				}
			}
			// Scores retain normalized cosine certainty; exact terms can increase
			// rank while a high-confidence semantic match is retained.
			keywordScore := float64(matched) / float64(max(1, len(terms)))
			hit.Score = max(hit.Score, .8*hit.Score+.2*keywordScore)
			if hit.Score >= minScore {
				byID[key] = hit
			}
		}
	}
	out := make([]ports.KnowledgeHit, 0, len(byID))
	for _, hit := range byID {
		out = append(out, hit)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].DocumentID+out[i].ChunkID < out[j].DocumentID+out[j].ChunkID
		}
		return out[i].Score > out[j].Score
	})
	return out[:min(limit, len(out))]
}

func (p *Postgres) ListKnowledgeChunks(ctx context.Context, tenant, documentID string) ([]model.KnowledgeChunk, error) {
	if p.pool == nil || strings.TrimSpace(tenant) == "" {
		return nil, errors.New("knowledge PostgreSQL and tenant scope are required")
	}
	version, err := p.readVersion(ctx)
	if err != nil {
		return nil, err
	}
	if version == "" {
		return []model.KnowledgeChunk{}, nil
	}
	rows, err := p.pool.Query(ctx, `SELECT document_id,chunk_id,chunk_index,start_char,end_char,character_count,overlap_chars,content FROM ai_knowledge_chunk WHERE version_id=$1 AND tenant_id=$2 AND ($3='' OR document_id=$3) AND EXISTS(SELECT 1 FROM ai_knowledge_doc d WHERE d.id=ai_knowledge_chunk.document_id AND d.tenant_id=ai_knowledge_chunk.tenant_id AND d.workflow_id=ai_knowledge_chunk.workflow_id AND d.status<>'DELETING') ORDER BY document_id,chunk_index,chunk_id`, version, tenant, documentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.KnowledgeChunk{}
	for rows.Next() {
		var chunk model.KnowledgeChunk
		if err = rows.Scan(&chunk.DocumentID, &chunk.ChunkID, &chunk.Index, &chunk.StartChar, &chunk.EndChar, &chunk.CharacterCount, &chunk.OverlapChars, &chunk.Content); err != nil {
			return nil, err
		}
		chunk.Vectorized = true
		out = append(out, chunk)
	}
	return out, rows.Err()
}

func (p *Postgres) DeleteKnowledgeDocument(ctx context.Context, tenant, documentID, workflowID string) error {
	if p.pool == nil || strings.TrimSpace(tenant) == "" || strings.TrimSpace(documentID) == "" {
		return errors.New("knowledge PostgreSQL, tenant and document are required")
	}
	_, err := p.pool.Exec(ctx, `DELETE FROM ai_knowledge_chunk WHERE tenant_id=$1 AND document_id=$2 AND ($3='' OR workflow_id=$3)`, tenant, documentID, workflowID)
	return err
}

func (p *Postgres) Health(ctx context.Context) error {
	if p.pool == nil {
		return errors.New("knowledge PostgreSQL pool is not configured")
	}
	var ready bool
	err := p.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname='vector') AND to_regclass('ai_knowledge_chunk') IS NOT NULL`).Scan(&ready)
	if err != nil {
		return err
	}
	if !ready {
		return errors.New("knowledge pgvector schema is not initialized")
	}
	return nil
}

func validateVectors(vectors [][]float32, count, dimensions int) error {
	if len(vectors) != count {
		return fmt.Errorf("embedding API returned %d vectors for %d inputs", len(vectors), count)
	}
	for _, vector := range vectors {
		if len(vector) != dimensions {
			return fmt.Errorf("embedding vector dimension is %d, expected %d", len(vector), dimensions)
		}
		norm := float64(0)
		for _, value := range vector {
			if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
				return errors.New("embedding API returned a non-finite vector")
			}
			norm += float64(value) * float64(value)
		}
		if norm == 0 {
			return errors.New("embedding API returned a zero vector")
		}
	}
	return nil
}

func vectorLiteral(vector []float32) string {
	parts := make([]string, len(vector))
	for i, value := range vector {
		parts[i] = strconv.FormatFloat(float64(value), 'g', -1, 32)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func normalizedValues(values []string) []string {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			set[value] = true
		}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

// keywordTokens keeps protocol/model identifiers intact and adds Chinese
// bigrams; PostgreSQL's default text search does not segment Chinese words.
func keywordTokens(text string) []string {
	out := []string{}
	var run []rune
	flush := func() {
		if len(run) == 0 {
			return
		}
		out = append(out, string(run))
		if unicode.Is(unicode.Han, run[0]) {
			for i := range run {
				out = append(out, string(run[i]))
				if i+1 < len(run) {
					out = append(out, string(run[i:i+2]))
				}
			}
		}
		run = nil
	}
	for _, r := range strings.ToLower(text) {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_' && r != '.' {
			flush()
			continue
		}
		if len(run) > 0 && unicode.Is(unicode.Han, r) != unicode.Is(unicode.Han, run[0]) {
			flush()
		}
		run = append(run, r)
	}
	flush()
	return normalizedValues(out)
}
