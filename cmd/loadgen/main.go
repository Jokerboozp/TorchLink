package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type result struct {
	Profile       string  `json:"profile"`
	Transport     string  `json:"transport"`
	DurationSec   float64 `json:"durationSeconds"`
	Sent          uint64  `json:"sent"`
	Failed        uint64  `json:"failed"`
	AchievedQPS   float64 `json:"achievedQps"`
	ErrorRate     float64 `json:"errorRate"`
	LatencyP50MS  float64 `json:"latencyP50Ms"`
	LatencyP95MS  float64 `json:"latencyP95Ms"`
	LatencyP99MS  float64 `json:"latencyP99Ms"`
	MaximumMS     float64 `json:"maximumMs"`
	AcceptanceOK  bool    `json:"acceptanceOk"`
	AcceptanceMsg string  `json:"acceptanceMessage"`
}

type sender interface {
	Send(context.Context, int, []byte) error
	Close()
}
type httpSender struct {
	client          *http.Client
	endpoint, token string
}

func (s *httpSender) Send(ctx context.Context, _ int, body []byte) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.token)
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}
func (*httpSender) Close() {}

type mqttSender struct {
	client mqtt.Client
	prefix string
	qos    byte
}

func (s *mqttSender) Send(ctx context.Context, n int, body []byte) error {
	token := s.client.Publish(fmt.Sprintf("%sdevice_%06d", s.prefix, n), s.qos, false, body)
	done := make(chan struct{})
	go func() { token.Wait(); close(done) }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return token.Error()
	}
}
func (s *mqttSender) Close() { s.client.Disconnect(500) }

var latencyBounds = []int64{1, 2, 5, 10, 20, 50, 100, 200, 500, 1000, 2000, 5000, 10000, 30000, 60000}

type histogram struct {
	buckets []atomic.Uint64
	maximum atomic.Int64
}

func newHistogram() *histogram {
	return &histogram{buckets: make([]atomic.Uint64, len(latencyBounds)+1)}
}
func (h *histogram) observe(d time.Duration) {
	ms := d.Milliseconds()
	i := sort.Search(len(latencyBounds), func(i int) bool { return latencyBounds[i] >= ms })
	h.buckets[i].Add(1)
	for {
		old := h.maximum.Load()
		if ms <= old || h.maximum.CompareAndSwap(old, ms) {
			break
		}
	}
}
func (h *histogram) percentile(q float64) float64 {
	var total uint64
	for i := range h.buckets {
		total += h.buckets[i].Load()
	}
	if total == 0 {
		return 0
	}
	target := uint64(float64(total) * q)
	if target == 0 {
		target = 1
	}
	var seen uint64
	for i := range h.buckets {
		seen += h.buckets[i].Load()
		if seen >= target {
			if i < len(latencyBounds) {
				return float64(latencyBounds[i])
			}
			return float64(h.maximum.Load())
		}
	}
	return float64(h.maximum.Load())
}

