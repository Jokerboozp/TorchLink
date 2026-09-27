package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"testing"
	"time"
)

type receiptMessage struct {
	mqtt.Message
	body []byte
}

func (m receiptMessage) Payload() []byte { return m.body }

type receiptToken struct {
	mqtt.Token
	done chan struct{}
}

func (t receiptToken) Done() <-chan struct{} { return t.done }
func (t receiptToken) Error() error          { return nil }

type receiptClient struct {
	mqtt.Client
	calls   int
	sent    [][]byte
	tracker *mqttReceipts
}

func (c *receiptClient) Publish(_ string, _ byte, _ bool, payload interface{}) mqtt.Token {
	body := payload.([]byte)
	c.calls++
	c.sent = append(c.sent, append([]byte{}, body...))
	hash := fmt.Sprintf("%x", sha256.Sum256(body))
	if c.calls == 1 {
		hash = "wrong-hash"
	}
	v, _ := json.Marshal(map[string]string{"id": "r", "status": "archived", "payloadHash": hash})
	c.tracker.receive(nil, receiptMessage{body: v})
	done := make(chan struct{})
	close(done)
	return receiptToken{done: done}
}
func TestMQTTReceiptsRetryIdenticalBytesAndValidateHash(t *testing.T) {
	oldWait, oldRetries := *receiptWait, *receiptRetries
	*receiptWait = 5 * time.Millisecond
	*receiptRetries = 1
	defer func() { *receiptWait = oldWait; *receiptRetries = oldRetries }()
	r := newMQTTReceipts()
	c := &receiptClient{tracker: r}
	body := []byte(`{"id":"r","timestamp":1}`)
	ok, status, _ := r.publish(context.Background(), c, "topic", body)
	if !ok || status != "archived_retry" || c.calls != 2 || string(c.sent[0]) != string(c.sent[1]) || len(r.pending) != 0 {
		t.Fatal(ok, status, c.calls)
	}
}
