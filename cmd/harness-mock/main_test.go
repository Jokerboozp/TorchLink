package main

import (
	"context"
	"errors"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	aiadapter "iot-platform/internal/adapters/ai"
	"iot-platform/internal/aioutput"
	"iot-platform/internal/ports"
)

const token = "mock-harness-token-at-least-32-characters"

func TestMockSatisfiesPlatformContract(t *testing.T) {
	srv := httptest.NewServer(newServer(token, 50*time.Millisecond, 0, 1, 0))
	defer srv.Close()
	client, err := aiadapter.NewHarness(srv.URL, token, "http://api/mcp/harness", "mock", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err = client.Health(ctx); err != nil {
		t.Fatal(err)
	}
	res, err := client.StreamChat(ctx, ports.AIWorkflowRequest{RunID: "r1", WorkflowID: "alarm-handler", Question: "q", MCPToken: "t"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	analysis, err := aioutput.DecodeAlarmAnalysis(res.Answer, "a1", res.Model)
	if err != nil || len(analysis.PossibleReasons) == 0 || len(analysis.Suggestions) == 0 {
		t.Fatal("alarm analysis answer rejected by the platform decoder", analysis, err)
	}
	if !res.UsageReported || res.Usage.InputTokens != 1 || res.Usage.OutputTokens == 0 {
		t.Fatalf("usage was not reported: %+v", res)
	}
	draft, err := client.StreamChat(ctx, ports.AIWorkflowRequest{RunID: "r2", WorkflowID: "rule-drafter", Question: "q", MCPToken: "t"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = aioutput.DecodeRuleDraft(draft.Answer); err != nil {
		t.Fatal("rule draft rejected", err)
	}
	// Concurrency 1: a second concurrent run gets the real gateway's busy signal.
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := client.StreamChat(ctx, ports.AIWorkflowRequest{RunID: "c" + string(rune('0'+i)), WorkflowID: "ops-assistant", Question: "q", MCPToken: "t"}, nil)
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	busy := 0
	for err := range errs {
		if errors.Is(err, ports.ErrAIWorkflowBusy) {
			busy++
		}
	}
	if busy != 1 {
		t.Fatal("concurrency limit not enforced", busy)
	}
	if _, err = aiadapter.NewHarness(srv.URL, "wrong-token-but-long-enough-0123456789", "http://api/mcp/harness", "mock", time.Second); err == nil {
		c, _ := aiadapter.NewHarness(srv.URL, "wrong-token-but-long-enough-0123456789", "http://api/mcp/harness", "mock", time.Second)
		if c.Health(ctx) == nil {
			t.Fatal("wrong token accepted")
		}
	}
}
