package mqttadapter /* 声明 mqttadapter 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"crypto/rand"                              /* 执行当前语句并推进处理流程。 */
	"crypto/sha256"                            /* 执行当前语句并推进处理流程。 */
	"encoding/hex"                             /* 执行当前语句并推进处理流程。 */
	"encoding/json"                            /* 执行当前语句并推进处理流程。 */
	"errors"                                   /* 执行当前语句并推进处理流程。 */
	"fmt"                                      /* 执行当前语句并推进处理流程。 */
	mqtt "github.com/eclipse/paho.mqtt.golang" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/durablequeue"       /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"              /* 执行当前语句并推进处理流程。 */
	"os"                                       /* 执行当前语句并推进处理流程。 */
	"path/filepath"                            /* 执行当前语句并推进处理流程。 */
	"runtime"                                  /* 执行当前语句并推进处理流程。 */
	"strings"                                  /* 执行当前语句并推进处理流程。 */
	"sync"                                     /* 执行当前语句并推进处理流程。 */
	"time"                                     /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

// Reuse the fsynced queue/lock/quarantine implementation shared by durable receivers.
// These RawMessage values are private transport envelopes, never parsed or
// inserted into business storage directly.
type durableInbox struct { /* 定义 durableInbox 类型。 */
	queues    []*durablequeue.Queue /* 执行当前语句并推进处理流程。 */
	wg        sync.WaitGroup        /* 执行当前语句并推进处理流程。 */
	mu        sync.Mutex            /* 执行当前语句并推进处理流程。 */
	lastError error                 /* 执行当前语句并推进处理流程。 */
	pending   map[int]error         /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func NewDurableWithCredentials(broker, root string, credentials mqtt.CredentialsProvider) (*Client, error) { /* 定义 NewDurableWithCredentials 函数。 */
	inbox, err := openInbox(root, 512<<20, 10000) /* 更新 err 的值。 */
	if err != nil {                               /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	clientID, err := inboxIdentity(root) /* 更新 err 的值。 */
	if err != nil {                      /* 判断条件并选择处理分支。 */
		inbox.close()   /* 执行当前语句并推进处理流程。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	c, err := newClient(broker, clientID, credentials, inbox) /* 更新 err 的值。 */
	if err != nil {                                           /* 判断条件并选择处理分支。 */
		inbox.close() /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	return c, err /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func openInbox(root string, maxBytes int64, maxItems int) (*durableInbox, error) { /* 定义 openInbox 函数。 */
	d := &durableInbox{pending: map[int]error{}} /* 更新 d 的值。 */
	for i := 0; i < 8; i++ {                     /* 循环处理当前数据。 */
		q, err := durablequeue.OpenQueue(filepath.Join(root, fmt.Sprint(i)), maxBytes/8, maxItems/8) /* 更新 err 的值。 */
		if err != nil {                                                                              /* 判断条件并选择处理分支。 */
			d.close()       /* 执行当前语句并推进处理流程。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		d.queues = append(d.queues, q) /* 更新 d.queues 的值。 */
	} /* 结束当前表达式或代码块。 */
	return d, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func inboxIdentity(root string) (string, error) { /* 定义 inboxIdentity 函数。 */
	path := filepath.Join(root, "client-id")     /* 更新 path 的值。 */
	if b, err := os.ReadFile(path); err == nil { /* 判断条件并选择处理分支。 */
		id := strings.TrimSpace(string(b))                         /* 更新 id 的值。 */
		if len(id) != 42 || !strings.HasPrefix(id, "iot-inbox-") { /* 判断条件并选择处理分支。 */
			return "", errors.New("invalid MQTT inbox client identity") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return id, flushInboxIdentity(root) /* 返回当前处理结果。 */
	} else if !os.IsNotExist(err) { /* 结束当前表达式或代码块。 */
		return "", err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var random [16]byte                             /* 声明 random。 */
	if _, err := rand.Read(random[:]); err != nil { /* 判断条件并选择处理分支。 */
		return "", err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	id := "iot-inbox-" + hex.EncodeToString(random[:])                   /* 更新 id 的值。 */
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600) /* 更新 err 的值。 */
	if err != nil {                                                      /* 判断条件并选择处理分支。 */
		return "", err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	_, err = f.WriteString(id) /* 更新 err 的值。 */
	if err == nil {            /* 判断条件并选择处理分支。 */
		err = f.Sync() /* 更新 err 的值。 */
	} /* 结束当前表达式或代码块。 */
	closeErr := f.Close() /* 更新 closeErr 的值。 */
	if err != nil {       /* 判断条件并选择处理分支。 */
		return "", err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if closeErr != nil { /* 判断条件并选择处理分支。 */
		return "", closeErr /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return id, flushInboxIdentity(root) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func flushInboxIdentity(root string) error { /* 定义 flushInboxIdentity 函数。 */
	// Match the queue's platform-specific directory flush policy.
	if runtime.GOOS == "windows" { /* 判断条件并选择处理分支。 */
		return nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	dir, err := os.Open(root) /* 更新 err 的值。 */
	if err != nil {           /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	flushErr := dir.Sync()  /* 更新 flushErr 的值。 */
	closeErr := dir.Close() /* 更新 closeErr 的值。 */
	if flushErr != nil {    /* 判断条件并选择处理分支。 */
		return flushErr /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return closeErr /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (d *durableInbox) put(topic string, payload []byte) error { /* 定义 put 函数。 */
	b, _ := json.Marshal(payload)                                                                                                                                                    /* 更新 _ 的值。 */
	envelope := model.RawMessage{TenantID: "mqtt-inbox", MessageID: ingressID(topic, payload), Source: "mqtt-receive-inbox", Headers: map[string]string{"topic": topic}, Payload: b} /* 更新 envelope 的值。 */
	h := sha256.Sum256([]byte(topic))                                                                                                                                                /* 更新 h 的值。 */
	err := d.queues[int(h[0])%len(d.queues)].PutReceived(envelope)                                                                                                                   /* 更新 err 的值。 */
	d.mu.Lock()                                                                                                                                                                      /* 执行当前语句并推进处理流程。 */
	d.lastError = err                                                                                                                                                                /* 更新 d.lastError 的值。 */
	d.mu.Unlock()                                                                                                                                                                    /* 执行当前语句并推进处理流程。 */
	return err                                                                                                                                                                       /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (d *durableInbox) health() error { /* 定义 health 函数。 */
	d.mu.Lock()        /* 执行当前语句并推进处理流程。 */
	err := d.lastError /* 更新 err 的值。 */
	if err == nil {    /* 判断条件并选择处理分支。 */
		for _, pending := range d.pending { /* 循环处理当前数据。 */
			if pending != nil { /* 判断条件并选择处理分支。 */
				err = pending /* 更新 err 的值。 */
				break         /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	d.mu.Unlock()   /* 执行当前语句并推进处理流程。 */
	if err != nil { /* 判断条件并选择处理分支。 */
		return fmt.Errorf("MQTT receive inbox: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (d *durableInbox) start(c *Client) { /* 定义 start 函数。 */
	for index, queue := range d.queues { /* 循环处理当前数据。 */
		d.wg.Add(1)                                 /* 执行当前语句并推进处理流程。 */
		go func(index int, q *durablequeue.Queue) { /* 执行当前语句并推进处理流程。 */
			defer d.wg.Done()        /* 安排函数结束时执行清理。 */
			for c.ctx.Err() == nil { /* 循环处理当前数据。 */
				raw, found, err := q.Next() /* 更新 err 的值。 */
				if err != nil {             /* 判断条件并选择处理分支。 */
					c.logger().Error("MQTT receive inbox read failed", "error", err) /* 执行当前语句并推进处理流程。 */
				} /* 结束当前表达式或代码块。 */
				if err != nil || !found { /* 判断条件并选择处理分支。 */
					d.mu.Lock()                           /* 执行当前语句并推进处理流程。 */
					d.pending[index] = err                /* 更新 d.pending[index] 的值。 */
					d.mu.Unlock()                         /* 执行当前语句并推进处理流程。 */
					if !c.pause(100 * time.Millisecond) { /* 判断条件并选择处理分支。 */
						return /* 返回当前处理结果。 */
					} /* 结束当前表达式或代码块。 */
					continue /* 执行当前语句并推进处理流程。 */
				} /* 结束当前表达式或代码块。 */
				var payload []byte                                           /* 声明 payload。 */
				if err = json.Unmarshal(raw.Payload, &payload); err == nil { /* 判断条件并选择处理分支。 */
					if raw.MessageID != ingressID(raw.Headers["topic"], payload) || route(raw.Headers["topic"]) == "" { /* 判断条件并选择处理分支。 */
						err = Reject(errors.New("MQTT inbox envelope integrity mismatch")) /* 更新 err 的值。 */
					} else { /* 结束当前表达式或代码块。 */
						err = c.handle(raw.Headers["topic"], payload, raw.ReceivedAt) /* 更新 err 的值。 */
					} /* 结束当前表达式或代码块。 */
				} else { /* 结束当前表达式或代码块。 */
					err = Reject(err) /* 更新 err 的值。 */
				} /* 结束当前表达式或代码块。 */
				if err == nil { /* 判断条件并选择处理分支。 */
					err = q.Ack(raw) /* 更新 err 的值。 */
				} else if permanent(err) { /* 结束当前表达式或代码块。 */
					c.logger().Warn("MQTT receive quarantined after rejection", "messageId", raw.MessageID, "error", err) /* 执行当前语句并推进处理流程。 */
					err = q.Reject(raw)                                                                                   /* 更新 err 的值。 */
				} /* 结束当前表达式或代码块。 */
				d.mu.Lock()            /* 执行当前语句并推进处理流程。 */
				d.pending[index] = err /* 更新 d.pending[index] 的值。 */
				d.mu.Unlock()          /* 执行当前语句并推进处理流程。 */
				if err != nil {        /* 判断条件并选择处理分支。 */
					c.logger().Warn("MQTT receive remains pending", "messageId", raw.MessageID, "error", err) /* 执行当前语句并推进处理流程。 */
					if !c.pause(time.Second) {                                                                /* 判断条件并选择处理分支。 */
						return /* 返回当前处理结果。 */
					} /* 结束当前表达式或代码块。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
		}(index, queue) /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
func (d *durableInbox) close() { /* 定义 close 函数。 */
	d.wg.Wait()                  /* 执行当前语句并推进处理流程。 */
	for _, q := range d.queues { /* 循环处理当前数据。 */
		_ = q.Close() /* 更新 _ 的值。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

// InboxCounts exposes durable backlog and isolated failures to existing metrics.
func (c *Client) InboxCounts() (pending, rejected, corrupt int) { /* 定义 InboxCounts 函数。 */
	if c.inbox == nil { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for _, q := range c.inbox.queues { /* 循环处理当前数据。 */
		pending += q.Depth()     /* 更新 pending 的值。 */
		rejected += q.Rejected() /* 更新 rejected 的值。 */
		corrupt += q.Corrupt()   /* 更新 corrupt 的值。 */
	} /* 结束当前表达式或代码块。 */
	return /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
