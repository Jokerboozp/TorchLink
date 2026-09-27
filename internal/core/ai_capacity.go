package core

import (
	"context"
	"errors"
	"iot-platform/internal/model"
	"sync"
	"time"
)

var errAIAlarmResolved = errors.New("告警已恢复，已取消自动研判")
var errAIWaitExpired = errors.New("自动研判等待超出预算，可在告警详情手动研判")

type aiCapacity struct {
	mu                sync.Mutex
	next              time.Time
	timeout, maxWait  time.Duration
	requestsPerMinute int64
}

// ConfigureAutomaticAnalysis is called before consumers start. Budgets are per
// process; cluster planning must sum them against the provider/Harness quotas.
func (e *Engine) ConfigureAutomaticAnalysis(timeout, maxWait time.Duration, rpm int64) {
	e.aiCapacity.timeout = timeout
	e.aiCapacity.maxWait = maxWait
	e.aiCapacity.requestsPerMinute = rpm
}
func (e *Engine) automaticAnalysisContext(parent context.Context, alarm model.Alarm) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(parent)
	timeout := e.aiCapacity.timeout
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	bounded, stop := context.WithTimeout(ctx, timeout)
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-bounded.Done():
				return
			case <-ticker.C:
				current, err := e.Repo.GetAlarm(bounded, alarm.TenantID, alarm.ID)
				if err == nil && (current.Status == "RECOVERED" || current.Status == "CLOSED") {
					cancel(errAIAlarmResolved)
					return
				}
			}
		}
	}()
	return bounded, func() { stop(); cancel(nil) }
}
func (e *Engine) waitAutomaticBudget(ctx context.Context, alarm model.Alarm) error {
	if e.aiCapacity.maxWait > 0 && alarm.LastTriggeredAt > 0 && e.Clock.Now().Sub(time.UnixMilli(alarm.LastTriggeredAt)) > e.aiCapacity.maxWait {
		return errAIWaitExpired
	}
	if e.aiCapacity.requestsPerMinute <= 0 {
		return nil
	}
	e.aiCapacity.mu.Lock()
	at := e.aiCapacity.next
	if at.Before(time.Now()) {
		at = time.Now()
	}
	e.aiCapacity.next = at.Add(time.Minute / time.Duration(e.aiCapacity.requestsPerMinute))
	e.aiCapacity.mu.Unlock()
	timer := time.NewTimer(time.Until(at))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case <-timer.C:
	}
	if e.aiCapacity.maxWait > 0 && alarm.LastTriggeredAt > 0 && e.Clock.Now().Sub(time.UnixMilli(alarm.LastTriggeredAt)) > e.aiCapacity.maxWait {
		return errAIWaitExpired
	}
	return nil
}
func (e *Engine) skipAutomaticAnalysis(ctx context.Context, alarm model.Alarm, reason string, cause error) error {
	e.countAISkip()
	if e.Metrics != nil {
		e.Metrics.Inc("ai_analysis_skipped_" + reason + "_total")
	}
	if cause == nil {
		return nil
	}
	// Keep the reason visible on the alarm. This is not a successful AI result.
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return e.Repo.SaveAIAnalysis(saveCtx, model.AIAnalysis{TenantID: alarm.TenantID, AlarmID: alarm.ID, KnowledgeScope: model.AIAnalysisScopeNone, Status: "skipped", Summary: cause.Error(), RiskLevel: alarm.AlarmLevel, CreatedAt: e.Clock.Now().UnixMilli(), Error: cause.Error()})
}
