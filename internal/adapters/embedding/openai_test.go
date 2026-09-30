package embedding

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"iot-platform/internal/ports"
)

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
	if len(batches) != 2 || len(batches[0]) != 32 || len(batches[1]) != 8 {
		t.Fatalf("batches = %d/%v", len(batches), len(batches[0]))
	}
	if len(vectors) != 40 || vectors[39][0] != 40 {
		t.Fatalf("vectors not returned in input order: %v", vectors[39])
	}
	if _, err = client.Embed(context.Background(), []string{"起火"}, ports.EmbedQuery); err != nil {
		t.Fatal(err)
	}
	if got := batches[2][0]; got != "Q: 起火" {
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
