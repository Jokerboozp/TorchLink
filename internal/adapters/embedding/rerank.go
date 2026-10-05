package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"iot-platform/internal/ports"
)

// Reranker scores passages against a question with a cross-encoder behind a
// /v1/rerank endpoint (the bundled llama.cpp reranker, or a Jina / Cohere
// compatible API).
type Reranker struct {
	baseURL string
	http    *http.Client
}

var _ ports.Reranker = (*Reranker)(nil)

// NewReranker accepts the bundled service (IOT_LOCAL_AI_HOSTS) or an HTTPS API.
func NewReranker(baseURL string, timeout time.Duration) (*Reranker, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && !ports.LocalAIEndpoint(baseURL, localHosts)) {
		return nil, errors.New("rerank URL must be the bundled rerank service or an HTTPS API")
	}
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	return &Reranker{baseURL: baseURL, http: &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("rerank service redirects are not followed")
	}}}, nil
}

// Rerank returns one relevance in (0, 1) per document, in document order.
func (r *Reranker) Rerank(ctx context.Context, query string, documents []string) ([]float64, error) {
	body, err := json.Marshal(map[string]any{"model": "reranker", "query": query, "documents": documents})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.baseURL+"/v1/rerank", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.http.Do(req)
	if err != nil {
		return nil, errors.New("rerank service unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("rerank service returned HTTP %d", resp.StatusCode)
	}
	var parsed struct {
		Results []struct {
			Index int     `json:"index"`
			Score float64 `json:"relevance_score"`
		} `json:"results"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&parsed); err != nil {
		return nil, errors.New("rerank service returned invalid JSON")
	}
	scores := make([]float64, len(documents))
	seen := make([]bool, len(documents))
	for _, item := range parsed.Results {
		if item.Index < 0 || item.Index >= len(documents) || seen[item.Index] || math.IsNaN(item.Score) || math.IsInf(item.Score, 0) {
			return nil, errors.New("rerank service returned invalid results")
		}
		// Cross-encoders return logits; the sigmoid maps them to a relevance.
		scores[item.Index], seen[item.Index] = 1/(1+math.Exp(-item.Score)), true
	}
	for _, ok := range seen {
		if !ok {
			return nil, errors.New("rerank service did not score every document")
		}
	}
	return scores, nil
}
