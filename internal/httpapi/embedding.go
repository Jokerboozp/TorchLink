package httpapi

import (
	"context"
	"errors"
	"iot-platform/internal/adapters/embedding"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"net/http"
	"strings"
	"time"
)

// bundledRerankModel is the model built into the deploy/local-ai image.
const bundledRerankModel = "bge-reranker-v2-m3"

type embeddingInput struct {
	BaseURL          string `json:"baseUrl"`
	Model            string `json:"model"`
	APIKey           string `json:"apiKey"`
	ClearAPIKey      bool   `json:"clearAPIKey"`
	Dimensions       int    `json:"dimensions"`
	BatchSize        int    `json:"batchSize"`
	QueryInstruction string `json:"queryInstruction"`
	TimeoutSeconds   int    `json:"timeoutSeconds"`
}

func (s *Server) embeddingView(cfg ports.EmbeddingConfig) map[string]any {
	view := map[string]any{"baseUrl": cfg.BaseURL, "model": cfg.Model, "apiKeyConfigured": strings.TrimSpace(cfg.APIKey) != "", "dimensions": cfg.Dimensions, "batchSize": cfg.BatchSize, "queryInstruction": cfg.QueryInstruction, "timeoutSeconds": cfg.TimeoutSeconds, "local": embedding.IsLocal(cfg)}
	// The deployment's own vector service, offered as a one-click setting.
	if bundled := embedding.NormalizeConfig(ports.EmbeddingConfig{BaseURL: s.cfg.EmbeddingURL, Model: s.cfg.EmbeddingModel, Dimensions: s.cfg.EmbeddingDimensions}); embedding.IsLocal(bundled) {
		view["bundled"] = map[string]any{"baseUrl": bundled.BaseURL, "model": bundled.Model, "dimensions": bundled.Dimensions}
	}
	// Reranking is deployment configuration (IOT_RERANK_URL), loaded at start.
	rerank := map[string]any{"enabled": s.cfg.RerankURL != ""}
	if s.cfg.RerankURL != "" {
		local := ports.LocalAIEndpoint(s.cfg.RerankURL, s.cfg.LocalAIHosts)
		rerank["baseUrl"], rerank["local"] = s.cfg.RerankURL, local
		if local {
			rerank["model"] = bundledRerankModel
		}
	}
	view["rerank"] = rerank
	return view
}

func (s *Server) embeddingCandidate(w http.ResponseWriter, r *http.Request) (ports.EmbeddingConfig, bool) {
	if s.embeddingRuntime == nil {
		problem(w, 503, "持久化知识库未配置")
		return ports.EmbeddingConfig{}, false
	}
	var in embeddingInput
	if decode(w, r, &in) != nil {
		return ports.EmbeddingConfig{}, false
	}
	cfg := ports.EmbeddingConfig{BaseURL: in.BaseURL, Model: in.Model, Dimensions: in.Dimensions, BatchSize: in.BatchSize, QueryInstruction: in.QueryInstruction, TimeoutSeconds: in.TimeoutSeconds, APIKey: s.embeddingRuntime.CurrentConfig().APIKey}
	if strings.TrimSpace(in.APIKey) != "" {
		cfg.APIKey = in.APIKey
	}
	if in.ClearAPIKey {
		cfg.APIKey = ""
	}
	cfg = embedding.NormalizeConfig(cfg)
	if _, err := embedding.ClientForConfig(cfg); err != nil {
		problem(w, 422, err.Error())
		return cfg, false
	}
	return cfg, true
}

func (s *Server) embeddingConfig(w http.ResponseWriter, r *http.Request) {
	if !s.platformActionAllowed(r) {
		problem(w, http.StatusForbidden, "模型、向量服务与智能体配置对全平台生效，只能由平台管理员或运维租户修改")
		return
	}
	if s.embeddingRuntime == nil {
		problem(w, 503, "持久化知识库未配置")
		return
	}
	write(w, 200, s.embeddingView(s.embeddingRuntime.CurrentConfig()))
}

