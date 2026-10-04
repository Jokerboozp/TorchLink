package postgres

import (
	"context"
	"sync"
	"time"
)

// rawMarks batches the two bookkeeping updates every raw message receives
// on raw_archive_index — "published to the queue" and "parse attempted" —
// into one UPDATE per kind every few milliseconds instead of one statement
// per message. They are not on the correctness path: a lost published mark
// only makes the publish-retry job republish (processing is idempotent per
// message), and a lost parse mark only leaves the parse status unknown.
// Failed publishes are still recorded synchronously.
type rawMarks struct {
	r       *Repository
	mu      sync.Mutex
	publish map[[2]string]int64
	parse   map[[2]string]parseMark
	wake    chan struct{}
	done    chan struct{}
	stopped chan struct{}
}

type parseMark struct {
	at      int64
	message string
}

const (
	rawMarkInterval = 50 * time.Millisecond
	rawMarkBatch    = 2000
)

func newRawMarks(r *Repository) *rawMarks {
	m := &rawMarks{r: r, publish: map[[2]string]int64{}, parse: map[[2]string]parseMark{}, wake: make(chan struct{}, 1), done: make(chan struct{}), stopped: make(chan struct{})}
	go m.run()
	return m
}

func (m *rawMarks) addPublished(tenant, id string, at int64) {
	m.mu.Lock()
	m.publish[[2]string{tenant, id}] = at
	full := len(m.publish) >= rawMarkBatch
	m.mu.Unlock()
	if full {
		m.signal()
	}
}

func (m *rawMarks) addParsed(tenant, id string, at int64, message string) {
	m.mu.Lock()
	m.parse[[2]string{tenant, id}] = parseMark{at, message}
	full := len(m.parse) >= rawMarkBatch
	m.mu.Unlock()
	if full {
		m.signal()
	}
}

func (m *rawMarks) signal() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *rawMarks) run() {
	defer close(m.stopped)
	ticker := time.NewTicker(rawMarkInterval)
	defer ticker.Stop()
	for {
		select {
		case <-m.done:
			m.flush()
			return
		case <-ticker.C:
		case <-m.wake:
		}
		m.flush()
	}
}

// flush writes the pending marks; on failure they are merged back for the
// next attempt unless a newer mark arrived meanwhile.
func (m *rawMarks) flush() {
	m.mu.Lock()
	publish, parse := m.publish, m.parse
	m.publish, m.parse = map[[2]string]int64{}, map[[2]string]parseMark{}
	m.mu.Unlock()
	if len(publish) == 0 && len(parse) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if len(publish) > 0 {
		tenants, ids, ats := make([]string, 0, len(publish)), make([]string, 0, len(publish)), make([]int64, 0, len(publish))
		for k, at := range publish {
			tenants, ids, ats = append(tenants, k[0]), append(ids, k[1]), append(ats, at)
		}
		if _, err := m.r.pool.Exec(ctx, `UPDATE raw_archive_index AS r SET publish_attempts=r.publish_attempts+1,last_publish_error='',published_at=v.at
FROM unnest($1::text[],$2::text[],$3::bigint[]) AS v(tenant,id,at) WHERE r.tenant_id=v.tenant AND r.message_id=v.id`, tenants, ids, ats); err != nil {
			m.mu.Lock()
			for k, at := range publish {
				if _, newer := m.publish[k]; !newer {
					m.publish[k] = at
				}
			}
			m.mu.Unlock()
		}
	}
	if len(parse) > 0 {
		tenants, ids, ats, messages := make([]string, 0, len(parse)), make([]string, 0, len(parse)), make([]int64, 0, len(parse)), make([]string, 0, len(parse))
		for k, v := range parse {
			tenants, ids, ats, messages = append(tenants, k[0]), append(ids, k[1]), append(ats, v.at), append(messages, v.message)
		}
		if _, err := m.r.pool.Exec(ctx, `UPDATE raw_archive_index AS r SET parse_attempted_at=v.at,parse_error=v.message
FROM unnest($1::text[],$2::text[],$3::bigint[],$4::text[]) AS v(tenant,id,at,message) WHERE r.tenant_id=v.tenant AND r.message_id=v.id`, tenants, ids, ats, messages); err != nil {
			m.mu.Lock()
			for k, v := range parse {
				if _, newer := m.parse[k]; !newer {
					m.parse[k] = v
				}
			}
			m.mu.Unlock()
		}
	}
}

// close flushes what is pending and stops the writer.
func (m *rawMarks) close() {
	select {
	case <-m.done:
	default:
		close(m.done)
	}
	<-m.stopped
}
