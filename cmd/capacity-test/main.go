// Command capacity-test drives step loads against a running platform and
// records, per step, client-side results together with the platform's own
// pipeline counters (archived / parsed per second and Kafka backlog). It is the
// tool behind docs/CAPACITY_TEST_REPORT.md; scenarios and flags are described
// in docs/CAPACITY_TEST_PLAN.md.
//
// Modes:
//
//	provision  enroll standard-protocol devices and save their credentials
//	ingest     device-credential HTTP reports (closed-loop -levels or open-loop -rates)
//	http       any management API with a bearer token
//	mqtttok    fetch device MQTT tokens for a credentials file
//	mqttconn   open and hold MQTT connections step by step
//	mqttpub    publish device reports over MQTT at open-loop rates
//	tcp        GB26875 devices over TCP, one connection per device
//	canary     one report per second, printing ingest and end-to-end parse latency
//	sample     print selected /metrics values as CSV
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	mrand "math/rand/v2"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Device is one enrolled device with its platform credential.
type Device struct {
	Tenant  string `json:"tenant"`
	Product string `json:"product"`
	Device  string `json:"device"`
	Key     string `json:"key"`
	Secret  string `json:"secret"`
}

// Level is the result of one load step.
type Level struct {
	Label       string         `json:"label"`
	Concurrency int            `json:"concurrency"`
	TargetRate  float64        `json:"targetRate,omitempty"`
	DurationSec float64        `json:"durationSec"`
	Total       int64          `json:"total"`
	OK          int64          `json:"ok"`
	Fail        int64          `json:"fail"`
	Codes       map[string]int `json:"codes"`
	QPS         float64        `json:"okQps"`
	ErrRate     float64        `json:"errRate"`
	P50         float64        `json:"p50ms"`
	P95         float64        `json:"p95ms"`
	P99         float64        `json:"p99ms"`
	Max         float64        `json:"maxms"`
	Bytes       int64          `json:"bytes"`
	ProbeP95    float64        `json:"probeP95ms"`
	ProbeFail   int64          `json:"probeFail"`
	ProbeN      int64          `json:"probeN"`
	Note        string         `json:"note,omitempty"`
	LagStart    float64        `json:"lagStart"`
	LagEnd      float64        `json:"lagEnd"`
	StorageLag  float64        `json:"storageLagEnd"`
	ArchivedPS  float64        `json:"archivedPerSec"`
	ParsedPS    float64        `json:"parsedPerSec"`
	AlarmsPS    float64        `json:"alarmsPerSec"`
}

var (
	base      = flag.String("base", "http://127.0.0.1:8081", "platform base URL")
	token     = flag.String("token", "", "management bearer token, or @file")
	mode      = flag.String("mode", "http", "provision|ingest|http|mqtttok|mqttconn|mqttpub|tcp|canary|sample")
	method    = flag.String("method", "GET", "http: request method")
	paths     = flag.String("path", "/api/v1/devices?page=1&pageSize=20", "http: request path(s), '|' separated and used round robin; {n} {id} {ts} {r} are substituted")
	bodyT     = flag.String("body", "", "http: request body template, or @file")
	levelsS   = flag.String("levels", "1,4,16,64,256", "closed-loop concurrency per step")
	ratesS    = flag.String("rates", "", "open-loop target rates (requests/s) per step; overrides -levels")
	stepDur   = flag.Duration("step", 20*time.Second, "duration of each step")
	timeout   = flag.Duration("timeout", 10*time.Second, "request timeout")
	stopErr   = flag.Float64("stop-err", 0.5, "stop after a step whose error rate exceeds this")
	stopDrop  = flag.Float64("stop-drop", 0.3, "stop after a step whose ok QPS falls below this fraction of the peak (0 disables)")
	outFile   = flag.String("out", "", "result file (JSON); mqtttok writes tokens here")
	name      = flag.String("name", "run", "scenario name printed with results")
	devFile   = flag.String("devices", "", "device credentials file (JSON) written by -mode provision")
	kind      = flag.String("kind", "property", "ingest: standard message kind")
	alarmFrac = flag.Float64("alarm-frac", 0, "fraction of reports with stressAlarm=1 (others carry 0) or, for tcp, fire-alarm frames")
	fields    = flag.Int("fields", 6, "telemetry fields per report")
	probe     = flag.String("probe", "/api/v1/devices?page=1&pageSize=20", "management path requested every 500ms during each step ('' disables)")
	keepAlive = flag.Bool("keepalive", true, "reuse HTTP connections")
	maxConns  = flag.Int("max-conns", 0, "HTTP connections per host (0 = step concurrency + 16)")
	sampleInt = flag.Duration("interval", 5*time.Second, "sample: interval")
)

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func readArg(s string) string {
	if strings.HasPrefix(s, "@") {
		b, err := os.ReadFile(s[1:])
		must(err)
		return strings.TrimSpace(string(b))
	}
	return s
}

