package aiadapter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestOllamaRejectsUnsafeURLAndCrossOriginRedirect(t *testing.T) {
	normalized, err := NewOllama("http://localhost:11434/v1", "model")
	if err != nil {
		t.Fatal(err)
	}
	if normalized.baseURL != "http://localhost:11434" {
		t.Fatalf("Ollama /v1 suffix was not normalized: %q", normalized.baseURL)
	}
	for _, rawURL := range []string{"not-a-url", "https://secret@example.com", "https://example.com?token=secret"} {
		if _, err := NewOllama(rawURL, "model"); err == nil {
			t.Fatalf("unsafe Ollama URL %q was accepted", rawURL)
		}
	}

	var redirectedRequests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirectedRequests.Add(1)
	}))
	defer target.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirector.Close()
	client, err := NewOllama(redirector.URL, "model")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.Chat(context.Background(), "tenant", "hello"); err == nil {
		t.Fatal("cross-origin Ollama redirect was followed")
	}
	if redirectedRequests.Load() != 0 {
		t.Fatalf("redirect target received %d requests", redirectedRequests.Load())
	}
}
