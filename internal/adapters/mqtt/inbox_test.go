package mqttadapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iot-platform/internal/model"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"iot-platform/internal/edgeagent"
)

type receivedMessage struct {
	topic   string
	payload []byte
	acked   atomic.Bool
}

func (m *receivedMessage) Duplicate() bool   { return false }
func (m *receivedMessage) Qos() byte         { return 1 }
func (m *receivedMessage) Retained() bool    { return false }
func (m *receivedMessage) Topic() string     { return m.topic }
func (m *receivedMessage) MessageID() uint16 { return 1 }
func (m *receivedMessage) Payload() []byte   { return m.payload }
func (m *receivedMessage) Ack()              { m.acked.Store(true) }
func inboxClient(t *testing.T, d *durableInbox) *Client {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{inbox: d, ctx: ctx, cancel: cancel, routes: map[string]ingressHandler{}, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	t.Cleanup(func() { cancel(); d.close() })
	return c
}
func eventually(t *testing.T, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not reached")
}
func inboxDepth(d *durableInbox) int {
	n := 0
	for _, q := range d.queues {
		n += q.Depth()
	}
	return n
}

func TestDurableReceiveRestartRetryAndQuarantine(t *testing.T) {
	root := t.TempDir()
	d, err := openInbox(root, 8<<20, 80)
	if err != nil {
		t.Fatal(err)
	}
	c := inboxClient(t, d)
	m := &receivedMessage{topic: "/iot/up/t/p/d/event", payload: []byte(`{"id":"fire","data":{"fireAlarm":true}}`)}
	c.receive(m)
	if !m.acked.Load() || inboxDepth(d) != 1 {
		t.Fatal("ACK was not backed by durable receipt")
	}
	// Simulate loss of all process memory before handler registration.
	c.cancel()
	d.close()
	d, err = openInbox(root, 8<<20, 80)
	if err != nil {
		t.Fatal(err)
	}
	c = inboxClient(t, d)
	var attempts atomic.Int32
	var ready atomic.Bool
	c.routes["standard"] = func(ctx context.Context, topic string, payload []byte) error {
		attempts.Add(1)
		if topic != m.topic || string(payload) != string(m.payload) {
			t.Error("persisted envelope changed")
		}
		if !ready.Load() {
			return errors.New("database offline")
		}
		return nil
	}
	d.start(c)
	eventually(t, func() bool { return attempts.Load() > 0 })
	if inboxDepth(d) != 1 {
		t.Fatal("failure lost receipt")
	}
	if d.health() == nil {
		t.Fatal("pending failure hidden")
	}
	ready.Store(true)
	eventually(t, func() bool { return inboxDepth(d) == 0 })
	c.routeMu.Lock()
	c.routes["standard"] = func(context.Context, string, []byte) error { return Reject(errors.New("disabled device")) }
	c.routeMu.Unlock()
	if err = d.put(m.topic, []byte(`{"id":"denied"}`)); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		n := 0
		for _, q := range d.queues {
			n += q.Rejected()
		}
		return n == 1
	})
	if _, rejected, _ := c.InboxCounts(); rejected != 1 {
		t.Fatal("rejection quarantine hidden")
	}
	if d.health() != nil {
		t.Fatal("isolated rejection blocked healthy receive", d.health())
	}
	files, err := filepath.Glob(filepath.Join(root, "*", "*.rejected"))
	if err != nil || len(files) != 1 {
		t.Fatal(files, err)
	}
	b, err := os.ReadFile(files[0])
	if err != nil || len(b) == 0 {
		t.Fatal("rejected evidence lost")
	}
}

