package aiadapter

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"iot-platform/internal/ports"
)

type fakeHarness struct {
	*httptest.Server
	runs, provider atomic.Int64
}

func newFakeHarness(t *testing.T) *fakeHarness {
	f := &fakeHarness{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/chat/stream":
			f.runs.Add(1)
			fmt.Fprintf(w, `{"type":"run.completed","answer":"ok from %s"}`+"\n", r.Host)
		case "/v1/plugins":
			_, _ = w.Write([]byte(`{"items":[]}`))
		case "/v1/provider":
			f.provider.Add(1)
			w.WriteHeader(204)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

const poolToken = "harness-service-token-at-least-32-chars"

func TestHarnessPoolKeepsConversationsOnOneInstance(t *testing.T) {
	a, b, c := newFakeHarness(t), newFakeHarness(t), newFakeHarness(t)
	pool, err := NewHarnessPool(strings.Join([]string{a.URL, b.URL, c.URL}, ","), poolToken, "http://api/mcp/harness", "m", time.Second)
	if err != nil || pool.Size() != 3 {
		t.Fatal(err)
	}
	ctx := context.Background()
	run := func(conv string) string {
		res, err := pool.StreamChat(ctx, ports.AIWorkflowRequest{RunID: "r-" + conv, ConversationID: conv, Question: "q", MCPToken: "t"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		return res.Answer
	}
	for i := 0; i < 30; i++ {
		conv := fmt.Sprint("conv-", i)
		first := run(conv)
		for j := 0; j < 3; j++ {
			if run(conv) != first {
				t.Fatal("a conversation moved between instances")
			}
		}
	}
	if a.runs.Load() == 0 || b.runs.Load() == 0 || c.runs.Load() == 0 {
		t.Fatal("conversations were not spread over the instances", a.runs.Load(), b.runs.Load(), c.runs.Load())
	}
	if err = pool.ConfigureProvider(ctx, ports.AIPluginConfig{Provider: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-chat", APIKey: "k"}); err != nil {
		t.Fatal(err)
	}
	if a.provider.Load() != 1 || b.provider.Load() != 1 || c.provider.Load() != 1 {
		t.Fatal("provider settings not applied to every instance")
	}
}

func TestHarnessPoolFailsOverOnlyWhenOwnerIsUnreachable(t *testing.T) {
	up := newFakeHarness(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	down := "http://" + listener.Addr().String()
	listener.Close()
	pool, err := NewHarnessPool(down+","+up.URL, poolToken, "http://api/mcp/harness", "m", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		conv := fmt.Sprint("c", i)
		if _, err := pool.StreamChat(context.Background(), ports.AIWorkflowRequest{RunID: conv, ConversationID: conv, Question: "q", MCPToken: "t"}, nil); err != nil {
			t.Fatal("conversation owned by a down instance did not fail over", err)
		}
	}
	if up.runs.Load() != 20 {
		t.Fatal(up.runs.Load())
	}
	if err := pool.Health(context.Background()); err != nil {
		t.Fatal("pool with a healthy instance reported unhealthy", err)
	}
}
