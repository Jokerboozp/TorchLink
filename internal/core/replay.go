package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// MaxReplayRatePerSecond bounds requested pacing, not guaranteed throughput.
const MaxReplayRatePerSecond = 10000

// Running replays are limited per process: one per tenant and a few in all,
// so replays cannot flood the raw queue past what live devices need.
const (
	maxReplaysPerTenant = 1
	maxReplaysTotal     = 4
)

// ErrReplayBusy rejects a replay while the tenant or the process already runs
// the maximum number of replays.
var ErrReplayBusy = errors.New("已有回放任务在运行，请等待其完成或先取消")

// ErrReplayNotRunning rejects cancelling a replay this process is not running.
var ErrReplayNotRunning = errors.New("回放任务不在本实例运行或已结束")

type replayRegistry struct {
	mu      sync.Mutex
	running map[string]runningReplay
}

type runningReplay struct {
	tenant string
	cancel context.CancelFunc
}

func (r *replayRegistry) begin(tenant, id string, cancel context.CancelFunc) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running == nil {
		r.running = map[string]runningReplay{}
	}
	sameTenant := 0
	for _, v := range r.running {
		if v.tenant == tenant {
			sameTenant++
		}
	}
	if sameTenant >= maxReplaysPerTenant || len(r.running) >= maxReplaysTotal {
		return false
	}
	r.running[id] = runningReplay{tenant: tenant, cancel: cancel}
	return true
}

func (r *replayRegistry) end(id string) {
	r.mu.Lock()
	delete(r.running, id)
	r.mu.Unlock()
}

func (r *replayRegistry) cancel(tenant, id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.running[id]
	if !ok || v.tenant != tenant {
		return false
	}
	v.cancel()
	return true
}

// CancelReplay stops a replay running in this process; the task is saved as
// CANCELLED with the counts reached so far.
func (e *Engine) CancelReplay(tenant, id string) error {
	if !e.replays.cancel(tenant, id) {
		return ErrReplayNotRunning
	}
	return nil
}

func (e *Engine) StartReplay(ctx context.Context, req model.ReplayRequest) (model.ReplayRequest, error) {
	if req.TenantID == "" || req.Start <= 0 || req.End <= req.Start {
		return req, fmt.Errorf("tenantId and a valid start/end range are required")
	}
	switch req.Mode {
	case "DRY_RUN", "REINGEST", "DIFF":
	default:
		return req, fmt.Errorf("mode must be DRY_RUN, REINGEST or DIFF")
	}
	if req.RatePerSecond <= 0 {
		req.RatePerSecond = 100
	}
	if req.RatePerSecond > MaxReplayRatePerSecond {
		return req, fmt.Errorf("ratePerSecond must not exceed %d", MaxReplayRatePerSecond)
	}
	req.ID = id("replay")
	req.Status = "PENDING"
	req.CreatedAt = e.Clock.Now().UnixMilli()
	// The replay belongs to the engine, not the request, and stops with it.
	base := e.runCtx
	if base == nil {
		base = context.Background()
	}
	runCtx, cancel := context.WithCancel(base)
	if !e.replays.begin(req.TenantID, req.ID, cancel) {
		cancel()
		return req, ErrReplayBusy
	}
	if err := e.Repo.SaveReplay(ctx, req); err != nil {
		e.replays.end(req.ID)
		cancel()
		return req, err
	}
	e.RecordAudit(ctx, model.AuditLog{TenantID: req.TenantID, Actor: req.CreatedBy, Action: "replay.create", TargetType: "replay", TargetID: req.ID, Details: map[string]any{"mode": req.Mode, "start": req.Start, "end": req.End}, CreatedAt: req.CreatedAt})
	go func() {
		defer cancel()
		defer e.replays.end(req.ID)
		e.runReplay(runCtx, req)
	}()
	return req, nil
}

// replayHeartbeat is how often a running replay proves it is alive;
// replayStale is when a silent RUNNING task is reported INTERRUPTED.
const (
	replayHeartbeat = 5 * time.Second
	replayStale     = 60 * time.Second
)

