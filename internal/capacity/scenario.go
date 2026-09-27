package capacity

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"math"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"iot-platform/internal/parser"
)

// ShortError classifies transport errors into stable result codes.
func ShortError(err error) string {
	if err == nil {
		return "ok"
	}
	e := err.Error()
	switch {
	case errors.Is(err, context.DeadlineExceeded) || strings.Contains(e, "Timeout") || strings.Contains(e, "timeout") || strings.Contains(e, "deadline"):
		return "timeout"
	case strings.Contains(e, "not Authorized") || strings.Contains(e, "bad user name"):
		return "auth"
	case strings.Contains(e, "refused"):
		return "refused"
	case strings.Contains(e, "reset"):
		return "reset"
	case strings.Contains(e, "broken pipe"):
		return "broken_pipe"
	case strings.Contains(e, "EOF"):
		return "eof"
	case strings.Contains(e, "assign requested address"):
		return "ephemeral_ports"
	case strings.Contains(e, "too many open files"):
		return "fd"
	case strings.Contains(e, "not currently connected"), strings.Contains(e, "connection lost"):
		return "disconnected"
	}
	return "error"
}

// mix hashes deterministic inputs into [0,1) so alarm choice, field values and
// query selection are reproducible from the plan seed.
func mix(parts ...uint64) float64 {
	h := fnv.New64a()
	var b [8]byte
	for _, p := range parts {
		binary.LittleEndian.PutUint64(b[:], p)
		_, _ = h.Write(b[:])
	}
	return float64(h.Sum64()>>11) / float64(1<<53)
}

var telemetryNames = []string{"temperature", "humidity", "pressure", "smokeDensity", "voltage", "current", "signal", "battery", "flow", "level"}

// reportBody builds a deterministic standard report of roughly messageBytes.
func reportBody(clientID string, tsMS int64, fields, messageBytes int, alarm bool, seed int64, seq uint64) []byte {
	data := map[string]any{"stressAlarm": 0}
	if alarm {
		data["stressAlarm"] = 1
	}
	for i := 0; i < fields; i++ {
		name := telemetryNames[i%len(telemetryNames)]
		if i >= len(telemetryNames) {
			name += strconv.Itoa(i / len(telemetryNames))
		}
		data[name] = math.Round(mix(uint64(seed), seq, uint64(i))*10000) / 10
	}
	body := map[string]any{"id": clientID, "timestamp": tsMS, "data": data}
	b, _ := json.Marshal(body)
	if pad := messageBytes - len(b) - len(`,"capPad":""`); pad > 0 {
		data["capPad"] = strings.Repeat("x", pad)
		b, _ = json.Marshal(body)
	}
	return b
}

type sendResult struct {
	ok       bool
	code     string
	bytes    int
	attempts int
	rawID    string
}

func newHTTPClient(timeout time.Duration, conns int) *http.Client {
	return &http.Client{Timeout: timeout, Transport: &http.Transport{
		Proxy:               nil,
		DialContext:         (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		MaxIdleConns:        conns * 2,
		MaxIdleConnsPerHost: conns,
		MaxConnsPerHost:     conns,
		IdleConnTimeout:     60 * time.Second,
		DisableCompression:  true,
	}}
}

// doHTTP reads the full response body before a request counts as successful.
func doHTTP(ctx context.Context, c *http.Client, method, url string, body []byte, hdr map[string]string) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rd)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("body: %w", err)
	}
	return resp.StatusCode, b, nil
}

// sendHTTPReport posts one standard property report with device credentials.
func sendHTTPReport(ctx context.Context, c *http.Client, api string, cfg AgentConfig, d DeviceCredential, body []byte) sendResult {
	url := fmt.Sprintf("%s/api/v1/device-ingest/standard/%s/%s/%s/property", api, cfg.Tenant, cfg.Product, d.ID)
	status, resp, err := doHTTP(ctx, c, http.MethodPost, url, body, map[string]string{"X-Device-Key": d.Key, "X-Device-Secret": d.Secret})
	if err != nil {
		return sendResult{code: ShortError(err), attempts: 1}
	}
	r := sendResult{code: strconv.Itoa(status), attempts: 1}
	if status == http.StatusAccepted {
		var v struct {
			MessageID string `json:"messageId"`
		}
		_ = json.Unmarshal(resp, &v)
		r.ok, r.bytes, r.rawID = true, len(body), v.MessageID
	}
	return r
}

// sendQuery requests one allowlisted management endpoint.
func sendQuery(ctx context.Context, c *http.Client, api, token, path string) sendResult {
	status, resp, err := doHTTP(ctx, c, http.MethodGet, api+path, nil, map[string]string{"Authorization": "Bearer " + token})
	if err != nil {
		return sendResult{code: ShortError(err), attempts: 1}
	}
	return sendResult{ok: status/100 == 2, code: strconv.Itoa(status), bytes: len(resp), attempts: 1}
}

// queryPicker maps a deterministic fraction onto the plan's query mix.
type queryPicker struct {
	names []string
	cum   []float64
}

func newQueryPicker(mixw map[string]float64) queryPicker {
	var p queryPicker
	names := sortedKeys(mixw)
	acc := 0.0
	for _, n := range names {
		if mixw[n] <= 0 {
			continue
		}
		acc += mixw[n]
		p.names = append(p.names, n)
		p.cum = append(p.cum, acc)
	}
	return p
}

