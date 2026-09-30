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
	job := model.AnalysisAIRevision{ID: "job", TenantID: "t", Kind: analytics.KindDataQuality, PromptVersion: analytics.AnalysisAIPromptVersion, RunID: "facts", SnapshotID: "snapshot", SnapshotVersion: 1, WorkflowID: analytics.WorkflowDataQuality, Creator: "operator", CreatorManaged: true, CreatorSessionVersion: 3, PermissionVersion: "scope1", LeaseToken: 7, HarnessRunID: "analysis_ai_fixed", DeviceIDs: []string{"d"}}
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

func TestResponseMaintenanceInvestmentWorkflowsKeepScopedHarnessAndKnowledgeBoundary(t *testing.T) {
	for _, kind := range []string{analytics.KindResponse, analytics.KindMaintenance, analytics.KindInvestment} {
		t.Run(kind, func(t *testing.T) {
			repo := memory.NewRepository()
			engine := New(repo, nil, nil, nil, nil, nil)
			harness := &aitest.Workflows{Answer: func(ports.AIWorkflowRequest) (string, error) { return `{}`, nil }}
			engine.AIWorkflows, engine.HarnessTokens = harness, aitest.Tokens()
			spec, _ := analytics.AnalysisWorkflow(kind)
			job := model.AnalysisAIRevision{ID: "job", TenantID: "t", Kind: kind, WorkflowID: spec.WorkflowID, PromptVersion: spec.PromptVersion, RunID: "facts", SnapshotID: "snapshot", SnapshotVersion: 2, Creator: "operator", CreatorManaged: true, CreatorSessionVersion: 3, PermissionVersion: "scope", LeaseToken: 9, HarnessRunID: "fixed-harness", DeviceIDs: []string{"d"}}
			input := model.AnalysisAIFacts{SnapshotID: job.SnapshotID, SnapshotVersion: job.SnapshotVersion, Statistics: json.RawMessage(`{"unknown":1}`)}
			ctx := ports.WithAIRunIdentity(context.Background(), analytics.AIIdentity(job))
			if _, err := engine.RunAnalysisWorkflow(ctx, job, input); err != nil {
				t.Fatal(err)
			}
			request := harness.Last()
			claims, err := aitest.Claims(request)
			if err != nil || claims.Workflow != spec.WorkflowID || claims.AnalysisLeaseToken != 9 || !slices.Equal(claims.Scopes, []string{ports.MCPToolScope("query_analysis_snapshot")}) {
				t.Fatal(claims, err)
			}
			if !strings.Contains(request.Question, `"unknown":1`) || !strings.Contains(request.Question, "limitations") {
				t.Fatal("missing accurate facts or schema")
			}
			if err := repo.SaveWorkflowKnowledgeBinding(context.Background(), model.WorkflowKnowledgeBinding{TenantID: "t", WorkflowID: spec.WorkflowID, RetrievalMode: "always", TopK: 5, MinScore: .25, NoMatchPolicy: "require-evidence"}); err != nil {
				t.Fatal(err)
			}
			if _, err := engine.RunAnalysisWorkflow(ctx, job, input); err == nil || len(harness.Requests()) != 1 {
				t.Fatal("disabled checkbox bypassed mandatory workflow knowledge", err)
			}
		})
	}
}

