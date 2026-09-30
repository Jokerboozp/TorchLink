package aiadapter

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"iot-platform/internal/ports"
)

func TestHarnessPoolRunManagementVisitsOwningInstance(t *testing.T) {
	servers := []*httptest.Server{}
	for _, runID := range []string{"one", "two"} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-IOT-Tenant-ID") != "tenant-a" {
				t.Error("missing tenant scope")
			}
			if r.Method == http.MethodGet {
				_ = json.NewEncoder(w).Encode(map[string]any{"items": []ports.AIWorkflowRun{{RunID: runID, TenantID: "tenant-a"}}})
				return
			}
			if r.URL.Path == "/v1/runs/"+runID+"/stop" {
				w.WriteHeader(202)
			} else {
				w.WriteHeader(404)
			}
		}))
		defer server.Close()
		servers = append(servers, server)
	}
	pool, err := NewHarnessPool(servers[0].URL+","+servers[1].URL, poolToken, "http://api/mcp/harness", "model", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	items, err := pool.ListWorkflowRuns(context.Background(), "tenant-a")
	if err != nil || len(items) != 2 {
		t.Fatalf("list: %v %v", items, err)
	}
	if err := pool.StopWorkflowRun(context.Background(), "tenant-a", "two"); err != nil {
		t.Fatal(err)
	}
	if err := pool.StopWorkflowRun(context.Background(), "tenant-a", "missing"); !errors.Is(err, ports.ErrAIWorkflowRunNotFound) {
		t.Fatal(err)
	}
}

func TestHarnessRunStopPropagatesWithoutSuccessfulResult(t *testing.T) {
	for _, queued := range []bool{false, true} {
		t.Run(map[bool]string{false: "running", true: "queued"}[queued], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				if body["tenantId"] != "tenant-a" || body["actor"] != "alice" {
					t.Error("trusted identity not forwarded")
				}
				if queued {
					w.WriteHeader(409)
					_, _ = w.Write([]byte(`{"error":{"code":"RUN_STOPPED"}}`))
					return
				}
				_, _ = w.Write([]byte("{\"type\":\"run.failed\",\"code\":\"RUN_STOPPED\",\"message\":\"secret-provider-body\"}\n"))
			}))
			defer server.Close()
			client, err := NewHarness(server.URL, poolToken, "http://api/mcp/harness", "model", time.Second)
			if err != nil {
				t.Fatal(err)
			}
			var event ports.AIWorkflowEvent
			_, err = client.StreamChat(context.Background(), ports.AIWorkflowRequest{TenantID: "tenant-a", Actor: "alice", RunID: "r", Question: "q", MCPToken: "token"}, func(e ports.AIWorkflowEvent) error { event = e; return nil })
			if !errors.Is(err, ports.ErrAIWorkflowStopped) || event.Code != "RUN_STOPPED" || strings.Contains(event.Message, "secret-provider-body") {
				t.Fatalf("stop lost: %v %+v", err, event)
			}
		})
	}
}

func TestHarnessStreamFailureUsesOnlySafeGatewayCategory(t *testing.T) {
	for _, code := range []string{"MODEL_ERROR", "RUNTIME_TIMEOUT", "RUNTIME_CLOSED", "RUNTIME_REJECTED", "RUNTIME_PROTOCOL_ERROR", "RUNTIME_ERROR", "RUN_ABORTED", "RUN_BLOCKED", "MAX_TOKENS", "RUN_INTERRUPTED", "RUN_STOP_FAILED", "RUN_FAILED", "UNKNOWN_CODE_secret-provider-body", ""} {
		t.Run(code, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/x-ndjson")
				_ = json.NewEncoder(w).Encode(map[string]any{"type": "run.failed", "code": code, "message": "secret-provider-body", "answer": "secret-provider-body", "data": map[string]string{"apiKey": "secret-provider-body"}})
			}))
			defer server.Close()
			client, err := NewHarness(server.URL, poolToken, "http://api/mcp/harness", "model", time.Second)
			if err != nil {
				t.Fatal(err)
			}
			var event ports.AIWorkflowEvent
			result, err := client.StreamChat(context.Background(), ports.AIWorkflowRequest{TenantID: "tenant-a", Actor: "alice", RunID: "r", WorkflowID: "recurring-alarm-analyst", Question: "q", MCPToken: "token"}, func(e ports.AIWorkflowEvent) error { event = e; return nil })
			want := code
			if code == "" || strings.HasPrefix(code, "UNKNOWN_") {
				want = "RUN_FAILED"
			}
			if err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "AI 工作流执行失败") || errors.Is(err, ports.ErrAIWorkflowStopped) || result.Answer != "" {
				t.Fatalf("failure category lost: result=%+v error=%v", result, err)
			}
			emitted, e := json.Marshal(event)
			if e != nil || event.Code != want || event.Message != err.Error() || event.RunID != "r" || event.WorkflowID != "recurring-alarm-analyst" || strings.Contains(string(emitted), "secret-provider-body") || strings.Contains(err.Error(), "secret-provider-body") {
				t.Fatalf("unsafe failure emitted: %s error=%v", emitted, err)
			}
		})
	}
}
