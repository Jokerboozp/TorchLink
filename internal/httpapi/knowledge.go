package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"iot-platform/internal/aiworkflow"
	"iot-platform/internal/core"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// knowledgeWorkflowExists reports whether documents may be bound to the
// workflow: an enabled chat Agent or the alarm analysis workflow.
func (s *Server) knowledgeWorkflowExists(ctx context.Context, workflowID string) (bool, error) {
	if s.engine.AIWorkflows == nil {
		// Without a Harness nothing can retrieve the documents yet; uploads stay
		// possible so an index can be prepared before the sidecar is deployed.
		return true, nil
	}
	items, err := s.engine.AIWorkflows.ListWorkflows(ctx)
	if err != nil {
		return false, err
	}
	for _, item := range knowledgeWorkflowPlugins(items) {
		if item.ID == workflowID {
			return true, nil
		}
	}
	return false, nil
}

func defaultWorkflowKnowledgeBinding(tenantID, workflowID string) model.WorkflowKnowledgeBinding {
	return aiworkflow.DefaultWorkflowKnowledgeBinding(tenantID, workflowID)
}

func (s *Server) workflowKnowledgeBinding(w http.ResponseWriter, r *http.Request) {
	c := claims(r)
	workflowID := strings.TrimSpace(r.PathValue("id"))
	if workflowID == "" || len(workflowID) > 128 {
		problem(w, 422, "valid workflow id is required")
		return
	}
	if r.Method == http.MethodGet {
		binding, err := s.engine.Repo.GetWorkflowKnowledgeBinding(r.Context(), c.TenantID, workflowID)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		if binding.WorkflowID == "" {
			binding = defaultWorkflowKnowledgeBinding(c.TenantID, workflowID)
		}
		write(w, 200, binding)
		return
	}
	var in struct {
		RetrievalMode string   `json:"retrievalMode"`
		TopK          int      `json:"topK"`
		MinScore      float64  `json:"minScore"`
		NoMatchPolicy string   `json:"noMatchPolicy"`
		ProductIDs    []string `json:"productIds"`
		Categories    []string `json:"categories"`
		Tags          []string `json:"tags"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	if !oneOf(in.RetrievalMode, "auto", "always", "disabled") || !oneOf(in.NoMatchPolicy, "allow-model", "require-evidence") || in.TopK < 1 || in.TopK > 20 || in.MinScore < 0 || in.MinScore > 1 || in.RetrievalMode == "disabled" && in.NoMatchPolicy == "require-evidence" {
		problem(w, 422, "invalid knowledge binding policy")
		return
	}
	binding := model.WorkflowKnowledgeBinding{
		TenantID: c.TenantID, WorkflowID: workflowID,
		// Knowledge documents are directly associated with a workflow/Agent;
		// this binding stores only retrieval policy, not another filter layer.
		ProductIDs: cleanStringList(in.ProductIDs, 16, 128), Categories: cleanStringList(in.Categories, 16, 40), Tags: cleanStringList(in.Tags, 16, 40),
		RetrievalMode: in.RetrievalMode, TopK: in.TopK, MinScore: in.MinScore, NoMatchPolicy: in.NoMatchPolicy, UpdatedAt: time.Now().UnixMilli(),
	}
	if err := s.engine.Repo.SaveWorkflowKnowledgeBinding(r.Context(), binding); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit(r, "ai.workflow.knowledge-binding.save", "ai-workflow", workflowID, map[string]any{"retrievalMode": binding.RetrievalMode, "topK": binding.TopK})
	write(w, 200, binding)
}

// testWorkflowKnowledge runs one search with the policy being edited, without
// saving it or calling a model, so operators can see what an Agent would cite.
// Results stay inside the caller's tenant and the Agent's own documents.
func (s *Server) testWorkflowKnowledge(w http.ResponseWriter, r *http.Request) {
	c := claims(r)
	workflowID := strings.TrimSpace(r.PathValue("id"))
	var in struct {
		Question string  `json:"question"`
		TopK     int     `json:"topK"`
		MinScore float64 `json:"minScore"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	question := ports.BoundKnowledgeQuery(in.Question)
	if workflowID == "" || len(workflowID) > 128 || question == "" || in.TopK < 1 || in.TopK > 20 || in.MinScore < 0 || in.MinScore > 1 {
		problem(w, http.StatusUnprocessableEntity, "请填写测试问题，召回数量 1–20，最低相似度 0–1")
		return
	}
	if s.engine.KB == nil {
		problem(w, http.StatusServiceUnavailable, "知识库检索服务未配置")
		return
	}
	binding, err := s.engine.Repo.GetWorkflowKnowledgeBinding(r.Context(), c.TenantID, workflowID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if binding.WorkflowID == "" {
		binding = defaultWorkflowKnowledgeBinding(c.TenantID, workflowID)
	}
	binding.TopK, binding.MinScore = in.TopK, in.MinScore
	started := time.Now()
	hits, err := s.searchWorkflowKnowledge(r.Context(), c.TenantID, question, binding)
	if err != nil {
		s.log.Warn("knowledge test search failed", "workflowId", workflowID, "error", err)
		problem(w, http.StatusBadGateway, "知识检索失败，请检查向量服务状态后重试")
		return
	}
	items := make([]map[string]any, 0, len(hits))
	for _, hit := range hits {
		content := []rune(hit.Content)
		if len(content) > 600 {
			content = append(content[:600], '…')
		}
		items = append(items, map[string]any{"documentId": hit.DocumentID, "filename": hit.Filename, "chunkIndex": hit.ChunkIndex, "score": math.Round(hit.Score*1000) / 1000, "keywordOnly": hit.KeywordOnly, "content": string(content)})
	}
	write(w, http.StatusOK, map[string]any{"items": items, "keywordOnly": aiworkflow.KeywordOnlyHits(hits), "durationMs": time.Since(started).Milliseconds()})
}

func (s *Server) searchWorkflowKnowledge(ctx context.Context, tenantID, question string, binding model.WorkflowKnowledgeBinding) ([]ports.KnowledgeHit, error) {
	return aiworkflow.SearchWorkflowKnowledge(ctx, s.engine.KB, tenantID, question, binding)
}

func (s *Server) knowledgeDocs(w http.ResponseWriter, r *http.Request) {
	pagination := parseListPagination(r)
	items, total, err := s.engine.Repo.ListKnowledgeDocsPage(r.Context(), claims(r).TenantID, pagination.PageSize, pagination.Offset)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	_, persistent := s.engine.KB.(ports.EmbeddingRuntime)
	indexMode := "local-memory"
	if persistent {
		indexMode = "postgres-pgvector"
	}
	summary, err := s.engine.Repo.KnowledgeDocSummary(r.Context(), claims(r).TenantID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	meta := map[string]any{"indexMode": indexMode, "persistentIndex": persistent, "indexState": s.engine.KnowledgeReindex.Status(), "summary": summary}
	if embeddingModel := knowledgeEmbeddingModel(s); embeddingModel != "" {
		meta["embeddingModel"] = embeddingModel
	}
	writeList(w, 200, items, total, pagination, meta)
}

// knowledgeEmbeddingModel names the vector space of the persistent index.
func knowledgeEmbeddingModel(s *Server) string {
	if index, ok := s.engine.KB.(interface{ EmbeddingModel() string }); ok {
		return index.EmbeddingModel()
	}
	return ""
}

func (s *Server) knowledgeDocumentDetail(w http.ResponseWriter, r *http.Request) {
	documentID := strings.TrimSpace(r.PathValue("id"))
	if documentID == "" {
		problem(w, http.StatusUnprocessableEntity, "document id is required")
		return
	}
	document, err := s.engine.Repo.GetKnowledgeDoc(r.Context(), claims(r).TenantID, documentID)
	if errors.Is(err, model.ErrNotFound) {
		problem(w, http.StatusNotFound, "knowledge document not found")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	inspector, ok := s.engine.KB.(ports.InspectableKnowledgeBase)
	if !ok {
		problem(w, http.StatusNotImplemented, "the configured knowledge index does not expose stored chunks")
		return
	}
	chunks, err := inspector.ListKnowledgeChunks(r.Context(), claims(r).TenantID, document.ID)
	if err != nil {
		s.internalError(w, r, fmt.Errorf("load indexed chunks: %w", err))
		return
	}
	write(w, http.StatusOK, map[string]any{
		"document": document,
		"index":    knowledgeIndexDetails(s, document, chunks),
		"chunks":   chunks,
	})
}

func knowledgeIndexDetails(s *Server, document model.KnowledgeDoc, chunks []model.KnowledgeChunk) map[string]any {
	_, persistent := s.engine.KB.(ports.EmbeddingRuntime)
	index := map[string]any{
		"mode":           "local-memory",
		"persistent":     persistent,
		"vectorizer":     "local-token-similarity",
		"embeddingModel": "",
		"chunkCount":     len(chunks),
		"extractedChars": document.Metadata["characters"],
		"chunking": map[string]any{
			"strategy":         "fixed-window-overlap",
			"size":             core.KnowledgeChunkSize,
			"overlap":          core.KnowledgeChunkOverlap,
			"unit":             "Unicode 字符（rune/code point）",
			"offsetConvention": "StartChar 包含，EndChar 不包含",
			"normalization":    "先提取文件文本，再清洗 XML/HTML 标签、空白并去除首尾空白",
		},
	}
	if persistent {
		index["mode"] = "postgres-pgvector"
		index["vectorizer"] = "external-embedding-api"
		index["embeddingModel"] = knowledgeEmbeddingModel(s)
	}
	return index
}

func (s *Server) knowledgeUpload(w http.ResponseWriter, r *http.Request) {
	const maxDocumentBytes = 32 << 20
	if s.engine.KB == nil {
		problem(w, 503, "knowledge base disabled")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxDocumentBytes+(1<<20))
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			problem(w, http.StatusRequestEntityTooLarge, "document exceeds 32 MiB")
			return
		}
		problem(w, 400, "invalid multipart form")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	f, h, err := r.FormFile("file")
	if err != nil {
		problem(w, 400, "file is required")
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxDocumentBytes+1))
	if err != nil {
		problem(w, 400, err.Error())
		return
	}
	if len(data) > maxDocumentBytes {
		problem(w, http.StatusRequestEntityTooLarge, "document exceeds 32 MiB")
		return
	}
	id := fmt.Sprintf("doc_%d", time.Now().UnixNano())
	c := claims(r)
	workflowID := strings.TrimSpace(r.FormValue("workflowId"))
	if workflowID == "" || len(workflowID) > 128 {
		problem(w, 422, "workflowId is required so every document is associated with an Agent")
		return
	}
	if known, err := s.knowledgeWorkflowExists(r.Context(), workflowID); err != nil {
		problem(w, http.StatusServiceUnavailable, "AI 工作流服务（Harness）暂不可用，无法确认文档归属的智能体")
		return
	} else if !known {
		problem(w, 422, "workflowId does not match any enabled Agent")
		return
	}
	productID := r.FormValue("productId")
	category := strings.TrimSpace(r.FormValue("category"))
	tags := cleanStringList(strings.Split(r.FormValue("tags"), ","), 16, 40)
	if len(category) > 40 {
		problem(w, 422, "category is too long")
		return
	}
	filename := strings.NewReplacer("/", "_", "\\", "_", "..", "_").Replace(h.Filename)
	bucket := "iot-knowledge-docs"
	objectKey := fmt.Sprintf("%s/agents/%s/%s/%s", c.TenantID, workflowID, id, filename)
	if _, err = s.engine.Archive.PutObject(r.Context(), bucket, objectKey, bytes.NewReader(data), int64(len(data)), h.Header.Get("Content-Type")); err != nil {
		s.log.Error("store knowledge document failed", "tenant", c.TenantID, "object", objectKey, "error", err)
		problem(w, http.StatusBadGateway, "原件保存到对象存储失败，请检查对象存储服务后重试")
		return
	}
	doc := model.KnowledgeDoc{ID: id, TenantID: c.TenantID, WorkflowID: workflowID, ProductID: productID, Category: category, Tags: tags, ObjectBucket: bucket, ObjectKey: objectKey, Filename: h.Filename, Status: "UPLOADED", CreatedAt: time.Now().UnixMilli()}
	doc.Metadata = map[string]any{"size": len(data), "contentType": h.Header.Get("Content-Type"), "indexStage": "pending", "indexProgress": map[string]int{"done": 0, "total": 0}, "chunking": map[string]any{"strategy": "fixed-window-overlap", "size": core.KnowledgeChunkSize, "overlap": core.KnowledgeChunkOverlap, "unit": "unicode-code-points", "offsetConvention": "start-inclusive,end-exclusive"}}
	if run := capacityRequestRunID(r); run != "" {
		doc.Metadata["capacityRunId"] = run
	}
	if err = s.engine.Repo.SaveKnowledgeDoc(r.Context(), doc); err != nil {
		s.failure(w, r, err, "文档记录保存失败")
		return
	}
	// The persistent worker recovers pending rows after a restart. Tests and
	// explicitly in-memory development retain synchronous indexing.
	if _, durable := s.engine.KB.(ports.EmbeddingRuntime); !durable {
		result, indexErr := core.IndexKnowledgeDocument(r.Context(), s.engine.KB, doc, data)
		if indexErr != nil {
			doc.Status = "INDEX_FAILED"
			doc.Metadata["indexError"] = indexErr.Error()
			_ = s.engine.Repo.SaveKnowledgeDoc(r.Context(), doc)
			problem(w, 422, indexErr.Error())
			return
		}
		doc.Status = "INDEXED"
		doc.Metadata["chunks"] = result.Chunks
		doc.Metadata["characters"] = result.Characters
		if err = s.engine.Repo.SaveKnowledgeDoc(r.Context(), doc); err != nil {
			s.internalError(w, r, err)
			return
		}
		write(w, 201, doc)
		return
	}
	s.audit(r, "knowledge.upload", "knowledge-document", doc.ID, map[string]any{"workflowId": workflowID})
	s.wakeKnowledgeJobs()
	write(w, 202, doc)
}