func ints(s string) []int {
	var out []int
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			n, err := strconv.Atoi(p)
			must(err)
			out = append(out, n)
		}
	}
	return out
}

func rid() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

var seq atomic.Int64

func subst(t string) string {
	if !strings.Contains(t, "{") {
		return t
	}
	r := strings.NewReplacer("{n}", strconv.FormatInt(seq.Add(1), 10), "{id}", rid(), "{ts}", strconv.FormatInt(time.Now().UnixMilli(), 10), "{r}", strconv.Itoa(mrand.IntN(1000000)))
	return r.Replace(t)
}

// rec keeps counters and a bounded reservoir of latencies for one step.
type rec struct {
	mu    sync.Mutex
	lat   []float64
	seen  int64
	ok    atomic.Int64
	fail  atomic.Int64
	bytes atomic.Int64
	codes sync.Map
}

const reservoirSize = 200000

func (r *rec) add(ms float64, ok bool, code string, n int64) {
	if ok {
		r.ok.Add(1)
	} else {
		r.fail.Add(1)
	}
	r.bytes.Add(n)
	v, _ := r.codes.LoadOrStore(code, new(atomic.Int64))
	v.(*atomic.Int64).Add(1)
	r.mu.Lock()
	r.seen++
	if len(r.lat) < reservoirSize {
		r.lat = append(r.lat, ms)
	} else if j := mrand.Int64N(r.seen); j < reservoirSize {
		r.lat[j] = ms
	}
	r.mu.Unlock()
}

func pct(s []float64, q float64) float64 {
	if len(s) == 0 {
		return 0
	}
	i := int(math.Ceil(q*float64(len(s)))) - 1
	if i < 0 {
		i = 0
	}
	return math.Round(s[i]*10) / 10
}

func (r *rec) level(label string, conc int, rate float64, d time.Duration) Level {
	r.mu.Lock()
	s := append([]float64(nil), r.lat...)
	r.mu.Unlock()
	sort.Float64s(s)
	codes := map[string]int{}
	r.codes.Range(func(k, v any) bool { codes[k.(string)] = int(v.(*atomic.Int64).Load()); return true })
	ok, fail := r.ok.Load(), r.fail.Load()
	l := Level{Label: label, Concurrency: conc, TargetRate: rate, DurationSec: d.Seconds(), Total: ok + fail, OK: ok, Fail: fail, Codes: codes, QPS: math.Round(float64(ok)/d.Seconds()*10) / 10, Bytes: r.bytes.Load()}
	if l.Total > 0 {
		l.ErrRate = math.Round(float64(fail)/float64(l.Total)*10000) / 10000
	}
	l.P50, l.P95, l.P99 = pct(s, .5), pct(s, .95), pct(s, .99)
	if len(s) > 0 {
		l.Max = s[len(s)-1]
	}
	return l
}

func httpClient(conc int) *http.Client {
	mc := *maxConns
	if mc == 0 {
		mc = conc + 16
	}
	return &http.Client{Timeout: *timeout, Transport: &http.Transport{
		Proxy:               nil,
		DialContext:         (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		MaxIdleConns:        mc * 2,
		MaxIdleConnsPerHost: mc,
		MaxConnsPerHost:     mc,
		IdleConnTimeout:     60 * time.Second,
		DisableKeepAlives:   !*keepAlive,
		DisableCompression:  true,
	}}
}

// reqFn performs one request of a step and classifies its result.
type reqFn func(ctx context.Context, c *http.Client, worker int) (ok bool, code string, n int64)

func doReq(ctx context.Context, c *http.Client, m, url string, body []byte, hdr map[string]string) (bool, string, int64) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, m, url, rd)
	if err != nil {
		return false, "build", 0
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		return false, shortErr(err.Error()), 0
	}
	n, _ := io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp.StatusCode/100 == 2 || resp.StatusCode == http.StatusNotModified, strconv.Itoa(resp.StatusCode), n
}

func shortErr(e string) string {
	switch {
	case strings.Contains(e, "not Authorized") || strings.Contains(e, "bad user name"):
		return "auth"
	case strings.Contains(e, "Timeout") || strings.Contains(e, "timeout") || strings.Contains(e, "deadline"):
		return "timeout"
	case strings.Contains(e, "refused"):
		return "refused"
	case strings.Contains(e, "reset"):
		return "reset"
	case strings.Contains(e, "EOF"):
		return "eof"
	case strings.Contains(e, "assign requested address"):
		return "ephemeral-ports"
	case strings.Contains(e, "too many open files"):
		return "fd"
	}
	if len(e) > 40 {
		e = e[:40]
	}
	return e
}

// prober measures management-side responsiveness while a step runs.
type prober struct {
	mu   sync.Mutex
	lat  []float64
	fail atomic.Int64
	n    atomic.Int64
}

