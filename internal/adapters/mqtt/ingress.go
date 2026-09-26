package mqttadapter

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"iot-platform/internal/model"
	"strings"
	"time"
)

type ingressHandler func(context.Context, string, []byte) error
type rejected struct{ error }

// Reject explicitly marks a permanent validation/authorization failure. Unknown
// errors (including storage/network failures) remain retryable by default.
func Reject(err error) error {
	if err == nil {
		return nil
	}
	return rejected{err}
}
func permanent(err error) bool {
	var r rejected
	return errors.As(err, &r) || errors.Is(err, model.ErrRawConflict)
}
func ingressID(topic string, payload []byte) string {
	return fmt.Sprintf("mqtt_%x", sha256.Sum256(append([]byte(topic+"\x00"), payload...)))
}
func route(topic string) string {
	switch {
	case strings.HasPrefix(topic, "/iot/up/"):
		return "standard"
	case strings.HasPrefix(topic, "/external/raw/"), strings.HasPrefix(topic, "/jetlinks/raw/"):
		return "raw"
	case strings.HasPrefix(topic, "/iot/device/state/"):
		return "state"
	case strings.HasPrefix(topic, "/external/video/alarm/"):
		return "video"
	default:
		return ""
	}
}
func (c *Client) register(kind string, topics []string, handler ingressHandler) error {
	c.routeMu.Lock()
	c.routes[kind] = handler
	filters := map[string]byte{}
	for _, topic := range topics {
		filter := c.subscription(topic)
		c.filters[filter] = 1
		filters[filter] = 1
	}
	c.routeMu.Unlock()
	token := c.client.SubscribeMultiple(filters, func(_ mqtt.Client, m mqtt.Message) { c.receive(m) })
	if !token.WaitTimeout(10 * time.Second) {
		return errors.New("MQTT subscription timeout")
	}
	return token.Error()
}
func (c *Client) resubscribe(client mqtt.Client) {
	c.routeMu.RLock()
	filters := map[string]byte{}
	for k, v := range c.filters {
		filters[k] = v
	}
	c.routeMu.RUnlock()
	if len(filters) == 0 {
		return
	}
	token := client.SubscribeMultiple(filters, func(_ mqtt.Client, m mqtt.Message) { c.receive(m) })
	if !token.WaitTimeout(10*time.Second) || token.Error() != nil {
		c.logger().Error("MQTT resubscription failed")
		c.retryConnection()
	}
}
func (c *Client) receive(m mqtt.Message) {
	if c.ctx.Err() != nil {
		return
	}
	if m.Retained() || route(m.Topic()) == "" || len(m.Payload()) > 128<<10 {
		c.logger().Warn("MQTT ingress rejected: retained, unknown topic or oversized payload", "topic", m.Topic())
		m.Ack()
		return
	}
	if c.inbox != nil {
		if err := c.inbox.put(m.Topic(), m.Payload()); err != nil {
			c.logger().Error("MQTT durable receive failed; no acknowledgement", "topic", m.Topic(), "error", err)
			c.retryConnection()
			return
		}
		m.Ack()
		return
	}
	// Preserve the legacy embedded/test constructor semantics. Production uses
	// the durable constructor above; no asynchronous ACK is sent to a closed
	// Paho session from these background workers.
	topic, payload, at := m.Topic(), append([]byte(nil), m.Payload()...), time.Now().UnixMilli()
	c.enqueue(topic, func() {
		if err := c.handle(topic, payload, at); err != nil {
			c.logger().Warn("non-durable MQTT handler failed", "topic", topic, "error", err)
		}
	})
}

type receptionKey struct{}

// ReceivedAt is the first durable transport reception, independent of a later
// retry's processing time. It is set only by the MQTT adapter.
func ReceivedAt(ctx context.Context) int64 { v, _ := ctx.Value(receptionKey{}).(int64); return v }
func (c *Client) handle(topic string, payload []byte, receivedAt int64) error {
	c.routeMu.RLock()
	handler := c.routes[route(topic)]
	c.routeMu.RUnlock()
	if handler == nil {
		return errors.New("MQTT ingress handler not registered yet")
	}
	ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
	defer cancel()
	return handler(context.WithValue(ctx, receptionKey{}, receivedAt), topic, payload)
}
func (c *Client) pause(d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-c.ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
func (c *Client) retryConnection() {
	if !c.reconnecting.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer c.reconnecting.Store(false)
		if !c.pause(time.Second) {
			return
		}
		// Expire the read to enter Paho's normal connection-loss path.
		// Paho suppresses a local Close error; Disconnect()+Connect() can
		// overlap cleanup. A read timeout triggers its reconnect state machine.
		c.connMu.Lock()
		if c.conn != nil {
			_ = c.conn.SetReadDeadline(time.Now())
		}
		c.connMu.Unlock()
	}()
}
