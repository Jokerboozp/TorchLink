package aiworkflow

import (
	"context"
	"errors"
	"time"

	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// AIRunMeta describes a run for its record; prompts are measured, not kept.
type AIRunMeta struct {
	TenantID, Actor, WorkflowID, PromptVersion, Model string
	InputBytes                                        int
	StartedAt                                         time.Time
}

// aiRunMetrics is implemented by the metrics registry; plain counters used
// in tests only receive the run counter.
type aiRunMetrics interface {
	Add(string, uint64)
	Observe(string, float64)
}

// RecordAIRun stores the run's record and updates the AI metrics. Recording
// never fails the run: a store error is only logged.
func (e *Service) RecordAIRun(meta AIRunMeta, result ports.AIWorkflowResult, runErr error) {
	// Durations use the wall clock like StartedAt, not the engine's clock.
	now := time.Now()
	record := model.AIRunRecord{RunID: result.RunID, TenantID: meta.TenantID, Actor: meta.Actor, WorkflowID: meta.WorkflowID, PromptVersion: meta.PromptVersion,
		Model: meta.Model, InputBytes: meta.InputBytes, OutputBytes: len(result.Answer), Usage: result.Usage, UsageReported: result.UsageReported, ToolCalls: result.ToolCalls,
		DurationMs: max(0, now.Sub(meta.StartedAt).Milliseconds()), Status: AIRunStatus(runErr), StartedAt: meta.StartedAt.UnixMilli(), FinishedAt: now.UnixMilli()}
	if result.Model != "" {
		record.Model = result.Model
	}
	if record.WorkflowID == "" {
		record.WorkflowID = result.WorkflowID
	}
	if runErr != nil {
		record.Error = truncateRunes(runErr.Error(), 512)
	}
	if e.engine.Metrics != nil {
		e.engine.Metrics.Inc(metrics.Series("ai_run_total", "workflow", record.WorkflowID, "status", record.Status))
		if m, ok := e.engine.Metrics.(aiRunMetrics); ok {
			m.Observe(metrics.Series("ai_run_duration_seconds", "workflow", record.WorkflowID), float64(record.DurationMs)/1000)
			for kind, value := range map[string]int64{"input": record.Usage.InputTokens, "output": record.Usage.OutputTokens, "cache_read": record.Usage.CacheReadTokens, "reasoning": record.Usage.ReasoningTokens} {
				if value > 0 {
					m.Add(metrics.Series("ai_tokens_total", "workflow", record.WorkflowID, "kind", kind), uint64(value))
				}
			}
		}
	}
	if e.engine.AIRuns == nil || record.RunID == "" || record.TenantID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := e.engine.AIRuns.SaveAIRun(ctx, record); err != nil && e.engine.Log != nil {
		e.engine.Log.Warn("save AI run record failed", "runId", record.RunID, "workflow", record.WorkflowID, "error", err)
	}
}

// AIRunStatus classifies a run's outcome for its record.
func AIRunStatus(err error) string {
	switch {
	case err == nil:
		return model.AIRunSucceeded
	case errors.Is(err, ports.ErrAIWorkflowStopped), errors.Is(err, context.Canceled):
		return model.AIRunStopped
	case errors.Is(err, context.DeadlineExceeded):
		return model.AIRunTimeout
	default:
		return model.AIRunFailed
	}
}
