package protocolruntime

import (
	"context"
	"errors"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"sync"
	"time"
)

type heldExecution struct {
	lease   model.ExecutionLease
	ctx     context.Context
	cancel  context.CancelFunc
	touched time.Time
	timer   *time.Timer
}

// Coordinator fences an execution session on database failure or lost ownership.
// It uses local monotonic deadlines shorter than the database lease, so stale
// sockets stop before another process may acquire the same resource.
type Coordinator struct {
	repo            ports.Repository
	owner, endpoint string
	mu              sync.Mutex
	held            map[string]*heldExecution
}

func NewCoordinator(repo ports.Repository, owner, endpoint string) *Coordinator {
	return &Coordinator{repo: repo, owner: owner, endpoint: endpoint, held: map[string]*heldExecution{}}
}
func (c *Coordinator) Claim(ctx context.Context, p model.DeviceAccessProfile) (context.Context, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	k := listenerKey(p.TenantID, p.ID)
	if h := c.held[k]; h != nil && h.ctx.Err() == nil {
		h.touched = time.Now()
		return h.ctx, true
	}
	start := time.Now()
	lease, ok, err := c.repo.AcquireExecutionLease(ctx, p.TenantID, "profile/"+p.ID, c.owner, c.endpoint, 10*time.Second)
	if err != nil || !ok {
		return ctx, false
	}
	session, cancel := context.WithCancel(ctx)
	timer := time.AfterFunc(time.Until(start.Add(8*time.Second)), cancel)
	c.held[k] = &heldExecution{lease: lease, ctx: session, cancel: cancel, touched: start, timer: timer}
	return session, true
}

func (c *Coordinator) Run(ctx context.Context) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	defer func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		for _, h := range c.held {
			h.timer.Stop()
			h.cancel()
			releaseCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			_ = c.repo.ReleaseExecutionLease(releaseCtx, h.lease)
			cancel()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		c.mu.Lock()
		for k, h := range c.held {
			if time.Since(h.touched) > 3*time.Second || h.ctx.Err() != nil {
				h.timer.Stop()
				h.cancel()
				_ = c.repo.ReleaseExecutionLease(ctx, h.lease)
				delete(c.held, k)
				continue
			}
			renewStarted := time.Now()
			renewCtx, cancel := context.WithTimeout(ctx, time.Second)
			lease, ok, err := c.repo.AcquireExecutionLease(renewCtx, h.lease.TenantID, h.lease.Resource, c.owner, c.endpoint, 10*time.Second)
			cancel()
			if err != nil || !ok || lease.Token != h.lease.Token {
				h.timer.Stop()
				h.cancel()
				delete(c.held, k)
			} else {
				h.lease = lease
				h.timer.Reset(time.Until(renewStarted.Add(8 * time.Second)))
			}
		}
		c.mu.Unlock()
	}
}
func (c *Coordinator) Validate(ctx context.Context, p model.DeviceAccessProfile) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	h := c.held[listenerKey(p.TenantID, p.ID)]
	var token int64
	if h != nil {
		token = h.lease.Token
	}
	c.mu.Unlock()
	current, err := c.repo.GetExecutionLease(ctx, p.TenantID, "profile/"+p.ID)
	if err != nil || current.Owner != c.owner || current.Token != token {
		return errors.New("execution ownership lost")
	}
	profile, err := c.repo.GetDeviceAccessProfile(ctx, p.TenantID, p.ID)
	if err != nil || profile.Configuration() != p.Configuration() || !profile.Enabled {
		return errors.New("execution configuration changed")
	}
	return nil
}
