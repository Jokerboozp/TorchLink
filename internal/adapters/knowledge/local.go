package knowledge

import (
	"context"
	"math"
	"sort"
	"strings"
	"sync"
	"unicode"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type Local struct {
	mu   sync.RWMutex
	docs map[string][]document
}
type document struct {
	id, documentID, workflow, product, category, text            string
	chunkIndex, startChar, endChar, characterCount, overlapChars int
	tags                                                         []string
}

func NewLocal() *Local { return &Local{docs: map[string][]document{}} }
func (k *Local) DeleteKnowledgeDocument(_ context.Context, tenant, documentID, _ string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	kept := k.docs[tenant][:0]
	for _, item := range k.docs[tenant] {
		if item.documentID != documentID {
			kept = append(kept, item)
		}
	}
	k.docs[tenant] = kept
	return nil
}
func (k *Local) Index(_ context.Context, tenant, product, id string, data []byte) error {
	return k.IndexKnowledge(context.Background(), ports.KnowledgeIndexInput{TenantID: tenant, ProductID: product, DocumentID: id, ChunkID: id, Content: data})
}
func (k *Local) IndexKnowledge(_ context.Context, in ports.KnowledgeIndexInput) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	characterCount := in.CharacterCount
	if characterCount <= 0 {
		characterCount = len([]rune(string(in.Content)))
	}
	k.docs[in.TenantID] = append(k.docs[in.TenantID], document{id: in.ChunkID, documentID: in.DocumentID, workflow: in.WorkflowID, product: in.ProductID, category: in.Category, chunkIndex: in.ChunkIndex, startChar: in.StartChar, endChar: in.EndChar, characterCount: characterCount, overlapChars: in.OverlapChars, tags: append([]string(nil), in.Tags...), text: string(in.Content)})
	return nil
}

func (k *Local) ListKnowledgeChunks(_ context.Context, tenant, documentID string) ([]model.KnowledgeChunk, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	chunks := make([]model.KnowledgeChunk, 0)
	for _, item := range k.docs[tenant] {
		if item.documentID != documentID {
			continue
		}
		index := item.chunkIndex
		if index <= 0 {
			index = len(chunks) + 1
		}
		start, end := item.startChar, item.endChar
		if end <= start {
			start, end = 0, item.characterCount
		}
		chunks = append(chunks, model.KnowledgeChunk{DocumentID: item.documentID, ChunkID: item.id, Index: index, StartChar: start, EndChar: end, CharacterCount: item.characterCount, OverlapChars: item.overlapChars, Content: item.text, Vectorized: false})
	}
	sort.SliceStable(chunks, func(i, j int) bool {
		if chunks[i].Index == chunks[j].Index {
			return chunks[i].ChunkID < chunks[j].ChunkID
		}
		return chunks[i].Index < chunks[j].Index
	})
	return chunks, nil
}
func (k *Local) Search(_ context.Context, tenant, q string, limit int) ([]string, error) {
	hits, err := k.SearchKnowledge(context.Background(), ports.KnowledgeSearchRequest{TenantID: tenant, Question: q, Limit: limit})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(hits))
	for _, hit := range hits {
		out = append(out, hit.Content)
	}
	return out, nil
}
func (k *Local) SearchKnowledge(_ context.Context, in ports.KnowledgeSearchRequest) ([]ports.KnowledgeHit, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	if in.Limit <= 0 {
		in.Limit = 5
	}
	terms := tokens(in.Question)
	type hit struct {
		score int
		doc   document
	}
	hits := []hit{}
	for _, d := range k.docs[in.TenantID] {
		if in.WorkflowID != "" && !strings.EqualFold(d.workflow, in.WorkflowID) {
			continue
		}
		if !matchesAny(d.product, in.ProductIDs) || !matchesAny(d.category, in.Categories) || !containsAll(d.tags, in.Tags) {
			continue
		}
		score := 0
		lower := strings.ToLower(d.text)
		for _, t := range terms {
			score += strings.Count(lower, t)
		}
		if score > 0 {
			snippet := d.text
			if len([]rune(snippet)) > 800 {
				snippet = string([]rune(snippet)[:800])
			}
			d.text = snippet
			hits = append(hits, hit{score, d})
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	out := []ports.KnowledgeHit{}
	for i, h := range hits {
		if i >= in.Limit {
			break
		}
		normalized := math.Min(1, float64(h.score)/float64(max(1, len(terms))))
		if normalized < in.MinScore {
			continue
		}
		out = append(out, ports.KnowledgeHit{DocumentID: h.doc.documentID, ChunkID: h.doc.id, WorkflowID: h.doc.workflow, ProductID: h.doc.product, Category: h.doc.category, Tags: append([]string(nil), h.doc.tags...), Content: h.doc.text, Score: normalized})
	}
	return out, nil
}
func (k *Local) Health(context.Context) error { return nil }
func tokens(s string) []string {
	f := func(r rune) bool { return unicode.IsSpace(r) || strings.ContainsRune(",，。；;:：/\\()（）", r) }
	out := []string{}
	for _, v := range strings.FieldsFunc(strings.ToLower(s), f) {
		if strings.TrimSpace(v) != "" {
			out = append(out, v)
		}
	}
	return out
}

func matchesAny(value string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, candidate := range allowed {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(candidate)) {
			return true
		}
	}
	return false
}

func containsAll(values, required []string) bool {
	for _, wanted := range required {
		found := false
		for _, value := range values {
			if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(wanted)) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
