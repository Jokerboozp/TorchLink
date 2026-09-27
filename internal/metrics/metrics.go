package metrics

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"
)

type Registry struct {
	info     string
	mu       sync.RWMutex
	counters map[string]uint64
	gauges   map[string]float64
	rates    map[string]*rate
}

type rate struct {
	count, last uint64
	at          time.Time
	value       float64
}

func New() *Registry {
	counters := map[string]uint64{}
	for _, name := range []string{"raw_archive_success_total", "raw_archive_failed_total", "raw_publish_failed_total", "parse_failed_total", "parse_success_total", "alarm_trigger_total", "video_alarm_ingest_total", "video_alarm_failed_total", "video_media_transfer_success_total", "video_media_transfer_failed_total", "ai_analysis_success_total", "ai_analysis_failed_total", "ai_analysis_skipped_total", "ai_analysis_started_total", "ai_analysis_timeout_total", "ai_analysis_skipped_unavailable_total", "ai_analysis_skipped_duplicate_total", "ai_analysis_skipped_resolved_total", "ai_analysis_skipped_cancelled_total", "ai_analysis_skipped_expired_total", "mqtt_archive_receipt_total"} {
		counters[name] = 0
	}
	gauges := map[string]float64{"storage_latency_ms": 0, "mqtt_inflight_messages": 0, "mqtt_subscription_count": 0, "mqtt_ws_client_count": 0, "kafka_lag": 0}
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

// SetProcessInfo exposes process_info{role,instance} 1 so per-instance
// scrapes can be attributed without per-device labels.
func (r *Registry) SetProcessInfo(role, instance string) {
	q := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", "")
	r.mu.Lock()
	r.info = fmt.Sprintf("# TYPE process_info gauge\nprocess_info{role=\"%s\",instance=\"%s\"} 1\n", q.Replace(role), q.Replace(instance))
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
	for k, v := range r.counters {
		fmt.Fprintf(&b, "# TYPE %s counter\n%s %d\n", k, k, v)
	}
	for k, v := range r.gauges {
		fmt.Fprintf(&b, "# TYPE %s gauge\n%s %g\n", k, k, v)
	}
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
