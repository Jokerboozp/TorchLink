package mqttadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"iot-platform/internal/model"
)

type Client struct {
	broker      string
	credentials mqtt.CredentialsProvider
	client      mqtt.Client
	log         *slog.Logger
}

func (c *Client) logger() *slog.Logger {
	if c.log != nil {
		return c.log
	}
	return slog.Default()
}

func New(broker, user, password, clientID string) (*Client, error) {
	return NewWithCredentials(broker, clientID, func() (string, string) { return user, password })
}

func NewWithCredentials(broker, clientID string, credentials mqtt.CredentialsProvider) (*Client, error) {
	opts := mqtt.NewClientOptions().AddBroker(broker).SetClientID(clientID).SetCredentialsProvider(credentials).SetConnectRetry(true).SetConnectRetryInterval(3 * time.Second).SetAutoReconnect(true).SetOrderMatters(false)
	c := mqtt.NewClient(opts)
	token := c.Connect()
	if !token.WaitTimeout(10 * time.Second) {
		return nil, fmt.Errorf("mqtt connect timeout")
	}
	if token.Error() != nil {
		return nil, token.Error()
	}
	return &Client{client: c, log: slog.Default(), broker: broker, credentials: credentials}, nil
}
func (c *Client) Publish(ctx context.Context, topic string, payload []byte, qos byte, retained bool) error {
	token := c.client.Publish(topic, qos, retained, payload)
	done := make(chan struct{})
	go func() { token.Wait(); close(done) }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return token.Error()
	}
}
func (c *Client) SubscribeRaw(handler func(context.Context, model.RawMessage) error) error {
	token := c.client.SubscribeMultiple(map[string]byte{"/external/raw/#": 1, "/jetlinks/raw/#": 1}, func(_ mqtt.Client, m mqtt.Message) {
		var raw model.RawMessage
		if err := json.Unmarshal(m.Payload(), &raw); err != nil {
			c.logger().Warn("mqtt raw payload rejected", "topic", m.Topic(), "error", err)
			return
		}
		if err := applyRawTopicIdentity(m.Topic(), &raw); err != nil {
			c.logger().Warn("mqtt raw topic rejected", "topic", m.Topic(), "error", err)
			return
		}
		if raw.Source == "" {
			raw.Source = "external-mqtt"
		}
		go c.dispatchRaw(handler, m.Topic(), raw)
	})
	token.Wait()
	return token.Error()
}
func (c *Client) SubscribeDeviceState(handler func(context.Context, model.DeviceState) error) error {
	token := c.client.Subscribe("/iot/device/state/#", 1, func(_ mqtt.Client, m mqtt.Message) {
		var state model.DeviceState
		if err := json.Unmarshal(m.Payload(), &state); err != nil {
			c.logger().Warn("mqtt device state payload rejected", "topic", m.Topic(), "error", err)
			return
		}
		if err := applyStateTopicIdentity(m.Topic(), &state); err != nil {
			c.logger().Warn("mqtt device state topic rejected", "topic", m.Topic(), "error", err)
			return
		}
		go c.dispatchState(handler, m.Topic(), state)
	})
	token.Wait()
	return token.Error()
}
func (c *Client) SubscribeVideo(handler func(context.Context, model.VideoAlarmEvent) error) error {
	token := c.client.Subscribe("/external/video/alarm/#", 1, func(_ mqtt.Client, m mqtt.Message) {
		var v model.VideoAlarmEvent
		if err := json.Unmarshal(m.Payload(), &v); err != nil {
			c.logger().Warn("mqtt video payload rejected", "topic", m.Topic(), "error", err)
			return
		}
		if err := applyVideoTopicIdentity(m.Topic(), &v); err != nil {
			c.logger().Warn("mqtt video topic rejected", "topic", m.Topic(), "error", err)
			return
		}
		go c.dispatchVideo(handler, m.Topic(), v)
	})
	token.Wait()
	return token.Error()
}
func (c *Client) dispatchRaw(handler func(context.Context, model.RawMessage) error, topic string, raw model.RawMessage) {
	if err := handler(context.Background(), raw); err != nil {
		c.logger().Error("mqtt raw handler failed", "topic", topic, "messageId", raw.MessageID, "error", err)
	}
}
func (c *Client) dispatchState(handler func(context.Context, model.DeviceState) error, topic string, state model.DeviceState) {
	if err := handler(context.Background(), state); err != nil {
		c.logger().Error("mqtt device state handler failed", "topic", topic, "deviceId", state.DeviceID, "error", err)
	}
}
func (c *Client) dispatchVideo(handler func(context.Context, model.VideoAlarmEvent) error, topic string, v model.VideoAlarmEvent) {
	if err := handler(context.Background(), v); err != nil {
		c.logger().Error("mqtt video handler failed", "topic", topic, "eventId", v.EventID, "error", err)
	}
}
func topicParts(topic string) []string {
	return strings.Split(strings.Trim(topic, "/"), "/")
}
func applyRawTopicIdentity(topic string, raw *model.RawMessage) error {
	parts := topicParts(topic)
	if len(parts) != 5 || (parts[0] != "external" && parts[0] != "jetlinks") || parts[1] != "raw" || parts[2] == "" || parts[3] == "" || parts[4] == "" {
		return fmt.Errorf("expected /external/raw/{tenant}/{product}/{device} or /jetlinks/raw/{tenant}/{product}/{device}")
	}
	raw.TenantID, raw.ProductID, raw.DeviceID = parts[2], parts[3], parts[4]
	return nil
}
func applyStateTopicIdentity(topic string, state *model.DeviceState) error {
	parts := topicParts(topic)
	if len(parts) != 6 || parts[0] != "iot" || parts[1] != "device" || parts[2] != "state" || parts[3] == "" || parts[4] == "" || parts[5] == "" {
		return fmt.Errorf("expected /iot/device/state/{tenant}/{product}/{device}")
	}
	state.TenantID, state.ProductID, state.DeviceID = parts[3], parts[4], parts[5]
	return nil
}
func applyVideoTopicIdentity(topic string, v *model.VideoAlarmEvent) error {
	parts := topicParts(topic)
	if len(parts) != 5 || parts[0] != "external" || parts[1] != "video" || parts[2] != "alarm" || parts[3] == "" || parts[4] == "" {
		return fmt.Errorf("expected /external/video/alarm/{tenant}/{camera}")
	}
	v.TenantID, v.CameraID = parts[3], parts[4]
	return nil
}
func (c *Client) Health(context.Context) error {
	if !c.client.IsConnected() {
		return fmt.Errorf("mqtt disconnected")
	}
	return nil
}
func (c *Client) Close() error { c.client.Disconnect(250); return nil }
