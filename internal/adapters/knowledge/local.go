package knowledge /* 声明 knowledge 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context" /* 执行当前语句并推进处理流程。 */
	"math"    /* 执行当前语句并推进处理流程。 */
	"sort"    /* 执行当前语句并推进处理流程。 */
	"strings" /* 执行当前语句并推进处理流程。 */
	"sync"    /* 执行当前语句并推进处理流程。 */
	"unicode" /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type Local struct { /* 定义 Local 类型。 */
	mu   sync.RWMutex          /* 执行当前语句并推进处理流程。 */
	docs map[string][]document /* 执行当前语句并推进处理流程。 */
}                      /* 结束当前表达式或代码块。 */
type document struct { /* 定义 document 类型。 */
	id, documentID, workflow, product, category, text            string   /* 执行当前语句并推进处理流程。 */
	chunkIndex, startChar, endChar, characterCount, overlapChars int      /* 执行当前语句并推进处理流程。 */
	tags                                                         []string /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func NewLocal() *Local { return &Local{docs: map[string][]document{}} } /* 定义 NewLocal 函数。 */
func (k *Local) Index(_ context.Context, tenant, product, id string, data []byte) error { /* 定义 Index 函数。 */
	return k.IndexKnowledge(context.Background(), ports.KnowledgeIndexInput{TenantID: tenant, ProductID: product, DocumentID: id, ChunkID: id, Content: data}) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (k *Local) IndexKnowledge(_ context.Context, in ports.KnowledgeIndexInput) error { /* 定义 IndexKnowledge 函数。 */
	k.mu.Lock()                         /* 执行当前语句并推进处理流程。 */
	defer k.mu.Unlock()                 /* 安排函数结束时执行清理。 */
	characterCount := in.CharacterCount /* 更新 characterCount 的值。 */
	if characterCount <= 0 {            /* 判断条件并选择处理分支。 */
		characterCount = len([]rune(string(in.Content))) /* 更新 characterCount 的值。 */
	} /* 结束当前表达式或代码块。 */
	k.docs[in.TenantID] = append(k.docs[in.TenantID], document{id: in.ChunkID, documentID: in.DocumentID, workflow: in.WorkflowID, product: in.ProductID, category: in.Category, chunkIndex: in.ChunkIndex, startChar: in.StartChar, endChar: in.EndChar, characterCount: characterCount, overlapChars: in.OverlapChars, tags: append([]string(nil), in.Tags...), text: string(in.Content)}) /* 更新 k.docs[in.TenantID] 的值。 */
	return nil                                                                                                                                                                                                                                                                                                                                                                               /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (k *Local) ListKnowledgeChunks(_ context.Context, tenant, documentID string) ([]model.KnowledgeChunk, error) { /* 定义 ListKnowledgeChunks 函数。 */
	k.mu.RLock()                              /* 执行当前语句并推进处理流程。 */
	defer k.mu.RUnlock()                      /* 安排函数结束时执行清理。 */
	chunks := make([]model.KnowledgeChunk, 0) /* 更新 chunks 的值。 */
	for _, item := range k.docs[tenant] {     /* 循环处理当前数据。 */
		if item.documentID != documentID { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		index := item.chunkIndex /* 更新 index 的值。 */
		if index <= 0 {          /* 判断条件并选择处理分支。 */
			index = len(chunks) + 1 /* 更新 index 的值。 */
		} /* 结束当前表达式或代码块。 */
		start, end := item.startChar, item.endChar /* 更新 end 的值。 */
		if end <= start {                          /* 判断条件并选择处理分支。 */
			start, end = 0, item.characterCount /* 更新 end 的值。 */
		} /* 结束当前表达式或代码块。 */
		chunks = append(chunks, model.KnowledgeChunk{DocumentID: item.documentID, ChunkID: item.id, Index: index, StartChar: start, EndChar: end, CharacterCount: item.characterCount, OverlapChars: item.overlapChars, Content: item.text, Vectorized: false}) /* 更新 chunks 的值。 */
	} /* 结束当前表达式或代码块。 */
	sort.SliceStable(chunks, func(i, j int) bool { /* 执行当前语句并推进处理流程。 */
		if chunks[i].Index == chunks[j].Index { /* 判断条件并选择处理分支。 */
			return chunks[i].ChunkID < chunks[j].ChunkID /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return chunks[i].Index < chunks[j].Index /* 返回当前处理结果。 */
	}) /* 结束当前表达式或代码块。 */
	return chunks, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (k *Local) Search(_ context.Context, tenant, q string, limit int) ([]string, error) { /* 定义 Search 函数。 */
	hits, err := k.SearchKnowledge(context.Background(), ports.KnowledgeSearchRequest{TenantID: tenant, Question: q, Limit: limit}) /* 更新 err 的值。 */
	if err != nil {                                                                                                                 /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	out := make([]string, 0, len(hits)) /* 更新 out 的值。 */
	for _, hit := range hits {          /* 循环处理当前数据。 */
		out = append(out, hit.Content) /* 更新 out 的值。 */
	} /* 结束当前表达式或代码块。 */
	return out, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (k *Local) SearchKnowledge(_ context.Context, in ports.KnowledgeSearchRequest) ([]ports.KnowledgeHit, error) { /* 定义 SearchKnowledge 函数。 */
	k.mu.RLock()         /* 执行当前语句并推进处理流程。 */
	defer k.mu.RUnlock() /* 安排函数结束时执行清理。 */
	if in.Limit <= 0 {   /* 判断条件并选择处理分支。 */
		in.Limit = 5 /* 更新 in.Limit 的值。 */
	} /* 结束当前表达式或代码块。 */
	terms := tokens(in.Question) /* 更新 terms 的值。 */
	type hit struct {            /* 定义 hit 类型。 */
		score int      /* 执行当前语句并推进处理流程。 */
		doc   document /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	hits := []hit{}                         /* 更新 hits 的值。 */
	for _, d := range k.docs[in.TenantID] { /* 循环处理当前数据。 */
		if in.WorkflowID != "" && !strings.EqualFold(d.workflow, in.WorkflowID) { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if !matchesAny(d.product, in.ProductIDs) || !matchesAny(d.category, in.Categories) || !containsAll(d.tags, in.Tags) { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		score := 0                       /* 更新 score 的值。 */
		lower := strings.ToLower(d.text) /* 更新 lower 的值。 */
		for _, t := range terms {        /* 循环处理当前数据。 */
			score += strings.Count(lower, t) /* 更新 score 的值。 */
		} /* 结束当前表达式或代码块。 */
		if score > 0 { /* 判断条件并选择处理分支。 */
			snippet := d.text               /* 更新 snippet 的值。 */
			if len([]rune(snippet)) > 800 { /* 判断条件并选择处理分支。 */
				snippet = string([]rune(snippet)[:800]) /* 更新 snippet 的值。 */
			} /* 结束当前表达式或代码块。 */
			d.text = snippet                   /* 更新 d.text 的值。 */
			hits = append(hits, hit{score, d}) /* 更新 hits 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	sort.Slice(hits, func(i, j int) bool { return hits[i].score > hits[j].score }) /* 执行当前语句并推进处理流程。 */
	out := []ports.KnowledgeHit{}                                                  /* 更新 out 的值。 */
	for i, h := range hits {                                                       /* 循环处理当前数据。 */
		if i >= in.Limit { /* 判断条件并选择处理分支。 */
			break /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		normalized := math.Min(1, float64(h.score)/float64(max(1, len(terms)))) /* 更新 normalized 的值。 */
		if normalized < in.MinScore {                                           /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		out = append(out, ports.KnowledgeHit{DocumentID: h.doc.documentID, ChunkID: h.doc.id, WorkflowID: h.doc.workflow, ProductID: h.doc.product, Category: h.doc.category, Tags: append([]string(nil), h.doc.tags...), Content: h.doc.text, Score: normalized}) /* 更新 out 的值。 */
	} /* 结束当前表达式或代码块。 */
	return out, nil /* 返回当前处理结果。 */
}                                             /* 结束当前表达式或代码块。 */
func (k *Local) Health(context.Context) error { return nil } /* 定义 Health 函数。 */
func tokens(s string) []string { /* 定义 tokens 函数。 */
	f := func(r rune) bool { return unicode.IsSpace(r) || strings.ContainsRune(",，。；;:：/\\()（）", r) } /* 更新 f 的值。 */
	out := []string{}                                                                                 /* 更新 out 的值。 */
	for _, v := range strings.FieldsFunc(strings.ToLower(s), f) {                                     /* 循环处理当前数据。 */
		if strings.TrimSpace(v) != "" { /* 判断条件并选择处理分支。 */
			out = append(out, v) /* 更新 out 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return out /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func matchesAny(value string, allowed []string) bool { /* 定义 matchesAny 函数。 */
	if len(allowed) == 0 { /* 判断条件并选择处理分支。 */
		return true /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for _, candidate := range allowed { /* 循环处理当前数据。 */
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(candidate)) { /* 判断条件并选择处理分支。 */
			return true /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return false /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func containsAll(values, required []string) bool { /* 定义 containsAll 函数。 */
	for _, wanted := range required { /* 循环处理当前数据。 */
		found := false                 /* 更新 found 的值。 */
		for _, value := range values { /* 循环处理当前数据。 */
			if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(wanted)) { /* 判断条件并选择处理分支。 */
				found = true /* 更新 found 的值。 */
				break        /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		if !found { /* 判断条件并选择处理分支。 */
			return false /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return true /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