func TestDurableReceiveFullAndCorruptionDoNotClaimSuccess(t *testing.T) {
	root := t.TempDir()
	d, err := openInbox(root, 8<<20, 8)
	if err != nil {
		t.Fatal(err)
	}
	c := inboxClient(t, d)
	topic := "/iot/up/t/p/d/event"
	one := &receivedMessage{topic: topic, payload: []byte(`{"id":"one"}`)}
	c.receive(one)
	two := &receivedMessage{topic: topic, payload: []byte(`{"id":"two"}`)}
	c.receive(two)
	if !one.acked.Load() || two.acked.Load() {
		t.Fatal("capacity failure incorrectly acknowledged")
	}
	if !errors.Is(d.health(), edgeagent.ErrQueueFull) {
		t.Fatal(d.health())
	}
	c.cancel()
	d.close()
	files, _ := filepath.Glob(filepath.Join(root, "*", "*.json"))
	if len(files) != 1 {
		t.Fatal(files)
	}
	if err = os.WriteFile(files[0], []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	d, err = openInbox(root, 8<<20, 80)
	if err != nil {
		t.Fatal(err)
	}
	c = inboxClient(t, d)
	var handled atomic.Int32
	c.routes["standard"] = func(context.Context, string, []byte) error { handled.Add(1); return nil }
	d.start(c)
	if err = d.put(topic, []byte(`{"id":"valid"}`)); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return handled.Load() == 1 && inboxDepth(d) == 0 })
	if b, err := os.ReadFile(files[0] + ".corrupt"); err != nil || string(b) != "broken" {
		t.Fatal("corrupt bytes not retained", err)
	}
	if _, _, corrupt := c.InboxCounts(); corrupt != 1 {
		t.Fatal("corruption hidden")
	}
}

func TestInboxIdentityPersistsAndDirectoryIsExclusive(t *testing.T) {
	root := t.TempDir()
	d, err := openInbox(root, 8<<20, 80)
	if err != nil {
		t.Fatal(err)
	}
	defer d.close()
	first, err := inboxIdentity(root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := inboxIdentity(root)
	if err != nil || first != second {
		t.Fatal(first, second, err)
	}
	if other, err := openInbox(root, 8<<20, 80); err == nil {
		other.close()
		t.Fatal("two receivers own the same inbox")
	}
	for i := 0; i < 200; i++ {
		if err = d.put(fmt.Sprintf("/iot/up/t/p/d%d/event", i), []byte(`{}`)); err != nil {
			break
		}
	}
}

func TestDurableReceiptPreservesBytesAndOriginalTime(t *testing.T) {
	root := t.TempDir()
	d, err := openInbox(root, 8<<20, 80)
	if err != nil {
		t.Fatal(err)
	}
	defer d.close()
	topic := "/iot/up/t/p/d/event"
	body := []byte{0xff, 0x00, 'a'}
	if err = d.put(topic, body); err != nil {
		t.Fatal(err)
	}
	var first model.RawMessage
	for _, q := range d.queues {
		raw, found, err := q.Next()
		if err != nil {
			t.Fatal(err)
		}
		if found {
			first = raw
		}
	}
	if first.ReceivedAt <= 0 {
		t.Fatal("receipt time missing")
	}
	time.Sleep(2 * time.Millisecond)
	if err = d.put(topic, body); err != nil {
		t.Fatal(err)
	}
	for _, q := range d.queues {
		raw, found, err := q.Next()
		if err != nil {
			t.Fatal(err)
		}
		if found {
			var got []byte
			if err = json.Unmarshal(raw.Payload, &got); err != nil || !bytes.Equal(got, body) || raw.ReceivedAt != first.ReceivedAt {
				t.Fatal("receipt bytes/time changed", err)
			}
		}
	}
}

func TestInboxReadFailureIsVisibleAndRecovers(t *testing.T) {
	root := t.TempDir()
	d, err := openInbox(root, 8<<20, 80)
	if err != nil {
		t.Fatal(err)
	}
	c := inboxClient(t, d)
	path := filepath.Join(root, "0")
	moved := filepath.Join(root, "unavailable")
	if err = os.Rename(path, moved); err != nil {
		t.Fatal(err)
	}
	d.start(c)
	eventually(t, func() bool { return d.health() != nil })
	if err = os.Rename(moved, path); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return d.health() == nil })
}
