package mqttadapter /* 声明 mqttadapter 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                                  /* 执行当前语句并推进处理流程。 */
	"crypto/sha256"                            /* 执行当前语句并推进处理流程。 */
	"errors"                                   /* 执行当前语句并推进处理流程。 */
	"fmt"                                      /* 执行当前语句并推进处理流程。 */
	mqtt "github.com/eclipse/paho.mqtt.golang" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"              /* 执行当前语句并推进处理流程。 */
	"strings"                                  /* 执行当前语句并推进处理流程。 */
	"time"                                     /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type ingressHandler func(context.Context, string, []byte) error /* 定义 ingressHandler 类型。 */
type rejected struct{ error }                                   /* 定义 rejected 类型。 */

// Reject explicitly marks a permanent validation/authorization failure. Unknown
// errors (including storage/network failures) remain retryable by default.
func Reject(err error) error { /* 定义 Reject 函数。 */
	if err == nil { /* 判断条件并选择处理分支。 */
		return nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return rejected{err} /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func permanent(err error) bool { /* 定义 permanent 函数。 */
	var r rejected                                                    /* 声明 r。 */
	return errors.As(err, &r) || errors.Is(err, model.ErrRawConflict) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func ingressID(topic string, payload []byte) string { /* 定义 ingressID 函数。 */
	return fmt.Sprintf("mqtt_%x", sha256.Sum256(append([]byte(topic+"\x00"), payload...))) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func route(topic string) string { /* 定义 route 函数。 */
	switch { /* 根据条件选择处理路径。 */
	case strings.HasPrefix(topic, "/iot/up/"): /* 处理当前分支。 */
		return "standard" /* 返回当前处理结果。 */
	case strings.HasPrefix(topic, "/external/raw/"), strings.HasPrefix(topic, "/jetlinks/raw/"): /* 处理当前分支。 */
		return "raw" /* 返回当前处理结果。 */
	case strings.HasPrefix(topic, "/iot/device/state/"): /* 处理当前分支。 */
		return "state" /* 返回当前处理结果。 */
	case strings.HasPrefix(topic, "/external/video/alarm/"): /* 处理当前分支。 */
		return "video" /* 返回当前处理结果。 */
	default: /* 处理当前分支。 */
		return "" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
func (c *Client) register(kind string, topics []string, handler ingressHandler) error { /* 定义 register 函数。 */
	c.routeMu.Lock()               /* 执行当前语句并推进处理流程。 */
	c.routes[kind] = handler       /* 更新 c.routes[kind] 的值。 */
	filters := map[string]byte{}   /* 更新 filters 的值。 */
	for _, topic := range topics { /* 循环处理当前数据。 */
		filter := c.subscription(topic) /* 更新 filter 的值。 */
		c.filters[filter] = 1           /* 更新 c.filters[filter] 的值。 */
		filters[filter] = 1             /* 更新 filters[filter] 的值。 */
	} /* 结束当前表达式或代码块。 */
	c.routeMu.Unlock()                                                                                 /* 执行当前语句并推进处理流程。 */
	token := c.client.SubscribeMultiple(filters, func(_ mqtt.Client, m mqtt.Message) { c.receive(m) }) /* 更新 token 的值。 */
	if !token.WaitTimeout(10 * time.Second) {                                                          /* 判断条件并选择处理分支。 */
		return errors.New("MQTT subscription timeout") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return token.Error() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (c *Client) resubscribe(client mqtt.Client) { /* 定义 resubscribe 函数。 */
	c.routeMu.RLock()             /* 执行当前语句并推进处理流程。 */
	filters := map[string]byte{}  /* 更新 filters 的值。 */
	for k, v := range c.filters { /* 循环处理当前数据。 */
		filters[k] = v /* 更新 filters[k] 的值。 */
	} /* 结束当前表达式或代码块。 */
	c.routeMu.RUnlock()    /* 执行当前语句并推进处理流程。 */
	if len(filters) == 0 { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	token := client.SubscribeMultiple(filters, func(_ mqtt.Client, m mqtt.Message) { c.receive(m) }) /* 更新 token 的值。 */
	if !token.WaitTimeout(10*time.Second) || token.Error() != nil {                                  /* 判断条件并选择处理分支。 */
		c.logger().Error("MQTT resubscription failed") /* 执行当前语句并推进处理流程。 */
		c.retryConnection()                            /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
func (c *Client) receive(m mqtt.Message) { /* 定义 receive 函数。 */
	if c.ctx.Err() != nil { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if m.Retained() || route(m.Topic()) == "" || len(m.Payload()) > 128<<10 { /* 判断条件并选择处理分支。 */
		c.logger().Warn("MQTT ingress rejected: retained, unknown topic or oversized payload", "topic", m.Topic()) /* 执行当前语句并推进处理流程。 */
		m.Ack()                                                                                                    /* 执行当前语句并推进处理流程。 */
		return                                                                                                     /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if c.inbox != nil { /* 判断条件并选择处理分支。 */
		if err := c.inbox.put(m.Topic(), m.Payload()); err != nil { /* 判断条件并选择处理分支。 */
			c.logger().Error("MQTT durable receive failed; no acknowledgement", "topic", m.Topic(), "error", err) /* 执行当前语句并推进处理流程。 */
			c.retryConnection()                                                                                   /* 执行当前语句并推进处理流程。 */
			return                                                                                                /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		m.Ack() /* 执行当前语句并推进处理流程。 */
		return  /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	// Preserve the legacy embedded/test constructor semantics. Production uses
	// the durable constructor above; no asynchronous ACK is sent to a closed
	// Paho session from these background workers.
	topic, payload, at := m.Topic(), append([]byte(nil), m.Payload()...), time.Now().UnixMilli() /* 更新 at 的值。 */
	c.enqueue(topic, func() {                                                                    /* 执行当前语句并推进处理流程。 */
		if err := c.handle(topic, payload, at); err != nil { /* 判断条件并选择处理分支。 */
			c.logger().Warn("non-durable MQTT handler failed", "topic", topic, "error", err) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	}) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

type receptionKey struct{} /* 定义 receptionKey 类型。 */

// ReceivedAt is the first durable transport reception, independent of a later
// retry's processing time. It is set only by the MQTT adapter.
func ReceivedAt(ctx context.Context) int64 { v, _ := ctx.Value(receptionKey{}).(int64); return v } /* 定义 ReceivedAt 函数。 */
func (c *Client) handle(topic string, payload []byte, receivedAt int64) error { /* 定义 handle 函数。 */
	c.routeMu.RLock()                 /* 执行当前语句并推进处理流程。 */
	handler := c.routes[route(topic)] /* 更新 handler 的值。 */
	c.routeMu.RUnlock()               /* 执行当前语句并推进处理流程。 */
	if handler == nil {               /* 判断条件并选择处理分支。 */
		return errors.New("MQTT ingress handler not registered yet") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)                          /* 更新 cancel 的值。 */
	defer cancel()                                                                     /* 安排函数结束时执行清理。 */
	return handler(context.WithValue(ctx, receptionKey{}, receivedAt), topic, payload) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (c *Client) pause(d time.Duration) bool { /* 定义 pause 函数。 */
	timer := time.NewTimer(d) /* 更新 timer 的值。 */
	defer timer.Stop()        /* 安排函数结束时执行清理。 */
	select {                  /* 根据条件选择处理路径。 */
	case <-c.ctx.Done(): /* 处理当前分支。 */
		return false /* 返回当前处理结果。 */
	case <-timer.C: /* 处理当前分支。 */
		return true /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
func (c *Client) retryConnection() { /* 定义 retryConnection 函数。 */
	if !c.reconnecting.CompareAndSwap(false, true) { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	go func() { /* 执行当前语句并推进处理流程。 */
		defer c.reconnecting.Store(false) /* 安排函数结束时执行清理。 */
		if !c.pause(time.Second) {        /* 判断条件并选择处理分支。 */
			return /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		// Expire the read to enter Paho's normal connection-loss path.
		// Paho suppresses a local Close error; Disconnect()+Connect() can
		// overlap cleanup. A read timeout triggers its reconnect state machine.
		c.connMu.Lock()    /* 执行当前语句并推进处理流程。 */
		if c.conn != nil { /* 判断条件并选择处理分支。 */
			_ = c.conn.SetReadDeadline(time.Now()) /* 更新 _ 的值。 */
		} /* 结束当前表达式或代码块。 */
		c.connMu.Unlock() /* 执行当前语句并推进处理流程。 */
	}() /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