func (p *prober) run(ctx context.Context) {
	if *probe == "" {
		return
	}
	c := &http.Client{Timeout: *timeout, Transport: &http.Transport{Proxy: nil, MaxIdleConnsPerHost: 4}}
	hdr := map[string]string{"Authorization": "Bearer " + *token}
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			go func() {
				st := time.Now()
				ok, _, _ := doReq(context.Background(), c, http.MethodGet, *base+*probe, nil, hdr)
				p.n.Add(1)
				if !ok {
					p.fail.Add(1)
				}
				p.mu.Lock()
				p.lat = append(p.lat, float64(time.Since(st).Microseconds())/1000)
				p.mu.Unlock()
			}()
		}
	}
}

// runLevels runs fn step by step, closed loop (-levels) or open loop (-rates),
// and stops early when errors or throughput collapse.
func runLevels(fn reqFn) []Level {
	var out []Level
	peak := 0.0
	levels, rates := ints(*levelsS), ints(*ratesS)
	openLoop := len(rates) > 0
	steps := len(levels)
	if openLoop {
		steps = len(rates)
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	for i := 0; i < steps; i++ {
		conc, rate := 0, 0.0
		if openLoop {
			rate = float64(rates[i])
			conc = int(math.Max(64, math.Min(4096, rate*timeout.Seconds()/4)))
		} else {
			conc = levels[i]
		}
		c := httpClient(conc)
		r := &rec{}
		p := &prober{}
		ctx, cancel := context.WithTimeout(context.Background(), *stepDur)
		pctx, pcancel := context.WithCancel(context.Background())
		go p.run(pctx)
		var tokens chan struct{}
		if openLoop {
			tokens = make(chan struct{}, conc*4)
			go func() {
				const tick = 10 * time.Millisecond
				per, acc := rate*tick.Seconds(), 0.0
				t := time.NewTicker(tick)
				defer t.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-t.C:
						for acc += per; acc >= 1; acc-- {
							select {
							case tokens <- struct{}{}:
							default:
								// Every worker is still busy: the target rate is not being met.
								r.add(0, false, "gen-backlog", 0)
							}
						}
					}
				}
			}()
		}
		m0 := scrape()
		start := time.Now()
		var wg sync.WaitGroup
		for w := 0; w < conc; w++ {
			wg.Add(1)
			go func(w int) {
				defer wg.Done()
				for ctx.Err() == nil {
					if openLoop {
						select {
						case <-ctx.Done():
							return
						case <-tokens:
						}
					}
					st := time.Now()
					rctx, rc := context.WithTimeout(context.Background(), *timeout)
					ok, code, n := fn(rctx, c, w)
					rc()
					r.add(float64(time.Since(st).Microseconds())/1000, ok, code, n)
				}
			}(w)
		}
		interrupted := false
		select {
		case <-ctx.Done():
		case <-sig:
			interrupted = true
			cancel()
		}
		wg.Wait()
		cancel()
		pcancel()
		el := time.Since(start)
		label := fmt.Sprintf("c=%d", conc)
		if openLoop {
			label = fmt.Sprintf("rate=%d", int(rate))
		}
		l := r.level(label, conc, rate, el)
		m1 := scrape()
		perSec := func(k string) float64 { return math.Round((m1[k]-m0[k])/el.Seconds()*10) / 10 }
		l.LagStart, l.LagEnd, l.StorageLag = m0["kafka_lag"], m1["kafka_lag"], m1["kafka_lag_storage"]
		l.ArchivedPS, l.ParsedPS, l.AlarmsPS = perSec("raw_archive_success_total"), perSec("parse_success_total"), perSec("alarm_trigger_total")
		p.mu.Lock()
		ps := append([]float64(nil), p.lat...)
		p.mu.Unlock()
		sort.Float64s(ps)
		l.ProbeP95, l.ProbeFail, l.ProbeN = pct(ps, .95), p.fail.Load(), p.n.Load()
		out = append(out, l)
		c.CloseIdleConnections()
		fmt.Printf("[%s] %-10s ok=%-8d fail=%-7d qps=%-9.1f err=%.3f p50=%.1f p95=%.1f p99=%.1f max=%.0f probeP95=%.1f probeFail=%d/%d | archived/s=%.0f parsed/s=%.0f alarms/s=%.1f lag %.0f->%.0f (storage %.0f) codes=%v\n",
			*name, label, l.OK, l.Fail, l.QPS, l.ErrRate, l.P50, l.P95, l.P99, l.Max, l.ProbeP95, l.ProbeFail, l.ProbeN, l.ArchivedPS, l.ParsedPS, l.AlarmsPS, l.LagStart, l.LagEnd, l.StorageLag, l.Codes)
		save(out)
		peak = math.Max(peak, l.QPS)
		if interrupted {
			break
		}
		if l.ErrRate > *stopErr {
			fmt.Printf("[%s] stop: error rate %.3f > %.2f\n", *name, l.ErrRate, *stopErr)
			break
		}
		if *stopDrop > 0 && peak > 0 && l.QPS < peak**stopDrop {
			fmt.Printf("[%s] stop: qps %.1f < %.0f%% of peak %.1f\n", *name, l.QPS, *stopDrop*100, peak)
			break
		}
		time.Sleep(3 * time.Second)
	}
	return out
}

