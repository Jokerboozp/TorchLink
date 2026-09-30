// Package embedding calls OpenAI-compatible /embeddings endpoints such as a
// privately deployed HuggingFace Text Embeddings Inference (TEI) or vLLM
// service.
package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"iot-platform/internal/ports"
)

const (
	maxBatchInputs   = 32
	maxResponseBytes = 64 << 20
)

type Config struct {
	BaseURL          string
	Model            string
	APIKey           string
	QueryInstruction string
	Timeout          time.Duration
}

type OpenAI struct {
	baseURL, model, apiKey, queryInstruction string
	http                                     *http.Client
}

var _ ports.Embedder = (*OpenAI)(nil)

func NewOpenAI(cfg Config) (*OpenAI, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, errors.New("embedding base URL must be an absolute HTTP(S) URL")
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		return nil, errors.New("embedding model is required")
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = time.Minute
	}
	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("embedding service redirects are not followed")
		},
	}
	return &OpenAI{baseURL: baseURL, model: model, apiKey: strings.TrimSpace(cfg.APIKey), queryInstruction: cfg.QueryInstruction, http: client}, nil
}

func (o *OpenAI) Model() string { return o.model }

// Embed returns one vector per input, in input order. All vectors must share
// one dimension, otherwise the service is misconfigured.
func (o *OpenAI) Embed(ctx context.Context, inputs []string, purpose ports.EmbedPurpose) ([][]float32, error) {
	out := make([][]float32, 0, len(inputs))
	for start := 0; start < len(inputs); start += maxBatchInputs {
		batch := inputs[start:min(start+maxBatchInputs, len(inputs))]
		if purpose == ports.EmbedQuery && o.queryInstruction != "" {
			prefixed := make([]string, len(batch))
			for i, text := range batch {
				prefixed[i] = o.queryInstruction + text
			}
			batch = prefixed
		}
		vectors, err := o.embedBatch(ctx, batch)
		if err != nil {
			return nil, err
		}
		out = append(out, vectors...)
	}
	for _, vector := range out {
		if len(vector) == 0 || len(vector) != len(out[0]) {
			return nil, errors.New("embedding service returned inconsistent vector dimensions")
		}
	}
	return out, nil
}

func (o *OpenAI) embedBatch(ctx context.Context, inputs []string) ([][]float32, error) {
	body, err := json.Marshal(map[string]any{"model": o.model, "input": inputs, "encoding_format": "float"})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if o.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.apiKey)
	}
	resp, err := o.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding service unavailable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("embedding service %s: %s", resp.Status, strings.TrimSpace(string(detail)))
	}
	var parsed struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}
	if len(parsed.Data) != len(inputs) {
		return nil, fmt.Errorf("embedding service returned %d vectors for %d inputs", len(parsed.Data), len(inputs))
	}
	vectors := make([][]float32, len(inputs))
	for _, item := range parsed.Data {
		if item.Index < 0 || item.Index >= len(inputs) || vectors[item.Index] != nil {
			return nil, errors.New("embedding service returned invalid vector indexes")
		}
		vectors[item.Index] = item.Embedding
	}
	return vectors, nil
}

// Health performs a one-word embedding so both reachability and the model
// configuration are verified.
func (o *OpenAI) Health(ctx context.Context) error {
	_, err := o.Embed(ctx, []string{"health"}, ports.EmbedDocument)
	return err
}
