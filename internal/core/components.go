package core

import (
	"context"
	"time"

	"iot-platform/internal/model"
)

// Components selects which consumers and background jobs this process runs.
// A combined process runs all of them; split roles run their own subset.
type Components struct {
	Parser    bool
	Processor bool
	Jobs      bool
	// OfflineScan is the device offline scan interval (jobs only).
	OfflineScan time.Duration
	// DeviceSignals is the device health signal interval (jobs only); the
	// job also needs Engine.DeviceSignals and Engine.TelemetryStats.
	DeviceSignals time.Duration
}

// AllComponents is the single-process configuration.
func AllComponents() Components {
	return Components{Parser: true, Processor: true, Jobs: true, OfflineScan: 30 * time.Second}
}

// Start runs every component, as a combined process does.
func (e *Engine) Start(ctx context.Context) error { return e.StartWith(ctx, AllComponents()) }

// StartWith subscribes the selected consumers and starts the selected jobs.
// Jobs run as cluster-wide singletons: every jobs process competes for a
// lease per job, so adding jobs replicas adds standbys, not duplicate work.
func (e *Engine) StartWith(ctx context.Context, c Components) error {
	type sub struct {
		topic, group string
		handler      func(context.Context, []byte) error
	}
	var subs []sub
	if c.Parser {
		subs = append(subs, sub{model.TopicRaw, GroupParser, e.handleRaw})
	}
	if c.Processor {
		subs = append(subs, sub{model.TopicDeviceBusiness, GroupProcessor, e.handleStandard}, sub{model.TopicDeviceState, "state", e.handleState})
	}
	// Alarm events remain available for notifications, but must never start
	// analysis. Only the explicit operator API starts an analysis job.
	for _, s := range subs {
		if err := e.Bus.Subscribe(ctx, s.topic, s.group, s.handler); err != nil {
			return err
		}
	}
	if c.Processor {
		// Outbox rows are drained with SKIP LOCKED, so every processor may relay.
		go e.relayOutbox(ctx)
	}
	if c.Jobs {
		interval := c.OfflineScan
		if interval <= 0 {
			interval = 30 * time.Second
		}
		e.RunSingleton(ctx, "raw-publish-retry", 5*time.Second, e.retryPendingRawOnce)
		e.RunSingleton(ctx, "video-media-retry", 30*time.Second, e.retryPendingVideoMediaOnce)
		e.RunSingleton(ctx, "offline-scan", interval, e.ScanOffline)
		if c.DeviceSignals > 0 && e.DeviceSignals != nil && e.TelemetryStats != nil {
			e.RunSingleton(ctx, "device-signals", c.DeviceSignals, e.ComputeDeviceSignalsOnce)
		}
	}
	return nil
}

// Consumer group names (Kafka groups are prefixed with "iot-platform-").
const (
	GroupParser    = "parser"
	GroupProcessor = "processor"
)