func main() {
	target := flag.String("url", "http://localhost:8080", "platform URL")
	token := flag.String("token", os.Getenv("IOT_TOKEN"), "platform admin JWT")
	transport := flag.String("transport", "http", "http or mqtt")
	broker := flag.String("mqtt-broker", "tcp://localhost:1883", "MQTT broker")
	profile := flag.String("profile", "steady", "steady, burst, or offline")
	rate := flag.Int("rate", 1000, "normal generated messages per second")
	burstRate := flag.Int("burst-rate", 50000, "burst/recovery send rate")
	duration := flag.Duration("duration", time.Minute, "test duration")
	burstDuration := flag.Duration("burst-duration", 5*time.Minute, "burst window")
	offlineDuration := flag.Duration("offline-duration", time.Minute, "offline buffering window")
	devices := flag.Int("devices", 10000, "device cardinality")
	workers := flag.Int("workers", 64, "concurrent workers")
	tenant := flag.String("tenant", "tenant_001", "tenant ID")
	product := flag.String("product", "fire_smoke_json", "product ID")
	reportPath := flag.String("report", "", "optional JSON report path")
	maxErrorRate := flag.Float64("max-error-rate", 0.001, "acceptance error-rate ceiling")
	maxP95 := flag.Float64("max-p95-ms", 500, "acceptance P95 latency ceiling")
	minQPS := flag.Float64("min-qps", 0, "acceptance achieved QPS floor; zero uses 90% of normal rate")
	flag.Parse()
	if *rate <= 0 || *workers <= 0 || *devices <= 0 {
		exitError("rate, workers and devices must be positive")
	}
	if !map[string]bool{"steady": true, "burst": true, "offline": true}[*profile] {
		exitError("profile must be steady, burst, or offline")
	}

	ctx, cancel := context.WithTimeout(context.Background(), *duration)
	defer cancel()
	var out sender
	var err error
	if *transport == "mqtt" {
		out, err = newMQTTSender(ctx, *target, *token, *broker, *product)
	} else {
		out = &httpSender{client: &http.Client{Timeout: 30 * time.Second}, endpoint: strings.TrimRight(*target, "/") + "/api/v1/raw-messages", token: *token}
	}
	if err != nil {
		exitError(err.Error())
	}
	defer out.Close()
	jobs := make(chan int, max(1024, *workers*64))
	var ok, failed atomic.Uint64
	hist := newHistogram()
	var wg sync.WaitGroup
	for w := 0; w < *workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := range jobs {
				body := payload(n, *devices, *tenant, *product)
				started := time.Now()
				sendCtx, stop := context.WithTimeout(context.Background(), 30*time.Second)
				sendErr := out.Send(sendCtx, n%*devices, body)
				stop()
				hist.observe(time.Since(started))
				if sendErr == nil {
					ok.Add(1)
				} else {
					failed.Add(1)
				}
			}
		}()
	}
	start := time.Now()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	scheduled := 0
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case now := <-ticker.C:
			targetCount := curveTarget(*profile, now.Sub(start), *duration, *rate, *burstRate, *burstDuration, *offlineDuration)
			for scheduled < targetCount {
				select {
				case jobs <- scheduled:
					scheduled++
				case <-ctx.Done():
					break loop
				}
			}
		}
	}
	close(jobs)
	wg.Wait()
	elapsed := time.Since(start).Seconds()
	total := ok.Load() + failed.Load()
	errorRate := 0.0
	if total > 0 {
		errorRate = float64(failed.Load()) / float64(total)
	}
	minimum := *minQPS
	if minimum == 0 {
		minimum = float64(*rate) * 0.9
	}
	achieved := float64(ok.Load()) / elapsed
	p95 := hist.percentile(.95)
	accepted := errorRate <= *maxErrorRate && p95 <= *maxP95 && achieved >= minimum
	message := "accepted"
	if !accepted {
		message = fmt.Sprintf("threshold failed: qps %.0f/%.0f, error %.4f/%.4f, p95 %.0f/%.0fms", achieved, minimum, errorRate, *maxErrorRate, p95, *maxP95)
	}
	report := result{Profile: *profile, Transport: *transport, DurationSec: elapsed, Sent: ok.Load(), Failed: failed.Load(), AchievedQPS: achieved, ErrorRate: errorRate, LatencyP50MS: hist.percentile(.50), LatencyP95MS: p95, LatencyP99MS: hist.percentile(.99), MaximumMS: float64(hist.maximum.Load()), AcceptanceOK: accepted, AcceptanceMsg: message}
	data, _ := json.MarshalIndent(report, "", "  ")
	fmt.Println(string(data))
	if *reportPath != "" {
		if writeErr := os.WriteFile(*reportPath, data, 0o600); writeErr != nil {
			exitError(writeErr.Error())
		}
	}
	if !accepted {
		os.Exit(2)
	}
}

func curveTarget(profile string, elapsed, total time.Duration, normal, burst int, burstWindow, offline time.Duration) int {
	seconds := elapsed.Seconds()
	switch profile {
	case "burst":
		start := total / 4
		end := start + burstWindow
		if end > total {
			end = total
		}
		if elapsed <= start {
			return int(seconds * float64(normal))
		}
		before := start.Seconds() * float64(normal)
		if elapsed <= end {
			return int(before + (elapsed-start).Seconds()*float64(burst))
		}
		return int(before + (end-start).Seconds()*float64(burst) + (elapsed-end).Seconds()*float64(normal))
	case "offline":
		if elapsed <= offline {
			return 0
		}
		generated := int(seconds * float64(normal))
		recovery := int((elapsed - offline).Seconds() * float64(burst))
		if recovery < generated {
			return recovery
		}
		return generated
	default:
		return int(seconds * float64(normal))
	}
}
func payload(n, devices int, tenant, product string) []byte {
	body, _ := json.Marshal(map[string]any{"messageId": fmt.Sprintf("load_%d_%d", time.Now().UnixNano(), n), "tenantId": tenant, "productId": product, "deviceId": fmt.Sprintf("device_%06d", n%devices), "protocol": "json", "payloadFormat": "json", "payload": map[string]any{"properties": map[string]any{"temperature": 20 + n%80, "smoke": n%1000 == 0}, "tags": map[string]string{"cityCode": "city_001", "districtCode": "district_01", "buildingId": fmt.Sprintf("building_%02d", n%100), "deviceType": "smoke"}}})
	return body
}
func newMQTTSender(ctx context.Context, platformURL, platformToken, broker, product string) (sender, error) {
	body, _ := json.Marshal(map[string]string{"productId": product})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(platformURL, "/")+"/api/v1/mqtt/load-token", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+platformToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var auth struct{ Username, Token, PublishTopicPrefix string }
	if err = json.NewDecoder(resp.Body).Decode(&auth); err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("load token HTTP %d", resp.StatusCode)
	}
	opts := mqtt.NewClientOptions().AddBroker(broker).SetClientID(fmt.Sprintf("iot-loadgen-%d", time.Now().UnixNano())).SetUsername(auth.Username).SetPassword(auth.Token).SetOrderMatters(false)
	client := mqtt.NewClient(opts)
	connect := client.Connect()
	if !connect.WaitTimeout(15 * time.Second) {
		return nil, fmt.Errorf("MQTT connect timeout")
	}
	if connect.Error() != nil {
		return nil, connect.Error()
	}
	return &mqttSender{client: client, prefix: auth.PublishTopicPrefix, qos: 1}, nil
}
func exitError(message string) { fmt.Fprintln(os.Stderr, message); os.Exit(1) }
