package notify

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"iot-platform/internal/model"
)

type fakeDirectory struct {
	mu     sync.Mutex
	alarms map[string]model.Alarm
	users  []Contact
	duty   []Contact
}

func (d *fakeDirectory) Alarm(_ context.Context, _, id string) (model.Alarm, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	a, ok := d.alarms[id]
	if !ok {
		return a, model.ErrNotFound
	}
	return a, nil
}
func (d *fakeDirectory) DeviceProduct(context.Context, string, string) (string, error) {
	return "smoke-detector", nil
}
func (d *fakeDirectory) UserContacts(context.Context, string, string, []string, []string) ([]Contact, error) {
	return d.users, nil
}
func (d *fakeDirectory) OnDutyContacts(context.Context, string, time.Time, []string) ([]Contact, error) {
	return d.duty, nil
}
func (d *fakeDirectory) setStatus(id, status string) {
	d.mu.Lock()
	a := d.alarms[id]
	a.Status = status
	d.alarms[id] = a
	d.mu.Unlock()
}

type received struct {
	mu     sync.Mutex
	bodies []map[string]any
	header []http.Header
}

func (r *received) handler(response string) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		data, _ := io.ReadAll(req.Body)
		var body map[string]any
		_ = json.Unmarshal(data, &body)
		body["_raw"], body["_query"] = string(data), req.URL.RawQuery
		r.mu.Lock()
		r.bodies = append(r.bodies, body)
		r.header = append(r.header, req.Header.Clone())
		r.mu.Unlock()
		_, _ = w.Write([]byte(response))
	}
}

func (r *received) count() int { r.mu.Lock(); defer r.mu.Unlock(); return len(r.bodies) }

func loopback() []*net.IPNet {
	_, n, _ := net.ParseCIDR("127.0.0.0/8")
	return []*net.IPNet{n}
}

type fixture struct {
	svc   *Service
	store *MemoryStore
	dir   *fakeDirectory
	now   time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	cipher, err := NewCipher("notification-test-secret")
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{store: NewMemoryStore(), dir: &fakeDirectory{alarms: map[string]model.Alarm{}}, now: time.Unix(1_800_000_000, 0)}
	f.svc = &Service{Store: f.store, Directory: f.dir, Cipher: cipher, Sender: NewSender(loopback()), WebURL: "https://iot.example", Now: func() time.Time { return f.now }}
	return f
}