func TestMonitoringWorkflowPromptScopesAndWorkflowProof(t *testing.T) {
	repo := memory.NewRepository()
	engine := New(repo, nil, nil, nil, nil, nil)
	harness := &aitest.Workflows{Answer: func(ports.AIWorkflowRequest) (string, error) {
		return `{"summary":"x","observedWeaknesses":[],"prioritizedChecks":[],"dependencyObservations":[],"limitations":[]}`, nil
	}}
	engine.AIWorkflows, engine.HarnessTokens = harness, aitest.Tokens()
	job := model.AnalysisAIRevision{ID: "monitoring-job", TenantID: "t", Kind: analytics.KindMonitoring, PromptVersion: analytics.MonitoringAIPromptVersion, RunID: "monitoring-facts", SnapshotID: "snapshot", SnapshotVersion: 2, WorkflowID: WorkflowMonitoring, Creator: "operator", CreatorManaged: true, CreatorSessionVersion: 3, PermissionVersion: "scope1", LeaseToken: 7, HarnessRunID: "analysis_ai_monitoring", DeviceIDs: []string{"d"}}
	input := model.AnalysisAIFacts{SnapshotID: job.SnapshotID, SnapshotVersion: 2, SummaryFactID: "snapshot/summary", Statistics: json.RawMessage(`{"knownUnavailableMs":500,"unknownMs":500,"currentDependencyOnly":true}`)}
	identity := analytics.AIIdentity(job)
	ctx := ports.WithAIRunIdentity(context.Background(), identity)
	result, err := engine.RunAnalysisWorkflow(ctx, job, input)
	if err != nil || result.RunID != job.HarnessRunID {
		t.Fatal(result, err)
	}
	request := harness.Last()
	claims, err := aitest.Claims(request)
	if err != nil || claims.Workflow != WorkflowMonitoring || claims.AnalysisJobID != job.ID || claims.RunID != job.HarnessRunID || !slices.Equal(claims.Scopes, []string{ports.MCPToolScope("query_analysis_snapshot")}) {
		t.Fatal(claims, request, err)
	}
	for _, text := range []string{"observedWeaknesses", "prioritizedChecks", "dependencyObservations", "intervals", "dependency-groups", "不能认定共同原因", "未知availableAt", `"knownUnavailableMs":500`} {
		if !strings.Contains(request.Question, text) {
			t.Fatal("monitoring prompt contract missing", text)
		}
	}
	wrong := identity
	wrong.AnalysisWorkflowID = WorkflowDataQuality
	if _, err := engine.RunAnalysisWorkflow(ports.WithAIRunIdentity(context.Background(), wrong), job, input); err == nil {
		t.Fatal("quality identity executed monitoring workflow")
	}
	wrongInput := input
	wrongInput.SnapshotVersion++
	if _, err := engine.RunAnalysisWorkflow(ctx, job, wrongInput); err == nil {
		t.Fatal("wrong fixed input version sent to model")
	}
	if err := repo.SaveWorkflowKnowledgeBinding(context.Background(), model.WorkflowKnowledgeBinding{TenantID: "t", WorkflowID: WorkflowMonitoring, RetrievalMode: "always", TopK: 5, MinScore: .25, NoMatchPolicy: "require-evidence"}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.RunAnalysisWorkflow(ctx, job, input); err == nil || len(harness.Requests()) != 1 {
		t.Fatal("monitoring checkbox bypassed saved knowledge policy", err, len(harness.Requests()))
	}
}

func TestRulePolicyWorkflowFixedPromptAndKnowledgeProof(t *testing.T) {
	repo := memory.NewRepository()
	engine := New(repo, nil, nil, nil, nil, nil)
	harness := &aitest.Workflows{Answer: func(ports.AIWorkflowRequest) (string, error) {
		return `{"summary":"x","behaviorDifferences":[],"verificationSuggestions":[],"limitations":[]}`, nil
	}}
	engine.AIWorkflows, engine.HarnessTokens = harness, aitest.Tokens()
	job := model.AnalysisAIRevision{ID: "policy-job", TenantID: "t", Kind: analytics.KindRuleLab, PromptVersion: analytics.RulePolicyAIPromptVersion, RunID: "policy-facts", SnapshotID: "snapshot", SnapshotVersion: 2, WorkflowID: WorkflowRulePolicy, Creator: "operator", CreatorManaged: true, CreatorSessionVersion: 3, PermissionVersion: "scope1", LeaseToken: 7, HarnessRunID: "analysis_ai_policy", DeviceIDs: []string{"d"}}
	input := model.AnalysisAIFacts{SnapshotID: job.SnapshotID, SnapshotVersion: 2, SummaryFactID: "snapshot/summary", Statistics: json.RawMessage(`{"knownUnavailableMs":500,"unknownMs":500,"currentDependencyOnly":true}`)}
	identity := analytics.AIIdentity(job)
	ctx := ports.WithAIRunIdentity(context.Background(), identity)
	result, err := engine.RunAnalysisWorkflow(ctx, job, input)
	if err != nil || result.RunID != job.HarnessRunID {
		t.Fatal(result, err)
	}
	request := harness.Last()
	claims, err := aitest.Claims(request)
	if err != nil || claims.Workflow != WorkflowRulePolicy || claims.AnalysisJobID != job.ID || claims.RunID != job.HarnessRunID || !slices.Equal(claims.Scopes, []string{ports.MCPToolScope("query_analysis_snapshot")}) {
		t.Fatal(claims, request, err)
	}
	for _, text := range []string{"behaviorDifferences", "verificationSuggestions", "outcomes", "diffs", "candidateDraft", "enabled 必须 false", "意图", "历史模拟", "代表性", `"knownUnavailableMs":500`} {
		if !strings.Contains(request.Question, text) {
			t.Fatal("monitoring prompt contract missing", text)
		}
	}
	wrong := identity
	wrong.AnalysisWorkflowID = WorkflowDataQuality
	if _, err := engine.RunAnalysisWorkflow(ports.WithAIRunIdentity(context.Background(), wrong), job, input); err == nil {
		t.Fatal("quality identity executed monitoring workflow")
	}
	wrongInput := input
	wrongInput.SnapshotVersion++
	if _, err := engine.RunAnalysisWorkflow(ctx, job, wrongInput); err == nil {
		t.Fatal("wrong fixed input version sent to model")
	}
	if err := repo.SaveWorkflowKnowledgeBinding(context.Background(), model.WorkflowKnowledgeBinding{TenantID: "t", WorkflowID: WorkflowRulePolicy, RetrievalMode: "always", TopK: 5, MinScore: .25, NoMatchPolicy: "require-evidence"}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.RunAnalysisWorkflow(ctx, job, input); err == nil || len(harness.Requests()) != 1 {
		t.Fatal("monitoring checkbox bypassed saved knowledge policy", err, len(harness.Requests()))
	}
}
