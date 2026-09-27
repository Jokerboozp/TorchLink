package kafkaadapter

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"
)

func TestDeadLetterPayloadIsDecodableForMalformedSource(t *testing.T) {
	for _, payload := range [][]byte{[]byte(`{"device":`), {0xff, 0x00, 0x7d}} {
		body := deadLetterPayload("iot.raw", "archive", errors.New("decode failed"), payload)
		if !json.Valid(body) {
			t.Fatalf("dead letter is not JSON for source %x: %q", payload, body)
		}
		var event map[string]any
		if err := json.Unmarshal(body, &event); err != nil {
			t.Fatal(err)
		}
		if event["sourceTopic"] != "iot.raw" || event["consumerGroup"] != "archive" || event["error"] != "decode failed" {
			t.Fatalf("dead letter metadata was lost: %#v", event)
		}
		encoded, ok := event["payload"].(string)
		if !ok || event["payloadEncoding"] != "base64" {
			t.Fatalf("malformed source must be recoverable as base64: %#v", event)
		}
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || !bytes.Equal(decoded, payload) {
			t.Fatalf("dead letter source changed: %x, %v", decoded, err)
		}
	}
}

func TestDeadLetterPayloadPreservesStructuredSource(t *testing.T) {
	body := deadLetterPayload("iot.raw", "archive", errors.New("decode failed"), []byte(`{"device":"sensor-1"}`))
	var event struct {
		Payload struct {
			Device string `json:"device"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(body, &event); err != nil || event.Payload.Device != "sensor-1" {
		t.Fatalf("structured source changed: %q, %v", body, err)
	}
}

func TestDeadLetterPayloadEscapesMetadata(t *testing.T) {
	body := deadLetterPayload("iot.raw", "archive", errors.New("decode\x00failed"), []byte(`{}`))
	if !json.Valid(body) {
		t.Fatalf("dead letter metadata is not JSON: %q", body)
	}
}

func TestDeadLetterPublishRetriesBeforeAdvancing(t *testing.T) {
	attempts := 0
	err := retryUntilSuccess(context.Background(), time.Millisecond, func() error {
		attempts++
		if attempts == 1 {
			return errors.New("broker temporarily unavailable")
		}
		return nil
	})
	if err != nil || attempts != 2 {
		t.Fatalf("dead letter must publish before next source message: attempts=%d, err=%v", attempts, err)
	}
}

func TestDeadLetterRetryStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	err := retryUntilSuccess(ctx, time.Hour, func() error {
		attempts++
		cancel()
		return errors.New("broker unavailable")
	})
	if !errors.Is(err, context.Canceled) || attempts != 1 {
		t.Fatalf("retry should stop after cancellation: attempts=%d, err=%v", attempts, err)
	}
}

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

// A consumer whose handler stops finishing messages is reported by Health,
// while an idle consumer is not.
func TestHealthReportsStalledConsumer(t *testing.T) {
	now := time.Unix(1000, 0)
	bus := New([]string{"unused:9092"})
	bus.now = func() time.Time { return now }
	bus.startedMessage("g")
	now = now.Add(consumerStallAfter + time.Second)
	bus.mu.Lock()
	p := bus.progress["g"]
	stalled := p.inFlight > 0 && now.Sub(p.lastDone) > consumerStallAfter
	bus.mu.Unlock()
	if !stalled {
		t.Fatal("pending work without progress must count as stalled")
	}
	if err := bus.Health(context.Background()); err == nil || !strings.Contains(err.Error(), "without progress") {
		t.Fatalf("Health must report the stall, got %v", err)
	}
	bus.finishedMessage("g")
	now = now.Add(time.Hour)
	bus.mu.Lock()
	idle := bus.progress["g"].inFlight == 0
	bus.mu.Unlock()
	if !idle {
		t.Fatal("finished work must leave the group idle")
	}
}

// Uses a throwaway topic on the broker in IOT_TEST_DISPOSABLE_KAFKA.
func TestConsumerLagCountsUncommittedMessages(t *testing.T) {
	broker := os.Getenv("IOT_TEST_DISPOSABLE_KAFKA")
	if broker == "" {
		t.Skip("IOT_TEST_DISPOSABLE_KAFKA is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	topic := fmt.Sprintf("lag-test-%d", time.Now().UnixNano())
	brokers := strings.Split(broker, ",")
	conn, err := kafka.DialContext(ctx, "tcp", brokers[0])
	if err != nil {
		t.Fatal(err)
	}
	if err = conn.CreateTopics(kafka.TopicConfig{Topic: topic, NumPartitions: 3, ReplicationFactor: 1}); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	defer func() {
		if c, err := kafka.Dial("tcp", brokers[0]); err == nil {
			_ = c.DeleteTopics(topic)
			_ = c.Close()
		}
	}()

	b := New(brokers)
	defer b.Close()
	for i := range 5 {
		if err = b.Publish(ctx, topic, fmt.Sprintf("key-%d", i), []byte(`{}`)); err != nil {
			t.Fatal(err)
		}
	}
	// A group that has not consumed yet owes every published message.
	b.subscriptions = append(b.subscriptions, subscription{topic: topic, group: "iot-platform-lag-idle"})
	lags, err := b.ConsumerLag(ctx)
	if err != nil || lags["iot-platform-lag-idle"] != 5 {
		t.Fatalf("idle group lag = %v, %v; want 5", lags, err)
	}

	// After the bus consumes and commits, the group has no backlog.
	handled := make(chan struct{}, 5)
	if err = b.Subscribe(ctx, topic, "lag-busy", func(context.Context, []byte) error { handled <- struct{}{}; return nil }); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		select {
		case <-handled:
		case <-ctx.Done():
			t.Fatal("messages were not consumed")
		}
	}
	for {
		lags, err = b.ConsumerLag(ctx)
		if err == nil && lags["iot-platform-lag-busy"] == 0 {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("busy group lag = %v, %v; want 0", lags, err)
		case <-time.After(500 * time.Millisecond):
		}
	}
}
