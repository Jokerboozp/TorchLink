package mqttadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"iot-platform/internal/model"
)

type Client struct {
	conn         net.Conn
	connMu       sync.Mutex
	inbox        *durableInbox
	ctx          context.Context
	cancel       context.CancelFunc
	routeMu      sync.RWMutex
	routes       map[string]ingressHandler
	filters      map[string]byte
	reconnecting atomic.Bool
	sharedGroup  string
	broker       string
	credentials  mqtt.CredentialsProvider
	client       mqtt.Client
	log          *slog.Logger
	jobs         chan func()
	stop         chan struct{}
	stopOnce     sync.Once
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
	return newClient(broker, clientID, credentials, nil)
}
func newClient(broker, clientID string, credentials mqtt.CredentialsProvider, inbox *durableInbox) (*Client, error) {
	ctx, cancel := context.WithCancel(context.Background())
	adapter := &Client{log: slog.Default(), broker: broker, credentials: credentials, inbox: inbox, ctx: ctx, cancel: cancel, routes: map[string]ingressHandler{}, filters: map[string]byte{}}
	if inbox == nil {
		adapter.startDispatch()
	} else {
		adapter.stop = make(chan struct{})
	}
	opts := mqtt.NewClientOptions().AddBroker(broker).SetClientID(clientID).SetCredentialsProvider(credentials).SetConnectRetry(false).SetAutoReconnect(true).SetMaxReconnectInterval(5 * time.Second).SetOrderMatters(true).SetCleanSession(inbox == nil).SetAutoAckDisabled(inbox != nil)
	opts.SetCustomOpenConnectionFn(adapter.openConnection)
	opts.SetDefaultPublishHandler(func(_ mqtt.Client, m mqtt.Message) { adapter.receive(m) })
	opts.SetOnConnectHandler(func(client mqtt.Client) { adapter.resubscribe(client) })
	adapter.client = mqtt.NewClient(opts)
	token := adapter.client.Connect()
	if !token.WaitTimeout(10 * time.Second) {
		adapter.Close()
		return nil, fmt.Errorf("mqtt connect timeout")
	}
	if token.Error() != nil {
		adapter.Close()
		return nil, token.Error()
	}
	if inbox != nil {
		inbox.start(adapter)
	}
	return adapter, nil
}
func (c *Client) Publish(ctx context.Context, topic string, payload []byte, qos byte, retained bool) error {
	token := c.client.Publish(topic, qos, retained, payload)
	done := token.Done()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return token.Error()
	}
}
func (c *Client) SubscribeRaw(handler func(context.Context, model.RawMessage) error) error {
	return c.register("raw", []string{"/external/raw/#", "/jetlinks/raw/#"}, func(ctx context.Context, topic string, payload []byte) error {
		var raw model.RawMessage
		if err := json.Unmarshal(payload, &raw); err != nil {
			return Reject(err)
		}
		if err := applyRawTopicIdentity(topic, &raw); err != nil {
			return Reject(err)
		}
		if raw.Source == "" {
			raw.Source = "external-mqtt"
		}
		if err := raw.Validate(); err != nil {
			return Reject(err)
		}
		raw.ReceivedAt = ReceivedAt(ctx)
		return handler(ctx, raw)
	})
}
func (c *Client) SubscribeDeviceState(handler func(context.Context, model.DeviceState) error) error {
	return c.register("state", []string{"/iot/device/state/#"}, func(ctx context.Context, topic string, payload []byte) error {
		var state model.DeviceState
		if err := json.Unmarshal(payload, &state); err != nil {
			return Reject(err)
		}
		if err := applyStateTopicIdentity(topic, &state); err != nil {
			return Reject(err)
		}
		return handler(ctx, state)
	})
}
func (c *Client) SubscribeVideo(handler func(context.Context, model.VideoAlarmEvent) error) error {
	return c.register("video", []string{"/external/video/alarm/#"}, func(ctx context.Context, topic string, payload []byte) error {
		var v model.VideoAlarmEvent
		if err := json.Unmarshal(payload, &v); err != nil {
			return Reject(err)
		}
		if err := applyVideoTopicIdentity(topic, &v); err != nil {
			return Reject(err)
		}
		if v.EventID == "" {
			return Reject(fmt.Errorf("video eventId is required for reliable MQTT receive"))
		}
		return handler(ctx, v)
	})
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
	if c.inbox != nil {
		if err := c.inbox.health(); err != nil {
			return err
		}
	}
	if !c.client.IsConnectionOpen() {
		return fmt.Errorf("mqtt disconnected")
	}
	return nil
}
func (c *Client) Close() error {
	c.stopOnce.Do(func() {
		if c.cancel != nil {
			c.cancel()
		}
		if c.stop != nil {
			close(c.stop)
		}
		if c.client != nil {
			c.client.Disconnect(250)
		}
		if c.inbox != nil {
			c.inbox.close()
		}
	})
	return nil
}

// Legacy embedded constructor only. Production uses the durable inbox and its
// fixed workers; neither path starts an application goroutine per message.
func (c *Client) startDispatch() {
	c.jobs = make(chan func(), 128)
	c.stop = make(chan struct{})
	for i := 0; i < 8; i++ {
		go func() {
			for {
				select {
				case <-c.stop:
					return
				case job := <-c.jobs:
					job()
				}
			}
		}()
	}
}
func (c *Client) enqueue(topic string, job func()) bool {
	select {
	case <-c.stop:
		return false
	default:
	}
	select {
	case c.jobs <- job:
		return true
	default:
		c.logger().Error("MQTT ingress busy: leaving delivery unacknowledged for session redelivery", "topic", topic)
		return false
	}
}

// ConfigureSharedSubscriptions is called before registering subscriptions.
func (c *Client) ConfigureSharedSubscriptions(group string) error {
	if group == "" || len(group) > 64 || strings.ContainsAny(group, "/+#\x00") {
		return fmt.Errorf("invalid MQTT shared subscription group")
	}
	c.sharedGroup = group
	return nil
}
func (c *Client) subscription(topic string) string {
	if c.sharedGroup == "" {
		return topic
	}
	return "$share/" + c.sharedGroup + "/" + topic
}
