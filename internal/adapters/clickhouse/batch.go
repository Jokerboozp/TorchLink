package clickhouse

import (
	"bytes"
	"context"
	"sync"
	"time"
)

// Rows written concurrently are combined into one INSERT. Each caller still
// returns only after the INSERT holding its row succeeded or failed, so the
// acknowledgement semantics of the single-row path are unchanged.
const (
	batchDelay    = 20 * time.Millisecond
	batchMaxRows  = 1000
	batchMaxBytes = 4 << 20
	batchTimeout  = 30 * time.Second
)

type insertBatcher struct {
	mu      sync.Mutex
	insert  func(context.Context, []byte) error
	rows    [][]byte
	waiters []chan error
	size    int
	timer   *time.Timer
}

func newInsertBatcher(insert func(context.Context, []byte) error) *insertBatcher {
	return &insertBatcher{insert: insert}
}

// add queues one JSONEachRow line and waits for its batch to be written.
func (b *insertBatcher) add(ctx context.Context, row []byte) error {
	done := make(chan error, 1)
	b.mu.Lock()
	b.rows = append(b.rows, row)
	b.waiters = append(b.waiters, done)
	b.size += len(row)
	if len(b.rows) >= batchMaxRows || b.size >= batchMaxBytes {
		rows, waiters := b.takeLocked()
		b.mu.Unlock()
		go b.flush(rows, waiters)
	} else {
		if b.timer == nil {
			b.timer = time.AfterFunc(batchDelay, b.flushPending)
		}
		b.mu.Unlock()
	}
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		// The row may still be written; callers retry and duplicates are
		// handled the same way as before batching.
		return ctx.Err()
	}
}

func (b *insertBatcher) takeLocked() ([][]byte, []chan error) {
	rows, waiters := b.rows, b.waiters
	b.rows, b.waiters, b.size = nil, nil, 0
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
	return rows, waiters
}

func (b *insertBatcher) flushPending() {
	b.mu.Lock()
	rows, waiters := b.takeLocked()
	b.mu.Unlock()
	b.flush(rows, waiters)
}

func (b *insertBatcher) flush(rows [][]byte, waiters []chan error) {
	if len(rows) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), batchTimeout)
	defer cancel()
	err := b.insert(ctx, bytes.Join(rows, nil))
	for _, waiter := range waiters {
		waiter <- err
	}
}