// RefreshReplay reports a RUNNING or PENDING replay whose owner stopped
// heartbeating (process exit, node loss) as INTERRUPTED and persists it.
func (e *Engine) RefreshReplay(ctx context.Context, req model.ReplayRequest) model.ReplayRequest {
	if req.Status != "RUNNING" && req.Status != "PENDING" {
		return req
	}
	last := max(req.HeartbeatAt, req.CreatedAt)
	now := e.Clock.Now().UnixMilli()
	if now-last <= replayStale.Milliseconds() {
		return req
	}
	req.Status = "INTERRUPTED"
	req.CompletedAt = now
	_ = e.Repo.UpdateReplay(ctx, req)
	return req
}

func (e *Engine) runReplay(ctx context.Context, req model.ReplayRequest) {
	// Status writes outlive cancellation so a stopped replay is still saved.
	store := context.WithoutCancel(ctx)
	req.Status = "RUNNING"
	req.Owner = e.Identity()
	req.HeartbeatAt = e.Clock.Now().UnixMilli()
	_ = e.Repo.UpdateReplay(store, req)
	beat := func() {
		if now := e.Clock.Now().UnixMilli(); now-req.HeartbeatAt >= replayHeartbeat.Milliseconds() {
			req.HeartbeatAt = now
			_ = e.Repo.UpdateReplay(store, req)
		}
	}
	ticker := time.NewTicker(time.Second / time.Duration(req.RatePerSecond))
	defer ticker.Stop()
	offset := 0
	req.DiffSummary = map[string]int{"unchanged": 0, "changed": 0, "missing": 0, "errors": 0}
	for {
		indexes, err := e.Repo.ListRawIndexes(ctx, ports.RawFilter{TenantID: req.TenantID, ProductID: req.ProductID, DeviceID: req.DeviceID, Start: req.Start, End: req.End, Limit: 500, Offset: offset})
		if err != nil {
			e.Log.Error("list replay archive indexes", "replayId", req.ID, "error", err)
			req.Status = "FAILED"
			req.Failed++
			break
		}
		if len(indexes) == 0 {
			req.Status = "COMPLETED"
			break
		}
		cancelled := false
		for _, idx := range indexes {
			select {
			case <-ticker.C:
			case <-ctx.Done():
			}
			if ctx.Err() != nil {
				cancelled = true
				break
			}
			beat()
			raw, err := e.GetRaw(ctx, idx)
			if err != nil {
				e.Log.Error("read replay archive", "replayId", req.ID, "messageId", idx.MessageID, "bucket", idx.ObjectBucket, "key", idx.ObjectKey, "error", err)
				req.Failed++
				continue
			}
			switch req.Mode {
			case "REINGEST":
				// Reingested messages join the live raw queue; while ingest is
				// paused for backpressure the replay waits instead of adding load.
				if err = e.waitIngestResumed(ctx); err != nil {
					cancelled = true
					break
				}
				raw, _, err = e.prepareReplayRaw(ctx, raw, req.ParserVersion)
				if err == nil {
					var b []byte
					b, err = json.Marshal(raw)
					if err == nil {
						err = e.Bus.Publish(ctx, model.TopicRaw, model.DeviceKey(raw.TenantID, raw.DeviceID), b)
					}
				}
			case "DRY_RUN":
				_, err = e.parseReplay(ctx, raw, req.ParserVersion)
			case "DIFF":
				var current *model.StandardMessage
				current, err = e.parseReplay(ctx, raw, req.ParserVersion)
				diff := model.ReplayDiff{RawMessageID: raw.MessageID, Current: current}
				if err == nil {
					previous, previousErr := e.Repo.GetStandardMessageByRaw(ctx, req.TenantID, raw.MessageID)
					if previousErr != nil {
						diff.Status = "MISSING"
						req.DiffSummary["missing"]++
					} else {
						diff.Previous = &previous
						if equivalentMessage(previous, *current) {
							diff.Status = "UNCHANGED"
							req.DiffSummary["unchanged"]++
						} else {
							diff.Status = "CHANGED"
							req.DiffSummary["changed"]++
						}
					}
				}
				if err != nil {
					diff.Status = "ERROR"
					diff.Error = err.Error()
					req.DiffSummary["errors"]++
				}
				if len(req.Diffs) < 1000 && diff.Status != "UNCHANGED" {
					req.Diffs = append(req.Diffs, diff)
				}
			}
			if cancelled {
				break
			}
			if err != nil {
				e.Log.Error("process replay message", "replayId", req.ID, "messageId", idx.MessageID, "mode", req.Mode, "error", err)
				req.Failed++
			} else {
				req.Processed++
			}
		}
		if cancelled {
			req.Status = "CANCELLED"
			break
		}
		offset += len(indexes)
		req.HeartbeatAt = e.Clock.Now().UnixMilli()
		_ = e.Repo.UpdateReplay(store, req)
		if len(indexes) < 500 {
			req.Status = "COMPLETED"
			break
		}
	}
	req.CompletedAt = e.Clock.Now().UnixMilli()
	_ = e.Repo.UpdateReplay(store, req)
}

