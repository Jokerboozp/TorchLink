package repositorytest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// AIRuns checks run records: tenant isolation, filters, newest-first paging,
// idempotent saves and per-day usage sums in the report zone.
func AIRuns(t *testing.T, store ports.AIRunStore) {
	t.Helper()
	ctx := context.Background()
	day := time.Date(2026, 10, 4, 20, 0, 0, 0, time.UTC) // 2026-10-05 in the report zone
	for i := range 5 {
		run := model.AIRunRecord{RunID: fmt.Sprintf("run-%d", i), TenantID: "t1", Actor: "admin", WorkflowID: []string{"alarm-handler", "ops-assistant"}[i%2], PromptVersion: "v1", Model: "m",
			InputBytes: 100, OutputBytes: 10, Usage: model.AIUsage{InputTokens: 10, OutputTokens: 2, CacheReadTokens: 1, ReasoningTokens: 1}, UsageReported: true, ToolCalls: i,
			DurationMs: 1000, Status: []string{model.AIRunSucceeded, model.AIRunFailed}[min(i, 1)], Error: "", StartedAt: day.Add(time.Duration(i) * time.Minute).UnixMilli()}
		run.FinishedAt = run.StartedAt + run.DurationMs
		if err := store.SaveAIRun(ctx, run); err != nil {
			t.Fatal(err)
		}
	}
	// Saving a run again keeps the first record.
	if err := store.SaveAIRun(ctx, model.AIRunRecord{RunID: "run-0", TenantID: "t1", WorkflowID: "changed", Status: model.AIRunFailed, StartedAt: 1}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAIRun(ctx, model.AIRunRecord{RunID: "other", TenantID: "t2", WorkflowID: "alarm-handler", Status: model.AIRunSucceeded, StartedAt: day.UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	runs, total, err := store.ListAIRuns(ctx, ports.AIRunFilter{TenantID: "t1", Limit: 2, Offset: 1})
	if err != nil || total != 5 || len(runs) != 2 || runs[0].RunID != "run-3" || runs[1].RunID != "run-2" || runs[0].Usage.InputTokens != 10 || !runs[0].UsageReported {
		t.Fatalf("page=%+v total=%d err=%v", runs, total, err)
	}
	if runs, total, err = store.ListAIRuns(ctx, ports.AIRunFilter{TenantID: "t1", WorkflowID: "alarm-handler", Status: model.AIRunFailed}); err != nil || total != 2 || len(runs) != 2 {
		t.Fatalf("filtered=%+v total=%d err=%v", runs, total, err)
	}
	if _, total, err = store.ListAIRuns(ctx, ports.AIRunFilter{TenantID: "t1", Start: day.Add(3 * time.Minute).UnixMilli()}); err != nil || total != 2 {
		t.Fatalf("range total=%d err=%v", total, err)
	}
	usage, err := store.AIRunUsage(ctx, ports.AIRunFilter{TenantID: "t1"})
	if err != nil || len(usage) != 2 {
		t.Fatalf("usage=%+v err=%v", usage, err)
	}
	alarm := usage[0]
	if alarm.Day != "2026-10-05" || alarm.WorkflowID != "alarm-handler" || alarm.Runs != 3 || alarm.Failed != 2 || alarm.Usage.InputTokens != 30 || alarm.ToolCalls != 6 || alarm.DurationMs != 3000 {
		t.Fatalf("alarm usage %+v", alarm)
	}
}
