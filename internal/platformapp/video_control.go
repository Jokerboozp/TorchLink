package platformapp

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"iot-platform/internal/core"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

const (
	videoControlResource = "video/control"
	videoControlTTL      = 20 * time.Second
	videoControlRenew    = 5 * time.Second
)

// videoControl elects one API instance to run the live video module (SIP
// server, play sessions, media tasks). The others forward live requests to
// the holder's node URL and keep a standby service for database-only views.
type videoControl struct {
	mu       sync.RWMutex
	local    bool
	endpoint string
}

func (v *videoControl) state() (bool, string) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.local, v.endpoint
}

func (v *videoControl) set(local bool, endpoint string) {
	v.mu.Lock()
	v.local, v.endpoint = local, endpoint
	v.mu.Unlock()
}

// run holds or watches the video/control lease. start runs the live module
// until its context ends; stop installs the standby service. Any renewal
// failure stops the module immediately, well before another instance can
// take the lease over.
func (v *videoControl) run(ctx context.Context, leases ports.OperationsStore, owner, nodeURL string, start func(context.Context), stop func(), log *slog.Logger, renew time.Duration) {
	var cancel context.CancelFunc
	var token int64
	release := func() {
		if cancel != nil {
			cancel()
			cancel = nil
			stop()
			v.set(false, "")
			log.Warn("live video control released")
		}
	}
	defer release()
	for ctx.Err() == nil {
		lease, held, err := leases.AcquireExecutionLease(ctx, core.SystemTenant, videoControlResource, owner, nodeURL, videoControlTTL)
		switch {
		case err == nil && held && cancel != nil && lease.Token == token:
			// Renewed.
		case err == nil && held:
			release()
			var leaderCtx context.Context
			leaderCtx, cancel = context.WithCancel(ctx)
			token = lease.Token
			start(leaderCtx)
			v.set(true, nodeURL)
			log.Info("live video control acquired", "node", nodeURL, "token", token)
		default:
			release()
			endpoint := ""
			if current, getErr := leases.GetExecutionLease(ctx, core.SystemTenant, videoControlResource); getErr == nil {
				endpoint = current.Endpoint
			}
			v.set(false, endpoint)
		}
		t := time.NewTimer(renew)
		select {
		case <-ctx.Done():
			t.Stop()
		case <-t.C:
		}
	}
	if cancel != nil {
		releaseCtx, done := context.WithTimeout(context.Background(), 2*time.Second)
		_ = leases.ReleaseExecutionLease(releaseCtx, model.ExecutionLease{TenantID: core.SystemTenant, Resource: videoControlResource, Owner: owner, Token: token})
		done()
	}
}
