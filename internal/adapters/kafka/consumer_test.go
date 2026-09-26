package kafkaadapter

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"
)

type fakeSource struct {
	mu        sync.Mutex
	messages  []kafka.Message
	next      int
	commits   []kafka.Message
	exhausted chan struct{}
}

func (f *fakeSource) FetchMessage(ctx context.Context) (kafka.Message, error) {
	f.mu.Lock()
	if f.next < len(f.messages) {
		m := f.messages[f.next]
		f.next++
		f.mu.Unlock()
		return m, nil
	}
	f.mu.Unlock()
	select {
	case f.exhausted <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return kafka.Message{}, ctx.Err()
}

func (f *fakeSource) CommitMessages(_ context.Context, messages ...kafka.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commits = append(f.commits, messages...)
	return nil
}

func (f *fakeSource) Close() error { return nil }

func (f *fakeSource) lastCommit(partition int) int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	last := int64(-1)
	for _, m := range f.commits {
		if m.Partition == partition {
			if m.Offset < last {
				panic(fmt.Sprintf("commit went backwards: %d after %d", m.Offset, last))
			}
			last = m.Offset
		}
	}
	return last
}

func messages(keys []string) []kafka.Message {
	out := make([]kafka.Message, 0, len(keys))
	for i, key := range keys {
		out = append(out, kafka.Message{Topic: "t", Partition: 0, Offset: int64(i), Key: []byte(key), Value: []byte(fmt.Sprintf("%s:%d", key, i))})
	}
	return out
}

// Messages of one key keep their order while different keys run in parallel,
// and every offset is committed once all earlier ones finished.
func TestConsumerKeepsPerKeyOrderAndRunsKeysInParallel(t *testing.T) {
	keys := []string{}
	for i := 0; i < 200; i++ {
		keys = append(keys, fmt.Sprintf("device-%d", i%10))
	}
	source := &fakeSource{messages: messages(keys), exhausted: make(chan struct{}, 1)}
	var mu sync.Mutex
	seen := map[string][]string{}
	var active, peak int32
	handler := func(_ context.Context, payload []byte) error {
		now := atomic.AddInt32(&active, 1)
		for {
			old := atomic.LoadInt32(&peak)
			if now <= old || atomic.CompareAndSwapInt32(&peak, old, now) {
				break
			}
		}
		time.Sleep(time.Millisecond)
		atomic.AddInt32(&active, -1)
		key, _, _ := strings.Cut(string(payload), ":")
		mu.Lock()
		seen[key] = append(seen[key], string(payload))
		mu.Unlock()
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	bus := New(nil)
	go func() { bus.consume(ctx, source, "t", "g", 4, handler); close(done) }()
	<-source.exhausted
	deadline := time.Now().Add(3 * time.Second)
	for source.lastCommit(0) != 199 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	if got := source.lastCommit(0); got != 199 {
		t.Fatalf("all offsets must be committed, last=%d", got)
	}
	if atomic.LoadInt32(&peak) < 2 {
		t.Fatalf("different keys must run in parallel, peak=%d", peak)
	}
	for key, payloads := range seen {
		for i := 1; i < len(payloads); i++ {
			_, previous, _ := strings.Cut(payloads[i-1], ":")
			_, current, _ := strings.Cut(payloads[i], ":")
			p, _ := strconv.Atoi(previous)
			c, _ := strconv.Atoi(current)
			if c <= p {
				t.Fatalf("key %s processed out of order: %v", key, payloads)
			}
		}
	}
}

// A slow message holds back the commit of later offsets in its partition even
// when those later messages finished first.
func TestConsumerCommitsOnlyContiguousFinishedOffsets(t *testing.T) {
	source := &fakeSource{messages: messages([]string{"slow", "a", "b", "c"}), exhausted: make(chan struct{}, 1)}
	release := make(chan struct{})
	handler := func(_ context.Context, payload []byte) error {
		if strings.HasPrefix(string(payload), "slow") {
			<-release
		}
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	bus := New(nil)
	go func() { bus.consume(ctx, source, "t", "g", 4, handler); close(done) }()
	<-source.exhausted
	time.Sleep(3 * commitInterval)
	if got := source.lastCommit(0); got != -1 {
		t.Fatalf("offsets after an unfinished message must not be committed, got %d", got)
	}
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for source.lastCommit(0) != 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	if got := source.lastCommit(0); got != 3 {
		t.Fatalf("finished prefix must be committed, got %d", got)
	}
}

func TestOffsetTrackerTracksPartitionsIndependently(t *testing.T) {
	tracker := newOffsetTracker()
	a0 := kafka.Message{Topic: "t", Partition: 0, Offset: 5}
	a1 := kafka.Message{Topic: "t", Partition: 0, Offset: 6}
	b0 := kafka.Message{Topic: "t", Partition: 1, Offset: 9}
	for _, m := range []kafka.Message{a0, a1, b0} {
		tracker.start(m)
	}
	tracker.finish(a1)
	tracker.finish(b0)
	ready := tracker.ready()
	if len(ready) != 1 || ready[0].Partition != 1 || ready[0].Offset != 9 {
		t.Fatalf("only partition 1 is contiguous: %v", ready)
	}
	tracker.committed(ready)
	tracker.finish(a0)
	ready = tracker.ready()
	if len(ready) != 1 || ready[0].Offset != 6 {
		t.Fatalf("partition 0 must advance to offset 6: %v", ready)
	}
}

// brokenSource fails its first fetch as a lost broker connection would.
type brokenSource struct{ fakeSource }

func (b *brokenSource) FetchMessage(context.Context) (kafka.Message, error) {
	return kafka.Message{}, errors.New("dial tcp: connect: cannot assign requested address")
}

// A reader whose fetch fails is replaced instead of leaving the topic
// unconsumed; Health reports the failure until the new reader fetches.
func TestSupervisorReplacesFailedReader(t *testing.T) {
	healthy := &fakeSource{messages: messages([]string{"a", "b"}), exhausted: make(chan struct{}, 1)}
	var created atomic.Int32
	bus := New([]string{"unused:9092"})
	bus.newSource = func(string, string) messageSource {
		if created.Add(1) == 1 {
			return &brokenSource{}
		}
		return healthy
	}
	var handled atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		bus.supervise(ctx, "t", "g", 2, func(context.Context, []byte) error { handled.Add(1); return nil })
		close(done)
	}()
	failure := func() error {
		bus.mu.Lock()
		defer bus.mu.Unlock()
		return bus.consumerErrors["g"]
	}
	deadline := time.Now().Add(time.Second / 2)
	for failure() == nil && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if failure() == nil {
		t.Fatal("a failed reader must be reported")
	}
	select {
	case <-healthy.exhausted:
	case <-time.After(5 * time.Second):
		t.Fatal("the failed reader was not replaced")
	}
	deadline = time.Now().Add(5 * time.Second)
	for handled.Load() != 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if handled.Load() != 2 || failure() != nil {
		t.Fatalf("replacement must consume and clear the failure: handled=%d failure=%v", handled.Load(), failure())
	}
	cancel()
	<-done
	if created.Load() != 2 {
		t.Fatalf("readers created: %d", created.Load())
	}
}
