package metrics /* 声明 metrics 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"fmt"     /* 执行当前语句并推进处理流程。 */
	"strings" /* 执行当前语句并推进处理流程。 */
	"sync"    /* 执行当前语句并推进处理流程。 */
	"time"    /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type Registry struct { /* 定义 Registry 类型。 */
	mu       sync.RWMutex       /* 执行当前语句并推进处理流程。 */
	counters map[string]uint64  /* 执行当前语句并推进处理流程。 */
	gauges   map[string]float64 /* 执行当前语句并推进处理流程。 */
	rates    map[string]*rate   /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type rate struct { /* 定义 rate 类型。 */
	count, last uint64    /* 执行当前语句并推进处理流程。 */
	at          time.Time /* 执行当前语句并推进处理流程。 */
	value       float64   /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func New() *Registry { /* 定义 New 函数。 */
	counters := map[string]uint64{}                                                                                                                                                                                                                                                                                                                                                      /* 更新 counters 的值。 */
	for _, name := range []string{"raw_archive_success_total", "raw_archive_failed_total", "raw_publish_failed_total", "parse_failed_total", "parse_success_total", "alarm_trigger_total", "video_alarm_ingest_total", "video_alarm_failed_total", "video_media_transfer_success_total", "video_media_transfer_failed_total", "ai_analysis_success_total", "ai_analysis_failed_total"} { /* 循环处理当前数据。 */
		counters[name] = 0 /* 更新 counters[name] 的值。 */
	} /* 结束当前表达式或代码块。 */
	gauges := map[string]float64{"storage_latency_ms": 0, "mqtt_inflight_messages": 0, "mqtt_subscription_count": 0, "mqtt_ws_client_count": 0, "kafka_lag": 0} /* 更新 gauges 的值。 */
	return &Registry{counters: counters, gauges: gauges, rates: map[string]*rate{"mqtt_ingest_qps": {at: time.Now()}}}                                          /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Registry) Inc(name string) { /* 定义 Inc 函数。 */
	r.mu.Lock()                     /* 执行当前语句并推进处理流程。 */
	if v, ok := r.rates[name]; ok { /* 判断条件并选择处理分支。 */
		v.count++ /* 执行当前语句并推进处理流程。 */
	} else { /* 结束当前表达式或代码块。 */
		r.counters[name]++ /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	r.mu.Unlock() /* 执行当前语句并推进处理流程。 */
}                                              /* 结束当前表达式或代码块。 */
func (r *Registry) Add(name string, v uint64)  { r.mu.Lock(); r.counters[name] += v; r.mu.Unlock() } /* 定义 Add 函数。 */
func (r *Registry) Set(name string, v float64) { r.mu.Lock(); r.gauges[name] = v; r.mu.Unlock() }    /* 定义 Set 函数。 */
func (r *Registry) ObserveMS(name string, start time.Time) { /* 定义 ObserveMS 函数。 */
	r.Set(name, float64(time.Since(start).Microseconds())/1000) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (r *Registry) Prometheus() string { /* 定义 Prometheus 函数。 */
	r.mu.Lock()                    /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()            /* 安排函数结束时执行清理。 */
	var b strings.Builder          /* 声明 b。 */
	for k, v := range r.counters { /* 循环处理当前数据。 */
		fmt.Fprintf(&b, "# TYPE %s counter\n%s %d\n", k, k, v) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	for k, v := range r.gauges { /* 循环处理当前数据。 */
		fmt.Fprintf(&b, "# TYPE %s gauge\n%s %g\n", k, k, v) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	now := time.Now()                  /* 更新 now 的值。 */
	for name, value := range r.rates { /* 循环处理当前数据。 */
		if elapsed := now.Sub(value.at).Seconds(); elapsed > 0 { /* 判断条件并选择处理分支。 */
			value.value = float64(value.count-value.last) / elapsed /* 更新 value.value 的值。 */
			value.last = value.count                                /* 更新 value.last 的值。 */
			value.at = now                                          /* 更新 value.at 的值。 */
		} /* 结束当前表达式或代码块。 */
		fmt.Fprintf(&b, "# TYPE %s gauge\n%s %g\n", name, name, value.value) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	return b.String() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
