package knowledge

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"iot-platform/internal/adapters/postgres"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type fakeEmbedder struct {
	mu        sync.Mutex
	model     string
	dimension int
	queries   []string
	err       error
}

func (f *fakeEmbedder) Model() string { return f.model }
func (f *fakeEmbedder) Health(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.err
}
func (f *fakeEmbedder) Embed(_ context.Context, inputs []string, purpose ports.EmbedPurpose) ([][]float32, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	out := make([][]float32, len(inputs))
	for i, input := range inputs {
		if purpose == ports.EmbedQuery {
			f.queries = append(f.queries, input)
		}
		out[i] = make([]float32, max(f.dimension, 2))
		out[i][0], out[i][1] = 1, .5
		if strings.Contains(input, "不相关") {
			out[i][0], out[i][1] = -1, -.5
		}
	}
	return out, nil
}

type pausedQueryEmbedder struct {
	base    *fakeEmbedder
	entered chan struct{}
	resume  chan struct{}
	once    sync.Once
}

func (p *pausedQueryEmbedder) Model() string                    { return p.base.Model() }
func (p *pausedQueryEmbedder) Health(ctx context.Context) error { return p.base.Health(ctx) }
func (p *pausedQueryEmbedder) Embed(ctx context.Context, texts []string, purpose ports.EmbedPurpose) ([][]float32, error) {
	if purpose == ports.EmbedQuery {
		p.once.Do(func() { close(p.entered) })
		select {
		case <-p.resume:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return p.base.Embed(ctx, texts, purpose)
}

type knowledgeQueryResult struct {
	hits []ports.KnowledgeHit
	err  error
}

func TestVectorValidationRejectsWrongSpaceAndInvalidValues(t *testing.T) {
	for _, test := range []struct {
		name       string
		vectors    [][]float32
		count, dim int
	}{
		{"missing", nil, 1, 2},
		{"wrong dimension", [][]float32{{1, 2, 3}}, 1, 2},
		{"mixed dimension", [][]float32{{1, 2}, {1}}, 2, 2},
		{"zero", [][]float32{{0, 0}}, 1, 2},
		{"nan", [][]float32{{float32(math.NaN()), 1}}, 1, 2},
		{"infinity", [][]float32{{float32(math.Inf(1)), 1}}, 1, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := validateVectors(test.vectors, test.count, test.dim); err == nil {
				t.Fatal("invalid vector accepted")
			}
		})
	}
	if err := validateVectors([][]float32{{1, .5}}, 1, 2); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresSignatureIsolatesProviderModelDimensionAndPreprocessing(t *testing.T) {
	base := NewPostgres(nil, &fakeEmbedder{model: "m"}, PostgresOptions{Provider: "a", Dimensions: 2, Preprocessing: "one"})
	for _, alternative := range []*Postgres{
		NewPostgres(nil, &fakeEmbedder{model: "m"}, PostgresOptions{Provider: "b", Dimensions: 2, Preprocessing: "one"}),
		NewPostgres(nil, &fakeEmbedder{model: "other"}, PostgresOptions{Provider: "a", Dimensions: 2, Preprocessing: "one"}),
		NewPostgres(nil, &fakeEmbedder{model: "m"}, PostgresOptions{Provider: "a", Dimensions: 3, Preprocessing: "one"}),
		NewPostgres(nil, &fakeEmbedder{model: "m"}, PostgresOptions{Provider: "a", Dimensions: 2, Preprocessing: "two"}),
	} {
		if alternative.signature == base.signature {
			t.Fatal("different vector spaces share a signature")
		}
	}
	defaultIndex := NewPostgres(nil, &fakeEmbedder{model: "m"}, PostgresOptions{})
	if defaultIndex.options.Dimensions != 1024 {
		t.Fatalf("default dimensions = %d", defaultIndex.options.Dimensions)
	}
}

func TestChineseKeywordsAndExactIdentifiers(t *testing.T) {
	terms := keywordTokens("GB26875-Dahua 烟雾告警复位 设备型号 X-200 MQTT ALARM_REPORT")
	for _, term := range []string{"gb26875-dahua", "烟雾", "告警", "复位", "x-200", "mqtt", "alarm_report"} {
		if !containsAll(terms, []string{term}) {
			t.Errorf("missing keyword %q in %#v", term, terms)
		}
	}
	if !containsAll(keywordTokens("水压异常"), []string{"水"}) {
		t.Fatal("single-character Chinese query not indexed")
	}
}

func TestHybridRetrievalAddsLexicalCandidateAndKeepsPositions(t *testing.T) {
	semantic := []ports.KnowledgeHit{{DocumentID: "semantic", ChunkID: "s", Content: "消防设备说明", Score: .7}}
	lexical := []ports.KnowledgeHit{{DocumentID: "specific", ChunkID: "l", Content: "X-200", Score: .69, StartChar: 50, EndChar: 55, CharacterCount: 5, ChunkIndex: 2}}
	hits := mergeKnowledgeHits(semantic, lexical, keywordTokens("X-200"), 5, .5)
	if len(hits) != 2 || hits[0].DocumentID != "specific" || hits[0].StartChar != 50 {
		t.Fatalf("unexpected hybrid result %#v", hits)
	}
	if hits := mergeKnowledgeHits(semantic, lexical, keywordTokens("X-200"), 5, .99); len(hits) != 0 {
		t.Fatalf("score threshold was not applied: %#v", hits)
	}
}

func TestKeywordFallbackRanksByMatchedTermsAndMarksHits(t *testing.T) {
	hits := []ports.KnowledgeHit{
		{DocumentID: "partial", Content: "烟感 维护"},
		{DocumentID: "full", Content: "烟感离线 复位 维护步骤"},
		{DocumentID: "none", Content: "水泵"},
	}
	ranked := rankKeywordHits(hits, keywordTokens("烟感 离线 复位"), 5, .25)
	if len(ranked) != 2 || ranked[0].DocumentID != "full" || !ranked[0].KeywordOnly || ranked[0].Score <= ranked[1].Score {
		t.Fatalf("unexpected keyword ranking %#v", ranked)
	}
}

// Uses a disposable schema in an explicitly supplied PostgreSQL database.
// The embedder is deterministic; this test never calls an external model API.
func TestPostgresKnowledgePersistenceScopesAndAtomicRebuild(t *testing.T) {
	dsn := os.Getenv("IOT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("IOT_TEST_POSTGRES_DSN is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schemaName := fmt.Sprintf("knowledge_test_%d", time.Now().UnixNano())
	identifier := pgx.Identifier{schemaName}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA "+identifier+" CASCADE") }()
	// Config.ConnString returns the original DSN; editing RuntimeParams and
	// reparsing it silently loses search_path and would use shared public data.
	scopedDSN := dsn + " search_path=" + schemaName + ",public"
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, parseErr := url.Parse(dsn)
		if parseErr != nil {
			t.Fatal("invalid test PostgreSQL URL")
		}
		query := u.Query()
		query.Set("search_path", schemaName+",public")
		u.RawQuery = query.Encode()
		scopedDSN = u.String()
	}
	repo, err := postgres.New(ctx, scopedDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	pool := repo.Pool()
	var selectedSchema string
	if err = pool.QueryRow(ctx, `SELECT current_schema()`).Scan(&selectedSchema); err != nil || selectedSchema != schemaName {
		t.Fatalf("PostgreSQL test pool did not select the isolated schema: %v", err)
	}
	embedder := &fakeEmbedder{model: "m", dimension: 2}
	index := NewPostgres(pool, embedder, PostgresOptions{Provider: "test", Dimensions: 2})
	embedder.err = errors.New("no embedding API key configured")
	if hits, queryErr := index.SearchKnowledge(ctx, ports.KnowledgeSearchRequest{TenantID: "a", WorkflowID: "ops", Question: "空知识库"}); queryErr != nil || len(hits) != 0 {
		t.Fatalf("empty index called embedding API: %#v %v", hits, queryErr)
	}
	embedder.err = nil
	queueDoc := model.KnowledgeDoc{ID: "queued", TenantID: "a", WorkflowID: "ops", ObjectBucket: "archive", ObjectKey: "queued", Filename: "manual.txt", Status: "UPLOADED"}
	if err = repo.SaveKnowledgeDoc(ctx, queueDoc); err != nil {
		t.Fatal(err)
	}
	if needed, rebuildErr := index.NeedsRebuild(ctx); rebuildErr != nil || needed {
		t.Fatalf("first queued upload must use document worker: %v %v", needed, rebuildErr)
	}
	if _, err = pool.Exec(ctx, `DELETE FROM ai_knowledge_doc WHERE id='queued'`); err != nil {
		t.Fatal(err)
	}
	inputs := []ports.KnowledgeIndexInput{
		{TenantID: "a", WorkflowID: "ops", ProductID: "X-200", Category: "SOP", Tags: []string{"Certified", "Fire"}, DocumentID: "doc-a", ChunkID: "a-1", ChunkIndex: 1, StartChar: 0, EndChar: 6, CharacterCount: 6, Content: []byte("烟雾告警复位")},
		{TenantID: "a", WorkflowID: "camera", ProductID: "X-200", Category: "SOP", Tags: []string{"Certified", "Fire"}, DocumentID: "doc-camera", ChunkID: "c-1", Content: []byte("烟雾告警摄像头")},
		{TenantID: "b", WorkflowID: "ops", ProductID: "X-200", Category: "SOP", Tags: []string{"Certified", "Fire"}, DocumentID: "doc-b", ChunkID: "b-1", Content: []byte("烟雾告警其他租户")},
		{TenantID: "a", WorkflowID: "ops", ProductID: "other", Category: "other", Tags: []string{"secret"}, DocumentID: "doc-other", ChunkID: "o-1", Content: []byte("烟雾告警其他范围")},
	}
	for _, input := range inputs {
		if err = repo.SaveKnowledgeDoc(ctx, model.KnowledgeDoc{ID: input.DocumentID, TenantID: input.TenantID, WorkflowID: input.WorkflowID, ObjectBucket: "archive", ObjectKey: input.DocumentID, Filename: "manual.txt", Status: "INDEXED"}); err != nil {
			t.Fatal(err)
		}
		if err = index.IndexKnowledgeBatch(ctx, []ports.KnowledgeIndexInput{input}); err != nil {
			t.Fatal(err)
		}
	}
	request := ports.KnowledgeSearchRequest{TenantID: "a", WorkflowID: "ops", ProductIDs: []string{"x-200"}, Categories: []string{"sop"}, Tags: []string{"certified", "fire"}, Question: "烟雾告警复位", Limit: 5, MinScore: .5}
	assertOne := func(index *Postgres, content string) {
		t.Helper()
		hits, queryErr := index.SearchKnowledge(ctx, request)
		if queryErr != nil || len(hits) != 1 || hits[0].DocumentID != "doc-a" || hits[0].Content != content {
			t.Fatalf("unexpected scoped retrieval hits=%#v err=%v", hits, queryErr)
		}
		if hits[0].StartChar != 0 || hits[0].EndChar != len([]rune(content)) || hits[0].CharacterCount != len([]rune(content)) {
			t.Fatalf("character positions lost: %#v", hits[0])
		}
	}
	assertOne(index, "烟雾告警复位")
	// An unavailable vector service falls back to keywords with the same scope.
	embedder.err = errors.New("embedding service unavailable")
	if hits, queryErr := index.SearchKnowledge(ctx, request); queryErr != nil || len(hits) != 1 || hits[0].DocumentID != "doc-a" || !hits[0].KeywordOnly {
		t.Fatalf("keyword fallback hits=%#v err=%v", hits, queryErr)
	}
	embedder.err = nil
	// A waiting retry is claimed only once its time has passed.
	retryDoc := model.KnowledgeDoc{ID: "retry", TenantID: "a", WorkflowID: "ops", ObjectBucket: "archive", ObjectKey: "retry", Filename: "manual.txt", Status: "UPLOADED", Metadata: map[string]any{"indexRetryAt": time.Now().Add(time.Hour).UnixMilli()}}
	if err = repo.SaveKnowledgeDoc(ctx, retryDoc); err != nil {
		t.Fatal(err)
	}
	if _, claimed, claimErr := repo.ClaimKnowledgeDocument(ctx); claimErr != nil || claimed {
		t.Fatalf("waiting retry claimed early: %v %v", claimed, claimErr)
	}
	retryDoc.Metadata["indexRetryAt"] = time.Now().Add(-time.Second).UnixMilli()
	if _, err = repo.UpdateKnowledgeDocument(ctx, retryDoc); err != nil {
		t.Fatal(err)
	}
	if doc, claimed, claimErr := repo.ClaimKnowledgeDocument(ctx); claimErr != nil || !claimed || doc.ID != "retry" {
		t.Fatalf("due retry not claimed: %v %v %v", doc.ID, claimed, claimErr)
	}
	if _, err = pool.Exec(ctx, `DELETE FROM ai_knowledge_doc WHERE id='retry'`); err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveKnowledgeDoc(ctx, queueDoc); err != nil {
		t.Fatal(err)
	}
	if needed, rebuildErr := index.NeedsRebuild(ctx); rebuildErr != nil || needed {
		t.Fatalf("queued upload triggered full rebuild: %v %v", needed, rebuildErr)
	}
	queueDoc.Status = "INDEX_FAILED"
	queueDoc.Metadata = map[string]any{"size": 2048, "chunks": 5}
	if err = repo.SaveKnowledgeDoc(ctx, queueDoc); err != nil {
		t.Fatal(err)
	}
	// Totals cover the whole tenant; failed documents add size but not chunks.
	if summary, summaryErr := repo.KnowledgeDocSummary(ctx, "a"); summaryErr != nil || summary != (model.KnowledgeSummary{Documents: 4, Indexed: 3, Failed: 1, Bytes: 2048}) {
		t.Fatalf("unexpected knowledge summary %#v %v", summary, summaryErr)
	}
	if summary, summaryErr := repo.KnowledgeDocSummary(ctx, "b"); summaryErr != nil || summary.Documents != 1 || summary.Failed != 0 {
		t.Fatalf("knowledge summary crossed tenants %#v %v", summary, summaryErr)
	}
	// Document jobs share the index lock with each other but never with a rebuild.
	releaseA, lockedA, lockErr := repo.TryKnowledgeDocumentLock(ctx)
	releaseB, lockedB, lockErrB := repo.TryKnowledgeDocumentLock(ctx)
	if lockErr != nil || lockErrB != nil || !lockedA || !lockedB {
		t.Fatalf("document jobs did not run in parallel: %v %v %v %v", lockedA, lockedB, lockErr, lockErrB)
	}
	if _, rebuilding, _ := repo.TryKnowledgeReindexLock(ctx); rebuilding {
		t.Fatal("rebuild started while document jobs were running")
	}
	releaseA()
	releaseB()
	releaseRebuild, rebuilding, lockErr := repo.TryKnowledgeReindexLock(ctx)
	if lockErr != nil || !rebuilding {
		t.Fatalf("rebuild lock not acquired after jobs finished: %v", lockErr)
	}
	if _, lockedA, _ = repo.TryKnowledgeDocumentLock(ctx); lockedA {
		t.Fatal("document job started during a rebuild")
	}
	releaseRebuild()
	if needed, rebuildErr := index.NeedsRebuild(ctx); rebuildErr != nil || needed {
		t.Fatalf("failed upload triggered repeated full rebuild: %v %v", needed, rebuildErr)
	}
	queueDoc.Status = "INDEXED"
	if err = repo.SaveKnowledgeDoc(ctx, queueDoc); err != nil {
		t.Fatal(err)
	}
	if needed, rebuildErr := index.NeedsRebuild(ctx); rebuildErr != nil || !needed {
		t.Fatalf("missing historical index was not detected: %v %v", needed, rebuildErr)
	}
	if _, err = pool.Exec(ctx, `DELETE FROM ai_knowledge_doc WHERE id='queued'`); err != nil {
		t.Fatal(err)
	}
	if len(embedder.queries) != 1 || embedder.queries[0] != request.Question {
		t.Fatalf("question was not embedded as query: %#v", embedder.queries)
	}
	embedder.err = errors.New("no embedding API key configured")
	for _, noScope := range []ports.KnowledgeSearchRequest{
		{TenantID: "missing", WorkflowID: "ops", Question: "无租户文档"},
		{TenantID: "a", WorkflowID: "missing", Question: "无Agent文档"},
		{TenantID: "a", WorkflowID: "ops", ProductIDs: []string{"missing"}, Question: "无产品文档"},
		{TenantID: "a", WorkflowID: "ops", Tags: []string{"missing"}, Question: "无标签文档"},
	} {
		if hits, queryErr := index.SearchKnowledge(ctx, noScope); queryErr != nil || len(hits) != 0 {
			t.Fatalf("empty authorized scope called embedding API: %#v %v", hits, queryErr)
		}
	}
	embedder.err = nil
	// A new adapter finds the same persisted index; no in-memory fallback.
	restarted := NewPostgres(pool, &fakeEmbedder{model: "m", dimension: 2}, PostgresOptions{Provider: "test", Dimensions: 2})
	assertOne(restarted, "烟雾告警复位")
	chunks, err := restarted.ListKnowledgeChunks(ctx, "a", "doc-a")
	if err != nil || len(chunks) != 1 || !chunks[0].Vectorized || chunks[0].Index != 1 {
		t.Fatalf("stored chunk inspection: %#v err=%v", chunks, err)
	}
	newEmbedder := &fakeEmbedder{model: "new-model", dimension: 2}
	oldConfig := ports.EmbeddingConfig{BaseURL: "https://example.test/v1", Model: "m", APIKey: "test-old-key", Dimensions: 2}
	newConfig := oldConfig
	newConfig.Model, newConfig.APIKey = "new-model", "test-new-key"
	if err = repo.SaveEmbeddingConfig(ctx, oldConfig, true); err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveEmbeddingConfig(ctx, newConfig, false); err != nil {
		t.Fatal(err)
	}
	replacement := NewPostgres(pool, newEmbedder, PostgresOptions{Provider: "test", Dimensions: 2, ExpectedConfig: &newConfig})
	if needed, rebuildErr := replacement.NeedsRebuild(ctx); rebuildErr != nil || !needed {
		t.Fatalf("new model must require rebuild: needed=%v err=%v", needed, rebuildErr)
	}
	if _, err = replacement.SearchKnowledge(ctx, request); !errors.Is(err, ErrKnowledgeIndexStale) {
		t.Fatalf("mixed vector space was allowed: %v", err)
	}
	if err = replacement.ResetIndex(ctx); err != nil {
		t.Fatal(err)
	}
	// Queued uploads and unsuccessful first uploads cannot prevent an otherwise
	// complete replacement. Failed documents with live old chunks still must be
	// included; a previous legacy rebuild failure remains required without chunks.
	for _, doc := range []model.KnowledgeDoc{
		{ID: "queued-during-rebuild", Status: "UPLOADED"},
		{ID: "never-indexed-failure", Status: "INDEX_FAILED"},
		{ID: "legacy-rebuild-failure", Status: "INDEX_FAILED", Metadata: map[string]any{"rebuildRequired": true}},
	} {
		doc.TenantID, doc.WorkflowID, doc.ObjectBucket, doc.ObjectKey, doc.Filename = "a", "ops", "archive", doc.ID, "manual.txt"
		if err = repo.SaveKnowledgeDoc(ctx, doc); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = pool.Exec(ctx, `UPDATE ai_knowledge_doc SET status='INDEX_FAILED' WHERE id='doc-other'`); err != nil {
		t.Fatal(err)
	}
	eligible, err := replacement.RebuildDocumentIDs(ctx)
	if err != nil || !eligible["doc-other"] || !eligible["legacy-rebuild-failure"] || eligible["queued-during-rebuild"] || eligible["never-indexed-failure"] {
		t.Fatalf("rebuild eligibility lost existing knowledge or included a new upload: %#v %v", eligible, err)
	}
	// The legacy row is checked above and then deleted, as it has no original in
	// this adapter test. The core reindexer tests exercise its retry metadata.
	if _, err = pool.Exec(ctx, `DELETE FROM ai_knowledge_doc WHERE id='legacy-rebuild-failure'`); err != nil {
		t.Fatal(err)
	}
	newEmbedder.err = errors.New("embedding API unavailable")
	if err = replacement.IndexKnowledgeBatch(ctx, []ports.KnowledgeIndexInput{inputs[0]}); err == nil {
		t.Fatal("failed embedding was accepted")
	}
	if err = replacement.ActivateIndex(ctx); err == nil {
		t.Fatal("incomplete replacement was activated")
	}
	assertOne(index, "烟雾告警复位")
	newEmbedder.err = nil
	// Pin an old version, then pause the network call while the replacement is
	// written and activated. The in-flight read must finish against old chunks.
	paused := &pausedQueryEmbedder{base: &fakeEmbedder{model: "m", dimension: 2}, entered: make(chan struct{}), resume: make(chan struct{})}
	inflight := NewPostgres(pool, paused, PostgresOptions{Provider: "test", Dimensions: 2})
	queryResult := make(chan knowledgeQueryResult, 1)
	go func() {
		hits, queryErr := inflight.SearchKnowledge(ctx, request)
		queryResult <- knowledgeQueryResult{hits, queryErr}
	}()
	select {
	case <-paused.entered:
	case <-ctx.Done():
		t.Fatal("in-flight query did not reach embedding")
	}
	for i, input := range inputs {
		if i == 0 {
			input.Content = []byte("烟雾告警复位新规")
			input.CharacterCount, input.EndChar = 8, 8
		}
		if err = replacement.IndexKnowledgeBatch(ctx, []ports.KnowledgeIndexInput{input}); err != nil {
			t.Fatal(err)
		}
	}
	assertOne(index, "烟雾告警复位")
	// Another replica can save a newer desired configuration while the candidate
	// is embedding. Reject the stale activation before changing SQL readers or
	// their credential snapshot.
	newerConfig := newConfig
	newerConfig.Model = "next-model"
	if err = repo.SaveEmbeddingConfig(ctx, newerConfig, false); err != nil {
		t.Fatal(err)
	}
	if err = replacement.ActivateIndex(ctx); err == nil {
		t.Fatal("a superseded embedding configuration was activated")
	}
	var activeModel string
	if err = pool.QueryRow(ctx, `SELECT model FROM ai_knowledge_index_version WHERE status='active'`).Scan(&activeModel); err != nil || activeModel != "m" {
		t.Fatalf("rejected activation changed the SQL vector space: %s %v", activeModel, err)
	}
	if saved, found, loadErr := repo.LoadEmbeddingConfig(ctx, true); loadErr != nil || !found || saved != oldConfig {
		t.Fatal("rejected activation changed the active embedding credentials")
	}
	if err = repo.SaveEmbeddingConfig(ctx, newConfig, false); err != nil {
		t.Fatal(err)
	}
	if err = replacement.ActivateIndex(ctx); err != nil {
		t.Fatal(err)
	}
	if !replacement.ActiveEmbeddingConfigPersisted() {
		t.Fatal("activation did not confirm atomic configuration persistence")
	}
	if saved, found, loadErr := repo.LoadEmbeddingConfig(ctx, true); loadErr != nil || !found || saved != newConfig {
		t.Fatal("activated SQL index and active embedding configuration differ")
	}
	// Same-space API key rotation does not rebuild vectors, but must still be
	// guarded against a newer desired configuration and persist for restarts.
	rotatedConfig := newConfig
	rotatedConfig.APIKey = "test-rotated-key"
	rotated := NewPostgres(pool, newEmbedder, PostgresOptions{Provider: "test", Dimensions: 2, ExpectedConfig: &rotatedConfig})
	if err = rotated.PersistActiveEmbeddingConfig(ctx); err == nil {
		t.Fatal("unsaved credential rotation overwrote the active configuration")
	}
	if err = repo.SaveEmbeddingConfig(ctx, rotatedConfig, false); err != nil {
		t.Fatal(err)
	}
	if err = rotated.PersistActiveEmbeddingConfig(ctx); err != nil || !rotated.ActiveEmbeddingConfigPersisted() {
		t.Fatalf("same-space credential rotation did not persist: %v", err)
	}
	if saved, found, loadErr := repo.LoadEmbeddingConfig(ctx, true); loadErr != nil || !found || saved != rotatedConfig {
		t.Fatal("same-space credential rotation was not durable")
	}
	close(paused.resume)
	select {
	case result := <-queryResult:
		if result.err != nil || len(result.hits) != 1 || result.hits[0].Content != "烟雾告警复位" {
			t.Fatalf("model activation broke in-flight query: %#v %v", result.hits, result.err)
		}
	case <-ctx.Done():
		t.Fatal("in-flight query did not complete")
	}
	assertOne(replacement, "烟雾告警复位新规")
	// In-flight old model readers can finish against their pinned retired version.
	assertOne(index, "烟雾告警复位")
	restartedOld := NewPostgres(pool, &fakeEmbedder{model: "m", dimension: 2}, PostgresOptions{Provider: "test", Dimensions: 2})
	assertOne(restartedOld, "烟雾告警复位")
	if needed, rebuildErr := restartedOld.NeedsRebuild(ctx); rebuildErr != nil || !needed {
		t.Fatalf("restored old vector space must remain stale: %v %v", needed, rebuildErr)
	}
	if needed, rebuildErr := replacement.NeedsRebuild(ctx); rebuildErr != nil || needed {
		t.Fatalf("completed replacement still needs rebuild: %v %v", needed, rebuildErr)
	}
	// Complete document batches replace obsolete chunks in a transaction.
	second := inputs[0]
	second.ChunkID, second.ChunkIndex, second.StartChar, second.EndChar = "a-2", 2, 4, 10
	if err = replacement.IndexKnowledgeBatch(ctx, []ports.KnowledgeIndexInput{inputs[0], second}); err != nil {
		t.Fatal(err)
	}
	if err = replacement.IndexKnowledgeBatch(ctx, []ports.KnowledgeIndexInput{inputs[0]}); err != nil {
		t.Fatal(err)
	}
	chunks, err = replacement.ListKnowledgeChunks(ctx, "a", "doc-a")
	if err != nil || len(chunks) != 1 {
		t.Fatalf("obsolete chunks survived replacement: %#v %v", chunks, err)
	}
	newEmbedder.dimension = 3
	if err = replacement.IndexKnowledgeBatch(ctx, []ports.KnowledgeIndexInput{inputs[0]}); err == nil {
		t.Fatal("wrong dimensions accepted")
	}
	newEmbedder.dimension = 2
	assertOne(replacement, "烟雾告警复位")
	pausedDelete := &pausedQueryEmbedder{base: &fakeEmbedder{model: "new-model", dimension: 2}, entered: make(chan struct{}), resume: make(chan struct{})}
	deletingReader := NewPostgres(pool, pausedDelete, PostgresOptions{Provider: "test", Dimensions: 2})
	deleteResult := make(chan knowledgeQueryResult, 1)
	go func() {
		hits, queryErr := deletingReader.SearchKnowledge(ctx, request)
		deleteResult <- knowledgeQueryResult{hits, queryErr}
	}()
	select {
	case <-pausedDelete.entered:
	case <-ctx.Done():
		t.Fatal("deletion query did not reach embedding")
	}
	// A persisted deletion intent immediately removes knowledge from retrieval
	// while object cleanup may still be retrying.
	if _, err = pool.Exec(ctx, `UPDATE ai_knowledge_doc SET status='DELETING' WHERE tenant_id='a' AND id='doc-a'`); err != nil {
		t.Fatal(err)
	}
	if hits, queryErr := replacement.SearchKnowledge(ctx, request); queryErr != nil || len(hits) != 0 {
		t.Fatalf("deleting knowledge recalled: %#v %v", hits, queryErr)
	}
	if err = replacement.IndexKnowledgeBatch(ctx, []ports.KnowledgeIndexInput{inputs[0]}); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("deleted document was resurrected by indexing: %v", err)
	}
	// A physically deleted metadata row cascades chunks from every version.
	if _, err = pool.Exec(ctx, `DELETE FROM ai_knowledge_doc WHERE tenant_id='a' AND id='doc-a'`); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM ai_knowledge_chunk WHERE tenant_id='a' AND document_id='doc-a'`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("deleted document left indexed versions: %d %v", remaining, err)
	}
	close(pausedDelete.resume)
	select {
	case result := <-deleteResult:
		if result.err != nil || len(result.hits) != 0 {
			t.Fatalf("deleted knowledge leaked into in-flight query: %#v %v", result.hits, result.err)
		}
	case <-ctx.Done():
		t.Fatal("deletion query did not complete")
	}
	// Deletion affects all model versions but cannot delete another tenant.
	if err = replacement.DeleteKnowledgeDocument(ctx, "a", "doc-a", "ops"); err != nil {
		t.Fatal(err)
	}
	if hits, queryErr := replacement.SearchKnowledge(ctx, request); queryErr != nil || len(hits) != 0 {
		t.Fatalf("deleted knowledge recalled: %#v %v", hits, queryErr)
	}
	other, err := replacement.SearchKnowledge(ctx, ports.KnowledgeSearchRequest{TenantID: "b", WorkflowID: "ops", Question: "烟雾告警"})
	if err != nil || len(other) != 1 || other[0].DocumentID != "doc-b" {
		t.Fatalf("another tenant was deleted: %#v %v", other, err)
	}
	var versions int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM ai_knowledge_index_version`).Scan(&versions); err != nil || versions != 2 {
		t.Fatalf("version history lost: %d %v", versions, err)
	}
	var hnswIndexes int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM pg_indexes WHERE schemaname=$1 AND indexname LIKE 'ai_knowledge_hnsw_%' AND indexdef LIKE '%hnsw%' AND indexdef LIKE '%vector_cosine_ops%'`, schemaName).Scan(&hnswIndexes); err != nil || hnswIndexes != 2 {
		t.Fatalf("version cosine HNSW indexes missing: %d %v", hnswIndexes, err)
	}
	if err = replacement.Health(ctx); err != nil {
		t.Fatal(err)
	}
}

type fakeReranker struct {
	scores []float64
	err    error
	seen   []string
}

func (f *fakeReranker) Rerank(_ context.Context, _ string, documents []string) ([]float64, error) {
	f.seen = documents
	return f.scores, f.err
}

func TestRerankReordersBoundedTrimmedCandidatesAndFallsBack(t *testing.T) {
	long := strings.Repeat("长", 1000)
	hits := []ports.KnowledgeHit{{DocumentID: "a", Content: long, Score: .9}, {DocumentID: "b", Content: "b", Score: .8}, {DocumentID: "c", Content: "c", Score: .7}}
	reranker := &fakeReranker{scores: []float64{.1, .95, .5}}
	p := &Postgres{options: PostgresOptions{Reranker: reranker}}
	ranked := p.rerank(context.Background(), "q", hits, 2)
	if len(ranked) != 2 || ranked[0].DocumentID != "b" || ranked[1].DocumentID != "c" || ranked[0].Score != .95 {
		t.Fatalf("rerank order %#v", ranked)
	}
	if len([]rune(reranker.seen[0])) != rerankPassageRunes {
		t.Fatal("passages are not trimmed before reranking")
	}
	reranker.err = errors.New("rerank service unavailable")
	if kept := p.rerank(context.Background(), "q", hits, 2); kept[0].DocumentID != "a" || kept[1].DocumentID != "b" {
		t.Fatalf("failed rerank must keep the hybrid order: %#v", kept)
	}
	if rerankPool(5) != rerankCandidates || rerankPool(100) != rerankMaxCandidates {
		t.Fatal("rerank candidate pool not bounded")
	}
}
