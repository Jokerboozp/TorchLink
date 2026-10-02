package mqttadapter

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
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

func TestCapacityRetainedPublishesOnlyExclusiveExactTopicsAndReportsFailure(t *testing.T) {
	publisher := &capacityPublisher{}
	c := &Client{client: publisher}
	q := model.CapacityCleanupBatch{Product: "p", Devices: []string{"fixture", "shared"}, RemoveDevices: []string{"fixture", "fixture"}}
	n, err := c.ClearCapacityRetained(context.Background(), "t", q)
	if err != nil || n.RetainedRequests != 1 || len(publisher.sent) != 1 {
		t.Fatal(n, err)
	}
	p := publisher.sent[0]
	if p.topic != "/iot/device/state/t/p/fixture" || len(p.payload) != 0 || p.qos != 1 || !p.retained {
		t.Fatal("wrong exact retained removal", p)
	}
	for _, bad := range []string{"+", "#", "other/device", "missing"} {
		q.RemoveDevices = []string{bad}
		if _, err := c.ClearCapacityRetained(context.Background(), "t", q); err == nil {
			t.Fatal("unsafe/outside manifest topic accepted", bad)
		}
	}
	q.Devices, q.RemoveDevices = []string{"first", "second"}, []string{"first", "second"}
	publisher.sent = nil
	publisher.failAt = 2
	if n, err := c.ClearCapacityRetained(context.Background(), "t", q); err == nil || n.RetainedRequests != 1 {
		t.Fatal("publish failure claimed completion", n, err)
	}
}

func TestCapacityInboxPreservesSharedForeignAndBrokenIdentity(t *testing.T) {
	d, err := openInbox(t.TempDir(), 1<<20, 1024)
	if err != nil {
		t.Fatal(err)
	}
	c := inboxClient(t, d)
	for _, topic := range []string{"/iot/up/t/p/fixture/event", "/iot/up/t/p/shared/event", "/iot/up/other/p/fixture/event", "/iot/up/t/other/fixture/event"} {
		if err := d.put(topic, []byte(`{"id":"report"}`)); err != nil {
			t.Fatal(err)
		}
	}
	topic := "/iot/up/t/p/fixture/state"
	if err := d.put(topic, []byte(`{"status":"bad"}`)); err != nil {
		t.Fatal(err)
	}
	queue := d.queues[d.shard(topic)]
	raw, found, err := queue.Next()
	if err != nil || !found {
		t.Fatal(err)
	}
	if err := queue.Reject(raw); err != nil {
		t.Fatal(err)
	}
	badPayload, _ := json.Marshal([]byte(`{"changed":true}`))
	bad := model.RawMessage{TenantID: "mqtt-inbox", MessageID: ingressID("/iot/up/t/p/fixture/alarm", []byte(`{"original":true}`)), Source: "mqtt-receive-inbox", Headers: map[string]string{"topic": "/iot/up/t/p/fixture/alarm"}, Payload: badPayload}
	if err := d.queues[d.shard(bad.Headers["topic"])].Put(bad); err != nil {
		t.Fatal(err)
	}
	q := model.CapacityCleanupBatch{Product: "p", Devices: []string{"fixture", "shared"}, RemoveDevices: []string{"fixture"}}
	n, err := c.CleanupCapacityInbox(context.Background(), "t", q)
	if err != nil || n.Inbox != 2 || n.InboxSkipped != 4 || len(n.Warnings) == 0 {
		t.Fatalf("wrong scoped discard: %+v %v", n, err)
	}
	if pending, rejected, _ := c.InboxCounts(); pending != 4 || rejected != 0 {
		t.Fatal("wrong remaining inbox", pending, rejected)
	}
	// Product cleanup is authorized only after server proof that the marked
	// historical product has no registered devices or business references.
	final := model.CapacityCleanupBatch{Product: "p", Historical: true, RemoveProduct: true}
	n, err = c.CleanupCapacityInbox(context.Background(), "t", final)
	if err != nil || n.Inbox != 1 || n.InboxSkipped != 3 {
		t.Fatal("wrong final product discard", n, err)
	}
}

func TestCapacityInboxWaitsForInFlightHandler(t *testing.T) {
	d, err := openInbox(t.TempDir(), 1<<20, 1024)
	if err != nil {
		t.Fatal(err)
	}
	c := inboxClient(t, d)
	entered, release := make(chan struct{}), make(chan struct{})
	c.routes["standard"] = func(context.Context, string, []byte) error { close(entered); <-release; return nil }
	if err := d.put("/iot/up/t/p/fixture/event", []byte(`{"id":"in-flight"}`)); err != nil {
		t.Fatal(err)
	}
	d.start(c)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	type result struct {
		n   ports.RuntimeCleanupCounts
		err error
	}
	done := make(chan result, 1)
	go func() {
		n, err := c.CleanupCapacityInbox(context.Background(), "t", model.CapacityCleanupBatch{Product: "p", Devices: []string{"fixture"}, RemoveDevices: []string{"fixture"}})
		done <- result{n, err}
	}()
	select {
	case <-done:
		close(release)
		t.Fatal("cleanup passed in-flight handler")
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatal(r.err)
		}
	case <-time.After(time.Second):
		t.Fatal("cleanup did not finish")
	}
	if pending, _, _ := c.InboxCounts(); pending != 0 {
		t.Fatal("handler/discard left a pending record", pending)
	}
}
