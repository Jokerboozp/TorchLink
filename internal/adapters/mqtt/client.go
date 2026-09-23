package mqttadapter /* 声明 mqttadapter 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"       /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */
	"log/slog"      /* 执行当前语句并推进处理流程。 */
	"net"           /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */
	"sync"          /* 执行当前语句并推进处理流程。 */
	"sync/atomic"   /* 执行当前语句并推进处理流程。 */
	"time"          /* 执行当前语句并推进处理流程。 */

	mqtt "github.com/eclipse/paho.mqtt.golang" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"              /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type Client struct { /* 定义 Client 类型。 */
	conn         net.Conn                  /* 执行当前语句并推进处理流程。 */
	connMu       sync.Mutex                /* 执行当前语句并推进处理流程。 */
	inbox        *durableInbox             /* 执行当前语句并推进处理流程。 */
	ctx          context.Context           /* 执行当前语句并推进处理流程。 */
	cancel       context.CancelFunc        /* 执行当前语句并推进处理流程。 */
	routeMu      sync.RWMutex              /* 执行当前语句并推进处理流程。 */
	routes       map[string]ingressHandler /* 执行当前语句并推进处理流程。 */
	filters      map[string]byte           /* 执行当前语句并推进处理流程。 */
	reconnecting atomic.Bool               /* 执行当前语句并推进处理流程。 */
	sharedGroup  string                    /* 执行当前语句并推进处理流程。 */
	broker       string                    /* 执行当前语句并推进处理流程。 */
	credentials  mqtt.CredentialsProvider  /* 执行当前语句并推进处理流程。 */
	client       mqtt.Client               /* 执行当前语句并推进处理流程。 */
	log          *slog.Logger              /* 执行当前语句并推进处理流程。 */
	jobs         chan func()               /* 执行当前语句并推进处理流程。 */
	stop         chan struct{}             /* 执行当前语句并推进处理流程。 */
	stopOnce     sync.Once                 /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (c *Client) logger() *slog.Logger { /* 定义 logger 函数。 */
	if c.log != nil { /* 判断条件并选择处理分支。 */
		return c.log /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return slog.Default() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func New(broker, user, password, clientID string) (*Client, error) { /* 定义 New 函数。 */
	return NewWithCredentials(broker, clientID, func() (string, string) { return user, password }) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func NewWithCredentials(broker, clientID string, credentials mqtt.CredentialsProvider) (*Client, error) { /* 定义 NewWithCredentials 函数。 */
	return newClient(broker, clientID, credentials, nil) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func newClient(broker, clientID string, credentials mqtt.CredentialsProvider, inbox *durableInbox) (*Client, error) { /* 定义 newClient 函数。 */
	ctx, cancel := context.WithCancel(context.Background())                                                                                                                                    /* 更新 cancel 的值。 */
	adapter := &Client{log: slog.Default(), broker: broker, credentials: credentials, inbox: inbox, ctx: ctx, cancel: cancel, routes: map[string]ingressHandler{}, filters: map[string]byte{}} /* 更新 adapter 的值。 */
	if inbox == nil {                                                                                                                                                                          /* 判断条件并选择处理分支。 */
		adapter.startDispatch() /* 执行当前语句并推进处理流程。 */
	} else { /* 结束当前表达式或代码块。 */
		adapter.stop = make(chan struct{}) /* 更新 adapter.stop 的值。 */
	} /* 结束当前表达式或代码块。 */
	opts := mqtt.NewClientOptions().AddBroker(broker).SetClientID(clientID).SetCredentialsProvider(credentials).SetConnectRetry(false).SetAutoReconnect(true).SetMaxReconnectInterval(5 * time.Second).SetOrderMatters(true).SetCleanSession(inbox == nil).SetAutoAckDisabled(inbox != nil) /* 更新 opts 的值。 */
	opts.SetCustomOpenConnectionFn(adapter.openConnection)                                                                                                                                                                                                                                  /* 执行当前语句并推进处理流程。 */
	opts.SetDefaultPublishHandler(func(_ mqtt.Client, m mqtt.Message) { adapter.receive(m) })                                                                                                                                                                                               /* 执行当前语句并推进处理流程。 */
	opts.SetOnConnectHandler(func(client mqtt.Client) { adapter.resubscribe(client) })                                                                                                                                                                                                      /* 执行当前语句并推进处理流程。 */
	adapter.client = mqtt.NewClient(opts)                                                                                                                                                                                                                                                   /* 更新 adapter.client 的值。 */
	token := adapter.client.Connect()                                                                                                                                                                                                                                                       /* 更新 token 的值。 */
	if !token.WaitTimeout(10 * time.Second) {                                                                                                                                                                                                                                               /* 判断条件并选择处理分支。 */
		adapter.Close()                                /* 执行当前语句并推进处理流程。 */
		return nil, fmt.Errorf("mqtt connect timeout") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if token.Error() != nil { /* 判断条件并选择处理分支。 */
		adapter.Close()           /* 执行当前语句并推进处理流程。 */
		return nil, token.Error() /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if inbox != nil { /* 判断条件并选择处理分支。 */
		inbox.start(adapter) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	return adapter, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (c *Client) Publish(ctx context.Context, topic string, payload []byte, qos byte, retained bool) error { /* 定义 Publish 函数。 */
	token := c.client.Publish(topic, qos, retained, payload) /* 更新 token 的值。 */
	done := token.Done()                                     /* 更新 done 的值。 */
	select {                                                 /* 根据条件选择处理路径。 */
	case <-ctx.Done(): /* 处理当前分支。 */
		return ctx.Err() /* 返回当前处理结果。 */
	case <-done: /* 处理当前分支。 */
		return token.Error() /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
func (c *Client) SubscribeRaw(handler func(context.Context, model.RawMessage) error) error { /* 定义 SubscribeRaw 函数。 */
	return c.register("raw", []string{"/external/raw/#", "/jetlinks/raw/#"}, func(ctx context.Context, topic string, payload []byte) error { /* 返回当前处理结果。 */
		var raw model.RawMessage                              /* 声明 raw。 */
		if err := json.Unmarshal(payload, &raw); err != nil { /* 判断条件并选择处理分支。 */
			return Reject(err) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if err := applyRawTopicIdentity(topic, &raw); err != nil { /* 判断条件并选择处理分支。 */
			return Reject(err) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if raw.Source == "" { /* 判断条件并选择处理分支。 */
			raw.Source = "external-mqtt" /* 更新 raw.Source 的值。 */
		} /* 结束当前表达式或代码块。 */
		if err := raw.Validate(); err != nil { /* 判断条件并选择处理分支。 */
			return Reject(err) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		raw.ReceivedAt = ReceivedAt(ctx) /* 更新 raw.ReceivedAt 的值。 */
		return handler(ctx, raw)         /* 返回当前处理结果。 */
	}) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
func (c *Client) SubscribeDeviceState(handler func(context.Context, model.DeviceState) error) error { /* 定义 SubscribeDeviceState 函数。 */
	return c.register("state", []string{"/iot/device/state/#"}, func(ctx context.Context, topic string, payload []byte) error { /* 返回当前处理结果。 */
		var state model.DeviceState                             /* 声明 state。 */
		if err := json.Unmarshal(payload, &state); err != nil { /* 判断条件并选择处理分支。 */
			return Reject(err) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if err := applyStateTopicIdentity(topic, &state); err != nil { /* 判断条件并选择处理分支。 */
			return Reject(err) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return handler(ctx, state) /* 返回当前处理结果。 */
	}) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
func (c *Client) SubscribeVideo(handler func(context.Context, model.VideoAlarmEvent) error) error { /* 定义 SubscribeVideo 函数。 */
	return c.register("video", []string{"/external/video/alarm/#"}, func(ctx context.Context, topic string, payload []byte) error { /* 返回当前处理结果。 */
		var v model.VideoAlarmEvent                         /* 声明 v。 */
		if err := json.Unmarshal(payload, &v); err != nil { /* 判断条件并选择处理分支。 */
			return Reject(err) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if err := applyVideoTopicIdentity(topic, &v); err != nil { /* 判断条件并选择处理分支。 */
			return Reject(err) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if v.EventID == "" { /* 判断条件并选择处理分支。 */
			return Reject(fmt.Errorf("video eventId is required for reliable MQTT receive")) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return handler(ctx, v) /* 返回当前处理结果。 */
	}) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
func topicParts(topic string) []string { /* 定义 topicParts 函数。 */
	return strings.Split(strings.Trim(topic, "/"), "/") /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func applyRawTopicIdentity(topic string, raw *model.RawMessage) error { /* 定义 applyRawTopicIdentity 函数。 */
	parts := topicParts(topic)                                                                                                                            /* 更新 parts 的值。 */
	if len(parts) != 5 || (parts[0] != "external" && parts[0] != "jetlinks") || parts[1] != "raw" || parts[2] == "" || parts[3] == "" || parts[4] == "" { /* 判断条件并选择处理分支。 */
		return fmt.Errorf("expected /external/raw/{tenant}/{product}/{device} or /jetlinks/raw/{tenant}/{product}/{device}") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	raw.TenantID, raw.ProductID, raw.DeviceID = parts[2], parts[3], parts[4] /* 更新 raw.DeviceID 的值。 */
	return nil                                                               /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func applyStateTopicIdentity(topic string, state *model.DeviceState) error { /* 定义 applyStateTopicIdentity 函数。 */
	parts := topicParts(topic)                                                                                                                     /* 更新 parts 的值。 */
	if len(parts) != 6 || parts[0] != "iot" || parts[1] != "device" || parts[2] != "state" || parts[3] == "" || parts[4] == "" || parts[5] == "" { /* 判断条件并选择处理分支。 */
		return fmt.Errorf("expected /iot/device/state/{tenant}/{product}/{device}") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	state.TenantID, state.ProductID, state.DeviceID = parts[3], parts[4], parts[5] /* 更新 state.DeviceID 的值。 */
	return nil                                                                     /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func applyVideoTopicIdentity(topic string, v *model.VideoAlarmEvent) error { /* 定义 applyVideoTopicIdentity 函数。 */
	parts := topicParts(topic)                                                                                                       /* 更新 parts 的值。 */
	if len(parts) != 5 || parts[0] != "external" || parts[1] != "video" || parts[2] != "alarm" || parts[3] == "" || parts[4] == "" { /* 判断条件并选择处理分支。 */
		return fmt.Errorf("expected /external/video/alarm/{tenant}/{camera}") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	v.TenantID, v.CameraID = parts[3], parts[4] /* 更新 v.CameraID 的值。 */
	return nil                                  /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (c *Client) Health(context.Context) error { /* 定义 Health 函数。 */
	if c.inbox != nil { /* 判断条件并选择处理分支。 */
		if err := c.inbox.health(); err != nil { /* 判断条件并选择处理分支。 */
			return err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if !c.client.IsConnectionOpen() { /* 判断条件并选择处理分支。 */
		return fmt.Errorf("mqtt disconnected") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (c *Client) Close() error { /* 定义 Close 函数。 */
	c.stopOnce.Do(func() { /* 执行当前语句并推进处理流程。 */
		if c.cancel != nil { /* 判断条件并选择处理分支。 */
			c.cancel() /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if c.stop != nil { /* 判断条件并选择处理分支。 */
			close(c.stop) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if c.client != nil { /* 判断条件并选择处理分支。 */
			c.client.Disconnect(250) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if c.inbox != nil { /* 判断条件并选择处理分支。 */
			c.inbox.close() /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	}) /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// Legacy embedded constructor only. Production uses the durable inbox and its
// fixed workers; neither path starts an application goroutine per message.
func (c *Client) startDispatch() { /* 定义 startDispatch 函数。 */
	c.jobs = make(chan func(), 128) /* 更新 c.jobs 的值。 */
	c.stop = make(chan struct{})    /* 更新 c.stop 的值。 */
	for i := 0; i < 8; i++ {        /* 循环处理当前数据。 */
		go func() { /* 执行当前语句并推进处理流程。 */
			for { /* 循环处理当前数据。 */
				select { /* 根据条件选择处理路径。 */
				case <-c.stop: /* 处理当前分支。 */
					return /* 返回当前处理结果。 */
				case job := <-c.jobs: /* 处理当前分支。 */
					job() /* 执行当前语句并推进处理流程。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
		}() /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
func (c *Client) enqueue(topic string, job func()) bool { /* 定义 enqueue 函数。 */
	select { /* 根据条件选择处理路径。 */
	case <-c.stop: /* 处理当前分支。 */
		return false /* 返回当前处理结果。 */
	default: /* 处理当前分支。 */
	} /* 结束当前表达式或代码块。 */
	select { /* 根据条件选择处理路径。 */
	case c.jobs <- job: /* 处理当前分支。 */
		return true /* 返回当前处理结果。 */
	default: /* 处理当前分支。 */
		c.logger().Error("MQTT ingress busy: leaving delivery unacknowledged for session redelivery", "topic", topic) /* 执行当前语句并推进处理流程。 */
		return false                                                                                                  /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

// ConfigureSharedSubscriptions is called before registering subscriptions.
func (c *Client) ConfigureSharedSubscriptions(group string) error { /* 定义 ConfigureSharedSubscriptions 函数。 */
	if group == "" || len(group) > 64 || strings.ContainsAny(group, "/+#\x00") { /* 判断条件并选择处理分支。 */
		return fmt.Errorf("invalid MQTT shared subscription group") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	c.sharedGroup = group /* 更新 c.sharedGroup 的值。 */
	return nil            /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (c *Client) subscription(topic string) string { /* 定义 subscription 函数。 */
	if c.sharedGroup == "" { /* 判断条件并选择处理分支。 */
		return topic /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return "$share/" + c.sharedGroup + "/" + topic /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
