package core

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/aitest"
	"iot-platform/internal/analytics"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func TestAnalysisWorkflowFixedRunTokenAndKnowledgePolicy(t *testing.T) {
	repo := memory.NewRepository()
	engine := New(repo, nil, nil, nil, nil, nil)
	harness := &aitest.Workflows{Answer: func(ports.AIWorkflowRequest) (string, error) {
		return `{"summary":"x","interpretations":[],"suggestedVerification":[],"limitations":[]}`, nil
	}}
	engine.AIWorkflows = harness
	engine.HarnessTokens = aitest.Tokens()
	job := model.AnalysisAIRevision{ID: "job", TenantID: "t", RunID: "facts", SnapshotID: "snapshot", SnapshotVersion: 1, WorkflowID: analytics.WorkflowDataQuality, Creator: "operator", CreatorManaged: true, CreatorSessionVersion: 3, PermissionVersion: "scope1", LeaseToken: 7, HarnessRunID: "analysis_ai_fixed", DeviceIDs: []string{"d"}}
	input := model.AnalysisAIFacts{SnapshotID: job.SnapshotID, SnapshotVersion: 1, SummaryFactID: "snapshot/summary", Statistics: json.RawMessage(`{"unknown":2}`)}
	ctx := ports.WithAIRunIdentity(context.Background(), analytics.AIIdentity(job))
	result, err := engine.RunAnalysisWorkflow(ctx, job, input)
	if err != nil || result.RunID != job.HarnessRunID {
		t.Fatal(result, err)
	}
	request := harness.Last()
	claims, err := aitest.Claims(request)
	if err != nil || claims.AnalysisJobID != job.ID || claims.AnalysisLeaseToken != 7 || claims.AnalysisSnapshotID != job.SnapshotID || claims.AnalysisAccessVersion != "scope1" || claims.SessionVersion != 3 || !claims.ManagedUser {
		t.Fatal(claims, err)
	}
	if !slices.Equal(claims.Scopes, []string{ports.MCPToolScope("query_analysis_snapshot")}) || claims.Knowledge != nil || !strings.Contains(request.Question, `"unknown":2`) {
		t.Fatal("unexpected tool scope or summary", claims, request.Question)
	}
	if err := repo.SaveWorkflowKnowledgeBinding(context.Background(), model.WorkflowKnowledgeBinding{TenantID: "t", WorkflowID: analytics.WorkflowDataQuality, RetrievalMode: "always", TopK: 5, MinScore: .25, NoMatchPolicy: "require-evidence"}); err != nil {
		t.Fatal(err)
	}
	if _, err = engine.RunAnalysisWorkflow(ctx, job, input); err == nil {
		t.Fatal("knowledge-disabled checkbox bypassed require-evidence policy")
	}
	if len(harness.Requests()) != 1 {
		t.Fatal("model called without required knowledge")
	}
}