func (f *fixture) channel(t *testing.T, id, kind string, cfg ChannelConfig, secret ChannelSecret) {
	t.Helper()
	sealed, err := f.svc.Cipher.Seal("t", id, secret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.SaveChannel(context.Background(), Channel{ID: id, TenantID: "t", Name: id, Type: kind, Enabled: true, Config: cfg}, &sealed); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) report(t *testing.T, a model.Alarm) {
	t.Helper()
	f.dir.mu.Lock()
	f.dir.alarms[a.ID] = a
	f.dir.mu.Unlock()
	payload, _ := json.Marshal(a)
	if err := f.svc.HandleReported(context.Background(), payload); err != nil {
		t.Fatal(err)
	}
}

func fireAlarm(id string) model.Alarm {
	return model.Alarm{ID: id, TenantID: "t", DeviceID: "d1", DeviceName: "一号楼烟感", AlarmType: "SMOKE_DETECTED", AlarmLevel: "CRITICAL", Status: "ACTIVE", TriggerCount: 1, FirstTriggeredAt: 1_800_000_000_000, ComponentLocation: "3F 东侧走廊"}
}

func TestEscalationStopsWhenAlarmIsAcknowledged(t *testing.T) {
	f := newFixture(t)
	ding, webhook := &received{}, &received{}
	dingServer := httptest.NewServer(ding.handler(`{"errcode":0}`))
	defer dingServer.Close()
	hookServer := httptest.NewServer(webhook.handler(`ok`))
	defer hookServer.Close()
	f.channel(t, "ding", ChannelDingTalk, ChannelConfig{}, ChannelSecret{URL: dingServer.URL + "/robot/send?access_token=tok", SignSecret: "SEC"})
	f.channel(t, "hook", ChannelWebhook, ChannelConfig{}, ChannelSecret{URL: hookServer.URL, SignSecret: "hook-secret"})
	f.dir.duty = []Contact{{Name: "值班员", Phone: "13800000000"}}
	ctx := context.Background()
	if _, err := f.store.SavePolicy(ctx, Policy{ID: "fire", TenantID: "t", Name: "火警", Enabled: true, Levels: []string{"critical"}, Stages: []Stage{
		{ChannelIDs: []string{"ding"}, OnDuty: true},
		{DelaySeconds: 180, ChannelIDs: []string{"hook"}},
		{DelaySeconds: 600, ChannelIDs: []string{"hook"}},
	}}); err != nil {
		t.Fatal(err)
	}
	a := fireAlarm("a1")
	f.report(t, a)
	f.report(t, a) // a redelivered event does not queue twice
	low := fireAlarm("a2")
	low.AlarmLevel = "LOW"
	f.report(t, low)
	repeat := fireAlarm("a3")
	repeat.TriggerCount = 2
	f.report(t, repeat)
	if err := f.svc.DeliverDue(ctx); err != nil {
		t.Fatal(err)
	}
	if ding.count() != 1 || webhook.count() != 0 {
		t.Fatalf("first stage: ding=%d webhook=%d", ding.count(), webhook.count())
	}
	body := ding.bodies[0]
	text := body["text"].(map[string]any)["content"].(string)
	if !strings.Contains(text, "【紧急】检测到烟雾") || !strings.Contains(text, "3F 东侧走廊") || !strings.Contains(text, "https://iot.example/alarms/a1") || !strings.Contains(text, "@13800000000") {
		t.Fatalf("message: %s", text)
	}
	if q := body["_query"].(string); !strings.Contains(q, "access_token=tok") || !strings.Contains(q, "sign=") || !strings.Contains(q, "timestamp=") {
		t.Fatalf("dingtalk signature missing: %s", q)
	}
	// After 3 minutes unacknowledged the second stage is sent and signed.
	f.now = f.now.Add(181 * time.Second)
	if err := f.svc.DeliverDue(ctx); err != nil {
		t.Fatal(err)
	}
	if webhook.count() != 1 {
		t.Fatalf("escalation not sent: %d", webhook.count())
	}
	h := webhook.header[0]
	mac := hmac.New(sha256.New, []byte("hook-secret"))
	mac.Write([]byte(h.Get("X-IoT-Timestamp") + "." + webhook.bodies[0]["_raw"].(string)))
	if h.Get("X-IoT-Signature") != hex.EncodeToString(mac.Sum(nil)) {
		t.Fatal("webhook signature mismatch")
	}
	// Acknowledging the alarm cancels the remaining stage.
	f.dir.setStatus("a1", "ACKED")
	f.now = f.now.Add(10 * time.Minute)
	if err := f.svc.DeliverDue(ctx); err != nil {
		t.Fatal(err)
	}
	if webhook.count() != 1 {
		t.Fatal("an acknowledged alarm must not escalate")
	}
	tasks, _ := f.store.ListAlarmTasks(ctx, "t", "a1")
	statuses := []string{}
	for _, task := range tasks {
		statuses = append(statuses, task.Status)
	}
	if strings.Join(statuses, ",") != "SENT,SENT,CANCELLED" {
		t.Fatalf("timeline %v", statuses)
	}
	if other, _ := f.store.ListAlarmTasks(ctx, "t", "a2"); len(other) != 0 {
		t.Fatal("a policy limited to critical alarms matched a low alarm")
	}
	if other, _ := f.store.ListAlarmTasks(ctx, "t", "a3"); len(other) != 0 {
		t.Fatal("a repeated report must not notify again")
	}
}

func TestFailedDeliveryRetriesThenFails(t *testing.T) {
	f := newFixture(t)
	f.svc.MaxAttempts = 2
	fail := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) }))
	defer fail.Close()
	f.channel(t, "hook", ChannelWebhook, ChannelConfig{}, ChannelSecret{URL: fail.URL})
	ctx := context.Background()
	_, _ = f.store.SavePolicy(ctx, Policy{ID: "p", TenantID: "t", Name: "p", Enabled: true, Stages: []Stage{{ChannelIDs: []string{"hook"}}}})
	f.report(t, fireAlarm("a1"))
	_ = f.svc.DeliverDue(ctx)
	tasks, _ := f.store.ListAlarmTasks(ctx, "t", "a1")
	if tasks[0].Status != StatusPending || tasks[0].Attempts != 1 || !strings.Contains(tasks[0].LastError, "HTTP 500") {
		t.Fatalf("first failure %+v", tasks[0])
	}
	f.now = f.now.Add(time.Hour)
	_ = f.svc.DeliverDue(ctx)
	tasks, _ = f.store.ListAlarmTasks(ctx, "t", "a1")
	if tasks[0].Status != StatusFailed {
		t.Fatalf("final %+v", tasks[0])
	}
}

