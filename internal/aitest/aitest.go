// Package aitest provides a scripted Harness workflow runtime for tests of the
// business AI features, which all run as Harness workflows.
package aitest

import (
	"context"
	"sync"

	"iot-platform/internal/auth"
	"iot-platform/internal/ports"
)

// Secret signs test business-run tokens; parse them with Tokens().Parse.
const Secret = "aitest-harness-token-secret-at-least-32-bytes"

// Tokens returns a real issuer so tests can inspect the granted scopes.
func Tokens() *auth.Manager { return auth.New(Secret) }

// Workflows records every run and answers with Answer.
type Workflows struct {
	mu       sync.Mutex
	requests []ports.AIWorkflowRequest
	// Answer returns the model text for a run; nil answers "{}".
	Answer func(ports.AIWorkflowRequest) (string, error)
	Model  string
}

func (w *Workflows) ListWorkflows(context.Context) ([]ports.AIWorkflowPlugin, error) {
	return nil, nil
}

func (w *Workflows) StreamChat(_ context.Context, req ports.AIWorkflowRequest, _ func(ports.AIWorkflowEvent) error) (ports.AIWorkflowResult, error) {
	w.mu.Lock()
	w.requests = append(w.requests, req)
	answer := w.Answer
	w.mu.Unlock()
	result := ports.AIWorkflowResult{RunID: req.RunID, WorkflowID: req.WorkflowID, Model: w.Model}
	if result.Model == "" {
		result.Model = "aitest-model"
	}
	if answer == nil {
		result.Answer = "{}"
		return result, nil
	}
	text, err := answer(req)
	result.Answer = text
	return result, err
}

func (w *Workflows) Health(context.Context) error { return nil }

// Requests returns a copy of the recorded runs.
func (w *Workflows) Requests() []ports.AIWorkflowRequest {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]ports.AIWorkflowRequest(nil), w.requests...)
}

// Last returns the most recent run.
func (w *Workflows) Last() ports.AIWorkflowRequest {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.requests) == 0 {
		return ports.AIWorkflowRequest{}
	}
	return w.requests[len(w.requests)-1]
}

// Claims decodes the MCP token a run received.
func Claims(req ports.AIWorkflowRequest) (auth.Claims, error) { return Tokens().Parse(req.MCPToken) }

// Context attaches an unmanaged identity allowed every Harness tool scope.
func Context(ctx context.Context) context.Context {
	return ports.WithAIRunIdentity(ctx, ports.AIRunIdentity{Username: "aitest", Scopes: auth.HarnessReadScopes()})
}
