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

// The bundled service may use plain HTTP and no key; any other plain-HTTP or
// internal endpoint is still refused.
func TestClientForConfigAcceptsOnlyTheBundledServiceWithoutHTTPS(t *testing.T) {
	defer SetLocalHosts(localHosts)
	SetLocalHosts("embedding,reranker,192.168.10.0/24")
	for url, ok := range map[string]bool{
		"http://embedding:8080/v1":    true,
		"http://192.168.10.5:8093/v1": true,
		"https://api.example.com/v1":  true,
		"http://api.example.com/v1":   false,
		"http://10.1.2.3:8080/v1":     false,
		"https://10.1.2.3/v1":         false,
		"http://ollama:11434/v1":      false,
		"http://user:pw@embedding/v1": false,
		"https://localhost:8443/v1":   false,
	} {
		client, err := ClientForConfig(ports.EmbeddingConfig{BaseURL: url, Model: "bge-m3", Dimensions: 1024})
		if (err == nil) != ok {
			t.Errorf("%s: accepted=%v want %v (%v)", url, err == nil, ok, err)
		}
		if err == nil && client.requireAPIKey == IsLocal(ports.EmbeddingConfig{BaseURL: url}) {
			t.Errorf("%s: API key requirement wrong", url)
		}
	}
}

func TestRerankerNormalizesLogitsInDocumentOrder(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/rerank" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"results":[{"index":1,"relevance_score":2.3},{"index":0,"relevance_score":-10.8}]}`))
	}))
	defer server.Close()
	defer SetLocalHosts(localHosts)
	SetLocalHosts("127.0.0.1")
	r, err := NewReranker(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	scores, err := r.Rerank(context.Background(), "烟感离线", []string{"水泵", "烟感离线处置"})
	if err != nil || len(scores) != 2 || scores[1] < .9 || scores[0] > .01 || got["query"] != "烟感离线" {
		t.Fatalf("scores=%v err=%v body=%v", scores, err, got)
	}
	if _, err = r.Rerank(context.Background(), "q", []string{"a", "b", "c"}); err == nil {
		t.Fatal("a response missing documents must be rejected")
	}
	if _, err = NewReranker("http://10.0.0.9/rerank", time.Second); err == nil {
		t.Fatal("unlisted plain-HTTP reranker accepted")
	}
}
