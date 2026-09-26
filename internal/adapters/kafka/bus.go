package kafkaadapter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
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
	readers []*kafka.Reader
	cancel  context.CancelFunc
	// lanes and topicLanes bound the parallel handlers of each subscription.
	lanes      int
	topicLanes map[string]int
	// subscriptions lists the consumer groups whose backlog ConsumerLag reports.
	subscriptions []subscription
}

func New(brokers []string) *Bus { return &Bus{brokers: brokers, writers: map[string]*kafka.Writer{}} }
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
	reader := kafka.NewReader(kafka.ReaderConfig{Brokers: b.brokers, Topic: topic, GroupID: "iot-platform-" + group, MinBytes: 1, MaxBytes: 10e6, CommitInterval: 0})
	b.mu.Lock()
	b.readers = append(b.readers, reader)
	b.subscriptions = append(b.subscriptions, subscription{topic: topic, group: "iot-platform-" + group})
	lanes := b.lanesFor(topic)
	b.mu.Unlock()
	go b.consume(ctx, reader, topic, group, lanes, h)
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
	var errs []string
	for _, r := range b.readers {
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
