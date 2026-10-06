package local

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"iot-platform/internal/ports"
)

type Bus struct {
	mu       sync.RWMutex
	handlers map[string][]ports.Handler
	closed   bool
}

func NewBus() *Bus { return &Bus{handlers: map[string][]ports.Handler{}} }
func (b *Bus) Publish(ctx context.Context, topic, key string, payload []byte) error {
	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return fmt.Errorf("bus closed")
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
	defer b.mu.Unlock()
	b.closed = true
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

// NewRealtime records every publication for inspection; tests use it.
// Processes without an MQTT broker use NewDiscardRealtime instead.
func NewRealtime() *Realtime { return &Realtime{} }

// DiscardRealtime drops realtime publications. A process without an MQTT
// broker has nobody to deliver them to, and keeping them would grow memory
// with every device message.
type DiscardRealtime struct{ Dropped atomic.Int64 }

func NewDiscardRealtime() *DiscardRealtime { return &DiscardRealtime{} }
func (r *DiscardRealtime) Publish(context.Context, string, []byte, byte, bool) error {
	r.Dropped.Add(1)
	return nil
}
func (r *DiscardRealtime) Health(context.Context) error { return nil }
func (r *DiscardRealtime) Close() error                 { return nil }
func (r *Realtime) Publish(_ context.Context, topic string, payload []byte, qos byte, retained bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Messages = append(r.Messages, Published{topic, append([]byte(nil), payload...), qos, retained})
	return nil
}

// Snapshot copies the published messages; safe while publishing continues.
func (r *Realtime) Snapshot() []Published {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]Published(nil), r.Messages...)
}
func (r *Realtime) Health(context.Context) error { return nil }
func (r *Realtime) Close() error                 { return nil }
