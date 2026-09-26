package metrics

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

type Registry struct {
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
	for _, name := range []string{"raw_archive_success_total", "raw_archive_failed_total", "raw_publish_failed_total", "parse_failed_total", "parse_success_total", "alarm_trigger_total", "video_alarm_ingest_total", "video_alarm_failed_total", "video_media_transfer_success_total", "video_media_transfer_failed_total", "ai_analysis_success_total", "ai_analysis_failed_total", "ai_analysis_skipped_total"} {
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
func (r *Registry) Add(name string, v uint64)  { r.mu.Lock(); r.counters[name] += v; r.mu.Unlock() }
func (r *Registry) Set(name string, v float64) { r.mu.Lock(); r.gauges[name] = v; r.mu.Unlock() }
func (r *Registry) ObserveMS(name string, start time.Time) {
	r.Set(name, float64(time.Since(start).Microseconds())/1000)
}
func (r *Registry) Prometheus() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var b strings.Builder
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
	return b.String()
}
