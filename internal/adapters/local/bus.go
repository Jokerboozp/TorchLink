package local /* 声明 local 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context" /* 执行当前语句并推进处理流程。 */
	"errors"
	"fmt"  /* 执行当前语句并推进处理流程。 */
	"sync" /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/ports" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type Bus struct { /* 定义 Bus 类型。 */
	mu       sync.RWMutex               /* 执行当前语句并推进处理流程。 */
	handlers map[string][]ports.Handler /* 执行当前语句并推进处理流程。 */
	wg       sync.WaitGroup             /* 执行当前语句并推进处理流程。 */
	closed   bool                       /* 执行当前语句并推进处理流程。 */
	async    map[string]chan asyncDelivery
} /* 结束当前表达式或代码块。 */

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

func NewBus() *Bus { return &Bus{handlers: map[string][]ports.Handler{}} } /* 定义 NewBus 函数。 */
func (b *Bus) Publish(ctx context.Context, topic, key string, payload []byte) error { /* 定义 Publish 函数。 */
	b.mu.RLock()  /* 执行当前语句并推进处理流程。 */
	if b.closed { /* 判断条件并选择处理分支。 */
		b.mu.RUnlock()                  /* 执行当前语句并推进处理流程。 */
		return fmt.Errorf("bus closed") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if queue, ok := b.async[topic]; ok {
		defer b.mu.RUnlock()
		select {
		case queue <- asyncDelivery{ctx: context.WithoutCancel(ctx), payload: append([]byte(nil), payload...)}:
			return nil
		default:
			return fmt.Errorf("topic %s key %s: %w", topic, key, ErrAsyncQueueFull)
		}
	}
	hs := append([]ports.Handler(nil), b.handlers[topic]...) /* 更新 hs 的值。 */
	b.mu.RUnlock()                                           /* 执行当前语句并推进处理流程。 */
	for _, h := range hs {                                   /* 循环处理当前数据。 */
		if err := h(ctx, append([]byte(nil), payload...)); err != nil { /* 判断条件并选择处理分支。 */
			return fmt.Errorf("topic %s key %s: %w", topic, key, err) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (b *Bus) Subscribe(_ context.Context, topic, group string, h ports.Handler) error { /* 定义 Subscribe 函数。 */
	b.mu.Lock()         /* 执行当前语句并推进处理流程。 */
	defer b.mu.Unlock() /* 安排函数结束时执行清理。 */
	if b.closed {       /* 判断条件并选择处理分支。 */
		return fmt.Errorf("bus closed") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	b.handlers[topic] = append(b.handlers[topic], h) /* 更新 b.handlers[topic] 的值。 */
	return nil                                       /* 返回当前处理结果。 */
}                                           /* 结束当前表达式或代码块。 */
func (b *Bus) Health(context.Context) error { return nil } /* 定义 Health 函数。 */
func (b *Bus) Close() error { /* 定义 Close 函数。 */
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

type Realtime struct { /* 定义 Realtime 类型。 */
	mu       sync.RWMutex /* 执行当前语句并推进处理流程。 */
	Messages []Published  /* 执行当前语句并推进处理流程。 */
}                       /* 结束当前表达式或代码块。 */
type Published struct { /* 定义 Published 类型。 */
	Topic    string /* 执行当前语句并推进处理流程。 */
	Payload  []byte /* 执行当前语句并推进处理流程。 */
	QoS      byte   /* 执行当前语句并推进处理流程。 */
	Retained bool   /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func NewRealtime() *Realtime { return &Realtime{} } /* 定义 NewRealtime 函数。 */
func (r *Realtime) Publish(_ context.Context, topic string, payload []byte, qos byte, retained bool) error { /* 定义 Publish 函数。 */
	r.mu.Lock()                                                                                       /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()                                                                               /* 安排函数结束时执行清理。 */
	r.Messages = append(r.Messages, Published{topic, append([]byte(nil), payload...), qos, retained}) /* 更新 r.Messages 的值。 */
	return nil                                                                                        /* 返回当前处理结果。 */
}                                                /* 结束当前表达式或代码块。 */
func (r *Realtime) Health(context.Context) error { return nil } /* 定义 Health 函数。 */
func (r *Realtime) Close() error                 { return nil } /* 定义 Close 函数。 */