func save(v any) {
	if *outFile == "" {
		return
	}
	b, _ := json.MarshalIndent(map[string]any{"name": *name, "mode": *mode, "path": *paths, "method": *method, "levels": v, "at": time.Now().Format(time.RFC3339)}, "", "  ")
	_ = os.WriteFile(*outFile, b, 0o644)
}

// scrape reads the platform's unlabelled /metrics values.
func scrape() map[string]float64 {
	out := map[string]float64{}
	c := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil}}
	resp, err := c.Get(*base + "/metrics")
	if err != nil {
		return out
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && !strings.HasPrefix(f[0], "#") {
			v, _ := strconv.ParseFloat(f[1], 64)
			out[f[0]] = v
		}
	}
	return out
}

// sample prints selected /metrics values as CSV until interrupted.
func sample() {
	keys := []string{"raw_archive_success_total", "raw_archive_failed_total", "raw_publish_failed_total", "parse_success_total", "parse_failed_total", "alarm_trigger_total", "kafka_lag", "kafka_lag_parser", "kafka_lag_storage", "kafka_lag_state", "kafka_lag_ai", "mqtt_inbox_pending", "mqtt_inbox_rejected", "mqtt_ingest_qps", "ai_analysis_failed_total", "storage_latency_ms"}
	fmt.Println("time," + strings.Join(keys, ","))
	for {
		m := scrape()
		row := []string{time.Now().Format("15:04:05")}
		for _, k := range keys {
			if len(m) == 0 {
				row = append(row, "ERR")
			} else {
				row = append(row, strconv.FormatFloat(m[k], 'f', -1, 64))
			}
		}
		fmt.Println(strings.Join(row, ","))
		time.Sleep(*sampleInt)
	}
}

func loadDevices() []Device {
	b, err := os.ReadFile(*devFile)
	must(err)
	var d []Device
	must(json.Unmarshal(b, &d))
	if len(d) == 0 {
		must(fmt.Errorf("no devices in %s", *devFile))
	}
	return d
}

// telemetry builds a standard report body; stressAlarm drives the test alarm rule.
func telemetry(alarm bool) map[string]any {
	names := []string{"temperature", "humidity", "pressure", "smokeDensity", "voltage", "current", "signal", "battery", "flow", "level"}
	data := map[string]any{"stressAlarm": 0}
	for i := 0; i < *fields && i < len(names); i++ {
		data[names[i]] = math.Round(mrand.Float64()*1000) / 10
	}
	if alarm {
		data["stressAlarm"] = 1
	}
	return data
}

func main() {
	flag.Parse()
	*token = readArg(*token)
	switch *mode {
	case "http":
		ps := strings.Split(*paths, "|")
		bt := readArg(*bodyT)
		hdr := map[string]string{"Authorization": "Bearer " + *token}
		var rr atomic.Int64
		save(runLevels(func(ctx context.Context, c *http.Client, _ int) (bool, string, int64) {
			var body []byte
			if bt != "" {
				body = []byte(subst(bt))
			}
			return doReq(ctx, c, *method, *base+subst(ps[int(rr.Add(1))%len(ps)]), body, hdr)
		}))
	case "ingest":
		devs := loadDevices()
		var rr atomic.Int64
		save(runLevels(func(ctx context.Context, c *http.Client, _ int) (bool, string, int64) {
			d := devs[int(rr.Add(1))%len(devs)]
			b, _ := json.Marshal(map[string]any{"id": "st-" + rid(), "timestamp": time.Now().UnixMilli(), "data": telemetry(mrand.Float64() < *alarmFrac)})
			url := fmt.Sprintf("%s/api/v1/device-ingest/standard/%s/%s/%s/%s", *base, d.Tenant, d.Product, d.Device, *kind)
			return doReq(ctx, c, http.MethodPost, url, b, map[string]string{"X-Device-Key": d.Key, "X-Device-Secret": d.Secret})
		}))
	case "provision":
		provision()
	case "mqtttok":
		mqttTokens()
	case "mqttconn":
		mqttConn()
	case "mqttpub":
		mqttPub()
	case "tcp":
		tcpMode()
	case "canary":
		canary()
	case "sample":
		sample()
	default:
		must(fmt.Errorf("unknown mode %q", *mode))
	}
}
