package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// Weaviate stores knowledge chunks with vectors computed by the private
// embedding service (Go side). The class is created lazily by this adapter and
// records the embedding model so a model change is detected instead of mixing
// vector spaces.
type Weaviate struct {
	url         string
	http        *http.Client
	embedder    ports.Embedder
	initMu      sync.Mutex
	initialized bool
}

const (
	knowledgeClass = "IotKnowledgeV2"
	// legacyKnowledgeClass was vectorized inside Weaviate by text2vec-ollama.
	legacyKnowledgeClass = "IotKnowledge"
	embeddingDescriptor  = "embedding="
)

// ErrKnowledgeIndexStale reports an index built with another embedding model
// (or the legacy Ollama-vectorized class). The rebuild job replaces it.
var ErrKnowledgeIndexStale = errors.New("嵌入模型已变更，需要重建知识库索引")

func NewWeaviate(url string, embedder ports.Embedder) *Weaviate {
	return &Weaviate{url: strings.TrimRight(url, "/"), http: &http.Client{Timeout: 30 * time.Second}, embedder: embedder}
}

func (w *Weaviate) EmbeddingModel() string {
	if w.embedder == nil {
		return ""
	}
	return w.embedder.Model()
}

// DeleteKnowledgeDocument removes every indexed chunk belonging to the document.
func (w *Weaviate) DeleteKnowledgeDocument(ctx context.Context, tenant, documentID, workflowID string) error {
	chunks, err := w.ListKnowledgeChunks(ctx, tenant, documentID)
	if err != nil {
		return err
	}
	for _, chunk := range chunks {
		objectID := knowledgeObjectID(tenant, workflowID, chunk.ChunkID)
		req, err := http.NewRequestWithContext(ctx, http.MethodDelete, w.url+"/v1/objects/"+knowledgeClass+"/"+objectID, nil)
		if err != nil {
			return err
		}
		resp, err := w.http.Do(req)
		if err != nil {
			return err
		}
		resp.Body.Close()
		if resp.StatusCode/100 != 2 && resp.StatusCode != http.StatusNotFound {
			return fmt.Errorf("weaviate delete chunk: %s", resp.Status)
		}
	}
	return nil
}

func knowledgeObjectID(tenant, workflowID, chunkID string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(strings.Join([]string{tenant, workflowID, chunkID}, "\x00"))).String()
}

func (w *Weaviate) Index(ctx context.Context, tenant, product, id string, data []byte) error {
	return w.IndexKnowledge(ctx, ports.KnowledgeIndexInput{TenantID: tenant, ProductID: product, DocumentID: id, ChunkID: id, Content: data})
}

func (w *Weaviate) IndexKnowledge(ctx context.Context, in ports.KnowledgeIndexInput) error {
	return w.IndexKnowledgeBatch(ctx, []ports.KnowledgeIndexInput{in})
}

// IndexKnowledgeBatch embeds all chunks in batches and writes them with the
// batch API. Object IDs are deterministic, so re-indexing replaces chunks.
func (w *Weaviate) IndexKnowledgeBatch(ctx context.Context, inputs []ports.KnowledgeIndexInput) error {
	if len(inputs) == 0 {
		return nil
	}
	if err := w.ensureInitialized(ctx); err != nil {
		return err
	}
	texts := make([]string, len(inputs))
	for i, in := range inputs {
		texts[i] = string(in.Content)
	}
	vectors, err := w.embedder.Embed(ctx, texts, ports.EmbedDocument)
	if err != nil {
		return fmt.Errorf("knowledge embedding: %w", err)
	}
	const writeBatch = 100
	for start := 0; start < len(inputs); start += writeBatch {
		end := min(start+writeBatch, len(inputs))
		objects := make([]map[string]any, 0, end-start)
		for i := start; i < end; i++ {
			objects = append(objects, knowledgeObject(inputs[i], vectors[i]))
		}
		if err := w.writeBatch(ctx, objects); err != nil {
			return err
		}
	}
	return nil
}

