// Package embedding calls external OpenAI-compatible /embeddings APIs.
package embedding

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"iot-platform/internal/ports"
)

const (
	maxBatchInputs   = 10
	maxResponseBytes = 64 << 20
)

type Config struct {
	BaseURL          string
	Model            string
	APIKey           string
	QueryInstruction string
	Timeout          time.Duration
	Dimensions       int
	BatchSize        int
}

type OpenAI struct {
	baseURL, model, apiKey, queryInstruction string
	http                                     *http.Client
	dimensions, batchSize                    int
	requireAPIKey                            bool
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
	if cfg.Dimensions < 0 || cfg.Dimensions > 2000 {
		return nil, errors.New("embedding dimensions must be between 1 and 2000")
	}
	if cfg.BatchSize == 0 {
		cfg.BatchSize = maxBatchInputs
	}
	if cfg.BatchSize < 1 || cfg.BatchSize > 100 {
		return nil, errors.New("embedding batch size must be between 1 and 100")
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
	return &OpenAI{baseURL: baseURL, model: model, apiKey: strings.TrimSpace(cfg.APIKey), queryInstruction: cfg.QueryInstruction, http: client, dimensions: cfg.Dimensions, batchSize: cfg.BatchSize}, nil
}

func (o *OpenAI) Model() string { return o.model }

// Embed returns one vector per input, in input order. All vectors must share
// one dimension, otherwise the service is misconfigured.
func (o *OpenAI) Embed(ctx context.Context, inputs []string, purpose ports.EmbedPurpose) ([][]float32, error) {
	if o.requireAPIKey && strings.TrimSpace(o.apiKey) == "" {
		return nil, errors.New("Embedding API Key 尚未配置")
	}
	out := make([][]float32, 0, len(inputs))
	for start := 0; start < len(inputs); start += o.batchSize {
		batch := inputs[start:min(start+o.batchSize, len(inputs))]
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
		ports.ReportKnowledgeIndexProgress(ctx, len(out), len(inputs))
	}
	for _, vector := range out {
		if len(vector) == 0 || len(vector) != len(out[0]) || (o.dimensions > 0 && len(vector) != o.dimensions) {
			return nil, errors.New("embedding service returned inconsistent vector dimensions")
		}
		for _, value := range vector {
			if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
				return nil, errors.New("embedding service returned non-finite vector values")
			}
		}
	}
	return out, nil
}

func (o *OpenAI) embedBatch(ctx context.Context, inputs []string) ([][]float32, error) {
	for attempt := 0; ; attempt++ {
		vectors, err := o.requestBatch(ctx, inputs)
		var status embeddingHTTPError
		if err == nil || attempt == 2 || ctx.Err() != nil || !errors.As(err, &status) || (status != 429 && status != 502 && status != 503 && status != 504) {
			return vectors, err
		}
		timer := time.NewTimer(time.Duration(1<<attempt) * 250 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

type embeddingHTTPError int

func (e embeddingHTTPError) Error() string {
	return fmt.Sprintf("embedding service returned HTTP %d", int(e))
}

func (o *OpenAI) requestBatch(ctx context.Context, inputs []string) ([][]float32, error) {
	payload := map[string]any{"model": o.model, "input": inputs, "encoding_format": "float"}
	if o.dimensions > 0 {
		payload["dimensions"] = o.dimensions
	}
	body, err := json.Marshal(payload)
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
		return nil, errors.New("embedding service unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
		return nil, embeddingHTTPError(resp.StatusCode)
	}
	var parsed struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&parsed); err != nil {
		return nil, errors.New("embedding service returned invalid JSON")
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

// Signature includes every vector-space setting, never the credential.
func (o *OpenAI) Signature() string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d\x00%s", o.baseURL, o.model, o.dimensions, o.queryInstruction)))
	return fmt.Sprintf("%x", sum)
}

func NormalizeConfig(cfg ports.EmbeddingConfig) ports.EmbeddingConfig {
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	cfg.Model = strings.TrimSpace(cfg.Model)
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	if cfg.Dimensions == 0 {
		cfg.Dimensions = 1024
	}
	if cfg.BatchSize == 0 {
		cfg.BatchSize = 10
	}
	if cfg.TimeoutSeconds == 0 {
		cfg.TimeoutSeconds = 60
	}
	return cfg
}

func ClientForConfig(cfg ports.EmbeddingConfig) (*OpenAI, error) {
	cfg = NormalizeConfig(cfg)
	if cfg.TimeoutSeconds < 1 || cfg.TimeoutSeconds > 300 {
		return nil, errors.New("embedding timeout must be between 1 and 300 seconds")
	}
	if len(cfg.APIKey) > 4096 || len(cfg.BaseURL) > 2048 || len(cfg.Model) > 256 || len(cfg.QueryInstruction) > 4096 {
		return nil, errors.New("embedding configuration is too long")
	}
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u.Scheme != "https" {
		return nil, errors.New("external embedding API must use HTTPS")
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".orb.internal") || host == "embedding" || host == "vllm" || host == "ollama" {
		return nil, errors.New("embedding API must be an external service")
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast()) {
		return nil, errors.New("embedding API must be an external service")
	}
	client, err := NewOpenAI(Config{BaseURL: cfg.BaseURL, Model: cfg.Model, APIKey: cfg.APIKey, Dimensions: cfg.Dimensions, BatchSize: cfg.BatchSize, QueryInstruction: cfg.QueryInstruction, Timeout: time.Duration(cfg.TimeoutSeconds) * time.Second})
	if client != nil {
		client.requireAPIKey = true
	}
	return client, err
}
