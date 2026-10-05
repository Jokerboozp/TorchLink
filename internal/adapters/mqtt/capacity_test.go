package mqttadapter

import (
	"context"
	"errors"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type capacityPublish struct {
	topic    string
	payload  []byte
	qos      byte
	retained bool
}
type capacityPublisher struct {
	mqtt.Client
	sent   []capacityPublish
	failAt int
}

func (p *capacityPublisher) Publish(topic string, qos byte, retained bool, payload interface{}) mqtt.Token {
	p.sent = append(p.sent, capacityPublish{topic, append([]byte{}, payload.([]byte)...), qos, retained})
	done := make(chan struct{})
	close(done)
	var err error
	if p.failAt == len(p.sent) {
		err = errors.New("publish unavailable")
	}
	return capacityToken{done: done, err: err}
}

type capacityToken struct {
	done chan struct{}
	err  error
}

func (t capacityToken) Wait() bool                     { return true }
func (t capacityToken) WaitTimeout(time.Duration) bool { return true }
func (t capacityToken) Done() <-chan struct{}          { return t.done }
func (t capacityToken) Error() error                   { return t.err }

func TestCapacityRetainedPublishesExactTopicsAndReportsFailure(t *testing.T) {
	publisher := &capacityPublisher{}
	c := &Client{client: publisher}
	n, err := c.ClearCapacityRetained(context.Background(), "t", "p", []string{"fixture", "fixture"})
	if err != nil || n != 1 || len(publisher.sent) != 1 {
		t.Fatal(n, err)
	}
	p := publisher.sent[0]
	if p.topic != "/iot/device/state/t/p/fixture" || len(p.payload) != 0 || p.qos != 1 || !p.retained {
		t.Fatal("wrong exact retained removal", p)
	}
	for _, bad := range []string{"+", "#", "other/device", ""} {
		if _, err := c.ClearCapacityRetained(context.Background(), "t", "p", []string{bad}); err == nil {
			t.Fatal("unsafe topic level accepted", bad)
		}
	}
	publisher.sent = nil
	publisher.failAt = 2
	if n, err := c.ClearCapacityRetained(context.Background(), "t", "p", []string{"first", "second"}); err == nil || n != 1 {
		t.Fatal("publish failure claimed completion", n, err)
	}
}

func TestDiscardCapacityInboxRemovesOnlyFixtureReceipts(t *testing.T) {
	d, err := openInbox(t.TempDir(), 8<<20, 80)
	if err != nil {
		t.Fatal(err)
	}
	c := inboxClient(t, d)
	// A quarantined fixture receipt is removed as well.
	state := "/iot/device/state/t1/cap/cap-2"
	if err := d.put(state, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	q := d.queues[d.shard(state)]
	if raw, found, err := q.Next(); err != nil || !found || q.Reject(raw) != nil || q.Rejected() != 1 {
		t.Fatalf("quarantine setup failed: %v", err)
	}
	for _, topic := range []string{
		"/iot/up/t1/cap/cap-1/property",        // pending fixture uplink
		"/external/raw/t1/cap/cap-2",           // pending fixture raw
		"/iot/up/t1/cap/cap-9/property",        // another device of the product
		"/iot/up/t2/cap/cap-1/property",        // same IDs in another tenant
		"/iot/device/state/t1/other/cap-1",     // another product
		"/iot/device/state/t1/cap/cap-1/extra", // malformed state topic
	} {
		if err := d.put(topic, []byte(`{"v":1}`)); err != nil {
			t.Fatal(err)
		}
	}
	removed, err := c.DiscardCapacityInbox(context.Background(), "t1", "cap", []string{"cap-1", "cap-2"})
	if err != nil || removed != 3 {
		t.Fatalf("removed=%d err=%v", removed, err)
	}
	if inboxDepth(d) != 4 || q.Rejected() != 0 {
		t.Fatalf("unrelated receipts changed: depth=%d rejected=%d", inboxDepth(d), q.Rejected())
	}
}
