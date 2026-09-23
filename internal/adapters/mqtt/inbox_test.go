package mqttadapter /* 声明 mqttadapter 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bytes"                       /* 执行当前语句并推进处理流程。 */
	"context"                     /* 执行当前语句并推进处理流程。 */
	"encoding/json"               /* 执行当前语句并推进处理流程。 */
	"errors"                      /* 执行当前语句并推进处理流程。 */
	"fmt"                         /* 执行当前语句并推进处理流程。 */
	"io"                          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"log/slog"                    /* 执行当前语句并推进处理流程。 */
	"os"                          /* 执行当前语句并推进处理流程。 */
	"path/filepath"               /* 执行当前语句并推进处理流程。 */
	"sync/atomic"                 /* 执行当前语句并推进处理流程。 */
	"testing"                     /* 执行当前语句并推进处理流程。 */
	"time"                        /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/durablequeue" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type receivedMessage struct { /* 定义 receivedMessage 类型。 */
	topic   string      /* 执行当前语句并推进处理流程。 */
	payload []byte      /* 执行当前语句并推进处理流程。 */
	acked   atomic.Bool /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (m *receivedMessage) Duplicate() bool   { return false }        /* 定义 Duplicate 函数。 */
func (m *receivedMessage) Qos() byte         { return 1 }            /* 定义 Qos 函数。 */
func (m *receivedMessage) Retained() bool    { return false }        /* 定义 Retained 函数。 */
func (m *receivedMessage) Topic() string     { return m.topic }      /* 定义 Topic 函数。 */
func (m *receivedMessage) MessageID() uint16 { return 1 }            /* 定义 MessageID 函数。 */
func (m *receivedMessage) Payload() []byte   { return m.payload }    /* 定义 Payload 函数。 */
func (m *receivedMessage) Ack()              { m.acked.Store(true) } /* 定义 Ack 函数。 */
func inboxClient(t *testing.T, d *durableInbox) *Client { /* 定义 inboxClient 函数。 */
	t.Helper()                                                                                                                                 /* 执行当前语句并推进处理流程。 */
	ctx, cancel := context.WithCancel(context.Background())                                                                                    /* 更新 cancel 的值。 */
	c := &Client{inbox: d, ctx: ctx, cancel: cancel, routes: map[string]ingressHandler{}, log: slog.New(slog.NewTextHandler(io.Discard, nil))} /* 更新 c 的值。 */
	t.Cleanup(func() { cancel(); d.close() })                                                                                                  /* 执行当前语句并推进处理流程。 */
	return c                                                                                                                                   /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func eventually(t *testing.T, f func() bool) { /* 定义 eventually 函数。 */
	t.Helper()                                  /* 执行当前语句并推进处理流程。 */
	deadline := time.Now().Add(8 * time.Second) /* 更新 deadline 的值。 */
	for time.Now().Before(deadline) {           /* 循环处理当前数据。 */
		if f() { /* 判断条件并选择处理分支。 */
			return /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		time.Sleep(20 * time.Millisecond) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	t.Fatal("condition not reached") /* 验证实际结果符合预期。 */
} /* 结束当前表达式或代码块。 */
func inboxDepth(d *durableInbox) int { /* 定义 inboxDepth 函数。 */
	n := 0                       /* 更新 n 的值。 */
	for _, q := range d.queues { /* 循环处理当前数据。 */
		n += q.Depth() /* 更新 n 的值。 */
	} /* 结束当前表达式或代码块。 */
	return n /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func TestDurableReceiveRestartRetryAndQuarantine(t *testing.T) { /* 定义 TestDurableReceiveRestartRetryAndQuarantine 函数。 */
	root := t.TempDir()                  /* 更新 root 的值。 */
	d, err := openInbox(root, 8<<20, 80) /* 更新 err 的值。 */
	if err != nil {                      /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	c := inboxClient(t, d)                                                                                          /* 更新 c 的值。 */
	m := &receivedMessage{topic: "/iot/up/t/p/d/event", payload: []byte(`{"id":"fire","data":{"fireAlarm":true}}`)} /* 更新 m 的值。 */
	c.receive(m)                                                                                                    /* 执行当前语句并推进处理流程。 */
	if !m.acked.Load() || inboxDepth(d) != 1 {                                                                      /* 判断条件并选择处理分支。 */
		t.Fatal("ACK was not backed by durable receipt") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	// Simulate loss of all process memory before handler registration.
	c.cancel()                          /* 执行当前语句并推进处理流程。 */
	d.close()                           /* 执行当前语句并推进处理流程。 */
	d, err = openInbox(root, 8<<20, 80) /* 更新 err 的值。 */
	if err != nil {                     /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	c = inboxClient(t, d)                                                                  /* 更新 c 的值。 */
	var attempts atomic.Int32                                                              /* 声明 attempts。 */
	var ready atomic.Bool                                                                  /* 声明 ready。 */
	c.routes["standard"] = func(ctx context.Context, topic string, payload []byte) error { /* 执行当前语句并推进处理流程。 */
		attempts.Add(1)                                               /* 执行当前语句并推进处理流程。 */
		if topic != m.topic || string(payload) != string(m.payload) { /* 判断条件并选择处理分支。 */
			t.Error("persisted envelope changed") /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		if !ready.Load() { /* 判断条件并选择处理分支。 */
			return errors.New("database offline") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	d.start(c)                                                /* 执行当前语句并推进处理流程。 */
	eventually(t, func() bool { return attempts.Load() > 0 }) /* 执行当前语句并推进处理流程。 */
	if inboxDepth(d) != 1 {                                   /* 判断条件并选择处理分支。 */
		t.Fatal("failure lost receipt") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if d.health() == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("pending failure hidden") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	ready.Store(true)                                                                                                   /* 执行当前语句并推进处理流程。 */
	eventually(t, func() bool { return inboxDepth(d) == 0 })                                                            /* 执行当前语句并推进处理流程。 */
	c.routeMu.Lock()                                                                                                    /* 执行当前语句并推进处理流程。 */
	c.routes["standard"] = func(context.Context, string, []byte) error { return Reject(errors.New("disabled device")) } /* 执行当前语句并推进处理流程。 */
	c.routeMu.Unlock()                                                                                                  /* 执行当前语句并推进处理流程。 */
	if err = d.put(m.topic, []byte(`{"id":"denied"}`)); err != nil {                                                    /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	eventually(t, func() bool { /* 执行当前语句并推进处理流程。 */
		n := 0                       /* 更新 n 的值。 */
		for _, q := range d.queues { /* 循环处理当前数据。 */
			n += q.Rejected() /* 更新 n 的值。 */
		} /* 结束当前表达式或代码块。 */
		return n == 1 /* 返回当前处理结果。 */
	}) /* 结束当前表达式或代码块。 */
	if _, rejected, _ := c.InboxCounts(); rejected != 1 { /* 判断条件并选择处理分支。 */
		t.Fatal("rejection quarantine hidden") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if d.health() != nil { /* 判断条件并选择处理分支。 */
		t.Fatal("isolated rejection blocked healthy receive", d.health()) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	files, err := filepath.Glob(filepath.Join(root, "*", "*.rejected")) /* 更新 err 的值。 */
	if err != nil || len(files) != 1 {                                  /* 判断条件并选择处理分支。 */
		t.Fatal(files, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	b, err := os.ReadFile(files[0]) /* 更新 err 的值。 */
	if err != nil || len(b) == 0 {  /* 判断条件并选择处理分支。 */
		t.Fatal("rejected evidence lost") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestDurableReceiveFullAndCorruptionDoNotClaimSuccess(t *testing.T) { /* 定义 TestDurableReceiveFullAndCorruptionDoNotClaimSuccess 函数。 */
	root := t.TempDir()                 /* 更新 root 的值。 */
	d, err := openInbox(root, 8<<20, 8) /* 更新 err 的值。 */
	if err != nil {                     /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	c := inboxClient(t, d)                                                 /* 更新 c 的值。 */
	topic := "/iot/up/t/p/d/event"                                         /* 更新 topic 的值。 */
	one := &receivedMessage{topic: topic, payload: []byte(`{"id":"one"}`)} /* 更新 one 的值。 */
	c.receive(one)                                                         /* 执行当前语句并推进处理流程。 */
	two := &receivedMessage{topic: topic, payload: []byte(`{"id":"two"}`)} /* 更新 two 的值。 */
	c.receive(two)                                                         /* 执行当前语句并推进处理流程。 */
	if !one.acked.Load() || two.acked.Load() {                             /* 判断条件并选择处理分支。 */
		t.Fatal("capacity failure incorrectly acknowledged") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if !errors.Is(d.health(), durablequeue.ErrQueueFull) { /* 判断条件并选择处理分支。 */
		t.Fatal(d.health()) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	c.cancel()                                                    /* 执行当前语句并推进处理流程。 */
	d.close()                                                     /* 执行当前语句并推进处理流程。 */
	files, _ := filepath.Glob(filepath.Join(root, "*", "*.json")) /* 更新 _ 的值。 */
	if len(files) != 1 {                                          /* 判断条件并选择处理分支。 */
		t.Fatal(files) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err = os.WriteFile(files[0], []byte("broken"), 0600); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	d, err = openInbox(root, 8<<20, 80) /* 更新 err 的值。 */
	if err != nil {                     /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	c = inboxClient(t, d)                                                                             /* 更新 c 的值。 */
	var handled atomic.Int32                                                                          /* 声明 handled。 */
	c.routes["standard"] = func(context.Context, string, []byte) error { handled.Add(1); return nil } /* 检查错误并决定后续处理。 */
	d.start(c)                                                                                        /* 执行当前语句并推进处理流程。 */
	if err = d.put(topic, []byte(`{"id":"valid"}`)); err != nil {                                     /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	eventually(t, func() bool { return handled.Load() == 1 && inboxDepth(d) == 0 })        /* 执行当前语句并推进处理流程。 */
	if b, err := os.ReadFile(files[0] + ".corrupt"); err != nil || string(b) != "broken" { /* 判断条件并选择处理分支。 */
		t.Fatal("corrupt bytes not retained", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, _, corrupt := c.InboxCounts(); corrupt != 1 { /* 判断条件并选择处理分支。 */
		t.Fatal("corruption hidden") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestInboxIdentityPersistsAndDirectoryIsExclusive(t *testing.T) { /* 定义 TestInboxIdentityPersistsAndDirectoryIsExclusive 函数。 */
	root := t.TempDir()                  /* 更新 root 的值。 */
	d, err := openInbox(root, 8<<20, 80) /* 更新 err 的值。 */
	if err != nil {                      /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	defer d.close()                   /* 安排函数结束时执行清理。 */
	first, err := inboxIdentity(root) /* 更新 err 的值。 */
	if err != nil {                   /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	second, err := inboxIdentity(root) /* 更新 err 的值。 */
	if err != nil || first != second { /* 判断条件并选择处理分支。 */
		t.Fatal(first, second, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if other, err := openInbox(root, 8<<20, 80); err == nil { /* 判断条件并选择处理分支。 */
		other.close()                               /* 执行当前语句并推进处理流程。 */
		t.Fatal("two receivers own the same inbox") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	for i := 0; i < 200; i++ { /* 循环处理当前数据。 */
		if err = d.put(fmt.Sprintf("/iot/up/t/p/d%d/event", i), []byte(`{}`)); err != nil { /* 判断条件并选择处理分支。 */
			break /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestDurableReceiptPreservesBytesAndOriginalTime(t *testing.T) { /* 定义 TestDurableReceiptPreservesBytesAndOriginalTime 函数。 */
	root := t.TempDir()                  /* 更新 root 的值。 */
	d, err := openInbox(root, 8<<20, 80) /* 更新 err 的值。 */
	if err != nil {                      /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	defer d.close()                           /* 安排函数结束时执行清理。 */
	topic := "/iot/up/t/p/d/event"            /* 更新 topic 的值。 */
	body := []byte{0xff, 0x00, 'a'}           /* 更新 body 的值。 */
	if err = d.put(topic, body); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	var first model.RawMessage   /* 声明 first。 */
	for _, q := range d.queues { /* 循环处理当前数据。 */
		raw, found, err := q.Next() /* 更新 err 的值。 */
		if err != nil {             /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		if found { /* 判断条件并选择处理分支。 */
			first = raw /* 更新 first 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if first.ReceivedAt <= 0 { /* 判断条件并选择处理分支。 */
		t.Fatal("receipt time missing") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	time.Sleep(2 * time.Millisecond)          /* 执行当前语句并推进处理流程。 */
	if err = d.put(topic, body); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	for _, q := range d.queues { /* 循环处理当前数据。 */
		raw, found, err := q.Next() /* 更新 err 的值。 */
		if err != nil {             /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		if found { /* 判断条件并选择处理分支。 */
			var got []byte                                                                                                            /* 声明 got。 */
			if err = json.Unmarshal(raw.Payload, &got); err != nil || !bytes.Equal(got, body) || raw.ReceivedAt != first.ReceivedAt { /* 判断条件并选择处理分支。 */
				t.Fatal("receipt bytes/time changed", err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestInboxReadFailureIsVisibleAndRecovers(t *testing.T) { /* 定义 TestInboxReadFailureIsVisibleAndRecovers 函数。 */
	root := t.TempDir()                  /* 更新 root 的值。 */
	d, err := openInbox(root, 8<<20, 80) /* 更新 err 的值。 */
	if err != nil {                      /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	c := inboxClient(t, d) /* 更新 c 的值。 */
	// A non-regular queue entry makes Next fail on every platform. Renaming
	// the queue root fails before the test starts on Windows because its
	// lock file remains open for the lifetime of the queue.
	path := filepath.Join(root, "0", "unreadable.json") /* 更新 path 的值。 */
	if err = os.Mkdir(path, 0700); err != nil {         /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	d.start(c)                                              /* 执行当前语句并推进处理流程。 */
	eventually(t, func() bool { return d.health() != nil }) /* 执行当前语句并推进处理流程。 */
	if err = os.Remove(path); err != nil {                  /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	eventually(t, func() bool { return d.health() == nil }) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