func (s *Server) updateEmbeddingConfig(w http.ResponseWriter, r *http.Request) {
	if !s.platformActionAllowed(r) {
		problem(w, http.StatusForbidden, "模型、向量服务与智能体配置对全平台生效，只能由平台管理员或运维租户修改")
		return
	}
	s.aiProviderUpdateMu.Lock()
	defer s.aiProviderUpdateMu.Unlock()
	cfg, ok := s.embeddingCandidate(w, r)
	if !ok {
		return
	}
	if err := s.embeddingRuntime.Configure(r.Context(), cfg); err != nil {
		s.failure(w, r, err, "向量配置保存失败")
		return
	}
	s.audit(r, "ai.embedding.update", "embedding-model", cfg.Model, map[string]any{"dimensions": cfg.Dimensions, "apiKeyConfigured": cfg.APIKey != ""})
	write(w, 200, s.embeddingView(cfg))
}

func (s *Server) testEmbeddingConfig(w http.ResponseWriter, r *http.Request) {
	if !s.platformActionAllowed(r) {
		problem(w, http.StatusForbidden, "模型、向量服务与智能体配置对全平台生效，只能由平台管理员或运维租户修改")
		return
	}
	cfg, ok := s.embeddingCandidate(w, r)
	if !ok {
		return
	}
	if cfg.APIKey == "" && !embedding.IsLocal(cfg) {
		write(w, 200, map[string]any{"success": false, "error": "请填写 Embedding API Key", "model": cfg.Model, "dimensions": cfg.Dimensions, "latencyMs": 0})
		return
	}
	client, err := embedding.ClientForConfig(cfg)
	if err != nil {
		problem(w, 422, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(cfg.TimeoutSeconds)*time.Second)
	defer cancel()
	start := time.Now()
	vectors, err := client.Embed(ctx, []string{"消防设备告警处置"}, ports.EmbedQuery)
	result := map[string]any{"success": err == nil, "model": cfg.Model, "dimensions": cfg.Dimensions, "latencyMs": time.Since(start).Milliseconds()}
	if err != nil {
		result["error"] = err.Error()
	} else {
		result["dimensions"] = len(vectors[0])
		result["message"] = "向量服务连接正常"
	}
	write(w, 200, result)
}

func (s *Server) retryKnowledgeDocument(w http.ResponseWriter, r *http.Request) {
	tenant := claims(r).TenantID
	doc, err := s.engine.Repo.GetKnowledgeDoc(r.Context(), tenant, r.PathValue("id"))
	if errors.Is(err, model.ErrNotFound) {
		problem(w, 404, "知识文档不存在")
		return
	}
	if err != nil {
		s.failure(w, r, err, "无法读取知识文档")
		return
	}
	// A document waiting for an automatic retry may be retried at once.
	if _, waiting := doc.Metadata["indexRetryAt"]; doc.Status == "INDEXING" || doc.Status == "UPLOADED" && !waiting {
		problem(w, 409, "文档正在等待或执行索引")
		return
	}
	if _, ok := s.engine.KB.(interface{ CurrentConfig() ports.EmbeddingConfig }); !ok {
		problem(w, 503, "后台知识索引未配置")
		return
	}
	doc.Status = "UPLOADED"
	if doc.Metadata == nil {
		doc.Metadata = map[string]any{}
	}
	doc.Metadata["indexStage"] = "pending"
	doc.Metadata["indexProgress"] = map[string]int{"done": 0, "total": 0}
	delete(doc.Metadata, "indexError")
	delete(doc.Metadata, "indexRetryAt")
	delete(doc.Metadata, "indexAttempts")
	if s.knowledgeJobs != nil {
		updated, err := s.knowledgeJobs.UpdateKnowledgeDocument(r.Context(), doc)
		if err != nil {
			s.failure(w, r, err, "无法重试知识索引")
			return
		}
		if !updated {
			problem(w, 409, "文档已删除，无法重试")
			return
		}
	} else if err := s.engine.Repo.SaveKnowledgeDoc(r.Context(), doc); err != nil {
		s.failure(w, r, err, "无法重试知识索引")
		return
	}
	s.audit(r, "knowledge.retry", "knowledge-document", doc.ID, nil)
	s.wakeKnowledgeJobs()
	write(w, 202, doc)
}

// wakeKnowledgeJobs asks this replica's knowledge runtime to process the
// queue now; other replicas pick the job up after their idle interval.
func (s *Server) wakeKnowledgeJobs() {
	if runtime, ok := s.embeddingRuntime.(interface{ Wake() }); ok {
		runtime.Wake()
	}
}