func TestSenderRefusesInternalAddresses(t *testing.T) {
	sender := NewSender(nil)
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	err := sender.Send(context.Background(), Channel{Type: ChannelWebhook}, ChannelSecret{URL: server.URL + "/?token=abc"}, Message{})
	if err == nil || !strings.Contains(err.Error(), "不在允许范围内") || strings.Contains(err.Error(), "token=abc") {
		t.Fatalf("internal address or token leak: %v", err)
	}
	if err := sender.ValidateTarget(ChannelDingTalk, ChannelConfig{}, ChannelSecret{URL: "https://evil.example/robot"}); err == nil {
		t.Fatal("robot host outside the official API accepted")
	}
	if err := sender.ValidateTarget(ChannelWeCom, ChannelConfig{}, ChannelSecret{URL: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=k"}); err != nil {
		t.Fatal(err)
	}
}

func TestCipherBindsTenantAndChannel(t *testing.T) {
	c, _ := NewCipher("secret")
	sealed, err := c.Seal("t1", "ch", ChannelSecret{URL: "https://x", Password: "p"})
	if err != nil || strings.Contains(sealed, "https://x") {
		t.Fatalf("sealed=%s err=%v", sealed, err)
	}
	if got, err := c.Open("t1", "ch", sealed); err != nil || got.Password != "p" {
		t.Fatalf("open %+v %v", got, err)
	}
	if _, err := c.Open("t2", "ch", sealed); err == nil {
		t.Fatal("a sealed secret opened under another tenant")
	}
}

// fakeSMTP accepts one message and records the conversation.
func fakeSMTP(t *testing.T) (string, chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	out := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		defer ln.Close()
		r := bufio.NewReader(conn)
		write := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }
		write("220 fake")
		var transcript strings.Builder
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			transcript.WriteString(line)
			switch cmd := strings.ToUpper(strings.TrimSpace(line)); {
			case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
				write("250 fake")
			case cmd == "DATA":
				write("354 go")
				for {
					data, err := r.ReadString('\n')
					if err != nil {
						return
					}
					transcript.WriteString(data)
					if data == ".\r\n" {
						break
					}
				}
				write("250 queued")
			case cmd == "QUIT":
				write("221 bye")
				out <- transcript.String()
				return
			default:
				write("250 ok")
			}
		}
	}()
	return ln.Addr().String(), out
}

func TestSMTPChannelSendsToResolvedEmails(t *testing.T) {
	f := newFixture(t)
	addr, transcript := fakeSMTP(t)
	host, port, _ := net.SplitHostPort(addr)
	p := 0
	for _, ch := range port {
		p = p*10 + int(ch-'0')
	}
	f.channel(t, "mail", ChannelSMTP, ChannelConfig{Host: host, Port: p, Security: "none", From: "alarm@iot.example"}, ChannelSecret{})
	f.dir.users = []Contact{{Name: "张三", Email: "zhang@iot.example"}}
	ctx := context.Background()
	_, _ = f.store.SavePolicy(ctx, Policy{ID: "p", TenantID: "t", Name: "p", Enabled: true, Stages: []Stage{{ChannelIDs: []string{"mail"}, Users: []string{"zhang"}, Emails: []string{"duty@iot.example"}}}})
	f.report(t, fireAlarm("a1"))
	if err := f.svc.DeliverDue(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-transcript:
		if !strings.Contains(got, "RCPT TO:<zhang@iot.example>") || !strings.Contains(got, "RCPT TO:<duty@iot.example>") || !strings.Contains(got, "Subject: =?UTF-8?b?") {
			t.Fatalf("smtp transcript: %s", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no mail sent")
	}
	tasks, _ := f.store.ListAlarmTasks(ctx, "t", "a1")
	if tasks[0].Status != StatusSent || strings.Join(tasks[0].Recipients, ",") != "张三" {
		t.Fatalf("task %+v", tasks[0])
	}
}

func TestValidatePolicy(t *testing.T) {
	channels := map[string]Channel{"c": {}}
	ok := Policy{ID: "p", Name: "火警", Stages: []Stage{{ChannelIDs: []string{"c"}}, {DelaySeconds: 60, ChannelIDs: []string{"c"}, Emails: []string{"a@b.cn"}}}}
	if err := ValidatePolicy(ok, channels); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Policy){
		"delayed first stage": func(p *Policy) { p.Stages[0].DelaySeconds = 10 },
		"non increasing":      func(p *Policy) { p.Stages[1].DelaySeconds = 0 },
		"unknown channel":     func(p *Policy) { p.Stages[1].ChannelIDs = []string{"x"} },
		"bad email":           func(p *Policy) { p.Stages[1].Emails = []string{"nope"} },
	} {
		p := ok
		p.Stages = append([]Stage(nil), ok.Stages...)
		mutate(&p)
		if ValidatePolicy(p, channels) == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}
