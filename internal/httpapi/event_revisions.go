package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"iot-platform/internal/model"
)

// eventRevisions numbers every change of a tenant's alarms and device states
// as the shared snapshot reloads, so a page that already holds the snapshot
// receives only what changed since its cursor instead of every active alarm
// and every device state (16 MB at 7,600 alarms and 16,700 devices).
//
// Cursors are valid only in the process that issued them (epoch) and for the
// same view of the user (access); anything else answers the full snapshot, so
// a request routed to another replica stays correct.
type eventRevisions struct {
	epoch   string
	mu      sync.Mutex
	tenants map[string]*tenantRevisions
}

type tenantRevisions struct {
	seq    int64
	hashes map[string][32]byte
	revs   map[string]int64
}

func newEventRevisions() *eventRevisions {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return &eventRevisions{epoch: hex.EncodeToString(b[:]), tenants: map[string]*tenantRevisions{}}
}

// observe records a freshly loaded snapshot and returns each row's revision,
// aligned with alarms and states, plus the tenant's current revision.
func (e *eventRevisions) observe(tenant string, alarms []model.Alarm, states []model.DeviceState) ([]int64, []int64, int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	t := e.tenants[tenant]
	if t == nil {
		t = &tenantRevisions{hashes: map[string][32]byte{}, revs: map[string]int64{}}
		e.tenants[tenant] = t
	}
	seen := make(map[string]bool, len(alarms)+len(states))
	revision := func(key string, value any) int64 {
		seen[key] = true
		body, _ := json.Marshal(value)
		sum := sha256.Sum256(body)
		if old, ok := t.hashes[key]; !ok || old != sum {
			t.seq++
			t.hashes[key], t.revs[key] = sum, t.seq
		}
		return t.revs[key]
	}
	alarmRevs := make([]int64, len(alarms))
	for i, v := range alarms {
		alarmRevs[i] = revision("alarm:"+v.ID, v)
	}
	stateRevs := make([]int64, len(states))
	for i, v := range states {
		stateRevs[i] = revision("state:"+v.DeviceID, v)
	}
	// Rows that left the snapshot need no delta: pages only react to rows that
	// appear or change. Forgetting them lets a returning row count as new.
	for key := range t.hashes {
		if !seen[key] {
			delete(t.hashes, key)
			delete(t.revs, key)
		}
	}
	return alarmRevs, stateRevs, t.seq
}

// cursor encodes the process, the tenant revision and the caller's view.
func (e *eventRevisions) cursor(seq int64, access string) string {
	return fmt.Sprintf("%s.%d.%s", e.epoch, seq, access)
}

// since returns the revision a valid cursor stands for; ok=false asks for the
// full snapshot.
func (e *eventRevisions) since(cursor, access string) (int64, bool) {
	parts := strings.Split(cursor, ".")
	if len(parts) != 3 || parts[0] != e.epoch || parts[2] != access {
		return 0, false
	}
	seq, err := strconv.ParseInt(parts[1], 10, 64)
	return seq, err == nil && seq >= 0
}

// eventAccess identifies what the caller may see, so a cursor issued before a
// permission or device-scope change is not reused.
func eventAccess(accessVersion string, permissions []string) string {
	sum := sha256.Sum256([]byte(accessVersion + "\x00" + strings.Join(permissions, ",")))
	return hex.EncodeToString(sum[:6])
}
