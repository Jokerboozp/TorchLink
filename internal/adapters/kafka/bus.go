package kafkaadapter /* 声明 kafkaadapter 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context" /* 执行当前语句并推进处理流程。 */
	"fmt"     /* 执行当前语句并推进处理流程。 */
	"strings" /* 执行当前语句并推进处理流程。 */
	"sync"    /* 执行当前语句并推进处理流程。 */
	"time"    /* 执行当前语句并推进处理流程。 */

	"github.com/segmentio/kafka-go" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports"   /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type Bus struct { /* 定义 Bus 类型。 */
	brokers []string                 /* 执行当前语句并推进处理流程。 */
	mu      sync.Mutex               /* 执行当前语句并推进处理流程。 */
	writers map[string]*kafka.Writer /* 执行当前语句并推进处理流程。 */
	readers []*kafka.Reader          /* 执行当前语句并推进处理流程。 */
	cancel  context.CancelFunc       /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func New(brokers []string) *Bus { return &Bus{brokers: brokers, writers: map[string]*kafka.Writer{}} } /* 定义 New 函数。 */
func (b *Bus) writer(topic string) *kafka.Writer { /* 定义 writer 函数。 */
	b.mu.Lock()                          /* 执行当前语句并推进处理流程。 */
	defer b.mu.Unlock()                  /* 安排函数结束时执行清理。 */
	if w := b.writers[topic]; w != nil { /* 判断条件并选择处理分支。 */
		return w /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	w := &kafka.Writer{Addr: kafka.TCP(b.brokers...), Topic: topic, Balancer: &kafka.Hash{}, RequiredAcks: kafka.RequireAll, Async: false, AllowAutoTopicCreation: true, BatchSize: 500, BatchBytes: 4 << 20, BatchTimeout: 10 * time.Millisecond} /* 更新 w 的值。 */
	b.writers[topic] = w                                                                                                                                                                                                                           /* 更新 b.writers[topic] 的值。 */
	return w                                                                                                                                                                                                                                       /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (b *Bus) Publish(ctx context.Context, topic, key string, payload []byte) error { /* 定义 Publish 函数。 */
	return b.writer(topic).WriteMessages(ctx, kafka.Message{Key: []byte(key), Value: payload}) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (b *Bus) Subscribe(ctx context.Context, topic, group string, h ports.Handler) error { /* 定义 Subscribe 函数。 */
	reader := kafka.NewReader(kafka.ReaderConfig{Brokers: b.brokers, Topic: topic, GroupID: "iot-platform-" + group, MinBytes: 1, MaxBytes: 10e6, CommitInterval: 0}) /* 更新 reader 的值。 */
	b.mu.Lock()                                                                                                                                                       /* 执行当前语句并推进处理流程。 */
	b.readers = append(b.readers, reader)                                                                                                                             /* 更新 b.readers 的值。 */
	b.mu.Unlock()                                                                                                                                                     /* 执行当前语句并推进处理流程。 */
	go func() {                                                                                                                                                       /* 执行当前语句并推进处理流程。 */
		for { /* 循环处理当前数据。 */
			m, err := reader.FetchMessage(ctx) /* 更新 err 的值。 */
			if err != nil {                    /* 判断条件并选择处理分支。 */
				return /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			var handleErr error                         /* 声明 handleErr。 */
			for attempt := 1; attempt <= 3; attempt++ { /* 循环处理当前数据。 */
				handleErr = h(ctx, m.Value) /* 更新 handleErr 的值。 */
				if handleErr == nil {       /* 判断条件并选择处理分支。 */
					break /* 执行当前语句并推进处理流程。 */
				} /* 结束当前表达式或代码块。 */
				select { /* 根据条件选择处理路径。 */
				case <-ctx.Done(): /* 处理当前分支。 */
					return /* 返回当前处理结果。 */
				case <-time.After(time.Duration(attempt*attempt) * 250 * time.Millisecond): /* 处理当前分支。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
			if handleErr != nil { /* 判断条件并选择处理分支。 */
				dlq := append([]byte(fmt.Sprintf(`{"sourceTopic":%q,"consumerGroup":%q,"retryCount":3,"error":%q,"payload":`, topic, group, handleErr.Error())), append(m.Value, '}')...) /* 更新 dlq 的值。 */
				if err := b.Publish(ctx, "iot.dlq."+group, string(m.Key), dlq); err != nil {                                                                                              /* 判断条件并选择处理分支。 */
					continue /* 执行当前语句并推进处理流程。 */
				} /* 结束当前表达式或代码块。 */
				_ = reader.CommitMessages(ctx, m) /* 更新 _ 的值。 */
				continue                          /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
			_ = reader.CommitMessages(ctx, m) /* 更新 _ 的值。 */
		} /* 结束当前表达式或代码块。 */
	}() /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (b *Bus) Health(ctx context.Context) error { /* 定义 Health 函数。 */
	if len(b.brokers) == 0 { /* 判断条件并选择处理分支。 */
		return fmt.Errorf("no kafka brokers") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	conn, err := kafka.DialContext(ctx, "tcp", b.brokers[0]) /* 更新 err 的值。 */
	if err != nil {                                          /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer conn.Close()      /* 安排函数结束时执行清理。 */
	_, err = conn.Brokers() /* 更新 err 的值。 */
	return err              /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (b *Bus) Close() error { /* 定义 Close 函数。 */
	b.mu.Lock()                   /* 执行当前语句并推进处理流程。 */
	defer b.mu.Unlock()           /* 安排函数结束时执行清理。 */
	var errs []string             /* 声明 errs。 */
	for _, r := range b.readers { /* 循环处理当前数据。 */
		if err := r.Close(); err != nil { /* 判断条件并选择处理分支。 */
			errs = append(errs, err.Error()) /* 更新 errs 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	for _, w := range b.writers { /* 循环处理当前数据。 */
		if err := w.Close(); err != nil { /* 判断条件并选择处理分支。 */
			errs = append(errs, err.Error()) /* 更新 errs 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if len(errs) > 0 { /* 判断条件并选择处理分支。 */
		return fmt.Errorf("%s", strings.Join(errs, "; ")) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
