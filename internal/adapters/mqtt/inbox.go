package mqttadapter

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"iot-platform/internal/edgeagent"
	"iot-platform/internal/model"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Reuse the fsynced queue/lock/quarantine implementation already used by Edge.
// These RawMessage values are private transport envelopes, never parsed or
// inserted into business storage directly.
type durableInbox struct {
	queues    []*edgeagent.Queue
	wg        sync.WaitGroup
	mu        sync.Mutex
	lastError error
	pending   map[int]error
}

func NewDurableWithCredentials(broker, root string, credentials mqtt.CredentialsProvider) (*Client, error) {
	inbox, err := openInbox(root, 512<<20, 10000)
	if err != nil {
		return nil, err
	}
	clientID, err := inboxIdentity(root)
	if err != nil {
		inbox.close()
		return nil, err
	}
	c, err := newClient(broker, clientID, credentials, inbox)
	if err != nil {
		inbox.close()
	}
	return c, err
}
func openInbox(root string, maxBytes int64, maxItems int) (*durableInbox, error) {
	d := &durableInbox{pending: map[int]error{}}
	for i := 0; i < 8; i++ {
		q, err := edgeagent.OpenQueue(filepath.Join(root, fmt.Sprint(i)), maxBytes/8, maxItems/8)
		if err != nil {
			d.close()
			return nil, err
		}
		d.queues = append(d.queues, q)
	}
	return d, nil
}
func inboxIdentity(root string) (string, error) {
	path := filepath.Join(root, "client-id")
	if b, err := os.ReadFile(path); err == nil {
		id := strings.TrimSpace(string(b))
		if len(id) != 42 || !strings.HasPrefix(id, "iot-inbox-") {
			return "", errors.New("invalid MQTT inbox client identity")
		}
		return id, flushInboxIdentity(root)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	id := "iot-inbox-" + hex.EncodeToString(random[:])
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	_, err = f.WriteString(id)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	return id, flushInboxIdentity(root)
}
func flushInboxIdentity(root string) error {
	// Match the queue's platform-specific directory flush policy.
	if runtime.GOOS == "windows" {
		return nil
	}
	dir, err := os.Open(root)
	if err != nil {
		return err
	}
	flushErr := dir.Sync()
	closeErr := dir.Close()
	if flushErr != nil {
		return flushErr
	}
	return closeErr
}
func (d *durableInbox) put(topic string, payload []byte) error {
	b, _ := json.Marshal(payload)
	envelope := model.RawMessage{TenantID: "mqtt-inbox", MessageID: ingressID(topic, payload), Source: "mqtt-receive-inbox", Headers: map[string]string{"topic": topic}, Payload: b}
	h := sha256.Sum256([]byte(topic))
	err := d.queues[int(h[0])%len(d.queues)].PutReceived(envelope)
	d.mu.Lock()
	d.lastError = err
	d.mu.Unlock()
	return err
}
func (d *durableInbox) health() error {
	d.mu.Lock()
	err := d.lastError
	if err == nil {
		for _, pending := range d.pending {
			if pending != nil {
				err = pending
				break
			}
		}
	}
	d.mu.Unlock()
	if err != nil {
		return fmt.Errorf("MQTT receive inbox: %w", err)
	}
	return nil
}
func (d *durableInbox) start(c *Client) {
	for index, queue := range d.queues {
		d.wg.Add(1)
		go func(index int, q *edgeagent.Queue) {
			defer d.wg.Done()
			for c.ctx.Err() == nil {
				raw, found, err := q.Next()
				if err != nil {
					c.logger().Error("MQTT receive inbox read failed", "error", err)
				}
				if err != nil || !found {
					d.mu.Lock()
					d.pending[index] = err
					d.mu.Unlock()
					if !c.pause(100 * time.Millisecond) {
						return
					}
					continue
				}
				var payload []byte
				if err = json.Unmarshal(raw.Payload, &payload); err == nil {
					if raw.MessageID != ingressID(raw.Headers["topic"], payload) || route(raw.Headers["topic"]) == "" {
						err = Reject(errors.New("MQTT inbox envelope integrity mismatch"))
					} else {
						err = c.handle(raw.Headers["topic"], payload, raw.ReceivedAt)
					}
				} else {
					err = Reject(err)
				}
				if err == nil {
					err = q.Ack(raw)
				} else if permanent(err) {
					c.logger().Warn("MQTT receive quarantined after rejection", "messageId", raw.MessageID, "error", err)
					err = q.Reject(raw)
				}
				d.mu.Lock()
				d.pending[index] = err
				d.mu.Unlock()
				if err != nil {
					c.logger().Warn("MQTT receive remains pending", "messageId", raw.MessageID, "error", err)
					if !c.pause(time.Second) {
						return
					}
				}
			}
		}(index, queue)
	}
}
func (d *durableInbox) close() {
	d.wg.Wait()
	for _, q := range d.queues {
		_ = q.Close()
	}
}

// InboxCounts exposes durable backlog and isolated failures to existing metrics.
func (c *Client) InboxCounts() (pending, rejected, corrupt int) {
	if c.inbox == nil {
		return
	}
	for _, q := range c.inbox.queues {
		pending += q.Depth()
		rejected += q.Rejected()
		corrupt += q.Corrupt()
	}
	return
}
