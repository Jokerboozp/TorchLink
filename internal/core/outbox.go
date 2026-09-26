package core

import (
	"context"
	"time"

	"iot-platform/internal/model"
)

const outboxBatch = 100

// flushOutbox publishes events committed with alarm changes. Delivery is
// at-least-once; consumers deduplicate by alarm and trigger identity. When a
// drain is already running, the wake-up makes the relay run again after it.
func (e *Engine) flushOutbox(ctx context.Context) {
	if !e.outboxMu.TryLock() {
		e.wakeOutbox()
		return
	}
	defer e.outboxMu.Unlock()
	for {
		n, err := e.Repo.DrainOutbox(ctx, outboxBatch, func(v model.OutboxEvent) error {
			return e.Bus.Publish(ctx, v.Topic, v.Key, v.Payload)
		})
		if err != nil {
			if ctx.Err() == nil && e.Log != nil {
				e.Log.Warn("publish outbox events", "error", err)
			}
			return
		}
		if n < outboxBatch {
			return
		}
	}
}

func (e *Engine) wakeOutbox() {
	select {
	case e.outboxWake <- struct{}{}:
	default:
	}
}

// relayOutbox retries events left behind by failed publishes or restarts.
func (e *Engine) relayOutbox(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		e.flushOutbox(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-e.outboxWake:
		}
	}
}
