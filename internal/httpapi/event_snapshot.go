package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// eventSnapshotTTL bounds how stale polled alarms and device states can be.
// Every signed-in page polls /api/v1/events, so tenant data is read once per
// window and shared by all users of the tenant; each request still applies its
// own permissions and device scope to the shared rows.
const eventSnapshotTTL = 2 * time.Second

type eventSnapshot struct {
	alarms   []model.Alarm
	states   []model.DeviceState
	err      error
	loadedAt time.Time
	ready    chan struct{}
}

type eventSnapshots struct {
	mu      sync.Mutex
	tenants map[string]*eventSnapshot
	now     func() time.Time
}

func newEventSnapshots() *eventSnapshots {
	return &eventSnapshots{tenants: map[string]*eventSnapshot{}, now: time.Now}
}

// get returns the tenant's unscoped active alarms and device states. Only one
// request per tenant reloads an expired snapshot; the others wait for it. The
// returned slices are shared and must not be modified.
func (c *eventSnapshots) get(ctx context.Context, repo ports.Repository, tenant string) ([]model.Alarm, []model.DeviceState, error) {
	c.mu.Lock()
	entry := c.tenants[tenant]
	if entry != nil {
		select {
		case <-entry.ready:
			if entry.err == nil && c.now().Sub(entry.loadedAt) < eventSnapshotTTL {
				c.mu.Unlock()
				return entry.alarms, entry.states, nil
			}
			entry = nil
		default:
		}
	}
	if entry == nil {
		entry = &eventSnapshot{ready: make(chan struct{})}
		c.tenants[tenant] = entry
		c.mu.Unlock()
		// The load is shared, so it must not be cancelled with the first caller.
		loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		entry.alarms, entry.states, entry.err = loadEventSnapshot(loadCtx, repo, tenant)
		cancel()
		entry.loadedAt = c.now()
		close(entry.ready)
	} else {
		c.mu.Unlock()
	}
	select {
	case <-entry.ready:
		return entry.alarms, entry.states, entry.err
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
}

func loadEventSnapshot(ctx context.Context, repo ports.Repository, tenant string) ([]model.Alarm, []model.DeviceState, error) {
	alarms := []model.Alarm{}
	filter := ports.AlarmFilter{TenantID: tenant, Status: "ACTIVE", Limit: 500}
	for {
		rows, err := repo.ListAlarms(ctx, filter)
		if err != nil {
			return nil, nil, err
		}
		alarms = append(alarms, rows...)
		if len(rows) < filter.Limit {
			break
		}
		filter.Offset += len(rows)
	}
	states, err := repo.ListDeviceStates(ctx, tenant)
	if err != nil {
		return nil, nil, err
	}
	return alarms, states, nil
}

func scopedEventAlarms(ctx context.Context, tenant string, rows []model.Alarm) []model.Alarm {
	out := []model.Alarm{}
	for _, v := range rows {
		if deviceAllowed(ctx, tenant, v.DeviceID) {
			out = append(out, v)
		}
	}
	return out
}

func scopedEventStates(ctx context.Context, tenant string, rows []model.DeviceState) []model.DeviceState {
	out := []model.DeviceState{}
	for _, v := range rows {
		if deviceAllowed(ctx, tenant, v.DeviceID) {
			out = append(out, v)
		}
	}
	return out
}

func eventETag(body []byte) string {
	sum := sha256.Sum256(body)
	return `"` + hex.EncodeToString(sum[:16]) + `"`
}
