package aiadapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
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

// statefulHarness keeps provider settings in memory like the sidecar, with an
// instance ID that changes on restart, and a dynamic Agent catalog.
type statefulHarness struct {
	*httptest.Server
	mu        sync.Mutex
	busy      bool
	runs      int
	instance  string
	provider  map[string]string
	puts      int
	manifests map[string]ports.AIWorkflowManifest
}

func newStatefulHarness(t *testing.T) *statefulHarness {
	f := &statefulHarness{instance: "boot-1", provider: map[string]string{"provider": "deepseek-official", "model": "env-model"}, manifests: map[string]ports.AIWorkflowManifest{}}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch {
		case r.URL.Path == "/v1/chat/stream":
			if f.busy {
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			f.runs++
			fmt.Fprintln(w, `{"type":"run.completed","answer":"ok"}`)
		case r.URL.Path == "/v1/provider":
			if r.Method == http.MethodPut {
				_ = json.NewDecoder(r.Body).Decode(&f.provider)
				f.puts++
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"provider": f.provider["provider"], "baseUrl": f.provider["baseUrl"], "model": f.provider["model"], "apiKeyConfigured": f.provider["apiKey"] != "", "instanceId": f.instance})
		case r.URL.Path == "/v1/plugins/admin":
			items := []ports.AIWorkflowManifest{{ID: "ops-assistant", Name: "builtin"}}
			for _, m := range f.manifests {
				items = append(items, m)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
		case r.URL.Path == "/v1/plugins" && r.Method == http.MethodPost:
			var m ports.AIWorkflowManifest
			_ = json.NewDecoder(r.Body).Decode(&m)
			f.manifests[m.ID] = m
			_ = json.NewEncoder(w).Encode(ports.AIWorkflowPlugin{ID: m.ID})
		case strings.HasPrefix(r.URL.Path, "/v1/plugins/") && r.Method == http.MethodDelete:
			id := strings.TrimPrefix(r.URL.Path, "/v1/plugins/")
			if _, ok := f.manifests[id]; !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			delete(f.manifests, id)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

// restart drops in-memory provider settings like a sidecar container restart.
func (f *statefulHarness) restart() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.instance = f.instance + "+"
	f.provider = map[string]string{"provider": "deepseek-official", "model": "env-model"}
}

func (f *statefulHarness) state() (puts int, provider map[string]string, manifests map[string]ports.AIWorkflowManifest) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.puts, maps.Clone(f.provider), maps.Clone(f.manifests)
}

func TestHarnessPoolMovesOnlyOneShotRunsAwayFromBusyInstances(t *testing.T) {
	a, b := newStatefulHarness(t), newStatefulHarness(t)
	a.busy, b.busy = true, false
	pool, err := NewHarnessPool(a.URL+","+b.URL, poolToken, "http://api/mcp/harness", "m", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	var ownedByA []string
	for i := 0; len(ownedByA) < 5; i++ {
		if id := fmt.Sprint("run-", i); pool.Owner(id) == a.URL {
			ownedByA = append(ownedByA, id)
		}
	}
	for _, id := range ownedByA {
		if _, err := pool.StreamChat(ctx, ports.AIWorkflowRequest{RunID: id, ConversationID: id, Question: "q", MCPToken: "t", OneShot: true}, nil); err != nil {
			t.Fatal("business run stayed on its busy owner:", err)
		}
		// A conversation keeps its history on its owner: busy is reported.
		if _, err := pool.StreamChat(ctx, ports.AIWorkflowRequest{RunID: id + "-chat", ConversationID: id, Question: "q", MCPToken: "t"}, nil); !errors.Is(err, ports.ErrAIWorkflowBusy) {
			t.Fatal("conversation left its owner:", err)
		}
	}
	if b.runs != len(ownedByA) {
		t.Fatal(b.runs)
	}
	b.busy = true
	if _, err := pool.StreamChat(ctx, ports.AIWorkflowRequest{RunID: ownedByA[0], Question: "q", MCPToken: "t", OneShot: true}, nil); !errors.Is(err, ports.ErrAIWorkflowBusy) {
		t.Fatal("all instances busy must report busy so the caller waits:", err)
	}
}

type memProviderStore struct {
	mu        sync.Mutex
	config    *ports.AIPluginConfig
	manifests map[string]ports.StoredAIWorkflowManifest
}

func (s *memProviderStore) LoadAIProviderConfig(context.Context) (ports.AIPluginConfig, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.config == nil {
		return ports.AIPluginConfig{}, false, nil
	}
	return *s.config, true, nil
}
func (s *memProviderStore) SaveAIProviderConfig(_ context.Context, c ports.AIPluginConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config = &c
	return nil
}
func (s *memProviderStore) ListAIWorkflowManifests(context.Context) ([]ports.StoredAIWorkflowManifest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []ports.StoredAIWorkflowManifest
	for _, v := range s.manifests {
		out = append(out, v)
	}
	return out, nil
}
func (s *memProviderStore) SaveAIWorkflowManifest(_ context.Context, v ports.StoredAIWorkflowManifest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.manifests[v.ID] = v
	return nil
}

func TestProviderSyncFollowsStoredSettingsAcrossReplicasAndHarnessRestarts(t *testing.T) {
	a, b := newStatefulHarness(t), newStatefulHarness(t)
	pool, err := NewHarnessPool(a.URL+","+b.URL, poolToken, "http://api/mcp/harness", "m", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	old := ports.AIPluginConfig{Provider: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-chat", APIKey: "old-key"}
	runtime, err := NewRuntimeProvider(NewProviderRegistry(), old)
	if err != nil {
		t.Fatal(err)
	}
	store := &memProviderStore{manifests: map[string]ports.StoredAIWorkflowManifest{}}
	reconciler := NewProviderSync(runtime, pool, store, store, nil)
	ctx := context.Background()
	// Another replica saved new settings; this replica still holds the old ones.
	saved := ports.AIPluginConfig{Provider: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-reasoner", APIKey: "new-key"}
	_ = store.SaveAIProviderConfig(ctx, saved)
	if err = reconciler.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if runtime.CurrentConfig() != saved {
		t.Fatalf("replica kept stale settings: %+v", runtime.CurrentConfig())
	}
	for _, h := range []*statefulHarness{a, b} {
		if _, p, _ := h.state(); p["model"] != "deepseek-reasoner" || p["apiKey"] != "new-key" {
			t.Fatalf("harness not on stored settings: %v", p)
		}
	}
	// Settled: nothing is pushed again.
	if err = reconciler.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if puts, _, _ := a.state(); puts != 1 {
		t.Fatal("settled instance was configured again", puts)
	}
	// A restarted sidecar falls back to its environment defaults.
	a.restart()
	if err = reconciler.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if puts, p, _ := a.state(); puts != 2 || p["apiKey"] != "new-key" {
		t.Fatalf("restarted harness not configured again: puts=%d %v", puts, p)
	}
	if puts, _, _ := b.state(); puts != 1 {
		t.Fatal("untouched instance was configured again", puts)
	}
}

func TestProviderSyncReconcilesDynamicAgents(t *testing.T) {
	a, b := newStatefulHarness(t), newStatefulHarness(t)
	pool, err := NewHarnessPool(a.URL+","+b.URL, poolToken, "http://api/mcp/harness", "m", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	runtime, _ := NewRuntimeProvider(NewProviderRegistry(), ports.AIPluginConfig{Provider: "deepseek"})
	store := &memProviderStore{manifests: map[string]ports.StoredAIWorkflowManifest{}}
	reconciler := NewProviderSync(runtime, pool, store, store, nil)
	ctx := context.Background()
	legacy := ports.AIWorkflowManifest{ID: "legacy-agent", Name: "created before the store", Capabilities: []string{"c"}, AllowedTools: []string{"t"}}
	a.manifests[legacy.ID] = legacy
	gone := ports.AIWorkflowManifest{ID: "gone-agent", Name: "deleted while b was down"}
	b.manifests[gone.ID] = gone
	_ = store.SaveAIWorkflowManifest(ctx, ports.StoredAIWorkflowManifest{ID: gone.ID, Manifest: ports.AIWorkflowManifest{ID: gone.ID}, Deleted: true})
	updated := ports.AIWorkflowManifest{ID: "agent", Name: "v2", Capabilities: []string{"c"}, AllowedTools: []string{"t"}}
	a.manifests["agent"] = updated
	b.manifests["agent"] = ports.AIWorkflowManifest{ID: "agent", Name: "v1 kept while b was down"}
	_ = store.SaveAIWorkflowManifest(ctx, ports.StoredAIWorkflowManifest{ID: "agent", Manifest: updated})
	// The first pass adopts the legacy Agent, the second copies it.
	for i := 0; i < 2; i++ {
		if err = reconciler.Reconcile(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for name, h := range map[string]*statefulHarness{"a": a, "b": b} {
		_, _, m := h.state()
		if len(m) != 2 || !reflect.DeepEqual(m["agent"], updated) || !reflect.DeepEqual(m["legacy-agent"], legacy) {
			t.Fatalf("instance %s not reconciled: %+v", name, m)
		}
	}
	if stored := store.manifests["legacy-agent"]; stored.Deleted || stored.Manifest.Name != legacy.Name {
		t.Fatalf("legacy Agent not adopted: %+v", stored)
	}
}

func TestHarnessPoolReportsPartialCatalogChanges(t *testing.T) {
	up := newStatefulHarness(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	down := "http://" + listener.Addr().String()
	listener.Close()
	pool, _ := NewHarnessPool(up.URL+","+down, poolToken, "http://api/mcp/harness", "m", time.Second)
	if _, err := pool.SaveWorkflow(context.Background(), ports.AIWorkflowManifest{ID: "agent"}); !errors.Is(err, ports.ErrAIWorkflowPartial) {
		t.Fatal("partial save not marked:", err)
	}
	single, _ := NewHarnessPool(down, poolToken, "http://api/mcp/harness", "m", time.Second)
	if _, err := single.SaveWorkflow(context.Background(), ports.AIWorkflowManifest{ID: "agent"}); err == nil || errors.Is(err, ports.ErrAIWorkflowPartial) {
		t.Fatal("a change no instance accepted is not partial:", err)
	}
}
