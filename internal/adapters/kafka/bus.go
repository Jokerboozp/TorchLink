package kafkaadapter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/segmentio/kafka-go"
	"iot-platform/internal/ports"
)

type Bus struct {
	brokers []string
	mu      sync.Mutex
	writers map[string]*kafka.Writer
	// readers holds the live reader of each running subscription.
	readers map[messageSource]struct{}
	closed  bool
	// consumerErrors records subscriptions whose reader failed and is being
	// replaced; Health reports them until they fetch again.
	consumerErrors map[string]error
	log            *slog.Logger
	// newSource creates a subscription reader; tests replace it.
	newSource func(topic, group string) messageSource
	// progress tracks, per consumer group, messages in flight and the last
	// completion, so Health can report a consumer that stopped finishing work.
	progress map[string]*groupProgress
	now      func() time.Time
	// lanes and topicLanes bound the parallel handlers of each subscription.
	lanes      int
	topicLanes map[string]int
	// subscriptions lists the consumer groups whose backlog ConsumerLag reports.
	subscriptions []subscription
}

func New(brokers []string) *Bus {
	b := &Bus{brokers: brokers, writers: map[string]*kafka.Writer{}, readers: map[messageSource]struct{}{}, consumerErrors: map[string]error{}, progress: map[string]*groupProgress{}, now: time.Now}
	b.newSource = func(topic, group string) messageSource {
		return kafka.NewReader(kafka.ReaderConfig{Brokers: b.brokers, Topic: topic, GroupID: "iot-platform-" + group, MinBytes: 1, MaxBytes: 10e6, CommitInterval: 0})
	}
	return b
}

// SetLogger sets the logger for consumer failures; slog.Default is used otherwise.
func (b *Bus) SetLogger(log *slog.Logger) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.log = log
}

func (b *Bus) logger() *slog.Logger {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.log != nil {
		return b.log
	}
	return slog.Default()
}

func (b *Bus) trackReader(source messageSource, live bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if live {
		b.readers[source] = struct{}{}
	} else {
		delete(b.readers, source)
	}
}

func (b *Bus) setConsumerError(group string, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err == nil {
		delete(b.consumerErrors, group)
		return
	}
	if !b.closed {
		b.consumerErrors[group] = err
	}
}

// consumerStallAfter is how long fetched messages may wait without any of the
// group's messages finishing before Health reports the group as stalled.
const consumerStallAfter = 2 * time.Minute

type groupProgress struct {
	inFlight int64
	lastDone time.Time
}

func (b *Bus) startedMessage(group string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	p := b.progress[group]
	if p == nil {
		p = &groupProgress{lastDone: b.now()}
		b.progress[group] = p
	}
	if p.inFlight == 0 {
		// Idle time before this message is not a stall.
		p.lastDone = b.now()
	}
	p.inFlight++
}

func (b *Bus) finishedMessage(group string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if p := b.progress[group]; p != nil {
		p.inFlight--
		p.lastDone = b.now()
	}
}

func (b *Bus) isClosed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closed
}
func (b *Bus) writer(topic string) *kafka.Writer {
	b.mu.Lock()
	defer b.mu.Unlock()
	if w := b.writers[topic]; w != nil {
		return w
	}
	w := &kafka.Writer{Addr: kafka.TCP(b.brokers...), Topic: topic, Balancer: &kafka.Hash{}, RequiredAcks: kafka.RequireAll, Async: false, AllowAutoTopicCreation: true, BatchSize: 500, BatchBytes: 4 << 20, BatchTimeout: 10 * time.Millisecond}
	b.writers[topic] = w
	return w
}
func (b *Bus) Publish(ctx context.Context, topic, key string, payload []byte) error {
	return b.writer(topic).WriteMessages(ctx, kafka.Message{Key: []byte(key), Value: payload})
}
func deadLetterPayload(topic, group string, cause error, payload []byte) []byte {
	message := struct {
		SourceTopic     string          `json:"sourceTopic"`
		ConsumerGroup   string          `json:"consumerGroup"`
		RetryCount      int             `json:"retryCount"`
		Error           string          `json:"error"`
		Payload         json.RawMessage `json:"payload"`
		PayloadEncoding string          `json:"payloadEncoding,omitempty"`
	}{SourceTopic: topic, ConsumerGroup: group, RetryCount: 3, Error: cause.Error()}
	if json.Valid(payload) {
		message.Payload = json.RawMessage(payload)
	} else {
		message.Payload, _ = json.Marshal(base64.StdEncoding.EncodeToString(payload))
		message.PayloadEncoding = "base64"
	}
	body, _ := json.Marshal(message)
	return body
}
func retryUntilSuccess(ctx context.Context, delay time.Duration, operation func() error) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := operation(); err == nil {
			return nil
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
func (b *Bus) Subscribe(ctx context.Context, topic, group string, h ports.Handler) error {
	b.mu.Lock()
	b.subscriptions = append(b.subscriptions, subscription{topic: topic, group: "iot-platform-" + group})
	lanes := b.lanesFor(topic)
	b.mu.Unlock()
	go b.supervise(ctx, topic, group, lanes, h)
	return nil
}

// SetConsumerConcurrency sets the parallel lanes per subscription; perTopic
// overrides the default for specific topics (for example alarm analysis).
func (b *Bus) SetConsumerConcurrency(defaultLanes int, perTopic map[string]int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lanes = defaultLanes
	b.topicLanes = map[string]int{}
	for topic, lanes := range perTopic {
		b.topicLanes[topic] = lanes
	}
}

func (b *Bus) lanesFor(topic string) int {
	if lanes, ok := b.topicLanes[topic]; ok && lanes > 0 {
		return lanes
	}
	if b.lanes > 0 {
		return b.lanes
	}
	return 1
}
func (b *Bus) Health(ctx context.Context) error {
	if len(b.brokers) == 0 {
		return fmt.Errorf("no kafka brokers")
	}
	b.mu.Lock()
	failed := make([]string, 0, len(b.consumerErrors))
	for group, err := range b.consumerErrors {
		failed = append(failed, group+": "+err.Error())
	}
	now := b.now()
	for group, p := range b.progress {
		if p.inFlight > 0 && now.Sub(p.lastDone) > consumerStallAfter {
			failed = append(failed, fmt.Sprintf("%s: %d messages without progress for %s", group, p.inFlight, now.Sub(p.lastDone).Round(time.Second)))
		}
	}
	b.mu.Unlock()
	if len(failed) > 0 {
		sort.Strings(failed)
		return fmt.Errorf("kafka consumer unhealthy: %s", strings.Join(failed, "; "))
	}
	conn, err := kafka.DialContext(ctx, "tcp", b.brokers[0])
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.Brokers()
	return err
}
func (b *Bus) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	var errs []string
	for r := range b.readers {
		if err := r.Close(); err != nil {
			errs = append(errs, err.Error())
		}
	}
	for _, w := range b.writers {
		if err := w.Close(); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}
