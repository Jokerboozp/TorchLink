package capacity

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// InstanceSample is one scrape of one platform instance. A failed scrape keeps
// OK=false and no values: missing is never reported as zero.
type InstanceSample struct {
	Instance string             `json:"instance"`
	Role     string             `json:"role"`
	OK       bool               `json:"ok"`
	Error    string             `json:"error,omitempty"`
	Values   map[string]float64 `json:"values,omitempty"`
}

// Round is one concurrent scrape of every inventory target.
type Round struct {
	At        int64            `json:"at"` // Unix ms, controller clock
	Instances []InstanceSample `json:"instances"`
}

// Metrics whose value is a consumer-group observation repeated by every
// process; they are de-duplicated with max, never summed across instances.
func groupWide(name string) bool {
	return strings.HasPrefix(name, "kafka_lag") || name == "pipeline_backlog" || name == "ingest_paused"
}

// ParsePrometheus reads the text exposition format, labelled or not.
func ParsePrometheus(r io.Reader) (map[string]float64, error) {
	out := map[string]float64{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// The value follows the series; a label set may contain spaces.
		cut := strings.LastIndexByte(line, '}')
		var name, rest string
		if cut >= 0 {
			name, rest = line[:cut+1], strings.TrimSpace(line[cut+1:])
		} else {
			f := strings.Fields(line)
			if len(f) < 2 {
				continue
			}
			name, rest = f[0], strings.Join(f[1:], " ")
		}
		f := strings.Fields(rest)
		if len(f) == 0 {
			continue
		}
		v, err := strconv.ParseFloat(f[0], 64)
		if err != nil {
			continue
		}
		out[name] = v
	}
	return out, sc.Err()
}

// Collector scrapes every inventory metrics target on a fixed interval and
// appends rounds to observations/metrics.jsonl.
type Collector struct {
	targets  []MetricsTarget
	interval time.Duration
	client   *http.Client
	mu       sync.Mutex
	rounds   []Round
	out      *os.File
	now      func() time.Time
	// reduce shrinks a scrape before it is kept (node-exporter hosts).
	reduce func(map[string]float64) map[string]float64
}

func NewCollector(targets []MetricsTarget, interval time.Duration, path string) (*Collector, error) {
	c := &Collector{targets: targets, interval: interval, client: &http.Client{Timeout: max(interval, 2*time.Second), Transport: &http.Transport{Proxy: nil}}, now: time.Now}
	if path != "" {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
		if err != nil {
			return nil, err
		}
		c.out = f
	}
	return c, nil
}

func (c *Collector) Run(ctx context.Context) {
	t := time.NewTicker(c.interval)
	defer t.Stop()
	c.Scrape(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.Scrape(ctx)
		}
	}
}

func (c *Collector) Close() {
	if c.out != nil {
		c.out.Close()
	}
}

// Scrape performs one round now.
func (c *Collector) Scrape(ctx context.Context) Round {
	round := Round{At: c.now().UnixMilli(), Instances: make([]InstanceSample, len(c.targets))}
	var wg sync.WaitGroup
	for i, t := range c.targets {
		wg.Add(1)
		go func(i int, t MetricsTarget) {
			defer wg.Done()
			s := InstanceSample{Instance: t.Instance, Role: t.Role}
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, t.URL, nil)
			resp, err := c.client.Do(req)
			if err != nil {
				s.Error = ShortError(err)
			} else {
				if resp.StatusCode != 200 {
					s.Error = "status_" + strconv.Itoa(resp.StatusCode)
				} else if v, perr := ParsePrometheus(resp.Body); perr != nil {
					s.Error = "parse"
				} else {
					if c.reduce != nil {
						v = c.reduce(v)
					}
					s.OK, s.Values = true, v
				}
				resp.Body.Close()
			}
			round.Instances[i] = s
		}(i, t)
	}
	wg.Wait()
	c.mu.Lock()
	c.rounds = append(c.rounds, round)
	if c.out != nil {
		b, _ := json.Marshal(round)
		_, _ = c.out.Write(append(b, '\n'))
	}
	c.mu.Unlock()
	return round
}

func (c *Collector) Rounds(from, to int64) []Round {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []Round
	for _, r := range c.rounds {
		if r.At >= from && r.At <= to {
			out = append(out, r)
		}
	}
	return out
}

// LoadRounds reads observations back for offline report regeneration.
func LoadRounds(path string) ([]Round, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Round
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 16<<20), 16<<20)
	for sc.Scan() {
		var r Round
		if json.Unmarshal(sc.Bytes(), &r) == nil {
			out = append(out, r)
		}
	}
	return out, sc.Err()
}

