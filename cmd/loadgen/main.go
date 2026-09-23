package main /* 声明 main 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bytes"         /* 执行当前语句并推进处理流程。 */
	"context"       /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"flag"          /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */
	"net/http"      /* 执行当前语句并推进处理流程。 */
	"os"            /* 执行当前语句并推进处理流程。 */
	"sort"          /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */
	"sync"          /* 执行当前语句并推进处理流程。 */
	"sync/atomic"   /* 执行当前语句并推进处理流程。 */
	"time"          /* 执行当前语句并推进处理流程。 */

	mqtt "github.com/eclipse/paho.mqtt.golang" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type result struct { /* 定义 result 类型。 */
	Profile       string  `json:"profile"`           /* 执行当前语句并推进处理流程。 */
	Transport     string  `json:"transport"`         /* 执行当前语句并推进处理流程。 */
	DurationSec   float64 `json:"durationSeconds"`   /* 执行当前语句并推进处理流程。 */
	Sent          uint64  `json:"sent"`              /* 执行当前语句并推进处理流程。 */
	Failed        uint64  `json:"failed"`            /* 执行当前语句并推进处理流程。 */
	AchievedQPS   float64 `json:"achievedQps"`       /* 执行当前语句并推进处理流程。 */
	ErrorRate     float64 `json:"errorRate"`         /* 执行当前语句并推进处理流程。 */
	LatencyP50MS  float64 `json:"latencyP50Ms"`      /* 执行当前语句并推进处理流程。 */
	LatencyP95MS  float64 `json:"latencyP95Ms"`      /* 执行当前语句并推进处理流程。 */
	LatencyP99MS  float64 `json:"latencyP99Ms"`      /* 执行当前语句并推进处理流程。 */
	MaximumMS     float64 `json:"maximumMs"`         /* 执行当前语句并推进处理流程。 */
	AcceptanceOK  bool    `json:"acceptanceOk"`      /* 执行当前语句并推进处理流程。 */
	AcceptanceMsg string  `json:"acceptanceMessage"` /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type sender interface { /* 定义 sender 类型。 */
	Send(context.Context, int, []byte) error /* 执行当前语句并推进处理流程。 */
	Close()                                  /* 执行当前语句并推进处理流程。 */
}                        /* 结束当前表达式或代码块。 */
type httpSender struct { /* 定义 httpSender 类型。 */
	client          *http.Client /* 执行当前语句并推进处理流程。 */
	endpoint, token string       /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (s *httpSender) Send(ctx context.Context, _ int, body []byte) error { /* 定义 Send 函数。 */
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body)) /* 更新 _ 的值。 */
	req.Header.Set("Content-Type", "application/json")                                            /* 执行当前语句并推进处理流程。 */
	req.Header.Set("Authorization", "Bearer "+s.token)                                            /* 执行当前语句并推进处理流程。 */
	resp, err := s.client.Do(req)                                                                 /* 更新 err 的值。 */
	if err != nil {                                                                               /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	resp.Body.Close()             /* 执行当前语句并推进处理流程。 */
	if resp.StatusCode/100 != 2 { /* 判断条件并选择处理分支。 */
		return fmt.Errorf("HTTP %d", resp.StatusCode) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
}                          /* 结束当前表达式或代码块。 */
func (*httpSender) Close() {} /* 定义 Close 函数。 */

type mqttSender struct { /* 定义 mqttSender 类型。 */
	client mqtt.Client /* 执行当前语句并推进处理流程。 */
	prefix string      /* 执行当前语句并推进处理流程。 */
	qos    byte        /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (s *mqttSender) Send(ctx context.Context, n int, body []byte) error { /* 定义 Send 函数。 */
	token := s.client.Publish(fmt.Sprintf("%sdevice_%06d", s.prefix, n), s.qos, false, body) /* 更新 token 的值。 */
	done := make(chan struct{})                                                              /* 更新 done 的值。 */
	go func() { token.Wait(); close(done) }()                                                /* 执行当前语句并推进处理流程。 */
	select {                                                                                 /* 根据条件选择处理路径。 */
	case <-ctx.Done(): /* 处理当前分支。 */
		return ctx.Err() /* 返回当前处理结果。 */
	case <-done: /* 处理当前分支。 */
		return token.Error() /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
}                            /* 结束当前表达式或代码块。 */
func (s *mqttSender) Close() { s.client.Disconnect(500) } /* 定义 Close 函数。 */

var latencyBounds = []int64{1, 2, 5, 10, 20, 50, 100, 200, 500, 1000, 2000, 5000, 10000, 30000, 60000} /* 声明 latencyBounds。 */

type histogram struct { /* 定义 histogram 类型。 */
	buckets []atomic.Uint64 /* 执行当前语句并推进处理流程。 */
	maximum atomic.Int64    /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func newHistogram() *histogram { /* 定义 newHistogram 函数。 */
	return &histogram{buckets: make([]atomic.Uint64, len(latencyBounds)+1)} /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (h *histogram) observe(d time.Duration) { /* 定义 observe 函数。 */
	ms := d.Milliseconds()                                                                   /* 更新 ms 的值。 */
	i := sort.Search(len(latencyBounds), func(i int) bool { return latencyBounds[i] >= ms }) /* 更新 i 的值。 */
	h.buckets[i].Add(1)                                                                      /* 执行当前语句并推进处理流程。 */
	for {                                                                                    /* 循环处理当前数据。 */
		old := h.maximum.Load()                             /* 更新 old 的值。 */
		if ms <= old || h.maximum.CompareAndSwap(old, ms) { /* 判断条件并选择处理分支。 */
			break /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
func (h *histogram) percentile(q float64) float64 { /* 定义 percentile 函数。 */
	var total uint64           /* 声明 total。 */
	for i := range h.buckets { /* 循环处理当前数据。 */
		total += h.buckets[i].Load() /* 更新 total 的值。 */
	} /* 结束当前表达式或代码块。 */
	if total == 0 { /* 判断条件并选择处理分支。 */
		return 0 /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	target := uint64(float64(total) * q) /* 更新 target 的值。 */
	if target == 0 {                     /* 判断条件并选择处理分支。 */
		target = 1 /* 更新 target 的值。 */
	} /* 结束当前表达式或代码块。 */
	var seen uint64            /* 声明 seen。 */
	for i := range h.buckets { /* 循环处理当前数据。 */
		seen += h.buckets[i].Load() /* 更新 seen 的值。 */
		if seen >= target {         /* 判断条件并选择处理分支。 */
			if i < len(latencyBounds) { /* 判断条件并选择处理分支。 */
				return float64(latencyBounds[i]) /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			return float64(h.maximum.Load()) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return float64(h.maximum.Load()) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func main() { /* 定义 main 函数。 */
	target := flag.String("url", "http://localhost:8080", "platform URL")                               /* 更新 target 的值。 */
	token := flag.String("token", os.Getenv("IOT_TOKEN"), "platform admin JWT")                         /* 更新 token 的值。 */
	transport := flag.String("transport", "http", "http or mqtt")                                       /* 更新 transport 的值。 */
	broker := flag.String("mqtt-broker", "tcp://localhost:1883", "MQTT broker")                         /* 更新 broker 的值。 */
	profile := flag.String("profile", "steady", "steady, burst, or offline")                            /* 更新 profile 的值。 */
	rate := flag.Int("rate", 1000, "normal generated messages per second")                              /* 更新 rate 的值。 */
	burstRate := flag.Int("burst-rate", 50000, "burst/recovery send rate")                              /* 更新 burstRate 的值。 */
	duration := flag.Duration("duration", time.Minute, "test duration")                                 /* 更新 duration 的值。 */
	burstDuration := flag.Duration("burst-duration", 5*time.Minute, "burst window")                     /* 更新 burstDuration 的值。 */
	offlineDuration := flag.Duration("offline-duration", time.Minute, "offline buffering window")       /* 更新 offlineDuration 的值。 */
	devices := flag.Int("devices", 10000, "device cardinality")                                         /* 更新 devices 的值。 */
	workers := flag.Int("workers", 64, "concurrent workers")                                            /* 更新 workers 的值。 */
	tenant := flag.String("tenant", "tenant_001", "tenant ID")                                          /* 更新 tenant 的值。 */
	product := flag.String("product", "fire_smoke_json", "product ID")                                  /* 更新 product 的值。 */
	reportPath := flag.String("report", "", "optional JSON report path")                                /* 更新 reportPath 的值。 */
	maxErrorRate := flag.Float64("max-error-rate", 0.001, "acceptance error-rate ceiling")              /* 更新 maxErrorRate 的值。 */
	maxP95 := flag.Float64("max-p95-ms", 500, "acceptance P95 latency ceiling")                         /* 更新 maxP95 的值。 */
	minQPS := flag.Float64("min-qps", 0, "acceptance achieved QPS floor; zero uses 90% of normal rate") /* 更新 minQPS 的值。 */
	flag.Parse()                                                                                        /* 执行当前语句并推进处理流程。 */
	if *rate <= 0 || *workers <= 0 || *devices <= 0 {                                                   /* 判断条件并选择处理分支。 */
		exitError("rate, workers and devices must be positive") /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if !map[string]bool{"steady": true, "burst": true, "offline": true}[*profile] { /* 判断条件并选择处理分支。 */
		exitError("profile must be steady, burst, or offline") /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */

	ctx, cancel := context.WithTimeout(context.Background(), *duration) /* 更新 cancel 的值。 */
	defer cancel()                                                      /* 安排函数结束时执行清理。 */
	var out sender                                                      /* 声明 out。 */
	var err error                                                       /* 声明 err。 */
	if *transport == "mqtt" {                                           /* 判断条件并选择处理分支。 */
		out, err = newMQTTSender(ctx, *target, *token, *broker, *product) /* 更新 err 的值。 */
	} else { /* 结束当前表达式或代码块。 */
		out = &httpSender{client: &http.Client{Timeout: 30 * time.Second}, endpoint: strings.TrimRight(*target, "/") + "/api/v1/raw-messages", token: *token} /* 更新 out 的值。 */
	} /* 结束当前表达式或代码块。 */
	if err != nil { /* 判断条件并选择处理分支。 */
		exitError(err.Error()) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	defer out.Close()                              /* 安排函数结束时执行清理。 */
	jobs := make(chan int, max(1024, *workers*64)) /* 更新 jobs 的值。 */
	var ok, failed atomic.Uint64                   /* 声明 ok。 */
	hist := newHistogram()                         /* 更新 hist 的值。 */
	var wg sync.WaitGroup                          /* 声明 wg。 */
	for w := 0; w < *workers; w++ {                /* 循环处理当前数据。 */
		wg.Add(1)   /* 执行当前语句并推进处理流程。 */
		go func() { /* 执行当前语句并推进处理流程。 */
			defer wg.Done()       /* 安排函数结束时执行清理。 */
			for n := range jobs { /* 循环处理当前数据。 */
				body := payload(n, *devices, *tenant, *product)                            /* 更新 body 的值。 */
				started := time.Now()                                                      /* 更新 started 的值。 */
				sendCtx, stop := context.WithTimeout(context.Background(), 30*time.Second) /* 更新 stop 的值。 */
				sendErr := out.Send(sendCtx, n%*devices, body)                             /* 更新 sendErr 的值。 */
				stop()                                                                     /* 执行当前语句并推进处理流程。 */
				hist.observe(time.Since(started))                                          /* 执行当前语句并推进处理流程。 */
				if sendErr == nil {                                                        /* 判断条件并选择处理分支。 */
					ok.Add(1) /* 执行当前语句并推进处理流程。 */
				} else { /* 结束当前表达式或代码块。 */
					failed.Add(1) /* 执行当前语句并推进处理流程。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
		}() /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	start := time.Now()                             /* 更新 start 的值。 */
	ticker := time.NewTicker(10 * time.Millisecond) /* 更新 ticker 的值。 */
	defer ticker.Stop()                             /* 安排函数结束时执行清理。 */
	scheduled := 0                                  /* 更新 scheduled 的值。 */
loop: /* 执行当前语句并推进处理流程。 */
	for { /* 循环处理当前数据。 */
		select { /* 根据条件选择处理路径。 */
		case <-ctx.Done(): /* 处理当前分支。 */
			break loop /* 执行当前语句并推进处理流程。 */
		case now := <-ticker.C: /* 处理当前分支。 */
			targetCount := curveTarget(*profile, now.Sub(start), *duration, *rate, *burstRate, *burstDuration, *offlineDuration) /* 更新 targetCount 的值。 */
			for scheduled < targetCount {                                                                                        /* 循环处理当前数据。 */
				select { /* 根据条件选择处理路径。 */
				case jobs <- scheduled: /* 处理当前分支。 */
					scheduled++ /* 执行当前语句并推进处理流程。 */
				case <-ctx.Done(): /* 处理当前分支。 */
					break loop /* 执行当前语句并推进处理流程。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	close(jobs)                            /* 执行当前语句并推进处理流程。 */
	wg.Wait()                              /* 执行当前语句并推进处理流程。 */
	elapsed := time.Since(start).Seconds() /* 更新 elapsed 的值。 */
	total := ok.Load() + failed.Load()     /* 更新 total 的值。 */
	errorRate := 0.0                       /* 更新 errorRate 的值。 */
	if total > 0 {                         /* 判断条件并选择处理分支。 */
		errorRate = float64(failed.Load()) / float64(total) /* 更新 errorRate 的值。 */
	} /* 结束当前表达式或代码块。 */
	minimum := *minQPS /* 更新 minimum 的值。 */
	if minimum == 0 {  /* 判断条件并选择处理分支。 */
		minimum = float64(*rate) * 0.9 /* 更新 minimum 的值。 */
	} /* 结束当前表达式或代码块。 */
	achieved := float64(ok.Load()) / elapsed                                        /* 更新 achieved 的值。 */
	p95 := hist.percentile(.95)                                                     /* 更新 p95 的值。 */
	accepted := errorRate <= *maxErrorRate && p95 <= *maxP95 && achieved >= minimum /* 更新 accepted 的值。 */
	message := "accepted"                                                           /* 更新 message 的值。 */
	if !accepted {                                                                  /* 判断条件并选择处理分支。 */
		message = fmt.Sprintf("threshold failed: qps %.0f/%.0f, error %.4f/%.4f, p95 %.0f/%.0fms", achieved, minimum, errorRate, *maxErrorRate, p95, *maxP95) /* 更新 message 的值。 */
	} /* 结束当前表达式或代码块。 */
	report := result{Profile: *profile, Transport: *transport, DurationSec: elapsed, Sent: ok.Load(), Failed: failed.Load(), AchievedQPS: achieved, ErrorRate: errorRate, LatencyP50MS: hist.percentile(.50), LatencyP95MS: p95, LatencyP99MS: hist.percentile(.99), MaximumMS: float64(hist.maximum.Load()), AcceptanceOK: accepted, AcceptanceMsg: message} /* 更新 report 的值。 */
	data, _ := json.MarshalIndent(report, "", "  ")                                                                                                                                                                                                                                                                                                           /* 更新 _ 的值。 */
	fmt.Println(string(data))                                                                                                                                                                                                                                                                                                                                 /* 执行当前语句并推进处理流程。 */
	if *reportPath != "" {                                                                                                                                                                                                                                                                                                                                    /* 判断条件并选择处理分支。 */
		if writeErr := os.WriteFile(*reportPath, data, 0o600); writeErr != nil { /* 判断条件并选择处理分支。 */
			exitError(writeErr.Error()) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if !accepted { /* 判断条件并选择处理分支。 */
		os.Exit(2) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func curveTarget(profile string, elapsed, total time.Duration, normal, burst int, burstWindow, offline time.Duration) int { /* 定义 curveTarget 函数。 */
	seconds := elapsed.Seconds() /* 更新 seconds 的值。 */
	switch profile {             /* 根据条件选择处理路径。 */
	case "burst": /* 处理当前分支。 */
		start := total / 4         /* 更新 start 的值。 */
		end := start + burstWindow /* 更新 end 的值。 */
		if end > total {           /* 判断条件并选择处理分支。 */
			end = total /* 更新 end 的值。 */
		} /* 结束当前表达式或代码块。 */
		if elapsed <= start { /* 判断条件并选择处理分支。 */
			return int(seconds * float64(normal)) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		before := start.Seconds() * float64(normal) /* 更新 before 的值。 */
		if elapsed <= end {                         /* 判断条件并选择处理分支。 */
			return int(before + (elapsed-start).Seconds()*float64(burst)) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return int(before + (end-start).Seconds()*float64(burst) + (elapsed-end).Seconds()*float64(normal)) /* 返回当前处理结果。 */
	case "offline": /* 处理当前分支。 */
		if elapsed <= offline { /* 判断条件并选择处理分支。 */
			return 0 /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		generated := int(seconds * float64(normal))                     /* 更新 generated 的值。 */
		recovery := int((elapsed - offline).Seconds() * float64(burst)) /* 更新 recovery 的值。 */
		if recovery < generated {                                       /* 判断条件并选择处理分支。 */
			return recovery /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return generated /* 返回当前处理结果。 */
	default: /* 处理当前分支。 */
		return int(seconds * float64(normal)) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
func payload(n, devices int, tenant, product string) []byte { /* 定义 payload 函数。 */
	body, _ := json.Marshal(map[string]any{"messageId": fmt.Sprintf("load_%d_%d", time.Now().UnixNano(), n), "tenantId": tenant, "productId": product, "deviceId": fmt.Sprintf("device_%06d", n%devices), "protocol": "json", "payloadFormat": "json", "payload": map[string]any{"properties": map[string]any{"temperature": 20 + n%80, "smoke": n%1000 == 0}, "tags": map[string]string{"cityCode": "city_001", "districtCode": "district_01", "buildingId": fmt.Sprintf("building_%02d", n%100), "deviceType": "smoke"}}}) /* 更新 _ 的值。 */
	return body                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func newMQTTSender(ctx context.Context, platformURL, platformToken, broker, product string) (sender, error) { /* 定义 newMQTTSender 函数。 */
	body, _ := json.Marshal(map[string]string{"productId": product})                                                                                 /* 更新 _ 的值。 */
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(platformURL, "/")+"/api/v1/mqtt/load-token", bytes.NewReader(body)) /* 更新 _ 的值。 */
	req.Header.Set("Content-Type", "application/json")                                                                                               /* 执行当前语句并推进处理流程。 */
	req.Header.Set("Authorization", "Bearer "+platformToken)                                                                                         /* 执行当前语句并推进处理流程。 */
	resp, err := http.DefaultClient.Do(req)                                                                                                          /* 更新 err 的值。 */
	if err != nil {                                                                                                                                  /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer resp.Body.Close()                                         /* 安排函数结束时执行清理。 */
	var auth struct{ Username, Token, PublishTopicPrefix string }   /* 声明 auth。 */
	if err = json.NewDecoder(resp.Body).Decode(&auth); err != nil { /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if resp.StatusCode/100 != 2 { /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("load token HTTP %d", resp.StatusCode) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	opts := mqtt.NewClientOptions().AddBroker(broker).SetClientID(fmt.Sprintf("iot-loadgen-%d", time.Now().UnixNano())).SetUsername(auth.Username).SetPassword(auth.Token).SetOrderMatters(false) /* 更新 opts 的值。 */
	client := mqtt.NewClient(opts)                                                                                                                                                                /* 更新 client 的值。 */
	connect := client.Connect()                                                                                                                                                                   /* 更新 connect 的值。 */
	if !connect.WaitTimeout(15 * time.Second) {                                                                                                                                                   /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("MQTT connect timeout") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if connect.Error() != nil { /* 判断条件并选择处理分支。 */
		return nil, connect.Error() /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return &mqttSender{client: client, prefix: auth.PublishTopicPrefix, qos: 1}, nil /* 返回当前处理结果。 */
}                              /* 结束当前表达式或代码块。 */
func exitError(message string) { fmt.Fprintln(os.Stderr, message); os.Exit(1) } /* 定义 exitError 函数。 */