func (p queryPicker) pick(f float64) string {
	if len(p.names) == 0 {
		return ""
	}
	i := sort.SearchFloat64s(p.cum, f*p.cum[len(p.cum)-1])
	return p.names[min(i, len(p.names)-1)]
}

// mqttPublisher is one device connection with its receipt subscription.
type mqttPublisher struct {
	device   string
	client   mqtt.Client
	topic    string
	receipts *MQTTReceipts
}

func mqttToken(ctx context.Context, c *http.Client, api string, d DeviceCredential) (username, token, topic, receipt string, err error) {
	status, body, err := doHTTP(ctx, c, http.MethodPost, api+"/api/v1/device-mqtt/token", nil, map[string]string{"X-Device-Key": d.Key, "X-Device-Secret": d.Secret})
	if err != nil {
		return "", "", "", "", err
	}
	var v struct {
		Username     string `json:"username"`
		Token        string `json:"token"`
		PublishTopic string `json:"publishTopic"`
		ReceiptTopic string `json:"receiptTopic"`
	}
	if status != http.StatusOK || json.Unmarshal(body, &v) != nil || v.Token == "" || v.ReceiptTopic == "" {
		return "", "", "", "", fmt.Errorf("token status %d", status)
	}
	return v.Username, v.Token, v.PublishTopic, v.ReceiptTopic, nil
}

func connectMQTT(ctx context.Context, c *http.Client, cfg AgentConfig, d DeviceCredential, runID string) (*mqttPublisher, string) {
	user, tok, topic, receiptTopic, err := mqttToken(ctx, c, cfg.API, d)
	if err != nil {
		return nil, "token_" + ShortError(err)
	}
	timeout := cfg.RequestTimeout.D()
	o := mqtt.NewClientOptions().AddBroker(cfg.MQTT).SetClientID(user + "-cap-" + runID[max(0, len(runID)-6):]).SetUsername(user).SetPassword(tok).
		SetAutoReconnect(false).SetConnectRetry(false).SetConnectTimeout(timeout).SetWriteTimeout(timeout).
		SetKeepAlive(60 * time.Second).SetCleanSession(true)
	client := mqtt.NewClient(o)
	if t := client.Connect(); !t.WaitTimeout(timeout) || t.Error() != nil {
		return nil, "connect_" + ShortError(t.Error())
	}
	p := &mqttPublisher{device: d.ID, client: client, topic: topic, receipts: NewMQTTReceipts()}
	if s := client.Subscribe(receiptTopic, 1, p.receipts.Receive); !s.WaitTimeout(timeout) || s.Error() != nil {
		client.Disconnect(100)
		return nil, "subscribe_receipt"
	}
	return p, ""
}

// tcpDevice is one GB26875 connection; frames on it are strictly sequential.
type tcpDevice struct {
	mu         sync.Mutex
	conn       net.Conn
	source     [6]byte
	seq        uint16
	registered bool
}

var gbLocation = time.FixedZone("UTC+8", 8*60*60)

func readGBFrame(c net.Conn) ([]byte, error) {
	head := make([]byte, 27)
	if _, err := io.ReadFull(c, head); err != nil {
		return nil, err
	}
	size := int(binary.LittleEndian.Uint16(head[24:26]))
	if size > 512 {
		return nil, errors.New("invalid frame length")
	}
	tail := make([]byte, size+3)
	if _, err := io.ReadFull(c, tail); err != nil {
		return nil, err
	}
	return append(head, tail...), nil
}

// sendGB26875 registers the device on first use, then sends a component status
// frame and waits for the platform's ACK (command 0x03 echoing the sequence).
func sendGB26875(ctx context.Context, addr string, d *tcpDevice, alarm bool, node uint16, timeout time.Duration) sendResult {
	d.mu.Lock()
	defer d.mu.Unlock()
	for step := 0; step < 2; step++ {
		if d.conn == nil {
			c, err := (&net.Dialer{Timeout: timeout}).DialContext(ctx, "tcp", addr)
			if err != nil {
				return sendResult{code: "dial_" + ShortError(err), attempts: 1}
			}
			d.conn, d.registered = c, false
		}
		d.seq++
		now := time.Now().In(gbLocation)
		frame := parser.BuildGB26875RegistrationFrame(d.seq, d.source, now)
		register := !d.registered
		if !register {
			status, desc := uint16(1), "capacity normal"
			if alarm {
				status, desc = 2, "capacity fire alarm"
			}
			frame = parser.BuildGB26875ComponentStatusFrame(d.seq, d.source, 1, 1, 23, node%64, node%200, status, desc, now)
		}
		deadline := time.Now().Add(timeout)
		if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
			deadline = dl
		}
		_ = d.conn.SetDeadline(deadline)
		fail := func(prefix string, err error) sendResult {
			d.conn.Close()
			d.conn = nil
			return sendResult{code: prefix + ShortError(err), attempts: 1}
		}
		if _, err := d.conn.Write(frame); err != nil {
			return fail("write_", err)
		}
		for {
			reply, err := readGBFrame(d.conn)
			if err != nil {
				return fail("read_", err)
			}
			if reply[26] == 0x03 && binary.LittleEndian.Uint16(reply[2:4]) == d.seq {
				break
			}
		}
		if register {
			d.registered = true
			continue
		}
		return sendResult{ok: true, code: "ack", bytes: len(frame), attempts: 1}
	}
	return sendResult{code: "register_loop", attempts: 1}
}
