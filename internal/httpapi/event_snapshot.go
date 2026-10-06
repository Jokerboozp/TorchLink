package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"iot-platform/internal/devicescope"
	"sync"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// eventSnapshotTTL bounds how stale polled alarms and device states can be.
// Every signed-in page polls /api/v1/events, so tenant data is read once per
// window per user/access version. Authorization is applied before pagination.
const eventSnapshotTTL = 2 * time.Second
const eventSnapshotLimit = 100

// eventSnapshotViews bounds retained snapshots: one per tenant for users who
// see every device, plus one per restricted user view.
const eventSnapshotViews = 1024

type eventSnapshot struct {
	alarms     []model.Alarm
	states     []model.DeviceState
	stateTotal int
	// alarmRevs and stateRevs hold each row's revision; seq is the tenant's
	// revision when the snapshot loaded.
	alarmRevs []int64
	stateRevs []int64
	seq       int64
	err       error
	loadedAt  time.Time
	ready     chan struct{}
}

type eventSnapshots struct {
	mu        sync.Mutex
	tenants   map[string]*eventSnapshot
	now       func() time.Time
	revisions *eventRevisions
}

func newEventSnapshots() *eventSnapshots {
	return &eventSnapshots{tenants: map[string]*eventSnapshot{}, now: time.Now, revisions: newEventRevisions()}
}

// get returns active alarms and device states using the supplied scoped repo.
// One request per authorized view reloads an expired snapshot. The
// returned slices are shared and must not be modified.
// eventSnapshotStore reads the alarms and states of an event snapshot.
type eventSnapshotStore interface {
	ports.AlarmStore
	ports.DeviceStateStore
}

func (c *eventSnapshots) get(ctx context.Context, repo eventSnapshotStore, tenant string) ([]model.Alarm, []model.DeviceState, error) {
	entry, err := c.snapshot(ctx, repo, tenant)
	if err != nil {
		return nil, nil, err
	}
	return entry.alarms, entry.states, nil
}

// snapshot returns the tenant's shared snapshot with row revisions.
func (c *eventSnapshots) snapshot(ctx context.Context, repo eventSnapshotStore, tenant string, view ...string) (*eventSnapshot, error) {
	key := tenant
	if len(view) > 0 {
		key += "\x00" + view[0]
	}
	c.mu.Lock()
	entry := c.tenants[key]
	if entry != nil {
		select {
		case <-entry.ready:
			if entry.err == nil && c.now().Sub(entry.loadedAt) < eventSnapshotTTL {
				c.mu.Unlock()
				return entry, nil
			}
			entry = nil
		default:
		}
	}
	if entry == nil {
		entry = &eventSnapshot{ready: make(chan struct{})}
		// Bound retained views and their revision maps; the least recently
		// loaded finished views go first.
		for len(c.tenants) >= eventSnapshotViews {
			oldest, found := "", false
			var oldestAt time.Time
			for k, v := range c.tenants {
				select {
				case <-v.ready:
					if !found || v.loadedAt.Before(oldestAt) {
						oldest, oldestAt, found = k, v.loadedAt, true
					}
				default:
				}
			}
			if !found {
				c.mu.Unlock()
				return nil, errors.New("event snapshot capacity exhausted")
			}
			delete(c.tenants, oldest)
			c.revisions.mu.Lock()
			delete(c.revisions.tenants, oldest)
			c.revisions.mu.Unlock()
		}
		c.tenants[key] = entry
		c.mu.Unlock()
		// The load is shared, so it must not be cancelled with the first caller.
		loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		entry.alarms, entry.states, entry.stateTotal, entry.err = loadEventSnapshot(loadCtx, repo, tenant)
		cancel()
		if entry.err == nil {
			entry.alarmRevs, entry.stateRevs, entry.seq = c.revisions.observe(key, entry.alarms, entry.states)
		}
		entry.loadedAt = c.now()
		close(entry.ready)
	} else {
		c.mu.Unlock()
	}
	select {
	case <-entry.ready:
		return entry, entry.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func loadEventSnapshot(ctx context.Context, repo eventSnapshotStore, tenant string) ([]model.Alarm, []model.DeviceState, int, error) {
	alarms, err := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: tenant, Status: "ACTIVE", Limit: eventSnapshotLimit, Summary: true})
	if err != nil {
		return nil, nil, 0, err
	}
	states, total, err := repo.ListDeviceStatesPage(ctx, tenant, eventSnapshotLimit, 0)
	if err != nil {
		return nil, nil, 0, err
	}
	// Event payloads are notifications; full details remain behind the existing
	// permission-checked detail and paginated list endpoints.
	for i := range alarms {
		alarms[i].Details = nil
		alarms[i].Cameras = nil
	}

	return alarms, states, total, nil
}

func scopedEventAlarms(ctx context.Context, tenant string, rows []model.Alarm) []model.Alarm {
	out := []model.Alarm{}
	for _, v := range rows {
		if devicescope.Allowed(ctx, tenant, v.DeviceID) {
			out = append(out, v)
		}
	}
	return out
}

func scopedEventStates(ctx context.Context, tenant string, rows []model.DeviceState) []model.DeviceState {
	out := []model.DeviceState{}
	for _, v := range rows {
		if devicescope.Allowed(ctx, tenant, v.DeviceID) {
			out = append(out, v)
		}
	}
	return out
}

func eventETag(body []byte) string {
	sum := sha256.Sum256(body)
	return `"` + hex.EncodeToString(sum[:16]) + `"`
}