func knowledgeObject(in ports.KnowledgeIndexInput, vector []float32) map[string]any {
	characterCount := in.CharacterCount
	if characterCount <= 0 {
		characterCount = len([]rune(string(in.Content)))
	}
	return map[string]any{"class": knowledgeClass, "id": knowledgeObjectID(in.TenantID, in.WorkflowID, in.ChunkID), "vector": vector, "properties": map[string]any{"tenantId": in.TenantID, "workflowId": in.WorkflowID, "productId": in.ProductID, "documentId": in.DocumentID, "chunkId": in.ChunkID, "chunkIndex": in.ChunkIndex, "startChar": in.StartChar, "endChar": in.EndChar, "characterCount": characterCount, "overlapChars": in.OverlapChars, "category": in.Category, "tags": in.Tags, "content": string(in.Content)}}
}

func (w *Weaviate) writeBatch(ctx context.Context, objects []map[string]any) error {
	body, err := json.Marshal(map[string]any{"objects": objects})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url+"/v1/batch/objects", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := w.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("weaviate index %s: %s", resp.Status, string(b))
	}
	// The batch endpoint reports per-object failures with HTTP 200.
	var results []struct {
		Result struct {
			Errors *struct {
				Error []struct {
					Message string `json:"message"`
				} `json:"error"`
			} `json:"errors"`
		} `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&results); err != nil {
		return fmt.Errorf("decode weaviate batch response: %w", err)
	}
	for _, item := range results {
		if item.Result.Errors != nil && len(item.Result.Errors.Error) > 0 {
			return fmt.Errorf("weaviate index: %s", item.Result.Errors.Error[0].Message)
		}
	}
	return nil
}

func (w *Weaviate) ensureInitialized(ctx context.Context) error {
	if w.embedder == nil {
		return errors.New("knowledge embedding service is not configured")
	}
	w.initMu.Lock()
	defer w.initMu.Unlock()
	if w.initialized {
		return nil
	}
	if err := w.ensureClass(ctx); err != nil {
		return err
	}
	w.initialized = true
	return nil
}

type weaviateClass struct {
	Description string `json:"description"`
	Properties  []struct {
		Name string `json:"name"`
	} `json:"properties"`
}

// getClass returns the class definition, or nil when it does not exist.
func (w *Weaviate) getClass(ctx context.Context, class string) (*weaviateClass, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, w.url+"/v1/schema/"+class, nil)
	if err != nil {
		return nil, err
	}
	resp, err := w.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("weaviate schema check %s", resp.Status)
	}
	var existing weaviateClass
	if err = json.NewDecoder(resp.Body).Decode(&existing); err != nil {
		return nil, err
	}
	return &existing, nil
}

func (w *Weaviate) ensureClass(ctx context.Context) error {
	existing, err := w.getClass(ctx, knowledgeClass)
	if err != nil {
		return err
	}
	if existing == nil {
		return w.createClass(ctx)
	}
	if existing.Description != embeddingDescriptor+w.embedder.Model() {
		return ErrKnowledgeIndexStale
	}
	present := map[string]bool{}
	for _, property := range existing.Properties {
		present[property.Name] = true
	}
	for _, property := range knowledgeProperties() {
		name, _ := property["name"].(string)
		if present[name] {
			continue
		}
		body, _ := json.Marshal(property)
		create, createErr := http.NewRequestWithContext(ctx, http.MethodPost, w.url+"/v1/schema/"+knowledgeClass+"/properties", bytes.NewReader(body))
		if createErr != nil {
			return createErr
		}
		create.Header.Set("Content-Type", "application/json")
		created, createErr := w.http.Do(create)
		if createErr != nil {
			return createErr
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(created.Body, 4096))
		created.Body.Close()
		if created.StatusCode/100 != 2 {
			return fmt.Errorf("weaviate add property %s: %s", name, created.Status)
		}
	}
	return nil
}

func (w *Weaviate) createClass(ctx context.Context) error {
	body, _ := json.Marshal(map[string]any{
		"class":             knowledgeClass,
		"description":       embeddingDescriptor + w.embedder.Model(),
		"vectorizer":        "none",
		"vectorIndexConfig": map[string]any{"distance": "cosine"},
		"properties":        knowledgeProperties(),
	})
	create, _ := http.NewRequestWithContext(ctx, http.MethodPost, w.url+"/v1/schema", bytes.NewReader(body))
	create.Header.Set("Content-Type", "application/json")
	created, err := w.http.Do(create)
	if err != nil {
		return err
	}
	defer created.Body.Close()
	if created.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(created.Body, 4096))
		return fmt.Errorf("weaviate create class %s: %s", created.Status, string(b))
	}
	return nil
}

func (w *Weaviate) deleteClass(ctx context.Context, class string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, w.url+"/v1/schema/"+class, nil)
	if err != nil {
		return err
	}
	resp, err := w.http.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode/100 != 2 && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("weaviate delete class %s: %s", class, resp.Status)
	}
	return nil
}

// NeedsRebuild reports whether stored knowledge must be re-embedded: the
// current class was built with another model, or only the legacy class exists.
func (w *Weaviate) NeedsRebuild(ctx context.Context) (bool, error) {
	if w.embedder == nil {
		return false, nil
	}
	current, err := w.getClass(ctx, knowledgeClass)
	if err != nil {
		return false, err
	}
	if current != nil && current.Description != embeddingDescriptor+w.embedder.Model() {
		return true, nil
	}
	legacy, err := w.getClass(ctx, legacyKnowledgeClass)
	if err != nil {
		return false, err
	}
	return legacy != nil, nil
}

// ResetIndex drops the current class and recreates it for the configured
// embedding model. Callers re-index every document afterwards.
func (w *Weaviate) ResetIndex(ctx context.Context) error {
	if w.embedder == nil {
		return errors.New("knowledge embedding service is not configured")
	}
	w.initMu.Lock()
	defer w.initMu.Unlock()
	w.initialized = false
	if err := w.deleteClass(ctx, knowledgeClass); err != nil {
		return err
	}
	if err := w.createClass(ctx); err != nil {
		return err
	}
	w.initialized = true
	return nil
}

// DropLegacyIndex removes the Ollama-vectorized class after a rebuild.
func (w *Weaviate) DropLegacyIndex(ctx context.Context) error {
	return w.deleteClass(ctx, legacyKnowledgeClass)
}

func knowledgeProperties() []map[string]any {
	return []map[string]any{
		{"name": "tenantId", "dataType": []string{"text"}, "tokenization": "field"},
		{"name": "workflowId", "dataType": []string{"text"}, "tokenization": "field"},
		{"name": "productId", "dataType": []string{"text"}, "tokenization": "field"},
		{"name": "documentId", "dataType": []string{"text"}, "tokenization": "field"},
		{"name": "chunkId", "dataType": []string{"text"}, "tokenization": "field"},
		{"name": "chunkIndex", "dataType": []string{"int"}},
		{"name": "startChar", "dataType": []string{"int"}},
		{"name": "endChar", "dataType": []string{"int"}},
		{"name": "characterCount", "dataType": []string{"int"}},
		{"name": "overlapChars", "dataType": []string{"int"}},
		{"name": "category", "dataType": []string{"text"}, "tokenization": "field"},
		{"name": "tags", "dataType": []string{"text[]"}, "tokenization": "field"},
		{"name": "content", "dataType": []string{"text"}},
	}
}
func (w *Weaviate) Search(ctx context.Context, tenant, q string, limit int) ([]string, error) {
	hits, err := w.SearchKnowledge(ctx, ports.KnowledgeSearchRequest{TenantID: tenant, Question: q, Limit: limit})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(hits))
	for _, hit := range hits {
		out = append(out, hit.Content)
	}
	return out, nil
}
func (w *Weaviate) SearchKnowledge(ctx context.Context, in ports.KnowledgeSearchRequest) ([]ports.KnowledgeHit, error) {
	if err := w.ensureInitialized(ctx); err != nil {
		return nil, err
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 5
	}
	operands := []string{fmt.Sprintf(`{path:["tenantId"],operator:Equal,valueText:%q}`, in.TenantID)}
	if in.WorkflowID != "" {
		operands = append(operands, fmt.Sprintf(`{path:["workflowId"],operator:Equal,valueText:%q}`, in.WorkflowID))
	}
	if len(in.ProductIDs) > 0 {
		operands = append(operands, orTextFilter("productId", in.ProductIDs))
	}
	if len(in.Categories) > 0 {
		operands = append(operands, orTextFilter("category", in.Categories))
	}
	where := operands[0]
	if len(operands) > 1 {
		where = `{operator:And,operands:[` + strings.Join(operands, ",") + `]}`
	}
	// Fetch extra candidates because tag matching is applied after vector search.
	candidateLimit := limit
	if len(in.Tags) > 0 {
		candidateLimit = min(100, max(limit*5, 20))
	}
	vectors, err := w.embedder.Embed(ctx, []string{in.Question}, ports.EmbedQuery)
	if err != nil {
		return nil, fmt.Errorf("knowledge query embedding: %w", err)
	}
	query := fmt.Sprintf(`{Get{%s(where:%s,nearVector:{vector:%s},limit:%d){workflowId documentId productId chunkId chunkIndex startChar endChar characterCount overlapChars category tags content _additional{certainty}}}}`, knowledgeClass, where, graphQLVector(vectors[0]), candidateLimit)
	body, _ := json.Marshal(map[string]string{"query": query})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, w.url+"/v1/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := w.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("weaviate search %s", resp.Status)
	}
	var raw struct {
		Data struct {
			Get struct {
				Items []struct {
					WorkflowID     string   `json:"workflowId"`
					DocumentID     string   `json:"documentId"`
					ProductID      string   `json:"productId"`
					ChunkID        string   `json:"chunkId"`
					ChunkIndex     int      `json:"chunkIndex"`
					StartChar      int      `json:"startChar"`
					EndChar        int      `json:"endChar"`
					CharacterCount int      `json:"characterCount"`
					OverlapChars   int      `json:"overlapChars"`
					Category       string   `json:"category"`
					Tags           []string `json:"tags"`
					Content        string   `json:"content"`
					Additional     struct {
						Certainty float64 `json:"certainty"`
					} `json:"_additional"`
				} `json:"IotKnowledgeV2"`
			} `json:"Get"`
		} `json:"data"`
		Errors []any `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	if len(raw.Errors) > 0 {
		return nil, fmt.Errorf("weaviate graphql: %v", raw.Errors)
	}
	out := []ports.KnowledgeHit{}
	for _, v := range raw.Data.Get.Items {
		if !containsAll(v.Tags, in.Tags) || v.Additional.Certainty < in.MinScore {
			continue
		}
		out = append(out, ports.KnowledgeHit{DocumentID: v.DocumentID, ChunkID: v.ChunkID, WorkflowID: v.WorkflowID, ProductID: v.ProductID, Category: v.Category, Tags: v.Tags, Content: v.Content, Score: v.Additional.Certainty})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (w *Weaviate) ListKnowledgeChunks(ctx context.Context, tenant, documentID string) ([]model.KnowledgeChunk, error) {
	if err := w.ensureInitialized(ctx); err != nil {
		return nil, err
	}
	operands := []string{fmt.Sprintf(`{path:["tenantId"],operator:Equal,valueText:%q}`, tenant)}
	if strings.TrimSpace(documentID) != "" {
		operands = append(operands, fmt.Sprintf(`{path:["documentId"],operator:Equal,valueText:%q}`, documentID))
	}
	where := operands[0]
	if len(operands) > 1 {
		where = `{operator:And,operands:[` + strings.Join(operands, ",") + `]}`
	}
	// Weaviate rejects values above its configured query maximum. Page through
	// the result so inspection also works for the largest accepted documents.
	const pageSize = 10000
	out := make([]model.KnowledgeChunk, 0)
	for offset := 0; ; {
		query := fmt.Sprintf(`{Get{%s(where:%s,limit:%d,offset:%d){documentId chunkId chunkIndex startChar endChar characterCount overlapChars content}}}`, knowledgeClass, where, pageSize, offset)
		body, _ := json.Marshal(map[string]string{"query": query})
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, w.url+"/v1/graphql", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := w.http.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode/100 != 2 {
			status := resp.Status
			resp.Body.Close()
			return nil, fmt.Errorf("weaviate list chunks %s", status)
		}
		var raw struct {
			Data struct {
				Get struct {
					Items []struct {
						DocumentID     string `json:"documentId"`
						ChunkID        string `json:"chunkId"`
						ChunkIndex     int    `json:"chunkIndex"`
						StartChar      int    `json:"startChar"`
						EndChar        int    `json:"endChar"`
						CharacterCount int    `json:"characterCount"`
						OverlapChars   int    `json:"overlapChars"`
						Content        string `json:"content"`
					} `json:"IotKnowledgeV2"`
				} `json:"Get"`
			} `json:"data"`
			Errors []any `json:"errors"`
		}
		err = json.NewDecoder(resp.Body).Decode(&raw)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if len(raw.Errors) > 0 {
			return nil, fmt.Errorf("weaviate list chunks: %v", raw.Errors)
		}
		for _, item := range raw.Data.Get.Items {
			characterCount := item.CharacterCount
			if characterCount <= 0 {
				characterCount = len([]rune(item.Content))
			}
			out = append(out, model.KnowledgeChunk{DocumentID: item.DocumentID, ChunkID: item.ChunkID, Index: item.ChunkIndex, StartChar: item.StartChar, EndChar: item.EndChar, CharacterCount: characterCount, OverlapChars: item.OverlapChars, Content: item.Content, Vectorized: true})
		}
		if len(raw.Data.Get.Items) < pageSize {
			break
		}
		offset += len(raw.Data.Get.Items)
	}
	return normalizeKnowledgeChunks(out, documentID), nil
}

func normalizeKnowledgeChunks(chunks []model.KnowledgeChunk, documentID string) []model.KnowledgeChunk {
	for index := range chunks {
		if chunks[index].DocumentID == "" {
			chunks[index].DocumentID = documentID
		}
		if chunks[index].Index <= 0 {
			chunks[index].Index = index + 1
		}
		if chunks[index].ChunkID == "" {
			chunks[index].ChunkID = fmt.Sprintf("%s-chunk-%04d", chunks[index].DocumentID, chunks[index].Index)
		}
		if chunks[index].EndChar <= chunks[index].StartChar {
			chunks[index].StartChar = 0
			chunks[index].EndChar = chunks[index].CharacterCount
		}
		if chunks[index].CharacterCount <= 0 {
			chunks[index].CharacterCount = len([]rune(chunks[index].Content))
		}
	}
	sort.SliceStable(chunks, func(i, j int) bool {
		if chunks[i].Index == chunks[j].Index {
			return chunks[i].ChunkID < chunks[j].ChunkID
		}
		return chunks[i].Index < chunks[j].Index
	})
	return chunks
}
func graphQLVector(vector []float32) string {
	parts := make([]string, len(vector))
	for i, value := range vector {
		parts[i] = strconv.FormatFloat(float64(value), 'g', -1, 32)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func orTextFilter(path string, values []string) string {
	operands := make([]string, 0, len(values))
	for _, value := range values {
		operands = append(operands, fmt.Sprintf(`{path:[%q],operator:Equal,valueText:%q}`, path, value))
	}
	if len(operands) == 1 {
		return operands[0]
	}
	return `{operator:Or,operands:[` + strings.Join(operands, ",") + `]}`
}
func (w *Weaviate) Health(ctx context.Context) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, w.url+"/v1/.well-known/ready", nil)
	resp, err := w.http.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("weaviate %s", resp.Status)
	}
	return nil
}