// CounterIncrease sums per-instance increases across the rounds. A value that
// drops is a process restart: the new value is the increase since restart.
// valid is false when any instance lacks two readings of the counter.
func CounterIncrease(rounds []Round, name string) (float64, bool) {
	type state struct {
		last  float64
		seen  int
		total float64
	}
	per := map[string]*state{}
	instances := map[string]bool{}
	for _, r := range rounds {
		for _, s := range r.Instances {
			instances[s.Instance] = true
			v, ok := s.Values[name]
			if !s.OK || !ok || math.IsNaN(v) || math.IsInf(v, 0) {
				continue
			}
			st := per[s.Instance]
			if st == nil {
				per[s.Instance] = &state{last: v, seen: 1}
				continue
			}
			if v >= st.last {
				st.total += v - st.last
			} else {
				st.total += v
			}
			st.last = v
			st.seen++
		}
	}
	if len(instances) == 0 {
		return 0, false
	}
	sum := 0.0
	for inst := range instances {
		st := per[inst]
		if st == nil || st.seen < 2 {
			return 0, false
		}
		sum += st.total
	}
	return sum, true
}

// appearingCounterIncrease sums the increase of a counter that a process only
// exports after its first increment (such as dlq_published_total): a
// successful scrape without the counter reads as zero. It is nil when no
// instance was scraped successfully twice.
func appearingCounterIncrease(rounds []Round, name string) *float64 {
	type state struct {
		last float64
		seen int
	}
	per := map[string]*state{}
	total, observed := 0.0, false
	for _, r := range rounds {
		for _, s := range r.Instances {
			if !s.OK {
				continue
			}
			v := s.Values[name]
			if math.IsNaN(v) || math.IsInf(v, 0) {
				continue
			}
			st := per[s.Instance]
			if st == nil {
				per[s.Instance] = &state{last: v, seen: 1}
				continue
			}
			if v >= st.last {
				total += v - st.last
			} else {
				total += v
			}
			st.last = v
			st.seen++
			observed = true
		}
	}
	if !observed {
		return nil
	}
	return &total
}

// Point is one chartable value; Valid=false marks a gap.
type Point struct {
	At    int64   `json:"at"`
	Value float64 `json:"value"`
	Valid bool    `json:"valid"`
}

// GaugeSeries aggregates a gauge per round: group-wide metrics use the max over
// instances, per-process metrics are summed. A round is a gap when no
// instance reported the metric, or (for summed metrics) any instance scrape failed.
func GaugeSeries(rounds []Round, name string) []Point {
	out := make([]Point, 0, len(rounds))
	for _, r := range rounds {
		p := Point{At: r.At}
		found, failed := false, false
		for _, s := range r.Instances {
			if !s.OK {
				failed = true
				continue
			}
			v, ok := s.Values[name]
			if !ok {
				continue
			}
			if groupWide(name) {
				p.Value = math.Max(p.Value, v)
			} else {
				p.Value += v
			}
			found = true
		}
		p.Valid = found && (groupWide(name) || !failed)
		if !p.Valid {
			p.Value = 0
		}
		out = append(out, p)
	}
	return out
}

// InstanceSeries is one instance's gauge over time.
func InstanceSeries(rounds []Round, instance, name string) []Point {
	out := make([]Point, 0, len(rounds))
	for _, r := range rounds {
		p := Point{At: r.At}
		for _, s := range r.Instances {
			if s.Instance == instance && s.OK {
				p.Value, p.Valid = s.Values[name], hasKey(s.Values, name)
			}
		}
		out = append(out, p)
	}
	return out
}

func hasKey(m map[string]float64, k string) bool { _, ok := m[k]; return ok }

// BacklogSeries is the unfinished pipeline work: the Kafka consumer lag plus
// every gateway's durable MQTT inbox. It is work in flight, not unique messages.
func BacklogSeries(rounds []Round) []Point {
	lag := GaugeSeries(rounds, "kafka_lag")
	inbox := GaugeSeries(rounds, "mqtt_inbox_pending")
	out := make([]Point, len(lag))
	for i := range lag {
		out[i] = Point{At: lag[i].At, Valid: lag[i].Valid, Value: lag[i].Value}
		if inbox[i].Valid {
			out[i].Value += inbox[i].Value
		}
	}
	return out
}

// Trend fits a least-squares line; slope is per second. ok needs three valid
// points spanning at least two intervals.
func Trend(points []Point) (slope float64, n int, ok bool) {
	var xs, ys []float64
	for _, p := range points {
		if p.Valid {
			xs = append(xs, float64(p.At)/1000)
			ys = append(ys, p.Value)
		}
	}
	n = len(xs)
	if n < 3 {
		return 0, n, false
	}
	var mx, my float64
	for i := range xs {
		mx += xs[i]
		my += ys[i]
	}
	mx /= float64(n)
	my /= float64(n)
	var num, den float64
	for i := range xs {
		num += (xs[i] - mx) * (ys[i] - my)
		den += (xs[i] - mx) * (xs[i] - mx)
	}
	if den == 0 {
		return 0, n, false
	}
	return num / den, n, true
}

