package metrics

import (
	"fmt"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Registry struct {
	info       string
	mu         sync.RWMutex
	counters   map[string]uint64
	gauges     map[string]float64
	rates      map[string]*rate
	histograms map[string]*histogram
}

// histogramBuckets are the default upper bounds, in seconds; they suit AI
// runs, which take seconds to minutes.
var histogramBuckets = []float64{1, 2, 5, 10, 20, 30, 60, 120, 300, 600}

// RequestBuckets suit HTTP requests, which mostly take milliseconds.
var RequestBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

type histogram struct {
	bounds []float64
	counts []uint64
	sum    float64
	count  uint64
}

// Series returns a metric name with labels, such as
// ai_run_total{workflow="x",status="y"}; Inc and Add accept it.
func Series(name string, labels ...string) string {
	if len(labels) < 2 {
		return name
	}
	q := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", "")
	parts := make([]string, 0, len(labels)/2)
	for i := 0; i+1 < len(labels); i += 2 {
		parts = append(parts, labels[i]+`="`+q.Replace(labels[i+1])+`"`)
	}
	return name + "{" + strings.Join(parts, ",") + "}"
}

// Observe records value (seconds) in the histogram series name, which may
// carry labels from Series.
func (r *Registry) Observe(name string, value float64) {
	r.ObserveIn(name, histogramBuckets, value)
}

// ObserveIn is Observe with the bucket bounds of a new series; one metric
// name should always use the same bounds.
func (r *Registry) ObserveIn(name string, bounds []float64, value float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.histograms == nil {
		r.histograms = map[string]*histogram{}
	}
	h := r.histograms[name]
	if h == nil {
		h = &histogram{bounds: bounds, counts: make([]uint64, len(bounds))}
		r.histograms[name] = h
	}
	for i, bound := range h.bounds {
		if value <= bound {
			h.counts[i]++
		}
	}
	h.sum += value
	h.count++
}

// splitSeries separates a series into its metric name and label list.
func splitSeries(series string) (string, string) {
	if i := strings.IndexByte(series, '{'); i > 0 && strings.HasSuffix(series, "}") {
		return series[:i], series[i+1 : len(series)-1]
	}
	return series, ""
}

// writeSamples prints series grouped by metric name, one TYPE line per name.
func writeSamples[T any](b *strings.Builder, kind string, values map[string]T, format func(T) string) {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	typed := map[string]bool{}
	for _, series := range names {
		base, _ := splitSeries(series)
		if !typed[base] {
			typed[base] = true
			fmt.Fprintf(b, "# TYPE %s %s\n", base, kind)
		}
		fmt.Fprintf(b, "%s %s\n", series, format(values[series]))
	}
}

type rate struct {
	count, last uint64
	at          time.Time
	value       float64
}

func New() *Registry {
	counters := map[string]uint64{}
	for _, name := range []string{"raw_archive_success_total", "raw_archive_failed_total", "raw_publish_failed_total", "parse_failed_total", "parse_success_total", "alarm_trigger_total", "video_alarm_ingest_total", "video_alarm_failed_total", "video_media_transfer_success_total", "video_media_transfer_failed_total", "ai_analysis_success_total", "ai_analysis_failed_total", "ai_analysis_timeout_total", "mqtt_archive_receipt_total", "dlq_published_total", "retention_failed_total", "retention_deleted_total", "notification_sent_total", "notification_failed_total", "event_publish_failed_total", "audit_write_failed_total"} {
		counters[name] = 0
	}
	gauges := map[string]float64{"raw_publish_stalled": 0, "storage_latency_ms": 0, "mqtt_inflight_messages": 0, "mqtt_subscription_count": 0, "mqtt_ws_client_count": 0, "kafka_lag": 0}
	return &Registry{counters: counters, gauges: gauges, rates: map[string]*rate{"mqtt_ingest_qps": {at: time.Now()}}}
}
func (r *Registry) Inc(name string) {
	r.mu.Lock()
	if v, ok := r.rates[name]; ok {
		v.count++
	} else {
		r.counters[name]++
	}
	r.mu.Unlock()
}

// SetProcessInfo exposes process_info{role,instance,version} 1 so
// per-instance scrapes can be attributed without per-device labels.
func (r *Registry) SetProcessInfo(role, instance, version string) {
	q := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", "")
	r.mu.Lock()
	r.info = fmt.Sprintf("# TYPE process_info gauge\nprocess_info{role=\"%s\",instance=\"%s\",version=\"%s\"} 1\n", q.Replace(role), q.Replace(instance), q.Replace(version))
	r.mu.Unlock()
}
func (r *Registry) Add(name string, v uint64)  { r.mu.Lock(); r.counters[name] += v; r.mu.Unlock() }
func (r *Registry) Set(name string, v float64) { r.mu.Lock(); r.gauges[name] = v; r.mu.Unlock() }
func (r *Registry) ObserveMS(name string, start time.Time) {
	r.Set(name, float64(time.Since(start).Microseconds())/1000)
}
func (r *Registry) Prometheus() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var b strings.Builder
	b.WriteString(r.info)
	writeSamples(&b, "counter", r.counters, func(v uint64) string { return strconv.FormatUint(v, 10) })
	writeSamples(&b, "gauge", r.gauges, func(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) })
	r.writeHistograms(&b)
	now := time.Now()
	for name, value := range r.rates {
		if elapsed := now.Sub(value.at).Seconds(); elapsed > 0 {
			value.value = float64(value.count-value.last) / elapsed
			value.last = value.count
			value.at = now
		}
		fmt.Fprintf(&b, "# TYPE %s gauge\n%s %g\n", name, name, value.value)
	}
	// Process gauges let capacity runs chart each instance's resources.
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	fmt.Fprintf(&b, "# TYPE go_goroutines gauge\ngo_goroutines %d\n", runtime.NumGoroutine())
	fmt.Fprintf(&b, "# TYPE go_memstats_heap_inuse_bytes gauge\ngo_memstats_heap_inuse_bytes %d\n", mem.HeapInuse)
	fmt.Fprintf(&b, "# TYPE go_memstats_sys_bytes gauge\ngo_memstats_sys_bytes %d\n", mem.Sys)
	return b.String()
}

func (r *Registry) writeHistograms(b *strings.Builder) {
	names := make([]string, 0, len(r.histograms))
	for name := range r.histograms {
		names = append(names, name)
	}
	sort.Strings(names)
	typed := map[string]bool{}
	for _, series := range names {
		base, labels := splitSeries(series)
		if !typed[base] {
			typed[base] = true
			fmt.Fprintf(b, "# TYPE %s histogram\n", base)
		}
		prefix := ""
		if labels != "" {
			prefix = labels + ","
		}
		h := r.histograms[series]
		for i, bound := range h.bounds {
			fmt.Fprintf(b, "%s_bucket{%sle=\"%g\"} %d\n", base, prefix, bound, h.counts[i])
		}
		fmt.Fprintf(b, "%s_bucket{%sle=\"+Inf\"} %d\n", base, prefix, h.count)
		suffix := ""
		if labels != "" {
			suffix = "{" + labels + "}"
		}
		fmt.Fprintf(b, "%s_sum%s %g\n%s_count%s %d\n", base, suffix, h.sum, base, suffix, h.count)
	}
}
