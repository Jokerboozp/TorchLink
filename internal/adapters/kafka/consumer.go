package kafkaadapter

import (
	"context"
	"hash/fnv"
	"sync"
	"time"

	"github.com/segmentio/kafka-go"
	"iot-platform/internal/ports"
)

// messageSource is the part of *kafka.Reader the consumer uses.
type messageSource interface {
	FetchMessage(context.Context) (kafka.Message, error)
	CommitMessages(context.Context, ...kafka.Message) error
}

const commitInterval = 100 * time.Millisecond

// consume processes messages on parallel lanes. Messages with the same key
// (device ID for raw and standard topics, alarm ID for alarm topics) always use
// the same lane, so their order is preserved; different keys run in parallel.
// Offsets are committed only up to the highest contiguous finished message of
// each partition, so a crash can redeliver but never skip a message.
func (b *Bus) consume(ctx context.Context, source messageSource, topic, group string, lanes int, h ports.Handler) {
	if lanes < 1 {
		lanes = 1
	}
	tracker := newOffsetTracker()
	queues := make([]chan kafka.Message, lanes)
	var workers sync.WaitGroup
	for i := range queues {
		queues[i] = make(chan kafka.Message, 16)
		workers.Add(1)
		go func(queue <-chan kafka.Message) {
			defer workers.Done()
			for m := range queue {
				if !b.handle(ctx, topic, group, h, m) {
					return
				}
				tracker.finish(m)
			}
		}(queues[i])
	}
	commitCtx, stopCommits := context.WithCancel(context.Background())
	committed := make(chan struct{})
	go func() {
		defer close(committed)
		ticker := time.NewTicker(commitInterval)
		defer ticker.Stop()
		for {
			select {
			case <-commitCtx.Done():
				// Commit finished work once more on shutdown; the consumer
				// context is already cancelled, so use a short separate one.
				final, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				commitReady(final, source, tracker)
				cancel()
				return
			case <-ticker.C:
				if !commitReady(ctx, source, tracker) {
					return
				}
			}
		}
	}()
	for {
		m, err := source.FetchMessage(ctx)
		if err != nil {
			break
		}
		tracker.start(m)
		select {
		case queues[laneFor(m, lanes)] <- m:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}
	}
	for _, queue := range queues {
		close(queue)
	}
	workers.Wait()
	stopCommits()
	<-committed
}

// handle runs the handler with the existing retry and dead-letter policy. It
// returns false only when the context ends before the message is settled.
func (b *Bus) handle(ctx context.Context, topic, group string, h ports.Handler, m kafka.Message) bool {
	var handleErr error
	for attempt := 1; attempt <= 3; attempt++ {
		handleErr = h(ctx, m.Value)
		if handleErr == nil {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(time.Duration(attempt*attempt) * 250 * time.Millisecond):
		}
	}
	dlq := deadLetterPayload(topic, group, handleErr, m.Value)
	return retryUntilSuccess(ctx, 250*time.Millisecond, func() error { return b.Publish(ctx, "iot.dlq."+group, string(m.Key), dlq) }) == nil
}

// commitReady commits every partition whose contiguous finished offset moved.
func commitReady(ctx context.Context, source messageSource, tracker *offsetTracker) bool {
	ready := tracker.ready()
	if len(ready) == 0 {
		return true
	}
	if err := retryUntilSuccess(ctx, 250*time.Millisecond, func() error { return source.CommitMessages(ctx, ready...) }); err != nil {
		return false
	}
	tracker.committed(ready)
	return true
}

func laneFor(m kafka.Message, lanes int) int {
	if lanes == 1 {
		return 0
	}
	if len(m.Key) == 0 {
		return m.Partition % lanes
	}
	hash := fnv.New32a()
	_, _ = hash.Write(m.Key)
	return int(hash.Sum32() % uint32(lanes))
}

// offsetTracker records fetched offsets per partition in fetch order and the
// ones already finished, to find the highest contiguous finished offset.
type offsetTracker struct {
	mu         sync.Mutex
	partitions map[partitionKey]*partitionOffsets
}

type partitionKey struct {
	topic     string
	partition int
}

type partitionOffsets struct {
	pending   []int64
	done      map[int64]bool
	lastDone  kafka.Message
	hasReady  bool
	committed int64
}

func newOffsetTracker() *offsetTracker {
	return &offsetTracker{partitions: map[partitionKey]*partitionOffsets{}}
}

func (t *offsetTracker) start(m kafka.Message) {
	t.mu.Lock()
	defer t.mu.Unlock()
	key := partitionKey{m.Topic, m.Partition}
	state := t.partitions[key]
	if state == nil {
		state = &partitionOffsets{done: map[int64]bool{}, committed: -1}
		t.partitions[key] = state
	}
	state.pending = append(state.pending, m.Offset)
}

func (t *offsetTracker) finish(m kafka.Message) {
	t.mu.Lock()
	defer t.mu.Unlock()
	state := t.partitions[partitionKey{m.Topic, m.Partition}]
	if state == nil {
		return
	}
	state.done[m.Offset] = true
	for len(state.pending) > 0 && state.done[state.pending[0]] {
		offset := state.pending[0]
		delete(state.done, offset)
		state.pending = state.pending[1:]
		state.lastDone = kafka.Message{Topic: m.Topic, Partition: m.Partition, Offset: offset}
		state.hasReady = true
	}
}

// ready returns, per partition, the last message of the contiguous finished
// prefix that has not been committed yet.
func (t *offsetTracker) ready() []kafka.Message {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := []kafka.Message{}
	for _, state := range t.partitions {
		if state.hasReady && state.lastDone.Offset > state.committed {
			out = append(out, state.lastDone)
		}
	}
	return out
}

func (t *offsetTracker) committed(messages []kafka.Message) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, m := range messages {
		if state := t.partitions[partitionKey{m.Topic, m.Partition}]; state != nil && m.Offset > state.committed {
			state.committed = m.Offset
		}
	}
}
