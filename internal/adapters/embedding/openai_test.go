package embedding

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"iot-platform/internal/ports"
)

func TestMain(m *testing.M) {
	// Production delays are seconds; tests keep the policy, not the waits.
	retryBaseDelay, retryMaxDelay, queryRetryDelay = time.Millisecond, 4*time.Millisecond, time.Millisecond
	os.Exit(m.Run())
}

func TestOpenAIEmbedBatchesAndPrefixesQueries(t *testing.T) {
	var batches [][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("unexpected request %s auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		var body struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Model != "Qwen/Qwen3-Embedding-0.6B" {
			t.Errorf("model = %q", body.Model)
		}
		batches = append(batches, body.Input)
		data := []map[string]any{}
		// Reverse order proves the adapter honours the index field.
		for i := len(body.Input) - 1; i >= 0; i-- {
			data = append(data, map[string]any{"index": i, "embedding": []float32{float32(len([]rune(body.Input[i]))), 1}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer server.Close()
	client, err := NewOpenAI(Config{BaseURL: server.URL + "/v1/", Model: "Qwen/Qwen3-Embedding-0.6B", APIKey: "secret", QueryInstruction: "Q: "})
	if err != nil {
		t.Fatal(err)
	}
	inputs := make([]string, 40)
	for i := range inputs {
		inputs[i] = strings.Repeat("字", i+1)
	}
	vectors, err := client.Embed(context.Background(), inputs, ports.EmbedDocument)
	if err != nil {
		t.Fatal(err)
	}
	if len(batches) != 4 || len(batches[0]) != 10 || len(batches[3]) != 10 {
		t.Fatalf("batches = %d/%v", len(batches), len(batches[0]))
	}
	if len(vectors) != 40 || vectors[39][0] != 40 {
		t.Fatalf("vectors not returned in input order: %v", vectors[39])
	}
	if _, err = client.Embed(context.Background(), []string{"起火"}, ports.EmbedQuery); err != nil {
		t.Fatal(err)
	}
	if got := batches[len(batches)-1][0]; got != "Q: 起火" {
		t.Fatalf("query input = %q", got)
	}
}

func TestOpenAIEmbedRejectsBadResponses(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"status": func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "model not loaded", http.StatusServiceUnavailable)
		},
		"count": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"data":[]}`))
		},
		"dimensions": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[1,2]},{"index":1,"embedding":[1]}]}`))
		},
		"redirect": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "http://example.invalid/", http.StatusFound)
		},
	}
	for name, handler := range cases {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(handler)
			defer server.Close()
			client, err := NewOpenAI(Config{BaseURL: server.URL, Model: "m"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.Embed(context.Background(), []string{"a", "b"}, ports.EmbedDocument); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestNewOpenAIValidatesConfig(t *testing.T) {
	for _, cfg := range []Config{{BaseURL: "embedding:80", Model: "m"}, {BaseURL: "http://user:pw@embedding/v1", Model: "m"}, {BaseURL: "http://embedding/v1"}} {
		if _, err := NewOpenAI(cfg); err == nil {
			t.Fatalf("expected rejection for %+v", cfg)
		}
	}
}

func TestOpenAITransientRetryAndCredentialErrorRedaction(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable, http.StatusUnauthorized} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++
				if calls == 1 || status == http.StatusUnauthorized {
					http.Error(w, "secret-provider-credential", status)
					return
				}
				_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[1,2]}]}`))
			}))
			defer server.Close()
			client, err := NewOpenAI(Config{BaseURL: server.URL, Model: "cloud-model", Dimensions: 2})
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Embed(context.Background(), []string{"消防知识"}, ports.EmbedDocument)
			if status == http.StatusUnauthorized {
				if err == nil || calls != 1 || strings.Contains(err.Error(), "secret-provider-credential") {
					t.Fatalf("auth retry or error leak: calls=%d err=%v", calls, err)
				}
			} else if err != nil || calls != 2 {
				t.Fatalf("transient recovery: calls=%d err=%v", calls, err)
			}
		})
	}
}

// Indexing rides out rate limits (honouring Retry-After) and unreachable
// services; a query gives up after one retry so search can fall back.
func TestOpenAIRetryPolicyByPurpose(t *testing.T) {
	calls, waits := 0, []time.Duration{}
	var last time.Time
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if !last.IsZero() {
			waits = append(waits, time.Since(last))
		}
		last = time.Now()
		calls++
		if calls < 4 {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "slow down", http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[1,2]}]}`))
	}))
	defer server.Close()
	client, err := NewOpenAI(Config{BaseURL: server.URL, Model: "m", Dimensions: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.Embed(context.Background(), []string{"x"}, ports.EmbedDocument); err != nil || calls != 4 {
		t.Fatalf("indexing did not ride out the rate limit: calls=%d err=%v", calls, err)
	}
	if len(waits) == 0 || waits[0] < 900*time.Millisecond {
		t.Fatalf("Retry-After not honoured: %v", waits)
	}
	calls, last = 0, time.Time{}
	if _, err = client.Embed(context.Background(), []string{"x"}, ports.EmbedQuery); err == nil || calls != queryAttempts {
		t.Fatalf("query retried too long: calls=%d err=%v", calls, err)
	}
	if !IsTransientEmbeddingError(err) {
		t.Fatal("rate limit not classified as transient", err)
	}
	listener, _ := net.Listen("tcp", "127.0.0.1:0")
	down := "http://" + listener.Addr().String()
	listener.Close()
	unreachable, _ := NewOpenAI(Config{BaseURL: down, Model: "m", Dimensions: 2})
	if _, err = unreachable.Embed(context.Background(), []string{"x"}, ports.EmbedDocument); err == nil || !IsTransientEmbeddingError(err) {
		t.Fatal("unreachable service not transient", err)
	}
	unauthorized := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) }))
	defer unauthorized.Close()
	denied, _ := NewOpenAI(Config{BaseURL: unauthorized.URL, Model: "m", Dimensions: 2})
	if _, err = denied.Embed(context.Background(), []string{"x"}, ports.EmbedDocument); err == nil || IsTransientEmbeddingError(err) {
		t.Fatal("credential failure must not be retried later", err)
	}
}
