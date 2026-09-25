package kafkaadapter /* 声明 kafkaadapter 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context" /* 执行当前语句并推进处理流程。 */
	"encoding/base64"
	"encoding/json"
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
	// lanes and topicLanes bound the parallel handlers of each subscription.
	lanes      int
	topicLanes map[string]int
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
func deadLetterPayload(topic, group string, cause error, payload []byte) []byte {
	message := struct {
		SourceTopic     string          `json:"sourceTopic"`
		ConsumerGroup   string          `json:"consumerGroup"`
		RetryCount      int             `json:"retryCount"`
		Error           string          `json:"error"`
		Payload         json.RawMessage `json:"payload"`
		PayloadEncoding string          `json:"payloadEncoding,omitempty"`
	}{SourceTopic: topic, ConsumerGroup: group, RetryCount: 3, Error: cause.Error()}
	if json.Valid(payload) {
		message.Payload = json.RawMessage(payload)
	} else {
		message.Payload, _ = json.Marshal(base64.StdEncoding.EncodeToString(payload))
		message.PayloadEncoding = "base64"
	}
	body, _ := json.Marshal(message)
	return body
}
func retryUntilSuccess(ctx context.Context, delay time.Duration, operation func() error) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := operation(); err == nil {
			return nil
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
func (b *Bus) Subscribe(ctx context.Context, topic, group string, h ports.Handler) error { /* 定义 Subscribe 函数。 */
	reader := kafka.NewReader(kafka.ReaderConfig{Brokers: b.brokers, Topic: topic, GroupID: "iot-platform-" + group, MinBytes: 1, MaxBytes: 10e6, CommitInterval: 0}) /* 更新 reader 的值。 */
	b.mu.Lock()
	b.readers = append(b.readers, reader)
	lanes := b.lanesFor(topic)
	b.mu.Unlock()
	go b.consume(ctx, reader, topic, group, lanes, h)
	return nil
}

// SetConsumerConcurrency sets the parallel lanes per subscription; perTopic
// overrides the default for specific topics (for example alarm analysis).
func (b *Bus) SetConsumerConcurrency(defaultLanes int, perTopic map[string]int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lanes = defaultLanes
	b.topicLanes = map[string]int{}
	for topic, lanes := range perTopic {
		b.topicLanes[topic] = lanes
	}
}

func (b *Bus) lanesFor(topic string) int {
	if lanes, ok := b.topicLanes[topic]; ok && lanes > 0 {
		return lanes
	}
	if b.lanes > 0 {
		return b.lanes
	}
	return 1
}
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
