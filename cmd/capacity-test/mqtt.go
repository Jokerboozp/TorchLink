package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	mrand "math/rand/v2"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

var (
	receiptConfirm = flag.Bool("mqtt-confirm", true, "mqttpub: wait for platform archive receipt, retry the same message; false measures broker PUBACK only")
	receiptWait    = flag.Duration("receipt-wait", 5*time.Second, "mqttpub: time before retrying an unconfirmed message")
	receiptRetries = flag.Int("receipt-retries", 2, "mqttpub: same-message retry attempts after a missing archive receipt")
	mqttAddr       = flag.String("mqtt", "tcp://127.0.0.1:1883", "MQTT broker address")
	tokFile        = flag.String("mqtt-tokens", "", "MQTT token file written by -mode mqtttok")
	connStep       = flag.Int("conn-step", 2000, "mqttconn: connections added per step; mqttpub: publisher connections")
	connPar        = flag.Int("conn-par", 200, "parallel MQTT connect attempts")
	connMax        = flag.Int("conn-max", 1000000, "mqttconn: stop after this many connections")
	holdDur        = flag.Duration("hold", 30*time.Second, "mqttconn: how long to hold the connections after the ramp")
	cidSuffix      = flag.String("cid-suffix", "", "mqttconn: client ID suffix, to open extra connections per token for broker-only tests")
	qos            = flag.Int("qos", 1, "mqttpub: QoS")
	mqttLocalIPs   = flag.String("mqtt-local-ips", "", "comma-separated local source IPs for MQTT TCP load generators (addresses must be valid on the generator)")
)

var mqttDialSeq atomic.Uint64

type mqttToken struct {
	Username     string `json:"username"`
	Token        string `json:"token"`
	Topic        string `json:"topic"`
	Device       string `json:"device"`
	ReceiptTopic string `json:"receiptTopic"`
}

// mqttTokens fetches POST /api/v1/device-mqtt/token for every device in
// -devices and writes the tokens to -out (mode 0600). Device tokens are short
// lived, so fetch them right before a connection or publish test.
func mqttTokens() {
	devs := loadDevices()
	c := httpClient(64)
	out := make([]mqttToken, len(devs))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 64)
	var fails atomic.Int64
	st := time.Now()
	for i, d := range devs {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, d Device) {
			defer wg.Done()
			defer func() { <-sem }()
			req, _ := http.NewRequest(http.MethodPost, *base+"/api/v1/device-mqtt/token", nil)
			req.Header.Set("X-Device-Key", d.Key)
			req.Header.Set("X-Device-Secret", d.Secret)
			resp, err := c.Do(req)
			if err != nil {
				fails.Add(1)
				return
			}
			defer resp.Body.Close()
			var v struct {
				Username     string `json:"username"`
				Token        string `json:"token"`
				PublishTopic string `json:"publishTopic"`
				ReceiptTopic string `json:"receiptTopic"`
			}
			if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&v) != nil {
				fails.Add(1)
				return
			}
			out[i] = mqttToken{Username: v.Username, Token: v.Token, Topic: v.PublishTopic, Device: d.Device, ReceiptTopic: v.ReceiptTopic}
		}(i, d)
	}
	wg.Wait()
	b, _ := json.Marshal(out)
	must(os.WriteFile(*outFile, b, 0o600))
	fmt.Printf("tokens=%d fails=%d elapsed=%s rate=%.1f/s\n", len(devs), fails.Load(), time.Since(st).Round(time.Millisecond), float64(len(devs))/time.Since(st).Seconds())
	if fails.Load() > 0 {
		must(fmt.Errorf("%d MQTT tokens could not be issued; refresh the token file before publishing", fails.Load()))
	}
}

