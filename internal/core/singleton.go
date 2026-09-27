package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"time"
)

// SystemTenant scopes cluster-wide execution leases that belong to no tenant.
const SystemTenant = "_system"

// Singleton lease timing; variables so tests can shorten them.
var (
	singletonTTL   = 20 * time.Second
	singletonRenew = 5 * time.Second
)

// Identity returns this process's lease owner name, generated once.
func (e *Engine) Identity() string {
	e.identityOnce.Do(func() {
		if e.identity == "" {
			host, _ := os.Hostname()
			b := make([]byte, 6)
			_, _ = rand.Read(b)
			e.identity = host + "-" + hex.EncodeToString(b)
		}
	})
	return e.identity
}

// SetIdentity names this process (instance ID); call before Start.
func (e *Engine) SetIdentity(id string) {
	e.identityOnce.Do(func() {
		b := make([]byte, 4)
		_, _ = rand.Read(b)
		e.identity = id + "-" + hex.EncodeToString(b)
	})
}

// RunSingleton runs fn every interval on exactly one process of the cluster.
// The holder of lease schedule/<name> runs it; others stand by and take over
// when the lease expires. Losing the lease cancels a running fn.
func (e *Engine) RunSingleton(ctx context.Context, name string, interval time.Duration, fn func(context.Context) error) {
	ttl, renew := singletonTTL, singletonRenew
	go func() {
		resource := "schedule/" + name
		for ctx.Err() == nil {
			lease, held, err := e.Repo.AcquireExecutionLease(ctx, SystemTenant, resource, e.Identity(), "", ttl)
			if err != nil || !held {
				if err != nil && ctx.Err() == nil && e.Log != nil {
					e.Log.Warn("singleton lease unavailable", "job", name, "error", err)
				}
				sleepCtx(ctx, renew)
				continue
			}
			leaderCtx, cancel := context.WithCancel(ctx)
			done := make(chan struct{})
			go func() {
				defer close(done)
				ticker := time.NewTicker(interval)
				defer ticker.Stop()
				for {
					if err := fn(leaderCtx); err != nil && leaderCtx.Err() == nil && e.Log != nil {
						e.Log.Error("singleton job failed", "job", name, "error", err)
					}
					select {
					case <-leaderCtx.Done():
						return
					case <-ticker.C:
					}
				}
			}()
			// Renew until the lease is lost; a changed token means another
			// process took over after an expiry, so this one must stop.
			for leaderCtx.Err() == nil {
				sleepCtx(leaderCtx, renew)
				if leaderCtx.Err() != nil {
					break
				}
				renewed, ok, err := e.Repo.AcquireExecutionLease(ctx, SystemTenant, resource, e.Identity(), "", ttl)
				if err != nil || !ok || renewed.Token != lease.Token {
					break
				}
			}
			cancel()
			<-done
			if ctx.Err() != nil {
				releaseCtx, stop := context.WithTimeout(context.Background(), 2*time.Second)
				_ = e.Repo.ReleaseExecutionLease(releaseCtx, lease)
				stop()
			}
		}
	}()
}

func sleepCtx(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
