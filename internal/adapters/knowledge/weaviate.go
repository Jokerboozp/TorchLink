package knowledge /* 声明 knowledge 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bytes"         /* 执行当前语句并推进处理流程。 */
	"context"       /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */
	"io"            /* 执行当前语句并推进处理流程。 */
	"net/http"      /* 执行当前语句并推进处理流程。 */
	"sort"          /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */
	"sync"          /* 执行当前语句并推进处理流程。 */
	"time"          /* 执行当前语句并推进处理流程。 */

	"github.com/google/uuid" /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

// Weaviate uses a text2vec-enabled IotKnowledge class. Compose initializes this class.
type Weaviate struct { /* 定义 Weaviate 类型。 */
	url         string       /* 执行当前语句并推进处理流程。 */
	http        *http.Client /* 执行当前语句并推进处理流程。 */
	initMu      sync.Mutex   /* 执行当前语句并推进处理流程。 */
	initialized bool         /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func NewWeaviate(url string) *Weaviate { /* 定义 NewWeaviate 函数。 */
	return &Weaviate{url: strings.TrimRight(url, "/"), http: &http.Client{Timeout: 30 * time.Second}} /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
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
func (w *Weaviate) Index(ctx context.Context, tenant, product, id string, data []byte) error { /* 定义 Index 函数。 */
	return w.IndexKnowledge(ctx, ports.KnowledgeIndexInput{TenantID: tenant, ProductID: product, DocumentID: id, ChunkID: id, Content: data}) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (w *Weaviate) IndexKnowledge(ctx context.Context, in ports.KnowledgeIndexInput) error { /* 定义 IndexKnowledge 函数。 */
	if err := w.ensureInitialized(ctx); err != nil { /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	characterCount := in.CharacterCount /* 更新 characterCount 的值。 */
	if characterCount <= 0 {            /* 判断条件并选择处理分支。 */
		characterCount = len([]rune(string(in.Content))) /* 更新 characterCount 的值。 */
	} /* 结束当前表达式或代码块。 */
	objectID := uuid.NewSHA1(uuid.NameSpaceURL, []byte(strings.Join([]string{in.TenantID, in.WorkflowID, in.ChunkID}, "\x00")))                                                                                                                                                                                                                                                                                                                                                             /* 更新 objectID 的值。 */
	body, _ := json.Marshal(map[string]any{"class": "IotKnowledge", "id": objectID.String(), "properties": map[string]any{"tenantId": in.TenantID, "workflowId": in.WorkflowID, "productId": in.ProductID, "documentId": in.DocumentID, "chunkId": in.ChunkID, "chunkIndex": in.ChunkIndex, "startChar": in.StartChar, "endChar": in.EndChar, "characterCount": characterCount, "overlapChars": in.OverlapChars, "category": in.Category, "tags": in.Tags, "content": string(in.Content)}}) /* 更新 _ 的值。 */
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, w.url+"/v1/objects", bytes.NewReader(body))                                                                                                                                                                                                                                                                                                                                                                                  /* 更新 _ 的值。 */
	req.Header.Set("Content-Type", "application/json")                                                                                                                                                                                                                                                                                                                                                                                                                                      /* 执行当前语句并推进处理流程。 */
	resp, err := w.http.Do(req)                                                                                                                                                                                                                                                                                                                                                                                                                                                             /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                                                                                                                                                                                                                                                                                                         /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer resp.Body.Close()       /* 安排函数结束时执行清理。 */
	if resp.StatusCode/100 != 2 { /* 判断条件并选择处理分支。 */
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))                /* 更新 _ 的值。 */
		return fmt.Errorf("weaviate index %s: %s", resp.Status, string(b)) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (w *Weaviate) ensureInitialized(ctx context.Context) error { /* 定义 ensureInitialized 函数。 */
	w.initMu.Lock()         /* 执行当前语句并推进处理流程。 */
	defer w.initMu.Unlock() /* 安排函数结束时执行清理。 */
	if w.initialized {      /* 判断条件并选择处理分支。 */
		return nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err := w.ensureClass(ctx); err != nil { /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	w.initialized = true /* 更新 w.initialized 的值。 */
	return nil           /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (w *Weaviate) ensureClass(ctx context.Context) error { /* 定义 ensureClass 函数。 */
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, w.url+"/v1/schema/IotKnowledge", nil) /* 更新 _ 的值。 */
	resp, err := w.http.Do(req)                                                                     /* 更新 err 的值。 */
	if err != nil {                                                                                 /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if resp.StatusCode/100 == 2 { /* 判断条件并选择处理分支。 */
		defer resp.Body.Close() /* 安排函数结束时执行清理。 */
		var existing struct {   /* 声明 existing。 */
			Properties []struct { /* 执行当前语句并推进处理流程。 */
				Name string `json:"name"` /* 执行当前语句并推进处理流程。 */
			} `json:"properties"` /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		if err = json.NewDecoder(resp.Body).Decode(&existing); err != nil { /* 判断条件并选择处理分支。 */
			return err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		present := map[string]bool{}                   /* 更新 present 的值。 */
		for _, property := range existing.Properties { /* 循环处理当前数据。 */
			present[property.Name] = true /* 更新 present[property.Name] 的值。 */
		} /* 结束当前表达式或代码块。 */
		for _, property := range knowledgeProperties() { /* 循环处理当前数据。 */
			name, _ := property["name"].(string) /* 更新 _ 的值。 */
			if present[name] {                   /* 判断条件并选择处理分支。 */
				continue /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			body, _ := json.Marshal(property)                                                                                                        /* 更新 _ 的值。 */
			create, createErr := http.NewRequestWithContext(ctx, http.MethodPost, w.url+"/v1/schema/IotKnowledge/properties", bytes.NewReader(body)) /* 更新 createErr 的值。 */
			if createErr != nil {                                                                                                                    /* 判断条件并选择处理分支。 */
				return createErr /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			create.Header.Set("Content-Type", "application/json") /* 执行当前语句并推进处理流程。 */
			created, createErr := w.http.Do(create)               /* 更新 createErr 的值。 */
			if createErr != nil {                                 /* 判断条件并选择处理分支。 */
				return createErr /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			_, _ = io.Copy(io.Discard, io.LimitReader(created.Body, 4096)) /* 更新 _ 的值。 */
			created.Body.Close()                                           /* 执行当前语句并推进处理流程。 */
			if created.StatusCode/100 != 2 {                               /* 判断条件并选择处理分支。 */
				return fmt.Errorf("weaviate add property %s: %s", name, created.Status) /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		return nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	resp.Body.Close()                           /* 执行当前语句并推进处理流程。 */
	if resp.StatusCode != http.StatusNotFound { /* 判断条件并选择处理分支。 */
		return fmt.Errorf("weaviate schema check %s", resp.Status) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	body, _ := json.Marshal(map[string]any{ /* 更新 _ 的值。 */
		"class":      "IotKnowledge",    /* 执行当前语句并推进处理流程。 */
		"vectorizer": "text2vec-ollama", /* 执行当前语句并推进处理流程。 */
		"moduleConfig": map[string]any{ /* 执行当前语句并推进处理流程。 */
			"text2vec-ollama": map[string]any{ /* 执行当前语句并推进处理流程。 */
				"apiEndpoint": "http://ollama:11434", /* 执行当前语句并推进处理流程。 */
				"model":       "nomic-embed-text",    /* 执行当前语句并推进处理流程。 */
			}, /* 结束当前表达式或代码块。 */
		}, /* 结束当前表达式或代码块。 */
		"properties": knowledgeProperties(), /* 执行当前语句并推进处理流程。 */
	}) /* 结束当前表达式或代码块。 */
	create, _ := http.NewRequestWithContext(ctx, http.MethodPost, w.url+"/v1/schema", bytes.NewReader(body)) /* 更新 _ 的值。 */
	create.Header.Set("Content-Type", "application/json")                                                    /* 执行当前语句并推进处理流程。 */
	created, err := w.http.Do(create)                                                                        /* 更新 err 的值。 */
	if err != nil {                                                                                          /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer created.Body.Close()       /* 安排函数结束时执行清理。 */
	if created.StatusCode/100 != 2 { /* 判断条件并选择处理分支。 */
		b, _ := io.ReadAll(io.LimitReader(created.Body, 4096))                       /* 更新 _ 的值。 */
		return fmt.Errorf("weaviate create class %s: %s", created.Status, string(b)) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func knowledgeProperties() []map[string]any { /* 定义 knowledgeProperties 函数。 */
	return []map[string]any{ /* 返回当前处理结果。 */
		{"name": "tenantId", "dataType": []string{"text"}, "tokenization": "field"},   /* 执行当前语句并推进处理流程。 */
		{"name": "workflowId", "dataType": []string{"text"}, "tokenization": "field"}, /* 执行当前语句并推进处理流程。 */
		{"name": "productId", "dataType": []string{"text"}, "tokenization": "field"},  /* 执行当前语句并推进处理流程。 */
		{"name": "documentId", "dataType": []string{"text"}, "tokenization": "field"}, /* 执行当前语句并推进处理流程。 */
		{"name": "chunkId", "dataType": []string{"text"}, "tokenization": "field"},    /* 执行当前语句并推进处理流程。 */
		{"name": "chunkIndex", "dataType": []string{"int"}},                           /* 执行当前语句并推进处理流程。 */
		{"name": "startChar", "dataType": []string{"int"}},                            /* 执行当前语句并推进处理流程。 */
		{"name": "endChar", "dataType": []string{"int"}},                              /* 执行当前语句并推进处理流程。 */
		{"name": "characterCount", "dataType": []string{"int"}},                       /* 执行当前语句并推进处理流程。 */
		{"name": "overlapChars", "dataType": []string{"int"}},                         /* 执行当前语句并推进处理流程。 */
		{"name": "category", "dataType": []string{"text"}, "tokenization": "field"},   /* 执行当前语句并推进处理流程。 */
		{"name": "tags", "dataType": []string{"text[]"}, "tokenization": "field"},     /* 执行当前语句并推进处理流程。 */
		{"name": "content", "dataType": []string{"text"}},                             /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
func (w *Weaviate) Search(ctx context.Context, tenant, q string, limit int) ([]string, error) { /* 定义 Search 函数。 */
	hits, err := w.SearchKnowledge(ctx, ports.KnowledgeSearchRequest{TenantID: tenant, Question: q, Limit: limit}) /* 更新 err 的值。 */
	if err != nil {                                                                                                /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	out := make([]string, 0, len(hits)) /* 更新 out 的值。 */
	for _, hit := range hits {          /* 循环处理当前数据。 */
		out = append(out, hit.Content) /* 更新 out 的值。 */
	} /* 结束当前表达式或代码块。 */
	return out, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (w *Weaviate) SearchKnowledge(ctx context.Context, in ports.KnowledgeSearchRequest) ([]ports.KnowledgeHit, error) { /* 定义 SearchKnowledge 函数。 */
	if err := w.ensureInitialized(ctx); err != nil { /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	limit := in.Limit /* 更新 limit 的值。 */
	if limit <= 0 {   /* 判断条件并选择处理分支。 */
		limit = 5 /* 更新 limit 的值。 */
	} /* 结束当前表达式或代码块。 */
	operands := []string{fmt.Sprintf(`{path:["tenantId"],operator:Equal,valueText:%q}`, in.TenantID)} /* 更新 operands 的值。 */
	if in.WorkflowID != "" {                                                                          /* 判断条件并选择处理分支。 */
		operands = append(operands, fmt.Sprintf(`{path:["workflowId"],operator:Equal,valueText:%q}`, in.WorkflowID)) /* 更新 operands 的值。 */
	} /* 结束当前表达式或代码块。 */
	if len(in.ProductIDs) > 0 { /* 判断条件并选择处理分支。 */
		operands = append(operands, orTextFilter("productId", in.ProductIDs)) /* 更新 operands 的值。 */
	} /* 结束当前表达式或代码块。 */
	if len(in.Categories) > 0 { /* 判断条件并选择处理分支。 */
		operands = append(operands, orTextFilter("category", in.Categories)) /* 更新 operands 的值。 */
	} /* 结束当前表达式或代码块。 */
	where := operands[0]   /* 更新 where 的值。 */
	if len(operands) > 1 { /* 判断条件并选择处理分支。 */
		where = `{operator:And,operands:[` + strings.Join(operands, ",") + `]}` /* 更新 where 的值。 */
	} /* 结束当前表达式或代码块。 */
	// Fetch extra candidates because tag matching is applied after vector search.
	candidateLimit := limit /* 更新 candidateLimit 的值。 */
	if len(in.Tags) > 0 {   /* 判断条件并选择处理分支。 */
		candidateLimit = min(100, max(limit*5, 20)) /* 更新 candidateLimit 的值。 */
	} /* 结束当前表达式或代码块。 */
	query := fmt.Sprintf(`{Get{IotKnowledge(where:%s,nearText:{concepts:[%q]},limit:%d){workflowId documentId productId chunkId chunkIndex startChar endChar characterCount overlapChars category tags content _additional{certainty}}}}`, where, in.Question, candidateLimit) /* 更新 query 的值。 */
	body, _ := json.Marshal(map[string]string{"query": query})                                                                                                                                                                                                                 /* 更新 _ 的值。 */
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, w.url+"/v1/graphql", bytes.NewReader(body))                                                                                                                                                                     /* 更新 _ 的值。 */
	req.Header.Set("Content-Type", "application/json")                                                                                                                                                                                                                         /* 执行当前语句并推进处理流程。 */
	resp, err := w.http.Do(req)                                                                                                                                                                                                                                                /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                                                                                            /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer resp.Body.Close()       /* 安排函数结束时执行清理。 */
	if resp.StatusCode/100 != 2 { /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("weaviate search %s", resp.Status) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var raw struct { /* 声明 raw。 */
		Data struct { /* 执行当前语句并推进处理流程。 */
			Get struct { /* 执行当前语句并推进处理流程。 */
				Items []struct { /* 执行当前语句并推进处理流程。 */
					WorkflowID     string   `json:"workflowId"`     /* 执行当前语句并推进处理流程。 */
					DocumentID     string   `json:"documentId"`     /* 执行当前语句并推进处理流程。 */
					ProductID      string   `json:"productId"`      /* 执行当前语句并推进处理流程。 */
					ChunkID        string   `json:"chunkId"`        /* 执行当前语句并推进处理流程。 */
					ChunkIndex     int      `json:"chunkIndex"`     /* 执行当前语句并推进处理流程。 */
					StartChar      int      `json:"startChar"`      /* 执行当前语句并推进处理流程。 */
					EndChar        int      `json:"endChar"`        /* 执行当前语句并推进处理流程。 */
					CharacterCount int      `json:"characterCount"` /* 执行当前语句并推进处理流程。 */
					OverlapChars   int      `json:"overlapChars"`   /* 执行当前语句并推进处理流程。 */
					Category       string   `json:"category"`       /* 执行当前语句并推进处理流程。 */
					Tags           []string `json:"tags"`           /* 执行当前语句并推进处理流程。 */
					Content        string   `json:"content"`        /* 执行当前语句并推进处理流程。 */
					Additional     struct { /* 执行当前语句并推进处理流程。 */
						Certainty float64 `json:"certainty"` /* 执行当前语句并推进处理流程。 */
					} `json:"_additional"` /* 结束当前表达式或代码块。 */
				} `json:"IotKnowledge"` /* 结束当前表达式或代码块。 */
			} `json:"Get"` /* 结束当前表达式或代码块。 */
		} `json:"data"` /* 结束当前表达式或代码块。 */
		Errors []any `json:"errors"` /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil { /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(raw.Errors) > 0 { /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("weaviate graphql: %v", raw.Errors) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	out := []ports.KnowledgeHit{}          /* 更新 out 的值。 */
	for _, v := range raw.Data.Get.Items { /* 循环处理当前数据。 */
		if !containsAll(v.Tags, in.Tags) || v.Additional.Certainty < in.MinScore { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		out = append(out, ports.KnowledgeHit{DocumentID: v.DocumentID, ChunkID: v.ChunkID, WorkflowID: v.WorkflowID, ProductID: v.ProductID, Category: v.Category, Tags: v.Tags, Content: v.Content, Score: v.Additional.Certainty}) /* 更新 out 的值。 */
		if len(out) >= limit {                                                                                                                                                                                                       /* 判断条件并选择处理分支。 */
			break /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return out, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (w *Weaviate) ListKnowledgeChunks(ctx context.Context, tenant, documentID string) ([]model.KnowledgeChunk, error) { /* 定义 ListKnowledgeChunks 函数。 */
	if err := w.ensureInitialized(ctx); err != nil { /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	operands := []string{fmt.Sprintf(`{path:["tenantId"],operator:Equal,valueText:%q}`, tenant)} /* 更新 operands 的值。 */
	if strings.TrimSpace(documentID) != "" {                                                     /* 判断条件并选择处理分支。 */
		operands = append(operands, fmt.Sprintf(`{path:["documentId"],operator:Equal,valueText:%q}`, documentID)) /* 更新 operands 的值。 */
	} /* 结束当前表达式或代码块。 */
	where := operands[0]   /* 更新 where 的值。 */
	if len(operands) > 1 { /* 判断条件并选择处理分支。 */
		where = `{operator:And,operands:[` + strings.Join(operands, ",") + `]}` /* 更新 where 的值。 */
	} /* 结束当前表达式或代码块。 */
	// Weaviate rejects values above its configured query maximum. Page through
	// the result so inspection also works for the largest accepted documents.
	const pageSize = 10000                 /* 声明 pageSize。 */
	out := make([]model.KnowledgeChunk, 0) /* 更新 out 的值。 */
	for offset := 0; ; {                   /* 循环处理当前数据。 */
		query := fmt.Sprintf(`{Get{IotKnowledge(where:%s,limit:%d,offset:%d){documentId chunkId chunkIndex startChar endChar characterCount overlapChars content}}}`, where, pageSize, offset) /* 更新 query 的值。 */
		body, _ := json.Marshal(map[string]string{"query": query})                                                                                                                             /* 更新 _ 的值。 */
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, w.url+"/v1/graphql", bytes.NewReader(body))                                                                                 /* 更新 _ 的值。 */
		req.Header.Set("Content-Type", "application/json")                                                                                                                                     /* 执行当前语句并推进处理流程。 */
		resp, err := w.http.Do(req)                                                                                                                                                            /* 更新 err 的值。 */
		if err != nil {                                                                                                                                                                        /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if resp.StatusCode/100 != 2 { /* 判断条件并选择处理分支。 */
			status := resp.Status                                     /* 更新 status 的值。 */
			resp.Body.Close()                                         /* 执行当前语句并推进处理流程。 */
			return nil, fmt.Errorf("weaviate list chunks %s", status) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		var raw struct { /* 声明 raw。 */
			Data struct { /* 执行当前语句并推进处理流程。 */
				Get struct { /* 执行当前语句并推进处理流程。 */
					Items []struct { /* 执行当前语句并推进处理流程。 */
						DocumentID     string `json:"documentId"`     /* 执行当前语句并推进处理流程。 */
						ChunkID        string `json:"chunkId"`        /* 执行当前语句并推进处理流程。 */
						ChunkIndex     int    `json:"chunkIndex"`     /* 执行当前语句并推进处理流程。 */
						StartChar      int    `json:"startChar"`      /* 执行当前语句并推进处理流程。 */
						EndChar        int    `json:"endChar"`        /* 执行当前语句并推进处理流程。 */
						CharacterCount int    `json:"characterCount"` /* 执行当前语句并推进处理流程。 */
						OverlapChars   int    `json:"overlapChars"`   /* 执行当前语句并推进处理流程。 */
						Content        string `json:"content"`        /* 执行当前语句并推进处理流程。 */
					} `json:"IotKnowledge"` /* 结束当前表达式或代码块。 */
				} `json:"Get"` /* 结束当前表达式或代码块。 */
			} `json:"data"` /* 结束当前表达式或代码块。 */
			Errors []any `json:"errors"` /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		err = json.NewDecoder(resp.Body).Decode(&raw) /* 更新 err 的值。 */
		resp.Body.Close()                             /* 执行当前语句并推进处理流程。 */
		if err != nil {                               /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if len(raw.Errors) > 0 { /* 判断条件并选择处理分支。 */
			return nil, fmt.Errorf("weaviate list chunks: %v", raw.Errors) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		for _, item := range raw.Data.Get.Items { /* 循环处理当前数据。 */
			characterCount := item.CharacterCount /* 更新 characterCount 的值。 */
			if characterCount <= 0 {              /* 判断条件并选择处理分支。 */
				characterCount = len([]rune(item.Content)) /* 更新 characterCount 的值。 */
			} /* 结束当前表达式或代码块。 */
			out = append(out, model.KnowledgeChunk{DocumentID: item.DocumentID, ChunkID: item.ChunkID, Index: item.ChunkIndex, StartChar: item.StartChar, EndChar: item.EndChar, CharacterCount: characterCount, OverlapChars: item.OverlapChars, Content: item.Content, Vectorized: true}) /* 更新 out 的值。 */
		} /* 结束当前表达式或代码块。 */
		if len(raw.Data.Get.Items) < pageSize { /* 判断条件并选择处理分支。 */
			break /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		offset += len(raw.Data.Get.Items) /* 更新 offset 的值。 */
	} /* 结束当前表达式或代码块。 */
	return normalizeKnowledgeChunks(out, documentID), nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func normalizeKnowledgeChunks(chunks []model.KnowledgeChunk, documentID string) []model.KnowledgeChunk { /* 定义 normalizeKnowledgeChunks 函数。 */
	for index := range chunks { /* 循环处理当前数据。 */
		if chunks[index].DocumentID == "" { /* 判断条件并选择处理分支。 */
			chunks[index].DocumentID = documentID /* 更新 chunks[index].DocumentID 的值。 */
		} /* 结束当前表达式或代码块。 */
		if chunks[index].Index <= 0 { /* 判断条件并选择处理分支。 */
			chunks[index].Index = index + 1 /* 更新 chunks[index].Index 的值。 */
		} /* 结束当前表达式或代码块。 */
		if chunks[index].ChunkID == "" { /* 判断条件并选择处理分支。 */
			chunks[index].ChunkID = fmt.Sprintf("%s-chunk-%04d", chunks[index].DocumentID, chunks[index].Index) /* 更新 chunks[index].ChunkID 的值。 */
		} /* 结束当前表达式或代码块。 */
		if chunks[index].EndChar <= chunks[index].StartChar { /* 判断条件并选择处理分支。 */
			chunks[index].StartChar = 0                          /* 更新 chunks[index].StartChar 的值。 */
			chunks[index].EndChar = chunks[index].CharacterCount /* 更新 chunks[index].EndChar 的值。 */
		} /* 结束当前表达式或代码块。 */
		if chunks[index].CharacterCount <= 0 { /* 判断条件并选择处理分支。 */
			chunks[index].CharacterCount = len([]rune(chunks[index].Content)) /* 更新 chunks[index].CharacterCount 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	sort.SliceStable(chunks, func(i, j int) bool { /* 执行当前语句并推进处理流程。 */
		if chunks[i].Index == chunks[j].Index { /* 判断条件并选择处理分支。 */
			return chunks[i].ChunkID < chunks[j].ChunkID /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return chunks[i].Index < chunks[j].Index /* 返回当前处理结果。 */
	}) /* 结束当前表达式或代码块。 */
	return chunks /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func orTextFilter(path string, values []string) string { /* 定义 orTextFilter 函数。 */
	operands := make([]string, 0, len(values)) /* 更新 operands 的值。 */
	for _, value := range values {             /* 循环处理当前数据。 */
		operands = append(operands, fmt.Sprintf(`{path:[%q],operator:Equal,valueText:%q}`, path, value)) /* 更新 operands 的值。 */
	} /* 结束当前表达式或代码块。 */
	if len(operands) == 1 { /* 判断条件并选择处理分支。 */
		return operands[0] /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return `{operator:Or,operands:[` + strings.Join(operands, ",") + `]}` /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (w *Weaviate) Health(ctx context.Context) error { /* 定义 Health 函数。 */
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, w.url+"/v1/.well-known/ready", nil) /* 更新 _ 的值。 */
	resp, err := w.http.Do(req)                                                                   /* 更新 err 的值。 */
	if err != nil {                                                                               /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	resp.Body.Close()             /* 执行当前语句并推进处理流程。 */
	if resp.StatusCode/100 != 2 { /* 判断条件并选择处理分支。 */
		return fmt.Errorf("weaviate %s", resp.Status) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