func loadTokens() []mqttToken {
	b, err := os.ReadFile(*tokFile)
	must(err)
	var all []mqttToken
	must(json.Unmarshal(b, &all))
	var out []mqttToken
	for _, t := range all {
		if t.Token != "" {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		must(fmt.Errorf("no tokens in %s", *tokFile))
	}
	return out
}

func newMQTT(t mqttToken, clientID string) mqtt.Client {
	o := mqtt.NewClientOptions().AddBroker(*mqttAddr).SetClientID(clientID).SetUsername(t.Username).SetPassword(t.Token).
		SetAutoReconnect(false).SetConnectRetry(false).SetConnectTimeout(*timeout).SetWriteTimeout(*timeout).
		SetKeepAlive(120 * time.Second).SetCleanSession(true)
	if *mqttLocalIPs != "" {
		addresses := strings.Split(*mqttLocalIPs, ",")
		address := strings.TrimSpace(addresses[(mqttDialSeq.Add(1)-1)%uint64(len(addresses))])
		ip := net.ParseIP(address)
		if ip == nil {
			must(fmt.Errorf("invalid MQTT local source IP %q", address))
		}
		o.SetDialer(&net.Dialer{Timeout: *timeout, LocalAddr: &net.TCPAddr{IP: ip}})
	}
	return mqtt.NewClient(o)
}

func countOpen(clients []mqtt.Client) int {
	n := 0
	for _, c := range clients {
		if c.IsConnectionOpen() {
			n++
		}
	}
	return n
}

// mqttConn adds -conn-step connections per step and keeps all of them open,
// until connect failures dominate, then holds them for -hold.
func mqttConn() {
	toks := loadTokens()
	var clients []mqtt.Client
	var levels []Level
	idx := 0
	for len(clients) < *connMax && idx < len(toks) {
		r := &rec{}
		st := time.Now()
		var mu sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, *connPar)
		for k := 0; k < *connStep && idx < len(toks); k++ {
			t := toks[idx]
			idx++
			wg.Add(1)
			sem <- struct{}{}
			go func(t mqttToken) {
				defer wg.Done()
				defer func() { <-sem }()
				c := newMQTT(t, t.Username+*cidSuffix)
				s := time.Now()
				tk := c.Connect()
				ok := tk.WaitTimeout(*timeout) && tk.Error() == nil
				code := "ok"
				if !ok {
					code = "timeout"
					if tk.Error() != nil {
						code = shortErr(tk.Error().Error())
					}
				}
				r.add(float64(time.Since(s).Microseconds())/1000, ok, code, 0)
				if ok {
					mu.Lock()
					clients = append(clients, c)
					mu.Unlock()
				}
			}(t)
		}
		wg.Wait()
		l := r.level(fmt.Sprintf("conns=%d", len(clients)), *connPar, 0, time.Since(st))
		alive := countOpen(clients)
		l.Note = fmt.Sprintf("held=%d alive=%d", len(clients), alive)
		levels = append(levels, l)
		fmt.Printf("[%s] held=%d alive=%d step ok=%d fail=%d connect/s=%.0f p50=%.1f p95=%.1f p99=%.1f codes=%v\n", *name, len(clients), alive, l.OK, l.Fail, l.QPS, l.P50, l.P95, l.P99, l.Codes)
		save(levels)
		if l.ErrRate > *stopErr {
			fmt.Printf("[%s] stop: connect failures dominate\n", *name)
			break
		}
		time.Sleep(2 * time.Second)
	}
	fmt.Printf("[%s] holding %d connections for %s\n", *name, len(clients), *holdDur)
	for el := time.Duration(0); el < *holdDur; el += 30 * time.Second {
		time.Sleep(30 * time.Second)
		fmt.Printf("[%s] %s alive=%d/%d\n", *name, time.Now().Format("15:04:05"), countOpen(clients), len(clients))
	}
	alive := countOpen(clients)
	fmt.Printf("[%s] after hold: alive=%d/%d\n", *name, alive, len(clients))
	save(append(levels, Level{Label: "final", Note: fmt.Sprintf("alive=%d/%d", alive, len(clients))}))
	for _, c := range clients {
		c.Disconnect(10)
	}
}

// mqttPub connects -conn-step publishers and publishes at the -rates steps.
// PUBACK only proves the broker accepted a message; compare archived/s and the
// broker's dropped counters for the platform subscriber to see what arrived.
func mqttPub() {
	if *receiptWait <= 0 || *receiptRetries < 0 || *receiptRetries > 10 {
		must(fmt.Errorf("receipt-wait must be positive and receipt-retries between 0 and 10"))
	}
	toks := loadTokens()
	n := *connStep
	if n <= 0 || len(toks) < n {
		must(fmt.Errorf("requested %d MQTT publishers but only %d valid tokens are available", n, len(toks)))
	}
	var clients []mqtt.Client
	var topics []string
	var receipts []*mqttReceipts
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, *connPar)
	for _, t := range toks[:n] {
		wg.Add(1)
		sem <- struct{}{}
		go func(t mqttToken) {
			defer wg.Done()
			defer func() { <-sem }()
			c := newMQTT(t, t.Username+"-pub")
			if tk := c.Connect(); tk.WaitTimeout(*timeout) && tk.Error() == nil {
				tracker := newMQTTReceipts()
				if *receiptConfirm {
					if t.ReceiptTopic == "" {
						c.Disconnect(10)
						return
					}
					sub := c.Subscribe(t.ReceiptTopic, 1, tracker.receive)
					if !sub.WaitTimeout(*timeout) || sub.Error() != nil {
						c.Disconnect(10)
						return
					}
				}
				mu.Lock()
				receipts = append(receipts, tracker)
				clients, topics = append(clients, c), append(topics, t.Topic)
				mu.Unlock()
			}
		}(t)
	}
	wg.Wait()
	if len(clients) != n {
		for _, c := range clients {
			c.Disconnect(10)
		}
		must(fmt.Errorf("only %d/%d MQTT publishers connected; refusing to measure a different device population", len(clients), n))
	}
	fmt.Printf("[%s] connected %d publishers\n", *name, len(clients))
	var rr atomic.Int64
	save(runLevels(func(ctx context.Context, _ *http.Client, _ int) (bool, string, int64) {
		i := int(rr.Add(1)) % len(clients)
		b, _ := json.Marshal(map[string]any{"id": "mq-" + rid(), "timestamp": time.Now().UnixMilli(), "data": telemetry(mrand.Float64() < *alarmFrac)})
		if *receiptConfirm {
			return receipts[i].publish(ctx, clients[i], topics[i], b)
		}
		tk := clients[i].Publish(topics[i], byte(*qos), false, b)
		select {
		case <-ctx.Done():
			return false, "timeout", 0
		case <-tk.Done():
			if tk.Error() != nil {
				return false, shortErr(tk.Error().Error()), 0
			}
			return true, "puback", int64(len(b))
		}
	}))
	for _, c := range clients {
		c.Disconnect(10)
	}
}
