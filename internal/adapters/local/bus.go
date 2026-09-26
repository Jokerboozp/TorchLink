package local

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"iot-platform/internal/ports"
)

type Bus struct {
	mu       sync.RWMutex
	handlers map[string][]ports.Handler
	wg       sync.WaitGroup
	closed   bool
	async    map[string]chan asyncDelivery
}

type asyncDelivery struct {
	ctx     context.Context
	payload []byte
}

// ErrAsyncQueueFull reports that an asynchronous topic dropped a message.
var ErrAsyncQueueFull = errors.New("local bus asynchronous queue is full")

// SetAsyncTopic makes the topic's handlers run on background workers instead
// of inside Publish, like a separate Kafka consumer group. Slow handlers such
// as AI analysis then no longer hold up the publisher. Without Kafka there is
// no durable backlog: when queued messages reach queueSize, Publish drops the
// message and returns ErrAsyncQueueFull. Call it before publishing.
func (b *Bus) SetAsyncTopic(topic string, workers, queueSize int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.async == nil {
		b.async = map[string]chan asyncDelivery{}
	}
	if _, exists := b.async[topic]; exists || b.closed {
		return
	}
	queue := make(chan asyncDelivery, max(1, queueSize))
	b.async[topic] = queue
	for i := 0; i < max(1, workers); i++ {
		b.wg.Add(1)
		go func() {
			defer b.wg.Done()
			for delivery := range queue {
				b.mu.RLock()
				hs := append([]ports.Handler(nil), b.handlers[topic]...)
				b.mu.RUnlock()
				for _, h := range hs {
					_ = h(delivery.ctx, append([]byte(nil), delivery.payload...))
				}
			}
		}()
	}
}

func NewBus() *Bus { return &Bus{handlers: map[string][]ports.Handler{}} }
func (b *Bus) Publish(ctx context.Context, topic, key string, payload []byte) error {
	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return fmt.Errorf("bus closed")
	}
	if queue, ok := b.async[topic]; ok {
		defer b.mu.RUnlock()
		select {
		case queue <- asyncDelivery{ctx: context.WithoutCancel(ctx), payload: append([]byte(nil), payload...)}:
			return nil
		default:
			return fmt.Errorf("topic %s key %s: %w", topic, key, ErrAsyncQueueFull)
		}
	}
	hs := append([]ports.Handler(nil), b.handlers[topic]...)
	b.mu.RUnlock()
	for _, h := range hs {
		if err := h(ctx, append([]byte(nil), payload...)); err != nil {
			return fmt.Errorf("topic %s key %s: %w", topic, key, err)
		}
	}
	return nil
}
func (b *Bus) Subscribe(_ context.Context, topic, group string, h ports.Handler) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return fmt.Errorf("bus closed")
	}
	b.handlers[topic] = append(b.handlers[topic], h)
	return nil
}
func (b *Bus) Health(context.Context) error { return nil }
func (b *Bus) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	for _, queue := range b.async {
		close(queue)
	}
	b.mu.Unlock()
	// Workers finish the queued messages; they take the read lock per message.
	b.wg.Wait()
	return nil
}

type Realtime struct {
	mu       sync.RWMutex
	Messages []Published
}
type Published struct {
	Topic    string
	Payload  []byte
	QoS      byte
	Retained bool
}

func NewRealtime() *Realtime { return &Realtime{} }
func (r *Realtime) Publish(_ context.Context, topic string, payload []byte, qos byte, retained bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Messages = append(r.Messages, Published{topic, append([]byte(nil), payload...), qos, retained})
	return nil
}
func (r *Realtime) Health(context.Context) error { return nil }
func (r *Realtime) Close() error                 { return nil }
