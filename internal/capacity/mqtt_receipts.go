package capacity

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// MQTTReceipts tracks platform archive receipts for one device connection. A
// receipt must match both the client message ID and the SHA-256 of the exact
// published bytes, so a reused ID with different bytes cannot consume an
// earlier confirmation. Retries resend identical bytes.
type MQTTReceipts struct {
	mu      sync.Mutex
	pending map[string]chan string
}

func NewMQTTReceipts() *MQTTReceipts { return &MQTTReceipts{pending: map[string]chan string{}} }

// Receive is the subscription handler for the device receipt topic.
func (r *MQTTReceipts) Receive(_ mqtt.Client, m mqtt.Message) {
	var v struct {
		ID           string `json:"id"`
		Hash         string `json:"payloadHash"`
		Status       string `json:"status"`
		RawMessageID string `json:"rawMessageId"`
	}
	if json.Unmarshal(m.Payload(), &v) != nil || v.Status != "archived" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := v.ID + "\x00" + v.Hash
	if done, ok := r.pending[key]; ok {
		delete(r.pending, key)
		done <- v.RawMessageID
		close(done)
	}
}

// Pending reports unmatched waits (tests and leak checks).
func (r *MQTTReceipts) Pending() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.pending)
}

// ReceiptResult is the outcome of one logical message.
type ReceiptResult struct {
	OK       bool
	Code     string
	Bytes    int64
	Attempts int
	RawID    string
}

// Publish sends body (which must carry a JSON "id") and waits for its archive
// receipt, retrying the same bytes up to retries times.
func (r *MQTTReceipts) Publish(ctx context.Context, c mqtt.Client, topic string, body []byte, qos byte, wait time.Duration, retries int) ReceiptResult {
	var envelope struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.ID == "" {
		return ReceiptResult{Code: "invalid_id"}
	}
	key := envelope.ID + "\x00" + payloadHash(body)
	done := make(chan string, 1)
	r.mu.Lock()
	r.pending[key] = done
	r.mu.Unlock()
	defer func() { r.mu.Lock(); delete(r.pending, key); r.mu.Unlock() }()
	for attempt := 0; attempt <= retries; attempt++ {
		token := c.Publish(topic, qos, false, body)
		select {
		case <-ctx.Done():
			return ReceiptResult{Code: "timeout", Attempts: attempt + 1}
		case <-token.Done():
			if token.Error() != nil {
				return ReceiptResult{Code: ShortError(token.Error()), Attempts: attempt + 1}
			}
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ReceiptResult{Code: "receipt_timeout", Attempts: attempt + 1}
		case raw := <-done:
			timer.Stop()
			code := "archived"
			if attempt > 0 {
				code = "archived_retry"
			}
			return ReceiptResult{OK: true, Code: code, Bytes: int64(len(body)), Attempts: attempt + 1, RawID: raw}
		case <-timer.C:
		}
	}
	return ReceiptResult{Code: "receipt_timeout", Attempts: retries + 1}
}
