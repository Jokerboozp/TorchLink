package httpapi

import (
	"context"
	"iot-platform/internal/adapters/embedding"
	"iot-platform/internal/ports"
	"net/http"
	"strings"
	"time"
)

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

func embeddingView(cfg ports.EmbeddingConfig) map[string]any {
	return map[string]any{"baseUrl": cfg.BaseURL, "model": cfg.Model, "apiKeyConfigured": strings.TrimSpace(cfg.APIKey) != "", "dimensions": cfg.Dimensions, "batchSize": cfg.BatchSize, "queryInstruction": cfg.QueryInstruction, "timeoutSeconds": cfg.TimeoutSeconds}
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
	if s.embeddingRuntime == nil {
		problem(w, 503, "持久化知识库未配置")
		return
	}
	write(w, 200, embeddingView(s.embeddingRuntime.CurrentConfig()))
}

func (s *Server) updateEmbeddingConfig(w http.ResponseWriter, r *http.Request) {
	s.aiProviderUpdateMu.Lock()
	defer s.aiProviderUpdateMu.Unlock()
	cfg, ok := s.embeddingCandidate(w, r)
	if !ok {
		return
	}
	if err := s.embeddingRuntime.Configure(r.Context(), cfg); err != nil {
		problem(w, 500, "向量配置保存失败")
		return
	}
	s.audit(r, "ai.embedding.update", "embedding-model", cfg.Model, map[string]any{"dimensions": cfg.Dimensions, "apiKeyConfigured": cfg.APIKey != ""})
	write(w, 200, embeddingView(cfg))
}

func (s *Server) testEmbeddingConfig(w http.ResponseWriter, r *http.Request) {
	cfg, ok := s.embeddingCandidate(w, r)
	if !ok {
		return
	}
	if cfg.APIKey == "" {
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
		result["message"] = "云向量 API 连接正常"
	}
	write(w, 200, result)
}

func (s *Server) retryKnowledgeDocument(w http.ResponseWriter, r *http.Request) {
	tenant := claims(r).TenantID
	docs, err := s.engine.Repo.ListKnowledgeDocs(r.Context(), tenant)
	if err != nil {
		problem(w, 500, "无法读取知识文档")
		return
	}
	for _, doc := range docs {
		if doc.ID != r.PathValue("id") {
			continue
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
				problem(w, 500, "无法重试知识索引")
				return
			}
			if !updated {
				problem(w, 409, "文档已删除，无法重试")
				return
			}
		} else if err := s.engine.Repo.SaveKnowledgeDoc(r.Context(), doc); err != nil {
			problem(w, 500, "无法重试知识索引")
			return
		}
		s.audit(r, "knowledge.retry", "knowledge-document", doc.ID, nil)
		write(w, 202, doc)
		return
	}
	problem(w, 404, "知识文档不存在")
}
