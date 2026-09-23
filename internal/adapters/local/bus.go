package local /* 声明 local 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context" /* 执行当前语句并推进处理流程。 */
	"fmt"     /* 执行当前语句并推进处理流程。 */
	"sync"    /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/ports" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type Bus struct { /* 定义 Bus 类型。 */
	mu       sync.RWMutex               /* 执行当前语句并推进处理流程。 */
	handlers map[string][]ports.Handler /* 执行当前语句并推进处理流程。 */
	wg       sync.WaitGroup             /* 执行当前语句并推进处理流程。 */
	closed   bool                       /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func NewBus() *Bus { return &Bus{handlers: map[string][]ports.Handler{}} } /* 定义 NewBus 函数。 */
func (b *Bus) Publish(ctx context.Context, topic, key string, payload []byte) error { /* 定义 Publish 函数。 */
	b.mu.RLock()  /* 执行当前语句并推进处理流程。 */
	if b.closed { /* 判断条件并选择处理分支。 */
		b.mu.RUnlock()                  /* 执行当前语句并推进处理流程。 */
		return fmt.Errorf("bus closed") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
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
	b.mu.Lock()         /* 执行当前语句并推进处理流程。 */
	defer b.mu.Unlock() /* 安排函数结束时执行清理。 */
	b.closed = true     /* 更新 b.closed 的值。 */
	b.wg.Wait()         /* 执行当前语句并推进处理流程。 */
	return nil          /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

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
