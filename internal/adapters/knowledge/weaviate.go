package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// Weaviate uses a text2vec-enabled IotKnowledge class. Compose initializes this class.
type Weaviate struct {
	url         string
	http        *http.Client
	initMu      sync.Mutex
	initialized bool
}

func NewWeaviate(url string) *Weaviate {
	return &Weaviate{url: strings.TrimRight(url, "/"), http: &http.Client{Timeout: 30 * time.Second}}
}

// DeleteKnowledgeDocument removes every indexed chunk belonging to the document.
func (w *Weaviate) DeleteKnowledgeDocument(ctx context.Context, tenant, documentID, workflowID string) error {
	chunks, err := w.ListKnowledgeChunks(ctx, tenant, documentID)
	if err != nil {
		return err
	}
	for _, chunk := range chunks {
		objectID := uuid.NewSHA1(uuid.NameSpaceURL, []byte(strings.Join([]string{tenant, workflowID, chunk.ChunkID}, "\x00")))
		req, err := http.NewRequestWithContext(ctx, http.MethodDelete, w.url+"/v1/objects/IotKnowledge/"+objectID.String(), nil)
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
func (w *Weaviate) Index(ctx context.Context, tenant, product, id string, data []byte) error {
	return w.IndexKnowledge(ctx, ports.KnowledgeIndexInput{TenantID: tenant, ProductID: product, DocumentID: id, ChunkID: id, Content: data})
}
func (w *Weaviate) IndexKnowledge(ctx context.Context, in ports.KnowledgeIndexInput) error {
	if err := w.ensureInitialized(ctx); err != nil {
		return err
	}
	characterCount := in.CharacterCount
	if characterCount <= 0 {
		characterCount = len([]rune(string(in.Content)))
	}
	objectID := uuid.NewSHA1(uuid.NameSpaceURL, []byte(strings.Join([]string{in.TenantID, in.WorkflowID, in.ChunkID}, "\x00")))
	body, _ := json.Marshal(map[string]any{"class": "IotKnowledge", "id": objectID.String(), "properties": map[string]any{"tenantId": in.TenantID, "workflowId": in.WorkflowID, "productId": in.ProductID, "documentId": in.DocumentID, "chunkId": in.ChunkID, "chunkIndex": in.ChunkIndex, "startChar": in.StartChar, "endChar": in.EndChar, "characterCount": characterCount, "overlapChars": in.OverlapChars, "category": in.Category, "tags": in.Tags, "content": string(in.Content)}})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, w.url+"/v1/objects", bytes.NewReader(body))
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
	return nil
}

func (w *Weaviate) ensureInitialized(ctx context.Context) error {
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

func (w *Weaviate) ensureClass(ctx context.Context) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, w.url+"/v1/schema/IotKnowledge", nil)
	resp, err := w.http.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode/100 == 2 {
		defer resp.Body.Close()
		var existing struct {
			Properties []struct {
				Name string `json:"name"`
			} `json:"properties"`
		}
		if err = json.NewDecoder(resp.Body).Decode(&existing); err != nil {
			return err
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
			create, createErr := http.NewRequestWithContext(ctx, http.MethodPost, w.url+"/v1/schema/IotKnowledge/properties", bytes.NewReader(body))
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
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("weaviate schema check %s", resp.Status)
	}
	body, _ := json.Marshal(map[string]any{
		"class":      "IotKnowledge",
		"vectorizer": "text2vec-ollama",
		"moduleConfig": map[string]any{
			"text2vec-ollama": map[string]any{
				"apiEndpoint": "http://ollama:11434",
				"model":       "nomic-embed-text",
			},
		},
		"properties": knowledgeProperties(),
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
	query := fmt.Sprintf(`{Get{IotKnowledge(where:%s,nearText:{concepts:[%q]},limit:%d){workflowId documentId productId chunkId chunkIndex startChar endChar characterCount overlapChars category tags content _additional{certainty}}}}`, where, in.Question, candidateLimit)
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
				} `json:"IotKnowledge"`
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
		query := fmt.Sprintf(`{Get{IotKnowledge(where:%s,limit:%d,offset:%d){documentId chunkId chunkIndex startChar endChar characterCount overlapChars content}}}`, where, pageSize, offset)
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
					} `json:"IotKnowledge"`
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