// NewNodeCollector samples node-exporter hosts, keeping only the values the
// host charts need (CPU, memory, disk busy time, root filesystem, network).
func NewNodeCollector(nodes []NodeTarget, interval time.Duration, path string) (*Collector, error) {
	targets := make([]MetricsTarget, len(nodes))
	for i, n := range nodes {
		targets[i] = MetricsTarget{Role: "node", Instance: n.Name, URL: n.URL}
	}
	c, err := NewCollector(targets, interval, path)
	if err == nil {
		c.reduce = reduceNodeMetrics
	}
	return c, err
}

// seriesLabel returns a label value from a series key such as
// name{a="x",b="y"}.
func seriesLabel(key, label string) string {
	i := strings.Index(key, label+`="`)
	if i < 0 {
		return ""
	}
	rest := key[i+len(label)+2:]
	if j := strings.IndexByte(rest, '"'); j >= 0 {
		return rest[:j]
	}
	return ""
}

func physicalDisk(dev string) bool {
	for _, p := range []string{"loop", "ram", "sr", "fd", "dm-", "md", "zram", "nbd"} {
		if strings.HasPrefix(dev, p) {
			return false
		}
	}
	return dev != ""
}

func reduceNodeMetrics(in map[string]float64) map[string]float64 {
	out := map[string]float64{}
	for key, v := range in {
		name, _, _ := strings.Cut(key, "{")
		switch name {
		case "node_cpu_seconds_total":
			out["cpu_total"] += v
			if seriesLabel(key, "mode") == "idle" || seriesLabel(key, "mode") == "iowait" {
				out["cpu_idle"] += v
			}
		case "node_memory_MemTotal_bytes":
			out["mem_total"] = v
		case "node_memory_MemAvailable_bytes":
			out["mem_available"] = v
		case "node_disk_io_time_seconds_total":
			if dev := seriesLabel(key, "device"); physicalDisk(dev) {
				out["disk_io:"+dev] = v
			}
		case "node_filesystem_avail_bytes", "node_filesystem_size_bytes":
			if seriesLabel(key, "mountpoint") == "/" {
				out[strings.TrimPrefix(name, "node_filesystem_")+"_root"] = v
			}
		case "node_network_receive_bytes_total", "node_network_transmit_bytes_total":
			if dev := seriesLabel(key, "device"); dev != "lo" && !strings.HasPrefix(dev, "veth") && !strings.HasPrefix(dev, "docker") && !strings.HasPrefix(dev, "br-") {
				out[strings.TrimSuffix(strings.TrimPrefix(name, "node_network_"), "_total")] += v
			}
		}
	}
	return out
}

// HostPoint is one host's utilisation between two node samples.
type HostPoint struct {
	At         int64
	CPU        float64 // busy %, excluding idle and iowait
	Memory     float64 // used % (MemTotal - MemAvailable)
	Disk       float64 // busiest physical disk busy %
	CPUValid   bool
	MemValid   bool
	DiskValid  bool
	NetMBps    float64
	NetValid   bool
	RootFreePc float64
}

// HostSeries derives per-host utilisation from reduced node rounds.
func HostSeries(rounds []Round) map[string][]HostPoint {
	out := map[string][]HostPoint{}
	prev := map[string]InstanceSample{}
	prevAt := map[string]int64{}
	for _, r := range rounds {
		for _, s := range r.Instances {
			p := HostPoint{At: r.At}
			if s.OK {
				v := s.Values
				if v["mem_total"] > 0 {
					p.Memory, p.MemValid = 100*(1-v["mem_available"]/v["mem_total"]), true
				}
				if v["size_bytes_root"] > 0 {
					p.RootFreePc = 100 * v["avail_bytes_root"] / v["size_bytes_root"]
				}
				if last, ok := prev[s.Instance]; ok && last.OK {
					dt := float64(r.At-prevAt[s.Instance]) / 1000
					if dTotal := v["cpu_total"] - last.Values["cpu_total"]; dTotal > 0 {
						p.CPU, p.CPUValid = math.Max(0, math.Min(100, 100*(1-(v["cpu_idle"]-last.Values["cpu_idle"])/dTotal))), true
					}
					if dt > 0 {
						for k, x := range v {
							if strings.HasPrefix(k, "disk_io:") {
								if d := x - last.Values[k]; d >= 0 {
									p.Disk, p.DiskValid = math.Max(p.Disk, math.Min(100, 100*d/dt)), true
								}
							}
						}
						rx := v["receive_bytes"] - last.Values["receive_bytes"]
						tx := v["transmit_bytes"] - last.Values["transmit_bytes"]
						if rx >= 0 && tx >= 0 {
							p.NetMBps, p.NetValid = (rx+tx)/dt/1e6, true
						}
					}
				}
				prev[s.Instance], prevAt[s.Instance] = s, r.At
			} else {
				delete(prev, s.Instance)
			}
			out[s.Instance] = append(out[s.Instance], p)
		}
	}
	return out
}
