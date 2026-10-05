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
	if err := token.Error(); err != nil {
		return err
	}
	c.routeMu.RLock()
	c.subscribed.Store(int64(len(c.filters)))
	c.routeMu.RUnlock()
	return nil
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
		c.subscribed.Store(0)
		c.retryConnection()
		return
	}
	c.subscribed.Store(int64(len(filters)))
}
func (c *Client) receive(m mqtt.Message) {
	if c.ctx.Err() != nil {
		return
	}
	reason := ""
	switch {
	case route(m.Topic()) == "":
		reason = "unknown_topic"
	case len(m.Payload()) > 128<<10:
		reason = "payload_too_large"
	case len(m.Payload()) == 0 && applyStateTopicIdentity(m.Topic(), &model.DeviceState{}) == nil:
		// An empty retained publication (capacity cleanup) clears the state
		// snapshot; live subscribers receive it too. It carries no state, so
		// it is neither ingested nor quarantined, whose identical repeats
		// would otherwise fail every later receipt and readiness.
		c.logger().Debug("MQTT device state clear ignored", "topic", m.Topic())
		m.Ack()
		return
	case m.Retained():
		if applyStateTopicIdentity(m.Topic(), &model.DeviceState{}) == nil {
			// The platform publishes retained state for realtime subscribers.
			// Resubscription replays these snapshots; never ingest them as new
			// device reports or warn once per historical device at startup.
			c.logger().Debug("MQTT retained device state snapshot ignored", "topic", m.Topic())
			m.Ack()
			return
		}
		reason = "retained"
	}
	if reason != "" {
		c.logger().Warn("MQTT ingress rejected", "topic", m.Topic(), "reason", reason)
		m.Ack()
		return
	}
	if c.inbox != nil {
		// Paho v1's ACK closure belongs to this delivery callback's connection.
		// Keep the callback alive through fsync + ACK: after it returns the
		// router may close its ACK channel during disconnect/reconnect.
		// Business processing still runs on the durable queue's parallel lanes.
		c.persist(m)
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

// persist acknowledges a delivery only after it is durably in the inbox.
func (c *Client) persist(m mqtt.Message) {
	if err := c.inbox.put(m.Topic(), m.Payload()); err != nil {
		c.logger().Error("MQTT durable receive failed; no acknowledgement", "topic", m.Topic(), "error", err)
		c.retryConnection()
		return
	}
	m.Ack()
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
