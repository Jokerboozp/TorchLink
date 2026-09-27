package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"sync"
	"time"
)

// One receipt tracker per device connection. A reused ID with different bytes
// cannot consume an earlier archive confirmation. Retries keep the exact bytes.
type mqttReceipts struct {
	mu      sync.Mutex
	pending map[string]chan struct{}
}

func newMQTTReceipts() *mqttReceipts { return &mqttReceipts{pending: map[string]chan struct{}{}} }
func (r *mqttReceipts) receive(_ mqtt.Client, m mqtt.Message) {
	var v struct {
		ID     string `json:"id"`
		Hash   string `json:"payloadHash"`
		Status string `json:"status"`
	}
	if json.Unmarshal(m.Payload(), &v) != nil || v.Status != "archived" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := v.ID + "\x00" + v.Hash
	if done, ok := r.pending[key]; ok {
		delete(r.pending, key)
		close(done)
	}
}
func (r *mqttReceipts) publish(ctx context.Context, c mqtt.Client, topic string, body []byte) (bool, string, int64) {
	var envelope struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.ID == "" {
		return false, "invalid_id", 0
	}
	key := fmt.Sprintf("%s\x00%x", envelope.ID, sha256.Sum256(body))
	done := make(chan struct{})
	r.mu.Lock()
	r.pending[key] = done
	r.mu.Unlock()
	defer func() { r.mu.Lock(); delete(r.pending, key); r.mu.Unlock() }()
	for attempt := 0; attempt <= *receiptRetries; attempt++ {
		token := c.Publish(topic, byte(*qos), false, body)
		select {
		case <-ctx.Done():
			return false, "timeout", 0
		case <-token.Done():
			if token.Error() != nil {
				return false, shortErr(token.Error().Error()), 0
			}
		}
		timer := time.NewTimer(*receiptWait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false, "receipt_timeout", 0
		case <-done:
			timer.Stop()
			if attempt > 0 {
				return true, "archived_retry", int64(len(body))
			}
			return true, "archived", int64(len(body))
		case <-timer.C:
		}
	}
	return false, "receipt_timeout", 0
}
