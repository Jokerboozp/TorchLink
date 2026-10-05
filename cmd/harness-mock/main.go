// harness-mock is a stand-in for the DeepSeek Harness gateway for capacity
// tests of the platform's AI scheduling (plan §7.1). It speaks the same HTTP
// contract (/health, /v1/plugins, /v1/provider, /v1/chat/stream), answers
// every business workflow with a valid structured result after a configured
// latency, enforces a concurrency limit with 429 like the real gateway, and
// never calls a model. Results measured against it describe the platform,
// not a model provider, and reports must label them as mock.
//
//	go run ./cmd/harness-mock -listen :8091 -token-env IOT_AI_HARNESS_TOKEN -latency 2s -jitter 1s -concurrency 4
package main

import (
	"crypto/subtle"
	"encoding/json"
	"flag"
	"fmt"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type server struct {
	token       string
	latency     time.Duration
	jitter      time.Duration
	failRatio   float64
	slots       chan struct{}
	runs, fails atomic.Int64
	busy        atomic.Int64
	mu          sync.Mutex
	plugins     map[string]map[string]any
}

func newServer(token string, latency, jitter time.Duration, concurrency int, failRatio float64) *server {
	s := &server{token: token, latency: latency, jitter: jitter, failRatio: failRatio, slots: make(chan struct{}, max(concurrency, 1)), plugins: map[string]map[string]any{}}
	for _, id := range ports.BuiltinAIWorkflowIDs {
		s.plugins[id] = map[string]any{"schemaVersion": 1, "id": id, "name": id + " (mock)", "builtin": true}
	}
	return s
}

func (s *server) authorized(r *http.Request) bool {
	got := r.Header.Get("X-IOT-Harness-Token")
	return s.token != "" && subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) == 1
}

// answer returns a result every platform decoder accepts.
func answer(workflow, question string) string {
	switch workflow {
	case model.AlarmAnalysisWorkflowID:
		return `{"summary":"模拟研判：容量测试替身返回的固定结论，不代表模型判断。","riskLevel":"MEDIUM","confidence":0.5,"possibleReasons":["容量测试"],"suggestions":["核对现场"]}`
	case "rule-drafter":
		return `{"name":"模拟规则","description":"容量测试替身生成的规则草稿","alarmType":"FIRE","level":"HIGH","match":"all","conditions":[{"field":"stressAlarm","operator":"eq","value":1}],"durationSeconds":0,"recovery":[],"actions":[]}`
	default:
		return "模拟结果：容量测试替身返回的固定文本（" + workflow + "），问题长度 " + fmt.Sprint(len([]rune(question))) + "。"
	}
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/health":
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "mock": true, "runs": s.runs.Load(), "busy": s.busy.Load()})
		return
	case r.Method == http.MethodGet && r.URL.Path == "/metrics":
		fmt.Fprintf(w, "harness_mock_runs_total %d\nharness_mock_failed_total %d\nharness_mock_busy %d\n", s.runs.Load(), s.fails.Load(), s.busy.Load())
		return
	}
	if !s.authorized(r) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	switch {
	case r.URL.Path == "/v1/provider":
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodGet && (r.URL.Path == "/v1/plugins" || r.URL.Path == "/v1/plugins/admin"):
		s.mu.Lock()
		items := make([]map[string]any, 0, len(s.plugins))
		for _, p := range s.plugins {
			items = append(items, p)
		}
		s.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
	case r.Method == http.MethodPost && r.URL.Path == "/v1/plugins":
		var p map[string]any
		if json.NewDecoder(r.Body).Decode(&p) != nil || p["id"] == nil {
			http.Error(w, `{"error":"invalid manifest"}`, 400)
			return
		}
		s.mu.Lock()
		s.plugins[fmt.Sprint(p["id"])] = p
		s.mu.Unlock()
		_ = json.NewEncoder(w).Encode(p)
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/v1/plugins/"):
		s.mu.Lock()
		delete(s.plugins, strings.TrimPrefix(r.URL.Path, "/v1/plugins/"))
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/chat/stream":
		s.chat(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *server) chat(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RunID, WorkflowID, Question, Model string
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in) != nil || in.RunID == "" {
		http.Error(w, `{"error":"invalid request"}`, 400)
		return
	}
	select {
	case s.slots <- struct{}{}:
	default:
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"CAPACITY_EXCEEDED"}`))
		return
	}
	defer func() { <-s.slots }()
	s.busy.Add(1)
	defer s.busy.Add(-1)
	s.runs.Add(1)
	w.Header().Set("Content-Type", "application/x-ndjson")
	enc := json.NewEncoder(w)
	flusher, _ := w.(http.Flusher)
	emit := func(v map[string]any) {
		_ = enc.Encode(v)
		if flusher != nil {
			flusher.Flush()
		}
	}
	emit(map[string]any{"type": "run.started", "runId": in.RunID, "workflowId": in.WorkflowID, "model": "mock"})
	wait := s.latency
	if s.jitter > 0 {
		wait += time.Duration(rand.Int64N(int64(s.jitter)))
	}
	select {
	case <-r.Context().Done():
		return
	case <-time.After(wait):
	}
	if rand.Float64() < s.failRatio {
		s.fails.Add(1)
		emit(map[string]any{"type": "run.failed", "runId": in.RunID, "message": "mock failure"})
		return
	}
	text := answer(in.WorkflowID, in.Question)
	emit(map[string]any{"type": "text.delta", "runId": in.RunID, "delta": text})
	// Like the real gateway, the terminal event carries token usage; the mock
	// counts characters so capacity runs exercise run records and metrics.
	emit(map[string]any{"type": "run.completed", "runId": in.RunID, "answer": text, "toolCalls": 0,
		"usage": map[string]any{"inputTokens": len([]rune(in.Question)), "outputTokens": len([]rune(text)), "cacheReadTokens": 0, "reasoningTokens": 0}})
}

func main() {
	listen := flag.String("listen", ":8091", "监听地址")
	tokenEnv := flag.String("token-env", "IOT_AI_HARNESS_TOKEN", "服务令牌所在环境变量（与平台 IOT_AI_HARNESS_TOKEN 相同）")
	latency := flag.Duration("latency", 2*time.Second, "每次运行的基础耗时")
	jitter := flag.Duration("jitter", time.Second, "额外随机耗时上限")
	concurrency := flag.Int("concurrency", 4, "同时运行上限，超出返回 429")
	failRatio := flag.Float64("fail-ratio", 0, "模拟失败比例 0-1")
	flag.Parse()
	token := os.Getenv(*tokenEnv)
	if len(token) < 32 {
		log.Fatalf("%s must hold the platform's Harness token (32+ characters)", *tokenEnv)
	}
	s := newServer(token, *latency, *jitter, *concurrency, *failRatio)
	log.Printf("harness-mock listening on %s (latency %s + up to %s, concurrency %d)", *listen, *latency, *jitter, *concurrency)
	srv := &http.Server{Addr: *listen, Handler: s, ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(srv.ListenAndServe())
}