// waitIngestResumed blocks while raw ingest is paused for backpressure.
func (e *Engine) waitIngestResumed(ctx context.Context) error {
	for e.ingestPaused.Load() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return ctx.Err()
}

func (e *Engine) parseReplay(ctx context.Context, raw model.RawMessage, version string) (*model.StandardMessage, error) {
	raw, release, err := e.prepareReplayRaw(ctx, raw, version)
	if err != nil {
		return nil, err
	}
	if release != nil {
		return e.Parsers.ParseWithConfigContext(ctx, release.ParserType, release.Config, raw)
	}
	product, err := e.Repo.GetProduct(ctx, raw.TenantID, raw.ProductID)
	if err == nil && product.ProtocolPackageID != "" {
		pkg, pkgErr := e.Repo.GetProtocolPackage(ctx, raw.TenantID, product.ProtocolPackageID)
		if pkgErr == nil {
			return e.Parsers.ParseVersionWithConfigContext(ctx, pkg.ParserType, raw.ParserVersion, pkg.Config, raw)
		}
	}
	if version != "" {
		return nil, fmt.Errorf("cannot select parser version %s without a product protocol package", version)
	}
	return e.Parsers.Parse(raw)
}

// Both preview and reingest select the same immutable release. Only the
// in-memory copy changes; archived bytes and the frame's protocolState remain
// untouched. Without an override, keep the archived point-table snapshot.
func (e *Engine) prepareReplayRaw(ctx context.Context, raw model.RawMessage, version string) (model.RawMessage, *model.ProtocolRelease, error) {
	if raw.ProtocolID == "" || raw.ProtocolVersion == "" {
		binding, err := e.Repo.GetProductProtocolBinding(ctx, raw.TenantID, raw.ProductID)
		if err != nil && !errors.Is(err, model.ErrNotFound) {
			return raw, nil, err
		}
		if err == nil {
			if raw.ProtocolID == "" {
				raw.ProtocolID = binding.ProtocolID
			}
			if raw.ProtocolVersion == "" {
				raw.ProtocolVersion = binding.Version
			}
		}
	}
	if version != "" {
		raw.ParserVersion = version
		if raw.ProtocolID != "" {
			raw.ProtocolVersion = version
		}
	}
	if raw.ProtocolID != "" && raw.ProtocolVersion != "" {
		release, err := e.Repo.GetProtocolRelease(ctx, raw.TenantID, raw.ProtocolID, raw.ProtocolVersion)
		if err != nil {
			return raw, nil, err
		}
		if release.Status == "REVOKED" {
			return raw, nil, fmt.Errorf("protocol release %s@%s is revoked", raw.ProtocolID, raw.ProtocolVersion)
		}
		if version != "" || raw.PointTableVersion == "" {
			raw.PointTableVersion = release.PointTableVersion
		}
		return raw, &release, nil
	}
	if version != "" {
		product, err := e.Repo.GetProduct(ctx, raw.TenantID, raw.ProductID)
		if err != nil || product.ProtocolPackageID == "" {
			return raw, nil, fmt.Errorf("cannot select parser version %s without a product protocol package", version)
		}
	}
	return raw, nil, nil
}
func equivalentMessage(a, b model.StandardMessage) bool {
	return a.MessageType == b.MessageType && equivalentMap(a.Properties, b.Properties) && equivalentMap(a.Event, b.Event) && equivalentTags(a.Tags, b.Tags)
}
func equivalentMap(a, b map[string]any) bool {
	return len(a) == 0 && len(b) == 0 || reflect.DeepEqual(a, b)
}
func equivalentTags(a, b map[string]string) bool {
	return len(a) == 0 && len(b) == 0 || reflect.DeepEqual(a, b)
}
